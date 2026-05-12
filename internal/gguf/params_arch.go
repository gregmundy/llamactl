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

// paramsBFromTokenEmbd holds per-architecture formulas that estimate total
// parameter count in billions given hidden_dim (from token_embd.weight[0]),
// vocab_size (from token_embd.weight[1]), and block_count (from
// <arch>.block_count in the kv-block).
//
// Unknown arches return nil and the parser leaves ParamsCount=0 (preserves
// today's "?" display).
var paramsBFromTokenEmbd = map[string]func(hidden, vocab, blocks int) float64{
	"llama": llamaParams,
}
