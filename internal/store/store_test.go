package store_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zhubert/crowlink/internal/shortcode"
	"github.com/zhubert/crowlink/internal/store"
)

// storeCase names a Store implementation under test and knows how to
// construct a fresh, empty instance of it.
type storeCase struct {
	name string
	// newStoreClock builds an empty store that reads the current time from
	// now, so expiry can be exercised without sleeping. Pass nil for now to
	// get the default time.Now clock.
	newStoreClock func(t *testing.T, now func() time.Time) store.Store
}

// newStore builds an empty store using the default clock.
func (tc storeCase) newStore(t *testing.T) store.Store {
	return tc.newStoreClock(t, nil)
}

// storeCases enumerates the Store implementations exercised by the
// table-driven tests below: the in-memory MemStore and the persistent
// BoltStore (backed by a temp file per test).
var storeCases = []storeCase{
	{
		name: "MemStore",
		newStoreClock: func(t *testing.T, now func() time.Time) store.Store {
			return store.NewMemStore(store.WithClock(now))
		},
	},
	{
		name: "BoltStore",
		newStoreClock: func(t *testing.T, now func() time.Time) store.Store {
			dir := t.TempDir()
			bs, err := store.NewBoltStore(filepath.Join(dir, "test.db"), store.WithClock(now))
			if err != nil {
				t.Fatalf("NewBoltStore() error: %v", err)
			}
			t.Cleanup(func() {
				if err := bs.Close(); err != nil {
					t.Errorf("Close() error: %v", err)
				}
			})
			return bs
		},
	},
}

// TestPutGet verifies that a URL stored with Put can be retrieved with Get.
func TestPutGet(t *testing.T) {
	for _, tc := range storeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)

			const input = "https://example.com/some/path"
			code, err := s.Put(input, 0)
			if err != nil {
				t.Fatalf("Put(%q) returned unexpected error: %v", input, err)
			}
			if code == "" {
				t.Fatal("Put returned an empty code")
			}

			got, err := s.Get(code)
			if err != nil {
				t.Fatalf("Get(%q) returned unexpected error: %v", code, err)
			}
			if got != input {
				t.Errorf("Get(%q) = %q; want %q", code, got, input)
			}
		})
	}
}

// TestGetUnknown verifies that Get reports ErrNotFound for a code that was never stored.
func TestGetUnknown(t *testing.T) {
	for _, tc := range storeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)

			_, err := s.Get("doesnotexist")
			if !errors.Is(err, store.ErrNotFound) {
				t.Errorf("Get on unknown code error = %v; want it to wrap store.ErrNotFound", err)
			}
		})
	}
}

// TestStore_Concurrent exercises Put and Get from many goroutines simultaneously
// to confirm there are no data races (run with go test -race).
func TestStore_Concurrent(t *testing.T) {
	for _, tc := range storeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)

			const workers = 50
			const puts = 20

			// Phase 1: concurrent Puts — collect all (code, url) pairs.
			type entry struct{ code, url string }
			results := make([]entry, workers*puts)

			var wg sync.WaitGroup
			wg.Add(workers)
			for w := 0; w < workers; w++ {
				w := w
				go func() {
					defer wg.Done()
					for i := 0; i < puts; i++ {
						url := fmt.Sprintf("https://example.com/%d/%d", w, i)
						code, err := s.Put(url, 0)
						if err != nil {
							t.Errorf("Put(%q) error: %v", url, err)
							return
						}
						results[w*puts+i] = entry{code, url}
					}
				}()
			}
			wg.Wait()

			// Phase 2: concurrent Gets — verify every code resolves to its URL.
			wg.Add(workers)
			for w := 0; w < workers; w++ {
				w := w
				go func() {
					defer wg.Done()
					for i := 0; i < puts; i++ {
						e := results[w*puts+i]
						got, err := s.Get(e.code)
						if err != nil {
							t.Errorf("Get(%q) returned unexpected error after Put: %v", e.code, err)
							return
						}
						if got != e.url {
							t.Errorf("Get(%q) = %q; want %q", e.code, got, e.url)
						}
					}
				}()
			}
			wg.Wait()
		})
	}
}

// TestBoltStore_Persistence verifies that values written via a BoltStore
// survive closing and reopening the same underlying file, including the
// persisted id counter (so codes issued after reopening don't collide with
// codes issued before closing).
func TestBoltStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.db")

	bs, err := store.NewBoltStore(path)
	if err != nil {
		t.Fatalf("NewBoltStore() error: %v", err)
	}

	const input = "https://example.com/persisted"
	code, err := bs.Put(input, 0)
	if err != nil {
		t.Fatalf("Put(%q) error: %v", input, err)
	}

	if err := bs.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	reopened, err := store.NewBoltStore(path)
	if err != nil {
		t.Fatalf("NewBoltStore() (reopen) error: %v", err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("Close() error: %v", err)
		}
	}()

	got, err := reopened.Get(code)
	if err != nil {
		t.Fatalf("Get(%q) after reopen returned unexpected error: %v", code, err)
	}
	if got != input {
		t.Errorf("Get(%q) after reopen = %q; want %q", code, got, input)
	}

	// The id counter must also have survived the reopen: a new Put should
	// continue from where it left off, not collide with the earlier code.
	const second = "https://example.com/second"
	secondCode, err := reopened.Put(second, 0)
	if err != nil {
		t.Fatalf("Put(%q) error: %v", second, err)
	}
	if secondCode == code {
		t.Errorf("Put after reopen returned duplicate code %q; counter did not persist", secondCode)
	}
}

// TestIncrementClicksAndStats verifies that a freshly stored record starts at
// zero clicks with a populated created_at, and that each IncrementClicks call
// bumps the counter reported by Stats.
func TestIncrementClicksAndStats(t *testing.T) {
	for _, tc := range storeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)

			const input = "https://example.com/some/path"
			before := time.Now().UTC().Add(-time.Second)
			code, err := s.Put(input, 0)
			if err != nil {
				t.Fatalf("Put(%q) returned unexpected error: %v", input, err)
			}

			rec, err := s.Stats(code)
			if err != nil {
				t.Fatalf("Stats(%q) returned unexpected error: %v", code, err)
			}
			if rec.Code != code {
				t.Errorf("Stats(%q).Code = %q; want %q", code, rec.Code, code)
			}
			if rec.URL != input {
				t.Errorf("Stats(%q).URL = %q; want %q", code, rec.URL, input)
			}
			if rec.Clicks != 0 {
				t.Errorf("Stats(%q).Clicks = %d; want 0", code, rec.Clicks)
			}
			if rec.CreatedAt.Before(before) {
				t.Errorf("Stats(%q).CreatedAt = %v; want a time at or after %v", code, rec.CreatedAt, before)
			}
			if rec.ExpiresAt != nil {
				t.Errorf("Stats(%q).ExpiresAt = %v; want nil for a link stored without a ttl", code, rec.ExpiresAt)
			}

			const n = 3
			for i := 0; i < n; i++ {
				if err := s.IncrementClicks(code); err != nil {
					t.Fatalf("IncrementClicks(%q) returned unexpected error: %v", code, err)
				}
			}

			rec, err = s.Stats(code)
			if err != nil {
				t.Fatalf("Stats(%q) returned unexpected error after increments: %v", code, err)
			}
			if rec.Clicks != n {
				t.Errorf("Stats(%q).Clicks = %d; want %d", code, rec.Clicks, n)
			}
		})
	}
}

// TestStatsUnknown verifies that Stats and IncrementClicks both report ErrNotFound
// reports an error for a code that was never stored.
func TestStatsUnknown(t *testing.T) {
	for _, tc := range storeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)

			const unknown = "nope"
			if _, err := s.Stats(unknown); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("Stats(%q) error = %v; want it to wrap store.ErrNotFound", unknown, err)
			}
			err := s.IncrementClicks(unknown)
			if err == nil {
				t.Fatalf("IncrementClicks(%q) returned nil error; want an error", unknown)
			}
			if !errors.Is(err, store.ErrNotFound) {
				t.Errorf("IncrementClicks(%q) error = %v; want it to wrap store.ErrNotFound", unknown, err)
			}
		})
	}
}

// TestPutAlias verifies that a URL stored under a custom alias is retrievable
// under that alias and carries it as its code.
func TestPutAlias(t *testing.T) {
	for _, tc := range storeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)

			const alias = "my-link"
			const url = "https://example.com/custom"

			if err := s.PutAlias(url, alias, 0); err != nil {
				t.Fatalf("PutAlias(%q, %q) unexpected error: %v", url, alias, err)
			}

			got, err := s.Get(alias)
			if err != nil {
				t.Fatalf("Get(%q) returned unexpected error: %v", alias, err)
			}
			if got != url {
				t.Errorf("Get(%q) = %q; want %q", alias, got, url)
			}

			rec, err := s.Stats(alias)
			if err != nil {
				t.Fatalf("Stats(%q) returned unexpected error: %v", alias, err)
			}
			if rec.Code != alias {
				t.Errorf("Stats(%q).Code = %q; want %q", alias, rec.Code, alias)
			}
			if rec.CreatedAt.IsZero() {
				t.Errorf("Stats(%q).CreatedAt is zero; want a timestamp", alias)
			}
		})
	}
}

// TestPutAliasDuplicate verifies that reusing an alias fails with
// ErrAliasTaken and leaves the original entry untouched.
func TestPutAliasDuplicate(t *testing.T) {
	for _, tc := range storeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)

			const alias = "taken"
			const first = "https://example.com/first"

			if err := s.PutAlias(first, alias, 0); err != nil {
				t.Fatalf("PutAlias(%q, %q) unexpected error: %v", first, alias, err)
			}

			err := s.PutAlias("https://example.com/second", alias, 0)
			if err == nil {
				t.Fatalf("PutAlias with duplicate alias %q returned nil error; want an error", alias)
			}
			if !errors.Is(err, store.ErrAliasTaken) {
				t.Errorf("PutAlias duplicate error = %v; want it to wrap store.ErrAliasTaken", err)
			}

			if got, _ := s.Get(alias); got != first {
				t.Errorf("Get(%q) = %q after failed overwrite; want %q", alias, got, first)
			}
		})
	}
}

// TestPutSkipsAliasedCodes verifies that generated codes never overwrite an
// entry already claimed by a custom alias.
func TestPutSkipsAliasedCodes(t *testing.T) {
	for _, tc := range storeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)

			// The first generated code is deterministic; claim it as an
			// alias so Put must skip past it.
			first := shortcode.Encode(1)
			const aliasURL = "https://example.com/reserved"
			if err := s.PutAlias(aliasURL, first, 0); err != nil {
				t.Fatalf("PutAlias(%q, %q) unexpected error: %v", aliasURL, first, err)
			}

			code, err := s.Put("https://example.com/generated", 0)
			if err != nil {
				t.Fatalf("Put() unexpected error: %v", err)
			}
			if code == first {
				t.Fatalf("Put() returned %q, which is already claimed by an alias", code)
			}

			if got, _ := s.Get(first); got != aliasURL {
				t.Errorf("Get(%q) = %q; want the alias target %q", first, got, aliasURL)
			}
		})
	}
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

// TestPutWithTTL verifies that a link stored with a ttl resolves before its
// expiry and reports ErrExpired at and after it.
func TestPutWithTTL(t *testing.T) {
	for _, tc := range storeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
			s := tc.newStoreClock(t, clock.Now)

			const input = "https://example.com/expiring"
			const ttl = time.Hour

			code, err := s.Put(input, ttl)
			if err != nil {
				t.Fatalf("Put(%q, %v) unexpected error: %v", input, ttl, err)
			}

			rec, err := s.Stats(code)
			if err != nil {
				t.Fatalf("Stats(%q) before expiry: %v", code, err)
			}
			if rec.ExpiresAt == nil {
				t.Fatalf("Stats(%q).ExpiresAt is nil; want a timestamp", code)
			}
			if want := clock.Now().Add(ttl); !rec.ExpiresAt.Equal(want) {
				t.Errorf("Stats(%q).ExpiresAt = %v; want %v", code, rec.ExpiresAt, want)
			}

			// Just shy of expiry the link still resolves.
			clock.Advance(ttl - time.Nanosecond)
			got, err := s.Get(code)
			if err != nil {
				t.Fatalf("Get(%q) just before expiry: %v", code, err)
			}
			if got != input {
				t.Errorf("Get(%q) = %q; want %q", code, got, input)
			}

			// At the expiry instant it is gone, and stays gone after it.
			clock.Advance(time.Nanosecond)
			if _, err := s.Get(code); !errors.Is(err, store.ErrExpired) {
				t.Errorf("Get(%q) at expiry error = %v; want it to wrap store.ErrExpired", code, err)
			}
			if _, err := s.Stats(code); !errors.Is(err, store.ErrExpired) {
				t.Errorf("Stats(%q) at expiry error = %v; want it to wrap store.ErrExpired", code, err)
			}

			clock.Advance(24 * time.Hour)
			if _, err := s.Get(code); !errors.Is(err, store.ErrExpired) {
				t.Errorf("Get(%q) after expiry error = %v; want it to wrap store.ErrExpired", code, err)
			}
		})
	}
}

// TestPutWithoutTTLNeverExpires verifies that links stored with a zero ttl
// keep resolving no matter how far the clock moves.
func TestPutWithoutTTLNeverExpires(t *testing.T) {
	for _, tc := range storeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
			s := tc.newStoreClock(t, clock.Now)

			const input = "https://example.com/forever"
			code, err := s.Put(input, 0)
			if err != nil {
				t.Fatalf("Put(%q, 0) unexpected error: %v", input, err)
			}

			clock.Advance(100 * 365 * 24 * time.Hour)

			got, err := s.Get(code)
			if err != nil {
				t.Fatalf("Get(%q) a century later: %v", code, err)
			}
			if got != input {
				t.Errorf("Get(%q) = %q; want %q", code, got, input)
			}
			rec, err := s.Stats(code)
			if err != nil {
				t.Fatalf("Stats(%q) a century later: %v", code, err)
			}
			if rec.ExpiresAt != nil {
				t.Errorf("Stats(%q).ExpiresAt = %v; want nil", code, rec.ExpiresAt)
			}
		})
	}
}

// TestPutAliasWithTTL verifies that custom aliases honour expiry too.
func TestPutAliasWithTTL(t *testing.T) {
	for _, tc := range storeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
			s := tc.newStoreClock(t, clock.Now)

			const alias = "temp-link"
			const url = "https://example.com/temp"

			if err := s.PutAlias(url, alias, time.Minute); err != nil {
				t.Fatalf("PutAlias(%q, %q, 1m) unexpected error: %v", url, alias, err)
			}

			if got, err := s.Get(alias); err != nil || got != url {
				t.Fatalf("Get(%q) = (%q, %v); want (%q, nil)", alias, got, err, url)
			}

			clock.Advance(time.Minute)
			if _, err := s.Get(alias); !errors.Is(err, store.ErrExpired) {
				t.Errorf("Get(%q) after expiry error = %v; want it to wrap store.ErrExpired", alias, err)
			}
		})
	}
}
