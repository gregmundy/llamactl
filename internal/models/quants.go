// Package models holds the preferred-ID table, quantization tables, and the
// pure quant-selection algorithm. No I/O. No clocks. No env reads. The
// FileStore in this package is the one exception — it operates on Metadata
// and is the natural home for it.
package models

// Quant is a GGUF quantization preset name (e.g., "Q4_K_M").
type Quant string

const (
	Q5_K_M Quant = "Q5_K_M"
	Q4_K_M Quant = "Q4_K_M"
	Q4_K_S Quant = "Q4_K_S"
	IQ4_XS Quant = "IQ4_XS"
	IQ3_M  Quant = "IQ3_M"
	IQ3_XS Quant = "IQ3_XS"
	Q2_K   Quant = "Q2_K"

	// Q8_0 is only used as a KV-cache lookup key; never appears in
	// PreferenceOrder because the spec doesn't ship 8-bit weights as a
	// fallback (too large for the host classes we target).
	Q8_0 Quant = "Q8_0"
)

// PreferenceOrder is the descending-quality fallback chain from PRD §6.1.
// SelectQuant walks this list and returns the first quant whose size fits
// the computed model budget.
var PreferenceOrder = []Quant{Q5_K_M, Q4_K_M, Q4_K_S, IQ4_XS, IQ3_M, IQ3_XS, Q2_K}

// Arch is a model family tag used as a KV-cache lookup key.
type Arch string

const (
	ArchQwen25  Arch = "qwen2.5"
	ArchQwen3   Arch = "qwen3"
	ArchLlama3  Arch = "llama3"
	ArchMistral Arch = "mistral"
)

// Selector constants from PRD §6.1.
const (
	OSOverheadGB = 4.0
	HeadroomGB   = 2.0

	// DefaultIogpuRatio is the fraction of total RAM macOS will allow the
	// GPU to wire when iogpu.wired_limit_mb is not explicitly set. The
	// real default is dynamic; 0.67 is the empirical mean that makes PRD
	// AC#6 produce Q4_K_M on a 16 GB host with qwen2.5-7b, and it matches
	// what `sudo sysctl iogpu.wired_limit_mb` users typically observe
	// before overriding.
	DefaultIogpuRatio = 0.67
)

// QuantSizeTable[paramsB][quant] is approximate on-disk GGUF size in
// gigabytes. Numbers are starting estimates from llama.cpp's GGUF
// model-size docs + measured filesizes for the v1 preferred-IDs. The
// implementer should re-validate against HF file sizes during Task 11.
//
// Keys are whole-billion buckets. Callers must round via math.Round at
// lookup time (e.g. int(math.Round(model.ParamsB))). Sub-1B models
// (ParamsB < 0.5) round to 0 and will miss — add a dedicated row if needed.
var QuantSizeTable = map[int]map[Quant]float64{
	1:  {Q5_K_M: 0.7, Q4_K_M: 0.6, Q4_K_S: 0.6, IQ4_XS: 0.5, IQ3_M: 0.5, IQ3_XS: 0.5, Q2_K: 0.4},
	2:  {Q5_K_M: 1.4, Q4_K_M: 1.2, Q4_K_S: 1.1, IQ4_XS: 1.1, IQ3_M: 1.0, IQ3_XS: 0.9, Q2_K: 0.8},
	3:  {Q5_K_M: 2.2, Q4_K_M: 1.9, Q4_K_S: 1.8, IQ4_XS: 1.7, IQ3_M: 1.5, IQ3_XS: 1.4, Q2_K: 1.3},
	7:  {Q5_K_M: 5.1, Q4_K_M: 4.4, Q4_K_S: 4.1, IQ4_XS: 3.8, IQ3_M: 3.3, IQ3_XS: 3.1, Q2_K: 2.7},
	8:  {Q5_K_M: 5.7, Q4_K_M: 4.9, Q4_K_S: 4.6, IQ4_XS: 4.3, IQ3_M: 3.8, IQ3_XS: 3.5, Q2_K: 3.0},
	14: {Q5_K_M: 10.4, Q4_K_M: 8.9, Q4_K_S: 8.4, IQ4_XS: 7.8, IQ3_M: 6.9, IQ3_XS: 6.4, Q2_K: 5.5},
	70: {Q5_K_M: 49.9, Q4_K_M: 42.5, Q4_K_S: 40.3, IQ4_XS: 37.7, IQ3_M: 32.9, IQ3_XS: 30.8, Q2_K: 26.4},
}

// KVCachePerTokenKB[arch][kvQuant] is the combined K+V cache size per token
// in kilobytes. A single conservative number per arch covers the largest
// supported model in that family — overestimates KV for smaller models in
// the same family, biasing the selector slightly toward smaller weight
// quants. Acceptable for v1; refine when adding new models.
var KVCachePerTokenKB = map[Arch]map[Quant]float64{
	ArchQwen25: {Q8_0: 0.5},
	// Qwen3 uses more aggressive GQA than Qwen2.5, resulting in a smaller
	// KV cache footprint per token (0.4 vs 0.5 KiB/token).
	ArchQwen3:   {Q8_0: 0.4},
	ArchLlama3:  {Q8_0: 0.5},
	ArchMistral: {Q8_0: 0.5},
}
