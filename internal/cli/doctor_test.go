package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/hardware"
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
		ServerProber: &fakeProber{ver: server.Version{Build: 5000, SHA: "abc", Raw: "version: 5000 (abc)"}},
		LookPath:     func(string) (string, error) { return "", errors.New("not found") },
		Getenv:       func(string) string { return "" },
		Now:          func() time.Time { return time.Unix(1700000000, 0).UTC() },
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
