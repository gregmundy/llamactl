package models

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFileStorePutGetListDelete(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)
	ctx := context.Background()

	m := Metadata{
		ID:        "qwen2.5-7b-instruct",
		Repo:      "Qwen/Qwen2.5-7B-Instruct-GGUF",
		Quant:     Q4_K_M,
		SHA256:    "deadbeef",
		GGUFPath:  "/tmp/x.gguf",
		SizeBytes: 4_000_000_000,
		AddedAt:   time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	}
	if err := s.Put(ctx, m); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(ctx, m.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SHA256 != m.SHA256 || got.Quant != m.Quant {
		t.Errorf("Get returned %+v, want fields matching %+v", got, m)
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}

	if err := s.Delete(ctx, m.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: err = %v, want ErrNotFound", err)
	}
}

func TestFileStoreGetMissingReturnsErrNotFound(t *testing.T) {
	s := NewFileStore(t.TempDir())
	_, err := s.Get(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestFileStoreListEmpty(t *testing.T) {
	s := NewFileStore(t.TempDir())
	list, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("List len = %d, want 0", len(list))
	}
}
