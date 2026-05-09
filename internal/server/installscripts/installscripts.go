// Package installscripts embeds quick-install scripts and exposes them via
// HTTP at /install/<file>. The web admin's "show install command" modal
// hands operators a curl-pipe-bash one-liner that fetches agent.sh from this
// handler and feeds it environment variables (QT_TOKEN, QT_ENDPOINT). The
// scripts are self-contained — no source-of shared library — so they work
// when downloaded standalone.
package installscripts

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// agent.sh is the only script today; agent.ps1 is intentionally absent
// (the existing deploy/windows/install-agent.ps1 references local relative
// paths and isn't safe to iwr|iex without a rewrite). When Windows support
// lands, drop the file in this directory and add it to the embed line.
//
//go:embed agent.sh
var fsys embed.FS

// Handler returns an http.Handler serving /install/<file>. Requests that
// don't map to an embedded file return 404 (handled by http.FileServer).
func Handler() http.Handler {
	sub, err := fs.Sub(fsys, ".")
	if err != nil {
		// Should never happen — embed FS root always exists.
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.StripPrefix("/install/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Force shell-friendly content type for *.sh — Go's mime detection
		// from extension hits text/plain on most platforms, which is fine,
		// but explicit is better here so curl|bash never gets surprises.
		if strings.HasSuffix(r.URL.Path, ".sh") {
			w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		}
		fileServer.ServeHTTP(w, r)
	}))
}
