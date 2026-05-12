# llamactl Phase 3: serve / launchd / status / stop — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the `serve`, `stop`, `status` commands plus 5 new `doctor` checks. `serve --detach` registers a launchd LaunchAgent; foreground runs propagate Ctrl-C cleanly. Covers PRD acceptance criteria 8, 9, 10, 13, 14, 15, 16.

**Architecture:** Three new packages (`internal/recipes`, `internal/launchd`, `internal/proc`) plus extensions to `internal/cli` (4 new commands) and `internal/models` (extend Metadata, export one helper). All host subprocess work goes through Phase 1's `runner.CommandRunner` seam; the `Deps` struct gains 4 new narrow interfaces.

**Tech Stack:** Go 1.26.2 (unchanged). No new external deps. Stdlib `text/template`, `os/exec`, `os/signal`, `syscall`, `net`. Existing `golang.org/x/sys/unix` and `golang.org/x/term`.

**Working branch:** `phase3-serve`. **Every implementer prompt must state this branch and forbid `git checkout/switch/stash/branch`.**

---

## Spec coverage

| Spec § | Requirement | Task |
|--------|-------------|------|
| §3.1 + §8 | Metadata extension (LastServedAt), selector helper export | Task 1 |
| §4 | `internal/recipes` package | Task 2 |
| §5.1 | Plist template + Render | Task 3 |
| §5.2 | launchd.Service (Load/Bootout/Print) | Task 4 |
| §5.2 (list) | ListLLMServices directory scan | Task 5 |
| §3.1 (proc) | FreePort allocator | Task 6 |
| §3.1 (proc) | ps wrapper (RSS, Uptime) | Task 7 |
| §3.1 (proc) | Log tail / Rate parser | Task 8 |
| §7 | Deps additions + concrete adapters | Task 9 |
| §6.1 | `serve` (foreground + --detach) | Task 10 |
| §6.2 | `stop [<id>]` | Task 11 |
| §6.3 | `status [--json]` | Task 12 |
| §6.4 | `doctor` 6 new checks | Task 13 |
| §8 (list) | LAST-SERVED column | Task 14 |
| §7 (main) | cmd/llamactl/main.go wiring | Task 15 |
| §10.2 | Integration tests (foreground + detached) | Task 16 |

---

## File Structure

```
internal/
├── models/
│   ├── metadata.go               MODIFY — add LastServedAt
│   └── selector.go               MODIFY — export GpuAddressableGB
│
├── recipes/                      NEW
│   ├── recipes.go                Recipe type, Recipes map, FlagsFor
│   └── recipes_test.go
│
├── launchd/                      NEW
│   ├── plist.go                  PlistSpec + Render
│   ├── plist_test.go
│   ├── service.go                Service + Load/Bootout/Print
│   ├── service_test.go
│   ├── list.go                   ListLLMServices
│   ├── list_test.go
│   └── testdata/
│       └── sample.plist          golden render fixture
│
├── proc/                         NEW
│   ├── port.go                   FreePort, Allocator
│   ├── port_test.go
│   ├── ps.go                     Inspector (RSS, Uptime)
│   ├── ps_test.go
│   ├── logtail.go                TailRate.Rate
│   └── logtail_test.go
│
└── cli/
    ├── deps.go                   MODIFY — add LaunchdService, PortAllocator,
    │                                       ProcInspector, TokRateReader interfaces
    │                                       + adapter types
    ├── serve.go                  NEW
    ├── serve_test.go             NEW
    ├── stop.go                   NEW
    ├── stop_test.go              NEW
    ├── status.go                 NEW
    ├── status_test.go            NEW
    ├── doctor.go                 MODIFY — 6 new check functions
    ├── doctor_test.go            MODIFY — 6 new ✓/✗ test pairs
    ├── list.go                   MODIFY — add LAST-SERVED column
    ├── list_test.go              MODIFY — assert LAST-SERVED present
    ├── root.go                   MODIFY — register serve/stop/status commands
    └── integration_test.go       MODIFY — two new Phase 3 tests

cmd/llamactl/main.go              MODIFY — wire concrete launchd/proc adapters
```

---

## Task 0: Create feature branch

**Files:** none (git only)

- [ ] **Step 1: Confirm starting state**

```bash
git status && git branch --show-current && git log -1 --oneline
```
Expected: clean working tree, on `main` at `682f199` (Phase 3 spec) or later.

- [ ] **Step 2: Create branch**

```bash
git checkout -b phase3-serve
git branch --show-current
```
Expected: `phase3-serve`.

---

## Task 1: Metadata.LastServedAt + export GpuAddressableGB

**Files:**
- Modify: `internal/models/metadata.go`
- Modify: `internal/models/selector.go`

Two small mechanical edits combined in one commit because Phase 2 tests already cover the surrounding behavior.

- [ ] **Step 1: Extend `internal/models/metadata.go`**

Replace the `Metadata` struct entirely:

```go
package models

import "time"

// Metadata is the per-tool JSON record written to
// ~/.config/llamactl/models/<id>.json after `add` succeeds.
type Metadata struct {
	ID        string    `json:"id"`
	Repo      string    `json:"repo"`
	Quant     Quant     `json:"quant"`
	SHA256    string    `json:"sha256"`
	GGUFPath  string    `json:"gguf_path"`
	SizeBytes int64     `json:"size_bytes"`
	AddedAt   time.Time `json:"added_at"`

	ParamsB int  `json:"params_b,omitempty"`
	Arch    Arch `json:"arch,omitempty"`

	// Phase 3 addition. Updated by `serve` (foreground or detached)
	// immediately before launching llama-server.
	LastServedAt time.Time `json:"last_served_at,omitempty"`
}
```

- [ ] **Step 2: Export `GpuAddressableGB` in `internal/models/selector.go`**

Find the function `gpuAddressableGB` and rename it to `GpuAddressableGB`. Also update its single internal caller (`SelectQuant`). The function signature stays exactly the same; only the name changes.

```bash
grep -n "gpuAddressableGB" internal/models/selector.go
```
Expected: 2 hits (definition + one caller). Rename both.

- [ ] **Step 3: Build + test**

```bash
go build ./...
go test ./...
```
Expected: all passing. No behavior change.

- [ ] **Step 4: Commit**

```bash
git add internal/models/metadata.go internal/models/selector.go
git commit -m "feat(models): add Metadata.LastServedAt; export GpuAddressableGB for recipes"
```

---

## Task 2: `internal/recipes` package

**Files:**
- Create: `internal/recipes/recipes.go`
- Create: `internal/recipes/recipes_test.go`

The whole package is pure data + one pure function. No I/O. No mocking.

- [ ] **Step 1: Write the failing test**

Create `internal/recipes/recipes_test.go`:

```go
package recipes

import (
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/server"
)

func TestRecipesMapWellFormed(t *testing.T) {
	want := []string{"chat", "code", "long-context", "low-memory"}
	for _, name := range want {
		r, ok := Recipes[name]
		if !ok {
			t.Errorf("Recipes[%q] missing", name)
			continue
		}
		if r.Name != name {
			t.Errorf("Recipes[%q].Name = %q", name, r.Name)
		}
		if r.CtxSize <= 0 {
			t.Errorf("Recipes[%q].CtxSize = %d", name, r.CtxSize)
		}
	}
}

func argvFlag(args []string, name string) (string, bool) {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func argvHasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func mkModel(maxCtx int) models.Model {
	return models.Model{ID: "fake", HFRepo: "x/y", Arch: models.ArchLlama3, ParamsB: 7, MaxCtx: maxCtx}
}

func mkHW(ramGB int) hardware.Info {
	return hardware.Info{RAMBytes: uint64(ramGB) * (1 << 30)}
}

func mkVer(build int) server.Version {
	return server.Version{Build: build}
}

func TestFlagsFor_BaseArgvForChat(t *testing.T) {
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/path/to.gguf", mkHW(64), mkVer(4500), 4.4, 8080)
	if v, _ := argvFlag(args, "--ctx-size"); v != "8192" {
		t.Errorf("--ctx-size = %q, want 8192", v)
	}
	if v, _ := argvFlag(args, "--cache-type-k"); v != "f16" {
		t.Errorf("--cache-type-k = %q, want f16", v)
	}
	if v, _ := argvFlag(args, "--host"); v != "0.0.0.0" {
		t.Errorf("--host = %q, want 0.0.0.0", v)
	}
	if v, _ := argvFlag(args, "--port"); v != "8080" {
		t.Errorf("--port = %q, want 8080", v)
	}
	if v, _ := argvFlag(args, "--n-gpu-layers"); v != "999" {
		t.Errorf("--n-gpu-layers = %q, want 999", v)
	}
}

func TestFlagsFor_CtxSizeClampedToModelMaxCtx(t *testing.T) {
	args := FlagsFor(Recipes["long-context"], mkModel(4096), models.Q4_K_M, "/x", mkHW(64), mkVer(4500), 4.4, 8080)
	if v, _ := argvFlag(args, "--ctx-size"); v != "4096" {
		t.Errorf("--ctx-size = %q, want 4096 (clamped from 32768)", v)
	}
}

func TestFlagsFor_MlockOnLargeHost(t *testing.T) {
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/x", mkHW(64), mkVer(4500), 4.4, 8080)
	if !argvHasFlag(args, "--mlock") {
		t.Error("expected --mlock on 64GB host serving 4.4GB model")
	}
}

func TestFlagsFor_NoMlockOnTightHost(t *testing.T) {
	// 16 GB host serving a 14B Q5_K_M (10.4GB) — budget = 0.72, no mlock
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q5_K_M, "/x", mkHW(16), mkVer(4500), 10.4, 8080)
	if argvHasFlag(args, "--mlock") {
		t.Error("expected NO --mlock on tight host")
	}
}

func TestFlagsFor_LowMemoryRecipeAlwaysNoMlock(t *testing.T) {
	args := FlagsFor(Recipes["low-memory"], mkModel(32768), models.Q4_K_M, "/x", mkHW(128), mkVer(4500), 4.4, 8080)
	if argvHasFlag(args, "--mlock") {
		t.Error("low-memory recipe must never set --mlock, even on huge host")
	}
	if v, _ := argvFlag(args, "--cache-type-k"); v != "q4_0" {
		t.Errorf("low-memory --cache-type-k = %q, want q4_0", v)
	}
}

func TestFlagsFor_FlashAttnOnModernBuild(t *testing.T) {
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/x", mkHW(64), mkVer(4500), 4.4, 8080)
	if !argvHasFlag(args, "--flash-attn") {
		t.Error("expected --flash-attn on build 4500")
	}
}

func TestFlagsFor_FlashAttnSkippedOnOldHomebrew(t *testing.T) {
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/x", mkHW(64), mkVer(1500), 4.4, 8080)
	if argvHasFlag(args, "--flash-attn") {
		t.Error("expected NO --flash-attn on build 1500")
	}
}

func TestFlagsFor_FlashAttnOnLlamavmCustom(t *testing.T) {
	// llamavm-managed builds use cmake counter (small numbers). Assume modern.
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/x", mkHW(64), mkVer(3), 4.4, 8080)
	if !argvHasFlag(args, "--flash-attn") {
		t.Error("expected --flash-attn on llamavm build (Build=3)")
	}
}

func TestFlagsFor_ModelPathIncluded(t *testing.T) {
	args := FlagsFor(Recipes["chat"], mkModel(32768), models.Q4_K_M, "/path/to/model.gguf", mkHW(64), mkVer(4500), 4.4, 8080)
	v, _ := argvFlag(args, "--model")
	if v != "/path/to/model.gguf" {
		t.Errorf("--model = %q, want /path/to/model.gguf", v)
	}
	// And argv should never contain ; or & or other shell metachars from filenames
	for _, a := range args {
		if strings.ContainsAny(a, ";&|") {
			t.Errorf("argv entry %q contains shell metachars", a)
		}
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/recipes/...
```
Expected: `package does not exist`.

- [ ] **Step 3: Write `internal/recipes/recipes.go`**

```go
// Package recipes encodes the four PRD §6.2 recipe→flag mappings and
// the rules for assembling a llama-server argv from a recipe + model +
// host + version. Pure data and pure functions: no I/O, no clocks, no
// env reads.
package recipes

import (
	"fmt"

	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/platform"
	"github.com/gregmundy/llamactl/internal/server"
)

type MlockMode int

const (
	MlockAuto MlockMode = iota // include --mlock when usable_gb > size_gb + 4
	MlockOff                   // never include --mlock
)

type Recipe struct {
	Name       string
	CtxSize    int
	CacheTypeK string
	CacheTypeV string
	MlockMode  MlockMode
}

const DefaultRecipe = "chat"

// MinFlashAttnBuild is the empirical floor for stable --flash-attn on
// Apple Silicon. llamavm-managed customs use a cmake counter starting at
// 1; those are treated as "modern" by shouldAddFlashAttn.
const MinFlashAttnBuild = 2700

var Recipes = map[string]Recipe{
	"chat":         {Name: "chat", CtxSize: 8192, CacheTypeK: "f16", CacheTypeV: "f16", MlockMode: MlockAuto},
	"code":         {Name: "code", CtxSize: 16384, CacheTypeK: "f16", CacheTypeV: "f16", MlockMode: MlockAuto},
	"long-context": {Name: "long-context", CtxSize: 32768, CacheTypeK: "q8_0", CacheTypeV: "q8_0", MlockMode: MlockAuto},
	"low-memory":   {Name: "low-memory", CtxSize: 4096, CacheTypeK: "q4_0", CacheTypeV: "q4_0", MlockMode: MlockOff},
}

// FlagsFor assembles the llama-server argv. Inputs are read-only.
func FlagsFor(r Recipe, m models.Model, _ models.Quant, ggufPath string,
	hw hardware.Info, ver server.Version, sizeGB float64, port int) []string {

	ctxSize := r.CtxSize
	if m.MaxCtx > 0 && m.MaxCtx < ctxSize {
		ctxSize = m.MaxCtx
	}

	threads := platform.Cores() - 2
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
		args = append(args, "--flash-attn")
	}

	return args
}

// shouldAddFlashAttn returns true if the resolved llama-server is new
// enough (or appears to be a llamavm-managed custom build using the
// cmake counter, which we assume is modern).
func shouldAddFlashAttn(v server.Version) bool {
	if v.Build < 100 {
		return true
	}
	return v.Build >= MinFlashAttnBuild
}
```

- [ ] **Step 4: Verify `models.Quant` is still referenced (linter check)**

The unused `_ models.Quant` parameter is intentional — we keep the signature accepting the chosen quant for symmetry/future use, even though the current implementation doesn't read it. If a linter flags this, accept the warning; do not remove the parameter.

- [ ] **Step 5: Run, confirm pass**

```bash
go test ./internal/recipes/...
go vet ./internal/recipes/...
```
Expected: 9 tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/recipes/recipes.go internal/recipes/recipes_test.go
git commit -m "feat(recipes): chat/code/long-context/low-memory flag mapping"
```

---

## Task 3: `internal/launchd` plist render

**Files:**
- Create: `internal/launchd/plist.go`
- Create: `internal/launchd/plist_test.go`
- Create: `internal/launchd/testdata/sample.plist`

- [ ] **Step 1: Write the golden test**

Create `internal/launchd/testdata/sample.plist` (exact whitespace matters):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.llamactl.qwen2.5-7b-instruct</string>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/homebrew/bin/llama-server</string>
    <string>--model</string>
    <string>/Users/greg/.local/share/llama-models/qwen2.5-7b-instruct/Q4_K_M.gguf</string>
    <string>--host</string>
    <string>0.0.0.0</string>
    <string>--port</string>
    <string>8080</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>WorkingDirectory</key>
  <string>/Users/greg</string>
  <key>StandardOutPath</key>
  <string>/Users/greg/Library/Logs/llamactl/qwen2.5-7b-instruct.log</string>
  <key>StandardErrorPath</key>
  <string>/Users/greg/Library/Logs/llamactl/qwen2.5-7b-instruct.log</string>
  <key>ProcessType</key>
  <string>Interactive</string>
</dict>
</plist>
```

Create `internal/launchd/plist_test.go`:

```go
package launchd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderMatchesGolden(t *testing.T) {
	spec := PlistSpec{
		Label:       "com.llamactl.qwen2.5-7b-instruct",
		LlamaServer: "/opt/homebrew/bin/llama-server",
		Args: []string{
			"--model", "/Users/greg/.local/share/llama-models/qwen2.5-7b-instruct/Q4_K_M.gguf",
			"--host", "0.0.0.0",
			"--port", "8080",
		},
		LogPath:    "/Users/greg/Library/Logs/llamactl/qwen2.5-7b-instruct.log",
		WorkingDir: "/Users/greg",
	}
	got, err := Render(spec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "sample.plist"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Render output != golden\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderEscapesXMLInArgs(t *testing.T) {
	spec := PlistSpec{
		Label:       "com.llamactl.test",
		LlamaServer: "/x/llama-server",
		Args:        []string{"--model", "/p&t<file>.gguf"},
		LogPath:     "/log",
		WorkingDir:  "/home",
	}
	got, err := Render(spec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if bytes.Contains(got, []byte("/p&t<file>")) {
		t.Errorf("unescaped XML in output: %s", got)
	}
	if !bytes.Contains(got, []byte("/p&amp;t&lt;file&gt;")) {
		t.Errorf("expected escaped chars; got: %s", got)
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/launchd/...
```
Expected: package does not exist.

- [ ] **Step 3: Write `internal/launchd/plist.go`**

```go
// Package launchd renders LaunchAgent plists and wraps `launchctl` for
// loading, unloading, and querying llamactl-managed services.
package launchd

import (
	"bytes"
	"fmt"
	"text/template"
)

// PlistSpec captures every field that varies between llamactl services.
// All other plist contents are fixed by the template.
type PlistSpec struct {
	Label       string   // e.g. "com.llamactl.qwen2.5-7b-instruct"
	LlamaServer string   // absolute path to the resolved llama-server binary
	Args        []string // argv from recipes.FlagsFor (NOT including LlamaServer itself)
	LogPath     string   // ~/Library/Logs/llamactl/<id>.log
	WorkingDir  string   // user home
}

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>{{xml .Label}}</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{xml .LlamaServer}}</string>
{{range .Args}}    <string>{{xml .}}</string>
{{end}}  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>WorkingDirectory</key>
  <string>{{xml .WorkingDir}}</string>
  <key>StandardOutPath</key>
  <string>{{xml .LogPath}}</string>
  <key>StandardErrorPath</key>
  <string>{{xml .LogPath}}</string>
  <key>ProcessType</key>
  <string>Interactive</string>
</dict>
</plist>
`

// xmlEscape replaces &, <, > with their XML entities. The plist values
// are strings; no need for full XML entity coverage (quotes don't appear
// inside <string> bodies).
func xmlEscape(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var plistTpl = template.Must(template.New("plist").Funcs(template.FuncMap{
	"xml": xmlEscape,
}).Parse(plistTemplate))

// Render returns the rendered plist bytes for spec.
func Render(spec PlistSpec) ([]byte, error) {
	var buf bytes.Buffer
	if err := plistTpl.Execute(&buf, spec); err != nil {
		return nil, fmt.Errorf("render plist: %w", err)
	}
	return buf.Bytes(), nil
}
```

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/launchd/... -run Render -v
```
Expected: both tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/launchd/plist.go internal/launchd/plist_test.go internal/launchd/testdata/sample.plist
git commit -m "feat(launchd): PlistSpec + Render with XML escaping"
```

---

## Task 4: `internal/launchd.Service` — Load / Bootout / Print

**Files:**
- Create: `internal/launchd/service.go`
- Create: `internal/launchd/service_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/launchd/service_test.go`:

```go
package launchd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// fakeRunner mirrors the runner.CommandRunner shape used elsewhere.
// Keyed by name + " " + space-joined args.
type fakeRunner struct {
	outputs map[string]string
	errs    map[string]error
	calls   []string
}

func (r *fakeRunner) Run(_ context.Context, name string, args []string, _ string, stdout, _ io.Writer) error {
	key := name
	if len(args) > 0 {
		key += " " + strings.Join(args, " ")
	}
	r.calls = append(r.calls, key)
	if err, ok := r.errs[key]; ok {
		return err
	}
	if out, ok := r.outputs[key]; ok {
		_, _ = io.WriteString(stdout, out)
	}
	return nil
}

func TestServiceLoadInvokesBootstrap(t *testing.T) {
	r := &fakeRunner{}
	s := &Service{Runner: r, UID: 501}
	if err := s.Load(context.Background(), "/tmp/foo.plist"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := "launchctl bootstrap gui/501 /tmp/foo.plist"
	if len(r.calls) != 1 || r.calls[0] != want {
		t.Errorf("calls = %v, want [%q]", r.calls, want)
	}
}

func TestServiceBootoutInvokesBootout(t *testing.T) {
	r := &fakeRunner{}
	s := &Service{Runner: r, UID: 501}
	if err := s.Bootout(context.Background(), "com.llamactl.foo"); err != nil {
		t.Fatalf("Bootout: %v", err)
	}
	want := "launchctl bootout gui/501/com.llamactl.foo"
	if len(r.calls) != 1 || r.calls[0] != want {
		t.Errorf("calls = %v, want [%q]", r.calls, want)
	}
}

func TestServicePrintParsesLaunchctlOutput(t *testing.T) {
	output := `com.llamactl.qwen = {
	state = running
	pid = 12345
	last exit code = 0
	domain = gui/501
}
`
	r := &fakeRunner{
		outputs: map[string]string{
			"launchctl print gui/501/com.llamactl.qwen": output,
		},
	}
	s := &Service{Runner: r, UID: 501}
	info, err := s.Print(context.Background(), "com.llamactl.qwen")
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	if info.PID != 12345 {
		t.Errorf("PID = %d, want 12345", info.PID)
	}
	if info.State != "running" {
		t.Errorf("State = %q, want running", info.State)
	}
	if info.LastExit != 0 {
		t.Errorf("LastExit = %d, want 0", info.LastExit)
	}
	if info.Label != "com.llamactl.qwen" {
		t.Errorf("Label = %q", info.Label)
	}
}

func TestServicePrintReturnsZeroPIDOnNonZeroExit(t *testing.T) {
	r := &fakeRunner{
		errs: map[string]error{
			"launchctl print gui/501/com.llamactl.nope": errors.New("service does not exist"),
		},
	}
	s := &Service{Runner: r, UID: 501}
	info, err := s.Print(context.Background(), "com.llamactl.nope")
	if err != nil {
		t.Fatalf("Print should NOT return error for unloaded service: %v", err)
	}
	if info.PID != 0 {
		t.Errorf("PID = %d, want 0 for unloaded service", info.PID)
	}
	if info.Label != "com.llamactl.nope" {
		t.Errorf("Label = %q", info.Label)
	}
}

func TestServicePrintPartialOutput(t *testing.T) {
	// Service is loaded but spawning; no PID yet.
	output := `com.llamactl.qwen = {
	state = spawning
	domain = gui/501
}
`
	r := &fakeRunner{
		outputs: map[string]string{
			"launchctl print gui/501/com.llamactl.qwen": output,
		},
	}
	s := &Service{Runner: r, UID: 501}
	info, err := s.Print(context.Background(), "com.llamactl.qwen")
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	if info.PID != 0 {
		t.Errorf("PID = %d, want 0", info.PID)
	}
	if info.State != "spawning" {
		t.Errorf("State = %q, want spawning", info.State)
	}
}

// reference imports so the file compiles
var _ = bytes.NewBuffer
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/launchd/... -run Service
```
Expected: undefined symbols.

- [ ] **Step 3: Write `internal/launchd/service.go`**

```go
package launchd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// CommandRunner is the subprocess seam, locally redeclared so this
// package doesn't import internal/runner (same Go structural-typing
// pattern used in internal/hardware and internal/server).
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, stdin string, stdout, stderr io.Writer) error
}

// Service issues launchctl subcommands in the gui/<UID> domain.
type Service struct {
	Runner CommandRunner
	UID    int
}

// ServiceInfo is the subset of `launchctl print` output we consume.
type ServiceInfo struct {
	Label     string
	PlistPath string
	PID       int
	State     string
	LastExit  int
}

func (s *Service) domain() string {
	return fmt.Sprintf("gui/%d", s.UID)
}

// Load bootstraps a plist into the user GUI domain.
func (s *Service) Load(ctx context.Context, plistPath string) error {
	return s.Runner.Run(ctx, "launchctl", []string{"bootstrap", s.domain(), plistPath}, "", io.Discard, io.Discard)
}

// Bootout removes a service from the user GUI domain.
func (s *Service) Bootout(ctx context.Context, label string) error {
	target := s.domain() + "/" + label
	return s.Runner.Run(ctx, "launchctl", []string{"bootout", target}, "", io.Discard, io.Discard)
}

// Print queries launchctl for a service's state. Returns a zero-PID
// ServiceInfo (and nil error) when the service isn't loaded — that's
// not a programming error, it's just "not running".
func (s *Service) Print(ctx context.Context, label string) (ServiceInfo, error) {
	target := s.domain() + "/" + label
	var buf bytes.Buffer
	if err := s.Runner.Run(ctx, "launchctl", []string{"print", target}, "", &buf, io.Discard); err != nil {
		// launchctl exits nonzero when the service isn't loaded.
		// That's a normal "stopped" state, not a failure.
		return ServiceInfo{Label: label}, nil
	}
	return parsePrintOutput(label, buf.String()), nil
}

// parsePrintOutput extracts {state, pid, last exit code} from `launchctl
// print`'s human-readable output. Missing keys leave their fields zero.
func parsePrintOutput(label, output string) ServiceInfo {
	info := ServiceInfo{Label: label}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "state = "):
			info.State = strings.TrimPrefix(line, "state = ")
		case strings.HasPrefix(line, "pid = "):
			if n, err := strconv.Atoi(strings.TrimPrefix(line, "pid = ")); err == nil {
				info.PID = n
			}
		case strings.HasPrefix(line, "last exit code = "):
			if n, err := strconv.Atoi(strings.TrimPrefix(line, "last exit code = ")); err == nil {
				info.LastExit = n
			}
		}
	}
	return info
}
```

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/launchd/... -v
```
Expected: 6 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/launchd/service.go internal/launchd/service_test.go
git commit -m "feat(launchd): Service (Load/Bootout/Print) with parsed output"
```

---

## Task 5: `internal/launchd.ListLLMServices`

**Files:**
- Create: `internal/launchd/list.go`
- Create: `internal/launchd/list_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/launchd/list_test.go`:

```go
package launchd

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestListLLMServicesScansDirectory(t *testing.T) {
	dir := t.TempDir()
	// Three llamactl plists, one foreign plist, one non-plist file.
	for _, name := range []string{
		"com.llamactl.qwen2.5-7b-instruct.plist",
		"com.llamactl.llama3.2-3b.plist",
		"com.llamactl.mistral-7b-v0.3.plist",
		"com.example.other.plist",
		"README.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := &fakeRunner{}
	s := &Service{Runner: r, UID: 501}

	infos, err := ListLLMServices(context.Background(), dir, s)
	if err != nil {
		t.Fatalf("ListLLMServices: %v", err)
	}
	if len(infos) != 3 {
		t.Fatalf("len = %d, want 3", len(infos))
	}
	labels := make([]string, len(infos))
	for i, info := range infos {
		labels[i] = info.Label
	}
	sort.Strings(labels)
	want := []string{
		"com.llamactl.llama3.2-3b",
		"com.llamactl.mistral-7b-v0.3",
		"com.llamactl.qwen2.5-7b-instruct",
	}
	for i, w := range want {
		if labels[i] != w {
			t.Errorf("labels[%d] = %q, want %q", i, labels[i], w)
		}
	}
	for _, info := range infos {
		if info.PlistPath == "" {
			t.Errorf("PlistPath empty for %s", info.Label)
		}
	}
}

func TestListLLMServicesEmptyDir(t *testing.T) {
	dir := t.TempDir()
	r := &fakeRunner{}
	s := &Service{Runner: r, UID: 501}
	infos, err := ListLLMServices(context.Background(), dir, s)
	if err != nil {
		t.Fatalf("ListLLMServices: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("len = %d, want 0", len(infos))
	}
}

func TestListLLMServicesMissingDir(t *testing.T) {
	r := &fakeRunner{}
	s := &Service{Runner: r, UID: 501}
	infos, err := ListLLMServices(context.Background(), "/no/such/dir", s)
	if err != nil {
		t.Fatalf("ListLLMServices: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("len = %d, want 0", len(infos))
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/launchd/... -run List
```

- [ ] **Step 3: Write `internal/launchd/list.go`**

```go
package launchd

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const labelPrefix = "com.llamactl."

// ListLLMServices scans agentsDir for plist files whose basename starts
// with "com.llamactl.". For each, calls svc.Print to populate PID/State.
// Missing directory returns nil, nil (no services).
func ListLLMServices(ctx context.Context, agentsDir string, svc *Service) ([]ServiceInfo, error) {
	entries, err := os.ReadDir(agentsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []ServiceInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".plist") {
			continue
		}
		if !strings.HasPrefix(name, labelPrefix) {
			continue
		}
		label := strings.TrimSuffix(name, ".plist")
		info, _ := svc.Print(ctx, label)
		info.Label = label
		info.PlistPath = filepath.Join(agentsDir, name)
		out = append(out, info)
	}
	return out, nil
}
```

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/launchd/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/launchd/list.go internal/launchd/list_test.go
git commit -m "feat(launchd): ListLLMServices scans LaunchAgents directory"
```

---

## Task 6: `internal/proc.FreePort`

**Files:**
- Create: `internal/proc/port.go`
- Create: `internal/proc/port_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/proc/port_test.go`:

```go
package proc

import (
	"fmt"
	"net"
	"testing"
)

func TestFreePortReturnsPreferredWhenAvailable(t *testing.T) {
	// Find a known-free port by binding+releasing, then ask FreePort for it.
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	preferred := l.Addr().(*net.TCPAddr).Port
	l.Close()

	got, err := FreePort(preferred)
	if err != nil {
		t.Fatalf("FreePort: %v", err)
	}
	if got != preferred {
		t.Errorf("got %d, want %d (preferred should be returned when free)", got, preferred)
	}
}

func TestFreePortFallsThroughOnConflict(t *testing.T) {
	// Bind preferred port for the duration of the test.
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	preferred := l.Addr().(*net.TCPAddr).Port

	got, err := FreePort(preferred)
	if err != nil {
		t.Fatalf("FreePort: %v", err)
	}
	if got == preferred {
		t.Errorf("got %d, want anything but %d", got, preferred)
	}
	if got < preferred || got >= preferred+100 {
		t.Errorf("got %d, want value in [%d, %d)", got, preferred, preferred+100)
	}
}

func TestAllocatorImplementsPortAllocator(t *testing.T) {
	var a Allocator
	got, err := a.Free(0) // 0 means "let kernel pick"
	if err != nil {
		t.Fatalf("Free: %v", err)
	}
	if got <= 0 {
		t.Errorf("got %d, want >0", got)
	}
	_ = fmt.Sprint(got) // silence unused-import nag if we add fmt later
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/proc/...
```

- [ ] **Step 3: Write `internal/proc/port.go`**

```go
// Package proc provides host-process introspection helpers used by
// `serve`, `status`, and `doctor`. Each module (port, ps, logtail) is
// independent and testable in isolation.
package proc

import (
	"fmt"
	"net"
)

// FreePort returns preferred if it's currently bindable, or the next
// free port in [preferred, preferred+100). preferred=0 asks the kernel
// for an ephemeral port.
func FreePort(preferred int) (int, error) {
	if preferred == 0 {
		l, err := net.Listen("tcp", ":0")
		if err != nil {
			return 0, fmt.Errorf("kernel-assigned port: %w", err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		return port, nil
	}
	for p := preferred; p < preferred+100; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
		if err != nil {
			continue
		}
		_ = l.Close()
		return p, nil
	}
	return 0, fmt.Errorf("no free port in [%d, %d)", preferred, preferred+100)
}

// Allocator implements the cli.PortAllocator interface.
type Allocator struct{}

func (Allocator) Free(preferred int) (int, error) { return FreePort(preferred) }
```

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/proc/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/proc/port.go internal/proc/port_test.go
git commit -m "feat(proc): FreePort allocator with [preferred, preferred+100) scan"
```

---

## Task 7: `internal/proc.Inspector` — RSS + Uptime via `ps`

**Files:**
- Create: `internal/proc/ps.go`
- Create: `internal/proc/ps_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/proc/ps_test.go`:

```go
package proc

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeRunner mirrors runner.CommandRunner with a name+args key.
type fakeRunner struct {
	outputs map[string]string
	errs    map[string]error
}

func (r *fakeRunner) Run(_ context.Context, name string, args []string, _ string, stdout, _ io.Writer) error {
	key := name + " " + strings.Join(args, " ")
	if err, ok := r.errs[key]; ok {
		return err
	}
	if out, ok := r.outputs[key]; ok {
		_, _ = io.WriteString(stdout, out)
	}
	return nil
}

func TestRSSParsesKilobytesToBytes(t *testing.T) {
	r := &fakeRunner{
		outputs: map[string]string{
			"ps -o rss= -p 12345": "  1234567\n",
		},
	}
	i := &Inspector{Runner: r}
	got, err := i.RSS(12345)
	if err != nil {
		t.Fatalf("RSS: %v", err)
	}
	if got != 1234567*1024 {
		t.Errorf("got %d, want %d", got, 1234567*1024)
	}
}

func TestRSSProcessNotFound(t *testing.T) {
	r := &fakeRunner{
		errs: map[string]error{
			"ps -o rss= -p 99999": errors.New("exit 1"),
		},
	}
	i := &Inspector{Runner: r}
	_, err := i.RSS(99999)
	if !errors.Is(err, ErrProcessNotFound) {
		t.Errorf("err = %v, want ErrProcessNotFound", err)
	}
}

func TestUptimeMMSS(t *testing.T) {
	r := &fakeRunner{
		outputs: map[string]string{
			"ps -o etime= -p 100": "05:23\n",
		},
	}
	i := &Inspector{Runner: r}
	got, err := i.Uptime(100)
	if err != nil {
		t.Fatalf("Uptime: %v", err)
	}
	want := 5*time.Minute + 23*time.Second
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestUptimeHHMMSS(t *testing.T) {
	r := &fakeRunner{outputs: map[string]string{"ps -o etime= -p 100": "1:05:23\n"}}
	i := &Inspector{Runner: r}
	got, err := i.Uptime(100)
	if err != nil {
		t.Fatalf("Uptime: %v", err)
	}
	want := time.Hour + 5*time.Minute + 23*time.Second
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestUptimeDaysHHMMSS(t *testing.T) {
	r := &fakeRunner{outputs: map[string]string{"ps -o etime= -p 100": "2-01:05:23\n"}}
	i := &Inspector{Runner: r}
	got, err := i.Uptime(100)
	if err != nil {
		t.Fatalf("Uptime: %v", err)
	}
	want := 2*24*time.Hour + time.Hour + 5*time.Minute + 23*time.Second
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/proc/... -run RSS
```

- [ ] **Step 3: Write `internal/proc/ps.go`**

```go
package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ErrProcessNotFound is returned by Inspector when `ps` exits nonzero
// for the requested pid (process gone or never existed).
var ErrProcessNotFound = errors.New("process not found")

// CommandRunner is the subprocess seam, locally redeclared (same
// pattern as internal/hardware/internal/server).
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, stdin string, stdout, stderr io.Writer) error
}

// Inspector queries the kernel about a running pid via `ps`.
type Inspector struct {
	Runner CommandRunner
}

// RSS returns the resident-set size of pid in bytes.
func (i *Inspector) RSS(pid int) (int64, error) {
	var buf bytes.Buffer
	args := []string{"-o", "rss=", "-p", strconv.Itoa(pid)}
	if err := i.Runner.Run(context.Background(), "ps", args, "", &buf, io.Discard); err != nil {
		return 0, ErrProcessNotFound
	}
	field := strings.TrimSpace(buf.String())
	if field == "" {
		return 0, ErrProcessNotFound
	}
	kb, err := strconv.ParseInt(field, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse rss %q: %w", field, err)
	}
	return kb * 1024, nil
}

// Uptime returns elapsed wall time since pid started.
// `ps -o etime=` formats as [[dd-]hh:]mm:ss.
func (i *Inspector) Uptime(pid int) (time.Duration, error) {
	var buf bytes.Buffer
	args := []string{"-o", "etime=", "-p", strconv.Itoa(pid)}
	if err := i.Runner.Run(context.Background(), "ps", args, "", &buf, io.Discard); err != nil {
		return 0, ErrProcessNotFound
	}
	return parseEtime(strings.TrimSpace(buf.String()))
}

// parseEtime parses ps's etime field. Examples:
//   "05:23"        -> 5m 23s
//   "1:05:23"      -> 1h 5m 23s
//   "2-01:05:23"   -> 2d 1h 5m 23s
func parseEtime(s string) (time.Duration, error) {
	var days int
	if i := strings.Index(s, "-"); i != -1 {
		d, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, fmt.Errorf("parse etime days %q: %w", s, err)
		}
		days = d
		s = s[i+1:]
	}
	parts := strings.Split(s, ":")
	var h, m, sec int
	switch len(parts) {
	case 2:
		m, _ = strconv.Atoi(parts[0])
		sec, _ = strconv.Atoi(parts[1])
	case 3:
		h, _ = strconv.Atoi(parts[0])
		m, _ = strconv.Atoi(parts[1])
		sec, _ = strconv.Atoi(parts[2])
	default:
		return 0, fmt.Errorf("parse etime %q: unexpected format", s)
	}
	return time.Duration(days)*24*time.Hour +
		time.Duration(h)*time.Hour +
		time.Duration(m)*time.Minute +
		time.Duration(sec)*time.Second, nil
}
```

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/proc/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/proc/ps.go internal/proc/ps_test.go
git commit -m "feat(proc): Inspector RSS + Uptime via ps"
```

---

## Task 8: `internal/proc.TailRate` — log-tail tok/s parser

**Files:**
- Create: `internal/proc/logtail.go`
- Create: `internal/proc/logtail_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/proc/logtail_test.go`:

```go
package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLog(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRateMissingFileReturnsZero(t *testing.T) {
	r := &TailRate{}
	got, err := r.Rate("/no/such/file.log", time.Minute, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got != 0 {
		t.Errorf("got %f, want 0", got)
	}
}

func TestRateEmptyFileReturnsZero(t *testing.T) {
	path := writeLog(t, nil)
	r := &TailRate{}
	got, err := r.Rate(path, time.Minute, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got != 0 {
		t.Errorf("got %f, want 0", got)
	}
}

func TestRateNoEvalLinesReturnsZero(t *testing.T) {
	path := writeLog(t, []string{
		"some unrelated log line",
		"another line",
		"loaded model qwen2.5-7b",
	})
	r := &TailRate{}
	got, err := r.Rate(path, time.Minute, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got != 0 {
		t.Errorf("got %f, want 0", got)
	}
}

func TestRateParsesEvalLine(t *testing.T) {
	path := writeLog(t, []string{
		"eval time =    1000.00 ms /   100 tokens (   10.00 ms per token,   100.00 tokens per second)",
	})
	r := &TailRate{}
	got, err := r.Rate(path, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got < 99 || got > 101 {
		t.Errorf("got %f, want ~100", got)
	}
}

func TestRateWeightedAverageMultipleLines(t *testing.T) {
	// Sample A: 100 tokens in 1.0s → 100 t/s
	// Sample B: 200 tokens in 4.0s →  50 t/s
	// Weighted avg = (100+200) / (1.0+4.0) = 60 t/s
	path := writeLog(t, []string{
		"eval time =    1000.00 ms /   100 tokens (   10.00 ms per token,   100.00 tokens per second)",
		"eval time =    4000.00 ms /   200 tokens (   20.00 ms per token,    50.00 tokens per second)",
	})
	r := &TailRate{}
	got, err := r.Rate(path, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got < 59 || got > 61 {
		t.Errorf("got %f, want ~60 (weighted)", got)
	}
}

func TestRatePromptEvalAlsoParsed(t *testing.T) {
	path := writeLog(t, []string{
		"prompt eval time =    500.00 ms /    50 tokens (   10.00 ms per token,   100.00 tokens per second)",
	})
	r := &TailRate{}
	got, err := r.Rate(path, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got < 99 || got > 101 {
		t.Errorf("got %f, want ~100", got)
	}
}

func TestRateRespectsWindow(t *testing.T) {
	// Generate enough preceding garbage to exceed maxReadBytes (256 KiB),
	// then a single in-window eval line at the end. The line in window
	// should still be picked up because we scan from the end.
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, fmt.Sprintf("garbage line %d %s", i,
			"----------------------------------------------------------------"))
	}
	lines = append(lines,
		"eval time =    1000.00 ms /   100 tokens (   10.00 ms per token,   100.00 tokens per second)")
	path := writeLog(t, lines)
	r := &TailRate{}
	got, err := r.Rate(path, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got < 99 || got > 101 {
		t.Errorf("got %f, want ~100", got)
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/proc/... -run Rate
```

- [ ] **Step 3: Write `internal/proc/logtail.go`**

```go
package proc

import (
	"errors"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TailRate scans the tail of a llama-server log file and computes a
// token-weighted average tokens/sec from the eval lines.
//
// Note on the "rolling 60s window" PRD wording: llama-server's standard
// per-request log lines don't carry timestamps in every build, so we
// approximate "rolling" as "last maxReadBytes scanned from EOF". The
// window argument is accepted for API stability and reserved for a
// future build that does emit timestamps.
type TailRate struct{}

// maxReadBytes caps how far back from EOF we scan. 256 KiB easily
// covers the last few hundred eval lines on a busy server.
const maxReadBytes = 256 << 10

// evalLineRe matches both phrasings llama-server uses:
//   "eval time =   1000.00 ms /  100 tokens (   10.00 ms per token,    100.00 tokens per second)"
//   "prompt eval time =   500.00 ms /   50 tokens (   10.00 ms per token,    100.00 tokens per second)"
// Captures: ms (group 1), tokens (group 2).
var evalLineRe = regexp.MustCompile(
	`(?:prompt )?eval time\s*=\s*([0-9.]+)\s*ms\s*/\s*([0-9]+)\s+tokens`)

// Rate reads the tail of logPath and returns the token-weighted average
// tokens/sec across matched eval lines. Missing files and empty files
// return (0, nil).
func (r *TailRate) Rate(logPath string, window time.Duration, now time.Time) (float64, error) {
	_ = window // see TailRate doc comment

	f, err := os.Open(logPath)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := fi.Size()
	offset := int64(0)
	if size > maxReadBytes {
		offset = size - maxReadBytes
	}
	buf := make([]byte, size-offset)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return 0, err
	}

	var totalTokens int64
	var totalMs float64
	for _, line := range strings.Split(string(buf), "\n") {
		m := evalLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ms, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		tokens, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			continue
		}
		totalMs += ms
		totalTokens += tokens
	}
	if totalTokens == 0 || totalMs == 0 {
		return 0, nil
	}
	return float64(totalTokens) / (totalMs / 1000.0), nil
}
```

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/proc/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/proc/logtail.go internal/proc/logtail_test.go
git commit -m "feat(proc): TailRate computes weighted tok/s from llama-server eval lines"
```

---

## Task 9: Extend `Deps` + adapter types

**Files:**
- Modify: `internal/cli/deps.go`
- Create: `internal/cli/adapters_phase3.go`

- [ ] **Step 1: Inspect current Deps**

```bash
grep -n "type Deps struct\|^type " internal/cli/deps.go
```

- [ ] **Step 2: Modify `internal/cli/deps.go`**

Add these imports (alongside existing ones — preserve current imports):

```go
"time"

"github.com/gregmundy/llamactl/internal/launchd"
```

Add these interface declarations to `deps.go` (after the existing ProcInspector-adjacent types from Phase 2):

```go
// LaunchdService wraps launchctl operations.
type LaunchdService interface {
	Load(ctx context.Context, plistPath string) error
	Bootout(ctx context.Context, label string) error
	Print(ctx context.Context, label string) (launchd.ServiceInfo, error)
	List(ctx context.Context) ([]launchd.ServiceInfo, error)
}

// PortAllocator returns a bindable TCP port.
type PortAllocator interface {
	Free(preferred int) (int, error)
}

// ProcInspector queries the kernel about a running pid.
type ProcInspector interface {
	RSS(pid int) (int64, error)
	Uptime(pid int) (time.Duration, error)
}

// TokRateReader computes tokens/sec from a per-model log file.
type TokRateReader interface {
	Rate(logPath string, window time.Duration, now time.Time) (float64, error)
}
```

Extend the `Deps` struct (append, preserve order):

```go
type Deps struct {
	// ... Phase 1+2 fields ...

	LaunchdService LaunchdService
	PortAllocator  PortAllocator
	ProcInspector  ProcInspector
	TokRateReader  TokRateReader

	// Runner is the shared subprocess seam (Phase 1's runner.CommandRunner).
	// Doctor's Tailscale check shells out via this; future commands can too.
	Runner runner.CommandRunner

	LaunchAgentsDir string // ~/Library/LaunchAgents
	LogsDir         string // ~/Library/Logs/llamactl
}
```

Add to the import block: `"github.com/gregmundy/llamactl/internal/runner"`.

- [ ] **Step 3: Create `internal/cli/adapters_phase3.go`**

```go
package cli

import (
	"context"

	"github.com/gregmundy/llamactl/internal/launchd"
)

// LaunchdServiceAdapter wraps *launchd.Service so it satisfies the
// LaunchdService interface. The List method needs the agents dir, which
// the wrapper closes over so callers don't have to thread it through.
type LaunchdServiceAdapter struct {
	Service     *launchd.Service
	AgentsDir   string
}

func (a *LaunchdServiceAdapter) Load(ctx context.Context, plistPath string) error {
	return a.Service.Load(ctx, plistPath)
}

func (a *LaunchdServiceAdapter) Bootout(ctx context.Context, label string) error {
	return a.Service.Bootout(ctx, label)
}

func (a *LaunchdServiceAdapter) Print(ctx context.Context, label string) (launchd.ServiceInfo, error) {
	return a.Service.Print(ctx, label)
}

func (a *LaunchdServiceAdapter) List(ctx context.Context) ([]launchd.ServiceInfo, error) {
	return launchd.ListLLMServices(ctx, a.AgentsDir, a.Service)
}
```

- [ ] **Step 4: Build + test**

```bash
go build ./...
go test ./...
```
Expected: all passing. No commands consume the new fields yet.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/deps.go internal/cli/adapters_phase3.go
git commit -m "feat(cli): Deps additions for Phase 3 (launchd, port, proc, tokrate)"
```

---

## Task 10: `cli/serve` (foreground + --detach)

**Files:**
- Create: `internal/cli/serve.go`
- Create: `internal/cli/serve_test.go`
- Modify: `internal/cli/root.go` (register `newServeCmd`)
- Modify: `internal/cli/fakes_test.go` (add fakes for new interfaces)

The largest task in Phase 3. Test plan covers foreground + detached + port-shift + unknown-recipe + model-missing + re-serve.

- [ ] **Step 1: Add fakes to `internal/cli/fakes_test.go`**

Append to the existing `fakes_test.go`:

```go
// --- Phase 3 fakes ---

type fakeLaunchdService struct {
	Loaded     []string // plist paths passed to Load
	Booted     []string // labels passed to Bootout
	Services   map[string]launchd.ServiceInfo // by label
	ListResult []launchd.ServiceInfo
	LoadErr    error
	BootoutErr error
}

func (f *fakeLaunchdService) Load(_ context.Context, plistPath string) error {
	f.Loaded = append(f.Loaded, plistPath)
	return f.LoadErr
}
func (f *fakeLaunchdService) Bootout(_ context.Context, label string) error {
	f.Booted = append(f.Booted, label)
	return f.BootoutErr
}
func (f *fakeLaunchdService) Print(_ context.Context, label string) (launchd.ServiceInfo, error) {
	if f.Services == nil {
		return launchd.ServiceInfo{Label: label}, nil
	}
	info, ok := f.Services[label]
	if !ok {
		return launchd.ServiceInfo{Label: label}, nil
	}
	return info, nil
}
func (f *fakeLaunchdService) List(_ context.Context) ([]launchd.ServiceInfo, error) {
	return f.ListResult, nil
}

type fakePortAllocator struct {
	Allocated []int
	Returns   map[int]int // preferred → returned
}

func (f *fakePortAllocator) Free(preferred int) (int, error) {
	out := preferred
	if v, ok := f.Returns[preferred]; ok {
		out = v
	}
	f.Allocated = append(f.Allocated, out)
	return out, nil
}

type fakeProcInspector struct {
	RSSByPID    map[int]int64
	UptimeByPID map[int]time.Duration
}

func (f *fakeProcInspector) RSS(pid int) (int64, error) {
	if v, ok := f.RSSByPID[pid]; ok {
		return v, nil
	}
	return 0, nil
}
func (f *fakeProcInspector) Uptime(pid int) (time.Duration, error) {
	if v, ok := f.UptimeByPID[pid]; ok {
		return v, nil
	}
	return 0, nil
}

type fakeTokRateReader struct {
	RateByPath map[string]float64
}

func (f *fakeTokRateReader) Rate(logPath string, _ time.Duration, _ time.Time) (float64, error) {
	return f.RateByPath[logPath], nil
}
```

Also add these imports to the top of `fakes_test.go` if not already present:
```go
"github.com/gregmundy/llamactl/internal/launchd"
```

- [ ] **Step 2: Write the failing test `internal/cli/serve_test.go`**

```go
package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	return server.Resolution{Path: f.Path, Source: "test"}, nil
}

type fakeProberPhase3 struct{ Version server.Version }

func (f fakeProberPhase3) Probe(_ context.Context, _ string) (server.Version, error) {
	return f.Version, nil
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
```

- [ ] **Step 3: Run, confirm fail**

```bash
go test ./internal/cli/... -run TestServe
```
Expected: `unknown command "serve"`.

- [ ] **Step 4: Write `internal/cli/serve.go`**

```go
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gregmundy/llamactl/internal/launchd"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/recipes"
	"github.com/spf13/cobra"
)

const detachPollInterval = 250 * time.Millisecond
const detachPollDeadline = 5 * time.Second

func newServeCmd(d *Deps) *cobra.Command {
	var port int
	var recipe string
	var detach bool
	cmd := &cobra.Command{
		Use:   "serve <model-id>",
		Short: "Start llama-server for an installed model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd.Context(), d, args[0], port, recipe, detach)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "TCP port for the OpenAI-compatible endpoint")
	cmd.Flags().StringVar(&recipe, "recipe", recipes.DefaultRecipe, "chat | code | long-context | low-memory")
	cmd.Flags().BoolVar(&detach, "detach", false, "register a launchd LaunchAgent and return")
	return cmd
}

func runServe(ctx context.Context, d *Deps, id string, requestedPort int, recipeName string, detach bool) error {
	meta, err := d.ModelStore.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("%w: model %q is not installed; run `llamactl add %s`", ErrUserError, id, id)
	}

	hw, err := ensureHardware(ctx, d)
	if err != nil {
		return err
	}

	resolution, err := d.ServerResolver.Resolve(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUserError, err)
	}
	ver, err := d.ServerProber.Probe(ctx, resolution.Path)
	if err != nil {
		return fmt.Errorf("probe llama-server: %w", err)
	}
	if ver.Build < MinLlamaServerBuild {
		return fmt.Errorf("%w: llama-server at %s (build %d) below minimum supported build %d",
			ErrUserError, resolution.Path, ver.Build, MinLlamaServerBuild)
	}

	recipe, ok := recipes.Recipes[recipeName]
	if !ok {
		valid := make([]string, 0, len(recipes.Recipes))
		for k := range recipes.Recipes {
			valid = append(valid, k)
		}
		return fmt.Errorf("%w: unknown recipe %q (valid: %s)", ErrUserError, recipeName, strings.Join(valid, ", "))
	}

	model := models.Model{
		ID: meta.ID, HFRepo: meta.Repo, Arch: meta.Arch,
		ParamsB: meta.ParamsB, MaxCtx: lookupMaxCtx(meta),
	}
	sizeGB := float64(meta.SizeBytes) / (1 << 30)
	chosen, err := d.PortAllocator.Free(requestedPort)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUserError, err)
	}
	if chosen != requestedPort {
		fmt.Fprintf(d.Stderr, "bound to :%d (:%d was in use)\n", chosen, requestedPort)
	}

	argv := recipes.FlagsFor(recipe, model, meta.Quant, meta.GGUFPath, hw, ver, sizeGB, chosen)

	// Update metadata.LastServedAt before launching. If launch fails the
	// timestamp is slightly inaccurate; acceptable for v1.
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	meta.LastServedAt = now()
	if err := d.ModelStore.Put(ctx, meta); err != nil {
		fmt.Fprintf(d.Stderr, "llamactl: warning: could not persist LastServedAt: %v\n", err)
	}

	if detach {
		return runServeDetached(ctx, d, meta.ID, resolution.Path, argv, chosen, recipe.Name)
	}
	return runServeForeground(ctx, d, meta.ID, resolution.Path, argv, chosen, recipe.Name)
}

// lookupMaxCtx returns Model.MaxCtx if the model is in PreferredIDs,
// else falls back to 0 (which recipes.FlagsFor treats as "no cap").
// We don't put MaxCtx into Metadata, so this lookup is best-effort.
func lookupMaxCtx(meta models.Metadata) int {
	if m, ok := models.PreferredIDs[meta.ID]; ok {
		return m.MaxCtx
	}
	return 0
}

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
	cmd.Stdout = io.MultiWriter(logFile, d.Stdout)
	cmd.Stderr = io.MultiWriter(logFile, d.Stderr)
	return cmd.Run()
}

func runServeDetached(ctx context.Context, d *Deps, id, llamaServer string, argv []string, port int, recipeName string) error {
	label := "com.llamactl." + id
	if err := os.MkdirAll(d.LaunchAgentsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir LaunchAgents: %w", err)
	}
	if err := os.MkdirAll(d.LogsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir Logs: %w", err)
	}
	logPath := filepath.Join(d.LogsDir, id+".log")
	plistPath := filepath.Join(d.LaunchAgentsDir, label+".plist")

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	spec := launchd.PlistSpec{
		Label:       label,
		LlamaServer: llamaServer,
		Args:        argv,
		LogPath:     logPath,
		WorkingDir:  home,
	}
	body, err := launchd.Render(spec)
	if err != nil {
		return fmt.Errorf("render plist: %w", err)
	}
	tmp := plistPath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("write plist tmp: %w", err)
	}
	if err := os.Rename(tmp, plistPath); err != nil {
		return fmt.Errorf("rename plist: %w", err)
	}

	// If an old instance is loaded, bootout first.
	if existing, _ := d.LaunchdService.Print(ctx, label); existing.PID != 0 {
		_ = d.LaunchdService.Bootout(ctx, label)
	}

	if err := d.LaunchdService.Load(ctx, plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}

	// Poll for PID up to detachPollDeadline.
	deadline := time.Now().Add(detachPollDeadline)
	if d.Now != nil {
		deadline = d.Now().Add(detachPollDeadline)
	}
	for {
		info, _ := d.LaunchdService.Print(ctx, label)
		if info.PID > 0 {
			fmt.Fprintf(d.Stdout, "service %s started (pid=%d, recipe=%s); endpoint http://localhost:%d\n",
				id, info.PID, recipeName, port)
			return nil
		}
		var nowT time.Time
		if d.Now != nil {
			nowT = d.Now()
		} else {
			nowT = time.Now()
		}
		if nowT.After(deadline) {
			return fmt.Errorf("%w: service didn't start within %s; see %s",
				ErrUserError, detachPollDeadline, logPath)
		}
		time.Sleep(detachPollInterval)
	}
}
```

- [ ] **Step 5: Register `newServeCmd(d)` in `internal/cli/root.go`**

Append to the existing `AddCommand` chain in `NewRoot`:
```go
root.AddCommand(newServeCmd(d))
```

- [ ] **Step 6: Run, confirm pass**

```bash
go test ./internal/cli/... -run TestServe -v
go test ./...
go vet ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/cli/serve.go internal/cli/serve_test.go internal/cli/fakes_test.go internal/cli/root.go
git commit -m "feat(cli): serve command — foreground and --detach (launchd)"
```

---

## Task 11: `cli/stop`

**Files:**
- Create: `internal/cli/stop.go`
- Create: `internal/cli/stop_test.go`
- Modify: `internal/cli/root.go` (register `newStopCmd`)

- [ ] **Step 1: Write the failing test**

Create `internal/cli/stop_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/launchd"
)

func TestStopUnknownModel(t *testing.T) {
	d := &Deps{
		LaunchdService:  &fakeLaunchdService{},
		LaunchAgentsDir: t.TempDir(),
	}
	_, _, err := runRoot(t, d, "stop", "nope")
	if err == nil || !strings.Contains(err.Error(), "no detached service") {
		t.Fatalf("err = %v, want 'no detached service'", err)
	}
}

func TestStopOneRemovesPlist(t *testing.T) {
	tmp := t.TempDir()
	label := "com.llamactl.qwen"
	plistPath := filepath.Join(tmp, label+".plist")
	if err := os.WriteFile(plistPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ld := &fakeLaunchdService{}
	d := &Deps{LaunchdService: ld, LaunchAgentsDir: tmp}
	_, _, err := runRoot(t, d, "stop", "qwen")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ld.Booted) != 1 || ld.Booted[0] != label {
		t.Errorf("Bootout calls = %v, want [%q]", ld.Booted, label)
	}
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Errorf("plist should be deleted; got err=%v", err)
	}
}

func TestStopAllIteratesServices(t *testing.T) {
	tmp := t.TempDir()
	for _, label := range []string{
		"com.llamactl.qwen2.5-7b-instruct",
		"com.llamactl.mistral-7b-v0.3",
	} {
		_ = os.WriteFile(filepath.Join(tmp, label+".plist"), []byte("x"), 0o644)
	}
	ld := &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{
			{Label: "com.llamactl.qwen2.5-7b-instruct", PlistPath: filepath.Join(tmp, "com.llamactl.qwen2.5-7b-instruct.plist")},
			{Label: "com.llamactl.mistral-7b-v0.3", PlistPath: filepath.Join(tmp, "com.llamactl.mistral-7b-v0.3.plist")},
		},
	}
	d := &Deps{LaunchdService: ld, LaunchAgentsDir: tmp}
	_, _, err := runRoot(t, d, "stop")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ld.Booted) != 2 {
		t.Errorf("Bootout calls = %d, want 2", len(ld.Booted))
	}
}

func TestStopAllEmpty(t *testing.T) {
	d := &Deps{LaunchdService: &fakeLaunchdService{}, LaunchAgentsDir: t.TempDir()}
	out, _, err := runRoot(t, d, "stop")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "no llamactl services") {
		t.Errorf("out = %q, want 'no llamactl services'", out)
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/cli/... -run TestStop
```

- [ ] **Step 3: Write `internal/cli/stop.go`**

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newStopCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "stop [<model-id>]",
		Short: "Stop a detached llamactl service (or all services if no id)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return runStopAll(cmd.Context(), d)
			}
			return runStopOne(cmd.Context(), d, args[0])
		},
	}
}

func runStopAll(ctx context.Context, d *Deps) error {
	services, err := d.LaunchdService.List(ctx)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		fmt.Fprintln(d.Stdout, "no llamactl services running")
		return nil
	}
	for _, svc := range services {
		id := strings.TrimPrefix(svc.Label, "com.llamactl.")
		if err := stopOne(ctx, d, svc.Label, svc.PlistPath, id); err != nil {
			fmt.Fprintf(d.Stderr, "llamactl: warning: stop %s: %v\n", id, err)
		}
	}
	return nil
}

func runStopOne(ctx context.Context, d *Deps, id string) error {
	label := "com.llamactl." + id
	plistPath := filepath.Join(d.LaunchAgentsDir, label+".plist")
	if _, err := os.Stat(plistPath); err != nil {
		return fmt.Errorf("%w: no detached service for %q (looked at %s)", ErrUserError, id, plistPath)
	}
	return stopOne(ctx, d, label, plistPath, id)
}

func stopOne(ctx context.Context, d *Deps, label, plistPath, id string) error {
	_ = d.LaunchdService.Bootout(ctx, label) // best-effort
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	fmt.Fprintf(d.Stdout, "stopped %s and removed %s\n", id, plistPath)
	return nil
}
```

- [ ] **Step 4: Register in `internal/cli/root.go`**

```go
root.AddCommand(newStopCmd(d))
```

- [ ] **Step 5: Run, confirm pass**

```bash
go test ./internal/cli/... -run TestStop -v
go test ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/cli/stop.go internal/cli/stop_test.go internal/cli/root.go
git commit -m "feat(cli): stop command (bootout + delete plist)"
```

---

## Task 12: `cli/status`

**Files:**
- Create: `internal/cli/status.go`
- Create: `internal/cli/status_test.go`
- Modify: `internal/cli/root.go` (register `newStatusCmd`)

- [ ] **Step 1: Write the failing test**

Create `internal/cli/status_test.go`:

```go
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/launchd"
)

func TestStatusEmpty(t *testing.T) {
	d := &Deps{
		LaunchdService:  &fakeLaunchdService{ListResult: nil},
		LaunchAgentsDir: t.TempDir(),
	}
	out, _, err := runRoot(t, d, "status")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "no detached services") {
		t.Errorf("out = %q, want 'no detached services'", out)
	}
}

func writeMinimalPlist(t *testing.T, path string, port int) {
	t.Helper()
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/homebrew/bin/llama-server</string>
    <string>--port</string>
    <string>%d</string>
  </array>
</dict>
</plist>`, port)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStatusRunningService(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.qwen2.5-7b-instruct.plist")
	writeMinimalPlist(t, plistPath, 8080)

	logsDir := filepath.Join(tmp, "logs")
	d := &Deps{
		LaunchdService: &fakeLaunchdService{
			ListResult: []launchd.ServiceInfo{
				{Label: "com.llamactl.qwen2.5-7b-instruct", PlistPath: plistPath, PID: 12345, State: "running"},
			},
			Services: map[string]launchd.ServiceInfo{
				"com.llamactl.qwen2.5-7b-instruct": {Label: "com.llamactl.qwen2.5-7b-instruct", PID: 12345, State: "running"},
			},
		},
		ProcInspector: &fakeProcInspector{
			RSSByPID:    map[int]int64{12345: 4_000_000_000},
			UptimeByPID: map[int]time.Duration{12345: 3725 * time.Second},
		},
		TokRateReader:   &fakeTokRateReader{RateByPath: map[string]float64{filepath.Join(logsDir, "qwen2.5-7b-instruct.log"): 123.4}},
		LaunchAgentsDir: tmp,
		LogsDir:         logsDir,
	}
	out, _, err := runRoot(t, d, "status")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "qwen2.5-7b-instruct") {
		t.Errorf("out missing model id:\n%s", out)
	}
	if !strings.Contains(out, "8080") {
		t.Errorf("out missing port:\n%s", out)
	}
	if !strings.Contains(out, "123.4") {
		t.Errorf("out missing tok/s:\n%s", out)
	}
}

func TestStatusStoppedService(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.qwen.plist")
	writeMinimalPlist(t, plistPath, 8080)
	d := &Deps{
		LaunchdService: &fakeLaunchdService{
			ListResult: []launchd.ServiceInfo{
				{Label: "com.llamactl.qwen", PlistPath: plistPath, PID: 0, State: ""},
			},
		},
		LaunchAgentsDir: tmp,
	}
	out, _, err := runRoot(t, d, "status")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "stopped") {
		t.Errorf("out should show 'stopped':\n%s", out)
	}
}

func TestStatusJSONFormat(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.qwen.plist")
	writeMinimalPlist(t, plistPath, 8080)
	d := &Deps{
		LaunchdService: &fakeLaunchdService{
			ListResult: []launchd.ServiceInfo{
				{Label: "com.llamactl.qwen", PlistPath: plistPath, PID: 555, State: "running"},
			},
			Services: map[string]launchd.ServiceInfo{
				"com.llamactl.qwen": {Label: "com.llamactl.qwen", PID: 555, State: "running"},
			},
		},
		ProcInspector:   &fakeProcInspector{RSSByPID: map[int]int64{555: 1024}, UptimeByPID: map[int]time.Duration{555: time.Second}},
		TokRateReader:   &fakeTokRateReader{},
		LaunchAgentsDir: tmp,
		LogsDir:         tmp,
	}
	out, _, err := runRoot(t, d, "status", "--json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, `"model_id"`) || !strings.Contains(out, `"qwen"`) {
		t.Errorf("JSON out missing fields:\n%s", out)
	}
	if !strings.Contains(out, `"pid": 555`) {
		t.Errorf("JSON should contain pid:\n%s", out)
	}
}
```

**Note:** `writeMinimalPlist` is shared between `status_test.go` and `doctor_test.go` (T13 reuses it). Since both are in the `cli` package, define it once in `status_test.go` and reference it from `doctor_test.go` without redeclaring.

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/cli/... -run TestStatus
```

- [ ] **Step 3: Write `internal/cli/status.go`**

```go
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newStatusCmd(d *Deps) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "List detached llamactl services",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), d, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

type statusRow struct {
	ModelID       string  `json:"model_id"`
	Port          int     `json:"port"`
	State         string  `json:"state"`
	PID           int     `json:"pid,omitempty"`
	MemoryBytes   int64   `json:"memory_bytes,omitempty"`
	UptimeSeconds int64   `json:"uptime_seconds,omitempty"`
	TokensPerSec  float64 `json:"tokens_per_sec,omitempty"`
	Endpoint      string  `json:"endpoint,omitempty"`
}

var plistPortRe = regexp.MustCompile(`(?s)<string>--port</string>\s*<string>(\d+)</string>`)

func runStatus(ctx context.Context, d *Deps, asJSON bool) error {
	services, err := d.LaunchdService.List(ctx)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		if asJSON {
			fmt.Fprintln(d.Stdout, "[]")
		} else {
			fmt.Fprintln(d.Stdout, "no detached services")
		}
		return nil
	}

	rows := make([]statusRow, 0, len(services))
	for _, svc := range services {
		id := svc.Label
		if len(id) > len("com.llamactl.") && id[:len("com.llamactl.")] == "com.llamactl." {
			id = id[len("com.llamactl."):]
		}
		port := readPortFromPlist(svc.PlistPath)

		row := statusRow{ModelID: id, Port: port}
		info, _ := d.LaunchdService.Print(ctx, svc.Label)
		if info.PID == 0 {
			row.State = "stopped"
		} else {
			row.State = "running"
			row.PID = info.PID
			if rss, err := d.ProcInspector.RSS(info.PID); err == nil {
				row.MemoryBytes = rss
			}
			if up, err := d.ProcInspector.Uptime(info.PID); err == nil {
				row.UptimeSeconds = int64(up.Seconds())
			}
			logPath := d.LogsDir + "/" + id + ".log"
			rate, _ := d.TokRateReader.Rate(logPath, time.Minute, time.Now())
			row.TokensPerSec = rate
			if port > 0 {
				row.Endpoint = fmt.Sprintf("http://localhost:%d", port)
			}
		}
		rows = append(rows, row)
	}

	if asJSON {
		out, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(d.Stdout, string(out))
		return nil
	}

	tw := tabwriter.NewWriter(d.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL-ID\tPORT\tSTATE\tMEM\tUPTIME\tTOK/S\tENDPOINT")
	for _, r := range rows {
		mem := "—"
		upStr := "—"
		toks := "—"
		ep := "—"
		if r.State == "running" {
			mem = humanFileSize(r.MemoryBytes)
			upStr = humanDuration(time.Duration(r.UptimeSeconds) * time.Second)
			if r.TokensPerSec > 0 {
				toks = fmt.Sprintf("%.1f t/s", r.TokensPerSec)
			}
			ep = r.Endpoint
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%s\n",
			r.ModelID, r.Port, r.State, mem, upStr, toks, ep)
	}
	return tw.Flush()
}

func readPortFromPlist(plistPath string) int {
	data, err := os.ReadFile(plistPath)
	if errors.Is(err, fs.ErrNotExist) {
		return 0
	}
	if err != nil {
		return 0
	}
	m := plistPortRe.FindSubmatch(data)
	if m == nil {
		return 0
	}
	var n int
	_, _ = fmt.Sscanf(string(m[1]), "%d", &n)
	return n
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) - h*60
	return fmt.Sprintf("%dh%dm", h, m)
}
```

- [ ] **Step 4: Register in `internal/cli/root.go`**

```go
root.AddCommand(newStatusCmd(d))
```

- [ ] **Step 5: Run, confirm pass**

```bash
go test ./internal/cli/... -run TestStatus -v
go test ./...
go vet ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/cli/status.go internal/cli/status_test.go internal/cli/root.go
git commit -m "feat(cli): status command — table and --json output"
```

---

## Task 13: `cli/doctor` — 6 new checks

**Files:**
- Modify: `internal/cli/doctor.go`
- Modify: `internal/cli/doctor_test.go`

This task is a single commit because each check is small (~20 lines) and they share the same UI pattern. The implementer should add each check + at least one test (✓/✗) before moving to the next.

- [ ] **Step 1: Read existing `internal/cli/doctor.go`**

```bash
cat internal/cli/doctor.go | head -60
```

Note the existing `check func() (ok bool, msg string)` pattern. Reuse it.

- [ ] **Step 2: Append the 6 new check functions to `internal/cli/doctor.go`**

Add (preserve existing imports; add new ones as needed):

```go
import (
	// ... existing ...
	"net"
	"strconv"
	"syscall"
)

// ---- Phase 3 checks ----

func checkPortConflicts(ctx context.Context, d *Deps) (ok bool, msg string) {
	services, err := d.LaunchdService.List(ctx)
	if err != nil {
		return false, "could not list services: " + err.Error()
	}
	var problems []string
	for _, svc := range services {
		port := readPortFromPlist(svc.PlistPath)
		if port == 0 {
			continue
		}
		info, _ := d.LaunchdService.Print(ctx, svc.Label)
		if info.PID == 0 {
			continue // stopped services don't hold ports
		}
		// Service is loaded; the port should NOT be bindable.
		l, lerr := net.Listen("tcp", ":"+strconv.Itoa(port))
		if lerr == nil {
			_ = l.Close()
			id := svc.Label
			if len(id) > len("com.llamactl.") && id[:len("com.llamactl.")] == "com.llamactl." {
				id = id[len("com.llamactl."):]
			}
			problems = append(problems, fmt.Sprintf("%s loaded but port %d is free", id, port))
		}
	}
	if len(problems) > 0 {
		return false, strings.Join(problems, "; ")
	}
	return true, ""
}

func checkModelFilesMatchMetadata(ctx context.Context, d *Deps) (ok bool, msg string) {
	entries, err := d.ModelStore.List(ctx)
	if err != nil {
		return false, "list metadata: " + err.Error()
	}
	var problems []string
	for _, m := range entries {
		fi, err := d.FS.Stat(m.GGUFPath)
		if err != nil {
			continue // orphaned metadata is a separate check
		}
		size := fi.Size()
		// 1% tolerance
		if abs64(size-m.SizeBytes)*100 > m.SizeBytes {
			problems = append(problems,
				fmt.Sprintf("%s: on-disk size %d differs from metadata %d", m.ID, size, m.SizeBytes))
		}
	}
	if len(problems) > 0 {
		return false, strings.Join(problems, "; ") + "; re-add to repair"
	}
	return true, ""
}

func checkOrphanedMetadata(ctx context.Context, d *Deps) (ok bool, msg string) {
	entries, err := d.ModelStore.List(ctx)
	if err != nil {
		return false, "list metadata: " + err.Error()
	}
	var problems []string
	for _, m := range entries {
		if _, err := d.FS.Stat(m.GGUFPath); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %s missing", m.ID, m.GGUFPath))
		}
	}
	if len(problems) > 0 {
		return false, strings.Join(problems, "; ") + "; run `llamactl remove <id>`"
	}
	return true, ""
}

func checkDiskSpace(d *Deps) (ok bool, msg string) {
	const minFreeGB = 5
	var stat syscall.Statfs_t
	if err := syscall.Statfs(d.SharedModelsDir, &stat); err != nil {
		return false, "statfs " + d.SharedModelsDir + ": " + err.Error()
	}
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	freeGB := freeBytes / (1 << 30)
	if freeGB < minFreeGB {
		return false, fmt.Sprintf("only %d GiB free in %s; need at least %d GiB",
			freeGB, d.SharedModelsDir, minFreeGB)
	}
	return true, fmt.Sprintf("%d GiB free", freeGB)
}

func checkTailscale(ctx context.Context, d *Deps) (ok bool, msg string) {
	// Skip silently if tailscale isn't on PATH.
	if _, err := d.LookPath("tailscale"); err != nil {
		return true, "(not configured)"
	}
	var buf bytes.Buffer
	err := d.Runner.Run(ctx, "tailscale", []string{"status", "--json"}, "", &buf, io.Discard)
	if err != nil {
		return false, "tailscale status failed: " + err.Error() + "; run `tailscale up`"
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
		return false, "tailscale offline; run `tailscale up`"
	}
	return true, ""
}

func checkStalePlists(ctx context.Context, d *Deps) (ok bool, msg string) {
	services, err := d.LaunchdService.List(ctx)
	if err != nil {
		return false, "list services: " + err.Error()
	}
	var stale []string
	for _, svc := range services {
		data, err := os.ReadFile(svc.PlistPath)
		if err != nil {
			continue
		}
		// Match the first <string> inside <ProgramArguments> — that's the binary path.
		re := regexp.MustCompile(`<key>ProgramArguments</key>\s*<array>\s*<string>([^<]+)</string>`)
		m := re.FindSubmatch(data)
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
		return false, strings.Join(stale, "; ") + "; run `llamactl serve <id> --detach` to regenerate"
	}
	return true, ""
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
```

Then in the existing `runDoctor` function, append calls to these 6 checks alongside the Phase 1 checks. The implementer should look at the existing pattern (probably a slice of `struct{name string; fn func() (bool, string)}`) and add 6 entries.

For each: print `✓ <name>` if ok, `✗ <name>: <msg>` if not. Sum failures; exit 2 if any fail.

Two interface adjustments needed:
- `Deps.Runner` — Phase 1 already wires this for `tailscale`; verify it's accessible from doctor (it is — same package).
- `Deps.LookPath` — Phase 1 wired this; reused.
- `Deps.SharedModelsDir` — wired Phase 2; reused.

- [ ] **Step 3: Add a `makeDoctorDeps(t)` helper to `internal/cli/doctor_test.go`**

First, hoist the per-test Deps construction into a shared helper. Look at the existing doctor_test.go tests; the common Deps fields are: `HardwareDetector`, `HardwareJSONPath`, `ServerResolver`, `ServerProber`, `LookPath`, `Runner`, `Getenv`, plus the Phase 2 `ModelStore`/`FS`/`SharedModelsDir` and now Phase 3 `LaunchdService`/`PortAllocator`/`LaunchAgentsDir`. Provide sensible defaults in the helper; each test overrides the field(s) it needs.

```go
func makeDoctorDeps(t *testing.T) *Deps {
	t.Helper()
	tmp := t.TempDir()
	return &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{
			RAMBytes: 16 * (1 << 30), Chip: "Apple M2", MetalDeviceDetected: true,
		}},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		ServerResolver:   fakeResolverPhase3{Path: "/opt/homebrew/bin/llama-server"},
		ServerProber:     fakeProberPhase3{Version: server.Version{Build: 4500}},
		LookPath:         func(name string) (string, error) { return "", os.ErrNotExist },
		Runner:           &recordingRunner{},
		Getenv:           func(string) string { return "" },
		ModelStore:       newFakeModelStore(),
		FS:               OSFileSystem{},
		SharedModelsDir:  tmp,
		LaunchdService:   &fakeLaunchdService{},
		PortAllocator:    &fakePortAllocator{},
		LaunchAgentsDir:  tmp,
	}
}

// recordingRunner is a no-op runner that returns nil for every command.
// Doctor tests that need specific tailscale output override this.
type recordingRunner struct{}

func (r *recordingRunner) Run(_ context.Context, _ string, _ []string, _ string, _, _ io.Writer) error {
	return nil
}
```

- [ ] **Step 4: Append 6 test pairs (12 functions total)**

For each new check, write one `_OK` and one `_Failure` test. Here are all 6 templates — the implementer fills them in verbatim:

```go
func TestDoctor_PortConflicts_OK(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.x.plist")
	writeMinimalPlist(t, plistPath, 18080)
	// Real listener holds the port — bind will fail, which is healthy.
	l, _ := net.Listen("tcp", ":18080")
	defer l.Close()

	d := makeDoctorDeps(t)
	d.LaunchAgentsDir = tmp
	d.LaunchdService = &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{{Label: "com.llamactl.x", PlistPath: plistPath, PID: 12345, State: "running"}},
		Services:   map[string]launchd.ServiceInfo{"com.llamactl.x": {Label: "com.llamactl.x", PID: 12345, State: "running"}},
	}
	out, _, err := runRoot(t, d, "doctor")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.Contains(out, "✗ port conflicts") {
		t.Errorf("port-conflicts should pass:\n%s", out)
	}
}

func TestDoctor_PortConflicts_Failure(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.x.plist")
	// Pick a port that's certainly free (kernel-assigned), claim it via plist
	// as if loaded, but DON'T bind. The check should fail (loaded but port free).
	l, _ := net.Listen("tcp", ":0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	writeMinimalPlist(t, plistPath, port)

	d := makeDoctorDeps(t)
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

func TestDoctor_ModelFiles_OK(t *testing.T) {
	tmp := t.TempDir()
	gguf := filepath.Join(tmp, "model.gguf")
	if err := os.WriteFile(gguf, []byte(strings.Repeat("x", 1000)), 0o644); err != nil {
		t.Fatal(err)
	}
	d := makeDoctorDeps(t)
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID: "x", GGUFPath: gguf, SizeBytes: 1000,
	})
	d.ModelStore = store
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ model files match metadata") {
		t.Errorf("should pass:\n%s", out)
	}
}

func TestDoctor_ModelFiles_Failure(t *testing.T) {
	tmp := t.TempDir()
	gguf := filepath.Join(tmp, "model.gguf")
	_ = os.WriteFile(gguf, []byte("xxxx"), 0o644) // 4 bytes
	d := makeDoctorDeps(t)
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID: "x", GGUFPath: gguf, SizeBytes: 10_000_000, // wildly different
	})
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
	d := makeDoctorDeps(t)
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{ID: "x", GGUFPath: gguf, SizeBytes: 3})
	d.ModelStore = store
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ orphaned metadata") {
		t.Errorf("should pass:\n%s", out)
	}
}

func TestDoctor_OrphanedMetadata_Failure(t *testing.T) {
	d := makeDoctorDeps(t)
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID: "x", GGUFPath: "/no/such/file.gguf", SizeBytes: 3,
	})
	d.ModelStore = store
	out, _, _ := runRoot(t, d, "doctor")
	if !strings.Contains(out, "✗ orphaned metadata") {
		t.Errorf("expected failure:\n%s", out)
	}
}

func TestDoctor_DiskSpace_OK(t *testing.T) {
	d := makeDoctorDeps(t) // tmp dir has plenty of space
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ disk space") {
		t.Errorf("should pass:\n%s", out)
	}
}

func TestDoctor_DiskSpace_Failure(t *testing.T) {
	d := makeDoctorDeps(t)
	d.SharedModelsDir = "/no/such/path/at/all"
	out, _, _ := runRoot(t, d, "doctor")
	if !strings.Contains(out, "✗ disk space") {
		t.Errorf("expected failure (statfs on missing path):\n%s", out)
	}
}

// tailscaleRunner returns a canned tailscale status response.
type tailscaleRunner struct {
	jsonOutput string
	err        error
}

func (r *tailscaleRunner) Run(_ context.Context, name string, _ []string, _ string, stdout, _ io.Writer) error {
	if r.err != nil {
		return r.err
	}
	if name == "tailscale" {
		_, _ = io.WriteString(stdout, r.jsonOutput)
	}
	return nil
}

func TestDoctor_Tailscale_NotConfigured_Skipped(t *testing.T) {
	d := makeDoctorDeps(t) // LookPath returns err for tailscale → skip silently
	out, _, _ := runRoot(t, d, "doctor")
	// Skipped should still print a ✓ line with "(not configured)".
	if !strings.Contains(out, "✓ tailscale") {
		t.Errorf("expected ✓ tailscale (skipped):\n%s", out)
	}
}

func TestDoctor_Tailscale_Online_OK(t *testing.T) {
	d := makeDoctorDeps(t)
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
	d := makeDoctorDeps(t)
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

	d := makeDoctorDeps(t)
	d.LaunchAgentsDir = tmp
	d.LaunchdService = &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{{Label: "com.llamactl.x", PlistPath: plistPath}},
	}
	out, _, _ := runRoot(t, d, "doctor")
	if strings.Contains(out, "✗ stale plists") {
		t.Errorf("should pass:\n%s", out)
	}
}

func TestDoctor_StalePlists_Failure(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.x.plist")
	plistBody := `<plist><dict>
<key>ProgramArguments</key><array><string>/no/such/llama-server</string></array>
</dict></plist>`
	_ = os.WriteFile(plistPath, []byte(plistBody), 0o644)

	d := makeDoctorDeps(t)
	d.LaunchAgentsDir = tmp
	d.LaunchdService = &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{{Label: "com.llamactl.x", PlistPath: plistPath}},
	}
	out, _, _ := runRoot(t, d, "doctor")
	if !strings.Contains(out, "✗ stale plists") {
		t.Errorf("expected failure:\n%s", out)
	}
}
```

- [ ] **Step 5: Build + test**

```bash
go test ./internal/cli/... -run TestDoctor -v
go test ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/cli/doctor.go internal/cli/doctor_test.go
git commit -m "feat(doctor): port conflicts, file/metadata match, orphaned metadata, disk, Tailscale, stale plists"
```

---

## Task 14: `cli/list` — LAST-SERVED column

**Files:**
- Modify: `internal/cli/list.go`
- Modify: `internal/cli/list_test.go`

- [ ] **Step 1: Add failing test**

Append to `internal/cli/list_test.go`:

```go
func TestListShowsLastServedAt(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "new.gguf")
	if err := os.WriteFile(existing, []byte("xxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID: "qwen2.5-7b-instruct", Quant: models.Q4_K_M, GGUFPath: existing, SizeBytes: 3,
		AddedAt:      time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		LastServedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	})
	d := &Deps{ModelStore: store, FS: OSFileSystem{}}
	out, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "LAST-SERVED") {
		t.Errorf("output missing LAST-SERVED header:\n%s", out)
	}
	if !strings.Contains(out, "2026-05-11") {
		t.Errorf("output missing last-served date:\n%s", out)
	}
}
```

- [ ] **Step 2: Modify `internal/cli/list.go`**

Replace the `runList` function. Add LAST-SERVED column after ADDED. Blank for zero time:

```go
func runList(ctx context.Context, d *Deps) error {
	entries, err := d.ModelStore.List(ctx)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintln(d.Stdout, "no models installed")
		return nil
	}
	tw := tabwriter.NewWriter(d.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL-ID\tQUANT\tPARAMS\tSIZE\tPATH\tADDED\tLAST-SERVED")
	for _, m := range entries {
		size := humanFileSize(m.SizeBytes)
		fi, statErr := d.FS.Stat(m.GGUFPath)
		switch {
		case statErr == nil:
			size = humanFileSize(fi.Size())
		case errors.Is(statErr, fs.ErrNotExist):
			size = "(missing)"
		default:
			size = "(stat err)"
		}
		params := ""
		if m.ParamsB > 0 {
			params = fmt.Sprintf("%dB", m.ParamsB)
		}
		lastServed := ""
		if !m.LastServedAt.IsZero() {
			lastServed = m.LastServedAt.Format("2006-01-02")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			m.ID, m.Quant, params, size, m.GGUFPath, m.AddedAt.Format("2006-01-02"), lastServed)
	}
	return tw.Flush()
}
```

- [ ] **Step 3: Run, confirm pass**

```bash
go test ./internal/cli/... -run TestList -v
go test ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/cli/list.go internal/cli/list_test.go
git commit -m "feat(cli): list — LAST-SERVED column"
```

---

## Task 15: Wire concrete adapters in `cmd/llamactl/main.go`

**Files:**
- Modify: `cmd/llamactl/main.go`

- [ ] **Step 1: Read current main.go**

```bash
cat cmd/llamactl/main.go
```

- [ ] **Step 2: Add Phase 3 wiring**

After the existing Phase 1+2 `deps` setup, append:

```go
// Phase 3 wiring.
launchAgentsDir := filepath.Join(paths.Home, "Library", "LaunchAgents")
logsDir := filepath.Join(paths.Home, "Library", "Logs", "llamactl")

launchdSvc := &launchd.Service{Runner: run, UID: os.Getuid()}
deps.LaunchdService = &cli.LaunchdServiceAdapter{Service: launchdSvc, AgentsDir: launchAgentsDir}
deps.PortAllocator = proc.Allocator{}
deps.ProcInspector = &proc.Inspector{Runner: run}
deps.TokRateReader = &proc.TailRate{}
deps.Runner = run
deps.LaunchAgentsDir = launchAgentsDir
deps.LogsDir = logsDir
```

Add imports:

```go
"github.com/gregmundy/llamactl/internal/launchd"
"github.com/gregmundy/llamactl/internal/proc"
```

(`filepath` and `os` should already be imported in main.go.)

- [ ] **Step 3: Build + smoke test**

```bash
go build ./cmd/llamactl
./llamactl --help
```
Expected: shows `serve`, `stop`, `status` alongside the Phase 1+2 commands.

- [ ] **Step 4: Run full suite**

```bash
go test ./...
go vet ./...
```

- [ ] **Step 5: Commit**

```bash
git add cmd/llamactl/main.go
git commit -m "feat(cli): wire launchd, proc, port adapters in main"
```

---

## Task 16: Integration tests (foreground + detached)

**Files:**
- Modify: `internal/cli/integration_test.go`
- Create: `internal/cli/testdata/fakellamaserver/main.go`

The foreground test needs a real binary that behaves like llama-server. We compile a tiny Go program at test time.

- [ ] **Step 1: Create the fake llama-server binary**

`internal/cli/testdata/fakellamaserver/main.go`:

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
	// Honor --version probe.
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

- [ ] **Step 2: Append the integration test**

Add to `internal/cli/integration_test.go`:

```go
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
		ID: "qwen2.5-3b-instruct", Quant: models.Q4_K_M,
		Repo: "Qwen/Qwen2.5-3B-Instruct-GGUF",
		GGUFPath:  filepath.Join(tmp, "model.gguf"),
		SizeBytes: 1_900_000_000, ParamsB: 3, Arch: models.ArchQwen25,
		AddedAt: fakeNow(),
	})
	_ = os.WriteFile(filepath.Join(tmp, "model.gguf"), []byte("xxx"), 0o644)

	// Fake llama-server is built but we don't need to run it — Phase 3
	// detached path only writes a plist and shells out to launchctl
	// (which is faked via runner.CommandRunner). Resolver returns the
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
```

Add required imports to the top of `integration_test.go` (preserve existing): `os/exec`, `bytes`.

- [ ] **Step 3: Run, confirm pass**

```bash
go test ./internal/cli/... -run TestIntegrationPhase3 -v
go test ./...
go vet ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/cli/integration_test.go internal/cli/testdata/fakellamaserver/main.go
git commit -m "test(cli): integration test for detached serve→status→stop"
```

---

## Final verification

- [ ] **All tests pass**

```bash
go test ./...
go vet ./...
```

- [ ] **Binary smoke test (no network, no real launchd)**

```bash
go build -o llamactl ./cmd/llamactl
./llamactl --help                         # should list serve/stop/status
./llamactl serve nonexistent              # should error with 'is not installed'
./llamactl stop                           # should print 'no llamactl services running'
./llamactl status                         # should print 'no detached services'
```

- [ ] **Branch state**

```bash
git log main..phase3-serve --oneline
git diff main..phase3-serve --stat
```

Expected: ~16 commits, ~3500 lines added.

---

## Notes for the executing agent

1. **Branch:** All work on `phase3-serve`. Never `git checkout main`, never stash, never branch.
2. **Spec is authoritative:** `docs/superpowers/specs/2026-05-11-phase3-serve-launchd-status-design.md`.
3. **Per-task verification:** After each task, run that task's tests AND the full suite.
4. **Two-stage review:** Substantive tasks get dispatched spec+quality review (Tasks 2, 4, 7, 8, 10, 12, 13, 16). Trivial tasks verified by direct file read (1, 3, 5, 6, 9, 11, 14, 15).
5. **Type drift watch:** `LaunchdService`, `PortAllocator`, `ProcInspector`, `TokRateReader` are interfaces in `cli`; concretes are in their packages. Don't conflate. `launchd.ServiceInfo` (concrete type) is what the interface returns.
6. **Phase 1 references:**
   - `ErrUserError` in `internal/cli/deps.go`
   - `ensureHardware` helper in `internal/cli/add.go` (same package — direct call)
   - `runner.CommandRunner` interface in `internal/runner` — leaf packages redeclare it locally.
   - `server.Resolution`, `server.Version` in `internal/server`.
   - `platform.Cores()` in `internal/platform`.
7. **Live smoke test (post-merge):** see spec §10.3. NOT part of the plan — done manually after merge to main.
