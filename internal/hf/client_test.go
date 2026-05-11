package hf

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientSearchAndCache(t *testing.T) {
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !strings.HasPrefix(r.URL.Path, "/api/models") {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"Qwen/Qwen2.5-7B-Instruct-GGUF","downloads":1,"likes":1}]`))
	}))
	t.Cleanup(ts.Close)

	c := NewClient(ts.URL, NewCache(t.TempDir()), nil)
	got, err := c.Search(context.Background(), "qwen2.5")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].ID != "Qwen/Qwen2.5-7B-Instruct-GGUF" {
		t.Errorf("got %+v", got)
	}

	// Second call should hit cache.
	if _, err := c.Search(context.Background(), "qwen2.5"); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("server hits = %d, want 1 (second call should be cached)", hits)
	}
}

func TestClientSearchRefreshBypassesCache(t *testing.T) {
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(`[]`))
	}))
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL, NewCache(t.TempDir()), nil)
	if _, err := c.Search(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SearchRefresh(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2", hits)
	}
}

func TestClientRepoInfo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models/Qwen/Qwen2.5-7B-Instruct-GGUF" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(`{
			"id":"Qwen/Qwen2.5-7B-Instruct-GGUF",
			"siblings":[
				{"rfilename":"qwen2.5-7b-instruct-q4_k_m.gguf","lfs":{"sha256":"deadbeef","size":4500000000}}
			]
		}`))
	}))
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL, NewCache(t.TempDir()), nil)
	repo, err := c.RepoInfo(context.Background(), "Qwen/Qwen2.5-7B-Instruct-GGUF")
	if err != nil {
		t.Fatalf("RepoInfo: %v", err)
	}
	if len(repo.Siblings) != 1 || repo.Siblings[0].LFS == nil || repo.Siblings[0].LFS.SHA256 != "deadbeef" {
		t.Errorf("repo = %+v", repo)
	}
}

func TestClientFetchRangeHonorsRangeHeader(t *testing.T) {
	body := []byte("0123456789abcdef")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rng := r.Header.Get("Range")
		if rng != "bytes=4-" {
			t.Errorf("Range = %q", rng)
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write(body[4:])
	}))
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL, NewCache(t.TempDir()), nil)
	var buf bytes.Buffer
	if err := c.FetchRange(context.Background(), "Qwen/Repo", "file.gguf", 4, 0, &buf); err != nil {
		t.Fatalf("FetchRange: %v", err)
	}
	if buf.String() != "456789abcdef" {
		t.Errorf("got %q", buf.String())
	}
}

func TestClientFetchRangeServerIgnoresRangeReturns200(t *testing.T) {
	body := []byte("0123456789abcdef")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL, NewCache(t.TempDir()), nil)
	var buf bytes.Buffer
	err := c.FetchRange(context.Background(), "r", "f", 4, 0, &buf)
	if err == nil {
		t.Fatal("expected ErrRangeNotSupported")
	}
	// Caller (Downloader) detects this and restarts from offset 0.
}

func TestClientRetries5xxAndFails(t *testing.T) {
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(503)
	}))
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL, NewCache(t.TempDir()), nil)
	c.retrySleep = func(time.Duration) {} // no real delays
	_, err := c.Search(context.Background(), "q")
	if err == nil {
		t.Fatal("expected error")
	}
	if hits != 3 {
		t.Errorf("hits = %d, want 3", hits)
	}
}
