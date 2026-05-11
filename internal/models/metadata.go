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
}
