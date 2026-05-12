# Phase 5: `fit` command + backlog drain — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the `fit` discovery+sizing command and drain 19 backlog items (real bugs, hygiene, README polish, minor refactors) as v1.2.0.

**Architecture:** Single feature branch `phase5-fit-and-fixes` off `main`. Mostly targeted edits to existing files; new code in `internal/cli/fit.go`, `internal/cli/cache.go`, `internal/cli/logrotate.go`, `internal/models/paramcount.go`, `internal/gguftest/`, `internal/testutil/`. No new external dependencies. GoReleaser → cask → `brew upgrade` distribution path unchanged from Phase 4.

**Tech Stack:** Go 1.26.2, cobra, stdlib `text/template`/`encoding/binary`/`regexp`, GoReleaser, Homebrew tap.

**Reference:** `docs/superpowers/specs/2026-05-12-phase5-fit-and-fixes-design.md` (the approved spec — read alongside this plan).

---

## How to use this plan

- All 22 tasks land on a single feature branch `phase5-fit-and-fixes`. **Implementers must stay on this branch — do not `git checkout`, `git switch`, or `git stash` for any reason.**
- Each task = one commit. Run `go test ./... -race` after each task; commit only when green.
- Tasks 1, 2, 12, 13, 14, 18 are substantial (multi-file, behavior change). Tasks 3–9, 15, 16 are small (a few lines each). Task 17 is docs-only. Task 19 is README. Tasks 20–22 are release mechanics + memory.
- Spec section references like "(spec §4.2)" point to the approved spec — read for *why*. This plan is the *how*.

---

## File structure overview

**New files:**
- `internal/gguftest/builder.go` + `_test.go` (Task 1)
- `internal/testutil/fakerunner.go` + `_test.go` (Task 14)
- `internal/cli/logrotate.go` + `_test.go` (Task 12)
- `internal/cli/cache.go` + `_test.go` (Task 13)
- `internal/models/paramcount.go` + `_test.go` (Task 18)
- `internal/cli/fit.go` + `_test.go` (Task 18)

**Modified files (high-touch):**
- `internal/gguf/header.go` (Task 2, GGUF parser fix)
- `internal/cli/serve.go` (Tasks 7, 11, 12 — port-0 msg, ctx cancel, log rotate)
- `internal/cli/doctor.go` (Tasks 9, 12, 13 — remediation, two new checks)
- `internal/cli/add.go` (Tasks 4, 5 — dedupe message, remove double-SHA)
- `internal/download/download.go` (Tasks 5, 6 — verify ownership, flock log)
- `internal/server/resolver.go` (Task 10 — memoize)
- `internal/recipes/recipes.go` (Task 15 — cores parameterization)
- `internal/runner/runner.go` + `internal/launchd/service.go` + `internal/proc/ps.go` (Task 16 — stdin→dir rename)
- `internal/cli/root.go` (Task 13, 18 — register `cache` and `fit` parent commands)
- `internal/hf/cache.go` (Task 13 — auto-prune)
- `internal/cli/status.go` (Task 3 — TrimPrefix)
- `internal/proc/ps.go` (Task 8 — parseEtime error wrap)
- `README.md` (Task 19)
- `docs/llamactl-prd-v1.5.md` (Task 17 — doc-only)

---

## Task 1: Extract `internal/gguftest` builder

**Spec:** §7.2.

**Files:**
- Create: `internal/gguftest/builder.go`
- Create: `internal/gguftest/builder_test.go`
- Modify: `internal/gguf/header_test.go` (replace local `buildGGUF`)
- Modify: `internal/cli/add_test.go` (replace local `buildGGUF`)
- Modify: `internal/cli/integration_test.go` (replace local helper)

This must land before Task 2 because the GGUF parser fix wants new fixtures using the helper.

- [ ] **Step 1: Inspect the three existing helpers**

```bash
grep -n "buildGGUF\|func writeGGUF" internal/gguf/header_test.go internal/cli/add_test.go internal/cli/integration_test.go
```

Read each. Identify the union of features needed (KV types, version, byte layout). The new API must cover all three callsites.

- [ ] **Step 2: Write the failing test for `gguftest.Build`**

Create `internal/gguftest/builder_test.go`:

```go
package gguftest

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildMinimal(t *testing.T) {
	b := Build(t, 3,
		KV{Key: "general.architecture", Type: TypeString, Value: "llama"},
		KV{Key: "general.parameter_count", Type: TypeU64, Value: uint64(7_000_000_000)},
	)
	if !bytes.HasPrefix(b, []byte("GGUF")) {
		t.Fatalf("missing magic: %x", b[:8])
	}
	// Version at bytes 4..8
	ver := binary.LittleEndian.Uint32(b[4:8])
	if ver != 3 {
		t.Fatalf("version=%d, want 3", ver)
	}
}

func TestBuildArrayKV(t *testing.T) {
	// Tokenizer-like array; the gguf parser must skip past these.
	b := Build(t, 3,
		KV{Key: "tokenizer.ggml.tokens", Type: TypeArray, Value: ArrayValue{ElemType: TypeString, Items: []any{"a", "b"}}},
		KV{Key: "general.architecture", Type: TypeString, Value: "llama"},
	)
	if len(b) < 8 {
		t.Fatalf("short output: %d", len(b))
	}
}
```

Run: `go test ./internal/gguftest/... -v`
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement `gguftest.Build`**

Create `internal/gguftest/builder.go`:

```go
// Package gguftest builds synthetic GGUF byte streams for unit tests.
// Mirrors the layout in internal/gguf/header.go: magic("GGUF") + version(u32)
// + tensor_count(u64) + kv_count(u64) + N×{key_string, type_u32, value}.
package gguftest

import (
	"bytes"
	"encoding/binary"
	"testing"
)

const (
	TypeU8     uint32 = 0
	TypeI8     uint32 = 1
	TypeU16    uint32 = 2
	TypeI16    uint32 = 3
	TypeU32    uint32 = 4
	TypeI32    uint32 = 5
	TypeF32    uint32 = 6
	TypeBool   uint32 = 7
	TypeString uint32 = 8
	TypeArray  uint32 = 9
	TypeU64    uint32 = 10
	TypeI64    uint32 = 11
	TypeF64    uint32 = 12
)

type KV struct {
	Key   string
	Type  uint32
	Value any
}

// ArrayValue is the Value type to pass when Type == TypeArray.
type ArrayValue struct {
	ElemType uint32
	Items    []any
}

// Build returns synthetic GGUF bytes containing the given KV pairs.
// tensorCount is set to 0 (callers don't exercise tensor parsing).
func Build(t *testing.T, version uint32, kvs ...KV) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("GGUF")
	binary.Write(&buf, binary.LittleEndian, version)
	binary.Write(&buf, binary.LittleEndian, uint64(0)) // tensor_count
	binary.Write(&buf, binary.LittleEndian, uint64(len(kvs)))
	for _, kv := range kvs {
		writeString(&buf, kv.Key)
		binary.Write(&buf, binary.LittleEndian, kv.Type)
		if err := writeValue(&buf, kv.Type, kv.Value); err != nil {
			t.Fatalf("gguftest.Build: key=%q: %v", kv.Key, err)
		}
	}
	return buf.Bytes()
}

func writeString(w *bytes.Buffer, s string) {
	binary.Write(w, binary.LittleEndian, uint64(len(s)))
	w.WriteString(s)
}

func writeValue(w *bytes.Buffer, kind uint32, v any) error {
	switch kind {
	case TypeString:
		writeString(w, v.(string))
	case TypeU32:
		binary.Write(w, binary.LittleEndian, v.(uint32))
	case TypeU64:
		binary.Write(w, binary.LittleEndian, v.(uint64))
	case TypeI64:
		binary.Write(w, binary.LittleEndian, v.(int64))
	case TypeI32:
		binary.Write(w, binary.LittleEndian, v.(int32))
	case TypeBool:
		var b uint8
		if v.(bool) {
			b = 1
		}
		binary.Write(w, binary.LittleEndian, b)
	case TypeArray:
		av := v.(ArrayValue)
		binary.Write(w, binary.LittleEndian, av.ElemType)
		binary.Write(w, binary.LittleEndian, uint64(len(av.Items)))
		for _, item := range av.Items {
			if err := writeValue(w, av.ElemType, item); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("gguftest: unsupported type %d", kind)
	}
	return nil
}
```

(Add `"fmt"` to imports.)

- [ ] **Step 4: Run the test**

Run: `go test ./internal/gguftest/... -v`
Expected: PASS.

- [ ] **Step 5: Migrate `internal/gguf/header_test.go`**

Replace local `buildGGUF` calls with `gguftest.Build`. Add import `"github.com/gregmundy/llamactl/internal/gguftest"`. Run `go test ./internal/gguf/... -v` — must stay green.

- [ ] **Step 6: Migrate `internal/cli/add_test.go` and `integration_test.go`**

Same: replace local helpers with `gguftest.Build`. Run `go test ./internal/cli/... -v` — green.

- [ ] **Step 7: Run full suite**

Run: `go test ./... -race`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/gguftest/ internal/gguf/header_test.go internal/cli/add_test.go internal/cli/integration_test.go
git commit -m "refactor: extract internal/gguftest builder (consolidates 3 duplicate helpers)"
```

---

## Task 2: Fix GGUF parser `ParamsCount=0` bug

**Spec:** §4.1. This is an *investigation* task — fix may differ from the prediction.

**Files:**
- Modify: `internal/gguf/header.go`
- Modify: `internal/gguf/header_test.go` (use gguftest helpers from Task 1)

**Investigation hypothesis (likely):** `parseHeader` only checks `value.(uint64)` for `general.parameter_count` (header.go:124). GGUF spec allows the value to be stored as `uint32`, `int32`, `int64`, or `uint64`. Real-world files (Gemma 4 E4B, Qwen 2.5 3B) likely store it as `int64` (typeI64=11), which our type assertion rejects — silently leaving `h.ParamsCount = 0`.

- [ ] **Step 1: Confirm root cause**

Run `cmd/gguf-inspect` against a real GGUF and add tracing if needed:

```bash
go run ./cmd/gguf-inspect ~/.local/share/llama-models/qwen2.5-3b-instruct/Q5_K_M.gguf 2>&1 | head -40
```

If `ParamsCount=0` reproduces, write a one-off diagnostic that prints all keys + value types observed:

```bash
# Temporarily add to gguf-inspect's main or a scratch test:
# for _, e := range entries { fmt.Printf("%s -> %T\n", e.key, e.value) }
```

Look for `general.parameter_count -> int64` (or `uint32`/`int32`).

If reality matches the hypothesis: continue to Step 2. If not (e.g., key is missing entirely, or readLimit is exhausted): **STOP and ask the orchestrator** before continuing — the fix scope may differ from this plan.

- [ ] **Step 2: Write the failing unit test**

Add to `internal/gguf/header_test.go`:

```go
func TestParamsCountFromInt64(t *testing.T) {
	bytes := gguftest.Build(t, 3,
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "gemma3"},
		gguftest.KV{Key: "general.parameter_count", Type: gguftest.TypeI64, Value: int64(4_000_000_000)},
	)
	h, err := parseHeader(bytes.NewReader(bytes))
	if err != nil {
		t.Fatal(err)
	}
	if h.ParamsCount != 4_000_000_000 {
		t.Fatalf("ParamsCount=%d, want 4_000_000_000", h.ParamsCount)
	}
}

func TestParamsCountFromUint32(t *testing.T) {
	b := gguftest.Build(t, 3,
		gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "llama"},
		gguftest.KV{Key: "general.parameter_count", Type: gguftest.TypeU32, Value: uint32(3_000_000_000)},
	)
	h, err := parseHeader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if h.ParamsCount != 3_000_000_000 {
		t.Fatalf("ParamsCount=%d, want 3_000_000_000", h.ParamsCount)
	}
}
```

(Add the integer-type variants matching what the investigation in Step 1 actually found.)

Run: `go test ./internal/gguf/... -run TestParamsCount -v`
Expected: FAIL — current code only handles `uint64`.

- [ ] **Step 3: Implement the fix**

In `internal/gguf/header.go`, change the `general.parameter_count` handling (around line 124) to accept multiple integer types:

```go
case "general.parameter_count":
    switch v := value.(type) {
    case uint64:
        h.ParamsCount = v
    case int64:
        if v >= 0 {
            h.ParamsCount = uint64(v)
        }
    case uint32:
        h.ParamsCount = uint64(v)
    case int32:
        if v >= 0 {
            h.ParamsCount = uint64(v)
        }
    }
```

- [ ] **Step 4: Run unit tests**

Run: `go test ./internal/gguf/... -v`
Expected: PASS.

- [ ] **Step 5: Add real-file integration test (skippable)**

Append to `header_test.go`:

```go
func TestParseRealQwenFile(t *testing.T) {
	const path = "/Users/greg/.local/share/llama-models/qwen2.5-3b-instruct/Q5_K_M.gguf"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("real GGUF not present: %v", err)
	}
	h, err := ReadHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.ParamsCount < 2_700_000_000 || h.ParamsCount > 3_300_000_000 {
		t.Fatalf("ParamsCount=%d, want ~3e9 ±10%%", h.ParamsCount)
	}
}
```

(Add `"os"` import if needed.)

- [ ] **Step 6: Run full suite**

Run: `go test ./... -race`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/gguf/header.go internal/gguf/header_test.go
git commit -m "fix(gguf): parse general.parameter_count for int64/uint32/int32 value types"
```

---

## Task 3: `status.go` use `strings.TrimPrefix`

**Spec:** §4.8. Trivial.

**Files:**
- Modify: `internal/cli/status.go`

- [ ] **Step 1: Locate the open-coded strip**

```bash
grep -n 'com.llamactl.' internal/cli/status.go
```

- [ ] **Step 2: Replace with `strings.TrimPrefix`**

Before:
```go
id := svc.Label
if len(id) > len("com.llamactl.") && id[:len("com.llamactl.")] == "com.llamactl." {
    id = id[len("com.llamactl."):]
}
```

After:
```go
id := strings.TrimPrefix(svc.Label, "com.llamactl.")
```

Ensure `"strings"` is imported.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/cli/... -run TestStatus -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/status.go
git commit -m "refactor(status): replace open-coded prefix strip with strings.TrimPrefix"
```

---

## Task 4: `add` dedupe path no longer double-prints

**Spec:** §4.2.

**Files:**
- Modify: `internal/cli/add.go` (`finishAdd`)
- Modify: `internal/cli/add_test.go`

The current `finishAdd` prints `"already present"` in the dedupe branch, then unconditionally falls through to print `"installed %s"`. The fix: skip the "installed" line when dedupe hits.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/add_test.go`:

```go
func TestAddDedupeOnlyPrintsAlreadyPresent(t *testing.T) {
	// Set up a Deps where the destination file already exists with matching SHA.
	// Capture stdout. Run add. Assert "installed" is NOT in output, "already present" IS.
	// (Follow existing test fixture patterns in this file — fakeDownloader, tempDir, etc.)
}
```

(See existing `TestRunAdd*` patterns in the file; reuse the same `tempDir` + `fakeStore` + `fakeDownloader` scaffolding. Pre-populate the GGUF with content whose SHA matches what `findQuantFile` would return.)

Run: `go test ./internal/cli/... -run TestAddDedupe -v`
Expected: FAIL — output contains "installed ".

- [ ] **Step 2: Implement the fix**

In `internal/cli/add.go` `finishAdd`, refactor:

```go
alreadyPresent := false
if existing, _ := sha256OfFileIfExists(destPath); existing == expectedSHA {
    fmt.Fprintf(d.Stdout, "already present (matched SHA): %s\n", destPath)
    alreadyPresent = true
} else {
    req := download.Request{ /* ...unchanged... */ }
    if err := d.Downloader.Get(ctx, req); err != nil {
        return fmt.Errorf("download: %w", err)
    }
}

// ... GGUF header parsing, metadata Put, all unchanged ...

if !alreadyPresent {
    fmt.Fprintf(d.Stdout, "installed %s (%s, %s) -> %s\n",
        id, quant, humanFileSize(totalSize), destPath)
}
return nil
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/cli/... -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/add.go internal/cli/add_test.go
git commit -m "fix(add): suppress 'installed' message on dedupe path"
```

---

## Task 5: Remove double-SHA verify in `add.go`

**Spec:** §4.3.

**Files:**
- Modify: `internal/cli/add.go`
- Read: `internal/download/download.go` (confirm internal verify exists)

The current `add.go` computes the full SHA via `sha256OfFileIfExists` BEFORE calling `Downloader.Get`. `Downloader.Get` itself verifies the file's SHA post-write. The upper-layer call is pure waste — for a 5 GB GGUF it's ~30s of unnecessary I/O+hash on every re-`add` of an already-installed model.

The fix: keep the dedupe semantic but move ownership into the downloader. Simplest path: have `Downloader.Get` itself detect "already present with matching SHA" and skip the download (no-op). Then `add.go` removes its dedupe pre-check.

Read `internal/download/download.go` first — confirm whether `Get` already has this short-circuit or needs to be added.

- [ ] **Step 1: Read the downloader to determine fix shape**

```bash
sed -n '1,100p' internal/download/download.go
```

Two scenarios:
- **A)** Downloader already short-circuits when dest exists with matching SHA: just remove the upper-layer check in `add.go` and rely on Downloader.Get.
- **B)** Downloader does NOT short-circuit: add a fast pre-check inside `Get` that does `if sha256(destPath) == req.ExpectedSHA256 { return ErrAlreadyPresent (or nil with a flag) }`.

If scenario B, expose the "was already present" signal so `add.go` can still print "already present" vs "installed". Add a sentinel error or a `WasAlreadyPresent` field on `download.Request` populated by `Get`.

If the implementation goes beyond a 30-line change: **STOP and ask the orchestrator.**

- [ ] **Step 2: Write the failing test**

Add to `internal/cli/add_test.go` (or `internal/download/download_test.go` depending on Step 1 outcome):

```go
func TestAddDoesNotDoubleVerify(t *testing.T) {
    // Scenario: dest file pre-exists with correct SHA. Spy on sha256OfFileIfExists
    // or on a counter inside the test. Run add. Assert SHA is computed at most once.
}
```

For a more practical proxy: make the fake downloader record whether `Get` was called, and assert that when the file pre-exists, behavior matches Task 4's expectation (no "installed" line) AND the test's SHA-computation counter is 1, not 2.

- [ ] **Step 3: Implement**

Depending on scenario from Step 1, either:
- Remove `sha256OfFileIfExists` call from `finishAdd` and let `Downloader.Get` own dedupe; OR
- Add the short-circuit inside `Downloader.Get` and remove the upper-layer call.

Add a comment in `add.go` at the call site:

```go
// Dedupe/verify is owned by Downloader.Get — it short-circuits if the dest
// file already exists with the expected SHA-256. We don't pre-verify here.
```

- [ ] **Step 4: Run tests**

Run: `go test ./... -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/add.go internal/download/download.go internal/download/download_test.go internal/cli/add_test.go
git commit -m "fix(add): remove redundant pre-download SHA verify; downloader owns dedupe"
```

---

## Task 6: Log on flock contention

**Spec:** §4.4.

**Files:**
- Modify: `internal/download/download.go`
- Modify: `internal/download/download_test.go`

`Downloader.Get` blocks silently on flock when another llamactl instance holds the lock. The fix: try non-blocking first; on `EWOULDBLOCK`, emit one stderr line, then take the blocking lock.

- [ ] **Step 1: Find the existing flock call**

```bash
grep -n "flock\|syscall.LOCK_EX" internal/download/download.go
```

Identify the existing acquire pattern. It's likely `syscall.Flock(fd, LOCK_EX)`.

- [ ] **Step 2: Add `Stderr` field to Downloader if absent**

Inspect the Downloader struct. If there's no `Stderr io.Writer`, add one with a default of `os.Stderr`.

```go
type Downloader struct {
    // ...existing fields...
    Stderr io.Writer // optional; defaults to os.Stderr
}
```

In `Get`:
```go
stderr := d.Stderr
if stderr == nil {
    stderr = os.Stderr
}
```

- [ ] **Step 3: Write the failing test**

Add to `download_test.go`:

```go
func TestFlockContentionLogs(t *testing.T) {
    // Two goroutines call Get on the same dest path. First holds the lock for ~100ms.
    // Second's Stderr is a bytes.Buffer. Assert the buffer contains "waiting".
    // (The exact phrasing matches what we emit below.)
}
```

Run: FAIL — no waiting message today.

- [ ] **Step 4: Implement non-blocking-then-blocking flock**

```go
// Try non-blocking first; on contention, log once and fall back to blocking.
if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
    if errors.Is(err, syscall.EWOULDBLOCK) {
        fmt.Fprintf(stderr, "another llamactl instance is downloading %s; waiting…\n", req.RepoID)
        if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
            return fmt.Errorf("flock: %w", err)
        }
    } else {
        return fmt.Errorf("flock: %w", err)
    }
}
```

- [ ] **Step 5: Tests**

Run: `go test ./internal/download/... -race -v`
Expected: PASS, including the existing `TestDownloadFlockSerializesTwoCallers`.

- [ ] **Step 6: Commit**

```bash
git add internal/download/download.go internal/download/download_test.go
git commit -m "feat(download): log 'waiting…' when blocked on flock contention"
```

---

## Task 7: `--port 0` message wording

**Spec:** §4.5.

**Files:**
- Modify: `internal/cli/serve.go`
- Modify: `internal/cli/serve_test.go`

- [ ] **Step 1: Locate the message**

```bash
grep -n "was in use\|bound to" internal/cli/serve.go
```

- [ ] **Step 2: Write failing test**

In `serve_test.go`, add a test where `requestedPort == 0` and the allocator returns e.g. 51234. Capture stdout. Assert it says `"bound to ephemeral :51234"` and does NOT contain `":0 was in use"`.

- [ ] **Step 3: Implement**

Replace the current branching with:
```go
switch {
case requestedPort == 0:
    fmt.Fprintf(d.Stdout, "bound to ephemeral :%d\n", port)
case port != requestedPort:
    fmt.Fprintf(d.Stdout, "bound to :%d (:%d was in use)\n", port, requestedPort)
}
```

(Adapt the variable names to match what's actually in `serve.go`.)

- [ ] **Step 4: Tests**

Run: `go test ./internal/cli/... -run TestServe -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/serve.go internal/cli/serve_test.go
git commit -m "fix(serve): correct wording when --port 0 yields ephemeral port"
```

---

## Task 8: `parseEtime` error wrapping

**Spec:** §4.6.

**Files:**
- Modify: `internal/proc/ps.go`
- Modify: `internal/proc/ps_test.go`

- [ ] **Step 1: Locate the function**

```bash
grep -n "parseEtime\|strconv.Atoi" internal/proc/ps.go
```

- [ ] **Step 2: Write failing test**

```go
func TestParseEtimeMalformed(t *testing.T) {
    _, err := parseEtime("abc:de")
    if err == nil {
        t.Fatal("expected non-nil err for malformed input")
    }
}
```

Run: FAIL — silently returns 0.

- [ ] **Step 3: Implement**

Replace each `_, _ = strconv.Atoi(part)` (or `n, _ := strconv.Atoi(part)`) with:

```go
n, err := strconv.Atoi(part)
if err != nil {
    return 0, fmt.Errorf("parse etime %q: %w", s, err)
}
```

(Add `"fmt"` if missing.)

- [ ] **Step 4: Tests**

Run: `go test ./internal/proc/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/proc/ps.go internal/proc/ps_test.go
git commit -m "fix(proc): wrap parseEtime atoi errors instead of silent zero"
```

---

## Task 9: `diskSpaceCheck` remediation for missing dir

**Spec:** §4.7.

**Files:**
- Modify: `internal/cli/doctor.go`
- Modify: `internal/cli/doctor_test.go`

- [ ] **Step 1: Locate `diskSpaceCheck`**

```bash
grep -n "diskSpaceCheck\|Statfs" internal/cli/doctor.go
```

- [ ] **Step 2: Write failing test**

```go
func TestDiskSpaceCheckMissingDir(t *testing.T) {
    // Build a Deps with SharedModelsDir = "/nonexistent/path/that/does/not/exist".
    // Run diskSpaceCheck. Assert the remediation string contains "mkdir" and the directory path.
}
```

- [ ] **Step 3: Implement**

In `diskSpaceCheck`, detect `errno=ENOENT`-class errors:

```go
var stat syscall.Statfs_t
if err := syscall.Statfs(d.SharedModelsDir, &stat); err != nil {
    if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOENT) {
        return false, fmt.Sprintf("create the models directory: mkdir -p %s", d.SharedModelsDir)
    }
    return false, "free up space in " + d.SharedModelsDir
}
```

(Match the actual return type of the existing check — `(bool, string)` or similar.)

- [ ] **Step 4: Tests + commit**

Run: `go test ./internal/cli/... -run TestDoctor -v` and full suite.

```bash
git add internal/cli/doctor.go internal/cli/doctor_test.go
git commit -m "fix(doctor): suggest mkdir when models dir is missing (not 'free up space')"
```

---

## Task 10: Memoize `Resolver.Resolve`

**Spec:** §5.3.

**Files:**
- Modify: `internal/server/resolver.go`
- Modify: `internal/server/resolver_test.go`

- [ ] **Step 1: Read current Resolver**

```bash
sed -n '1,80p' internal/server/resolver.go
```

- [ ] **Step 2: Write failing test**

```go
func TestResolverMemoizes(t *testing.T) {
    calls := 0
    r := &Resolver{
        LookPath: func(name string) (string, error) {
            calls++
            return "/opt/homebrew/bin/" + name, nil
        },
        // ...other fields as needed...
    }
    _, _ = r.Resolve(context.Background())
    _, _ = r.Resolve(context.Background())
    if calls != 1 {
        t.Fatalf("LookPath called %d times, want 1", calls)
    }
}
```

Run: FAIL — today both calls hit LookPath.

- [ ] **Step 3: Implement**

Add `mu sync.Mutex` + `cached *Resolution` fields. In `Resolve`:

```go
func (r *Resolver) Resolve(ctx context.Context) (Resolution, error) {
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.cached != nil {
        return *r.cached, nil
    }
    res, err := r.resolveUncached(ctx) // existing body extracted into a helper
    if err != nil {
        return Resolution{}, err
    }
    r.cached = &res
    return res, nil
}
```

(Extract the current body into `resolveUncached`. Don't cache error results — only success.)

- [ ] **Step 4: Tests**

Run: `go test ./internal/server/... -race -v`
Expected: PASS, including existing Resolver tests.

- [ ] **Step 5: Commit**

```bash
git add internal/server/resolver.go internal/server/resolver_test.go
git commit -m "perf(resolver): memoize successful Resolve calls (mirrors Prober pattern)"
```

---

## Task 11: Detached poll honors ctx cancel

**Spec:** §5.5.

**Files:**
- Modify: `internal/cli/serve.go`
- Modify: `internal/cli/serve_test.go`

- [ ] **Step 1: Locate `runServeDetached` poll loop**

```bash
grep -n "detachPollInterval\|time.Sleep" internal/cli/serve.go
```

- [ ] **Step 2: Write failing test**

```go
func TestRunServeDetachedHonorsCtxCancel(t *testing.T) {
    // Set up Deps with a LaunchdService.Print that always returns PID=0
    // (service never starts). Cancel the context after 50ms. Call runServeDetached.
    // Assert returned error is context.Canceled (or ctx.Err()) within a generous deadline.
}
```

Run: FAIL — today the poll uses bare time.Sleep and won't return until the 5s overall deadline.

- [ ] **Step 3: Implement**

Replace `time.Sleep(detachPollInterval)` with:
```go
select {
case <-ctx.Done():
    return ctx.Err()
case <-time.After(detachPollInterval):
}
```

- [ ] **Step 4: Tests + commit**

Run: `go test ./internal/cli/... -race -v`

```bash
git add internal/cli/serve.go internal/cli/serve_test.go
git commit -m "fix(serve): detached poll loop honors ctx cancellation"
```

---

## Task 12: Log rotation + new doctor check

**Spec:** §5.1.

**Files:**
- Create: `internal/cli/logrotate.go`
- Create: `internal/cli/logrotate_test.go`
- Modify: `internal/cli/serve.go` (call `RotateIfLarge` before opening log)
- Modify: `internal/cli/doctor.go` (new check)
- Modify: `internal/cli/doctor_test.go`

- [ ] **Step 1: Write failing test for `RotateIfLarge`**

`internal/cli/logrotate_test.go`:

```go
package cli

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestRotateIfLargeBelowThreshold(t *testing.T) {
    dir := t.TempDir()
    p := filepath.Join(dir, "x.log")
    os.WriteFile(p, []byte("small"), 0o644)
    rotated, err := RotateIfLarge(p, 1<<20, 3)
    if err != nil {
        t.Fatal(err)
    }
    if rotated {
        t.Fatal("should not rotate")
    }
}

func TestRotateIfLargeAboveThreshold(t *testing.T) {
    dir := t.TempDir()
    p := filepath.Join(dir, "x.log")
    os.WriteFile(p, []byte(strings.Repeat("x", 2<<20)), 0o644)
    rotated, err := RotateIfLarge(p, 1<<20, 3)
    if err != nil {
        t.Fatal(err)
    }
    if !rotated {
        t.Fatal("should rotate")
    }
    if _, err := os.Stat(p + ".1"); err != nil {
        t.Fatalf("expected %s.1: %v", p, err)
    }
}

func TestRotateIfLargeKeepBound(t *testing.T) {
    dir := t.TempDir()
    p := filepath.Join(dir, "x.log")
    // Seed: p, p.1, p.2, p.3.
    for _, suffix := range []string{"", ".1", ".2", ".3"} {
        os.WriteFile(p+suffix, []byte(strings.Repeat("x", 2<<20)), 0o644)
    }
    rotated, err := RotateIfLarge(p, 1<<20, 3)
    if err != nil {
        t.Fatal(err)
    }
    if !rotated {
        t.Fatal("should rotate")
    }
    // After rotate: p.1, p.2, p.3 exist; p.4 must NOT exist.
    if _, err := os.Stat(p + ".4"); !os.IsNotExist(err) {
        t.Fatalf("p.4 should not exist; err=%v", err)
    }
}
```

Run: FAIL — `RotateIfLarge` undefined.

- [ ] **Step 2: Implement `RotateIfLarge`**

`internal/cli/logrotate.go`:

```go
package cli

import (
    "fmt"
    "os"
)

// RotateIfLarge rotates path -> path.1 -> path.2 ... -> path.<keep> when
// path exceeds maxBytes. Older numbered files past `keep` are removed.
// Returns true if rotation happened. Missing files are not an error.
func RotateIfLarge(path string, maxBytes int64, keep int) (bool, error) {
    fi, err := os.Stat(path)
    if err != nil {
        if os.IsNotExist(err) {
            return false, nil
        }
        return false, fmt.Errorf("stat %s: %w", path, err)
    }
    if fi.Size() < maxBytes {
        return false, nil
    }
    // Remove the oldest if present.
    _ = os.Remove(fmt.Sprintf("%s.%d", path, keep))
    // Shift path.(N-1) -> path.N, descending.
    for i := keep - 1; i >= 1; i-- {
        src := fmt.Sprintf("%s.%d", path, i)
        dst := fmt.Sprintf("%s.%d", path, i+1)
        if _, err := os.Stat(src); err == nil {
            if err := os.Rename(src, dst); err != nil {
                return false, fmt.Errorf("rename %s -> %s: %w", src, dst, err)
            }
        }
    }
    if err := os.Rename(path, path+".1"); err != nil {
        return false, fmt.Errorf("rename %s -> %s.1: %w", path, path, err)
    }
    return true, nil
}
```

- [ ] **Step 3: Wire rotation into serve**

In `internal/cli/serve.go`, in both `runServeForeground` and `runServeDetached`, before opening `logPath` for write:

```go
if _, err := RotateIfLarge(logPath, 10<<20, 3); err != nil {
    fmt.Fprintf(d.Stderr, "llamactl: warning: log rotation failed: %v\n", err)
    // Don't fail; serving is more important than rotation hygiene.
}
```

- [ ] **Step 4: Add doctor check `logFilesNotOversized`**

In `internal/cli/doctor.go`, define a new `doctorCheck`:

```go
func logFilesNotOversizedCheck(d *Deps) doctorCheck {
    return doctorCheck{
        label:       "Log files within size limit (10 MiB)",
        remediation: "rotate or remove oversized log: ls -lh " + d.LogsDir,
        run: func(ctx context.Context) (bool, string) {
            if d.LogsDir == "" {
                return true, "(not configured)"
            }
            entries, err := os.ReadDir(d.LogsDir)
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
```

Wire it into the doctor's checks slice. Total check count: was 10, becomes 11 here (12 after Task 13).

- [ ] **Step 5: Update doctor tests for new check**

Add a Pass case (logs dir empty) and a Fail case (one oversized fake file) following the existing test pattern. Update any test that counts total checks (search for `len(checks)`).

- [ ] **Step 6: Run full suite**

Run: `go test ./... -race`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/logrotate.go internal/cli/logrotate_test.go internal/cli/serve.go internal/cli/doctor.go internal/cli/doctor_test.go
git commit -m "feat(serve): rotate per-model logs at 10 MiB; doctor check for oversized logs"
```

---

## Task 13: HF cache prune (auto + manual command + doctor check)

**Spec:** §5.2.

**Files:**
- Modify: `internal/hf/cache.go` (`PruneOlderThan` method)
- Modify: `internal/hf/cache_test.go`
- Modify: `internal/hf/client.go` (call PruneOlderThan from Search/RepoInfo)
- Create: `internal/cli/cache.go` (cobra command)
- Create: `internal/cli/cache_test.go`
- Modify: `internal/cli/root.go` (register `cache` subcommand)
- Modify: `internal/cli/doctor.go` (new `hfCacheSize` check)
- Modify: `internal/cli/doctor_test.go`

- [ ] **Step 1: Implement `Cache.PruneOlderThan` TDD**

Add to `internal/hf/cache_test.go`:

```go
func TestCachePruneOlderThan(t *testing.T) {
    dir := t.TempDir()
    c := &Cache{Dir: dir} // adapt to actual Cache type
    // Write two files: one with mtime 60 days ago, one fresh.
    oldFile := filepath.Join(dir, "old.json")
    os.WriteFile(oldFile, []byte("{}"), 0o644)
    oldTime := time.Now().Add(-60 * 24 * time.Hour)
    os.Chtimes(oldFile, oldTime, oldTime)
    newFile := filepath.Join(dir, "new.json")
    os.WriteFile(newFile, []byte("{}"), 0o644)

    n, err := c.PruneOlderThan(30 * 24 * time.Hour)
    if err != nil {
        t.Fatal(err)
    }
    if n != 1 {
        t.Fatalf("removed=%d, want 1", n)
    }
    if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
        t.Fatal("old file should be removed")
    }
    if _, err := os.Stat(newFile); err != nil {
        t.Fatal("new file should still exist")
    }
}
```

Read `internal/hf/cache.go` to learn the actual `Cache` struct shape. Adapt field names.

Run: FAIL — `PruneOlderThan` undefined.

- [ ] **Step 2: Implement `PruneOlderThan`**

In `internal/hf/cache.go`:

```go
// PruneOlderThan removes cache files whose mtime is older than d.
// Returns the count removed. Walks recursively under the cache root.
func (c *Cache) PruneOlderThan(d time.Duration) (int, error) {
    cutoff := time.Now().Add(-d)
    removed := 0
    err := filepath.WalkDir(c.Dir, func(p string, e fs.DirEntry, err error) error {
        if err != nil {
            return nil // best-effort: skip unreadable entries
        }
        if e.IsDir() {
            return nil
        }
        info, ierr := e.Info()
        if ierr != nil {
            return nil
        }
        if info.ModTime().Before(cutoff) {
            if rmErr := os.Remove(p); rmErr == nil {
                removed++
            }
        }
        return nil
    })
    if err != nil && !errors.Is(err, fs.ErrNotExist) {
        return removed, err
    }
    return removed, nil
}
```

(Add `time`, `os`, `io/fs`, `path/filepath`, `errors` imports as needed.)

- [ ] **Step 3: Wire auto-prune into Search and RepoInfo**

In `internal/hf/client.go`, at the top of `Search` and `RepoInfo`:

```go
if _, err := c.cache.PruneOlderThan(30 * 24 * time.Hour); err != nil {
    // Best-effort; log to stderr but don't fail the operation.
    fmt.Fprintf(os.Stderr, "llamactl: warning: hf cache prune: %v\n", err)
}
```

(Match the actual cache reference inside HFClient. If `Client` has no `Stderr` writer, accept logging to `os.Stderr` directly; this is non-critical.)

- [ ] **Step 4: Write failing test for `cache prune` cobra command**

`internal/cli/cache_test.go`:

```go
func TestCachePruneAll(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "stale.json"), []byte("{}"), 0o644)
    d := &Deps{
        HFCacheDir: dir,
        Stdout:     io.Discard,
        Stderr:     io.Discard,
    }
    cmd := newCacheCmd(d)
    cmd.SetArgs([]string{"prune", "--all"})
    if err := cmd.Execute(); err != nil {
        t.Fatal(err)
    }
    if _, err := os.Stat(filepath.Join(dir, "stale.json")); !os.IsNotExist(err) {
        t.Fatal("expected stale.json removed")
    }
}

func TestCachePruneDefault(t *testing.T) {
    dir := t.TempDir()
    // One old, one fresh.
    oldP := filepath.Join(dir, "old.json")
    os.WriteFile(oldP, []byte("{}"), 0o644)
    oldTime := time.Now().Add(-60 * 24 * time.Hour)
    os.Chtimes(oldP, oldTime, oldTime)
    freshP := filepath.Join(dir, "fresh.json")
    os.WriteFile(freshP, []byte("{}"), 0o644)
    d := &Deps{HFCacheDir: dir, Stdout: io.Discard, Stderr: io.Discard}
    cmd := newCacheCmd(d)
    cmd.SetArgs([]string{"prune"})
    if err := cmd.Execute(); err != nil {
        t.Fatal(err)
    }
    if _, err := os.Stat(oldP); !os.IsNotExist(err) {
        t.Fatal("old should be gone")
    }
    if _, err := os.Stat(freshP); err != nil {
        t.Fatal("fresh should remain")
    }
}
```

Run: FAIL — command doesn't exist yet.

- [ ] **Step 5: Implement `cache` parent + `prune` subcommand**

`internal/cli/cache.go`:

```go
package cli

import (
    "fmt"
    "io/fs"
    "os"
    "path/filepath"
    "time"

    "github.com/spf13/cobra"
)

func newCacheCmd(d *Deps) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "cache",
        Short: "Manage the HuggingFace API response cache",
    }
    cmd.AddCommand(newCachePruneCmd(d))
    return cmd
}

func newCachePruneCmd(d *Deps) *cobra.Command {
    var all bool
    cmd := &cobra.Command{
        Use:   "prune",
        Short: "Remove stale HuggingFace cache entries",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runCachePrune(d, all)
        },
    }
    cmd.Flags().BoolVar(&all, "all", false, "remove all cache entries, not just stale ones")
    return cmd
}

func runCachePrune(d *Deps, all bool) error {
    if d.HFCacheDir == "" {
        return fmt.Errorf("%w: HF cache dir not configured", ErrUserError)
    }
    if all {
        n := 0
        err := filepath.WalkDir(d.HFCacheDir, func(p string, e fs.DirEntry, err error) error {
            if err != nil || e.IsDir() {
                return nil
            }
            if rmErr := os.Remove(p); rmErr == nil {
                n++
            }
            return nil
        })
        fmt.Fprintf(d.Stdout, "removed %d cache file(s)\n", n)
        if err != nil && !os.IsNotExist(err) {
            return err
        }
        return nil
    }
    // Default: 30-day prune. Walk by hand (mirrors hf.Cache.PruneOlderThan but
    // operates directly on the dir; we don't need the hf.Cache instance here).
    cutoff := time.Now().Add(-30 * 24 * time.Hour)
    removed := 0
    err := filepath.WalkDir(d.HFCacheDir, func(p string, e fs.DirEntry, err error) error {
        if err != nil || e.IsDir() {
            return nil
        }
        info, ierr := e.Info()
        if ierr != nil {
            return nil
        }
        if info.ModTime().Before(cutoff) {
            if rmErr := os.Remove(p); rmErr == nil {
                removed++
            }
        }
        return nil
    })
    fmt.Fprintf(d.Stdout, "removed %d stale cache file(s)\n", removed)
    if err != nil && !os.IsNotExist(err) {
        return err
    }
    return nil
}
```

- [ ] **Step 6: Register in root.go**

In `internal/cli/root.go`, where other subcommands are added, insert:

```go
rootCmd.AddCommand(newCacheCmd(d))
```

- [ ] **Step 7: Add `hfCacheSize` doctor check**

In `internal/cli/doctor.go`:

```go
func hfCacheSizeCheck(d *Deps) doctorCheck {
    return doctorCheck{
        label:       "HuggingFace API cache size (<500 MiB)",
        remediation: "run: llamactl cache prune",
        run: func(ctx context.Context) (bool, string) {
            if d.HFCacheDir == "" {
                return true, "(not configured)"
            }
            var total int64
            err := filepath.WalkDir(d.HFCacheDir, func(p string, e fs.DirEntry, werr error) error {
                if werr != nil || e.IsDir() {
                    return nil
                }
                info, ierr := e.Info()
                if ierr == nil {
                    total += info.Size()
                }
                return nil
            })
            if err != nil && !os.IsNotExist(err) {
                return false, err.Error()
            }
            if total > 500<<20 {
                return false, fmt.Sprintf("cache is %d MiB", total>>20)
            }
            return true, ""
        },
    }
}
```

Wire into the doctor checks slice. Total checks: 12.

- [ ] **Step 8: Update doctor tests**

Add Pass + Fail cases for `hfCacheSize`. Update any test counting `len(checks)`.

- [ ] **Step 9: Full suite + commit**

Run: `go test ./... -race`

```bash
git add internal/hf/cache.go internal/hf/cache_test.go internal/hf/client.go internal/cli/cache.go internal/cli/cache_test.go internal/cli/root.go internal/cli/doctor.go internal/cli/doctor_test.go
git commit -m "feat(cache): hf cache prune subcommand + auto-prune + doctor check"
```

---

## Task 14: Unify `fakeRunner` into `internal/testutil`

**Spec:** §7.3.

**Files:**
- Create: `internal/testutil/fakerunner.go`
- Create: `internal/testutil/fakerunner_test.go`
- Modify: `internal/hardware/detector_test.go` (migrate, fix key construction)
- Modify: `internal/server/resolver_test.go` (migrate)
- Modify: `internal/server/capabilities_test.go` (migrate)
- Modify: `internal/launchd/service_test.go` (migrate)
- Modify: `internal/launchd/list_test.go` (migrate)
- Modify: `internal/proc/ps_test.go` (migrate)
- Modify: `internal/cli/integration_test.go` (migrate if `intRunner` is identical)

Mechanical migration. Do this AFTER most behavior work to minimize merge conflicts.

- [ ] **Step 1: Write `FakeRunner` with tests**

`internal/testutil/fakerunner.go`:

```go
// Package testutil holds test-only utilities shared across llamactl packages.
package testutil

import (
    "context"
    "fmt"
    "io"
    "strings"
)

// FakeRunner is a controllable runner.CommandRunner used in tests.
// Keys map by "name + ' ' + strings.Join(args, ' ')". Set Outputs to inject
// stdout strings; set Errs to inject non-nil errors. Records every call in Calls.
type FakeRunner struct {
    Outputs map[string]string
    Errs    map[string]error
    Calls   []string
}

func (f *FakeRunner) Run(ctx context.Context, name string, args []string, dir string,
    stdout, stderr io.Writer) error {
    key := name
    if len(args) > 0 {
        key = name + " " + strings.Join(args, " ")
    }
    f.Calls = append(f.Calls, key)
    if out, ok := f.Outputs[key]; ok {
        fmt.Fprint(stdout, out)
    }
    if err, ok := f.Errs[key]; ok {
        return err
    }
    return nil
}
```

`internal/testutil/fakerunner_test.go`:

```go
package testutil

import (
    "bytes"
    "context"
    "errors"
    "testing"
)

func TestFakeRunnerOutputs(t *testing.T) {
    f := &FakeRunner{Outputs: map[string]string{"sysctl -n hw.ncpu": "8"}}
    var buf bytes.Buffer
    if err := f.Run(context.Background(), "sysctl", []string{"-n", "hw.ncpu"}, "", &buf, nil); err != nil {
        t.Fatal(err)
    }
    if buf.String() != "8" {
        t.Fatalf("got %q", buf.String())
    }
}

func TestFakeRunnerErrs(t *testing.T) {
    sentinel := errors.New("boom")
    f := &FakeRunner{Errs: map[string]error{"foo bar": sentinel}}
    if err := f.Run(context.Background(), "foo", []string{"bar"}, "", nil, nil); !errors.Is(err, sentinel) {
        t.Fatalf("got %v", err)
    }
}

func TestFakeRunnerRecordsCalls(t *testing.T) {
    f := &FakeRunner{}
    f.Run(context.Background(), "a", []string{"b"}, "", nil, nil)
    f.Run(context.Background(), "c", nil, "", nil, nil)
    if len(f.Calls) != 2 || f.Calls[0] != "a b" || f.Calls[1] != "c" {
        t.Fatalf("calls=%v", f.Calls)
    }
}
```

Run: `go test ./internal/testutil/... -v` — PASS.

- [ ] **Step 2: Migrate `hardware` tests (keying change)**

`internal/hardware/detector_test.go` uses keys like `"sysctl"` or `"sysctl -n hw.ncpu"` — check first. If it uses `name + " " + args[0]` (single arg), the migration may need to rewrite test fixtures to use the full args form.

```bash
grep -n "fakeRunner\|Outputs\[" internal/hardware/detector_test.go
```

Inspect every key in the fixtures; rewrite to `name + " " + strings.Join(args, " ")` form. Replace the local fakeRunner struct with `testutil.FakeRunner`. Run `go test ./internal/hardware/... -v` until green.

- [ ] **Step 3: Migrate `server`, `launchd`, `proc` tests**

These already use the joined-args form. Mechanical replacement: delete the local struct, change uses to `testutil.FakeRunner`, add the import.

Run after each package: `go test ./internal/server/... ./internal/launchd/... ./internal/proc/... -race -v`.

- [ ] **Step 4: Migrate cli integration if applicable**

If `internal/cli/integration_test.go`'s `intRunner` matches the same shape, migrate. If it has extra functionality (e.g., spawning a subprocess), leave it alone or keep it as a wrapper around `testutil.FakeRunner`.

- [ ] **Step 5: Full suite**

Run: `go test ./... -race`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/testutil/ internal/hardware/detector_test.go internal/server/*_test.go internal/launchd/*_test.go internal/proc/ps_test.go internal/cli/integration_test.go
git commit -m "refactor(testutil): unify fakeRunner across 6 packages"
```

---

## Task 15: Parameterize `platform.Cores` in recipes

**Spec:** §7.4.

**Files:**
- Modify: `internal/recipes/recipes.go` (signature change)
- Modify: `internal/recipes/recipes_test.go`
- Modify: `internal/cli/serve.go` (caller)

- [ ] **Step 1: Locate `FlagsFor`**

```bash
grep -n "FlagsFor\|platform.Default" internal/recipes/recipes.go internal/cli/serve.go
```

- [ ] **Step 2: Change signature**

Add `cores int` as the last positional arg. Inside `FlagsFor`, replace `platform.Default{}.Cores()` with the new param.

Old:
```go
func FlagsFor(r Recipe, m models.Model, q models.Quant, ggufPath string,
    hw hardware.Info, ver server.Version, caps server.Capabilities,
    sizeGB float64, port int) []string
```

New:
```go
func FlagsFor(r Recipe, m models.Model, q models.Quant, ggufPath string,
    hw hardware.Info, ver server.Version, caps server.Capabilities,
    sizeGB float64, port int, cores int) []string
```

- [ ] **Step 3: Update caller in serve.go**

```go
flags := recipes.FlagsFor(recipe, model, quant, ggufPath, hw, ver, caps,
    sizeGB, port, platform.Default{}.Cores())
```

- [ ] **Step 4: Update recipe tests**

Pass an explicit `cores` value (e.g., 10 for Apple M5) in every `FlagsFor` test call.

- [ ] **Step 5: Tests + commit**

Run: `go test ./... -race`

```bash
git add internal/recipes/recipes.go internal/recipes/recipes_test.go internal/cli/serve.go
git commit -m "refactor(recipes): make FlagsFor accept cores as a param (test-injectable)"
```

---

## Task 16: `CommandRunner` "stdin" → "dir" rename

**Spec:** §5.4.

**Files (search for redeclarations):**
- Modify: `internal/launchd/service.go`
- Modify: `internal/proc/ps.go`
- Any other file that redeclares the `CommandRunner` interface

Note: `internal/runner/runner.go` already uses `dir`. The bug is in *local redeclarations* within `internal/launchd` and `internal/proc`.

- [ ] **Step 1: Find local redeclarations**

```bash
grep -rn "Run(ctx context.Context, name string, args \[\]string, stdin" --include="*.go" internal/ cmd/
```

Expected output: `internal/launchd/service.go`, `internal/proc/ps.go` (confirmed earlier).

- [ ] **Step 2: Rename parameter in each interface declaration**

For both files, change `stdin string` to `dir string` in the locally-declared `CommandRunner` interface. Also add a one-line doc:

```go
// CommandRunner runs an external command. dir is the working directory
// (empty = cwd). Implementations mirror runner.CommandRunner.
type CommandRunner interface {
    Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
}
```

- [ ] **Step 3: Run vet + test**

Run: `go vet ./... && go test ./... -race`
Expected: PASS. (Pure rename; Go structural typing means runner.ExecRunner still satisfies all locals.)

- [ ] **Step 4: Commit**

```bash
git add internal/launchd/service.go internal/proc/ps.go
git commit -m "refactor: rename CommandRunner 4th param 'stdin' -> 'dir' (matches semantics)"
```

---

## Task 17: PRD doc update for `download.Request`

**Spec:** §7.1. Docs only — no code change.

**Files:**
- Modify: `docs/llamactl-prd-v1.5.md`

- [ ] **Step 1: Find the stale reference**

```bash
grep -n "ProgressOut\|download.Request" docs/llamactl-prd-v1.5.md
```

- [ ] **Step 2: Update to reflect code reality**

Replace `ProgressOut io.Writer` with `Progress *Progress` and add a short note: "Progress is a state-bearing struct (byte count + rate); see `internal/download/progress.go`."

- [ ] **Step 3: Commit**

```bash
git add docs/llamactl-prd-v1.5.md
git commit -m "docs(prd): update download.Request field to match code (Progress *Progress)"
```

---

## Task 18: `fit` command

**Spec:** §3. The headliner. Depends on Tasks 1 (gguftest), 2 (parser fix), 13 (HF cache stable).

**Files:**
- Create: `internal/models/paramcount.go`
- Create: `internal/models/paramcount_test.go`
- Create: `internal/cli/fit.go`
- Create: `internal/cli/fit_test.go`
- Modify: `internal/cli/root.go` (register `fit`)

### Subtask 18a: `paramcount.ParseParamCountFromRepo` (TDD)

- [ ] **Step 1: Write failing tests**

`internal/models/paramcount_test.go`:

```go
package models

import "testing"

func TestParseParamCountFromRepo(t *testing.T) {
    cases := []struct {
        repo string
        want float64
    }{
        {"Qwen/Qwen2.5-7B-Instruct-GGUF", 7},
        {"Qwen/Qwen3-0.6B-GGUF", 0.6},
        {"unsloth/gemma-4-31B-it-GGUF", 31},
        {"unsloth/gemma-4-E4B-it-GGUF", 4},
        {"meta-llama/Llama-3.3-70B-Instruct", 70},
        {"qwen2.5-7b-instruct", 7},
        {"unknown-repo-no-digits", 0},
        {"", 0},
    }
    for _, c := range cases {
        t.Run(c.repo, func(t *testing.T) {
            got := ParseParamCountFromRepo(c.repo)
            if got != c.want {
                t.Fatalf("got %v, want %v", got, c.want)
            }
        })
    }
}

func TestParseParamCountPrefersRepoName(t *testing.T) {
    // owner has "7B"; repo has "13B" — repo wins.
    got := ParseParamCountFromRepo("foo-7B/model-13B-it")
    if got != 13 {
        t.Fatalf("got %v, want 13", got)
    }
}
```

Run: FAIL.

- [ ] **Step 2: Implement**

`internal/models/paramcount.go`:

```go
// Package-level addition to models. Implements parameter-count extraction
// from HuggingFace repo paths via regex. See spec §3.4.
package models

import (
    "regexp"
    "strconv"
    "strings"
)

var (
    // E4B-style: capture the digit between E and B. Example: "Gemma-4-E4B-it" -> 4.
    paramCountReEffective = regexp.MustCompile(`E(\d+(?:\.\d+)?)B`)
    // Standard: digit(s) followed by 'b' or 'B'. Example: "Qwen3-0.6B" -> 0.6.
    paramCountReStandard = regexp.MustCompile(`(\d+(?:\.\d+)?)[bB]`)
)

// ParseParamCountFromRepo extracts a parameter count in billions from a
// HuggingFace repo path. Returns 0 if no recognizable pattern is found.
// Patterns: E4B (effective), then \d+B (standard). Searches the last path
// segment first (repo name); falls back to earlier segments.
func ParseParamCountFromRepo(repo string) float64 {
    if repo == "" {
        return 0
    }
    parts := strings.Split(repo, "/")
    // Prefer the repo name (last segment) over the owner.
    for i := len(parts) - 1; i >= 0; i-- {
        if v := matchParamCount(parts[i]); v > 0 {
            return v
        }
    }
    return 0
}

func matchParamCount(s string) float64 {
    if m := paramCountReEffective.FindStringSubmatch(s); m != nil {
        if v, err := strconv.ParseFloat(m[1], 64); err == nil {
            return v
        }
    }
    if m := paramCountReStandard.FindStringSubmatch(s); m != nil {
        if v, err := strconv.ParseFloat(m[1], 64); err == nil {
            return v
        }
    }
    return 0
}
```

- [ ] **Step 3: KVCacheGB function**

Add to same file:

```go
// KVCacheGB estimates the KV-cache memory footprint at runtime.
// Uses KVCachePerTokenKB[arch][Q8_0] (Phase 2 table — nested by arch+kvQuant).
// Returns 0 if arch is unknown so the caller can fall back to padding.
func KVCacheGB(arch Arch, paramsB float64, ctx int) float64 {
    row, ok := KVCachePerTokenKB[arch]
    if !ok {
        return 0
    }
    perTokKB, ok := row[Q8_0]
    if !ok || perTokKB <= 0 {
        return 0
    }
    // perTokKB is K+V per-token cache size in KiB. Total GB = perTokKB * ctx / 1024^2.
    return perTokKB * float64(ctx) / (1024.0 * 1024.0)
}
```

**Important:** `KVCachePerTokenKB` is `map[Arch]map[Quant]float64` (nested), not flat. The
inner key is the *KV cache* quantization (Q8_0 for the chat recipe default), NOT the model
weight quant. Phase 2 only populated `Q8_0` entries; if a future arch adds f16 rows, this
function may need a quant parameter. Don't add that now — YAGNI.

- [ ] **Step 4: Run and commit subtask 18a**

Run: `go test ./internal/models/... -v` — PASS.

```bash
git add internal/models/paramcount.go internal/models/paramcount_test.go
git commit -m "feat(models): ParseParamCountFromRepo + KVCacheGB helpers for fit command"
```

### Subtask 18b: `fit` cobra command

- [ ] **Step 1: Write failing tests**

`internal/cli/fit_test.go`:

```go
package cli

import (
    "bytes"
    "context"
    "encoding/json"
    "strings"
    "testing"

    "github.com/gregmundy/llamactl/internal/hardware"
    "github.com/gregmundy/llamactl/internal/hf"
)

func TestFitShowsRankedTable(t *testing.T) {
    d := buildFitTestDeps(t, fakeFitFixtures{
        hits: []hf.SearchHit{
            {ID: "unsloth/gemma-4-E4B-it-GGUF"},
            {ID: "unsloth/gemma-4-31B-it-GGUF"},
        },
        repos: map[string]hf.Repo{
            "unsloth/gemma-4-E4B-it-GGUF": {Files: []hf.File{
                {Path: "Q5_K_M.gguf", Size: int64(3.4 * 1e9)},
            }},
            "unsloth/gemma-4-31B-it-GGUF": {Files: []hf.File{
                {Path: "Q4_K_M.gguf", Size: int64(17.2 * 1e9)},
                {Path: "Q5_K_M.gguf", Size: int64(20.5 * 1e9)},
            }},
        },
        hw: hardware.Info{TotalMemGB: 24}, // 24 GB host
    })
    var out bytes.Buffer
    d.Stdout = &out
    cmd := newFitCmd(d)
    cmd.SetArgs([]string{"gemma", "4"})
    if err := cmd.ExecuteContext(context.Background()); err != nil {
        t.Fatal(err)
    }
    s := out.String()
    if !strings.Contains(s, "gemma-4-E4B-it-GGUF") {
        t.Fatalf("missing E4B row in output:\n%s", s)
    }
    if !strings.Contains(s, "✓") {
        t.Fatalf("missing checkmark verdict:\n%s", s)
    }
}

func TestFitNoGGUFRepos(t *testing.T) {
    d := buildFitTestDeps(t, fakeFitFixtures{
        hits:  []hf.SearchHit{{ID: "meta-llama/Llama-3-70B"}}, // no -GGUF suffix; no Q* files
        repos: map[string]hf.Repo{"meta-llama/Llama-3-70B": {Files: nil}},
        hw:    hardware.Info{TotalMemGB: 24},
    })
    var out bytes.Buffer
    d.Stdout = &out
    cmd := newFitCmd(d)
    cmd.SetArgs([]string{"llama"})
    if err := cmd.ExecuteContext(context.Background()); err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(out.String(), "no GGUF") {
        t.Fatalf("missing no-GGUF message:\n%s", out.String())
    }
}

func TestFitAllExceedHost(t *testing.T) {
    d := buildFitTestDeps(t, fakeFitFixtures{
        hits: []hf.SearchHit{{ID: "unsloth/gemma-4-31B-it-GGUF"}},
        repos: map[string]hf.Repo{
            "unsloth/gemma-4-31B-it-GGUF": {Files: []hf.File{
                {Path: "Q8_0.gguf", Size: int64(50 * 1e9)}, // way too big
            }},
        },
        hw: hardware.Info{TotalMemGB: 16},
    })
    var out bytes.Buffer
    d.Stdout = &out
    cmd := newFitCmd(d)
    cmd.SetArgs([]string{"gemma"})
    if err := cmd.ExecuteContext(context.Background()); err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(out.String(), "✗") {
        t.Fatalf("expected ✗ verdict for oversized model:\n%s", out.String())
    }
}

func TestFitInstallShortcut(t *testing.T) {
    fakeDl := &fakeDownloaderRecording{}
    d := buildFitTestDeps(t, fakeFitFixtures{
        hits: []hf.SearchHit{{ID: "unsloth/gemma-4-E4B-it-GGUF"}},
        repos: map[string]hf.Repo{
            "unsloth/gemma-4-E4B-it-GGUF": {Files: []hf.File{
                {Path: "Q5_K_M.gguf", Size: int64(3.4 * 1e9), LFS: hf.LFSInfo{SHA256: "abc123"}},
            }},
        },
        hw: hardware.Info{TotalMemGB: 24},
    })
    d.Downloader = fakeDl
    cmd := newFitCmd(d)
    cmd.SetArgs([]string{"gemma", "4", "--install"})
    if err := cmd.ExecuteContext(context.Background()); err != nil {
        t.Fatal(err)
    }
    if len(fakeDl.requests) != 1 {
        t.Fatalf("expected 1 download, got %d", len(fakeDl.requests))
    }
    if fakeDl.requests[0].RepoID != "unsloth/gemma-4-E4B-it-GGUF" {
        t.Fatalf("wrong repo: %s", fakeDl.requests[0].RepoID)
    }
}

func TestFitInstallNoCandidate(t *testing.T) {
    d := buildFitTestDeps(t, fakeFitFixtures{
        hits: []hf.SearchHit{{ID: "unsloth/gemma-4-31B-it-GGUF"}},
        repos: map[string]hf.Repo{
            "unsloth/gemma-4-31B-it-GGUF": {Files: []hf.File{
                {Path: "Q8_0.gguf", Size: int64(50 * 1e9)},
            }},
        },
        hw: hardware.Info{TotalMemGB: 8},
    })
    cmd := newFitCmd(d)
    cmd.SetArgs([]string{"gemma", "--install"})
    err := cmd.ExecuteContext(context.Background())
    if err == nil || !errors.Is(err, ErrUserError) {
        t.Fatalf("expected user error, got %v", err)
    }
}

func TestFitJSON(t *testing.T) {
    d := buildFitTestDeps(t, fakeFitFixtures{
        hits: []hf.SearchHit{{ID: "unsloth/gemma-4-E4B-it-GGUF"}},
        repos: map[string]hf.Repo{
            "unsloth/gemma-4-E4B-it-GGUF": {Files: []hf.File{
                {Path: "Q5_K_M.gguf", Size: int64(3.4 * 1e9)},
            }},
        },
        hw: hardware.Info{TotalMemGB: 24},
    })
    var out bytes.Buffer
    d.Stdout = &out
    cmd := newFitCmd(d)
    cmd.SetArgs([]string{"gemma", "--json"})
    if err := cmd.ExecuteContext(context.Background()); err != nil {
        t.Fatal(err)
    }
    var rows []map[string]any
    if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
        t.Fatalf("invalid JSON: %v\n%s", err, out.String())
    }
    if len(rows) == 0 {
        t.Fatal("empty JSON array")
    }
    for _, k := range []string{"repo", "quant", "size_gb", "verdict"} {
        if _, ok := rows[0][k]; !ok {
            t.Fatalf("missing key %q in row: %v", k, rows[0])
        }
    }
}

// Test fixture helpers — implement once. See existing patterns in
// integration_test.go for fake HFClient / fake Downloader / fake Detector.
type fakeFitFixtures struct {
    hits  []hf.SearchHit
    repos map[string]hf.Repo
    hw    hardware.Info
}

func buildFitTestDeps(t *testing.T, f fakeFitFixtures) *Deps {
    t.Helper()
    // ... wire a Deps with fake HFClient (Search returns f.hits, RepoInfo
    // returns f.repos[id]), fake HardwareDetector (returns f.hw), fake
    // ModelStore + FileSystem, etc. Reuse patterns from add_test.go.
}

type fakeDownloaderRecording struct {
    requests []download.Request
}

func (f *fakeDownloaderRecording) Get(ctx context.Context, req download.Request) error {
    f.requests = append(f.requests, req)
    return nil
}
```

(The helper functions and missing fixture wiring should follow the existing patterns in `internal/cli/integration_test.go` and `add_test.go`. The implementer should read those first.)

Run: FAIL — `newFitCmd` undefined.

- [ ] **Step 2: Implement `fit` command**

`internal/cli/fit.go`:

```go
package cli

import (
    "context"
    "encoding/json"
    "fmt"
    "regexp"
    "sort"
    "strings"
    "text/tabwriter"

    "github.com/gregmundy/llamactl/internal/hardware"
    "github.com/gregmundy/llamactl/internal/hf"
    "github.com/gregmundy/llamactl/internal/models"
    "github.com/spf13/cobra"
)

const (
    fitHeadroomGB = 4.0
    // Default ctx for chat recipe = 8192.
    fitDefaultCtx = 8192
)

// quantFromPath extracts "Q5_K_M" from "Q5_K_M.gguf" or "name.Q5_K_M.gguf".
var fitQuantRe = regexp.MustCompile(`(Q\d+_[A-Z0-9_]+)`)

type fitRow struct {
    Repo       string  `json:"repo"`
    Quant      string  `json:"quant"`
    SizeGB     float64 `json:"size_gb"`
    Verdict    string  `json:"verdict"` // "ok", "tight", "too-big"
    FreeGB     float64 `json:"free_gb,omitempty"`
    DeficitGB  float64 `json:"deficit_gb,omitempty"`
    Note       string  `json:"note,omitempty"`
    // Carry the underlying file info for --install.
    file       hf.File `json:"-"`
}

func newFitCmd(d *Deps) *cobra.Command {
    var install bool
    var ctxSize int
    var limit int
    var asJSON bool
    cmd := &cobra.Command{
        Use:   "fit <query...>",
        Short: "Search HF + rank GGUF variants by fit on this host",
        Args:  cobra.MinimumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            return runFit(cmd.Context(), d, strings.Join(args, " "), install, ctxSize, limit, asJSON)
        },
    }
    cmd.Flags().BoolVar(&install, "install", false, "install the top-ranked ✓ row")
    cmd.Flags().IntVar(&ctxSize, "ctx", fitDefaultCtx, "context size for KV-cache estimation")
    cmd.Flags().IntVar(&limit, "limit", 10, "cap rows shown")
    cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
    return cmd
}

func runFit(ctx context.Context, d *Deps, query string, install bool, ctxSize, limit int, asJSON bool) error {
    hw, err := ensureHardware(ctx, d)
    if err != nil {
        return err
    }
    hits, err := d.HFClient.Search(ctx, query)
    if err != nil {
        return fmt.Errorf("hf search: %w", err)
    }
    usable := models.GpuAddressableGB(hw) - models.OSOverheadGB - models.HeadroomGB

    var rows []fitRow
    for _, hit := range hits {
        repo, err := d.HFClient.RepoInfo(ctx, hit.ID)
        if err != nil {
            continue
        }
        paramsB := models.ParseParamCountFromRepo(hit.ID)
        if paramsB == 0 {
            paramsB = 13 // conservative fallback
        }
        // Arch detection: try PreferredIDs first (the static map keys to known repos),
        // then fall back to "" which makes KVCacheGB return 0 → caller pads with 1 GB.
        // We deliberately do NOT download + parse the GGUF here — fit is a fast lookup.
        arch := models.Arch("")
        for _, m := range models.PreferredIDs {
            if strings.EqualFold(m.HFRepo, hit.ID) {
                arch = m.Arch
                break
            }
        }
        for _, f := range repo.Files {
            q := fitQuantRe.FindString(f.Path)
            if q == "" {
                continue
            }
            sizeGB := float64(f.Size) / 1e9
            kvGB := models.KVCacheGB(arch, paramsB, ctxSize)
            if kvGB == 0 {
                kvGB = 1.0 // padding for unknown arch
            }
            total := sizeGB + kvGB
            row := fitRow{Repo: hit.ID, Quant: q, SizeGB: sizeGB, file: f}
            switch {
            case usable-total >= fitHeadroomGB:
                row.Verdict = "ok"
                row.FreeGB = usable - total
                row.Note = fmt.Sprintf("%.0f GB free", row.FreeGB)
            case usable >= total:
                row.Verdict = "tight"
                row.FreeGB = usable - total
                row.Note = "tight headroom"
            default:
                row.Verdict = "too-big"
                row.DeficitGB = total - usable
                row.Note = fmt.Sprintf("need %.0f GB more", row.DeficitGB)
            }
            rows = append(rows, row)
        }
    }

    if len(rows) == 0 {
        fmt.Fprintln(d.Stdout, "no GGUF repos matched")
        return nil
    }

    sort.SliceStable(rows, func(i, j int) bool {
        // ok > tight > too-big; within bucket, ok rows by descending FreeGB,
        // too-big rows by ascending DeficitGB, tight rows by size ascending.
        return fitRank(rows[i]) > fitRank(rows[j])
    })
    if len(rows) > limit {
        rows = rows[:limit]
    }

    if install {
        for _, r := range rows {
            if r.Verdict == "ok" {
                return runAdd(ctx, d, r.Repo, r.Quant, ctxSize)
            }
        }
        return fmt.Errorf("%w: no ✓ candidate; use `llamactl fit %s` to see all options", ErrUserError, query)
    }

    if asJSON {
        return json.NewEncoder(d.Stdout).Encode(rows)
    }
    return renderFitTable(d.Stdout, rows)
}

func fitRank(r fitRow) float64 {
    switch r.Verdict {
    case "ok":
        return 1000 + r.FreeGB
    case "tight":
        return 100 - r.SizeGB
    default:
        return -r.DeficitGB
    }
}

func renderFitTable(w io.Writer, rows []fitRow) error {
    tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
    fmt.Fprintln(tw, "RECOMMENDED\tREPO\tQUANT\tSIZE\tVERDICT\tNOTES")
    for _, r := range rows {
        var verdictSym string
        switch r.Verdict {
        case "ok":
            verdictSym = "   ✓"
        case "tight":
            verdictSym = "   ⚠"
        default:
            verdictSym = "   ✗"
        }
        sizeStr := fmt.Sprintf("%.1f GB", r.SizeGB)
        fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
            verdictSym, r.Repo, r.Quant, sizeStr, r.Verdict, r.Note)
    }
    return tw.Flush()
}
```

**Caveats for the implementer:**
- `models.PreferredIDs` is a `map[string]models.Model` keyed by short ID (per Phase 2.5 rename). Iterate values looking for `HFRepo == hit.ID`. Verify shape with `grep -n "PreferredIDs " internal/models/whitelist.go` before coding.
- `runAdd` is the existing function from `add.go`. Its signature is `runAdd(ctx, d, input, quantOverride, targetCtx) error` and `quantOverride` must be a string like `"Q5_K_M"` — pass `r.Quant` (already a string), not the full file path.
- `io` and `errors` need importing.
- The `tabwriter` "RECOMMENDED" column with leading whitespace plus a Unicode symbol — eyeball the alignment locally before committing. The spec's example formatting may not survive tabwriter cleanly; adjust width hints if it looks off.

If anything in this implementation requires a Deps interface that doesn't exist (e.g., arch lookup): **STOP and ask the orchestrator.**

- [ ] **Step 3: Register `fit` in root.go**

```go
rootCmd.AddCommand(newFitCmd(d))
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cli/... -v -run TestFit`
Expected: PASS for all 6 tests.

- [ ] **Step 5: Full suite**

Run: `go test ./... -race`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/fit.go internal/cli/fit_test.go internal/cli/root.go
git commit -m "feat(fit): discovery + per-quant fit verdict against host (with --install)"
```

---

## Task 19: README polish (Using the endpoint + Tips)

**Spec:** §6.1, §6.2.

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Identify insertion points**

```bash
grep -n "^## " README.md
```

Locate the Quick Start section end and the License section start.

- [ ] **Step 2: Insert "Using the endpoint" between Quick Start and Commands**

Paste the markdown block from spec §6.1 verbatim. Use single backticks for inline code; triple backticks with `python`/`js` language tags for code blocks.

- [ ] **Step 3: Insert "Tips" before License**

Paste the markdown block from spec §6.2 verbatim.

- [ ] **Step 4: Verify local render**

```bash
# Sanity-check with a markdown previewer if available, else just eyeball it.
head -120 README.md
```

- [ ] **Step 5: Commit**

```bash
git add README.md
git commit -m "docs(readme): add 'Using the endpoint' and 'Tips' sections"
```

---

## Task 20: Final code review, merge, tag v1.2.0, release

This task is for the orchestrator (NOT a single implementer subagent — it spans the whole branch).

- [ ] **Step 1: Dispatch a final cross-cutting code review**

Use the code-reviewer agent (model=opus) on the diff `main...phase5-fit-and-fixes`. Prompt should:
- Highlight any task-to-task inconsistencies (e.g., a function renamed in one task but old name used in a later task)
- Flag any TDD shortcuts where the failing-test step was skipped
- Verify the `fit` command's table renders correctly with edge inputs (empty rows, single row, very long repo names)

- [ ] **Step 2: Apply review fixes (if any)**

Each review fix = one commit on `phase5-fit-and-fixes`. If a fix is non-trivial, ask the orchestrator first.

- [ ] **Step 3: Live smoke on Apple M5 host (spec §9)**

```bash
# All commands run from a fresh terminal — do NOT use the build under test
# from an existing serve. Stop everything first:
llamactl status
llamactl stop --all 2>/dev/null

# Build and install locally:
go install ./cmd/llamactl
which llamactl
llamactl --version  # confirms v1.2.0-dev or similar

# Smoke 1: fit lookup
llamactl fit gemma 4
# Expected: ranked table; at least one ✓ row.

# Smoke 2: fit --install
llamactl fit gemma 4 --install
# Expected: starts download, completes, model appears in `llamactl list`.

# Smoke 3: ParamsCount fix
go run ./cmd/gguf-inspect ~/.local/share/llama-models/qwen2.5-3b-instruct/Q5_K_M.gguf | grep -i params
# Expected: a non-zero number.

# Smoke 4: cache prune
ls ~/.cache/llamactl/hf-* | head -5
llamactl cache prune --all
ls ~/.cache/llamactl/hf-* 2>&1
# Expected: empty (or all subdirs empty).

# Smoke 5: doctor count
llamactl doctor | grep -c '\['  # rough check; or just eyeball
# Expected: 12 checks.

# Smoke 6: log rotation
echo 'force rotation' >> ~/Library/Logs/llamactl/test.log
dd if=/dev/zero bs=1m count=11 >> ~/Library/Logs/llamactl/test.log
llamactl serve qwen2.5-3b-instruct  # or whatever's installed
# Then in another shell:
ls ~/Library/Logs/llamactl/test.log*
# Expected: test.log.1 appears.
```

**If any smoke fails: STOP. Investigate. Fix on `phase5-fit-and-fixes`. Do NOT merge until all smokes pass.**

Phase 2.5 caught 2 bugs and Phase 4 caught 1 bug only at the live-smoke stage. Treat this step as load-bearing.

- [ ] **Step 4: Merge to main**

```bash
git checkout main
git merge --no-ff phase5-fit-and-fixes -m "Merge phase5-fit-and-fixes: fit command + 19-item backlog drain (v1.2.0)"
```

- [ ] **Step 5: Tag and push**

```bash
git tag -a v1.2.0 -m "v1.2.0: fit command + backlog drain"
git push origin main
git push origin v1.2.0
```

- [ ] **Step 6: Watch release workflow**

```bash
gh run watch
# When green, verify the cask PR was opened/merged:
gh pr list --repo gregmundy/homebrew-tap | head
```

- [ ] **Step 7: Verify brew upgrade in <30s**

```bash
time brew upgrade llamactl
llamactl --version
# Expected: v1.2.0; elapsed < 30s.
```

---

## Task 21: Update project_state memory

**Files:**
- Modify: `/Users/greg/.claude/projects/-Users-greg-Development-llamactl/memory/project_state.md`

- [ ] **Step 1: Append a "Phase 5 (fit + backlog drain) shipped …" section to project_state.md**

Record:
- Date shipped, tag, merge commit
- What landed (one-line each: fit command, GGUF parser fix, log rotation, cache prune, doctor goes 10→12, etc.)
- Any bugs the live smoke caught and how they were fixed
- New deferred concerns surfaced (if any)
- Move "RESOLVED" items out of the deferred list

Keep the file under the same structure as existing entries.

- [ ] **Step 2: Update MEMORY.md index pointer**

The pointer line should now read something like:

```md
- [llamactl project state](project_state.md) — v1.2.0 shipped 2026-05-12 (fit + backlog drain)
```

- [ ] **Step 3: Commit memory (NOTE: this lives outside the project repo)**

Memory files live in `~/.claude/projects/.../memory/` — they are managed by the auto-memory system, not by git in `~/Development/llamactl`. Just save the files; no commit needed.

---

## Task 22: Wrap-up summary

- [ ] **Step 1: Post a final summary to the user**

One short message:
- v1.2.0 tagged and live via Homebrew
- All 22 tasks shipped
- Smoke results (each AC ✅ / ❌)
- Any deferred concerns added to project_state.md

That's the end of Phase 5.

---

## Self-review checklist (orchestrator runs before handoff)

- **Spec coverage:** All 8 bug items (4.1–4.8), 5 hygiene items (5.1–5.5), 4 refactors (7.1–7.4), 2 README items (6.1–6.2), the fit command (§3), plus release/memory mechanics — covered by Tasks 1–22.
- **Placeholder scan:** All steps have concrete code or shell commands. The `buildFitTestDeps` helper in Task 18 is the one "see existing patterns" stub — acceptable because the existing patterns are well-established in this repo. If the implementer struggles, they should ask the orchestrator.
- **Type consistency:** `KVCacheGB(arch Arch, paramsB float64, ctx int) float64` and `ParseParamCountFromRepo(repo string) float64` consistent across Tasks 18a → 18b. `RotateIfLarge(path string, maxBytes int64, keep int) (bool, error)` consistent across Tasks 12 spec → impl. `FakeRunner` field names consistent across Tasks 14 → all migration sites.
- **Sequence safety:** Task 14 (fakeRunner unify) lands AFTER Tasks 1–13 to minimize merge-conflict surface across test files. Task 16 (stdin→dir rename) is near the end for the same reason. Task 18 (fit) depends on 1 (gguftest) and 2 (parser fix); both land first.

---

## Branch discipline reminder (read before dispatching any implementer)

Every implementer subagent must be told **explicitly**:

> "You are on branch `phase5-fit-and-fixes`. Do not `git checkout`, `git switch`, `git stash`, `git reset`, or any branch-changing operation. If `git status` shows unexpected files, stop and ask. Your task is exactly Task N below; do not start Task N+1."

Skipping this primer in a subagent prompt is how Phase 3 lost an afternoon to silent branch switches.
