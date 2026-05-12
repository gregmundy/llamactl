package models

import (
	"fmt"
	"sort"
	"strings"
)

// Model is a preferred-ID model entry. The PreferredIDs map's key matches Model.ID.
type Model struct {
	ID      string // canonical llamactl id, e.g. "qwen2.5-7b-instruct"
	HFRepo  string // HuggingFace repo, e.g. "Qwen/Qwen2.5-7B-Instruct-GGUF"
	Arch    Arch
	ParamsB float64 // parameter count in billions, must have a row in QuantSizeTable
	MaxCtx  int     // maximum context tokens supported by the model family
}

// PreferredIDs is the curated table of short-ids with pre-tuned ParamsB/Arch/MaxCtx.
// Adding entries is a code change; `add` is not gated by this table — any HF GGUF repo
// path is accepted with explicit --quant (see PRD §4).
var PreferredIDs = map[string]Model{
	"qwen2.5-3b-instruct":  {ID: "qwen2.5-3b-instruct", HFRepo: "Qwen/Qwen2.5-3B-Instruct-GGUF", Arch: ArchQwen25, ParamsB: 3.0, MaxCtx: 32768},
	"qwen2.5-7b-instruct":  {ID: "qwen2.5-7b-instruct", HFRepo: "Qwen/Qwen2.5-7B-Instruct-GGUF", Arch: ArchQwen25, ParamsB: 7.0, MaxCtx: 32768},
	"qwen2.5-14b-instruct": {ID: "qwen2.5-14b-instruct", HFRepo: "Qwen/Qwen2.5-14B-Instruct-GGUF", Arch: ArchQwen25, ParamsB: 14.0, MaxCtx: 32768},
	"qwen2.5-coder-3b":     {ID: "qwen2.5-coder-3b", HFRepo: "Qwen/Qwen2.5-Coder-3B-Instruct-GGUF", Arch: ArchQwen25, ParamsB: 3.0, MaxCtx: 32768},
	"qwen2.5-coder-7b":     {ID: "qwen2.5-coder-7b", HFRepo: "Qwen/Qwen2.5-Coder-7B-Instruct-GGUF", Arch: ArchQwen25, ParamsB: 7.0, MaxCtx: 32768},
	"qwen2.5-coder-14b":    {ID: "qwen2.5-coder-14b", HFRepo: "Qwen/Qwen2.5-Coder-14B-Instruct-GGUF", Arch: ArchQwen25, ParamsB: 14.0, MaxCtx: 32768},
	"llama3.1-8b":          {ID: "llama3.1-8b", HFRepo: "bartowski/Meta-Llama-3.1-8B-Instruct-GGUF", Arch: ArchLlama3, ParamsB: 8.0, MaxCtx: 131072},
	"llama3.2-3b":          {ID: "llama3.2-3b", HFRepo: "bartowski/Llama-3.2-3B-Instruct-GGUF", Arch: ArchLlama3, ParamsB: 3.0, MaxCtx: 131072},
	"llama3.3-70b":         {ID: "llama3.3-70b", HFRepo: "bartowski/Llama-3.3-70B-Instruct-GGUF", Arch: ArchLlama3, ParamsB: 70.0, MaxCtx: 131072},
	"mistral-7b-v0.3":      {ID: "mistral-7b-v0.3", HFRepo: "bartowski/Mistral-7B-Instruct-v0.3-GGUF", Arch: ArchMistral, ParamsB: 7.0, MaxCtx: 32768},
}

// LookupOrSuggest returns the PreferredIDs entry for id, or an error listing
// available ids if it isn't found. Error message is suitable for
// printing to the user verbatim (no further formatting needed by callers).
func LookupOrSuggest(id string) (Model, error) {
	if m, ok := PreferredIDs[id]; ok {
		return m, nil
	}
	ids := make([]string, 0, len(PreferredIDs))
	for k := range PreferredIDs {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	return Model{}, fmt.Errorf("unknown model %q; available: %s", id, strings.Join(ids, ", "))
}
