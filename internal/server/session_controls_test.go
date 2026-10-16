package server

import (
	"agent-workbench/internal/agent"
	"agent-workbench/internal/project"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionControlsHTTP(t *testing.T) {
	s, dir := discussionServer(t)
	parent, child := createDiscussionFixture(t, s)
	before, err := os.ReadFile(filepath.Join(dir, ".agent-state/collaboration.json"))
	if err != nil {
		t.Fatal(err)
	}
	if w := request(t, s, "GET", "/api/authorizations", "", "wrong", ""); w.Code != 403 {
		t.Fatal("unauthenticated policy")
	}
	var policy agent.CommandAuthorizations
	if code := jsonRequest(t, s, "/api/authorizations", agent.CommandAuthorizations{Rules: []agent.CommandAuthorization{{ID: "one", Prefix: []string{"git", "status"}, SessionID: parent.ID}}}, &policy); code != 200 || policy.Generation != 1 {
		t.Fatal("save", code, policy)
	}
	if code := jsonRequest(t, s, "/api/authorizations", agent.CommandAuthorizations{}, nil); code != 409 {
		t.Fatal("stale policy", code)
	}
	if code := jsonRequest(t, s, "/api/agents/"+parent.ID+"/delete", map[string]any{}, nil); code != 200 {
		t.Fatal("delete", code)
	}
	m, _ := s.manager(false)
	if _, err = m.Get(child.ID); err != nil {
		t.Fatal("deleted child")
	}
	if len(m.Authorizations().Rules) != 0 {
		t.Fatal("deleted scope still granted")
	}
	after, _ := os.ReadFile(filepath.Join(dir, ".agent-state/collaboration.json"))
	if string(before) != string(after) {
		t.Fatal("discussion changed")
	}
	if _, err = os.Stat(filepath.Join(dir, ".agent-state/sessions", parent.ID)); !os.IsNotExist(err) {
		t.Fatal("logs remain", err)
	}
}
func TestCompactHTTP(t *testing.T) {
	s, _ := discussionServer(t)
	var created agent.Session
	if code := jsonRequest(t, s, "/api/agents", map[string]string{"name": "压缩"}, &created); code != 201 {
		t.Fatal("create", code)
	}
	var compacted agent.Session
	if code := jsonRequest(t, s, "/api/agents/"+created.ID+"/compact", map[string]any{}, &compacted); code != 200 {
		t.Fatal("compact", code)
	}
	if compacted.ID != created.ID || compacted.ThreadID == created.ThreadID || compacted.State != "idle" {
		t.Fatal("thread not replaced", compacted)
	}
	if w := request(t, s, "POST", "/api/agents/missing/compact", `{}`, testToken, ""); w.Code != 409 {
		t.Fatal("missing session", w.Code)
	}
	if w := request(t, s, "POST", "/api/agents/"+created.ID+"/bogus", `{}`, testToken, ""); w.Code != 404 {
		t.Fatal("unknown action", w.Code)
	}

	_, child := createDiscussionFixture(t, s)
	input := PreviewInput{Text: "回应", SessionID: child.ID, ThreadIDs: []string{"D"}, Documents: []string{"PLAN.md"}}
	var preview PreviewResult
	if code := jsonRequest(t, s, "/api/agents/preview", input, &preview); code != 200 {
		t.Fatal("preview", code)
	}
	if code := jsonRequest(t, s, "/api/agents/"+child.ID+"/input", map[string]any{"preview": input, "checksum": preview.Checksum}, nil); code != 200 {
		t.Fatal("send", code)
	}
	waitReply(t, s, child.ID, func(a agent.Session) bool { return len(a.Replies) == 1 && a.Replies[0].Synced })
	if code := jsonRequest(t, s, "/api/agents/"+child.ID+"/compact", map[string]any{}, &compacted); code != 200 {
		t.Fatal("compact discussion", code)
	}
	input.Text = "再回应"
	if code := jsonRequest(t, s, "/api/agents/preview", input, &preview); code != 200 || preview.Mode != "完整" || preview.Context == nil || preview.Context.ThreadID != compacted.ThreadID {
		t.Fatal("preview after compact", code, preview.Mode, preview.Context)
	}

	dir := filepath.Join(t.TempDir(), "project")
	p, err := project.Create(dir, "未接入")
	if err != nil {
		t.Fatal(err)
	}
	empty := New(testToken, p)
	defer empty.Close()
	if w := request(t, empty, "POST", "/api/agents/anything/compact", `{}`, testToken, ""); w.Code != 404 {
		t.Fatal("compact without manager", w.Code)
	}
}
