package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeBackend returns an httptest.Server that responds to the paths in
// `bodies` with the bodies given. Status codes in `status` override the
// default 200 per path.
func fakeBackend(t *testing.T, status map[string]int, bodies map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for p, body := range bodies {
		p, body := p, body
		mux.HandleFunc(p, func(w http.ResponseWriter, _ *http.Request) {
			code, ok := status[p]
			if !ok {
				code = 200
			}
			w.WriteHeader(code)
			fmt.Fprint(w, body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestScrape_HappyPath_Idle(t *testing.T) {
	srv := fakeBackend(t, nil, map[string]string{
		"/health":  `{"status":"ok"}`,
		"/metrics": sampleMetricsIdle,
		"/slots":   `[{"is_processing":false},{"is_processing":false}]`,
	})

	s := Scrape(context.Background(), srv.Client(), srv.URL, 18080)
	if s.State != "idle" {
		t.Errorf("State = %q, want idle", s.State)
	}
	if s.TokensPredictedTotal != 60 {
		t.Errorf("TokensPredictedTotal = %d, want 60", s.TokensPredictedTotal)
	}
	if s.Port != 18080 {
		t.Errorf("Port = %d, want 18080", s.Port)
	}
}

func TestScrape_Active(t *testing.T) {
	srv := fakeBackend(t, nil, map[string]string{
		"/health":  `{"status":"ok"}`,
		"/metrics": sampleMetricsActive,
		"/slots":   `[{"is_processing":true}]`,
	})

	s := Scrape(context.Background(), srv.Client(), srv.URL, 1)
	if s.State != "active" {
		t.Errorf("State = %q, want active", s.State)
	}
}

func TestScrape_Loading_HealthReturns503(t *testing.T) {
	srv := fakeBackend(t,
		map[string]int{"/health": 503},
		map[string]string{"/health": ""},
	)
	s := Scrape(context.Background(), srv.Client(), srv.URL, 1)
	if s.State != "loading" {
		t.Errorf("State = %q, want loading", s.State)
	}
}

func TestScrape_MetricsDisabled_501(t *testing.T) {
	srv := fakeBackend(t,
		map[string]int{"/metrics": 501},
		map[string]string{
			"/health":  `{"status":"ok"}`,
			"/metrics": `{"error":"not supported"}`,
			"/slots":   `[{"is_processing":false}]`,
		},
	)
	s := Scrape(context.Background(), srv.Client(), srv.URL, 1)
	if s.State != "metrics_disabled" {
		t.Errorf("State = %q, want metrics_disabled", s.State)
	}
}

func TestScrape_MetricsDisabledButSlotBusy(t *testing.T) {
	srv := fakeBackend(t,
		map[string]int{"/metrics": 501},
		map[string]string{
			"/health":  `{"status":"ok"}`,
			"/metrics": ``,
			"/slots":   `[{"is_processing":true}]`,
		},
	)
	s := Scrape(context.Background(), srv.Client(), srv.URL, 1)
	if s.State != "active" {
		t.Errorf("State = %q, want active (slots show busy)", s.State)
	}
}

func TestScrape_Unreachable(t *testing.T) {
	s := Scrape(context.Background(), http.DefaultClient, "http://127.0.0.1:1", 1)
	if s.State != "unreachable" {
		t.Errorf("State = %q, want unreachable", s.State)
	}
	if s.ScrapeError == "" {
		t.Error("expected ScrapeError to be populated")
	}
}

func TestScrape_RespectsContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte("too late"))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	s := Scrape(ctx, srv.Client(), srv.URL, 1)
	if s.State != "unreachable" {
		t.Errorf("State = %q, want unreachable on timeout", s.State)
	}
}

const sampleMetricsIdle = `llamacpp:tokens_predicted_total 60
llamacpp:tokens_predicted_seconds_total 0.255
llamacpp:requests_processing 0
`

const sampleMetricsActive = `llamacpp:tokens_predicted_total 200
llamacpp:tokens_predicted_seconds_total 1.0
llamacpp:requests_processing 1
`
