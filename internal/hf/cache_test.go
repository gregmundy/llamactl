package hf

import (
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
