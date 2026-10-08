package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestMemoryURLAndHealth(t *testing.T) {
	for _, raw := range []string{"https://127.0.0.1:37701", "http://example.com:80", "http://127.0.0.1:12/path", "http://user:pass@localhost:123", "http://localhost:123?token=x", "http://localhost:0"} {
		if _, err := localMemoryURL(raw); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Error("arbitrary path")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","version":"13.34.2"}`))
	}))
	defer worker.Close()
	s, _ := discussionServer(t)
	w := request(t, s, "GET", "/api/memory/health?url="+url.QueryEscape(worker.URL), "", testToken, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	foreign := request(t, s, "GET", "/api/memory/health?url="+url.QueryEscape("http://example.com:80"), "", testToken, "")
	if foreign.Code != 400 {
		t.Fatal("remote probe allowed")
	}
}
