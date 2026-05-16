package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/launchd"
)

func TestIntegration_PollerAndHandler(t *testing.T) {
	tokens := int64(60)
	micros := int64(255000) // microseconds; converted to seconds in handler

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/slots":
			fmt.Fprint(w, `[{"is_processing":false}]`)
		case "/metrics":
			fmt.Fprintf(w, "llamacpp:tokens_predicted_total %d\nllamacpp:tokens_predicted_seconds_total %.6f\nllamacpp:requests_processing 0\n",
				atomic.LoadInt64(&tokens), float64(atomic.LoadInt64(&micros))/1_000_000)
		}
	}))
	defer backend.Close()

	state := NewState()
	p := &Poller{
		State:      state,
		Lister:     &fakeLister{services: []launchd.RunningService{{ID: "fake", Port: 1, Args: []string{"--ctx-size", "8192", "--cache-type-k", "f16", "--cache-type-v", "f16"}}}},
		PlistDir:   t.TempDir(),
		HTTPClient: backend.Client(),
		BaseURLFn:  func(_ int) string { return backend.URL },
	}
	p.tickOnce(context.Background())

	// Simulate +240 tokens over 1 second of generation.
	atomic.AddInt64(&tokens, 240)
	atomic.AddInt64(&micros, 1_000_000)
	p.tickOnce(context.Background())

	h := NewHandler(state, nil, "", func() time.Time { return time.Now().UTC() })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/telemetry", nil))
	if rec.Code != 200 {
		t.Fatalf("Code = %d", rec.Code)
	}
	var resp TelemetryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Running) != 1 {
		t.Fatalf("Running len = %d", len(resp.Running))
	}
	r := resp.Running[0]
	if r.TokensPerSecond == nil {
		t.Fatal("expected non-nil TokensPerSecond after 2 ticks")
	}
	if got := *r.TokensPerSecond; got < 239.9 || got > 240.1 {
		t.Errorf("TokensPerSecond = %v, want ~240.0", got)
	}
	if r.Recipe != "chat" {
		t.Errorf("Recipe = %q, want chat", r.Recipe)
	}
}

func TestIntegration_MetricsDisabledMidRun(t *testing.T) {
	metricsEnabled := int64(1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/slots":
			fmt.Fprint(w, `[{"is_processing":false}]`)
		case "/metrics":
			if atomic.LoadInt64(&metricsEnabled) == 0 {
				w.WriteHeader(http.StatusNotImplemented)
				return
			}
			fmt.Fprint(w, sampleMetricsIdle)
		}
	}))
	defer backend.Close()

	state := NewState()
	p := &Poller{
		State:      state,
		Lister:     &fakeLister{services: []launchd.RunningService{{ID: "fake", Port: 1}}},
		PlistDir:   t.TempDir(),
		HTTPClient: backend.Client(),
		BaseURLFn:  func(_ int) string { return backend.URL },
	}
	p.tickOnce(context.Background())
	atomic.StoreInt64(&metricsEnabled, 0)
	p.tickOnce(context.Background())

	got, _ := state.Get("fake")
	if got.State != "metrics_disabled" {
		t.Errorf("State = %q, want metrics_disabled", got.State)
	}
}
