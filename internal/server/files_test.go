package server

import (
	"agent-workbench/internal/project"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileEndpoints(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"PLAN.md": "# 计划", "cmd/main.go": "package main", ".env": "SECRET=1"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	s := New(testToken, p)
	defer s.Close()
	s.SetExternalEditor("true")

	w := request(t, s, "GET", "/api/project", "", testToken, "")
	if !strings.Contains(w.Body.String(), `"cmd/main.go"`) || strings.Contains(w.Body.String(), ".env") {
		t.Fatal(w.Body.String())
	}
	if w = request(t, s, "GET", "/api/file?path=cmd/main.go", "", testToken, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "package main") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w = request(t, s, "GET", "/api/file?path=.env", "", testToken, ""); w.Code != 400 {
		t.Fatalf("隐藏文件可读：%d", w.Code)
	}
	if w = request(t, s, "GET", "/api/file?path=cmd/main.go", "", "", ""); w.Code != 403 {
		t.Fatalf("无凭证可读：%d", w.Code)
	}
	if w = request(t, s, "POST", "/api/file/open", `{"path":"cmd/main.go"}`, testToken, ""); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w = request(t, s, "POST", "/api/file/open", `{"path":".env"}`, testToken, ""); w.Code != 400 {
		t.Fatalf("隐藏文件可外部打开：%d", w.Code)
	}

	w = request(t, s, "POST", "/api/document/create", `{"path":"docs/new.md"}`, testToken, "")
	var created project.DocumentContent
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &created) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w = request(t, s, "POST", "/api/document/create", `{"path":"docs/new.md"}`, testToken, ""); w.Code != 409 {
		t.Fatalf("重复新建：%d", w.Code)
	}
	body, _ := json.Marshal(map[string]string{"path": "docs/new.md", "target": "docs/moved.md", "revision": "stale"})
	if w = request(t, s, "POST", "/api/document/rename", string(body), testToken, ""); w.Code != 409 {
		t.Fatalf("旧版本重命名：%d", w.Code)
	}
	body, _ = json.Marshal(map[string]string{"path": "docs/new.md", "target": "docs/moved.md", "revision": created.Revision})
	if w = request(t, s, "POST", "/api/document/rename", string(body), testToken, ""); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body, _ = json.Marshal(map[string]string{"path": "docs/moved.md", "revision": created.Revision})
	if w = request(t, s, "POST", "/api/document/delete", string(body), testToken, ""); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if _, err = os.Stat(filepath.Join(root, "docs/moved.md")); !os.IsNotExist(err) {
		t.Fatal("文件未删除")
	}
}
