package telemetry

import (
	"encoding/json"
	"fmt"
)

// SlotsState is the subset of /slots we use.
type SlotsState struct {
	TotalSlots int
	BusySlots  int
}

// ParseSlots returns counts of total + busy slots from /slots JSON.
func ParseSlots(body []byte) (SlotsState, error) {
	var arr []struct {
		IsProcessing bool `json:"is_processing"`
	}
	if err := json.Unmarshal(body, &arr); err != nil {
		return SlotsState{}, fmt.Errorf("parse slots: %w", err)
	}
	var busy int
	for _, s := range arr {
		if s.IsProcessing {
			busy++
		}
	}
	return SlotsState{TotalSlots: len(arr), BusySlots: busy}, nil
}
