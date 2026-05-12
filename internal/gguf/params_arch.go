package gguf

// paramsBFromTokenEmbd holds per-architecture formulas that estimate total
// parameter count in billions given hidden_dim (from token_embd.weight[0]),
// vocab_size (from token_embd.weight[1]), and block_count (from
// <arch>.block_count in the kv-block).
//
// Formulas land arch-by-arch in Tasks 3-5. Unknown arches return nil and
// the parser leaves ParamsCount=0 (preserves today's "?" display).
var paramsBFromTokenEmbd = map[string]func(hiddenDim, vocabSize, blockCount int) float64{}
