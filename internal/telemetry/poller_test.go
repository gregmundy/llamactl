package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gregmundy/llamactl/internal/launchd"
)

// fakeLister returns a hardcoded set of running services.
type fakeLister struct {
	services []launchd.RunningService
}

func (f *fakeLister) ListRunningServices(_ string) ([]launchd.RunningService, error) {
	return f.services, nil
}

func TestPoller_RunsOnceAndUpdatesState(t *testing.T) {
	calls := int64(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/metrics":
			fmt.Fprint(w, sampleMetricsIdle)
		case "/slots":
			fmt.Fprint(w, `[{"is_processing":false}]`)
		}
	}))
	defer srv.Close()

	state := NewState()
	p := &Poller{
		State:      state,
		Lister:     &fakeLister{services: []launchd.RunningService{{ID: "fake", Port: 12345}}},
		PlistDir:   t.TempDir(),
		HTTPClient: srv.Client(),
		BaseURLFn:  func(_ int) string { return srv.URL },
	}
	p.tickOnce(context.Background())

	if _, ok := state.Get("fake"); !ok {
		t.Fatal("expected state to have entry for fake")
	}
	if atomic.LoadInt64(&calls) == 0 {
		t.Fatal("expected HTTP calls to fake backend")
	}
}

func TestPoller_ForgetsServicesThatDisappear(t *testing.T) {
	state := NewState()
	state.Update("gone", Sample{State: "idle"})
	state.Update("gone", Sample{State: "idle"}) // two samples so it's "really" there
	p := &Poller{
		State:    state,
		Lister:   &fakeLister{services: nil},
		PlistDir: t.TempDir(),
	}
	p.tickOnce(context.Background())
	if _, ok := state.Get("gone"); ok {
		t.Error("expected state to forget id 'gone'")
	}
}

func TestPoller_PopulatesRecipeFromArgs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/metrics":
			fmt.Fprint(w, sampleMetricsIdle)
		case "/slots":
			fmt.Fprint(w, `[{"is_processing":false}]`)
		}
	}))
	defer srv.Close()

	state := NewState()
	p := &Poller{
		State: state,
		Lister: &fakeLister{services: []launchd.RunningService{{
			ID: "model", Port: 12345,
			// chat recipe shape
			Args: []string{
				"--ctx-size", "8192",
				"--cache-type-k", "f16",
				"--cache-type-v", "f16",
			},
		}}},
		PlistDir:   t.TempDir(),
		HTTPClient: srv.Client(),
		BaseURLFn:  func(_ int) string { return srv.URL },
	}
	p.tickOnce(context.Background())
	got, _ := state.Get("model")
	if got.Recipe != "chat" {
		t.Errorf("Recipe = %q, want chat", got.Recipe)
	}
}
