package models

import (
	"errors"
	"testing"

	"github.com/gregmundy/llamactl/internal/hardware"
)

func hw(ramGB int, iogpuMB int) hardware.Info {
	return hardware.Info{
		RAMBytes:          uint64(ramGB) * (1 << 30),
		IogpuWiredLimitMB: iogpuMB,
	}
}

func TestSelectQuant_PRDExample16GBQwen7B(t *testing.T) {
	m := PreferredIDs["qwen2.5-7b-instruct"]
	got, err := SelectQuant(m, hw(16, 0), 8192)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != Q4_K_M {
		t.Errorf("got %s, want Q4_K_M (PRD AC#6)", got)
	}
}

func TestSelectQuant_NoneFit(t *testing.T) {
	m := PreferredIDs["llama3.3-70b"]
	_, err := SelectQuant(m, hw(8, 0), 8192)
	if !errors.Is(err, ErrNoQuantFits) {
		t.Fatalf("got %v, want ErrNoQuantFits", err)
	}
}

func TestSelectQuant_HighRAMPicksHighestQuant(t *testing.T) {
	m := PreferredIDs["qwen2.5-7b-instruct"]
	got, err := SelectQuant(m, hw(64, 0), 8192)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != Q5_K_M {
		t.Errorf("got %s, want Q5_K_M", got)
	}
}

func TestSelectQuant_IogpuOverrideUsed(t *testing.T) {
	// 64 GB RAM but iogpu pinned to 8 GB → tight budget.
	// usable = 8 - 4 - 2 = 2 GB → Q2_K (2.7 GB) > 2 → none fit → ErrNoQuantFits.
	m := PreferredIDs["qwen2.5-7b-instruct"]
	_, err := SelectQuant(m, hw(64, 8*1024), 8192)
	if !errors.Is(err, ErrNoQuantFits) {
		t.Errorf("got err=%v; expected ErrNoQuantFits", err)
	}
}

func TestSelectQuant_UnknownParamsBRow(t *testing.T) {
	m := Model{ID: "fake", HFRepo: "x/y", Arch: ArchQwen25, ParamsB: 999, MaxCtx: 4096}
	_, err := SelectQuant(m, hw(64, 0), 8192)
	if err == nil {
		t.Fatal("expected error for missing QuantSizeTable row")
	}
}
