package agent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// StartOptions 只包含本阶段支持的原生会话配置。
type StartOptions struct {
	Name           string `json:"name"`
	Model          string `json:"model"`
	Sandbox        string `json:"sandbox"`
	ApprovalPolicy string `json:"approvalPolicy"`
	// ResumeThreadID 非空时接入本机已有原生 thread，而非新建上下文。
	ResumeThreadID string `json:"resumeThreadId,omitempty"`
}
type Input struct {
	ID        string             `json:"id"`
	Text      string             `json:"text"`
	CreatedAt string             `json:"createdAt"`
	Context   *DiscussionContext `json:"context,omitempty"`
}
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Session 区分项目记录与当前进程内真实运行状态。
type Session struct {
	ParentSessionID   string             `json:"parentSessionId,omitempty"`
	DocumentID        string             `json:"documentId,omitempty"`
	DiscussionContext *DiscussionContext `json:"discussionContext,omitempty"`
	ContextUncertain  bool               `json:"contextUncertain,omitempty"`
	Replies           []DiscussionReply  `json:"replies,omitempty"`
	DraftReply        *DiscussionReply   `json:"draftReply,omitempty"`
	ID                string             `json:"id"`
	Name              string             `json:"name"`
	ThreadID          string             `json:"threadId"`
	Model             string             `json:"model"`
	Sandbox           json.RawMessage    `json:"sandbox"`
	Options           StartOptions       `json:"options"`
	ApprovalPolicy    json.RawMessage    `json:"approvalPolicy"`
	ApprovalsReviewer string             `json:"approvalsReviewer"`
	State             string             `json:"state"`
	ActiveTurnID      string             `json:"activeTurnId"`
	ActiveInput       *Input             `json:"activeInput"`
	LastTurnID        string             `json:"lastTurnId,omitempty"`
	LastTurnStatus    string             `json:"lastTurnStatus"`
	LastError         string             `json:"lastError"`
	CreatedAt         string             `json:"createdAt"`
	Sequence          int                `json:"sequence"`
	Queue             []Input            `json:"queue"`
	QueuePaused       bool               `json:"queuePaused"`
	Requests          []Request          `json:"requests"`
}
type Event struct {
	Sequence  int             `json:"sequence"`
	CreatedAt string          `json:"createdAt"`
	Method    string          `json:"method"`
	RequestID json.RawMessage `json:"requestId,omitempty"`
	Params    json.RawMessage `json:"params"`
}
type SendResult struct {
	InputID string `json:"inputId"`
	Queued  bool   `json:"queued"`
}
type runtimeSession struct {
	Session
	client         *client
	busy           bool
	deleting       bool
	answering      map[string]bool
	completedTurns map[string]bool
	recent         []Event
}

// Manager 持有本工作台创建的子进程和受限日志目录。
type Manager struct {
	mu       sync.Mutex
	policyMu sync.RWMutex
	root     *os.Root
	cwd      string
	factory  func() *exec.Cmd
	sessions map[string]*runtimeSession
	closed   bool
	policy   CommandAuthorizations
}

// NewManager 接管 storage 句柄；只载入已有记录，不启动任何原生进程。
func NewManager(cwd string, storage *os.Root, factory func() *exec.Cmd) (*Manager, error) {
	m := &Manager{root: storage, cwd: cwd, factory: factory, sessions: map[string]*runtimeSession{}}
	if err := m.load(); err != nil {
		return nil, err
	}
	if err := m.loadAuthorizations(); err != nil {
		return nil, err
	}
	return m, nil
}

// CodexCommand 构造固定协议入口，不通过 shell 执行。
func CodexCommand(binary string) func() *exec.Cmd {
	if binary == "" {
		binary = "codex"
	}
	return func() *exec.Cmd { return exec.Command(binary, "app-server", "--listen", "stdio://") }
}
func cloneSession(s Session) Session {
	data, _ := json.Marshal(s)
	var copied Session
	_ = json.Unmarshal(data, &copied)
	return copied
}
func (m *Manager) Get(id string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.sessions[id]
	if r == nil {
		return Session{}, errors.New("会话不存在")
	}
	return cloneSession(r.Session), nil
}
func (m *Manager) List() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := []Session{}
	for _, r := range m.sessions {
		all = append(all, cloneSession(r.Session))
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt < all[j].CreatedAt })
	return all
}
func validateOptions(o *StartOptions) error {
	o.Name = strings.TrimSpace(o.Name)
	if o.Name == "" {
		o.Name = "Codex"
	}
	if utf8.RuneCountInString(o.Name) > 80 || !utf8.ValidString(o.Name) || len(o.Model) > 200 {
		return errors.New("名称或模型参数过长")
	}
	if o.Sandbox == "" {
		o.Sandbox = "workspace-write"
	}
	if o.ApprovalPolicy == "" {
		o.ApprovalPolicy = "on-request"
	}
	if o.Sandbox != "read-only" && o.Sandbox != "workspace-write" {
		return errors.New("此阶段沙箱必须为 read-only 或 workspace-write")
	}
	if o.ApprovalPolicy != "on-request" && o.ApprovalPolicy != "never" {
		return errors.New("审批策略必须为 on-request 或 never")
	}
	o.ResumeThreadID = strings.TrimSpace(o.ResumeThreadID)
	if strings.IndexFunc(o.ResumeThreadID, unicode.IsControl) >= 0 || len(o.ResumeThreadID) > 200 {
		return errors.New("原生 thread ID 含控制字符或超过 200 字符")
	}
	return nil
}
func (m *Manager) Create(ctx context.Context, o StartOptions) (Session, error) {
	return m.create(ctx, o, "", "")
}
func (m *Manager) create(ctx context.Context, o StartOptions, parentID, documentID string) (Session, error) {
	if err := validateOptions(&o); err != nil {
		return Session{}, err
	}
	m.mu.Lock()
	if parentID != "" {
		parent := m.sessions[parentID]
		if parent == nil || parent.ParentSessionID != "" {
			m.mu.Unlock()
			return Session{}, errors.New("请选择一个工作会话作为参与 Agent")
		}
		for _, existing := range m.sessions {
			if existing.ParentSessionID == parentID && existing.DocumentID == documentID {
				m.mu.Unlock()
				return cloneSession(existing.Session), nil
			}
		}
	}
	if m.closed {
		m.mu.Unlock()
		return Session{}, errors.New("会话服务已关闭")
	}
	count := 0
	for _, r := range m.sessions {
		if r.client != nil || r.State == "starting" {
			count++
		}
	}
	if count >= 8 {
		m.mu.Unlock()
		return Session{}, errors.New("最多同时运行 8 个会话")
	}
	r := &runtimeSession{Session: Session{ID: rand.Text(), Name: o.Name, Options: o, State: "starting", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Queue: []Input{}, Requests: []Request{}}, answering: map[string]bool{}, completedTurns: map[string]bool{}, recent: []Event{}}
	r.ParentSessionID, r.DocumentID = parentID, documentID
	if parentID == "" && o.ResumeThreadID != "" {
		// 接入本机已有原生 thread：记录预置 thread ID，走 thread/resume 连接历史。
		r.ThreadID = o.ResumeThreadID
	}
	m.sessions[r.ID] = r
	if err := m.persistLocked(r); err != nil {
		delete(m.sessions, r.ID)
		m.mu.Unlock()
		return Session{}, err
	}
	m.mu.Unlock()
	return m.start(ctx, r, o.ResumeThreadID != "")
}
func (m *Manager) start(ctx context.Context, r *runtimeSession, resume bool) (Session, error) {
	command := m.factory()
	command.Dir = m.cwd
	c, err := startClient(command, func(method string, id, params json.RawMessage) { m.onEvent(r.ID, method, id, params) })
	if err != nil {
		m.failStart(r, err)
		return Session{}, fmt.Errorf("无法启动 Codex，请确认已安装 CLI：%w", err)
	}
	m.mu.Lock()
	if m.closed || r.deleting || r.State == "closed" {
		m.mu.Unlock()
		c.Close()
		return Session{}, errors.New("会话服务已关闭")
	}
	r.client = c
	m.mu.Unlock()
	requestContext, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	_, err = c.Call(requestContext, "initialize", map[string]any{"clientInfo": map[string]string{"name": "agent_workbench", "title": "Agent Workbench", "version": "0.2.0"}})
	if err == nil {
		err = c.Notify("initialized", map[string]any{})
	}
	params := map[string]any{"cwd": m.cwd, "sandbox": r.Options.Sandbox, "approvalPolicy": r.Options.ApprovalPolicy, "approvalsReviewer": "user"}
	if r.Options.Model != "" {
		params["model"] = r.Options.Model
	}
	method := "thread/start"
	if resume {
		method = "thread/resume"
		params["threadId"] = r.ThreadID
	}
	var value json.RawMessage
	if err == nil {
		value, err = c.Call(requestContext, method, params)
	}
	previousThread := ""
	var rpcErr *nativeRPCError
	// 空会话重建只适用于工作台自建的 thread；按 ID 接入时直接返回原生错误，不静默替换用户指定的历史。
	if resume && r.Options.ResumeThreadID == "" && errors.As(err, &rpcErr) && rpcErr.Code == -32600 && rpcErr.Message == "no rollout found for thread id "+r.ThreadID {
		m.mu.Lock()
		empty, checkErr := m.canRecreateEmptyLocked(r)
		m.mu.Unlock()
		if checkErr != nil {
			err = checkErr
		} else if empty {
			previousThread = r.ThreadID
			delete(params, "threadId")
			value, err = c.Call(requestContext, "thread/start", params)
		}
	}
	var native struct {
		Thread struct {
			ID     string `json:"id"`
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
		} `json:"thread"`
		Model             string          `json:"model"`
		Sandbox           json.RawMessage `json:"sandbox"`
		ApprovalPolicy    json.RawMessage `json:"approvalPolicy"`
		ApprovalsReviewer string          `json:"approvalsReviewer"`
	}
	if err == nil {
		err = json.Unmarshal(value, &native)
		if err == nil && native.Thread.ID == "" {
			err = errors.New("原生会话未返回 thread ID")
		}
	}
	if err != nil {
		c.Close()
		m.failStart(r, err)
		return Session{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || r.deleting || r.State == "closed" {
		go c.Close()
		return Session{}, errors.New("会话已关闭")
	}
	select {
	case <-c.done:
		r.State = "disconnected"
		return Session{}, errors.New("原生进程已退出")
	default:
	}
	r.ThreadID = native.Thread.ID
	r.Model = native.Model
	r.Sandbox = native.Sandbox
	r.ApprovalPolicy = native.ApprovalPolicy
	r.ApprovalsReviewer = native.ApprovalsReviewer
	r.State = "idle"
	r.busy = false
	r.Requests = []Request{}
	r.ActiveTurnID = ""
	if native.Thread.Status.Type == "active" {
		r.State = "disconnected"
		r.LastError = "原生会话仍显示活动状态，不能在此状态发送新回合"
	} else {
		r.LastError = ""
	}
	if previousThread != "" {
		change, _ := json.Marshal(map[string]string{"previousThreadId": previousThread, "threadId": r.ThreadID, "reason": "空会话尚无可恢复历史，已重新连接；没有重发任务"})
		if err = m.recordLocked(r, "workbench/emptySessionRecreated", nil, change); err != nil {
			return Session{}, err
		}
	}
	if err = m.recordLocked(r, "workbench/sessionConnected", nil, value); err != nil {
		return Session{}, err
	}
	if err = m.persistLocked(r); err != nil {
		return Session{}, err
	}
	return cloneSession(r.Session), nil
}
func (m *Manager) failStart(r *runtimeSession, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.State != "closed" {
		r.State = "disconnected"
		r.LastError = err.Error()
		r.client = nil
		r.busy = false
		r.QueuePaused = true
		_ = m.persistLocked(r)
	}
}
func (m *Manager) Send(ctx context.Context, id, text string) (SendResult, error) {
	return m.send(ctx, id, text, nil, "")
}
func (m *Manager) send(ctx context.Context, id, text string, snapshot *DiscussionContext, baseline string) (SendResult, error) {
	if strings.TrimSpace(text) == "" || len(text) > 2*1024*1024 || !utf8.ValidString(text) {
		return SendResult{}, errors.New("输入须为有效文本，且不超过 2 MiB")
	}
	input := Input{ID: rand.Text(), Text: text, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	input.Context = snapshot
	m.mu.Lock()
	r := m.sessions[id]
	if r == nil || r.client == nil || r.State == "disconnected" || r.State == "closed" || r.State == "starting" {
		m.mu.Unlock()
		return SendResult{}, errors.New("会话未连接，请先创建或恢复")
	}
	// 讨论专用会话只接收其文档的讨论上下文；其他会话可附带任一文档的讨论上下文，也可发送普通输入。
	if r.ParentSessionID != "" && (snapshot == nil || snapshot.DocumentID != r.DocumentID) {
		m.mu.Unlock()
		return SendResult{}, errors.New("讨论专用会话须发送与其关联的上下文预览")
	}
	if snapshot != nil {
		if snapshot.ThreadID != r.ThreadID {
			m.mu.Unlock()
			return SendResult{}, errors.New("原生会话已变化，请重新预览")
		}
		if ContextHash(r.DiscussionContext) != baseline || r.ContextUncertain && !snapshot.Full {
			m.mu.Unlock()
			return SendResult{}, errors.New("同步基线已变化或送达结果不明，请重新预览并完整发送")
		}
	}
	queued := r.busy || len(r.Queue) > 0 || r.QueuePaused
	if queued && len(r.Queue) >= 20 {
		m.mu.Unlock()
		return SendResult{}, errors.New("排队输入已达 20 条")
	}
	if queued {
		r.Queue = append(r.Queue, input)
	} else {
		r.busy = true
		r.State = "running"
		r.ActiveInput = &input
	}
	data, _ := json.Marshal(map[string]any{"input": input, "queued": queued})
	if err := m.recordLocked(r, "workbench/inputAccepted", nil, data); err != nil {
		if queued {
			r.Queue = r.Queue[:len(r.Queue)-1]
		} else {
			r.busy = false
			r.State = "idle"
			r.ActiveInput = nil
		}
		m.mu.Unlock()
		return SendResult{}, err
	}
	if err := m.persistLocked(r); err != nil {
		r.State = "disconnected"
		r.QueuePaused = true
		m.mu.Unlock()
		return SendResult{}, err
	}
	m.mu.Unlock()
	if !queued {
		if err := m.runTurn(ctx, r, input); err != nil {
			return SendResult{InputID: input.ID}, err
		}
	}
	return SendResult{InputID: input.ID, Queued: queued}, nil
}
func (m *Manager) runTurn(ctx context.Context, r *runtimeSession, input Input) error {
	m.mu.Lock()
	c := r.client
	threadID := r.ThreadID
	if c != nil {
		data, _ := json.Marshal(map[string]string{"inputId": input.ID})
		if err := m.recordLocked(r, "workbench/turnSubmitted", nil, data); err != nil {
			r.LastError = err.Error()
			r.QueuePaused = true
			r.State = "disconnected"
			_ = m.persistLocked(r)
			m.mu.Unlock()
			c.Close()
			return err
		}
	}
	m.mu.Unlock()
	if c == nil {
		return errors.New("原生连接已关闭")
	}
	requestContext, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	value, err := c.Call(requestContext, "turn/start", map[string]any{"threadId": threadID, "clientUserMessageId": input.ID, "input": []map[string]string{{"type": "text", "text": input.Text}}})
	m.mu.Lock()
	if err != nil {
		if input.Context != nil {
			r.ContextUncertain = true
		}
		r.LastError = err.Error()
		if r.State != "closed" {
			r.State = "disconnected"
		}
		r.QueuePaused = true
		_ = m.persistLocked(r)
		m.mu.Unlock()
		c.Close()
		return err
	}
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(value, &response)
	if r.busy && r.ActiveInput != nil && r.ActiveInput.ID == input.ID && r.State != "closed" && r.State != "disconnected" {
		r.ActiveTurnID = response.Turn.ID
		if input.Context != nil && (r.DiscussionContext == nil || r.DiscussionContext.InputID != input.ID) {
			data, _ := json.Marshal(map[string]string{"inputId": input.ID, "turnId": response.Turn.ID, "threadId": threadID})
			if err = m.recordLocked(r, "workbench/inputDelivered", nil, data); err != nil {
				r.ContextUncertain = true
				r.QueuePaused = true
				_ = m.persistLocked(r)
				m.mu.Unlock()
				return err
			}
		}
		m.deliverDiscussionLocked(r, input, response.Turn.ID)
	}
	persistErr := m.persistLocked(r)
	m.mu.Unlock()
	return persistErr
}
func (m *Manager) onEvent(id, method string, requestID, params json.RawMessage) {
	m.mu.Lock()
	r := m.sessions[id]
	if r == nil {
		m.mu.Unlock()
		return
	}
	if err := m.recordLocked(r, method, requestID, params); err != nil {
		r.LastError = "日志保存失败：" + err.Error()
		if r.State != "closed" {
			r.State = "disconnected"
		}
		r.QueuePaused = true
		c := r.client
		m.mu.Unlock()
		if c != nil {
			go c.Close()
		}
		return
	}
	var p struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID     string          `json:"id"`
			Status string          `json:"status"`
			Error  json.RawMessage `json:"error"`
		} `json:"turn"`
		RequestID json.RawMessage `json:"requestId"`
		Error     string          `json:"error"`
	}
	_ = json.Unmarshal(params, &p)
	if method == "turn/started" && r.completedTurns[p.Turn.ID] {
		m.mu.Unlock()
		return
	}
	m.observeDiscussionLocked(r, method, params)
	persist := false
	if len(requestID) > 0 && string(requestID) != "null" {
		key := string(requestID)
		r.Requests = append(r.Requests, Request{ID: key, Method: method, Params: append(json.RawMessage(nil), params...)})
		if r.State != "closed" {
			r.State = "waiting"
		}
		persist = true
	}
	switch method {
	case "turn/started":
		if p.ThreadID != "" && p.ThreadID != r.ThreadID {
			break
		}
		if r.ActiveInput == nil || r.ActiveTurnID != "" && r.ActiveTurnID != p.Turn.ID {
			break
		}
		if r.State != "closed" {
			r.ActiveTurnID = p.Turn.ID
			r.State = "running"
			r.busy = true
		}
		persist = true
	case "serverRequest/resolved":
		key := string(p.RequestID)
		for i, q := range r.Requests {
			if q.ID == key {
				r.Requests = append(r.Requests[:i], r.Requests[i+1:]...)
				break
			}
		}
		if len(r.Requests) == 0 && r.State == "waiting" {
			r.State = "running"
		}
		persist = true
	case "turn/completed":
		if p.ThreadID != "" && p.ThreadID != r.ThreadID {
			break
		}
		if p.Turn.ID == "" || p.Turn.ID != r.ActiveTurnID {
			break
		}
		r.ActiveTurnID = ""
		r.ActiveInput = nil
		r.busy = false
		r.completedTurns[p.Turn.ID] = true
		r.LastTurnID = p.Turn.ID
		r.LastTurnStatus = p.Turn.Status
		r.Requests = []Request{}
		r.answering = map[string]bool{}
		if r.State != "closed" && r.State != "disconnected" {
			r.State = "idle"
		}
		if p.Turn.Status != "completed" {
			r.QueuePaused = true
			if len(p.Turn.Error) > 0 && string(p.Turn.Error) != "null" {
				r.LastError = string(p.Turn.Error)
			}
		}
		persist = true
	case "workbench/runtimeExited":
		r.client = nil
		r.busy = false
		r.Requests = []Request{}
		r.ActiveTurnID = ""
		r.QueuePaused = true
		if r.State != "closed" {
			r.State = "disconnected"
			r.LastError = p.Error
		}
		persist = true
	}
	if persist {
		if err := m.persistLocked(r); err != nil {
			r.LastError = err.Error()
			r.QueuePaused = true
		}
	}
	var next *Input
	if method == "turn/completed" && (p.ThreadID == "" || p.ThreadID == r.ThreadID) && r.State == "idle" && !r.QueuePaused && len(r.Queue) > 0 {
		input := r.Queue[0]
		r.Queue = r.Queue[1:]
		r.ActiveInput = &input
		r.busy = true
		r.State = "running"
		next = &input
		_ = m.persistLocked(r)
	}
	m.mu.Unlock()
	if method == "item/commandExecution/requestApproval" && len(requestID) > 0 {
		go m.autoApproveCommand(id, string(requestID))
	}
	if next != nil {
		go func() { _ = m.runTurn(context.Background(), r, *next) }()
	}
}
func (m *Manager) Interrupt(ctx context.Context, id string) error {
	m.mu.Lock()
	r := m.sessions[id]
	if r == nil || r.client == nil || !r.busy || r.ActiveTurnID == "" {
		m.mu.Unlock()
		return errors.New("没有可中断的运行回合")
	}
	c, threadID, turnID := r.client, r.ThreadID, r.ActiveTurnID
	r.QueuePaused = true
	r.Requests = []Request{}
	_ = m.persistLocked(r)
	m.mu.Unlock()
	requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := c.Call(requestContext, "turn/interrupt", map[string]string{"threadId": threadID, "turnId": turnID})
	return err
}
func (m *Manager) ControlQueue(ctx context.Context, id, action string) error {
	m.mu.Lock()
	r := m.sessions[id]
	if r == nil {
		m.mu.Unlock()
		return errors.New("会话不存在")
	}
	if action == "clear" {
		r.Queue = []Input{}
		r.QueuePaused = false
		if err := m.recordLocked(r, "workbench/queueCleared", nil, json.RawMessage(`{}`)); err != nil {
			m.mu.Unlock()
			return err
		}
		err := m.persistLocked(r)
		m.mu.Unlock()
		return err
	}
	if action != "continue" || r.client == nil || r.State != "idle" || r.busy {
		m.mu.Unlock()
		return errors.New("仅空闲连接可继续队列")
	}
	if r.ContextUncertain {
		m.mu.Unlock()
		return errors.New("上次送达结果不明，请清空旧队列并完整重新预览")
	}
	r.QueuePaused = false
	var next *Input
	if len(r.Queue) > 0 {
		input := r.Queue[0]
		r.Queue = r.Queue[1:]
		r.ActiveInput = &input
		r.busy = true
		r.State = "running"
		next = &input
	}
	if err := m.persistLocked(r); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	if next != nil {
		return m.runTurn(ctx, r, *next)
	}
	return nil
}

// Resume 只恢复本工作台记录的 thread；不自动重发旧输入。
func (m *Manager) Resume(ctx context.Context, id string) (Session, error) {
	m.mu.Lock()
	r := m.sessions[id]
	if m.closed || r == nil || r.deleting || r.ThreadID == "" || r.client != nil || r.State == "starting" {
		m.mu.Unlock()
		return Session{}, errors.New("此会话不能恢复")
	}
	count := 0
	for _, session := range m.sessions {
		if session.client != nil || session.State == "starting" {
			count++
		}
	}
	if count >= 8 {
		m.mu.Unlock()
		return Session{}, errors.New("最多同时运行 8 个会话")
	}
	r.State = "starting"
	r.QueuePaused = len(r.Queue) > 0
	r.busy = false
	_ = m.persistLocked(r)
	m.mu.Unlock()
	return m.start(ctx, r, true)
}
func (m *Manager) CloseSession(id string) error {
	m.mu.Lock()
	r := m.sessions[id]
	if r == nil {
		m.mu.Unlock()
		return errors.New("会话不存在")
	}
	r.State = "closed"
	r.QueuePaused = true
	r.Requests = []Request{}
	c := r.client
	err := m.persistLocked(r)
	m.mu.Unlock()
	if c != nil {
		c.Close()
	}
	return err
}

// Compact 清空原生上下文：在现有连接上重建 thread，保留工作台会话、选项与全部日志。
func (m *Manager) Compact(ctx context.Context, id string) (Session, error) {
	m.mu.Lock()
	r := m.sessions[id]
	if m.closed || r == nil || r.deleting {
		m.mu.Unlock()
		return Session{}, errors.New("会话不存在")
	}
	if r.client == nil || r.State != "idle" || r.busy || r.ActiveInput != nil || r.ActiveTurnID != "" || len(r.Queue) != 0 || len(r.Requests) != 0 {
		m.mu.Unlock()
		return Session{}, errors.New("仅空闲且已连接、无队列的会话可清空上下文")
	}
	c := r.client
	previousThread := r.ThreadID
	params := map[string]any{"cwd": m.cwd, "sandbox": r.Options.Sandbox, "approvalPolicy": r.Options.ApprovalPolicy, "approvalsReviewer": "user"}
	if r.Options.Model != "" {
		params["model"] = r.Options.Model
	}
	r.State = "starting"
	if err := m.persistLocked(r); err != nil {
		r.State = "idle"
		m.mu.Unlock()
		return Session{}, err
	}
	m.mu.Unlock()
	requestContext, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	value, err := c.Call(requestContext, "thread/start", params)
	var native struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model             string          `json:"model"`
		Sandbox           json.RawMessage `json:"sandbox"`
		ApprovalPolicy    json.RawMessage `json:"approvalPolicy"`
		ApprovalsReviewer string          `json:"approvalsReviewer"`
	}
	if err == nil {
		err = json.Unmarshal(value, &native)
		if err == nil && native.Thread.ID == "" {
			err = errors.New("原生会话未返回 thread ID")
		}
	}
	if err != nil {
		m.mu.Lock()
		select {
		case <-c.done:
		default:
			if !m.closed && !r.deleting && r.State == "starting" {
				r.State = "idle"
				r.LastError = err.Error()
				_ = m.persistLocked(r)
			}
		}
		m.mu.Unlock()
		return Session{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || r.deleting || r.State == "closed" {
		go c.Close()
		return Session{}, errors.New("会话已关闭")
	}
	r.ThreadID = native.Thread.ID
	r.Model = native.Model
	r.Sandbox = native.Sandbox
	r.ApprovalPolicy = native.ApprovalPolicy
	r.ApprovalsReviewer = native.ApprovalsReviewer
	r.State = "idle"
	r.busy = false
	r.Requests = []Request{}
	r.ActiveTurnID = ""
	r.ActiveInput = nil
	r.LastTurnID = ""
	r.LastTurnStatus = ""
	r.LastError = ""
	r.DiscussionContext = nil
	r.ContextUncertain = false
	change, _ := json.Marshal(map[string]string{"previousThreadId": previousThread, "threadId": r.ThreadID, "reason": "已清空原生上下文并重建 thread；未重发任务"})
	if err = m.recordLocked(r, "workbench/contextCleared", nil, change); err != nil {
		return Session{}, err
	}
	if err = m.persistLocked(r); err != nil {
		return Session{}, err
	}
	return cloneSession(r.Session), nil
}

// Close 只结束本 Manager 创建的子进程并释放状态目录。
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	clients := []*client{}
	for _, r := range m.sessions {
		if r.client != nil {
			clients = append(clients, r.client)
		}
	}
	m.mu.Unlock()
	for _, c := range clients {
		c.Close()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.root.Close()
}
