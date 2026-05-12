# llamactl Phase 4: Homebrew Distribution + Polish — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship llamactl v1.0.0 via `brew install gregmundy/tap/llamactl` (PRD AC#1) and resolve three carryover items from Phase 3 live smoke: flash-attn syntax detection, foreground graceful shutdown, Gemma 4 PARAMS column.

**Architecture:** Polish (three small code changes inside `internal/`) + distribution (GitHub repo bootstrap + GoReleaser + two GitHub Actions workflows + cask auto-publish to `gregmundy/homebrew-tap`). All work on a `phase4-polish-and-tap` branch; tagged `v1.0.0` on `main` after merge.

**Tech Stack:** Go 1.26.2 (unchanged). New external tooling: GoReleaser v2 (via `goreleaser/goreleaser-action@v6`), GitHub Actions on macos-14, GitHub CLI (`gh`) for repo bootstrap. No new Go dependencies.

**Working branch:** `phase4-polish-and-tap`. **Every implementer prompt must state this branch and forbid `git checkout/switch/stash/branch`.**

---

## Spec coverage

| Spec § | Requirement | Task(s) |
|--------|-------------|---------|
| §3 | Polish #1: server.Capabilities + flash-attn probe | 1, 2, 3 |
| §4 | Polish #2: foreground graceful shutdown | 4 |
| §5 | Polish #3: Gemma 4 PARAMS investigation | 5, 6 |
| §6 | Repo bootstrap (gh repo create) | 11 |
| §7 | GoReleaser config | 8 |
| §8 | GitHub Actions ci.yml + release.yml | 9 |
| §9 | README refresh | 7 |
| §10 | Sequence + tagging | 0, 10, 12, 13, 14 |

---

## File structure

```
.github/workflows/
├── ci.yml                              NEW — go test ./... -race on push/PR
└── release.yml                         NEW — GoReleaser on v* tag

internal/
├── server/
│   ├── probe.go                        MODIFY — add Capabilities method
│   ├── capabilities.go                 NEW — Capabilities struct + parseHelpForCaps
│   └── capabilities_test.go            NEW — parse tests + cache test
│
├── recipes/
│   ├── recipes.go                      MODIFY — FlagsFor signature + emission rule
│   └── recipes_test.go                 MODIFY — pass Capabilities; legacy syntax test
│
└── cli/
    ├── deps.go                         MODIFY — extend ServerProber interface
    ├── serve.go                        MODIFY — call Capabilities; graceful shutdown
    ├── serve_test.go                   MODIFY — update fakeProberPhase3
    ├── doctor_test.go                  MODIFY — update fakeProber
    ├── integration_test.go             MODIFY — add foreground integration test
    ├── list.go                         MODIFY (conditional) — "?" for blank PARAMS
    └── list_test.go                    MODIFY (conditional)

cmd/gguf-inspect/main.go                NEW — one-shot diagnostic tool

.goreleaser.yml                         NEW
README.md                               MODIFY — rewrite for v1.0.0
```

---

## Task 0: Create feature branch

**Files:** none (git only)

- [ ] **Step 1: Confirm starting state**

```bash
cd /Users/greg/Development/llamactl
git status && git branch --show-current && git log -1 --oneline
```
Expected: clean working tree, on `main` at `5504a5b` (phase 4 spec) or later.

- [ ] **Step 2: Create branch**

```bash
git checkout -b phase4-polish-and-tap
git branch --show-current
```
Expected: `phase4-polish-and-tap`.

---

## Task 1: `server.Capabilities` — type + parser

**Files:**
- Create: `internal/server/capabilities.go`
- Create: `internal/server/capabilities_test.go`

Pure type + pure parser. No subprocess yet (Task 2 wires Prober).

- [ ] **Step 1: Write the failing test**

Create `internal/server/capabilities_test.go`:

```go
package server

import "testing"

func TestParseHelpForCaps_Tristate(t *testing.T) {
	// Recent llama-server help output (b1 d05fe1d, late 2026).
	help := `usage: llama-server [options]

-fa,   --flash-attn [on|off|auto]       set Flash Attention use ('on', 'off', or 'auto', default: 'auto')
                                        (env: LLAMA_ARG_FLASH_ATTN)
`
	caps := parseHelpForCaps(help)
	if !caps.FlashAttnTristate {
		t.Errorf("FlashAttnTristate = false, want true")
	}
}

func TestParseHelpForCaps_LegacyBoolean(t *testing.T) {
	// Pre-tristate llama-server help (Homebrew b4500 era).
	help := `usage: llama-server [options]

  -fa, --flash-attn                       enable Flash Attention (default: disabled)
`
	caps := parseHelpForCaps(help)
	if caps.FlashAttnTristate {
		t.Errorf("FlashAttnTristate = true, want false (legacy bare-flag syntax)")
	}
}

func TestParseHelpForCaps_FlashAttnAbsent(t *testing.T) {
	// Hypothetical build with no flash-attn flag at all.
	help := `usage: llama-server [options]

  -h, --help                              print usage
`
	caps := parseHelpForCaps(help)
	if caps.FlashAttnTristate {
		t.Errorf("FlashAttnTristate = true on help with no flash-attn line at all")
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/server/... -run Caps
```
Expected: undefined symbols `parseHelpForCaps`, `caps.FlashAttnTristate`.

- [ ] **Step 3: Write `internal/server/capabilities.go`**

```go
package server

import "strings"

// Capabilities is the per-binary feature surface detected from
// `llama-server --help`. Each field gates a recipe-emission decision.
type Capabilities struct {
	// FlashAttnTristate is true when --help shows the modern tristate
	// signature `--flash-attn [on|off|auto]`. Pre-tristate llama.cpp
	// builds accept a bare `--flash-attn` flag and would error with
	// "expected value" if we passed `--flash-attn on`.
	FlashAttnTristate bool
}

// parseHelpForCaps extracts capability flags from the combined
// stdout+stderr of `llama-server --help`. Best-effort: any field whose
// signal isn't found stays at its zero value.
func parseHelpForCaps(help string) Capabilities {
	var c Capabilities
	// Look for the tristate signature anywhere in the help text.
	// Pre-tristate help shows just "--flash-attn" followed by description.
	// Modern help shows "--flash-attn [on|off|auto]".
	for _, line := range strings.Split(help, "\n") {
		if !strings.Contains(line, "--flash-attn") {
			continue
		}
		if strings.Contains(line, "[on|off|auto]") || strings.Contains(line, "[on") {
			c.FlashAttnTristate = true
			break
		}
	}
	return c
}
```

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/server/... -run Caps -v
go vet ./internal/server/...
```
Expected: 3 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/server/capabilities.go internal/server/capabilities_test.go
git commit -m "feat(server): Capabilities type + parseHelpForCaps (flash-attn tristate detection)"
```

---

## Task 2: Wire `Capabilities` through `Prober`

**Files:**
- Modify: `internal/server/probe.go`
- Modify: `internal/server/capabilities_test.go` (append Prober-cache tests)

Add `Prober.Capabilities(ctx, path)` that invokes `<path> --help`, parses, and caches per binary path — same pattern as `Probe(...) (Version, error)`.

- [ ] **Step 1: Append failing tests to `internal/server/capabilities_test.go`**

```go
// Add to capabilities_test.go (above the existing parse tests is fine).
import (
	"context"
	"errors"
	"io"
	"strings"
)

type capsFakeRunner struct {
	helpOut string
	helpErr error
	calls   int
}

func (r *capsFakeRunner) Run(_ context.Context, name string, args []string, _ string, stdout, _ io.Writer) error {
	r.calls++
	if len(args) > 0 && args[0] == "--help" {
		if r.helpErr != nil {
			return r.helpErr
		}
		_, _ = io.WriteString(stdout, r.helpOut)
		return nil
	}
	return errors.New("unexpected args")
}

func TestProberCapabilitiesParsesAndCaches(t *testing.T) {
	r := &capsFakeRunner{helpOut: "--flash-attn [on|off|auto] do thing\n"}
	p := &Prober{Runner: r}

	caps, err := p.Capabilities(context.Background(), "/x/llama-server")
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if !caps.FlashAttnTristate {
		t.Errorf("FlashAttnTristate = false, want true")
	}
	if r.calls != 1 {
		t.Errorf("calls = %d, want 1", r.calls)
	}

	// Second call: should be cached, no extra runner invocation.
	_, _ = p.Capabilities(context.Background(), "/x/llama-server")
	if r.calls != 1 {
		t.Errorf("after 2nd call, calls = %d, want 1 (cache miss)", r.calls)
	}
}

func TestProberCapabilitiesRunnerError(t *testing.T) {
	r := &capsFakeRunner{helpErr: errors.New("boom")}
	p := &Prober{Runner: r}
	_, err := p.Capabilities(context.Background(), "/x/llama-server")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want wraps 'boom'", err)
	}
}
```

The `import` block at the top of `capabilities_test.go` currently only has `"testing"`. Replace it with the merged block above. If you prefer, split the cache tests into a new file `internal/server/capabilities_cache_test.go` with its own imports — same package, same test runner, no functional difference.

- [ ] **Step 2: Run, confirm fail**

```bash
cd /Users/greg/Development/llamactl
go test ./internal/server/... -run Capabilities
```
Expected: `Prober.Capabilities` undefined.

- [ ] **Step 3: Modify `internal/server/probe.go`**

Extend the `Prober` struct with a second cache and add the `Capabilities` method. Replace the file's contents with:

```go
package server

import (
	"bytes"
	"context"
	"fmt"
	"sync"
)

// Prober caches `llama-server --version` and `--help` output keyed by the
// binary path. Used by doctor, recipe-flag gating, and capability-aware
// argv emission.
type Prober struct {
	Runner CommandRunner

	mu        sync.Mutex
	versions  map[string]Version
	capsCache map[string]Capabilities
}

// Probe runs `<path> --version` (only on first call per path) and returns
// the parsed Version.
func (p *Prober) Probe(ctx context.Context, path string) (Version, error) {
	p.mu.Lock()
	if v, ok := p.versions[path]; ok {
		p.mu.Unlock()
		return v, nil
	}
	p.mu.Unlock()

	var combined bytes.Buffer
	if err := p.Runner.Run(ctx, path, []string{"--version"}, "", &combined, &combined); err != nil {
		return Version{}, fmt.Errorf("run %s --version: %w", path, err)
	}
	v, err := ParseVersion(combined.String())
	if err != nil {
		return Version{}, err
	}
	p.mu.Lock()
	if p.versions == nil {
		p.versions = make(map[string]Version)
	}
	p.versions[path] = v
	p.mu.Unlock()
	return v, nil
}

// Capabilities runs `<path> --help` (only on first call per path) and
// returns capability flags parsed from the help text. Returns the zero
// Capabilities (and a wrapped error) if the subprocess fails — callers
// should treat that as "assume legacy syntax."
func (p *Prober) Capabilities(ctx context.Context, path string) (Capabilities, error) {
	p.mu.Lock()
	if c, ok := p.capsCache[path]; ok {
		p.mu.Unlock()
		return c, nil
	}
	p.mu.Unlock()

	var combined bytes.Buffer
	if err := p.Runner.Run(ctx, path, []string{"--help"}, "", &combined, &combined); err != nil {
		return Capabilities{}, fmt.Errorf("run %s --help: %w", path, err)
	}
	c := parseHelpForCaps(combined.String())
	p.mu.Lock()
	if p.capsCache == nil {
		p.capsCache = make(map[string]Capabilities)
	}
	p.capsCache[path] = c
	p.mu.Unlock()
	return c, nil
}
```

The struct field `cache` is renamed to `versions` and `capsCache` is added. If anything in the codebase references `Prober.cache` directly, that compile error tells you to update it — but a quick grep confirms nothing does.

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/server/... -v
go vet ./internal/server/...
go build ./...
```
Expected: 5 tests pass (3 parser + 2 cache). Build clean across all packages — the rename of `cache` → `versions` is internal to probe.go.

- [ ] **Step 5: Commit**

```bash
git add internal/server/probe.go internal/server/capabilities_test.go
git commit -m "feat(server): Prober.Capabilities — cached --help parse for flash-attn syntax"
```

---

## Task 3: `recipes.FlagsFor` signature change + caller updates

**Files:**
- Modify: `internal/recipes/recipes.go`
- Modify: `internal/recipes/recipes_test.go`
- Modify: `internal/cli/deps.go` (extend ServerProber interface)
- Modify: `internal/cli/serve.go` (call Capabilities, pass to FlagsFor)
- Modify: `internal/cli/serve_test.go` (extend fakeProberPhase3)
- Modify: `internal/cli/doctor_test.go` (extend fakeProber)

This task is large because the signature change ripples to every caller. Do it as one commit so the tree never sits in a broken state.

- [ ] **Step 1: Modify `internal/recipes/recipes.go`**

Replace the `FlagsFor` function (only — leave types/constants/`shouldAddFlashAttn` alone) with:

```go
// FlagsFor assembles the llama-server argv. Inputs are read-only.
// `caps.FlashAttnTristate` selects between modern `--flash-attn on` and
// legacy bare-flag syntax.
func FlagsFor(r Recipe, m models.Model, _ models.Quant, ggufPath string,
	hw hardware.Info, ver server.Version, caps server.Capabilities,
	sizeGB float64, port int) []string {

	ctxSize := r.CtxSize
	if m.MaxCtx > 0 && m.MaxCtx < ctxSize {
		ctxSize = m.MaxCtx
	}

	threads := platform.Default{}.Cores() - 2
	if threads < 1 {
		threads = 1
	}

	args := []string{
		"--model", ggufPath,
		"--host", "0.0.0.0",
		"--port", fmt.Sprintf("%d", port),
		"--ctx-size", fmt.Sprintf("%d", ctxSize),
		"--n-gpu-layers", "999",
		"--cache-type-k", r.CacheTypeK,
		"--cache-type-v", r.CacheTypeV,
		"--threads", fmt.Sprintf("%d", threads),
	}

	if r.MlockMode == MlockAuto {
		usableGB := models.GpuAddressableGB(hw) - models.OSOverheadGB - models.HeadroomGB
		if usableGB > sizeGB+4.0 {
			args = append(args, "--mlock")
		}
	}

	if shouldAddFlashAttn(ver) {
		if caps.FlashAttnTristate {
			args = append(args, "--flash-attn", "on")
		} else {
			args = append(args, "--flash-attn")
		}
	}

	return args
}
```

- [ ] **Step 2: Update tests in `internal/recipes/recipes_test.go`**

Every existing call to `FlagsFor(..., 4.4, 8080)` needs to become `FlagsFor(..., server.Capabilities{FlashAttnTristate: true}, 4.4, 8080)` so the asserted output (`--flash-attn on`) is preserved.

Search-and-replace (with the right number of args — there are several call sites):

```bash
grep -n "FlagsFor(" internal/recipes/recipes_test.go
```

For each call site, insert `server.Capabilities{FlashAttnTristate: true}` as the new second-to-last positional arg (immediately before the `sizeGB` arg). Example transformation:

Before:
```go
args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/x", mkHW(64), mkVer(4500), 4.4, 8080)
```
After:
```go
args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/x", mkHW(64), mkVer(4500), server.Capabilities{FlashAttnTristate: true}, 4.4, 8080)
```

Apply this to every `FlagsFor(...)` call in the file. (Should be 9 call sites based on existing tests.)

Append a new test for the legacy path:

```go
func TestFlagsFor_FlashAttnLegacySyntaxOnOldBuild(t *testing.T) {
	// Modern build threshold met, but caps report legacy (no tristate).
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/x", mkHW(64),
		mkVer(4500), server.Capabilities{FlashAttnTristate: false}, 4.4, 8080)
	// Should contain bare "--flash-attn" but NOT "--flash-attn", "on".
	if !argvHasFlag(args, "--flash-attn") {
		t.Error("expected --flash-attn (bare) when caps say not tristate")
	}
	for i, a := range args {
		if a == "--flash-attn" && i+1 < len(args) && args[i+1] == "on" {
			t.Errorf("argv[%d:%d] = [%q, %q] — should be bare --flash-attn, not tristate", i, i+2, a, args[i+1])
		}
	}
}
```

- [ ] **Step 3: Modify `internal/cli/deps.go`**

Extend the `ServerProber` interface (around line 30) to require `Capabilities`:

```go
// ServerProber runs `llama-server --version` / `--help` and caches results.
type ServerProber interface {
	Probe(ctx context.Context, path string) (server.Version, error)
	Capabilities(ctx context.Context, path string) (server.Capabilities, error)
}
```

- [ ] **Step 4: Modify `internal/cli/serve.go`**

Around line 55, after `Probe`, call `Capabilities`. Then update the `FlagsFor` call:

Find:
```go
	ver, err := d.ServerProber.Probe(ctx, resolution.Path)
	if err != nil {
		return fmt.Errorf("probe llama-server: %w", err)
	}
	if ver.Build < MinLlamaServerBuild {
```

Insert after the `if ver.Build` block (keep that block as-is), but BEFORE the `recipe, ok := recipes.Recipes[recipeName]` block:

```go
	caps, err := d.ServerProber.Capabilities(ctx, resolution.Path)
	if err != nil {
		// Capabilities probe failed — log and assume legacy syntax.
		// llama-server will reject `--flash-attn on` on truly old builds,
		// but legacy emission gives the best chance of compatibility.
		fmt.Fprintf(d.Stderr, "llamactl: warning: capability probe failed (%v); assuming legacy syntax\n", err)
		caps = server.Capabilities{}
	}
```

Find the `argv := recipes.FlagsFor(...)` line (around line 86) and add `caps` before `sizeGB`:

Before:
```go
	argv := recipes.FlagsFor(recipe, model, meta.Quant, meta.GGUFPath, hw, ver, sizeGB, chosen)
```
After:
```go
	argv := recipes.FlagsFor(recipe, model, meta.Quant, meta.GGUFPath, hw, ver, caps, sizeGB, chosen)
```

Verify `internal/server` is in the import block (it is — `server.Version` is referenced for the build check).

- [ ] **Step 5: Modify `internal/cli/serve_test.go`**

Add `Capabilities` method to `fakeProberPhase3`. Find:

```go
type fakeProberPhase3 struct{ Version server.Version }

func (f fakeProberPhase3) Probe(_ context.Context, _ string) (server.Version, error) {
	return f.Version, nil
}
```

Replace with:

```go
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
```

The existing `Caps` field defaults to zero (`FlashAttnTristate: false`) which means existing tests will now produce bare `--flash-attn`. That's actually the correct legacy behavior. Existing serve_test.go tests don't assert anything about the `--flash-attn` flag specifically (they check port, mlock, model paths) so no further changes are needed there.

- [ ] **Step 6: Modify `internal/cli/doctor_test.go`**

Find the `fakeProber` definition near the top (around line 23):

```go
type fakeProber struct {
	ver server.Version
	err error
}

func (f *fakeProber) Probe(_ context.Context, _ string) (server.Version, error) {
	return f.ver, f.err
}
```

Add the `Capabilities` method:

```go
func (f *fakeProber) Capabilities(_ context.Context, _ string) (server.Capabilities, error) {
	return server.Capabilities{}, nil
}
```

- [ ] **Step 7: Build + test**

```bash
cd /Users/greg/Development/llamactl
go build ./...
go test ./...
go vet ./...
```
Expected: all packages build, all tests pass. The new recipes test passes; existing serve/doctor tests pass (the zero-value Caps doesn't affect assertions they make).

If `internal/cli/integration_test.go` calls `&server.Prober{Runner: r}` directly (it does, around line 101 from the earlier grep), that still satisfies the interface because the real Prober now has both Probe and Capabilities methods. But the underlying `r` (`intRunner`) doesn't know how to handle `--help` requests and will return `os.ErrNotExist`. The integration tests that exercise `Capabilities` need either:
- a runner that serves `--help` responses, OR
- to swap `ServerProber` with `fakeProberPhase3` for tests that traverse the serve path

Quick fix: extend the existing `intRunner.outputs` map in integration tests to include a `--help` response. Specifically, find any integration test that traverses `runRoot(t, d, "serve", ...)` and add an entry like:
```go
outputs: map[string]string{
    "/path --version": "version: 4500 (abc)",
    "/path --help":    "--flash-attn [on|off|auto]\n",
},
```
Adjust as needed for whichever integration tests fail.

- [ ] **Step 8: Commit**

```bash
git add internal/recipes/recipes.go internal/recipes/recipes_test.go \
        internal/cli/deps.go internal/cli/serve.go internal/cli/serve_test.go \
        internal/cli/doctor_test.go internal/cli/integration_test.go
git commit -m "feat(recipes): FlagsFor takes Capabilities; emit --flash-attn on/bare per probe"
```

(Include `integration_test.go` in the commit only if it needed changes for Step 7.)

---

## Task 4: Foreground graceful shutdown

**Files:**
- Modify: `internal/cli/serve.go`
- Modify: `internal/cli/integration_test.go` (add foreground integration test)
- Modify: `internal/cli/testdata/fakellamaserver/main.go` (already exists from Phase 3 Task 16; verify it honors SIGTERM and logs "shutting down")

The Phase 3 integration_test.go test (`TestIntegrationPhase3DetachedRoundtrip`) exists; this task adds a foreground complement.

- [ ] **Step 1: Verify the fake llama-server already handles SIGTERM**

```bash
cat /Users/greg/Development/llamactl/internal/cli/testdata/fakellamaserver/main.go
```
Expected: the file from Phase 3 with `signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)` and `fmt.Println("shutting down")` on signal. If those are absent (they should be present per the Phase 3 plan), add them now. The reference body from Phase 3 Task 16:

```go
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	fmt.Println("loaded model")
	for _, a := range os.Args[1:] {
		if a == "--version" {
			fmt.Println("version: test")
			fmt.Println("build: 99999")
			return
		}
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-sigs:
		fmt.Println("shutting down")
	case <-time.After(30 * time.Second):
		fmt.Println("timeout")
	}
}
```

- [ ] **Step 2: Modify `internal/cli/serve.go` `runServeForeground`**

The current function (around line 115) constructs `exec.CommandContext(ctx, llamaServer, argv...)` and calls `cmd.Run()`. Cancel defaults to SIGKILL. Replace the body with:

```go
func runServeForeground(ctx context.Context, d *Deps, id, llamaServer string, argv []string, port int, recipeName string) error {
	if err := os.MkdirAll(d.LogsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir logs: %w", err)
	}
	logPath := filepath.Join(d.LogsDir, id+".log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log %s: %w", logPath, err)
	}
	defer logFile.Close()

	fmt.Fprintf(d.Stdout, "starting llama-server (recipe=%s, port=%d)…\n", recipeName, port)

	cmd := exec.CommandContext(ctx, llamaServer, argv...)
	// Override the default Cancel (SIGKILL) with SIGTERM + 5s grace.
	// llama-server flushes Metal state on SIGTERM in well under that.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdout = io.MultiWriter(logFile, d.Stdout)
	cmd.Stderr = io.MultiWriter(logFile, d.Stderr)
	return cmd.Run()
}
```

Add `"syscall"` to the import block of `internal/cli/serve.go` if not already present. (`"time"` is already imported.)

- [ ] **Step 3: Add foreground integration test to `internal/cli/integration_test.go`**

Append at the end of the file:

```go
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
```

This test relies on:
- `buildFakeLlamaServer` (Phase 3 Task 16, already present)
- `NewRoot` (Phase 1, already present)
- `fakeProberPhase3` extended in Task 3 (has `Caps` field)
- `Deps.Stdout`/`Stderr` set to `io.Discard` so we don't pollute test output

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/cli/... -run "TestIntegrationPhase4ForegroundGracefulShutdown" -v
go test ./...
go vet ./...
```
Expected: the new test passes. The full suite stays green.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/serve.go internal/cli/integration_test.go
git commit -m "fix(serve): SIGTERM with 5s WaitDelay on ctx cancel; add foreground integration test"
```

---

## Task 5: GGUF inspector (diagnostic for Polish #3)

**Files:**
- Create: `cmd/gguf-inspect/main.go`

One-shot tool to dump a GGUF file's metadata keys/values/types. Output drives Task 6's fix decision.

- [ ] **Step 1: Create `cmd/gguf-inspect/main.go`**

```go
// gguf-inspect dumps the top-level metadata of a GGUF file.
//
// Usage:
//   go run ./cmd/gguf-inspect <path-to-gguf>
//
// Prints one line per key: "<key> (<type>) = <value>". Tokenizer arrays
// are shown as "<key> (array, skipped)".
package main

import (
	"fmt"
	"os"

	"github.com/gregmundy/llamactl/internal/gguf"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gguf-inspect <file.gguf>")
		os.Exit(2)
	}
	h, err := gguf.ReadHeader(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	// Header is the public struct; print all populated fields.
	fmt.Printf("Architecture:      %s\n", h.Architecture)
	fmt.Printf("ContextLength:     %d\n", h.ContextLength)
	fmt.Printf("ParameterCount:    %d\n", h.ParameterCount)
	// If the gguf package exposes raw metadata (Extras / KV map), print it
	// here. Inspect internal/gguf/header.go to confirm what's available.
}
```

Check `internal/gguf/header.go:23` (`type Header struct{ ... }`) to confirm the exact field names. If the struct has different fields (e.g., `Arch` instead of `Architecture`), adjust the format strings.

- [ ] **Step 2: Build + run against Gemma 4 and Qwen**

```bash
cd /Users/greg/Development/llamactl
go build -o /tmp/gguf-inspect ./cmd/gguf-inspect
/tmp/gguf-inspect ~/.local/share/llama-models/gemma-4-e4b-it/Q4_K_M.gguf
/tmp/gguf-inspect ~/.local/share/llama-models/qwen2.5-3b-instruct/Q5_K_M.gguf
```

**Record the output of both commands in the commit message body.** The diff between Gemma and Qwen output is the input for Task 6.

- [ ] **Step 3: Commit**

```bash
git add cmd/gguf-inspect/main.go
git commit -m "tool(gguf): cmd/gguf-inspect for diagnosing metadata reads

Gemma 4 E4B output:
<paste output>

Qwen 2.5 3B output:
<paste output>

Diff: <one sentence describing what's different — e.g., 'Gemma reports
ParameterCount=0 because general.parameter_count is absent from this
GGUF variant' or 'Gemma reports ParameterCount=4360000000 but our int
conversion truncates to int(4.36e9)=4'>"
```

---

## Task 6: Polish #3 fix (driven by Task 5 findings)

**Files:** depend on Task 5 findings. Three possible paths.

Read the Task 5 commit message body before starting. Pick the path that matches the diagnosis.

### Path A — ParameterCount is absent from Gemma 4 GGUF

Most likely outcome. The Gemma 4 author didn't write `general.parameter_count` into the metadata. Our parser returns 0. List displays blank.

**Fix:** make `list` distinguish "unknown" from "small."

- [ ] **Step A1: Modify `internal/cli/list.go`**

Find the existing block:
```go
params := ""
if m.ParamsB > 0 {
    params = fmt.Sprintf("%dB", m.ParamsB)
}
```
Replace with:
```go
params := "?"
if m.ParamsB > 0 {
    params = fmt.Sprintf("%dB", m.ParamsB)
}
```

- [ ] **Step A2: Update `internal/cli/list_test.go`**

If an existing test asserts that PARAMS is blank for ParamsB==0, change the expectation to `?`. Look for any test that creates a `Metadata` with `ParamsB: 0` (or omits the field) and asserts list output.

Append a new test:
```go
func TestListShowsQuestionMarkForUnknownParams(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "x.gguf")
	_ = os.WriteFile(existing, []byte("xxx"), 0o644)
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID: "no-params", Quant: models.Q4_K_M, GGUFPath: existing, SizeBytes: 3,
		AddedAt: time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		// ParamsB intentionally zero.
	})
	d := &Deps{ModelStore: store, FS: OSFileSystem{}}
	out, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Should show "?" for the params column.
	if !strings.Contains(out, "?") {
		t.Errorf("expected '?' for unknown params:\n%s", out)
	}
}
```

- [ ] **Step A3: Run tests + commit**

```bash
go test ./internal/cli/... -run TestList -v
git add internal/cli/list.go internal/cli/list_test.go
git commit -m "feat(cli): list shows '?' for models with unknown param count

Gemma 4 GGUF variants don't write general.parameter_count to metadata;
the empty PARAMS column was indistinguishable from a sub-1B model.
'?' makes the unknown case explicit."
```

### Path B — ParameterCount is present but truncated by int conversion

E4B might report 4.36e9 → `int(4.36e9 / 1e9) = 4`. Wait — if it's truncated to 4, list would show `4B`, not blank. So this path only applies if the truncation goes to 0 (sub-1B precision loss).

If Gemma 4 E4B reports something like 596049920 (sub-1B), `int(596049920/1e9) = 0` → blank.

**Fix:** switch PARAMS rendering to show a sensible value for sub-1B counts.

- [ ] **Step B1: Modify `internal/cli/list.go`**

Replace the `params := ""` block:
```go
params := "?"
switch {
case m.ParamsB > 0:
    params = fmt.Sprintf("%dB", m.ParamsB)
}
```

This is the minimal change; the existing parser stores ParamsB as an int. A richer fix would require touching `internal/gguf` to expose the raw float, plus changing `Metadata.ParamsB` to a float, plus migrating existing metadata files — too much for this task. The `?` fallback covers both the absent-field and truncated-to-zero cases.

- [ ] **Step B2: Run tests + commit** (same as Path A2 + A3)

### Path C — Some other failure mode (unsupported value type, etc.)

If Task 5 surfaces an unexpected GGUF parser limitation (e.g., Gemma 4 uses a value type our parser skips), the right fix is in `internal/gguf/header.go`.

- [ ] **Step C1: Extend the parser**

Look at `internal/gguf/header.go:165` `readValue(r, kind uint32)` switch. Add the missing case. Keep the test pattern from `internal/gguf/header_test.go` for new value types.

- [ ] **Step C2: Add a parser test, run, commit**

Write a test in `internal/gguf/header_test.go` that synthesizes the offending value type and confirms the parser now reads it. Commit with a message naming the value type number.

### Choose-your-path completion

Whichever path applies, the task ends with a green test suite and a single commit. If unsure between Path A and Path B, default to Path A (the `?` fix in list.go) — it handles both absent and zero-truncated cases without changing the parser.

---

## Task 7: README refresh

**Files:**
- Modify: `README.md`

Replace the current README contents with a v1.0.0 version that covers all 9 commands and points users at `brew install gregmundy/tap/llamactl`.

- [ ] **Step 1: Read the current README**

```bash
wc -l /Users/greg/Development/llamactl/README.md
cat /Users/greg/Development/llamactl/README.md
```

- [ ] **Step 2: Write the new README**

Use this content as the basis; adjust phrasing to match the existing tone if the current README has a strong voice. Don't preserve sections that are stale (e.g., "build from source" if it's the primary install path — keep it but demote it).

```markdown
# llamactl

A single-binary Go CLI for running llama.cpp on Apple Silicon. llamactl handles model
download, hardware-aware quantization selection, recipe-based llama-server invocation,
launchd lifecycle, and a doctor that catches the configuration mistakes people actually make.

Built and tested on M-series Macs. Linux and Intel are out of scope for v1.

## Install

    brew install gregmundy/tap/llamactl

llamactl is a Homebrew Cask (prebuilt unsigned binary, ~3 MB). It does NOT auto-install
`llama.cpp` — you bring your own:

    brew install llama.cpp                          # one option
    brew install gregmundy/tap/llamavm              # the other; manages multiple builds

Then check the environment:

    llamactl doctor

## Quick start

    llamactl add qwen2.5-3b-instruct                # downloads, picks Q4_K_M or higher
    llamactl serve qwen2.5-3b-instruct --detach     # registers a launchd service
    curl http://localhost:8080/v1/chat/completions  # OpenAI-compatible endpoint
    llamactl status                                 # MEM, UPTIME, TOK/S
    llamactl stop qwen2.5-3b-instruct               # bootout + plist removal

## Commands

| Command   | What it does                                                     |
|-----------|------------------------------------------------------------------|
| `hardware`| Detect chip, RAM, GPU memory, OS version; cache to hardware.json |
| `doctor`  | Run 10 health checks; exits 2 on any failure                     |
| `search`  | Search HuggingFace for GGUF repos (preferred IDs marked `*`)     |
| `add`     | Download a preferred short-id or any HF GGUF repo                |
| `list`    | List installed models with PARAMS, SIZE, ADDED, LAST-SERVED      |
| `remove`  | Remove metadata (use `--purge` to also delete the GGUF)          |
| `serve`   | Run llama-server (foreground or `--detach` to launchd)           |
| `status`  | Show running detached services (table or `--json`)               |
| `stop`    | Stop a service (or all services if no id)                        |

## Recipes

`serve` flags are assembled from a named recipe. Default is `chat`.

| Recipe         | Context  | KV cache    | Notes                          |
|----------------|----------|-------------|--------------------------------|
| `chat`         | 8 K      | f16         | Default                        |
| `code`         | 16 K     | f16         | Longer context for code work   |
| `long-context` | 32 K     | q8_0        | Memory-efficient large context |
| `low-memory`   | 4 K      | q4_0        | Minimal footprint              |

## Storage

| Path                                              | What lives there                          |
|---------------------------------------------------|-------------------------------------------|
| `~/.local/share/llama-models/<id>/<quant>.gguf`   | Model weights (shared with llamavm)       |
| `~/.config/llamactl/models/<id>.json`             | Per-model metadata (llamactl-specific)    |
| `~/.cache/llamactl/hf-*`                          | HuggingFace API cache                     |
| `~/Library/LaunchAgents/com.llamactl.<id>.plist`  | Detached-service plists                   |
| `~/Library/Logs/llamactl/<id>.log`                | Per-model server logs                     |

## Build from source

    git clone https://github.com/gregmundy/llamactl
    cd llamactl
    go build -o llamactl ./cmd/llamactl
    ./llamactl --help

## License

MIT — see [LICENSE](LICENSE).
```

Note: the install URL must use `gregmundy/tap` (the formula path), not `gregmundy/llamactl`. The cask lives in `gregmundy/homebrew-tap`.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: refresh README for v1.0.0 (brew install, all 9 commands)"
```

---

## Task 8: GoReleaser config

**Files:**
- Create: `.goreleaser.yml`
- Modify: `.gitignore` (verify `/dist/` is ignored — it already is per Phase 4 setup check)

- [ ] **Step 1: Create `.goreleaser.yml`**

```yaml
version: 2
project_name: llamactl

before:
  hooks:
    - go mod tidy

builds:
  - id: llamactl
    main: ./cmd/llamactl
    binary: llamactl
    env:
      - CGO_ENABLED=0
    goos: [darwin]
    goarch: [arm64]
    ldflags:
      - -s -w -X main.llamactlVersion={{.Version}}

archives:
  - id: default
    ids:
      - llamactl
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format: tar.gz
    files:
      - LICENSE*
      - README.md

checksum:
  name_template: "checksums.txt"

snapshot:
  version_template: "{{ incpatch .Version }}-next"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^chore:"
      - "^test:"

homebrew_casks:
  - name: llamactl
    binaries:
      - llamactl
    repository:
      owner: gregmundy
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}"
    homepage: "https://github.com/gregmundy/llamactl"
    description: "Run llama.cpp on Apple Silicon"
    license: "MIT"
    hooks:
      post:
        install: |
          if OS.mac?
            system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", staged_path]
          end
    caveats: |
      Verify your environment after install:

        llamactl doctor

      Add your first model:

        llamactl add qwen2.5-3b-instruct
```

- [ ] **Step 2: Verify `.gitignore` excludes `/dist/`**

```bash
grep "^/dist" /Users/greg/Development/llamactl/.gitignore
```
Expected: `/dist/` already present (verified earlier in this session). If missing, append it.

- [ ] **Step 3: Local dry-run (optional but valuable)**

```bash
brew install --quiet goreleaser
goreleaser release --snapshot --clean --skip=homebrew-cask
ls dist/
```
Expected: builds an `llamactl_0.0.0-next_darwin_arm64.tar.gz` in `dist/`. Skips the cask publish since that requires a real release. Cleanup: `rm -rf dist/`.

If goreleaser isn't installed and you don't want to install it locally, skip this step — the workflow will exercise it.

- [ ] **Step 4: Commit**

```bash
git add .goreleaser.yml
git commit -m "build(release): GoReleaser config — darwin/arm64 + Homebrew cask"
```

---

## Task 9: GitHub Actions — CI

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Create `.github/workflows/ci.yml`**

```yaml
name: ci

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
          go-version: '1.26'
          cache: true

      - run: go vet ./...
      - run: go build ./...
      - run: go test ./... -race
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: go vet + build + test -race on push/PR"
```

---

## Task 10: GitHub Actions — Release

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create `.github/workflows/release.yml`**

```yaml
name: release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  goreleaser:
    runs-on: macos-14
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
          cache: true

      - uses: goreleaser/goreleaser-action@v6
        with:
          distribution: goreleaser
          version: '~> v2'
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          # Fine-grained PAT scoped to gregmundy/homebrew-tap (Contents: write).
          # The default GITHUB_TOKEN is scoped to this repo and would 403 when
          # pushing to a different repo.
          HOMEBREW_TAP_GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: release workflow — GoReleaser on v* tags, publishes cask to homebrew-tap"
```

---

## Task 11: Merge to main + bootstrap GitHub repo

**Files:** none (git/gh operations only)

- [ ] **Step 1: Final pre-merge check**

```bash
cd /Users/greg/Development/llamactl
git status                  # clean
git log --oneline main..HEAD   # ~10 commits across Tasks 1-10
go test ./... -race         # all pass
go vet ./...                # clean
```

- [ ] **Step 2: Merge to main**

```bash
git checkout main
git merge --no-ff phase4-polish-and-tap -m "Merge phase4-polish-and-tap: Homebrew distribution + polish

Polish:
- server.Capabilities: cached --help probe; recipes emit --flash-attn on
  (tristate) or bare flag based on detected syntax
- Foreground serve: SIGTERM with 5s WaitDelay instead of default SIGKILL
- list shows '?' for models with unknown param count (covers Gemma 4
  E4B variants whose GGUF lacks general.parameter_count)

Distribution:
- .goreleaser.yml — darwin/arm64 binary, tar.gz archive, Homebrew cask
- .github/workflows/{ci,release}.yml — test-on-push + GoReleaser on v* tag
- README rewritten for v1.0.0

Ships as v1.0.0; covers PRD AC#1 (brew install in under 30s)."
```

- [ ] **Step 3: Delete the feature branch**

```bash
git branch -d phase4-polish-and-tap
```

- [ ] **Step 4: Create the GitHub repo**

```bash
gh repo create gregmundy/llamactl \
  --public \
  --source . \
  --remote origin \
  --description "Run llama.cpp on Apple Silicon" \
  --push
```

Expected:
- New repo at `https://github.com/gregmundy/llamactl`
- `origin` set to that repo
- `main` pushed including the full 77-ish commit history

If `gh` isn't authenticated, run `gh auth login` first.

- [ ] **Step 5: Verify CI runs on the initial push**

Open `https://github.com/gregmundy/llamactl/actions`. The `ci` workflow should have triggered on the initial push to main. Wait for it to complete (~2-3 minutes). Expected: green.

If CI fails, investigate before tagging. Common causes:
- Go version mismatch (workflow specifies '1.26' — confirm `go.mod` line `go 1.26.x` matches)
- Race detector flags a real bug not caught locally

---

## Task 12: Set HOMEBREW_TAP_GITHUB_TOKEN secret

**Files:** none

- [ ] **Step 1: Identify or generate the PAT**

The `HOMEBREW_TAP_GITHUB_TOKEN` reused from `gregmundy/llamavm` is a fine-grained PAT with `Contents: write` on `gregmundy/homebrew-tap`. If you still have access to it:
- Find it in your password manager / 1Password / shell history
- Or generate a new one at `https://github.com/settings/personal-access-tokens/new`:
  - Resource owner: gregmundy
  - Repository access: Only select repositories → `gregmundy/homebrew-tap`
  - Repository permissions: Contents → Read and write
  - Expiration: as you prefer (90 days is the GitHub default, fine for a release token)

- [ ] **Step 2: Set the secret on the new repo**

```bash
gh secret set HOMEBREW_TAP_GITHUB_TOKEN --repo gregmundy/llamactl
```

`gh secret set` will prompt for the value interactively (paste, then Enter). Do not echo the token into shell history.

Verify:
```bash
gh secret list --repo gregmundy/llamactl
```
Expected: shows `HOMEBREW_TAP_GITHUB_TOKEN` with a recent updated timestamp.

---

## Task 13: Tag v1.0.0 and watch release

**Files:** none

- [ ] **Step 1: Tag**

```bash
cd /Users/greg/Development/llamactl
git tag -a v1.0.0 -m "v1.0.0 — MVP (PRD AC#1-16)

First public release. brew install gregmundy/tap/llamactl.

Includes:
- 9 subcommands: hardware, doctor, search, add, list, remove, serve, status, stop
- launchd-managed detached services
- 10 doctor checks
- Hardware-aware quant selection
- 4 named recipes (chat / code / long-context / low-memory)"
git push origin v1.0.0
```

- [ ] **Step 2: Watch the release workflow**

```bash
gh run watch --repo gregmundy/llamactl
```

Or open `https://github.com/gregmundy/llamactl/actions/workflows/release.yml` in a browser. The workflow takes 3-5 minutes:
1. Checkout
2. Setup Go 1.26
3. `goreleaser release --clean` — builds darwin/arm64 binary, creates tar.gz + checksums, uploads to GitHub release, opens commit/PR to homebrew-tap

Expected on green:
- GitHub release at `https://github.com/gregmundy/llamactl/releases/tag/v1.0.0` lists:
  - `llamactl_1.0.0_darwin_arm64.tar.gz`
  - `checksums.txt`
- New commit at `https://github.com/gregmundy/homebrew-tap` adding `Casks/llamactl.rb`

If the homebrew-tap commit didn't appear, the most likely cause is a misscoped PAT. Check the workflow log; look for a 403 error in the GoReleaser "publish" step. Fix the PAT scope and re-run the release: `git tag -d v1.0.0; git push --delete origin v1.0.0; git tag -a v1.0.0 ...; git push origin v1.0.0`.

- [ ] **Step 3: Verify the cask**

```bash
cat ~/Development/homebrew-tap/Casks/llamactl.rb 2>/dev/null
git -C ~/Development/homebrew-tap pull
cat ~/Development/homebrew-tap/Casks/llamactl.rb
```
Expected: a cask file structurally identical to `Casks/llamavm.rb` but with `llamactl` references and `v1.0.0`'s SHA256.

---

## Task 14: Live install smoke

**Files:** none

- [ ] **Step 1: Clean local state**

```bash
# Remove the local-build binary so we install from brew, not run /tmp/llamactl.
rm /tmp/llamactl 2>/dev/null
# In case llamactl was tap'd or installed previously, untap to start fresh.
brew untap gregmundy/tap 2>/dev/null
```

- [ ] **Step 2: Time the install**

```bash
time brew install gregmundy/tap/llamactl
```
Expected: completes in under 30 seconds (PRD AC#1). The install fetches the tar.gz, unquarantines the binary via the postflight hook, and puts `llamactl` on PATH (typically `/opt/homebrew/bin/llamactl`).

- [ ] **Step 3: Smoke**

```bash
which llamactl
llamactl --version
llamactl --help | grep -E "serve|stop|status|doctor|add|list|remove|search|hardware"
llamactl doctor
```
Expected:
- `which`: `/opt/homebrew/bin/llamactl`
- `--version`: prints something containing `1.0.0` (depends on whether cobra is configured to print version; if not, this command may not exist — that's OK)
- `--help`: lists all 9 subcommands
- `doctor`: all 10 checks pass (your existing models still installed, no orphan/stale)

- [ ] **Step 4: Run the existing detached-serve smoke against the brew-installed binary**

```bash
llamactl serve qwen2.5-3b-instruct --detach
launchctl print gui/$UID/com.llamactl.qwen2.5-3b-instruct | grep -E "state|pid"
curl -s -m 30 http://localhost:8082/v1/chat/completions -H "Content-Type: application/json" \
  -d '{"model":"x","messages":[{"role":"user","content":"hi"}],"max_tokens":5}'
llamactl status
llamactl stop qwen2.5-3b-instruct
```
Expected: same successful behavior as the Phase 3 smoke, but now exercising the brew-installed binary, the v1.0.0 ldflags-injected version, and the new flash-attn capability detection.

---

## Task 15: Update project_state memory

**Files:**
- Modify: `/Users/greg/.claude/projects/-Users-greg-Development-llamactl/memory/project_state.md`

- [ ] **Step 1: Edit the memory file**

Find the section beginning `**Next: Phase 4** —` (added during Phase 3) and replace it. Add a new section above the existing deferred concerns:

```markdown
**Phase 4 (Homebrew tap + polish) shipped on `main` 2026-05-11** via merge commit `<sha>` of feature branch `phase4-polish-and-tap`, tagged `v1.0.0`. Public repo at `https://github.com/gregmundy/llamactl`; cask at `gregmundy/homebrew-tap/Casks/llamactl.rb`. Install: `brew install gregmundy/tap/llamactl` (verified under 30 s — PRD AC#1).

**Polish landed in Phase 4:**
- `server.Capabilities` + `Prober.Capabilities(ctx, path)` — cached `--help` probe; recipes emit `--flash-attn on` (tristate) or bare flag based on detected syntax. Old Homebrew builds no longer break silently.
- Foreground serve uses `cmd.Cancel = SIGTERM`, `cmd.WaitDelay = 5s` instead of the default SIGKILL on context cancel. Integration test `TestIntegrationPhase4ForegroundGracefulShutdown` verifies "shutting down" hits the log.
- `cli/list` shows `?` for models with unknown param count (Gemma 4 E4B and similar GGUFs that omit `general.parameter_count`).

**MVP complete (PRD AC#1–16).**
```

Then strip the Phase 4 items from the "Deferred concerns" list — they're done. Leave the older Phase 2 / 2.5 / 1 carryovers since most are still open.

- [ ] **Step 2: There's nothing to commit (memory lives outside the repo)**

The memory file is in `~/.claude/projects/...`. It's automatically picked up by future Claude sessions.

---

## Final verification

- [ ] **Repository state**

```bash
git log --oneline main..HEAD                              # empty (you're on main)
git log --oneline | head -20                              # v1.0.0 tag visible
git tag --list                                            # v1.0.0 present
git ls-remote origin                                       # main + v1.0.0 visible
```

- [ ] **GitHub state**

```bash
gh release list --repo gregmundy/llamactl                 # v1.0.0 visible
gh run list --workflow=ci.yml --repo gregmundy/llamactl   # at least one success
gh run list --workflow=release.yml --repo gregmundy/llamactl   # v1.0.0 run successful
```

- [ ] **Homebrew tap state**

```bash
ls ~/Development/homebrew-tap/Casks/llamactl.rb           # exists
brew info gregmundy/tap/llamactl                          # shows 1.0.0
```

- [ ] **PRD acceptance**

```bash
which llamactl                                            # /opt/homebrew/bin/llamactl (AC#1)
llamactl doctor                                            # all 10 checks pass
llamactl serve qwen2.5-3b-instruct --detach               # AC#8, #9
llamactl status                                            # AC#10
llamactl stop qwen2.5-3b-instruct                          # AC#15
```

Phase 4 is complete.

---

## Notes for the executing agent

1. **Branch:** All work on `phase4-polish-and-tap` until Task 11. Never `git checkout main`, never stash, never branch.
2. **Spec is authoritative:** `docs/superpowers/specs/2026-05-11-phase4-distribution-design.md`. If the spec and this plan disagree, the spec wins — flag the contradiction and ask.
3. **Per-task verification:** After each task, run that task's tests AND the full suite.
4. **Two-stage review:** Substantive tasks get dispatched spec+quality review (Tasks 1, 2, 3, 4, 5/6). Trivial tasks verified by direct file read (7, 8, 9, 10). Distribution tasks (11–14) are interactive — controller drives them.
5. **PAT handling:** Never echo or log `HOMEBREW_TAP_GITHUB_TOKEN`. Use `gh secret set` interactive input only.
6. **Path branching at Task 6:** Default to Path A if uncertain. Don't try to do all three paths.
7. **README tone:** match the existing voice if the current README has one. Don't rewrite the project's personality.
