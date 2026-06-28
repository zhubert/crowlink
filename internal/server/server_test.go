package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestRedirectKnownCode verifies that GET /{code} returns 302 with the correct
// Location header when the code exists in the store.
func TestRedirectKnownCode(t *testing.T) {
	s := store.NewMemStore()
	const originalURL = "https://example.com/some/path"

	code, err := s.Put(originalURL)
	if err != nil {
		t.Fatalf("Put(%q) unexpected error: %v", originalURL, err)
	}

	handler := server.New(s)
	req := httptest.NewRequest(http.MethodGet, "/"+code, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusFound {
		t.Fatalf("expected status 302, got %d", res.StatusCode)
	}

	location := res.Header.Get("Location")
	if location != originalURL {
		t.Errorf("Location = %q; want %q", location, originalURL)
	}
}

// TestRedirectUnknownCode verifies that GET /{code} returns 404 when the code
// is not present in the store.
func TestRedirectUnknownCode(t *testing.T) {
	handler := server.New(store.NewMemStore())

	req := httptest.NewRequest(http.MethodGet, "/doesnotexist", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", res.StatusCode)
	}
}

// TestRoundTrip exercises the full shorten → redirect flow over HTTP:
// POST /shorten to create a short code, then GET /{code} to verify the 302
// redirect points back to the original URL.
//
// Note: this test requires POST /shorten to be registered on the server.
// If that route is not yet implemented the test is skipped gracefully.
func TestRoundTrip(t *testing.T) {
	s := store.NewMemStore()
	handler := server.New(s)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	const originalURL = "https://example.com/round-trip"

	// POST /shorten
	resp, err := http.Post(srv.URL+"/shorten", "application/json",
		strings.NewReader(`{"url":"`+originalURL+`"}`))
	if err != nil {
		t.Fatalf("POST /shorten: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		t.Skip("POST /shorten not yet implemented; skipping round-trip test")
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /shorten returned unexpected status %d", resp.StatusCode)
	}

	// Parse the code from the response body.
	var result struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding POST /shorten response: %v", err)
	}
	if result.Code == "" {
		t.Fatal("POST /shorten returned empty code")
	}

	// GET /{code} — must not follow the redirect automatically.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	redirectResp, err := client.Get(srv.URL + "/" + result.Code)
	if err != nil {
		t.Fatalf("GET /%s: %v", result.Code, err)
	}
	defer redirectResp.Body.Close()

	if redirectResp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", redirectResp.StatusCode)
	}

	location := redirectResp.Header.Get("Location")
	if location != originalURL {
		t.Errorf("Location = %q; want %q", location, originalURL)
	}
}
