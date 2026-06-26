package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhubert/crowlink/internal/server"
	"github.com/zhubert/crowlink/internal/store"
)

func TestHealthz(t *testing.T) {
	handler := server.New(store.NewMemStore())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	if string(body) != "ok" {
		t.Fatalf("expected body %q, got %q", "ok", string(body))
	}
}
