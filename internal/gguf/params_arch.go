package gguf

// llamaParams estimates total parameters for Llama-family models given:
//
//	hidden:  token_embd.weight dimension 0 (hidden_dim)
//	vocab:   token_embd.weight dimension 1 (vocab_size)
//	blocks:  <arch>.block_count
//
// Calibrated against Llama-3-8B (hidden=4096, vocab=128256, blocks=32 →
// reference 8.03 B) and Llama-3-70B (hidden=8192, vocab=128256, blocks=80 →
// reference 70.6 B). Accuracy ~10-15% across the family; overestimates large
// models slightly due to the constant per-block coefficient.
func llamaParams(hidden, vocab, blocks int) float64 {
	if hidden <= 0 || vocab <= 0 || blocks <= 0 {
		return 0
	}
	embedding := float64(vocab) * float64(hidden)
	perBlock := 14.5 * float64(hidden) * float64(hidden)
	output := float64(vocab) * float64(hidden) // untied; ~+10% when models use tied embeddings
	total := embedding + perBlock*float64(blocks) + output
	return total / 1e9
}

// qwen2Params estimates total parameters for Qwen2.x-family models.
// Calibrated against Qwen2.5-7B (3584/152064/28 → 7.62 B reference) and
// Qwen2.5-32B (5120/152064/64 → 32.76 B reference). The 18.1 per-block
// coefficient reflects Qwen2.5's larger MLP-to-hidden ratio vs Llama.
func qwen2Params(hidden, vocab, blocks int) float64 {
	if hidden <= 0 || vocab <= 0 || blocks <= 0 {
		return 0
	}
	embedding := float64(vocab) * float64(hidden)
	perBlock := 18.1 * float64(hidden) * float64(hidden)
	output := float64(vocab) * float64(hidden)
	return (embedding + perBlock*float64(blocks) + output) / 1e9
}

// qwen3Params estimates Qwen3-family models. Calibrated against Qwen3-0.6B
// (1024/151936/28 → 0.6 B) and Qwen3-1.7B (2048/151936/28 → 1.72 B). The
// 9.4 coefficient is much lower than qwen2's 18.1 because Qwen3 uses more
// aggressive MLP compression with the same family size buckets.
func qwen3Params(hidden, vocab, blocks int) float64 {
	if hidden <= 0 || vocab <= 0 || blocks <= 0 {
		return 0
	}
	embedding := float64(vocab) * float64(hidden)
	perBlock := 9.4 * float64(hidden) * float64(hidden)
	output := float64(vocab) * float64(hidden)
	return (embedding + perBlock*float64(blocks) + output) / 1e9
}

// gemma3Params estimates Gemma3-family models. Calibrated against
// Gemma-3-4B (2560/262144/34 → 4.30 B) and Gemma-3-27B (5376/262144/62 →
// 27.0 B). Gemma's large vocabulary (262k tokens) makes embedding+output
// contribute disproportionately for smaller models.
func gemma3Params(hidden, vocab, blocks int) float64 {
	if hidden <= 0 || vocab <= 0 || blocks <= 0 {
		return 0
	}
	embedding := float64(vocab) * float64(hidden)
	perBlock := 13.3 * float64(hidden) * float64(hidden)
	output := float64(vocab) * float64(hidden)
	return (embedding + perBlock*float64(blocks) + output) / 1e9
}

// paramsBFromTokenEmbd holds per-architecture formulas that estimate total
// parameter count in billions given hidden_dim (from token_embd.weight[0]),
// vocab_size (from token_embd.weight[1]), and block_count (from
// <arch>.block_count in the kv-block).
//
// Unknown arches return nil and the parser leaves ParamsCount=0 (preserves
// today's "?" display).
var paramsBFromTokenEmbd = map[string]func(hidden, vocab, blocks int) float64{
	"llama":  llamaParams,
	"qwen2":  qwen2Params,
	"qwen3":  qwen3Params,
	"gemma3": gemma3Params,
}
