package gguf

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/gguftest"
)

// buildGGUF wraps gguftest.Build with the legacy signature used by these
// parser tests so the table-driven tests below stay terse.
//
// Pass empty arch ("") to omit the architecture KV.
// Pass paramsCount=0 to omit the parameter_count KV.
// Pass contextLength=0 to omit the <arch>.context_length KV.
// If badType is true, appends a KV with an unsupported type code (99).
func buildGGUF(t *testing.T, version uint32, arch string, paramsCount, contextLength uint64, badType bool) []byte {
	t.Helper()
	var kvs []gguftest.KV
	if arch != "" {
		kvs = append(kvs, gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: arch})
	}
	if paramsCount > 0 {
		kvs = append(kvs, gguftest.KV{Key: "general.parameter_count", Type: gguftest.TypeU64, Value: paramsCount})
	}
	if contextLength > 0 && arch != "" {
		kvs = append(kvs, gguftest.KV{Key: arch + ".context_length", Type: gguftest.TypeU64, Value: contextLength})
	}
	if badType {
		kvs = append(kvs, gguftest.KV{Key: "extra.bad", Type: 99, RawTypeOnly: true})
	}
	return gguftest.Build(t, version, kvs...)
}

func TestReadHeaderHappyPath(t *testing.T) {
	data := buildGGUF(t, 3, "llama", 8030000000, 131072, false)
	h, err := parseHeader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}
	if h.Version != 3 {
		t.Errorf("Version = %d, want 3", h.Version)
	}
	if h.Architecture != "llama" {
		t.Errorf("Architecture = %q, want llama", h.Architecture)
	}
	if h.ParamsCount != 8030000000 {
		t.Errorf("ParamsCount = %d, want 8030000000", h.ParamsCount)
	}
	if h.ContextLength != 131072 {
		t.Errorf("ContextLength = %d, want 131072", h.ContextLength)
	}
}

func TestReadHeaderMissingContextLength(t *testing.T) {
	data := buildGGUF(t, 3, "qwen3", 8030000000, 0, false)
	h, err := parseHeader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}
	if h.ContextLength != 0 {
		t.Errorf("ContextLength = %d, want 0", h.ContextLength)
	}
	if h.Architecture != "qwen3" {
		t.Errorf("Architecture = %q, want qwen3", h.Architecture)
	}
}

func TestReadHeaderBadMagic(t *testing.T) {
	data := []byte("WRONG\x03\x00\x00\x00")
	_, err := parseHeader(bytes.NewReader(data))
	if !errors.Is(err, ErrBadMagic) {
		t.Fatalf("err = %v, want ErrBadMagic", err)
	}
}

func TestReadHeaderUnsupportedVersion(t *testing.T) {
	data := buildGGUF(t, 99, "llama", 0, 0, false)
	_, err := parseHeader(bytes.NewReader(data))
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("err = %v, want ErrUnsupportedVersion", err)
	}
}

func TestReadHeaderUnsupportedValueType(t *testing.T) {
	data := buildGGUF(t, 3, "llama", 8030000000, 0, true)
	_, err := parseHeader(bytes.NewReader(data))
	if !errors.Is(err, ErrUnsupportedGGUFType) {
		t.Fatalf("err = %v, want ErrUnsupportedGGUFType", err)
	}
	if !strings.Contains(err.Error(), "99") {
		t.Errorf("error should mention type code 99; got: %v", err)
	}
}

func TestReadHeaderSkipsArrayValues(t *testing.T) {
	// Every real GGUF has tokenizer arrays. Confirm the parser walks past
	// them and still picks up subsequent keys.
	data := gguftest.Build(t, 3,
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "llama"},
		gguftest.KV{Key: "tokenizer.ggml.tokens", Type: gguftest.TypeArray, Value: gguftest.ArrayValue{
			ElemType: gguftest.TypeString,
			Items:    []any{"a", "b", "c"},
		}},
		gguftest.KV{Key: "general.parameter_count", Type: gguftest.TypeU64, Value: uint64(7615616512)},
	)

	h, err := parseHeader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}
	if h.Architecture != "llama" {
		t.Errorf("Architecture = %q, want llama", h.Architecture)
	}
	if h.ParamsCount != 7615616512 {
		t.Errorf("ParamsCount = %d, want 7615616512 (array should have been skipped)", h.ParamsCount)
	}
}

func TestReadHeaderFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tiny.gguf")
	data := buildGGUF(t, 3, "llama", 8030000000, 131072, false)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := ReadHeader(path)
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if h.Architecture != "llama" || h.ParamsCount != 8030000000 {
		t.Errorf("got %+v", h)
	}
}

func TestReadHeaderParamsCountInt64(t *testing.T) {
	data := gguftest.Build(t, 3,
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "gemma3"},
		gguftest.KV{Key: "general.parameter_count", Type: gguftest.TypeI64, Value: int64(4_000_000_000)},
	)
	h, err := parseHeader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if h.ParamsCount != 4_000_000_000 {
		t.Fatalf("ParamsCount=%d, want 4_000_000_000", h.ParamsCount)
	}
}

func TestReadHeaderParamsCountUint32(t *testing.T) {
	data := gguftest.Build(t, 3,
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "llama"},
		gguftest.KV{Key: "general.parameter_count", Type: gguftest.TypeU32, Value: uint32(3_000_000_000)},
	)
	h, err := parseHeader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if h.ParamsCount != 3_000_000_000 {
		t.Fatalf("ParamsCount=%d, want 3_000_000_000", h.ParamsCount)
	}
}

func TestReadHeaderParamsCountInt32(t *testing.T) {
	data := gguftest.Build(t, 3,
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "llama"},
		gguftest.KV{Key: "general.parameter_count", Type: gguftest.TypeI32, Value: int32(1_500_000_000)},
	)
	h, err := parseHeader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if h.ParamsCount != 1_500_000_000 {
		t.Fatalf("ParamsCount=%d, want 1_500_000_000", h.ParamsCount)
	}
}

func TestReadHeaderSizeLabelFallback(t *testing.T) {
	cases := []struct {
		label string
		want  uint64
	}{
		{"3.4B", 3_400_000_000},
		{"7.5B", 7_500_000_000},
		{"650M", 650_000_000},
		{"31B", 31_000_000_000},
		{"0.6B", 600_000_000},
		{"1.2T", 1_200_000_000_000},
		{"500K", 500_000},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			data := gguftest.Build(t, 3,
				gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "llama"},
				gguftest.KV{Key: "general.size_label", Type: gguftest.TypeString, Value: c.label},
			)
			h, err := parseHeader(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if h.ParamsCount != c.want {
				t.Fatalf("size_label=%q ParamsCount=%d, want %d", c.label, h.ParamsCount, c.want)
			}
		})
	}
}

func TestReadHeaderParamsCountPreferredOverSizeLabel(t *testing.T) {
	// When both keys are present, parameter_count wins (it's more authoritative).
	data := gguftest.Build(t, 3,
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "llama"},
		gguftest.KV{Key: "general.parameter_count", Type: gguftest.TypeU64, Value: uint64(7_200_000_000)},
		gguftest.KV{Key: "general.size_label", Type: gguftest.TypeString, Value: "7B"},
	)
	h, err := parseHeader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if h.ParamsCount != 7_200_000_000 {
		t.Fatalf("ParamsCount=%d, want 7_200_000_000 (parameter_count should win over size_label)", h.ParamsCount)
	}
}

func TestReadHeaderSizeLabelMalformed(t *testing.T) {
	// Garbage size_label leaves ParamsCount at 0 (the documented fallback behavior).
	data := gguftest.Build(t, 3,
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "llama"},
		gguftest.KV{Key: "general.size_label", Type: gguftest.TypeString, Value: "unknown"},
	)
	h, err := parseHeader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if h.ParamsCount != 0 {
		t.Fatalf("ParamsCount=%d, want 0 (malformed size_label should yield zero)", h.ParamsCount)
	}
}

func TestReadHeaderRealQwenFile(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no HOME")
	}
	path := filepath.Join(home, ".local/share/llama-models/qwen2.5-3b-instruct/Q5_K_M.gguf")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("real GGUF not present: %v", err)
	}
	h, err := ReadHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.ParamsCount < 2_700_000_000 || h.ParamsCount > 4_000_000_000 {
		t.Fatalf("ParamsCount=%d, want ~3-3.4B", h.ParamsCount)
	}
}

func TestReadHeaderRealGemmaFile(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no HOME")
	}
	path := filepath.Join(home, ".local/share/llama-models/gemma-4-e4b-it/Q4_K_M.gguf")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("real GGUF not present: %v", err)
	}
	h, err := ReadHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.ParamsCount < 6_500_000_000 || h.ParamsCount > 8_500_000_000 {
		t.Fatalf("ParamsCount=%d, want ~7-8B (Gemma 4 E4B reports size_label=7.5B)", h.ParamsCount)
	}
}

func TestReadHeaderWithTensorsNoFormulaArch(t *testing.T) {
	raw := gguftest.BuildWithTensors(t, 3,
		[]gguftest.Tensor{
			{Name: "token_embd.weight", Dims: []uint64{4096, 32000}, Type: 0, Offset: 0},
		},
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "exotic"},
		gguftest.KV{Key: "exotic.block_count", Type: gguftest.TypeU32, Value: uint32(32)},
	)
	path := filepath.Join(t.TempDir(), "exotic.gguf")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := ReadHeaderWithTensors(path)
	if err != nil {
		t.Fatalf("ReadHeaderWithTensors: %v", err)
	}
	if h.Architecture != "exotic" {
		t.Errorf("Architecture=%q, want %q", h.Architecture, "exotic")
	}
	if h.ParamsCount != 0 {
		t.Errorf("ParamsCount=%d, want 0 (no formula for exotic)", h.ParamsCount)
	}
	if h.BlockCount != 32 {
		t.Errorf("BlockCount=%d, want 32", h.BlockCount)
	}
}

func TestReadHeaderWithTensorsParamsCountAlreadySet(t *testing.T) {
	raw := gguftest.BuildWithTensors(t, 3,
		[]gguftest.Tensor{
			{Name: "token_embd.weight", Dims: []uint64{4096, 32000}, Type: 0, Offset: 0},
		},
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "llama"},
		gguftest.KV{Key: "general.parameter_count", Type: gguftest.TypeU64, Value: uint64(7_000_000_000)},
	)
	path := filepath.Join(t.TempDir(), "f.gguf")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := ReadHeaderWithTensors(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.ParamsCount != 7_000_000_000 {
		t.Errorf("ParamsCount=%d, want 7B preserved from kv-block", h.ParamsCount)
	}
}

func TestReadHeaderWithTensorsTruncatedDescriptor(t *testing.T) {
	raw := gguftest.BuildWithTensors(t, 3,
		[]gguftest.Tensor{
			{Name: "token_embd.weight", Dims: []uint64{4096, 32000}, Type: 0, Offset: 0},
		},
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "exotic"},
	)
	truncated := raw[:len(raw)-8]
	path := filepath.Join(t.TempDir(), "trunc.gguf")
	if err := os.WriteFile(path, truncated, 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := ReadHeaderWithTensors(path)
	if err != nil {
		t.Fatalf("expected no error on truncation; got %v", err)
	}
	if h.ParamsCount != 0 {
		t.Errorf("ParamsCount=%d, want 0 on truncation", h.ParamsCount)
	}
}
