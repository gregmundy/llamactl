package hf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	SearchTTL = 24 * time.Hour
	RepoTTL   = 7 * 24 * time.Hour
)

// ErrRangeNotSupported is returned by FetchRange when the offset is non-zero
// but the server responds with 200 (full body) instead of 206 (partial).
// The caller (Downloader) handles this by restarting from offset 0.
var ErrRangeNotSupported = errors.New("server did not honor Range header")

// Client talks to the HuggingFace Hub. Search and RepoInfo are cached;
// FetchRange streams bytes.
type Client struct {
	baseURL    string
	cache      *Cache
	httpClient *http.Client
	token      string // from HF_TOKEN / LLAMACTL_HF_TOKEN; empty if not set
	retrySleep func(time.Duration)
}

// NewClient returns a Client. If httpClient is nil, http.DefaultClient is used.
func NewClient(baseURL string, cache *Cache, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:    baseURL,
		cache:      cache,
		httpClient: httpClient,
		retrySleep: time.Sleep,
	}
}

// WithToken sets the bearer token used on every request.
func (c *Client) WithToken(t string) *Client { c.token = t; return c }

// Search returns all HF GGUF repo hits matching the query (caller
// annotates which ones are in models.PreferredIDs). Cached with SearchTTL.
func (c *Client) Search(ctx context.Context, query string) ([]SearchHit, error) {
	return c.search(ctx, query, false)
}

// SearchRefresh bypasses the cache (used for --refresh).
func (c *Client) SearchRefresh(ctx context.Context, query string) ([]SearchHit, error) {
	return c.search(ctx, query, true)
}

func (c *Client) search(ctx context.Context, query string, refresh bool) ([]SearchHit, error) {
	if !refresh {
		if data, fresh, err := c.cache.Get("hf-search", query, SearchTTL); err == nil && fresh && data != nil {
			var hits []SearchHit
			if jerr := json.Unmarshal(data, &hits); jerr == nil {
				return hits, nil
			}
			// fall through on decode failure
		}
	}
	endpoint := c.baseURL + "/api/models?search=" + url.QueryEscape(query)
	data, err := c.doJSON(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var hits []SearchHit
	if err := json.Unmarshal(data, &hits); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	_ = c.cache.Put("hf-search", query, data)
	return hits, nil
}

// RepoInfo fetches /api/models/<repoID>. Cached with RepoTTL.
func (c *Client) RepoInfo(ctx context.Context, repoID string) (Repo, error) {
	if data, fresh, err := c.cache.Get("hf-repo-v2", repoID, RepoTTL); err == nil && fresh && data != nil {
		var r Repo
		if jerr := json.Unmarshal(data, &r); jerr == nil {
			return r, nil
		}
	}
	// blobs=true is required for HF to include the `lfs` block on each
	// sibling. Without it, lfs.sha256 is absent for most community repos
	// (only certain official repos happen to expose it by default), and
	// our dedupe + verify path can't function.
	endpoint := c.baseURL + "/api/models/" + repoID + "?blobs=true"
	data, err := c.doJSON(ctx, endpoint)
	if err != nil {
		return Repo{}, err
	}
	var r Repo
	if err := json.Unmarshal(data, &r); err != nil {
		return Repo{}, fmt.Errorf("decode repo response: %w", err)
	}
	_ = c.cache.Put("hf-repo-v2", repoID, data)
	return r, nil
}

// FetchRange streams [offset, end) of repoID/file into w. end == 0 means EOF.
// If offset > 0 and the server returns 200 (instead of 206), returns
// ErrRangeNotSupported without writing anything to w.
func (c *Client) FetchRange(ctx context.Context, repoID, file string, offset, end int64, w io.Writer) error {
	endpoint := c.baseURL + "/" + repoID + "/resolve/main/" + file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if offset > 0 || end > 0 {
		if end > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, end-1))
		} else {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if offset > 0 && resp.StatusCode == http.StatusOK {
		return ErrRangeNotSupported
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("fetch %s: status %d", endpoint, resp.StatusCode)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("fetch body %s: %w", endpoint, err)
	}
	return nil
}

// doJSON does a GET with up to 3 attempts on 5xx + transport errors.
func (c *Client) doJSON(ctx context.Context, endpoint string) ([]byte, error) {
	delays := []time.Duration{0, time.Second, 2 * time.Second}
	var lastErr error
	for _, d := range delays {
		if d > 0 {
			c.retrySleep(d)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("GET %s: %w", endpoint, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch {
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("GET %s: status %d", endpoint, resp.StatusCode)
			continue
		case resp.StatusCode >= 400:
			return nil, fmt.Errorf("GET %s: status %d: %s", endpoint, resp.StatusCode, string(body))
		}
		return body, nil
	}
	return nil, lastErr
}
