package telemetry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Scrape fetches /health, /metrics, /slots from one backend at baseURL
// and returns a Sample. baseURL must not include a trailing slash.
// The caller owns the *http.Client (used for testing with httptest).
//
// apiKey, when non-empty, is sent as `Authorization: Bearer <apiKey>`
// on every request. Required when llamactl is configured with `api_key`
// — `serve --detach` then passes `--api-key` to each llama-server,
// which gates /health, /slots, and /metrics on Bearer auth. The
// telemetry daemon uses the same key as both consumer-of-self and
// consumer-of-backends.
//
// State derivation rules (see spec §5.4):
//   - any error before metrics → "unreachable"
//   - health 503 → "loading"
//   - metrics 501 → "metrics_disabled" (idle/active still derivable from /slots)
//   - requests_processing > 0 OR any slot is_processing → "active"
//   - otherwise → "idle"
func Scrape(ctx context.Context, client *http.Client, baseURL string, port int, apiKey string) Sample {
	sample := Sample{ScrapedAt: time.Now(), Port: port}

	healthCode, _, err := fetch(ctx, client, baseURL+"/health", apiKey)
	if err != nil {
		sample.State = "unreachable"
		sample.ScrapeError = err.Error()
		return sample
	}
	if healthCode == http.StatusServiceUnavailable {
		sample.State = "loading"
		return sample
	}

	slotsState := SlotsState{}
	if _, body, err := fetch(ctx, client, baseURL+"/slots", apiKey); err == nil {
		slotsState, _ = ParseSlots(body)
	}

	metricsCode, body, err := fetch(ctx, client, baseURL+"/metrics", apiKey)
	switch {
	case err != nil:
		sample.State = "unreachable"
		sample.ScrapeError = err.Error()
		return sample
	case metricsCode == http.StatusNotImplemented:
		sample.State = "metrics_disabled"
		if slotsState.BusySlots > 0 {
			sample.State = "active"
		}
		return sample
	case metricsCode != http.StatusOK:
		sample.State = "unreachable"
		sample.ScrapeError = fmt.Sprintf("/metrics returned %d", metricsCode)
		return sample
	}

	mv, _ := ParseMetrics(string(body))
	sample.TokensPredictedTotal = mv.TokensPredictedTotal
	sample.TokensPredictedSeconds = mv.TokensPredictedSeconds

	if mv.RequestsProcessing > 0 || slotsState.BusySlots > 0 {
		sample.State = "active"
	} else {
		sample.State = "idle"
	}
	return sample
}

// fetch propagates context, reads the full body, returns (status, body, err).
// When apiKey is non-empty an Authorization: Bearer header is added.
func fetch(ctx context.Context, client *http.Client, url, apiKey string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}
