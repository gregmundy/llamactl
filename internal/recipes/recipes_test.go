package recipes

import (
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/server"
)

func TestRecipesMapWellFormed(t *testing.T) {
	want := []string{"chat", "code", "long-context", "low-memory"}
	for _, name := range want {
		r, ok := Recipes[name]
		if !ok {
			t.Errorf("Recipes[%q] missing", name)
			continue
		}
		if r.Name != name {
			t.Errorf("Recipes[%q].Name = %q", name, r.Name)
		}
		if r.CtxSize <= 0 {
			t.Errorf("Recipes[%q].CtxSize = %d", name, r.CtxSize)
		}
	}
}

func argvFlag(args []string, name string) (string, bool) {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func argvHasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func mkModel(maxCtx int) models.Model {
	return models.Model{ID: "fake", HFRepo: "x/y", Arch: models.ArchLlama3, ParamsB: 7, MaxCtx: maxCtx}
}

func mkHW(ramGB int) hardware.Info {
	return hardware.Info{RAMBytes: uint64(ramGB) * (1 << 30)}
}

func mkVer(build int) server.Version {
	return server.Version{Build: build}
}

func TestFlagsFor_BaseArgvForChat(t *testing.T) {
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/path/to.gguf", mkHW(64), mkVer(4500), server.Capabilities{FlashAttnTristate: true}, 4.4, 8080)
	if v, _ := argvFlag(args, "--ctx-size"); v != "8192" {
		t.Errorf("--ctx-size = %q, want 8192", v)
	}
	if v, _ := argvFlag(args, "--cache-type-k"); v != "f16" {
		t.Errorf("--cache-type-k = %q, want f16", v)
	}
	if v, _ := argvFlag(args, "--host"); v != "0.0.0.0" {
		t.Errorf("--host = %q, want 0.0.0.0", v)
	}
	if v, _ := argvFlag(args, "--port"); v != "8080" {
		t.Errorf("--port = %q, want 8080", v)
	}
	if v, _ := argvFlag(args, "--n-gpu-layers"); v != "999" {
		t.Errorf("--n-gpu-layers = %q, want 999", v)
	}
}

func TestFlagsFor_CtxSizeClampedToModelMaxCtx(t *testing.T) {
	args := FlagsFor(Recipes["long-context"], mkModel(4096), models.Q4_K_M, "/x", mkHW(64), mkVer(4500), server.Capabilities{FlashAttnTristate: true}, 4.4, 8080)
	if v, _ := argvFlag(args, "--ctx-size"); v != "4096" {
		t.Errorf("--ctx-size = %q, want 4096 (clamped from 32768)", v)
	}
}

func TestFlagsFor_MlockOnLargeHost(t *testing.T) {
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/x", mkHW(64), mkVer(4500), server.Capabilities{FlashAttnTristate: true}, 4.4, 8080)
	if !argvHasFlag(args, "--mlock") {
		t.Error("expected --mlock on 64GB host serving 4.4GB model")
	}
}

func TestFlagsFor_NoMlockOnTightHost(t *testing.T) {
	// 16 GB host serving a 14B Q5_K_M (10.4GB) — budget = 0.72, no mlock
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q5_K_M, "/x", mkHW(16), mkVer(4500), server.Capabilities{FlashAttnTristate: true}, 10.4, 8080)
	if argvHasFlag(args, "--mlock") {
		t.Error("expected NO --mlock on tight host")
	}
}

func TestFlagsFor_LowMemoryRecipeAlwaysNoMlock(t *testing.T) {
	args := FlagsFor(Recipes["low-memory"], mkModel(32768), models.Q4_K_M, "/x", mkHW(128), mkVer(4500), server.Capabilities{FlashAttnTristate: true}, 4.4, 8080)
	if argvHasFlag(args, "--mlock") {
		t.Error("low-memory recipe must never set --mlock, even on huge host")
	}
	if v, _ := argvFlag(args, "--cache-type-k"); v != "q4_0" {
		t.Errorf("low-memory --cache-type-k = %q, want q4_0", v)
	}
}

func TestFlagsFor_FlashAttnOnModernBuild(t *testing.T) {
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/x", mkHW(64), mkVer(4500), server.Capabilities{FlashAttnTristate: true}, 4.4, 8080)
	if v, ok := argvFlag(args, "--flash-attn"); !ok || v != "on" {
		t.Errorf("--flash-attn = %q (ok=%v), want on", v, ok)
	}
}

func TestFlagsFor_FlashAttnSkippedOnOldHomebrew(t *testing.T) {
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/x", mkHW(64), mkVer(1500), server.Capabilities{FlashAttnTristate: true}, 4.4, 8080)
	if argvHasFlag(args, "--flash-attn") {
		t.Error("expected NO --flash-attn on build 1500")
	}
}

func TestFlagsFor_FlashAttnOnLlamavmCustom(t *testing.T) {
	// llamavm-managed builds use cmake counter (small numbers). Assume modern.
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/x", mkHW(64), mkVer(3), server.Capabilities{FlashAttnTristate: true}, 4.4, 8080)
	if v, ok := argvFlag(args, "--flash-attn"); !ok || v != "on" {
		t.Errorf("--flash-attn = %q (ok=%v), want on (llamavm build)", v, ok)
	}
}

func TestFlagsFor_ModelPathIncluded(t *testing.T) {
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/path/to/model.gguf", mkHW(64), mkVer(4500), server.Capabilities{FlashAttnTristate: true}, 4.4, 8080)
	v, _ := argvFlag(args, "--model")
	if v != "/path/to/model.gguf" {
		t.Errorf("--model = %q, want /path/to/model.gguf", v)
	}
	// And argv should never contain ; or & or other shell metachars from filenames
	for _, a := range args {
		if strings.ContainsAny(a, ";&|") {
			t.Errorf("argv entry %q contains shell metachars", a)
		}
	}
}

func TestFlagsFor_FlashAttnLegacySyntaxOnOldBuild(t *testing.T) {
	// Modern build threshold met, but caps report legacy (no tristate).
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/x", mkHW(64),
		mkVer(4500), server.Capabilities{FlashAttnTristate: false}, 4.4, 8080)
	// Should contain bare "--flash-attn" but NOT "--flash-attn", "on".
	if !argvHasFlag(args, "--flash-attn") {
		t.Error("expected --flash-attn (bare) when caps say not tristate")
	}
	for i, a := range args {
		if a == "--flash-attn" && i+1 < len(args) && args[i+1] == "on" {
			t.Errorf("argv[%d:%d] = [%q, %q] — should be bare --flash-attn, not tristate", i, i+2, a, args[i+1])
		}
	}
}
