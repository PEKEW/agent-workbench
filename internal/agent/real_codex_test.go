package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstalledCodexHandshake(t *testing.T) {
	if os.Getenv("AWB_REAL_CODEX") != "1" {
		t.Skip("explicit real CLI verification")
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "PLAN.md"), []byte("# 临时验证项目\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	if err = os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(dir, root, CodexCommand(binary))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := m.Create(ctx, StartOptions{Name: "本机协议验证", Sandbox: "read-only", ApprovalPolicy: "on-request"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native thread=%s model=%s sandbox=%s policy=%s", s.ThreadID, s.Model, s.Sandbox, s.ApprovalPolicy)
	if s.ThreadID == "" || s.State != "idle" {
		t.Fatal("real session not connected")
	}
	m.Close()
	reopenedRoot, err := os.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := NewManager(dir, reopenedRoot, CodexCommand(binary))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restoredSession, err := restored.Resume(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restoredSession.State != "idle" {
		t.Fatal("empty session not reconnected")
	}
	t.Logf("empty session reconnect: previous=%s current=%s", s.ThreadID, restoredSession.ThreadID)
}

// TestInstalledCodexAttachMissingRollout 验证接入不存在的 thread 直接返回原生错误，不静默重建。
func TestInstalledCodexAttachMissingRollout(t *testing.T) {
	if os.Getenv("AWB_REAL_CODEX") != "1" {
		t.Skip("explicit real CLI verification")
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "PLAN.md"), []byte("# 临时验证项目\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	if err = os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(dir, root, CodexCommand(binary))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err = m.Create(ctx, StartOptions{Name: "接入不存在", Sandbox: "read-only", ApprovalPolicy: "never", ResumeThreadID: "00000000-0000-0000-0000-000000000000"}); err == nil || !strings.Contains(err.Error(), "no rollout found") {
		t.Fatal("missing rollout silently recreated", err)
	}
}

// TestInstalledCodexAttachThread 验证按 ID 接入本机已有原生 thread；需要一次最小真实回合产生原生历史。
func TestInstalledCodexAttachThread(t *testing.T) {
	if os.Getenv("AWB_REAL_CODEX_TURN") != "1" {
		t.Skip("explicit real model verification")
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "PLAN.md"), []byte("# 临时验证项目\nAWB_NATIVE_ATTACH_OK\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	if err = os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(dir, root, CodexCommand(binary))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	a, err := m.Create(ctx, StartOptions{Name: "原会话", Sandbox: "read-only", ApprovalPolicy: "never"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Send(ctx, a.ID, "请仅回复标记 AWB_NATIVE_ATTACH_OK，不要执行任何工具或读取文件。"); err != nil {
		t.Fatal(err)
	}
	awaitRealTurn(t, m, a.ID)
	if err = m.CloseSession(a.ID); err != nil {
		t.Fatal(err)
	}
	attached, err := m.Create(ctx, StartOptions{Name: "接入会话", Sandbox: "read-only", ApprovalPolicy: "never", ResumeThreadID: a.ThreadID})
	if err != nil {
		t.Fatal(err)
	}
	if attached.ThreadID != a.ThreadID || attached.State != "idle" {
		t.Fatalf("real attach did not resume thread: %s -> %s", a.ThreadID, attached.ThreadID)
	}
	t.Logf("native attach: thread=%s", attached.ThreadID)
}

func awaitRealTurn(t *testing.T, m *Manager, id string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		current, err := m.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if current.State == "waiting" {
			t.Fatalf("native request requires human response: %+v", current.Requests)
		}
		if current.State == "idle" && current.LastTurnStatus != "" {
			if current.LastTurnStatus != "completed" {
				t.Fatalf("native turn %s: %s", current.LastTurnStatus, current.LastError)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("turn timeout")
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func TestInstalledCodexCompact(t *testing.T) {
	if os.Getenv("AWB_REAL_CODEX") != "1" {
		t.Skip("explicit real CLI verification")
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "PLAN.md"), []byte("# 临时验证项目\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	if err = os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(dir, root, CodexCommand(binary))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := m.Create(ctx, StartOptions{Name: "本机清空验证", Sandbox: "read-only", ApprovalPolicy: "never"})
	if err != nil {
		t.Fatal(err)
	}
	compacted, err := m.Compact(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if compacted.ThreadID == "" || compacted.ThreadID == s.ThreadID || compacted.State != "idle" {
		t.Fatalf("real compact did not recreate thread: %s -> %s", s.ThreadID, compacted.ThreadID)
	}
	events, err := m.Events(s.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Method == "workbench/contextCleared" {
			found = true
		}
	}
	if !found {
		t.Fatal("real compact not disclosed in log")
	}
	t.Logf("native compact: previous=%s current=%s", s.ThreadID, compacted.ThreadID)
}

func TestInstalledCodexReadOnlyTurn(t *testing.T) {
	if os.Getenv("AWB_REAL_CODEX_TURN") != "1" {
		t.Skip("explicit real model verification")
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "PLAN.md"), []byte("# 临时验证项目\nAWB_NATIVE_READ_OK\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	if err = os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(dir, root, CodexCommand(binary))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	session, err := m.Create(ctx, StartOptions{Name: "真实只读回合验证", Sandbox: "read-only", ApprovalPolicy: "on-request"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Send(ctx, session.ID, "请使用 shell 读取当前目录的 PLAN.md，验证其中的标记，仅回复标记 AWB_NATIVE_READ_OK。不要修改文件、安装依赖或访问网络。"); err != nil {
		t.Fatal(err)
	}
	for {
		current, err := m.Get(session.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.State == "waiting" {
			t.Fatalf("native request requires human response: %+v", current.Requests)
		}
		if current.State == "disconnected" {
			t.Fatalf("native runtime disconnected: %s", current.LastError)
		}
		if current.State == "idle" && current.LastTurnStatus != "" {
			if current.LastTurnStatus != "completed" {
				t.Fatalf("native turn %s: %s", current.LastTurnStatus, current.LastError)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
	events, err := m.Events(session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	shellObserved, replyObserved := false, false
	for _, event := range events {
		if event.Method == "item/completed" {
			var observed struct {
				Item struct {
					Type             string `json:"type"`
					Command          string `json:"command"`
					AggregatedOutput string `json:"aggregatedOutput"`
					Text             string `json:"text"`
				} `json:"item"`
			}
			if json.Unmarshal(event.Params, &observed) != nil {
				t.Fatal("invalid native item")
			}
			if observed.Item.Type == "commandExecution" && strings.Contains(observed.Item.Command, "PLAN.md") && strings.Contains(observed.Item.AggregatedOutput, "AWB_NATIVE_READ_OK") {
				shellObserved = true
			}
			if observed.Item.Type == "agentMessage" && strings.Contains(observed.Item.Text, "AWB_NATIVE_READ_OK") {
				replyObserved = true
			}
		}
	}
	if !shellObserved || !replyObserved {
		t.Fatalf("missing native read or reply evidence: shell=%v reply=%v", shellObserved, replyObserved)
	}
	t.Logf("native read completed: model=%s thread=%s events=%d", session.Model, session.ThreadID, len(events))
	m.Close()
	reopenedRoot, err := os.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := NewManager(dir, reopenedRoot, CodexCommand(binary))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restoredSession, err := restored.Resume(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restoredSession.ThreadID != session.ThreadID || restoredSession.State != "idle" {
		t.Fatal("real thread resume changed identity or failed")
	}
	t.Logf("native resume completed: thread=%s", restoredSession.ThreadID)
}
