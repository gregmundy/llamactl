package models

import (
	"strings"
	"testing"
)

func TestPreferredIDsEntriesWellFormed(t *testing.T) {
	if len(PreferredIDs) == 0 {
		t.Fatal("PreferredIDs is empty")
	}
	for id, m := range PreferredIDs {
		if m.ID != id {
			t.Errorf("PreferredIDs[%q].ID = %q (must equal map key)", id, m.ID)
		}
		if m.HFRepo == "" {
			t.Errorf("PreferredIDs[%q].HFRepo empty", id)
		}
		if m.ParamsB <= 0 {
			t.Errorf("PreferredIDs[%q].ParamsB = %d", id, m.ParamsB)
		}
		if m.MaxCtx <= 0 {
			t.Errorf("PreferredIDs[%q].MaxCtx = %d", id, m.MaxCtx)
		}
		if _, ok := QuantSizeTable[m.ParamsB]; !ok {
			t.Errorf("PreferredIDs[%q].ParamsB = %d has no QuantSizeTable row", id, m.ParamsB)
		}
		switch m.Arch {
		case ArchQwen25, ArchLlama3, ArchMistral:
		default:
			t.Errorf("PreferredIDs[%q].Arch = %q (not a known Arch)", id, m.Arch)
		}
	}
}

func TestLookupOrSuggestHit(t *testing.T) {
	m, err := LookupOrSuggest("qwen2.5-7b-instruct")
	if err != nil {
		t.Fatalf("LookupOrSuggest returned error: %v", err)
	}
	if m.ID != "qwen2.5-7b-instruct" {
		t.Errorf("ID = %q", m.ID)
	}
}

func TestLookupOrSuggestMiss(t *testing.T) {
	_, err := LookupOrSuggest("not-a-real-model")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "qwen2.5-7b-instruct") {
		t.Errorf("error should list valid IDs, got: %s", msg)
	}
}
