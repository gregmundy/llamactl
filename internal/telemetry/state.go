// Package telemetry implements the llamactl-telemetryd sidecar: it
// scrapes each running llama-server backend on a timer, caches the
// results, and serves an aggregated JSON snapshot over HTTP.
package telemetry

import (
	"math"
	"sync"
	"time"
)

// Sample is one poll-cycle result for a single backend.
type Sample struct {
	ScrapedAt              time.Time
	State                  string // idle | active | loading | metrics_disabled | unreachable
	TokensPredictedTotal   uint64
	TokensPredictedSeconds float64
	MemoryBytes            int64
	UptimeSeconds          int64
	Port                   int
	Recipe                 string
	ScrapeError            string // empty when state != "unreachable"
}

// State is the in-memory cache shared between poller and aggregator.
// Reads and writes are mutex-guarded. Holds at most two Samples per
// modelID — the latest and its immediate predecessor — so tokens/sec
// can be computed as a delta.
type State struct {
	mu      sync.Mutex
	samples map[string]Sample
	prev    map[string]Sample
}

func NewState() *State {
	return &State{
		samples: make(map[string]Sample),
		prev:    make(map[string]Sample),
	}
}

// Update sets the latest Sample for modelID; the previous latest is
// preserved in `prev` for delta math.
func (s *State) Update(modelID string, sample Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if curr, ok := s.samples[modelID]; ok {
		s.prev[modelID] = curr
	}
	s.samples[modelID] = sample
}

// Get returns the current Sample for modelID.
func (s *State) Get(modelID string) (Sample, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sample, ok := s.samples[modelID]
	return sample, ok
}

// TokensPerSecond returns the rolling rate between the prior and most
// recent Sample for modelID. ok=false when no prior exists or state is
// non-numeric (loading/unreachable/metrics_disabled). ok=true with
// rate=0 means "we have two samples but generation didn't progress."
func (s *State) TokensPerSecond(modelID string) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	curr, ok := s.samples[modelID]
	if !ok {
		return 0, false
	}
	switch curr.State {
	case "loading", "unreachable", "metrics_disabled":
		return 0, false
	}
	prev, ok := s.prev[modelID]
	if !ok {
		return 0, false
	}
	dt := curr.TokensPredictedSeconds - prev.TokensPredictedSeconds
	if dt <= 0 {
		return 0, true
	}
	dtok := float64(curr.TokensPredictedTotal - prev.TokensPredictedTotal)
	rate := dtok / dt
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, false
	}
	return rate, true
}

// Forget removes modelID from both maps. Called by the poller when a
// model that was running disappears from the plist directory.
func (s *State) Forget(modelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.samples, modelID)
	delete(s.prev, modelID)
}

// IDs returns the currently-tracked model IDs in arbitrary order.
func (s *State) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.samples))
	for id := range s.samples {
		out = append(out, id)
	}
	return out
}
