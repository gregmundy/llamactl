package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"

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
// printed only on failure. If remediationFn is non-nil it is consulted after
// run() and its return value (when non-empty) overrides remediation — used
// when the remediation depends on which failure branch fired.
type doctorCheck struct {
	label         string
	remediation   string
	remediationFn func() string
	run           func(ctx context.Context, deps *Deps) (ok bool, detail string)
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
		if !ok {
			rem := c.remediation
			if c.remediationFn != nil {
				if r := c.remediationFn(); r != "" {
					rem = r
				}
			}
			if rem != "" {
				fmt.Fprintf(deps.Stdout, "    → %s\n", rem)
			}
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
		portConflictsCheck(deps),
		modelFilesMatchMetadataCheck(deps),
		orphanedMetadataCheck(deps),
		diskSpaceCheck(deps),
		tailscaleCheck(deps),
		stalePlistsCheck(deps),
		logFilesNotOversizedCheck(deps),
	}
	return checks, ""
}

// logFilesNotOversizedCheck flags any *.log under LogsDir that exceeds
// 10 MiB. Rotation happens at serve-time, so this fires only when a
// llama-server process has been running long enough to refill the file
// past the threshold, or rotation has failed silently.
func logFilesNotOversizedCheck(deps *Deps) doctorCheck {
	return doctorCheck{
		label:       "Log files within size limit (10 MiB)",
		remediation: "rotate or remove oversized log: ls -lh " + deps.LogsDir,
		run: func(_ context.Context, _ *Deps) (bool, string) {
			if deps.LogsDir == "" {
				return true, "(not configured)"
			}
			entries, err := os.ReadDir(deps.LogsDir)
			if err != nil {
				if os.IsNotExist(err) {
					return true, "no logs directory yet"
				}
				return false, err.Error()
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				info, err := e.Info()
				if err != nil {
					continue
				}
				if info.Size() > 10<<20 {
					return false, fmt.Sprintf("%s exceeds 10 MiB", e.Name())
				}
			}
			return true, ""
		},
	}
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

// ---- Phase 3 checks ----

// portConflictsCheck verifies that every loaded llamactl service has its
// declared port actually bound (i.e. not bindable by a free probe).
func portConflictsCheck(deps *Deps) doctorCheck {
	return doctorCheck{
		label:       "port conflicts",
		remediation: "stop the service (`llamactl stop <id>`) and re-serve",
		run: func(ctx context.Context, _ *Deps) (bool, string) {
			if deps.LaunchdService == nil {
				return true, "(not configured)"
			}
			services, err := deps.LaunchdService.List(ctx)
			if err != nil {
				return false, "list services: " + err.Error()
			}
			var problems []string
			for _, svc := range services {
				port := readPortFromPlist(svc.PlistPath)
				if port == 0 {
					continue
				}
				info, _ := deps.LaunchdService.Print(ctx, svc.Label)
				if info.PID == 0 {
					continue
				}
				l, lerr := net.Listen("tcp", ":"+strconv.Itoa(port))
				if lerr == nil {
					_ = l.Close()
					id := strings.TrimPrefix(svc.Label, "com.llamactl.")
					problems = append(problems, fmt.Sprintf("%s loaded but port %d is free", id, port))
				}
			}
			if len(problems) > 0 {
				return false, strings.Join(problems, "; ")
			}
			return true, ""
		},
	}
}

// modelFilesMatchMetadataCheck flags metadata records whose on-disk size
// is more than 1% off from the recorded SizeBytes.
func modelFilesMatchMetadataCheck(deps *Deps) doctorCheck {
	return doctorCheck{
		label:       "model files match metadata",
		remediation: "re-add the affected models (`llamactl remove <id> --purge` then `llamactl add <id>`)",
		run: func(ctx context.Context, _ *Deps) (bool, string) {
			if deps.ModelStore == nil || deps.FS == nil {
				return true, "(not configured)"
			}
			entries, err := deps.ModelStore.List(ctx)
			if err != nil {
				return false, "list metadata: " + err.Error()
			}
			var problems []string
			for _, m := range entries {
				fi, err := deps.FS.Stat(m.GGUFPath)
				if err != nil {
					continue // orphaned metadata is a separate check
				}
				size := fi.Size()
				if absInt64(size-m.SizeBytes)*100 > m.SizeBytes {
					problems = append(problems,
						fmt.Sprintf("%s: on-disk %d vs metadata %d", m.ID, size, m.SizeBytes))
				}
			}
			if len(problems) > 0 {
				return false, strings.Join(problems, "; ")
			}
			return true, ""
		},
	}
}

// orphanedMetadataCheck flags metadata records whose GGUF file no longer exists.
func orphanedMetadataCheck(deps *Deps) doctorCheck {
	return doctorCheck{
		label:       "orphaned metadata",
		remediation: "run `llamactl remove <id>` for each missing entry",
		run: func(ctx context.Context, _ *Deps) (bool, string) {
			if deps.ModelStore == nil || deps.FS == nil {
				return true, "(not configured)"
			}
			entries, err := deps.ModelStore.List(ctx)
			if err != nil {
				return false, "list metadata: " + err.Error()
			}
			var problems []string
			for _, m := range entries {
				if _, err := deps.FS.Stat(m.GGUFPath); err != nil {
					problems = append(problems, fmt.Sprintf("%s: %s missing", m.ID, m.GGUFPath))
				}
			}
			if len(problems) > 0 {
				return false, strings.Join(problems, "; ")
			}
			return true, ""
		},
	}
}

// diskSpaceCheck verifies at least 5 GiB is available in SharedModelsDir.
//
// If the directory itself is missing (fresh install before any `add`), Statfs
// returns ENOENT and the right remediation is `mkdir -p`, not "free up space".
// remediationFn closes over a flag set inside run() so the remediation line
// matches the actual failure mode.
func diskSpaceCheck(deps *Deps) doctorCheck {
	var missingDir bool
	return doctorCheck{
		label:       "disk space",
		remediation: "free up space in " + deps.SharedModelsDir + " (run `llamactl remove` or move large files)",
		remediationFn: func() string {
			if missingDir {
				return fmt.Sprintf("create the models directory: mkdir -p %s", deps.SharedModelsDir)
			}
			return ""
		},
		run: func(_ context.Context, _ *Deps) (bool, string) {
			missingDir = false
			if deps.SharedModelsDir == "" {
				return true, "(not configured)"
			}
			const minFreeGB = 5
			var stat syscall.Statfs_t
			if err := syscall.Statfs(deps.SharedModelsDir, &stat); err != nil {
				if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOENT) {
					missingDir = true
					return false, "models directory does not exist: " + deps.SharedModelsDir
				}
				return false, "statfs " + deps.SharedModelsDir + ": " + err.Error()
			}
			freeBytes := stat.Bavail * uint64(stat.Bsize)
			freeGB := freeBytes / (1 << 30)
			if freeGB < minFreeGB {
				return false, fmt.Sprintf("only %d GiB free in %s; need %d GiB",
					freeGB, deps.SharedModelsDir, minFreeGB)
			}
			return true, fmt.Sprintf("%d GiB free", freeGB)
		},
	}
}

// tailscaleCheck queries `tailscale status --json` if tailscale is on PATH.
// If absent, the check passes silently with "(not configured)".
func tailscaleCheck(deps *Deps) doctorCheck {
	return doctorCheck{
		label:       "tailscale",
		remediation: "run `tailscale up` (or remove tailscale from PATH if not needed)",
		run: func(ctx context.Context, _ *Deps) (bool, string) {
			if deps.LookPath == nil {
				return true, "(not configured)"
			}
			if _, err := deps.LookPath("tailscale"); err != nil {
				return true, "(not configured)"
			}
			if deps.Runner == nil {
				return true, "(not configured)"
			}
			var buf bytes.Buffer
			if err := deps.Runner.Run(ctx, "tailscale", []string{"status", "--json"}, "", &buf, io.Discard); err != nil {
				return false, "tailscale status failed: " + err.Error()
			}
			var ts struct {
				Self struct {
					Online bool `json:"Online"`
				} `json:"Self"`
			}
			if jerr := json.Unmarshal(buf.Bytes(), &ts); jerr != nil {
				return false, "parse tailscale JSON: " + jerr.Error()
			}
			if !ts.Self.Online {
				return false, "tailscale offline"
			}
			return true, "online"
		},
	}
}

// stalePlistArgvRe matches the first ProgramArguments string in a plist.
var stalePlistArgvRe = regexp.MustCompile(`<key>ProgramArguments</key>\s*<array>\s*<string>([^<]+)</string>`)

// stalePlistsCheck flags llamactl plists whose first ProgramArguments
// element no longer points at a real file.
func stalePlistsCheck(deps *Deps) doctorCheck {
	return doctorCheck{
		label:       "stale plists",
		remediation: "run `llamactl serve <id> --detach` to regenerate the plist",
		run: func(ctx context.Context, _ *Deps) (bool, string) {
			if deps.LaunchdService == nil {
				return true, "(not configured)"
			}
			services, err := deps.LaunchdService.List(ctx)
			if err != nil {
				return false, "list services: " + err.Error()
			}
			var stale []string
			for _, svc := range services {
				data, err := os.ReadFile(svc.PlistPath)
				if err != nil {
					continue
				}
				m := stalePlistArgvRe.FindSubmatch(data)
				if m == nil {
					continue
				}
				path := string(m[1])
				if _, err := os.Stat(path); err != nil {
					id := strings.TrimPrefix(svc.Label, "com.llamactl.")
					stale = append(stale, fmt.Sprintf("%s: %s missing", id, path))
				}
			}
			if len(stale) > 0 {
				return false, strings.Join(stale, "; ")
			}
			return true, ""
		},
	}
}

func absInt64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
