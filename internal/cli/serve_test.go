package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/launchd"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/server"
)

func makeServeDeps(t *testing.T) (*Deps, *fakeLaunchdService, *fakePortAllocator) {
	t.Helper()
	tmp := t.TempDir()
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID:        "qwen2.5-7b-instruct",
		Repo:      "Qwen/Qwen2.5-7B-Instruct-GGUF",
		Quant:     models.Q4_K_M,
		SHA256:    "abc",
		GGUFPath:  filepath.Join(tmp, "model.gguf"),
		SizeBytes: 4_400_000_000,
		AddedAt:   time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		ParamsB:   7,
		Arch:      models.ArchQwen25,
	})
	ld := &fakeLaunchdService{Services: map[string]launchd.ServiceInfo{}}
	alloc := &fakePortAllocator{Returns: map[int]int{}}
	d := &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 64 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		ServerResolver:   fakeResolverPhase3{Path: "/opt/homebrew/bin/llama-server"},
		ServerProber:     fakeProberPhase3{Version: server.Version{Build: 4500}},
		ModelStore:       store,
		LaunchdService:   ld,
		PortAllocator:    alloc,
		LaunchAgentsDir:  filepath.Join(tmp, "LaunchAgents"),
		LogsDir:          filepath.Join(tmp, "Logs"),
		Now:              fakeNow,
		FS:               OSFileSystem{},
	}
	return d, ld, alloc
}

// fakeResolverPhase3 and fakeProberPhase3 satisfy the Phase 1 interfaces.
type fakeResolverPhase3 struct{ Path string }

func (f fakeResolverPhase3) Resolve(_ context.Context) (server.Resolution, error) {
	return server.Resolution{Path: f.Path}, nil
}

type fakeProberPhase3 struct{ Version server.Version }

func (f fakeProberPhase3) Probe(_ context.Context, _ string) (server.Version, error) {
	return f.Version, nil
}

func TestServeUnknownModel(t *testing.T) {
	d, _, _ := makeServeDeps(t)
	_, _, err := runRoot(t, d, "serve", "nope")
	if err == nil || !strings.Contains(err.Error(), "is not installed") {
		t.Fatalf("err = %v, want 'is not installed'", err)
	}
}

func TestServeUnknownRecipe(t *testing.T) {
	d, _, _ := makeServeDeps(t)
	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--recipe", "nope")
	if err == nil || !strings.Contains(err.Error(), "unknown recipe") {
		t.Fatalf("err = %v, want 'unknown recipe'", err)
	}
}

func TestServeDetachedWritesPlistAndLoads(t *testing.T) {
	d, ld, alloc := makeServeDeps(t)
	ld.Services["com.llamactl.qwen2.5-7b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-7b-instruct",
		PID:   12345,
		State: "running",
	}
	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ld.Loaded) != 1 {
		t.Errorf("Load called %d times, want 1", len(ld.Loaded))
	}
	plistPath := filepath.Join(d.LaunchAgentsDir, "com.llamactl.qwen2.5-7b-instruct.plist")
	if ld.Loaded[0] != plistPath {
		t.Errorf("plist path = %q, want %q", ld.Loaded[0], plistPath)
	}
	if len(alloc.Allocated) != 1 || alloc.Allocated[0] != 8080 {
		t.Errorf("port allocator calls = %v, want [8080]", alloc.Allocated)
	}
}

func TestServePortShiftLoggedToStderr(t *testing.T) {
	d, ld, alloc := makeServeDeps(t)
	alloc.Returns[8080] = 8081
	ld.Services["com.llamactl.qwen2.5-7b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-7b-instruct",
		PID:   12345,
		State: "running",
	}
	_, stderr, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(stderr, "8081") || !strings.Contains(stderr, "8080") {
		t.Errorf("stderr should mention both ports; got: %q", stderr)
	}
}

func TestServeDetachedBootsOutExistingService(t *testing.T) {
	d, ld, _ := makeServeDeps(t)
	// Initial Print: service already running. After Bootout it's "stopped".
	ld.Services["com.llamactl.qwen2.5-7b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-7b-instruct",
		PID:   99999,
		State: "running",
	}
	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Verify Bootout was called for the existing service before Load.
	if len(ld.Booted) != 1 {
		t.Errorf("Bootout calls = %d, want 1", len(ld.Booted))
	}
}

func TestServeUpdatesLastServedAt(t *testing.T) {
	d, ld, _ := makeServeDeps(t)
	ld.Services["com.llamactl.qwen2.5-7b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-7b-instruct",
		PID:   12345,
		State: "running",
	}
	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	store := d.ModelStore.(*fakeModelStore)
	got, _ := store.Get(context.Background(), "qwen2.5-7b-instruct")
	if got.LastServedAt.IsZero() {
		t.Error("LastServedAt should be set after serve")
	}
}
