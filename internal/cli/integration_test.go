package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/download"
	"github.com/gregmundy/llamactl/internal/gguftest"
	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/launchd"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/proc"
	"github.com/gregmundy/llamactl/internal/server"
)

// intRunner is a fake CommandRunner satisfying both hardware.CommandRunner
// and server.CommandRunner — Go's structural typing means one fake satisfies
// both shapes.
type intRunner struct {
	outputs map[string]string
	errs    map[string]error
}

func (r *intRunner) Run(_ context.Context, name string, args []string, _ string, stdout, _ io.Writer) error {
	key := name
	if len(args) > 0 {
		key += " " + strings.Join(args, " ")
	}
	if err, ok := r.errs[key]; ok {
		return err
	}
	if out, ok := r.outputs[key]; ok {
		_, _ = io.WriteString(stdout, out)
		return nil
	}
	return os.ErrNotExist
}

func TestEndToEnd_HardwareThenDoctorOnHealthyHost(t *testing.T) {
	tmp := t.TempDir()

	// Touch a fake llama-server file so resolver's exists() check succeeds
	// (and the env-var branch wins discovery).
	binPath := filepath.Join(tmp, "fake", "llama-server")
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Hardware detector calls system_profiler with -json args. fakeRunner's
	// key construction in hardware_test.go uses only the first arg
	// ("SPHardwareDataType"), but this integration test uses the full args
	// joined — match that pattern.
	r := &intRunner{
		outputs: map[string]string{
			"system_profiler SPHardwareDataType -json": `{"SPHardwareDataType":[{"chip_type":"Apple M2 Pro"}]}`,
			"system_profiler SPDisplaysDataType -json": `{"SPDisplaysDataType":[{"_name":"d"}]}`,
			"sysctl hw.memsize":                        "hw.memsize: 34359738368\n",
			"sysctl iogpu.wired_limit_mb":              "iogpu.wired_limit_mb: 24576\n",
			"sysctl kern.hv_vmm_present":               "kern.hv_vmm_present: 0\n",
			"sw_vers -productVersion":                  "14.4.1\n",
			binPath + " --version":                     "version: 5000 (deadbeef)\n",
		},
		errs: map[string]error{},
	}

	deps := &Deps{
		Stdout:           &bytes.Buffer{},
		Stderr:           &bytes.Buffer{},
		HardwareDetector: &hardware.Detector{Runner: r},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		ServerResolver: server.Resolver{
			Getenv: func(k string) string {
				if k == "LLAMACTL_LLAMA_SERVER_PATH" {
					return binPath
				}
				return ""
			},
			LookPath:   func(string) (string, error) { return "", os.ErrNotExist },
			HomeDir:    tmp,
			ConfigPath: filepath.Join(tmp, "config.yaml"),
			Runner:     r,
		},
		ServerProber: &server.Prober{Runner: r},
		LookPath:     func(string) (string, error) { return "", os.ErrNotExist },
		Getenv: func(k string) string {
			if k == "LLAMACTL_LLAMA_SERVER_PATH" {
				return binPath
			}
			return ""
		},
		Now: func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}

	// Run hardware first.
	out, _, err := runRoot(t, deps, "hardware")
	if err != nil {
		t.Fatalf("hardware: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Apple M2 Pro") {
		t.Fatalf("hardware output missing chip:\n%s", out)
	}

	b, err := os.ReadFile(filepath.Join(tmp, "hardware.json"))
	if err != nil {
		t.Fatal(err)
	}
	var info hardware.Info
	if err := json.Unmarshal(b, &info); err != nil {
		t.Fatal(err)
	}
	if info.RAMBytes != 34359738368 {
		t.Errorf("RAMBytes = %d", info.RAMBytes)
	}

	// Then doctor.
	out2, _, err := runRoot(t, deps, "doctor")
	if err != nil {
		t.Fatalf("doctor failed on healthy host: %v\n%s", err, out2)
	}
	if !strings.HasSuffix(strings.TrimRight(out2, "\n"), "\nOK") {
		t.Fatalf("expected OK suffix:\n%s", out2)
	}
}

func TestIntegrationPhase2AddListRemove(t *testing.T) {
	body := []byte("integration bytes")
	sum := sha256.Sum256(body)
	shaHex := hex.EncodeToString(sum[:])

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/models/Qwen/Qwen2.5-7B-Instruct-GGUF"):
			fmt.Fprintf(w, `{"id":"Qwen/Qwen2.5-7B-Instruct-GGUF","siblings":[{"rfilename":"qwen2.5-7b-instruct-q4_k_m.gguf","lfs":{"sha256":"%s","size":%d}}]}`, shaHex, len(body))
		case strings.Contains(r.URL.Path, "/resolve/main/"):
			rng := r.Header.Get("Range")
			off := int64(0)
			if rng != "" {
				_, _ = fmt.Sscanf(rng, "bytes=%d-", &off)
				w.WriteHeader(http.StatusPartialContent)
			}
			w.Write(body[off:])
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(ts.Close)

	configDir := t.TempDir()
	sharedDir := t.TempDir()
	cacheDir := t.TempDir()

	hfClient := hf.NewClient(ts.URL, hf.NewCache(cacheDir), nil)
	store := models.NewFileStore(filepath.Join(configDir, "models"))

	d := &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 16 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(configDir, "hardware.json"),
		HFClient:         hfClient,
		Downloader:       &download.Downloader{Ranger: hfClient},
		QuantSelector:    SelectorAdapter{},
		ModelStore:       store,
		FS:               OSFileSystem{},
		ModelsConfigDir:  filepath.Join(configDir, "models"),
		SharedModelsDir:  sharedDir,
		Now:              fakeNow,
	}

	if _, _, err := runRoot(t, d, "add", "qwen2.5-7b-instruct"); err != nil {
		t.Fatalf("add: %v", err)
	}
	listOut, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listOut, "qwen2.5-7b-instruct") {
		t.Errorf("list output missing model:\n%s", listOut)
	}
	// Verify dedupe on re-add: no second on-disk download (mtime stable).
	gguf := filepath.Join(sharedDir, "qwen2.5-7b-instruct", "Q4_K_M.gguf")
	fi1, _ := os.Stat(gguf)
	if _, _, err := runRoot(t, d, "add", "qwen2.5-7b-instruct"); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	fi2, _ := os.Stat(gguf)
	if fi1.ModTime() != fi2.ModTime() {
		t.Errorf("re-add should not rewrite the file (dedupe fast path)")
	}
	// Remove --purge.
	if _, _, err := runRoot(t, d, "remove", "qwen2.5-7b-instruct", "--purge"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(gguf); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GGUF should be gone after --purge; err=%v", err)
	}
}

func TestIntegrationPhase25AddHFPath(t *testing.T) {
	// Build a synthetic GGUF body that the real gguf.ReadHeader will parse.
	body := gguftest.Build(t, 3,
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "qwen3"},
		gguftest.KV{Key: "general.parameter_count", Type: gguftest.TypeU64, Value: uint64(8030000000)},
	)
	sum := sha256.Sum256(body)
	shaHex := hex.EncodeToString(sum[:])

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/models/Qwen/Qwen3-8B-Instruct-GGUF"):
			fmt.Fprintf(w,
				`{"id":"Qwen/Qwen3-8B-Instruct-GGUF","siblings":[{"rfilename":"qwen3-8b-instruct-q4_k_m.gguf","lfs":{"sha256":"%s","size":%d}}]}`,
				shaHex, len(body))
		case strings.Contains(r.URL.Path, "/resolve/main/"):
			rng := r.Header.Get("Range")
			off := int64(0)
			if rng != "" {
				_, _ = fmt.Sscanf(rng, "bytes=%d-", &off)
				w.WriteHeader(http.StatusPartialContent)
			}
			w.Write(body[off:])
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(ts.Close)

	configDir := t.TempDir()
	sharedDir := t.TempDir()
	cacheDir := t.TempDir()
	hfClient := hf.NewClient(ts.URL, hf.NewCache(cacheDir), nil)
	store := models.NewFileStore(filepath.Join(configDir, "models"))
	d := &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 16 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(configDir, "hardware.json"),
		HFClient:         hfClient,
		Downloader:       &download.Downloader{Ranger: hfClient},
		QuantSelector:    SelectorAdapter{},
		ModelStore:       store,
		FS:               OSFileSystem{},
		ModelsConfigDir:  filepath.Join(configDir, "models"),
		SharedModelsDir:  sharedDir,
		Now:              fakeNow,
	}

	if _, _, err := runRoot(t, d, "add", "Qwen/Qwen3-8B-Instruct-GGUF", "--quant", "Q4_K_M"); err != nil {
		t.Fatalf("add: %v", err)
	}
	listOut, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listOut, "qwen3-8b-instruct") {
		t.Errorf("list missing derived id:\n%s", listOut)
	}
	if !strings.Contains(listOut, "8B") {
		t.Errorf("list missing 8B param from GGUF header:\n%s", listOut)
	}

	// Verify on-disk metadata captured ParamsB and Arch.
	got, err := store.Get(context.Background(), "qwen3-8b-instruct")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ParamsB != 8 {
		t.Errorf("ParamsB = %d, want 8", got.ParamsB)
	}
	if string(got.Arch) != "qwen3" {
		t.Errorf("Arch = %q, want qwen3", got.Arch)
	}

	// Clean up via remove --purge.
	if _, _, err := runRoot(t, d, "remove", "qwen3-8b-instruct", "--purge"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	gguf := filepath.Join(sharedDir, "qwen3-8b-instruct", "Q4_K_M.gguf")
	if _, err := os.Stat(gguf); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GGUF should be gone after --purge; err=%v", err)
	}
}

func buildFakeLlamaServer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "llama-server")
	cmd := exec.Command("go", "build", "-o", out, "./testdata/fakellamaserver")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build fake llama-server: %v", err)
	}
	return out
}

func TestIntegrationPhase3DetachedRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	store := models.NewFileStore(filepath.Join(tmp, "models"))
	_ = store.Put(context.Background(), models.Metadata{
		ID:        "qwen2.5-3b-instruct",
		Quant:     models.Q4_K_M,
		Repo:      "Qwen/Qwen2.5-3B-Instruct-GGUF",
		GGUFPath:  filepath.Join(tmp, "model.gguf"),
		SizeBytes: 1_900_000_000,
		ParamsB:   3,
		Arch:      models.ArchQwen25,
		AddedAt:   fakeNow(),
	})
	_ = os.WriteFile(filepath.Join(tmp, "model.gguf"), []byte("xxx"), 0o644)

	// Fake llama-server is built but we don't need to run it — Phase 3
	// detached path only writes a plist and shells out to launchctl
	// (which is faked here via fakeLaunchdService). Resolver returns the
	// fake binary path.
	fakeBin := buildFakeLlamaServer(t)

	ld := &fakeLaunchdService{Services: map[string]launchd.ServiceInfo{
		"com.llamactl.qwen2.5-3b-instruct": {
			Label: "com.llamactl.qwen2.5-3b-instruct", PID: 4242, State: "running",
		},
	}}
	d := &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 16 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		ServerResolver:   fakeResolverPhase3{Path: fakeBin},
		ServerProber:     fakeProberPhase3{Version: server.Version{Build: 4500}},
		ModelStore:       store,
		LaunchdService:   ld,
		PortAllocator:    proc.Allocator{},
		ProcInspector:    &fakeProcInspector{RSSByPID: map[int]int64{4242: 1024 * 1024}, UptimeByPID: map[int]time.Duration{4242: time.Minute}},
		TokRateReader:    &fakeTokRateReader{},
		LaunchAgentsDir:  filepath.Join(tmp, "LaunchAgents"),
		LogsDir:          filepath.Join(tmp, "Logs"),
		Now:              fakeNow,
		FS:               OSFileSystem{},
	}

	if _, _, err := runRoot(t, d, "serve", "qwen2.5-3b-instruct", "--detach"); err != nil {
		t.Fatalf("serve: %v", err)
	}
	plistPath := filepath.Join(d.LaunchAgentsDir, "com.llamactl.qwen2.5-3b-instruct.plist")
	if _, err := os.Stat(plistPath); err != nil {
		t.Fatalf("plist should exist: %v", err)
	}
	plistBytes, _ := os.ReadFile(plistPath)
	if !bytes.Contains(plistBytes, []byte("com.llamactl.qwen2.5-3b-instruct")) {
		t.Errorf("plist missing label:\n%s", plistBytes)
	}

	// status — service shows running.
	// Reuse the same ld which still returns the running PID.
	ld.ListResult = []launchd.ServiceInfo{{Label: "com.llamactl.qwen2.5-3b-instruct", PlistPath: plistPath, PID: 4242, State: "running"}}
	statusOut, _, err := runRoot(t, d, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(statusOut, "qwen2.5-3b-instruct") {
		t.Errorf("status missing model:\n%s", statusOut)
	}

	// stop — plist removed.
	if _, _, err := runRoot(t, d, "stop", "qwen2.5-3b-instruct"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Errorf("plist should be gone after stop; err=%v", err)
	}
}

// TestIntegrationPhase4ForegroundGracefulShutdown verifies that canceling
// the context during a foreground serve sends SIGTERM (not SIGKILL) to
// the child process and lets it log "shutting down" before exit.
func TestIntegrationPhase4ForegroundGracefulShutdown(t *testing.T) {
	tmp := t.TempDir()
	store := models.NewFileStore(filepath.Join(tmp, "models"))
	_ = store.Put(context.Background(), models.Metadata{
		ID:        "fake-tiny",
		Quant:     models.Q4_K_M,
		Repo:      "fake/fake",
		GGUFPath:  filepath.Join(tmp, "model.gguf"),
		SizeBytes: 1000,
		ParamsB:   1,
		Arch:      models.ArchQwen25,
		AddedAt:   fakeNow(),
	})
	_ = os.WriteFile(filepath.Join(tmp, "model.gguf"), []byte("xxx"), 0o644)

	fakeBin := buildFakeLlamaServer(t)
	logsDir := filepath.Join(tmp, "Logs")

	d := &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 16 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		ServerResolver:   fakeResolverPhase3{Path: fakeBin},
		ServerProber:     fakeProberPhase3{Version: server.Version{Build: 4500}, Caps: server.Capabilities{FlashAttnTristate: true}},
		ModelStore:       store,
		LaunchdService:   &fakeLaunchdService{},
		PortAllocator:    proc.Allocator{},
		ProcInspector:    &fakeProcInspector{},
		TokRateReader:    &fakeTokRateReader{},
		LaunchAgentsDir:  filepath.Join(tmp, "LaunchAgents"),
		LogsDir:          logsDir,
		Now:              fakeNow,
		FS:               OSFileSystem{},
		Stdout:           io.Discard,
		Stderr:           io.Discard,
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Run serve in a goroutine; it'll block on cmd.Run().
	done := make(chan error, 1)
	go func() {
		root := NewRoot(d, "test")
		root.SetArgs([]string{"serve", "fake-tiny"})
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		done <- root.ExecuteContext(ctx)
	}()

	// Wait for the fake binary to print "loaded model" to its log file.
	logPath := filepath.Join(logsDir, "fake-tiny.log")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(logPath)
		if bytes.Contains(data, []byte("loaded model")) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Cancel the context — should trigger SIGTERM via cmd.Cancel.
	cancel()

	// Wait for serve to return.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not return within 10s of context cancel")
	}

	// Log must contain "shutting down" — proves SIGTERM was delivered,
	// not SIGKILL (SIGKILL gives no chance to print).
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !bytes.Contains(data, []byte("shutting down")) {
		t.Errorf("log missing 'shutting down' — SIGTERM may not have been delivered\nlog:\n%s", data)
	}
}
