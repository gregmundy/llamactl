package telemetry

import (
	"sort"
	"time"

	"github.com/gregmundy/llamactl/internal/models"
)

// TelemetryResponse is the JSON payload of GET /v1/telemetry.
type TelemetryResponse struct {
	GeneratedAt string         `json:"generated_at"`
	Installed   []InstalledRow `json:"installed"`
	Running     []RunningRow   `json:"running"`
}

// InstalledRow is one entry of the installed[] array.
type InstalledRow struct {
	ID           string  `json:"id"`
	ParamsB      float64 `json:"params_b"`
	Quant        string  `json:"quant"`
	SizeBytes    int64   `json:"size_bytes"`
	AddedAt      string  `json:"added_at,omitempty"`
	LastServedAt *string `json:"last_served_at"`
}

// RunningRow is one entry of the running[] array.
type RunningRow struct {
	ID                   string   `json:"id"`
	Port                 int      `json:"port"`
	Recipe               string   `json:"recipe"`
	SizeBytes            int64    `json:"size_bytes"`
	MemoryBytes          int64    `json:"memory_bytes"`
	State                string   `json:"state"`
	TokensPerSecond      *float64 `json:"tokens_per_second"`
	TokensPredictedTotal uint64   `json:"tokens_predicted_total"`
	UptimeSeconds        int64    `json:"uptime_seconds"`
	Error                string   `json:"error,omitempty"`
}

// Aggregate builds the response from the current State and the
// installed-model list. now is the timestamp embedded in generated_at.
func Aggregate(state *State, installed []models.Metadata, now time.Time) TelemetryResponse {
	resp := TelemetryResponse{
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Installed:   make([]InstalledRow, 0, len(installed)),
		Running:     []RunningRow{},
	}

	sizeByID := make(map[string]int64, len(installed))
	for _, m := range installed {
		sizeByID[m.ID] = m.SizeBytes
		var lastServed *string
		if !m.LastServedAt.IsZero() {
			s := m.LastServedAt.UTC().Format(time.RFC3339)
			lastServed = &s
		}
		row := InstalledRow{
			ID:           m.ID,
			ParamsB:      m.ParamsB,
			Quant:        string(m.Quant),
			SizeBytes:    m.SizeBytes,
			LastServedAt: lastServed,
		}
		if !m.AddedAt.IsZero() {
			row.AddedAt = m.AddedAt.UTC().Format(time.RFC3339)
		}
		resp.Installed = append(resp.Installed, row)
	}
	sort.Slice(resp.Installed, func(i, j int) bool { return resp.Installed[i].ID < resp.Installed[j].ID })

	for _, id := range state.IDs() {
		sample, _ := state.Get(id)
		row := RunningRow{
			ID:                   id,
			Port:                 sample.Port,
			Recipe:               sample.Recipe,
			SizeBytes:            sizeByID[id],
			MemoryBytes:          sample.MemoryBytes,
			State:                sample.State,
			TokensPredictedTotal: sample.TokensPredictedTotal,
			UptimeSeconds:        sample.UptimeSeconds,
			Error:                sample.ScrapeError,
		}
		if rate, ok := state.TokensPerSecond(id); ok {
			r := rate
			row.TokensPerSecond = &r
		}
		resp.Running = append(resp.Running, row)
	}
	sort.Slice(resp.Running, func(i, j int) bool { return resp.Running[i].ID < resp.Running[j].ID })
	return resp
}
