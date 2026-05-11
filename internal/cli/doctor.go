package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/server"
)

func newDoctorCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:           "doctor",
		Short:         "Diagnose the llamactl environment",
		SilenceErrors: true, // doctor prints its own transcript
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd.Context(), deps)
		},
	}
}

// doctorCheck is one row in the doctor transcript. run returns (ok, detail);
// detail is printed alongside the label whether ok or not. remediation is
// printed only on failure.
type doctorCheck struct {
	label       string
	remediation string
	run         func(ctx context.Context, deps *Deps) (ok bool, detail string)
}

func runDoctor(ctx context.Context, deps *Deps) error {
	checks, fatal := buildDoctorChecks(ctx, deps)
	failed := 0
	for _, c := range checks {
		ok, detail := c.run(ctx, deps)
		mark := "✓"
		if !ok {
			mark = "✗"
			failed++
		}
		if detail != "" {
			fmt.Fprintf(deps.Stdout, "%s %s — %s\n", mark, c.label, detail)
		} else {
			fmt.Fprintf(deps.Stdout, "%s %s\n", mark, c.label)
		}
		if !ok && c.remediation != "" {
			fmt.Fprintf(deps.Stdout, "    → %s\n", c.remediation)
		}
	}
	if fatal != "" {
		fmt.Fprintln(deps.Stdout, fatal)
	}
	if failed > 0 {
		fmt.Fprintln(deps.Stdout, "FAIL")
		return fmt.Errorf("doctor: %d check(s) failed: %w", failed, ErrUserError)
	}
	fmt.Fprintln(deps.Stdout, "OK")
	return nil
}

// buildDoctorChecks returns the ordered check list and an optional fatal
// message printed below the transcript.
func buildDoctorChecks(ctx context.Context, deps *Deps) ([]doctorCheck, string) {
	info, _ := deps.HardwareDetector.Detect(ctx)
	vmOverride := deps.Getenv("LLAMACTL_ALLOW_VM") == "1"

	checks := []doctorCheck{
		bareMetalCheck(info, vmOverride),
		llamaServerResolvesCheck(deps),
		llamaServerVersionCheck(deps),
		iogpuWiredLimitCheck(info),
	}
	return checks, ""
}

func bareMetalCheck(info hardware.Info, override bool) doctorCheck {
	return doctorCheck{
		label: "Bare-metal Apple Silicon",
		remediation: "set LLAMACTL_ALLOW_VM=1 only if you have real GPU passthrough; " +
			"otherwise llamactl would silently fall back to CPU (~5-10x slower)",
		run: func(_ context.Context, _ *Deps) (bool, string) {
			if !info.HypervisorPresent {
				return true, "no hypervisor detected"
			}
			if info.MetalDeviceDetected {
				return true, "hypervisor present, Metal GPU detected (passthrough)"
			}
			if override {
				return true, "VM override enabled via LLAMACTL_ALLOW_VM=1"
			}
			return false, "VM without Metal — refusing (PRD §6.3)"
		},
	}
}

func llamaServerResolvesCheck(deps *Deps) doctorCheck {
	return doctorCheck{
		label: "llama-server is resolvable",
		remediation: "install via:\n" +
			"      brew install llama.cpp\n" +
			"      brew install gregmundy/tap/llamavm && llamavm install latest\n" +
			"    or set llama_server_path in ~/.config/llamactl/config.yaml",
		run: func(ctx context.Context, _ *Deps) (bool, string) {
			res, err := deps.ServerResolver.Resolve(ctx)
			if err != nil {
				if errors.Is(err, server.ErrNotFound) {
					return false, "not found in env, config, PATH, llamavm shims, or Homebrew"
				}
				return false, err.Error()
			}
			return true, fmt.Sprintf("%s (via %s)", res.Path, res.Source)
		},
	}
}

func llamaServerVersionCheck(deps *Deps) doctorCheck {
	return doctorCheck{
		label:       "llama-server version meets floor",
		remediation: fmt.Sprintf("upgrade llama.cpp; minimum build is %d (MinLlamaServerBuild)", MinLlamaServerBuild),
		run: func(ctx context.Context, _ *Deps) (bool, string) {
			res, err := deps.ServerResolver.Resolve(ctx)
			if err != nil {
				// Resolver already flagged this. Don't double-count;
				// just note the skip so the transcript is honest.
				return true, "skipped (resolver did not locate llama-server)"
			}
			v, err := deps.ServerProber.Probe(ctx, res.Path)
			if err != nil {
				return false, "could not probe --version: " + err.Error()
			}
			if !v.AtLeast(MinLlamaServerBuild) {
				return false, fmt.Sprintf("found b%d, minimum b%d", v.Build, MinLlamaServerBuild)
			}
			return true, fmt.Sprintf("b%d (%s)", v.Build, v.SHA)
		},
	}
}

// iogpuWiredLimitCheck enforces PRD AC#11: on hosts ≥32 GB, an unset or
// too-low iogpu.wired_limit_mb is flagged. Heuristic: recommend ~75% of RAM.
func iogpuWiredLimitCheck(info hardware.Info) doctorCheck {
	totalMB := int(info.RAMBytes / (1024 * 1024))
	recommendMB := int(float64(totalMB) * 0.75)
	return doctorCheck{
		label: "iogpu.wired_limit_mb is appropriate",
		remediation: fmt.Sprintf("run: sudo sysctl iogpu.wired_limit_mb=%d\n"+
			"    (persist by adding to /etc/sysctl.conf)", recommendMB),
		run: func(_ context.Context, _ *Deps) (bool, string) {
			// On <32GB hosts, the macOS default suffices.
			if totalMB < 32*1024 {
				return true, fmt.Sprintf("host has %d MB RAM; default sufficient", totalMB)
			}
			if info.IogpuWiredLimitMB == 0 {
				return false, fmt.Sprintf("unset on %d MB host", totalMB)
			}
			if info.IogpuWiredLimitMB < recommendMB-1024 {
				return false, fmt.Sprintf("set to %d MB, recommended ~%d MB",
					info.IogpuWiredLimitMB, recommendMB)
			}
			return true, fmt.Sprintf("%d MB (host has %d MB)", info.IogpuWiredLimitMB, totalMB)
		},
	}
}
