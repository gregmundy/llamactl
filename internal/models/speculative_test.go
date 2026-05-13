package models_test

import (
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/models"
)

func TestSpeculativePairArchMismatch(t *testing.T) {
	main := models.Model{ID: "qwen2.5-32b", Arch: models.ArchQwen25, ParamsB: 32}
	draft := models.Model{ID: "llama-3-1b", Arch: models.ArchLlama3, ParamsB: 1}
	hw := hardware.Info{RAMBytes: 64 << 30}

	v := models.SpeculativePair(main, draft, hw, "chat")
	if v.Ok {
		t.Errorf("expected Ok=false on arch mismatch; verdict=%+v", v)
	}
	if !strings.Contains(v.Reason, "arch") {
		t.Errorf("Reason should mention arch; got %q", v.Reason)
	}
}

func TestSpeculativePairRatioTooSmall(t *testing.T) {
	main := models.Model{ID: "qwen2.5-7b", Arch: models.ArchQwen25, ParamsB: 7}
	draft := models.Model{ID: "qwen2.5-7b-alt", Arch: models.ArchQwen25, ParamsB: 7}
	hw := hardware.Info{RAMBytes: 64 << 30}

	v := models.SpeculativePair(main, draft, hw, "chat")
	if v.Ok {
		t.Errorf("expected Ok=false at ratio 1×; got %+v", v)
	}
}

func TestSpeculativePairRatioOk(t *testing.T) {
	main := models.Model{ID: "qwen2.5-32b", Arch: models.ArchQwen25, ParamsB: 32, MaxCtx: 32768}
	draft := models.Model{ID: "qwen2.5-3b", Arch: models.ArchQwen25, ParamsB: 3, MaxCtx: 32768}
	hw := hardware.Info{RAMBytes: 64 << 30}

	v := models.SpeculativePair(main, draft, hw, "chat")
	if !v.Ok {
		t.Errorf("expected Ok=true at ratio ~10×; got %+v", v)
	}
	if v.Reason != "" {
		t.Errorf("expected no warning at ratio ~10×; Reason=%q", v.Reason)
	}
	if v.SizeRatio < 9 || v.SizeRatio > 12 {
		t.Errorf("SizeRatio=%.2f, want ~10.7", v.SizeRatio)
	}
}

func TestSpeculativePairRatioWarning(t *testing.T) {
	main := models.Model{ID: "qwen2.5-32b", Arch: models.ArchQwen25, ParamsB: 32, MaxCtx: 32768}
	draft := models.Model{ID: "qwen2.5-0.5b", Arch: models.ArchQwen25, ParamsB: 0.5, MaxCtx: 32768}
	hw := hardware.Info{RAMBytes: 64 << 30}

	v := models.SpeculativePair(main, draft, hw, "chat")
	if !v.Ok {
		t.Errorf("ratio 64× should be Ok=true (warning only); got %+v", v)
	}
	if v.Reason == "" {
		t.Errorf("expected warning Reason at ratio 64×; got empty")
	}
}

func TestSpeculativePairCombinedRAMTooBig(t *testing.T) {
	main := models.Model{ID: "qwen2.5-70b", Arch: models.ArchQwen25, ParamsB: 70, MaxCtx: 8192}
	draft := models.Model{ID: "qwen2.5-7b", Arch: models.ArchQwen25, ParamsB: 7, MaxCtx: 8192}
	hw := hardware.Info{RAMBytes: 32 << 30} // 32 GB → ~21 GB usable (insufficient for 70B Q4)

	v := models.SpeculativePair(main, draft, hw, "chat")
	if v.Ok {
		t.Errorf("expected Ok=false on RAM exhaustion; verdict=%+v", v)
	}
	if !strings.Contains(v.Reason, "RAM") {
		t.Errorf("Reason should mention RAM; got %q", v.Reason)
	}
}

func TestSpeculativePairZeroParamsB(t *testing.T) {
	main := models.Model{ID: "unknown", Arch: models.ArchQwen25, ParamsB: 0}
	draft := models.Model{ID: "qwen2.5-3b", Arch: models.ArchQwen25, ParamsB: 3}
	hw := hardware.Info{RAMBytes: 64 << 30}

	v := models.SpeculativePair(main, draft, hw, "chat")
	if v.Ok {
		t.Errorf("expected Ok=false on zero paramsB; got %+v", v)
	}
}

func TestSpeculativePairUnknownArch(t *testing.T) {
	main := models.Model{ID: "exotic-1", Arch: models.Arch("exotic"), ParamsB: 10}
	draft := models.Model{ID: "exotic-2", Arch: models.Arch("exotic"), ParamsB: 1}
	hw := hardware.Info{RAMBytes: 64 << 30}

	v := models.SpeculativePair(main, draft, hw, "chat")
	if !v.ArchMatch {
		t.Errorf("expected ArchMatch=true for same unknown arch")
	}
	// KVCacheGB returns 0 for unknown arch; CombinedRAMGB is just weights.
	// No panic should occur. The verdict's Ok value depends on whether the
	// weights fit — for a 10B + 1B model on a 64 GB host, they should fit.
	_ = v.CombinedRAMGB
}
