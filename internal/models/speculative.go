// Package models — speculative.go: SpeculativePair eligibility logic for
// llama-server's --model-draft pairing. Pure function; no I/O.
package models

import (
	"fmt"
	"math"

	"github.com/gregmundy/llamactl/internal/hardware"
)

// PairVerdict reports whether using draft as the speculative-decoding draft
// for main is viable on the given host. Reason is non-empty when !Ok (the
// refusal message) or when a warning applies (size ratio outside ideal).
type PairVerdict struct {
	Ok            bool
	Reason        string
	CombinedRAMGB float64
	SizeRatio     float64 // main.ParamsB / draft.ParamsB
	ArchMatch     bool
}

// Speculative-decoding constants.
const (
	speculativeMinRatio      = 2.0  // below this: draft is too close to main, no speedup
	speculativeWarnLowRatio  = 5.0  // below this: warn (overhead may eat speedup)
	speculativeWarnHighRatio = 15.0 // above this: warn (draft too small, alignment poor)
	speculativeHeadroomGB    = 4.0  // same as fit's headroom
)

// SpeculativePair returns the verdict for using draft as the speculative-
// decoding draft for main, on hw running the named recipe.
//
// Refusal conditions (Ok=false):
//   - main.ParamsB <= 0 or draft.ParamsB <= 0 (size unknown)
//   - draft.Arch != main.Arch (tokenizer compatibility cannot be assumed)
//   - SizeRatio < speculativeMinRatio (no speedup possible)
//   - CombinedRAMGB > GpuAddressableGB(hw) - speculativeHeadroomGB (too-big)
//
// Warning conditions (Ok=true, Reason non-empty):
//   - SizeRatio < speculativeWarnLowRatio (overhead may exceed speedup)
//   - SizeRatio > speculativeWarnHighRatio (alignment likely poor)
func SpeculativePair(main, draft Model, hw hardware.Info, recipe string) PairVerdict {
	v := PairVerdict{
		ArchMatch: main.Arch == draft.Arch,
	}

	if main.ParamsB <= 0 || draft.ParamsB <= 0 {
		v.Reason = fmt.Sprintf("paramsB unknown (main=%.2f, draft=%.2f); cannot compute eligibility",
			main.ParamsB, draft.ParamsB)
		return v
	}
	if !v.ArchMatch {
		v.Reason = fmt.Sprintf("arch mismatch: main=%s, draft=%s (must match for tokenizer compatibility)",
			main.Arch, draft.Arch)
		return v
	}

	v.SizeRatio = main.ParamsB / draft.ParamsB
	if v.SizeRatio < speculativeMinRatio {
		v.Reason = fmt.Sprintf("size ratio %.1f× too small (draft must be at least %.0f× smaller than main)",
			v.SizeRatio, speculativeMinRatio)
		return v
	}

	// Combined RAM math: weights + KV cache for each model.
	ctx := ctxForRecipe(recipe)
	v.CombinedRAMGB = approxWeightsGB(main) + approxWeightsGB(draft) +
		KVCacheGB(main.Arch, main.ParamsB, ctx) + KVCacheGB(draft.Arch, draft.ParamsB, ctx)

	usable := GpuAddressableGB(hw)
	budget := usable - speculativeHeadroomGB
	if v.CombinedRAMGB > budget {
		v.Reason = fmt.Sprintf("combined weights + KV cache (%.1f GB) exceeds usable RAM (%.1f GB); free %.1f GB or pick a smaller draft",
			v.CombinedRAMGB, budget, v.CombinedRAMGB-budget)
		return v
	}

	v.Ok = true
	switch {
	case v.SizeRatio < speculativeWarnLowRatio:
		v.Reason = fmt.Sprintf("size ratio %.1f× below recommended 5-15× (overhead may eat speedup)", v.SizeRatio)
	case v.SizeRatio > speculativeWarnHighRatio:
		v.Reason = fmt.Sprintf("size ratio %.1f× above recommended 5-15× (draft alignment may be poor)", v.SizeRatio)
	}
	return v
}

// approxWeightsGB picks the Q4_K_M row from QuantSizeTable as a conservative
// default. Speculative pairing doesn't commit to a specific quant at
// eligibility time — the eventual `serve` call uses whatever quant is
// installed. Q4_K_M is the spec-decoding sweet spot for both main and draft.
func approxWeightsGB(m Model) float64 {
	bucket := int(math.Round(m.ParamsB))
	if row, ok := QuantSizeTable[bucket]; ok {
		if size, ok := row[Q4_K_M]; ok {
			return size
		}
	}
	// Unknown bucket: rough estimate of 0.6 GB per billion params at Q4_K_M.
	return m.ParamsB * 0.6
}

// ctxForRecipe maps a recipe name to its default ctx-size for the eligibility
// math. Matches the values in internal/recipes/recipes.go but inlined here
// to avoid an import cycle (models package is below recipes in the import
// graph).
func ctxForRecipe(recipe string) int {
	switch recipe {
	case "long-context":
		return 32768
	case "low-memory":
		return 2048
	case "code", "chat":
		return 8192
	default:
		return 8192
	}
}
