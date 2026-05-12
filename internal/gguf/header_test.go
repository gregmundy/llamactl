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
