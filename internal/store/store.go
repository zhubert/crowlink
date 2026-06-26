// Package store provides a storage abstraction for the URL shortener service.
package store

import (
	"sync"

	"github.com/zhubert/crowlink/internal/shortcode"
)

// Store is the interface that wraps the basic Put and Get methods.
//
// Put stores the given URL and returns the short code assigned to it.
// Get retrieves the URL associated with the given short code; ok is false
// if the code is not found.
type Store interface {
	Put(url string) (code string, err error)
	Get(code string) (url string, ok bool)
}

// MemStore is an in-memory implementation of Store. It is safe for concurrent
// use by multiple goroutines.
type MemStore struct {
	mu      sync.Mutex
	entries map[string]string // code → url
	counter uint64
}

// NewMemStore returns an initialized *MemStore.
func NewMemStore() *MemStore {
	return &MemStore{
		entries: make(map[string]string),
	}
}

// Put stores url and returns the short code derived from an auto-incrementing
// counter encoded via shortcode.Encode.
func (m *MemStore) Put(url string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.counter++
	code := shortcode.Encode(m.counter)
	m.entries[code] = url
	return code, nil
}

// Get returns the URL stored under code. ok is false when code is unknown.
func (m *MemStore) Get(code string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	url, ok := m.entries[code]
	return url, ok
}
