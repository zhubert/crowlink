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

func TestPostShorten(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        string
		contentType string
		wantStatus  int
		checkBody   func(t *testing.T, body []byte)
	}{
		{
			name:        "success 201",
			body:        `{"url":"https://example.com/some/path"}`,
			contentType: "application/json",
			wantStatus:  http.StatusCreated,
			checkBody: func(t *testing.T, body []byte) {
				var resp map[string]string
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Fatalf("response is not valid JSON: %v", err)
				}
				if resp["code"] == "" {
					t.Error("expected non-empty 'code' field in response")
				}
				const wantPrefix = "http://localhost:8080/"
				if !strings.HasPrefix(resp["short_url"], wantPrefix) {
					t.Errorf("short_url %q does not start with %q", resp["short_url"], wantPrefix)
				}
				if !strings.HasSuffix(resp["short_url"], resp["code"]) {
					t.Errorf("short_url %q does not end with code %q", resp["short_url"], resp["code"])
				}
			},
		},
		{
			name:        "invalid URL returns 400",
			body:        `{"url":"not-a-url"}`,
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
			checkBody:   nil,
		},
		{
			name:        "wrong content-type returns 415",
			body:        `{"url":"https://example.com"}`,
			contentType: "text/plain",
			wantStatus:  http.StatusUnsupportedMediaType,
			checkBody:   nil,
		},
		{
			name:        "missing content-type returns 415",
			body:        `{"url":"https://example.com"}`,
			contentType: "",
			wantStatus:  http.StatusUnsupportedMediaType,
			checkBody:   nil,
		},
		{
			name:        "malformed JSON returns 400",
			body:        `{not valid json`,
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
			checkBody:   nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := server.New(store.NewMemStore())

			req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, res.StatusCode)
			}

			if tc.checkBody != nil {
				body, err := io.ReadAll(res.Body)
				if err != nil {
					t.Fatalf("reading body: %v", err)
				}
				tc.checkBody(t, body)
			}
		})
	}
}
