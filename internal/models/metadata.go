package models

import "time"

// Metadata is the per-tool JSON record written to
// ~/.config/llamactl/models/<id>.json after `add` succeeds.
type Metadata struct {
	ID        string    `json:"id"`
	Repo      string    `json:"repo"`
	Quant     Quant     `json:"quant"`
	SHA256    string    `json:"sha256"`
	GGUFPath  string    `json:"gguf_path"`
	SizeBytes int64     `json:"size_bytes"`
	AddedAt   time.Time `json:"added_at"`

	// Phase 2.5 additions; omitempty so legacy files decode cleanly
	// (legacy entries lack these — selector will not run on them).
	ParamsB int  `json:"params_b,omitempty"`
	Arch    Arch `json:"arch,omitempty"`

	// Phase 3 addition. Updated by `serve` (foreground or detached)
	// immediately before launching llama-server.
	LastServedAt time.Time `json:"last_served_at,omitempty"`
}
