# llamactl Phase 6a: CLI completions + backlog drain — Design Spec

**Status:** Approved 2026-05-12
**Ships as:** `v1.3.0`
**Branch:** `phase6a-completions`
**Covers:** the two outstanding PRD CLI line items (`update`, `config`), opt-in endpoint authentication, plus 13 backlog items from Phase 5 testing.

---

## 1. Goal

Phase 5 (v1.2.0 + v1.2.1 hotfix) drained 20+ items including the new `fit` command. Two PRD §CLI line items remain undone: `llamactl update` and `llamactl config <key> [<value>]`. Phase 6a finishes those, adds opt-in endpoint authentication (a PRD §Non-goals item being re-elevated because Tailnet exposure means anyone on the tailnet can hit the endpoint), and drains 13 small backlog items surfaced during Phase 5 testing.

Ships as `v1.3.0` — minor bump for the new commands and auth surface, no breaking API changes.

Out of scope: hot model swap, speculative decoding, GGUF tensor-shape parsing → Phase 6b. Web UI → Phase 6c.

---

## 2. Architecture overview

All work on a single branch (`phase6a-completions`). No new packages. Three pieces of new code, all in existing layers:

```
internal/config/config.go      + Save(path, cfg) error    (atomic write+rename)
                               + struct field: APIKey string `yaml:"api_key"`

internal/cli/config.go         NEW: newConfigCmd(d) cobra → get/set/list subcommands
internal/cli/update.go         NEW: newUpdateCmd(d) cobra → brew wrapper + version check
internal/cli/version.go        NEW: fetchLatestVersion + currentVersion helpers

internal/cli/serve.go          + read d.Config.APIKey / LLAMACTL_API_KEY env
                               + append --api-key <token> to llama-server args when set

internal/cli/doctor.go         + authOnPublicBindCheck  (warns 0.0.0.0 + no api_key)
                               + latestVersionCheck     (info-level; soft-pass offline)

internal/cli/deps.go           + Config *config.Config
                               + UserHomeDir func() (string, error)   (#23)
```

**No interface changes.** `Deps` gains two fields (additive). `config.Config` gains one field (additive). All new code follows the established `Deps` + narrow-interface + `runner.CommandRunner` seam pattern.

Same GoReleaser → cask → `brew upgrade` distribution path as v1.0.0.

---

## 3. The `config` command (PRD §CLI line 214)

### 3.1 UX

```
$ llamactl config get default_port
8080

$ llamactl config set api_key sk-abc123
api_key updated

$ llamactl config list
api_key             ********  (set; redacted)
default_port        8080
hf_token            (unset)
llama_server_path   (unset)
log_level           (unset)
models_dir          (unset)
```

### 3.2 Subcommands

| Subcommand | Behavior |
|---|---|
| `config get <key>` | Prints the value (or empty line) for the named key. Unknown key → `ErrUserError` exit 2. |
| `config set <key> <value>` | Writes the value to `~/.config/llamactl/config.yaml` via atomic temp+rename. Unknown key → user error. Empty value clears the key (writes zero-value). |
| `config list` | Tabular listing of all 6 keys, current values; `api_key` and `hf_token` shown as `********` if set, `(unset)` if zero-value. |

### 3.3 Validation

Allowed keys are derived from `config.Config` struct field tags (`yaml:"..."`) — the validator iterates struct fields at runtime via reflection so adding a new field automatically adds it to the allowlist. The 6 keys are:

```
llama_server_path
default_port
models_dir
hf_token
log_level
api_key            ← new in Phase 6a
```

Per-field type validation:
- Integer keys (`default_port`): parse via `strconv.Atoi`; reject negative or > 65535
- Path keys (`llama_server_path`, `models_dir`): no validation at set time (just store); doctor warns on resolution failure
- Token keys (`hf_token`, `api_key`): no format validation (opaque strings)
- Enum keys (`log_level`): one of `debug`, `info`, `warn`, `error`; reject others

### 3.4 Tests

`internal/cli/config_test.go`:
- `TestConfigGetUnknownKey` — exits with `ErrUserError`
- `TestConfigSetThenGet` — round-trip on every key
- `TestConfigSetInvalidPortRejected` — negative or >65535 rejected
- `TestConfigSetInvalidLogLevelRejected`
- `TestConfigListRedactsSecrets` — `api_key` / `hf_token` print as `********`
- `TestConfigSetCreatesFileIfMissing` — first `set` creates `~/.config/llamactl/config.yaml`

`internal/config/config_test.go`:
- `TestSaveRoundTrip` — `Save` then `Load` returns equivalent struct
- `TestSaveAtomicRename` — write fails partway → no partial file visible

---

## 4. Endpoint authentication

### 4.1 UX

Setup:
```bash
llamactl config set api_key sk-abc123        # OR
export LLAMACTL_API_KEY=sk-abc123
```

Use (env var or config; env wins on conflict):
```bash
llamactl serve qwen2.5-3b-instruct --detach  # serve picks up the key automatically
curl http://localhost:8082/v1/chat/completions ...                          # → 401
curl -H "Authorization: Bearer sk-abc123" http://localhost:8082/v1/chat/...  # → 200
```

Default behavior: **opt-in**. Without a key set, llamactl serves unauthenticated (current behavior), and Tailnet remains the access boundary. Doctor flags this as a warning (✗) but does not block.

### 4.2 Wiring

- `recipes.FlagsFor` does NOT learn about auth — auth is policy, recipes are runtime config. The caller in `serve.go` appends `--api-key <token>` to the flags slice after `recipes.FlagsFor` returns, when a key is set.
- Precedence: `os.Getenv("LLAMACTL_API_KEY")` > `d.Config.APIKey` > unset.
- Embedded in the launchd plist's `ProgramArguments` when set. The plist is regenerated on every `serve --detach`, so updating the key + re-serving picks up the new value.

### 4.3 Doctor check

New: `authOnPublicBindCheck` — runs after the existing port-conflict check.

Logic:
1. Walk `~/Library/LaunchAgents/com.llamactl.*.plist` (existing infrastructure from v1.2.1 hotfix's `launchd.PortsInUse`).
2. For each plist, look for `--host` in `ProgramArguments`. If absent OR value is `0.0.0.0`, the service binds publicly.
3. If publicly-bound AND no `--api-key` arg present: flag ✗ with remediation `"set api_key via 'llamactl config set api_key <token>' or LLAMACTL_API_KEY env var"`.
4. If publicly-bound AND `--api-key` present: ✓.
5. If only loopback-bound: ✓.

A new helper `launchd.HasAPIKey(agentsDir, label string) bool` mirrors `launchd.PortFor` for plist parsing.

### 4.4 Tests

`internal/cli/serve_test.go`:
- `TestServeAppendsAPIKeyFromConfig` — Deps has `Config.APIKey="X"`, asserts plist contains `--api-key X`.
- `TestServeAppendsAPIKeyFromEnv` — `LLAMACTL_API_KEY=Y` overrides config "X".
- `TestServeNoAPIKeyWhenUnset` — plist does NOT contain `--api-key`.

`internal/cli/doctor_test.go`:
- `TestAuthCheckPublicBindNoKey` — fixture: plist with `--host 0.0.0.0` + no `--api-key` → ✗.
- `TestAuthCheckPublicBindWithKey` — fixture: plist with both → ✓.
- `TestAuthCheckLoopbackBind` — fixture: plist with `--host 127.0.0.1` → ✓ regardless of api_key.

---

## 5. The `update` command (PRD §CLI line 211)

### 5.1 UX

```
$ llamactl update
current: v1.2.1
latest:  v1.3.0
==> brew update
==> brew upgrade gregmundy/tap/llamactl
🍺  llamactl was successfully upgraded!
done.

$ llamactl update                # already on latest
already on latest (v1.3.0)

$ llamactl update                # non-brew install (e.g., go install)
llamactl is not installed via Homebrew; the binary is at /Users/greg/go/bin/llamactl.
Upgrade with your installer (e.g., `go install github.com/gregmundy/llamactl/cmd/llamactl@latest`).
```

### 5.2 Version resolution

**Current version** baked at build time via `-ldflags '-X main.version=v1.3.0'`. `.goreleaser.yml` gains:
```yaml
builds:
  - ldflags:
      - -X main.version={{.Version}}
```

Local `go build` without ldflags shows `dev`. Phase 6a Task 20 adds the ldflags wiring.

**Latest version** fetched from the cask file in `gregmundy/homebrew-tap`:
```
GET https://raw.githubusercontent.com/gregmundy/homebrew-tap/main/Casks/llamactl.rb
```
Parse the `version "X.Y.Z"` line via regex `version\s+"([^"]+)"`. Returns `"X.Y.Z"` (no `v` prefix in cask files).

**Caching** to avoid hammering GitHub: write to `~/.cache/llamactl/last-version-check.json`:
```json
{"checked_at": "2026-05-12T10:00:00Z", "latest": "1.3.0"}
```
TTL: 24 hours. `--refresh` flag on `update` bypasses cache. `doctor`'s `latestVersionCheck` reuses the cache; never refreshes (avoids one HTTP call per `doctor` invocation).

### 5.3 Brew-install detection

`os.Executable()` returns the absolute path to the running binary. Detection:
- Path starts with `/opt/homebrew/Cellar/llamactl/` → brew install (Apple Silicon)
- Path starts with `/usr/local/Cellar/llamactl/` → brew install (Intel; legacy)
- Anything else → not brew

Brew installs invoke `brew update && brew upgrade gregmundy/tap/llamactl` via `runner.CommandRunner`. Non-brew installs print the helpful message and exit 0.

### 5.4 Doctor check `latestVersionCheck`

Info-level — passes regardless. Output:
- Same version or version-check disabled: `✓ on latest version (v1.3.0)`
- Newer available: `✓ update available: v1.3.0 → v1.4.0` (still a pass; just informational)
- Network failure or cache stale + offline: `✓ version check unavailable: <err>` (soft pass)

Doctor total: 12 → 14 checks (adds `authOnPublicBindCheck` + `latestVersionCheck`).

### 5.5 Tests

`internal/cli/update_test.go`:
- `TestUpdateOnLatest` — fake fetcher returns current version; output contains "already on latest".
- `TestUpdateBrewInstalled` — fake `os.Executable()` returns brew path; fake runner records `brew upgrade` call.
- `TestUpdateNonBrewInstall` — fake executable returns `/Users/foo/go/bin/llamactl`; output contains "not installed via Homebrew".
- `TestUpdateNewerAvailable` — fake fetcher returns newer; runner is called.
- `TestUpdateRefreshBypassesCache` — `--refresh` flag forces HTTP fetch even when cache fresh.

`internal/cli/version_test.go`:
- `TestParseCaskVersion` — parses `version "1.3.0"` correctly; handles whitespace variants.
- `TestFetchLatestVersionWritesCache` — successful fetch writes cache file.
- `TestFetchLatestVersionUsesFreshCache` — cache <24h → no HTTP call.
- `TestFetchLatestVersionRefreshesStaleCache` — cache >24h → HTTP call.

---

## 6. Backlog items (13)

### 6.1 GitHub Actions Node 24 bump

**Files:** `.github/workflows/ci.yml`, `.github/workflows/release.yml`

Bump to latest versions that target Node 24:
- `actions/checkout@v4` → `@v5` (if available)
- `actions/setup-go@v5` → latest
- `goreleaser/goreleaser-action@v6` → latest

Verify by re-running CI after merge; no deprecation warnings in run logs.

### 6.2 Foreground integration test

**Files:** `internal/cli/integration_test.go`

New `TestIntegrationPhase6ForegroundServe`. Uses existing `testdata/fakellamaserver/main.go` binary as `llama-server`. Runs `serve` in foreground (no `--detach`), captures stdout, sends SIGINT after a beat, asserts the fake server exited cleanly. Closes a Phase 3 plan §10.2 gap.

### 6.3 Cobra `SilenceUsage` propagation fix

**Files:** `internal/cli/root.go`

Today's root cobra has `SilenceUsage: true` at the root level but it doesn't propagate — `RunE` errors print usage. Fix: also set `cmd.SilenceUsage = true` inside each command's RunE error path, OR (cleaner) set `cobra.Command.SilenceUsage = true` on each subcommand at construction time.

Test: any failing command (e.g., `llamactl add nonexistent-id`) does NOT print usage in stderr.

### 6.4 `fit` popularity-weighted ranking (#14)

**Files:** `internal/cli/fit.go`, `internal/cli/fit_test.go`

Within `"ok"` verdict bucket, change `fitRank` from `1000 + FreeGB` to popularity-weighted:

```go
case "ok":
    // Prefer popular repos; tiebreak on size (higher fidelity preferred).
    // hf.SearchHit has Downloads + Likes.
    return 100_000_000 + float64(r.Downloads) + r.SizeGB
```

`fitRow` gains `Downloads int` and `Likes int` fields, populated from `hf.SearchHit` at iteration time.

Test: `TestFitRanksByDownloadsWithinOK` — two ✓ rows differing only in Downloads → higher-downloads row ranks first.

### 6.5 `list` self-heal stale ParamsB / Arch (#15)

**Files:** `internal/cli/list.go`, `internal/cli/list_test.go`

When iterating models in `list`, if `metadata.ParamsB == 0` AND `metadata.GGUFPath` exists on disk: re-parse the GGUF header via `gguf.ReadHeader`. If new ParamsB > 0 or Arch is now known, write the updated metadata back via `ModelStore.Put` and use the fresh values for display.

Logging: silent on success (UX shouldn't notice). On parse error: print one-line stderr warning and use stale metadata.

Test: `TestListSelfHealsZeroParamsB` — pre-populate metadata with `ParamsB: 0`; create a real GGUF file with parameter_count or size_label; run `list`; assert (a) output shows non-zero ParamsB and (b) ModelStore was updated.

### 6.6 Doctor port-conflict false positive (#18)

**Files:** `internal/cli/doctor.go`

Live testing showed: after `llamactl stop gemma-4-e4b-it`, `doctor` reports `✗ port conflicts — gemma-4-e4b-it loaded but port 8082 is free`. The check is firing on stale state — likely reading the plist file that still exists post-stop, or comparing against a stopped launchd entry.

Investigation: read the current `portConflictsCheck` implementation. Identify whether it checks PID > 0 from `launchctl print` or just iterates plist files. Fix to skip services whose `launchctl print` returns PID 0 (per `launchd.Service.Print` semantics from Phase 3).

Test: `TestPortConflictsCheckIgnoresStoppedServices` — fake launchd returns PID 0 for a service; check does NOT flag it.

### 6.7 Sub-1B coordinated bundle (#19 + #20 + #21)

**Files:** `internal/models/metadata.go`, `internal/models/whitelist.go`, `internal/models/quants.go`, `internal/models/selector.go`, `internal/cli/add.go`, `internal/cli/list.go`, `internal/cli/fit.go`

**`ParamsB` type change int → float64** (#20):

```go
// models/metadata.go
type Metadata struct {
    // ...
    ParamsB float64 `json:"params_b"`
    // ...
}

// models/whitelist.go (PreferredIDs entries unchanged for whole-billion models; gain decimals for sub-1B)
"qwen3-0.6b":    {ID: "qwen3-0.6b", HFRepo: "Qwen/Qwen3-0.6B-GGUF", Arch: ArchQwen3, ParamsB: 0.6, MaxCtx: 32768},
"qwen3-1.7b":    {ID: "qwen3-1.7b", HFRepo: "Qwen/Qwen3-1.7B-GGUF", Arch: ArchQwen3, ParamsB: 1.7, MaxCtx: 32768},

// models/selector.go: convert at use site
row, ok := QuantSizeTable[int(math.Round(model.ParamsB))]

// cli/add.go: no truncation
paramsB := float64(header.ParamsCount) / 1e9

// cli/list.go: render
fmt.Fprintf(w, "%g B", metadata.ParamsB)  // "0.6 B", "3 B", "7.5 B"
```

JSON migration: Go's `encoding/json` deserializes integer JSON values into `float64` fields automatically. Existing metadata files with `"params_b": 3` deserialize to `3.0`. **No on-disk migration required.**

`QuantSizeTable` map keys stay `int` — adding `0.6` or `1.7` as float keys would invite NaN/precision footguns. The selector converts via `int(math.Round(...))`. For sub-1B (0.6 → rounds to 1) the lookup uses the 1B row; for 1.7B it rounds to 2 and uses the 2B row. Both rows need adding:

```go
// approximate; refine with measured HF filesizes during implementation
// (matches the existing QuantSizeTable comment's "starting estimates" pattern)
1: {Q5_K_M: 0.7, Q4_K_M: 0.6, Q4_K_S: 0.6, IQ4_XS: 0.5, IQ3_M: 0.5, IQ3_XS: 0.5, Q2_K: 0.4},
2: {Q5_K_M: 1.4, Q4_K_M: 1.2, Q4_K_S: 1.1, IQ4_XS: 1.1, IQ3_M: 1.0, IQ3_XS: 0.9, Q2_K: 0.8},
```

The rounding strategy is intentionally approximate: sub-1B models always fit on any reasonable host so selector accuracy isn't critical for them. For 1.7B → row 2, the lookup may be ±15% off from actual file sizes; acceptable for v1.3.0 since the verdict logic has 4 GB headroom built in. Refine table values when post-release smoke shows actual HF filesizes for the new PreferredIDs.

If `ArchQwen3` is a new `Arch` constant (it doesn't exist in `internal/models/arch.go` today; Qwen3 family is unrepresented in current PreferredIDs), add it to `arch.go` + add a `KVCachePerTokenKB[ArchQwen3]` row with Q8_0: 0.4 KiB/token. The 0.4 value is an estimate (Qwen3 uses GQA more aggressively than Qwen2.5's 0.5); verify against actual model behavior post-release and adjust if KV-cache estimates drift.

**Add sub-1B PreferredIDs** (#21): `qwen3-0.6b` and `qwen3-1.7b` (popular small models for retrieval/embedding/classification roles).

**`fitMinModelBytes` conditional** (#19): the current 500 MiB floor over-filters sub-1B Q4_K_M files (theoretical 600 MB for 1B at Q4). Lower the floor to 200 MiB. Imatrix shards are typically 100 MB or less, so 200 MiB still filters them. Validated by re-running `llamactl fit qwen3 0.6b` after the change — expect canonical `Qwen/Qwen3-0.6B-GGUF` Q4/Q5 results.

Tests:
- `TestModelsParamsBFloat64Migration` — old JSON `{"params_b": 3}` deserializes to `ParamsB: 3.0`.
- `TestSelectQuantSub1BModel` — qwen3-0.6b on 16 GB host picks a viable quant from QuantSizeTable[1].
- `TestListRendersSub1BParamsB` — list of qwen3-0.6b shows `0.6 B`, not `0 B`.

### 6.8 Detached-poll clock asymmetry (#22)

**Files:** `internal/cli/serve.go`

`runServeDetached`'s deadline uses `d.Now()` for "have we waited 5s yet" but the timer uses real `time.After`. A test with frozen `d.Now` would hang. Fix: pass through a `d.Sleep func(time.Duration) <-chan time.Time` (defaults to `time.After`) and use it consistently. Phase 5 Task 11 introduced the `select { ctx.Done; time.After }` pattern — extend with the seam.

Test: `TestRunServeDetachedFrozenClock` — fake `d.Now` returning a fixed value, fake sleeper, asserts deadline triggers after the expected number of sleeper calls.

### 6.9 `UserHomeDir` test-injectability (#23)

**Files:** `internal/cli/deps.go`, `internal/cli/serve.go`, `cmd/llamactl/main.go`

Today `runServeDetached` calls `os.UserHomeDir()` directly. Add `Deps.UserHomeDir func() (string, error)`; production wires `os.UserHomeDir` in `main.go`; tests override to point at `tempDir`.

Test: `TestServeWritesPlistsUnderTestHome` — fake `UserHomeDir` returns a tempDir; serve writes its plist there; assert path.

### 6.10 HF cache namespace-bump GC (#24)

**Files:** `internal/hf/cache.go`, `internal/cli/cache.go`

`Cache.PruneOlderThan` removes files but leaves empty namespace dirs (e.g., `hf-repo/` after a bump to `hf-repo-v2`). Add `Cache.GCEmptyNamespaces()` that removes empty subdirs of `Cache.root`. Call from `cache prune` (both with and without `--all`).

Test: `TestCacheGCEmptyNamespaces` — populate `hf-old/`, prune to empty, GC removes the dir.

### 6.11 Sentinel errors for download in-progress (#25)

**Files:** `internal/download/download.go`, `internal/cli/add.go`, `internal/cli/add_test.go`

Today `add.go` test errors string-match `"in progress"`. Export sentinel:
```go
// internal/download/download.go
var ErrInProgress = errors.New("download in progress")
```

Wherever `Downloader.Get` returns the in-progress error, wrap with this sentinel. Callers use `errors.Is(err, download.ErrInProgress)`.

Tests updated to use the sentinel.

---

## 7. Sequence

Tasks land in this order on `phase6a-completions`:

1. CI bump (#12) — XS, safe
2. Cobra SilenceUsage (#17) — XS
3. Foreground integration test (#13) — S
4. Detached-poll clock symmetry (#22) — XS
5. `UserHomeDir` injection (#23) — XS
6. Download in-progress sentinel (#25) — S
7. HF cache namespace GC (#24) — XS
8. Doctor port-conflict false positive fix (#18) — S
9. `ParamsB` float64 migration — type change rippled across models package — M
10. Add `qwen3-0.6b` + `qwen3-1.7b` PreferredIDs + ArchQwen3 + QuantSizeTable[1] row (#21) — S
11. `fitMinModelBytes` lowered to 200 MiB (#19) — XS
12. `list` self-heal stale metadata (#15) — S
13. `fit` popularity-weighted ranking (#14) — S
14. `config.Save` + APIKey field (foundation) — S
15. `Deps.Config` wired in main.go — XS
16. `llamactl config get/set/list` cobra command — M
17. `serve` reads APIKey, appends `--api-key` — S
18. `launchd.HasAPIKey` plist scanner — S
19. Doctor `authOnPublicBindCheck` — S
20. Build-time `-ldflags '-X main.version={{.Version}}'` — XS
21. `version.go`: `fetchLatestVersion` + 24h cache + semver compare — S
22. `llamactl update` cobra command — M
23. Doctor `latestVersionCheck` — S
24. README + PRD doc updates — S
25. Merge → tag `v1.3.0` → release pipeline + brew upgrade verify + live smoke
26. Update `project_state.md` memory

Each task = one commit on the feature branch.

---

## 8. Acceptance criteria

Phase 6a ships when:

- ✅ All 24 implementation tasks complete; each one commit on `phase6a-completions`.
- ✅ `go test ./... -race` clean; `go vet ./...` clean; `gofmt -l .` clean.
- ✅ Live: `llamactl config set api_key sk-test`, then `llamactl config get api_key` → `sk-test`. `llamactl config list` shows all 6 keys.
- ✅ Live: `llamactl serve qwen2.5-3b-instruct --detach` with `api_key` set → plist contains `--api-key sk-test`. `curl http://localhost:8082/v1/chat/completions ...` → 401. `curl -H "Authorization: Bearer sk-test" ...` → success.
- ✅ Live: `llamactl doctor` reports **14 checks** (added: auth-on-public-bind, latest-version-available).
- ✅ Live: with `api_key` unset and a 0.0.0.0-bound service running, doctor flags auth-on-public-bind as ✗ with remediation hint.
- ✅ Live: `llamactl update` on the brew-installed binary → version transition printed + brew upgrade ran; on a `go install` binary → helpful message, exit 0.
- ✅ Live: `llamactl list` against `gemma-4-e4b-it` (which currently shows `?` because the metadata was cached pre-Phase-5 with `ParamsB: 0`) now shows `7.5 B` after self-heal kicks in.
- ✅ Live: `llamactl fit qwen 2.5 3b` returns canonical `Qwen/Qwen2.5-3B-Instruct-GGUF` in the top 3 (popularity-weighted).
- ✅ Live: `llamactl add qwen3-0.6b`, then `llamactl list` → shows `0.6 B` (not `0` or blank).
- ✅ Live: after `llamactl stop <id>`, `llamactl doctor` no longer reports the port-conflict false positive.
- ✅ GitHub Actions CI + release workflows complete green; no Node 20 deprecation warnings in run logs.
- ✅ `brew upgrade llamactl` to v1.3.0 in <30s.
- ✅ `project_state.md` memory updated; `MEMORY.md` index pointer bumped.

---

## 9. Non-goals / risks

- **No breaking API changes.** Config struct gains a field (additive). Deps gains two fields (additive). No interface signature changes.
- **No new external dependencies.** Everything is stdlib or existing imports.
- **Auth is opt-in.** Existing serves without `api_key` continue to bind 0.0.0.0 unauthenticated. Doctor warns but never blocks.
- **`update` requires network** for the version check. Graceful soft-pass when offline (cache stale + no network → doctor shows "version check unavailable").
- **Version check uses GitHub raw URL** — depends on `gregmundy/homebrew-tap/main/Casks/llamactl.rb` being readable. Public repo, no auth needed.
- **JSON metadata float64 migration risk:** old metadata files with `"params_b": 3` deserialize to `3.0` automatically. Verified by `TestModelsParamsBFloat64Migration`. No bake-time scripts needed.
- **`QuantSizeTable[1]` row for sub-1B is approximated** from llama.cpp file-size docs since we don't ship a 1B model in PreferredIDs today. Refine when adding more sub-1B models in future phases.
- **Auth via `--api-key` requires modern llama-server.** Old builds without the flag will reject it. Doctor's `llama-server version meets floor` check from Phase 3 doesn't gate on this specifically; if a user has an old build, `serve` will fail with a clear llama-server error. Acceptable for v1.3.0 — the floor moves over time anyway.
- **Doctor's latestVersionCheck cache** lives at `~/.cache/llamactl/last-version-check.json`. The existing `cache prune --all` from Phase 5 would wipe it; not a problem (it'd just trigger one fresh HTTP fetch next time).

---

## 10. References

- PRD §CLI lines 211, 214 (the two outstanding line items)
- PRD §Non-goals line 55 (authentication, re-elevated)
- Phase 5 spec §9 acceptance criteria (live-smoke pattern reused here)
- v1.2.1 hotfix (`launchd.PortsInUse` infrastructure reused by `launchd.HasAPIKey`)
- Phase 5 project_state.md memory entry (deferred concerns list)
