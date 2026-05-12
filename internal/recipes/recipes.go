// Package recipes encodes the four PRD §6.2 recipe→flag mappings and
// the rules for assembling a llama-server argv from a recipe + model +
// host + version. Pure data and pure functions: no I/O, no clocks, no
// env reads.
package recipes

import (
	"fmt"

	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/platform"
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
}

// FlagsFor assembles the llama-server argv. Inputs are read-only.
func FlagsFor(r Recipe, m models.Model, _ models.Quant, ggufPath string,
	hw hardware.Info, ver server.Version, sizeGB float64, port int) []string {

	ctxSize := r.CtxSize
	if m.MaxCtx > 0 && m.MaxCtx < ctxSize {
		ctxSize = m.MaxCtx
	}

	threads := platform.Default{}.Cores() - 2
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
		// Recent llama-server builds made --flash-attn tristate
		// (on|off|auto) — bare flag now errors with "expected value".
		// We always pass `on` since Apple Silicon Metal benefits from it.
		args = append(args, "--flash-attn", "on")
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
