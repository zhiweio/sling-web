// ui.go holds the injection point for the embedded web UI. cmd/sling-server
// imports the web package and assigns web.Handler so the server package
// stays free of frontend build coupling.
package server

import "net/http"

// UIHandler returns the embedded SPA handler, or nil in tests.
// Replaced by cmd/sling-server at startup.
var UIHandler = func() http.Handler { return nil }
