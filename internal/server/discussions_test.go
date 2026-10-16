package server

import (
	"agent-workbench/internal/agent"
	"agent-workbench/internal/project"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func discussionServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "project")
	p, err := project.Create(dir, "讨论测试")
	if err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../scripts/fixtures/codex-runtime.mjs")
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "runtime.mjs")
	if err = os.WriteFile(fixture, append([]byte("#!"+node+"\n"), data...), 0700); err != nil {
		t.Fatal(err)
	}
	s := NewWithCodex(testToken, p, fixture)
	t.Cleanup(func() { s.Close() })
	return s, dir
}
func jsonRequest(t *testing.T, s *Server, path string, input, output any) int {
	t.Helper()
	body, _ := json.Marshal(input)
	w := request(t, s, "POST", path, string(body), testToken, "")
	if output != nil && w.Code < 400 {
		if err := json.Unmarshal(w.Body.Bytes(), output); err != nil {
			t.Fatal(err)
		}
	}
	return w.Code
}
func createDiscussionFixture(t *testing.T, s *Server) (agent.Session, agent.Session) {
	t.Helper()
	var parent, child agent.Session
	if code := jsonRequest(t, s, "/api/agents", map[string]string{"name": "工作"}, &parent); code != 201 {
		t.Fatal("create parent", code)
	}
	s.mu.Lock()
	state, err := s.project.Collaboration()
	if err == nil {
		_, err = s.project.SaveCollaboration(state.Generation, []project.Thread{{ID: "D", DocumentID: "PLAN.md", Kind: "comment", Status: "open", Messages: []project.Message{{ID: "C1", Author: "我", Role: "human", Intent: "comment", Body: "第一条 **意见**", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}}})
	}
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if code := jsonRequest(t, s, "/api/agents/"+parent.ID+"/discussion", map[string]string{"documentId": "PLAN.md"}, &child); code != 201 {
		t.Fatal("create discussion", code)
	}
	return parent, child
}
func TestDiscussionPreviewIncrementalAndBackgroundWriteback(t *testing.T) {
	s, _ := discussionServer(t)
	parent, child := createDiscussionFixture(t, s)
	if parent.ThreadID == child.ThreadID || child.ParentSessionID != parent.ID {
		t.Fatal("not independent native threads")
	}
	input := PreviewInput{Text: "回应", SessionID: child.ID, ThreadIDs: []string{"D"}, Documents: []string{"PLAN.md"}}
	var preview PreviewResult
	if code := jsonRequest(t, s, "/api/agents/preview", input, &preview); code != 200 || !strings.Contains(preview.Text, "第一条") || !strings.Contains(preview.Text, "选定文档 PLAN.md") {
		t.Fatal("first preview", code, preview)
	}
	var result agent.SendResult
	if code := jsonRequest(t, s, "/api/agents/"+child.ID+"/input", map[string]any{"preview": input, "checksum": preview.Checksum}, &result); code != 200 {
		t.Fatal("send", code)
	}
	deadline := time.Now().Add(5 * time.Second)
	var state project.CollaborationState
	for time.Now().Before(deadline) {
		s.mu.RLock()
		state, _ = s.project.Collaboration()
		s.mu.RUnlock()
		if len(state.Threads[0].Messages) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(state.Threads[0].Messages) != 2 || state.Threads[0].Messages[1].Source == nil {
		t.Fatal("headless reply not synchronized", state.Threads)
	}
	s.mu.Lock()
	state.Threads[0].Messages = append(state.Threads[0].Messages, project.Message{ID: "C2", Author: "我", Role: "human", Intent: "comment", Body: "第二条意见", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	_, err := s.project.SaveCollaboration(state.Generation, state.Threads)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if code := jsonRequest(t, s, "/api/agents/preview", input, &preview); code != 200 || !strings.Contains(preview.Text, "第二条意见") || strings.Contains(preview.Text, "第一条") || strings.Contains(preview.Text, "协议测试回复") || strings.Contains(preview.Text, "选定文档") {
		t.Fatal("not incremental", code, preview.Text)
	}
	s.syncDiscussionReplies(s.agents)
	s.mu.RLock()
	state, _ = s.project.Collaboration()
	s.mu.RUnlock()
	if len(state.Threads[0].Messages) != 3 {
		t.Fatal("duplicate synchronization")
	}
	// 来源哈希及并发修改参与预览校验。
	s.mu.Lock()
	state.Threads[0].Messages[2].Body = "修改后的意见"
	_, err = s.project.SaveCollaboration(state.Generation, state.Threads)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if code := jsonRequest(t, s, "/api/agents/"+child.ID+"/input", map[string]any{"preview": input, "checksum": preview.Checksum}, nil); code != 409 {
		t.Fatal("stale preview accepted", code)
	}
	if code := jsonRequest(t, s, "/api/agents/"+child.ID+"/input", map[string]string{"text": "bypass"}, nil); code != 409 {
		t.Fatal("child bypass accepted", code)
	}
}

func waitReply(t *testing.T, s *Server, id string, predicate func(agent.Session) bool) agent.Session {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, err := s.agents.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if predicate(current) {
			return current
		}
		time.Sleep(20 * time.Millisecond)
	}
	current, _ := s.agents.Get(id)
	t.Fatalf("reply timeout: %+v", current)
	return current
}
func TestDiscussionDocumentEditsDeletionAndFullResend(t *testing.T) {
	s, dir := discussionServer(t)
	_, child := createDiscussionFixture(t, s)
	input := PreviewInput{Text: "回应", SessionID: child.ID, Documents: []string{"PLAN.md"}}
	var preview PreviewResult
	if jsonRequest(t, s, "/api/agents/preview", input, &preview) != 200 {
		t.Fatal("preview")
	}
	if jsonRequest(t, s, "/api/agents/"+child.ID+"/input", map[string]any{"preview": input, "checksum": preview.Checksum}, nil) != 200 {
		t.Fatal("send")
	}
	waitReply(t, s, child.ID, func(a agent.Session) bool { return len(a.Replies) == 1 && a.Replies[0].Synced })
	s.mu.Lock()
	state, err := s.project.Collaboration()
	if err == nil {
		state.Threads[0].Messages = state.Threads[0].Messages[1:]
		state.Threads[0].Messages[0].Body = "编辑过的 Agent 回复"
		_, err = s.project.SaveCollaboration(state.Generation, state.Threads)
	}
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "PLAN.md"), []byte("# 新正文版本\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if code := jsonRequest(t, s, "/api/agents/preview", input, &preview); code != 200 || preview.Mode != "增量" || !strings.Contains(preview.Text, "讨论消息已删除：C1") || !strings.Contains(preview.Text, "编辑过的 Agent 回复") || !strings.Contains(preview.Text, "新正文版本") {
		t.Fatal("change not included", code, preview.Text)
	}
	input.Full = true
	if code := jsonRequest(t, s, "/api/agents/preview", input, &preview); code != 200 || preview.Mode != "完整" || !strings.Contains(preview.Text, "编辑过的 Agent 回复") {
		t.Fatal("full resend", code, preview.Text)
	}
	if jsonRequest(t, s, "/api/agents/"+child.ID+"/input", map[string]any{"preview": input, "checksum": preview.Checksum}, nil) != 200 {
		t.Fatal("send after edits")
	}
	s.mu.Lock()
	state, err = s.project.Collaboration()
	if err == nil {
		_, err = s.project.SaveCollaboration(state.Generation, []project.Thread{})
	}
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	current := waitReply(t, s, child.ID, func(a agent.Session) bool { return len(a.Replies) == 2 && a.Replies[1].Discarded })
	if current.LastTurnStatus != "completed" || current.Replies[1].Error == "" {
		t.Fatal("deleted target changed native outcome")
	}
	s.mu.RLock()
	state, err = s.project.Collaboration()
	s.mu.RUnlock()
	if err != nil || len(state.Threads) != 0 {
		t.Fatal("deleted discussion recreated", err)
	}
}

func TestDiscussionRestartRetriesWritebackWithoutModel(t *testing.T) {
	s, dir := discussionServer(t)
	_, child := createDiscussionFixture(t, s)
	input := PreviewInput{Text: "回应", SessionID: child.ID, Documents: []string{"PLAN.md"}}
	var preview PreviewResult
	if jsonRequest(t, s, "/api/agents/preview", input, &preview) != 200 {
		t.Fatal("preview")
	}
	if jsonRequest(t, s, "/api/agents/"+child.ID+"/input", map[string]any{"preview": input, "checksum": preview.Checksum}, nil) != 200 {
		t.Fatal("send")
	}
	waitReply(t, s, child.ID, func(a agent.Session) bool { return len(a.Replies) == 1 && a.Replies[0].Synced })
	s.Close()
	path := filepath.Join(dir, ".agent-state", "sessions", child.ID, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved agent.Session
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	saved.Replies[0].Synced = false
	data, _ = json.Marshal(saved)
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := project.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewWithCodex(testToken, p, "/definitely-not-a-model-runtime")
	defer restarted.Close()
	if _, err = restarted.manager(false); err != nil {
		t.Fatal(err)
	}
	recovered := waitReply(t, restarted, child.ID, func(a agent.Session) bool { return len(a.Replies) == 1 && a.Replies[0].Synced })
	if recovered.State != "disconnected" {
		t.Fatal("background recovery started model")
	}
	restarted.mu.RLock()
	state, err := restarted.project.Collaboration()
	restarted.mu.RUnlock()
	if err != nil || len(state.Threads[0].Messages) != 2 {
		t.Fatal("restart duplicated reply", err)
	}
}

func TestDeletedMessagePreviewChecksumIsStable(t *testing.T) {
	s, _ := discussionServer(t)
	_, child := createDiscussionFixture(t, s)
	s.mu.Lock()
	state, _ := s.project.Collaboration()
	for i := 0; i < 12; i++ {
		state.Threads[0].Messages = append(state.Threads[0].Messages, project.Message{ID: fmt.Sprintf("removed-%02d", i), Author: "我", Role: "human", Intent: "comment", Body: "将被删除", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	}
	_, err := s.project.SaveCollaboration(state.Generation, state.Threads)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	input := PreviewInput{Text: "回应", SessionID: child.ID, Documents: []string{"PLAN.md"}}
	var preview PreviewResult
	if jsonRequest(t, s, "/api/agents/preview", input, &preview) != 200 {
		t.Fatal("preview")
	}
	if jsonRequest(t, s, "/api/agents/"+child.ID+"/input", map[string]any{"preview": input, "checksum": preview.Checksum}, nil) != 200 {
		t.Fatal("send")
	}
	waitReply(t, s, child.ID, func(a agent.Session) bool { return len(a.Replies) == 1 && a.Replies[0].Synced })
	s.mu.Lock()
	state, _ = s.project.Collaboration()
	state.Threads[0].Messages = state.Threads[0].Messages[len(state.Threads[0].Messages)-1:]
	_, err = s.project.SaveCollaboration(state.Generation, state.Threads)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if jsonRequest(t, s, "/api/agents/preview", input, &preview) != 200 {
		t.Fatal("deletion preview")
	}
	first := preview.Checksum
	for i := 0; i < 30; i++ {
		if jsonRequest(t, s, "/api/agents/preview", input, &preview) != 200 || preview.Checksum != first {
			t.Fatal("unchanged deletion preview has nondeterministic checksum")
		}
	}
}
func TestWritebackRetryDoesNotMarkHumanEditAsDelivered(t *testing.T) {
	s, dir := discussionServer(t)
	_, child := createDiscussionFixture(t, s)
	input := PreviewInput{Text: "回应", SessionID: child.ID, Documents: []string{"PLAN.md"}}
	var preview PreviewResult
	if jsonRequest(t, s, "/api/agents/preview", input, &preview) != 200 {
		t.Fatal("preview")
	}
	if jsonRequest(t, s, "/api/agents/"+child.ID+"/input", map[string]any{"preview": input, "checksum": preview.Checksum}, nil) != 200 {
		t.Fatal("send")
	}
	waitReply(t, s, child.ID, func(a agent.Session) bool { return len(a.Replies) == 1 && a.Replies[0].Synced })
	s.Close()
	path := filepath.Join(dir, ".agent-state", "sessions", child.ID, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved agent.Session
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	saved.Replies[0].Synced = false
	data, _ = json.Marshal(saved)
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := project.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	state, err := p.Collaboration()
	if err != nil {
		t.Fatal(err)
	}
	state.Threads[0].Messages[1].Body = "人工修改后的意见"
	if _, err = p.SaveCollaboration(state.Generation, state.Threads); err != nil {
		t.Fatal(err)
	}
	restarted := NewWithCodex(testToken, p, "/no-runtime")
	defer restarted.Close()
	if _, err = restarted.manager(false); err != nil {
		t.Fatal(err)
	}
	waitReply(t, restarted, child.ID, func(a agent.Session) bool { return a.Replies[0].Synced })
	if jsonRequest(t, restarted, "/api/agents/preview", input, &preview) != 200 || !strings.Contains(preview.Text, "人工修改后的意见") {
		t.Fatal("human edit falsely marked already delivered", preview.Text)
	}
}

func TestDiscussionDocumentRequiresExplicitSelection(t *testing.T) {
	s, _ := discussionServer(t)
	_, child := createDiscussionFixture(t, s)
	input := PreviewInput{Text: "回应引用", SessionID: child.ID}
	var preview PreviewResult
	if jsonRequest(t, s, "/api/agents/preview", input, &preview) != 200 || !strings.Contains(preview.Text, "第一条") || strings.Contains(preview.Text, "选定文档") || preview.Context.DocumentRevision != "" {
		t.Fatalf("default discussion must exclude document body: %s", preview.Text)
	}
	input.Documents = []string{"PLAN.md"}
	if jsonRequest(t, s, "/api/agents/preview", input, &preview) != 200 || !strings.Contains(preview.Text, "选定文档 PLAN.md") {
		t.Fatal("explicit document selection omitted", preview.Text)
	}
	revision := preview.Context.DocumentRevision
	if jsonRequest(t, s, "/api/agents/"+child.ID+"/input", map[string]any{"preview": input, "checksum": preview.Checksum}, nil) != 200 {
		t.Fatal("send selected body")
	}
	waitReply(t, s, child.ID, func(a agent.Session) bool { return len(a.Replies) == 1 && a.Replies[0].Synced })
	input.Documents = nil
	if jsonRequest(t, s, "/api/agents/preview", input, &preview) != 200 || strings.Contains(preview.Text, "选定文档") || preview.Context.DocumentRevision != revision {
		t.Fatal("unchecked body must stay omitted without forgetting delivered revision", preview.Text)
	}
	if jsonRequest(t, s, "/api/agents/"+child.ID+"/input", map[string]any{"preview": input, "checksum": preview.Checksum}, nil) != 200 {
		t.Fatal("send discussion only")
	}
	waitReply(t, s, child.ID, func(a agent.Session) bool { return len(a.Replies) == 2 && a.Replies[1].Synced })
	input.Documents = []string{"PLAN.md"}
	if jsonRequest(t, s, "/api/agents/preview", input, &preview) != 200 || strings.Contains(preview.Text, "选定文档") {
		t.Fatal("unchanged body resent", preview.Text)
	}
}

func TestDocumentDiscussionsShareOneSessionAndRouteReplies(t *testing.T) {
	s, _ := discussionServer(t)
	parent, child := createDiscussionFixture(t, s)
	s.mu.Lock()
	state, err := s.project.Collaboration()
	if err == nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		state.Threads = append(state.Threads,
			project.Thread{ID: "E", DocumentID: "PLAN.md", Kind: "comment", Status: "open", Anchor: &project.Anchor{Quote: "当前计划", Revision: 1}, Messages: []project.Message{{ID: "E1", Author: "我", Role: "human", Intent: "comment", Body: "第二条讨论", CreatedAt: now}}},
			project.Thread{ID: "F", DocumentID: "PLAN.md", Kind: "comment", Status: "resolved", Messages: []project.Message{{ID: "F1", Author: "我", Role: "human", Intent: "comment", Body: "已解决的讨论", CreatedAt: now}}},
			project.Thread{ID: "G", DocumentID: "README.md", Kind: "comment", Status: "open", Messages: []project.Message{{ID: "G1", Author: "我", Role: "human", Intent: "comment", Body: "其他文档", CreatedAt: now}}})
		_, err = s.project.SaveCollaboration(state.Generation, state.Threads)
	}
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	var again, other agent.Session
	if jsonRequest(t, s, "/api/agents/"+parent.ID+"/discussion", map[string]string{"documentId": "PLAN.md"}, &again) != 201 || again.ID != child.ID {
		t.Fatal("same document must reuse one session")
	}
	if jsonRequest(t, s, "/api/agents/"+parent.ID+"/discussion", map[string]string{"documentId": "README.md"}, &other) != 201 || other.ID == child.ID {
		t.Fatal("other document must use its own session")
	}
	if code := jsonRequest(t, s, "/api/agents/"+parent.ID+"/discussion", map[string]string{"documentId": "docs/none.md"}, nil); code != 404 {
		t.Fatal("document without discussions accepted", code)
	}
	if code := jsonRequest(t, s, "/api/agents/preview", PreviewInput{Text: "回应", SessionID: child.ID, ThreadIDs: []string{"G"}}, nil); code != 400 {
		t.Fatal("thread from another document accepted", code)
	}
	input := PreviewInput{Text: "回应", SessionID: child.ID}
	var preview PreviewResult
	if code := jsonRequest(t, s, "/api/agents/preview", input, &preview); code != 200 || !strings.Contains(preview.Text, "=== 讨论 D ") || !strings.Contains(preview.Text, "=== 讨论 E ") || strings.Contains(preview.Text, "已解决的讨论") || strings.Contains(preview.Text, "其他文档") || !strings.Contains(preview.Text, "### 讨论 <ID>") {
		t.Fatal("default preview must list open threads of the document", code, preview.Text)
	}
	if len(preview.Context.Selected) != 2 {
		t.Fatal(preview.Context.Selected)
	}
	if jsonRequest(t, s, "/api/agents/"+child.ID+"/input", map[string]any{"preview": input, "checksum": preview.Checksum}, nil) != 200 {
		t.Fatal("send")
	}
	current := waitReply(t, s, child.ID, func(a agent.Session) bool { return len(a.Replies) == 1 && a.Replies[0].Synced })
	s.mu.RLock()
	state, _ = s.project.Collaboration()
	s.mu.RUnlock()
	bodies := map[string]string{}
	for _, thread := range state.Threads {
		last := thread.Messages[len(thread.Messages)-1]
		if last.Role == "agent" {
			bodies[thread.ID] = last.Body
		}
	}
	if bodies["D"] != "协议测试回复 D" || bodies["E"] != "协议测试回复 E" || len(bodies) != 2 {
		t.Fatalf("reply not routed per discussion: %v", bodies)
	}
	if len(current.DiscussionContext.Discussions) != 2 {
		t.Fatal("baseline must cover both discussions", current.DiscussionContext)
	}
	s.syncDiscussionReplies(s.agents)
	s.mu.RLock()
	state, _ = s.project.Collaboration()
	s.mu.RUnlock()
	if len(state.Threads[0].Messages) != 2 || len(state.Threads[1].Messages) != 2 {
		t.Fatal("duplicate routed writeback")
	}
	input.ThreadIDs = []string{"E"}
	if code := jsonRequest(t, s, "/api/agents/preview", input, &preview); code != 200 || preview.Mode != "增量" || !strings.Contains(preview.Text, "=== 讨论 E ") || strings.Contains(preview.Text, "=== 讨论 D ") || !strings.Contains(preview.Text, "无新消息") || strings.Contains(preview.Text, "协议测试回复") || strings.Contains(preview.Text, "### 讨论 <ID>") {
		t.Fatal("explicit single discussion must be incremental without own replies", code, preview.Text)
	}
	if _, ok := preview.Context.Discussions["D"]; !ok {
		t.Fatal("unselected discussion baseline dropped")
	}
}

func TestUnroutedMultiDiscussionReplyIsNotGuessed(t *testing.T) {
	s, _ := discussionServer(t)
	_, child := createDiscussionFixture(t, s)
	s.mu.Lock()
	state, err := s.project.Collaboration()
	if err == nil {
		state.Threads = append(state.Threads, project.Thread{ID: "E", DocumentID: "PLAN.md", Kind: "comment", Status: "open", Messages: []project.Message{{ID: "E1", Author: "我", Role: "human", Intent: "comment", Body: "第二条", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}})
		_, err = s.project.SaveCollaboration(state.Generation, state.Threads)
	}
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.agents.FinishReply(child.ID, "", "", "", nil, "", false); err == nil {
		t.Fatal("unknown reply accepted")
	}
	session, _ := s.agents.Get(child.ID)
	snapshot := agent.DiscussionContext{ThreadID: session.ThreadID, DocumentID: "PLAN.md", Discussions: map[string]agent.DiscussionBaseline{"D": {Messages: map[string]string{}}, "E": {Messages: map[string]string{}}}, Selected: []string{"D", "E"}}
	// 协议进程只在输入中列出两条讨论标题时分段回复；这里的输入不含标题，模拟模型未按要求分段。
	if _, err = s.agents.SendDiscussion(context.Background(), child.ID, "回应", snapshot, agent.ContextHash(nil)); err != nil {
		t.Fatal(err)
	}
	current := waitReply(t, s, child.ID, func(a agent.Session) bool { return len(a.Replies) == 1 && a.Replies[0].Discarded })
	if current.Replies[0].Error != agent.ErrUnroutedReply.Error() {
		t.Fatal(current.Replies[0].Error)
	}
	s.mu.RLock()
	state, _ = s.project.Collaboration()
	s.mu.RUnlock()
	for _, thread := range state.Threads {
		if len(thread.Messages) != 1 {
			t.Fatal("unrouted reply written to a discussion", thread.ID)
		}
	}
}
