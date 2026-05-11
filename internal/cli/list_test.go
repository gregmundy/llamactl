package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	// PARAMS column should be blank for legacy entries — must not show stray "0B".
	if strings.Contains(out, "0B") {
		t.Errorf("PARAMS column should be blank, not '0B':\n%s", out)
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
	if !strings.Contains(out, "8B") {
		t.Errorf("output should show 8B; got:\n%s", out)
	}
}
