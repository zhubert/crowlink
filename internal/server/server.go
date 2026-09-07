// Package server provides the HTTP handler for the crowlink URL shortener.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/zhubert/crowlink/internal/store"
	"github.com/zhubert/crowlink/internal/validate"
)

// New builds and returns an http.Handler configured with all routes.
// The provided store.Store is available to route handlers for persistence,
// and baseURL is used to construct the short_url field in responses (see
// internal/config).
func New(s store.Store, baseURL string) http.Handler {
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
			URL   string `json:"url"`
			Alias string `json:"alias"`
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

		// 4. Store the URL, under the requested alias if one was given.
		var code string
		if req.Alias != "" {
			if err := validate.Alias(req.Alias); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := s.PutAlias(req.URL, req.Alias); err != nil {
				if errors.Is(err, store.ErrAliasTaken) {
					http.Error(w, "alias is already taken", http.StatusConflict)
					return
				}
				http.Error(w, "failed to store URL: "+err.Error(), http.StatusInternalServerError)
				return
			}
			code = req.Alias
		} else {
			var err error
			code, err = s.Put(req.URL)
			if err != nil {
				http.Error(w, "failed to store URL: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// 5. Respond 201 with JSON body.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{
			"code":      code,
			"short_url": baseURL + "/" + code,
		})
	})

	// GET /{code}/stats – report click analytics for a short code as JSON.
	mux.HandleFunc("GET /{code}/stats", func(w http.ResponseWriter, r *http.Request) {
		code := r.PathValue("code")
		rec, ok := s.Stats(code)
		if !ok {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rec)
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

		// Record the click. A failure here must not cost the visitor their
		// redirect, so log it and carry on.
		if err := s.IncrementClicks(code); err != nil {
			slog.Error("incrementing clicks", "code", code, "error", err)
		}

		http.Redirect(w, r, url, http.StatusFound)
	})

	return loggingMiddleware(mux)
}
