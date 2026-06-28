// Package server provides the HTTP handler for the crowlink URL shortener.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/zhubert/crowlink/internal/store"
	"github.com/zhubert/crowlink/internal/validate"
)

// baseURL is the base URL used to construct short URLs. It will be made
// configurable in a later issue (#9).
const baseURL = "http://localhost:8080"

// New builds and returns an http.Handler configured with all routes.
// The provided store.Store is available to route handlers for persistence.
func New(s store.Store) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	mux.HandleFunc("POST /shorten", func(w http.ResponseWriter, r *http.Request) {
		// 1. Validate Content-Type header.
		ct := r.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}

		// 2. Decode JSON body.
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "malformed JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		// 3. Validate the URL.
		if err := validate.URL(req.URL); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// 4. Store the URL.
		code, err := s.Put(req.URL)
		if err != nil {
			http.Error(w, "failed to store URL: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// 5. Respond 201 with JSON body.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{
			"code":      code,
			"short_url": baseURL + "/" + code,
		})
	})

	return mux
}
