// Package server 提供本地项目入口和文档读写接口。
package server

import (
	"agent-workbench/internal/agent"
	"agent-workbench/internal/project"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
)

//go:embed web/*
var assets embed.FS

// Server 在一个进程内只绑定一个项目。
type Server struct {
	mu          sync.RWMutex
	project     *project.Project
	token       string
	handler     http.Handler
	agents      *agent.Manager
	codexBinary string
}

// New 构建本地接口；项目句柄的所有权交给 Server。
func New(token string, p *project.Project) *Server {
	return NewWithCodex(token, p, "codex")
}

// NewWithCodex 仅从本地启动配置接收 CLI 路径，网页不能指定可执行文件。
func NewWithCodex(token string, p *project.Project, binary string) *Server {
	s := &Server{project: p, token: token, codexBinary: binary}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/project", s.getProject)
	mux.HandleFunc("POST /api/project", s.bindProject)
	mux.HandleFunc("GET /api/document", s.getDocument)
	mux.HandleFunc("POST /api/document", s.saveDocument)
	mux.HandleFunc("GET /api/diagram", s.getDiagram)
	mux.HandleFunc("GET /api/collaboration", s.getCollaboration)
	mux.HandleFunc("POST /api/collaboration", s.saveCollaboration)
	s.registerAgents(mux)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "接口不存在") })
	web, _ := fs.Sub(assets, "web")
	files := http.FileServer(http.FS(web))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/" {
			f, err := web.Open(strings.TrimPrefix(r.URL.Path, "/"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			st, err := f.Stat()
			f.Close()
			if err != nil || st.IsDir() {
				http.NotFound(w, r)
				return
			}
		}
		files.ServeHTTP(w, r)
	})
	s.handler = mux
	return s
}

// ServeHTTP 验证本地 API 请求并设置页面约束。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src data: blob:; connect-src 'self'; frame-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		if s.token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Workbench-Token")), []byte(s.token)) != 1 {
			fail(w, 403, "访问凭证无效，请使用终端显示的完整地址打开页面")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			fail(w, 403, "拒绝外部网页请求")
			return
		}
	}
	s.handler.ServeHTTP(w, r)
}

// Close 等待读请求完成后释放项目句柄。
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agents != nil {
		s.agents.Close()
		s.agents = nil
	}
	if s.project != nil {
		err := s.project.Close()
		s.project = nil
		return err
	}
	return nil
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.project == nil {
		respond(w, 200, nil)
		return
	}
	info, err := s.project.Snapshot()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	respond(w, 200, info)
}
func (s *Server) bindProject(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action string `json:"action"`
		Path   string `json:"path"`
		Name   string `json:"name"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fail(w, 400, "请求格式错误或超过 8 KiB")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		fail(w, 400, "请求只允许一个 JSON 对象")
		return
	}
	if input.Action != "new" && input.Action != "open" {
		fail(w, 400, "操作必须是 new 或 open")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.project != nil {
		fail(w, 409, "此工作台已绑定项目；打开另一项目请启动新的工作台进程")
		return
	}
	var p *project.Project
	var err error
	status := 200
	if input.Action == "new" {
		p, err = project.Create(input.Path, input.Name)
		status = 201
	} else {
		p, err = project.Open(input.Path)
	}
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	info, err := p.Snapshot()
	if err != nil {
		p.Close()
		fail(w, 500, err.Error())
		return
	}
	s.project = p
	respond(w, status, info)
}
func (s *Server) getDocument(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.project == nil {
		fail(w, 409, "请先新建或打开项目")
		return
	}
	path := r.URL.Query().Get("path")
	body, err := s.project.DocumentState(path)
	if err != nil {
		status := 400
		if errors.Is(err, fs.ErrNotExist) {
			status = 404
		}
		fail(w, status, err.Error())
		return
	}
	respond(w, 200, body)
}

func (s *Server) getDiagram(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.project == nil {
		fail(w, 409, "请先打开项目")
		return
	}
	path := r.URL.Query().Get("path")
	body, err := s.project.ReadDiagram(path)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 200, map[string]string{"path": path, "html": body})
}

func (s *Server) saveDocument(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Path     string `json:"path"`
		Markdown string `json:"markdown"`
		Revision string `json:"revision"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fail(w, 400, "保存请求格式错误或正文过大")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(w, 400, "请求只允许一个 JSON 对象")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.project == nil {
		fail(w, 409, "请先打开项目")
		return
	}
	saved, err := s.project.SaveDocument(input.Path, input.Markdown, input.Revision)
	if err != nil {
		status := 400
		if errors.Is(err, project.ErrConflict) {
			status = 409
		}
		if errors.Is(err, project.ErrReadOnly) {
			status = 403
		}
		fail(w, status, err.Error())
		return
	}
	respond(w, 200, saved)
}

func (s *Server) getCollaboration(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.project == nil {
		fail(w, 409, "请先打开项目")
		return
	}
	var state project.CollaborationState
	var err error
	if r.URL.Query().Get("export") == "1" {
		state, err = s.project.ExportCollaboration()
	} else {
		state, err = s.project.Collaboration()
	}
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 200, state)
}
func (s *Server) saveCollaboration(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Generation int              `json:"generation"`
		Threads    []project.Thread `json:"threads"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Threads == nil {
		fail(w, 400, "讨论请求格式错误或超过 16 MiB")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(w, 400, "请求只允许一个 JSON 对象")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.project == nil {
		fail(w, 409, "请先打开项目")
		return
	}
	saved, err := s.project.SaveCollaboration(input.Generation, input.Threads)
	if err != nil {
		status := 400
		if errors.Is(err, project.ErrCollaborationConflict) {
			status = 409
		}
		fail(w, status, err.Error())
		return
	}
	respond(w, 200, saved)
}
