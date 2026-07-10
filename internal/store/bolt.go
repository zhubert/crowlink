package store

import (
	"encoding/binary"
	"fmt"

	"go.etcd.io/bbolt"

	"github.com/zhubert/crowlink/internal/shortcode"
)

var (
	linksBucket = []byte("links")
	metaBucket  = []byte("meta")
	counterKey  = []byte("counter")
)

// BoltStore is a persistent implementation of Store backed by a bbolt file.
// It reuses the integer-id → base62 encoding scheme from MemStore, storing
// the id counter in a "meta" bucket and code→url records in a "links"
// bucket so that entries survive process restarts.
//
// BoltStore is safe for concurrent use by multiple goroutines; bbolt
// transactions provide the necessary locking.
type BoltStore struct {
	db *bbolt.DB
}

// Ensure BoltStore satisfies the Store interface.
var _ Store = (*BoltStore)(nil)

// NewBoltStore opens (creating if necessary) the bbolt database file at
// path and ensures the buckets required by BoltStore exist.
func NewBoltStore(path string) (*BoltStore, error) {
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("store: opening bolt db %q: %w", path, err)
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(linksBucket); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(metaBucket); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: initializing bolt buckets in %q: %w", path, err)
	}

	return &BoltStore{db: db}, nil
}

// Put stores url under a new short code derived from a persisted,
// auto-incrementing counter (encoded via shortcode.Encode), and returns the
// code.
func (b *BoltStore) Put(url string) (string, error) {
	var code string

	err := b.db.Update(func(tx *bbolt.Tx) error {
		meta := tx.Bucket(metaBucket)
		links := tx.Bucket(linksBucket)

		var counter uint64
		if v := meta.Get(counterKey); v != nil {
			counter = binary.BigEndian.Uint64(v)
		}
		counter++

		code = shortcode.Encode(counter)

		if err := links.Put([]byte(code), []byte(url)); err != nil {
			return err
		}

		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, counter)
		return meta.Put(counterKey, buf)
	})
	if err != nil {
		return "", fmt.Errorf("store: put: %w", err)
	}

	return code, nil
}

// Get returns the URL stored under code. ok is false when code is unknown.
func (b *BoltStore) Get(code string) (string, bool) {
	var url string
	var ok bool

	// bbolt's View never returns an error for read-only lookups like this;
	// the closure itself cannot fail, so the error is safely discarded.
	_ = b.db.View(func(tx *bbolt.Tx) error {
		links := tx.Bucket(linksBucket)
		if links == nil {
			return nil
		}
		if v := links.Get([]byte(code)); v != nil {
			url = string(v)
			ok = true
		}
		return nil
	})

	return url, ok
}

// Close closes the underlying bbolt database, flushing any pending writes
// to disk. It should be called before the process exits.
func (b *BoltStore) Close() error {
	return b.db.Close()
}
