package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestResponseWriterCapturesExplicitStatus verifies that WriteHeader correctly
// captures the status code written by the handler.
func TestResponseWriterCapturesExplicitStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rw, req)

	if rw.status != http.StatusCreated {
		t.Fatalf("expected captured status %d, got %d", http.StatusCreated, rw.status)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected recorder status %d, got %d", http.StatusCreated, rec.Code)
	}
}

// TestResponseWriterImplicit200 verifies that a handler that only calls Write
// (without an explicit WriteHeader) results in a captured status of 200.
func TestResponseWriterImplicit200(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rw, req)

	if rw.status != http.StatusOK {
		t.Fatalf("expected captured status %d, got %d", http.StatusOK, rw.status)
	}
}

// TestLoggingMiddlewareTransparent verifies that loggingMiddleware does not
// alter the handler's response status or body.
func TestLoggingMiddlewareTransparent(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
	})

	wrapped := loggingMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(body) != "accepted" {
		t.Fatalf("expected body %q, got %q", "accepted", string(body))
	}
}
