// Package hf is a thin client for the HuggingFace Hub HTTP API plus a
// filesystem-backed cache. It owns net/http for this project — no command
// in internal/cli should import net/http directly.
package hf

// SearchHit is a HuggingFace /api/models?search=... entry.
type SearchHit struct {
	ID           string `json:"id"`        // e.g. "Qwen/Qwen2.5-7B-Instruct-GGUF"
	Downloads    int    `json:"downloads"`
	Likes        int    `json:"likes"`
	LastModified string `json:"lastModified"`
}

// LFSInfo is the per-file LFS metadata exposed by HF's /api/models/<repo>.
type LFSInfo struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// File is a sibling entry in an HF repo listing.
type File struct {
	RFilename string   `json:"rfilename"`
	LFS       *LFSInfo `json:"lfs,omitempty"`
}

// Repo is a HuggingFace /api/models/<repo> response (subset we care about).
type Repo struct {
	ID       string `json:"id"`
	Siblings []File `json:"siblings"`
}
