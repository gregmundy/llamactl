package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHFHTTPClientResponseHeaderTimeout verifies hfHTTPClient's transport-
// level ResponseHeaderTimeout actually kicks in when an upstream stalls
// before sending response headers. Without this protection, a single
// hung HF API response could stall `fit` indefinitely (the v1.4.2 bug
// reported via `llamactl fit gemma 4 31b`).
//
// The test spins up an httptest.Server that holds the request open for
// 5 seconds before responding, then asserts the client fails fast.
func TestHFHTTPClientResponseHeaderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hold the request open longer than ResponseHeaderTimeout so the
		// client must give up. We don't call WriteHeader, so no response
		// headers are sent until the sleep completes.
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	client := hfHTTPClient()
	// Override the production 30s timeout with something fast enough for
	// CI. The test cares that the timeout fires, not the exact wall time.
	client.Transport.(*http.Transport).ResponseHeaderTimeout = 200 * time.Millisecond

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	if err == nil {
		resp.Body.Close()
		t.Fatalf("expected timeout error; got success after %s", elapsed)
	}
	// Sanity: the timeout fired before the server's 5s sleep would have
	// returned. Allow generous slack for test-runner stalls.
	if elapsed > 2*time.Second {
		t.Errorf("ResponseHeaderTimeout did not fire fast enough: %s", elapsed)
	}
	t.Logf("client errored after %s with: %v", elapsed, err)
}

// TestHFHTTPClientNoBodyReadTimeout verifies hfHTTPClient does NOT have a
// total client-level Timeout set. The Timeout field caps the full request
// duration including body read, which would kill long-running GGUF
// downloads. Downloads must rely on ResponseHeaderTimeout + context
// cancellation, not a global cap.
func TestHFHTTPClientNoBodyReadTimeout(t *testing.T) {
	client := hfHTTPClient()
	if client.Timeout != 0 {
		t.Errorf("hfHTTPClient.Timeout = %s, want 0 (downloads must not be capped)", client.Timeout)
	}
}
