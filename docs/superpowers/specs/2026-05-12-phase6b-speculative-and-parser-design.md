# llamactl Phase 6b: Speculative decoding + GGUF tensor-shape inference — Design Spec

**Status:** Approved 2026-05-12
**Ships as:** `v1.4.0`
**Branch:** `phase6b-speculative-and-parser`
**Covers:** speculative decoding auto-config via explicit `--draft` flag + `fit --speculative` discovery, and a scope-limited GGUF tensor-shape fallback for ParamsB inference.
**Supersedes:** `2026-05-12-phase6b-performance-parser-design.md` (draft sketch; hot swap split off to Phase 6c).

---

## 1. Goal

Phase 6a (v1.3.0) shipped 30 commits: `update`, `config`, opt-in endpoint auth, ParamsB float64 migration, 14 doctor checks, plus 13 backlog drains. Two items from the original Phase 6 backlog remain natural fits for v1.4.0:

1. **Speculative decoding auto-config** — llama-server supports `--model-draft` for draft-model assisted decoding (2-3× speedup on compatible pairs); llamactl exposes nothing to drive it today.
2. **GGUF tensor-shape inference for ParamsB** — Phase 6a's self-heal-on-`list` fills in ParamsB when either `general.parameter_count` or `general.size_label` is in the GGUF header. A small population of older fine-tunes carry neither, and display `?` in `list`.

A third candidate from the Phase 6 backlog — **hot model swap** — was deferred from this phase. Hot swap's two viable mechanisms (HTTP proxy vs. graceful handoff) both fork the operational model: a proxy means llamactl itself becomes a long-running daemon. That decision should be made jointly with the Phase 6c web UI brainstorming, since both features share the proxy substrate. Hot swap will be specced in Phase 6c.

Ships as `v1.4.0` — additive features, no breaking API changes.

Out of scope: hot model swap (Phase 6c), web UI (Phase 6c), multi-draft cascading speculation, tokenizer-equality validation, auto-install of draft models on serve.

---

## 2. Architecture overview

All work on a single branch (`phase6b-speculative-and-parser`). No new packages. Two pieces of new code, both extending existing layers:

```
internal/gguf/header.go        + ReadHeaderWithTensors(path) (Header, error)
                                 — extended parser that walks the tensor info
                                   block when both ParamsCount and SizeLabel
                                   fallbacks are absent. Only looks for
                                   token_embd.weight; returns early on hit.
                               + paramsBFromTokenEmbd[arch] formula table
                                 (qwen2, qwen3, llama, gemma3, mistral)

internal/models/speculative.go NEW: SpeculativePair(main, draft, hw, recipe)
                               PairVerdict {Ok, Reason, CombinedRAMGB,
                                            SizeRatio, ArchMatch}
                               — pure function; shared by serve.go + fit.go

internal/cli/serve.go          + --draft <id> flag
                               + DraftID field on serveOpts
                               + post-recipes append of --model-draft <path>
                               + SpeculativePair validation at serve time

internal/cli/fit.go            + --speculative flag (changes positional arg
                                 semantics: arg = MAIN model query)
                               + draft-candidate enumeration from ModelStore
                               + per-candidate SpeculativePair invocation
                               + adapted tabwriter columns

internal/launchd/plist.go      + DraftPath, DraftCtxSize fields on PlistSpec
                               + template renders --model-draft / --ctx-size-draft
                                 when DraftPath != ""
```

**No interface changes.** `Deps` gains zero fields. All work fits existing seams (`Deps.ModelStore`, `Deps.HFClient`, hardware info from `ensureHardware`, `runner.CommandRunner` for any new shell-outs — there are none in 6b). The `cli.PortAllocator` interface from v1.2.1 is unchanged. Same GoReleaser → cask → `brew upgrade` distribution path.

---

## 3. Speculative decoding

### 3.1 UX

Setup (assumes both models already installed via `add`):

```bash
$ llamactl fit --speculative qwen2.5-32b-instruct
Draft candidates for qwen2.5-32b-instruct (32.0 B, qwen2):

DRAFT ID                  ARCH    PARAMSB  RATIO   COMBINED RAM   VERDICT
qwen2.5-3b-instruct       qwen2   3.0 B    10.7×   24.1 GB        ✓ ok
qwen2.5-0.5b-instruct     qwen2   0.5 B    64.0×   22.3 GB        ⚠ ratio>15× (overhead may eat speedup)

Note: speculative decoding speedup depends on workload; ratio is a heuristic only.

$ llamactl serve qwen2.5-32b-instruct --draft qwen2.5-3b-instruct
[2026-05-12T14:00:00] speculative decoding enabled (draft=qwen2.5-3b-instruct, ratio=10.7×)
[2026-05-12T14:00:01] llama-server starting on port 8080 with --model-draft /Users/greg/.local/share/llama-models/qwen2.5-3b-instruct/Q4_K_M.gguf
...

$ llamactl serve qwen2.5-32b-instruct --draft llama-3-1b-instruct  # arch mismatch
llamactl: draft model llama-3-1b-instruct (arch=llama) is incompatible with main qwen2.5-32b-instruct (arch=qwen2); arch must match

$ llamactl serve qwen2.5-32b-instruct --draft qwen2.5-3b-instruct  # combined RAM exceeds budget
llamactl: combined weights + KV cache (28.5 GB) exceeds usable RAM (24.0 GB); free 4.5 GB or pick a smaller draft
```

Default behavior: **opt-in via `--draft` flag**. No auto-detection. Without the flag, `serve` behavior is unchanged from v1.3.0.

### 3.2 The `--draft` flag on `serve`

**New cobra flag** in `internal/cli/serve.go`:
```go
serveCmd.Flags().String("draft", "", "draft model id for speculative decoding (must be installed)")
```

Bound to a new `serveOpts.DraftID string` field.

**Flow** (additions to `runServeForeground` and `runServeDetached`, both of which already share `serveOpts`):

1. After `d.ModelStore.Get(opts.ID)` returns the main model, if `opts.DraftID != ""`:
   - `draft, err := d.ModelStore.Get(opts.DraftID)` — if `err` is "not found", return `ErrUserError`: `"draft model <id> is not installed; run 'llamactl add <id>' first"`.
   - Resolve the draft's GGUF path the same way main does (`models.Model.GGUFPath` or recompute from quant).
   - Call `models.SpeculativePair(main, draft, hw, opts.Recipe)`.
   - If `!verdict.Ok`: return `ErrUserError` with `verdict.Reason`.
   - If `verdict.SizeRatio < 5 || verdict.SizeRatio > 15`: print warning to stderr (`fmt.Fprintf(d.Stderr, "warning: ratio %.1fx outside recommended 5-15x range; speedup may be marginal\n", verdict.SizeRatio)`) but continue.
2. After `recipes.FlagsFor(...)` returns the flag slice, if `DraftID != ""`:
   - Append `--model-draft <draft.GGUFPath>` to the slice.
   - Append `--ctx-size-draft <draft_ctx>` where `draft_ctx = min(main_ctx, draft.MaxCtx)` (draft can't exceed its own training context).
3. Log line: `"speculative decoding enabled (draft=<id>, ratio=<X.X>×)"` to the log file before launching llama-server.

**Recipe interaction:** `recipes.FlagsFor` stays pure-function over the main model. The `--model-draft` append happens post-recipe in `serve.go`. This keeps `internal/recipes` from learning about draft pairing and avoids a Cartesian recipe × draft explosion.

### 3.3 The `fit --speculative` mode

**New cobra flag** in `internal/cli/fit.go`:
```go
fitCmd.Flags().Bool("speculative", false, "list installed draft candidates for the named main model")
```

**Behavior when `--speculative` is set:**

- Positional arg interpretation changes: arg = MAIN model id or HF repo path. The arg is resolved against `ModelStore` (must be installed; not-found is `ErrUserError`).
- HF search is **skipped** (drafts must already be installed).
- Iterate `ModelStore.List()`; for each candidate that isn't the main model itself, call `models.SpeculativePair(main, candidate, hw, "chat")` (chat recipe as the default for combined-RAM math; users can re-run with `--recipe` once that flag exists — out of scope for 6b).
- Drop candidates with `!ArchMatch` (these are noise, not warnings).
- Sort remaining: `Ok` rows first, sorted by absolute distance from the ideal midpoint `7.5` (i.e., `|SizeRatio - 7.5|` ascending) so ratios closest to the sweet spot rise to the top; `!Ok` rows last for transparency, sorted by `Reason`.
- Apply `--limit` (defaults to 20, same as `fit`'s existing default).
- Output: tabwriter with columns `DRAFT ID | ARCH | PARAMSB | RATIO | COMBINED RAM | VERDICT`. Footer line about heuristic caveat.
- Empty result (no installed drafts pass `ArchMatch`): print `"no installed draft candidates for <main>; run 'llamactl fit <arch>' to find smaller variants of the same family"` and exit 0.

No `--install` semantics (drafts are installed by construction). No `--json` mode in 6b (additive opportunity for 6c if needed; not blocking).

### 3.4 Eligibility logic (`SpeculativePair`)

**New file** `internal/models/speculative.go`:

```go
type PairVerdict struct {
    Ok            bool
    Reason        string  // non-empty when !Ok, or when warning (e.g., size ratio)
    CombinedRAMGB float64 // weights + KV cache for both models
    SizeRatio     float64 // main.ParamsB / draft.ParamsB
    ArchMatch     bool    // main.Arch == draft.Arch
}

// SpeculativePair returns the verdict for using draft as the speculative-decoding
// draft for main, on hw running under the given recipe.
//
// Refusal conditions (Ok=false):
//   - draft.Arch != main.Arch (ArchMatch=false; tokenizer compatibility cannot be assumed)
//   - main.ParamsB <= 0 or draft.ParamsB <= 0 (size unknown; cannot compute ratio or budget)
//   - SizeRatio < 2 (draft is larger or comparable to main; no speedup possible)
//   - CombinedRAMGB > hw.UsableGB - 4.0 (too-big; same 4 GB headroom as fit)
//
// Warning conditions (Ok=true, Reason non-empty):
//   - SizeRatio < 5 (overhead may exceed speedup)
//   - SizeRatio > 15 (draft too small; alignment likely poor)
//
// CombinedRAMGB = main weights + main KV cache + draft weights + draft KV cache,
// using KVCachePerTokenKB[arch][quant] with each model's recipe ctx-size.
func SpeculativePair(main, draft Model, hw hardware.Info, recipe string) PairVerdict
```

Pure function. No I/O. Lives in `internal/models` next to `selector.go` and `quants.go`.

**Combined-RAM math:**
- Main weights: from `QuantSizeTable[int(math.Round(main.ParamsB))][main.Quant]` (existing path).
- Main KV cache: `KVCachePerTokenKB[main.Arch][Q8_0] * mainCtx / 1024 / 1024` GB.
- Draft weights: same lookup for draft.
- Draft KV cache: same KV-per-token table; draft ctx = `min(mainCtx, draft.MaxCtx)`.
- For unknown arch in `KVCachePerTokenKB`: estimate 0.5 KiB/token (existing fallback in `KVCacheGB`).

**The 4 GB headroom** matches `fit`'s existing verdict threshold. `tight` verdict from `fit` becomes `ok` with a warning Reason here (the user has already committed to running this pair; the warning is informational).

### 3.5 Plist embedding

`internal/launchd/plist.go` gains two fields on `PlistSpec`:

```go
type PlistSpec struct {
    // ... existing fields ...
    DraftPath    string // empty when no draft; renders as --model-draft <path>
    DraftCtxSize int    // empty (zero) when no draft; renders as --ctx-size-draft <N>
}
```

The template adds:
```xml
{{- if .DraftPath}}
<string>--model-draft</string>
<string>{{xml .DraftPath}}</string>
<string>--ctx-size-draft</string>
<string>{{.DraftCtxSize}}</string>
{{- end}}
```

`serve.go`'s detached path populates these fields from `opts.DraftID` resolution before calling `launchd.Render`.

A new helper `launchd.HasDraft(agentsDir, label string) (string, bool)` returns the draft path embedded in a plist, mirroring `HasAPIKey` from v1.3.0. Used in tests; no production caller in 6b (could feed a future doctor check on draft availability — out of scope here).

### 3.6 Tests

`internal/models/speculative_test.go`:
- `TestSpeculativePairArchMismatch` — qwen vs llama → `Ok=false`, ArchMatch=false.
- `TestSpeculativePairRatioTooSmall` — 7B main + 7B draft → `Ok=false`, Reason mentions ratio.
- `TestSpeculativePairRatioOk` — 32B main + 3B draft → `Ok=true`, no warning.
- `TestSpeculativePairRatioWarning` — 32B main + 0.5B draft → `Ok=true`, Reason notes ratio>15×.
- `TestSpeculativePairCombinedRAMTooBig` — 70B main + 7B draft on 32GB host → `Ok=false`, Reason quotes shortfall.
- `TestSpeculativePairZeroParamsB` — main with ParamsB=0 → `Ok=false`, Reason mentions unknown size.
- `TestSpeculativePairUnknownArch` — both models with arch="exotic" → ArchMatch=true (they match each other), no KV-cache table entry → uses 0.5 KiB/token fallback.

`internal/cli/serve_test.go`:
- `TestServeWithDraftAppendsModelDraftFlag` — fake ModelStore with installed main+draft; assert argv contains `--model-draft <draft.gguf>` after recipe flags.
- `TestServeDraftNotInstalled` — `--draft missing-id` → `ErrUserError`, message names the missing id.
- `TestServeDraftArchMismatch` — `--draft` with non-matching arch → `ErrUserError`.
- `TestServeDraftCombinedRAMTooBig` — fake hw with low UsableGB → `ErrUserError`.
- `TestServeDraftWarnsOnRatioOutsideRange` — `--draft` with ratio 20× → warning to stderr; serve continues; argv contains `--model-draft`.
- `TestServeDetachedDraftEmbedsInPlist` — `--detach --draft` writes plist containing `--model-draft <path>` + `--ctx-size-draft <N>`.

`internal/cli/fit_test.go`:
- `TestFitSpeculativeListsInstalledDrafts` — fake ModelStore with one main + three candidates (one arch-mismatch, two arch-match different sizes) → output contains the two arch-matches sorted by ratio, omits the mismatch.
- `TestFitSpeculativeMainNotInstalled` — main id not in ModelStore → `ErrUserError`.
- `TestFitSpeculativeEmptyCandidates` — only the main is installed → output is the "no installed draft candidates" message; exit 0.
- `TestFitSpeculativeRatioOrder` — candidates with ratios 8×, 12×, 4× → output orders 8× first (closest to ideal 5-10× midpoint), then 4×, then 12×.

`internal/launchd/plist_test.go`:
- `TestRenderPlistWithDraft` — PlistSpec with DraftPath + DraftCtxSize → rendered XML contains `--model-draft` + `--ctx-size-draft`.
- `TestRenderPlistNoDraft` — empty DraftPath → XML has neither.
- `TestHasDraftFindsEmbeddedPath` — fixture plist with draft → `HasDraft` returns the path + true.
- `TestHasDraftAbsent` — fixture plist without draft → returns ("", false).

---

## 4. GGUF tensor-shape inference

### 4.1 Problem

`internal/gguf/header.go` parses the kv-block of GGUF v3 files and reads `general.parameter_count` (Phase 2.5) plus `general.size_label` fallback (Phase 5). For files that carry **neither** (a real population of older fine-tunes from specific HF uploaders), `ParamsCount` returns 0 and `list` shows `?`.

Phase 6a's self-heal-on-`list` re-parses every invocation, so improving the parser ships retroactively to the existing model library on next list. Phase 6b adds a third fallback: walk into the tensor info block, find `token_embd.weight`, compute paramsB from its dimensions plus the arch's per-block parameter formula.

This matters in 6b specifically because the new `SpeculativePair` eligibility check depends on `ParamsB > 0` for both models. A user with an obscure fine-tune as their main model would get a silent eligibility failure on `fit --speculative` today. Closing the parser gap closes that quiet UX hole.

### 4.2 Parser extension

`internal/gguf/header.go`:

```go
// ReadHeader returns the existing header parse. Unchanged signature.
func ReadHeader(path string) (Header, error)

// ReadHeaderWithTensors is the same parse, plus a tensor-info walk when
// neither parameter_count nor size_label produced a non-zero ParamsCount.
// Walks tensor descriptors looking for "token_embd.weight"; on hit, computes
// paramsB via paramsBFromTokenEmbd[arch] and sets ParamsCount. Returns early
// after finding the tensor or after exhausting the readLimit.
//
// Callers that already have ParamsCount > 0 from ReadHeader can skip this;
// it's an optional, cost-aware fallback path.
func ReadHeaderWithTensors(path string) (Header, error)
```

`list.go`'s self-heal path (Phase 6a #15) switches from `ReadHeader` to `ReadHeaderWithTensors` so the self-heal can recover values for the older-fine-tune population. `cmd/gguf-inspect` uses `ReadHeaderWithTensors` as well and notes `(via tensor-shape fallback)` in its output when the kv-block path produced 0.

**Tensor info block layout** (GGUF v3 spec):
1. After the kv-block, the file holds `header.tensor_count` tensor descriptors, then aligned tensor data.
2. Each descriptor is: `name_len (u64), name (bytes), n_dims (u32), dims[n_dims] (u64 each), type (u32), offset (u64)`.
3. We only care about the descriptor whose `name == "token_embd.weight"`. Its dims are `[hidden_dim, vocab_size]` (or `[vocab_size, hidden_dim]` depending on storage order — verify against real fixtures).

**readLimit:** stays 64 MiB unless a real-world fixture exceeds it. The kv-block is typically <1 MiB; the tensor info block scales with `tensor_count`. A 70B model has on the order of 1000 tensors × ~50 bytes each = ~50 KiB of descriptors. 64 MiB is comfortably above any realistic descriptor block size.

### 4.3 Per-arch formula constants

`internal/gguf/params_arch.go` (new):

```go
// paramsBFromTokenEmbd estimates total parameter count in billions given:
//   hiddenDim:    second dimension of token_embd.weight
//   vocabSize:    first dimension of token_embd.weight (size of token vocabulary)
//   blockCount:   value of <arch>.block_count from the kv-block
//
// Returns 0 for unknown arches; callers leave ParamsCount=0 in that case.
//
// Formulas derived from llama.cpp model loader source + Hugging Face model card
// arithmetic. Accurate to ~10-15% for the supported family lineages.
var paramsBFromTokenEmbd = map[string]func(hiddenDim, vocabSize, blockCount int) float64{
    "qwen2":   qwen2Params,
    "qwen3":   qwen3Params,
    "llama":   llamaParams,
    "gemma3":  gemma3Params,
    "mistral": llamaParams, // Mistral 7B/8x7B share llama layer structure
}

func llamaParams(hidden, vocab, blocks int) float64 {
    // Embedding: vocab * hidden
    // Per-block attention+MLP (GQA): ~12 * hidden^2 (rough; assumes ffn_dim≈4*hidden)
    // Output head: vocab * hidden (tied or untied; we count untied; ~10% overestimate when tied)
    embedding := float64(vocab) * float64(hidden)
    perBlock := 12 * float64(hidden) * float64(hidden)
    output := float64(vocab) * float64(hidden)
    total := embedding + perBlock*float64(blocks) + output
    return total / 1e9
}

// qwen2Params, qwen3Params, gemma3Params: similar shape, family-specific
// multipliers reflecting GQA group counts and MoE active-param accounting.
// Concrete coefficients land during implementation by fitting against
// known-paramsB fixtures (Qwen2.5-7B, Qwen3-1.7B, Llama-3-8B, Gemma-3-4B).
```

Unknown arch (e.g., `falcon`, `gpt-j`, `phi`) → `paramsBFromTokenEmbd[arch]` returns nil → `ReadHeaderWithTensors` leaves `ParamsCount=0` → existing `?` display preserved.

### 4.4 Fall-through behavior

Any error during the tensor-info walk (truncated read, unexpected dim count, name not found within readLimit) → return the kv-block-derived header with `ParamsCount=0`. **Never abort.** The fallback is best-effort; the worst case matches today's behavior.

`cmd/gguf-inspect` reports parse errors verbosely (existing behavior); `list`'s self-heal silently uses the zero value and renders `?`.

### 4.5 Tests

`internal/gguf/header_test.go` additions:
- `TestReadHeaderWithTensorsFindsTokenEmbed` — fixture file (built via `internal/gguftest.Build` extended to write a token_embd.weight descriptor) without parameter_count or size_label, arch=qwen2 → ParamsCount within 15% of expected.
- `TestReadHeaderWithTensorsUnknownArch` — same fixture, arch=falcon → ParamsCount stays 0; no error.
- `TestReadHeaderWithTensorsTruncated` — fixture truncated mid-descriptor → no error, ParamsCount=0.
- `TestReadHeaderWithTensorsExceedsReadLimit` — synthetic header with bogus `tensor_count = 1_000_000` causing walk to bail past readLimit → no error, ParamsCount=0.
- Per-arch fixture round-trip: one test per supported arch (qwen2, qwen3, llama, gemma3, mistral) asserting recovery within 15% of the model's actual paramsB. Fixtures built from real GGUF headers downloaded from the corresponding HF repos and trimmed to header + tensor descriptors (no tensor data).

`internal/gguftest/build.go` extension:
- Add `Tensor` struct + `WithTensor(name, dims, dtype)` option to `Build()` so test fixtures can include token_embd.weight without writing tensor data.

`internal/cli/list_test.go`:
- `TestListSelfHealsViaTensorShape` — pre-populate metadata with ParamsB=0; create a GGUF fixture missing both kv fallbacks but with token_embd.weight; run list; assert ParamsB updates.

`cmd/gguf-inspect`:
- Add a `(via tensor-shape fallback)` annotation when ParamsCount was filled from the tensor walk, not the kv-block. No standalone test (smoke-checked during live verification).

---

## 5. Sequence

Tasks land in this order on `phase6b-speculative-and-parser`:

1. `internal/gguftest` extension: `WithTensor` option + tensor descriptor writer — XS
2. `internal/gguf`: `ReadHeaderWithTensors` skeleton (no formula yet) with truncation/limit safety — S
3. `internal/gguf`: per-arch formula table + `llamaParams` formula + tests for arch=llama — S
4. `internal/gguf`: `qwen2Params` / `qwen3Params` / `gemma3Params` formulas + fixture tests — M
5. `internal/gguf`: register `mistral` → `llamaParams` alias + test — XS
6. `cmd/gguf-inspect`: switch to `ReadHeaderWithTensors`, add fallback annotation — XS
7. `internal/cli/list.go`: self-heal path switches to `ReadHeaderWithTensors` + test — S
8. `internal/models/speculative.go`: `SpeculativePair` + `PairVerdict` + all pair tests — M
9. `internal/launchd/plist.go`: `DraftPath` + `DraftCtxSize` fields + template branch + render tests — S
10. `internal/launchd/plist.go`: `HasDraft` helper + test — XS
11. `internal/cli/serve.go`: `--draft` flag, opts wiring, ModelStore.Get(draft), SpeculativePair invocation — M
12. `internal/cli/serve.go`: post-recipe append of `--model-draft` + `--ctx-size-draft`, log line — S
13. `internal/cli/serve.go`: detached path populates PlistSpec draft fields — S
14. `internal/cli/serve_test.go`: all six new tests — M
15. `internal/cli/fit.go`: `--speculative` flag + positional-arg branch — S
16. `internal/cli/fit.go`: candidate enumeration from ModelStore.List + SpeculativePair loop — S
17. `internal/cli/fit.go`: sort + tabwriter columns + footer caveat — S
18. `internal/cli/fit_test.go`: all four new tests — S
19. README + PRD doc updates (new `--draft` flag on serve; `fit --speculative` mode; parser fallback) — S
20. Merge → tag `v1.4.0` → release pipeline + brew upgrade verify + live smoke — M
21. Update `project_state.md` memory — XS

Each task = one commit on the feature branch.

---

## 6. Acceptance criteria

Phase 6b ships when:

- ✅ All 21 implementation tasks complete; each one commit on `phase6b-speculative-and-parser`.
- ✅ `go test ./... -race` clean; `go vet ./...` clean; `gofmt -l .` clean.
- ✅ Live: `llamactl add qwen2.5-7b-instruct && llamactl add qwen2.5-0.5b-instruct`, then `llamactl serve qwen2.5-7b-instruct --draft qwen2.5-0.5b-instruct` → foreground log contains `"speculative decoding enabled (draft=qwen2.5-0.5b-instruct, ratio=14.0×)"` and llama-server starts with `--model-draft` in argv.
- ✅ Live: `curl -d '{"model":"qwen2.5-7b-instruct","messages":[...]}' http://localhost:8080/v1/chat/completions` returns generated tokens; tok/s recorded in log.
- ✅ Live: `llamactl serve qwen2.5-7b-instruct --draft llama-3-1b-instruct` (after installing both) → `ErrUserError` exit 2, stderr names arch mismatch.
- ✅ Live: `llamactl serve qwen2.5-7b-instruct --draft nonexistent-id` → `ErrUserError` exit 2, stderr suggests `llamactl add`.
- ✅ Live: `llamactl fit --speculative qwen2.5-7b-instruct` → tabwriter listing installed qwen2-arch candidates with verdict column; non-qwen2 installs omitted.
- ✅ Live: `llamactl serve qwen2.5-7b-instruct --draft qwen2.5-0.5b-instruct --detach` → `~/Library/LaunchAgents/com.llamactl.qwen2.5-7b-instruct.plist` contains both `--model-draft` and `--ctx-size-draft` entries.
- ✅ Live: an installed model from the older-fine-tune population (lacking both `parameter_count` and `size_label`) shows real ParamsB in `list` after one invocation (self-heal via tensor-shape fallback). Identify a candidate during implementation; fall back to creating a synthetic fixture only if no real instance is found in the user's library.
- ✅ Live: `cmd/gguf-inspect <fixture.gguf>` reports `ParamsCount: 7500000000 (via tensor-shape fallback)` for files that hit the new path.
- ✅ Live: existing 14 doctor checks all pass after upgrade (no regression).
- ✅ Live: `brew upgrade llamactl` to v1.4.0 in <10s.
- ✅ `project_state.md` memory updated; `MEMORY.md` index pointer updated to reference v1.4.0.

---

## 7. Non-goals / risks

**Non-goals:**

- **Hot model swap** — deferred to Phase 6c. The proxy-vs-handoff architectural choice rides with the web UI brainstorming because both features share the long-running-daemon substrate.
- **Implicit draft auto-detection.** `--draft` is always explicit; `fit --speculative` is a discovery surface, not an enabler. No "serve <main> auto-picks a draft from cache" behavior.
- **Tokenizer-equality validation.** Arch match is necessary but not sufficient; the parser does not inspect `tokenizer.ggml.tokens` arrays (those are deliberately skipped today via the readLimit + skip-arrays logic). User trusts the arch match; llama-server is ground truth and will reject incompatible pairs at startup with a clear error.
- **Auto-install of draft on serve.** If the draft id isn't in ModelStore, `serve` exits with an error pointing the user at `llamactl add`. No implicit network operations.
- **Multi-draft / cascading speculation.** llama-server supports only single-draft today; llamactl matches that surface.
- **Full tensor-info parse.** The parser walks the tensor info block looking only for `token_embd.weight`; it does not read tensor data, does not sum tensor byte sizes, does not produce a full tensor manifest.
- **Arches beyond qwen2/qwen3/llama/gemma3/mistral in the tensor fallback.** Other arches fall through to `ParamsCount=0` (today's `?` behavior). Adding arches is a single-formula PR in a future phase.
- **`fit --speculative --json` output mode.** Tabwriter only in 6b; JSON is additive in a future phase if needed.
- **Doctor check for draft availability.** A check like "main-with-draft service references a draft model that's no longer installed" is plausible but out of scope; `launchd.HasDraft` is shipped as the foundation for that future check.

**Risks:**

- **Tensor-info block layout edge cases.** GGUF v3 is stable but real-world files include vendor extensions; a fixture from one repo may parse while a sibling file from a different uploader does not. *Mitigation:* fixture set covers one file per supported arch from at least two different uploaders; fall-through to `ParamsCount=0` on any parse error never aborts the parse.
- **`paramsBFromTokenEmbd` coefficient drift.** The per-arch formulas approximate paramsB to ~10-15%. As llama.cpp evolves (e.g., new arch revisions changing layer structure), coefficients may drift further. *Mitigation:* per-arch fixture tests pin expected paramsB ± 15%; if a formula drifts past tolerance, the test fails and the coefficient is re-fit before release.
- **readLimit bump risk.** If a real fixture's tensor info block exceeds 64 MiB (improbable but possible for very large models), the walk truncates. *Mitigation:* measure fixture sizes during implementation; bump readLimit to 128 MiB only if necessary. The walk's truncation handling never errors — worst case is ParamsCount=0 for an oversized file.
- **Speculative decoding wall-clock regression.** Some draft pairings slow generation rather than speed it up (small batch size, mismatched temperature, draft alignment poor). *Mitigation:* `--draft` produces a warning when SizeRatio is outside the recommended range; output footer in `fit --speculative` notes the heuristic caveat; documentation in README's new "Speculative decoding" section sets expectations.
- **Plist regeneration on `serve --detach` overwrites old draft pairing.** If a user re-runs `serve <main> --detach` without `--draft`, the new plist omits the draft fields. This is correct behavior (re-serving without `--draft` means the user disabled it) but worth flagging in docs. *Mitigation:* README note in the speculative decoding section.
- **`ModelStore.List()` performance in `fit --speculative`.** With hundreds of installed models, the candidate loop runs `SpeculativePair` per candidate. `SpeculativePair` is pure and cheap (no I/O); even 1000 candidates is well under 1 ms. No risk in practice; flagging for completeness.
- **JSON-tagged float64 in PairVerdict** is not serialized in 6b (no `--json` mode). When `fit --speculative --json` ships in a future phase, NaN/Inf handling will need attention. Out of scope for now.
- **No `--api-key` interaction with `--model-draft`.** The draft model is loaded by the same llama-server process; the existing `--api-key` from v1.3.0 protects all endpoints uniformly. No additional auth surface to design.

---

## 8. References

- Phase 6a spec (`2026-05-12-phase6a-cli-completions-design.md`) — architecture template, acceptance criteria pattern, ParamsB float64 migration (line 312-356), launchd.HasAPIKey pattern (line 147) mirrored by HasDraft.
- Phase 6b draft sketch (`2026-05-12-phase6b-performance-parser-design.md`) — superseded by this document. The hot-swap items deferred to Phase 6c.
- PRD §Out of scope, post-v1 candidates (lines 398-408) — speculative decoding listed.
- llama.cpp documentation for `--model-draft`, `--ctx-size-draft` flags.
- GGUF v3 specification — tensor info block layout (descriptor format used in §4.2).
- Phase 5 spec §9 acceptance criteria — live-smoke pattern reused here.
- Phase 6a project_state.md memory entry — deferred concerns list (line 129 specifically calls out the tensor-shape fallback opportunity).
