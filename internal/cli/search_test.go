package cli

import (
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/hf"
)

func TestSearchFiltersToWhitelistAndFormatsTable(t *testing.T) {
	hfc := &fakeHFClient{
		SearchHits: map[string][]hf.SearchHit{
			"qwen": {
				{ID: "Qwen/Qwen2.5-7B-Instruct-GGUF"},
				{ID: "Qwen/SomeOtherRepo-NotWhitelisted"},
			},
		},
		Repos: map[string]hf.Repo{
			"Qwen/Qwen2.5-7B-Instruct-GGUF": {
				ID: "Qwen/Qwen2.5-7B-Instruct-GGUF",
				Siblings: []hf.File{
					{RFilename: "qwen2.5-7b-instruct-q4_k_m.gguf"},
					{RFilename: "qwen2.5-7b-instruct-q5_k_m.gguf"},
				},
			},
		},
	}
	d := &Deps{HFClient: hfc}

	out, _, err := runRoot(t, d, "search", "qwen")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "qwen2.5-7b-instruct") {
		t.Errorf("output missing whitelisted id; got:\n%s", out)
	}
	if strings.Contains(out, "SomeOtherRepo") {
		t.Errorf("output included non-whitelisted repo:\n%s", out)
	}
	if !strings.Contains(out, "Q4_K_M") || !strings.Contains(out, "Q5_K_M") {
		t.Errorf("output missing quants:\n%s", out)
	}
}

func TestSearchEmptyOK(t *testing.T) {
	hfc := &fakeHFClient{SearchHits: map[string][]hf.SearchHit{"x": {}}}
	d := &Deps{HFClient: hfc}
	out, _, err := runRoot(t, d, "search", "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "no matches") {
		t.Errorf("expected 'no matches', got: %s", out)
	}
}
