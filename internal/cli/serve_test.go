package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/config"
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

type fakeProberPhase3 struct {
	Version server.Version
	Caps    server.Capabilities
}

func (f fakeProberPhase3) Probe(_ context.Context, _ string) (server.Version, error) {
	return f.Version, nil
}

func (f fakeProberPhase3) Capabilities(_ context.Context, _ string) (server.Capabilities, error) {
	return f.Caps, nil
}

// The detached poll loop must honor ctx cancellation. Previously a bare
// time.Sleep ignored SIGINT, so a launchd service that never reached PID>0
// would hang for the full detachPollDeadline (5s). With select-on-ctx-or-timer,
// canceling the parent context breaks the poll immediately.
func TestRunServeDetachedHonorsCtxCancel(t *testing.T) {
	d, ld, _ := makeServeDeps(t)
	// Service never starts — Print always returns PID 0. We need to
	// disable the fake's "auto-populate PID on Load" behavior or the
	// poll loop will succeed immediately and never exercise ctx cancel.
	ld.Services = map[string]launchd.ServiceInfo{}
	ld.NoAutoLoadPID = true

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after we enter the poll loop.
	time.AfterFunc(50*time.Millisecond, cancel)

	start := time.Now()
	err := runServeDetached(ctx, d, "qwen2.5-7b-instruct",
		"/opt/homebrew/bin/llama-server", []string{"--port", "8080"},
		8080, "balanced")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected error from cancelled ctx, got nil")
	}
	// A bare time.Sleep loop would have pinned for the full 5s deadline.
	// 1s is a generous upper bound that still proves we honored cancel.
	if elapsed > 1*time.Second {
		t.Fatalf("loop did not honor ctx cancel: took %s (expected <1s)", elapsed)
	}
	// Cause attribution: error chain must mention cancellation, not the
	// "didn't start within" deadline message.
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Errorf("err = %v, want it to mention context canceled", err)
	}
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

func TestServePortZeroEphemeralMessage(t *testing.T) {
	d, ld, alloc := makeServeDeps(t)
	alloc.Returns[0] = 51234
	ld.Services["com.llamactl.qwen2.5-7b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-7b-instruct",
		PID:   12345,
		State: "running",
	}
	_, stderr, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach", "--port", "0")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(stderr, "bound to ephemeral :51234") {
		t.Errorf("stderr should mention 'bound to ephemeral :51234'; got: %q", stderr)
	}
	if strings.Contains(stderr, ":0 was in use") {
		t.Errorf("stderr must not say ':0 was in use'; got: %q", stderr)
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

// Reproducer for the v1.2.0 collision bug: a sibling com.llamactl.*.plist
// already claims port 8080. The new serve call must pass that port in
// the skip list so PortAllocator avoids it even though net.Listen on
// 8080 would succeed (the sibling's child llama-server is still loading
// and hasn't called bind() yet).
func TestServeDetachedSkipsSiblingPorts(t *testing.T) {
	d, ld, alloc := makeServeDeps(t)
	if err := os.MkdirAll(d.LaunchAgentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a sibling plist claiming port 8080.
	siblingPlist := `<plist><array><string>--port</string><string>8080</string></array></plist>`
	if err := os.WriteFile(
		filepath.Join(d.LaunchAgentsDir, "com.llamactl.other-model.plist"),
		[]byte(siblingPlist), 0o644); err != nil {
		t.Fatal(err)
	}
	ld.Services["com.llamactl.qwen2.5-7b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-7b-instruct",
		PID:   12345,
		State: "running",
	}
	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(alloc.Skipped) != 1 {
		t.Fatalf("allocator called %d times, want 1", len(alloc.Skipped))
	}
	skip := alloc.Skipped[0]
	if len(skip) != 1 || skip[0] != 8080 {
		t.Errorf("skip = %v, want [8080] (sibling's port)", skip)
	}
}

// Re-serving the same model id must NOT skip its own current port —
// otherwise rapid restarts would needlessly walk forward by one each time.
func TestServeDetachedDoesNotSkipOwnPort(t *testing.T) {
	d, ld, alloc := makeServeDeps(t)
	if err := os.MkdirAll(d.LaunchAgentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing plist for THIS model id on port 8080.
	ownPlist := `<plist><array><string>--port</string><string>8080</string></array></plist>`
	if err := os.WriteFile(
		filepath.Join(d.LaunchAgentsDir, "com.llamactl.qwen2.5-7b-instruct.plist"),
		[]byte(ownPlist), 0o644); err != nil {
		t.Fatal(err)
	}
	ld.Services["com.llamactl.qwen2.5-7b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-7b-instruct",
		PID:   12345,
		State: "running",
	}
	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(alloc.Skipped) != 1 {
		t.Fatalf("allocator called %d times, want 1", len(alloc.Skipped))
	}
	for _, p := range alloc.Skipped[0] {
		if p == 8080 {
			t.Errorf("own port 8080 should NOT be in skip list; got %v", alloc.Skipped[0])
		}
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

// TestRunServeDetachedSleepSeam verifies that runServeDetached uses the
// injected Sleep seam rather than the real time.After. With a frozen clock
// (Now returns t0 then t0+10s) and an always-closed Sleep channel, the
// function must return a deadline error quickly — not hang waiting for a
// real 250ms timer.
// TestServeDetachedUsesInjectedUserHomeDir verifies that runServeDetached uses
// Deps.UserHomeDir (when non-nil) to populate the plist WorkingDirectory,
// rather than calling os.UserHomeDir() directly. This lets tests point the
// plist's WorkingDirectory at a tempDir-rooted fake home.
func TestServeDetachedUsesInjectedUserHomeDir(t *testing.T) {
	d, ld, _ := makeServeDeps(t)
	tempHome := t.TempDir()
	d.UserHomeDir = func() (string, error) { return tempHome, nil }

	ld.Services["com.llamactl.qwen2.5-7b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-7b-instruct",
		PID:   12345,
		State: "running",
	}

	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	plistPath := filepath.Join(d.LaunchAgentsDir, "com.llamactl.qwen2.5-7b-instruct.plist")
	data, readErr := os.ReadFile(plistPath)
	if readErr != nil {
		t.Fatalf("could not read plist: %v", readErr)
	}
	if !strings.Contains(string(data), tempHome) {
		t.Errorf("plist WorkingDirectory should contain injected tempHome %q; plist:\n%s", tempHome, data)
	}
}

// TestServeAppendsAPIKeyFromConfig verifies that when Config.APIKey is set and
// no LLAMACTL_API_KEY env var is present, runServe appends --api-key <token>
// to the llama-server argv (and therefore into the plist ProgramArguments).
func TestServeAppendsAPIKeyFromConfig(t *testing.T) {
	d, ld, _ := makeServeDeps(t)
	d.Config = &config.Config{APIKey: "sk-from-config"}
	d.Getenv = func(key string) string { return "" } // env empty → fallback to config
	ld.Services["com.llamactl.qwen2.5-7b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-7b-instruct",
		PID:   12345,
		State: "running",
	}

	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	plistPath := filepath.Join(d.LaunchAgentsDir, "com.llamactl.qwen2.5-7b-instruct.plist")
	data, readErr := os.ReadFile(plistPath)
	if readErr != nil {
		t.Fatalf("could not read plist: %v", readErr)
	}
	body := string(data)
	if !strings.Contains(body, "--api-key") {
		t.Errorf("plist should contain --api-key; got:\n%s", body)
	}
	if !strings.Contains(body, "sk-from-config") {
		t.Errorf("plist should contain sk-from-config; got:\n%s", body)
	}
}

// TestServeEnvAPIKeyBeatsConfig verifies that LLAMACTL_API_KEY env var takes
// precedence over Config.APIKey when both are set.
func TestServeEnvAPIKeyBeatsConfig(t *testing.T) {
	d, ld, _ := makeServeDeps(t)
	d.Config = &config.Config{APIKey: "sk-from-config"}
	d.Getenv = func(key string) string {
		if key == "LLAMACTL_API_KEY" {
			return "sk-from-env"
		}
		return ""
	}
	ld.Services["com.llamactl.qwen2.5-7b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-7b-instruct",
		PID:   12345,
		State: "running",
	}

	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	plistPath := filepath.Join(d.LaunchAgentsDir, "com.llamactl.qwen2.5-7b-instruct.plist")
	data, readErr := os.ReadFile(plistPath)
	if readErr != nil {
		t.Fatalf("could not read plist: %v", readErr)
	}
	body := string(data)
	if !strings.Contains(body, "sk-from-env") {
		t.Errorf("plist should contain sk-from-env; got:\n%s", body)
	}
	if strings.Contains(body, "sk-from-config") {
		t.Errorf("plist must NOT contain sk-from-config (env beats config); got:\n%s", body)
	}
}

// TestServeNoAPIKeyWhenUnset verifies that when neither env nor config provide
// an API key, --api-key is not present in the plist at all.
func TestServeNoAPIKeyWhenUnset(t *testing.T) {
	d, ld, _ := makeServeDeps(t)
	// Config nil + Getenv returning empty → no api key
	d.Config = nil
	d.Getenv = func(key string) string { return "" }
	ld.Services["com.llamactl.qwen2.5-7b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-7b-instruct",
		PID:   12345,
		State: "running",
	}

	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	plistPath := filepath.Join(d.LaunchAgentsDir, "com.llamactl.qwen2.5-7b-instruct.plist")
	data, readErr := os.ReadFile(plistPath)
	if readErr != nil {
		t.Fatalf("could not read plist: %v", readErr)
	}
	if strings.Contains(string(data), "--api-key") {
		t.Errorf("plist must NOT contain --api-key when unset; got:\n%s", string(data))
	}
}

func TestRunServeDetachedSleepSeam(t *testing.T) {
	d, ld, _ := makeServeDeps(t)

	t0 := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	callCount := 0
	d.Now = func() time.Time {
		callCount++
		if callCount == 1 {
			return t0
		}
		// Second call (inside the loop's deadline check): past the deadline.
		return t0.Add(10 * time.Second)
	}

	// Sleep returns an already-closed channel so the select never blocks.
	closed := make(chan time.Time)
	close(closed)
	d.Sleep = func(dur time.Duration) <-chan time.Time {
		return closed
	}

	// Service never starts (Print always returns PID=0). Need to disable
	// the fake's auto-populate-on-Load behavior or the poll exits
	// immediately with a "started" message instead of testing the deadline.
	ld.NoAutoLoadPID = true

	done := make(chan error, 1)
	go func() {
		err := runServeDetached(context.Background(), d,
			"qwen2.5-7b-instruct",
			"/opt/homebrew/bin/llama-server",
			[]string{"--port", "8080"},
			8080, "balanced")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a deadline error, got nil")
		}
		if !strings.Contains(err.Error(), "didn't start within") {
			t.Errorf("err = %v; want message containing \"didn't start within\"", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("runServeDetached did not return within 1s — Sleep seam not honoured")
	}
}

func TestServeWithDraftAppendsModelDraftFlag(t *testing.T) {
	d, ld, _ := makeServeDeps(t)
	// Seed an additional draft model (main is already seeded by makeServeDeps).
	store := d.ModelStore.(*fakeModelStore)
	if err := store.Put(context.Background(), models.Metadata{
		ID:        "qwen2.5-0.5b-instruct",
		Repo:      "Qwen/Qwen2.5-0.5B-Instruct-GGUF",
		Quant:     models.Q4_K_M,
		GGUFPath:  filepath.Join(t.TempDir(), "draft.gguf"),
		SizeBytes: 400_000_000,
		ParamsB:   0.5,
		Arch:      models.ArchQwen25,
	}); err != nil {
		t.Fatal(err)
	}
	ld.Services["com.llamactl.qwen2.5-7b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-7b-instruct",
		PID:   12345,
		State: "running",
	}

	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach", "--draft", "qwen2.5-0.5b-instruct")
	if err != nil {
		t.Fatalf("runRoot: %v", err)
	}

	plistPath := filepath.Join(d.LaunchAgentsDir, "com.llamactl.qwen2.5-7b-instruct.plist")
	data, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "<string>--model-draft</string>") {
		t.Errorf("plist missing --model-draft arg:\n%s", s)
	}
	if !strings.Contains(s, "<string>--ctx-size-draft</string>") {
		t.Errorf("plist missing --ctx-size-draft arg:\n%s", s)
	}
}

func TestServeDraftNotInstalled(t *testing.T) {
	d, _, _ := makeServeDeps(t)
	// Main is installed; draft is not.

	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach", "--draft", "missing-draft-id")
	if err == nil {
		t.Fatal("expected error for missing draft id")
	}
	if !errors.Is(err, ErrUserError) {
		t.Errorf("expected ErrUserError; got %v", err)
	}
	if !strings.Contains(err.Error(), "missing-draft-id") {
		t.Errorf("error should name missing draft id; got %v", err)
	}
	if !strings.Contains(err.Error(), "llamactl add") {
		t.Errorf("error should suggest `llamactl add`; got %v", err)
	}
}

func TestServeDraftArchMismatch(t *testing.T) {
	d, _, _ := makeServeDeps(t)
	store := d.ModelStore.(*fakeModelStore)
	if err := store.Put(context.Background(), models.Metadata{
		ID:        "llama-3-1b-instruct",
		Repo:      "fake/llama-3-1b",
		Quant:     models.Q4_K_M,
		GGUFPath:  filepath.Join(t.TempDir(), "llama.gguf"),
		SizeBytes: 800_000_000,
		ParamsB:   1,
		Arch:      models.ArchLlama3,
	}); err != nil {
		t.Fatal(err)
	}

	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach", "--draft", "llama-3-1b-instruct")
	if err == nil {
		t.Fatal("expected error for arch mismatch")
	}
	if !errors.Is(err, ErrUserError) {
		t.Errorf("expected ErrUserError; got %v", err)
	}
	if !strings.Contains(err.Error(), "arch") {
		t.Errorf("error should mention arch mismatch; got %v", err)
	}
}

func TestServeDraftCombinedRAMTooBig(t *testing.T) {
	d, _, _ := makeServeDeps(t)
	// Override hardware to a tiny host.
	d.HardwareDetector = fakeHardwareDetector{Info: hardware.Info{RAMBytes: 8 * (1 << 30)}}

	store := d.ModelStore.(*fakeModelStore)
	// Add a 32B model (too big even before draft).
	if err := store.Put(context.Background(), models.Metadata{
		ID:        "qwen2.5-32b-instruct",
		Repo:      "Qwen/Qwen2.5-32B-Instruct-GGUF",
		Quant:     models.Q4_K_M,
		GGUFPath:  filepath.Join(t.TempDir(), "main.gguf"),
		SizeBytes: 20_000_000_000,
		ParamsB:   32,
		Arch:      models.ArchQwen25,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), models.Metadata{
		ID:        "qwen2.5-3b-instruct",
		Repo:      "Qwen/Qwen2.5-3B-Instruct-GGUF",
		Quant:     models.Q4_K_M,
		GGUFPath:  filepath.Join(t.TempDir(), "draft.gguf"),
		SizeBytes: 2_000_000_000,
		ParamsB:   3,
		Arch:      models.ArchQwen25,
	}); err != nil {
		t.Fatal(err)
	}

	_, _, err := runRoot(t, d, "serve", "qwen2.5-32b-instruct", "--detach", "--draft", "qwen2.5-3b-instruct")
	if err == nil {
		t.Fatal("expected error for RAM exhaustion")
	}
	if !errors.Is(err, ErrUserError) {
		t.Errorf("expected ErrUserError; got %v", err)
	}
	if !strings.Contains(err.Error(), "RAM") {
		t.Errorf("error should mention RAM; got %v", err)
	}
}

func TestServeDraftWarnsOnRatioOutsideRange(t *testing.T) {
	d, ld, _ := makeServeDeps(t)
	store := d.ModelStore.(*fakeModelStore)
	if err := store.Put(context.Background(), models.Metadata{
		ID:        "qwen2.5-32b-instruct",
		Repo:      "Qwen/Qwen2.5-32B-Instruct-GGUF",
		Quant:     models.Q4_K_M,
		GGUFPath:  filepath.Join(t.TempDir(), "main.gguf"),
		SizeBytes: 20_000_000_000,
		ParamsB:   32,
		Arch:      models.ArchQwen25,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), models.Metadata{
		ID:        "qwen2.5-0.5b-instruct",
		Repo:      "Qwen/Qwen2.5-0.5B-Instruct-GGUF",
		Quant:     models.Q4_K_M,
		GGUFPath:  filepath.Join(t.TempDir(), "draft.gguf"),
		SizeBytes: 400_000_000,
		ParamsB:   0.5,
		Arch:      models.ArchQwen25,
	}); err != nil {
		t.Fatal(err)
	}
	ld.Services["com.llamactl.qwen2.5-32b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-32b-instruct",
		PID:   12345,
		State: "running",
	}

	// Ratio 32/0.5 = 64× — outside the 5-15× warning band.
	_, stderr, err := runRoot(t, d, "serve", "qwen2.5-32b-instruct", "--detach", "--draft", "qwen2.5-0.5b-instruct")
	if err != nil {
		t.Fatalf("expected no error (warning only); got %v", err)
	}
	if !strings.Contains(stderr, "warning") {
		t.Errorf("expected stderr warning at ratio 64×; got: %s", stderr)
	}
}

func TestServeDetachedDraftEmbedsInPlist(t *testing.T) {
	d, ld, _ := makeServeDeps(t)
	store := d.ModelStore.(*fakeModelStore)
	draftPath := filepath.Join(t.TempDir(), "draft.gguf")
	if err := store.Put(context.Background(), models.Metadata{
		ID:        "qwen2.5-0.5b-instruct",
		Repo:      "Qwen/Qwen2.5-0.5B-Instruct-GGUF",
		Quant:     models.Q4_K_M,
		GGUFPath:  draftPath,
		SizeBytes: 400_000_000,
		ParamsB:   0.5,
		Arch:      models.ArchQwen25,
	}); err != nil {
		t.Fatal(err)
	}
	ld.Services["com.llamactl.qwen2.5-7b-instruct"] = launchd.ServiceInfo{
		Label: "com.llamactl.qwen2.5-7b-instruct",
		PID:   12345,
		State: "running",
	}

	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach", "--draft", "qwen2.5-0.5b-instruct")
	if err != nil {
		t.Fatal(err)
	}

	gotPath, ok := launchd.HasDraft(d.LaunchAgentsDir, "com.llamactl.qwen2.5-7b-instruct")
	if !ok {
		t.Fatalf("HasDraft returned ok=false for serve --draft")
	}
	if gotPath != draftPath {
		t.Errorf("HasDraft path=%q, want %q", gotPath, draftPath)
	}
}

// --- v1.5.0: run names + bootout race ---

// TestServeDefaultsNameToModelID confirms the single-instance UX is
// preserved: omitting --name uses model id, so plist and log paths
// match the pre-v1.5.0 convention. Anyone scripting against the old
// behavior keeps working.
func TestServeDefaultsNameToModelID(t *testing.T) {
	d, _, _ := makeServeDeps(t)
	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	plistPath := filepath.Join(d.LaunchAgentsDir, "com.llamactl.qwen2.5-7b-instruct.plist")
	if _, err := os.Stat(plistPath); err != nil {
		t.Errorf("plist not at default-name path: %v", err)
	}
}

// TestServeWithExplicitNameWritesDistinctPlist is the primary v1.5.0
// feature test: --name foo derives label com.llamactl.foo, distinct
// from the model-id-named default.
func TestServeWithExplicitNameWritesDistinctPlist(t *testing.T) {
	d, _, _ := makeServeDeps(t)
	_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach", "--name", "agent-1")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	wantPath := filepath.Join(d.LaunchAgentsDir, "com.llamactl.agent-1.plist")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("plist not at --name path %s: %v", wantPath, err)
	}
	// The default-name path should NOT exist — the run uses agent-1.
	defaultPath := filepath.Join(d.LaunchAgentsDir, "com.llamactl.qwen2.5-7b-instruct.plist")
	if _, err := os.Stat(defaultPath); err == nil {
		t.Errorf("unexpected plist at default-name path %s", defaultPath)
	}
}

// TestServeTwoNamesSameModelCoexist verifies the multi-instance use
// case from the v1.5.0 user report: same model, two --name values,
// both run in parallel with distinct plists.
func TestServeTwoNamesSameModelCoexist(t *testing.T) {
	d, _, _ := makeServeDeps(t)
	if _, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach", "--name", "chat-run"); err != nil {
		t.Fatalf("first serve: %v", err)
	}
	if _, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach", "--name", "agent-run"); err != nil {
		t.Fatalf("second serve: %v", err)
	}
	for _, name := range []string{"chat-run", "agent-run"} {
		p := filepath.Join(d.LaunchAgentsDir, "com.llamactl."+name+".plist")
		if _, err := os.Stat(p); err != nil {
			t.Errorf("plist for %s missing: %v", name, err)
		}
	}
}

// TestServeRejectsInvalidName: validation catches launchd-unsafe names.
func TestServeRejectsInvalidName(t *testing.T) {
	d, _, _ := makeServeDeps(t)
	// Empty --name is intentionally treated as "no flag" and falls back
	// to the model id (which validates fine). The bad cases here all
	// fail the launchd-safe-charset / leading-alphanumeric rules.
	bad := []string{
		"has spaces",
		"slash/here",
		".dot-prefix",
		"-dash-prefix",
	}
	for _, name := range bad {
		_, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach", "--name", name)
		if err == nil {
			t.Errorf("name %q should have been rejected", name)
		}
	}
}

// TestServeSameNameSecondTimeReplacesAtomically pins the v1.5.0 bootout
// race fix: re-serving the same name booots out the existing service,
// waits for launchctl teardown, then re-bootstraps. Pre-v1.5.0 this
// exit-5'd on the bootstrap because Bootout was async.
func TestServeSameNameSecondTimeReplacesAtomically(t *testing.T) {
	d, ld, _ := makeServeDeps(t)
	// First serve.
	if _, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach"); err != nil {
		t.Fatalf("first serve: %v", err)
	}
	beforeBooted := len(ld.Booted)
	// Second serve (same name; default = model id).
	if _, _, err := runRoot(t, d, "serve", "qwen2.5-7b-instruct", "--detach"); err != nil {
		t.Fatalf("second serve: %v", err)
	}
	// Bootout must have been called exactly once between the two serves.
	if got := len(ld.Booted) - beforeBooted; got != 1 {
		t.Errorf("Booted growth = %d, want 1 (existing service should have been bootouted)", got)
	}
}
