// Package-level addition. Helpers for the `fit` command:
//   - ParseParamCountFromRepo extracts a parameter count (in billions) from
//     HuggingFace repo paths via regex.
//   - KVCacheGB estimates KV-cache memory footprint at runtime ctx.
package models

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	// E4B-style "effective" capacity: capture the digit between E and B.
	paramCountReEffective = regexp.MustCompile(`E(\d+(?:\.\d+)?)B`)
	// Standard digits-then-b/B.
	paramCountReStandard = regexp.MustCompile(`(\d+(?:\.\d+)?)[bB]`)
)

// ParseParamCountFromRepo extracts a parameter count in billions from a
// HuggingFace repo path. Returns 0 if no recognizable pattern is found.
// Prefers the repo name (last path segment) over the owner segment.
func ParseParamCountFromRepo(repo string) float64 {
	if repo == "" {
		return 0
	}
	parts := strings.Split(repo, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if v := matchParamCount(parts[i]); v > 0 {
			return v
		}
	}
	return 0
}

func matchParamCount(s string) float64 {
	if m := paramCountReEffective.FindStringSubmatch(s); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			return v
		}
	}
	if m := paramCountReStandard.FindStringSubmatch(s); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			return v
		}
	}
	return 0
}

// KVCacheGB estimates the KV-cache memory footprint at the given ctx length.
// Uses KVCachePerTokenKB[arch][Q8_0]. Returns 0 if the arch is unknown so
// callers can fall back to a conservative padding.
func KVCacheGB(arch Arch, paramsB float64, ctx int) float64 {
	row, ok := KVCachePerTokenKB[arch]
	if !ok {
		return 0
	}
	perTokKB, ok := row[Q8_0]
	if !ok || perTokKB <= 0 {
		return 0
	}
	return perTokKB * float64(ctx) / (1024.0 * 1024.0)
}
