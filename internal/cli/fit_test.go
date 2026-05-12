package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/hf"
)

func buildFitTestDeps(t *testing.T, hits []hf.SearchHit, repos map[string]hf.Repo, hw hardware.Info) *Deps {
	t.Helper()
	tempDir := t.TempDir()
	hfc := &fakeHFClient{
		SearchHits: map[string][]hf.SearchHit{},
		Repos:      repos,
	}
	// Make Search return `hits` for any query.
	// We re-key by query inside test below if needed; here we just stash them
	// all under an empty key and override the Search behavior via a wrapper.
	return &Deps{
		Stdout:           io.Discard,
		Stderr:           io.Discard,
		HardwareDetector: fakeHardwareDetector{Info: hw},
		HardwareJSONPath: filepath.Join(tempDir, "hardware.json"),
		HFClient:         &fitFakeHFClient{inner: hfc, hits: hits},
		Downloader:       &fakeDownloader{HFClient: hfc},
		ModelStore:       newFakeModelStore(),
		FS:               OSFileSystem{},
		QuantSelector:    SelectorAdapter{},
		SharedModelsDir:  t.TempDir(),
		ModelsConfigDir:  filepath.Join(tempDir, "models"),
		Now:              fakeNow,
	}
}

// fitFakeHFClient wraps fakeHFClient so Search returns a fixed list regardless
// of query (the fit command joins all args, so we can't easily key by query).
type fitFakeHFClient struct {
	inner *fakeHFClient
	hits  []hf.SearchHit
}

func (f *fitFakeHFClient) Search(ctx context.Context, q string) ([]hf.SearchHit, error) {
	return f.hits, nil
}
func (f *fitFakeHFClient) SearchRefresh(ctx context.Context, q string) ([]hf.SearchHit, error) {
	return f.hits, nil
}
func (f *fitFakeHFClient) RepoInfo(ctx context.Context, id string) (hf.Repo, error) {
	return f.inner.RepoInfo(ctx, id)
}
func (f *fitFakeHFClient) FetchRange(ctx context.Context, repoID, file string, off, end int64, w io.Writer) error {
	return f.inner.FetchRange(ctx, repoID, file, off, end, w)
}

func TestFitShowsRankedTable(t *testing.T) {
	hits := []hf.SearchHit{
		{ID: "unsloth/gemma-4-E4B-it-GGUF"},
		{ID: "unsloth/gemma-4-31B-it-GGUF"},
	}
	repos := map[string]hf.Repo{
		"unsloth/gemma-4-E4B-it-GGUF": {Siblings: []hf.File{
			{RFilename: "Q5_K_M.gguf", LFS: &hf.LFSInfo{Size: int64(3.4e9), SHA256: "abc"}},
		}},
		"unsloth/gemma-4-31B-it-GGUF": {Siblings: []hf.File{
			{RFilename: "Q4_K_M.gguf", LFS: &hf.LFSInfo{Size: int64(17.2e9), SHA256: "def"}},
			{RFilename: "Q5_K_M.gguf", LFS: &hf.LFSInfo{Size: int64(20.5e9), SHA256: "ghi"}},
		}},
	}
	d := buildFitTestDeps(t, hits, repos, hardware.Info{RAMBytes: 24 << 30})
	var out bytes.Buffer
	d.Stdout = &out
	cmd := newFitCmd(d)
	cmd.SetArgs([]string{"gemma", "4"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "gemma-4-E4B-it-GGUF") {
		t.Fatalf("missing E4B row in output:\n%s", s)
	}
	if !strings.Contains(s, "✓") {
		t.Fatalf("missing checkmark verdict:\n%s", s)
	}
}

func TestFitNoGGUFRepos(t *testing.T) {
	hits := []hf.SearchHit{{ID: "meta-llama/Llama-3-70B"}}
	repos := map[string]hf.Repo{"meta-llama/Llama-3-70B": {Siblings: nil}}
	d := buildFitTestDeps(t, hits, repos, hardware.Info{RAMBytes: 24 << 30})
	var out bytes.Buffer
	d.Stdout = &out
	cmd := newFitCmd(d)
	cmd.SetArgs([]string{"llama"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no GGUF") {
		t.Fatalf("missing no-GGUF message:\n%s", out.String())
	}
}

func TestFitAllExceedHost(t *testing.T) {
	hits := []hf.SearchHit{{ID: "unsloth/gemma-4-31B-it-GGUF"}}
	repos := map[string]hf.Repo{
		"unsloth/gemma-4-31B-it-GGUF": {Siblings: []hf.File{
			{RFilename: "Q8_0.gguf", LFS: &hf.LFSInfo{Size: int64(50e9), SHA256: "x"}},
		}},
	}
	d := buildFitTestDeps(t, hits, repos, hardware.Info{RAMBytes: 16 << 30})
	var out bytes.Buffer
	d.Stdout = &out
	cmd := newFitCmd(d)
	cmd.SetArgs([]string{"gemma"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "✗") {
		t.Fatalf("expected x verdict for oversized model:\n%s", out.String())
	}
}

func TestFitInstallNoCandidate(t *testing.T) {
	hits := []hf.SearchHit{{ID: "unsloth/gemma-4-31B-it-GGUF"}}
	repos := map[string]hf.Repo{
		"unsloth/gemma-4-31B-it-GGUF": {Siblings: []hf.File{
			{RFilename: "Q8_0.gguf", LFS: &hf.LFSInfo{Size: int64(50e9), SHA256: "x"}},
		}},
	}
	d := buildFitTestDeps(t, hits, repos, hardware.Info{RAMBytes: 8 << 30})
	cmd := newFitCmd(d)
	cmd.SetArgs([]string{"gemma", "--install"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("expected ErrUserError, got %v", err)
	}
}

func TestFitSkipsTinyAuxiliaryFiles(t *testing.T) {
	hits := []hf.SearchHit{{ID: "user/some-model-GGUF"}}
	repos := map[string]hf.Repo{
		"user/some-model-GGUF": {Siblings: []hf.File{
			// Imatrix calibration shard (~100 MiB) — has a quant tag but must be filtered.
			{RFilename: "imatrix-Q4_K_M.gguf", LFS: &hf.LFSInfo{Size: 100 << 20, SHA256: "a"}},
			// Sub-1B Q4_K_M (e.g. qwen3-0.6b, ~600 MB) — must pass the 200 MiB floor.
			{RFilename: "qwen3-0.6b-Q4_K_M.gguf", LFS: &hf.LFSInfo{Size: 600 << 20, SHA256: "c"}},
			// Real larger model — should also appear.
			{RFilename: "model-Q5_K_M.gguf", LFS: &hf.LFSInfo{Size: 4 << 30, SHA256: "b"}},
		}},
	}
	d := buildFitTestDeps(t, hits, repos, hardware.Info{RAMBytes: 32 << 30})
	var out bytes.Buffer
	d.Stdout = &out
	cmd := newFitCmd(d)
	cmd.SetArgs([]string{"some-model"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "imatrix") {
		t.Fatalf("imatrix shard should have been filtered:\n%s", s)
	}
	if !strings.Contains(s, "Q4_K_M") {
		t.Fatalf("sub-1B Q4_K_M row missing (600 MiB should clear 200 MiB floor):\n%s", s)
	}
	if !strings.Contains(s, "Q5_K_M") {
		t.Fatalf("real model row missing:\n%s", s)
	}
}

func TestFitSkipsMmprojSiblings(t *testing.T) {
	hits := []hf.SearchHit{{ID: "user/multimodal-GGUF"}}
	repos := map[string]hf.Repo{
		"user/multimodal-GGUF": {Siblings: []hf.File{
			// mmproj — should be filtered.
			{RFilename: "mmproj-model-Q8_0.gguf", LFS: &hf.LFSInfo{Size: 600 << 20, SHA256: "a"}},
			// Real model — should appear.
			{RFilename: "model-Q5_K_M.gguf", LFS: &hf.LFSInfo{Size: 5 << 30, SHA256: "b"}},
		}},
	}
	d := buildFitTestDeps(t, hits, repos, hardware.Info{RAMBytes: 32 << 30})
	var out bytes.Buffer
	d.Stdout = &out
	cmd := newFitCmd(d)
	cmd.SetArgs([]string{"multimodal"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "mmproj") {
		t.Fatalf("mmproj row leaked through:\n%s", s)
	}
	if !strings.Contains(s, "Q5_K_M") {
		t.Fatalf("real model row missing:\n%s", s)
	}
}

func TestFitJSON(t *testing.T) {
	hits := []hf.SearchHit{{ID: "unsloth/gemma-4-E4B-it-GGUF"}}
	repos := map[string]hf.Repo{
		"unsloth/gemma-4-E4B-it-GGUF": {Siblings: []hf.File{
			{RFilename: "Q5_K_M.gguf", LFS: &hf.LFSInfo{Size: int64(3.4e9), SHA256: "x"}},
		}},
	}
	d := buildFitTestDeps(t, hits, repos, hardware.Info{RAMBytes: 24 << 30})
	var out bytes.Buffer
	d.Stdout = &out
	cmd := newFitCmd(d)
	cmd.SetArgs([]string{"gemma", "--json"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if len(rows) == 0 {
		t.Fatal("empty JSON array")
	}
	for _, k := range []string{"repo", "quant", "size_gb", "verdict"} {
		if _, ok := rows[0][k]; !ok {
			t.Fatalf("missing key %q in row: %v", k, rows[0])
		}
	}
}

func TestFitSkipsMultiShardAndNonGGUF(t *testing.T) {
	hits := []hf.SearchHit{{ID: "user/sharded-model-GGUF"}}
	repos := map[string]hf.Repo{
		"user/sharded-model-GGUF": {Siblings: []hf.File{
			// Multi-shard — should be filtered.
			{RFilename: "model-Q8_0-00001-of-00002.gguf", LFS: &hf.LFSInfo{Size: 1 << 30, SHA256: "a"}},
			{RFilename: "model-Q8_0-00002-of-00002.gguf", LFS: &hf.LFSInfo{Size: 1 << 30, SHA256: "b"}},
			// Non-GGUF — should be filtered.
			{RFilename: "model-Q4_K_M.bin", LFS: &hf.LFSInfo{Size: 2 << 30, SHA256: "c"}},
			// Single-file GGUF — should appear.
			{RFilename: "model-Q5_K_M.gguf", LFS: &hf.LFSInfo{Size: 3 << 30, SHA256: "d"}},
		}},
	}
	d := buildFitTestDeps(t, hits, repos, hardware.Info{RAMBytes: 32 << 30})
	var out bytes.Buffer
	d.Stdout = &out
	cmd := newFitCmd(d)
	cmd.SetArgs([]string{"sharded"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "-of-") {
		t.Fatalf("multi-shard row leaked through:\n%s", s)
	}
	if strings.Contains(s, ".bin") {
		t.Fatalf("non-GGUF row leaked through:\n%s", s)
	}
	if !strings.Contains(s, "Q5_K_M") {
		t.Fatalf("single-file row missing:\n%s", s)
	}
}
