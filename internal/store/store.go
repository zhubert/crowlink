// Package store provides a storage abstraction for the URL shortener service.
package store

import (
	"fmt"
	"sync"
	"time"

	"github.com/zhubert/crowlink/internal/shortcode"
)

// Record is the full stored entry for a short code: the target URL plus the
// click analytics tracked alongside it. It is the JSON shape served by
// GET /{code}/stats.
type Record struct {
	Code      string    `json:"code"`
	URL       string    `json:"url"`
	Clicks    uint64    `json:"clicks"`
	CreatedAt time.Time `json:"created_at"`
}

// Store is the interface that wraps the basic Put, Get, IncrementClicks and
// Stats methods.
//
// Put stores the given URL and returns the short code assigned to it.
// Get retrieves the URL associated with the given short code; ok is false
// if the code is not found.
// IncrementClicks bumps the click counter for code, returning an error if
// the code is unknown or the write fails.
// Stats returns the full Record for code; ok is false if the code is not
// found.
//
// MemStore (below) and BoltStore (bolt.go) both satisfy Store: MemStore
// keeps everything in memory, while BoltStore persists records to a bbolt
// file on disk so they survive process restarts.
type Store interface {
	Put(url string) (code string, err error)
	Get(code string) (url string, ok bool)
	IncrementClicks(code string) error
	Stats(code string) (rec Record, ok bool)
}

// ErrNotFound is returned by IncrementClicks when the given code is unknown.
var ErrNotFound = fmt.Errorf("store: code not found")

// MemStore is an in-memory implementation of Store. It is safe for concurrent
// use by multiple goroutines.
type MemStore struct {
	mu      sync.Mutex
	entries map[string]Record // code → record
	counter uint64
}

// Ensure MemStore satisfies the Store interface.
var _ Store = (*MemStore)(nil)

// NewMemStore returns an initialized *MemStore.
func NewMemStore() *MemStore {
	return &MemStore{
		entries: make(map[string]Record),
	}
}

// Put stores url and returns the short code derived from an auto-incrementing
// counter encoded via shortcode.Encode.
func (m *MemStore) Put(url string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.counter++
	code := shortcode.Encode(m.counter)
	m.entries[code] = Record{
		Code:      code,
		URL:       url,
		CreatedAt: time.Now().UTC(),
	}
	return code, nil
}

// Get returns the URL stored under code. ok is false when code is unknown.
func (m *MemStore) Get(code string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.entries[code]
	return rec.URL, ok
}

// IncrementClicks bumps the click counter for code by one. It returns
// ErrNotFound when code is unknown.
func (m *MemStore) IncrementClicks(code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.entries[code]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, code)
	}
	rec.Clicks++
	m.entries[code] = rec
	return nil
}

// Stats returns the full Record stored under code. ok is false when code is
// unknown.
func (m *MemStore) Stats(code string) (Record, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.entries[code]
	return rec, ok
}
