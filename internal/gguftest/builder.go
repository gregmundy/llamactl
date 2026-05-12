// Package gguftest builds synthetic GGUF byte streams for unit tests.
// Mirrors the layout in internal/gguf/header.go: magic("GGUF") + version(u32)
// + tensor_count(u64) + kv_count(u64) + N×{key_string, type_u32, value}.
package gguftest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

// GGUF value type codes (subset). Source: GGUF v3 spec.
const (
	TypeU8     uint32 = 0
	TypeI8     uint32 = 1
	TypeU16    uint32 = 2
	TypeI16    uint32 = 3
	TypeU32    uint32 = 4
	TypeI32    uint32 = 5
	TypeF32    uint32 = 6
	TypeBool   uint32 = 7
	TypeString uint32 = 8
	TypeArray  uint32 = 9
	TypeU64    uint32 = 10
	TypeI64    uint32 = 11
	TypeF64    uint32 = 12
)

// KV is a single GGUF metadata entry.
//
// If RawTypeOnly is true, Build writes only the key and the type code (Type)
// and skips writing a value. This is used by the gguf parser tests to inject
// malformed/unsupported type codes that the parser should reject before it
// tries to read a value.
type KV struct {
	Key         string
	Type        uint32
	Value       any
	RawTypeOnly bool
}

// ArrayValue is the payload for a KV with Type == TypeArray.
type ArrayValue struct {
	ElemType uint32
	Items    []any
}

// Build returns synthetic GGUF bytes containing the given KV pairs.
// tensor_count is set to 0 (callers don't exercise tensor parsing).
func Build(t *testing.T, version uint32, kvs ...KV) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("GGUF")
	if err := binary.Write(&buf, binary.LittleEndian, version); err != nil {
		t.Fatalf("gguftest.Build: write version: %v", err)
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint64(0)); err != nil { // tensor_count
		t.Fatalf("gguftest.Build: write tensor_count: %v", err)
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint64(len(kvs))); err != nil {
		t.Fatalf("gguftest.Build: write kv_count: %v", err)
	}
	for _, kv := range kvs {
		writeString(&buf, kv.Key)
		if err := binary.Write(&buf, binary.LittleEndian, kv.Type); err != nil {
			t.Fatalf("gguftest.Build: key=%q: write type: %v", kv.Key, err)
		}
		if kv.RawTypeOnly {
			continue
		}
		if err := writeValue(&buf, kv.Type, kv.Value); err != nil {
			t.Fatalf("gguftest.Build: key=%q: %v", kv.Key, err)
		}
	}
	return buf.Bytes()
}

func writeString(w *bytes.Buffer, s string) {
	binary.Write(w, binary.LittleEndian, uint64(len(s)))
	w.WriteString(s)
}

func writeValue(w *bytes.Buffer, kind uint32, v any) error {
	switch kind {
	case TypeString:
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("gguftest: TypeString value must be string, got %T", v)
		}
		writeString(w, s)
	case TypeU32:
		return binary.Write(w, binary.LittleEndian, v.(uint32))
	case TypeU64:
		return binary.Write(w, binary.LittleEndian, v.(uint64))
	case TypeI64:
		return binary.Write(w, binary.LittleEndian, v.(int64))
	case TypeI32:
		return binary.Write(w, binary.LittleEndian, v.(int32))
	case TypeBool:
		var b uint8
		if v.(bool) {
			b = 1
		}
		return binary.Write(w, binary.LittleEndian, b)
	case TypeArray:
		av, ok := v.(ArrayValue)
		if !ok {
			return fmt.Errorf("gguftest: TypeArray value must be ArrayValue, got %T", v)
		}
		if err := binary.Write(w, binary.LittleEndian, av.ElemType); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, uint64(len(av.Items))); err != nil {
			return err
		}
		for _, item := range av.Items {
			if err := writeValue(w, av.ElemType, item); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("gguftest: unsupported type %d", kind)
	}
	return nil
}
