package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBoundary(t *testing.T) {
	var mode string
	g := gateway{proxy: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) }), key: func() (string, error) { return "test-key", nil }, apply: func(_ context.Context, m string) error { mode = m; return nil }}
	for _, c := range []struct {
		method, path, body, token string
		code                      int
	}{
		{"GET", "/configs", "", "", 401}, {"GET", "/configs", "", "test-key", 202},
		{"PATCH", "/configs", `{"mode":"direct"}`, "test-key", 204},
		{"PATCH", "/configs", `{"mode":"tun"}`, "test-key", 400},
		{"PATCH", "/configs", `{"mode":"global","ipv6":true}`, "test-key", 409},
		{"PUT", "/configs", `{"path":"/tmp/unreviewed"}`, "test-key", 409},
		{"POST", "/upgrade", "", "test-key", 409}, {"PUT", "/proxies/custom", `{"name":"node"}`, "test-key", 202},
		{"POST", "/ui/../upgrade", "", "test-key", 409},
		{"GET", "/ui/index.html", "", "", 202},
		{"PATCH", "/configs", `{"log-level":"warning"}`, "test-key", 202},
	} {
		r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		if c.token != "" {
			r.Header.Set("Authorization", "Bearer "+c.token)
		}
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		if w.Code != c.code {
			t.Errorf("%s %s: %d", c.method, c.path, w.Code)
		}
	}
	if mode != "direct" {
		t.Fatal("mode did not reach coordinator")
	}
	r := httptest.NewRequest("GET", "/configs", nil)
	r.Header.Set("Authorization", "Bearer test-key")
	r.Header.Set("Origin", "https://untrusted.invalid")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin request accepted")
	}
}
