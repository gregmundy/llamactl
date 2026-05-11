# llamactl Phase 1: Foundation, Hardware Detection, Doctor — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a working `llamactl` binary with `hardware`, `doctor`, and `version` commands — enough to introspect an Apple Silicon Mac, locate `llama-server`, and refuse to run in a VM without Metal passthrough.

**Architecture:** Single Go binary using `cobra`. `cmd/llamactl/main.go` is wiring only; all command logic lives in `internal/cli` and consumes narrow interfaces from a `Deps` struct (mirrors llamavm exactly). Per-domain packages: `internal/runner` (os/exec seam), `internal/platform` (host facts), `internal/hardware` (chip/RAM/GPU/VM detection via `sysctl` + `system_profiler`), `internal/config` (YAML config, paths, hardware.json), `internal/server` (llama-server discovery + version probe). Every external command call goes through the `CommandRunner` interface so tests use fakes — no real `sysctl`/`system_profiler` invocations in the test suite.

**Tech Stack:** Go 1.26.2, `github.com/spf13/cobra` v1.10+, `gopkg.in/yaml.v3` for config. macOS 14+ on Apple Silicon. No other runtime dependencies.

---

## Spec coverage (Phase 1 → PRD acceptance criteria)

This plan covers PRD acceptance criteria **2, 3, 4, 5, 11, 12** and the underlying infrastructure required for the rest. Out of scope here: model add/serve/launchd/HF (those are later phases). The `llamactl config` command is stubbed read-only — write support arrives in Phase 4.

| AC | Requirement | Covered by |
|----|-------------|------------|
| 2  | `doctor` on a system without llama.cpp reports absence + suggests both Homebrew and llamavm | Tasks 16, 17 |
| 3  | `doctor` passes resolution + reports version on a Homebrew install | Tasks 14, 17 |
| 4  | `doctor` passes resolution + reports version on a llamavm install | Tasks 13, 14, 17 |
| 5  | `hardware` identifies chip, RAM, GPU memory regardless of llama.cpp install method | Tasks 7–11 |
| 11 | `doctor` detects unset/low `iogpu.wired_limit_mb`, outputs exact `sudo sysctl` command | Task 17 |
| 12 | `doctor` detects VM without Metal passthrough, refuses before any model operation | Tasks 10, 17 |

---

## File Structure

```
llamactl/
├── go.mod                                      module github.com/gregmundy/llamactl
├── go.sum
├── LICENSE                                     MIT (Greg Mundy 2026)
├── README.md                                   placeholder — full docs in Phase 4
├── .gitignore                                  Go defaults + /llamactl binary
├── .github/workflows/ci.yml                    fmt, vet, test on macOS-14 arm64
├── cmd/llamactl/main.go                        wiring only — constructs Deps, executes root
└── internal/
    ├── cli/
    │   ├── deps.go                             Deps struct, narrow interfaces, ErrUserError
    │   ├── root.go                             NewRoot — wires all subcommands
    │   ├── root_test.go                        test helpers: runRoot, fakes
    │   ├── hardware.go                         newHardwareCmd
    │   ├── hardware_test.go
    │   ├── doctor.go                           newDoctorCmd + doctorCheck + check funcs
    │   └── doctor_test.go
    ├── runner/
    │   ├── runner.go                           CommandRunner interface + ExecRunner
    │   └── runner_test.go
    ├── platform/
    │   └── platform.go                         IsAppleSilicon, Cores — runtime-backed
    ├── hardware/
    │   ├── detector.go                         Detector type, Detect() returns Info
    │   ├── detector_test.go
    │   ├── types.go                            Info struct + JSON tags
    │   └── testdata/
    │       ├── sphardware_m2pro.json           system_profiler SPHardwareDataType fixture
    │       ├── spdisplays_m2pro.json           system_profiler SPDisplaysDataType fixture
    │       └── spdisplays_vm_nometal.json      VM without Metal device fixture
    ├── config/
    │   ├── paths.go                            ConfigDir, CacheDir, DataDir, HardwareJSON
    │   ├── config.go                           Config struct (yaml), Load() — write deferred
    │   └── config_test.go
    └── server/
        ├── resolver.go                         5-step llama-server discovery
        ├── resolver_test.go
        ├── version.go                          ParseVersion + comparison
        ├── version_test.go
        ├── probe.go                            Probe(): cached `llama-server --version`
        └── probe_test.go
```

**Decomposition note:** `internal/cli/doctor.go` will be ~250 lines but stays cohesive (single responsibility: orchestrate checks). The check predicates live there — they're tightly coupled to the `Deps` shape and shouldn't migrate to `internal/hardware` or `internal/server`.

---

## Task 1: Repository scaffold

**Files:**
- Create: `/Users/greg/Development/llamactl/go.mod`
- Create: `/Users/greg/Development/llamactl/LICENSE`
- Create: `/Users/greg/Development/llamactl/.gitignore`
- Create: `/Users/greg/Development/llamactl/README.md`

- [ ] **Step 1: Initialize go module**

Run:
```bash
cd /Users/greg/Development/llamactl
go mod init github.com/gregmundy/llamactl
go mod edit -go=1.26.2
go get github.com/spf13/cobra@latest
go get gopkg.in/yaml.v3@latest
```

Expected: `go.mod` exists, contains the module path, Go directive `1.26.2`, and both deps under `require`.

- [ ] **Step 2: Write LICENSE**

Copy MIT text. Use:
```
MIT License

Copyright (c) 2026 Greg Mundy

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```

- [ ] **Step 3: Write .gitignore**

```
# Binaries
/llamactl
/dist/

# Go
*.test
*.out
coverage.txt

# IDE
.idea/
.vscode/
*.swp

# OS
.DS_Store

# Claude Code per-machine settings
.claude/settings.local.json
```

- [ ] **Step 4: Write README placeholder**

```markdown
# llamactl

> Single-binary CLI for running llama.cpp on Apple Silicon.

Currently under construction. See `docs/llamactl-prd-v1.5.md` for the spec.

## Requirements

- macOS 14+ on Apple Silicon
- A `llama-server` binary on PATH (via `brew install llama.cpp` or `brew install gregmundy/tap/llamavm && llamavm install latest`)

## Development

```bash
go build ./cmd/llamactl
./llamactl --help
go test ./...
```
```

- [ ] **Step 5: Commit**

```bash
cd /Users/greg/Development/llamactl
git add go.mod go.sum LICENSE .gitignore README.md
git commit -m "feat: scaffold llamactl Go module"
```

Expected: clean commit. `git status` shows working tree clean.

---

## Task 2: cobra root command + main.go skeleton

**Files:**
- Create: `cmd/llamactl/main.go`
- Create: `internal/cli/deps.go`
- Create: `internal/cli/root.go`
- Create: `internal/cli/root_test.go`

- [ ] **Step 1: Write the failing test**

`internal/cli/root_test.go`:
```go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

// runRoot executes the root command with the given args using deps for I/O
// and returns captured stdout, stderr, and any error.
func runRoot(t *testing.T, deps *Deps, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	deps.Stdout = &out
	deps.Stderr = &errOut
	root := NewRoot(deps, "test")
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errOut.String(), err
}

func TestRoot_VersionFlag(t *testing.T) {
	deps := &Deps{}
	out, _, err := runRoot(t, deps, "--version")
	if err != nil {
		t.Fatalf("--version: %v", err)
	}
	if !strings.Contains(out, "test") {
		t.Fatalf("expected version string in output, got: %q", out)
	}
}

func TestRoot_HelpShowsShortDescription(t *testing.T) {
	deps := &Deps{}
	out, _, err := runRoot(t, deps, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	// Cobra's default --help for a non-runnable, no-subcommand root prints
	// only the Short string. Once subcommands land in later tasks, the full
	// usage block (including "llamactl") will appear.
	if !strings.Contains(out, "Run llama.cpp on Apple Silicon") {
		t.Fatalf("expected Short string in help output, got: %q", out)
	}
}
```

- [ ] **Step 2: Write Deps and NewRoot to make tests pass**

`internal/cli/deps.go`:
```go
// Package cli builds the cobra command tree and orchestrates llamactl flows.
// The package never imports its concrete dependencies — instead it consumes
// the narrow interfaces below, which the binary wires up at main.go.
package cli

import (
	"errors"
	"io"
)

// ErrUserError marks errors caused by user input or environment state
// (no llama-server found, VM detected, etc.). main.go maps this to exit 2.
var ErrUserError = errors.New("user error")

// Deps collects everything the cli subcommands need. Later tasks add fields.
type Deps struct {
	Stdout io.Writer
	Stderr io.Writer
}
```

`internal/cli/root.go`:
```go
package cli

import "github.com/spf13/cobra"

// NewRoot returns the root cobra command. Pass the llamactl version string
// (e.g. "v0.1.0") so `--version` can report it.
func NewRoot(deps *Deps, llamactlVersion string) *cobra.Command {
	root := &cobra.Command{
		Use:           "llamactl",
		Short:         "Run llama.cpp on Apple Silicon",
		Version:       llamactlVersion,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	if deps.Stdout != nil {
		root.SetOut(deps.Stdout)
	}
	if deps.Stderr != nil {
		root.SetErr(deps.Stderr)
	}
	return root
}
```

- [ ] **Step 3: Write main.go**

`cmd/llamactl/main.go`:
```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gregmundy/llamactl/internal/cli"
)

var llamactlVersion = "dev"

func main() {
	deps := &cli.Deps{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	root := cli.NewRoot(deps, llamactlVersion)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, cli.ErrUserError) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "llamactl:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Run tests + build**

```bash
cd /Users/greg/Development/llamactl
go test ./internal/cli/...
go build ./cmd/llamactl
./llamactl --version
./llamactl --help
```

Expected: tests pass, binary builds, `--version` prints `llamactl version dev`, `--help` includes `llamactl` and `Run llama.cpp on Apple Silicon`.

- [ ] **Step 5: Commit**

```bash
git add cmd internal go.sum
git commit -m "feat: cobra root command + Deps skeleton"
```

---

## Task 3: CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Write CI config**

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: macos-14
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26.2'
          cache: true
      - name: gofmt
        run: |
          unformatted=$(gofmt -l .)
          if [ -n "$unformatted" ]; then
            echo "Unformatted files:"; echo "$unformatted"; exit 1
          fi
      - name: go vet
        run: go vet ./...
      - name: go test
        run: go test -race -count=1 ./...
      - name: go build
        run: go build ./cmd/llamactl
```

- [ ] **Step 2: Verify it parses locally (no remote run needed)**

Run:
```bash
cd /Users/greg/Development/llamactl
go vet ./...
gofmt -l .
go test -race -count=1 ./...
```

Expected: all commands exit 0, gofmt outputs nothing.

- [ ] **Step 3: Commit**

```bash
git add .github
git commit -m "ci: add macOS arm64 build + test workflow"
```

---

## Task 4: CommandRunner package

**Files:**
- Create: `internal/runner/runner.go`
- Create: `internal/runner/runner_test.go`

- [ ] **Step 1: Write the failing test**

`internal/runner/runner_test.go`:
```go
package runner

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExecRunner_RunCapturesStdout(t *testing.T) {
	var out bytes.Buffer
	err := ExecRunner{}.Run(context.Background(), "echo", []string{"hello"}, "", &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("run echo: %v", err)
	}
	if strings.TrimSpace(out.String()) != "hello" {
		t.Fatalf("got stdout %q, want %q", out.String(), "hello")
	}
}

func TestExecRunner_RunReturnsErrorOnNonZeroExit(t *testing.T) {
	err := ExecRunner{}.Run(context.Background(), "sh", []string{"-c", "exit 7"}, "", &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected non-nil error from exit 7")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/runner/...
```
Expected: FAIL — package missing.

- [ ] **Step 3: Implement**

`internal/runner/runner.go`:
```go
// Package runner provides an os/exec seam so command execution can be faked
// in tests. All llamactl subcommands that shell out (sysctl, system_profiler,
// llama-server --version, etc.) go through CommandRunner.
package runner

import (
	"context"
	"io"
	"os/exec"
)

// CommandRunner runs an external command. Implementations decide whether to
// shell out for real or simulate.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
}

// ExecRunner is the production CommandRunner backed by os/exec.
type ExecRunner struct{}

// Run invokes name with args in dir (empty dir = cwd), routing stdout/stderr
// to the supplied writers. Returns the underlying os/exec error verbatim so
// callers can use errors.As to inspect *exec.ExitError.
func (ExecRunner) Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
```

- [ ] **Step 4: Run to verify pass**

```bash
go test ./internal/runner/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runner
git commit -m "feat(runner): CommandRunner interface + ExecRunner"
```

---

## Task 5: Platform package

**Files:**
- Create: `internal/platform/platform.go`

- [ ] **Step 1: Implement (test is trivial — defer to integration coverage)**

`internal/platform/platform.go`:
```go
// Package platform answers host-environment questions the cli flows care
// about. Backed by stdlib runtime; mock in tests by satisfying the Platform
// interface defined in internal/cli/deps.go.
package platform

import "runtime"

// Default is the production Platform. The cli package consumes it via the
// Platform interface; tests substitute their own.
type Default struct{}

func (Default) IsAppleSilicon() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}

func (Default) Cores() int { return runtime.NumCPU() }
```

- [ ] **Step 2: Verify build**

```bash
go build ./...
```
Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add internal/platform
git commit -m "feat(platform): host-environment facts"
```

---

## Task 6: Hardware types and detector skeleton

**Files:**
- Create: `internal/hardware/types.go`
- Create: `internal/hardware/detector.go`
- Create: `internal/hardware/detector_test.go`
- Create: `internal/hardware/testdata/sphardware_m2pro.json`

- [ ] **Step 1: Write the failing test**

`internal/hardware/detector_test.go`:
```go
package hardware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// fakeRunner replays canned stdout per (name, args[0]) and never invokes a
// real binary. Unmatched calls fail the test.
type fakeRunner struct {
	t       *testing.T
	outputs map[string]string // key: name + " " + args[0]
	errs    map[string]error
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, _ string, stdout, stderr io.Writer) error {
	key := name
	if len(args) > 0 {
		key = name + " " + args[0]
	}
	if err, ok := f.errs[key]; ok {
		return err
	}
	out, ok := f.outputs[key]
	if !ok {
		f.t.Fatalf("unexpected runner call: %s %v", name, args)
	}
	_, _ = io.WriteString(stdout, out)
	_ = stderr
	return nil
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

func TestDetect_ReturnsZeroValueWhenAllCommandsFail(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		errs: map[string]error{
			"system_profiler SPHardwareDataType":  errors.New("fail"),
			"system_profiler SPDisplaysDataType":  errors.New("fail"),
			"sysctl hw.memsize":                   errors.New("fail"),
			"sysctl iogpu.wired_limit_mb":         errors.New("fail"),
			"sysctl kern.hv_vmm_present":          errors.New("fail"),
		},
	}
	d := &Detector{Runner: runner}
	info, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect should not error on subcommand failures: %v", err)
	}
	if info.Chip != "" || info.RAMBytes != 0 {
		t.Fatalf("expected zero values, got %+v", info)
	}
}

func TestInfo_JSONRoundTrip(t *testing.T) {
	in := Info{Chip: "Apple M2 Pro", ChipGen: "M2", RAMBytes: 32 * 1024 * 1024 * 1024, IogpuWiredLimitMB: 0, HypervisorPresent: false, MetalDeviceDetected: true}
	b, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var out Info
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("roundtrip mismatch:\n got %+v\nwant %+v", out, in)
	}
}
```

- [ ] **Step 2: Write fixture**

`internal/hardware/testdata/sphardware_m2pro.json`:
```json
{
  "SPHardwareDataType": [
    {
      "_name": "hardware_overview",
      "chip_type": "Apple M2 Pro",
      "machine_model": "Mac14,12",
      "machine_name": "Mac mini",
      "number_processors": "proc 12:8:4",
      "physical_memory": "32 GB",
      "platform_UUID": "00000000-0000-0000-0000-000000000000",
      "serial_number": "XXXXXXXXXX"
    }
  ]
}
```

- [ ] **Step 3: Write types and detector skeleton**

`internal/hardware/types.go`:
```go
package hardware

// Info is the structured snapshot the hardware command writes to
// ~/.config/llamactl/hardware.json. Every field is best-effort: when a
// detection probe fails, the zero value is preserved and Detect does not
// surface the underlying error (doctor's job, not hardware's).
type Info struct {
	Chip                string `json:"chip"`                  // "Apple M2 Pro"
	ChipGen             string `json:"chip_gen"`              // "M2"
	RAMBytes            uint64 `json:"ram_bytes"`             // from sysctl hw.memsize
	IogpuWiredLimitMB   int    `json:"iogpu_wired_limit_mb"`  // 0 = unset (uses default ~75% of RAM)
	HypervisorPresent   bool   `json:"hypervisor_present"`    // sysctl kern.hv_vmm_present == 1
	MetalDeviceDetected bool   `json:"metal_device_detected"` // system_profiler SPDisplaysDataType has Metal entry
	OSVersion           string `json:"os_version"`            // sw_vers -productVersion
}
```

`internal/hardware/detector.go`:
```go
// Package hardware introspects an Apple Silicon Mac's chip, memory, GPU
// memory cap, and hypervisor state. Every probe is best-effort: a failing
// sysctl or system_profiler invocation leaves the corresponding Info field
// at its zero value rather than failing the whole detection. Doctor is the
// component that converts zero values into actionable error messages.
package hardware

import (
	"context"
	"io"
)

// CommandRunner mirrors runner.CommandRunner — we redeclare locally to avoid
// importing the runner package from a leaf detection package. The cli wiring
// passes the same concrete ExecRunner to both.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
}

// Detector probes host hardware via CommandRunner. Construct with the
// production runner.ExecRunner in main.go, or a fake in tests.
type Detector struct {
	Runner CommandRunner
}

// Detect runs every probe and returns an Info populated with whatever
// succeeded. The error return is reserved for future use (e.g. catastrophic
// runner failure); today Detect never returns a non-nil error.
func (d *Detector) Detect(ctx context.Context) (Info, error) {
	var info Info
	// Each probe is filled in by later tasks (7–10). The skeleton just
	// ensures the public surface is stable.
	return info, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/hardware/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/hardware
git commit -m "feat(hardware): Info type + Detector skeleton"
```

---

## Task 7: Chip detection (system_profiler SPHardwareDataType)

**Files:**
- Modify: `internal/hardware/detector.go`
- Modify: `internal/hardware/detector_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/hardware/detector_test.go`:
```go
func TestDetect_ParsesChipFromSystemProfiler(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		outputs: map[string]string{
			"system_profiler SPHardwareDataType": readFixture(t, "sphardware_m2pro.json"),
		},
		errs: map[string]error{
			"system_profiler SPDisplaysDataType": errors.New("not needed"),
			"sysctl hw.memsize":                  errors.New("not needed"),
			"sysctl iogpu.wired_limit_mb":        errors.New("not needed"),
			"sysctl kern.hv_vmm_present":         errors.New("not needed"),
			"sw_vers -productVersion":            errors.New("not needed"),
		},
	}
	info, _ := (&Detector{Runner: runner}).Detect(context.Background())
	if info.Chip != "Apple M2 Pro" {
		t.Fatalf("Chip = %q, want %q", info.Chip, "Apple M2 Pro")
	}
	if info.ChipGen != "M2" {
		t.Fatalf("ChipGen = %q, want %q", info.ChipGen, "M2")
	}
}

func TestParseChipGen(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Apple M1", "M1"},
		{"Apple M1 Pro", "M1"},
		{"Apple M2 Max", "M2"},
		{"Apple M3 Ultra", "M3"},
		{"Apple M4", "M4"},
		{"Unknown", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := parseChipGen(c.in)
		if got != c.want {
			t.Errorf("parseChipGen(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/hardware/...
```
Expected: FAIL — `parseChipGen` undefined, chip empty.

- [ ] **Step 3: Implement**

Replace the body of `Detect` in `internal/hardware/detector.go` and add helpers:
```go
import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

func (d *Detector) Detect(ctx context.Context) (Info, error) {
	var info Info
	d.probeChip(ctx, &info)
	return info, nil
}

func (d *Detector) probeChip(ctx context.Context, info *Info) {
	var stdout bytes.Buffer
	if err := d.Runner.Run(ctx, "system_profiler", []string{"SPHardwareDataType", "-json"}, "", &stdout, io.Discard); err != nil {
		return
	}
	var doc struct {
		SPHardwareDataType []struct {
			ChipType string `json:"chip_type"`
		} `json:"SPHardwareDataType"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		return
	}
	if len(doc.SPHardwareDataType) == 0 {
		return
	}
	info.Chip = strings.TrimSpace(doc.SPHardwareDataType[0].ChipType)
	info.ChipGen = parseChipGen(info.Chip)
}

var chipGenRe = regexp.MustCompile(`\bM(\d+)\b`)

// parseChipGen extracts the generation token ("M1", "M2", ...) from an
// Apple Silicon chip name like "Apple M2 Pro". Returns "" when not Apple
// Silicon or unparseable.
func parseChipGen(chip string) string {
	m := chipGenRe.FindStringSubmatch(chip)
	if len(m) < 2 {
		return ""
	}
	return "M" + m[1]
}
```

Update the test fixture call: the args used in `probeChip` are `["SPHardwareDataType", "-json"]`, but the fake's key is `"system_profiler SPHardwareDataType"` (first arg only). That matches. Confirm by reading the fakeRunner key construction in `internal/hardware/detector_test.go` from Task 6 Step 1.

- [ ] **Step 4: Run to verify pass**

```bash
go test ./internal/hardware/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/hardware
git commit -m "feat(hardware): probe chip and generation"
```

---

## Task 8: RAM and OS version detection

**Files:**
- Modify: `internal/hardware/detector.go`
- Modify: `internal/hardware/detector_test.go`

- [ ] **Step 1: Write the failing test**

Append to `detector_test.go`:
```go
func TestDetect_ParsesRAMAndOSVersion(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		outputs: map[string]string{
			"system_profiler SPHardwareDataType": readFixture(t, "sphardware_m2pro.json"),
			"sysctl hw.memsize":                  "hw.memsize: 34359738368\n",
			"sw_vers -productVersion":            "14.4.1\n",
		},
		errs: map[string]error{
			"system_profiler SPDisplaysDataType": errors.New("not needed"),
			"sysctl iogpu.wired_limit_mb":        errors.New("not needed"),
			"sysctl kern.hv_vmm_present":         errors.New("not needed"),
		},
	}
	info, _ := (&Detector{Runner: runner}).Detect(context.Background())
	if info.RAMBytes != 34359738368 {
		t.Fatalf("RAMBytes = %d, want 34359738368", info.RAMBytes)
	}
	if info.OSVersion != "14.4.1" {
		t.Fatalf("OSVersion = %q, want %q", info.OSVersion, "14.4.1")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/hardware/...
```
Expected: FAIL — fields zero.

- [ ] **Step 3: Implement**

Add to `internal/hardware/detector.go`:
```go
import "strconv"

func (d *Detector) Detect(ctx context.Context) (Info, error) {
	var info Info
	d.probeChip(ctx, &info)
	d.probeRAM(ctx, &info)
	d.probeOSVersion(ctx, &info)
	return info, nil
}

func (d *Detector) probeRAM(ctx context.Context, info *Info) {
	var stdout bytes.Buffer
	if err := d.Runner.Run(ctx, "sysctl", []string{"hw.memsize"}, "", &stdout, io.Discard); err != nil {
		return
	}
	// Output is "hw.memsize: 34359738368\n"
	parts := strings.SplitN(strings.TrimSpace(stdout.String()), ":", 2)
	if len(parts) != 2 {
		return
	}
	n, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return
	}
	info.RAMBytes = n
}

func (d *Detector) probeOSVersion(ctx context.Context, info *Info) {
	var stdout bytes.Buffer
	if err := d.Runner.Run(ctx, "sw_vers", []string{"-productVersion"}, "", &stdout, io.Discard); err != nil {
		return
	}
	info.OSVersion = strings.TrimSpace(stdout.String())
}
```

- [ ] **Step 4: Run to verify pass**

```bash
go test ./internal/hardware/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/hardware
git commit -m "feat(hardware): probe RAM via sysctl + OS version via sw_vers"
```

---

## Task 9: iogpu.wired_limit_mb detection

**Files:**
- Modify: `internal/hardware/detector.go`
- Modify: `internal/hardware/detector_test.go`

**Note on macOS behavior:** `sysctl iogpu.wired_limit_mb` returns nothing on unset (or in some macOS versions, returns 0). Both states mean "use the default ~75% of RAM". Treat 0 and missing key identically — `IogpuWiredLimitMB` field stays 0 in both cases.

- [ ] **Step 1: Write the failing test**

Append to `detector_test.go`:
```go
func TestDetect_ParsesIogpuWiredLimit(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want int
	}{
		{"set explicitly", "iogpu.wired_limit_mb: 24576\n", 24576},
		{"set to zero", "iogpu.wired_limit_mb: 0\n", 0},
		{"unset (empty)", "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runner := &fakeRunner{
				t: t,
				outputs: map[string]string{
					"sysctl iogpu.wired_limit_mb": c.out,
				},
				errs: map[string]error{
					"system_profiler SPHardwareDataType": errors.New("not needed"),
					"system_profiler SPDisplaysDataType": errors.New("not needed"),
					"sysctl hw.memsize":                  errors.New("not needed"),
					"sysctl kern.hv_vmm_present":         errors.New("not needed"),
					"sw_vers -productVersion":            errors.New("not needed"),
				},
			}
			info, _ := (&Detector{Runner: runner}).Detect(context.Background())
			if info.IogpuWiredLimitMB != c.want {
				t.Fatalf("IogpuWiredLimitMB = %d, want %d", info.IogpuWiredLimitMB, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/hardware/...
```
Expected: FAIL — field zero in all cases (the "set" case fails because parsing isn't implemented yet; the others would already match the zero default but the test still drives the implementation).

- [ ] **Step 3: Implement**

Add to `internal/hardware/detector.go`:
```go
func (d *Detector) Detect(ctx context.Context) (Info, error) {
	var info Info
	d.probeChip(ctx, &info)
	d.probeRAM(ctx, &info)
	d.probeOSVersion(ctx, &info)
	d.probeIogpu(ctx, &info)
	return info, nil
}

func (d *Detector) probeIogpu(ctx context.Context, info *Info) {
	var stdout bytes.Buffer
	if err := d.Runner.Run(ctx, "sysctl", []string{"iogpu.wired_limit_mb"}, "", &stdout, io.Discard); err != nil {
		return
	}
	s := strings.TrimSpace(stdout.String())
	if s == "" {
		return
	}
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return
	}
	info.IogpuWiredLimitMB = n
}
```

- [ ] **Step 4: Run to verify pass**

```bash
go test ./internal/hardware/...
```
Expected: PASS for all three subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/hardware
git commit -m "feat(hardware): probe iogpu.wired_limit_mb"
```

---

## Task 10: VM and Metal device detection

**Files:**
- Modify: `internal/hardware/detector.go`
- Modify: `internal/hardware/detector_test.go`
- Create: `internal/hardware/testdata/spdisplays_m2pro.json`
- Create: `internal/hardware/testdata/spdisplays_vm_nometal.json`

- [ ] **Step 1: Write fixtures**

`internal/hardware/testdata/spdisplays_m2pro.json`:
```json
{
  "SPDisplaysDataType": [
    {
      "_name": "spdisplays_display",
      "spdisplays_mtlgpufamilysupport": "spdisplays_metal3",
      "sppci_model": "Apple M2 Pro"
    }
  ]
}
```

`internal/hardware/testdata/spdisplays_vm_nometal.json`:
```json
{
  "SPDisplaysDataType": []
}
```

- [ ] **Step 2: Write the failing test**

Append to `detector_test.go`:
```go
func TestDetect_HypervisorAndMetal(t *testing.T) {
	cases := []struct {
		name             string
		hvmm             string
		displays         string
		wantHV           bool
		wantMetal        bool
	}{
		{
			name:      "bare metal Apple Silicon",
			hvmm:      "kern.hv_vmm_present: 0\n",
			displays:  readFixture(t, "spdisplays_m2pro.json"),
			wantHV:    false,
			wantMetal: true,
		},
		{
			name:      "VM with Metal passthrough",
			hvmm:      "kern.hv_vmm_present: 1\n",
			displays:  readFixture(t, "spdisplays_m2pro.json"),
			wantHV:    true,
			wantMetal: true,
		},
		{
			name:      "VM without Metal",
			hvmm:      "kern.hv_vmm_present: 1\n",
			displays:  readFixture(t, "spdisplays_vm_nometal.json"),
			wantHV:    true,
			wantMetal: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runner := &fakeRunner{
				t: t,
				outputs: map[string]string{
					"sysctl kern.hv_vmm_present":         c.hvmm,
					"system_profiler SPDisplaysDataType": c.displays,
				},
				errs: map[string]error{
					"system_profiler SPHardwareDataType": errors.New("not needed"),
					"sysctl hw.memsize":                  errors.New("not needed"),
					"sysctl iogpu.wired_limit_mb":        errors.New("not needed"),
					"sw_vers -productVersion":            errors.New("not needed"),
				},
			}
			info, _ := (&Detector{Runner: runner}).Detect(context.Background())
			if info.HypervisorPresent != c.wantHV {
				t.Errorf("HypervisorPresent = %v, want %v", info.HypervisorPresent, c.wantHV)
			}
			if info.MetalDeviceDetected != c.wantMetal {
				t.Errorf("MetalDeviceDetected = %v, want %v", info.MetalDeviceDetected, c.wantMetal)
			}
		})
	}
}
```

- [ ] **Step 3: Run to verify failure**

```bash
go test ./internal/hardware/...
```
Expected: FAIL — both bool fields stay false.

- [ ] **Step 4: Implement**

Add to `internal/hardware/detector.go`:
```go
func (d *Detector) Detect(ctx context.Context) (Info, error) {
	var info Info
	d.probeChip(ctx, &info)
	d.probeRAM(ctx, &info)
	d.probeOSVersion(ctx, &info)
	d.probeIogpu(ctx, &info)
	d.probeHypervisor(ctx, &info)
	d.probeMetalDevice(ctx, &info)
	return info, nil
}

func (d *Detector) probeHypervisor(ctx context.Context, info *Info) {
	var stdout bytes.Buffer
	if err := d.Runner.Run(ctx, "sysctl", []string{"kern.hv_vmm_present"}, "", &stdout, io.Discard); err != nil {
		return
	}
	parts := strings.SplitN(strings.TrimSpace(stdout.String()), ":", 2)
	if len(parts) != 2 {
		return
	}
	info.HypervisorPresent = strings.TrimSpace(parts[1]) == "1"
}

func (d *Detector) probeMetalDevice(ctx context.Context, info *Info) {
	var stdout bytes.Buffer
	if err := d.Runner.Run(ctx, "system_profiler", []string{"SPDisplaysDataType", "-json"}, "", &stdout, io.Discard); err != nil {
		return
	}
	var doc struct {
		SPDisplaysDataType []map[string]any `json:"SPDisplaysDataType"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		return
	}
	// Any non-empty SPDisplaysDataType entry on Apple Silicon represents a
	// real Metal-capable GPU. VMs without GPU passthrough return an empty
	// array. This is the same heuristic the PRD §6.3 algorithm prescribes.
	info.MetalDeviceDetected = len(doc.SPDisplaysDataType) > 0
}
```

- [ ] **Step 5: Run to verify pass**

```bash
go test ./internal/hardware/...
```
Expected: PASS for all three subtests.

- [ ] **Step 6: Commit**

```bash
git add internal/hardware
git commit -m "feat(hardware): probe hypervisor flag + Metal device presence"
```

---

## Task 11: Config paths and hardware.json persistence

**Files:**
- Create: `internal/config/paths.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

`internal/config/config_test.go`:
```go
package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPaths_RespectXDGOverrides(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	t.Setenv("XDG_CACHE_HOME", "/tmp/cache")
	t.Setenv("XDG_DATA_HOME", "/tmp/data")
	p := Paths{Home: "/home/u"}
	if got := p.ConfigDir(); got != "/tmp/cfg/llamactl" {
		t.Errorf("ConfigDir = %q", got)
	}
	if got := p.CacheDir(); got != "/tmp/cache/llamactl" {
		t.Errorf("CacheDir = %q", got)
	}
	if got := p.DataDir(); got != "/tmp/data/llama-models" {
		t.Errorf("DataDir = %q", got)
	}
}

func TestPaths_DefaultsWhenNoXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	p := Paths{Home: "/home/u"}
	if got := p.ConfigDir(); got != "/home/u/.config/llamactl" {
		t.Errorf("ConfigDir = %q", got)
	}
	if got := p.CacheDir(); got != "/home/u/.cache/llamactl" {
		t.Errorf("CacheDir = %q", got)
	}
	if got := p.DataDir(); got != "/home/u/.local/share/llama-models" {
		t.Errorf("DataDir = %q", got)
	}
	if got := p.HardwareJSON(); got != "/home/u/.config/llamactl/hardware.json" {
		t.Errorf("HardwareJSON = %q", got)
	}
}

func TestLoad_MissingFileReturnsZeroConfig(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if cfg.LlamaServerPath != "" {
		t.Errorf("expected empty LlamaServerPath, got %q", cfg.LlamaServerPath)
	}
}

func TestLoad_ParsesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := "llama_server_path: /opt/custom/llama-server\ndefault_port: 9090\n"
	if err := writeFile(t, path, body); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LlamaServerPath != "/opt/custom/llama-server" {
		t.Errorf("LlamaServerPath = %q", cfg.LlamaServerPath)
	}
	if cfg.DefaultPort != 9090 {
		t.Errorf("DefaultPort = %d", cfg.DefaultPort)
	}
}

func TestLoad_RejectsMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := writeFile(t, path, "::: not yaml :::"); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func writeFile(t *testing.T, path, content string) error {
	t.Helper()
	return writeFileImpl(path, content)
}
```

For now, also create a tiny helper file so the test compiles:

`internal/config/testing_helpers_test.go`:
```go
package config

import "os"

func writeFileImpl(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
```

- [ ] **Step 2: Implement paths**

`internal/config/paths.go`:
```go
// Package config owns llamactl's filesystem layout (XDG-aware) and the YAML
// config loader. Phase 1 is read-only; the `llamactl config` write command
// arrives in Phase 4.
package config

import (
	"os"
	"path/filepath"
)

// Paths resolves llamactl's config/cache/data directories. Construct via
// New() to pick up $HOME and XDG_* env vars at startup. Tests construct
// directly with their own Home.
type Paths struct {
	Home string
}

// New returns a Paths anchored to the calling user's home directory.
func New() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	return Paths{Home: home}, nil
}

// xdgDir returns $envVar/llamactl if envVar is set, else the fallback.
func (p Paths) xdgDir(envVar, fallback string) string {
	if v := os.Getenv(envVar); v != "" {
		return filepath.Join(v, "llamactl")
	}
	return fallback
}

func (p Paths) ConfigDir() string {
	return p.xdgDir("XDG_CONFIG_HOME", filepath.Join(p.Home, ".config", "llamactl"))
}

func (p Paths) CacheDir() string {
	return p.xdgDir("XDG_CACHE_HOME", filepath.Join(p.Home, ".cache", "llamactl"))
}

// DataDir is the SHARED model directory (note: not namespaced under "llamactl"
// because it's an open convention per PRD §4).
func (p Paths) DataDir() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "llama-models")
	}
	return filepath.Join(p.Home, ".local", "share", "llama-models")
}

func (p Paths) ConfigFile() string    { return filepath.Join(p.ConfigDir(), "config.yaml") }
func (p Paths) HardwareJSON() string  { return filepath.Join(p.ConfigDir(), "hardware.json") }
func (p Paths) ModelsMetaDir() string { return filepath.Join(p.ConfigDir(), "models") }
```

- [ ] **Step 3: Implement Load**

`internal/config/config.go`:
```go
package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk representation of ~/.config/llamactl/config.yaml.
// Fields are camel-cased in YAML to match `llamactl config <key>` argument
// names. Zero values mean "unset, fall back to defaults".
type Config struct {
	LlamaServerPath string `yaml:"llama_server_path"`
	DefaultPort     int    `yaml:"default_port"`
	ModelsDir       string `yaml:"models_dir"`
	HFToken         string `yaml:"hf_token"`
	LogLevel        string `yaml:"log_level"`
}

// Load reads path and returns the parsed Config. A missing file is not an
// error — Load returns the zero Config. Malformed YAML is.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/config/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat(config): XDG-aware paths + YAML loader"
```

---

## Task 12: `llamactl hardware` command

**Files:**
- Modify: `internal/cli/deps.go` — add HardwareDetector, Paths, Now fields
- Modify: `internal/cli/root.go` — wire newHardwareCmd
- Create: `internal/cli/hardware.go`
- Create: `internal/cli/hardware_test.go`
- Modify: `cmd/llamactl/main.go` — wire concrete Detector and Paths

- [ ] **Step 1: Write the failing test**

`internal/cli/hardware_test.go`:
```go
package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/hardware"
)

type fakeDetector struct {
	info hardware.Info
	err  error
}

func (f *fakeDetector) Detect(_ context.Context) (hardware.Info, error) {
	return f.info, f.err
}

func TestHardware_WritesJSONAndPrints(t *testing.T) {
	tmp := t.TempDir()
	deps := &Deps{
		HardwareDetector: &fakeDetector{info: hardware.Info{
			Chip:                "Apple M2 Pro",
			ChipGen:             "M2",
			RAMBytes:            32 * 1024 * 1024 * 1024,
			IogpuWiredLimitMB:   0,
			HypervisorPresent:   false,
			MetalDeviceDetected: true,
			OSVersion:           "14.4.1",
		}},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		Now:              func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
	out, _, err := runRoot(t, deps, "hardware")
	if err != nil {
		t.Fatalf("hardware: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Apple M2 Pro") {
		t.Errorf("expected chip in output, got %q", out)
	}
	if !strings.Contains(out, "32 GB") {
		t.Errorf("expected RAM in output, got %q", out)
	}

	b, err := os.ReadFile(filepath.Join(tmp, "hardware.json"))
	if err != nil {
		t.Fatalf("read hardware.json: %v", err)
	}
	var got hardware.Info
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Chip != "Apple M2 Pro" {
		t.Errorf("persisted Chip = %q", got.Chip)
	}
}

func TestHardware_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	deps := &Deps{
		HardwareDetector: &fakeDetector{info: hardware.Info{Chip: "Apple M2"}},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		Now:              func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
	if _, _, err := runRoot(t, deps, "hardware"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, _, err := runRoot(t, deps, "hardware"); err != nil {
		t.Fatalf("second run: %v", err)
	}
	// Second run should overwrite cleanly without crashing or producing
	// duplicate output. Verify file is parseable.
	b, _ := os.ReadFile(filepath.Join(tmp, "hardware.json"))
	var info hardware.Info
	if err := json.Unmarshal(b, &info); err != nil {
		t.Fatalf("unmarshal after rerun: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/cli/...
```
Expected: FAIL — `HardwareDetector` undefined on Deps, `hardware` subcommand missing.

- [ ] **Step 3: Update Deps**

Replace `internal/cli/deps.go` with:
```go
package cli

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/gregmundy/llamactl/internal/hardware"
)

var ErrUserError = errors.New("user error")

// HardwareDetector matches *hardware.Detector. Tests substitute a fake.
type HardwareDetector interface {
	Detect(ctx context.Context) (hardware.Info, error)
}

type Deps struct {
	Stdout io.Writer
	Stderr io.Writer

	HardwareDetector HardwareDetector
	HardwareJSONPath string

	Now func() time.Time
}
```

- [ ] **Step 4: Implement hardware command**

`internal/cli/hardware.go`:
```go
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newHardwareCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "hardware",
		Short: "Detect chip, RAM, GPU memory, OS version; cache to hardware.json",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHardware(cmd.Context(), deps)
		},
	}
}

func runHardware(ctx context.Context, deps *Deps) error {
	info, err := deps.HardwareDetector.Detect(ctx)
	if err != nil {
		return fmt.Errorf("detect hardware: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(deps.HardwareJSONPath), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal hardware.json: %w", err)
	}
	if err := os.WriteFile(deps.HardwareJSONPath, b, 0o644); err != nil {
		return fmt.Errorf("write hardware.json: %w", err)
	}

	fmt.Fprintf(deps.Stdout, "Chip:        %s\n", nonEmpty(info.Chip, "unknown"))
	fmt.Fprintf(deps.Stdout, "RAM:         %s\n", humanBytes(info.RAMBytes))
	fmt.Fprintf(deps.Stdout, "OS:          %s\n", nonEmpty(info.OSVersion, "unknown"))
	if info.IogpuWiredLimitMB > 0 {
		fmt.Fprintf(deps.Stdout, "iogpu cap:   %d MB\n", info.IogpuWiredLimitMB)
	} else {
		fmt.Fprintln(deps.Stdout, "iogpu cap:   unset (default ~75% of RAM)")
	}
	fmt.Fprintf(deps.Stdout, "Hypervisor:  %v\n", info.HypervisorPresent)
	fmt.Fprintf(deps.Stdout, "Metal GPU:   %v\n", info.MetalDeviceDetected)
	fmt.Fprintf(deps.Stdout, "Saved to:    %s\n", deps.HardwareJSONPath)
	return nil
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func humanBytes(n uint64) string {
	if n == 0 {
		return "unknown"
	}
	gb := float64(n) / (1 << 30)
	return fmt.Sprintf("%.0f GB", gb)
}
```

- [ ] **Step 5: Wire into root**

Update `internal/cli/root.go`:
```go
package cli

import "github.com/spf13/cobra"

func NewRoot(deps *Deps, llamactlVersion string) *cobra.Command {
	root := &cobra.Command{
		Use:           "llamactl",
		Short:         "Run llama.cpp on Apple Silicon",
		Version:       llamactlVersion,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	if deps.Stdout != nil {
		root.SetOut(deps.Stdout)
	}
	if deps.Stderr != nil {
		root.SetErr(deps.Stderr)
	}
	root.AddCommand(newHardwareCmd(deps))
	return root
}
```

- [ ] **Step 6: Wire concrete deps in main.go**

Replace `cmd/llamactl/main.go`:
```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gregmundy/llamactl/internal/cli"
	"github.com/gregmundy/llamactl/internal/config"
	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/runner"
)

var llamactlVersion = "dev"

func main() {
	paths, err := config.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "llamactl: cannot resolve home directory:", err)
		os.Exit(1)
	}
	run := runner.ExecRunner{}

	deps := &cli.Deps{
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
		HardwareDetector: &hardware.Detector{Runner: run},
		HardwareJSONPath: paths.HardwareJSON(),
		Now:              time.Now,
	}

	root := cli.NewRoot(deps, llamactlVersion)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, cli.ErrUserError) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "llamactl:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 7: Run tests + smoke test on the host**

```bash
cd /Users/greg/Development/llamactl
go test ./...
go build ./cmd/llamactl
./llamactl hardware
```

Expected: tests pass; on a real Apple Silicon Mac, `./llamactl hardware` prints real chip/RAM/OS values and writes `~/.config/llamactl/hardware.json`.

- [ ] **Step 8: Commit**

```bash
git add cmd internal
git commit -m "feat: llamactl hardware command (introspect + persist)"
```

---

## Task 13: llama-server resolver

**Files:**
- Create: `internal/server/resolver.go`
- Create: `internal/server/resolver_test.go`

**Discovery order (PRD §4):**
1. `LLAMACTL_LLAMA_SERVER_PATH` env var
2. `llama_server_path` from `~/.config/llamactl/config.yaml`
3. `llama-server` on `$PATH`
4. `~/.llamavm/shims/llama-server` (direct probe)
5. `$(brew --prefix llama.cpp)/bin/llama-server`

- [ ] **Step 1: Write the failing test**

`internal/server/resolver_test.go`:
```go
package server

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	stdoutByCmd map[string]string
	errByCmd    map[string]error
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, _ string, stdout, stderr io.Writer) error {
	key := name
	if len(args) > 0 {
		key += " " + strings.Join(args, " ")
	}
	if err, ok := f.errByCmd[key]; ok {
		return err
	}
	if out, ok := f.stdoutByCmd[key]; ok {
		_, _ = io.WriteString(stdout, out)
		return nil
	}
	_ = stderr
	return errors.New("unexpected: " + key)
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_EnvVarWins(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, "from-env", "llama-server")
	touch(t, envPath)
	r := Resolver{
		Getenv:    func(k string) string { if k == "LLAMACTL_LLAMA_SERVER_PATH" { return envPath }; return "" },
		LookPath:  func(string) (string, error) { return "", errors.New("nope") },
		HomeDir:   tmp,
		ConfigPath: "/does/not/exist/config.yaml",
		Runner:    &fakeRunner{errByCmd: map[string]error{"brew --prefix llama.cpp": errors.New("nope")}},
	}
	res, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Path != envPath {
		t.Errorf("Path = %q, want %q", res.Path, envPath)
	}
	if res.Source != SourceEnv {
		t.Errorf("Source = %v, want SourceEnv", res.Source)
	}
}

func TestResolve_ConfigPathSecond(t *testing.T) {
	tmp := t.TempDir()
	cfgServer := filepath.Join(tmp, "from-cfg", "llama-server")
	touch(t, cfgServer)
	cfgFile := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("llama_server_path: "+cfgServer+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Resolver{
		Getenv:     func(string) string { return "" },
		LookPath:   func(string) (string, error) { return "", errors.New("nope") },
		HomeDir:    tmp,
		ConfigPath: cfgFile,
		Runner:     &fakeRunner{errByCmd: map[string]error{"brew --prefix llama.cpp": errors.New("nope")}},
	}
	res, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != cfgServer || res.Source != SourceConfig {
		t.Errorf("got Path=%q Source=%v", res.Path, res.Source)
	}
}

func TestResolve_PATHThird(t *testing.T) {
	r := Resolver{
		Getenv:     func(string) string { return "" },
		LookPath:   func(name string) (string, error) {
			if name == "llama-server" {
				return "/usr/local/bin/llama-server", nil
			}
			return "", errors.New("nope")
		},
		HomeDir:    "/no/such/home",
		ConfigPath: "/no/such/config",
		Runner:     &fakeRunner{errByCmd: map[string]error{"brew --prefix llama.cpp": errors.New("nope")}},
	}
	res, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "/usr/local/bin/llama-server" || res.Source != SourcePATH {
		t.Errorf("got Path=%q Source=%v", res.Path, res.Source)
	}
}

func TestResolve_LlamavmShimFourth(t *testing.T) {
	tmp := t.TempDir()
	shim := filepath.Join(tmp, ".llamavm", "shims", "llama-server")
	touch(t, shim)
	r := Resolver{
		Getenv:     func(string) string { return "" },
		LookPath:   func(string) (string, error) { return "", errors.New("nope") },
		HomeDir:    tmp,
		ConfigPath: "/no/such",
		Runner:     &fakeRunner{errByCmd: map[string]error{"brew --prefix llama.cpp": errors.New("nope")}},
	}
	res, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != shim || res.Source != SourceLlamavmShim {
		t.Errorf("got Path=%q Source=%v", res.Path, res.Source)
	}
}

func TestResolve_BrewFifth(t *testing.T) {
	tmp := t.TempDir()
	brewPrefix := filepath.Join(tmp, "homebrew", "opt", "llama.cpp")
	brewBin := filepath.Join(brewPrefix, "bin", "llama-server")
	touch(t, brewBin)
	r := Resolver{
		Getenv:     func(string) string { return "" },
		LookPath:   func(string) (string, error) { return "", errors.New("nope") },
		HomeDir:    "/no/such",
		ConfigPath: "/no/such",
		Runner: &fakeRunner{stdoutByCmd: map[string]string{
			"brew --prefix llama.cpp": brewPrefix + "\n",
		}},
	}
	res, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != brewBin || res.Source != SourceBrew {
		t.Errorf("got Path=%q Source=%v", res.Path, res.Source)
	}
}

func TestResolve_NoneReturnsErrNotFound(t *testing.T) {
	r := Resolver{
		Getenv:     func(string) string { return "" },
		LookPath:   func(string) (string, error) { return "", errors.New("nope") },
		HomeDir:    "/no/such",
		ConfigPath: "/no/such",
		Runner:     &fakeRunner{errByCmd: map[string]error{"brew --prefix llama.cpp": errors.New("not installed")}},
	}
	_, err := r.Resolve(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/server/...
```
Expected: FAIL — package missing.

- [ ] **Step 3: Implement**

`internal/server/resolver.go`:
```go
// Package server resolves and probes the llama-server binary. The resolver
// follows PRD §4 discovery order; the probe runs `llama-server --version`
// once and caches the parsed output. Both are constructed in main.go and
// passed to cli via narrow interfaces.
package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Source identifies which discovery step produced the resolved path. Useful
// for `doctor` to tell the user where the binary came from.
type Source int

const (
	SourceUnknown Source = iota
	SourceEnv
	SourceConfig
	SourcePATH
	SourceLlamavmShim
	SourceBrew
)

func (s Source) String() string {
	switch s {
	case SourceEnv:
		return "LLAMACTL_LLAMA_SERVER_PATH"
	case SourceConfig:
		return "config.yaml (llama_server_path)"
	case SourcePATH:
		return "$PATH"
	case SourceLlamavmShim:
		return "~/.llamavm/shims/llama-server"
	case SourceBrew:
		return "brew --prefix llama.cpp"
	}
	return "unknown"
}

// Resolution is the outcome of a successful Resolve.
type Resolution struct {
	Path   string
	Source Source
}

// ErrNotFound is returned by Resolve when none of the discovery steps locate
// a llama-server binary.
var ErrNotFound = errors.New("no llama-server found")

// CommandRunner is the runner.CommandRunner shape, redeclared here so this
// package has no dependency on internal/runner.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
}

// Resolver finds llama-server using the PRD §4 priority order. All
// dependencies are injectable so tests don't touch the real filesystem
// outside their TempDir.
type Resolver struct {
	Getenv     func(string) string
	LookPath   func(string) (string, error)
	HomeDir    string // typically os.UserHomeDir() at startup
	ConfigPath string // typically config.Paths{}.ConfigFile()
	Runner     CommandRunner
}

// Resolve walks the five-step discovery order and returns the first match.
// A path counts as a match only if it points to an existing file.
func (r Resolver) Resolve(ctx context.Context) (Resolution, error) {
	if p := r.Getenv("LLAMACTL_LLAMA_SERVER_PATH"); p != "" {
		if exists(p) {
			return Resolution{Path: p, Source: SourceEnv}, nil
		}
	}
	if p := r.fromConfig(); p != "" && exists(p) {
		return Resolution{Path: p, Source: SourceConfig}, nil
	}
	if p, err := r.LookPath("llama-server"); err == nil && exists(p) {
		return Resolution{Path: p, Source: SourcePATH}, nil
	}
	shim := filepath.Join(r.HomeDir, ".llamavm", "shims", "llama-server")
	if exists(shim) {
		return Resolution{Path: shim, Source: SourceLlamavmShim}, nil
	}
	if p, ok := r.fromBrew(ctx); ok && exists(p) {
		return Resolution{Path: p, Source: SourceBrew}, nil
	}
	return Resolution{}, ErrNotFound
}

func (r Resolver) fromConfig() string {
	b, err := os.ReadFile(r.ConfigPath)
	if err != nil {
		return ""
	}
	var doc struct {
		LlamaServerPath string `yaml:"llama_server_path"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return ""
	}
	return doc.LlamaServerPath
}

func (r Resolver) fromBrew(ctx context.Context) (string, bool) {
	var stdout bytes.Buffer
	if err := r.Runner.Run(ctx, "brew", []string{"--prefix", "llama.cpp"}, "", &stdout, io.Discard); err != nil {
		return "", false
	}
	prefix := strings.TrimSpace(stdout.String())
	if prefix == "" {
		return "", false
	}
	return filepath.Join(prefix, "bin", "llama-server"), true
}

func exists(p string) bool {
	if p == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// _ keeps fmt imported in case future error wrapping expands.
var _ = fmt.Sprintf
```

- [ ] **Step 4: Run to verify pass**

```bash
go test ./internal/server/...
```
Expected: PASS for all six subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/server
git commit -m "feat(server): 5-step llama-server discovery resolver"
```

---

## Task 14: llama-server version parser and probe

**Files:**
- Create: `internal/server/version.go`
- Create: `internal/server/version_test.go`
- Create: `internal/server/probe.go`
- Create: `internal/server/probe_test.go`

**Real-world `llama-server --version` output** (as of llama.cpp b9000-ish):
```
version: 4567 (a1b2c3d4)
built with Apple clang version 15.0.0 ...
```

The first line is what we parse. The build number (4567) and short SHA are both useful for `doctor` and for future flag-gating.

- [ ] **Step 1: Write the failing version test**

`internal/server/version_test.go`:
```go
package server

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantBuild int
		wantSHA   string
		wantErr   bool
	}{
		{
			name:      "standard llama.cpp output",
			input:     "version: 4567 (a1b2c3d4)\nbuilt with Apple clang version 15.0.0\n",
			wantBuild: 4567,
			wantSHA:   "a1b2c3d4",
		},
		{
			name:      "b-prefixed tag form",
			input:     "version: b4567 (a1b2c3d4)\n",
			wantBuild: 4567,
			wantSHA:   "a1b2c3d4",
		},
		{
			name:    "garbage",
			input:   "not a version",
			wantErr: true,
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := ParseVersion(c.input)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", v)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseVersion: %v", err)
			}
			if v.Build != c.wantBuild {
				t.Errorf("Build = %d, want %d", v.Build, c.wantBuild)
			}
			if v.SHA != c.wantSHA {
				t.Errorf("SHA = %q, want %q", v.SHA, c.wantSHA)
			}
		})
	}
}

func TestVersion_AtLeast(t *testing.T) {
	v := Version{Build: 4500}
	if !v.AtLeast(4000) {
		t.Error("4500 >= 4000 should be true")
	}
	if v.AtLeast(5000) {
		t.Error("4500 >= 5000 should be false")
	}
}
```

- [ ] **Step 2: Implement Version**

`internal/server/version.go`:
```go
package server

import (
	"fmt"
	"regexp"
	"strconv"
)

// Version is the parsed output of `llama-server --version`.
type Version struct {
	Build int    // monotonically increasing release counter
	SHA   string // upstream git short SHA
	Raw   string // first line of output, verbatim — useful for printing
}

func (v Version) String() string {
	if v.Build == 0 && v.SHA == "" {
		return v.Raw
	}
	return fmt.Sprintf("b%d (%s)", v.Build, v.SHA)
}

// AtLeast reports whether this version's Build is >= minBuild. Used by
// recipe assembly to gate flags like --flash-attn that require a minimum
// llama.cpp version.
func (v Version) AtLeast(minBuild int) bool { return v.Build >= minBuild }

// Output looks like: "version: 4567 (a1b2c3d4)" — optionally with a "b"
// prefix on the build number.
var versionLineRe = regexp.MustCompile(`version:\s+b?(\d+)\s+\(([^)]+)\)`)

// ParseVersion extracts the build number and SHA from `llama-server --version`
// stdout. Only the first matching line is used.
func ParseVersion(s string) (Version, error) {
	m := versionLineRe.FindStringSubmatch(s)
	if len(m) != 3 {
		return Version{}, fmt.Errorf("unrecognized llama-server --version output: %q", s)
	}
	build, err := strconv.Atoi(m[1])
	if err != nil {
		return Version{}, fmt.Errorf("parse build %q: %w", m[1], err)
	}
	return Version{Build: build, SHA: m[2], Raw: s}, nil
}
```

- [ ] **Step 3: Run version tests**

```bash
go test ./internal/server/...
```
Expected: PASS.

- [ ] **Step 4: Write probe test**

`internal/server/probe_test.go`:
```go
package server

import (
	"context"
	"errors"
	"io"
	"testing"
)

// countingRunner counts how many times Run is called and returns canned
// stdout. Used to verify caching behavior in the prober.
type countingRunner struct {
	calls int
	out   string
	err   error
}

func (c *countingRunner) Run(_ context.Context, _ string, _ []string, _ string, stdout, _ io.Writer) error {
	c.calls++
	if c.err != nil {
		return c.err
	}
	_, _ = io.WriteString(stdout, c.out)
	return nil
}

func TestProbe_RunsOnceAndCaches(t *testing.T) {
	r := &countingRunner{out: "version: 4567 (a1b2c3d4)\n"}
	p := &Prober{Runner: r}

	v1, err := p.Probe(context.Background(), "/bin/llama-server")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := p.Probe(context.Background(), "/bin/llama-server")
	if err != nil {
		t.Fatal(err)
	}
	if v1 != v2 {
		t.Fatalf("cache mismatch: %v vs %v", v1, v2)
	}
	if r.calls != 1 {
		t.Fatalf("expected one runner call, got %d", r.calls)
	}
}

func TestProbe_BinaryError(t *testing.T) {
	p := &Prober{Runner: &countingRunner{err: errors.New("exec failed")}}
	if _, err := p.Probe(context.Background(), "/bin/llama-server"); err == nil {
		t.Fatal("expected error")
	}
}

func TestProbe_PathChangeInvalidatesCache(t *testing.T) {
	r := &countingRunner{out: "version: 4567 (a1b2c3d4)\n"}
	p := &Prober{Runner: r}
	if _, err := p.Probe(context.Background(), "/path/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Probe(context.Background(), "/path/b"); err != nil {
		t.Fatal(err)
	}
	if r.calls != 2 {
		t.Fatalf("expected two runner calls after path change, got %d", r.calls)
	}
}
```

- [ ] **Step 5: Implement Prober**

`internal/server/probe.go`:
```go
package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
)

// Prober caches the parsed `llama-server --version` output keyed by the
// binary path. Used by doctor and by Phase 3's recipe-flag gating.
type Prober struct {
	Runner CommandRunner

	mu    sync.Mutex
	cache map[string]Version
}

// Probe runs `<path> --version` (only on first call per path) and returns
// the parsed Version.
func (p *Prober) Probe(ctx context.Context, path string) (Version, error) {
	p.mu.Lock()
	if v, ok := p.cache[path]; ok {
		p.mu.Unlock()
		return v, nil
	}
	p.mu.Unlock()

	var stdout bytes.Buffer
	if err := p.Runner.Run(ctx, path, []string{"--version"}, "", &stdout, io.Discard); err != nil {
		return Version{}, fmt.Errorf("run %s --version: %w", path, err)
	}
	v, err := ParseVersion(stdout.String())
	if err != nil {
		return Version{}, err
	}
	p.mu.Lock()
	if p.cache == nil {
		p.cache = make(map[string]Version)
	}
	p.cache[path] = v
	p.mu.Unlock()
	return v, nil
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/server/...
```
Expected: PASS for all version + probe subtests.

- [ ] **Step 7: Commit**

```bash
git add internal/server
git commit -m "feat(server): version parser + cached --version probe"
```

---

## Task 15: Doctor framework + Deps wiring

**Files:**
- Modify: `internal/cli/deps.go` — add ServerResolver, ServerProber, Config, LookPath, Getenv
- Create: `internal/cli/doctor.go`
- Create: `internal/cli/doctor_test.go`

- [ ] **Step 1: Extend Deps**

Replace `internal/cli/deps.go`:
```go
package cli

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/server"
)

var ErrUserError = errors.New("user error")

type HardwareDetector interface {
	Detect(ctx context.Context) (hardware.Info, error)
}

// ServerResolver locates the llama-server binary.
type ServerResolver interface {
	Resolve(ctx context.Context) (server.Resolution, error)
}

// ServerProber runs `llama-server --version` and caches the result.
type ServerProber interface {
	Probe(ctx context.Context, path string) (server.Version, error)
}

// MinLlamaServerBuild is the lowest llama.cpp build number llamactl supports.
// Below this, doctor warns and recipes fall back to a conservative flag set.
// (Choose 3500 as a starting floor — recent enough for stable Metal + flash
// attention; revise as we learn more.)
const MinLlamaServerBuild = 3500

type Deps struct {
	Stdout io.Writer
	Stderr io.Writer

	HardwareDetector HardwareDetector
	HardwareJSONPath string

	ServerResolver ServerResolver
	ServerProber   ServerProber

	LookPath func(name string) (string, error)
	Getenv   func(key string) string
	Now      func() time.Time
}
```

- [ ] **Step 2: Write the failing test**

`internal/cli/doctor_test.go`:
```go
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
		RAMBytes:            16 * 1024 * 1024 * 1024,
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
	deps.ServerProber = &fakeProber{ver: server.Version{Build: 100, SHA: "old", Raw: "version: 100 (old)"}}
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
```

- [ ] **Step 3: Implement doctor.go**

`internal/cli/doctor.go`:
```go
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
// message printed below the transcript. The bare-metal check short-circuits
// further checks when a VM without Metal is detected (and no override is
// set) — those checks would be misleading.
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
```

- [ ] **Step 4: Wire doctor into root**

Update `internal/cli/root.go`:
```go
package cli

import "github.com/spf13/cobra"

func NewRoot(deps *Deps, llamactlVersion string) *cobra.Command {
	root := &cobra.Command{
		Use:           "llamactl",
		Short:         "Run llama.cpp on Apple Silicon",
		Version:       llamactlVersion,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	if deps.Stdout != nil {
		root.SetOut(deps.Stdout)
	}
	if deps.Stderr != nil {
		root.SetErr(deps.Stderr)
	}
	root.AddCommand(newHardwareCmd(deps))
	root.AddCommand(newDoctorCmd(deps))
	return root
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/cli/...
```
Expected: all doctor tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/cli
git commit -m "feat(cli): doctor command with bare-metal/server/version/iogpu checks"
```

---

## Task 16: Wire production Deps for doctor in main.go

**Files:**
- Modify: `cmd/llamactl/main.go`

- [ ] **Step 1: Update main.go**

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/gregmundy/llamactl/internal/cli"
	"github.com/gregmundy/llamactl/internal/config"
	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/runner"
	"github.com/gregmundy/llamactl/internal/server"
)

var llamactlVersion = "dev"

func main() {
	paths, err := config.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "llamactl: cannot resolve home directory:", err)
		os.Exit(1)
	}
	run := runner.ExecRunner{}

	resolver := server.Resolver{
		Getenv:     os.Getenv,
		LookPath:   exec.LookPath,
		HomeDir:    paths.Home,
		ConfigPath: paths.ConfigFile(),
		Runner:     run,
	}

	deps := &cli.Deps{
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
		HardwareDetector: &hardware.Detector{Runner: run},
		HardwareJSONPath: paths.HardwareJSON(),
		ServerResolver:   resolver,
		ServerProber:     &server.Prober{Runner: run},
		LookPath:         exec.LookPath,
		Getenv:           os.Getenv,
		Now:              time.Now,
	}

	root := cli.NewRoot(deps, llamactlVersion)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, cli.ErrUserError) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "llamactl:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Build + smoke test**

```bash
cd /Users/greg/Development/llamactl
go build ./cmd/llamactl
./llamactl --help
./llamactl hardware
./llamactl doctor
```

Expected:
- `--help` lists `hardware` and `doctor`
- `hardware` prints real chip/RAM/OS values for this Mac mini, writes hardware.json
- `doctor` runs four checks. If you have llama.cpp installed, expect ✓ across the board; if not, ✗ on resolver + version with the documented remediation. If `iogpu.wired_limit_mb` is unset on a ≥32GB host, expect ✗ with the exact `sudo sysctl` command.

Capture the actual output of `./llamactl doctor` here as a sanity check before committing. If any field looks wrong (wrong chip name, wrong RAM, anything else), stop and investigate before continuing.

- [ ] **Step 3: Commit**

```bash
git add cmd/llamactl/main.go
git commit -m "feat: wire doctor in main.go (real resolver + prober)"
```

---

## Task 17: End-to-end smoke test + README update

**Files:**
- Modify: `README.md`
- Create: `internal/cli/integration_test.go`

- [ ] **Step 1: Write an integration test that exercises the whole graph with fakes**

`internal/cli/integration_test.go`:
```go
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

// fakeRunner used here matches the shape required by both hardware.Detector
// and server.Prober/Resolver (all consume the same CommandRunner shape).
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

	r := &intRunner{
		outputs: map[string]string{
			"system_profiler SPHardwareDataType -json": `{"SPHardwareDataType":[{"chip_type":"Apple M2 Pro"}]}`,
			"system_profiler SPDisplaysDataType -json": `{"SPDisplaysDataType":[{"_name":"d"}]}`,
			"sysctl hw.memsize":                        "hw.memsize: 34359738368\n",
			"sysctl iogpu.wired_limit_mb":              "iogpu.wired_limit_mb: 24576\n",
			"sysctl kern.hv_vmm_present":               "kern.hv_vmm_present: 0\n",
			"sw_vers -productVersion":                  "14.4.1\n",
			"/fake/llama-server --version":             "version: 5000 (deadbeef)\n",
		},
		errs: map[string]error{},
	}

	// Touch a fake llama-server file so the resolver's exists() check passes.
	binPath := filepath.Join(tmp, "fake", "llama-server")
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Update the version key to match the actual touched path.
	r.outputs[binPath+" --version"] = r.outputs["/fake/llama-server --version"]

	deps := &Deps{
		Stdout:           &bytes.Buffer{},
		Stderr:           &bytes.Buffer{},
		HardwareDetector: &hardware.Detector{Runner: r},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		ServerResolver: server.Resolver{
			Getenv:     func(k string) string { if k == "LLAMACTL_LLAMA_SERVER_PATH" { return binPath }; return "" },
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
```

- [ ] **Step 2: Run all tests**

```bash
cd /Users/greg/Development/llamactl
go test -race -count=1 ./...
```

Expected: all tests pass, including the end-to-end one.

- [ ] **Step 3: Update README with what's working**

Replace `README.md`:
```markdown
# llamactl

> Single-binary CLI for running llama.cpp on Apple Silicon.

**Status:** Phase 1 (foundation + introspection). `hardware` and `doctor` work; `add` and `serve` arrive in later phases. See `docs/llamactl-prd-v1.5.md` for the spec.

## Requirements

- macOS 14+ on Apple Silicon
- A `llama-server` binary on PATH (via `brew install llama.cpp` or `brew install gregmundy/tap/llamavm && llamavm install latest`)

## Install (development)

```bash
git clone https://github.com/gregmundy/llamactl
cd llamactl
go build ./cmd/llamactl
./llamactl --help
```

## Commands

| Command | Description |
|---------|-------------|
| `llamactl hardware` | Detect chip, RAM, OS, iogpu cap, VM state. Writes `~/.config/llamactl/hardware.json`. |
| `llamactl doctor` | Verify bare-metal Apple Silicon, llama-server resolvable, version floor, iogpu cap. Exits 2 on any failure. |

## Environment variables

| Variable | Effect |
|----------|--------|
| `LLAMACTL_LLAMA_SERVER_PATH` | Override llama-server discovery |
| `LLAMACTL_ALLOW_VM` | Permit running in a VM without Metal passthrough (NOT recommended) |

## Development

```bash
go test ./...
```
```

- [ ] **Step 4: Commit**

```bash
git add internal/cli/integration_test.go README.md
git commit -m "test: end-to-end hardware+doctor + README update"
```

---

## Wrap-up checklist

After Task 17, verify acceptance criteria for Phase 1 against the real machine:

- [ ] `./llamactl hardware` correctly identifies your M-series chip, RAM, OS — AC#5
- [ ] `./llamactl doctor` on a system without llama.cpp prints the resolver-failed message naming both Homebrew and llamavm — AC#2
- [ ] `./llamactl doctor` with Homebrew's llama.cpp installed reports the version via `--version` — AC#3
- [ ] Same with llamavm-active llama-server — AC#4
- [ ] `./llamactl doctor` flags an unset/low `iogpu.wired_limit_mb` with the exact `sudo sysctl iogpu.wired_limit_mb=<N>` command — AC#11
- [ ] On a VM without Metal passthrough (if you have one to test on), doctor refuses with the PRD §6.3 message — AC#12
- [ ] `go test -race -count=1 ./...` is green
- [ ] CI passes on push to GitHub

Once all boxes are ticked, this phase is done. Next plan: **Phase 2 — model download (`add`, `search`, `list`, `remove`)**, written after we've run Phase 1 and learned what the production hardware/resolver behavior actually looks like.
