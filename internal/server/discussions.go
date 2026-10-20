package server

import (
	"agent-workbench/internal/agent"
	"agent-workbench/internal/project"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// PreviewInput 表达任务和选定材料；文档讨论会话自动读取所列讨论，正文须明确选择。
type PreviewInput struct {
	Text      string   `json:"text"`
	Documents []string `json:"documents"`
	ThreadIDs []string `json:"threadIds"`
	SessionID string   `json:"sessionId"`
	Full      bool     `json:"full"`
}

// PreviewResult 含精确输入和材料摘要，发送时重新核验。
type PreviewResult struct {
	Text     string                   `json:"text"`
	Checksum string                   `json:"checksum"`
	Baseline string                   `json:"baseline"`
	Mode     string                   `json:"mode"`
	Context  *agent.DiscussionContext `json:"context,omitempty"`
}

func valueHash(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func messageHash(message project.Message) string {
	return valueHash(struct{ Author, Role, Intent, Body string }{message.Author, message.Role, message.Intent, message.Body})
}

// replyMessageID 为同一原生回复写入不同讨论的各段生成稳定且互不相同的消息 ID。
func replyMessageID(source project.MessageSource, discussion string) string {
	return "agent-" + valueHash(struct {
		Source     project.MessageSource
		Discussion string
	}{source, discussion})
}

func (s *Server) createDiscussion(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DocumentID string `json:"documentId"`
	}
	if !decodeAgent(w, r, &input) {
		return
	}
	m, err := s.manager(false)
	if err != nil || m == nil {
		fail(w, 400, "请先创建工作会话")
		return
	}
	s.mu.RLock()
	state, err := s.project.Collaboration()
	s.mu.RUnlock()
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	for _, thread := range state.Threads {
		if thread.DocumentID == input.DocumentID {
			session, err := m.CreateDiscussion(r.Context(), r.PathValue("id"), input.DocumentID)
			if err != nil {
				fail(w, 409, err.Error())
				return
			}
			respond(w, 201, session)
			return
		}
	}
	fail(w, 404, "该文档没有讨论")
}

// writeDiscussion 输出一条讨论相对基线的变化，并返回该讨论的新基线。
func writeDiscussion(prompt *strings.Builder, thread project.Thread, previous *agent.DiscussionBaseline, session agent.Session) agent.DiscussionBaseline {
	next := agent.DiscussionBaseline{AnchorHash: valueHash(thread.Anchor), Messages: map[string]string{}}
	kind, status := "批注", "待处理"
	if thread.Kind == "memo" {
		kind = "备忘"
	}
	if thread.Status == "resolved" {
		status = "已解决"
	}
	var body strings.Builder
	if previous == nil || previous.AnchorHash != next.AnchorHash {
		if thread.Anchor != nil {
			fmt.Fprintf(&body, "引用（rev. %d）：%s\n", thread.Anchor.Revision, thread.Anchor.Quote)
		} else {
			body.WriteString("整篇讨论，无段落引用。\n")
		}
	}
	for _, message := range thread.Messages {
		hash := messageHash(message)
		next.Messages[message.ID] = hash
		if previous != nil && previous.Messages[message.ID] == hash {
			continue
		}
		// 自身回复只有与回写原文一致时才能跳过；导入后修改的消息仍须送达。
		if previous != nil && ownReply(message, thread.ID, session) {
			continue
		}
		fmt.Fprintf(&body, "\n[%s · %s · %s · %s]\n%s\n", message.Author, message.Role, message.Intent, message.ID, message.Body)
	}
	if previous != nil {
		deleted := []string{}
		for id := range previous.Messages {
			if _, exists := next.Messages[id]; !exists {
				deleted = append(deleted, id)
			}
		}
		sort.Strings(deleted)
		for _, id := range deleted {
			fmt.Fprintf(&body, "\n讨论消息已删除：%s\n", id)
		}
	}
	fmt.Fprintf(prompt, "\n=== 讨论 %s · %s · %s ===\n", thread.ID, kind, status)
	if body.Len() == 0 {
		prompt.WriteString("无新消息。\n")
	} else {
		prompt.WriteString(body.String())
	}
	return next
}

func ownReply(message project.Message, discussion string, session agent.Session) bool {
	source := message.Source
	if source == nil || source.SessionID != session.ID || source.ThreadID != session.ThreadID || message.Author != session.Name || message.Role != "agent" || message.Intent != "comment" {
		return false
	}
	for _, reply := range session.Replies {
		if reply.ThreadID != source.ThreadID || reply.TurnID != source.TurnID || reply.ItemID != source.ItemID {
			continue
		}
		sections, err := agent.RouteReply(reply.Body, reply.Discussions)
		return err == nil && sections[discussion] == message.Body
	}
	return false
}

func (s *Server) buildPreviewLocked(input PreviewInput, m *agent.Manager) (PreviewResult, error) {
	result := PreviewResult{Mode: "普通消息"}
	if s.project == nil {
		return result, errors.New("请先打开项目")
	}
	if strings.TrimSpace(input.Text) == "" || len(input.Text) > 2*1024*1024 || len(input.Documents) > 20 || len(input.ThreadIDs) > 200 {
		return result, errors.New("请输入任务，选定文档最多 20 份，讨论最多 200 条")
	}
	var session agent.Session
	var baseline *agent.DiscussionContext
	if input.SessionID != "" {
		var err error
		session, err = m.Get(input.SessionID)
		if err != nil {
			return result, err
		}
		// 讨论专用会话按其文档同步；其他会话只在明确列出讨论时接收材料，按讨论 ID 增量。
		if session.ParentSessionID != "" || len(input.ThreadIDs) > 0 {
			baseline = session.DiscussionContext
			if baseline != nil && (baseline.ThreadID != session.ThreadID || baseline.Discussions == nil) {
				baseline = nil
			}
			if session.ContextUncertain && !input.Full {
				return result, errors.New("上次送达结果不明，请勾选完整重发")
			}
		}
	} else if len(input.ThreadIDs) > 0 {
		return result, errors.New("请选择接收讨论的会话")
	}
	result.Baseline = agent.ContextHash(session.DiscussionContext)
	var prompt strings.Builder
	prompt.WriteString(input.Text)
	bound := session.ParentSessionID != ""
	if bound || len(input.ThreadIDs) > 0 {
		state, err := s.project.Collaboration()
		if err != nil {
			return result, err
		}
		requested := map[string]bool{}
		for _, id := range input.ThreadIDs {
			requested[id] = true
		}
		explicit := len(requested) > 0
		documentID := session.DocumentID
		if !bound {
			for _, thread := range state.Threads {
				if requested[thread.ID] {
					documentID = thread.DocumentID
					break
				}
			}
		}
		threads, current := []project.Thread{}, map[string]bool{}
		for _, thread := range state.Threads {
			// 非专用会话的已送达记录可能跨文档，按讨论 ID 保留全部仍存在的讨论。
			if !bound {
				current[thread.ID] = true
			}
			if thread.DocumentID != documentID {
				continue
			}
			current[thread.ID] = true
			if explicit && requested[thread.ID] || !explicit && thread.Status == "open" {
				threads = append(threads, thread)
			}
		}
		if explicit && len(threads) != len(requested) {
			if !bound {
				return result, errors.New("所选讨论不存在或不属于同一文档")
			}
			return result, errors.New("所选讨论不存在或不属于该会话的文档")
		}
		if len(threads) == 0 {
			return result, errors.New("该文档没有待处理的讨论")
		}
		full := input.Full || baseline == nil
		result.Mode = "增量"
		if full {
			result.Mode = "完整"
		}
		snapshot := &agent.DiscussionContext{ThreadID: session.ThreadID, DocumentID: documentID, Discussions: map[string]agent.DiscussionBaseline{}, Selected: []string{}, Full: full}
		if baseline != nil {
			if baseline.DocumentID == documentID {
				snapshot.DocumentRevision = baseline.DocumentRevision
			}
			if !full {
				for id, previous := range baseline.Discussions {
					if current[id] {
						snapshot.Discussions[id] = previous
					}
				}
			}
		}
		result.Context = snapshot
		fmt.Fprintf(&prompt, "\n\n--- %s文档讨论 · %s · %d 条 ---\n", result.Mode, documentID, len(threads))
		if len(threads) > 1 {
			prompt.WriteString("请逐条回应以下讨论。每条回应单独成段，以「### 讨论 <ID>」开头，ID 原样保留；无需回应的讨论可以省略。\n")
		}
		for _, thread := range threads {
			var previous *agent.DiscussionBaseline
			if saved, ok := snapshot.Discussions[thread.ID]; ok && !full {
				previous = &saved
			}
			snapshot.Discussions[thread.ID] = writeDiscussion(&prompt, thread, previous, session)
			snapshot.Selected = append(snapshot.Selected, thread.ID)
		}
		if !full && bound {
			deleted := []string{}
			for id := range baseline.Discussions {
				if !current[id] {
					deleted = append(deleted, id)
				}
			}
			sort.Strings(deleted)
			for _, id := range deleted {
				fmt.Fprintf(&prompt, "\n讨论已删除：%s\n", id)
			}
		}
	}
	seen := map[string]bool{}
	for _, path := range input.Documents {
		if seen[path] {
			continue
		}
		seen[path] = true
		doc, err := s.project.DocumentState(path)
		if err != nil {
			return result, err
		}
		if result.Context != nil && path == result.Context.DocumentID {
			result.Context.DocumentRevision = doc.Revision
			if result.Mode == "增量" && baseline.DocumentID == path && baseline.DocumentRevision == doc.Revision {
				continue
			}
		}
		fmt.Fprintf(&prompt, "\n\n--- 选定文档 %s · SHA-256 %s ---\n%s", path, doc.Revision, doc.Markdown)
	}
	if prompt.Len() > 2*1024*1024 {
		return result, errors.New("输入与上下文超过 2 MiB")
	}
	result.Text = prompt.String()
	data, _ := json.Marshal(struct {
		Input          PreviewInput
		Text, Baseline string
		Context        *agent.DiscussionContext
	}{input, result.Text, result.Baseline, result.Context})
	mac := hmac.New(sha256.New, []byte(s.token))
	_, _ = mac.Write(data)
	result.Checksum = hex.EncodeToString(mac.Sum(nil))
	return result, nil
}
func (s *Server) discussionWorker(m *agent.Manager, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.syncDiscussionReplies(m)
		}
	}
}
func (s *Server) syncDiscussionReplies(m *agent.Manager) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	for _, session := range m.List() {
		for _, reply := range session.Replies {
			if reply.Synced || reply.Discarded {
				continue
			}
			source := project.MessageSource{SessionID: session.ID, ThreadID: reply.ThreadID, TurnID: reply.TurnID, ItemID: reply.ItemID}
			sections, err := agent.RouteReply(reply.Body, reply.Discussions)
			if err != nil {
				if m.FinishReply(session.ID, reply.ThreadID, reply.TurnID, reply.ItemID, nil, err.Error(), true) != nil {
					return
				}
				continue
			}
			written := []agent.WrittenReply{}
			s.mu.Lock()
			if s.project == nil {
				s.mu.Unlock()
				return
			}
			state, err := s.project.Collaboration()
			if err == nil {
				changed := false
				for _, id := range reply.Discussions {
					body, ok := sections[id]
					if !ok {
						continue
					}
					for i := range state.Threads {
						thread := &state.Threads[i]
						if thread.ID != id {
							continue
						}
						messageID := replyMessageID(source, id)
						saved := project.Message{}
						for _, message := range thread.Messages {
							if message.ID == messageID {
								saved = message
								break
							}
						}
						if saved.ID == "" {
							copied := source
							saved = project.Message{ID: messageID, Author: session.Name, Role: "agent", Intent: "comment", Body: body, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Source: &copied}
							thread.Messages = append(thread.Messages, saved)
							changed = true
						}
						written = append(written, agent.WrittenReply{Discussion: id, MessageID: saved.ID, Hash: messageHash(project.Message{Author: session.Name, Role: "agent", Intent: "comment", Body: body})})
						break
					}
				}
				if changed {
					_, err = s.project.SaveCollaboration(state.Generation, state.Threads)
				}
			}
			s.mu.Unlock()
			errorText, discarded := "", false
			if err != nil {
				errorText = err.Error()
			} else if len(written) == 0 {
				errorText, discarded = "目标讨论已删除；回复保留在会话中", true
			}
			if m.FinishReply(session.ID, reply.ThreadID, reply.TurnID, reply.ItemID, written, errorText, discarded) != nil {
				return
			}
		}
	}
}
