package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhubert/crowlink/internal/server"
	"github.com/zhubert/crowlink/internal/store"
)

func TestHealthz(t *testing.T) {
	handler := server.New(store.NewMemStore(), "http://localhost:8080")

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

	code, err := s.Put(originalURL, 0)
	if err != nil {
		t.Fatalf("Put(%q) unexpected error: %v", originalURL, err)
	}

	handler := server.New(s, "http://localhost:8080")
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
	handler := server.New(store.NewMemStore(), "http://localhost:8080")

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
	handler := server.New(s, "http://localhost:8080")
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

			handler := server.New(store.NewMemStore(), "http://localhost:8080")

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

// TestPostShorten_UsesConfiguredBaseURL verifies that short_url reflects
// whatever base URL was passed to server.New, not a hardcoded value.
func TestPostShorten_UsesConfiguredBaseURL(t *testing.T) {
	const customBaseURL = "https://short.example.com"

	handler := server.New(store.NewMemStore(), customBaseURL)

	req := httptest.NewRequest(http.MethodPost, "/shorten",
		strings.NewReader(`{"url":"https://example.com/some/path"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	var resp map[string]string
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	wantPrefix := customBaseURL + "/"
	if !strings.HasPrefix(resp["short_url"], wantPrefix) {
		t.Errorf("short_url %q does not start with configured base URL %q", resp["short_url"], wantPrefix)
	}
}

// TestStatsAfterRedirects covers the acceptance criterion for click
// analytics: redirect N times, then GET /{code}/stats and expect clicks == N.
func TestStatsAfterRedirects(t *testing.T) {
	s := store.NewMemStore()
	const originalURL = "https://example.com/some/path"

	code, err := s.Put(originalURL, 0)
	if err != nil {
		t.Fatalf("Put(%q) unexpected error: %v", originalURL, err)
	}

	handler := server.New(s, "http://localhost:8080")

	const n = 5
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodGet, "/"+code, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("redirect %d: expected status %d, got %d", i+1, http.StatusFound, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/"+code+"/stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected JSON Content-Type, got %q", ct)
	}

	var body struct {
		Code      string    `json:"code"`
		URL       string    `json:"url"`
		Clicks    uint64    `json:"clicks"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decoding stats body: %v", err)
	}

	if body.Code != code {
		t.Errorf("code = %q; want %q", body.Code, code)
	}
	if body.URL != originalURL {
		t.Errorf("url = %q; want %q", body.URL, originalURL)
	}
	if body.Clicks != n {
		t.Errorf("clicks = %d; want %d", body.Clicks, n)
	}
	if body.CreatedAt.IsZero() {
		t.Error("created_at is the zero time; want the record's creation timestamp")
	}
}

// TestStatsUnknownCode verifies that stats for a code that was never stored
// returns 404.
func TestStatsUnknownCode(t *testing.T) {
	handler := server.New(store.NewMemStore(), "http://localhost:8080")

	req := httptest.NewRequest(http.MethodGet, "/doesnotexist/stats", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

// postShorten issues a POST /shorten with the given raw JSON body and returns
// the recorded response.
func postShorten(t *testing.T, handler http.Handler, body string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Result()
}

// TestPostShortenWithAlias verifies that a valid custom alias is used as the
// short code and resolves on redirect.
func TestPostShortenWithAlias(t *testing.T) {
	handler := server.New(store.NewMemStore(), "http://localhost:8080")

	res := postShorten(t, handler, `{"url":"https://example.com/custom","alias":"my-link"}`)
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", res.StatusCode)
	}

	var got struct {
		Code     string `json:"code"`
		ShortURL string `json:"short_url"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.Code != "my-link" {
		t.Errorf("code = %q; want %q", got.Code, "my-link")
	}
	if want := "http://localhost:8080/my-link"; got.ShortURL != want {
		t.Errorf("short_url = %q; want %q", got.ShortURL, want)
	}

	// The alias must resolve on redirect.
	req := httptest.NewRequest(http.MethodGet, "/my-link", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("GET /my-link: expected status 302, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://example.com/custom" {
		t.Errorf("Location = %q; want %q", loc, "https://example.com/custom")
	}
}

// TestPostShortenDuplicateAlias verifies that requesting an alias that is
// already in use returns 409 Conflict and leaves the original mapping intact.
func TestPostShortenDuplicateAlias(t *testing.T) {
	handler := server.New(store.NewMemStore(), "http://localhost:8080")

	first := postShorten(t, handler, `{"url":"https://example.com/first","alias":"dupe"}`)
	first.Body.Close()
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first POST: expected status 201, got %d", first.StatusCode)
	}

	second := postShorten(t, handler, `{"url":"https://example.com/second","alias":"dupe"}`)
	defer second.Body.Close()
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate alias: expected status 409, got %d", second.StatusCode)
	}

	req := httptest.NewRequest(http.MethodGet, "/dupe", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if loc := rec.Header().Get("Location"); loc != "https://example.com/first" {
		t.Errorf("Location = %q after conflicting request; want %q", loc, "https://example.com/first")
	}
}

// TestPostShortenInvalidAlias verifies that aliases with a disallowed charset
// or matching a reserved server path are rejected with 400.
func TestPostShortenInvalidAlias(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid charset", body: `{"url":"https://example.com","alias":"bad alias!"}`},
		{name: "path separator", body: `{"url":"https://example.com","alias":"foo/bar"}`},
		{name: "too long", body: `{"url":"https://example.com","alias":"` + strings.Repeat("a", 65) + `"}`},
		{name: "reserved healthz", body: `{"url":"https://example.com","alias":"healthz"}`},
		{name: "reserved shorten", body: `{"url":"https://example.com","alias":"shorten"}`},
		{name: "reserved metrics", body: `{"url":"https://example.com","alias":"metrics"}`},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			handler := server.New(store.NewMemStore(), "http://localhost:8080")

			res := postShorten(t, handler, tc.body)
			defer res.Body.Close()

			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", res.StatusCode)
			}
		})
	}
}

// TestExpiredLinkIsGone covers the acceptance criterion for link expiry: with
// an injected clock, a code created with expires_in resolves before its expiry
// and returns 410 Gone — on both redirect and stats — once it has passed.
func TestExpiredLinkIsGone(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	s := store.NewMemStore(store.WithClock(clock.Now))
	handler := server.New(s, "http://localhost:8080")

	res := postShorten(t, handler, `{"url":"https://example.com/temp","expires_in":"1h"}`)
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", res.StatusCode)
	}

	var created struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	// Before expiry: redirect and stats both work.
	if got := get(t, handler, "/"+created.Code); got.Code != http.StatusFound {
		t.Fatalf("GET /%s before expiry: expected status 302, got %d", created.Code, got.Code)
	}
	statsRec := get(t, handler, "/"+created.Code+"/stats")
	if statsRec.Code != http.StatusOK {
		t.Fatalf("GET /%s/stats before expiry: expected status 200, got %d", created.Code, statsRec.Code)
	}
	var rec store.Record
	if err := json.NewDecoder(statsRec.Body).Decode(&rec); err != nil {
		t.Fatalf("decoding stats: %v", err)
	}
	if rec.ExpiresAt == nil {
		t.Error("stats expires_at is missing; want the link's expiry")
	} else if want := clock.Now().Add(time.Hour); !rec.ExpiresAt.Equal(want) {
		t.Errorf("stats expires_at = %v; want %v", rec.ExpiresAt, want)
	}

	// After expiry: both are 410 Gone.
	clock.Advance(time.Hour)
	if got := get(t, handler, "/"+created.Code); got.Code != http.StatusGone {
		t.Errorf("GET /%s after expiry: expected status 410, got %d", created.Code, got.Code)
	}
	if got := get(t, handler, "/"+created.Code+"/stats"); got.Code != http.StatusGone {
		t.Errorf("GET /%s/stats after expiry: expected status 410, got %d", created.Code, got.Code)
	}
}

// TestLinkWithoutExpiresInNeverExpires verifies that a link created without
// expires_in keeps resolving however far the clock advances.
func TestLinkWithoutExpiresInNeverExpires(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	s := store.NewMemStore(store.WithClock(clock.Now))
	handler := server.New(s, "http://localhost:8080")

	res := postShorten(t, handler, `{"url":"https://example.com/forever"}`)
	defer res.Body.Close()

	var created struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	clock.Advance(100 * 365 * 24 * time.Hour)

	rec := get(t, handler, "/"+created.Code)
	if rec.Code != http.StatusFound {
		t.Fatalf("GET /%s a century later: expected status 302, got %d", created.Code, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://example.com/forever" {
		t.Errorf("Location = %q; want %q", loc, "https://example.com/forever")
	}
	if got := get(t, handler, "/"+created.Code+"/stats"); got.Code != http.StatusOK {
		t.Errorf("GET /%s/stats a century later: expected status 200, got %d", created.Code, got.Code)
	}
}

// TestExpiringAlias verifies that expires_in applies to custom aliases too.
func TestExpiringAlias(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	s := store.NewMemStore(store.WithClock(clock.Now))
	handler := server.New(s, "http://localhost:8080")

	res := postShorten(t, handler, `{"url":"https://example.com/temp","alias":"temp","expires_in":600}`)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", res.StatusCode)
	}

	if got := get(t, handler, "/temp"); got.Code != http.StatusFound {
		t.Fatalf("GET /temp before expiry: expected status 302, got %d", got.Code)
	}

	clock.Advance(10 * time.Minute)
	if got := get(t, handler, "/temp"); got.Code != http.StatusGone {
		t.Errorf("GET /temp after expiry: expected status 410, got %d", got.Code)
	}
}

// TestPostShortenInvalidExpiresIn verifies that an unusable expires_in value
// is rejected with 400 before anything is stored.
func TestPostShortenInvalidExpiresIn(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "zero", body: `{"url":"https://example.com","expires_in":0}`},
		{name: "negative", body: `{"url":"https://example.com","expires_in":-60}`},
		{name: "unparseable duration", body: `{"url":"https://example.com","expires_in":"soon"}`},
		{name: "wrong type", body: `{"url":"https://example.com","expires_in":true}`},
		{name: "beyond maximum", body: `{"url":"https://example.com","expires_in":"87601h"}`},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			handler := server.New(store.NewMemStore(), "http://localhost:8080")

			res := postShorten(t, handler, tc.body)
			defer res.Body.Close()

			if res.StatusCode != http.StatusBadRequest {
				t.Errorf("expected status 400, got %d", res.StatusCode)
			}
		})
	}
}

// get issues a GET request against handler and returns the recorder.
func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// fakeClock is a manually advanced clock used to exercise expiry without
// sleeping. It is safe for concurrent use.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{t: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
