package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func testManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(dir, root, helperCommand)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m, dir
}
func awaitSession(t *testing.T, m *Manager, id string, predicate func(Session) bool) Session {
	t.Helper()
	for i := 0; i < 200; i++ {
		s, err := m.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if predicate(s) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	s, _ := m.Get(id)
	t.Fatalf("session timeout: %+v", s)
	return s
}
func TestSessionQueueApprovalsInterruptAndRecovery(t *testing.T) {
	m, dir := testManager(t)
	ctx := context.Background()
	session, err := m.Create(ctx, StartOptions{Name: "Codex 评审", Sandbox: "workspace-write", ApprovalPolicy: "on-request"})
	if err != nil {
		t.Fatal(err)
	}
	if session.ThreadID != "native-thread" || session.Model != "test-native" || session.State != "idle" {
		t.Fatalf("bad native state: %+v", session)
	}
	if _, err = m.Send(ctx, session.ID, "wait"); err != nil {
		t.Fatal(err)
	}
	if result, err := m.Send(ctx, session.ID, "queued"); err != nil || !result.Queued {
		t.Fatalf("busy input not queued: %+v %v", result, err)
	}
	if err = m.Interrupt(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	stopped := awaitSession(t, m, session.ID, func(s Session) bool { return s.State == "idle" && s.LastTurnStatus == "interrupted" })
	if len(stopped.Queue) != 1 || !stopped.QueuePaused {
		t.Fatal("interrupt replayed queue")
	}
	if err = m.ControlQueue(ctx, session.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, session.ID, func(s Session) bool { return s.State == "idle" && len(s.Queue) == 0 && s.LastTurnStatus == "completed" })
	if _, err = m.Send(ctx, session.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	waiting := awaitSession(t, m, session.ID, func(s Session) bool { return s.State == "waiting" && len(s.Requests) == 1 })
	if err = m.Answer(ctx, session.ID, waiting.Requests[0].ID, json.RawMessage(`{"decision":"acceptForSession"}`)); err == nil {
		t.Fatal("unexpected permanent approval accepted")
	}
	if err = m.Answer(ctx, session.ID, waiting.Requests[0].ID, json.RawMessage(`{"decision":"accept"}`)); err != nil {
		t.Fatal(err)
	}
	if err = m.Answer(ctx, session.ID, waiting.Requests[0].ID, json.RawMessage(`{"decision":"accept"}`)); err == nil {
		t.Fatal("request replied twice")
	}
	awaitSession(t, m, session.ID, func(s Session) bool { return s.State == "idle" })
	events, err := m.Events(session.ID, 0)
	if err != nil || len(events) == 0 {
		t.Fatal("events missing", err)
	}
	last := events[len(events)-1].Sequence
	if tail, err := m.Events(session.ID, last); err != nil || len(tail) != 0 {
		t.Fatal("cursor repeated events", err)
	}
	m.Close()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := NewManager(dir, root, helperCommand)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	saved, err := resumed.Get(session.ID)
	if err != nil || saved.State != "disconnected" || len(saved.Requests) != 0 {
		t.Fatalf("restart claims live runtime: %+v %v", saved, err)
	}
	if _, err = resumed.Send(ctx, session.ID, "must not replay"); err == nil {
		t.Fatal("disconnected input accepted")
	}
	if _, err = resumed.Resume(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if err = resumed.CloseSession(session.ID); err != nil {
		t.Fatal(err)
	}
	closed, _ := resumed.Get(session.ID)
	if closed.State != "closed" {
		t.Fatal("close didn't change state")
	}
}
func TestSessionValidationAndSymlinkStorage(t *testing.T) {
	m, _ := testManager(t)
	for _, options := range []StartOptions{{Name: "x", Sandbox: "danger-full-access", ApprovalPolicy: "never"}, {Name: "x", Sandbox: "workspace-write", ApprovalPolicy: "automatic"}} {
		if _, err := m.Create(context.Background(), options); err == nil {
			t.Fatal("invalid policy accepted")
		}
	}
	if _, err := m.Get("../outside"); err == nil {
		t.Fatal("invalid session accepted")
	}
}

func TestChildThreadCompletionDoesNotCompleteParent(t *testing.T) {
	m, _ := testManager(t)
	session, err := m.Create(context.Background(), StartOptions{Name: "parent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Send(context.Background(), session.ID, "wait"); err != nil {
		t.Fatal(err)
	}
	before, _ := m.Get(session.ID)
	m.onEvent(session.ID, "turn/completed", nil, json.RawMessage(`{"threadId":"child-thread","turn":{"id":"child-turn","status":"completed"}}`))
	after, _ := m.Get(session.ID)
	if after.State != "running" || after.ActiveTurnID != before.ActiveTurnID {
		t.Fatalf("child completion ended parent: %+v", after)
	}
}

func TestInterruptInvalidatesApprovalWaitingToWrite(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	session, err := m.Create(ctx, StartOptions{Name: "审批中断"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Send(ctx, session.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	waiting := awaitSession(t, m, session.ID, func(s Session) bool { return len(s.Requests) == 1 })
	m.mu.Lock()
	c := m.sessions[session.ID].client
	m.mu.Unlock()
	c.writeMu.Lock()
	locked := true
	t.Cleanup(func() {
		if locked {
			c.writeMu.Unlock()
		}
	})
	answered := make(chan error, 1)
	go func() {
		answered <- m.Answer(ctx, session.ID, waiting.Requests[0].ID, json.RawMessage(`{"decision":"accept"}`))
	}()
	started := false
	for i := 0; i < 100; i++ {
		m.mu.Lock()
		started = m.sessions[session.ID].answering[waiting.Requests[0].ID]
		m.mu.Unlock()
		if started {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !started {
		t.Fatal("answer didn't reach write boundary")
	}
	interrupted := make(chan error, 1)
	go func() { interrupted <- m.Interrupt(ctx, session.ID) }()
	awaitSession(t, m, session.ID, func(s Session) bool { return s.QueuePaused })
	c.writeMu.Unlock()
	locked = false
	select {
	case err := <-answered:
		if err == nil {
			t.Fatal("late approval succeeded after interrupt invalidated request")
		}
	case <-time.After(time.Second):
		t.Fatal("approval blocked")
	}
	select {
	case err := <-interrupted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("interrupt blocked")
	}
}

func TestSecretQuestionAnswersAreNotPersistedInLogs(t *testing.T) {
	m, _ := testManager(t)
	session, err := m.Create(context.Background(), StartOptions{Name: "秘密回答"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Send(context.Background(), session.ID, "wait"); err != nil {
		t.Fatal(err)
	}
	m.onEvent(session.ID, "item/tool/requestUserInput", json.RawMessage(`"secret-question"`), json.RawMessage(`{"threadId":"native-thread","questions":[{"id":"token","question":"输入敏感回答","isSecret":true}]}`))
	if err = m.Answer(context.Background(), session.ID, `"secret-question"`, json.RawMessage(`{"answers":{"token":{"answers":["do-not-log-this-secret"]}}}`)); err != nil {
		t.Fatal(err)
	}
	events, err := m.Events(session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if strings.Contains(string(event.Params), "do-not-log-this-secret") {
			t.Fatal("secret answer persisted in event log")
		}
	}
}

func TestEmptySessionReconnectsWhenRolloutMissing(t *testing.T) {
	m, _ := testManager(t)
	m.factory = func() *exec.Cmd {
		cmd := helperCommand()
		cmd.Env = append(cmd.Env, "AWB_RESUME_MISSING=1")
		return cmd
	}
	ctx := context.Background()
	session, err := m.Create(ctx, StartOptions{Name: "empty"})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.CloseSession(session.ID); err != nil {
		t.Fatal(err)
	}
	reconnected, err := m.Resume(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reconnected.ID != session.ID || reconnected.State != "idle" {
		t.Fatal("empty workbench session not reconnected")
	}
	events, err := m.Events(session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Method == "workbench/emptySessionRecreated" {
			found = true
		}
	}
	if !found {
		t.Fatal("native thread replacement not disclosed in log")
	}
}

func TestMissingRolloutDoesNotReplaceSessionWithInput(t *testing.T) {
	m, _ := testManager(t)
	m.factory = func() *exec.Cmd {
		cmd := helperCommand()
		cmd.Env = append(cmd.Env, "AWB_RESUME_MISSING=1")
		return cmd
	}
	ctx := context.Background()
	session, err := m.Create(ctx, StartOptions{Name: "used"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Send(ctx, session.ID, "wait"); err != nil {
		t.Fatal(err)
	}
	if err = m.CloseSession(session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Resume(ctx, session.ID); err == nil {
		t.Fatal("lost history silently replaced")
	}
	saved, _ := m.Get(session.ID)
	if saved.ThreadID != session.ThreadID || saved.State != "disconnected" {
		t.Fatal("failed resume altered original thread")
	}
}

func TestMissingRolloutKeepsLegacyInputOutsideRecentCache(t *testing.T) {
	m, dir := testManager(t)
	factory := func() *exec.Cmd {
		cmd := helperCommand()
		cmd.Env = append(cmd.Env, "AWB_RESUME_MISSING=1")
		return cmd
	}
	m.factory = factory
	ctx := context.Background()
	session, err := m.Create(ctx, StartOptions{Name: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.CloseSession(session.ID); err != nil {
		t.Fatal(err)
	}
	// 旧版状态没有回合标记，但磁盘最早仍存在已接收输入的证据。
	m.mu.Lock()
	r := m.sessions[session.ID]
	err = m.recordLocked(r, "workbench/inputAccepted", nil, json.RawMessage(`{"input":{"text":"previous submission"}}`))
	for i := 0; err == nil && i < 1005; i++ {
		err = m.recordLocked(r, "fixture/diagnostic", nil, nil)
	}
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := NewManager(dir, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, err = restored.Resume(ctx, session.ID); err == nil {
		t.Fatal("old input beyond recent cache silently lost")
	}
}

func TestEmptySessionDoesNotReplaceOnOtherNativeError(t *testing.T) {
	m, _ := testManager(t)
	m.factory = func() *exec.Cmd {
		cmd := helperCommand()
		cmd.Env = append(cmd.Env, "AWB_RESUME_OTHER_ERROR=1")
		return cmd
	}
	ctx := context.Background()
	session, err := m.Create(ctx, StartOptions{Name: "empty"})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.CloseSession(session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Resume(ctx, session.ID); err == nil || !strings.Contains(err.Error(), "configuration unavailable") {
		t.Fatal("unrelated native error hidden", err)
	}
}
