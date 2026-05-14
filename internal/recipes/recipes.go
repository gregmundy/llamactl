// Package recipes encodes the four PRD §6.2 recipe→flag mappings and
// the rules for assembling a llama-server argv from a recipe + model +
// host + version. Pure data and pure functions: no I/O, no clocks, no
// env reads.
package recipes

import (
	"fmt"

	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/server"
)

type MlockMode int

const (
	MlockAuto MlockMode = iota // include --mlock when usable_gb > size_gb + 4
	MlockOff                   // never include --mlock
)

type Recipe struct {
	Name       string
	CtxSize    int
	CacheTypeK string
	CacheTypeV string
	MlockMode  MlockMode

	// Sampling, when non-nil, pins server-side defaults for --temp,
	// --top-p, --top-k, and --predict. nil means "leave llama-server's
	// own defaults alone" — every recipe through v1.4.4 chose this.
	//
	// Pointer rather than embedded struct because zero values for the
	// inner fields are meaningful: temp=0 is the deterministic-output
	// case that motivated this field's existence.
	Sampling *Sampling

	// Reasoning, when non-empty, passes through as `--reasoning <value>`.
	// "off" disables thinking server-wide for reasoning-capable families
	// (Qwen3, DeepSeek-R1, future). The default empty string lets
	// llama-server use its `auto` behavior.
	Reasoning string
}

// Sampling carries the four request-default knobs the agent recipe pins.
// All four fields are emitted together when Sampling != nil — partial
// override isn't a use case today.
type Sampling struct {
	Temperature float64
	TopP        float64
	TopK        int
	NPredict    int
}

const DefaultRecipe = "chat"

// MinFlashAttnBuild is the empirical floor for stable --flash-attn on
// Apple Silicon. llamavm-managed customs use a cmake counter starting at
// 1; those are treated as "modern" by shouldAddFlashAttn.
const MinFlashAttnBuild = 2700

var Recipes = map[string]Recipe{
	"chat":         {Name: "chat", CtxSize: 8192, CacheTypeK: "f16", CacheTypeV: "f16", MlockMode: MlockAuto},
	"code":         {Name: "code", CtxSize: 16384, CacheTypeK: "f16", CacheTypeV: "f16", MlockMode: MlockAuto},
	"long-context": {Name: "long-context", CtxSize: 32768, CacheTypeK: "q8_0", CacheTypeV: "q8_0", MlockMode: MlockAuto},
	"low-memory":   {Name: "low-memory", CtxSize: 4096, CacheTypeK: "q4_0", CacheTypeV: "q4_0", MlockMode: MlockOff},

	// agent: deterministic, non-interactive utility workloads (summarize,
	// extract, classify, rewrite, agent offload). Pins sampling so output
	// is repeatable across identical prompts and forces --reasoning off
	// so reasoning-capable models don't burn the generation budget on
	// internal thinking and return empty content.
	"agent": {
		Name: "agent", CtxSize: 8192,
		CacheTypeK: "f16", CacheTypeV: "f16",
		MlockMode: MlockAuto,
		Sampling: &Sampling{
			Temperature: 0,
			TopP:        1.0,
			TopK:        0,
			NPredict:    2048,
		},
		Reasoning: "off",
	},

	// thinking: explicit deep-reasoning workloads on Qwen3 / DeepSeek-R1
	// / future reasoning families. Same deterministic-sampling shape as
	// agent (temp 0, top_p 1, top_k 0) so the chain-of-thought is
	// reproducible across runs, but with --reasoning on so the model
	// actually enters <think> blocks instead of jumping to a one-shot
	// answer. Doubled NPredict (4096) gives the model room to think AND
	// respond — internal reasoning can easily consume 1000-2000 tokens
	// before the user-facing answer begins.
	"thinking": {
		Name: "thinking", CtxSize: 8192,
		CacheTypeK: "f16", CacheTypeV: "f16",
		MlockMode: MlockAuto,
		Sampling: &Sampling{
			Temperature: 0,
			TopP:        1.0,
			TopK:        0,
			NPredict:    4096,
		},
		Reasoning: "on",
	},
}

// FlagsFor assembles the llama-server argv. Inputs are read-only.
// `caps.FlashAttnTristate` selects between modern `--flash-attn on` and
// legacy bare-flag syntax. `cores` is the host CPU core count (callers
// typically pass platform.Default{}.Cores(); tests pass a fixed value).
func FlagsFor(r Recipe, m models.Model, _ models.Quant, ggufPath string,
	hw hardware.Info, ver server.Version, caps server.Capabilities,
	sizeGB float64, port int, cores int) []string {

	ctxSize := r.CtxSize
	if m.MaxCtx > 0 && m.MaxCtx < ctxSize {
		ctxSize = m.MaxCtx
	}

	threads := cores - 2
	if threads < 1 {
		threads = 1
	}

	args := []string{
		"--model", ggufPath,
		"--host", "0.0.0.0",
		"--port", fmt.Sprintf("%d", port),
		"--ctx-size", fmt.Sprintf("%d", ctxSize),
		"--n-gpu-layers", "999",
		"--cache-type-k", r.CacheTypeK,
		"--cache-type-v", r.CacheTypeV,
		"--threads", fmt.Sprintf("%d", threads),
	}

	if r.MlockMode == MlockAuto {
		usableGB := models.GpuAddressableGB(hw) - models.OSOverheadGB - models.HeadroomGB
		if usableGB > sizeGB+4.0 {
			args = append(args, "--mlock")
		}
	}

	if shouldAddFlashAttn(ver) {
		if caps.FlashAttnTristate {
			args = append(args, "--flash-attn", "on")
		} else {
			args = append(args, "--flash-attn")
		}
	}

	// Optional per-recipe server-side defaults. Emitted in a stable order
	// for snapshot-style test assertions.
	if r.Sampling != nil {
		args = append(args,
			"--temp", fmt.Sprintf("%g", r.Sampling.Temperature),
			"--top-p", fmt.Sprintf("%g", r.Sampling.TopP),
			"--top-k", fmt.Sprintf("%d", r.Sampling.TopK),
			"--predict", fmt.Sprintf("%d", r.Sampling.NPredict),
		)
	}
	if r.Reasoning != "" {
		args = append(args, "--reasoning", r.Reasoning)
	}

	return args
}

// shouldAddFlashAttn returns true if the resolved llama-server is new
// enough (or appears to be a llamavm-managed custom build using the
// cmake counter, which we assume is modern).
func shouldAddFlashAttn(v server.Version) bool {
	if v.Build < 100 {
		return true
	}
	return v.Build >= MinFlashAttnBuild
}
