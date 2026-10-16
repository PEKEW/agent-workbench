package server

import (
	"agent-workbench/internal/project"
	"errors"
	"io/fs"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
)

func (s *Server) registerFiles(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/document/create", s.createDocument)
	mux.HandleFunc("POST /api/document/rename", s.renameDocument)
	mux.HandleFunc("POST /api/document/delete", s.deleteDocument)
	mux.HandleFunc("GET /api/file", s.getFile)
	mux.HandleFunc("POST /api/file/open", s.openExternal)
}

// SetExternalEditor 只接受本地启动参数；网页只能选择项目文件，不能指定可执行程序。
func (s *Server) SetExternalEditor(command string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.editor = strings.Fields(command)
}

func fileStatus(err error) int {
	switch {
	case errors.Is(err, project.ErrConflict), errors.Is(err, project.ErrExists):
		return 409
	case errors.Is(err, project.ErrReadOnly):
		return 403
	case errors.Is(err, fs.ErrNotExist):
		return 404
	}
	return 400
}

func (s *Server) createDocument(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Path string `json:"path"`
	}
	if !decodeAgent(w, r, &input) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.project == nil {
		fail(w, 409, "请先打开项目")
		return
	}
	created, err := s.project.CreateDocument(input.Path)
	if err != nil {
		fail(w, fileStatus(err), err.Error())
		return
	}
	respond(w, 201, created)
}

func (s *Server) renameDocument(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Path     string `json:"path"`
		Target   string `json:"target"`
		Revision string `json:"revision"`
	}
	if !decodeAgent(w, r, &input) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.project == nil {
		fail(w, 409, "请先打开项目")
		return
	}
	if err := s.project.RenameDocument(input.Path, input.Target, input.Revision); err != nil {
		fail(w, fileStatus(err), err.Error())
		return
	}
	respond(w, 200, map[string]string{"path": input.Target})
}

func (s *Server) deleteDocument(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Path     string `json:"path"`
		Revision string `json:"revision"`
	}
	if !decodeAgent(w, r, &input) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.project == nil {
		fail(w, 409, "请先打开项目")
		return
	}
	if err := s.project.DeleteDocument(input.Path, input.Revision); err != nil {
		fail(w, fileStatus(err), err.Error())
		return
	}
	respond(w, 200, map[string]string{"path": input.Path})
}

func (s *Server) getFile(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.project == nil {
		fail(w, 409, "请先打开项目")
		return
	}
	content, err := s.project.ReadTextFile(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, fileStatus(err), err.Error())
		return
	}
	respond(w, 200, content)
}

// externalCommand 未配置编辑器时，macOS 用 open -t 交给默认文本编辑器，避免按文件类型执行脚本或应用。
func externalCommand(editor []string, target string) (*exec.Cmd, error) {
	if len(editor) > 0 {
		return exec.Command(editor[0], append(append([]string{}, editor[1:]...), target)...), nil
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-t", target), nil
	}
	return nil, errors.New("未配置外部编辑器；请用 --editor 启动工作台，例如 --editor code")
}

func (s *Server) openExternal(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Path string `json:"path"`
	}
	if !decodeAgent(w, r, &input) {
		return
	}
	s.mu.RLock()
	if s.project == nil {
		s.mu.RUnlock()
		fail(w, 409, "请先打开项目")
		return
	}
	target, err := s.project.ExternalPath(input.Path)
	editor := s.editor
	s.mu.RUnlock()
	if err != nil {
		fail(w, fileStatus(err), err.Error())
		return
	}
	cmd, err := externalCommand(editor, target)
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		fail(w, 400, "无法启动外部编辑器："+err.Error())
		return
	}
	go func() { _ = cmd.Wait() }()
	respond(w, 200, map[string]string{"path": input.Path})
}
