package server

import (
	"agent-workbench/internal/agent"
	"agent-workbench/internal/project"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstalledCodexDiscussionWriteback(t *testing.T) {
	if os.Getenv("AWB_REAL_CODEX_DISCUSSION") != "1" {
		t.Skip("explicit real discussion verification")
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "project")
	p, err := project.Create(dir, "原生讨论验证")
	if err != nil {
		t.Fatal(err)
	}
	s := NewWithCodex(testToken, p, binary)
	defer s.Close()
	var parent, child agent.Session
	if code := jsonRequest(t, s, "/api/agents", map[string]string{"name": "工作", "sandbox": "read-only", "approvalPolicy": "on-request"}, &parent); code != 201 {
		t.Fatal("native parent start", code)
	}
	s.mu.Lock()
	state, err := s.project.Collaboration()
	if err == nil {
		_, err = s.project.SaveCollaboration(state.Generation, []project.Thread{{ID: "native-discussion", DocumentID: "PLAN.md", Kind: "comment", Status: "open", Messages: []project.Message{{ID: "native-C1", Author: "我", Role: "human", Intent: "comment", Body: "验证讨论回复自动写回。", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}}})
	}
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if code := jsonRequest(t, s, "/api/agents/"+parent.ID+"/discussion", map[string]string{"discussionId": "native-discussion"}, &child); code != 201 {
		t.Fatal("native child start", code)
	}
	if child.ThreadID == parent.ThreadID {
		t.Fatal("native contexts not isolated")
	}
	input := PreviewInput{SessionID: child.ID, Text: "只回复 **AWB_DISCUSSION_OK**。不要执行命令、修改文件或访问网络。"}
	var preview PreviewResult
	if code := jsonRequest(t, s, "/api/agents/preview", input, &preview); code != 200 || preview.Mode != "完整" {
		t.Fatal("native preview", code)
	}
	if code := jsonRequest(t, s, "/api/agents/"+child.ID+"/input", map[string]any{"preview": input, "checksum": preview.Checksum}, nil); code != 200 {
		t.Fatal("native turn start", code)
	}
	deadline := time.Now().Add(90 * time.Second)
	synced := false
	for time.Now().Before(deadline) {
		current, _ := s.agents.Get(child.ID)
		if current.State == "waiting" || current.State == "disconnected" {
			t.Fatalf("native turn blocked: %s %s", current.State, current.LastError)
		}
		if len(current.Replies) > 0 && current.Replies[0].Synced {
			synced = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !synced {
		t.Fatal("native reply not synchronized")
	}
	s.mu.RLock()
	state, err = s.project.Collaboration()
	s.mu.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Threads[0].Messages) != 2 || state.Threads[0].Messages[1].Source == nil || !strings.Contains(state.Threads[0].Messages[1].Body, "AWB_DISCUSSION_OK") {
		t.Fatal("native final reply lost")
	}
	if code := jsonRequest(t, s, "/api/agents/preview", input, &preview); code != 200 || preview.Mode != "增量" || strings.Contains(preview.Text, "验证讨论回复自动写回") || strings.Contains(preview.Text, "选定文档") {
		t.Fatal("native baseline not recorded", code, preview.Text)
	}
	t.Logf("native parent=%s child=%s model=%s; final reply written once; second preview incremental", parent.ThreadID, child.ThreadID, child.Model)
}
