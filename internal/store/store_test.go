package store_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/zhubert/crowlink/internal/store"
)

// TestPutGet verifies that a URL stored with Put can be retrieved with Get.
func TestPutGet(t *testing.T) {
	s := store.NewMemStore()

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
}

// TestGetUnknown verifies that Get returns ok=false for a code that was never stored.
func TestGetUnknown(t *testing.T) {
	s := store.NewMemStore()

	_, ok := s.Get("doesnotexist")
	if ok {
		t.Error("Get on unknown code returned ok=true; want ok=false")
	}
}

// TestMemStore_Concurrent exercises Put and Get from many goroutines simultaneously
// to confirm there are no data races (run with go test -race).
func TestMemStore_Concurrent(t *testing.T) {
	s := store.NewMemStore()

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
}
