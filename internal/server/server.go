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

	return mux
}
