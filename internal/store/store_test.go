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
	name     string
	newStore func(t *testing.T) store.Store
}

// storeCases enumerates the Store implementations exercised by the
// table-driven tests below: the in-memory MemStore and the persistent
// BoltStore (backed by a temp file per test).
var storeCases = []storeCase{
	{
		name: "MemStore",
		newStore: func(t *testing.T) store.Store {
			return store.NewMemStore()
		},
	},
	{
		name: "BoltStore",
		newStore: func(t *testing.T) store.Store {
			dir := t.TempDir()
			bs, err := store.NewBoltStore(filepath.Join(dir, "test.db"))
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
			code, err := s.Put(input)
			if err != nil {
				t.Fatalf("Put(%q) returned unexpected error: %v", input, err)
			}
			if code == "" {
				t.Fatal("Put returned an empty code")
			}

			got, ok := s.Get(code)
			if !ok {
				t.Fatalf("Get(%q) returned ok=false; want ok=true", code)
			}
			if got != input {
				t.Errorf("Get(%q) = %q; want %q", code, got, input)
			}
		})
	}
}

// TestGetUnknown verifies that Get returns ok=false for a code that was never stored.
func TestGetUnknown(t *testing.T) {
	for _, tc := range storeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)

			_, ok := s.Get("doesnotexist")
			if ok {
				t.Error("Get on unknown code returned ok=true; want ok=false")
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
						code, err := s.Put(url)
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
						got, ok := s.Get(e.code)
						if !ok {
							t.Errorf("Get(%q) returned ok=false after Put", e.code)
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
	code, err := bs.Put(input)
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

	got, ok := reopened.Get(code)
	if !ok {
		t.Fatalf("Get(%q) after reopen returned ok=false; want ok=true", code)
	}
	if got != input {
		t.Errorf("Get(%q) after reopen = %q; want %q", code, got, input)
	}

	// The id counter must also have survived the reopen: a new Put should
	// continue from where it left off, not collide with the earlier code.
	const second = "https://example.com/second"
	secondCode, err := reopened.Put(second)
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
			code, err := s.Put(input)
			if err != nil {
				t.Fatalf("Put(%q) returned unexpected error: %v", input, err)
			}

			rec, ok := s.Stats(code)
			if !ok {
				t.Fatalf("Stats(%q) returned ok=false; want ok=true", code)
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

			const n = 3
			for i := 0; i < n; i++ {
				if err := s.IncrementClicks(code); err != nil {
					t.Fatalf("IncrementClicks(%q) returned unexpected error: %v", code, err)
				}
			}

			rec, ok = s.Stats(code)
			if !ok {
				t.Fatalf("Stats(%q) returned ok=false after increments; want ok=true", code)
			}
			if rec.Clicks != n {
				t.Errorf("Stats(%q).Clicks = %d; want %d", code, rec.Clicks, n)
			}
		})
	}
}

// TestStatsUnknown verifies that Stats reports ok=false and IncrementClicks
// reports an error for a code that was never stored.
func TestStatsUnknown(t *testing.T) {
	for _, tc := range storeCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := tc.newStore(t)

			const unknown = "nope"
			if _, ok := s.Stats(unknown); ok {
				t.Errorf("Stats(%q) returned ok=true; want ok=false", unknown)
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

			if err := s.PutAlias(url, alias); err != nil {
				t.Fatalf("PutAlias(%q, %q) unexpected error: %v", url, alias, err)
			}

			got, ok := s.Get(alias)
			if !ok {
				t.Fatalf("Get(%q) returned ok=false; want ok=true", alias)
			}
			if got != url {
				t.Errorf("Get(%q) = %q; want %q", alias, got, url)
			}

			rec, ok := s.Stats(alias)
			if !ok {
				t.Fatalf("Stats(%q) returned ok=false; want ok=true", alias)
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

			if err := s.PutAlias(first, alias); err != nil {
				t.Fatalf("PutAlias(%q, %q) unexpected error: %v", first, alias, err)
			}

			err := s.PutAlias("https://example.com/second", alias)
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
			if err := s.PutAlias(aliasURL, first); err != nil {
				t.Fatalf("PutAlias(%q, %q) unexpected error: %v", aliasURL, first, err)
			}

			code, err := s.Put("https://example.com/generated")
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
