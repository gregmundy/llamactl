# Telemetry Sidecar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `llamactl-telemetryd` — a sidecar daemon that exposes installed-model and running-model telemetry over HTTP for consumption by an external website on a different host.

**Architecture:** New `cmd/llamactl-telemetryd/` binary (wiring only) + new `internal/telemetry/` package (poller, scraper, aggregator, HTTP handler). New `internal/launchd/telemetryd.go` for the launchd plist. New `llamactl telemetry enable/disable/status` management commands inside the main `llamactl` binary. One-line `--metrics` addition to recipes so the daemon has live throughput data to scrape. Doctor gains one check (14→15).

**Tech Stack:** Go 1.26.2, `net/http`, `text/template` for the plist, `gopkg.in/yaml.v3` (already imported), standard library only — no new third-party deps.

**Spec:** `docs/superpowers/specs/2026-05-16-telemetry-sidecar-design.md`

---

## Branch primer (read every time you re-enter the plan)

> **You are on branch `telemetry-sidecar`.** Do NOT `git checkout`, `git switch`, `git stash`, `git reset`, or any branch-changing operation. If `git status` shows unexpected files, stop and ask. Each task below = one commit on this branch. Do not start Task N+1 before finishing Task N.

Create the branch before Task 1:

```bash
git checkout main
git pull origin main
git checkout -b telemetry-sidecar
```

---

## Task 1: Add `--metrics` to every recipe

The sidecar's tok/s computation requires `llama-server`'s `/metrics` endpoint, currently disabled in llamactl-started backends. One-line append to `recipes.FlagsFor`.

**Files:**
- Modify: `internal/recipes/recipes.go`
- Test: `internal/recipes/recipes_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/recipes/recipes_test.go`:

```go
// TestFlagsFor_AllRecipesIncludeMetrics — the telemetry sidecar reads
// /metrics on every backend; this regression test ensures no recipe
// can quietly drop the flag.
func TestFlagsFor_AllRecipesIncludeMetrics(t *testing.T) {
	for name := range Recipes {
		args := FlagsFor(Recipes[name], mkModel(32768), models.Q4_K_M, "/x",
			mkHW(64), mkVer(4500), server.Capabilities{FlashAttnTristate: true}, 4.4, 8080, 10)
		if !argvHasFlag(args, "--metrics") {
			t.Errorf("recipe %q missing --metrics", name)
		}
	}
}
```

- [ ] **Step 2: Run test, expect FAIL**

```bash
go test ./internal/recipes/ -run TestFlagsFor_AllRecipesIncludeMetrics -v
```

Expected: 6 failures (one per recipe).

- [ ] **Step 3: Implement**

In `internal/recipes/recipes.go`, inside `FlagsFor`, append `--metrics` after the flash-attn block and before the sampling block. The exact spot is right before `if r.Sampling != nil {`:

```go
	args = append(args, "--metrics")

	// Optional per-recipe server-side defaults. Emitted in a stable order
	// for snapshot-style test assertions.
	if r.Sampling != nil {
```

- [ ] **Step 4: Run all recipe tests, expect PASS**

```bash
go test ./internal/recipes/ -race
```

Expected: PASS, no failures, no regressions.

- [ ] **Step 5: Commit**

```bash
git add internal/recipes/recipes.go internal/recipes/recipes_test.go
git commit -m "$(cat <<'EOF'
feat(recipes): enable /metrics endpoint on every llama-server invocation

The telemetry sidecar needs Prometheus counters from each backend to
compute rolling tokens/sec. Negligible cost on the llama-server side.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Add telemetry config fields

Three new keys on `config.Config`: `TelemetryPort`, `TelemetryHost`, `TelemetryInterval`. Stored as `int`, `string`, `string` respectively. `TelemetryInterval` is a `string` (parsed via `time.ParseDuration` at use time) rather than a `time.Duration` so YAML round-trip stays human-readable (`"2s"` not `"2000000000"`).

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go` (create the file if it doesn't exist with the test below as its first test):

```go
package config

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadTelemetryFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := Config{
		TelemetryPort:     18080,
		TelemetryHost:     "0.0.0.0",
		TelemetryInterval: "2s",
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.TelemetryPort != 18080 {
		t.Errorf("TelemetryPort = %d, want 18080", got.TelemetryPort)
	}
	if got.TelemetryHost != "0.0.0.0" {
		t.Errorf("TelemetryHost = %q, want 0.0.0.0", got.TelemetryHost)
	}
	if got.TelemetryInterval != "2s" {
		t.Errorf("TelemetryInterval = %q, want 2s", got.TelemetryInterval)
	}
}
```

- [ ] **Step 2: Run test, expect FAIL**

```bash
go test ./internal/config/ -run TestSaveLoadTelemetryFields -v
```

Expected: compile error — unknown fields `TelemetryPort`, `TelemetryHost`, `TelemetryInterval`.

- [ ] **Step 3: Implement**

In `internal/config/config.go`, add three fields to the `Config` struct (after `APIKey`):

```go
type Config struct {
	LlamaServerPath string `yaml:"llama_server_path"`
	DefaultPort     int    `yaml:"default_port"`
	ModelsDir       string `yaml:"models_dir"`
	HFToken         string `yaml:"hf_token"`
	LogLevel        string `yaml:"log_level"`
	APIKey          string `yaml:"api_key"`

	// Telemetry sidecar (llamactl-telemetryd) configuration. Defaults
	// applied at use time: port=18080, host=0.0.0.0, interval=2s.
	TelemetryPort     int    `yaml:"telemetry_port"`
	TelemetryHost     string `yaml:"telemetry_host"`
	TelemetryInterval string `yaml:"telemetry_interval"`
}
```

- [ ] **Step 4: Run test, expect PASS**

```bash
go test ./internal/config/ -race
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "$(cat <<'EOF'
feat(config): add telemetry_port/host/interval config keys

Picked up automatically by the reflection-based allowlist in
internal/cli/config.go — config get/set/list will see them with no
further wiring. Interval stored as string so YAML stays readable.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Validate telemetry_port and telemetry_interval on `config set`

The reflection-based set in `internal/cli/config.go` accepts the new fields automatically but doesn't validate values. Add port-range and duration-parsability checks alongside the existing `default_port` and `log_level` validators.

**Files:**
- Modify: `internal/cli/config.go:138-160` (the `switch fv.Kind()` block in `runConfigSet`)
- Test: `internal/cli/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/config_test.go` (it already exists from Phase 6a):

```go
func TestConfigSet_TelemetryPortValidation(t *testing.T) {
	d, cfgPath := newConfigTestDeps(t)
	cmd := newConfigSetCmd(d)
	cmd.SetArgs([]string{"telemetry_port", "70000"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected port-range error, got nil")
	}
	cmd = newConfigSetCmd(d)
	cmd.SetArgs([]string{"telemetry_port", "18080"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.TelemetryPort != 18080 {
		t.Errorf("TelemetryPort = %d, want 18080", got.TelemetryPort)
	}
}

func TestConfigSet_TelemetryIntervalValidation(t *testing.T) {
	d, _ := newConfigTestDeps(t)
	cmd := newConfigSetCmd(d)
	cmd.SetArgs([]string{"telemetry_interval", "not-a-duration"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected ParseDuration error, got nil")
	}
	cmd = newConfigSetCmd(d)
	cmd.SetArgs([]string{"telemetry_interval", "2s"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error setting 2s: %v", err)
	}
	cmd = newConfigSetCmd(d)
	cmd.SetArgs([]string{"telemetry_interval", "500ms"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error setting 500ms: %v", err)
	}
}
```

If `newConfigTestDeps` doesn't exist, look at the existing config tests in the same file and reuse the pattern (typically: `tempdir + write empty config + return &Deps{Config: &cfg, ConfigPath: path, Stdout: io.Discard}`).

- [ ] **Step 2: Run tests, expect FAIL**

```bash
go test ./internal/cli/ -run 'TestConfigSet_Telemetry' -v
```

Expected: `telemetry_port=70000` succeeds (no validation yet — should fail), `telemetry_interval=not-a-duration` succeeds (should fail).

- [ ] **Step 3: Implement**

In `internal/cli/config.go`, modify the `switch fv.Kind()` block in `runConfigSet` (around line 138). Add a `time` import at top and two new conditional branches:

```go
import (
	// ... existing imports ...
	"time"
)
```

```go
	switch fv.Kind() {
	case reflect.String:
		if key == "log_level" {
			valid := map[string]bool{"debug": true, "info": true, "warn": true, "error": true, "": true}
			if !valid[value] {
				return fmt.Errorf("%w: log_level must be one of debug|info|warn|error (or empty to clear), got %q", ErrUserError, value)
			}
		}
		if key == "telemetry_interval" && value != "" {
			if _, err := time.ParseDuration(value); err != nil {
				return fmt.Errorf("%w: telemetry_interval must be a Go duration (e.g. 2s, 500ms), got %q", ErrUserError, value)
			}
		}
		fv.SetString(value)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%w: %s must be an integer, got %q", ErrUserError, key, value)
		}
		if key == "default_port" && (n < 0 || n > 65535) {
			return fmt.Errorf("%w: default_port must be between 0 and 65535, got %d", ErrUserError, n)
		}
		if key == "telemetry_port" && (n < 1 || n > 65535) {
			return fmt.Errorf("%w: telemetry_port must be between 1 and 65535, got %d", ErrUserError, n)
		}
		fv.SetInt(int64(n))

	default:
		return fmt.Errorf("%w: unsupported field type for key %q", ErrUserError, key)
	}
```

- [ ] **Step 4: Run tests, expect PASS**

```bash
go test ./internal/cli/ -run 'TestConfigSet' -race -v
```

Expected: all `TestConfigSet_*` pass (existing ones for default_port/log_level + new ones for telemetry_*).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/config.go internal/cli/config_test.go
git commit -m "$(cat <<'EOF'
feat(cli): validate telemetry_port range and telemetry_interval duration

Mirrors the default_port range check and log_level enum pattern.
telemetry_interval accepts any Go-parseable duration; empty clears.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `launchd.ListRunningServices` — enumerate model service plists

The poller needs to know which backends are running. Add a helper that scans the LaunchAgents directory and returns one struct per llamactl model service plist (excluding the telemetryd plist itself).

**Files:**
- Create: `internal/launchd/services_list.go`
- Test: `internal/launchd/services_list_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/launchd/services_list_test.go`:

```go
package launchd

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

const fakePlistFmt = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/homebrew/bin/llama-server</string>
    <string>--model</string>
    <string>/tmp/foo.gguf</string>
    <string>--port</string>
    <string>%d</string>
    <string>--ctx-size</string>
    <string>8192</string>
    <string>--cache-type-k</string>
    <string>f16</string>
  </array>
</dict>
</plist>
`

func TestListRunningServices(t *testing.T) {
	dir := t.TempDir()

	mustWrite := func(label string, port int) {
		t.Helper()
		body := []byte(plistFor(label, port))
		if err := os.WriteFile(filepath.Join(dir, label+".plist"), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("com.llamactl.qwen2.5-3b-instruct", 8082)
	mustWrite("com.llamactl.gemma-4-e4b-it", 8083)
	mustWrite("com.llamactl.telemetryd", 18080) // must be excluded
	mustWrite("com.apple.something", 9999)      // must be excluded (wrong prefix)

	got, err := ListRunningServices(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d services, want 2: %+v", len(got), got)
	}

	sort.Slice(got, func(i, j int) bool { return got[i].ID < got[j].ID })
	if got[0].ID != "gemma-4-e4b-it" || got[0].Port != 8083 {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].ID != "qwen2.5-3b-instruct" || got[1].Port != 8082 {
		t.Errorf("got[1] = %+v", got[1])
	}
	// Args must include the recipe-identifying flags.
	if !contains(got[0].Args, "--ctx-size") {
		t.Errorf("got[0].Args missing --ctx-size: %v", got[0].Args)
	}
}

func TestListRunningServices_MissingDir(t *testing.T) {
	got, err := ListRunningServices(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %+v", got)
	}
}

func plistFor(label string, port int) string {
	return fmt.Sprintf(fakePlistFmt, label, port)
}

func contains(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}
```

Add `"fmt"` to the test file's imports.

- [ ] **Step 2: Run test, expect FAIL**

```bash
go test ./internal/launchd/ -run 'TestListRunningServices' -v
```

Expected: compile error — `ListRunningServices` undefined.

- [ ] **Step 3: Implement**

Create `internal/launchd/services_list.go`:

```go
package launchd

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

// RunningService is one llamactl-managed model service entry. The ID is
// the suffix of the plist Label (i.e. the run-name from `serve --detach`).
// Args is the full ProgramArguments slice EXCLUDING the binary path at
// index 0, so callers can pattern-match recipe-identifying flags without
// special-casing index 0.
type RunningService struct {
	ID   string
	Port int
	Args []string
}

// ListRunningServices scans dir for com.llamactl.*.plist files (excluding
// com.llamactl.telemetryd.plist) and returns one entry per plist. A
// missing directory is not an error — returns (nil, nil) matching the
// "fresh install, no services" case used by PortsInUse.
func ListRunningServices(dir string) ([]RunningService, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []RunningService
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, llamactlPlistPrefix) || !strings.HasSuffix(name, ".plist") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, llamactlPlistPrefix), ".plist")
		if id == "telemetryd" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue // best-effort; one unreadable plist must not nuke the rest
		}
		port := extractPortArg(data)
		if port == 0 {
			continue
		}
		args := extractProgramArguments(data)
		out = append(out, RunningService{ID: id, Port: port, Args: args})
	}
	return out, nil
}

// programArgsDoc is the minimal subset of a plist we need to extract
// the ProgramArguments slice. We use encoding/xml because string-scanning
// the way extractPortArg does scales poorly for arbitrary slice extraction.
type programArgsDoc struct {
	Dict struct {
		Keys    []string `xml:"key"`
		Strings []string `xml:"string"`
		Arrays  []struct {
			Strings []string `xml:"string"`
		} `xml:"array"`
	} `xml:"dict"`
}

// extractProgramArguments reads the <array> following the
// <key>ProgramArguments</key> entry in a plist and returns all <string>
// values except the first (the binary path). Returns nil on any parse
// failure or absence; callers tolerate empty slices.
func extractProgramArguments(data []byte) []string {
	var doc programArgsDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	// Find the array that follows the "ProgramArguments" key. Plist's
	// XML structure interleaves key/value pairs: keys[i] pairs with the
	// i-th value, but values are split across multiple slices (strings,
	// arrays, etc.). We rely on the convention that ProgramArguments is
	// the only <array> in our plists.
	for _, k := range doc.Dict.Keys {
		if k == "ProgramArguments" && len(doc.Dict.Arrays) > 0 {
			args := doc.Dict.Arrays[0].Strings
			if len(args) <= 1 {
				return nil
			}
			return args[1:] // strip binary path
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test, expect PASS**

```bash
go test ./internal/launchd/ -race -v
```

Expected: all tests pass; no regressions in `ports_test.go`, `plist_test.go`, etc.

- [ ] **Step 5: Commit**

```bash
git add internal/launchd/services_list.go internal/launchd/services_list_test.go
git commit -m "$(cat <<'EOF'
feat(launchd): ListRunningServices enumerates model plists for telemetry

Returns RunningService{ID, Port, Args} per com.llamactl.*.plist,
excluding com.llamactl.telemetryd.plist. The full Args slice (minus
binary path) lets the telemetry sidecar identify recipes downstream.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: `recipes.IdentifyFromArgv` — reverse-engineer recipe name from plist args

The telemetry response includes a `recipe` field per running service. Recipe choice isn't stored in metadata — it's encoded by the ctx-size + cache-type-k + cache-type-v + reasoning trio in the argv. Match those against `Recipes` to recover the name. Best-effort: returns empty string when no match (e.g., user has customized).

**Files:**
- Create: `internal/recipes/identify.go`
- Test: `internal/recipes/identify_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/recipes/identify_test.go`:

```go
package recipes

import (
	"fmt"
	"testing"
)

func TestIdentifyFromArgv_AllRecipes(t *testing.T) {
	for name, r := range Recipes {
		argv := []string{
			"--ctx-size", fmt.Sprintf("%d", r.CtxSize),
			"--cache-type-k", r.CacheTypeK,
			"--cache-type-v", r.CacheTypeV,
		}
		if r.Reasoning != "" {
			argv = append(argv, "--reasoning", r.Reasoning)
		}
		got := IdentifyFromArgv(argv)
		if got != name {
			t.Errorf("recipe %q: IdentifyFromArgv = %q", name, got)
		}
	}
}

func TestIdentifyFromArgv_ClampedCtxSizeStillMatches(t *testing.T) {
	// long-context normally has 32768 ctx, but a model with MaxCtx=4096
	// gets clamped. The cache-type combo (q8_0/q8_0) is unique to
	// long-context so we should still match.
	argv := []string{
		"--ctx-size", "4096",
		"--cache-type-k", "q8_0",
		"--cache-type-v", "q8_0",
	}
	if got := IdentifyFromArgv(argv); got != "long-context" {
		t.Errorf("got %q, want long-context", got)
	}
}

func TestIdentifyFromArgv_NoMatchReturnsEmpty(t *testing.T) {
	argv := []string{
		"--ctx-size", "16384",
		"--cache-type-k", "iq4_nl",
		"--cache-type-v", "iq4_nl",
	}
	if got := IdentifyFromArgv(argv); got != "" {
		t.Errorf("got %q, want empty (no recipe matches)", got)
	}
}

func TestIdentifyFromArgv_DistinguishesAgentAndThinking(t *testing.T) {
	// agent and thinking share ctx/cache; --reasoning is the only
	// distinguishing flag.
	agentArgv := []string{
		"--ctx-size", "8192", "--cache-type-k", "f16", "--cache-type-v", "f16",
		"--reasoning", "off",
	}
	thinkingArgv := []string{
		"--ctx-size", "8192", "--cache-type-k", "f16", "--cache-type-v", "f16",
		"--reasoning", "on",
	}
	if got := IdentifyFromArgv(agentArgv); got != "agent" {
		t.Errorf("agent argv → %q", got)
	}
	if got := IdentifyFromArgv(thinkingArgv); got != "thinking" {
		t.Errorf("thinking argv → %q", got)
	}
}
```

- [ ] **Step 2: Run test, expect FAIL**

```bash
go test ./internal/recipes/ -run TestIdentifyFromArgv -v
```

Expected: compile error — `IdentifyFromArgv` undefined.

- [ ] **Step 3: Implement**

Create `internal/recipes/identify.go`:

```go
package recipes

import "fmt"

// IdentifyFromArgv returns the recipe Name whose CacheTypeK + CacheTypeV
// + Reasoning match the recipe-defining flags present in args, or ""
// when no recipe matches.
//
// CtxSize is ignored because llamactl clamps it to the model's MaxCtx
// at serve time; matching strictly on the recipe's documented CtxSize
// would mis-identify clamped runs.
//
// Used by the telemetry sidecar to populate the `recipe` field per
// running service. Best-effort: customized recipes return "" and the
// API renders an empty string.
func IdentifyFromArgv(args []string) string {
	var ctk, ctv, reasoning string
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "--cache-type-k":
			ctk = args[i+1]
		case "--cache-type-v":
			ctv = args[i+1]
		case "--reasoning":
			reasoning = args[i+1]
		}
	}
	for name, r := range Recipes {
		if r.CacheTypeK == ctk && r.CacheTypeV == ctv && r.Reasoning == reasoning {
			return name
		}
	}
	// Best-effort fallthrough for forward-compat: if we can't match a
	// full recipe but the args look llamactl-shaped, surface the unique
	// triple so a future debugger can grep for it. We return "" so the
	// API consumer can render "" cleanly.
	_ = fmt.Sprintf // keep fmt import if needed for future formatting
	return ""
}
```

Remove the `fmt` import if you don't use it — that placeholder line is a hedge against a future tweak. Final version should just be:

```go
package recipes

// IdentifyFromArgv returns the recipe Name whose CacheTypeK + CacheTypeV
// + Reasoning match the recipe-defining flags present in args, or ""
// when no recipe matches.
//
// CtxSize is ignored because llamactl clamps it to the model's MaxCtx
// at serve time; matching strictly would mis-identify clamped runs.
func IdentifyFromArgv(args []string) string {
	var ctk, ctv, reasoning string
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "--cache-type-k":
			ctk = args[i+1]
		case "--cache-type-v":
			ctv = args[i+1]
		case "--reasoning":
			reasoning = args[i+1]
		}
	}
	for name, r := range Recipes {
		if r.CacheTypeK == ctk && r.CacheTypeV == ctv && r.Reasoning == reasoning {
			return name
		}
	}
	return ""
}
```

- [ ] **Step 4: Run tests, expect PASS**

```bash
go test ./internal/recipes/ -race -v
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/recipes/identify.go internal/recipes/identify_test.go
git commit -m "$(cat <<'EOF'
feat(recipes): IdentifyFromArgv reverse-engineers recipe name from plist

Best-effort match on CacheTypeK + CacheTypeV + Reasoning. CtxSize is
intentionally excluded because llamactl clamps it to MaxCtx, which
would mis-identify long-context runs on small-MaxCtx models. Used by
the telemetry sidecar to populate the `recipe` field.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `launchd.RenderTelemetryd` — telemetryd plist template

Telemetryd plist differs from model plists: `KeepAlive: false` (don't thrash-restart a crashed daemon), `ProcessType: Background` (low priority), no `--metrics`/`--port` flags (daemon reads them from config).

**Files:**
- Create: `internal/launchd/telemetryd.go`
- Test: `internal/launchd/telemetryd_test.go`

- [ ] **Step 1: Failing test** — `internal/launchd/telemetryd_test.go`:

```go
package launchd

import (
	"strings"
	"testing"
)

func TestRenderTelemetryd_KeyFields(t *testing.T) {
	body, err := RenderTelemetryd(TelemetrydSpec{
		Label:      "com.llamactl.telemetryd",
		BinaryPath: "/opt/homebrew/bin/llamactl-telemetryd",
		LogPath:    "/Users/x/Library/Logs/llamactl/telemetryd.log",
		WorkingDir: "/Users/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"<string>com.llamactl.telemetryd</string>",
		"<string>/opt/homebrew/bin/llamactl-telemetryd</string>",
		"<key>KeepAlive</key>\n  <false/>",
		"<key>ProcessType</key>\n  <string>Background</string>",
		"<key>RunAtLoad</key>\n  <true/>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("plist missing %q\n%s", want, s)
		}
	}
	// Crucially must NOT include KeepAlive true.
	if strings.Contains(s, "<key>KeepAlive</key>\n  <true/>") {
		t.Error("telemetryd plist should NOT have KeepAlive=true")
	}
}

func TestTelemetrydLabelConstant(t *testing.T) {
	if TelemetrydLabel != "com.llamactl.telemetryd" {
		t.Errorf("TelemetrydLabel = %q", TelemetrydLabel)
	}
}
```

- [ ] **Step 2: Run** → FAIL (undefined).

- [ ] **Step 3: Implement** — `internal/launchd/telemetryd.go`:

```go
package launchd

import (
	"bytes"
	"fmt"
	"text/template"
)

// TelemetrydLabel is the launchd label for the telemetry sidecar.
const TelemetrydLabel = "com.llamactl.telemetryd"

// TelemetrydSpec captures the few values that vary across telemetryd
// plist instances. Configuration (port/host/interval/api_key) is read
// by the daemon itself from ~/.config/llamactl/config.yaml — not baked
// into the plist — so updating config requires `telemetry disable` then
// `enable`, which is acceptable for a sidecar.
type TelemetrydSpec struct {
	Label      string // always TelemetrydLabel
	BinaryPath string // absolute path to llamactl-telemetryd
	LogPath    string // ~/Library/Logs/llamactl/telemetryd.log
	WorkingDir string // user home
}

const telemetrydTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>{{xml .Label}}</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{xml .BinaryPath}}</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <false/>
  <key>WorkingDirectory</key>
  <string>{{xml .WorkingDir}}</string>
  <key>StandardOutPath</key>
  <string>{{xml .LogPath}}</string>
  <key>StandardErrorPath</key>
  <string>{{xml .LogPath}}</string>
  <key>ProcessType</key>
  <string>Background</string>
</dict>
</plist>
`

var telemetrydTpl = template.Must(template.New("telemetryd").Funcs(template.FuncMap{
	"xml": xmlEscape,
}).Parse(telemetrydTemplate))

// RenderTelemetryd returns the rendered plist bytes for spec.
func RenderTelemetryd(spec TelemetrydSpec) ([]byte, error) {
	var buf bytes.Buffer
	if err := telemetrydTpl.Execute(&buf, spec); err != nil {
		return nil, fmt.Errorf("render telemetryd plist: %w", err)
	}
	return buf.Bytes(), nil
}
```

- [ ] **Step 4: Run** → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/launchd/telemetryd.go internal/launchd/telemetryd_test.go
git commit -m "$(cat <<'EOF'
feat(launchd): RenderTelemetryd + TelemetrydLabel for the sidecar plist

KeepAlive=false (daemon stays down on crash; user re-enables manually)
and ProcessType=Background (low priority). Args are empty — the daemon
reads port/host/interval from config so plist stays trivial.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: `telemetry.State` — in-memory sample cache + tok/s delta

The state holds the most recent `Sample` per model ID plus its predecessor (for delta math). Mutex-guarded. The poller writes; the aggregator reads.

**Files:**
- Create: `internal/telemetry/state.go`
- Test: `internal/telemetry/state_test.go`

- [ ] **Step 1: Failing test** — `internal/telemetry/state_test.go`:

```go
package telemetry

import (
	"testing"
	"time"
)

func TestState_TokensPerSecond_NoPrior(t *testing.T) {
	s := NewState()
	s.Update("a", Sample{State: "idle", TokensPredictedTotal: 100, TokensPredictedSeconds: 1.0})
	if rate, ok := s.TokensPerSecond("a"); ok || rate != 0 {
		t.Errorf("first sample: rate=%v ok=%v, want 0,false", rate, ok)
	}
}

func TestState_TokensPerSecond_Delta(t *testing.T) {
	s := NewState()
	s.Update("a", Sample{State: "idle", TokensPredictedTotal: 100, TokensPredictedSeconds: 1.0})
	s.Update("a", Sample{State: "active", TokensPredictedTotal: 300, TokensPredictedSeconds: 2.0})
	rate, ok := s.TokensPerSecond("a")
	if !ok {
		t.Fatal("expected ok=true after second update")
	}
	if rate != 200.0 {
		t.Errorf("rate = %v, want 200.0", rate)
	}
}

func TestState_TokensPerSecond_ZeroDelta(t *testing.T) {
	s := NewState()
	s.Update("a", Sample{State: "idle", TokensPredictedTotal: 100, TokensPredictedSeconds: 1.0})
	s.Update("a", Sample{State: "idle", TokensPredictedTotal: 100, TokensPredictedSeconds: 1.0})
	rate, ok := s.TokensPerSecond("a")
	if !ok || rate != 0 {
		t.Errorf("zero delta: rate=%v ok=%v, want 0,true", rate, ok)
	}
}

func TestState_TokensPerSecond_NonNumericState(t *testing.T) {
	for _, st := range []string{"loading", "unreachable", "metrics_disabled"} {
		s := NewState()
		s.Update("a", Sample{State: "idle", TokensPredictedTotal: 100, TokensPredictedSeconds: 1.0})
		s.Update("a", Sample{State: st, TokensPredictedTotal: 100, TokensPredictedSeconds: 1.0})
		if _, ok := s.TokensPerSecond("a"); ok {
			t.Errorf("state=%q must yield ok=false", st)
		}
	}
}

func TestState_Forget(t *testing.T) {
	s := NewState()
	s.Update("a", Sample{State: "idle", ScrapedAt: time.Now()})
	s.Update("a", Sample{State: "idle", ScrapedAt: time.Now()})
	s.Forget("a")
	if _, ok := s.Get("a"); ok {
		t.Error("Get after Forget should return false")
	}
	if _, ok := s.TokensPerSecond("a"); ok {
		t.Error("TokensPerSecond after Forget should return false")
	}
}

func TestState_IDs(t *testing.T) {
	s := NewState()
	s.Update("a", Sample{State: "idle"})
	s.Update("b", Sample{State: "idle"})
	ids := s.IDs()
	if len(ids) != 2 {
		t.Errorf("IDs len = %d, want 2", len(ids))
	}
}
```

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** — `internal/telemetry/state.go`:

```go
// Package telemetry implements the llamactl-telemetryd sidecar: it
// scrapes each running llama-server backend on a timer, caches the
// results, and serves an aggregated JSON snapshot over HTTP.
package telemetry

import (
	"math"
	"sync"
	"time"
)

// Sample is one poll-cycle result for a single backend.
type Sample struct {
	ScrapedAt              time.Time
	State                  string // idle | active | loading | metrics_disabled | unreachable
	TokensPredictedTotal   uint64
	TokensPredictedSeconds float64
	MemoryBytes            int64
	UptimeSeconds          int64
	Port                   int
	Recipe                 string
	ScrapeError            string // empty when state != "unreachable"
}

// State is the in-memory cache shared between poller and aggregator.
// Reads and writes are mutex-guarded. Holds at most two Samples per
// modelID — the latest and its immediate predecessor — so tokens/sec
// can be computed as a delta.
type State struct {
	mu      sync.Mutex
	samples map[string]Sample
	prev    map[string]Sample
}

func NewState() *State {
	return &State{
		samples: make(map[string]Sample),
		prev:    make(map[string]Sample),
	}
}

// Update sets the latest Sample for modelID; the previous latest is
// preserved in `prev` for delta math.
func (s *State) Update(modelID string, sample Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if curr, ok := s.samples[modelID]; ok {
		s.prev[modelID] = curr
	}
	s.samples[modelID] = sample
}

// Get returns the current Sample for modelID.
func (s *State) Get(modelID string) (Sample, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sample, ok := s.samples[modelID]
	return sample, ok
}

// TokensPerSecond returns the rolling rate between the prior and most
// recent Sample for modelID. ok=false when no prior exists or state is
// non-numeric (loading/unreachable/metrics_disabled). ok=true with
// rate=0 means "we have two samples but generation didn't progress."
func (s *State) TokensPerSecond(modelID string) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	curr, ok := s.samples[modelID]
	if !ok {
		return 0, false
	}
	switch curr.State {
	case "loading", "unreachable", "metrics_disabled":
		return 0, false
	}
	prev, ok := s.prev[modelID]
	if !ok {
		return 0, false
	}
	dt := curr.TokensPredictedSeconds - prev.TokensPredictedSeconds
	if dt <= 0 {
		return 0, true
	}
	dtok := float64(curr.TokensPredictedTotal - prev.TokensPredictedTotal)
	rate := dtok / dt
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, false
	}
	return rate, true
}

// Forget removes modelID from both maps. Called by the poller when a
// model that was running disappears from the plist directory.
func (s *State) Forget(modelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.samples, modelID)
	delete(s.prev, modelID)
}

// IDs returns the currently-tracked model IDs in arbitrary order.
func (s *State) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.samples))
	for id := range s.samples {
		out = append(out, id)
	}
	return out
}
```

- [ ] **Step 4: Run** → `go test ./internal/telemetry/ -race -v` PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/telemetry/state.go internal/telemetry/state_test.go
git commit -m "feat(telemetry): State holds Sample cache with tok/s delta math

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Prometheus text parser

Pulls 3 series out of `/metrics`: `llamacpp:tokens_predicted_total`, `llamacpp:tokens_predicted_seconds_total`, `llamacpp:requests_processing`. Unknown lines ignored. Missing series leave their field at zero.

**Files:** create `internal/telemetry/metrics.go` + `metrics_test.go`.

- [ ] **Step 1: Failing test** — `internal/telemetry/metrics_test.go`:

```go
package telemetry

import "testing"

const sampleMetrics = `# HELP llamacpp:prompt_tokens_total Number of prompt tokens processed.
# TYPE llamacpp:prompt_tokens_total counter
llamacpp:prompt_tokens_total 52
# HELP llamacpp:tokens_predicted_total Number of generation tokens processed.
# TYPE llamacpp:tokens_predicted_total counter
llamacpp:tokens_predicted_total 60
# HELP llamacpp:tokens_predicted_seconds_total Predict process time
# TYPE llamacpp:tokens_predicted_seconds_total counter
llamacpp:tokens_predicted_seconds_total 0.255
# HELP llamacpp:requests_processing Number of requests processing.
# TYPE llamacpp:requests_processing gauge
llamacpp:requests_processing 1
`

func TestParseMetrics_Happy(t *testing.T) {
	v, err := ParseMetrics(sampleMetrics)
	if err != nil {
		t.Fatal(err)
	}
	if v.TokensPredictedTotal != 60 {
		t.Errorf("TokensPredictedTotal = %d, want 60", v.TokensPredictedTotal)
	}
	if v.TokensPredictedSeconds != 0.255 {
		t.Errorf("TokensPredictedSeconds = %v, want 0.255", v.TokensPredictedSeconds)
	}
	if v.RequestsProcessing != 1.0 {
		t.Errorf("RequestsProcessing = %v, want 1.0", v.RequestsProcessing)
	}
}

func TestParseMetrics_Empty(t *testing.T) {
	v, err := ParseMetrics("")
	if err != nil {
		t.Fatal(err)
	}
	if v.TokensPredictedTotal != 0 || v.TokensPredictedSeconds != 0 {
		t.Errorf("empty body should yield zero values, got %+v", v)
	}
}

func TestParseMetrics_Malformed(t *testing.T) {
	body := "llamacpp:tokens_predicted_total not-a-number\nllamacpp:requests_processing 2"
	v, err := ParseMetrics(body)
	if err != nil {
		t.Fatal(err)
	}
	if v.TokensPredictedTotal != 0 {
		t.Error("malformed value should leave field zero")
	}
	if v.RequestsProcessing != 2 {
		t.Errorf("RequestsProcessing = %v, want 2", v.RequestsProcessing)
	}
}
```

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** — `internal/telemetry/metrics.go`:

```go
package telemetry

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// MetricsValues is the subset of llama.cpp's /metrics output we use.
type MetricsValues struct {
	TokensPredictedTotal   uint64
	TokensPredictedSeconds float64
	RequestsProcessing     float64
}

// ParseMetrics extracts known series from a Prometheus text-format body.
// Unknown lines, malformed values, and missing series are tolerated
// silently — the goal is a best-effort snapshot, not strict validation.
func ParseMetrics(body string) (MetricsValues, error) {
	var v MetricsValues
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if i := strings.Index(name, "{"); i >= 0 {
			name = name[:i]
		}
		valStr := fields[len(fields)-1]
		switch name {
		case "llamacpp:tokens_predicted_total":
			if n, err := strconv.ParseUint(valStr, 10, 64); err == nil {
				v.TokensPredictedTotal = n
			}
		case "llamacpp:tokens_predicted_seconds_total":
			if f, err := strconv.ParseFloat(valStr, 64); err == nil {
				v.TokensPredictedSeconds = f
			}
		case "llamacpp:requests_processing":
			if f, err := strconv.ParseFloat(valStr, 64); err == nil {
				v.RequestsProcessing = f
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return v, fmt.Errorf("scan metrics: %w", err)
	}
	return v, nil
}
```

- [ ] **Step 4: Run** → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/telemetry/metrics.go internal/telemetry/metrics_test.go
git commit -m "feat(telemetry): Prometheus text parser for llama-server /metrics

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: `/slots` JSON parser

**Files:** create `internal/telemetry/slots.go` + `slots_test.go`.

- [ ] **Step 1: Failing test**:

```go
package telemetry

import "testing"

func TestParseSlots_AllIdle(t *testing.T) {
	body := []byte(`[{"id":0,"is_processing":false},{"id":1,"is_processing":false}]`)
	s, err := ParseSlots(body)
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalSlots != 2 || s.BusySlots != 0 {
		t.Errorf("got %+v, want total=2 busy=0", s)
	}
}

func TestParseSlots_Mixed(t *testing.T) {
	body := []byte(`[{"id":0,"is_processing":false},{"id":1,"is_processing":true},{"id":2,"is_processing":true}]`)
	s, err := ParseSlots(body)
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalSlots != 3 || s.BusySlots != 2 {
		t.Errorf("got %+v, want total=3 busy=2", s)
	}
}

func TestParseSlots_BadJSON(t *testing.T) {
	if _, err := ParseSlots([]byte("not-json")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
```

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** — `internal/telemetry/slots.go`:

```go
package telemetry

import (
	"encoding/json"
	"fmt"
)

type SlotsState struct {
	TotalSlots int
	BusySlots  int
}

func ParseSlots(body []byte) (SlotsState, error) {
	var arr []struct {
		IsProcessing bool `json:"is_processing"`
	}
	if err := json.Unmarshal(body, &arr); err != nil {
		return SlotsState{}, fmt.Errorf("parse slots: %w", err)
	}
	var busy int
	for _, s := range arr {
		if s.IsProcessing {
			busy++
		}
	}
	return SlotsState{TotalSlots: len(arr), BusySlots: busy}, nil
}
```

- [ ] **Step 4: Run** → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/telemetry/slots.go internal/telemetry/slots_test.go
git commit -m "feat(telemetry): /slots parser returns total + busy counts

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: `Scrape` — backend orchestrator

Fans out `/metrics`, `/slots`, `/health` for a single backend with per-call timeout. Returns a `Sample` with state derived per §5.4 of the spec.

**Files:** create `internal/telemetry/backend.go` + `backend_test.go`.

- [ ] **Step 1: Failing test** — `internal/telemetry/backend_test.go`:

```go
package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeBackend returns an httptest.Server that responds to /health,
// /metrics, /slots per the maps provided. Missing entries → 404.
func fakeBackend(t *testing.T, status map[string]int, bodies map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for p, body := range bodies {
		p, body := p, body
		mux.HandleFunc(p, func(w http.ResponseWriter, _ *http.Request) {
			code, ok := status[p]
			if !ok {
				code = 200
			}
			w.WriteHeader(code)
			fmt.Fprint(w, body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestScrape_HappyPath_Idle(t *testing.T) {
	srv := fakeBackend(t, nil, map[string]string{
		"/health":  `{"status":"ok"}`,
		"/metrics": sampleMetricsIdle,
		"/slots":   `[{"is_processing":false},{"is_processing":false}]`,
	})
	defer srv.Close()
	port := portFromURL(t, srv.URL)

	s := Scrape(context.Background(), srv.Client(), srv.URL, port)
	if s.State != "idle" {
		t.Errorf("State = %q, want idle", s.State)
	}
	if s.TokensPredictedTotal != 60 {
		t.Errorf("TokensPredictedTotal = %d, want 60", s.TokensPredictedTotal)
	}
	if s.Port != port {
		t.Errorf("Port = %d, want %d", s.Port, port)
	}
}

func TestScrape_Active(t *testing.T) {
	srv := fakeBackend(t, nil, map[string]string{
		"/health":  `{"status":"ok"}`,
		"/metrics": sampleMetricsActive,
		"/slots":   `[{"is_processing":true}]`,
	})
	defer srv.Close()
	port := portFromURL(t, srv.URL)

	s := Scrape(context.Background(), srv.Client(), srv.URL, port)
	if s.State != "active" {
		t.Errorf("State = %q, want active", s.State)
	}
}

func TestScrape_Loading_HealthReturns503(t *testing.T) {
	srv := fakeBackend(t,
		map[string]int{"/health": 503},
		map[string]string{"/health": ""},
	)
	defer srv.Close()
	s := Scrape(context.Background(), srv.Client(), srv.URL, portFromURL(t, srv.URL))
	if s.State != "loading" {
		t.Errorf("State = %q, want loading", s.State)
	}
}

func TestScrape_MetricsDisabled_501(t *testing.T) {
	srv := fakeBackend(t,
		map[string]int{"/metrics": 501},
		map[string]string{
			"/health":  `{"status":"ok"}`,
			"/metrics": `{"error":"not supported"}`,
			"/slots":   `[{"is_processing":false}]`,
		},
	)
	defer srv.Close()
	s := Scrape(context.Background(), srv.Client(), srv.URL, portFromURL(t, srv.URL))
	if s.State != "metrics_disabled" {
		t.Errorf("State = %q, want metrics_disabled", s.State)
	}
}

func TestScrape_Unreachable(t *testing.T) {
	// Construct a URL pointing at a free port that no one is listening on.
	s := Scrape(context.Background(), http.DefaultClient, "http://127.0.0.1:1", 1)
	if s.State != "unreachable" {
		t.Errorf("State = %q, want unreachable", s.State)
	}
	if s.ScrapeError == "" {
		t.Error("expected ScrapeError to be populated")
	}
}

func TestScrape_RespectsContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte("too late"))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	s := Scrape(ctx, srv.Client(), srv.URL, portFromURL(t, srv.URL))
	if s.State != "unreachable" {
		t.Errorf("State = %q, want unreachable on timeout", s.State)
	}
}

const sampleMetricsIdle = `llamacpp:tokens_predicted_total 60
llamacpp:tokens_predicted_seconds_total 0.255
llamacpp:requests_processing 0
`

const sampleMetricsActive = `llamacpp:tokens_predicted_total 200
llamacpp:tokens_predicted_seconds_total 1.0
llamacpp:requests_processing 1
`

func portFromURL(t *testing.T, u string) int {
	t.Helper()
	// httptest.Server URL is http://127.0.0.1:PORT
	var p int
	if _, err := fmt.Sscanf(u, "http://127.0.0.1:%d", &p); err != nil {
		t.Fatalf("bad url %q: %v", u, err)
	}
	return p
}
```

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** — `internal/telemetry/backend.go`:

```go
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Scrape fetches /health, /metrics, /slots from one backend at baseURL
// and returns a Sample. baseURL must not include a trailing slash.
// The caller owns the *http.Client (used for testing with httptest).
//
// State derivation rules (see spec §5.4):
//   - health 503 → "loading"
//   - any other error before metrics → "unreachable"
//   - metrics 501 → "metrics_disabled" (idle/active still derivable from /slots)
//   - requests_processing > 0 OR any slot is_processing → "active"
//   - otherwise → "idle"
func Scrape(ctx context.Context, client *http.Client, baseURL string, port int) Sample {
	now := time.Now()
	sample := Sample{ScrapedAt: now, Port: port}

	// /health — fast up/down check first
	healthCode, _, err := fetch(ctx, client, baseURL+"/health")
	if err != nil {
		sample.State = "unreachable"
		sample.ScrapeError = err.Error()
		return sample
	}
	if healthCode == http.StatusServiceUnavailable {
		sample.State = "loading"
		return sample
	}

	// /slots — always enabled
	slotsState := SlotsState{}
	if _, body, err := fetch(ctx, client, baseURL+"/slots"); err == nil {
		slotsState, _ = ParseSlots(body)
	}

	// /metrics — may return 501 if --metrics wasn't passed
	metricsCode, body, err := fetch(ctx, client, baseURL+"/metrics")
	switch {
	case err != nil:
		sample.State = "unreachable"
		sample.ScrapeError = err.Error()
		return sample
	case metricsCode == http.StatusNotImplemented:
		sample.State = "metrics_disabled"
		// still derive idle/active from /slots
		if slotsState.BusySlots > 0 {
			sample.State = "active"
		}
		return sample
	case metricsCode != http.StatusOK:
		sample.State = "unreachable"
		sample.ScrapeError = fmt.Sprintf("/metrics returned %d", metricsCode)
		return sample
	}

	mv, _ := ParseMetrics(string(body))
	sample.TokensPredictedTotal = mv.TokensPredictedTotal
	sample.TokensPredictedSeconds = mv.TokensPredictedSeconds

	if mv.RequestsProcessing > 0 || slotsState.BusySlots > 0 {
		sample.State = "active"
	} else {
		sample.State = "idle"
	}
	return sample
}

// fetch is a small wrapper that propagates context, reads the full body,
// and returns (statusCode, body, err).
func fetch(ctx context.Context, client *http.Client, url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// ErrScrapeTimeout is documented for callers that want to discriminate
// timeout errors from connection-refused. Not currently used directly
// but kept for callers in tests.
var ErrScrapeTimeout = errors.New("scrape timeout")
```

- [ ] **Step 4: Run** → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/telemetry/backend.go internal/telemetry/backend_test.go
git commit -m "feat(telemetry): Scrape orchestrates /health+/metrics+/slots per backend

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 11: Poller

Background goroutine that ticks every `Config.Interval`, calls `launchd.ListRunningServices`, fans out `Scrape` per backend with bounded concurrency, updates `State`, and `Forget`s IDs no longer running. Also reads `models.Metadata` once per tick for the installed list cache.

**Files:** create `internal/telemetry/poller.go` + `poller_test.go`.

- [ ] **Step 1: Failing test** — `internal/telemetry/poller_test.go`:

```go
package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/launchd"
)

// fakeLister returns a hardcoded set of running services.
type fakeLister struct {
	services []launchd.RunningService
}

func (f *fakeLister) ListRunningServices(_ string) ([]launchd.RunningService, error) {
	return f.services, nil
}

func TestPoller_RunsOnceAndUpdatesState(t *testing.T) {
	calls := int64(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok"}`))
		case "/metrics":
			fmt.Fprint(w, sampleMetricsIdle)
		case "/slots":
			fmt.Fprint(w, `[{"is_processing":false}]`)
		}
	}))
	defer srv.Close()

	state := NewState()
	port := portFromURL(t, srv.URL)
	p := &Poller{
		State:     state,
		Lister:    &fakeLister{services: []launchd.RunningService{{ID: "fake", Port: port}}},
		PlistDir:  t.TempDir(), // unused but required
		HTTPClient: srv.Client(),
		BaseURLFn: func(_ int) string { return srv.URL },
	}
	p.tickOnce(context.Background())

	if _, ok := state.Get("fake"); !ok {
		t.Fatal("expected state to have entry for fake")
	}
	if atomic.LoadInt64(&calls) == 0 {
		t.Fatal("expected HTTP calls to fake backend")
	}
}

func TestPoller_ForgetsServicesThatDisappear(t *testing.T) {
	state := NewState()
	state.Update("gone", Sample{State: "idle"})
	p := &Poller{
		State:    state,
		Lister:   &fakeLister{services: nil}, // no services
		PlistDir: t.TempDir(),
	}
	p.tickOnce(context.Background())
	if _, ok := state.Get("gone"); ok {
		t.Error("expected state to forget id 'gone'")
	}
}

// helper to keep path package referenced
var _ = filepath.Join
```

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** — `internal/telemetry/poller.go`:

```go
package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gregmundy/llamactl/internal/launchd"
)

// Lister is the seam over launchd.ListRunningServices.
type Lister interface {
	ListRunningServices(dir string) ([]launchd.RunningService, error)
}

// LauchdListerAdapter wraps the package-level function. (Yes, the
// typo is in the name to match Go's promotion of typed functions —
// rename if desired during code review.)
type LaunchdLister struct{}

func (LaunchdLister) ListRunningServices(dir string) ([]launchd.RunningService, error) {
	return launchd.ListRunningServices(dir)
}

// Poller scrapes each running backend on a tick. Field values must be
// set before Run is called.
type Poller struct {
	State      *State
	Lister     Lister
	PlistDir   string
	HTTPClient *http.Client
	Interval   time.Duration
	// BaseURLFn lets tests redirect requests to httptest URLs while
	// production builds use http://127.0.0.1:<port>.
	BaseURLFn func(port int) string
}

// Run blocks until ctx is canceled, ticking every Interval.
func (p *Poller) Run(ctx context.Context) {
	if p.Interval <= 0 {
		p.Interval = 2 * time.Second
	}
	p.tickOnce(ctx) // immediate first scrape so /v1/telemetry isn't empty
	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tickOnce(ctx)
		}
	}
}

// tickOnce runs one poll cycle: enumerate, fan out scrape, update state,
// forget disappeared IDs.
func (p *Poller) tickOnce(ctx context.Context) {
	services, err := p.Lister.ListRunningServices(p.PlistDir)
	if err != nil {
		return // transient; next tick will retry
	}
	known := make(map[string]bool, len(services))
	var wg sync.WaitGroup
	for _, svc := range services {
		known[svc.ID] = true
		wg.Add(1)
		go func(svc launchd.RunningService) {
			defer wg.Done()
			scrapeCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
			defer cancel()
			base := p.BaseURLFn(svc.Port)
			sample := Scrape(scrapeCtx, p.HTTPClient, base, svc.Port)
			p.State.Update(svc.ID, sample)
		}(svc)
	}
	wg.Wait()
	for _, id := range p.State.IDs() {
		if !known[id] {
			p.State.Forget(id)
		}
	}
}

// DefaultBaseURL is the production BaseURLFn — points at 127.0.0.1.
func DefaultBaseURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}
```

- [ ] **Step 4: Run** → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/telemetry/poller.go internal/telemetry/poller_test.go
git commit -m "feat(telemetry): Poller fan-out scrapes backends and updates State

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: Aggregator — build the `/v1/telemetry` JSON response

Reads `models.Metadata` from `~/.config/llamactl/models/`, joins with `State`, returns the documented JSON shape (spec §5.3).

**Files:** create `internal/telemetry/aggregator.go` + `aggregator_test.go`.

- [ ] **Step 1: Failing test**:

```go
package telemetry

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/models"
)

func TestAggregate_EmptyState(t *testing.T) {
	now := time.Date(2026, 5, 16, 14, 23, 11, 0, time.UTC)
	agg := Aggregate(NewState(), nil, now)
	if agg.GeneratedAt != "2026-05-16T14:23:11Z" {
		t.Errorf("GeneratedAt = %q", agg.GeneratedAt)
	}
	if len(agg.Installed) != 0 {
		t.Errorf("Installed len = %d, want 0", len(agg.Installed))
	}
	if len(agg.Running) != 0 {
		t.Errorf("Running len = %d, want 0", len(agg.Running))
	}
}

func TestAggregate_IncludesInstalledAndRunning(t *testing.T) {
	installed := []models.Metadata{{
		ID: "qwen2.5-3b-instruct", ParamsB: 3.0, Quant: "Q5_K_M",
		SizeBytes: 2469606195,
		GGUFPath:  "/tmp/Q5_K_M.gguf",
	}}
	state := NewState()
	state.Update("qwen2.5-3b-instruct", Sample{
		State: "idle", Port: 8082, Recipe: "chat",
		MemoryBytes: 695894016, UptimeSeconds: 3600,
		TokensPredictedTotal:   1280,
		TokensPredictedSeconds: 5.0,
	})
	state.Update("qwen2.5-3b-instruct", Sample{
		State: "idle", Port: 8082, Recipe: "chat",
		MemoryBytes: 695894016, UptimeSeconds: 3605,
		TokensPredictedTotal:   1280,
		TokensPredictedSeconds: 5.0,
	})

	agg := Aggregate(state, installed, time.Now().UTC())
	if len(agg.Installed) != 1 || agg.Installed[0].ID != "qwen2.5-3b-instruct" {
		t.Errorf("Installed = %+v", agg.Installed)
	}
	if len(agg.Running) != 1 {
		t.Fatalf("Running len = %d", len(agg.Running))
	}
	r := agg.Running[0]
	if r.SizeBytes != 2469606195 {
		t.Errorf("SizeBytes = %d", r.SizeBytes)
	}
	if r.TokensPerSecond == nil || *r.TokensPerSecond != 0.0 {
		t.Errorf("expected tok/s == 0.0 on zero delta, got %v", r.TokensPerSecond)
	}
}

func TestAggregate_NullsTokensForUnreachable(t *testing.T) {
	state := NewState()
	state.Update("x", Sample{State: "unreachable"})
	agg := Aggregate(state, nil, time.Now().UTC())
	if len(agg.Running) != 1 {
		t.Fatalf("Running len = %d", len(agg.Running))
	}
	if agg.Running[0].TokensPerSecond != nil {
		t.Errorf("expected nil tok/s for unreachable, got %v", agg.Running[0].TokensPerSecond)
	}
}

func TestAggregate_JSONShape(t *testing.T) {
	agg := Aggregate(NewState(), nil, time.Date(2026, 5, 16, 14, 0, 0, 0, time.UTC))
	b, err := json.Marshal(agg)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"generated_at":"2026-05-16T14:00:00Z","installed":[],"running":[]}`
	if string(b) != want {
		t.Errorf("got %s", string(b))
	}
}
```

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** — `internal/telemetry/aggregator.go`:

```go
package telemetry

import (
	"sort"
	"time"

	"github.com/gregmundy/llamactl/internal/models"
)

// TelemetryResponse is the JSON payload of GET /v1/telemetry.
type TelemetryResponse struct {
	GeneratedAt string          `json:"generated_at"`
	Installed   []InstalledRow  `json:"installed"`
	Running     []RunningRow    `json:"running"`
}

type InstalledRow struct {
	ID           string  `json:"id"`
	ParamsB      float64 `json:"params_b"`
	Quant        string  `json:"quant"`
	SizeBytes    int64   `json:"size_bytes"`
	AddedAt      string  `json:"added_at,omitempty"`
	LastServedAt *string `json:"last_served_at"` // null when never served
}

type RunningRow struct {
	ID                    string   `json:"id"`
	Port                  int      `json:"port"`
	Recipe                string   `json:"recipe"`
	SizeBytes             int64    `json:"size_bytes"`
	MemoryBytes           int64    `json:"memory_bytes"`
	State                 string   `json:"state"`
	TokensPerSecond       *float64 `json:"tokens_per_second"`
	TokensPredictedTotal  uint64   `json:"tokens_predicted_total"`
	UptimeSeconds         int64    `json:"uptime_seconds"`
	Error                 string   `json:"error,omitempty"`
}

// Aggregate builds the response from current State and the installed-
// model list. now is the timestamp embedded in generated_at.
func Aggregate(state *State, installed []models.Metadata, now time.Time) TelemetryResponse {
	resp := TelemetryResponse{
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Installed:   make([]InstalledRow, 0, len(installed)),
		Running:     []RunningRow{},
	}

	sizeByID := make(map[string]int64, len(installed))
	for _, m := range installed {
		sizeByID[m.ID] = m.SizeBytes
		var lastServed *string
		if !m.LastServedAt.IsZero() {
			s := m.LastServedAt.UTC().Format(time.RFC3339)
			lastServed = &s
		}
		row := InstalledRow{
			ID:           m.ID,
			ParamsB:      m.ParamsB,
			Quant:        string(m.Quant),
			SizeBytes:    m.SizeBytes,
			LastServedAt: lastServed,
		}
		if !m.AddedAt.IsZero() {
			row.AddedAt = m.AddedAt.UTC().Format(time.RFC3339)
		}
		resp.Installed = append(resp.Installed, row)
	}
	sort.Slice(resp.Installed, func(i, j int) bool { return resp.Installed[i].ID < resp.Installed[j].ID })

	for _, id := range state.IDs() {
		sample, _ := state.Get(id)
		row := RunningRow{
			ID:                   id,
			Port:                 sample.Port,
			Recipe:               sample.Recipe,
			SizeBytes:            sizeByID[id],
			MemoryBytes:          sample.MemoryBytes,
			State:                sample.State,
			TokensPredictedTotal: sample.TokensPredictedTotal,
			UptimeSeconds:        sample.UptimeSeconds,
			Error:                sample.ScrapeError,
		}
		if rate, ok := state.TokensPerSecond(id); ok {
			r := rate
			row.TokensPerSecond = &r
		}
		resp.Running = append(resp.Running, row)
	}
	sort.Slice(resp.Running, func(i, j int) bool { return resp.Running[i].ID < resp.Running[j].ID })
	return resp
}
```

If `models.Metadata` doesn't have `SizeBytes` or `LastServedAt` or `AddedAt` fields with the exact names assumed above, adjust to match what exists. (Look at `internal/models/store.go` to verify before implementing.)

- [ ] **Step 4: Run** → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/telemetry/aggregator.go internal/telemetry/aggregator_test.go
git commit -m "feat(telemetry): Aggregate builds /v1/telemetry response shape

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 13: HTTP Server + Bearer auth middleware

Routes: `GET /v1/telemetry` (auth-gated), `GET /health` (open). Other routes → 404; non-GET → 405. Auth middleware is a no-op when `apiKey` is empty.

**Files:** create `internal/telemetry/server.go` + `server_test.go`.

- [ ] **Step 1: Failing test**:

```go
package telemetry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestHandler(apiKey string) http.Handler {
	state := NewState()
	state.Update("a", Sample{State: "idle"})
	return NewHandler(state, nil, apiKey, func() time.Time { return time.Date(2026, 5, 16, 14, 0, 0, 0, time.UTC) })
}

func TestHandler_HealthNoAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler("sk-xxx").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != 200 {
		t.Errorf("Code = %d", rec.Code)
	}
}

func TestHandler_TelemetryRequiresAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler("sk-xxx").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/telemetry", nil))
	if rec.Code != 401 {
		t.Errorf("Code = %d, want 401", rec.Code)
	}
}

func TestHandler_TelemetryWrongToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/telemetry", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	newTestHandler("sk-xxx").ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("Code = %d, want 401", rec.Code)
	}
}

func TestHandler_TelemetryRightToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/telemetry", nil)
	req.Header.Set("Authorization", "Bearer sk-xxx")
	rec := httptest.NewRecorder()
	newTestHandler("sk-xxx").ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("Code = %d, want 200", rec.Code)
	}
	var resp TelemetryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.GeneratedAt == "" {
		t.Error("GeneratedAt missing")
	}
}

func TestHandler_NoAPIKeyMeansNoAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler("").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/telemetry", nil))
	if rec.Code != 200 {
		t.Errorf("Code = %d, want 200 (no apikey configured)", rec.Code)
	}
}

func TestHandler_404On405(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler("").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wat", nil))
	if rec.Code != 404 {
		t.Errorf("404: Code = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	newTestHandler("").ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/telemetry", strings.NewReader("")))
	if rec.Code != 405 {
		t.Errorf("405: Code = %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** — `internal/telemetry/server.go`:

```go
package telemetry

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gregmundy/llamactl/internal/models"
)

// InstalledLister is the seam used by the server to fetch the
// installed-model list on each request. Production wires a function
// that reads from the model metadata directory.
type InstalledLister func() []models.Metadata

// NewHandler builds the http.Handler for telemetryd. apiKey enables
// Bearer auth when non-empty. nowFn is the time source (tests inject
// fixed times).
func NewHandler(state *State, installed InstalledLister, apiKey string, nowFn func() time.Time) http.Handler {
	if nowFn == nil {
		nowFn = time.Now
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.Handle("/v1/telemetry", authMiddleware(apiKey, telemetryHandler(state, installed, nowFn)))
	return methodGuard(mux)
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func telemetryHandler(state *State, installed InstalledLister, nowFn func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		var meta []models.Metadata
		if installed != nil {
			meta = installed()
		}
		resp := Aggregate(state, meta, nowFn())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func authMiddleware(apiKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("Authorization")
		want := "Bearer " + apiKey
		if got != want {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// methodGuard returns 405 for non-GET against our two real routes.
func methodGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isOurs := r.URL.Path == "/health" || r.URL.Path == "/v1/telemetry"
		if isOurs && r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// strip any trailing slash variants if needed (not for now)
		_ = strings.TrimRight
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 4: Run** → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/telemetry/server.go internal/telemetry/server_test.go
git commit -m "feat(telemetry): HTTP handler with /health + /v1/telemetry + Bearer auth

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 14: Integration test

End-to-end test: fake llama-server (httptest), real Poller + Handler, drive ticks, assert response.

**Files:** create `internal/telemetry/integration_test.go`.

```go
package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/launchd"
)

func TestIntegration_PollerAndHandler(t *testing.T) {
	calls := int64(0)
	tokens := int64(60)
	secs := int64(255) // tenths of milliseconds (0.255 s)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok"}`))
		case "/slots":
			fmt.Fprint(w, `[{"is_processing":false}]`)
		case "/metrics":
			fmt.Fprintf(w, "llamacpp:tokens_predicted_total %d\nllamacpp:tokens_predicted_seconds_total %.3f\nllamacpp:requests_processing 0\n",
				atomic.LoadInt64(&tokens), float64(atomic.LoadInt64(&secs))/1000)
		}
	}))
	defer backend.Close()
	port := portFromURL(t, backend.URL)

	state := NewState()
	p := &Poller{
		State:      state,
		Lister:     &fakeLister{services: []launchd.RunningService{{ID: "fake", Port: port}}},
		PlistDir:   t.TempDir(),
		HTTPClient: backend.Client(),
		BaseURLFn:  func(_ int) string { return backend.URL },
	}
	p.tickOnce(context.Background())

	// Simulate generation between ticks.
	atomic.AddInt64(&tokens, 240)
	atomic.AddInt64(&secs, 1000) // +1.000 s of generation
	p.tickOnce(context.Background())

	h := NewHandler(state, nil, "", func() time.Time { return time.Now().UTC() })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/telemetry", nil))
	if rec.Code != 200 {
		t.Fatalf("Code = %d", rec.Code)
	}
	var resp TelemetryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Running) != 1 {
		t.Fatalf("Running len = %d", len(resp.Running))
	}
	r := resp.Running[0]
	if r.TokensPerSecond == nil {
		t.Fatal("expected non-nil TokensPerSecond after 2 ticks")
	}
	if *r.TokensPerSecond != 240.0 {
		t.Errorf("TokensPerSecond = %v, want 240.0", *r.TokensPerSecond)
	}
}

func TestIntegration_MetricsDisabledMidRun(t *testing.T) {
	metricsEnabled := int64(1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok"}`))
		case "/slots":
			fmt.Fprint(w, `[{"is_processing":false}]`)
		case "/metrics":
			if atomic.LoadInt64(&metricsEnabled) == 0 {
				w.WriteHeader(http.StatusNotImplemented)
				return
			}
			fmt.Fprint(w, sampleMetricsIdle)
		}
	}))
	defer backend.Close()
	port := portFromURL(t, backend.URL)

	state := NewState()
	p := &Poller{
		State:      state,
		Lister:     &fakeLister{services: []launchd.RunningService{{ID: "fake", Port: port}}},
		PlistDir:   t.TempDir(),
		HTTPClient: backend.Client(),
		BaseURLFn:  func(_ int) string { return backend.URL },
	}
	p.tickOnce(context.Background())
	atomic.StoreInt64(&metricsEnabled, 0)
	p.tickOnce(context.Background())

	got, _ := state.Get("fake")
	if got.State != "metrics_disabled" {
		t.Errorf("State = %q, want metrics_disabled", got.State)
	}
}
```

- [ ] Run: `go test ./internal/telemetry/ -race -v` PASS.
- [ ] Commit:

```bash
git add internal/telemetry/integration_test.go
git commit -m "test(telemetry): integration test for poller + handler end-to-end

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 15: `cmd/llamactl-telemetryd/main.go`

Daemon entry point. Loads config, validates host/api_key combo, starts logger to `~/Library/Logs/llamactl/telemetryd.log`, builds Poller + Handler, listens on configured port, handles SIGTERM.

**Files:** create `cmd/llamactl-telemetryd/main.go`.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregmundy/llamactl/internal/config"
	"github.com/gregmundy/llamactl/internal/launchd"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/telemetry"
)

var telemetrydVersion = "dev"

const (
	defaultPort     = 18080
	defaultHost     = "0.0.0.0"
	defaultInterval = 2 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "llamactl-telemetryd:", err)
		os.Exit(1)
	}
}

func run() error {
	paths, err := config.New()
	if err != nil {
		return fmt.Errorf("resolve paths: %w", err)
	}
	cfg, err := config.Load(paths.ConfigFile())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	host := strings.TrimSpace(cfg.TelemetryHost)
	if host == "" {
		host = defaultHost
	}
	port := cfg.TelemetryPort
	if port == 0 {
		port = defaultPort
	}
	interval := defaultInterval
	if cfg.TelemetryInterval != "" {
		if d, err := time.ParseDuration(cfg.TelemetryInterval); err == nil {
			interval = d
		}
	}

	apiKey := os.Getenv("LLAMACTL_API_KEY")
	if apiKey == "" {
		apiKey = cfg.APIKey
	}

	if isPublicHost(host) && apiKey == "" {
		return errors.New(
			"telemetryd would bind " + host + " without authentication;\n" +
				"set api_key via `llamactl config set api_key <token>` or\n" +
				"bind locally via `llamactl config set telemetry_host 127.0.0.1`")
	}

	// Set up logger.
	logsDir := filepath.Join(paths.Home, "Library", "Logs", "llamactl")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir logs: %w", err)
	}
	logPath := filepath.Join(logsDir, "telemetryd.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log %s: %w", logPath, err)
	}
	defer logFile.Close()
	logger := slog.New(slog.NewTextHandler(io.MultiWriter(logFile, os.Stderr), &slog.HandlerOptions{
		Level: slogLevel(cfg.LogLevel),
	}))

	logger.Info("telemetryd starting", "version", telemetrydVersion, "host", host, "port", port, "interval", interval)

	state := telemetry.NewState()
	launchAgentsDir := filepath.Join(paths.Home, "Library", "LaunchAgents")
	modelsDir := paths.ModelsMetaDir()

	httpClient := &http.Client{Timeout: 0} // per-request timeout via context in Scrape
	poller := &telemetry.Poller{
		State:      state,
		Lister:     telemetry.LaunchdLister{},
		PlistDir:   launchAgentsDir,
		HTTPClient: httpClient,
		Interval:   interval,
		BaseURLFn:  telemetry.DefaultBaseURL,
	}

	listInstalled := func() []models.Metadata {
		store := models.NewFileStore(modelsDir)
		got, err := store.List(context.Background())
		if err != nil {
			logger.Warn("list installed", "err", err)
			return nil
		}
		return got
	}

	handler := telemetry.NewHandler(state, listInstalled, apiKey, time.Now)

	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", host, port),
		Handler: handler,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Run poller in the background.
	go poller.Run(ctx)

	// Run server until ctx done.
	go func() {
		<-ctx.Done()
		shutdown, sCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer sCancel()
		_ = srv.Shutdown(shutdown)
	}()

	logger.Info("listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", err)
	}
	logger.Info("telemetryd stopped")
	_ = launchd.TelemetrydLabel // keep launchd import for future use
	return nil
}

func isPublicHost(h string) bool {
	switch h {
	case "127.0.0.1", "::1", "localhost":
		return false
	}
	return true
}

func slogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
```

- [ ] Verify it compiles: `go build ./cmd/llamactl-telemetryd/`
- [ ] Smoke: `LLAMACTL_API_KEY=test ./llamactl-telemetryd` should start, log "listening", and respond on `/health`. Ctrl-C should shut it down cleanly.
- [ ] Commit:

```bash
git add cmd/llamactl-telemetryd/main.go
git commit -m "feat(telemetryd): cmd/llamactl-telemetryd main entry point

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 16: CLI `telemetry enable/disable/status` commands

**Files:**
- Create: `internal/cli/telemetry.go`
- Test: `internal/cli/telemetry_test.go`
- Modify: `internal/cli/root.go` to register the new command.

Implementation: cobra parent `telemetry` with three subcommands. `enable` validates config (api_key + public-host), resolves the absolute path to `llamactl-telemetryd` via `exec.LookPath`, renders the plist via `launchd.RenderTelemetryd`, writes via atomic temp+rename, bootstraps via `LaunchdService.Load`. `disable` calls `LaunchdService.Bootout` + `os.Remove(plistPath)`. `status` calls `LaunchdService.Print`.

Use the pattern from `internal/cli/stop.go` + `internal/cli/serve.go` for plist writing. Use `internal/cli/config.go` for the api_key check.

Skeleton:

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gregmundy/llamactl/internal/launchd"
	"github.com/spf13/cobra"
)

func newTelemetryCmd(d *Deps) *cobra.Command {
	cmd := &cobra.Command{Use: "telemetry", Short: "Manage the telemetry sidecar daemon"}
	cmd.AddCommand(newTelemetryEnableCmd(d))
	cmd.AddCommand(newTelemetryDisableCmd(d))
	cmd.AddCommand(newTelemetryStatusCmd(d))
	return cmd
}

func newTelemetryEnableCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use: "enable", Short: "Start the telemetry sidecar (persistent across reboots)",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runTelemetryEnable(cmd.Context(), d) },
	}
}

func runTelemetryEnable(ctx context.Context, d *Deps) error {
	host := d.Config.TelemetryHost
	if host == "" {
		host = "0.0.0.0"
	}
	apiKey := d.Getenv("LLAMACTL_API_KEY")
	if apiKey == "" {
		apiKey = d.Config.APIKey
	}
	if isPublicHost(host) && apiKey == "" {
		return fmt.Errorf("%w: telemetry_host=%s requires api_key; run `llamactl config set api_key <token>` or `llamactl config set telemetry_host 127.0.0.1`",
			ErrUserError, host)
	}

	binPath, err := resolveTelemetrydBinary(d)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(d.LaunchAgentsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir LaunchAgents: %w", err)
	}
	logsDir := d.LogsDir
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir Logs: %w", err)
	}

	home, err := d.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}

	spec := launchd.TelemetrydSpec{
		Label:      launchd.TelemetrydLabel,
		BinaryPath: binPath,
		LogPath:    filepath.Join(logsDir, "telemetryd.log"),
		WorkingDir: home,
	}
	body, err := launchd.RenderTelemetryd(spec)
	if err != nil {
		return err
	}
	plistPath := filepath.Join(d.LaunchAgentsDir, launchd.TelemetrydLabel+".plist")
	tmp := plistPath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("write plist tmp: %w", err)
	}
	if err := os.Rename(tmp, plistPath); err != nil {
		return fmt.Errorf("rename plist: %w", err)
	}

	// Bootout any old instance.
	if existing, _ := d.LaunchdService.Print(ctx, launchd.TelemetrydLabel); existing.PID != 0 {
		_ = d.LaunchdService.Bootout(ctx, launchd.TelemetrydLabel)
	}
	if err := d.LaunchdService.Load(ctx, plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}
	port := d.Config.TelemetryPort
	if port == 0 {
		port = 18080
	}
	fmt.Fprintf(d.Stdout, "telemetryd started on http://%s:%d\n", host, port)
	return nil
}

func resolveTelemetrydBinary(d *Deps) (string, error) {
	if p, err := d.LookPath("llamactl-telemetryd"); err == nil {
		return p, nil
	}
	// Fallback: same dir as the running llamactl binary.
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "llamactl-telemetryd")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: cannot find llamactl-telemetryd binary in PATH", ErrUserError)
}

func newTelemetryDisableCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use: "disable", Short: "Stop and remove the telemetry sidecar",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runTelemetryDisable(cmd.Context(), d) },
	}
}

func runTelemetryDisable(ctx context.Context, d *Deps) error {
	_ = d.LaunchdService.Bootout(ctx, launchd.TelemetrydLabel)
	plistPath := filepath.Join(d.LaunchAgentsDir, launchd.TelemetrydLabel+".plist")
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	fmt.Fprintln(d.Stdout, "telemetryd stopped")
	return nil
}

func newTelemetryStatusCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use: "status", Short: "Show telemetry sidecar status",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runTelemetryStatus(cmd.Context(), d) },
	}
}

func runTelemetryStatus(ctx context.Context, d *Deps) error {
	info, _ := d.LaunchdService.Print(ctx, launchd.TelemetrydLabel)
	if info.PID == 0 {
		fmt.Fprintln(d.Stdout, "telemetryd: stopped")
		return nil
	}
	port := d.Config.TelemetryPort
	if port == 0 {
		port = 18080
	}
	host := d.Config.TelemetryHost
	if host == "" {
		host = "0.0.0.0"
	}
	fmt.Fprintf(d.Stdout, "telemetryd: running (pid=%d, host=%s, port=%d)\n", info.PID, host, port)
	return nil
}

func isPublicHost(h string) bool {
	switch h {
	case "127.0.0.1", "::1", "localhost":
		return false
	}
	return true
}

// silence unused import lint
var _ = exec.LookPath
```

Register in `internal/cli/root.go` alongside the other `root.AddCommand(new*Cmd(d))` lines: `root.AddCommand(newTelemetryCmd(deps))`.

Tests (`internal/cli/telemetry_test.go`): cover refuse-without-api-key, plist-write happens, disable cleans up. Use a `fakeLaunchdService` (the pattern likely already exists in `internal/cli/*_test.go`).

- [ ] Test, implement, commit:

```bash
git add internal/cli/telemetry.go internal/cli/telemetry_test.go internal/cli/root.go
git commit -m "feat(cli): \`llamactl telemetry enable/disable/status\` management commands

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 17: Doctor `telemetryAuthCheck` (14 → 15)

Returns warning when telemetryd plist exists AND host is public AND api_key empty. Returns warning when telemetryd plist exists but port is unreachable (dial-probe via existing `proc.portInUse` pattern).

**Files:** modify `internal/cli/doctor.go` (add function + append to `buildDoctorChecks`), `internal/cli/doctor_test.go`.

Append after the existing `authOnPublicBindCheck`:

```go
func telemetryAuthCheck(deps *Deps) doctorCheck {
	return doctorCheck{
		label:       "Telemetry daemon authentication",
		remediation: "if you've enabled telemetry on a public bind, set api_key: `llamactl config set api_key <token>`",
		run: func(ctx context.Context, deps *Deps) (bool, string) {
			plistPath := filepath.Join(deps.LaunchAgentsDir, launchd.TelemetrydLabel+".plist")
			if _, err := os.Stat(plistPath); err != nil {
				return true, "(not enabled)"
			}
			host := deps.Config.TelemetryHost
			if host == "" {
				host = "0.0.0.0"
			}
			apiKey := deps.Getenv("LLAMACTL_API_KEY")
			if apiKey == "" {
				apiKey = deps.Config.APIKey
			}
			if isPublicHost(host) && apiKey == "" {
				return false, fmt.Sprintf("bound %s without api_key", host)
			}
			port := deps.Config.TelemetryPort
			if port == 0 {
				port = 18080
			}
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
			if err != nil {
				return false, fmt.Sprintf("plist present but :%d not responding", port)
			}
			_ = conn.Close()
			return true, fmt.Sprintf("running on %s:%d (auth: %s)", host, port, redactedAuth(apiKey))
		},
	}
}

func redactedAuth(k string) string {
	if k == "" {
		return "none"
	}
	return "bearer"
}
```

Append to `buildDoctorChecks` slice: `telemetryAuthCheck(deps),` — bringing the count from 14 to 15.

- [ ] Tests covering the three branches (not-enabled / unauth-public / not-responding).
- [ ] Commit:

```bash
git add internal/cli/doctor.go internal/cli/doctor_test.go
git commit -m "feat(doctor): telemetryAuthCheck (15th check)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 18: GoReleaser — second binary + cask binaries

Modify `.goreleaser.yml` to add a second build entry and include both binaries in the archive and cask.

**Files:** modify `.goreleaser.yml`.

```yaml
builds:
  - id: llamactl
    main: ./cmd/llamactl
    binary: llamactl
    env: [CGO_ENABLED=0]
    goos: [darwin]
    goarch: [arm64]
    ldflags:
      - -s -w -X main.llamactlVersion=v{{.Version}}
  - id: llamactl-telemetryd
    main: ./cmd/llamactl-telemetryd
    binary: llamactl-telemetryd
    env: [CGO_ENABLED=0]
    goos: [darwin]
    goarch: [arm64]
    ldflags:
      - -s -w -X main.telemetrydVersion=v{{.Version}}

archives:
  - id: default
    ids:
      - llamactl
      - llamactl-telemetryd
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    formats: [tar.gz]
    files:
      - LICENSE*
      - README.md
      - completions/*

homebrew_casks:
  - name: llamactl
    binaries:
      - llamactl
      - llamactl-telemetryd
    completions: ...
```

(Preserve the rest of the file as-is. Just add the second `builds:` entry, add `llamactl-telemetryd` to `archives.ids` and to `homebrew_casks.binaries`.)

- [ ] Validate locally: `goreleaser check`
- [ ] Commit:

```bash
git add .goreleaser.yml
git commit -m "feat(release): ship llamactl-telemetryd alongside llamactl in cask

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 19: README — Telemetry API section

Add a new section to `README.md` between "Authentication" and "Reference":

```markdown
## Telemetry API

`llamactl-telemetryd` is an optional sidecar daemon that exposes a JSON
endpoint summarizing installed and running models. Useful for personal
dashboards, status widgets, or any external API that wants visibility
into what the host is doing.

Enable it (requires `api_key` when binding publicly):

```sh
llamactl config set api_key sk-your-token-here
llamactl telemetry enable
```

The daemon listens on `0.0.0.0:18080` by default. Override per-key:
`telemetry_host`, `telemetry_port`, `telemetry_interval`.

Fetch the current snapshot:

```sh
curl -H "Authorization: Bearer sk-your-token-here" \
  http://llm-mini.tailnet.ts.net:18080/v1/telemetry
```

Response (abbreviated — see `docs/superpowers/specs/2026-05-16-telemetry-sidecar-design.md` §5.3 for the full schema):

```json
{
  "generated_at": "2026-05-16T14:23:11Z",
  "installed": [{"id":"qwen2.5-3b-instruct","size_bytes":2469606195, ...}],
  "running":   [{"id":"qwen2.5-3b-instruct","port":8082,"state":"idle","tokens_per_second":0.0, ...}]
}
```

`tokens_per_second` is a rolling rate computed over the most recent
`telemetry_interval` (default 2 s). It is `null` immediately after enable
(no prior sample to delta against), and `0.0` when no generation has
happened between two polls.

`llamactl telemetry status` reports the daemon state; `llamactl telemetry disable`
boots it out and removes the plist. `llamactl doctor` flags telemetry
misconfigurations (15th check).
```

- [ ] Commit:

```bash
git add README.md
git commit -m "docs(readme): document the telemetry sidecar API

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Final verification before tagging

- [ ] `gofmt -l .` must produce no output.
- [ ] `go vet ./...` must be clean.
- [ ] `go test ./... -race` must be clean.
- [ ] `go install ./cmd/llamactl ./cmd/llamactl-telemetryd` builds both.
- [ ] Live smoke per spec §12.3.
- [ ] Update memory `project_state.md` with what shipped + the new `--metrics` recipe behavior + the [no-daemon-rule-scope] formalization.

