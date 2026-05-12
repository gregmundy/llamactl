package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseCaskVersion(t *testing.T) {
	raw := []byte(`# header
cask "llamactl" do
  version "1.3.0"

  on_macos do
    on_arm do
      sha256 "abc"
    end
  end
end`)
	got, err := parseCaskVersion(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.3.0" {
		t.Fatalf("got %q, want 1.3.0", got)
	}
}

func TestParseCaskVersionMissing(t *testing.T) {
	if _, err := parseCaskVersion([]byte("no version here")); err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchLatestVersionWritesCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `cask "llamactl" do`)
		fmt.Fprintln(w, `  version "1.3.0"`)
		fmt.Fprintln(w, `end`)
	}))
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "v.json")
	got, err := fetchLatestVersion(context.Background(), server.URL, cachePath, false, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.3.0" {
		t.Fatalf("got %q, want 1.3.0", got)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var cache versionCache
	json.Unmarshal(data, &cache)
	if cache.Latest != "1.3.0" {
		t.Fatalf("cache.Latest=%q, want 1.3.0", cache.Latest)
	}
}

func TestFetchLatestVersionUsesFreshCache(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "v.json")
	fresh := versionCache{Latest: "9.9.9", CheckedAt: time.Now().Add(-1 * time.Hour)}
	raw, _ := json.Marshal(fresh)
	os.WriteFile(cachePath, raw, 0o644)
	got, err := fetchLatestVersion(context.Background(), "http://unreachable.invalid/", cachePath, false, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if got != "9.9.9" {
		t.Fatalf("got %q, want 9.9.9 (from cache)", got)
	}
}

func TestFetchLatestVersionRefreshesStaleCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `version "2.0.0"`)
	}))
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "v.json")
	stale := versionCache{Latest: "1.0.0", CheckedAt: time.Now().Add(-48 * time.Hour)}
	raw, _ := json.Marshal(stale)
	os.WriteFile(cachePath, raw, 0o644)
	got, err := fetchLatestVersion(context.Background(), server.URL, cachePath, false, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2.0.0" {
		t.Fatalf("got %q; cache stale should have refreshed to 2.0.0", got)
	}
}

func TestFetchLatestVersionRefreshFlagBypassesCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `version "2.0.0"`)
	}))
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "v.json")
	fresh := versionCache{Latest: "1.0.0", CheckedAt: time.Now().Add(-1 * time.Hour)}
	raw, _ := json.Marshal(fresh)
	os.WriteFile(cachePath, raw, 0o644)
	got, err := fetchLatestVersion(context.Background(), server.URL, cachePath, true, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2.0.0" {
		t.Fatalf("refresh=true should bypass cache; got %q", got)
	}
}

func TestUpdateAvailable(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"1.2.0", "1.3.0", true},
		{"1.3.0", "1.3.0", false},
		{"1.3.0", "1.2.0", false},
		{"v1.3.0", "1.3.0", false},
		{"1.2.9", "1.3.0", true},
		{"1.9.0", "1.10.0", true}, // numeric comparison: 10 > 9 (latest newer than current)
	}
	for _, c := range cases {
		got := updateAvailable(c.current, c.latest)
		if got != c.want {
			t.Errorf("updateAvailable(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}
