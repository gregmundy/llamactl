package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CaskURL is the canonical location of the published llamactl homebrew cask.
const CaskURL = "https://raw.githubusercontent.com/gregmundy/homebrew-tap/main/Casks/llamactl.rb"

const versionCacheTTL = 24 * time.Hour

type versionCache struct {
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checked_at"`
}

var caskVersionRe = regexp.MustCompile(`(?m)^\s*version\s+"([^"]+)"`)

// parseCaskVersion extracts the version string from a Homebrew cask file body.
func parseCaskVersion(data []byte) (string, error) {
	m := caskVersionRe.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("no version directive in cask")
	}
	return string(m[1]), nil
}

// fetchLatestVersion returns the latest published version of llamactl by
// reading the homebrew cask at url. Results are cached at cachePath for 24h.
// Pass refresh=true to bypass the cache and force a network fetch.
// now is injectable for testing.
func fetchLatestVersion(ctx context.Context, url, cachePath string, refresh bool, now func() time.Time) (string, error) {
	if !refresh {
		if cached, err := readVersionCache(cachePath); err == nil && now().Sub(cached.CheckedAt) < versionCacheTTL {
			return cached.Latest, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	version, err := parseCaskVersion(body)
	if err != nil {
		return "", err
	}
	_ = writeVersionCache(cachePath, versionCache{Latest: version, CheckedAt: now()})
	return version, nil
}

func readVersionCache(path string) (versionCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return versionCache{}, err
	}
	var c versionCache
	if err := json.Unmarshal(data, &c); err != nil {
		return versionCache{}, err
	}
	return c, nil
}

func writeVersionCache(path string, c versionCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// versionNewer reports whether b is strictly newer than a in semver-ish
// ordering (i.e., "is there an update?" when a=current, b=latest).
// Strips leading 'v' on either input. Returns false on parse failure.
func versionNewer(a, b string) bool {
	aa := parseSemver(a)
	bb := parseSemver(b)
	for i := 0; i < 3; i++ {
		if bb[i] > aa[i] {
			return true
		}
		if bb[i] < aa[i] {
			return false
		}
	}
	return false
}

func parseSemver(s string) [3]int {
	s = strings.TrimPrefix(s, "v")
	var out [3]int
	parts := strings.SplitN(s, ".", 3)
	for i := 0; i < 3 && i < len(parts); i++ {
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out
}
