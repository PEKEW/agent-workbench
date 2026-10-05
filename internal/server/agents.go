package server

import (
	"agent-workbench/internal/agent"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) registerAgents(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents", s.listAgents)
	mux.HandleFunc("POST /api/agents", s.createAgent)
	mux.HandleFunc("POST /api/agents/preview", s.previewAgentInput)
	mux.HandleFunc("GET /api/agents/{id}/events", s.agentEvents)
	for _, action := range []string{"input", "interrupt", "close", "resume", "queue"} {
		mux.HandleFunc("POST /api/agents/{id}/"+action, s.agentAction)
	}
	mux.HandleFunc("POST /api/agents/{id}/requests/{requestId}", s.answerAgentRequest)
}
func (s *Server) manager(create bool) (*agent.Manager, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.project == nil {
		return nil, errors.New("请先打开项目")
	}
	if s.agents != nil {
		return s.agents, nil
	}
	root, err := s.project.OpenSessionStorage(create)
	if !create && errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := s.project.Snapshot()
	if err != nil {
		root.Close()
		return nil, err
	}
	manager, err := agent.NewManager(info.Root, root, agent.CodexCommand(s.codexBinary))
	if err != nil {
		root.Close()
		return nil, err
	}
	s.agents = manager
	return manager, nil
}
func decodeAgent(w http.ResponseWriter, r *http.Request, input any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 3*1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		fail(w, 400, "请求格式错误或超过 3 MiB")
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(w, 400, "只允许一个 JSON 对象")
		return false
	}
	return true
}
func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	m, err := s.manager(false)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if m == nil {
		respond(w, 200, []agent.Session{})
		return
	}
	respond(w, 200, m.List())
}

// CreateAgent 供专用 connect 命令及固定 HTTP 接口创建原生会话。
func (s *Server) CreateAgent(ctx context.Context, options agent.StartOptions) (agent.Session, error) {
	m, err := s.manager(true)
	if err != nil {
		return agent.Session{}, err
	}
	return m.Create(ctx, options)
}
func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	var options agent.StartOptions
	if !decodeAgent(w, r, &options) {
		return
	}
	session, err := s.CreateAgent(r.Context(), options)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 201, session)
}
func (s *Server) agentEvents(w http.ResponseWriter, r *http.Request) {
	after := 0
	var err error
	if value := r.URL.Query().Get("after"); value != "" {
		after, err = strconv.Atoi(value)
	}
	if err != nil || after < 0 {
		fail(w, 400, "事件序号无效")
		return
	}
	m, err := s.manager(false)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if m == nil {
		fail(w, 404, "会话不存在")
		return
	}
	events, err := m.Events(r.PathValue("id"), after)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 200, events)
}
func (s *Server) agentAction(w http.ResponseWriter, r *http.Request) {
	m, err := s.manager(false)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if m == nil {
		fail(w, 404, "会话不存在")
		return
	}
	id := r.PathValue("id")
	action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	switch action {
	case "input":
		var input struct {
			Text string `json:"text"`
		}
		if !decodeAgent(w, r, &input) {
			return
		}
		result, e := m.Send(r.Context(), id, input.Text)
		if e != nil {
			fail(w, 409, e.Error())
			return
		}
		respond(w, 200, result)
		return
	case "queue":
		var input struct {
			Action string `json:"action"`
		}
		if !decodeAgent(w, r, &input) {
			return
		}
		err = m.ControlQueue(r.Context(), id, input.Action)
	case "interrupt":
		err = m.Interrupt(r.Context(), id)
	case "close":
		err = m.CloseSession(id)
	case "resume":
		var session agent.Session
		session, err = m.Resume(r.Context(), id)
		if err == nil {
			respond(w, 200, session)
			return
		}
	}
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	session, err := m.Get(id)
	if err != nil {
		fail(w, 404, err.Error())
		return
	}
	respond(w, 200, session)
}
func (s *Server) answerAgentRequest(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Result json.RawMessage `json:"result"`
	}
	if !decodeAgent(w, r, &input) {
		return
	}
	m, err := s.manager(false)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if m == nil {
		fail(w, 404, "会话不存在")
		return
	}
	if err = m.Answer(r.Context(), r.PathValue("id"), r.PathValue("requestId"), input.Result); err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 200, map[string]bool{"answered": true})
}
func (s *Server) previewAgentInput(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Text      string   `json:"text"`
		Documents []string `json:"documents"`
		ThreadID  string   `json:"threadId"`
	}
	if !decodeAgent(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Text) == "" || len(input.Documents) > 20 {
		fail(w, 400, "请输入任务，选定文档最多 20 份")
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.project == nil {
		fail(w, 409, "请先打开项目")
		return
	}
	var prompt strings.Builder
	prompt.WriteString(input.Text)
	seen := map[string]bool{}
	for _, path := range input.Documents {
		if seen[path] {
			continue
		}
		seen[path] = true
		doc, err := s.project.DocumentState(path)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		fmt.Fprintf(&prompt, "\n\n--- 选定文档 %s · SHA-256 %s ---\n%s", path, doc.Revision, doc.Markdown)
		if prompt.Len() > 2*1024*1024 {
			fail(w, 400, "选定上下文超过 2 MiB")
			return
		}
	}
	if input.ThreadID != "" {
		collaboration, err := s.project.Collaboration()
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		found := false
		for _, thread := range collaboration.Threads {
			if thread.ID != input.ThreadID {
				continue
			}
			found = true
			fmt.Fprintf(&prompt, "\n\n--- 手动转发讨论 %s · 文档 %s ---\n", thread.ID, thread.DocumentID)
			if thread.Anchor != nil {
				fmt.Fprintf(&prompt, "引用（rev. %d）：%s\n", thread.Anchor.Revision, thread.Anchor.Quote)
			}
			for _, message := range thread.Messages {
				fmt.Fprintf(&prompt, "\n[%s · %s · %s · %s]\n%s\n", message.Author, message.Role, message.Intent, message.ID, message.Body)
			}
			break
		}
		if !found {
			fail(w, 400, "讨论不存在")
			return
		}
	}
	if prompt.Len() > 2*1024*1024 {
		fail(w, 400, "输入与上下文超过 2 MiB")
		return
	}
	respond(w, 200, map[string]string{"text": prompt.String()})
}
