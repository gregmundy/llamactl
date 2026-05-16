package telemetry

import (
	"testing"
	"time"
)

func TestState_TokensPerSecond_NoPrior(t *testing.T) {
	s := NewState()
	s.Update("a", Sample{State: "idle", TokensPredictedTotal: 100, TokensPredictedSeconds: 1.0})
	if rate, ok := s.TokensPerSecond("a"); ok || rate != 0 {
		t.Errorf("first sample: rate=%v ok=%v, want 0,false", rate, ok)
	}
}

func TestState_TokensPerSecond_Delta(t *testing.T) {
	s := NewState()
	s.Update("a", Sample{State: "idle", TokensPredictedTotal: 100, TokensPredictedSeconds: 1.0})
	s.Update("a", Sample{State: "active", TokensPredictedTotal: 300, TokensPredictedSeconds: 2.0})
	rate, ok := s.TokensPerSecond("a")
	if !ok {
		t.Fatal("expected ok=true after second update")
	}
	if rate != 200.0 {
		t.Errorf("rate = %v, want 200.0", rate)
	}
}

func TestState_TokensPerSecond_ZeroDelta(t *testing.T) {
	s := NewState()
	s.Update("a", Sample{State: "idle", TokensPredictedTotal: 100, TokensPredictedSeconds: 1.0})
	s.Update("a", Sample{State: "idle", TokensPredictedTotal: 100, TokensPredictedSeconds: 1.0})
	rate, ok := s.TokensPerSecond("a")
	if !ok || rate != 0 {
		t.Errorf("zero delta: rate=%v ok=%v, want 0,true", rate, ok)
	}
}

func TestState_TokensPerSecond_NonNumericState(t *testing.T) {
	for _, st := range []string{"loading", "unreachable", "metrics_disabled"} {
		s := NewState()
		s.Update("a", Sample{State: "idle", TokensPredictedTotal: 100, TokensPredictedSeconds: 1.0})
		s.Update("a", Sample{State: st, TokensPredictedTotal: 100, TokensPredictedSeconds: 1.0})
		if _, ok := s.TokensPerSecond("a"); ok {
			t.Errorf("state=%q must yield ok=false", st)
		}
	}
}

func TestState_Forget(t *testing.T) {
	s := NewState()
	s.Update("a", Sample{State: "idle", ScrapedAt: time.Now()})
	s.Update("a", Sample{State: "idle", ScrapedAt: time.Now()})
	s.Forget("a")
	if _, ok := s.Get("a"); ok {
		t.Error("Get after Forget should return false")
	}
	if _, ok := s.TokensPerSecond("a"); ok {
		t.Error("TokensPerSecond after Forget should return false")
	}
}

func TestState_IDs(t *testing.T) {
	s := NewState()
	s.Update("a", Sample{State: "idle"})
	s.Update("b", Sample{State: "idle"})
	ids := s.IDs()
	if len(ids) != 2 {
		t.Errorf("IDs len = %d, want 2", len(ids))
	}
}
