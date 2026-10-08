package server

import (
	"agent-workbench/internal/agent"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) registerAgents(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/agents/{id}/discussion", s.createDiscussion)
	mux.HandleFunc("GET /api/agents/{id}/memory-hooks", s.memoryHooks)
	mux.HandleFunc("POST /api/agents/{id}/memory-hooks/trust", s.trustMemoryHooks)
	mux.HandleFunc("GET /api/memory/health", s.memoryHealth)
	mux.HandleFunc("GET /api/memory/endpoint", s.memoryEndpoint)
	mux.HandleFunc("GET /api/agents", s.listAgents)
	mux.HandleFunc("GET /api/authorizations", s.authorizations)
	mux.HandleFunc("POST /api/authorizations", s.authorizations)
	mux.HandleFunc("POST /api/agents", s.createAgent)
	mux.HandleFunc("POST /api/agents/preview", s.previewAgentInput)
	mux.HandleFunc("GET /api/agents/{id}/events", s.agentEvents)
	for _, action := range []string{"input", "interrupt", "close", "resume", "queue", "delete"} {
		mux.HandleFunc("POST /api/agents/{id}/"+action, s.agentAction)
	}
	mux.HandleFunc("POST /api/agents/{id}/requests/{requestId}", s.answerAgentRequest)
}
func (s *Server) manager(create bool) (*agent.Manager, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return nil, errors.New("工作台已关闭")
	}
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
	s.syncStop = make(chan struct{})
	s.syncDone = make(chan struct{})
	go s.discussionWorker(manager, s.syncStop, s.syncDone)
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
	case "delete":
		var cleanup struct {
			AbandonUnknown bool `json:"abandonUnknown"`
		}
		if !decodeAgent(w, r, &cleanup) {
			return
		}
		if err = m.DeleteSessionWithOptions(id, cleanup.AbandonUnknown); err != nil {
			fail(w, 409, err.Error())
			return
		}
		respond(w, 200, map[string]bool{"deleted": true})
		return
	case "input":
		var input struct {
			Text     string        `json:"text"`
			Preview  *PreviewInput `json:"preview,omitempty"`
			Checksum string        `json:"checksum,omitempty"`
		}
		if !decodeAgent(w, r, &input) {
			return
		}
		var result agent.SendResult
		var e error
		if input.Preview != nil {
			if input.Preview.SessionID != id {
				fail(w, 409, "预览不属于此会话")
				return
			}
			s.mu.RLock()
			preview, err := s.buildPreviewLocked(*input.Preview, m)
			if err != nil || preview.Checksum != input.Checksum {
				s.mu.RUnlock()
				fail(w, 409, "预览材料已变化，请重新预览；输入已保留")
				return
			}
			if preview.Context != nil {
				result, e = m.SendDiscussion(r.Context(), id, preview.Text, *preview.Context, preview.Baseline)
			} else {
				result, e = m.Send(r.Context(), id, preview.Text)
			}
			s.mu.RUnlock()
		} else {
			result, e = m.Send(r.Context(), id, input.Text)
		}
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
	var input PreviewInput
	if !decodeAgent(w, r, &input) {
		return
	}
	var m *agent.Manager
	var err error
	if input.SessionID != "" {
		m, err = s.manager(false)
		if err != nil || m == nil {
			fail(w, 400, "会话不存在")
			return
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	preview, err := s.buildPreviewLocked(input, m)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 200, preview)
}

func (s *Server) authorizations(w http.ResponseWriter, r *http.Request) {
	m, err := s.manager(r.Method == "POST")
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if m == nil {
		respond(w, 200, agent.CommandAuthorizations{Rules: []agent.CommandAuthorization{}})
		return
	}
	if r.Method == "GET" {
		respond(w, 200, m.Authorizations())
		return
	}
	var input agent.CommandAuthorizations
	if !decodeAgent(w, r, &input) {
		return
	}
	p, err := m.SaveAuthorizations(input.Generation, input.Rules)
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 200, p)
}
