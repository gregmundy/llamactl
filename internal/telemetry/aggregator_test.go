package telemetry

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/models"
)

func TestAggregate_EmptyState(t *testing.T) {
	now := time.Date(2026, 5, 16, 14, 23, 11, 0, time.UTC)
	agg := Aggregate(NewState(), nil, now)
	if agg.GeneratedAt != "2026-05-16T14:23:11Z" {
		t.Errorf("GeneratedAt = %q", agg.GeneratedAt)
	}
	if len(agg.Installed) != 0 {
		t.Errorf("Installed len = %d, want 0", len(agg.Installed))
	}
	if len(agg.Running) != 0 {
		t.Errorf("Running len = %d, want 0", len(agg.Running))
	}
}

func TestAggregate_IncludesInstalledAndRunning(t *testing.T) {
	installed := []models.Metadata{{
		ID: "qwen2.5-3b-instruct", ParamsB: 3.0, Quant: "Q5_K_M",
		SizeBytes: 2469606195,
		GGUFPath:  "/tmp/Q5_K_M.gguf",
	}}
	state := NewState()
	state.Update("qwen2.5-3b-instruct", Sample{
		State: "idle", Port: 8082, Recipe: "chat",
		MemoryBytes: 695894016, UptimeSeconds: 3600,
		TokensPredictedTotal:   1280,
		TokensPredictedSeconds: 5.0,
	})
	state.Update("qwen2.5-3b-instruct", Sample{
		State: "idle", Port: 8082, Recipe: "chat",
		MemoryBytes: 695894016, UptimeSeconds: 3605,
		TokensPredictedTotal:   1280,
		TokensPredictedSeconds: 5.0,
	})

	agg := Aggregate(state, installed, time.Now().UTC())
	if len(agg.Installed) != 1 || agg.Installed[0].ID != "qwen2.5-3b-instruct" {
		t.Errorf("Installed = %+v", agg.Installed)
	}
	if len(agg.Running) != 1 {
		t.Fatalf("Running len = %d", len(agg.Running))
	}
	r := agg.Running[0]
	if r.SizeBytes != 2469606195 {
		t.Errorf("SizeBytes = %d", r.SizeBytes)
	}
	if r.TokensPerSecond == nil || *r.TokensPerSecond != 0.0 {
		t.Errorf("expected tok/s == 0.0 on zero delta, got %v", r.TokensPerSecond)
	}
}

func TestAggregate_NullsTokensForUnreachable(t *testing.T) {
	state := NewState()
	state.Update("x", Sample{State: "unreachable", ScrapeError: "connect: refused"})
	agg := Aggregate(state, nil, time.Now().UTC())
	if len(agg.Running) != 1 {
		t.Fatalf("Running len = %d", len(agg.Running))
	}
	if agg.Running[0].TokensPerSecond != nil {
		t.Errorf("expected nil tok/s for unreachable, got %v", agg.Running[0].TokensPerSecond)
	}
	if agg.Running[0].Error != "connect: refused" {
		t.Errorf("Error = %q", agg.Running[0].Error)
	}
}

func TestAggregate_JSONShape(t *testing.T) {
	agg := Aggregate(NewState(), nil, time.Date(2026, 5, 16, 14, 0, 0, 0, time.UTC))
	b, err := json.Marshal(agg)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"generated_at":"2026-05-16T14:00:00Z","installed":[],"running":[]}`
	if string(b) != want {
		t.Errorf("got %s", string(b))
	}
}
