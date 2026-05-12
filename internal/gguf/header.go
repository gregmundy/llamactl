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
	"strconv"
	"strings"
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

// readLimit caps the number of bytes ReadHeader will pull from disk. Real
// GGUFs embed the full tokenizer vocabulary in the header; Qwen-class
// tokenizers (150k+ tokens) easily span 5-10 MiB. 64 MiB covers any
// reasonable tokenizer and still protects against pathological inputs.
const readLimit = 64 << 20

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
	var sizeLabel string
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
			switch v := value.(type) {
			case uint64:
				h.ParamsCount = v
			case int64:
				if v >= 0 {
					h.ParamsCount = uint64(v)
				}
			case uint32:
				h.ParamsCount = uint64(v)
			case int32:
				if v >= 0 {
					h.ParamsCount = uint64(v)
				}
			}
		case "general.size_label":
			if s, ok := value.(string); ok {
				sizeLabel = s
			}
		}
	}

	// Fallback: derive ParamsCount from general.size_label when
	// parameter_count was absent or unparseable. Phase 5 finding:
	// Unsloth/community quants for Gemma 4 and Qwen 2.5 omit
	// general.parameter_count but carry size_label as a string ("3.4B").
	if h.ParamsCount == 0 && sizeLabel != "" {
		h.ParamsCount = parseSizeLabel(sizeLabel)
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

// parseSizeLabel converts strings like "3.4B", "7.5B", "650M", "31B", "1.2T",
// "500K" into a raw parameter count. Suffix is K/M/B/T (case-insensitive).
// Returns 0 if the input doesn't match the expected shape; callers treat 0
// as "unparseable, leave fallback alone".
func parseSizeLabel(s string) uint64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// Last char is the multiplier suffix.
	last := s[len(s)-1]
	var mult float64
	switch last {
	case 'K', 'k':
		mult = 1e3
	case 'M', 'm':
		mult = 1e6
	case 'B', 'b':
		mult = 1e9
	case 'T', 't':
		mult = 1e12
	default:
		return 0
	}
	n, err := strconv.ParseFloat(s[:len(s)-1], 64)
	if err != nil || n < 0 {
		return 0
	}
	return uint64(n * mult)
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
	case typeArray:
		// Arrays appear in every real-world GGUF (tokenizer vocabularies,
		// merges, etc.). We don't need their contents — we just need to
		// advance the cursor past them so subsequent KVs parse correctly.
		var elemType uint32
		if err := binary.Read(r, binary.LittleEndian, &elemType); err != nil {
			return nil, fmt.Errorf("array elem type: %w", err)
		}
		var arrayLen uint64
		if err := binary.Read(r, binary.LittleEndian, &arrayLen); err != nil {
			return nil, fmt.Errorf("array length: %w", err)
		}
		for i := uint64(0); i < arrayLen; i++ {
			if _, err := readValue(r, elemType); err != nil {
				return nil, fmt.Errorf("array[%d]: %w", i, err)
			}
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedGGUFType, kind)
	}
}
