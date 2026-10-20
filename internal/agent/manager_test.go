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
	for _, options := range []StartOptions{{Name: "x", ResumeThreadID: strings.Repeat("t", 201)}, {Name: "x", ResumeThreadID: "bad\nid"}} {
		if _, err := m.Create(context.Background(), options); err == nil {
			t.Fatal("invalid resume thread id accepted")
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

func compactSequenceFactory() func() *exec.Cmd {
	return func() *exec.Cmd {
		cmd := helperCommand()
		cmd.Env = append(cmd.Env, "AWB_THREAD_SEQUENCE=1")
		return cmd
	}
}
func TestCompactRecreatesThreadAndKeepsWorkbenchHistory(t *testing.T) {
	m, _ := testManager(t)
	m.factory = compactSequenceFactory()
	ctx := context.Background()
	session, err := m.Create(ctx, StartOptions{Name: "压缩"})
	if err != nil {
		t.Fatal(err)
	}
	firstThread := session.ThreadID
	if _, err = m.Send(ctx, session.ID, "short task"); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, session.ID, func(s Session) bool { return s.State == "idle" && s.LastTurnStatus == "completed" })
	compacted, err := m.Compact(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if compacted.ID != session.ID || compacted.ThreadID == "" || compacted.ThreadID == firstThread {
		t.Fatalf("compact did not recreate thread: %+v", compacted)
	}
	if compacted.State != "idle" || compacted.LastTurnStatus != "" {
		t.Fatalf("compact left turn state behind: %+v", compacted)
	}
	events, err := m.Events(session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundCleared, keptHistory := false, false
	for _, event := range events {
		if event.Method == "workbench/contextCleared" {
			foundCleared = true
			if !strings.Contains(string(event.Params), firstThread) || !strings.Contains(string(event.Params), compacted.ThreadID) {
				t.Fatal("contextCleared missing thread transition", string(event.Params))
			}
		}
		if event.Method == "turn/completed" {
			keptHistory = true
		}
	}
	if !foundCleared || !keptHistory {
		t.Fatal("compact lost workbench history", foundCleared, keptHistory)
	}
	if _, err = m.Send(ctx, session.ID, "after compact"); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, session.ID, func(s Session) bool { return s.State == "idle" && s.LastTurnStatus == "completed" })
}

func TestCompactRefusesBusyQueuedClosedAndMissing(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	if _, err := m.Compact(ctx, "missing"); err == nil {
		t.Fatal("missing session accepted")
	}
	session, err := m.Create(ctx, StartOptions{Name: "边界"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Send(ctx, session.ID, "wait"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Compact(ctx, session.ID); err == nil {
		t.Fatal("busy session compacted")
	}
	if result, err := m.Send(ctx, session.ID, "queued"); err != nil || !result.Queued {
		t.Fatalf("busy input not queued: %+v %v", result, err)
	}
	if _, err = m.Compact(ctx, session.ID); err == nil {
		t.Fatal("queued session compacted")
	}
	if err = m.Interrupt(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, session.ID, func(s Session) bool { return s.State == "idle" && s.LastTurnStatus == "interrupted" })
	if err = m.CloseSession(session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Compact(ctx, session.ID); err == nil {
		t.Fatal("closed session compacted")
	}
}

func TestCompactResetsEmptyRecreateEvidence(t *testing.T) {
	m, _ := testManager(t)
	m.factory = func() *exec.Cmd {
		cmd := helperCommand()
		cmd.Env = append(cmd.Env, "AWB_THREAD_SEQUENCE=1", "AWB_RESUME_MISSING=1")
		return cmd
	}
	ctx := context.Background()
	session, err := m.Create(ctx, StartOptions{Name: "清空后重建"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Send(ctx, session.ID, "work"); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, session.ID, func(s Session) bool { return s.State == "idle" && s.LastTurnStatus == "completed" })
	if _, err = m.Compact(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if err = m.CloseSession(session.ID); err != nil {
		t.Fatal(err)
	}
	reconnected, err := m.Resume(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reconnected.State != "idle" {
		t.Fatal("compacted empty thread not recreated", reconnected.State)
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
		t.Fatal("thread replacement not disclosed in log")
	}
}

func TestCompactClearsDiscussionBaseline(t *testing.T) {
	m, _ := testManager(t)
	m.factory = compactSequenceFactory()
	ctx := context.Background()
	parent, err := m.Create(ctx, StartOptions{Name: "工作"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := m.CreateDiscussion(ctx, parent.ID, "PLAN.md")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func(threadID string) DiscussionContext {
		return DiscussionContext{ThreadID: threadID, DocumentID: "PLAN.md", Discussions: map[string]DiscussionBaseline{"D": {Messages: map[string]string{}}}, Selected: []string{"D"}, Full: true}
	}
	if _, err = m.SendDiscussion(ctx, child.ID, "请回应讨论", snapshot(child.ThreadID), ContextHash(nil)); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, child.ID, func(s Session) bool { return s.State == "idle" && s.DiscussionContext != nil })
	compacted, err := m.Compact(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if compacted.DiscussionContext != nil || compacted.ContextUncertain {
		t.Fatalf("compact kept discussion baseline: %+v", compacted)
	}
	if _, err = m.SendDiscussion(ctx, child.ID, "清空后再次回应", snapshot(compacted.ThreadID), ContextHash(nil)); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, child.ID, func(s Session) bool {
		return s.State == "idle" && s.LastTurnStatus == "completed" && s.DiscussionContext != nil && s.DiscussionContext.ThreadID == compacted.ThreadID
	})
}

func TestCreateAttachesExistingNativeThread(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	attached, err := m.Create(ctx, StartOptions{Name: "接入", ResumeThreadID: "cli-thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	if attached.ThreadID != "cli-thread-1" || attached.State != "idle" {
		t.Fatalf("attach did not resume thread: %+v", attached)
	}
	if _, err = m.Send(ctx, attached.ID, "hello"); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, attached.ID, func(s Session) bool { return s.State == "idle" && s.LastTurnStatus == "completed" })
}

func TestAttachDoesNotRecreateMissingRollout(t *testing.T) {
	m, _ := testManager(t)
	m.factory = func() *exec.Cmd {
		cmd := helperCommand()
		cmd.Env = append(cmd.Env, "AWB_RESUME_MISSING=1")
		return cmd
	}
	if _, err := m.Create(context.Background(), StartOptions{Name: "接入缺失", ResumeThreadID: "cli-thread-gone"}); err == nil || !strings.Contains(err.Error(), "no rollout found") {
		t.Fatal("missing rollout silently recreated", err)
	}
	sessions := m.List()
	if len(sessions) != 1 || sessions[0].State != "disconnected" || sessions[0].ThreadID != "cli-thread-gone" {
		t.Fatalf("attach failure altered session: %+v", sessions)
	}
}

func TestAttachedParentDiscussionStartsFreshThread(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	parent, err := m.Create(ctx, StartOptions{Name: "父", ResumeThreadID: "cli-thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	if parent.ThreadID != "cli-thread-1" {
		t.Fatal("parent not attached", parent.ThreadID)
	}
	child, err := m.CreateDiscussion(ctx, parent.ID, "PLAN.md")
	if err != nil {
		t.Fatal(err)
	}
	if child.ThreadID == "" || child.ThreadID == "cli-thread-1" {
		t.Fatal("discussion inherited attached thread", child.ThreadID)
	}
}

func TestListNativeThreads(t *testing.T) {
	threads, err := ListNativeThreads(context.Background(), t.TempDir(), helperCommand)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 3 || threads[0].ID != "native-resumed" || threads[1].ID != "native-new" || threads[2].ID != "native-old" {
		t.Fatalf("native threads not ordered by update time: %+v", threads)
	}
	if threads[0].Preview == "" || threads[1].Name != "新会话" || threads[0].UpdatedAt == "" {
		t.Fatalf("native thread fields: %+v", threads)
	}
}

// 旧版原生不支持 sortKey 时退回不带筛选的请求，仍按更新时间排序。
func TestListNativeThreadsFallsBackForOldRuntime(t *testing.T) {
	old := func() *exec.Cmd {
		cmd := helperCommand()
		cmd.Env = append(cmd.Env, "AWB_OLD_THREAD_LIST=1")
		return cmd
	}
	threads, err := ListNativeThreads(context.Background(), t.TempDir(), old)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 3 || threads[0].ID != "native-resumed" {
		t.Fatalf("fallback list: %+v", threads)
	}
}
