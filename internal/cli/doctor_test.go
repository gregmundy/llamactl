package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
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

type fakeResolver struct {
	res server.Resolution
	err error
}

func (f *fakeResolver) Resolve(_ context.Context) (server.Resolution, error) {
	return f.res, f.err
}

type fakeProber struct {
	ver server.Version
	err error
}

func (f *fakeProber) Probe(_ context.Context, _ string) (server.Version, error) {
	return f.ver, f.err
}

func (f *fakeProber) Capabilities(_ context.Context, _ string) (server.Capabilities, error) {
	return server.Capabilities{}, nil
}

// healthyDoctorDeps returns a Deps wired so every doctor check passes.
// Individual tests override one field to drive a single check to fail.
func healthyDoctorDeps(t *testing.T) *Deps {
	t.Helper()
	return &Deps{
		HardwareDetector: &fakeDetector{info: hardware.Info{
			Chip:                "Apple M2 Pro",
			RAMBytes:            32 * 1024 * 1024 * 1024,
			IogpuWiredLimitMB:   24576,
			HypervisorPresent:   false,
			MetalDeviceDetected: true,
		}},
		ServerResolver: &fakeResolver{res: server.Resolution{
			Path: "/opt/homebrew/bin/llama-server", Source: server.SourcePATH,
		}},
		ServerProber:    &fakeProber{ver: server.Version{Build: 5000, SHA: "abc", Raw: "version: 5000 (abc)"}},
		LookPath:        func(string) (string, error) { return "", errors.New("not found") },
		Getenv:          func(string) string { return "" },
		Now:             func() time.Time { return time.Unix(1700000000, 0).UTC() },
		ModelStore:      newFakeModelStore(),
		FS:              OSFileSystem{},
		SharedModelsDir: t.TempDir(),
		LaunchdService:  &fakeLaunchdService{},
		PortAllocator:   &fakePortAllocator{},
		LaunchAgentsDir: t.TempDir(),
		Runner:          &noopDoctorRunner{},
	}
}

func TestDoctor_AllChecksPass(t *testing.T) {
	deps := healthyDoctorDeps(t)
	out, _, err := runRoot(t, deps, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\noutput:\n%s", err, out)
	}
	if strings.Contains(out, "✗") {
		t.Fatalf("expected no failures in healthy run, got:\n%s", out)
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "\nOK") {
		t.Fatalf("expected trailing OK, got:\n%s", out)
	}
}

func TestDoctor_RefusesOnVMWithoutMetal(t *testing.T) {
	deps := healthyDoctorDeps(t)
	deps.HardwareDetector = &fakeDetector{info: hardware.Info{
		Chip:                "Apple Virtual Machine",
		HypervisorPresent:   true,
		MetalDeviceDetected: false,
		RAMBytes:            16 * 1024 * 1024 * 1024,
	}}
	out, _, err := runRoot(t, deps, "doctor")
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("want ErrUserError, got %v", err)
	}
	if !strings.Contains(out, "VM without Metal") && !strings.Contains(out, "bare-metal") {
		t.Errorf("expected VM message, got:\n%s", out)
	}
}

func TestDoctor_VMOverrideAllowsRun(t *testing.T) {
	deps := healthyDoctorDeps(t)
	deps.HardwareDetector = &fakeDetector{info: hardware.Info{
		HypervisorPresent: true, MetalDeviceDetected: false,
		RAMBytes:          16 * 1024 * 1024 * 1024,
		IogpuWiredLimitMB: 12288,
	}}
	deps.Getenv = func(k string) string {
		if k == "LLAMACTL_ALLOW_VM" {
			return "1"
		}
		return ""
	}
	out, _, err := runRoot(t, deps, "doctor")
	if err != nil {
		t.Fatalf("doctor with override should pass: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "VM override") {
		t.Errorf("expected mention of VM override in output: %s", out)
	}
}

func TestDoctor_NoLlamaServer(t *testing.T) {
	deps := healthyDoctorDeps(t)
	deps.ServerResolver = &fakeResolver{err: server.ErrNotFound}
	out, _, err := runRoot(t, deps, "doctor")
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("want ErrUserError, got %v", err)
	}
	if !strings.Contains(out, "brew install llama.cpp") {
		t.Errorf("expected Homebrew suggestion, got:\n%s", out)
	}
	if !strings.Contains(out, "llamavm") {
		t.Errorf("expected llamavm suggestion, got:\n%s", out)
	}
}

func TestDoctor_LowLlamaServerVersionWarns(t *testing.T) {
	deps := healthyDoctorDeps(t)
	deps.ServerProber = &fakeProber{ver: server.Version{Build: 0, SHA: "old", Raw: "version: 0 (old)"}}
	out, _, err := runRoot(t, deps, "doctor")
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("want ErrUserError on old version, got %v", err)
	}
	if !strings.Contains(out, "MinLlamaServerBuild") && !strings.Contains(out, "minimum") {
		t.Errorf("expected min-version message, got:\n%s", out)
	}
}

func TestDoctor_IogpuUnsetWithLargeRAM(t *testing.T) {
	deps := healthyDoctorDeps(t)
	deps.HardwareDetector = &fakeDetector{info: hardware.Info{
		RAMBytes:            64 * 1024 * 1024 * 1024,
		IogpuWiredLimitMB:   0, // unset
		MetalDeviceDetected: true,
	}}
	out, _, err := runRoot(t, deps, "doctor")
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("want ErrUserError when iogpu unset on 64GB host: %v", err)
	}
	if !strings.Contains(out, "sudo sysctl") {
		t.Errorf("expected exact remediation command, got:\n%s", out)
	}
	if !strings.Contains(out, "iogpu.wired_limit_mb") {
		t.Errorf("expected sysctl key in output, got:\n%s", out)
	}
}

// noopDoctorRunner is the default Runner for doctor tests; returns nil
// for every command. Tailscale-specific tests override with a different Runner.
type noopDoctorRunner struct{}

func (noopDoctorRunner) Run(_ context.Context, _ string, _ []string, _ string, _, _ io.Writer) error {
	return nil
}

// tailscaleRunner returns a canned tailscale status response.
type tailscaleRunner struct {
	jsonOutput string
	runErr     error
}

func (r *tailscaleRunner) Run(_ context.Context, name string, _ []string, _ string, stdout, _ io.Writer) error {
	if r.runErr != nil {
		return r.runErr
	}
	if name == "tailscale" {
		_, _ = io.WriteString(stdout, r.jsonOutput)
	}
	return nil
}

func TestDoctor_PortConflicts_OK(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.x.plist")
	writeMinimalPlist(t, plistPath, 18080)
	l, _ := net.Listen("tcp", ":18080")
	if l == nil {
		t.Skip("could not bind :18080, skipping")
	}
	defer l.Close()

	d := healthyDoctorDeps(t)
	d.LaunchAgentsDir = tmp
	d.LaunchdService = &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{{Label: "com.llamactl.x", PlistPath: plistPath, PID: 12345, State: "running"}},
		Services:   map[string]launchd.ServiceInfo{"com.llamactl.x": {Label: "com.llamactl.x", PID: 12345, State: "running"}},
	}
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ port conflicts") {
		t.Errorf("port-conflicts should pass:\n%s", out)
	}
}

func TestDoctor_PortConflicts_Failure(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.x.plist")
	l, _ := net.Listen("tcp", ":0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	writeMinimalPlist(t, plistPath, port)

	d := healthyDoctorDeps(t)
	d.LaunchAgentsDir = tmp
	d.LaunchdService = &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{{Label: "com.llamactl.x", PlistPath: plistPath, PID: 12345, State: "running"}},
		Services:   map[string]launchd.ServiceInfo{"com.llamactl.x": {Label: "com.llamactl.x", PID: 12345, State: "running"}},
	}
	out, _, _ := runRoot(t, d, "doctor")
	if !strings.Contains(out, "✗ port conflicts") {
		t.Errorf("expected port-conflicts to fail:\n%s", out)
	}
}

// TestDoctor_PortConflicts_StoppedServiceNoFalsePositive verifies that a service
// whose plist is still on disk but whose PID is 0 (stopped, not running) does
// NOT trigger a port-conflict failure. Before the liveness filter was added,
// doctor would report "<id> loaded but port N is free" for any stopped service
// whose port happened to be unbound.
func TestDoctor_PortConflicts_StoppedServiceNoFalsePositive(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.gemma-4-e4b-it.plist")

	// Grab a free port and immediately release it so it stays unbound.
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Skip("could not bind :0, skipping")
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	writeMinimalPlist(t, plistPath, port)

	d := healthyDoctorDeps(t)
	d.LaunchAgentsDir = tmp
	// Stopped service: PID=0 in List result; Services map has the label with PID=0
	// so Print also returns PID=0.
	d.LaunchdService = &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{{
			Label:     "com.llamactl.gemma-4-e4b-it",
			PlistPath: plistPath,
			PID:       0, // stopped
			State:     "",
		}},
		Services: map[string]launchd.ServiceInfo{
			"com.llamactl.gemma-4-e4b-it": {
				Label: "com.llamactl.gemma-4-e4b-it",
				PID:   0, // stopped
			},
		},
	}
	out, _, err := runRoot(t, d, "doctor")
	if err != nil && strings.Contains(out, "✗ port conflicts") {
		t.Errorf("stopped service (PID=0) should NOT trigger port-conflict false positive:\n%s", out)
	}
}

// TestPortConflictsHealthyServingService verifies that when a service has a
// real TCP listener on its declared port, portConflictsCheck returns ✓.
// This is the regression test for the net.Listen/SO_REUSEADDR bug: the old
// probe could bind alongside an active listener and incorrectly flag a
// conflict. The Dial-based probe will succeed here, so no conflict is reported.
func TestPortConflictsHealthyServingService(t *testing.T) {
	// Bind a real TCP listener to simulate a running llama-server.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.test.plist")
	writeMinimalPlist(t, plistPath, port)

	fakeSvc := &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{{Label: "com.llamactl.test", PlistPath: plistPath, PID: 12345, State: "running"}},
		Services:   map[string]launchd.ServiceInfo{"com.llamactl.test": {Label: "com.llamactl.test", PID: 12345, State: "running"}},
	}
	deps := &Deps{LaunchdService: fakeSvc}
	check := portConflictsCheck(deps)
	ok, detail := check.run(context.Background(), deps)
	if !ok {
		t.Fatalf("expected ✓ for healthy serving service; got %q", detail)
	}
}

// TestPortConflictsServiceDownButLoaded verifies that when a service is
// "loaded" per launchctl (PID > 0) but its port is not actually listening,
// portConflictsCheck returns ✗.
func TestPortConflictsServiceDownButLoaded(t *testing.T) {
	// Grab a free port and immediately release it — so nothing is listening.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close() // free the port; no listener

	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.test.plist")
	writeMinimalPlist(t, plistPath, port)

	fakeSvc := &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{{Label: "com.llamactl.test", PlistPath: plistPath, PID: 12345, State: "running"}},
		Services:   map[string]launchd.ServiceInfo{"com.llamactl.test": {Label: "com.llamactl.test", PID: 12345, State: "running"}},
	}
	deps := &Deps{LaunchdService: fakeSvc}
	check := portConflictsCheck(deps)
	ok, detail := check.run(context.Background(), deps)
	if ok {
		t.Fatalf("expected ✗ for loaded-but-not-serving service; got ok=true detail=%q", detail)
	}
}

func TestDoctor_ModelFiles_OK(t *testing.T) {
	tmp := t.TempDir()
	gguf := filepath.Join(tmp, "model.gguf")
	if err := os.WriteFile(gguf, []byte(strings.Repeat("x", 1000)), 0o644); err != nil {
		t.Fatal(err)
	}
	d := healthyDoctorDeps(t)
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{ID: "x", GGUFPath: gguf, SizeBytes: 1000})
	d.ModelStore = store
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ model files match metadata") {
		t.Errorf("should pass:\n%s", out)
	}
}

func TestDoctor_ModelFiles_Failure(t *testing.T) {
	tmp := t.TempDir()
	gguf := filepath.Join(tmp, "model.gguf")
	_ = os.WriteFile(gguf, []byte("xxxx"), 0o644)
	d := healthyDoctorDeps(t)
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{ID: "x", GGUFPath: gguf, SizeBytes: 10_000_000})
	d.ModelStore = store
	out, _, _ := runRoot(t, d, "doctor")
	if !strings.Contains(out, "✗ model files match metadata") {
		t.Errorf("expected failure:\n%s", out)
	}
}

func TestDoctor_OrphanedMetadata_OK(t *testing.T) {
	tmp := t.TempDir()
	gguf := filepath.Join(tmp, "model.gguf")
	_ = os.WriteFile(gguf, []byte("xxx"), 0o644)
	d := healthyDoctorDeps(t)
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{ID: "x", GGUFPath: gguf, SizeBytes: 3})
	d.ModelStore = store
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ orphaned metadata") {
		t.Errorf("should pass:\n%s", out)
	}
}

func TestDoctor_OrphanedMetadata_Failure(t *testing.T) {
	d := healthyDoctorDeps(t)
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{ID: "x", GGUFPath: "/no/such/file.gguf", SizeBytes: 3})
	d.ModelStore = store
	out, _, _ := runRoot(t, d, "doctor")
	if !strings.Contains(out, "✗ orphaned metadata") {
		t.Errorf("expected failure:\n%s", out)
	}
}

func TestDoctor_DiskSpace_OK(t *testing.T) {
	d := healthyDoctorDeps(t)
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ disk space") {
		t.Errorf("should pass:\n%s", out)
	}
}

func TestDoctor_DiskSpace_Failure(t *testing.T) {
	d := healthyDoctorDeps(t)
	d.SharedModelsDir = "/no/such/path/at/all"
	out, _, _ := runRoot(t, d, "doctor")
	if !strings.Contains(out, "✗ disk space") {
		t.Errorf("expected failure (statfs on missing path):\n%s", out)
	}
}

// When SharedModelsDir doesn't exist (fresh install, before any `add`), the
// remediation should tell the user to `mkdir -p`, not to free up space.
func TestDoctor_DiskSpace_MissingDir_SuggestsMkdir(t *testing.T) {
	d := healthyDoctorDeps(t)
	missing := "/definitely-does-not-exist/llamactl-test-xyz"
	d.SharedModelsDir = missing
	out, _, _ := runRoot(t, d, "doctor")
	if !strings.Contains(out, "✗ disk space") {
		t.Fatalf("expected disk space check to fail:\n%s", out)
	}
	if !strings.Contains(out, "mkdir") {
		t.Errorf("expected remediation to mention 'mkdir' for missing dir; got:\n%s", out)
	}
	if !strings.Contains(out, missing) {
		t.Errorf("expected remediation to include the path %q; got:\n%s", missing, out)
	}
	if strings.Contains(out, "free up space") {
		t.Errorf("misleading 'free up space' remediation shown for a missing dir:\n%s", out)
	}
}

func TestDoctor_Tailscale_NotConfigured_Skipped(t *testing.T) {
	d := healthyDoctorDeps(t)
	out, _, _ := runRoot(t, d, "doctor")
	if !strings.Contains(out, "✓ tailscale") {
		t.Errorf("expected ✓ tailscale (skipped):\n%s", out)
	}
}

func TestDoctor_Tailscale_Online_OK(t *testing.T) {
	d := healthyDoctorDeps(t)
	d.LookPath = func(name string) (string, error) {
		if name == "tailscale" {
			return "/usr/local/bin/tailscale", nil
		}
		return "", os.ErrNotExist
	}
	d.Runner = &tailscaleRunner{jsonOutput: `{"Self":{"Online":true}}`}
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ tailscale") {
		t.Errorf("should pass:\n%s", out)
	}
}

func TestDoctor_Tailscale_Offline_Failure(t *testing.T) {
	d := healthyDoctorDeps(t)
	d.LookPath = func(name string) (string, error) {
		if name == "tailscale" {
			return "/usr/local/bin/tailscale", nil
		}
		return "", os.ErrNotExist
	}
	d.Runner = &tailscaleRunner{jsonOutput: `{"Self":{"Online":false}}`}
	out, _, _ := runRoot(t, d, "doctor")
	if !strings.Contains(out, "✗ tailscale") {
		t.Errorf("expected failure:\n%s", out)
	}
}

func TestDoctor_StalePlists_OK(t *testing.T) {
	tmp := t.TempDir()
	llamaServer := filepath.Join(tmp, "llama-server")
	_ = os.WriteFile(llamaServer, []byte("#!/bin/sh\n"), 0o755)
	plistPath := filepath.Join(tmp, "com.llamactl.x.plist")
	plistBody := `<plist><dict>
<key>ProgramArguments</key><array><string>` + llamaServer + `</string></array>
</dict></plist>`
	_ = os.WriteFile(plistPath, []byte(plistBody), 0o644)

	d := healthyDoctorDeps(t)
	d.LaunchAgentsDir = tmp
	d.LaunchdService = &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{{Label: "com.llamactl.x", PlistPath: plistPath}},
	}
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ stale plists") {
		t.Errorf("should pass:\n%s", out)
	}
}

func TestDoctor_LogFiles_NotConfigured_OK(t *testing.T) {
	d := healthyDoctorDeps(t)
	// healthyDoctorDeps leaves LogsDir empty.
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ Log files within size limit") {
		t.Errorf("should pass when LogsDir is unset:\n%s", out)
	}
	if !strings.Contains(out, "Log files within size limit") {
		t.Errorf("expected new log-size check in transcript:\n%s", out)
	}
}

func TestDoctor_LogFiles_OK(t *testing.T) {
	tmp := t.TempDir()
	// Small log file under the limit.
	if err := os.WriteFile(filepath.Join(tmp, "tiny.log"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := healthyDoctorDeps(t)
	d.LogsDir = tmp
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ Log files within size limit") {
		t.Errorf("should pass for under-limit log:\n%s", out)
	}
}

func TestDoctor_LogFiles_Oversized_Failure(t *testing.T) {
	tmp := t.TempDir()
	big := filepath.Join(tmp, "huge.log")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", (10<<20)+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	d := healthyDoctorDeps(t)
	d.LogsDir = tmp
	out, _, _ := runRoot(t, d, "doctor")
	if !strings.Contains(out, "✗ Log files within size limit") {
		t.Errorf("expected log-size check to fail:\n%s", out)
	}
	if !strings.Contains(out, "huge.log") {
		t.Errorf("expected oversized filename in detail:\n%s", out)
	}
}

func TestDoctor_HFCacheSize_NotConfigured_OK(t *testing.T) {
	d := healthyDoctorDeps(t)
	// HFCacheDir left empty.
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ HuggingFace API cache size") {
		t.Errorf("should pass when HFCacheDir is unset:\n%s", out)
	}
	if !strings.Contains(out, "HuggingFace API cache size") {
		t.Errorf("expected hf cache size check in transcript:\n%s", out)
	}
}

func TestDoctor_HFCacheSize_OK(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "small.json"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := healthyDoctorDeps(t)
	d.HFCacheDir = tmp
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ HuggingFace API cache size") {
		t.Errorf("should pass for under-limit cache:\n%s", out)
	}
}

func TestDoctor_HFCacheSize_Oversized_Failure(t *testing.T) {
	tmp := t.TempDir()
	big := filepath.Join(tmp, "big.json")
	// 501 MiB; safely past the 500 MiB threshold.
	if err := os.WriteFile(big, make([]byte, (500<<20)+1), 0o644); err != nil {
		t.Fatal(err)
	}
	d := healthyDoctorDeps(t)
	d.HFCacheDir = tmp
	out, _, _ := runRoot(t, d, "doctor")
	if !strings.Contains(out, "✗ HuggingFace API cache size") {
		t.Errorf("expected hf cache size check to fail:\n%s", out)
	}
	if !strings.Contains(out, "cache prune") {
		t.Errorf("expected remediation pointing at `cache prune`:\n%s", out)
	}
}

func TestDoctor_StalePlists_Failure(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.x.plist")
	plistBody := `<plist><dict>
<key>ProgramArguments</key><array><string>/no/such/llama-server</string></array>
</dict></plist>`
	_ = os.WriteFile(plistPath, []byte(plistBody), 0o644)

	d := healthyDoctorDeps(t)
	d.LaunchAgentsDir = tmp
	d.LaunchdService = &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{{Label: "com.llamactl.x", PlistPath: plistPath}},
	}
	out, _, _ := runRoot(t, d, "doctor")
	if !strings.Contains(out, "✗ stale plists") {
		t.Errorf("expected failure:\n%s", out)
	}
}

func TestAuthCheckPublicBindNoKey(t *testing.T) {
	tempDir := t.TempDir()
	publicNoKey := `<plist><array><string>--port</string><string>8080</string></array></plist>`
	os.WriteFile(filepath.Join(tempDir, "com.llamactl.foo.plist"), []byte(publicNoKey), 0o644)
	deps := &Deps{
		LaunchAgentsDir: tempDir,
		LaunchdService:  &fakeLaunchdService{ListResult: []launchd.ServiceInfo{{Label: "com.llamactl.foo", PID: 12345}}},
		Config:          &config.Config{},
		Getenv:          func(string) string { return "" },
	}
	check := authOnPublicBindCheck(deps)
	ok, detail := check.run(context.Background(), deps)
	if ok {
		t.Fatalf("expected ✗ for public bind without api_key; got detail=%q", detail)
	}
}

func TestAuthCheckPublicBindWithKey(t *testing.T) {
	tempDir := t.TempDir()
	publicWithKey := `<plist><array><string>--port</string><string>8080</string><string>--api-key</string><string>sk-XYZ</string></array></plist>`
	os.WriteFile(filepath.Join(tempDir, "com.llamactl.foo.plist"), []byte(publicWithKey), 0o644)
	deps := &Deps{
		LaunchAgentsDir: tempDir,
		LaunchdService:  &fakeLaunchdService{ListResult: []launchd.ServiceInfo{{Label: "com.llamactl.foo", PID: 12345}}},
		Config:          &config.Config{APIKey: "sk-XYZ"},
		Getenv:          func(string) string { return "" },
	}
	check := authOnPublicBindCheck(deps)
	ok, _ := check.run(context.Background(), deps)
	if !ok {
		t.Fatal("expected ✓ for public bind with api_key")
	}
}

func TestAuthCheckLoopbackBindNoKey(t *testing.T) {
	tempDir := t.TempDir()
	loopback := `<plist><array><string>--host</string><string>127.0.0.1</string><string>--port</string><string>8080</string></array></plist>`
	os.WriteFile(filepath.Join(tempDir, "com.llamactl.foo.plist"), []byte(loopback), 0o644)
	deps := &Deps{
		LaunchAgentsDir: tempDir,
		LaunchdService:  &fakeLaunchdService{ListResult: []launchd.ServiceInfo{{Label: "com.llamactl.foo", PID: 12345}}},
		Config:          &config.Config{},
		Getenv:          func(string) string { return "" },
	}
	check := authOnPublicBindCheck(deps)
	ok, _ := check.run(context.Background(), deps)
	if !ok {
		t.Fatal("expected ✓ for loopback bind regardless of api_key")
	}
}

func TestAuthCheckStoppedServiceSkipped(t *testing.T) {
	tempDir := t.TempDir()
	publicNoKey := `<plist><array><string>--port</string><string>8080</string></array></plist>`
	os.WriteFile(filepath.Join(tempDir, "com.llamactl.foo.plist"), []byte(publicNoKey), 0o644)
	deps := &Deps{
		LaunchAgentsDir: tempDir,
		LaunchdService:  &fakeLaunchdService{ListResult: []launchd.ServiceInfo{{Label: "com.llamactl.foo", PID: 0}}}, // stopped
		Config:          &config.Config{},
		Getenv:          func(string) string { return "" },
	}
	check := authOnPublicBindCheck(deps)
	ok, _ := check.run(context.Background(), deps)
	if !ok {
		t.Fatal("expected ✓ when only stopped services exist (no live public unauthenticated endpoint)")
	}
}

func TestLatestVersionCheckOnLatest(t *testing.T) {
	tempDir := t.TempDir()
	raw, _ := json.Marshal(versionCache{Latest: "1.3.0", CheckedAt: time.Now()})
	os.WriteFile(filepath.Join(tempDir, "last-version-check.json"), raw, 0o644)
	deps := &Deps{HFCacheDir: tempDir, LlamactlVersion: "v1.3.0", Now: time.Now}
	check := latestVersionCheck(deps)
	ok, detail := check.run(context.Background(), deps)
	if !ok {
		t.Fatal("expected pass")
	}
	if !strings.Contains(detail, "on latest") {
		t.Fatalf("missing 'on latest': %q", detail)
	}
}

func TestLatestVersionCheckUpdateAvailable(t *testing.T) {
	tempDir := t.TempDir()
	raw, _ := json.Marshal(versionCache{Latest: "1.4.0", CheckedAt: time.Now()})
	os.WriteFile(filepath.Join(tempDir, "last-version-check.json"), raw, 0o644)
	deps := &Deps{HFCacheDir: tempDir, LlamactlVersion: "v1.3.0", Now: time.Now}
	check := latestVersionCheck(deps)
	ok, detail := check.run(context.Background(), deps)
	if !ok {
		t.Fatal("expected pass (info-level)")
	}
	if !strings.Contains(detail, "update available") {
		t.Fatalf("missing 'update available': %q", detail)
	}
}

func TestLatestVersionCheckSoftPassOnMissingCache(t *testing.T) {
	deps := &Deps{HFCacheDir: filepath.Join(t.TempDir(), "no-cache-here"), LlamactlVersion: "v1.3.0", Now: time.Now}
	check := latestVersionCheck(deps)
	ok, detail := check.run(context.Background(), deps)
	if !ok {
		t.Fatal("expected soft-pass")
	}
	if !strings.Contains(detail, "skipped") {
		t.Fatalf("missing 'skipped': %q", detail)
	}
}

func TestLatestVersionCheckStaleCache(t *testing.T) {
	tempDir := t.TempDir()
	raw, _ := json.Marshal(versionCache{Latest: "1.3.0", CheckedAt: time.Now().Add(-48 * time.Hour)})
	os.WriteFile(filepath.Join(tempDir, "last-version-check.json"), raw, 0o644)
	deps := &Deps{HFCacheDir: tempDir, LlamactlVersion: "v1.3.0", Now: time.Now}
	check := latestVersionCheck(deps)
	ok, detail := check.run(context.Background(), deps)
	if !ok {
		t.Fatal("expected soft-pass")
	}
	if !strings.Contains(detail, "stale") {
		t.Fatalf("missing 'stale': %q", detail)
	}
}
