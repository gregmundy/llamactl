package hf

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Cache is a filesystem-backed cache with TTL envelopes. Entries are stored
// under <root>/<namespace>/<sha1(key)>.json.
//
// Get returns (payload, fresh, error). A stale hit still returns payload —
// the caller decides whether to use it (offline fallback) or refresh.
type Cache struct {
	root string
	now  func() time.Time
}

// NewCache returns a Cache rooted at dir. Directory creation is lazy.
func NewCache(dir string) *Cache {
	return &Cache{root: dir, now: time.Now}
}

type envelope struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Payload   json.RawMessage `json:"payload"`
}

func (c *Cache) path(ns, key string) string {
	sum := sha1.Sum([]byte(key))
	return filepath.Join(c.root, ns, hex.EncodeToString(sum[:])+".json")
}

func (c *Cache) Put(ns, key string, payload []byte) error {
	p := c.path(ns, key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("mkdir cache: %w", err)
	}
	env := envelope{FetchedAt: c.now(), Payload: payload}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// PruneOlderThan removes files under the cache root whose mtime is older
// than d. Returns the count removed. Missing cache root is not an error.
// Best-effort: per-entry errors are skipped; the walk continues.
func (c *Cache) PruneOlderThan(d time.Duration) (int, error) {
	cutoff := c.now().Add(-d)
	removed := 0
	err := filepath.WalkDir(c.root, func(p string, e fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if e.IsDir() {
			return nil
		}
		info, ierr := e.Info()
		if ierr != nil {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(p)
			if _, statErr := os.Stat(p); os.IsNotExist(statErr) {
				removed++
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return removed, err
	}
	return removed, nil
}

func (c *Cache) Get(ns, key string, ttl time.Duration) ([]byte, bool, error) {
	data, err := os.ReadFile(c.path(ns, key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read cache: %w", err)
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		// Corrupt entry — treat as miss.
		return nil, false, nil
	}
	fresh := c.now().Sub(env.FetchedAt) <= ttl
	return env.Payload, fresh, nil
}
