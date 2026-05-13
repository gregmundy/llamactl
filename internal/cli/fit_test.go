package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/models"
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

func TestFitRanksByDownloadsWithinOK(t *testing.T) {
	hits := []hf.SearchHit{
		{ID: "obscure/gemma-fork-GGUF", Downloads: 50},
		{ID: "canonical/gemma-official-GGUF", Downloads: 50_000},
	}
	repos := map[string]hf.Repo{
		"obscure/gemma-fork-GGUF": {Siblings: []hf.File{
			{RFilename: "model-Q5_K_M.gguf", LFS: &hf.LFSInfo{Size: 3 << 30, SHA256: "a"}},
		}},
		"canonical/gemma-official-GGUF": {Siblings: []hf.File{
			{RFilename: "model-Q5_K_M.gguf", LFS: &hf.LFSInfo{Size: 3 << 30, SHA256: "b"}},
		}},
	}
	d := buildFitTestDeps(t, hits, repos, hardware.Info{RAMBytes: 32 << 30})
	var out bytes.Buffer
	d.Stdout = &out
	cmd := newFitCmd(d)
	cmd.SetArgs([]string{"gemma"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	canonicalIdx := strings.Index(s, "canonical/gemma-official-GGUF")
	obscureIdx := strings.Index(s, "obscure/gemma-fork-GGUF")
	if canonicalIdx == -1 || obscureIdx == -1 {
		t.Fatalf("missing one of the repos:\n%s", s)
	}
	if canonicalIdx > obscureIdx {
		t.Fatalf("canonical should rank above obscure; got order:\n%s", s)
	}
}

func TestFitDedupesRepoFirstThenAlternates(t *testing.T) {
	// Two repos, each with two quants. Both have the same Downloads count.
	// After dedupe, the top 3 rows must include both repos — not three quants
	// from the same repo.
	hits := []hf.SearchHit{
		{ID: "alpha/qwen-GGUF", Downloads: 100},
		{ID: "beta/qwen-GGUF", Downloads: 100},
	}
	repos := map[string]hf.Repo{
		"alpha/qwen-GGUF": {Siblings: []hf.File{
			{RFilename: "model-Q5_K_M.gguf", LFS: &hf.LFSInfo{Size: 3 << 30, SHA256: "a1"}},
			{RFilename: "model-Q4_K_M.gguf", LFS: &hf.LFSInfo{Size: 2 << 30, SHA256: "a2"}},
		}},
		"beta/qwen-GGUF": {Siblings: []hf.File{
			{RFilename: "model-Q5_K_M.gguf", LFS: &hf.LFSInfo{Size: 3 << 30, SHA256: "b1"}},
			{RFilename: "model-Q4_K_M.gguf", LFS: &hf.LFSInfo{Size: 2 << 30, SHA256: "b2"}},
		}},
	}
	d := buildFitTestDeps(t, hits, repos, hardware.Info{RAMBytes: 32 << 30})
	var out bytes.Buffer
	d.Stdout = &out
	cmd := newFitCmd(d)
	cmd.SetArgs([]string{"qwen", "--limit", "3"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	// The top 3 rows must not all be from alpha — per-repo dedupe should push
	// the best beta quant into the primary group before any alpha alternates.
	if strings.Count(s, "alpha/qwen-GGUF") == 3 {
		t.Fatalf("expected per-repo dedupe; got 3 alpha rows:\n%s", s)
	}
	if !strings.Contains(s, "beta/qwen-GGUF") {
		t.Fatalf("beta repo missing from top 3 — dedupe didn't push it up:\n%s", s)
	}
}

// With a deep alternates pool and a large --limit, the 60/40 bucketing
// reserves slots for alternate quants of the top repos so users can compare
// Q5/Q4/IQ3 variants without scrolling past unrelated repos. 10 repos with
// 2 quants each + --limit 10:
//   - Old behavior: 10 primaries, 0 alternates → 10 unique repos
//   - New 60/40: 6 primaries (60%) + 4 alternates (40%) → 6 unique repos
func TestFitBucketingPreservesAlternatesAtLargerLimit(t *testing.T) {
	var hits []hf.SearchHit
	repos := map[string]hf.Repo{}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("user%d/qwen-GGUF", i)
		hits = append(hits, hf.SearchHit{ID: id, Downloads: 1000 - i})
		repos[id] = hf.Repo{Siblings: []hf.File{
			{RFilename: "model-Q5_K_M.gguf", LFS: &hf.LFSInfo{Size: 3 << 30, SHA256: fmt.Sprintf("%dh", i)}},
			{RFilename: "model-Q4_K_M.gguf", LFS: &hf.LFSInfo{Size: 2 << 30, SHA256: fmt.Sprintf("%dl", i)}},
		}}
	}
	d := buildFitTestDeps(t, hits, repos, hardware.Info{RAMBytes: 32 << 30})
	var out bytes.Buffer
	d.Stdout = &out
	cmd := newFitCmd(d)
	cmd.SetArgs([]string{"qwen", "--limit", "10"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	// Count unique repos in the output. Each repo's name appears at least
	// once per row that references it.
	unique := map[string]bool{}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("user%d/qwen-GGUF", i)
		if strings.Contains(s, id) {
			unique[id] = true
		}
	}
	// With 60/40 bucketing, we expect 6 unique repos in the table (the 6
	// primaries), not all 10. The remaining 4 slots are alternates of the
	// top 4 repos.
	if len(unique) != 6 {
		t.Errorf("expected 6 unique repos under 60/40 bucketing; got %d\noutput:\n%s",
			len(unique), s)
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

// --- Phase 6b: --speculative tests ---

func TestFitSpeculativeListsInstalledDrafts(t *testing.T) {
	tmp := t.TempDir()
	store := newFakeModelStore()
	// Main + three candidates: one arch-mismatch (dropped), two arch-match.
	seedSpec(t, store, "qwen2.5-7b-instruct", models.ArchQwen25, 7)
	seedSpec(t, store, "qwen2.5-0.5b-instruct", models.ArchQwen25, 0.5) // ratio 14×
	seedSpec(t, store, "qwen2.5-1.5b-instruct", models.ArchQwen25, 1.5) // ratio ~4.67×
	seedSpec(t, store, "llama-3-1b-instruct", models.ArchLlama3, 1)     // dropped

	var out bytes.Buffer
	d := &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 64 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		ModelStore:       store,
		Stdout:           &out,
		FS:               OSFileSystem{},
	}
	if err := runFitSpeculative(context.Background(), d, "qwen2.5-7b-instruct", 10); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "qwen2.5-0.5b-instruct") || !strings.Contains(s, "qwen2.5-1.5b-instruct") {
		t.Errorf("expected both qwen2 drafts in output:\n%s", s)
	}
	if strings.Contains(s, "llama-3-1b-instruct") {
		t.Errorf("llama-arch draft should be dropped from output:\n%s", s)
	}
}

func TestFitSpeculativeMainNotInstalled(t *testing.T) {
	tmp := t.TempDir()
	store := newFakeModelStore()
	d := &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 64 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		ModelStore:       store,
		FS:               OSFileSystem{},
	}
	err := runFitSpeculative(context.Background(), d, "nonexistent", 10)
	if err == nil {
		t.Fatal("expected error for missing main")
	}
	if !errors.Is(err, ErrUserError) {
		t.Errorf("expected ErrUserError; got %v", err)
	}
}

func TestFitSpeculativeEmptyCandidates(t *testing.T) {
	tmp := t.TempDir()
	store := newFakeModelStore()
	seedSpec(t, store, "qwen2.5-7b-instruct", models.ArchQwen25, 7)

	var out bytes.Buffer
	d := &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 64 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		ModelStore:       store,
		Stdout:           &out,
		FS:               OSFileSystem{},
	}
	if err := runFitSpeculative(context.Background(), d, "qwen2.5-7b-instruct", 10); err != nil {
		t.Fatalf("expected no error on empty candidates; got %v", err)
	}
	if !strings.Contains(out.String(), "no installed draft candidates") {
		t.Errorf("expected empty-candidates message; got:\n%s", out.String())
	}
}

func TestFitSpeculativeRatioOrder(t *testing.T) {
	tmp := t.TempDir()
	store := newFakeModelStore()
	// Main + three candidates whose ratios bracket the 7.5 midpoint.
	seedSpec(t, store, "qwen2.5-32b-instruct", models.ArchQwen25, 32)
	seedSpec(t, store, "draft-a", models.ArchQwen25, 4)    // ratio 8 → closest to 7.5
	seedSpec(t, store, "draft-b", models.ArchQwen25, 2.67) // ratio ~12
	seedSpec(t, store, "draft-c", models.ArchQwen25, 8)    // ratio 4

	var out bytes.Buffer
	d := &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 128 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		ModelStore:       store,
		Stdout:           &out,
		FS:               OSFileSystem{},
	}
	if err := runFitSpeculative(context.Background(), d, "qwen2.5-32b-instruct", 10); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	aIdx := strings.Index(s, "draft-a")
	bIdx := strings.Index(s, "draft-b")
	cIdx := strings.Index(s, "draft-c")
	// Expected order: a (|8-7.5|=0.5) first, then c (|4-7.5|=3.5), then b (|12-7.5|=4.5).
	if !(aIdx < cIdx && cIdx < bIdx) {
		t.Errorf("rows out of order: a=%d, b=%d, c=%d (want a < c < b)\n%s", aIdx, bIdx, cIdx, s)
	}
}

// seedSpec adds a model to the fakeModelStore with minimal metadata
// suitable for SpeculativePair tests.
func seedSpec(t *testing.T, store *fakeModelStore, id string, arch models.Arch, paramsB float64) {
	t.Helper()
	if err := store.Put(context.Background(), models.Metadata{
		ID:        id,
		Repo:      "fake/" + id,
		Quant:     models.Q4_K_M,
		GGUFPath:  "/fake/" + id + ".gguf",
		SizeBytes: int64(paramsB * 600_000_000),
		ParamsB:   paramsB,
		Arch:      arch,
	}); err != nil {
		t.Fatal(err)
	}
}
