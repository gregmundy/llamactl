package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/gguftest"
	"github.com/gregmundy/llamactl/internal/models"
)

func TestListShowsAllEntriesWithStatus(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "qwen.gguf")
	if err := os.WriteFile(existing, []byte("xxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID: "qwen2.5-7b-instruct", Quant: models.Q4_K_M, SHA256: "abc",
		GGUFPath: existing, SizeBytes: 3, AddedAt: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	})
	_ = store.Put(context.Background(), models.Metadata{
		ID: "llama3.1-8b", Quant: models.Q4_K_M, SHA256: "def",
		GGUFPath: filepath.Join(dir, "missing.gguf"), SizeBytes: 1, AddedAt: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	})

	d := &Deps{ModelStore: store, FS: OSFileSystem{}}
	out, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "qwen2.5-7b-instruct") || !strings.Contains(out, "llama3.1-8b") {
		t.Errorf("output missing models:\n%s", out)
	}
	if !strings.Contains(out, "(missing)") {
		t.Errorf("output should mark missing GGUF:\n%s", out)
	}
}

func TestListEmpty(t *testing.T) {
	d := &Deps{ModelStore: newFakeModelStore(), FS: OSFileSystem{}}
	out, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "no models installed") {
		t.Errorf("output:\n%s", out)
	}
}

func TestListShowsLegacyMetadataWithoutParamsB(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "old.gguf")
	if err := os.WriteFile(existing, []byte("xxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeModelStore()
	// Pre-2.5 entry: no ParamsB or Arch.
	_ = store.Put(context.Background(), models.Metadata{
		ID: "legacy-model", Quant: models.Q4_K_M, GGUFPath: existing, SizeBytes: 3,
		AddedAt: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	})
	d := &Deps{ModelStore: store, FS: OSFileSystem{}}
	out, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "legacy-model") {
		t.Errorf("output missing legacy entry:\n%s", out)
	}
	// PARAMS column should show "?" for legacy entries with no ParamsB.
	if !strings.Contains(out, "?") {
		t.Errorf("PARAMS column should show '?' for unknown params:\n%s", out)
	}
	if strings.Contains(out, "0B") {
		t.Errorf("PARAMS column should not show '0B':\n%s", out)
	}
}

func TestListShowsParamsBForPhase25Entries(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "new.gguf")
	if err := os.WriteFile(existing, []byte("xxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID: "qwen3-8b-instruct", Quant: models.Q4_K_M, GGUFPath: existing, SizeBytes: 3,
		AddedAt: time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		ParamsB: 8, Arch: models.Arch("qwen3"),
	})
	d := &Deps{ModelStore: store, FS: OSFileSystem{}}
	out, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "8 B") {
		t.Errorf("output should show '8 B'; got:\n%s", out)
	}
}

func TestListShowsLastServedAt(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "new.gguf")
	if err := os.WriteFile(existing, []byte("xxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID: "qwen2.5-7b-instruct", Quant: models.Q4_K_M, GGUFPath: existing, SizeBytes: 3,
		AddedAt:      time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		LastServedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	})
	d := &Deps{ModelStore: store, FS: OSFileSystem{}}
	out, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "LAST-SERVED") {
		t.Errorf("output missing LAST-SERVED header:\n%s", out)
	}
	if !strings.Contains(out, "2026-05-11") {
		t.Errorf("output missing last-served date:\n%s", out)
	}
}

func TestListRendersSub1BParamsB(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "small.gguf")
	if err := os.WriteFile(existing, []byte("xxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID:        "qwen3-0.6b-instruct",
		Quant:     models.Q4_K_M,
		GGUFPath:  existing,
		SizeBytes: 3,
		AddedAt:   time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
		ParamsB:   0.6,
	})
	d := &Deps{ModelStore: store, FS: OSFileSystem{}}
	out, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "0.6 B") {
		t.Errorf("PARAMS column should show '0.6 B'; got:\n%s", out)
	}
}

func TestListShowsQuestionMarkForUnknownParams(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "x.gguf")
	if err := os.WriteFile(existing, []byte("xxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID:        "no-params",
		Quant:     models.Q4_K_M,
		GGUFPath:  existing,
		SizeBytes: 3,
		AddedAt:   time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		// ParamsB intentionally zero — simulates an HF-path add where the
		// GGUF parser couldn't determine param count.
	})
	d := &Deps{ModelStore: store, FS: OSFileSystem{}}
	out, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "?") {
		t.Errorf("expected '?' for unknown params:\n%s", out)
	}
}

func TestListSelfHealsZeroParamsB(t *testing.T) {
	tempDir := t.TempDir()
	ggufPath := filepath.Join(tempDir, "test", "Q5_K_M.gguf")
	if err := os.MkdirAll(filepath.Dir(ggufPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Build a real GGUF with parameter_count + architecture.
	ggufBytes := gguftest.Build(t, 3,
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "qwen3"},
		gguftest.KV{Key: "general.size_label", Type: gguftest.TypeString, Value: "3.4B"},
	)
	if err := os.WriteFile(ggufPath, ggufBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID:        "qwen3-3b-stale",
		Quant:     models.Q5_K_M,
		GGUFPath:  ggufPath,
		SizeBytes: int64(len(ggufBytes)),
		AddedAt:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		// ParamsB == 0 and Arch == "" — simulates stale pre-Phase-5 metadata.
	})

	d := &Deps{ModelStore: store, FS: OSFileSystem{}}
	out, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("list err: %v", err)
	}

	// After self-heal the PARAMS column should show a non-? value.
	// tabwriter separates columns with spaces; look for "?" surrounded by spaces.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "qwen3-3b-stale") {
			fields := strings.Fields(line)
			paramsField := ""
			// PARAMS is the 3rd column (index 2): MODEL-ID QUANT PARAMS SIZE PATH ADDED LAST-SERVED
			if len(fields) >= 3 {
				paramsField = fields[2]
			}
			if paramsField == "?" {
				t.Errorf("PARAMS column should be healed (not '?'); line: %q", line)
			}
			break
		}
	}

	// The store record should now have ParamsB != 0.
	list, listErr := store.List(context.Background())
	if listErr != nil {
		t.Fatalf("store.List: %v", listErr)
	}
	if len(list) == 0 || list[0].ParamsB == 0 {
		t.Errorf("store record ParamsB should be healed; got ParamsB=%v", list[0].ParamsB)
	}
}

func TestListSelfHealsViaTensorShape(t *testing.T) {
	tempDir := t.TempDir()
	ggufPath := filepath.Join(tempDir, "test", "Q4_K_M.gguf")
	if err := os.MkdirAll(filepath.Dir(ggufPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Build a synthetic GGUF: arch=qwen2, no parameter_count, no size_label,
	// but with a token_embd.weight tensor descriptor (3584 x 152064 = Qwen2.5-7B dims).
	ggufBytes := gguftest.BuildWithTensors(t, 3,
		[]gguftest.Tensor{
			{Name: "token_embd.weight", Dims: []uint64{3584, 152064}, Type: 0, Offset: 0},
		},
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "qwen2"},
		gguftest.KV{Key: "qwen2.block_count", Type: gguftest.TypeU32, Value: uint32(28)},
	)
	if err := os.WriteFile(ggufPath, ggufBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID:        "qwen2-7b-tensor-heal",
		Quant:     models.Q4_K_M,
		GGUFPath:  ggufPath,
		SizeBytes: int64(len(ggufBytes)),
		AddedAt:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		// ParamsB == 0 and Arch == "" — no parameter_count and no size_label in GGUF.
	})

	d := &Deps{ModelStore: store, FS: OSFileSystem{}}
	out, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("list err: %v", err)
	}

	// After self-heal the PARAMS column should show a non-? value.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "qwen2-7b-tensor-heal") {
			fields := strings.Fields(line)
			paramsField := ""
			// PARAMS is the 3rd column (index 2): MODEL-ID QUANT PARAMS SIZE PATH ADDED LAST-SERVED
			if len(fields) >= 3 {
				paramsField = fields[2]
			}
			if paramsField == "?" {
				t.Errorf("PARAMS column should be healed via tensor-shape (not '?'); line: %q", line)
			}
			break
		}
	}

	// The store record should now have ParamsB in [6.5, 9.0].
	// qwen2Params(3584, 152064, 28) ≈ 7.62 B for Qwen2.5-7B dims.
	list, listErr := store.List(context.Background())
	if listErr != nil {
		t.Fatalf("store.List: %v", listErr)
	}
	if len(list) == 0 {
		t.Fatal("store is empty after self-heal")
	}
	healed := list[0]
	if healed.ParamsB < 6.5 || healed.ParamsB > 9.0 {
		t.Errorf("healed ParamsB should be in [6.5, 9.0] B; got %v", healed.ParamsB)
	}
	t.Logf("healed ParamsB = %.4f B", healed.ParamsB)
}
