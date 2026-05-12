package gguftest

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildMinimal(t *testing.T) {
	b := Build(t, 3,
		KV{Key: "general.architecture", Type: TypeString, Value: "llama"},
		KV{Key: "general.parameter_count", Type: TypeU64, Value: uint64(7_000_000_000)},
	)
	if !bytes.HasPrefix(b, []byte("GGUF")) {
		t.Fatalf("missing magic: %x", b[:8])
	}
	ver := binary.LittleEndian.Uint32(b[4:8])
	if ver != 3 {
		t.Fatalf("version=%d, want 3", ver)
	}
	tensorCount := binary.LittleEndian.Uint64(b[8:16])
	if tensorCount != 0 {
		t.Fatalf("tensor_count=%d, want 0", tensorCount)
	}
	kvCount := binary.LittleEndian.Uint64(b[16:24])
	if kvCount != 2 {
		t.Fatalf("kv_count=%d, want 2", kvCount)
	}
}

func TestBuildArrayKV(t *testing.T) {
	b := Build(t, 3,
		KV{Key: "tokenizer.ggml.tokens", Type: TypeArray, Value: ArrayValue{ElemType: TypeString, Items: []any{"a", "b"}}},
		KV{Key: "general.architecture", Type: TypeString, Value: "llama"},
	)
	if len(b) < 8 {
		t.Fatalf("short output: %d", len(b))
	}
}

func TestBuildRawTypeOnly(t *testing.T) {
	// RawTypeOnly is used to inject an unsupported type code with no value;
	// the gguf parser should error on the type code before reading a value.
	b := Build(t, 3,
		KV{Key: "extra.bad", Type: 99, RawTypeOnly: true},
	)
	// header(24) + key_len(8) + key("extra.bad"=9) + type(4) = 45
	if len(b) != 24+8+9+4 {
		t.Fatalf("len=%d, want 45", len(b))
	}
}
