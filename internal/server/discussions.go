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

// PreviewInput 表达任务和选定材料；专用会话自动读取讨论，正文须明确选择。
type PreviewInput struct {
	Text      string   `json:"text"`
	Documents []string `json:"documents"`
	ThreadID  string   `json:"threadId"`
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
func (s *Server) createDiscussion(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DiscussionID string `json:"discussionId"`
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
		if thread.ID == input.DiscussionID {
			session, err := m.CreateDiscussion(r.Context(), r.PathValue("id"), thread.ID, thread.DocumentID)
			if err != nil {
				fail(w, 409, err.Error())
				return
			}
			respond(w, 201, session)
			return
		}
	}
	fail(w, 404, "讨论不存在")
}
func (s *Server) buildPreviewLocked(input PreviewInput, m *agent.Manager) (PreviewResult, error) {
	result := PreviewResult{Mode: "普通消息"}
	if s.project == nil {
		return result, errors.New("请先打开项目")
	}
	if strings.TrimSpace(input.Text) == "" || len(input.Text) > 2*1024*1024 || len(input.Documents) > 20 {
		return result, errors.New("请输入任务，选定文档最多 20 份")
	}
	var session agent.Session
	var baseline *agent.DiscussionContext
	if input.SessionID != "" {
		var err error
		session, err = m.Get(input.SessionID)
		if err != nil {
			return result, err
		}
		if session.DiscussionID != "" {
			if input.ThreadID != "" && input.ThreadID != session.DiscussionID {
				return result, errors.New("请选择该会话所属讨论")
			}
			input.ThreadID = session.DiscussionID
			baseline = session.DiscussionContext
			if baseline != nil && baseline.ThreadID != session.ThreadID {
				baseline = nil
			}
			if session.ContextUncertain && !input.Full {
				return result, errors.New("上次送达结果不明，请勾选完整重发")
			}
		} else if input.ThreadID != "" {
			return result, errors.New("请使用独立讨论专用会话")
		}
	}
	result.Baseline = agent.ContextHash(session.DiscussionContext)
	var prompt strings.Builder
	prompt.WriteString(input.Text)
	var thread *project.Thread
	if input.ThreadID != "" {
		state, err := s.project.Collaboration()
		if err != nil {
			return result, err
		}
		for i := range state.Threads {
			if state.Threads[i].ID == input.ThreadID {
				thread = &state.Threads[i]
				break
			}
		}
		if thread == nil {
			return result, errors.New("讨论不存在")
		}
		full := input.Full || baseline == nil
		result.Mode = "增量"
		if full {
			result.Mode = "完整"
		}
		snapshot := &agent.DiscussionContext{ThreadID: session.ThreadID, DiscussionID: thread.ID, DocumentID: thread.DocumentID, AnchorHash: valueHash(thread.Anchor), Messages: map[string]string{}, Full: full}
		if baseline != nil {
			snapshot.DocumentRevision = baseline.DocumentRevision
		}
		result.Context = snapshot
		fmt.Fprintf(&prompt, "\n\n--- %s讨论 %s · 文档 %s ---\n", result.Mode, thread.ID, thread.DocumentID)
		if full || baseline.AnchorHash != snapshot.AnchorHash {
			if thread.Anchor != nil {
				fmt.Fprintf(&prompt, "引用（rev. %d）：%s\n", thread.Anchor.Revision, thread.Anchor.Quote)
			} else {
				prompt.WriteString("整篇讨论，无段落引用。\n")
			}
		}
		for _, message := range thread.Messages {
			hash := messageHash(message)
			snapshot.Messages[message.ID] = hash
			if !full && baseline.Messages[message.ID] == hash {
				continue
			}
			// 自身回复只有与回写原文一致时才能跳过；导入后修改的消息仍须送达。
			own := false
			if !full && message.Source != nil && message.Source.SessionID == session.ID && message.Source.ThreadID == session.ThreadID {
				for _, reply := range session.Replies {
					if reply.ThreadID == message.Source.ThreadID && reply.TurnID == message.Source.TurnID && reply.ItemID == message.Source.ItemID && reply.Body == message.Body && message.Author == session.Name && message.Role == "agent" && message.Intent == "comment" {
						own = true
						break
					}
				}
			}
			if own {
				continue
			}
			fmt.Fprintf(&prompt, "\n[%s · %s · %s · %s]\n%s\n", message.Author, message.Role, message.Intent, message.ID, message.Body)
		}
		if !full {
			deleted := []string{}
			for id := range baseline.Messages {
				if _, exists := snapshot.Messages[id]; !exists {
					deleted = append(deleted, id)
				}
			}
			sort.Strings(deleted)
			for _, id := range deleted {
				fmt.Fprintf(&prompt, "\n讨论消息已删除：%s\n", id)
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
			if result.Mode == "增量" && baseline.DocumentRevision == doc.Revision {
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
			source := &project.MessageSource{SessionID: session.ID, ThreadID: reply.ThreadID, TurnID: reply.TurnID, ItemID: reply.ItemID}
			id := "agent-" + valueHash(source)
			var savedMessage project.Message
			discarded := false
			s.mu.Lock()
			if s.project == nil {
				s.mu.Unlock()
				return
			}
			state, err := s.project.Collaboration()
			found := false
			if err == nil {
				for i := range state.Threads {
					thread := &state.Threads[i]
					if thread.ID != reply.DiscussionID {
						continue
					}
					found = true
					for _, message := range thread.Messages {
						if message.ID == id || message.Source != nil && *message.Source == *source {
							savedMessage = message
							break
						}
					}
					if savedMessage.ID == "" {
						savedMessage = project.Message{ID: id, Author: session.Name, Role: "agent", Intent: "comment", Body: reply.Body, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Source: source}
						thread.Messages = append(thread.Messages, savedMessage)
						_, err = s.project.SaveCollaboration(state.Generation, state.Threads)
					}
					break
				}
				if !found {
					discarded = true
				}
			}
			s.mu.Unlock()
			errorText := ""
			if err != nil {
				errorText = err.Error()
			}
			if discarded {
				errorText = "目标讨论已删除；回复保留在会话中"
			}
			if finishErr := m.FinishReply(session.ID, reply.ThreadID, reply.TurnID, reply.ItemID, savedMessage.ID, messageHash(project.Message{Author: session.Name, Role: "agent", Intent: "comment", Body: reply.Body}), errorText, discarded); finishErr != nil {
				return
			}
		}
	}
}
