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

// agent.sh covers Linux/macOS; agent.ps1 is the iwr|iex-safe Windows
// counterpart. Both are self-contained — no shared lib sourcing — so they
// work when fetched standalone via the web admin's install-command modal.
//
//go:embed agent.sh agent.ps1
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
