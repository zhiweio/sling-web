//go:build dev

// Package web: dev build reverse-proxies to the Vite dev server so the Go
// server serves one origin (no CORS) while HMR stays live. Run with
// `go run -tags dev ./cmd/sling-server` plus `npm run dev` in web/.
package web

import (
	"embed"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/flarco/g"
)

// FS is a stub in dev mode; assets come from Vite.
//
//go:embed all:dist
var FS embed.FS

// Handler proxies non-API requests to the Vite dev server on :5173.
func Handler() http.Handler {
	target, _ := url.Parse("http://127.0.0.1:5173")
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		g.Debug("vite dev server unreachable (is `npm run dev` running in web/?)")
		http.Error(w, "vite dev server unreachable on :5173 - run `npm run dev` inside web/", http.StatusBadGateway)
	}
	return proxy
}
