// Package gguf reads the metadata header of a GGUF file. It does NOT parse
// tensor data — only the leading kv_count section, which contains the
// three keys we care about: general.architecture,
// general.parameter_count, and <arch>.context_length.
package gguf

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

var (
	ErrBadMagic            = errors.New("GGUF magic not present")
	ErrUnsupportedVersion  = errors.New("unsupported GGUF version")
	ErrUnsupportedGGUFType = errors.New("unsupported GGUF value type")
)

// Header is the subset of GGUF metadata that llamactl needs.
type Header struct {
	Version       uint32
	TensorCount   uint64
	Architecture  string // general.architecture
	ParamsCount   uint64 // general.parameter_count
	ContextLength uint64 // <Architecture>.context_length; 0 if absent
}

// GGUF value type codes (subset). Source: GGUF v3 spec.
const (
	typeU8     uint32 = 0
	typeI8     uint32 = 1
	typeU16    uint32 = 2
	typeI16    uint32 = 3
	typeU32    uint32 = 4
	typeI32    uint32 = 5
	typeF32    uint32 = 6
	typeBool   uint32 = 7
	typeString uint32 = 8
	typeArray  uint32 = 9
	typeU64    uint32 = 10
	typeI64    uint32 = 11
	typeF64    uint32 = 12
)

// readLimit caps the number of bytes ReadHeader will pull from disk. The
// header section is at the start of the file and is typically <100 KiB;
// 1 MiB is comfortable headroom and prevents pathological reads on
// corrupted files.
const readLimit = 1 << 20

// ReadHeader opens path and parses the GGUF metadata header. Returns
// typed errors for bad magic, unsupported version, and unsupported
// value types.
func ReadHeader(path string) (Header, error) {
	f, err := os.Open(path)
	if err != nil {
		return Header{}, err
	}
	defer f.Close()
	return parseHeader(io.LimitReader(f, readLimit))
}

func parseHeader(r io.Reader) (Header, error) {
	br := bufio.NewReader(r)

	magic := make([]byte, 4)
	if _, err := io.ReadFull(br, magic); err != nil {
		return Header{}, fmt.Errorf("read magic: %w", err)
	}
	if string(magic) != "GGUF" {
		return Header{}, fmt.Errorf("%w: got %q", ErrBadMagic, magic)
	}

	var h Header
	if err := binary.Read(br, binary.LittleEndian, &h.Version); err != nil {
		return Header{}, fmt.Errorf("read version: %w", err)
	}
	if h.Version != 2 && h.Version != 3 {
		return Header{}, fmt.Errorf("%w: %d", ErrUnsupportedVersion, h.Version)
	}
	if err := binary.Read(br, binary.LittleEndian, &h.TensorCount); err != nil {
		return Header{}, fmt.Errorf("read tensor_count: %w", err)
	}
	var kvCount uint64
	if err := binary.Read(br, binary.LittleEndian, &kvCount); err != nil {
		return Header{}, fmt.Errorf("read kv_count: %w", err)
	}

	// Collect all KVs first because <arch>.context_length depends on
	// the value of general.architecture, and the spec doesn't guarantee
	// the order keys appear in.
	type kvEntry struct {
		key   string
		value any
	}
	const maxKVCount = 10_000
	if kvCount > maxKVCount {
		return Header{}, fmt.Errorf("implausible kv_count %d (exceeds %d)", kvCount, maxKVCount)
	}
	entries := make([]kvEntry, 0, kvCount)
	for i := uint64(0); i < kvCount; i++ {
		key, err := readString(br)
		if err != nil {
			return Header{}, fmt.Errorf("kv[%d] key: %w", i, err)
		}
		var kind uint32
		if err := binary.Read(br, binary.LittleEndian, &kind); err != nil {
			return Header{}, fmt.Errorf("kv[%d] type: %w", i, err)
		}
		value, err := readValue(br, kind)
		if err != nil {
			return Header{}, fmt.Errorf("kv[%d] value (key=%q): %w", i, key, err)
		}
		entries = append(entries, kvEntry{key: key, value: value})

		switch key {
		case "general.architecture":
			if s, ok := value.(string); ok {
				h.Architecture = s
			}
		case "general.parameter_count":
			if v, ok := value.(uint64); ok {
				h.ParamsCount = v
			}
		}
	}

	if h.Architecture != "" {
		wantKey := h.Architecture + ".context_length"
		for _, kv := range entries {
			if kv.key != wantKey {
				continue
			}
			switch v := kv.value.(type) {
			case uint64:
				h.ContextLength = v
			case uint32:
				h.ContextLength = uint64(v)
			}
			break
		}
	}

	return h, nil
}

func readString(r io.Reader) (string, error) {
	var n uint64
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return "", err
	}
	if n > readLimit {
		return "", fmt.Errorf("string length %d exceeds limit", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func readValue(r io.Reader, kind uint32) (any, error) {
	switch kind {
	case typeU8:
		var v uint8
		err := binary.Read(r, binary.LittleEndian, &v)
		return v, err
	case typeI8:
		var v int8
		err := binary.Read(r, binary.LittleEndian, &v)
		return v, err
	case typeU16:
		var v uint16
		err := binary.Read(r, binary.LittleEndian, &v)
		return v, err
	case typeI16:
		var v int16
		err := binary.Read(r, binary.LittleEndian, &v)
		return v, err
	case typeU32:
		var v uint32
		err := binary.Read(r, binary.LittleEndian, &v)
		return v, err
	case typeI32:
		var v int32
		err := binary.Read(r, binary.LittleEndian, &v)
		return v, err
	case typeF32:
		var v float32
		err := binary.Read(r, binary.LittleEndian, &v)
		return v, err
	case typeBool:
		var v uint8
		err := binary.Read(r, binary.LittleEndian, &v)
		return v != 0, err
	case typeString:
		return readString(r)
	case typeU64:
		var v uint64
		err := binary.Read(r, binary.LittleEndian, &v)
		return v, err
	case typeI64:
		var v int64
		err := binary.Read(r, binary.LittleEndian, &v)
		return v, err
	case typeF64:
		var v float64
		err := binary.Read(r, binary.LittleEndian, &v)
		return v, err
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedGGUFType, kind)
	}
}
