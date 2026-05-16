package telemetry

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gregmundy/llamactl/internal/models"
)

// InstalledLister is the seam used by the server to fetch the
// installed-model list on each request. Production wires a function
// that reads from the model metadata directory.
type InstalledLister func() []models.Metadata

// NewHandler builds the http.Handler for telemetryd. apiKey enables
// Bearer auth when non-empty. nowFn is the time source (tests inject
// fixed times).
func NewHandler(state *State, installed InstalledLister, apiKey string, nowFn func() time.Time) http.Handler {
	if nowFn == nil {
		nowFn = time.Now
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.Handle("/v1/telemetry", authMiddleware(apiKey, telemetryHandler(state, installed, nowFn)))
	return methodGuard(mux)
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func telemetryHandler(state *State, installed InstalledLister, nowFn func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		var meta []models.Metadata
		if installed != nil {
			meta = installed()
		}
		resp := Aggregate(state, meta, nowFn())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func authMiddleware(apiKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("Authorization")
		want := "Bearer " + apiKey
		if got != want {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// methodGuard returns 405 for non-GET against our real routes.
func methodGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isOurs := r.URL.Path == "/health" || r.URL.Path == "/v1/telemetry"
		if isOurs && r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}
