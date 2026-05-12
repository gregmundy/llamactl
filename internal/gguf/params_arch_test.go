package gguf_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregmundy/llamactl/internal/gguf"
	"github.com/gregmundy/llamactl/internal/gguftest"
)

func TestParamsArchLlama3_8B(t *testing.T) {
	raw := gguftest.BuildWithTensors(t, 3,
		[]gguftest.Tensor{
			{Name: "token_embd.weight", Dims: []uint64{4096, 128256}, Type: 0, Offset: 0},
		},
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "llama"},
		gguftest.KV{Key: "llama.block_count", Type: gguftest.TypeU32, Value: uint32(32)},
	)
	path := filepath.Join(t.TempDir(), "llama-8b.gguf")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := gguf.ReadHeaderWithTensors(path)
	if err != nil {
		t.Fatal(err)
	}
	paramsB := float64(h.ParamsCount) / 1e9
	if paramsB < 8.03*0.85 || paramsB > 8.03*1.15 {
		t.Errorf("Llama-3-8B: paramsB=%.2f, want 8.03 ± 15%%", paramsB)
	}
}

func TestParamsArchLlama3_70B(t *testing.T) {
	raw := gguftest.BuildWithTensors(t, 3,
		[]gguftest.Tensor{
			{Name: "token_embd.weight", Dims: []uint64{8192, 128256}, Type: 0, Offset: 0},
		},
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "llama"},
		gguftest.KV{Key: "llama.block_count", Type: gguftest.TypeU32, Value: uint32(80)},
	)
	path := filepath.Join(t.TempDir(), "llama-70b.gguf")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := gguf.ReadHeaderWithTensors(path)
	if err != nil {
		t.Fatal(err)
	}
	paramsB := float64(h.ParamsCount) / 1e9
	if paramsB < 70.6*0.85 || paramsB > 70.6*1.15 {
		t.Errorf("Llama-3-70B: paramsB=%.2f, want 70.6 ± 15%%", paramsB)
	}
}

func TestParamsArchQwen2_5_7B(t *testing.T) {
	paramsB := paramsBFor(t, "qwen2",
		[]uint64{3584, 152064}, 28)
	if paramsB < 7.62*0.85 || paramsB > 7.62*1.15 {
		t.Errorf("Qwen2.5-7B: paramsB=%.2f, want 7.62 ± 15%%", paramsB)
	}
}

func TestParamsArchQwen2_5_32B(t *testing.T) {
	paramsB := paramsBFor(t, "qwen2",
		[]uint64{5120, 152064}, 64)
	if paramsB < 32.76*0.85 || paramsB > 32.76*1.15 {
		t.Errorf("Qwen2.5-32B: paramsB=%.2f, want 32.76 ± 15%%", paramsB)
	}
}

func TestParamsArchQwen3_1_7B(t *testing.T) {
	paramsB := paramsBFor(t, "qwen3",
		[]uint64{2048, 151936}, 28)
	if paramsB < 1.72*0.85 || paramsB > 1.72*1.15 {
		t.Errorf("Qwen3-1.7B: paramsB=%.2f, want 1.72 ± 15%%", paramsB)
	}
}

func TestParamsArchQwen3_0_6B(t *testing.T) {
	paramsB := paramsBFor(t, "qwen3",
		[]uint64{1024, 151936}, 28)
	if paramsB < 0.6*0.85 || paramsB > 0.6*1.15 {
		t.Errorf("Qwen3-0.6B: paramsB=%.2f, want 0.6 ± 15%%", paramsB)
	}
}

func TestParamsArchGemma3_4B(t *testing.T) {
	paramsB := paramsBFor(t, "gemma3",
		[]uint64{2560, 262144}, 34)
	if paramsB < 4.30*0.85 || paramsB > 4.30*1.15 {
		t.Errorf("Gemma-3-4B: paramsB=%.2f, want 4.30 ± 15%%", paramsB)
	}
}

func TestParamsArchGemma3_27B(t *testing.T) {
	paramsB := paramsBFor(t, "gemma3",
		[]uint64{5376, 262144}, 62)
	if paramsB < 27.0*0.85 || paramsB > 27.0*1.15 {
		t.Errorf("Gemma-3-27B: paramsB=%.2f, want 27.0 ± 15%%", paramsB)
	}
}

func TestParamsArchMistral7B(t *testing.T) {
	paramsB := paramsBFor(t, "mistral",
		[]uint64{4096, 32000}, 32)
	if paramsB < 7.24*0.85 || paramsB > 7.24*1.15 {
		t.Errorf("Mistral-7B: paramsB=%.2f, want 7.24 ± 15%%", paramsB)
	}
}

// paramsBFor builds a synthetic fixture and runs ReadHeaderWithTensors
// against it, returning paramsB. Test helper shared across arch tests.
func paramsBFor(t *testing.T, arch string, tokenEmbdDims []uint64, blocks uint32) float64 {
	t.Helper()
	raw := gguftest.BuildWithTensors(t, 3,
		[]gguftest.Tensor{
			{Name: "token_embd.weight", Dims: tokenEmbdDims, Type: 0, Offset: 0},
		},
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: arch},
		gguftest.KV{Key: arch + ".block_count", Type: gguftest.TypeU32, Value: blocks},
	)
	path := filepath.Join(t.TempDir(), arch+".gguf")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := gguf.ReadHeaderWithTensors(path)
	if err != nil {
		t.Fatal(err)
	}
	return float64(h.ParamsCount) / 1e9
}
