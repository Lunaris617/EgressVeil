// SPDX-License-Identifier: GPL-3.0-only
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type gateway struct {
	proxy http.Handler
	key   func() (string, error)
	apply func(context.Context, string) error
}

func (g gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	routePath := path.Clean(r.URL.Path)
	// Static dashboard files contain no credentials. API calls always authenticate.
	if r.Method == "GET" && routePath == "/ui" {
		http.Redirect(w, r, "/ui/metacubexd/", http.StatusTemporaryRedirect)
		return
	}
	if r.Method == "GET" && strings.HasPrefix(routePath, "/ui/") {
		g.proxy.ServeHTTP(w, r)
		return
	}
	key, err := g.key()
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	// Mihomo's WebSocket clients use the token query parameter.
	if token == "" && strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		token = r.URL.Query().Get("token")
	}
	if err != nil || key == "" || subtle.ConstantTimeCompare([]byte(token), []byte(key)) != 1 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	// Only the bundled same-origin dashboard is supported; do not grant wildcard CORS.
	if origin := r.Header.Get("Origin"); origin != "" {
		u, e := url.Parse(origin)
		if e != nil || u.Host != r.Host {
			http.Error(w, "Origin rejected", http.StatusForbidden)
			return
		}
	}
	if routePath == "/configs" && r.Method == "PATCH" {
		var patch map[string]json.RawMessage
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
		if dec.Decode(&patch) != nil {
			http.Error(w, "Invalid configuration", 400)
			return
		}
		if len(patch) == 1 && patch["log-level"] != nil {
			var level string
			if json.Unmarshal(patch["log-level"], &level) != nil || !strings.Contains("|silent|error|warning|info|debug|", "|"+level+"|") {
				http.Error(w, "Invalid log level", 400)
				return
			}
			b, _ := json.Marshal(patch)
			r.Body = io.NopCloser(strings.NewReader(string(b)))
			r.ContentLength = int64(len(b))
			g.proxy.ServeHTTP(w, r)
			return
		}
		if len(patch) != 1 || patch["mode"] == nil {
			http.Error(w, "Use OpenClash settings to change configuration", 409)
			return
		}
		var mode string
		if json.Unmarshal(patch["mode"], &mode) != nil || (mode != "direct" && mode != "rule" && mode != "global") {
			http.Error(w, "Invalid mode", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
		defer cancel()
		if g.apply(ctx, mode) != nil {
			http.Error(w, "Configuration not applied; inspect LuCI status", 503)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if routePath == "/configs" && r.Method == "PUT" {
		var body struct {
			Path string `json:"path"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&body) != nil {
			http.Error(w, "Invalid reload", 400)
			return
		}
		// Only the selected config's generated runtime file may be reloaded. The
		// coordinator regenerates and validates it rather than trusting API payloads.
		if body.Path != "" {
			out, e := exec.Command("uci", "-q", "get", "openclash.config.config_path").Output()
			expected := "/etc/openclash/" + filepath.Base(strings.TrimSpace(string(out)))
			if e != nil || body.Path != expected {
				http.Error(w, "Use configuration management to select files", 409)
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
		defer cancel()
		if g.apply(ctx, "reload") != nil {
			http.Error(w, "Reload failed; inspect LuCI status", 503)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// All other config writes are deliberately excluded from the public API.
	if routePath == "/configs" && r.Method != "GET" {
		http.Error(w, "Reload through OpenClash configuration management", 409)
		return
	}
	// Core upgrade, restart and remote UI replacement are package-owned operations.
	if r.Method != "GET" && (strings.HasPrefix(routePath, "/upgrade") || routePath == "/restart") {
		http.Error(w, "Upgrade EgressVeil as a package", 409)
		return
	}
	g.proxy.ServeHTTP(w, r)
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: controller LAN_ADDRESS:9090")
	}
	upstream, _ := url.Parse("http://127.0.0.1:9091")
	p := httputil.NewSingleHostReverseProxy(upstream)
	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) { http.Error(w, "Core unavailable", 503) }
	p.ModifyResponse = func(r *http.Response) error { r.Header.Del("Access-Control-Allow-Origin"); return nil }
	g := gateway{proxy: p, key: func() (string, error) {
		b, e := os.ReadFile("/etc/egressveil/controller.key")
		return strings.TrimSpace(string(b)), e
	}, apply: func(ctx context.Context, mode string) error {
		args := []string{"mode", mode}
		if mode == "reload" {
			args = []string{"reload"}
		}
		c := exec.CommandContext(ctx, "/usr/libexec/egressveil/setup", args...)
		c.Stdout = io.Discard
		c.Stderr = io.Discard
		return c.Run()
	}}
	s := &http.Server{Addr: os.Args[1], Handler: g, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 65536}
	if e := s.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
		log.Fatal("controller listener failed")
	}
}
