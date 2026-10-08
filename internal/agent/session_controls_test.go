package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteSessionPreservesProjectAndRejectsBusy(t *testing.T) {
	m, dir := testManager(t)
	ctx := context.Background()
	s, err := m.Create(ctx, StartOptions{Name: "清理"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Send(ctx, s.ID, "wait"); err != nil {
		t.Fatal(err)
	}
	if err = m.DeleteSession(s.ID); err == nil {
		t.Fatal("deleted running session")
	}
	if err = m.Interrupt(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, s.ID, func(s Session) bool { return s.State == "idle" })
	keep := filepath.Join(dir, "keep.txt")
	if err = os.WriteFile(keep, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = m.DeleteSession(s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Get(s.ID); err == nil {
		t.Fatal("deleted session listed")
	}
	if _, err = os.Stat(filepath.Join(dir, s.ID)); !os.IsNotExist(err) {
		t.Fatal("logs not cleaned", err)
	}
	if b, err := os.ReadFile(keep); err != nil || string(b) != "keep" {
		t.Fatal("project changed")
	}
	m.Close()
	root, _ := os.OpenRoot(dir)
	restored, err := NewManager(dir, root, helperCommand)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if len(restored.List()) != 0 {
		t.Fatal("deleted session restored")
	}
}
func TestCommandAuthorizationParsing(t *testing.T) {
	cases := []struct {
		command string
		valid   bool
	}{{"go test ./...", true}, {"'/usr/bin/git' status", true}, {"/bin/zsh -lc 'go test ./...'", true}, {"go test ./... && rm -rf x", false}, {"go test $(echo x)", false}, {"go test > out", false}, {"bash -c 'go test; echo bad'", false}, {"go test\nrm x", false}, {"go test `echo x`", false}, {"go\u00a0test ./...", false}, {"/bin/zsh -lc 'go test =(touch /tmp/awb)'", false}, {"go test 'unterminated", false}}
	for _, c := range cases {
		_, err := simpleCommand(c.command)
		if (err == nil) != c.valid {
			t.Errorf("%q valid=%v err=%v", c.command, c.valid, err)
		}
	}
}
func TestCommandAuthorizationsAutoApprovalAndRevocation(t *testing.T) {
	m, dir := testManager(t)
	ctx := context.Background()
	s, err := m.Create(ctx, StartOptions{Name: "授权"})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := m.SaveAuthorizations(0, []CommandAuthorization{{ID: "echo", Prefix: []string{"echo", "approved"}, SessionID: s.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.SaveAuthorizations(0, policy.Rules); err == nil {
		t.Fatal("stale policy accepted")
	}
	for i := 0; i < 2; i++ {
		if _, err = m.Send(ctx, s.ID, "approve"); err != nil {
			t.Fatal(err)
		}
		awaitSession(t, m, s.ID, func(s Session) bool { return s.State == "idle" && s.LastTurnStatus == "completed" })
	}
	events, _ := m.Events(s.ID, 0)
	count := 0
	for _, e := range events {
		if e.Method == "workbench/commandAutoApproved" {
			count++
		}
	}
	if count != 2 {
		t.Fatal("approval not audited", count)
	}
	if _, err = m.SaveAuthorizations(policy.Generation, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Send(ctx, s.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	waiting := awaitSession(t, m, s.ID, func(s Session) bool { return s.State == "waiting" })
	if len(waiting.Requests) != 1 {
		t.Fatal("revoked policy still auto-approved")
	}
	if err = m.Answer(ctx, s.ID, waiting.Requests[0].ID, json.RawMessage(`{"decision":"decline"}`)); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, s.ID, func(s Session) bool { return s.State == "idle" })
	m.Close()
	root, _ := os.OpenRoot(dir)
	restored, err := NewManager(dir, root, helperCommand)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if len(restored.Authorizations().Rules) != 0 {
		t.Fatal("revocation lost")
	}
}
func TestAuthorizationScopeAndCompoundCommands(t *testing.T) {
	m, _ := testManager(t)
	s, err := m.Create(context.Background(), StartOptions{Name: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.SaveAuthorizations(0, []CommandAuthorization{{ID: "go", Prefix: []string{"go", "test"}, SessionID: s.ID}}); err != nil {
		t.Fatal(err)
	}
	params := func(command, cwd string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"threadId": s.ThreadID, "turnId": "T", "command": command, "cwd": cwd})
		return b
	}
	m.mu.Lock()
	r := m.sessions[s.ID]
	r.ActiveTurnID = "T"
	defer m.mu.Unlock()
	for _, cmd := range []string{"go testing ./...", "go test ./... && rm x", "go test $(echo x)"} {
		if _, ok := m.authorizedRuleLocked(r, Request{Method: "item/commandExecution/requestApproval", Params: params(cmd, m.cwd)}); ok {
			t.Fatalf("unsafe auto approval %q", cmd)
		}
	}
	if _, ok := m.authorizedRuleLocked(r, Request{Method: "item/commandExecution/requestApproval", Params: params("go test ./...", filepath.Dir(m.cwd))}); ok {
		t.Fatal("outside cwd approved")
	}
	if _, ok := m.authorizedRuleLocked(r, Request{Method: "item/commandExecution/requestApproval", Params: params("go test ./...", m.cwd)}); !ok {
		t.Fatal("matching prefix rejected")
	}
}

func TestDeleteSessionRejectsPendingReplyAndSymlink(t *testing.T) {
	m, dir := testManager(t)
	s, err := m.Create(context.Background(), StartOptions{Name: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.sessions[s.ID].Replies = []DiscussionReply{{Body: "unsaved"}}
	m.mu.Unlock()
	if err = m.DeleteSession(s.ID); err == nil {
		t.Fatal("deleted pending result")
	}
	m.mu.Lock()
	m.sessions[s.ID].Replies = nil
	m.mu.Unlock()
	outside := filepath.Join(t.TempDir(), "keep")
	if err = os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(dir, s.ID, "link")); err != nil {
		t.Fatal(err)
	}
	if err = m.DeleteSession(s.ID); err == nil {
		t.Fatal("followed symlink")
	}
	b, _ := os.ReadFile(outside)
	if string(b) != "keep" {
		t.Fatal("outside file changed")
	}
}
func TestAuthorizationIsolationAndRequestKinds(t *testing.T) {
	m, _ := testManager(t)
	s, _ := m.Create(context.Background(), StartOptions{Name: "one"})
	other, _ := m.Create(context.Background(), StartOptions{Name: "two"})
	_, err := m.SaveAuthorizations(0, []CommandAuthorization{{ID: "go", Prefix: []string{"go", "test"}, SessionID: s.ID}})
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.sessions[s.ID]
	r.ActiveTurnID = "T"
	m.sessions[other.ID].ActiveTurnID = "T"
	for _, extra := range []map[string]any{{"kind": "writeStdin"}, {"networkApprovalContext": map[string]any{"host": "x"}}, {"additionalPermissions": map[string]any{}}, {"turnId": "foreign"}, {"availableDecisions": []string{"decline"}}} {
		p := map[string]any{"threadId": s.ThreadID, "turnId": "T", "command": "go test ./...", "cwd": m.cwd}
		for k, v := range extra {
			p[k] = v
		}
		b, _ := json.Marshal(p)
		if _, ok := m.authorizedRuleLocked(r, Request{Method: "item/commandExecution/requestApproval", Params: b}); ok {
			t.Fatal("approved special request", extra)
		}
	}
	b, _ := json.Marshal(map[string]any{"threadId": other.ThreadID, "turnId": "T", "command": "go test ./...", "cwd": m.cwd})
	if _, ok := m.authorizedRuleLocked(m.sessions[other.ID], Request{Method: "item/commandExecution/requestApproval", Params: b}); ok {
		t.Fatal("session scope leaked")
	}
}

// 阻塞 writer 验证实际批准写入与撤销串行，状态读取不受管道阻塞。
type approvalWriter struct {
	entered chan struct{}
	release chan struct{}
}

func (w *approvalWriter) Write(b []byte) (int, error) {
	close(w.entered)
	<-w.release
	return len(b), nil
}
func (w *approvalWriter) Close() error { return nil }
func TestAuthorizationWriteAndRevocationTransaction(t *testing.T) {
	m, _ := testManager(t)
	writer := &approvalWriter{entered: make(chan struct{}), release: make(chan struct{})}
	c := &client{stdin: writer, done: make(chan struct{})}
	written := make(chan error, 1)
	go func() {
		written <- c.replyGuarded(json.RawMessage(`"approval"`), map[string]string{"decision": "accept"}, func() (func(), error) { m.policyMu.RLock(); return m.policyMu.RUnlock, nil })
	}()
	<-writer.entered
	revoked := make(chan error, 1)
	go func() { _, err := m.SaveAuthorizations(0, nil); revoked <- err }()
	read := make(chan struct{})
	go func() { m.List(); close(read) }()
	select {
	case <-read:
	case <-time.After(time.Second):
		t.Fatal("approval IO blocked session state")
	}
	select {
	case err := <-revoked:
		t.Fatal("revocation crossed accepted write", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(writer.release)
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-revoked:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("revocation remained blocked")
	}
}
func TestDeleteDisconnectedInputRequiresExplicitAbandon(t *testing.T) {
	m, _ := testManager(t)
	s, _ := m.Create(context.Background(), StartOptions{Name: "unknown"})
	_, err := m.Send(context.Background(), s.ID, "wait")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.CloseSession(s.ID); err != nil {
		t.Fatal(err)
	}
	if err = m.DeleteSession(s.ID); err == nil {
		t.Fatal("silently abandoned unknown input")
	}
	if err = m.DeleteSessionWithOptions(s.ID, true); err != nil {
		t.Fatal(err)
	}
}
