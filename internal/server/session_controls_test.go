package server

import (
	"agent-workbench/internal/agent"
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
