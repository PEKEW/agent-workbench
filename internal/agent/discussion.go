package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
)

// DiscussionBaseline 记录一条讨论已送达的引用与消息版本。
type DiscussionBaseline struct {
	AnchorHash string            `json:"anchorHash"`
	Messages   map[string]string `json:"messages"`
}

// DiscussionContext 保存已预览材料的版本，送达后作为该文档讨论会话的增量同步基线。
type DiscussionContext struct {
	InputID           string                        `json:"inputId,omitempty"`
	TurnID            string                        `json:"turnId,omitempty"`
	DeliveredSequence int                           `json:"deliveredSequence,omitempty"`
	ThreadID          string                        `json:"threadId"`
	DocumentID        string                        `json:"documentId"`
	DocumentRevision  string                        `json:"documentRevision"`
	Discussions       map[string]DiscussionBaseline `json:"discussions"`
	Selected          []string                      `json:"selected"`
	Full              bool                          `json:"full,omitempty"`
}

// DiscussionReply 记录可重试的最终答复；Discussions 为本回合需要回应的讨论，回写据此分配。
type DiscussionReply struct {
	DocumentID  string   `json:"documentId"`
	Discussions []string `json:"discussions"`
	ThreadID    string   `json:"threadId"`
	TurnID      string   `json:"turnId"`
	ItemID      string   `json:"itemId"`
	Body        string   `json:"body"`
	Phase       string   `json:"phase,omitempty"`
	Synced      bool     `json:"synced"`
	Discarded   bool     `json:"discarded,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// WrittenReply 是一段回复写回讨论后的消息 ID 与内容哈希。
type WrittenReply struct {
	Discussion string
	MessageID  string
	Hash       string
}

// ContextHash 生成确定的基线摘要，防止同会话并发发送过期预览。
func ContextHash(snapshot *DiscussionContext) string {
	data, _ := json.Marshal(snapshot)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ErrUnroutedReply 表示多条讨论的回复缺少可识别的讨论标题，不能确定归属。
var ErrUnroutedReply = errors.New("回复未按「### 讨论 <ID>」分段，无法确定归属，未自动写回；可在会话中查看并手动加入讨论")

// RouteReply 按「讨论 <ID>」标题把最终回复分配到本回合需要回应的讨论；只列出一条讨论且没有标题时整条归属该讨论。
func RouteReply(body string, discussions []string) (map[string]string, error) {
	known := map[string]bool{}
	for _, id := range discussions {
		known[id] = true
	}
	sections, current, marked := map[string]*strings.Builder{}, "", false
	for _, line := range strings.SplitAfter(body, "\n") {
		if id := replyHeading(line); known[id] {
			current, marked = id, true
			if sections[id] == nil {
				sections[id] = &strings.Builder{}
			}
			continue
		}
		if current != "" {
			sections[current].WriteString(line)
		}
	}
	result := map[string]string{}
	if !marked {
		if len(discussions) == 1 && strings.TrimSpace(body) != "" {
			result[discussions[0]] = strings.TrimSpace(body)
			return result, nil
		}
		return nil, ErrUnroutedReply
	}
	for id, section := range sections {
		if text := strings.TrimSpace(section.String()); text != "" {
			result[id] = text
		}
	}
	if len(result) == 0 {
		return nil, ErrUnroutedReply
	}
	return result, nil
}

// replyHeading 识别 Markdown 标题形式的「讨论 <ID>」，ID 后可跟「·」或空白分隔的说明。
func replyHeading(line string) string {
	text := strings.TrimSpace(line)
	if !strings.HasPrefix(text, "#") {
		return ""
	}
	text = strings.TrimSpace(strings.TrimLeft(text, "#"))
	text, found := strings.CutPrefix(text, "讨论")
	if !found {
		return ""
	}
	text = strings.TrimLeft(text, " \t:：")
	if cut := strings.IndexAny(text, " \t·"); cut >= 0 {
		text = text[:cut]
	}
	return strings.Trim(text, "「」`*")
}

// CreateDiscussion 创建或返回父工作会话在指定文档上的讨论专用会话；同一文档的全部讨论共用该会话。
func (m *Manager) CreateDiscussion(ctx context.Context, parentID, documentID string) (Session, error) {
	if !fs.ValidPath(documentID) || documentID == "." {
		return Session{}, errors.New("文档标识无效")
	}
	parent, err := m.Get(parentID)
	if err != nil {
		return Session{}, err
	}
	options := parent.Options
	if parent.Model != "" {
		options.Model = parent.Model
	}
	options.ResumeThreadID = ""
	options.Name = "讨论 · " + parent.Name
	if len([]rune(options.Name)) > 80 {
		options.Name = string([]rune(options.Name)[:80])
	}
	return m.create(ctx, options, parentID, documentID)
}

// SendDiscussion 按预览基线接收讨论输入；排队不推进已送达状态。
func (m *Manager) SendDiscussion(ctx context.Context, id, text string, snapshot DiscussionContext, baseline string) (SendResult, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return SendResult{}, err
	}
	if len(data) > 2*1024*1024 || snapshot.Discussions == nil || len(snapshot.Selected) == 0 {
		return SendResult{}, errors.New("讨论同步记录无效或超过 2 MiB")
	}
	var copy DiscussionContext
	_ = json.Unmarshal(data, &copy)
	return m.send(ctx, id, text, &copy, baseline)
}

func (m *Manager) deliverDiscussionLocked(r *runtimeSession, input Input, turnID string) {
	if input.Context == nil {
		return
	}
	if r.DiscussionContext != nil && r.DiscussionContext.InputID == input.ID && r.DiscussionContext.TurnID == turnID {
		return
	}
	data, _ := json.Marshal(input.Context)
	var copy DiscussionContext
	_ = json.Unmarshal(data, &copy)
	copy.InputID = input.ID
	copy.TurnID = turnID
	copy.DeliveredSequence = r.Sequence
	r.DiscussionContext = &copy
	r.ContextUncertain = false
}

// observeDiscussionLocked 同时供实时事件和完整日志恢复使用。
func (m *Manager) observeDiscussionLocked(r *runtimeSession, method string, params json.RawMessage) {
	if r.ParentSessionID == "" {
		return
	}
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Input    Input  `json:"input"`
		Queued   bool   `json:"queued"`
		Turn     struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
		Item struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Text  string `json:"text"`
			Phase string `json:"phase"`
		} `json:"item"`
	}
	if json.Unmarshal(params, &p) != nil || p.ThreadID != "" && p.ThreadID != r.ThreadID {
		return
	}
	switch method {
	case "turn/started", "workbench/inputDelivered":
		turnID := p.Turn.ID
		if method == "workbench/inputDelivered" {
			turnID = p.TurnID
		}
		if r.ActiveInput != nil && r.ActiveInput.Context != nil && turnID != "" && (r.ActiveTurnID == "" || r.ActiveTurnID == turnID) {
			m.deliverDiscussionLocked(r, *r.ActiveInput, turnID)
		}
	case "item/completed":
		if p.Item.Type != "agentMessage" || p.Item.ID == "" || strings.TrimSpace(p.Item.Text) == "" || p.Item.Phase == "commentary" || r.ActiveInput == nil || r.ActiveInput.Context == nil || p.TurnID == "" || p.TurnID != r.ActiveTurnID {
			return
		}
		if r.DraftReply != nil && r.DraftReply.Phase == "final_answer" && p.Item.Phase != "final_answer" {
			return
		}
		r.DraftReply = &DiscussionReply{DocumentID: r.DocumentID, Discussions: append([]string{}, r.ActiveInput.Context.Selected...), ThreadID: r.ThreadID, TurnID: p.TurnID, ItemID: p.Item.ID, Body: p.Item.Text, Phase: p.Item.Phase}
	case "turn/completed":
		if p.Turn.ID == "" || p.Turn.ID != r.ActiveTurnID {
			return
		}
		if p.Turn.Status == "completed" && r.DraftReply != nil && r.DraftReply.TurnID == p.Turn.ID {
			exists := false
			for _, reply := range r.Replies {
				if reply.ThreadID == r.DraftReply.ThreadID && reply.TurnID == p.Turn.ID && reply.ItemID == r.DraftReply.ItemID {
					exists = true
					break
				}
			}
			if !exists {
				r.Replies = append(r.Replies, *r.DraftReply)
			}
		}
		r.DraftReply = nil
	}
}

// FinishReply 登记幂等回写结果，失败保留待重试记录。
func (m *Manager) FinishReply(id, threadID, turnID, itemID string, written []WrittenReply, syncError string, discarded bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.sessions[id]
	if r == nil {
		return errors.New("会话不存在")
	}
	for i := range r.Replies {
		reply := &r.Replies[i]
		if reply.ThreadID != threadID || reply.TurnID != turnID || reply.ItemID != itemID {
			continue
		}
		reply.Error = syncError
		reply.Discarded = discarded
		reply.Synced = syncError == "" && !discarded
		if reply.Synced && r.DiscussionContext != nil && r.DiscussionContext.ThreadID == threadID {
			for _, w := range written {
				if baseline, ok := r.DiscussionContext.Discussions[w.Discussion]; ok && baseline.Messages != nil {
					baseline.Messages[w.MessageID] = w.Hash
				}
			}
		}
		return m.persistLocked(r)
	}
	return errors.New("待同步回复不存在")
}

func (m *Manager) restoreDiscussionLocked(r *runtimeSession) error {
	if r.ParentSessionID == "" {
		return nil
	}
	// 日志是完整输入和回合的依据；不依赖网页缓存或最后 1000 条事件。
	scratch := &runtimeSession{Session: Session{ParentSessionID: r.ParentSessionID, DocumentID: r.DocumentID, ThreadID: r.ThreadID}, completedTurns: map[string]bool{}}
	inputs := map[string]Input{}
	_, err := m.readEventsObserved(r.ID, 0, 0, func(event Event) {
		scratch.Sequence = event.Sequence
		var p struct {
			Input   Input  `json:"input"`
			InputID string `json:"inputId"`
			TurnID  string `json:"turnId"`
			Turn    struct {
				ID string `json:"id"`
			} `json:"turn"`
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(event.Params, &p)
		if p.ThreadID != "" && p.ThreadID != scratch.ThreadID {
			return
		}
		if event.Method == "turn/started" && scratch.completedTurns[p.Turn.ID] {
			return
		}
		if event.Method == "workbench/inputAccepted" && p.Input.Context != nil {
			inputs[p.Input.ID] = p.Input
		}
		if event.Method == "workbench/turnSubmitted" {
			if input, ok := inputs[p.InputID]; ok {
				scratch.ActiveInput = &input
				scratch.ActiveTurnID = ""
				delete(inputs, p.InputID)
			}
		}
		if event.Method == "workbench/inputDelivered" && scratch.ActiveInput != nil && scratch.ActiveInput.ID == p.InputID {
			scratch.ActiveTurnID = p.TurnID
		}
		if event.Method == "turn/started" && scratch.ActiveInput != nil && (scratch.ActiveTurnID == "" || scratch.ActiveTurnID == p.Turn.ID) {
			scratch.ActiveTurnID = p.Turn.ID
		}
		m.observeDiscussionLocked(scratch, event.Method, event.Params)
		if event.Method == "turn/completed" && p.Turn.ID == scratch.ActiveTurnID {
			scratch.completedTurns[p.Turn.ID] = true
			scratch.ActiveInput = nil
			scratch.ActiveTurnID = ""
		}
		if event.Method == "workbench/queueCleared" {
			inputs = map[string]Input{}
		}
	})
	if err != nil {
		return err
	}
	for _, reply := range scratch.Replies {
		exists := false
		for _, saved := range r.Replies {
			if saved.ThreadID == reply.ThreadID && saved.TurnID == reply.TurnID && saved.ItemID == reply.ItemID {
				exists = true
				break
			}
		}
		if !exists {
			r.Replies = append(r.Replies, reply)
		}
	}
	// 保留已持久化的已送达状态；只从日志补齐最后一次确认后的丢失状态。
	if scratch.DiscussionContext != nil && (r.DiscussionContext == nil || scratch.DiscussionContext.DeliveredSequence > r.DiscussionContext.DeliveredSequence) {
		r.DiscussionContext = scratch.DiscussionContext
	}
	if r.ActiveInput != nil && r.ActiveInput.Context != nil {
		r.ContextUncertain = true
	}
	r.DraftReply = nil
	return nil
}
