package server

import (
	"agent-workbench/internal/project"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentListReadOnlyAuthorizationAndInputBoundary(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "project")
	p, err := project.Create(dir, "会话")
	if err != nil {
		t.Fatal(err)
	}
	s := New(testToken, p)
	defer s.Close()
	if w := request(t, s, "GET", "/api/agents", "", "", ""); w.Code != 403 {
		t.Fatal("agent list without authentication")
	}
	if w := request(t, s, "GET", "/api/agents", "", testToken, ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal(w.Body.String())
	}
	if _, err = os.Stat(filepath.Join(dir, ".agent-state")); !os.IsNotExist(err) {
		t.Fatal("listing created state")
	}
	if w := request(t, s, "POST", "/api/agents/preview", `{"text":"任务","documents":["../../secret.md"]}`, testToken, ""); w.Code != 400 {
		t.Fatal("preview accepted outside document")
	}
	w := request(t, s, "POST", "/api/agents/preview", `{"text":"只看计划","documents":["PLAN.md"]}`, testToken, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "PLAN.md") || strings.Contains(w.Body.String(), "AGENTS.md") {
		t.Fatal(w.Body.String())
	}
	if w = request(t, s, "POST", "/api/agents/missing/input", `{"text":"任务"}`, testToken, ""); w.Code < 400 {
		t.Fatal("missing session accepted")
	}
}
