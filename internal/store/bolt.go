package store

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
// the id counter in a "meta" bucket and code→record entries in a "links"
// bucket so that entries survive process restarts. Records are serialized
// as JSON.
//
// BoltStore is safe for concurrent use by multiple goroutines; bbolt
// transactions provide the necessary locking.
type BoltStore struct {
	db  *bbolt.DB
	now func() time.Time
}

// Ensure BoltStore satisfies the Store interface.
var _ Store = (*BoltStore)(nil)

// NewBoltStore opens (creating if necessary) the bbolt database file at
// path and ensures the buckets required by BoltStore exist.
func NewBoltStore(path string, opts ...Option) (*BoltStore, error) {
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

	return &BoltStore{db: db, now: newOptions(opts).now}, nil
}

// decodeRecord unmarshals a stored value into a Record. Databases written
// before click analytics existed hold the bare URL string rather than JSON;
// those values are read as a Record carrying only the URL.
func decodeRecord(code string, v []byte) Record {
	var rec Record
	if err := json.Unmarshal(v, &rec); err != nil {
		return Record{Code: code, URL: string(v)}
	}
	rec.Code = code
	return rec
}

// Put stores url under a new short code derived from a persisted,
// auto-incrementing counter (encoded via shortcode.Encode), and returns the
// code. Codes already claimed by a custom alias are skipped. A positive ttl
// makes the link expire that far in the future; a zero or negative ttl stores
// a link that never expires.
func (b *BoltStore) Put(url string, ttl time.Duration) (string, error) {
	var code string

	err := b.db.Update(func(tx *bbolt.Tx) error {
		meta := tx.Bucket(metaBucket)
		links := tx.Bucket(linksBucket)

		var counter uint64
		if v := meta.Get(counterKey); v != nil {
			counter = binary.BigEndian.Uint64(v)
		}
		for {
			counter++
			code = shortcode.Encode(counter)
			if links.Get([]byte(code)) == nil {
				break
			}
		}

		now := b.now().UTC()
		rec, err := json.Marshal(Record{
			Code:      code,
			URL:       url,
			CreatedAt: now,
			ExpiresAt: expiryFor(now, ttl),
		})
		if err != nil {
			return err
		}
		if err := links.Put([]byte(code), rec); err != nil {
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

// PutAlias stores url under the caller-chosen alias, honouring ttl the same
// way Put does. It returns ErrAliasTaken if the alias is already in use. The
// alias is assumed to have been validated by the caller (see
// internal/validate.Alias).
func (b *BoltStore) PutAlias(url, alias string, ttl time.Duration) error {
	err := b.db.Update(func(tx *bbolt.Tx) error {
		links := tx.Bucket(linksBucket)

		if links.Get([]byte(alias)) != nil {
			return fmt.Errorf("%w: %q", ErrAliasTaken, alias)
		}

		now := b.now().UTC()
		rec, err := json.Marshal(Record{
			Code:      alias,
			URL:       url,
			CreatedAt: now,
			ExpiresAt: expiryFor(now, ttl),
		})
		if err != nil {
			return err
		}
		return links.Put([]byte(alias), rec)
	})
	if err != nil {
		if errors.Is(err, ErrAliasTaken) {
			return err
		}
		return fmt.Errorf("store: put alias: %w", err)
	}
	return nil
}

// Get returns the URL stored under code. It returns ErrNotFound when code is
// unknown and ErrExpired once the link's expiry has passed.
func (b *BoltStore) Get(code string) (string, error) {
	rec, err := b.Stats(code)
	return rec.URL, err
}

// IncrementClicks bumps the click counter for code by one. It returns
// ErrNotFound when code is unknown. Expired records are left to Get and
// Stats to reject, so this counts a click on whatever entry is stored.
func (b *BoltStore) IncrementClicks(code string) error {
	err := b.db.Update(func(tx *bbolt.Tx) error {
		links := tx.Bucket(linksBucket)
		if links == nil {
			return fmt.Errorf("%w: %q", ErrNotFound, code)
		}
		v := links.Get([]byte(code))
		if v == nil {
			return fmt.Errorf("%w: %q", ErrNotFound, code)
		}

		rec := decodeRecord(code, v)
		rec.Clicks++

		encoded, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		return links.Put([]byte(code), encoded)
	})
	if err != nil {
		return fmt.Errorf("store: increment clicks: %w", err)
	}
	return nil
}

// Stats returns the full Record stored under code. It returns ErrNotFound
// when code is unknown and ErrExpired once the link's expiry has passed.
func (b *BoltStore) Stats(code string) (Record, error) {
	var rec Record
	var ok bool

	// bbolt's View never returns an error for read-only lookups like this;
	// the closure itself cannot fail, so the error is safely discarded.
	_ = b.db.View(func(tx *bbolt.Tx) error {
		links := tx.Bucket(linksBucket)
		if links == nil {
			return nil
		}
		if v := links.Get([]byte(code)); v != nil {
			rec = decodeRecord(code, v)
			ok = true
		}
		return nil
	})

	if !ok {
		return Record{}, fmt.Errorf("%w: %q", ErrNotFound, code)
	}
	if rec.expired(b.now()) {
		return Record{}, fmt.Errorf("%w: %q", ErrExpired, code)
	}
	return rec, nil
}

// Close closes the underlying bbolt database, flushing any pending writes
// to disk. It should be called before the process exits.
func (b *BoltStore) Close() error {
	return b.db.Close()
}
