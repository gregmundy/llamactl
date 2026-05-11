package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/models"
)

func setupRemove(t *testing.T) (*Deps, *fakeModelStore, string) {
	t.Helper()
	dir := t.TempDir()
	gguf := filepath.Join(dir, "qwen.gguf")
	if err := os.WriteFile(gguf, []byte("xxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID: "qwen2.5-7b-instruct", Quant: models.Q4_K_M, GGUFPath: gguf, SizeBytes: 3,
	})
	d := &Deps{ModelStore: store, FS: OSFileSystem{}}
	return d, store, gguf
}

func TestRemoveMetadataOnly(t *testing.T) {
	d, store, gguf := setupRemove(t)
	if _, _, err := runRoot(t, d, "remove", "qwen2.5-7b-instruct"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := store.Get(context.Background(), "qwen2.5-7b-instruct"); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("metadata still present")
	}
	if _, err := os.Stat(gguf); err != nil {
		t.Errorf("GGUF should remain (no --purge); err=%v", err)
	}
}

func TestRemovePurgeDeletesGGUF(t *testing.T) {
	d, store, gguf := setupRemove(t)
	if _, _, err := runRoot(t, d, "remove", "qwen2.5-7b-instruct", "--purge"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := os.Stat(gguf); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GGUF should be deleted; err=%v", err)
	}
	if _, err := store.Get(context.Background(), "qwen2.5-7b-instruct"); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("metadata still present")
	}
}

func TestRemovePurgeRefusesIfPartialExists(t *testing.T) {
	d, _, gguf := setupRemove(t)
	if err := os.WriteFile(gguf+".partial", []byte("incomplete"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := runRoot(t, d, "remove", "qwen2.5-7b-instruct", "--purge")
	if err == nil || !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("err = %v, want 'in progress'", err)
	}
}

func TestRemoveUnknownModelErrors(t *testing.T) {
	d, _, _ := setupRemove(t)
	_, _, err := runRoot(t, d, "remove", "nope")
	if err == nil {
		t.Fatal("expected error")
	}
}
