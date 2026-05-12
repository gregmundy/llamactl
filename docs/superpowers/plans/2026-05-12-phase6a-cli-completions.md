# Phase 6a: CLI completions + backlog drain — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the two outstanding PRD CLI line items (`update`, `config`), add opt-in endpoint authentication, drain 13 small backlog items from Phase 5 testing. Ships as v1.3.0.

**Architecture:** Single feature branch `phase6a-completions` off `main`. No new packages. Three new files in `internal/cli/` (config.go, update.go, version.go). Additive changes to `config.Config`, `Deps`, `internal/launchd/ports.go`. Same GoReleaser → cask → brew upgrade distribution path.

**Tech Stack:** Go 1.26.2, cobra, stdlib `encoding/json` / `encoding/yaml` / `net/http` / `reflect`, GoReleaser, Homebrew tap.

**Reference:** `docs/superpowers/specs/2026-05-12-phase6a-cli-completions-design.md` (the approved spec — read alongside this plan).

---

## How to use this plan

- All 24 tasks land on a single feature branch `phase6a-completions`. **Implementers must stay on this branch — do not `git checkout`, `git switch`, or `git stash` for any reason.**
- Each task = one commit. Run `go test ./... -race` after each task; commit only when green.
- Tasks are ordered: small/safe bugs first (#1-8), then the float64 type migration (#9-12), then `fit`/`list` improvements (#12-13), then the `config` foundation (#14-16), then features built on it (`auth` #17-19, `update` #20-23), docs (#24).
- Spec section references like "(spec §6.1)" point to the approved spec — read for *why*. This plan is the *how*.

---

## File structure overview

**New files:**
- `internal/cli/config.go` + `_test.go` (Task 16)
- `internal/cli/update.go` + `_test.go` (Task 22)
- `internal/cli/version.go` + `_test.go` (Task 21)

**Modified files (high-touch):**
- `internal/config/config.go` (Tasks 14, 15 — Save + APIKey field)
- `internal/cli/deps.go` (Tasks 5, 15 — UserHomeDir + Config)
- `internal/cli/serve.go` (Tasks 4, 5, 17 — clock symmetry, UserHomeDir, APIKey)
- `internal/cli/doctor.go` (Tasks 8, 19, 23 — port-conflict fix, auth check, version check)
- `internal/cli/root.go` (Tasks 2, 16, 22 — SilenceUsage, register commands)
- `internal/cli/list.go` (Tasks 9, 12 — float64, self-heal)
- `internal/cli/fit.go` (Tasks 11, 13 — min-bytes, popularity rank)
- `internal/models/metadata.go` (Task 9 — ParamsB float64)
- `internal/models/whitelist.go` (Tasks 9, 10 — float literals, new IDs)
- `internal/models/quants.go` (Task 10 — new rows, ArchQwen3)
- `internal/models/arch.go` (Task 10 — ArchQwen3)
- `internal/models/selector.go` (Task 9 — int(round) at lookup)
- `internal/cli/add.go` (Task 9 — float64 ParamsB)
- `internal/download/download.go` (Task 6 — sentinel error)
- `internal/launchd/ports.go` (Task 18 — HasAPIKey + HasHost helpers)
- `internal/hf/cache.go` (Task 7 — GCEmptyNamespaces)
- `internal/cli/cache.go` (Task 7 — invoke GC after prune)
- `.github/workflows/ci.yml` (Task 1)
- `.github/workflows/release.yml` (Tasks 1, 20)
- `.goreleaser.yml` (Task 20 — ldflags)
- `README.md` (Task 24)
- `docs/llamactl-prd-v1.5.md` (Task 24 — api_key key documented)
- `cmd/llamactl/main.go` (Tasks 5, 15 — wire UserHomeDir, Config)

---

## Branch discipline (read before dispatching any implementer)

Every implementer subagent must be told **explicitly**:

> "You are on branch `phase6a-completions`. Do not `git checkout`, `git switch`, `git stash`, `git reset`, or any branch-changing operation. If `git status` shows unexpected files, stop and ask. Your task is exactly Task N below; do not start Task N+1."

Skipping this primer is how Phase 3 lost an afternoon to silent branch switches. Phase 5 used it consistently and had zero branch issues across 25+ implementer dispatches.

---

## Task 1: Bump GitHub Actions to Node 24-compatible versions

**Spec:** §6.1.

**Files:**
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release.yml`

- [ ] **Step 1: Inventory current pinned versions**

```bash
grep -n "uses:" .github/workflows/ci.yml .github/workflows/release.yml
```

Expected current actions:
- `actions/checkout@v4`
- `actions/setup-go@v5`
- `goreleaser/goreleaser-action@v6`

- [ ] **Step 2: Determine latest Node-24-compatible versions**

Each action's latest Node-24-ready version (as of 2026-05-12):
- `actions/checkout@v5` (Node 24 by default)
- `actions/setup-go@v6` (Node 24 by default)
- `goreleaser/goreleaser-action@v7` (Node 24 by default)

If a newer version is available at implementation time, prefer it. Verify via `https://github.com/<owner>/<repo>/releases` if uncertain.

- [ ] **Step 3: Update both workflow files**

Replace each `uses:` line with the new version. Confirm no other lines reference the old major versions.

- [ ] **Step 4: Sanity-check workflow YAML**

```bash
git diff .github/workflows/ | head -30
```

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml .github/workflows/release.yml
git commit -m "$(cat <<'EOF'
build(ci): bump actions to Node 24-compatible versions

GitHub Actions Node 20 is deprecated; forced default switch to Node 24
on June 2, 2026 and Node 20 removed September 16, 2026. Bumps:
- actions/checkout@v4 → @v5
- actions/setup-go@v5 → @v6
- goreleaser/goreleaser-action@v6 → @v7

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

The verification happens at merge time when the workflows actually run.

---

## Task 2: Cobra SilenceUsage propagation fix

**Spec:** §6.3.

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go`

The root cobra has `SilenceUsage: true` but children don't inherit it; failing subcommands still print usage.

- [ ] **Step 1: Locate the root command construction**

```bash
grep -n "SilenceUsage\|NewRoot\|rootCmd.AddCommand" internal/cli/root.go
```

- [ ] **Step 2: Write the failing test**

`internal/cli/root_test.go` — add:

```go
func TestSubcommandErrorsDoNotPrintUsage(t *testing.T) {
    deps := minimalDeps(t)
    var out, errBuf bytes.Buffer
    root := NewRoot(deps, "dev")
    root.SetOut(&out)
    root.SetErr(&errBuf)
    // Use a guaranteed-fail command: `add` with no args fails arg-validation.
    root.SetArgs([]string{"add"})
    _ = root.Execute()
    combined := out.String() + errBuf.String()
    if strings.Contains(combined, "Usage:") {
        t.Fatalf("subcommand error printed usage:\n%s", combined)
    }
}
```

(If `minimalDeps` doesn't exist in the test file, follow the existing test scaffolding pattern — most root_test.go variants build a minimal `Deps` for command-shape tests. If you can't find it, build inline.)

Run: `go test ./internal/cli/... -run TestSubcommandErrorsDoNotPrintUsage -v` → expect FAIL.

- [ ] **Step 3: Implement the fix**

In `internal/cli/root.go`, propagate `SilenceUsage` to every child as they're added. Either:

**Option A (mechanical):** for each `rootCmd.AddCommand(newXxxCmd(d))` line, wrap:
```go
addSilent(rootCmd, newAddCmd(d))
```
with helper:
```go
func addSilent(parent, child *cobra.Command) {
    child.SilenceUsage = true
    parent.AddCommand(child)
}
```

**Option B (single-point):** after all `AddCommand` calls, walk children and set the flag:
```go
for _, sub := range rootCmd.Commands() {
    sub.SilenceUsage = true
    // Recurse for nested subcommands (cache prune, config get/set, etc.)
    for _, grand := range sub.Commands() {
        grand.SilenceUsage = true
    }
}
```

Option B is one place to maintain. Use B.

- [ ] **Step 4: Run tests**

```bash
go test ./... -race
```
Expected: PASS, including the new TestSubcommandErrorsDoNotPrintUsage.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "$(cat <<'EOF'
fix(root): propagate SilenceUsage to all subcommands

Cobra's SilenceUsage on the root doesn't auto-propagate; failing
subcommands (e.g., `add` with no args) printed usage to stderr,
cluttering scripts. Walk the command tree at construction time and
set SilenceUsage on each child + grandchild.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Foreground integration test

**Spec:** §6.2.

**Files:**
- Modify: `internal/cli/integration_test.go`

Closes a Phase 3 plan §10.2 gap: the fake llama-server at `internal/cli/testdata/fakellamaserver/main.go` is built but only used as a resolver target. Wire a true foreground integration test.

- [ ] **Step 1: Read existing integration tests for the pattern**

```bash
grep -n "TestIntegration\|fakellamaserver" internal/cli/integration_test.go | head -10
```

Identify how the detached integration test invokes the fake binary and how it sets up Deps.

- [ ] **Step 2: Write the new foreground test**

Append to `internal/cli/integration_test.go`:

```go
func TestIntegrationForegroundServe(t *testing.T) {
    if testing.Short() {
        t.Skip("integration test")
    }
    // Build the fake llama-server binary into the tempDir; the existing
    // integration_test.go has helpers for this (e.g., buildFakeServer or similar).
    // If not, use go run via os/exec inline.
    tempDir := t.TempDir()
    fakeServer := buildFakeLlamaServer(t, tempDir) // helper defined in this file or sibling test file

    // Set up a Deps that resolves llama-server to the fake binary.
    deps := buildIntegrationDeps(t, tempDir, fakeServer) // existing pattern; adapt

    // Pre-install a model so `serve` has something to load. Mirror what the
    // detached integration test does for fixture setup.
    seedInstalledModel(t, deps, "qwen2.5-3b-instruct") // existing helper

    // Capture stdout + stderr; run `serve qwen2.5-3b-instruct` foreground.
    var stdout, stderr bytes.Buffer
    deps.Stdout = &stdout
    deps.Stderr = &stderr

    // Use context.WithCancel to terminate the serve after we see startup.
    ctx, cancel := context.WithCancel(context.Background())
    done := make(chan error, 1)
    go func() {
        done <- runServeForeground(ctx, deps, "qwen2.5-3b-instruct", 0, "chat")
    }()

    // Wait for the fake server to emit its "ready" line (the fake binary
    // prints something stable on stdin/stdout). Adjust the marker string
    // to match what testdata/fakellamaserver/main.go actually prints.
    waitForOutput(t, &stdout, "fake-server: ready", 5*time.Second)

    cancel()
    err := <-done
    // SIGTERM-on-cancel + 5s WaitDelay (Phase 4) means we expect context.Canceled
    // or a clean exit; not a kill signal.
    if err != nil && !errors.Is(err, context.Canceled) {
        t.Fatalf("foreground serve exit error: %v", err)
    }
    if !strings.Contains(stdout.String(), "fake-server: ready") {
        t.Fatalf("missing ready marker in stdout:\n%s", stdout.String())
    }
}

func waitForOutput(t *testing.T, buf *bytes.Buffer, marker string, timeout time.Duration) {
    t.Helper()
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        if strings.Contains(buf.String(), marker) {
            return
        }
        time.Sleep(20 * time.Millisecond)
    }
    t.Fatalf("timed out waiting for %q in:\n%s", marker, buf.String())
}
```

**Implementer note:** the exact helper names (`buildFakeLlamaServer`, `buildIntegrationDeps`, `seedInstalledModel`) depend on what the existing detached integration test already uses. Read the file first, mirror its patterns. If a helper doesn't exist, build it inline rather than refactoring out-of-scope.

Verify what `testdata/fakellamaserver/main.go` actually prints to stdout — adjust the marker string accordingly. Run:

```bash
go run ./internal/cli/testdata/fakellamaserver --port 8080 --model /dev/null &
sleep 0.5
# Note the output; kill the process.
```

- [ ] **Step 3: Run the new test**

```bash
go test ./internal/cli/... -race -run TestIntegrationForegroundServe -v
```

Expected: PASS. If it fails on missing helpers, build them inline.

- [ ] **Step 4: Run full suite**

```bash
go test ./... -race
```

- [ ] **Step 5: Commit**

```bash
git add internal/cli/integration_test.go
git commit -m "$(cat <<'EOF'
test(integration): add foreground serve round-trip

Closes a Phase 3 plan §10.2 gap. The fake llama-server binary at
testdata/fakellamaserver was built but only used as a resolver target;
this test exercises the full foreground serve path with ctx-cancel
shutdown.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

If you hit BLOCKED on helper functions or the fake server output format: stop and ask the orchestrator.

---

## Task 4: Detached-poll clock symmetry

**Spec:** §6.8.

**Files:**
- Modify: `internal/cli/deps.go` (add Sleep seam)
- Modify: `internal/cli/serve.go` (use seam)
- Modify: `internal/cli/serve_test.go`
- Modify: `cmd/llamactl/main.go` (wire `time.After`)

Today's `runServeDetached` uses `d.Now()` for "have 5s passed yet" but `time.After` for the timer. Frozen-clock tests would hang.

- [ ] **Step 1: Add Sleep seam to Deps**

In `internal/cli/deps.go`:

```go
type Deps struct {
    // ...existing fields...
    Now   func() time.Time
    Sleep func(d time.Duration) <-chan time.Time  // defaults to time.After; tests override
}
```

- [ ] **Step 2: Wire in production**

In `cmd/llamactl/main.go`, where `deps.Now = time.Now` is set, add:

```go
deps.Sleep = time.After
```

- [ ] **Step 3: Locate the poll loop in serve.go**

```bash
grep -n "time.After\|detachPollInterval\|d.Now" internal/cli/serve.go
```

The Phase 5 fix uses:
```go
select {
case <-ctx.Done():
    return ctx.Err()
case <-time.After(detachPollInterval):
}
```

- [ ] **Step 4: Replace `time.After` with `d.Sleep`**

```go
select {
case <-ctx.Done():
    return ctx.Err()
case <-d.Sleep(detachPollInterval):
}
```

(Use `d.Sleep` or `deps.Sleep` — match the variable name in the existing function.)

- [ ] **Step 5: Write the failing test**

In `internal/cli/serve_test.go`, add:

```go
func TestRunServeDetachedFrozenClockBreaksAtDeadline(t *testing.T) {
    // Fake Now: returns t0 first, then t0+10s on second call (past 5s deadline).
    // Fake Sleep: returns an already-closed channel so the select picks
    // the timer branch immediately, advancing the loop.
    var nowCalls int
    t0 := time.Now()
    deps := minimalServeDeps(t)
    deps.Now = func() time.Time {
        nowCalls++
        if nowCalls > 1 {
            return t0.Add(10 * time.Second)
        }
        return t0
    }
    deps.Sleep = func(d time.Duration) <-chan time.Time {
        ch := make(chan time.Time, 1)
        ch <- t0 // immediate
        return ch
    }
    // LaunchdService.Print returns PID=0 always (service never starts).
    // Call runServeDetached (or whatever name). It should give up after
    // hitting the deadline, NOT hang.
    done := make(chan error, 1)
    go func() {
        done <- runServeDetached(context.Background(), deps, /* args */)
    }()
    select {
    case err := <-done:
        // Expect a deadline-exceeded-ish error, not nil
        if err == nil {
            t.Fatalf("expected error when service never starts")
        }
    case <-time.After(1 * time.Second):
        t.Fatal("runServeDetached hung")
    }
}
```

Adapt `minimalServeDeps` and the `runServeDetached` invocation to whatever signature actually exists. If frozen-clock semantics interact poorly with other parts of the function, simplify the test scope.

Run: FAIL (current code uses real `time.After`, so the frozen Now alone doesn't advance through the deadline check).

- [ ] **Step 6: Verify the test passes after Step 4's fix**

Run: `go test ./internal/cli/... -race -run TestRunServeDetached -v` → PASS.

- [ ] **Step 7: Full suite + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/deps.go internal/cli/serve.go internal/cli/serve_test.go cmd/llamactl/main.go
git commit -m "$(cat <<'EOF'
refactor(serve): inject Sleep seam for detached-poll clock symmetry

runServeDetached's deadline check used d.Now() but the timer used the
real-time time.After — frozen-clock tests would hang. Add Deps.Sleep
seam (defaults to time.After in production), use it consistently with
d.Now. No production behavior change.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: UserHomeDir test-injectability

**Spec:** §6.9.

**Files:**
- Modify: `internal/cli/deps.go`
- Modify: `internal/cli/serve.go` (replace `os.UserHomeDir` call)
- Modify: `internal/cli/serve_test.go`
- Modify: `cmd/llamactl/main.go` (wire `os.UserHomeDir`)

- [ ] **Step 1: Add field to Deps**

In `internal/cli/deps.go`:

```go
type Deps struct {
    // ...existing fields...
    UserHomeDir func() (string, error)  // defaults to os.UserHomeDir; tests override
}
```

- [ ] **Step 2: Wire in production**

`cmd/llamactl/main.go`:
```go
deps.UserHomeDir = os.UserHomeDir
```

- [ ] **Step 3: Find and replace inline os.UserHomeDir calls**

```bash
grep -n "os.UserHomeDir" internal/cli/*.go
```

Each call in `internal/cli/*.go` (not tests) should become `d.UserHomeDir()`. Likely just one or two sites in `serve.go`'s `runServeDetached`.

- [ ] **Step 4: Failing test**

In `internal/cli/serve_test.go`:

```go
func TestRunServeDetachedUsesInjectedHomeDir(t *testing.T) {
    tempHome := t.TempDir()
    deps := minimalServeDeps(t)
    deps.UserHomeDir = func() (string, error) { return tempHome, nil }
    // Arrange a model + run runServeDetached. Existing patterns establish
    // the rest of the fixture.
    // ...
    // Assert: plist was written under tempHome (not the real $HOME).
    plistPath := filepath.Join(tempHome, "Library/LaunchAgents/com.llamactl.test.plist")
    if _, err := os.Stat(plistPath); err != nil {
        t.Fatalf("plist not under injected home: %v", err)
    }
}
```

If the existing serve tests already populate `Deps.LaunchAgentsDir` directly (bypassing the home-dir resolution), the test should assert that `runServeDetached`'s INTERNAL use of `UserHomeDir` for WorkingDir defaults uses the injected value. Adapt to the actual code path.

Run: FAIL until step 3 lands.

- [ ] **Step 5: Tests + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/deps.go internal/cli/serve.go internal/cli/serve_test.go cmd/llamactl/main.go
git commit -m "$(cat <<'EOF'
refactor(serve): inject UserHomeDir via Deps

runServeDetached called os.UserHomeDir() directly, which fought tests
that wanted a tempDir-rooted fake home. Add Deps.UserHomeDir (defaults
to os.UserHomeDir in production), use it consistently.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Download in-progress sentinel error

**Spec:** §6.11.

**Files:**
- Modify: `internal/download/download.go`
- Modify: `internal/cli/add.go` (use sentinel where applicable)
- Modify: `internal/cli/add_test.go` (use errors.Is)

- [ ] **Step 1: Find in-progress error sites**

```bash
grep -rn "in progress\|InProgress" internal/ --include="*.go" | head -20
```

Identify all sites that:
- Return an "in progress" error.
- Match against `"in progress"` via string contains.

- [ ] **Step 2: Export the sentinel**

In `internal/download/download.go`:

```go
import "errors"

// ErrInProgress signals that another caller is currently downloading the
// same dest path. The current caller can either back off or wait — current
// flock-based serialization automatically blocks, so ErrInProgress only
// fires for non-blocking probes.
var ErrInProgress = errors.New("download in progress")
```

(If no non-blocking probe path exists today, defer the sentinel use and just export the error. The test-side cleanup is the immediate value.)

- [ ] **Step 3: Replace string-match callers**

Wherever a test or production caller does `strings.Contains(err.Error(), "in progress")`, replace with `errors.Is(err, download.ErrInProgress)`. The corresponding return site must wrap errors with `%w` against the sentinel:

```go
return fmt.Errorf("%w: %s pending", download.ErrInProgress, repoID)
```

Search all callers:
```bash
grep -rn "in progress" internal/ --include="*.go"
```

- [ ] **Step 4: Test the round-trip**

Add to `internal/download/download_test.go`:

```go
func TestErrInProgressIsWrapped(t *testing.T) {
    base := fmt.Errorf("%w: foo pending", ErrInProgress)
    if !errors.Is(base, ErrInProgress) {
        t.Fatalf("errors.Is failed on wrapped sentinel")
    }
}
```

- [ ] **Step 5: Tests + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/download/download.go internal/download/download_test.go internal/cli/add.go internal/cli/add_test.go
git commit -m "$(cat <<'EOF'
refactor(download): export ErrInProgress sentinel; tests use errors.Is

Phase 2-era tests string-matched err.Error() for 'in progress' to detect
the concurrent-download case. Replace with errors.Is against a proper
sentinel so renaming the message won't break tests.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: HF cache namespace GC

**Spec:** §6.10.

**Files:**
- Modify: `internal/hf/cache.go`
- Modify: `internal/hf/cache_test.go`
- Modify: `internal/cli/cache.go` (call from prune)
- Modify: `internal/cli/cache_test.go`

- [ ] **Step 1: Write failing test for `GCEmptyNamespaces`**

`internal/hf/cache_test.go` — append:

```go
func TestCacheGCEmptyNamespaces(t *testing.T) {
    dir := t.TempDir()
    // Populate hf-old/ (empty namespace) and hf-new/file.json (non-empty).
    if err := os.MkdirAll(filepath.Join(dir, "hf-old"), 0o755); err != nil {
        t.Fatal(err)
    }
    if err := os.MkdirAll(filepath.Join(dir, "hf-new"), 0o755); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(dir, "hf-new", "x.json"), []byte("{}"), 0o644); err != nil {
        t.Fatal(err)
    }
    c := NewCache(dir)
    if err := c.GCEmptyNamespaces(); err != nil {
        t.Fatal(err)
    }
    if _, err := os.Stat(filepath.Join(dir, "hf-old")); !os.IsNotExist(err) {
        t.Fatal("hf-old should be removed")
    }
    if _, err := os.Stat(filepath.Join(dir, "hf-new")); err != nil {
        t.Fatal("hf-new should still exist")
    }
}

func TestCacheGCMissingRoot(t *testing.T) {
    c := NewCache("/definitely-does-not-exist/llamactl-test")
    if err := c.GCEmptyNamespaces(); err != nil {
        t.Fatalf("missing root should not error: %v", err)
    }
}
```

Run: FAIL.

- [ ] **Step 2: Implement**

In `internal/hf/cache.go`:

```go
// GCEmptyNamespaces removes empty subdirectories of the cache root.
// Useful after PruneOlderThan or cache prune --all leaves namespace dirs
// (e.g., "hf-repo-v1") drained. Missing root is not an error.
func (c *Cache) GCEmptyNamespaces() error {
    entries, err := os.ReadDir(c.root)
    if err != nil {
        if os.IsNotExist(err) {
            return nil
        }
        return err
    }
    for _, e := range entries {
        if !e.IsDir() {
            continue
        }
        nsPath := filepath.Join(c.root, e.Name())
        children, err := os.ReadDir(nsPath)
        if err != nil {
            continue
        }
        if len(children) == 0 {
            _ = os.Remove(nsPath)
        }
    }
    return nil
}
```

Run: `go test ./internal/hf/... -v` → PASS.

- [ ] **Step 3: Wire into `cache prune`**

In `internal/cli/cache.go`, at the end of `runCachePrune` (after the count is printed):

```go
// Best-effort GC of namespace dirs that pruning may have emptied.
cache := hf.NewCache(d.HFCacheDir)
_ = cache.GCEmptyNamespaces()
```

(Import `github.com/gregmundy/llamactl/internal/hf` if not already.)

Add a test in `internal/cli/cache_test.go`:

```go
func TestCachePruneAllAlsoGCsEmptyNamespaces(t *testing.T) {
    dir := t.TempDir()
    os.MkdirAll(filepath.Join(dir, "hf-old"), 0o755)
    os.MkdirAll(filepath.Join(dir, "hf-new"), 0o755)
    os.WriteFile(filepath.Join(dir, "hf-new", "x.json"), []byte("{}"), 0o644)
    d := &Deps{HFCacheDir: dir, Stdout: io.Discard, Stderr: io.Discard}
    cmd := newCacheCmd(d)
    cmd.SetArgs([]string{"prune", "--all"})
    if err := cmd.Execute(); err != nil {
        t.Fatal(err)
    }
    if _, err := os.Stat(filepath.Join(dir, "hf-old")); !os.IsNotExist(err) {
        t.Fatal("hf-old (empty namespace) should be GC'd after prune --all")
    }
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/hf/cache.go internal/hf/cache_test.go internal/cli/cache.go internal/cli/cache_test.go
git commit -m "$(cat <<'EOF'
feat(cache): GC empty namespace dirs after prune

After namespace bumps (hf-repo → hf-repo-v2), the old namespace dir
lingered empty. GCEmptyNamespaces removes empty namespace subdirs of
the cache root. Invoked from `cache prune` and `cache prune --all`.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Doctor port-conflict false positive fix

**Spec:** §6.6.

**Files:**
- Modify: `internal/cli/doctor.go` (portConflictsCheck)
- Modify: `internal/cli/doctor_test.go`

Live testing showed: after `llamactl stop gemma-4-e4b-it`, `doctor` reports `✗ port conflicts — gemma-4-e4b-it loaded but port 8082 is free`. The check fires on stale plist or stale launchd state.

- [ ] **Step 1: Read the current check**

```bash
sed -n '273,310p' internal/cli/doctor.go
```

(That range covers `portConflictsCheck` based on the orchestrator's grep earlier.)

Understand the existing logic. Likely it:
1. Enumerates services via `LaunchdService.List`.
2. For each service, tries to bind the port.
3. If the port is free but the service "exists", flags conflict.

The bug is in step 1 vs reality: `stop` invokes `launchctl bootout` which should make `List` not return the service, but the plist file may still be on disk. Or `Print` returns a stale entry.

Investigation expected to find: the check enumerates plist files (not running services). After `stop`, the plist file is deleted by `stop.go`. But there might be a race or an "orphan plist" state.

OR: the check uses `LaunchdService.List` (post-Phase 3 helper) which reads launchctl entries, and a stopped service may appear with PID=0 but still in the list.

- [ ] **Step 2: Write the failing test**

`internal/cli/doctor_test.go` — add:

```go
func TestPortConflictsCheckIgnoresStoppedServices(t *testing.T) {
    deps := minimalDoctorDeps(t)
    // Fake LaunchdService.List returns one entry with PID=0 (stopped).
    deps.LaunchdService = &fakeLaunchdServiceList{
        services: []launchd.ServiceInfo{
            {Label: "com.llamactl.test-model", PID: 0},
        },
    }
    check := portConflictsCheck(deps)
    ok, detail := check.run(context.Background(), deps)
    if !ok {
        t.Fatalf("expected ✓ for stopped service; got detail=%q", detail)
    }
}

type fakeLaunchdServiceList struct {
    services []launchd.ServiceInfo
}

func (f *fakeLaunchdServiceList) Load(ctx context.Context, p string) error    { return nil }
func (f *fakeLaunchdServiceList) Bootout(ctx context.Context, l string) error { return nil }
func (f *fakeLaunchdServiceList) Print(ctx context.Context, l string) (launchd.ServiceInfo, error) {
    return launchd.ServiceInfo{}, nil
}
func (f *fakeLaunchdServiceList) List(ctx context.Context) ([]launchd.ServiceInfo, error) {
    return f.services, nil
}
```

Run: FAIL.

- [ ] **Step 3: Fix the check**

In `portConflictsCheck`, before treating a service as "loaded", filter on PID:

```go
for _, svc := range services {
    if svc.PID <= 0 {
        continue  // stopped/never-loaded service; not a port conflict candidate
    }
    // ...existing logic that checks the port...
}
```

(Adapt to actual code — the existing loop variable name might differ.)

If the check currently enumerates plist files directly (not via `LaunchdService.List`), the fix is different: call `LaunchdService.Print(ctx, label)` for each plist's label, skip those returning PID=0.

- [ ] **Step 4: Verify + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/doctor.go internal/cli/doctor_test.go
git commit -m "$(cat <<'EOF'
fix(doctor): port-conflict check skips stopped (PID=0) services

After `llamactl stop <id>`, doctor was reporting "<id> loaded but port
N is free" — false positive caused by enumerating plist/launchctl entries
without checking liveness. Skip services whose PID is 0.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: `ParamsB int → float64` migration

**Spec:** §6.7.

**Files:**
- Modify: `internal/models/metadata.go`
- Modify: `internal/models/whitelist.go`
- Modify: `internal/models/selector.go`
- Modify: `internal/cli/add.go`
- Modify: `internal/cli/list.go`
- Modify: `internal/models/whitelist_test.go`, `selector_test.go`, etc.

This is a type change rippled across the models package. Mechanical, but every callsite touches.

- [ ] **Step 1: Inventory current usage**

```bash
grep -rn "ParamsB\b" --include="*.go" internal/ cmd/
```

Note every site. Categorize:
- Struct field definitions
- Struct literals (PreferredIDs entries, test fixtures)
- Lookups (`QuantSizeTable[m.ParamsB]`)
- Display (`fmt.Sprintf("%dB", m.ParamsB)`)
- Arithmetic (`ParamsB / 1e9`, etc.)

- [ ] **Step 2: Write the migration test FIRST (drives confidence)**

`internal/models/metadata_test.go` — add or extend:

```go
func TestMetadataParamsBJSONBackwardsCompat(t *testing.T) {
    // Old metadata file stored ParamsB as integer; ensure deserialization
    // into float64 field works without migration.
    raw := []byte(`{"id":"foo","params_b":3,"arch":"qwen2.5","quant":"Q5_K_M"}`)
    var m Metadata
    if err := json.Unmarshal(raw, &m); err != nil {
        t.Fatal(err)
    }
    if m.ParamsB != 3.0 {
        t.Fatalf("ParamsB=%v, want 3.0", m.ParamsB)
    }
}

func TestMetadataParamsBFractionalRoundTrip(t *testing.T) {
    m := Metadata{ID: "qwen3-0.6b", ParamsB: 0.6, Arch: "qwen3"}
    raw, err := json.Marshal(m)
    if err != nil {
        t.Fatal(err)
    }
    var back Metadata
    if err := json.Unmarshal(raw, &back); err != nil {
        t.Fatal(err)
    }
    if back.ParamsB != 0.6 {
        t.Fatalf("round-trip ParamsB=%v, want 0.6", back.ParamsB)
    }
}
```

Run: FAIL (compile error — `ParamsB` is currently `int`, can't assign 3.0 directly).

- [ ] **Step 3: Type change**

In `internal/models/metadata.go`:
```go
type Metadata struct {
    // ...
    ParamsB float64 `json:"params_b"`
    // ...
}
```

In `internal/models/whitelist.go`, `type Model`:
```go
type Model struct {
    // ...
    ParamsB float64
    // ...
}
```

- [ ] **Step 4: Update PreferredIDs entries**

`internal/models/whitelist.go` — existing entries with `ParamsB: 3, 7, 14, 70, 8` etc. don't strictly need rewriting (Go accepts integer literals for float64 fields), but for clarity update them to `3.0, 7.0` etc. **Optional but recommended for readability.** If skipping, note the choice in the commit message.

- [ ] **Step 5: Fix the QuantSizeTable lookup**

In `internal/models/selector.go`, where the lookup occurs:

```go
import "math"
// ...
sizeRow, ok := QuantSizeTable[int(math.Round(model.ParamsB))]
```

Verify the existing variable name (`model.ParamsB` vs `m.ParamsB`).

- [ ] **Step 6: Fix `cli/add.go`'s truncation**

`internal/cli/add.go` line ~152:
```go
paramsB := float64(header.ParamsCount) / 1e9
```

The variable type must be `float64`. Verify the surrounding context expects that.

- [ ] **Step 7: Fix `cli/list.go`'s display**

```bash
grep -n "ParamsB\|PARAMS" internal/cli/list.go
```

Change the format directive. Two reasonable choices:
- `fmt.Fprintf(w, "%g B", m.ParamsB)` — "3 B", "0.6 B", "7.5 B" (drops trailing .0 cleanly)
- `fmt.Fprintf(w, "%.1f B", m.ParamsB)` — always one decimal: "3.0 B", "0.6 B"

Go with `%g` for terser output. Verify existing tests' expected output strings and update them.

- [ ] **Step 8: Fix every other ParamsB-mentioning file**

Run `grep` again to verify nothing's broken:
```bash
grep -rn "ParamsB\b" --include="*.go" internal/ cmd/
```

For each site:
- `int` arithmetic? Convert to float64 (likely `float64(x) * y` patterns are already fine).
- Lookups in maps? Wrap with `int(math.Round(...))`.

`whitelist_test.go`, `selector_test.go`, `add_test.go`, `list_test.go` — fix any int literals or expected output strings that assumed int.

- [ ] **Step 9: Tests + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/models/metadata.go internal/models/whitelist.go internal/models/selector.go internal/models/metadata_test.go internal/cli/add.go internal/cli/list.go
# Plus any test files that needed string updates:
git add internal/models/whitelist_test.go internal/models/selector_test.go internal/cli/add_test.go internal/cli/list_test.go
git commit -m "$(cat <<'EOF'
refactor(models): ParamsB int → float64 (preserve sub-1B precision)

int storage truncated qwen3-0.6b to "0 B" in `list` and lost fidelity
for any sub-1B or fractional model (e.g., gemma-4-E4B at 7.5B reading
as 7). Migrate ParamsB to float64 across Metadata, Model, callers, and
test fixtures. JSON deserialization handles old int values transparently.
QuantSizeTable stays keyed by int; selector converts via math.Round at
lookup. Display uses %g for terse output.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

**If you hit BLOCKED on a callsite that needs more thought** (e.g., a hash function that uses ParamsB as int): stop and ask the orchestrator.

---

## Task 10: Add Qwen3 PreferredIDs + ArchQwen3 + QuantSizeTable rows

**Spec:** §6.7.

**Files:**
- Modify: `internal/models/arch.go`
- Modify: `internal/models/quants.go`
- Modify: `internal/models/whitelist.go`
- Modify: `internal/models/quants_test.go` (validate new entries)
- Modify: `internal/models/whitelist_test.go` (cover new IDs)

- [ ] **Step 1: Add `ArchQwen3` constant**

In `internal/models/quants.go` (next to ArchQwen25):

```go
const (
    ArchQwen25  Arch = "qwen2.5"
    ArchQwen3   Arch = "qwen3"     // NEW
    ArchLlama3  Arch = "llama3"
    ArchMistral Arch = "mistral"
)
```

In `internal/models/arch.go` `ArchFromGGUF`: add a case mapping the GGUF `general.architecture` value to `ArchQwen3`. Verify what real Qwen3 GGUFs report — likely `"qwen3"` directly. If you can't verify, look at `internal/models/whitelist_test.go::TestArchFromGGUF` for the existing mapping pattern.

- [ ] **Step 2: Add KVCachePerTokenKB row for ArchQwen3**

`internal/models/quants.go`:

```go
var KVCachePerTokenKB = map[Arch]map[Quant]float64{
    ArchQwen25:  {Q8_0: 0.5},
    ArchQwen3:   {Q8_0: 0.4},  // NEW — slightly lower than Qwen2.5 (more aggressive GQA)
    ArchLlama3:  {Q8_0: 0.5},
    ArchMistral: {Q8_0: 0.5},
}
```

- [ ] **Step 3: Add QuantSizeTable rows for 1B and 2B**

`internal/models/quants.go`:

```go
var QuantSizeTable = map[int]map[Quant]float64{
    1:  {Q5_K_M: 0.7, Q4_K_M: 0.6, Q4_K_S: 0.6, IQ4_XS: 0.5, IQ3_M: 0.5, IQ3_XS: 0.5, Q2_K: 0.4},  // NEW
    2:  {Q5_K_M: 1.4, Q4_K_M: 1.2, Q4_K_S: 1.1, IQ4_XS: 1.1, IQ3_M: 1.0, IQ3_XS: 0.9, Q2_K: 0.8},  // NEW
    3:  {Q5_K_M: 2.2, Q4_K_M: 1.9, Q4_K_S: 1.8, IQ4_XS: 1.7, IQ3_M: 1.5, IQ3_XS: 1.4, Q2_K: 1.3},
    7:  {Q5_K_M: 5.1, Q4_K_M: 4.4, Q4_K_S: 4.1, IQ4_XS: 3.8, IQ3_M: 3.3, IQ3_XS: 3.1, Q2_K: 2.7},
    8:  {Q5_K_M: 5.7, Q4_K_M: 4.9, Q4_K_S: 4.6, IQ4_XS: 4.3, IQ3_M: 3.8, IQ3_XS: 3.5, Q2_K: 3.0},
    14: {Q5_K_M: 10.4, Q4_K_M: 8.9, Q4_K_S: 8.4, IQ4_XS: 7.8, IQ3_M: 6.9, IQ3_XS: 6.4, Q2_K: 5.5},
    70: {Q5_K_M: 49.9, Q4_K_M: 42.5, Q4_K_S: 40.3, IQ4_XS: 37.7, IQ3_M: 32.9, IQ3_XS: 30.8, Q2_K: 26.4},
}
```

- [ ] **Step 4: Add Qwen3 PreferredIDs**

`internal/models/whitelist.go`:

```go
"qwen3-0.6b":     {ID: "qwen3-0.6b", HFRepo: "Qwen/Qwen3-0.6B-GGUF", Arch: ArchQwen3, ParamsB: 0.6, MaxCtx: 32768},
"qwen3-1.7b":     {ID: "qwen3-1.7b", HFRepo: "Qwen/Qwen3-1.7B-GGUF", Arch: ArchQwen3, ParamsB: 1.7, MaxCtx: 32768},
```

- [ ] **Step 5: Verify existing tests still pass**

```bash
go test ./internal/models/... -v
```

The existing `TestKVCachePerTokenKBCovered` (or equivalent) iterates over Arch constants — make sure ArchQwen3 has a row.

- [ ] **Step 6: Add a quant-selection test for sub-1B**

`internal/models/selector_test.go` — append:

```go
func TestSelectQuantSub1BModel(t *testing.T) {
    model := PreferredIDs["qwen3-0.6b"]
    info := hardware.Info{RAMBytes: 16 << 30}
    q, err := SelectQuant(model, info, 8192)
    if err != nil {
        t.Fatalf("SelectQuant: %v", err)
    }
    // 0.6B model fits in any quant on 16 GB; expect the largest (Q5_K_M).
    if q != Q5_K_M {
        t.Fatalf("got %s, want Q5_K_M", q)
    }
}

func TestSelectQuant1_7BModel(t *testing.T) {
    model := PreferredIDs["qwen3-1.7b"]
    info := hardware.Info{RAMBytes: 16 << 30}
    q, err := SelectQuant(model, info, 8192)
    if err != nil {
        t.Fatalf("SelectQuant: %v", err)
    }
    // 1.7B → rounds to 2; row 2 fits trivially in 16 GB; expect Q5_K_M.
    if q != Q5_K_M {
        t.Fatalf("got %s, want Q5_K_M", q)
    }
}
```

- [ ] **Step 7: Tests + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/models/arch.go internal/models/quants.go internal/models/whitelist.go internal/models/quants_test.go internal/models/whitelist_test.go internal/models/selector_test.go
git commit -m "$(cat <<'EOF'
feat(models): add Qwen3 PreferredIDs + QuantSizeTable rows for 1B and 2B

New PreferredIDs: qwen3-0.6b, qwen3-1.7b. New Arch constant ArchQwen3 +
KVCachePerTokenKB row (0.4 KiB/token, slightly less than Qwen2.5's 0.5
due to Qwen3's more aggressive GQA). New QuantSizeTable rows for 1B and
2B model sizes (estimates from llama.cpp filesize docs; refine with
real HF measurements post-release).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Lower `fitMinModelBytes` to 200 MiB

**Spec:** §6.7 (last paragraph).

**Files:**
- Modify: `internal/cli/fit.go`
- Modify: `internal/cli/fit_test.go`

- [ ] **Step 1: Update the constant**

`internal/cli/fit.go`:

```go
// fitMinModelBytes filters out imatrix calibration shards and other small
// auxiliary GGUFs that match the quant regex but aren't actual model weights.
// 200 MiB is below the smallest realistic Q4_K_M of a 1B model (~600 MB) but
// above typical imatrix shards (~100 MB). Phase 6a lowered from 500 MiB to
// admit sub-1B PreferredIDs (qwen3-0.6b at Q4_K_M ≈ 600 MB).
const fitMinModelBytes = 200 << 20
```

- [ ] **Step 2: Update the failing-test expectation**

`internal/cli/fit_test.go` — find `TestFitSkipsTinyAuxiliaryFiles`:

```go
func TestFitSkipsTinyAuxiliaryFiles(t *testing.T) {
    hits := []hf.SearchHit{{ID: "user/some-model-GGUF"}}
    repos := map[string]hf.Repo{
        "user/some-model-GGUF": {Siblings: []hf.File{
            // Imatrix shard at 100 MiB — should be filtered (below 200 MiB floor).
            {RFilename: "imatrix-Q4_K_M.gguf", LFS: &hf.LFSInfo{Size: 100 << 20, SHA256: "a"}},
            // Real sub-1B model at 600 MB — should appear.
            {RFilename: "model-Q4_K_M.gguf", LFS: &hf.LFSInfo{Size: 600 << 20, SHA256: "b"}},
        }},
    }
    d := buildFitTestDeps(t, hits, repos, hardware.Info{RAMBytes: 32 << 30})
    var out bytes.Buffer
    d.Stdout = &out
    cmd := newFitCmd(d)
    cmd.SetArgs([]string{"some-model"})
    if err := cmd.ExecuteContext(context.Background()); err != nil {
        t.Fatal(err)
    }
    s := out.String()
    if strings.Contains(s, "imatrix") {
        t.Fatalf("imatrix shard should have been filtered:\n%s", s)
    }
    if !strings.Contains(s, "Q4_K_M") {
        t.Fatalf("real sub-1B model row missing:\n%s", s)
    }
}
```

(The pre-Phase-6a test was using 4 GB for the real model; switch to 600 MB to actually exercise the new floor.)

- [ ] **Step 3: Tests + commit**

```bash
go test ./internal/cli/... -race -run TestFit -v
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/fit.go internal/cli/fit_test.go
git commit -m "$(cat <<'EOF'
fix(fit): lower fitMinModelBytes to 200 MiB for sub-1B model support

Phase 5's 500 MiB floor over-filtered sub-1B Q4_K_M files (qwen3-0.6b
at ~600 MB). 200 MiB still excludes imatrix shards (~100 MB) while
admitting legitimate small models. Test updated with 600 MB realistic
sub-1B fixture.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: `list` self-heal stale ParamsB / Arch

**Spec:** §6.5.

**Files:**
- Modify: `internal/cli/list.go`
- Modify: `internal/cli/list_test.go`

When `metadata.ParamsB == 0` AND `metadata.GGUFPath` exists, re-parse the GGUF and write back the updated metadata. Self-healing on each `list` invocation.

- [ ] **Step 1: Read current list.go structure**

```bash
sed -n '1,80p' internal/cli/list.go
```

Note where the model iteration happens. The self-heal logic slots in there.

- [ ] **Step 2: Failing test**

`internal/cli/list_test.go`:

```go
func TestListSelfHealsZeroParamsB(t *testing.T) {
    tempDir := t.TempDir()
    // Create a real GGUF file with size_label="3.4B" via gguftest helper.
    ggufPath := filepath.Join(tempDir, "test", "Q5_K_M.gguf")
    if err := os.MkdirAll(filepath.Dir(ggufPath), 0o755); err != nil {
        t.Fatal(err)
    }
    ggufBytes := gguftest.Build(t, 3,
        gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "qwen3"},
        gguftest.KV{Key: "general.size_label", Type: gguftest.TypeString, Value: "3.4B"},
    )
    if err := os.WriteFile(ggufPath, ggufBytes, 0o644); err != nil {
        t.Fatal(err)
    }
    // Seed metadata with ParamsB=0 (the stale state we're healing from).
    fakeStore := &fakeStore{
        models: map[string]models.Metadata{
            "test": {ID: "test", GGUFPath: ggufPath, Quant: models.Q5_K_M, ParamsB: 0, Arch: ""},
        },
    }
    var out bytes.Buffer
    d := &Deps{
        Stdout:     &out,
        ModelStore: fakeStore,
        FS:         OSFileSystem{},
    }
    if err := runList(context.Background(), d); err != nil {
        t.Fatal(err)
    }
    // Output should show the self-healed value.
    if !strings.Contains(out.String(), "3.4") {
        t.Fatalf("self-heal didn't surface params count:\n%s", out.String())
    }
    // Store should have been updated.
    healed := fakeStore.models["test"]
    if healed.ParamsB == 0 {
        t.Fatalf("metadata not written back; ParamsB still 0")
    }
}
```

(Adapt `fakeStore` / Deps construction to existing test patterns in `list_test.go`. Use `gguftest.Build` from the existing Phase 5 helper.)

Run: FAIL.

- [ ] **Step 3: Implement self-heal**

In `internal/cli/list.go`'s iteration loop:

```go
for i, m := range list {
    if m.ParamsB == 0 && m.GGUFPath != "" {
        if _, statErr := os.Stat(m.GGUFPath); statErr == nil {
            if h, perr := gguf.ReadHeader(m.GGUFPath); perr == nil {
                if h.ParamsCount > 0 {
                    m.ParamsB = float64(h.ParamsCount) / 1e9
                }
                if m.Arch == "" && h.Architecture != "" {
                    m.Arch = models.ArchFromGGUF(h.Architecture)
                }
                if m.ParamsB != 0 || m.Arch != "" {
                    // Best-effort write-back; don't fail list on store errors.
                    _ = d.ModelStore.Put(ctx, m)
                    list[i] = m
                }
            }
        }
    }
    // ... existing rendering for `m` ...
}
```

(Imports: `os`, `github.com/gregmundy/llamactl/internal/gguf`, `github.com/gregmundy/llamactl/internal/models`. Confirm against actual file.)

- [ ] **Step 4: Tests + commit**

```bash
go test ./internal/cli/... -race -run TestList -v
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/list.go internal/cli/list_test.go
git commit -m "$(cat <<'EOF'
feat(list): self-heal stale ParamsB/Arch metadata via GGUF re-parse

When a model's stored metadata has ParamsB==0 (pre-Phase-5 GGUF parser
couldn't read it), re-parse the on-disk GGUF and update metadata in
place. Resolves the surprise where gemma-4-e4b-it still showed `?`
in list even after Phase 5's parser fixed the parameter_count gap.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: `fit` popularity-weighted ranking

**Spec:** §6.4.

**Files:**
- Modify: `internal/cli/fit.go`
- Modify: `internal/cli/fit_test.go`

- [ ] **Step 1: Add `Downloads` and `Likes` to `fitRow`**

`internal/cli/fit.go`:

```go
type fitRow struct {
    Repo      string  `json:"repo"`
    Quant     string  `json:"quant"`
    SizeGB    float64 `json:"size_gb"`
    Verdict   string  `json:"verdict"`
    FreeGB    float64 `json:"free_gb,omitempty"`
    DeficitGB float64 `json:"deficit_gb,omitempty"`
    Note      string  `json:"note,omitempty"`
    Downloads int     `json:"downloads,omitempty"`
    Likes     int     `json:"likes,omitempty"`
}
```

- [ ] **Step 2: Populate the new fields**

In `runFit`'s iteration loop, where `row := fitRow{...}` is built, add:
```go
row.Downloads = hit.Downloads
row.Likes = hit.Likes
```

- [ ] **Step 3: Update `fitRank`**

```go
func fitRank(r fitRow) float64 {
    switch r.Verdict {
    case "ok":
        // Within ✓: weight by downloads (canonical repos surface first);
        // tiebreak on size (higher fidelity preferred among equally-popular).
        return 100_000_000 + float64(r.Downloads) + r.SizeGB
    case "tight":
        return 100 - r.SizeGB
    default:
        return -r.DeficitGB
    }
}
```

- [ ] **Step 4: Failing test**

`internal/cli/fit_test.go`:

```go
func TestFitRanksByDownloadsWithinOK(t *testing.T) {
    hits := []hf.SearchHit{
        {ID: "obscure/gemma-fork-GGUF", Downloads: 50},
        {ID: "canonical/gemma-official-GGUF", Downloads: 50_000},
    }
    repos := map[string]hf.Repo{
        "obscure/gemma-fork-GGUF": {Siblings: []hf.File{
            {RFilename: "model-Q5_K_M.gguf", LFS: &hf.LFSInfo{Size: 3 << 30, SHA256: "a"}},
        }},
        "canonical/gemma-official-GGUF": {Siblings: []hf.File{
            {RFilename: "model-Q5_K_M.gguf", LFS: &hf.LFSInfo{Size: 3 << 30, SHA256: "b"}},
        }},
    }
    d := buildFitTestDeps(t, hits, repos, hardware.Info{RAMBytes: 32 << 30})
    var out bytes.Buffer
    d.Stdout = &out
    cmd := newFitCmd(d)
    cmd.SetArgs([]string{"gemma"})
    if err := cmd.ExecuteContext(context.Background()); err != nil {
        t.Fatal(err)
    }
    s := out.String()
    // Canonical (high-downloads) should appear before obscure.
    canonicalIdx := strings.Index(s, "canonical/gemma-official-GGUF")
    obscureIdx := strings.Index(s, "obscure/gemma-fork-GGUF")
    if canonicalIdx == -1 || obscureIdx == -1 {
        t.Fatalf("missing one of the repos:\n%s", s)
    }
    if canonicalIdx > obscureIdx {
        t.Fatalf("canonical should rank above obscure; got order:\n%s", s)
    }
}
```

Run: FAIL (current `fitRank` doesn't use Downloads).

- [ ] **Step 5: Tests + commit**

```bash
go test ./internal/cli/... -race -run TestFit -v
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/fit.go internal/cli/fit_test.go
git commit -m "$(cat <<'EOF'
feat(fit): popularity-weighted ranking within ✓ bucket

`fit gemma 4` previously top-ranked obscure community fine-tunes because
the rank formula (1000 + FreeGB) rewarded smaller files. Now uses HF's
Downloads count as the primary signal within the ✓ verdict bucket,
tiebreak on size. Canonical repos surface first.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: `config.Save` + APIKey field

**Spec:** §3.

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Add the APIKey field**

In `internal/config/config.go`:

```go
type Config struct {
    LlamaServerPath string `yaml:"llama_server_path"`
    DefaultPort     int    `yaml:"default_port"`
    ModelsDir       string `yaml:"models_dir"`
    HFToken         string `yaml:"hf_token"`
    LogLevel        string `yaml:"log_level"`
    APIKey          string `yaml:"api_key"`  // NEW
}
```

- [ ] **Step 2: Failing test for Save**

`internal/config/config_test.go`:

```go
func TestSaveRoundTrip(t *testing.T) {
    tempDir := t.TempDir()
    path := filepath.Join(tempDir, "config.yaml")
    orig := Config{
        LlamaServerPath: "/path/to/llama",
        DefaultPort:     8080,
        APIKey:          "sk-test-123",
    }
    if err := Save(path, orig); err != nil {
        t.Fatal(err)
    }
    loaded, err := Load(path)
    if err != nil {
        t.Fatal(err)
    }
    if loaded != orig {
        t.Fatalf("round-trip mismatch:\nwant %+v\ngot  %+v", orig, loaded)
    }
}

func TestSaveAtomicNoPartialOnError(t *testing.T) {
    // Try to save to a path inside a read-only dir.
    tempDir := t.TempDir()
    readOnly := filepath.Join(tempDir, "ro")
    if err := os.Mkdir(readOnly, 0o500); err != nil {
        t.Fatal(err)
    }
    path := filepath.Join(readOnly, "config.yaml")
    err := Save(path, Config{DefaultPort: 8080})
    if err == nil {
        t.Fatal("expected error writing to read-only dir")
    }
    // No tmp file should be left behind.
    entries, _ := os.ReadDir(readOnly)
    if len(entries) > 0 {
        t.Fatalf("partial tmp file left behind: %v", entries)
    }
}
```

Run: FAIL (no `Save` function).

- [ ] **Step 3: Implement Save**

In `internal/config/config.go`:

```go
// Save writes cfg to path via atomic temp+rename. Creates parent dirs if
// needed.
func Save(path string, cfg Config) error {
    data, err := yaml.Marshal(cfg)
    if err != nil {
        return fmt.Errorf("marshal: %w", err)
    }
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        return fmt.Errorf("mkdir parent: %w", err)
    }
    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, data, 0o600); err != nil {
        return fmt.Errorf("write tmp: %w", err)
    }
    if err := os.Rename(tmp, path); err != nil {
        _ = os.Remove(tmp)
        return fmt.Errorf("rename: %w", err)
    }
    return nil
}
```

(Imports: `path/filepath`. Mode 0o600 because config holds tokens.)

- [ ] **Step 4: Tests + commit**

```bash
go test ./internal/config/... -race -v
gofmt -l . && go vet ./...
git add internal/config/config.go internal/config/config_test.go
git commit -m "$(cat <<'EOF'
feat(config): add Save + APIKey field (foundation for config command + auth)

Config.APIKey is the new opt-in endpoint authentication token. Save
implements atomic write+rename to the same path Load reads from.
Foundation for Task 16 (config command) and Task 17 (auth wiring).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 15: Wire `Deps.Config` in main.go

**Spec:** §2.

**Files:**
- Modify: `internal/cli/deps.go`
- Modify: `cmd/llamactl/main.go`

- [ ] **Step 1: Add Config field**

`internal/cli/deps.go`:

```go
import "github.com/gregmundy/llamactl/internal/config"

type Deps struct {
    // ...existing fields...
    Config *config.Config  // loaded from ~/.config/llamactl/config.yaml; never nil after wiring
}
```

- [ ] **Step 2: Wire in main.go**

`cmd/llamactl/main.go`:

```go
// Determine config path. Use existing convention (~/.config/llamactl/config.yaml)
// resolved via internal/config/paths.go (Phase 1).
configPath := config.DefaultPath() // verify this helper exists; if not, build the path inline
cfg, err := config.Load(configPath)
if err != nil {
    fmt.Fprintf(os.Stderr, "llamactl: warning: load config: %v\n", err)
}
deps.Config = &cfg
deps.ConfigPath = configPath // also add this field to Deps for `config set` to know where to Save
```

If `config.DefaultPath()` doesn't exist (it may not), add it to `internal/config/paths.go` or build the path here:
```go
home, _ := os.UserHomeDir()
configPath := filepath.Join(home, ".config/llamactl/config.yaml")
```

Also add `ConfigPath string` to `Deps`:
```go
type Deps struct {
    // ...
    Config     *config.Config
    ConfigPath string // path where Save writes back to
}
```

- [ ] **Step 3: Verify no existing test breaks**

```bash
go test ./... -race
```

Existing tests that build a `Deps` without `Config` will need a `Config: &config.Config{}` field added wherever the missing field causes a nil-deref. Locate them:
```bash
go test ./... 2>&1 | grep -i "panic\|nil"
```

For tests that intentionally don't need Config: `Deps{Config: &config.Config{}}`. Don't add Config wiring to test fixtures that don't exercise it.

- [ ] **Step 4: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/cli/deps.go cmd/llamactl/main.go
# plus any test fixture files you had to touch
git commit -m "$(cat <<'EOF'
feat(deps): wire Config + ConfigPath into Deps

main.go now loads ~/.config/llamactl/config.yaml at startup and exposes
it via Deps.Config. Deps.ConfigPath records the path for write-back
(used by Task 16's `config set`). Future config-reading sites read from
deps.Config instead of calling config.Load themselves.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 16: `llamactl config get/set/list` cobra command

**Spec:** §3.

**Files:**
- Create: `internal/cli/config.go`
- Create: `internal/cli/config_test.go`
- Modify: `internal/cli/root.go` (register)

- [ ] **Step 1: Write failing tests**

`internal/cli/config_test.go`:

```go
package cli

import (
    "bytes"
    "io"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/gregmundy/llamactl/internal/config"
)

func TestConfigGetExistingKey(t *testing.T) {
    cfg := &config.Config{DefaultPort: 8080}
    var out bytes.Buffer
    d := &Deps{Config: cfg, Stdout: &out, Stderr: io.Discard}
    cmd := newConfigCmd(d)
    cmd.SetArgs([]string{"get", "default_port"})
    if err := cmd.Execute(); err != nil {
        t.Fatal(err)
    }
    if strings.TrimSpace(out.String()) != "8080" {
        t.Fatalf("got %q, want 8080", out.String())
    }
}

func TestConfigGetUnknownKeyErrors(t *testing.T) {
    d := &Deps{Config: &config.Config{}, Stdout: io.Discard, Stderr: io.Discard}
    cmd := newConfigCmd(d)
    cmd.SetArgs([]string{"get", "made_up_key"})
    err := cmd.Execute()
    if err == nil || !errors.Is(err, ErrUserError) {
        t.Fatalf("expected ErrUserError, got %v", err)
    }
}

func TestConfigSetWritesFile(t *testing.T) {
    tempDir := t.TempDir()
    cfgPath := filepath.Join(tempDir, "config.yaml")
    d := &Deps{
        Config:     &config.Config{},
        ConfigPath: cfgPath,
        Stdout:     io.Discard,
        Stderr:     io.Discard,
    }
    cmd := newConfigCmd(d)
    cmd.SetArgs([]string{"set", "api_key", "sk-test-xyz"})
    if err := cmd.Execute(); err != nil {
        t.Fatal(err)
    }
    loaded, err := config.Load(cfgPath)
    if err != nil {
        t.Fatal(err)
    }
    if loaded.APIKey != "sk-test-xyz" {
        t.Fatalf("APIKey=%q, want sk-test-xyz", loaded.APIKey)
    }
}

func TestConfigSetRejectsInvalidPort(t *testing.T) {
    tempDir := t.TempDir()
    d := &Deps{
        Config:     &config.Config{},
        ConfigPath: filepath.Join(tempDir, "config.yaml"),
        Stdout:     io.Discard,
        Stderr:     io.Discard,
    }
    cmd := newConfigCmd(d)
    cmd.SetArgs([]string{"set", "default_port", "99999"})
    err := cmd.Execute()
    if err == nil || !errors.Is(err, ErrUserError) {
        t.Fatalf("expected ErrUserError for out-of-range port, got %v", err)
    }
}

func TestConfigSetRejectsInvalidLogLevel(t *testing.T) {
    tempDir := t.TempDir()
    d := &Deps{
        Config:     &config.Config{},
        ConfigPath: filepath.Join(tempDir, "config.yaml"),
        Stdout:     io.Discard,
        Stderr:     io.Discard,
    }
    cmd := newConfigCmd(d)
    cmd.SetArgs([]string{"set", "log_level", "purple"})
    err := cmd.Execute()
    if err == nil || !errors.Is(err, ErrUserError) {
        t.Fatalf("expected ErrUserError, got %v", err)
    }
}

func TestConfigListRedactsSecrets(t *testing.T) {
    cfg := &config.Config{
        DefaultPort: 8080,
        APIKey:      "sk-secret",
        HFToken:     "hf_secret",
    }
    var out bytes.Buffer
    d := &Deps{Config: cfg, Stdout: &out, Stderr: io.Discard}
    cmd := newConfigCmd(d)
    cmd.SetArgs([]string{"list"})
    if err := cmd.Execute(); err != nil {
        t.Fatal(err)
    }
    s := out.String()
    if strings.Contains(s, "sk-secret") {
        t.Fatalf("APIKey value leaked in list output:\n%s", s)
    }
    if strings.Contains(s, "hf_secret") {
        t.Fatalf("HFToken value leaked in list output:\n%s", s)
    }
    if !strings.Contains(s, "********") {
        t.Fatalf("missing redacted indicator:\n%s", s)
    }
    if !strings.Contains(s, "8080") {
        t.Fatalf("non-secret value missing:\n%s", s)
    }
}
```

Run: FAIL — `newConfigCmd` undefined.

- [ ] **Step 2: Implement**

`internal/cli/config.go`:

```go
package cli

import (
    "fmt"
    "reflect"
    "strconv"
    "strings"

    "github.com/gregmundy/llamactl/internal/config"
    "github.com/spf13/cobra"
)

// secretKeys are config keys whose values are redacted in `config list`.
var secretKeys = map[string]bool{
    "api_key":  true,
    "hf_token": true,
}

// allowedLogLevels gates `config set log_level <value>`.
var allowedLogLevels = map[string]bool{
    "debug": true, "info": true, "warn": true, "error": true,
}

func newConfigCmd(d *Deps) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "config",
        Short: "Get or set llamactl configuration keys",
    }
    cmd.AddCommand(newConfigGetCmd(d), newConfigSetCmd(d), newConfigListCmd(d))
    return cmd
}

func newConfigGetCmd(d *Deps) *cobra.Command {
    return &cobra.Command{
        Use:   "get <key>",
        Short: "Print the current value of a config key",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            return runConfigGet(d, args[0])
        },
    }
}

func newConfigSetCmd(d *Deps) *cobra.Command {
    return &cobra.Command{
        Use:   "set <key> <value>",
        Short: "Set a config key and write to ~/.config/llamactl/config.yaml",
        Args:  cobra.ExactArgs(2),
        RunE: func(cmd *cobra.Command, args []string) error {
            return runConfigSet(d, args[0], args[1])
        },
    }
}

func newConfigListCmd(d *Deps) *cobra.Command {
    return &cobra.Command{
        Use:   "list",
        Short: "List all config keys with their current values (secrets redacted)",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runConfigList(d)
        },
    }
}

// allowedKeys returns the set of YAML field names from config.Config.
func allowedKeys() map[string]reflect.StructField {
    out := map[string]reflect.StructField{}
    t := reflect.TypeOf(config.Config{})
    for i := 0; i < t.NumField(); i++ {
        f := t.Field(i)
        tag := f.Tag.Get("yaml")
        if tag != "" && tag != "-" {
            out[strings.Split(tag, ",")[0]] = f
        }
    }
    return out
}

func runConfigGet(d *Deps, key string) error {
    keys := allowedKeys()
    field, ok := keys[key]
    if !ok {
        return fmt.Errorf("%w: unknown key %q (known: %s)",
            ErrUserError, key, strings.Join(sortedKeys(keys), ", "))
    }
    v := reflect.ValueOf(*d.Config).FieldByIndex(field.Index)
    fmt.Fprintln(d.Stdout, formatValue(v))
    return nil
}

func runConfigSet(d *Deps, key, value string) error {
    keys := allowedKeys()
    field, ok := keys[key]
    if !ok {
        return fmt.Errorf("%w: unknown key %q (known: %s)",
            ErrUserError, key, strings.Join(sortedKeys(keys), ", "))
    }
    // Type-specific validation + assignment.
    cfg := *d.Config
    v := reflect.ValueOf(&cfg).Elem().FieldByIndex(field.Index)
    switch v.Kind() {
    case reflect.String:
        if key == "log_level" && value != "" && !allowedLogLevels[value] {
            return fmt.Errorf("%w: log_level must be one of debug|info|warn|error, got %q",
                ErrUserError, value)
        }
        v.SetString(value)
    case reflect.Int:
        n, err := strconv.Atoi(value)
        if err != nil {
            return fmt.Errorf("%w: %s must be an integer: %v", ErrUserError, key, err)
        }
        if key == "default_port" && (n < 0 || n > 65535) {
            return fmt.Errorf("%w: default_port must be 0-65535, got %d", ErrUserError, n)
        }
        v.SetInt(int64(n))
    default:
        return fmt.Errorf("%w: unsupported field kind %v for key %q",
            ErrUserError, v.Kind(), key)
    }
    if err := config.Save(d.ConfigPath, cfg); err != nil {
        return fmt.Errorf("save config: %w", err)
    }
    *d.Config = cfg
    fmt.Fprintf(d.Stdout, "%s updated\n", key)
    return nil
}

func runConfigList(d *Deps) error {
    keys := allowedKeys()
    sorted := sortedKeys(keys)
    val := reflect.ValueOf(*d.Config)
    for _, k := range sorted {
        f := keys[k]
        v := val.FieldByIndex(f.Index)
        display := formatValue(v)
        if secretKeys[k] && display != "(unset)" && display != "0" {
            display = "********  (set; redacted)"
        }
        fmt.Fprintf(d.Stdout, "%-20s %s\n", k, display)
    }
    return nil
}

func formatValue(v reflect.Value) string {
    switch v.Kind() {
    case reflect.String:
        if v.String() == "" {
            return "(unset)"
        }
        return v.String()
    case reflect.Int:
        if v.Int() == 0 {
            return "(unset)"
        }
        return strconv.FormatInt(v.Int(), 10)
    default:
        return fmt.Sprintf("%v", v.Interface())
    }
}

func sortedKeys(m map[string]reflect.StructField) []string {
    out := make([]string, 0, len(m))
    for k := range m {
        out = append(out, k)
    }
    sort.Strings(out)
    return out
}
```

(Imports: add `"sort"` to the import block. Adapt `errors` import if not already there.)

- [ ] **Step 3: Register in root.go**

`internal/cli/root.go`:
```go
rootCmd.AddCommand(newConfigCmd(d))
```

(Place near the other `AddCommand` lines. Don't worry about alphabetical order.)

- [ ] **Step 4: Tests + lint**

```bash
go test ./internal/cli/... -race -v -run TestConfig
go test ./... -race && gofmt -l . && go vet ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/cli/config.go internal/cli/config_test.go internal/cli/root.go
git commit -m "$(cat <<'EOF'
feat(config): llamactl config get/set/list

Cobra command for inspecting and updating ~/.config/llamactl/config.yaml.
- get <key>: prints current value or '(unset)'
- set <key> <value>: validates per-type (default_port 0-65535, log_level
  enum), writes via config.Save
- list: tabular output of all 6 keys with secrets (api_key, hf_token)
  redacted as ********

Key allowlist derived via reflection from Config struct yaml tags so
adding a future config field automatically extends the command.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 17: `serve` reads APIKey + passes `--api-key`

**Spec:** §4.

**Files:**
- Modify: `internal/cli/serve.go`
- Modify: `internal/cli/serve_test.go`

- [ ] **Step 1: Add API key resolution near the start of `runServe`/`runServeDetached`**

```go
// resolveAPIKey picks env > config; "" means no auth (existing behavior).
apiKey := d.Getenv("LLAMACTL_API_KEY")
if apiKey == "" && d.Config != nil {
    apiKey = d.Config.APIKey
}
```

(Place this AFTER `recipes.FlagsFor` returns the flags slice, before the launchd plist is rendered.)

- [ ] **Step 2: Append `--api-key` to flags when set**

```go
if apiKey != "" {
    flags = append(flags, "--api-key", apiKey)
}
```

Adapt variable name (`flags` may be `argv` or similar).

- [ ] **Step 3: Failing tests**

`internal/cli/serve_test.go`:

```go
func TestServeAppendsAPIKeyFromConfig(t *testing.T) {
    d := minimalServeDeps(t)
    d.Config = &config.Config{APIKey: "sk-from-config"}
    d.Getenv = func(string) string { return "" }
    // Run serve in --detach mode; capture the rendered plist via fake LaunchdService.
    // The fake captures the Args slice passed to Render.
    // Assert: args contain "--api-key" followed by "sk-from-config".
}

func TestServeEnvAPIKeyBeatsConfig(t *testing.T) {
    d := minimalServeDeps(t)
    d.Config = &config.Config{APIKey: "sk-from-config"}
    d.Getenv = func(k string) string {
        if k == "LLAMACTL_API_KEY" {
            return "sk-from-env"
        }
        return ""
    }
    // Assert plist args contain --api-key sk-from-env (env wins).
}

func TestServeNoAPIKeyWhenUnset(t *testing.T) {
    d := minimalServeDeps(t)
    d.Config = &config.Config{}
    d.Getenv = func(string) string { return "" }
    // Assert plist args do NOT contain --api-key.
}
```

The exact fixture wiring depends on existing serve_test patterns. Look for how Phase 5's `TestServeDetachedSkipsSiblingPorts` captures the plist Args — reuse that approach.

- [ ] **Step 4: Tests + commit**

```bash
go test ./internal/cli/... -race -run TestServe -v
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/serve.go internal/cli/serve_test.go
git commit -m "$(cat <<'EOF'
feat(serve): pass --api-key to llama-server when configured

Resolves LLAMACTL_API_KEY env var (precedence) or Config.APIKey to
the --api-key flag on llama-server. Empty value (default) means no
auth — preserves existing behavior. Enables opt-in endpoint
authentication.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 18: `launchd.HasAPIKey` + `launchd.HasPublicBind` plist helpers

**Spec:** §4.3.

**Files:**
- Modify: `internal/launchd/ports.go`
- Modify: `internal/launchd/ports_test.go`

Phase 5 introduced `launchd.PortsInUse` and `launchd.PortFor` for plist arg extraction. Add two siblings for the new doctor check.

- [ ] **Step 1: Failing tests**

`internal/launchd/ports_test.go` — append:

```go
func TestHasAPIKey(t *testing.T) {
    dir := t.TempDir()
    withKey := `<plist><array><string>--api-key</string><string>sk-XYZ</string></array></plist>`
    if err := os.WriteFile(filepath.Join(dir, "com.llamactl.with.plist"), []byte(withKey), 0o644); err != nil {
        t.Fatal(err)
    }
    withoutKey := `<plist><array><string>--port</string><string>8080</string></array></plist>`
    if err := os.WriteFile(filepath.Join(dir, "com.llamactl.without.plist"), []byte(withoutKey), 0o644); err != nil {
        t.Fatal(err)
    }
    if !HasAPIKey(dir, "com.llamactl.with") {
        t.Fatal("expected true for plist containing --api-key")
    }
    if HasAPIKey(dir, "com.llamactl.without") {
        t.Fatal("expected false for plist without --api-key")
    }
    if HasAPIKey(dir, "com.llamactl.missing") {
        t.Fatal("expected false for missing plist")
    }
}

func TestHasPublicBind(t *testing.T) {
    dir := t.TempDir()
    // No --host present → default is 0.0.0.0 → public.
    defaultBind := `<plist><array><string>--port</string><string>8080</string></array></plist>`
    os.WriteFile(filepath.Join(dir, "com.llamactl.default.plist"), []byte(defaultBind), 0o644)
    // Explicit 0.0.0.0 → public.
    publicBind := `<plist><array><string>--host</string><string>0.0.0.0</string></array></plist>`
    os.WriteFile(filepath.Join(dir, "com.llamactl.public.plist"), []byte(publicBind), 0o644)
    // Loopback → not public.
    loopback := `<plist><array><string>--host</string><string>127.0.0.1</string></array></plist>`
    os.WriteFile(filepath.Join(dir, "com.llamactl.loopback.plist"), []byte(loopback), 0o644)

    if !HasPublicBind(dir, "com.llamactl.default") {
        t.Fatal("missing --host defaults public")
    }
    if !HasPublicBind(dir, "com.llamactl.public") {
        t.Fatal("explicit 0.0.0.0 is public")
    }
    if HasPublicBind(dir, "com.llamactl.loopback") {
        t.Fatal("127.0.0.1 is not public")
    }
}
```

Run: FAIL.

- [ ] **Step 2: Implement**

`internal/launchd/ports.go` — append:

```go
// HasAPIKey reports whether the plist for label contains an --api-key arg.
// Missing plist returns false.
func HasAPIKey(dir, label string) bool {
    data, err := os.ReadFile(filepath.Join(dir, label+".plist"))
    if err != nil {
        return false
    }
    return strings.Contains(string(data), "<string>--api-key</string>")
}

// HasPublicBind reports whether the plist for label binds publicly.
// Missing --host arg is treated as default-public (llama-server binds 0.0.0.0
// by default). Explicit 127.0.0.1 / ::1 / localhost is non-public.
func HasPublicBind(dir, label string) bool {
    data, err := os.ReadFile(filepath.Join(dir, label+".plist"))
    if err != nil {
        return false
    }
    s := string(data)
    idx := strings.Index(s, "<string>--host</string>")
    if idx < 0 {
        return true // default-bind is public
    }
    // Read the next <string>...</string>
    rest := s[idx+len("<string>--host</string>"):]
    open := strings.Index(rest, "<string>")
    if open < 0 {
        return true
    }
    rest = rest[open+len("<string>"):]
    close := strings.Index(rest, "</string>")
    if close < 0 {
        return true
    }
    host := strings.TrimSpace(rest[:close])
    return host != "127.0.0.1" && host != "::1" && host != "localhost"
}
```

- [ ] **Step 3: Tests + commit**

```bash
go test ./internal/launchd/... -race -v
go test ./... -race && gofmt -l . && go vet ./...
git add internal/launchd/ports.go internal/launchd/ports_test.go
git commit -m "$(cat <<'EOF'
feat(launchd): HasAPIKey + HasPublicBind plist scanners

Two helpers for the new auth-on-public-bind doctor check (Task 19).
HasAPIKey looks for --api-key in ProgramArguments. HasPublicBind
returns true when --host is absent (default 0.0.0.0) or explicitly
non-loopback. Mirrors the pattern of PortFor/PortsInUse from v1.2.1.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 19: Doctor `authOnPublicBindCheck`

**Spec:** §4.3.

**Files:**
- Modify: `internal/cli/doctor.go`
- Modify: `internal/cli/doctor_test.go`

- [ ] **Step 1: Add the check function**

`internal/cli/doctor.go` — alongside the other check builders:

```go
func authOnPublicBindCheck(deps *Deps) doctorCheck {
    return doctorCheck{
        label: "Public-bound endpoints have api_key set",
        remediation: "set api_key: `llamactl config set api_key <token>` or " +
            "export LLAMACTL_API_KEY=<token>",
        run: func(ctx context.Context, d *Deps) (bool, string) {
            if d.LaunchAgentsDir == "" {
                return true, "(no LaunchAgentsDir configured)"
            }
            services, err := d.LaunchdService.List(ctx)
            if err != nil {
                return true, "(list failed: " + err.Error() + ")"
            }
            envKey := d.Getenv("LLAMACTL_API_KEY")
            cfgKey := ""
            if d.Config != nil {
                cfgKey = d.Config.APIKey
            }
            // Resolved key applies to NEW serves; existing plists need their own --api-key.
            for _, svc := range services {
                if svc.PID == 0 {
                    continue // stopped service, skip
                }
                if !launchd.HasPublicBind(d.LaunchAgentsDir, svc.Label) {
                    continue
                }
                if launchd.HasAPIKey(d.LaunchAgentsDir, svc.Label) {
                    continue
                }
                // Public bind without --api-key in the plist itself.
                _ = envKey
                _ = cfgKey
                return false, svc.Label + " binds publicly without --api-key"
            }
            return true, ""
        },
    }
}
```

(Imports: `github.com/gregmundy/llamactl/internal/launchd`.)

- [ ] **Step 2: Wire into the checks list**

In `buildDoctorChecks`, append `authOnPublicBindCheck(deps)` after the existing checks (Phase 5 introduced `logFilesNotOversizedCheck` and `hfCacheSizeCheck` — slot the new one after them, before any "stale plists" footer check).

The check count goes 12 → 13 here; Task 23 adds the 14th.

- [ ] **Step 3: Failing tests**

`internal/cli/doctor_test.go`:

```go
func TestAuthCheckPublicBindNoKey(t *testing.T) {
    tempDir := t.TempDir()
    publicNoKey := `<plist><array><string>--port</string><string>8080</string></array></plist>`
    os.WriteFile(filepath.Join(tempDir, "com.llamactl.foo.plist"), []byte(publicNoKey), 0o644)
    deps := &Deps{
        LaunchAgentsDir: tempDir,
        LaunchdService:  &fakeLaunchdServiceList{services: []launchd.ServiceInfo{{Label: "com.llamactl.foo", PID: 12345}}},
        Config:          &config.Config{},
        Getenv:          func(string) string { return "" },
    }
    check := authOnPublicBindCheck(deps)
    ok, detail := check.run(context.Background(), deps)
    if ok {
        t.Fatalf("expected ✗ for public bind without api_key; got detail=%q", detail)
    }
}

func TestAuthCheckPublicBindWithKey(t *testing.T) {
    tempDir := t.TempDir()
    publicWithKey := `<plist><array><string>--port</string><string>8080</string><string>--api-key</string><string>sk-XYZ</string></array></plist>`
    os.WriteFile(filepath.Join(tempDir, "com.llamactl.foo.plist"), []byte(publicWithKey), 0o644)
    deps := &Deps{
        LaunchAgentsDir: tempDir,
        LaunchdService:  &fakeLaunchdServiceList{services: []launchd.ServiceInfo{{Label: "com.llamactl.foo", PID: 12345}}},
        Config:          &config.Config{APIKey: "sk-XYZ"},
        Getenv:          func(string) string { return "" },
    }
    check := authOnPublicBindCheck(deps)
    ok, _ := check.run(context.Background(), deps)
    if !ok {
        t.Fatal("expected ✓ for public bind with api_key")
    }
}

func TestAuthCheckLoopbackBindNoKey(t *testing.T) {
    tempDir := t.TempDir()
    loopback := `<plist><array><string>--host</string><string>127.0.0.1</string><string>--port</string><string>8080</string></array></plist>`
    os.WriteFile(filepath.Join(tempDir, "com.llamactl.foo.plist"), []byte(loopback), 0o644)
    deps := &Deps{
        LaunchAgentsDir: tempDir,
        LaunchdService:  &fakeLaunchdServiceList{services: []launchd.ServiceInfo{{Label: "com.llamactl.foo", PID: 12345}}},
        Config:          &config.Config{},
        Getenv:          func(string) string { return "" },
    }
    check := authOnPublicBindCheck(deps)
    ok, _ := check.run(context.Background(), deps)
    if !ok {
        t.Fatal("expected ✓ for loopback bind regardless of api_key")
    }
}
```

(Reuses `fakeLaunchdServiceList` from Task 8. If Task 8's fake isn't shared between test files, copy it locally.)

Run: FAIL.

- [ ] **Step 4: Update `buildDoctorChecks` total-count assertion if any**

```bash
grep -n "len(checks)" internal/cli/doctor_test.go
```

Bump 12 → 13 in any such assertion. Task 23 bumps to 14.

- [ ] **Step 5: Tests + commit**

```bash
go test ./internal/cli/... -race -run TestAuthCheck -v
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/doctor.go internal/cli/doctor_test.go
git commit -m "$(cat <<'EOF'
feat(doctor): authOnPublicBindCheck warns on 0.0.0.0 without api_key

Walks active (PID>0) com.llamactl.* services; flags any that bind
publicly (no --host or explicit 0.0.0.0) without --api-key in the
plist. Doctor goes 12 → 13 checks.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 20: Build-time version baking via ldflags

**Spec:** §5.2.

**Files:**
- Modify: `.goreleaser.yml`

`cmd/llamactl/main.go` already has `var llamactlVersion = "dev"` and `NewRoot` accepts the version. Just need the ldflags wiring.

- [ ] **Step 1: Read current .goreleaser.yml**

```bash
grep -n "ldflags\|builds:" .goreleaser.yml
```

- [ ] **Step 2: Add ldflags**

`.goreleaser.yml` builds section:

```yaml
builds:
  - id: llamactl
    main: ./cmd/llamactl
    binary: llamactl
    goos: [darwin]
    goarch: [arm64]
    ldflags:
      - -s -w
      - -X main.llamactlVersion=v{{.Version}}
```

(If `ldflags` already exists with other entries, append the `-X` directive. The `v{{.Version}}` produces `v1.3.0` matching the tag style.)

- [ ] **Step 3: Verify locally**

```bash
goreleaser build --single-target --snapshot --clean
./dist/llamactl_*/llamactl --version
# Expected: "llamactl version v0.0.0-SNAPSHOT-XXXXX" or similar (the snapshot suffix is fine)
```

If goreleaser isn't installed locally, skip the smoke and rely on CI verification at merge time.

- [ ] **Step 4: Commit**

```bash
git add .goreleaser.yml
git commit -m "$(cat <<'EOF'
build(release): bake version string via ldflags

Adds -X main.llamactlVersion=v{{.Version}} to goreleaser builds so
the binary reports its release tag via --version. Foundation for
Task 22 (update command needs to know current version) and Task 23
(doctor's latest-version-available check).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 21: `version.go` — fetchLatestVersion + 24h cache + semver compare

**Spec:** §5.2.

**Files:**
- Create: `internal/cli/version.go`
- Create: `internal/cli/version_test.go`

- [ ] **Step 1: Failing tests**

`internal/cli/version_test.go`:

```go
package cli

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

func TestParseCaskVersion(t *testing.T) {
    raw := `# header
cask "llamactl" do
  version "1.3.0"

  on_macos do
    on_arm do
      sha256 "abc"
    end
  end
end`
    got, err := parseCaskVersion([]byte(raw))
    if err != nil {
        t.Fatal(err)
    }
    if got != "1.3.0" {
        t.Fatalf("got %q, want 1.3.0", got)
    }
}

func TestParseCaskVersionMissing(t *testing.T) {
    _, err := parseCaskVersion([]byte("no version here"))
    if err == nil {
        t.Fatal("expected error")
    }
}

func TestFetchLatestVersionWritesCache(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintln(w, `cask "llamactl" do`)
        fmt.Fprintln(w, `  version "1.3.0"`)
        fmt.Fprintln(w, `end`)
    }))
    defer server.Close()

    cachePath := filepath.Join(t.TempDir(), "v.json")
    got, err := fetchLatestVersion(context.Background(), server.URL, cachePath, false, time.Now)
    if err != nil {
        t.Fatal(err)
    }
    if got != "1.3.0" {
        t.Fatalf("got %q, want 1.3.0", got)
    }
    // Cache file should exist with the version + timestamp.
    data, err := os.ReadFile(cachePath)
    if err != nil {
        t.Fatal(err)
    }
    var cache versionCache
    json.Unmarshal(data, &cache)
    if cache.Latest != "1.3.0" {
        t.Fatalf("cache.Latest=%q, want 1.3.0", cache.Latest)
    }
}

func TestFetchLatestVersionUsesFreshCache(t *testing.T) {
    cachePath := filepath.Join(t.TempDir(), "v.json")
    fresh := versionCache{Latest: "9.9.9", CheckedAt: time.Now().Add(-1 * time.Hour)}
    raw, _ := json.Marshal(fresh)
    os.WriteFile(cachePath, raw, 0o644)

    // URL doesn't matter — cache hit means no HTTP call.
    got, err := fetchLatestVersion(context.Background(), "http://unreachable/", cachePath, false, time.Now)
    if err != nil {
        t.Fatal(err)
    }
    if got != "9.9.9" {
        t.Fatalf("got %q, want 9.9.9 (from cache)", got)
    }
}

func TestFetchLatestVersionRefreshesStaleCache(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintln(w, `version "2.0.0"`)
    }))
    defer server.Close()

    cachePath := filepath.Join(t.TempDir(), "v.json")
    stale := versionCache{Latest: "1.0.0", CheckedAt: time.Now().Add(-48 * time.Hour)}
    raw, _ := json.Marshal(stale)
    os.WriteFile(cachePath, raw, 0o644)

    got, err := fetchLatestVersion(context.Background(), server.URL, cachePath, false, time.Now)
    if err != nil {
        t.Fatal(err)
    }
    if got != "2.0.0" {
        t.Fatalf("got %q (cache stale should have refreshed to 2.0.0)", got)
    }
}

func TestFetchLatestVersionRefreshFlagBypassesCache(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintln(w, `version "2.0.0"`)
    }))
    defer server.Close()

    cachePath := filepath.Join(t.TempDir(), "v.json")
    fresh := versionCache{Latest: "1.0.0", CheckedAt: time.Now().Add(-1 * time.Hour)}
    raw, _ := json.Marshal(fresh)
    os.WriteFile(cachePath, raw, 0o644)

    got, err := fetchLatestVersion(context.Background(), server.URL, cachePath, true, time.Now)
    if err != nil {
        t.Fatal(err)
    }
    if got != "2.0.0" {
        t.Fatalf("refresh=true should bypass cache; got %q", got)
    }
}

func TestVersionNewer(t *testing.T) {
    cases := []struct {
        a, b string
        want bool
    }{
        {"1.2.0", "1.3.0", true},
        {"1.3.0", "1.3.0", false},
        {"1.3.0", "1.2.0", false},
        {"v1.3.0", "1.3.0", false},   // same after normalize
        {"1.2.9", "1.3.0", true},
        {"1.10.0", "1.9.0", false},   // numeric, not lexical
    }
    for _, c := range cases {
        got := versionNewer(c.a, c.b)
        if got != c.want {
            t.Errorf("versionNewer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
        }
    }
}
```

Run: FAIL.

- [ ] **Step 2: Implement**

`internal/cli/version.go`:

```go
package cli

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "regexp"
    "strconv"
    "strings"
    "time"
)

// CaskURL is the raw URL of the llamactl cask in gregmundy/homebrew-tap.
const CaskURL = "https://raw.githubusercontent.com/gregmundy/homebrew-tap/main/Casks/llamactl.rb"

// versionCache TTL for the on-disk last-version-check.json.
const versionCacheTTL = 24 * time.Hour

type versionCache struct {
    Latest    string    `json:"latest"`
    CheckedAt time.Time `json:"checked_at"`
}

var caskVersionRe = regexp.MustCompile(`(?m)^\s*version\s+"([^"]+)"`)

func parseCaskVersion(data []byte) (string, error) {
    m := caskVersionRe.FindSubmatch(data)
    if m == nil {
        return "", fmt.Errorf("no version directive in cask")
    }
    return string(m[1]), nil
}

// fetchLatestVersion returns the latest published version, using a local
// cache with versionCacheTTL TTL. refresh=true bypasses cache. URL is
// overridable for testing.
func fetchLatestVersion(ctx context.Context, url, cachePath string, refresh bool, now func() time.Time) (string, error) {
    if !refresh {
        if cached, err := readVersionCache(cachePath); err == nil && now().Sub(cached.CheckedAt) < versionCacheTTL {
            return cached.Latest, nil
        }
    }
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return "", err
    }
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
    }
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return "", err
    }
    version, err := parseCaskVersion(body)
    if err != nil {
        return "", err
    }
    _ = writeVersionCache(cachePath, versionCache{Latest: version, CheckedAt: now()})
    return version, nil
}

func readVersionCache(path string) (versionCache, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return versionCache{}, err
    }
    var c versionCache
    if err := json.Unmarshal(data, &c); err != nil {
        return versionCache{}, err
    }
    return c, nil
}

func writeVersionCache(path string, c versionCache) error {
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        return err
    }
    data, err := json.Marshal(c)
    if err != nil {
        return err
    }
    return os.WriteFile(path, data, 0o644)
}

// versionNewer reports whether a > b in semver-ish ordering. Strips leading
// 'v' on either input. Returns false on parse failure (treats as equal).
func versionNewer(a, b string) bool {
    aa := parseSemver(a)
    bb := parseSemver(b)
    for i := 0; i < 3; i++ {
        if aa[i] > bb[i] {
            return true
        }
        if aa[i] < bb[i] {
            return false
        }
    }
    return false
}

func parseSemver(s string) [3]int {
    s = strings.TrimPrefix(s, "v")
    var out [3]int
    parts := strings.SplitN(s, ".", 3)
    for i := 0; i < 3 && i < len(parts); i++ {
        n, _ := strconv.Atoi(parts[i])
        out[i] = n
    }
    return out
}
```

- [ ] **Step 3: Tests + commit**

```bash
go test ./internal/cli/... -race -run TestParseCask -v
go test ./internal/cli/... -race -run TestFetchLatestVersion -v
go test ./internal/cli/... -race -run TestVersionNewer -v
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/version.go internal/cli/version_test.go
git commit -m "$(cat <<'EOF'
feat(version): fetchLatestVersion from cask + 24h cache + semver compare

Reads the published cask file from gregmundy/homebrew-tap, parses the
`version "X.Y.Z"` line, caches the result at
~/.cache/llamactl/last-version-check.json for 24h. Foundation for
Task 22 (update command) and Task 23 (doctor passive check).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 22: `llamactl update` cobra command

**Spec:** §5.

**Files:**
- Create: `internal/cli/update.go`
- Create: `internal/cli/update_test.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Failing tests**

`internal/cli/update_test.go`:

```go
package cli

import (
    "bytes"
    "context"
    "io"
    "path/filepath"
    "strings"
    "testing"
)

func TestUpdateOnLatest(t *testing.T) {
    var out bytes.Buffer
    d := &Deps{Stdout: &out, Stderr: io.Discard}
    // Inject a fetcher that returns the same version.
    err := runUpdate(context.Background(), d, "v1.3.0", false,
        func(ctx context.Context, refresh bool) (string, error) { return "1.3.0", nil },
        func() (string, error) { return "/opt/homebrew/Cellar/llamactl/1.3.0/bin/llamactl", nil },
        nil) // runner: not called when on latest
    if err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(out.String(), "already on latest") {
        t.Fatalf("missing 'already on latest':\n%s", out.String())
    }
}

func TestUpdateBrewInstalledRunsBrew(t *testing.T) {
    var out bytes.Buffer
    captured := []string{}
    fakeRunner := &recordingRunner{run: func(name string, args []string) error {
        captured = append(captured, name+" "+strings.Join(args, " "))
        return nil
    }}
    d := &Deps{Stdout: &out, Stderr: io.Discard, Runner: fakeRunner}
    err := runUpdate(context.Background(), d, "v1.2.0", false,
        func(ctx context.Context, refresh bool) (string, error) { return "1.3.0", nil },
        func() (string, error) { return "/opt/homebrew/Cellar/llamactl/1.2.0/bin/llamactl", nil },
        fakeRunner)
    if err != nil {
        t.Fatal(err)
    }
    found := false
    for _, c := range captured {
        if strings.Contains(c, "brew upgrade gregmundy/tap/llamactl") {
            found = true
        }
    }
    if !found {
        t.Fatalf("expected brew upgrade call; captured: %v", captured)
    }
}

func TestUpdateNonBrewInstallMessage(t *testing.T) {
    var out bytes.Buffer
    d := &Deps{Stdout: &out, Stderr: io.Discard}
    err := runUpdate(context.Background(), d, "v1.2.0", false,
        func(ctx context.Context, refresh bool) (string, error) { return "1.3.0", nil },
        func() (string, error) { return "/Users/greg/go/bin/llamactl", nil },
        nil)
    if err != nil {
        t.Fatal(err)
    }
    s := out.String()
    if !strings.Contains(s, "not installed via Homebrew") {
        t.Fatalf("missing non-brew message:\n%s", s)
    }
}
```

Plus a `recordingRunner` helper that satisfies `runner.CommandRunner`:

```go
type recordingRunner struct {
    run func(name string, args []string) error
}

func (r *recordingRunner) Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
    return r.run(name, args)
}
```

Run: FAIL.

- [ ] **Step 2: Implement**

`internal/cli/update.go`:

```go
package cli

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/gregmundy/llamactl/internal/runner"
    "github.com/spf13/cobra"
)

type latestFetcher func(ctx context.Context, refresh bool) (string, error)
type executableFn func() (string, error)

func newUpdateCmd(d *Deps) *cobra.Command {
    var refresh bool
    cmd := &cobra.Command{
        Use:   "update",
        Short: "Upgrade llamactl to the latest published version (Homebrew)",
        RunE: func(cmd *cobra.Command, args []string) error {
            cachePath := d.HFCacheDir
            if cachePath != "" {
                // Stash the version-check cache adjacent to the HF cache; same root.
                cachePath = filepath.Join(filepath.Dir(d.HFCacheDir), "last-version-check.json")
            }
            // Real production: fetch from CaskURL, exec path from os.Executable, runner from Deps.
            fetcher := func(ctx context.Context, refresh bool) (string, error) {
                return fetchLatestVersion(ctx, CaskURL, cachePath, refresh, d.Now)
            }
            return runUpdate(cmd.Context(), d, getLlamactlVersion(cmd), refresh, fetcher, os.Executable, d.Runner)
        },
    }
    cmd.Flags().BoolVar(&refresh, "refresh", false, "bypass the 24h version-check cache")
    return cmd
}

// getLlamactlVersion reads the root's bound --version string.
func getLlamactlVersion(cmd *cobra.Command) string {
    if cmd.Root() != nil {
        return cmd.Root().Version
    }
    return "dev"
}

func runUpdate(ctx context.Context, d *Deps, currentVersion string, refresh bool,
    fetch latestFetcher, executable executableFn, run runner.CommandRunner) error {

    latest, err := fetch(ctx, refresh)
    if err != nil {
        return fmt.Errorf("fetch latest version: %w", err)
    }
    fmt.Fprintf(d.Stdout, "current: %s\n", currentVersion)
    fmt.Fprintf(d.Stdout, "latest:  %s\n", latest)

    if !versionNewer(latest, currentVersion) {
        fmt.Fprintf(d.Stdout, "already on latest (%s)\n", currentVersion)
        return nil
    }

    execPath, _ := executable()
    if !isBrewInstall(execPath) {
        fmt.Fprintf(d.Stdout,
            "llamactl is not installed via Homebrew; the binary is at %s.\n"+
                "Upgrade with your installer (e.g., `go install github.com/gregmundy/llamactl/cmd/llamactl@latest`).\n",
            execPath)
        return nil
    }

    if run == nil {
        return fmt.Errorf("internal: no runner configured")
    }
    fmt.Fprintln(d.Stdout, "==> brew update")
    if err := run.Run(ctx, "brew", []string{"update"}, "", d.Stdout, d.Stderr); err != nil {
        return fmt.Errorf("brew update: %w", err)
    }
    fmt.Fprintln(d.Stdout, "==> brew upgrade gregmundy/tap/llamactl")
    if err := run.Run(ctx, "brew", []string{"upgrade", "gregmundy/tap/llamactl"}, "", d.Stdout, d.Stderr); err != nil {
        return fmt.Errorf("brew upgrade: %w", err)
    }
    fmt.Fprintln(d.Stdout, "done.")
    return nil
}

func isBrewInstall(path string) bool {
    return strings.HasPrefix(path, "/opt/homebrew/") || strings.HasPrefix(path, "/usr/local/Cellar/")
}
```

- [ ] **Step 3: Register**

`internal/cli/root.go`:
```go
rootCmd.AddCommand(newUpdateCmd(d))
```

- [ ] **Step 4: Tests + commit**

```bash
go test ./internal/cli/... -race -run TestUpdate -v
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/update.go internal/cli/update_test.go internal/cli/root.go
git commit -m "$(cat <<'EOF'
feat(update): llamactl update wraps brew upgrade

Resolves current version (via root cobra's --version, baked at build via
Task 20's ldflags), fetches latest from the tap cask file (Task 21).
On brew-installed binaries: invokes `brew update && brew upgrade
gregmundy/tap/llamactl`. On other installs (go install, manual builds):
prints a helpful message instead of silently failing.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 23: Doctor `latestVersionCheck`

**Spec:** §5.4.

**Files:**
- Modify: `internal/cli/doctor.go`
- Modify: `internal/cli/doctor_test.go`

- [ ] **Step 1: Add the check**

`internal/cli/doctor.go`:

```go
func latestVersionCheck(deps *Deps, currentVersion string) doctorCheck {
    return doctorCheck{
        label: "Latest version available",
        run: func(ctx context.Context, d *Deps) (bool, string) {
            cachePath := ""
            if d.HFCacheDir != "" {
                cachePath = filepath.Join(filepath.Dir(d.HFCacheDir), "last-version-check.json")
            }
            // Read cache only; doctor never refreshes (avoids HTTP per `doctor` invocation).
            cached, err := readVersionCache(cachePath)
            if err != nil {
                return true, "(version check skipped: no cache yet)"
            }
            if d.Now().Sub(cached.CheckedAt) > versionCacheTTL {
                return true, "(version check stale; run `llamactl update --refresh`)"
            }
            if versionNewer(cached.Latest, currentVersion) {
                return true, "update available: " + currentVersion + " → " + cached.Latest
            }
            return true, "on latest (" + currentVersion + ")"
        },
    }
}
```

The check **always returns true** — it's info-level. Doctor renders it as `✓ Latest version available — <detail>`.

- [ ] **Step 2: Wire into `buildDoctorChecks`**

Pass `currentVersion` from the existing version-aware infrastructure (root's `Version` field is exposed via `cmd.Root().Version`). Either:
- Pass currentVersion through `buildDoctorChecks` signature, OR
- Add `Deps.LlamactlVersion string` field; main.go wires it; doctor reads it.

Use the Deps approach for cleanliness — adds a field but keeps the function signature stable:

```go
// internal/cli/deps.go
type Deps struct {
    // ...
    LlamactlVersion string  // baked-in version string (e.g., "v1.3.0")
}
```

Wire in `cmd/llamactl/main.go`:
```go
deps.LlamactlVersion = llamactlVersion
```

Then `latestVersionCheck(deps)` reads `deps.LlamactlVersion`.

In `buildDoctorChecks`, append `latestVersionCheck(deps)` after `authOnPublicBindCheck`. Total checks: 14.

- [ ] **Step 3: Failing tests**

`internal/cli/doctor_test.go`:

```go
func TestLatestVersionCheckOnLatest(t *testing.T) {
    tempDir := t.TempDir()
    cacheDir := filepath.Join(tempDir, "hf-cache") // d.HFCacheDir is sibling of last-version-check.json
    cachePath := filepath.Join(tempDir, "last-version-check.json")
    raw, _ := json.Marshal(versionCache{Latest: "1.3.0", CheckedAt: time.Now()})
    os.WriteFile(cachePath, raw, 0o644)
    deps := &Deps{
        HFCacheDir:      cacheDir,
        LlamactlVersion: "v1.3.0",
        Now:             time.Now,
    }
    check := latestVersionCheck(deps, "v1.3.0")
    ok, detail := check.run(context.Background(), deps)
    if !ok {
        t.Fatal("expected pass")
    }
    if !strings.Contains(detail, "on latest") {
        t.Fatalf("missing 'on latest':\n%s", detail)
    }
}

func TestLatestVersionCheckUpdateAvailable(t *testing.T) {
    tempDir := t.TempDir()
    cacheDir := filepath.Join(tempDir, "hf-cache")
    cachePath := filepath.Join(tempDir, "last-version-check.json")
    raw, _ := json.Marshal(versionCache{Latest: "1.4.0", CheckedAt: time.Now()})
    os.WriteFile(cachePath, raw, 0o644)
    deps := &Deps{HFCacheDir: cacheDir, LlamactlVersion: "v1.3.0", Now: time.Now}
    check := latestVersionCheck(deps, "v1.3.0")
    ok, detail := check.run(context.Background(), deps)
    if !ok {
        t.Fatal("expected pass (info-level)")
    }
    if !strings.Contains(detail, "update available") {
        t.Fatalf("missing 'update available':\n%s", detail)
    }
}

func TestLatestVersionCheckSoftPassOnMissingCache(t *testing.T) {
    deps := &Deps{
        HFCacheDir:      filepath.Join(t.TempDir(), "hf-cache"),
        LlamactlVersion: "v1.3.0",
        Now:             time.Now,
    }
    check := latestVersionCheck(deps, "v1.3.0")
    ok, _ := check.run(context.Background(), deps)
    if !ok {
        t.Fatal("expected soft-pass when cache missing")
    }
}
```

- [ ] **Step 4: Bump check-count assertion to 14**

```bash
grep -n "len(checks)" internal/cli/doctor_test.go
```

Update any assertion to expect 14 checks.

- [ ] **Step 5: Tests + commit**

```bash
go test ./internal/cli/... -race -run TestLatestVersion -v
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/doctor.go internal/cli/doctor_test.go internal/cli/deps.go cmd/llamactl/main.go
git commit -m "$(cat <<'EOF'
feat(doctor): latestVersionCheck (info-level, soft-pass on cache miss)

14th doctor check. Reads the version cache populated by `llamactl
update` or `update --refresh`; doctor itself never makes an HTTP call.
Always passes (info-level); detail says 'on latest', 'update available
1.3.0 → 1.4.0', or '(version check skipped: no cache yet)' when cache
is missing.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 24: README + PRD doc updates

**Spec:** §10, §3.1, §5.1.

**Files:**
- Modify: `README.md`
- Modify: `docs/llamactl-prd-v1.5.md`

- [ ] **Step 1: README — refresh command table**

Find the "Commands" table in `README.md`. Add three rows: `config`, `update`. Bump doctor's check count to 14.

```markdown
| `config`   | `config get/set/list <key> [<value>]` — manage llamactl settings    |
| `update`   | Upgrade llamactl via Homebrew                                       |
| `doctor`   | Run 14 health checks; exits 2 on any failure                        |
```

- [ ] **Step 2: README — add "Authentication" section**

Insert between "Using the endpoint" and "Tips":

```markdown
## Authentication

By default, llamactl serves unauthenticated — anyone on your Tailnet
can reach the endpoint. To enable opt-in token authentication:

```bash
llamactl config set api_key sk-your-token-here    # or:
export LLAMACTL_API_KEY=sk-your-token-here        # env var precedence
llamactl serve qwen2.5-3b-instruct --detach       # plist embeds --api-key
```

Then clients pass `Authorization: Bearer sk-your-token-here`:

```bash
curl -H "Authorization: Bearer sk-your-token-here" \
  http://localhost:8082/v1/chat/completions \
  -d '{"model":"llamactl","messages":[{"role":"user","content":"hello"}]}'
```

`llamactl doctor` warns when a service binds publicly (0.0.0.0) without
an api_key configured.
```

- [ ] **Step 3: PRD update — document `api_key` config key**

In `docs/llamactl-prd-v1.5.md`, find line 214 (`llamactl config <key> [<value>]`) and update:

```
llamactl config <key> [<value>]
  Get/set keys: default_port, models_dir, hf_token, log_level,
  llama_server_path, api_key.
```

Also update the Non-goals section: line 55 ("Authentication on the endpoint (relies on Tailscale for access control)") add a strikethrough or note: "**(elevated to opt-in feature in v1.3.0; see §Authentication in README)**".

- [ ] **Step 4: Commit**

```bash
git add README.md docs/llamactl-prd-v1.5.md
git commit -m "$(cat <<'EOF'
docs: README adds authentication section + config/update; PRD bumps api_key

README:
- New 'Authentication' section between 'Using the endpoint' and 'Tips'
- Command table adds `config`, `update`; doctor count 12 → 14

PRD:
- `config` allowed keys list adds api_key
- Non-goals authentication note links to the new feature

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 25: Final review + live smoke + merge + tag + release

This is for the orchestrator (NOT a single implementer subagent — it spans the whole branch).

- [ ] **Step 1: Dispatch a final cross-cutting code review**

Use the `general-purpose` agent with model=opus. Prompt should:
- Diff `main...phase6a-completions`
- Verify the ParamsB float64 migration is consistent (no leftover `int` arithmetic on params)
- Verify config command's reflection allowlist matches the Config struct fields
- Verify ldflags wiring + doctor check count (14)
- Check that auth-related fixtures in serve_test use config + env both

- [ ] **Step 2: Apply review fixes**

Each fix = one commit on `phase6a-completions`.

- [ ] **Step 3: Live smoke on Apple M5**

```bash
# Stop everything; build locally
llamactl status
llamactl stop --all 2>/dev/null
go install ./cmd/llamactl

# AC: config command
llamactl config list                                              # all 6 keys, api_key (unset)
llamactl config set api_key sk-test-live                          # writes file
llamactl config get api_key                                       # → sk-test-live
llamactl config list                                              # api_key now ******** redacted
llamactl config set default_port 99999                            # → ErrUserError
llamactl config set log_level purple                              # → ErrUserError

# AC: update command
llamactl update                                                   # current=v1.2.1 latest=1.2.1 → "already on latest"
llamactl update --refresh                                         # bypasses cache; still latest

# AC: list self-heal
llamactl list                                                     # gemma-4-e4b-it now shows 7.5 B (was ?)

# AC: fit popularity-weighted
llamactl fit qwen 2.5 3b --limit 3                                # canonical Qwen/Qwen2.5-3B-Instruct-GGUF in top 3

# AC: sub-1B model support
llamactl add qwen3-0.6b                                            # downloads
llamactl list                                                     # qwen3-0.6b shows 0.6 B

# AC: doctor count = 14
llamactl doctor | grep -c '^[✓✗]'                                 # 14

# AC: auth check fires
llamactl config set api_key ""                                    # clear
llamactl serve qwen2.5-3b-instruct --detach
sleep 3
llamactl doctor                                                   # "Public-bound endpoints have api_key set" ✗
llamactl stop qwen2.5-3b-instruct

# AC: auth check satisfied
llamactl config set api_key sk-live-test
llamactl serve qwen2.5-3b-instruct --detach
sleep 3
llamactl doctor                                                   # auth check ✓
curl http://localhost:8082/v1/chat/completions ...                # → 401
curl -H "Authorization: Bearer sk-live-test" ...                  # → 200
llamactl stop qwen2.5-3b-instruct

# AC: port-conflict false positive gone
llamactl doctor                                                   # no false-positive port-conflict

# AC: doctor port-conflict on real conflict
llamactl serve qwen2.5-3b-instruct --detach
llamactl serve qwen3-0.6b --detach                                # different ports (v1.2.1 hotfix)
lsof -nP -iTCP -sTCP:LISTEN | grep llama                          # two binds on different ports
llamactl stop --all
```

If any AC fails: STOP, investigate, fix on `phase6a-completions`. Don't merge until all green.

- [ ] **Step 4: Merge**

```bash
git checkout main
git merge --no-ff phase6a-completions -m "Merge phase6a-completions: update + config + auth + 13 backlog items (v1.3.0)"
```

- [ ] **Step 5: Tag and push**

```bash
git tag -a v1.3.0 -m "v1.3.0: update + config + auth + 13 backlog items"
git push origin main
git push origin v1.3.0
```

- [ ] **Step 6: Watch release pipeline**

```bash
gh run watch
# After green, verify cask published:
curl -s https://raw.githubusercontent.com/gregmundy/homebrew-tap/main/Casks/llamactl.rb | head -5
# Should show version "1.3.0"
```

- [ ] **Step 7: Verify brew upgrade in <30s**

```bash
time brew upgrade llamactl
/opt/homebrew/bin/llamactl --version          # → llamactl version v1.3.0
/opt/homebrew/bin/llamactl --help | grep -E "config|update"
```

---

## Task 26: Update project_state.md memory

**Files:**
- Modify: `/Users/greg/.claude/projects/-Users-greg-Development-llamactl/memory/project_state.md`
- Modify: `/Users/greg/.claude/projects/-Users-greg-Development-llamactl/memory/MEMORY.md`

- [ ] **Step 1: Add a Phase 6a section**

Append after the v1.2.1 section in `project_state.md`. Record:
- Date shipped, tag (v1.3.0), merge commit
- 3 headline features + 13 backlog items
- ParamsB float64 migration as architectural change
- New PreferredIDs (qwen3-0.6b, qwen3-1.7b)
- Doctor: 12 → 14 checks
- Any live-smoke surprises caught (typical pattern: 1-3 bugs surface that synthetic tests missed)
- Anything moved out of the deferred-concerns list

- [ ] **Step 2: Update MEMORY.md index**

```md
- [llamactl project state](project_state.md) — v1.3.0 shipped 2026-05-XX (update + config + auth); Phase 6b (hot swap, spec decoding) next
```

- [ ] **Step 3: No git commit needed** — memory files live outside the repo.

---

## Self-review checklist (run before handing off to subagent-driven-development)

- **Spec coverage:** every section of `2026-05-12-phase6a-cli-completions-design.md` maps to at least one task:
  - §3 (config) → Tasks 14, 15, 16
  - §4 (auth) → Tasks 17, 18, 19
  - §5 (update) → Tasks 20, 21, 22, 23
  - §6.1 CI bump → Task 1
  - §6.2 foreground integration test → Task 3
  - §6.3 SilenceUsage → Task 2
  - §6.4 fit ranking → Task 13
  - §6.5 list self-heal → Task 12
  - §6.6 port-conflict false positive → Task 8
  - §6.7 sub-1B bundle → Tasks 9, 10, 11
  - §6.8 clock symmetry → Task 4
  - §6.9 UserHomeDir injection → Task 5
  - §6.10 HF cache GC → Task 7
  - §6.11 sentinel errors → Task 6
  - §8 acceptance criteria → Task 25 live smoke
- **Placeholder scan:** no "TODO" / "TBD" remaining. The `runUpdate` and `versionNewer` types are concrete. The `latestFetcher` / `executableFn` function types are defined.
- **Type consistency:** `ParamsB float64` used consistently from Task 9 onward; `QuantSizeTable` rows added in Task 10 match the Task 9 lookup pattern (`int(math.Round(...))`); `fitMinModelBytes` lowered in Task 11 doesn't conflict with the sub-1B PreferredIDs added in Task 10; `Deps.Config`/`Deps.ConfigPath`/`Deps.LlamactlVersion`/`Deps.UserHomeDir`/`Deps.Sleep` all consistently named and used.

If issues found at implementation time: fix inline and continue. If a task reveals a missing requirement: add a sub-task rather than re-planning.

---

## Branch discipline reminder (read before dispatching any implementer)

Every implementer subagent prompt must end with:

> "You are on branch `phase6a-completions`. Do not `git checkout`, `git switch`, `git stash`, `git reset`, or any branch-changing operation. If `git status` shows unexpected files, stop and ask. Your task is exactly Task N below; do not start Task N+1."

This primer is non-negotiable; subagents otherwise silently switch branches and lose work.
