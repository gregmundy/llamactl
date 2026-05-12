package launchd

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestListLLMServicesScansDirectory(t *testing.T) {
	dir := t.TempDir()
	// Three llamactl plists, one foreign plist, one non-plist file.
	for _, name := range []string{
		"com.llamactl.qwen2.5-7b-instruct.plist",
		"com.llamactl.llama3.2-3b.plist",
		"com.llamactl.mistral-7b-v0.3.plist",
		"com.example.other.plist",
		"README.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := &fakeRunner{}
	s := &Service{Runner: r, UID: 501}

	infos, err := ListLLMServices(context.Background(), dir, s)
	if err != nil {
		t.Fatalf("ListLLMServices: %v", err)
	}
	if len(infos) != 3 {
		t.Fatalf("len = %d, want 3", len(infos))
	}
	labels := make([]string, len(infos))
	for i, info := range infos {
		labels[i] = info.Label
	}
	sort.Strings(labels)
	want := []string{
		"com.llamactl.llama3.2-3b",
		"com.llamactl.mistral-7b-v0.3",
		"com.llamactl.qwen2.5-7b-instruct",
	}
	for i, w := range want {
		if labels[i] != w {
			t.Errorf("labels[%d] = %q, want %q", i, labels[i], w)
		}
	}
	for _, info := range infos {
		if info.PlistPath == "" {
			t.Errorf("PlistPath empty for %s", info.Label)
		}
	}
}

func TestListLLMServicesEmptyDir(t *testing.T) {
	dir := t.TempDir()
	r := &fakeRunner{}
	s := &Service{Runner: r, UID: 501}
	infos, err := ListLLMServices(context.Background(), dir, s)
	if err != nil {
		t.Fatalf("ListLLMServices: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("len = %d, want 0", len(infos))
	}
}

func TestListLLMServicesMissingDir(t *testing.T) {
	r := &fakeRunner{}
	s := &Service{Runner: r, UID: 501}
	infos, err := ListLLMServices(context.Background(), "/no/such/dir", s)
	if err != nil {
		t.Fatalf("ListLLMServices: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("len = %d, want 0", len(infos))
	}
}
