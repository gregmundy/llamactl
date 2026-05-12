package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotateIfLargeBelowThreshold(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.log")
	if err := os.WriteFile(p, []byte("small"), 0o644); err != nil {
		t.Fatal(err)
	}
	rotated, err := RotateIfLarge(p, 1<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	if rotated {
		t.Fatal("should not rotate")
	}
}

func TestRotateIfLargeAboveThreshold(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.log")
	if err := os.WriteFile(p, []byte(strings.Repeat("x", 2<<20)), 0o644); err != nil {
		t.Fatal(err)
	}
	rotated, err := RotateIfLarge(p, 1<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !rotated {
		t.Fatal("should rotate")
	}
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Fatalf("expected %s.1: %v", p, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be moved (gone); err=%v", p, err)
	}
}

func TestRotateIfLargeKeepBound(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.log")
	for _, suffix := range []string{"", ".1", ".2", ".3"} {
		if err := os.WriteFile(p+suffix, []byte(strings.Repeat("x", 2<<20)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rotated, err := RotateIfLarge(p, 1<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !rotated {
		t.Fatal("should rotate")
	}
	if _, err := os.Stat(p + ".4"); !os.IsNotExist(err) {
		t.Fatalf("p.4 should not exist; err=%v", err)
	}
	// p.1, p.2, p.3 should all exist.
	for _, suffix := range []string{".1", ".2", ".3"} {
		if _, err := os.Stat(p + suffix); err != nil {
			t.Fatalf("expected %s%s to exist: %v", p, suffix, err)
		}
	}
}

func TestRotateIfLargeMissingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nope.log")
	rotated, err := RotateIfLarge(p, 1<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	if rotated {
		t.Fatal("missing file should not rotate")
	}
}
