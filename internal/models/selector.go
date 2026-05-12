package models

import (
	"errors"
	"fmt"

	"github.com/gregmundy/llamactl/internal/hardware"
)

// ErrNoQuantFits is returned when even the smallest quant in
// PreferenceOrder does not fit the computed model budget.
var ErrNoQuantFits = errors.New("no quant fits available memory")

// GpuAddressableGB returns the GPU-addressable memory in gigabytes, derived
// from hw.IogpuWiredLimitMB if explicitly set, else from RAMBytes scaled by
// DefaultIogpuRatio (the empirical macOS default — see quants.go).
func GpuAddressableGB(hw hardware.Info) float64 {
	if hw.IogpuWiredLimitMB > 0 {
		return float64(hw.IogpuWiredLimitMB) / 1024.0
	}
	return float64(hw.RAMBytes) / (1 << 30) * DefaultIogpuRatio
}

// SelectQuant implements PRD §6.1. Pure function: no I/O.
func SelectQuant(model Model, info hardware.Info, targetCtx int) (Quant, error) {
	sizeRow, ok := QuantSizeTable[model.ParamsB]
	if !ok {
		return "", fmt.Errorf("no QuantSizeTable row for ParamsB=%d (model %q)", model.ParamsB, model.ID)
	}
	kvRow, ok := KVCachePerTokenKB[model.Arch]
	if !ok {
		return "", fmt.Errorf("no KVCachePerTokenKB row for Arch=%s (model %q)", model.Arch, model.ID)
	}
	kvPerTok, ok := kvRow[Q8_0]
	if !ok {
		return "", fmt.Errorf("no Q8_0 entry in KVCachePerTokenKB[%s]", model.Arch)
	}

	usable := GpuAddressableGB(info) - OSOverheadGB - HeadroomGB
	kvCacheGB := float64(targetCtx) * kvPerTok / (1024.0 * 1024.0)
	budget := usable - kvCacheGB
	if budget <= 0 {
		return "", fmt.Errorf("%w: usable memory after OS+headroom+KV is %.2f GB", ErrNoQuantFits, budget)
	}

	for _, q := range PreferenceOrder {
		size, ok := sizeRow[q]
		if !ok {
			continue
		}
		if size <= budget {
			return q, nil
		}
	}
	return "", fmt.Errorf("%w: smallest available quant (%s, %.2f GB) exceeds budget %.2f GB; try a smaller model or shorter --ctx",
		ErrNoQuantFits, PreferenceOrder[len(PreferenceOrder)-1], sizeRow[PreferenceOrder[len(PreferenceOrder)-1]], budget)
}
