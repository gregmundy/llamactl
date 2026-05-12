package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCachePruneAll(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stale.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	d := &Deps{
		HFCacheDir: dir,
		Stdout:     &out,
		Stderr:     io.Discard,
	}
	cmd := newCacheCmd(d)
	cmd.SetArgs([]string{"prune", "--all"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.json")); !os.IsNotExist(err) {
		t.Fatal("expected stale.json removed")
	}
}

func TestCachePruneAlsoGCsEmptyNamespaces(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "hf-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "hf-new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hf-new", "x.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Deps{HFCacheDir: dir, Stdout: io.Discard, Stderr: io.Discard}
	cmd := newCacheCmd(d)
	cmd.SetArgs([]string{"prune", "--all"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hf-old")); !os.IsNotExist(err) {
		t.Fatal("hf-old should be GC'd after prune --all")
	}
}

func TestCachePruneDefault(t *testing.T) {
	dir := t.TempDir()
	oldP := filepath.Join(dir, "old.json")
	if err := os.WriteFile(oldP, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-60 * 24 * time.Hour)
	if err := os.Chtimes(oldP, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	freshP := filepath.Join(dir, "fresh.json")
	if err := os.WriteFile(freshP, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	d := &Deps{HFCacheDir: dir, Stdout: &out, Stderr: io.Discard}
	cmd := newCacheCmd(d)
	cmd.SetArgs([]string{"prune"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldP); !os.IsNotExist(err) {
		t.Fatal("old should be gone")
	}
	if _, err := os.Stat(freshP); err != nil {
		t.Fatal("fresh should remain")
	}
}
