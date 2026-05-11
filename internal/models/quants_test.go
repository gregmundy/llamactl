package models

import "testing"

func TestPreferenceOrder(t *testing.T) {
	want := []Quant{Q5_K_M, Q4_K_M, Q4_K_S, IQ4_XS, IQ3_M, IQ3_XS, Q2_K}
	if len(PreferenceOrder) != len(want) {
		t.Fatalf("PreferenceOrder length = %d, want %d", len(PreferenceOrder), len(want))
	}
	for i, q := range want {
		if PreferenceOrder[i] != q {
			t.Errorf("PreferenceOrder[%d] = %s, want %s", i, PreferenceOrder[i], q)
		}
	}
}

func TestQuantSizeTableMonotonic(t *testing.T) {
	for params, row := range QuantSizeTable {
		var prev float64 = 1e9
		for _, q := range PreferenceOrder {
			size, ok := row[q]
			if !ok {
				t.Errorf("QuantSizeTable[%d] missing %s", params, q)
				continue
			}
			if size > prev {
				t.Errorf("QuantSizeTable[%d][%s]=%.2f > previous %.2f (preference order should be larger->smaller)",
					params, q, size, prev)
			}
			prev = size
		}
	}
}

func TestKVCacheTablesPopulated(t *testing.T) {
	for _, arch := range []Arch{ArchQwen25, ArchLlama3, ArchMistral} {
		row, ok := KVCachePerTokenKB[arch]
		if !ok {
			t.Errorf("KVCachePerTokenKB missing arch %s", arch)
			continue
		}
		if _, ok := row[Q8_0]; !ok {
			t.Errorf("KVCachePerTokenKB[%s] missing Q8_0", arch)
		}
	}
}
