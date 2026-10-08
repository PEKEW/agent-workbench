package server

import (
	"agent-workbench/internal/project"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testToken = "test-session-token"

func request(t *testing.T, s http.Handler, method, path, body, token, origin string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://127.0.0.1:9876"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Workbench-Token", token)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestAPIAuthorization(t *testing.T) {
	s := New(testToken, nil)
	defer s.Close()
	for _, tc := range []struct {
		token, origin string
		want          int
	}{{"", "", 403}, {"wrong", "", 403}, {testToken, "https://elsewhere.example", 403}, {testToken, "null", 403}, {testToken, "http://127.0.0.1:9876", 200}} {
		w := request(t, s, "GET", "/api/project", "", tc.token, tc.origin)
		if w.Code != tc.want {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
	}
}
func TestCreateBindReadAndConflict(t *testing.T) {
	s := New(testToken, nil)
	defer s.Close()
	root := filepath.Join(t.TempDir(), "new project")
	payload, _ := json.Marshal(map[string]string{"action": "new", "path": root, "name": "项目"})
	w := request(t, s, "POST", "/api/project", string(payload), testToken, "")
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = request(t, s, "GET", "/api/document?path=PLAN.md", "", testToken, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "当前") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	second := filepath.Join(t.TempDir(), "must-not-create")
	payload, _ = json.Marshal(map[string]string{"action": "new", "path": second, "name": "x"})
	w = request(t, s, "POST", "/api/project", string(payload), testToken, "")
	if w.Code != 409 {
		t.Fatalf("%d", w.Code)
	}
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Fatal("第二个目录被创建")
	}
	w = request(t, s, "GET", "/api/document?path=../README.md", "", testToken, "")
	if w.Code == 200 {
		t.Fatal("越界读取成功")
	}
}
func TestBadRequestsAndMissingProject(t *testing.T) {
	s := New(testToken, nil)
	defer s.Close()
	w := request(t, s, "GET", "/api/document?path=PLAN.md", "", testToken, "")
	if w.Code != 409 {
		t.Fatalf("%d", w.Code)
	}
	for _, body := range []string{"{bad", `{"action":"delete","path":"/"}`, `{"action":"open","path":""}`, `{"action":"open","unknown":true}`, `{} {}`, strings.Repeat("x", 10000)} {
		w = request(t, s, "POST", "/api/project", body, testToken, "")
		if w.Code != 400 {
			t.Fatalf("%q: %d", body[:min(len(body), 40)], w.Code)
		}
	}
}
func TestOpenOverHTTPDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	before := []byte("# 原文\n未提交修改")
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), before, 0644); err != nil {
		t.Fatal(err)
	}
	s := New(testToken, nil)
	defer s.Close()
	b, _ := json.Marshal(map[string]string{"action": "open", "path": root})
	w := request(t, s, "POST", "/api/project", string(b), testToken, "")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = request(t, s, "GET", "/api/project", "", testToken, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "缺少") {
		t.Fatal(w.Body.String())
	}
	after, _ := os.ReadFile(filepath.Join(root, "PLAN.md"))
	entries, _ := os.ReadDir(root)
	if !bytes.Equal(before, after) || len(entries) != 1 {
		t.Fatal("打开改变了项目")
	}
}
func TestConcurrentBinding(t *testing.T) {
	s := New(testToken, nil)
	defer s.Close()
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]string{"action": "open", "path": root})
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			results <- request(t, s, "POST", "/api/project", body, testToken, "").Code
		}(string(b))
	}
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for code := range results {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("%v", counts)
	}
}
func TestEmbeddedPageAndInitialProject(t *testing.T) {
	root := filepath.Join(t.TempDir(), "p")
	p, err := project.Create(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	s := New(testToken, p)
	defer s.Close()
	w := request(t, s, "GET", "/", "", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Agent Workbench") {
		t.Fatalf("%d", w.Code)
	}
	w = request(t, s, "GET", "/api/project", "", testToken, "")
	var info project.Info
	if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Name != "test" {
		t.Fatalf("%+v", info)
	}
	w = request(t, s, "GET", "/missing", "", "", "")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestRealHTTP(t *testing.T) {
	s := New(testToken, nil)
	defer s.Close()
	ts := httptest.NewServer(s)
	defer ts.Close()
	r, _ := http.NewRequest("GET", ts.URL+"/api/project", nil)
	r.Header.Set("X-Workbench-Token", testToken)
	res, err := ts.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || strings.TrimSpace(string(body)) != "null" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
}

func TestDiagramEndpointRequiresToken(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs/diagrams"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs/diagrams/plan.html"), []byte("<title>计划图</title>"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	s := New(testToken, p)
	defer s.Close()
	w := request(t, s, "GET", "/api/diagram?path=docs/diagrams/plan.html", "", testToken, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "计划图") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = request(t, s, "GET", "/api/diagram?path=docs/diagrams/plan.html", "", "", "")
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestSaveOverHTTPAndRejectStaleRevision(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("before"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	s := New(testToken, p)
	defer s.Close()
	w := request(t, s, "GET", "/api/document?path=PLAN.md", "", testToken, "")
	var doc project.DocumentContent
	if err = json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Revision == "" || !doc.Editable {
		t.Fatal(doc)
	}
	payload, _ := json.Marshal(map[string]string{"path": "PLAN.md", "markdown": "after", "revision": doc.Revision})
	w = request(t, s, "POST", "/api/document", string(payload), testToken, "")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = request(t, s, "POST", "/api/document", string(payload), testToken, "")
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	w = request(t, s, "POST", "/api/document", string(payload), "", "")
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	body, _ := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if string(body) != "after" {
		t.Fatal(string(body))
	}
}

func TestCollaborationHTTPPersistenceAuthorizationAndConflict(t *testing.T) {
	root := filepath.Join(t.TempDir(), "discussion")
	p, err := project.Create(root, "协作")
	if err != nil {
		t.Fatal(err)
	}
	s := New(testToken, p)
	defer s.Close()
	if w := request(t, s, "GET", "/api/collaboration", "", "", ""); w.Code != 403 {
		t.Fatal("unauthenticated collaboration")
	}
	w := request(t, s, "GET", "/api/collaboration", "", testToken, "")
	var state project.CollaborationState
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil {
		t.Fatal(w.Body.String())
	}
	if len(state.Documents) == 0 || state.Documents[0].Source == "" {
		t.Fatal("collaboration omitted document metadata")
	}
	payload := `{"generation":1,"threads":[{"id":"t","documentId":"PLAN.md","kind":"comment","status":"open","dueDate":"","anchor":{"quote":"当前","before":"","after":"","revision":1},"messages":[{"id":"m","author":"我","role":"human","intent":"comment","body":"第一条意见","createdAt":"2026-10-05T10:00:00Z"}]}]}`
	if w = request(t, s, "POST", "/api/collaboration", payload, testToken, "https://example.com"); w.Code != 403 {
		t.Fatal("external write accepted")
	}
	if _, err = os.Stat(filepath.Join(root, ".agent-state")); !os.IsNotExist(err) {
		t.Fatal("read wrote state")
	}
	if w = request(t, s, "POST", "/api/collaboration", payload, testToken, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = request(t, s, "POST", "/api/collaboration", payload, testToken, ""); w.Code != 409 {
		t.Fatal("stale write accepted")
	}
	if w = request(t, s, "GET", "/api/collaboration", "", testToken, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "第一条意见") {
		t.Fatal("saved discussion missing")
	}
	if w = request(t, s, "POST", "/api/collaboration", `{"generation":2,"threads":[]} {}`, testToken, ""); w.Code != 400 {
		t.Fatal("trailing data accepted")
	}
}
