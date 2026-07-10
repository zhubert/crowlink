package store_test

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

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
