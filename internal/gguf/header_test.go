package gguf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildGGUF constructs a synthetic GGUF byte slice for tests.
// Pass empty arch ("") to omit the architecture KV.
// Pass paramsCount=0 to omit the parameter_count KV.
// Pass contextLength=0 to omit the <arch>.context_length KV.
// If badType is true, appends a KV with value_type=array (unsupported).
func buildGGUF(t *testing.T, version uint32, arch string, paramsCount, contextLength uint64, badType bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("GGUF")
	must(t, binary.Write(&buf, binary.LittleEndian, version))
	must(t, binary.Write(&buf, binary.LittleEndian, uint64(0))) // tensor_count

	var kvCount uint64
	if arch != "" {
		kvCount++
	}
	if paramsCount > 0 {
		kvCount++
	}
	if contextLength > 0 && arch != "" {
		kvCount++
	}
	if badType {
		kvCount++
	}
	must(t, binary.Write(&buf, binary.LittleEndian, kvCount))

	if arch != "" {
		writeKVString(t, &buf, "general.architecture", arch)
	}
	if paramsCount > 0 {
		writeKVU64(t, &buf, "general.parameter_count", paramsCount)
	}
	if contextLength > 0 && arch != "" {
		writeKVU64(t, &buf, arch+".context_length", contextLength)
	}
	if badType {
		writeKey(t, &buf, "extra.bad")
		must(t, binary.Write(&buf, binary.LittleEndian, uint32(9))) // array type
	}
	return buf.Bytes()
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}

func writeKey(t *testing.T, buf *bytes.Buffer, k string) {
	must(t, binary.Write(buf, binary.LittleEndian, uint64(len(k))))
	buf.WriteString(k)
}

func writeKVString(t *testing.T, buf *bytes.Buffer, key, value string) {
	writeKey(t, buf, key)
	must(t, binary.Write(buf, binary.LittleEndian, uint32(8))) // string
	must(t, binary.Write(buf, binary.LittleEndian, uint64(len(value))))
	buf.WriteString(value)
}

func writeKVU64(t *testing.T, buf *bytes.Buffer, key string, value uint64) {
	writeKey(t, buf, key)
	must(t, binary.Write(buf, binary.LittleEndian, uint32(10))) // u64
	must(t, binary.Write(buf, binary.LittleEndian, value))
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
	if !strings.Contains(err.Error(), "9") {
		t.Errorf("error should mention type code 9; got: %v", err)
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
