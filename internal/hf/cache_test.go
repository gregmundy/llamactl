package hf

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCachePutGet(t *testing.T) {
	c := NewCache(t.TempDir())
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	payload := []byte(`{"hello":"world"}`)
	if err := c.Put("ns", "key", payload); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, fresh, err := c.Get("ns", "key", time.Hour)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !fresh {
		t.Errorf("fresh = false; want true (just written)")
	}
	if string(got) != string(payload) {
		t.Errorf("payload = %q, want %q", string(got), string(payload))
	}
}

func TestCacheStale(t *testing.T) {
	c := NewCache(t.TempDir())
	c.now = func() time.Time { return time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC) }
	if err := c.Put("ns", "key", []byte(`{}`)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	c.now = func() time.Time { return time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC) }
	got, fresh, err := c.Get("ns", "key", time.Hour)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fresh {
		t.Errorf("fresh = true; want false (24h elapsed, TTL 1h)")
	}
	if got == nil {
		t.Errorf("payload still returned even when stale (caller decides)")
	}
}

func TestCacheMiss(t *testing.T) {
	c := NewCache(t.TempDir())
	got, fresh, err := c.Get("ns", "missing", time.Hour)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil || fresh {
		t.Errorf("miss should return nil/false, got (%v, %v)", got, fresh)
	}
}

func TestCachePruneOlderThan(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)

	oldFile := filepath.Join(dir, "ns1", "old.json")
	if err := os.MkdirAll(filepath.Dir(oldFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldFile, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-60 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	freshFile := filepath.Join(dir, "ns2", "fresh.json")
	if err := os.MkdirAll(filepath.Dir(freshFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(freshFile, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := c.PruneOlderThan(30 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("removed=%d, want 1", n)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("old file should be removed")
	}
	if _, err := os.Stat(freshFile); err != nil {
		t.Fatal("fresh file should still exist")
	}
}

func TestCachePruneOlderThanMissingRoot(t *testing.T) {
	c := NewCache("/definitely-does-not-exist/llamactl-test")
	n, err := c.PruneOlderThan(1 * time.Hour)
	if err != nil {
		t.Fatalf("missing root should not error: %v", err)
	}
	if n != 0 {
		t.Fatalf("removed=%d, want 0", n)
	}
}
