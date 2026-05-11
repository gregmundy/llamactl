package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/hardware"
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
