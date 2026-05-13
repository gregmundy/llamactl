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
	for _, arch := range []Arch{ArchQwen25, ArchQwen3, ArchLlama3, ArchGemma3, ArchGemma4} {
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

// TestArchFromGGUFNormalization documents which raw GGUF general.architecture
// values map to which canonical Arch constants. Real-world GGUFs report:
//   - Qwen 2 / Qwen 2.5 → "qwen2"
//   - Qwen 3            → "qwen3"
//   - Llama 3.x         → "llama"  (also covers Mistral, which uses Llama arch)
//   - Mistral self-id   → "mistral" (rare; most Mistral GGUFs report "llama")
//   - Gemma 3 / Gemma 4 → "gemma3" / "gemma4"
//
// The mapping ensures preferred-id adds (Arch: ArchQwen25) and HF-path adds
// (ArchFromGGUF on GGUF.Architecture) write the same string to Metadata.Arch.
func TestArchFromGGUFNormalization(t *testing.T) {
	cases := []struct {
		ggufArch string
		want     Arch
	}{
		{"qwen2", ArchQwen25},
		{"qwen3", ArchQwen3},
		{"llama", ArchLlama3},
		// "mistral" GGUF arch is non-standard (real Mistral GGUFs report
		// "llama"). When a GGUF does report "mistral" explicitly, it maps
		// to ArchLlama3 since Mistral uses Llama layer structure.
		{"mistral", ArchLlama3},
		{"gemma3", ArchGemma3},
		{"gemma4", ArchGemma4},
		{"falcon", Arch("falcon")}, // pass-through for unknown
	}
	for _, tc := range cases {
		got := ArchFromGGUF(tc.ggufArch)
		if got != tc.want {
			t.Errorf("ArchFromGGUF(%q) = %q, want %q", tc.ggufArch, got, tc.want)
		}
	}
}
