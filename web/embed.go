//go:build !dev

// Package web serves the embedded React workbench in production builds.
// web/dist is produced by `npm run build`; dist/.placeholder keeps the
// tree compilable without a frontend toolchain.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var FS embed.FS

// Handler returns the SPA file server with client-side routing fallback.
func Handler() http.Handler {
	sub, err := fs.Sub(FS, "dist")
	if err != nil {
		panic(err) // static, fail loudly at startup
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		switch {
		case p == "" || p == "index.html":
			w.Header().Set("Cache-Control", "no-cache")
		default:
			if _, err := fs.Stat(sub, p); err != nil {
				// unknown route -> index.html so client-side routing works.
				// Must stay no-cache: marking the SPA entry immutable would
				// pin users to the JS bundle shipped with an old binary.
				r.URL.Path = "/"
				w.Header().Set("Cache-Control", "no-cache")
				fileServer.ServeHTTP(w, r)
				return
			}
			// content-hashed assets can be cached forever
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}
