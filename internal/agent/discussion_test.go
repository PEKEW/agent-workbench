package agent

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestDiscussionIsolationDeliveryAndRecovery(t *testing.T) {
	m, dir := testManager(t)
	ctx := context.Background()
	parent, err := m.Create(ctx, StartOptions{Name: "工作"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := m.CreateDiscussion(ctx, parent.ID, "PLAN.md")
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.CreateDiscussion(ctx, parent.ID, "PLAN.md")
	if err != nil || again.ID != child.ID {
		t.Fatal("discussion not reused", err)
	}
	other, err := m.CreateDiscussion(ctx, parent.ID, "docs/other.md")
	if err != nil || other.ID == child.ID || child.ID == parent.ID || child.ParentSessionID != parent.ID {
		t.Fatal("contexts not isolated", err)
	}
	snapshot := DiscussionContext{ThreadID: child.ThreadID, DocumentID: "PLAN.md", DocumentRevision: "rev1", Discussions: map[string]DiscussionBaseline{"discussion-A": {Messages: map[string]string{"C1": "hash1"}}}, Selected: []string{"discussion-A"}}
	if _, err = m.SendDiscussion(ctx, child.ID, "wait", snapshot, ContextHash(nil)); err != nil {
		t.Fatal(err)
	}
	active := awaitSession(t, m, child.ID, func(s Session) bool { return s.DiscussionContext != nil })
	if active.DiscussionContext.Discussions["discussion-A"].Messages["C1"] != "hash1" {
		t.Fatal("delivery not recorded")
	}
	queued := snapshot
	queued.Discussions = map[string]DiscussionBaseline{"discussion-A": {Messages: map[string]string{"C1": "hash1", "C2": "hash2"}}}
	if _, err = m.SendDiscussion(ctx, child.ID, "next", queued, ContextHash(active.DiscussionContext)); err != nil {
		t.Fatal(err)
	}
	state, _ := m.Get(child.ID)
	if state.DiscussionContext.Discussions["discussion-A"].Messages["C2"] != "" {
		t.Fatal("queue advanced delivered cursor")
	}
	if err = m.ControlQueue(ctx, child.ID, "clear"); err != nil {
		t.Fatal(err)
	}
	if err = m.Interrupt(ctx, child.ID); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, child.ID, func(s Session) bool { return s.State == "idle" })
	state, _ = m.Get(child.ID)
	if len(state.Replies) != 0 {
		t.Fatal("interrupted reply synchronized")
	}
	if err = m.ControlQueue(ctx, child.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.SendDiscussion(ctx, child.ID, "done", queued, ContextHash(state.DiscussionContext)); err != nil {
		t.Fatal(err)
	}
	state = awaitSession(t, m, child.ID, func(s Session) bool { return len(s.Replies) == 1 })
	reply := state.Replies[0]
	if len(reply.Discussions) != 1 || reply.Discussions[0] != "discussion-A" || reply.ItemID == "" || reply.TurnID == "" {
		t.Fatal("reply lost origin")
	}
	if _, err = m.SendDiscussion(ctx, child.ID, "stale", snapshot, "stale"); err == nil {
		t.Fatal("stale baseline accepted")
	}
	if _, err = m.Send(ctx, child.ID, "bypass"); err == nil {
		t.Fatal("discussion input bypassed snapshot")
	}
	m.Close()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := NewManager(dir, root, helperCommand)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	saved, _ := restored.Get(child.ID)
	if len(saved.Replies) != 1 || saved.ParentSessionID != parent.ID || saved.DiscussionContext.Discussions["discussion-A"].Messages["C2"] != "hash2" {
		t.Fatal("restart lost synchronization state")
	}
}

func TestDiscussionIgnoresForeignAndCommentaryItems(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	parent, _ := m.Create(ctx, StartOptions{Name: "工作"})
	child, err := m.CreateDiscussion(ctx, parent.ID, "PLAN.md")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := DiscussionContext{ThreadID: child.ThreadID, DocumentID: "PLAN.md", Discussions: map[string]DiscussionBaseline{"D": {Messages: map[string]string{}}}, Selected: []string{"D"}}
	if _, err = m.SendDiscussion(ctx, child.ID, "wait", snapshot, ContextHash(nil)); err != nil {
		t.Fatal(err)
	}
	state := awaitSession(t, m, child.ID, func(s Session) bool { return s.ActiveTurnID != "" })
	emit := func(thread, turn, item, text, phase string) {
		data, _ := json.Marshal(map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{"id": item, "type": "agentMessage", "text": text, "phase": phase}})
		m.onEvent(child.ID, "item/completed", nil, data)
	}
	emit(child.ThreadID, state.ActiveTurnID, "final", "final reply", "final_answer")
	emit(child.ThreadID, state.ActiveTurnID, "comment", "late commentary", "commentary")
	emit("foreign", state.ActiveTurnID, "foreign", "wrong", "final_answer")
	data, _ := json.Marshal(map[string]any{"threadId": child.ThreadID, "turn": map[string]any{"id": state.ActiveTurnID, "status": "completed"}})
	m.onEvent(child.ID, "turn/completed", nil, data)
	m.onEvent(child.ID, "turn/completed", nil, data)
	state, _ = m.Get(child.ID)
	if len(state.Replies) != 1 || state.Replies[0].Body != "final reply" {
		t.Fatalf("incorrect final reply: %+v", state.Replies)
	}
}

func TestDiscussionRecoveryUsesFullLog(t *testing.T) {
	m, dir := testManager(t)
	ctx := context.Background()
	parent, _ := m.Create(ctx, StartOptions{Name: "工作"})
	child, err := m.CreateDiscussion(ctx, parent.ID, "PLAN.md")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := DiscussionContext{ThreadID: child.ThreadID, DocumentID: "PLAN.md", Discussions: map[string]DiscussionBaseline{"D": {Messages: map[string]string{}}}, Selected: []string{"D"}}
	if _, err = m.SendDiscussion(ctx, child.ID, "done", snapshot, ContextHash(nil)); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, child.ID, func(s Session) bool { return len(s.Replies) == 1 })
	m.mu.Lock()
	r := m.sessions[child.ID]
	r.Replies = nil // 模拟最终回合写了日志，但待回写状态尚未落盘。
	for i := 0; i < 1100; i++ {
		if err = m.recordLocked(r, "fixture/diagnostic", nil, nil); err != nil {
			break
		}
	}
	if err == nil {
		err = m.persistLocked(r)
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
	restored, err := NewManager(dir, root, helperCommand)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	saved, _ := restored.Get(child.ID)
	if len(saved.Replies) != 1 {
		t.Fatal("completed reply outside recent cache lost during recovery")
	}
}

func TestDuplicateCompletionDoesNotEndNextDiscussionTurn(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	parent, _ := m.Create(ctx, StartOptions{Name: "工作"})
	child, err := m.CreateDiscussion(ctx, parent.ID, "PLAN.md")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := DiscussionContext{ThreadID: child.ThreadID, DocumentID: "PLAN.md", Discussions: map[string]DiscussionBaseline{"D": {Messages: map[string]string{}}}, Selected: []string{"D"}}
	if _, err = m.SendDiscussion(ctx, child.ID, "done", snapshot, ContextHash(nil)); err != nil {
		t.Fatal(err)
	}
	prior := awaitSession(t, m, child.ID, func(s Session) bool { return len(s.Replies) == 1 && s.State == "idle" })
	if _, err = m.SendDiscussion(ctx, child.ID, "wait", snapshot, ContextHash(prior.DiscussionContext)); err != nil {
		t.Fatal(err)
	}
	active := awaitSession(t, m, child.ID, func(s Session) bool { return s.ActiveTurnID != "" })
	data, _ := json.Marshal(map[string]any{"threadId": child.ThreadID, "turn": map[string]any{"id": prior.Replies[0].TurnID, "status": "completed"}})
	m.onEvent(child.ID, "turn/completed", nil, data)
	after, _ := m.Get(child.ID)
	if after.State != "running" || after.ActiveTurnID != active.ActiveTurnID {
		t.Fatal("old duplicate completion ended new turn")
	}
}

func TestUncertainDiscussionCannotContinueOldQueue(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	parent, _ := m.Create(ctx, StartOptions{Name: "工作"})
	child, err := m.CreateDiscussion(ctx, parent.ID, "PLAN.md")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := DiscussionContext{ThreadID: child.ThreadID, DocumentID: "PLAN.md", Discussions: map[string]DiscussionBaseline{"D": {Messages: map[string]string{"C1": "h1"}}}, Selected: []string{"D"}}
	if _, err = m.SendDiscussion(ctx, child.ID, "done", snapshot, ContextHash(nil)); err != nil {
		t.Fatal(err)
	}
	state := awaitSession(t, m, child.ID, func(s Session) bool { return len(s.Replies) == 1 && s.State == "idle" })
	baseline := ContextHash(state.DiscussionContext)
	timeout, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, e := m.SendDiscussion(timeout, child.ID, "uncertain", snapshot, baseline); finished <- e }()
	awaitSession(t, m, child.ID, func(s Session) bool { return s.State == "running" })
	if result, e := m.SendDiscussion(ctx, child.ID, "queued", snapshot, baseline); e != nil || !result.Queued {
		t.Fatal("queue", e)
	}
	if err = <-finished; err == nil {
		t.Fatal("silent request unexpectedly confirmed")
	}
	if _, err = m.Resume(ctx, child.ID); err != nil {
		t.Fatal(err)
	}
	if err = m.ControlQueue(ctx, child.ID, "continue"); err == nil {
		t.Fatal("unconfirmed context replayed old queue")
	}
	state, _ = m.Get(child.ID)
	if !state.ContextUncertain || len(state.Queue) != 1 {
		t.Fatal("uncertainty or queue was cleared")
	}
}
func TestDiscussionRecoveryAdvancesStaleDeliveredContext(t *testing.T) {
	m, dir := testManager(t)
	ctx := context.Background()
	parent, _ := m.Create(ctx, StartOptions{Name: "工作"})
	child, err := m.CreateDiscussion(ctx, parent.ID, "PLAN.md")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := DiscussionContext{ThreadID: child.ThreadID, DocumentID: "PLAN.md", Discussions: map[string]DiscussionBaseline{"D": {Messages: map[string]string{"C1": "h1"}}}, Selected: []string{"D"}}
	if _, err = m.SendDiscussion(ctx, child.ID, "done", snapshot, ContextHash(nil)); err != nil {
		t.Fatal(err)
	}
	first := awaitSession(t, m, child.ID, func(s Session) bool { return len(s.Replies) == 1 && s.State == "idle" })
	snapshot.Discussions = map[string]DiscussionBaseline{"D": {Messages: map[string]string{"C1": "h1", "C2": "h2"}}}
	if _, err = m.SendDiscussion(ctx, child.ID, "done", snapshot, ContextHash(first.DiscussionContext)); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, child.ID, func(s Session) bool { return len(s.Replies) == 2 && s.State == "idle" })
	m.mu.Lock()
	m.sessions[child.ID].DiscussionContext = first.DiscussionContext
	err = m.persistLocked(m.sessions[child.ID])
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := NewManager(dir, root, helperCommand)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	saved, _ := restored.Get(child.ID)
	if saved.DiscussionContext.Discussions["D"].Messages["C2"] != "h2" {
		t.Fatal("newer confirmed context lost behind stale state")
	}
}
func TestDuplicateStartedCannotRedirectActiveDiscussion(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	parent, _ := m.Create(ctx, StartOptions{Name: "工作"})
	child, err := m.CreateDiscussion(ctx, parent.ID, "PLAN.md")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := DiscussionContext{ThreadID: child.ThreadID, DocumentID: "PLAN.md", Discussions: map[string]DiscussionBaseline{"D": {Messages: map[string]string{}}}, Selected: []string{"D"}}
	if _, err = m.SendDiscussion(ctx, child.ID, "done", snapshot, ContextHash(nil)); err != nil {
		t.Fatal(err)
	}
	first := awaitSession(t, m, child.ID, func(s Session) bool { return len(s.Replies) == 1 && s.State == "idle" })
	if _, err = m.SendDiscussion(ctx, child.ID, "wait", snapshot, ContextHash(first.DiscussionContext)); err != nil {
		t.Fatal(err)
	}
	active := awaitSession(t, m, child.ID, func(s Session) bool { return s.ActiveTurnID != "" })
	data, _ := json.Marshal(map[string]any{"threadId": child.ThreadID, "turn": map[string]any{"id": first.Replies[0].TurnID, "status": "inProgress"}})
	m.onEvent(child.ID, "turn/started", nil, data)
	state, _ := m.Get(child.ID)
	if state.ActiveTurnID != active.ActiveTurnID {
		t.Fatal("old turn/started redirected active input")
	}
}

func TestQueuedDiscussionRejectsOldStartBeforeNextConfirmation(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	parent, _ := m.Create(ctx, StartOptions{Name: "工作"})
	child, err := m.CreateDiscussion(ctx, parent.ID, "PLAN.md")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := DiscussionContext{ThreadID: child.ThreadID, DocumentID: "PLAN.md", Discussions: map[string]DiscussionBaseline{"D": {Messages: map[string]string{"C1": "h1"}}}, Selected: []string{"D"}}
	if _, err = m.SendDiscussion(ctx, child.ID, "wait", snapshot, ContextHash(nil)); err != nil {
		t.Fatal(err)
	}
	active := awaitSession(t, m, child.ID, func(s Session) bool { return s.ActiveTurnID != "" })
	queued := snapshot
	queued.Discussions = map[string]DiscussionBaseline{"D": {Messages: map[string]string{"C1": "h1", "C2": "h2"}}}
	result, err := m.SendDiscussion(ctx, child.ID, "wait", queued, ContextHash(active.DiscussionContext))
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	c := m.sessions[child.ID].client
	m.mu.Unlock()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	completed, _ := json.Marshal(map[string]any{"threadId": child.ThreadID, "turn": map[string]any{"id": active.ActiveTurnID, "status": "completed"}})
	m.onEvent(child.ID, "turn/completed", nil, completed)
	started, _ := json.Marshal(map[string]any{"threadId": child.ThreadID, "turn": map[string]any{"id": active.ActiveTurnID, "status": "inProgress"}})
	m.onEvent(child.ID, "turn/started", nil, started)
	m.onEvent(child.ID, "turn/completed", nil, completed)
	state, _ := m.Get(child.ID)
	if state.ActiveInput == nil || state.ActiveInput.ID != result.InputID || state.ActiveTurnID != "" || state.DiscussionContext.Discussions["D"].Messages["C2"] != "" {
		t.Fatal("old events consumed queued input before native confirmation")
	}
}

func TestRouteReplyAssignsSectionsByHeading(t *testing.T) {
	single, err := RouteReply("  整条回复  ", []string{"A"})
	if err != nil || single["A"] != "整条回复" {
		t.Fatal(single, err)
	}
	body := "说明不回写\n### 讨论 A\n回应 A\n\n## 讨论：B · 关于第二段\n回应 B\n### 讨论 Z\n未列出的标题留在 B 中\n#### 讨论 `A`\n补充 A"
	sections, err := RouteReply(body, []string{"A", "B"})
	if err != nil || sections["A"] != "回应 A\n\n补充 A" || sections["B"] != "回应 B\n### 讨论 Z\n未列出的标题留在 B 中" || len(sections) != 2 {
		t.Fatalf("%q %v", sections, err)
	}
	for _, bad := range []string{"没有标题的多条回复", "### 讨论 A\n   \n### 讨论 B\n"} {
		if _, err = RouteReply(bad, []string{"A", "B"}); err != ErrUnroutedReply {
			t.Fatalf("%q 应拒绝：%v", bad, err)
		}
	}
	if _, err = RouteReply("### 讨论 A\n回应", nil); err != ErrUnroutedReply {
		t.Fatal("未列出讨论时不应回写", err)
	}
}
