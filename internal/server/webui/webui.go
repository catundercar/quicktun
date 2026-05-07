// Package webui exposes the embedded SPA originally produced by web/dist.
//
// The Vite build emits assets to web/dist; the Makefile syncs them into
// internal/server/webui/dist (this directory) before invoking `go build`,
// because Go's //go:embed directive cannot reach across the module tree
// (no parent traversal, no symlinks).
//
// The embed target always exists — internal/server/webui/dist/.gitkeep is
// committed — so `go build` succeeds even when the developer hasn't run
// `npm run build`. In that case IsAvailable returns false and Handler
// serves a friendly 404 placeholder pointing at the build instructions.
package webui

import (
	"embed"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// IsAvailable reports whether a built SPA is embedded. Returns false when
// dist/index.html is missing (i.e. only the .gitkeep sentinel is present).
func IsAvailable() bool {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return false
	}
	f, err := sub.Open("index.html")
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// Handler returns an http.Handler that serves the SPA. Static assets get
// served verbatim from dist; any path that doesn't match a file falls back
// to index.html so client-side routing (react-router) can handle it.
//
// If no SPA is embedded, the handler returns 404 with a placeholder JSON.
func Handler() http.Handler {
	if !IsAvailable() {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"web UI not built. Run 'make build' or 'cd web && npm run build'"}`)
		})
	}

	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		// Defensive: IsAvailable already validated this path.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		})
	}

	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip leading slash for fs.Open; empty path → index.html.
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		f, err := sub.Open(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// Client-side route — serve index.html with the original URL
				// rewritten to "/" so http.FileServer returns the SPA shell.
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = f.Close()
		fileServer.ServeHTTP(w, r)
	})
}
