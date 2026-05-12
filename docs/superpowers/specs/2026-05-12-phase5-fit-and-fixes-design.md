# llamactl Phase 5: `fit` command + backlog drain — Design Spec

**Status:** Approved 2026-05-12
**Ships as:** `v1.2.0`
**Branch:** `phase5-fit-and-fixes`
**Covers:** new `fit` discovery+sizing command + 19 deferred items spanning bugs, hygiene, README polish, and minor refactors.

---

## 1. Goal

Phase 4 shipped v1.0.0 (MVP). Phase 5 has two themes:

1. **Headliner feature — `fit`:** discovery + per-quant fit verdict against the host, with optional `--install` shortcut. Mostly replaces `search` for the "I want a new model" flow.
2. **Backlog drain:** clear 19 accumulated deferred items, ranging from real bugs (GGUF parser returns `ParamsCount=0` for valid files) to cosmetic UX cleanup (port-0 message) to operational hardening (log rotation, HF cache pruning).

Ships as `v1.2.0` — breadth justifies the minor bump despite no breaking API changes.

Out of scope (still deferred): `update` self-updater, `config <key> <value>` writes, endpoint auth, hot model swap, status for foreground serves, Linux/Intel support.

---

## 2. Architecture overview

All work on a single branch (`phase5-fit-and-fixes`). New code mostly lives in `internal/cli/fit.go` and `internal/models/paramcount.go`; the rest is targeted edits to existing files. No new external dependencies. Same GoReleaser → cask → `brew upgrade` distribution path as v1.0.0.

---

## 3. The `fit` command

### 3.1 UX

```
$ llamactl fit gemma 4
RECOMMENDED  REPO                              QUANT    SIZE      VERDICT      NOTES
   ✓         unsloth/gemma-4-E4B-it-GGUF       Q5_K_M   3.4 GB    20 GB free   E4B effective ~4B
   ✓         unsloth/gemma-4-26B-A4B-it-GGUF   Q4_K_M   14.0 GB   6 GB free    MoE, 4B active
   ⚠         unsloth/gemma-4-31B-it-GGUF       Q4_K_M   17.2 GB   2 GB free    tight headroom
   ✗         unsloth/gemma-4-31B-it-GGUF       Q5_K_M   20.5 GB   exceeds      need 4 GB more
```

Top ✓ row is what `--install` picks. Plain `fit` is lookup-only — prints the table and exits.

### 3.2 Flags

| Flag | Effect |
|------|--------|
| `--install` | Run `add` on the top-ranked ✓ row. Errors if there are no ✓ rows. |
| `--ctx N` | Override context size for KV-cache estimation (default: recipe `chat` 8K). |
| `--limit N` | Cap rows shown (default 10). |
| `--json` | Emit machine-readable JSON (same fields as table). |

### 3.3 Verdict logic

`usable := models.GpuAddressableGB(hw) - models.OSOverheadGB - models.HeadroomGB`

For each `(repo, quant)`:
- Compute `total := quant_size_gb + kvCacheGB(arch, params, ctx)`
- `usable - total >= 4` → `✓` with note `"<N> GB free"`
- `0 <= usable - total < 4` → `⚠` with note `"tight headroom"` and `"<N> GB free"`
- `usable < total` → `✗` with note `"need <N> GB more"`

The 4 GB threshold matches the existing `MlockAuto` heuristic in `recipes.FlagsFor`.

### 3.4 Param-count extraction

New file: `internal/models/paramcount.go`

```go
// ParseParamCountFromRepo extracts a parameter count (in billions) from
// a HuggingFace repo path. Returns 0 if no recognizable pattern is found.
//
// Patterns tried in order:
//   "qwen2.5-7b-instruct"        → 7
//   "Qwen3-0.6B-GGUF"            → 0.6
//   "unsloth/gemma-4-31B-it-GGUF" → 31
//   "unsloth/gemma-4-E4B-it-GGUF" → 4   (E4B effective)
//   "Llama-3.3-70B-Instruct"     → 70
func ParseParamCountFromRepo(repo string) float64
```

Implementation: two regexes attempted left-to-right per path segment:
- `E(\d+(?:\.\d+)?)B` — captures `E4B` → `4`
- `(\d+(?:\.\d+)?)[bB]` — captures `7b`, `0.6B`, `70B`

Returns the FIRST match per path; if multiple segments match, prefer the repo name (last segment) over the owner. Returns 0 when unparseable; caller treats as "unknown, conservative fallback" (13B).

### 3.5 KV cache estimation

```go
// KVCacheGB estimates the KV-cache memory footprint at runtime.
// arch + paramsB drive head count and head dim via existing
// models.KVCachePerTokenKB[arch]. ctx is the requested context length.
// f16 cache is assumed (worst case among the chat-recipe defaults).
func KVCacheGB(arch models.Arch, paramsB float64, ctx int) float64
```

Reuses `models.KVCachePerTokenKB` from Phase 2. Returns 0 when arch is unknown — caller adds a conservative ~1 GB padding instead. Implementation lives alongside `paramcount.go` for cohesion.

### 3.6 HFClient extensions

`HFClient.RepoInfo(repoID)` already exists; returns blob sizes per file. `fit` uses it as-is.

For discovery, `fit` reuses `HFClient.Search(query)` — same path used today by the `search` command.

### 3.7 Tests

`internal/cli/fit_test.go`:
- `TestFitShowsRankedTable` — fake HFClient returns 3 repos with various sizes; assert sort order and verdict assignment.
- `TestFitNoGGUFRepos` — search returns only original-weights repos (no GGUF); assert "no GGUF repos matched" message.
- `TestFitAllExceedHost` — small host, large model variants; assert all ✗ rows are still shown with deficit notes.
- `TestFitInstallShortcut` — `--install` flag picks top ✓ and calls add. Fake downloader records the call. Assert correct repo+quant passed.
- `TestFitInstallNoCandidate` — `--install` with no ✓ rows → user-error exit.
- `TestFitJSON` — `--json` flag emits valid JSON with all expected fields.

`internal/models/paramcount_test.go`:
- Table-driven tests covering all five repo-path patterns from §3.4.
- Edge cases: empty string, no digits, multiple matches (verify repo-name preference).

---

## 4. Genuine bugs (8 items)

### 4.1 GGUF parser returns `ParamsCount=0`

Phase 4 Task 5 ran `cmd/gguf-inspect` against Gemma 4 E4B AND Qwen 2.5 3B; both reported `ParamsCount = 0`. The static `PreferredIDs` map saved Qwen's `list` display, but HF-path adds (Gemma) silently store `ParamsB=0` in metadata.

**Investigation:** add a one-off test that opens the local Qwen GGUF, dumps the raw `general.*` key-value pairs (not just the parsed Header struct), and prints the byte position + GGUF type code for `general.parameter_count`. Compare to what `parseHeader` is actually reading. The most likely causes:

- Parser looks up `parameter_count` instead of `general.parameter_count`
- Parser reads the value as `uint32` when the GGUF file stores it as `uint64`
- Parser reads it correctly but a later kv pair overwrites it (key collision)
- The `readLimit` of 64 MiB is exhausted before reaching `general.parameter_count` (it's late in the metadata block on some files)

**Fix:** depends on root cause. Likely a 1-3 line correction in `internal/gguf/header.go`. Add a real-file integration test that runs against `~/.local/share/llama-models/qwen2.5-3b-instruct/Q5_K_M.gguf` and asserts `ParamsCount == 3_000_000_000` (±10%); skip the test gracefully if the file is absent.

### 4.2 `add` dedupe path double-prints

When `add <id>` is run and the model is already installed, output today is:
```
qwen2.5-3b-instruct: already present at /Users/greg/...
installed qwen2.5-3b-instruct (Q5_K_M, 2.3 GiB) -> /Users/greg/...
```

Second line is misleading — nothing was installed. Trace `finishAdd` in `add.go`; conditional the "installed" print on `wasNewlyInstalled bool` returned by the downloader. Test: dedupe path emits only the "already present" line.

### 4.3 `Downloader.Get` double SHA verify

`add.go` calls `verifyExisting` (full SHA-256 recompute) before invoking `Downloader.Get`, which internally also verifies post-write. For a 5 GB GGUF, the upper-layer verify is 30+ seconds of pure waste.

**Fix:** remove the upper-layer call. Push verification entirely into `Downloader.Get`. Add a comment in `add.go` explaining the responsibility lives in the downloader. Test: existing tests already cover the verify path; ensure they still pass.

### 4.4 Silent flock contention

`Downloader.Get` blocks on `flock` when another llamactl is downloading the same model. Today it blocks silently. PRD wanted a one-line log.

**Fix:** in `Downloader.Get`, try a non-blocking `flock` first; if it fails with `EWOULDBLOCK`, emit `fmt.Fprintf(stderr, "another llamactl instance is downloading %s; waiting…\n", repoID)` and then call the blocking `flock`. Use an exported `io.Writer` field on the Downloader (`Stderr io.Writer`) defaulting to `os.Stderr`. Test: spawn two goroutines, capture the second's stderr, assert exactly one "waiting" message.

### 4.5 `--port 0` message

`serve` accepts `--port 0` to ask the kernel for an ephemeral port. The current "bound to :N (:0 was in use)" message is wrong wording.

**Fix:** in `serve.go`, check if `requestedPort == 0`; if so, print `"bound to ephemeral :%d\n"` and skip the "was in use" wording.

### 4.6 `parseEtime` silent atoi errors

In `internal/proc/ps.go`, `parseEtime` uses `_, _ = strconv.Atoi(part)` for the day-prefix and hour/min/sec parts. Malformed input silently parses as 0.

**Fix:** check each error and return `0, fmt.Errorf("parse etime %q: %w", s, err)`. Add a test for malformed input like `"abc:de"` asserting non-nil error.

### 4.7 `diskSpaceCheck` remediation wording

If `syscall.Statfs(SharedModelsDir)` fails because the dir doesn't exist (rare but possible — fresh install before any `add`), the doctor remediation "free up space" is wrong.

**Fix:** in `diskSpaceCheck`, if `Statfs` returns `ENOENT`-like error, set a different remediation: `"create the models directory: mkdir -p ~/.local/share/llama-models"`.

### 4.8 `status.go` open-coded prefix strip

```go
id := svc.Label
if len(id) > len("com.llamactl.") && id[:len("com.llamactl.")] == "com.llamactl." {
    id = id[len("com.llamactl."):]
}
```

Replace with:
```go
id := strings.TrimPrefix(svc.Label, "com.llamactl.")
```

Same pattern as `stop.go` and `doctor.go`.

---

## 5. Hygiene & operational items (5)

### 5.1 Log rotation

New file: `internal/cli/logrotate.go`

```go
// RotateIfLarge rotates path → path.1 → path.2 → ... → path.<keep> when
// path's size exceeds maxBytes. Older numbered files past `keep` are removed.
// Returns true if rotation happened.
func RotateIfLarge(path string, maxBytes int64, keep int) (bool, error)
```

**Wiring:**
- `serve.go` `runServeForeground` calls `RotateIfLarge(logPath, 10<<20, 3)` before opening the log for write.
- `serve.go` `runServeDetached` does the same before plist regen.
- `doctor.go` new check `logFilesNotOversized` flags any `~/Library/Logs/llamactl/*.log` exceeding 10 MiB (doctor doesn't auto-rotate; just reports). Total doctor checks goes from 10 to 11.

**Tests:**
- `logrotate_test.go` — set up files, call RotateIfLarge, assert post-state matches expectation.
- `doctor_test.go` extended with one OK + one Failure pair for the new check.

### 5.2 HF cache pruning

Two pieces:

**Auto-prune** in `internal/hf/cache.go`:
```go
// PruneOlderThan removes cache files older than d. Returns count removed.
func (c *Cache) PruneOlderThan(d time.Duration) (int, error)
```
Called lazily at the start of `Search` and `RepoInfo` with `d = 30 * 24 * time.Hour`. Best-effort: prune errors are logged to stderr, never block the operation.

**Manual command** in `internal/cli/cache.go`:
```
llamactl cache prune [--all]
```
- Without `--all`: prune entries older than 30 days (same as auto).
- With `--all`: nuke `~/.cache/llamactl/hf-*` entirely.

`cache` is a parent command with `prune` as subcommand (parallels `add`, `list`, etc).

**Doctor check** new `hfCacheSize` flags when cache exceeds 500 MiB (~suggests `cache prune`). Doctor goes from 11 → 12 checks.

### 5.3 Resolver memoize

`internal/server/resolve.go` (or wherever `Resolver` lives): add the same caching pattern as `Prober`:

```go
type Resolver struct {
    Getenv     func(string) string
    LookPath   func(string) (string, error)
    HomeDir    string
    ConfigPath string
    Runner     CommandRunner

    mu     sync.Mutex
    cached *Resolution
}
```

First `Resolve` computes; subsequent return cached. Doctor's "llama-server is resolvable" and "llama-server version meets floor" checks both call Resolve; today that's 2× the work for no reason.

Test: fake `LookPath` increments a counter; call `Resolve` twice; assert counter == 1.

### 5.4 `CommandRunner` "stdin" → "dir" rename

The `runner.CommandRunner` interface signature is:
```go
Run(ctx context.Context, name string, args []string, stdin string, stdout, stderr io.Writer) error
```

The 4th positional arg is named "stdin" but `internal/runner.ExecRunner` treats it as the working directory. Every caller passes `""` so behavior is unaffected, but the naming is wrong.

**Fix:** rename in:
- `internal/runner/runner.go` (defining package)
- `internal/launchd/service.go` (local interface declaration)
- `internal/proc/ps.go` (local interface declaration)
- `internal/server/probe.go` and any other local-redeclarations
- `internal/hardware/...`
- Every fake runner in `*_test.go`

Mechanical search-and-replace. Add a comment to the interface doc explaining what `dir` is.

### 5.5 Detached poll loop ctx cancellation

`internal/cli/serve.go` `runServeDetached`'s polling loop uses bare `time.Sleep(detachPollInterval)` which doesn't honor context cancel:

```go
for {
    info, _ := d.LaunchdService.Print(ctx, label)
    if info.PID > 0 { return nil }
    // ...
    time.Sleep(detachPollInterval)  // <-- doesn't respect ctx
}
```

**Fix:**
```go
select {
case <-ctx.Done():
    return ctx.Err()
case <-time.After(detachPollInterval):
}
```

Now SIGINT during a detached service startup breaks the poll cleanly.

---

## 6. README polish (2)

### 6.1 Add "Using the endpoint" section

Insert between Quick Start and Commands. Concrete code samples in Python + JS, then editor integrations, then Tailnet access. ~25 lines total.

```markdown
## Using the endpoint

llama-server's OpenAI-compatible API works with any client that takes a `base_url`.

### Python

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8080/v1", api_key="not-needed")
resp = client.chat.completions.create(
    model="llamactl",  # any non-empty string
    messages=[{"role": "user", "content": "hello"}],
)
print(resp.choices[0].message.content)
```

### JavaScript

```js
import OpenAI from "openai";

const client = new OpenAI({ baseURL: "http://localhost:8080/v1", apiKey: "not-needed" });
const resp = await client.chat.completions.create({
  model: "llamactl",
  messages: [{ role: "user", content: "hello" }],
});
```

### Editor integrations

Aider, Continue, Cursor, and Zed all accept a custom OpenAI base URL. Set the model
provider to OpenAI-compatible and the base URL to `http://localhost:8080/v1` (or the
port shown by `llamactl status`).

### From another machine on your Tailnet

llama-server binds to `0.0.0.0`, so any host on your Tailnet can reach it:

```
base_url=http://llm-mini.tailnet.ts.net:8080/v1
```

### 6.2 Add "Tips" section

Insert before License:

```markdown
## Tips

- Default port is 8080. If it's busy, `serve` shifts to the next free port in [8080, 8180); `status` shows the actual port.
- Per-model logs live at `~/Library/Logs/llamactl/<id>.log` — `tail -f` for live debugging. Rotated automatically at 10 MiB (kept: 3 generations).
- Detached services survive reboot (launchd `RunAtLoad` + `KeepAlive`). Run `stop` to free GPU memory; `serve --detach` to bring it back.
- `llamactl fit <query>` ranks new HuggingFace models against your host before download.
- `llamactl cache prune` clears stale HuggingFace API cache (auto-pruned at 30 days but command lets you force it).
```

---

## 7. Refactors (4)

### 7.1 `download.Request` spec/code drift

Spec said `ProgressOut io.Writer`. Code uses `Progress *Progress` (a struct). Decide which to keep:
- **Code current behavior** preferred — `*Progress` provides byte-count + rate state that a plain Writer can't track structured.
- **Action:** keep `Progress *Progress`. Update the spec/PRD reference at `docs/llamactl-prd-v1.5.md` to match code. No code change.

### 7.2 Consolidate `buildGGUF` test helpers

Today three files have near-identical helpers:
- `internal/gguf/header_test.go:18` `buildGGUF`
- `internal/cli/add_test.go` similar
- `internal/cli/integration_test.go` similar

**Action:** extract to `internal/gguftest/builder.go`:

```go
package gguftest

// Build returns synthetic GGUF bytes with the given metadata kv pairs.
func Build(t *testing.T, version uint32, kvs ...KV) []byte

type KV struct {
    Key   string
    Type  uint32
    Value any
}
```

Update the 3 callers. This task ties to bug 4.1 (GGUF parser) since the parser fix will want new test fixtures; better to land the consolidation first.

### 7.3 Unify `fakeRunner` key construction

Today:
- `internal/hardware/*_test.go` uses `name + " " + args[0]`
- `internal/server/*_test.go`, `internal/launchd/*_test.go`, `internal/proc/*_test.go` use `name + " " + strings.Join(args, " ")`
- `internal/cli/integration_test.go`'s `intRunner` matches the latter

**Action:** new `internal/testutil/fakerunner.go`:

```go
package testutil

// FakeRunner is a controllable CommandRunner used across packages.
// Keys outputs/errs maps by `name + " " + strings.Join(args, " ")`.
type FakeRunner struct {
    Outputs map[string]string
    Errs    map[string]error
    Calls   []string
}

func (f *FakeRunner) Run(ctx context.Context, name string, args []string, dir string,
    stdout, stderr io.Writer) error { /* ... */ }
```

Migrate the existing fakes. The `hardware` tests need updating because they use `args[0]` keying — change their fixtures to use full-args keying.

### 7.4 `platform.Default{}.Cores()` coupling

`recipes.FlagsFor` calls `platform.Default{}.Cores()` directly, hard-coupling to the production platform type. Tests can't override.

**Action:** simplest fix — add `Cores int` field to recipes' input, computed by the caller:

```go
func FlagsFor(r Recipe, m models.Model, _ models.Quant, ggufPath string,
    hw hardware.Info, ver server.Version, caps server.Capabilities,
    sizeGB float64, port int, cores int) []string
```

Caller in `serve.go` passes `platform.Default{}.Cores()`. Tests pass a fixed value. One more positional arg but it's pure data, not a behavior seam.

**Alternative considered and rejected:** package-level var `platform.Cores = Default{}.Cores`. Cleaner test API but creates a global mutable that surprises readers.

---

## 8. Sequence

Tasks land in this order on `phase5-fit-and-fixes`:

1. **Refactor 7.2** — extract `internal/gguftest` (no behavior change; needed before 4.1's new tests)
2. **Bug 4.1** — fix GGUF parser; add real-file integration test using the new gguftest helpers
3. **Bug 4.8** — `status.go` prefix strip (one-line cleanup; group with 4.1's commit if it touches `status.go` tests)
4. **Bug 4.2** — `add` dedupe message
5. **Bug 4.3** — remove double-SHA verify
6. **Bug 4.4** — flock contention log
7. **Bug 4.5** — `--port 0` message
8. **Bug 4.6** — `parseEtime` error wrapping
9. **Bug 4.7** — `diskSpaceCheck` remediation
10. **Hygiene 5.3** — Resolver memoize (small, isolated)
11. **Hygiene 5.5** — detached poll ctx fix
12. **Hygiene 5.1** — log rotation + new doctor check
13. **Hygiene 5.2** — HF cache prune + new `cache` subcommand + new doctor check
14. **Refactor 7.3** — unify `fakeRunner` (touches many test files; do after most behavior is stable)
15. **Refactor 7.4** — `platform.Cores` parameterization
16. **Refactor 5.4** — `CommandRunner` "stdin" → "dir" rename (very mechanical; do near the end for low merge-conflict risk)
17. **Refactor 7.1** — `download.Request` spec/PRD doc update
18. **Feature: `fit` command** — depends on 7.2 (gguftest), 4.1 (parser fix improves accuracy though regex still preferred for sizing), 5.2 (cache), and uses existing HFClient/RepoInfo
19. **README 6.1, 6.2** — both sections in one commit, last
20. Merge → tag `v1.2.0` → release pipeline same as Phase 4

Each task = one commit on the feature branch.

---

## 9. Acceptance criteria

Phase 5 ships when:

- ✅ All 22 tasks above complete; each has its own commit on `phase5-fit-and-fixes`.
- ✅ `go test ./... -race` clean; `go vet ./...` clean.
- ✅ Live: `llamactl fit gemma 4` returns a populated ranked table.
- ✅ Live: `llamactl fit gemma 4 --install` actually adds the recommended model.
- ✅ Live: `cmd/gguf-inspect` against a freshly-downloaded model shows non-zero `ParamsCount` (proves bug 4.1 fix).
- ✅ Live: after spamming `serve foo --detach` enough to push the log past 10 MiB, the next `serve` rotates it (`<id>.log.1` appears).
- ✅ Live: `llamactl cache prune --all` empties `~/.cache/llamactl/hf-*`.
- ✅ `llamactl doctor` reports 12 checks (added: log size, HF cache size).
- ✅ `brew upgrade llamactl` picks up v1.2.0 in <30 s.
- ✅ README "Using the endpoint" and "Tips" sections render correctly on GitHub.
- ✅ `project_state.md` memory updated.

---

## 10. Non-goals / risks

- **No breaking API changes.** `FlagsFor`'s new `cores` arg is a positional internal API; callers all live in this repo.
- **No new external dependencies.** Everything is stdlib + existing imports.
- **`fit` and `search` coexist.** `fit` doesn't replace `search`; the latter remains for quick browsing without sizing math.
- **GGUF parser fix risk:** depending on what 4.1 finds, the fix might touch more than the parameter_count path (e.g., reveal a class of metadata-block bugs). The investigation phase has up to ~30 minutes budgeted; if the fix balloons, narrow to "make `general.parameter_count` work" and file the rest as Phase 6.
- **Log rotation race:** `serve` rotates before opening for write, then opens. Another instance writing concurrently could race. Mitigation: rotation runs under the existing `internal/download.flock`-style lock OR we accept that two foreground serves on the same model are already racy elsewhere. Document and skip.
