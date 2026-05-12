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
