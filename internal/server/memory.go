package server

import (
	"agent-workbench/internal/agent"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func (s *Server) memoryHooks(w http.ResponseWriter, r *http.Request) {
	m, err := s.manager(false)
	if err != nil || m == nil {
		fail(w, 400, "请先连接 Codex 会话")
		return
	}
	state, err := m.MemoryHooks(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 200, state)
}
func (s *Server) trustMemoryHooks(w http.ResponseWriter, r *http.Request) {
	var review agent.MemoryHooksReview
	if !decodeAgent(w, r, &review) {
		return
	}
	m, err := s.manager(false)
	if err != nil || m == nil {
		fail(w, 400, "请先连接 Codex 会话")
		return
	}
	if err = m.TrustMemoryHooks(r.Context(), r.PathValue("id"), review); err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 200, map[string]bool{"trusted": true})
}
func localMemoryURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" {
		return nil, errors.New("请填写本机 HTTP 查看器地址（127.0.0.1 或 localhost），不含路径、凭证或查询参数")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("查看器地址需要有效端口")
	}
	u.Host = "127.0.0.1:" + strconv.Itoa(port)
	return u, nil
}
func (s *Server) memoryHealth(w http.ResponseWriter, r *http.Request) {
	u, err := localMemoryURL(r.URL.Query().Get("url"))
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	u.Path = "/api/health"
	client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("不跟随查看器重定向") }}
	req, _ := http.NewRequestWithContext(r.Context(), "GET", u.String(), nil)
	response, err := client.Do(req)
	if err != nil {
		fail(w, 503, "记忆服务未连接；请启动 claude-mem worker 并检查端口")
		return
	}
	defer response.Body.Close()
	var health struct {
		Status string `json:"status"`
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil || response.StatusCode != 200 || json.Unmarshal(b, &health) != nil || health.Status != "ok" {
		fail(w, 503, "查看器健康检查未通过；请确认这是 claude-mem worker 地址")
		return
	}
	respond(w, 200, map[string]string{"status": "ok", "url": u.Scheme + "://" + u.Host + "/"})
}

// memoryEndpoint 只读取本机 worker 端口，不返回账户或模型凭据。
func (s *Server) memoryEndpoint(w http.ResponseWriter, r *http.Request) {
	uid := os.Getuid()
	if uid < 0 {
		uid = 77
	}
	port := 37700 + uid%100
	if home, err := os.UserHomeDir(); err == nil {
		if root, err := os.OpenRoot(filepath.Join(home, ".claude-mem")); err == nil {
			defer root.Close()
			for _, name := range []string{"worker.pid", "settings.json"} {
				info, err := root.Lstat(name)
				if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
					continue
				}
				b, err := root.ReadFile(name)
				if err != nil {
					continue
				}
				var p map[string]json.RawMessage
				if json.Unmarshal(b, &p) != nil {
					continue
				}
				var number int
				var text string
				if name == "worker.pid" {
					_ = json.Unmarshal(p["port"], &number)
				} else if json.Unmarshal(p["CLAUDE_MEM_WORKER_PORT"], &text) == nil {
					number, _ = strconv.Atoi(text)
				}
				if number > 0 && number <= 65535 {
					port = number
					break
				}
			}
		}
	}
	respond(w, 200, map[string]string{"url": "http://127.0.0.1:" + strconv.Itoa(port) + "/"})
}
