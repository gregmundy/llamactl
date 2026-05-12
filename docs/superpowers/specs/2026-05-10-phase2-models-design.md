# llamactl Phase 2: Models — Design Spec

**Status:** Approved 2026-05-10 (this session).
**Phase 1:** shipped on `main` 2026-05-10 (commits `375ef2b..de7bd23`).
**Phase 2 scope:** PRD v1.5 §3 commands `search`, `add`, `list`, `remove`, plus §5 storage convention and §6.1 quantization selection algorithm. Covers PRD acceptance criteria **6** and **7**.

**Out of scope:** `serve`, launchd, status, stop, `config write`, `update`, anything Phase 3+.

---

## 1. Goals

1. `llamactl search <query>` — list whitelisted models matching a query, with available quants.
2. `llamactl add <model-id>` — pick the best-fitting quant for the host hardware, download to the shared GGUF directory, verify SHA256, persist per-tool metadata.
3. `llamactl list` — show installed models.
4. `llamactl remove <model-id> [--purge]` — drop per-tool metadata (and, with `--purge`, the shared GGUF).
5. Idempotent, concurrency-safe, offline-after-cache, no real HTTP/`system_profiler` in the test suite.

## 2. Non-goals

- Multi-shard GGUFs (`*.gguf-00001-of-N`). Error clearly if encountered.
- HF auth beyond optional `HF_TOKEN` env passthrough.
- Cross-tool metadata interop. `--purge` only knows about llamactl's own metadata dir.
- A `--json` output mode for any Phase 2 command (defer until a real need).
- Progress UI when stderr is not a TTY (suppress).

## 3. Architecture

### 3.1 New packages

```
internal/
├── models/          pure data + pure logic, no I/O
│   ├── whitelist.go         var Whitelist map[string]Model — curated set
│   ├── whitelist_test.go
│   ├── quants.go            Quant type, QuantSizeTable, KVCachePerTokenKB,
│   │                         canonical preference list
│   ├── selector.go          func SelectQuant(model, hw, targetCtx) (Quant, error)
│   ├── selector_test.go
│   └── metadata.go          type Metadata { ID, Repo, Quant, SHA256, GGUFPath,
│                                            SizeBytes, AddedAt }
│
├── hf/              HuggingFace metadata + range fetch + cache
│   ├── client.go            type Client; Search, RepoInfo, FetchRange
│   ├── client_test.go       httptest-backed
│   ├── cache.go             filesystem cache under ~/.cache/llamactl/
│   ├── cache_test.go
│   └── types.go             Repo, File, SearchHit DTOs
│
├── download/        file-locked, resumable, SHA256-verified download
│   ├── download.go          type Downloader; Get(ctx, Request) error
│   ├── download_test.go     httptest range responses, mid-stream interrupt, lock
│   └── progress.go          carriage-return TTY progress writer
│
└── cli/             (extended)
    ├── search.go      + search_test.go
    ├── add.go         + add_test.go
    ├── list.go        + list_test.go
    ├── remove.go      + remove_test.go
    └── deps.go        ADDS interfaces + scalar paths (§3.3)
```

### 3.2 Whitelist (PRD §4)

`var Whitelist = map[string]models.Model{ ... }` in `internal/models/whitelist.go`. Entries:

| ID | HFRepo | Arch | ParamsB | MaxCtx |
|----|--------|------|---------|--------|
| `qwen2.5-3b-instruct` | `Qwen/Qwen2.5-3B-Instruct-GGUF` | `qwen2.5` | 3 | 32768 |
| `qwen2.5-7b-instruct` | `Qwen/Qwen2.5-7B-Instruct-GGUF` | `qwen2.5` | 7 | 32768 |
| `qwen2.5-14b-instruct` | `Qwen/Qwen2.5-14B-Instruct-GGUF` | `qwen2.5` | 14 | 32768 |
| `qwen2.5-coder-3b` | `Qwen/Qwen2.5-Coder-3B-Instruct-GGUF` | `qwen2.5` | 3 | 32768 |
| `qwen2.5-coder-7b` | `Qwen/Qwen2.5-Coder-7B-Instruct-GGUF` | `qwen2.5` | 7 | 32768 |
| `qwen2.5-coder-14b` | `Qwen/Qwen2.5-Coder-14B-Instruct-GGUF` | `qwen2.5` | 14 | 32768 |
| `llama3.1-8b` | `bartowski/Meta-Llama-3.1-8B-Instruct-GGUF` | `llama3` | 8 | 131072 |
| `llama3.2-3b` | `bartowski/Llama-3.2-3B-Instruct-GGUF` | `llama3` | 3 | 131072 |
| `llama3.3-70b` | `bartowski/Llama-3.3-70B-Instruct-GGUF` | `llama3` | 70 | 131072 |
| `mistral-7b-v0.3` | `bartowski/Mistral-7B-Instruct-v0.3-GGUF` | `mistral` | 7 | 32768 |

Repos and exact param counts to be re-validated during implementation by hitting HF once and pinning. The whitelist test asserts every entry has non-empty `HFRepo`, `ParamsB > 0`, `MaxCtx > 0`.

Expanding the whitelist is a code change. PRD §4 endorses this.

### 3.3 `Deps` additions (`internal/cli/deps.go`)

Existing Phase 1 fields stay. Adds:

```go
type HFClient interface {
    Search(ctx context.Context, query string) ([]hf.SearchHit, error)
    RepoInfo(ctx context.Context, repoID string) (hf.Repo, error)
    FetchRange(ctx context.Context, repoID, file string, offset, end int64, w io.Writer) error
}

type Downloader interface {
    Get(ctx context.Context, req download.Request) error
}

type QuantSelector interface {
    Select(model models.Model, hw hardware.Info, targetCtx int) (models.Quant, error)
}

type ModelStore interface {
    List(ctx context.Context) ([]models.Metadata, error)
    Get(ctx context.Context, id string) (models.Metadata, error)
    Put(ctx context.Context, m models.Metadata) error
    Delete(ctx context.Context, id string) error
}

type FileSystem interface {
    Stat(path string) (os.FileInfo, error)
    Remove(path string) error
    MkdirAll(path string, perm os.FileMode) error
}

type Deps struct {
    // ... Phase 1 fields ...

    HFClient        HFClient
    Downloader      Downloader
    QuantSelector   QuantSelector
    ModelStore      ModelStore
    FS              FileSystem

    ModelsConfigDir string // ~/.config/llamactl/models/
    SharedModelsDir string // ~/.local/share/llama-models/
    HFCacheDir      string // ~/.cache/llamactl/
}
```

Concrete construction in `cmd/llamactl/main.go` wires `*hf.Client`, `*download.Downloader`, `*models.Selector`, `*models.FileStore`. Tests substitute in-memory fakes.

## 4. Quantization selection (PRD §6.1)

Pure function in `internal/models/selector.go`:

```go
func SelectQuant(model Model, hw hardware.Info, targetCtx int) (Quant, error)
```

Implements the PRD algorithm verbatim. Note that `hardware.Info` does **not** expose a `GPUAddressableMemoryGB` field — the selector derives it from existing Phase 1 fields:

```go
func gpuAddressableGB(hw hardware.Info) float64 {
    if hw.IogpuWiredLimitMB > 0 {
        return float64(hw.IogpuWiredLimitMB) / 1024.0
    }
    // macOS default on Apple Silicon: ~75% of RAM is GPU-addressable.
    return float64(hw.RAMBytes) / (1 << 30) * 0.75
}
```

Then:

1. `usable_gb = gpuAddressableGB(hw) − osOverheadGB(=4) − headroomGB(=2)`.
2. `kv_cache_gb = targetCtx × KVCachePerTokenKB[model.Arch][Q8_0] / 1024²`.
3. `model_budget_gb = usable_gb − kv_cache_gb`.
4. For each quant in `[Q5_K_M, Q4_K_M, Q4_K_S, IQ4_XS, IQ3_M, IQ3_XS, Q2_K]`:
   if `QuantSizeTable[model.ParamsB][quant] ≤ model_budget_gb`, return it.
5. Else return `ErrNoQuantFits` with a message suggesting a smaller model or shorter context.

Tables (`quants.go`) start with values from llama.cpp documentation + measured GGUF filesizes. The implementation task is responsible for populating concrete numbers; the spec freezes the *shape* and *fallback order*, not the values.

Selector is pure: no I/O, no clock, no env. Test is purely table-driven.

## 5. HuggingFace client (`internal/hf`)

### 5.1 Endpoints used

- `GET https://huggingface.co/api/models?search=<query>` → list of repos.
- `GET https://huggingface.co/api/models/<repoID>` → repo with `siblings[]`, each carrying `rfilename`, `lfs.sha256`, `lfs.size`.
- `GET https://huggingface.co/<repoID>/resolve/main/<rfilename>` (with `Range`) → bytes.

### 5.2 Caching

Envelope: `{"fetched_at": "<RFC3339>", "payload": <opaque>}`.

```
~/.cache/llamactl/hf-search/<sha1(query)>.json    TTL 24h
~/.cache/llamactl/hf-repo/<sha1(repoID)>.json     TTL 7d
```

- TTLs checked on read; stale entries are not pruned proactively.
- `--refresh` flag on `search` bypasses the cache and overwrites the entry.
- Per-quant SHA256 lives inside the repo cache entry; no separate forever-cache file.

### 5.3 Auth

If `HF_TOKEN` (or `LLAMACTL_HF_TOKEN`) is set, every HF request adds `Authorization: Bearer <token>`. Not required for any v1 whitelist repo.

### 5.4 Retry policy

- Idempotent GETs: exponential backoff `1s, 2s, 4s`, max 3 attempts on 5xx or `net.Error` with `Timeout()/Temporary()`.
- 4xx returns immediately as a typed error.
- Range streams are not retried inside `FetchRange`; the next `add` invocation resumes via the partial file.

## 6. Download orchestrator (`internal/download`)

### 6.1 Request

```go
type Request struct {
    RepoID         string
    File           string
    DestPath       string // .../<model-id>/<quant>.gguf
    ExpectedSHA256 string // hex, from hf.Repo.siblings[].lfs.sha256
    Progress       *Progress // optional state-bearing struct that tracks byte count + rate
                             // (see internal/download/progress.go). Nil disables progress reporting.
}
```

### 6.2 Algorithm

```
1. mkdir -p filepath.Dir(DestPath).
2. partial := DestPath + ".partial".
3. Open partial with O_CREAT|O_RDWR|0o644.
4. unix.Flock(fd, LOCK_EX).
   If contended, log "another llamactl is downloading <basename>; waiting…" once,
   then block.
5. resumeOffset, _ := f.Seek(0, io.SeekEnd).
6. h := sha256.New().
   If resumeOffset > 0:
     re-hash existing partial bytes from disk into h
     (Seek(0,0), io.CopyN(h, f, resumeOffset), Seek(0,2)).
7. hf.FetchRange(ctx, repo, file, resumeOffset, 0, io.MultiWriter(f, h, progress)).
   If server returned 200 instead of 206 (no range support):
     truncate partial, reset h, restart from offset 0 once.
8. fsync(f), close(f) → releases flock.
9. if hex(h.Sum(nil)) != ExpectedSHA256:
     os.Remove(partial); return ErrSHAMismatch.
10. os.Rename(partial, DestPath).
```

### 6.3 Progress

`progress.go` wraps an `io.Writer` and emits `\r<pct>%  <mb>/<mb>  <speed MiB/s>  ETA <hh:mm:ss>` to stderr at most every 250 ms. No-op when `golang.org/x/term.IsTerminal(int(stderr.Fd())) == false`. Final write is a newline to leave the line clean.

### 6.4 Concurrency invariants

- Two `add` invocations for different models: independent, no contention.
- Two `add` invocations for the same model: serialized by flock; second wakes after first releases, sees the final GGUF exists with matching SHA, takes the dedupe fast path, becomes a metadata-only no-op.
- `add` racing `remove --purge` on the same model: `remove --purge` refuses if `.partial` exists; if only the final file exists, narrow window where `remove` may delete just before `add` writes metadata — acceptable for v1.

## 7. Storage layout (PRD §5, unchanged)

```
~/.config/llamactl/models/<model-id>.json     per-tool metadata
~/.local/share/llama-models/<model-id>/<quant>.gguf            shared file
~/.local/share/llama-models/<model-id>/<quant>.gguf.partial    in-progress, flocked
~/.cache/llamactl/hf-search/<sha1>.json                         24h TTL
~/.cache/llamactl/hf-repo/<sha1>.json                           7d TTL
```

`models.Metadata` JSON shape:

```json
{
  "id": "qwen2.5-7b-instruct",
  "repo": "Qwen/Qwen2.5-7B-Instruct-GGUF",
  "quant": "Q4_K_M",
  "sha256": "abcdef...",
  "gguf_path": "/Users/greg/.local/share/llama-models/qwen2.5-7b-instruct/Q4_K_M.gguf",
  "size_bytes": 4731234567,
  "added_at": "2026-05-10T10:00:00Z"
}
```

## 8. Command surface

### 8.1 `search <query> [--refresh]`

```
1. Load whitelist.
2. cache.Get("search/" + query) unless --refresh.
3. On miss: hf.Search(query); cache.Put.
4. RepoInfo for each whitelisted result (7d cache).
5. tabwriter table to stdout. Columns:
   MODEL-ID    PARAMS    QUANTS    REPO
6. Exit 0 even if empty. Exit 2 on HF unreachable.
```

### 8.2 `add <model-id> [--quant <preset>] [--ctx <int>]`

```
 1. Resolve <model-id> in whitelist; ErrUserError listing IDs if absent.
 2. Load hardware:
      hardware.json exists → read.
      else → HardwareDetector.Detect(); config.WriteHardware (auto-bootstrap).
 3. Compute quant:
      --quant given → validate it's a known Quant value, use as-is.
                      (Existence on HF is checked in step 4.)
      else → QuantSelector.Select(model, hw, --ctx or 8192).
 4. hf.RepoInfo(model.HFRepo); locate the .gguf matching <model>-<quant>.gguf
    (case-insensitive). Multi-shard files → ErrUserError. Missing quant
    (only with --quant override) → ErrUserError listing available quants.
    Capture expectedSHA = sibling.lfs.sha256.
 5. destPath = <SharedModelsDir>/<model-id>/<quant>.gguf.
 6. If destPath exists and sha256sum(destPath) == expectedSHA → skip download
    (PRD AC#7). The on-disk SHA is computed fresh; we do not trust a prior
    Metadata.SHA256 here (the file may have been replaced by another tool).
 7. Else Downloader.Get(...) with progress on stderr.
 8. ModelStore.Put(Metadata{...}).
 9. Print: "installed <model-id> (<quant>, <human-size>) → <destPath>".
```

### 8.3 `list`

```
1. ModelStore.List().
2. For each entry, FS.Stat(gguf_path); mark "(missing)" if it fails.
3. tabwriter table. Columns:
   MODEL-ID    QUANT    SIZE    PATH    ADDED
```

### 8.4 `remove <model-id> [--purge]`

```
1. ModelStore.Get(id); ErrUserError if absent.
2. Default (no --purge): ModelStore.Delete(id). Shared GGUF stays.
3. With --purge:
     a. Log: "best-effort: cannot detect other tools' use of this file".
     b. If <gguf_path>.partial exists → ErrUserError ("download in progress").
     c. FS.Remove(gguf_path).
     d. FS.Remove(filepath.Dir(gguf_path)) only if empty.
     e. ModelStore.Delete(id).
4. Print outcome.
```

## 9. Error model (unchanged from Phase 1)

- `ErrUserError` → exit code 2, no `llamactl:` prefix.
- Other errors → exit code 1, wrapped with `llamactl:` prefix.
- All commands accept `ctx context.Context` plumbed from cobra. SIGINT cancels in-flight downloads; the `.partial` survives for resume on next `add`.

## 10. Testing strategy

### 10.1 Coverage

- `internal/models`
  - `whitelist_test.go`: every entry has non-empty fields.
  - `selector_test.go`: table-driven `(model_size_b, hw_gb, target_ctx) → expected_quant_or_err`. Walks PRD §6.1 examples: 16 GB + qwen2.5-7b + 8192 → Q4_K_M; 8 GB + llama3.3-70b → "none fit"; 64 GB + qwen2.5-7b → Q5_K_M.
- `internal/hf`
  - `client_test.go`: `httptest.Server` returning canned `/api/models` and `/api/models/<repo>` JSON. Asserts cache hit/miss, `--refresh`, 4xx → typed error, 5xx → retried then fails.
  - `cache_test.go`: `t.TempDir()`, asserts TTL boundaries, envelope decode.
- `internal/download`
  - `download_test.go`: httptest with deterministic Range handling, no-range variant (200 + full bytes → restart-once path), resume from non-zero offset, SHA mismatch → typed error + unlink partial, lock contention via two in-process goroutines, `ctx.Cancel` mid-stream leaves a valid partial.
  - `progress_test.go`: fake clock, 1 MiB through `bytes.Buffer`; suppressed when `IsTerminal` is false.
- `internal/cli`
  - `search_test.go`: fake `HFClient`; asserts whitelist filter and tabwriter alignment.
  - `add_test.go`: full happy path; AC#7 dedupe (zero Downloader calls); `--quant` override; unknown model; "no quant fits"; auto hardware bootstrap.
  - `list_test.go`: two entries, one missing GGUF → "(missing)".
  - `remove_test.go`: default metadata-only; `--purge`; `--purge` with `.partial` present refuses; unknown id.
  - `integration_test.go`: end-to-end add → list → remove --purge with all fakes; verifies on-disk state.

### 10.2 Discipline

- Zero real network, zero real `system_profiler`, zero real GGUF files.
- `runner.CommandRunner` and `httptest.Server` are the only seams.
- Subagent prompts during execution name the branch (`phase2-models`) and forbid `git checkout/switch/stash/branch`, per the project's branch-safety practice.

## 11. PRD acceptance criteria addressed

| AC | Requirement | Covered by |
|----|-------------|------------|
| 6  | `add qwen2.5-7b` on 16 GB host selects Q4_K_M, downloads, verifies SHA, completes in <10 min on 100 Mbps | §4 selector + §6 downloader |
| 7  | If GGUF already present at shared path with matching SHA, `add` skips download and only writes metadata | §6.2 dedupe fast path; `add_test.go` |

(Phase 3 covers AC#1, #8–10, #13–16. Phase 4 covers `config write` and `update`. Phase 1 covered AC#2–5, #11, #12.)

## 12. Deferred concerns from Phase 1 (out of Phase 2 scope unless trivially in-the-way)

- Shared `internal/testutil.FakeRunner` across packages — defer unless test friction grows during Phase 2.
- `Prober.Probe` TOCTOU window — irrelevant until Phase 3.
- `Resolver.Resolve` double-invocation memoization — irrelevant until Phase 3.
- `Deps.LookPath` and `Deps.Now` unused — `Deps.Now` becomes used by `add` (`AddedAt`); `LookPath` stays unused until Phase 3.

## 13. Risks

- HF API schema drift: the `siblings[].lfs.sha256` field is undocumented-but-stable; if it disappears we lose the dedupe fast path. Mitigation: integration test pins the JSON shape we expect.
- Multi-shard GGUFs: a future whitelist addition could ship as split files; we explicitly error for v1.
- macOS flock vs fcntl semantics: `unix.Flock` uses BSD flock, which is per-fd. Verified by tests; if surprises emerge we fall back to `unix.FcntlFlock`.
- The whitelist's bartowski/Llama-3.x repos are community uploads — verify they're still authoritative at implementation time, swap to Meta's own repos if available.
