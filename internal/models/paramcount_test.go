package models

import "testing"

func TestParseParamCountFromRepo(t *testing.T) {
	cases := []struct {
		repo string
		want float64
	}{
		{"Qwen/Qwen2.5-7B-Instruct-GGUF", 7},
		{"Qwen/Qwen3-0.6B-GGUF", 0.6},
		{"unsloth/gemma-4-31B-it-GGUF", 31},
		{"unsloth/gemma-4-E4B-it-GGUF", 4},
		{"meta-llama/Llama-3.3-70B-Instruct", 70},
		{"qwen2.5-7b-instruct", 7},
		{"unknown-repo-no-digits", 0},
		{"", 0},
	}
	for _, c := range cases {
		t.Run(c.repo, func(t *testing.T) {
			got := ParseParamCountFromRepo(c.repo)
			if got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestParseParamCountPrefersRepoName(t *testing.T) {
	got := ParseParamCountFromRepo("foo-7B/model-13B-it")
	if got != 13 {
		t.Fatalf("got %v, want 13", got)
	}
}

func TestKVCacheGB(t *testing.T) {
	// Known arch with Q8_0 row populated.
	got := KVCacheGB(ArchQwen25, 7, 8192)
	if got <= 0 {
		t.Fatalf("KVCacheGB returned non-positive for known arch: %v", got)
	}
	// Unknown arch returns 0.
	if v := KVCacheGB(Arch("unknown-arch"), 7, 8192); v != 0 {
		t.Fatalf("KVCacheGB(unknown) = %v, want 0", v)
	}
}
