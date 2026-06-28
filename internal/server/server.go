// Package server provides the HTTP handler for the crowlink URL shortener.
package server

import (
	"fmt"
	"net/http"

	"github.com/zhubert/crowlink/internal/store"
)

// New builds and returns an http.Handler configured with all routes.
// The provided store.Store is available to route handlers for persistence.
func New(s store.Store) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	// GET /{code} – look up short code and issue a 302 redirect.
	// This catch-all pattern is registered last so it does not shadow
	// the more-specific /healthz and /shorten routes.
	mux.HandleFunc("GET /{code}", func(w http.ResponseWriter, r *http.Request) {
		code := r.PathValue("code")
		url, ok := s.Get(code)
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, url, http.StatusFound)
	})

	return mux
}
