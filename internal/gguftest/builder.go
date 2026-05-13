// Package gguftest builds synthetic GGUF byte streams for unit tests.
// Mirrors the layout in internal/gguf/header.go: magic("GGUF") + version(u32)
// + tensor_count(u64) + kv_count(u64) + N×{key_string, type_u32, value}.
//
// All writes target *bytes.Buffer, whose Write method is documented to never
// return an error; binary.Write therefore cannot fail here. We drop the error
// checks accordingly to keep this test helper concise.
package gguftest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

// GGUF value type codes (subset). Source: GGUF v3 spec.
// Only the codes writeValue actually serializes are exported.
const (
	TypeU32    uint32 = 4
	TypeI32    uint32 = 5
	TypeBool   uint32 = 7
	TypeString uint32 = 8
	TypeArray  uint32 = 9
	TypeU64    uint32 = 10
	TypeI64    uint32 = 11
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
	binary.Write(&buf, binary.LittleEndian, version)
	binary.Write(&buf, binary.LittleEndian, uint64(0)) // tensor_count
	binary.Write(&buf, binary.LittleEndian, uint64(len(kvs)))
	for _, kv := range kvs {
		writeString(&buf, kv.Key)
		binary.Write(&buf, binary.LittleEndian, kv.Type)
		if kv.RawTypeOnly {
			continue
		}
		if err := writeValue(&buf, kv.Type, kv.Value); err != nil {
			t.Fatalf("gguftest.Build: key=%q: %v", kv.Key, err)
		}
	}
	return buf.Bytes()
}

// Tensor is a GGUF tensor descriptor. Type and Offset are ignored by the
// parser's tensor-shape walk; tests typically set both to 0.
type Tensor struct {
	Name   string
	Dims   []uint64
	Type   uint32 // 0 = F32; ignored by ReadHeaderWithTensors
	Offset uint64 // ignored by ReadHeaderWithTensors
}

// BuildWithTensors mirrors Build but also writes a tensor info block after
// the kv-block. tensor_count in the GGUF header is set to len(tensors).
func BuildWithTensors(t *testing.T, version uint32, tensors []Tensor, kvs ...KV) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("GGUF")
	binary.Write(&buf, binary.LittleEndian, version)
	binary.Write(&buf, binary.LittleEndian, uint64(len(tensors))) // tensor_count
	binary.Write(&buf, binary.LittleEndian, uint64(len(kvs)))
	for _, kv := range kvs {
		writeString(&buf, kv.Key)
		binary.Write(&buf, binary.LittleEndian, kv.Type)
		if kv.RawTypeOnly {
			continue
		}
		if err := writeValue(&buf, kv.Type, kv.Value); err != nil {
			t.Fatalf("gguftest.BuildWithTensors: key=%q: %v", kv.Key, err)
		}
	}
	for _, tn := range tensors {
		writeString(&buf, tn.Name)
		binary.Write(&buf, binary.LittleEndian, uint32(len(tn.Dims)))
		for _, d := range tn.Dims {
			binary.Write(&buf, binary.LittleEndian, d)
		}
		binary.Write(&buf, binary.LittleEndian, tn.Type)
		binary.Write(&buf, binary.LittleEndian, tn.Offset)
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
		binary.Write(w, binary.LittleEndian, v.(uint32))
	case TypeU64:
		binary.Write(w, binary.LittleEndian, v.(uint64))
	case TypeI64:
		binary.Write(w, binary.LittleEndian, v.(int64))
	case TypeI32:
		binary.Write(w, binary.LittleEndian, v.(int32))
	case TypeBool:
		var b uint8
		if v.(bool) {
			b = 1
		}
		binary.Write(w, binary.LittleEndian, b)
	case TypeArray:
		av, ok := v.(ArrayValue)
		if !ok {
			return fmt.Errorf("gguftest: TypeArray value must be ArrayValue, got %T", v)
		}
		binary.Write(w, binary.LittleEndian, av.ElemType)
		binary.Write(w, binary.LittleEndian, uint64(len(av.Items)))
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
