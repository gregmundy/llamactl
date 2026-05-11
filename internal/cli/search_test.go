package cli

import (
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/hf"
)

func TestSearchShowsAllResultsWithPreferredMarked(t *testing.T) {
	hfc := &fakeHFClient{
		SearchHits: map[string][]hf.SearchHit{
			"qwen": {
				{ID: "Qwen/Qwen2.5-7B-Instruct-GGUF"},
				{ID: "Qwen/SomeOtherRepo-NotPreferred-GGUF"},
				{ID: "Qwen/Qwen2.5-Coder-7B-Instruct-GGUF"},
			},
		},
		Repos: map[string]hf.Repo{
			"Qwen/Qwen2.5-7B-Instruct-GGUF": {
				ID: "Qwen/Qwen2.5-7B-Instruct-GGUF",
				Siblings: []hf.File{
					{RFilename: "qwen2.5-7b-instruct-q4_k_m.gguf"},
				},
			},
			"Qwen/Qwen2.5-Coder-7B-Instruct-GGUF": {
				ID: "Qwen/Qwen2.5-Coder-7B-Instruct-GGUF",
				Siblings: []hf.File{
					{RFilename: "qwen2.5-coder-7b-q5_k_m.gguf"},
				},
			},
			"Qwen/SomeOtherRepo-NotPreferred-GGUF": {
				ID: "Qwen/SomeOtherRepo-NotPreferred-GGUF",
				Siblings: []hf.File{
					{RFilename: "some-q4_k_m.gguf"},
				},
			},
		},
	}
	d := &Deps{HFClient: hfc}
	out, _, err := runRoot(t, d, "search", "qwen")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, want := range []string{"qwen2.5-7b-instruct", "qwen2.5-coder-7b", "SomeOtherRepo-NotPreferred-GGUF"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "* qwen2.5-7b-instruct") && !strings.Contains(out, "*  qwen2.5-7b-instruct") {
		t.Errorf("preferred row should be marked with `*`:\n%s", out)
	}
	if !strings.Contains(out, "?") {
		t.Errorf("non-preferred row should show `?` for PARAMS:\n%s", out)
	}
}

func TestSearchSortsPreferredFirst(t *testing.T) {
	hfc := &fakeHFClient{
		SearchHits: map[string][]hf.SearchHit{
			"qwen": {
				{ID: "Qwen/OtherRepo-GGUF"},
				{ID: "Qwen/Qwen2.5-7B-Instruct-GGUF"},
			},
		},
		Repos: map[string]hf.Repo{
			"Qwen/Qwen2.5-7B-Instruct-GGUF": {ID: "Qwen/Qwen2.5-7B-Instruct-GGUF"},
			"Qwen/OtherRepo-GGUF":           {ID: "Qwen/OtherRepo-GGUF"},
		},
	}
	d := &Deps{HFClient: hfc}
	out, _, err := runRoot(t, d, "search", "qwen")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	prefIdx := strings.Index(out, "qwen2.5-7b-instruct")
	otherIdx := strings.Index(out, "OtherRepo")
	if prefIdx == -1 || otherIdx == -1 {
		t.Fatalf("both rows should appear; got:\n%s", out)
	}
	if prefIdx > otherIdx {
		t.Errorf("preferred row should appear before non-preferred:\n%s", out)
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
