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
// GET /{code}/stats. ExpiresAt is nil for links that never expire.
type Record struct {
	Code      string     `json:"code"`
	URL       string     `json:"url"`
	Clicks    uint64     `json:"clicks"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// expired reports whether the record has an expiry that is at or before now.
// Records without an ExpiresAt never expire.
func (r Record) expired(now time.Time) bool {
	return r.ExpiresAt != nil && !now.Before(*r.ExpiresAt)
}

// Store is the interface that wraps the basic Put, PutAlias, Get,
// IncrementClicks and Stats methods.
//
// Put stores the given URL and returns the short code assigned to it.
// PutAlias stores the given URL under a caller-chosen alias, returning
// ErrAliasTaken if that alias is already in use. Both take a ttl: a positive
// ttl expires the link that far past the store's clock, while a zero or
// negative ttl stores a link that never expires.
// Get retrieves the URL associated with the given short code, returning
// ErrNotFound if the code is unknown and ErrExpired if its ttl has elapsed.
// IncrementClicks bumps the click counter for code, returning an error if
// the code is unknown or the write fails.
// Stats returns the full Record for code, with the same ErrNotFound and
// ErrExpired semantics as Get.
//
// MemStore (below) and BoltStore (bolt.go) both satisfy Store: MemStore
// keeps everything in memory, while BoltStore persists records to a bbolt
// file on disk so they survive process restarts.
type Store interface {
	Put(url string, ttl time.Duration) (code string, err error)
	PutAlias(url, alias string, ttl time.Duration) error
	Get(code string) (url string, err error)
	IncrementClicks(code string) error
	Stats(code string) (rec Record, err error)
}

// ErrNotFound is returned by Get, Stats and IncrementClicks when the given
// code is unknown.
var ErrNotFound = fmt.Errorf("store: code not found")

// ErrExpired is returned by Get and Stats when the given code exists but its
// expiry has passed.
var ErrExpired = fmt.Errorf("store: code expired")

// ErrAliasTaken is returned by PutAlias when the requested alias is already
// in use.
var ErrAliasTaken = fmt.Errorf("store: alias already taken")

// options carries the settings shared by every Store implementation.
type options struct {
	now func() time.Time
}

// Option configures a Store implementation at construction time.
type Option func(*options)

// WithClock overrides the clock a store uses to stamp CreatedAt/ExpiresAt and
// to decide whether a record has expired. It exists so expiry can be tested
// without sleeping; production code leaves the default of time.Now.
func WithClock(now func() time.Time) Option {
	return func(o *options) {
		if now != nil {
			o.now = now
		}
	}
}

// newOptions applies opts on top of the defaults.
func newOptions(opts []Option) options {
	o := options{now: time.Now}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// expiryFor returns the expiry stamp for a link created now with the given
// ttl, or nil when the link should never expire.
func expiryFor(now time.Time, ttl time.Duration) *time.Time {
	if ttl <= 0 {
		return nil
	}
	at := now.Add(ttl)
	return &at
}

// MemStore is an in-memory implementation of Store. It is safe for concurrent
// use by multiple goroutines.
type MemStore struct {
	mu      sync.Mutex
	entries map[string]Record // code → record
	counter uint64
	now     func() time.Time
}

// Ensure MemStore satisfies the Store interface.
var _ Store = (*MemStore)(nil)

// NewMemStore returns an initialized *MemStore.
func NewMemStore(opts ...Option) *MemStore {
	return &MemStore{
		entries: make(map[string]Record),
		now:     newOptions(opts).now,
	}
}

// Put stores url and returns the short code derived from an auto-incrementing
// counter encoded via shortcode.Encode. Codes already claimed by a custom
// alias are skipped. A positive ttl makes the link expire that far in the
// future; a zero or negative ttl stores a link that never expires.
func (m *MemStore) Put(url string, ttl time.Duration) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var code string
	for {
		m.counter++
		code = shortcode.Encode(m.counter)
		if _, taken := m.entries[code]; !taken {
			break
		}
	}

	now := m.now().UTC()
	m.entries[code] = Record{
		Code:      code,
		URL:       url,
		CreatedAt: now,
		ExpiresAt: expiryFor(now, ttl),
	}
	return code, nil
}

// PutAlias stores url under the caller-chosen alias, honouring ttl the same
// way Put does. It returns ErrAliasTaken if the alias is already in use. The
// alias is assumed to have been validated by the caller (see
// internal/validate.Alias).
func (m *MemStore) PutAlias(url, alias string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, taken := m.entries[alias]; taken {
		return fmt.Errorf("%w: %q", ErrAliasTaken, alias)
	}

	now := m.now().UTC()
	m.entries[alias] = Record{
		Code:      alias,
		URL:       url,
		CreatedAt: now,
		ExpiresAt: expiryFor(now, ttl),
	}
	return nil
}

// Get returns the URL stored under code. It returns ErrNotFound when code is
// unknown and ErrExpired once the link's expiry has passed.
func (m *MemStore) Get(code string) (string, error) {
	rec, err := m.Stats(code)
	return rec.URL, err
}

// IncrementClicks bumps the click counter for code by one. It returns
// ErrNotFound when code is unknown. Expired records are left to Get and
// Stats to reject, so this counts a click on whatever entry is stored.
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

// Stats returns the full Record stored under code. It returns ErrNotFound
// when code is unknown and ErrExpired once the link's expiry has passed.
func (m *MemStore) Stats(code string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.entries[code]
	if !ok {
		return Record{}, fmt.Errorf("%w: %q", ErrNotFound, code)
	}
	if rec.expired(m.now()) {
		return Record{}, fmt.Errorf("%w: %q", ErrExpired, code)
	}
	return rec, nil
}
