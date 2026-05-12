# Phase 6b: Speculative decoding + GGUF tensor-shape fallback — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add explicit `--draft` flag to `serve` plus `fit --speculative` discovery surface, and extend the GGUF parser with a `token_embd.weight` fallback when both `general.parameter_count` and `general.size_label` are absent. Ships as v1.4.0.

**Architecture:** Single feature branch `phase6b-speculative-and-parser` off `main`. Two new files: `internal/gguf/params_arch.go` and `internal/models/speculative.go`. Additive changes to `internal/gguf/header.go`, `internal/gguftest/builder.go`, `internal/cli/serve.go`, `internal/cli/fit.go`, `internal/cli/list.go`, `internal/launchd/ports.go`, `cmd/gguf-inspect/main.go`. **Zero changes to `PlistSpec` or the plist template** — the v1.3.0 `--api-key` pattern (append to `argv` after `recipes.FlagsFor`) carries the draft flags through unchanged. **Zero `Deps` field additions.**

**Tech Stack:** Go 1.26.2, cobra, stdlib `encoding/binary` / `bufio` / `text/tabwriter` / `sort` / `math`, GoReleaser, Homebrew tap.

**Reference:** `docs/superpowers/specs/2026-05-12-phase6b-speculative-and-parser-design.md` (the approved spec — read alongside this plan).

---

## How to use this plan

- All 17 implementation tasks (Tasks 1-17) land on a single feature branch `phase6b-speculative-and-parser`. **Implementers must stay on this branch — do not `git checkout`, `git switch`, or `git stash` for any reason.**
- Each implementation task = one commit. Run `go test ./... -race` after each task; commit only when green.
- Tasks 18-19 (release + memory) are orchestrator-driven and span the whole branch.
- Tasks are ordered: parser foundation first (Tasks 1-7) — keeps the speculative-decoding work clean of parser drama. Then the speculative pair logic (Task 8). Then plist helper (Task 9). Then `serve --draft` (Tasks 10-12). Then `fit --speculative` (Tasks 13-16). Docs (Task 17). Release + memory (Tasks 18-19).
- Spec section references like "(spec §3.2)" point to the approved spec — read for *why*. This plan is the *how*.

---

## File structure overview

**New files:**
- `internal/gguf/params_arch.go` (Task 3)
- `internal/gguf/params_arch_test.go` (Task 3)
- `internal/models/speculative.go` (Task 8)
- `internal/models/speculative_test.go` (Task 8)

**Modified files (high-touch):**
- `internal/gguftest/builder.go` (Task 1 — `WithTensor` option + tensor descriptor writer)
- `internal/gguf/header.go` (Task 2 — `ReadHeaderWithTensors`)
- `internal/gguf/params_arch.go` (Tasks 3, 4, 5 — formula table grows arch-by-arch)
- `cmd/gguf-inspect/main.go` (Task 6 — switch to `ReadHeaderWithTensors`)
- `internal/cli/list.go` (Task 7 — self-heal path switches to `ReadHeaderWithTensors`)
- `internal/launchd/ports.go` (Task 9 — `HasDraft` helper)
- `internal/cli/serve.go` (Tasks 10, 11 — `--draft` flag, post-recipe append)
- `internal/cli/serve_test.go` (Task 12 — six new tests)
- `internal/cli/fit.go` (Tasks 13, 14, 15 — `--speculative` flag, candidate enumeration, sort + table)
- `internal/cli/fit_test.go` (Task 16 — four new tests)
- `README.md` (Task 17)
- `docs/llamactl-prd-v1.5.md` (Task 17 — `--draft` flag documented)

**Untouched (by design):**
- `internal/launchd/plist.go` — no PlistSpec or template changes.
- `internal/cli/deps.go` — no new `Deps` fields.
- `internal/recipes/recipes.go` — recipes stay pure-function over the main model only.

---

## Branch discipline (read before dispatching any implementer)

Every implementer subagent must be told **explicitly**:

> "You are on branch `phase6b-speculative-and-parser`. Do not `git checkout`, `git switch`, `git stash`, `git reset`, or any branch-changing operation. If `git status` shows unexpected files, stop and ask. Your task is exactly Task N below; do not start Task N+1."

Skipping this primer is how Phase 3 lost an afternoon to silent branch switches. Phase 6a used it consistently across 30+ implementer dispatches and had zero branch issues.

---

## Task 1: `internal/gguftest` — `WithTensor` option

**Spec:** §4.5 ("`internal/gguftest/build.go` extension").

**Files:**
- Modify: `internal/gguftest/builder.go`
- Test: `internal/gguftest/builder_test.go`

The existing `Build(t, version, kvs...)` writes `tensor_count=0`. We need to inject tensor descriptors so the parser's tensor-info walk has something to find.

- [ ] **Step 1: Read the current builder + the GGUF v3 tensor descriptor format**

```bash
cat internal/gguftest/builder.go
```

Tensor descriptor layout (per GGUF v3 spec):
- `name_len` (u64)
- `name` (bytes)
- `n_dims` (u32)
- `dims[n_dims]` (u64 each)
- `type` (u32) — ignored by parser; we'll set to 0 (F32) for fixtures
- `offset` (u64) — ignored by parser; we'll set to 0

The parser will read past every descriptor until it finds `token_embd.weight` or exhausts the tensor info section.

- [ ] **Step 2: Write the failing test**

`internal/gguftest/builder_test.go` — add:

```go
func TestBuildWithTensor(t *testing.T) {
    bytes := gguftest.Build(t, 3,
        gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "qwen2"},
        gguftest.KV{Key: "qwen2.block_count", Type: gguftest.TypeU32, Value: uint32(28)},
    ).WithTensors(
        gguftest.Tensor{Name: "token_embd.weight", Dims: []uint64{3584, 152064}, Type: 0, Offset: 0},
    ).Bytes()

    // Verify: header parses with tensor_count=1
    // Verify: the tensor descriptor block follows the kv-block
    if len(bytes) < 100 {
        t.Fatalf("expected real bytes, got %d", len(bytes))
    }
}
```

But — note that `Build` currently returns `[]byte` directly, not a builder. We need a small API change.

- [ ] **Step 3: Refactor `Build` to support tensor descriptors**

The cleanest path: add a new function `BuildWithTensors(t, version, tensors, kvs...)` rather than changing `Build`'s signature (preserves backwards-compatibility for existing callers). Or: introduce a fluent builder. For Phase 6b's small surface, prefer the explicit function:

In `internal/gguftest/builder.go`:

```go
// Tensor is a GGUF tensor descriptor. Type and Offset are ignored by the
// parser's tensor-shape walk; tests typically set both to 0.
type Tensor struct {
    Name   string
    Dims   []uint64
    Type   uint32 // 0 = F32; ignored by ReadHeaderWithTensors
    Offset uint64 // ignored by ReadHeaderWithTensors
}

// BuildWithTensors mirrors Build but also writes a tensor info block after
// the kv-block. tensor_count in the GGUF header is set to len(tensors).
func BuildWithTensors(t *testing.T, version uint32, tensors []Tensor, kvs ...KV) []byte {
    t.Helper()
    var buf bytes.Buffer
    buf.WriteString("GGUF")
    binary.Write(&buf, binary.LittleEndian, version)
    binary.Write(&buf, binary.LittleEndian, uint64(len(tensors))) // tensor_count
    binary.Write(&buf, binary.LittleEndian, uint64(len(kvs)))
    for _, kv := range kvs {
        writeString(&buf, kv.Key)
        binary.Write(&buf, binary.LittleEndian, kv.Type)
        if kv.RawTypeOnly {
            continue
        }
        if err := writeValue(&buf, kv.Type, kv.Value); err != nil {
            t.Fatalf("gguftest.BuildWithTensors: key=%q: %v", kv.Key, err)
        }
    }
    for _, tn := range tensors {
        writeString(&buf, tn.Name)
        binary.Write(&buf, binary.LittleEndian, uint32(len(tn.Dims)))
        for _, d := range tn.Dims {
            binary.Write(&buf, binary.LittleEndian, d)
        }
        binary.Write(&buf, binary.LittleEndian, tn.Type)
        binary.Write(&buf, binary.LittleEndian, tn.Offset)
    }
    return buf.Bytes()
}
```

Keep `Build` unchanged (still writes tensor_count=0).

- [ ] **Step 4: Replace the failing test with one that uses the real API**

```go
func TestBuildWithTensors(t *testing.T) {
    raw := gguftest.BuildWithTensors(t, 3,
        []gguftest.Tensor{
            {Name: "token_embd.weight", Dims: []uint64{3584, 152064}, Type: 0, Offset: 0},
            {Name: "blk.0.attn_norm.weight", Dims: []uint64{3584}, Type: 0, Offset: 0},
        },
        gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "qwen2"},
    )

    // tensor_count is at offset 4 (magic) + 4 (version) = 8, u64 = 8 bytes
    tensorCount := binary.LittleEndian.Uint64(raw[8:16])
    if tensorCount != 2 {
        t.Fatalf("tensor_count = %d, want 2", tensorCount)
    }
    // Real assertions about the descriptor layout come in Task 2's tests;
    // this test just confirms the bytes structure.
}
```

Run: `go test ./internal/gguftest/... -v` → expect PASS.

- [ ] **Step 5: Verify + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/gguftest/builder.go internal/gguftest/builder_test.go
git commit -m "$(cat <<'EOF'
test(gguftest): add BuildWithTensors for tensor-info block fixtures

Phase 6b's GGUF parser extension needs fixtures with real tensor
descriptors (token_embd.weight specifically). BuildWithTensors mirrors
Build but emits a tensor info block after the kv-block, setting
tensor_count to len(tensors). Build itself is unchanged (tensor_count=0).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `internal/gguf` — `ReadHeaderWithTensors` skeleton

**Spec:** §4.2 (parser extension).

**Files:**
- Modify: `internal/gguf/header.go`
- Test: `internal/gguf/header_test.go`

This task adds the function but leaves `paramsBFromTokenEmbd` empty — the walk finds `token_embd.weight` and returns (with `ParamsCount=0` because no formula is registered yet). Task 3 fills in the formula table.

- [ ] **Step 1: Write the failing test for "no formula registered" behavior**

`internal/gguf/header_test.go` — add:

```go
func TestReadHeaderWithTensorsNoFormulaArch(t *testing.T) {
    // Arch "exotic" has no formula registered. The parser walks the tensor
    // info block, finds token_embd.weight, but leaves ParamsCount=0 because
    // paramsBFromTokenEmbd["exotic"] returns nil.
    raw := gguftest.BuildWithTensors(t, 3,
        []gguftest.Tensor{
            {Name: "token_embd.weight", Dims: []uint64{4096, 32000}, Type: 0, Offset: 0},
        },
        gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "exotic"},
        gguftest.KV{Key: "exotic.block_count", Type: gguftest.TypeU32, Value: uint32(32)},
    )
    path := filepath.Join(t.TempDir(), "exotic.gguf")
    if err := os.WriteFile(path, raw, 0o644); err != nil {
        t.Fatal(err)
    }

    h, err := gguf.ReadHeaderWithTensors(path)
    if err != nil {
        t.Fatalf("ReadHeaderWithTensors: %v", err)
    }
    if h.Architecture != "exotic" {
        t.Errorf("Architecture=%q, want %q", h.Architecture, "exotic")
    }
    if h.ParamsCount != 0 {
        t.Errorf("ParamsCount=%d, want 0 (no formula for exotic)", h.ParamsCount)
    }
}
```

Run: `go test ./internal/gguf/... -run TestReadHeaderWithTensorsNoFormulaArch -v` → expect FAIL ("undefined: gguf.ReadHeaderWithTensors").

- [ ] **Step 2: Implement `ReadHeaderWithTensors`**

In `internal/gguf/header.go`, append:

```go
// ReadHeaderWithTensors is ReadHeader plus a best-effort tensor-info walk
// when the kv-block paths leave ParamsCount=0. Walks tensor descriptors
// looking for "token_embd.weight"; on hit, computes paramsB via
// paramsBFromTokenEmbd[arch] (defined in params_arch.go) and updates
// ParamsCount. Returns early after the hit. On any walk error, returns the
// kv-block-derived header unchanged — never aborts.
func ReadHeaderWithTensors(path string) (Header, error) {
    f, err := os.Open(path)
    if err != nil {
        return Header{}, err
    }
    defer f.Close()
    return parseHeaderWithTensors(io.LimitReader(f, readLimit))
}

func parseHeaderWithTensors(r io.Reader) (Header, error) {
    br := bufio.NewReader(r)
    h, err := parseHeader(br)
    if err != nil {
        return h, err
    }
    if h.ParamsCount > 0 {
        return h, nil // kv-block already filled it; skip the walk
    }
    if h.TensorCount == 0 {
        return h, nil // nothing to walk
    }
    // Cap the walk so a corrupted tensor_count doesn't drive us off a cliff.
    const maxTensors = 100_000
    walkCount := h.TensorCount
    if walkCount > maxTensors {
        walkCount = maxTensors
    }
    for i := uint64(0); i < walkCount; i++ {
        name, err := readString(br)
        if err != nil {
            return h, nil // ran past available data; leave ParamsCount=0
        }
        var nDims uint32
        if err := binary.Read(br, binary.LittleEndian, &nDims); err != nil {
            return h, nil
        }
        if nDims > 8 { // sanity cap on dimensions
            return h, nil
        }
        dims := make([]uint64, nDims)
        for j := range dims {
            if err := binary.Read(br, binary.LittleEndian, &dims[j]); err != nil {
                return h, nil
            }
        }
        var tensorType uint32
        if err := binary.Read(br, binary.LittleEndian, &tensorType); err != nil {
            return h, nil
        }
        var offset uint64
        if err := binary.Read(br, binary.LittleEndian, &offset); err != nil {
            return h, nil
        }
        if name == "token_embd.weight" {
            // GGUF stores token_embd.weight as [hidden_dim, vocab_size]
            // (verified against real Qwen2.5/Llama-3 GGUFs during Phase 6b
            // fixture work; the smaller dim is hidden, larger is vocab).
            if len(dims) < 2 {
                return h, nil
            }
            hiddenDim := int(dims[0])
            vocabSize := int(dims[1])
            blockCount := blockCountFromHeader(h)
            if formula, ok := paramsBFromTokenEmbd[h.Architecture]; ok && formula != nil {
                paramsB := formula(hiddenDim, vocabSize, blockCount)
                if paramsB > 0 {
                    h.ParamsCount = uint64(paramsB * 1e9)
                }
            }
            return h, nil
        }
    }
    return h, nil
}

// blockCountFromHeader extracts the <arch>.block_count value from the parsed
// kv-block. The header struct doesn't carry it directly (would require
// extending Header), so we recompute by re-parsing — but parseHeader
// already discarded the entries. This helper is wired in Task 3 once we
// have a place to store block_count. Returns 0 in this task.
//
// TODO(Task 3): wire block_count from parseHeader into Header struct.
func blockCountFromHeader(h Header) int {
    return 0
}
```

Wait — `blockCountFromHeader` is a circular dependency on Task 3. Instead, extend `Header` with `BlockCount` now:

In `internal/gguf/header.go` around line 25, extend the struct:

```go
type Header struct {
    Version       uint32
    TensorCount   uint64
    Architecture  string
    ParamsCount   uint64
    ContextLength uint64
    BlockCount    int    // <arch>.block_count; 0 if absent
}
```

In `parseHeader`, after the existing context_length lookup loop (around line 157-171), add:

```go
if h.Architecture != "" {
    wantKey := h.Architecture + ".block_count"
    for _, kv := range entries {
        if kv.key != wantKey {
            continue
        }
        switch v := kv.value.(type) {
        case uint32:
            h.BlockCount = int(v)
        case uint64:
            h.BlockCount = int(v)
        case int32:
            if v >= 0 {
                h.BlockCount = int(v)
            }
        case int64:
            if v >= 0 {
                h.BlockCount = int(v)
            }
        }
        break
    }
}
```

Replace the placeholder `blockCountFromHeader` call with `h.BlockCount`.

- [ ] **Step 3: Add `paramsBFromTokenEmbd` declaration (empty for now)**

Create a stub `internal/gguf/params_arch.go`:

```go
package gguf

// paramsBFromTokenEmbd holds per-architecture formulas that estimate total
// parameter count in billions given hidden_dim (from token_embd.weight[0]),
// vocab_size (from token_embd.weight[1]), and block_count (from
// <arch>.block_count in the kv-block).
//
// Formulas land arch-by-arch in Tasks 3-5. Unknown arches return nil and
// the parser leaves ParamsCount=0 (preserves today's "?" display).
var paramsBFromTokenEmbd = map[string]func(hiddenDim, vocabSize, blockCount int) float64{}
```

- [ ] **Step 4: Verify the test passes**

```bash
go test ./internal/gguf/... -run TestReadHeaderWithTensorsNoFormulaArch -v
```

Expected: PASS (the walk completes, paramsBFromTokenEmbd["exotic"] is nil → no update → ParamsCount stays 0 as the test expects).

- [ ] **Step 5: Add the "no tensors to walk" + truncation tests**

Still in `internal/gguf/header_test.go`:

```go
func TestReadHeaderWithTensorsParamsCountAlreadySet(t *testing.T) {
    // parameter_count is present in the kv-block; walk should be skipped
    // entirely (ParamsCount preserved at the kv-block value).
    raw := gguftest.BuildWithTensors(t, 3,
        []gguftest.Tensor{
            {Name: "token_embd.weight", Dims: []uint64{4096, 32000}, Type: 0, Offset: 0},
        },
        gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "llama"},
        gguftest.KV{Key: "general.parameter_count", Type: gguftest.TypeU64, Value: uint64(7_000_000_000)},
    )
    path := filepath.Join(t.TempDir(), "f.gguf")
    if err := os.WriteFile(path, raw, 0o644); err != nil {
        t.Fatal(err)
    }
    h, err := gguf.ReadHeaderWithTensors(path)
    if err != nil {
        t.Fatal(err)
    }
    if h.ParamsCount != 7_000_000_000 {
        t.Errorf("ParamsCount=%d, want 7B preserved from kv-block", h.ParamsCount)
    }
}

func TestReadHeaderWithTensorsTruncatedDescriptor(t *testing.T) {
    raw := gguftest.BuildWithTensors(t, 3,
        []gguftest.Tensor{
            {Name: "token_embd.weight", Dims: []uint64{4096, 32000}, Type: 0, Offset: 0},
        },
        gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "exotic"},
    )
    // Truncate before the descriptor finishes.
    truncated := raw[:len(raw)-8]
    path := filepath.Join(t.TempDir(), "trunc.gguf")
    if err := os.WriteFile(path, truncated, 0o644); err != nil {
        t.Fatal(err)
    }
    h, err := gguf.ReadHeaderWithTensors(path)
    if err != nil {
        t.Fatalf("expected no error on truncation; got %v", err)
    }
    if h.ParamsCount != 0 {
        t.Errorf("ParamsCount=%d, want 0 on truncation", h.ParamsCount)
    }
}

func TestReadHeaderWithTensorsImplausibleTensorCount(t *testing.T) {
    // Hand-craft bytes so tensor_count is bogus (overflows the walk cap).
    var buf bytes.Buffer
    buf.WriteString("GGUF")
    binary.Write(&buf, binary.LittleEndian, uint32(3))
    binary.Write(&buf, binary.LittleEndian, uint64(1_000_000)) // tensor_count over the 100k cap
    binary.Write(&buf, binary.LittleEndian, uint64(1))        // kv_count
    // One KV: general.architecture = "exotic"
    var keyBuf bytes.Buffer
    binary.Write(&keyBuf, binary.LittleEndian, uint64(len("general.architecture")))
    keyBuf.WriteString("general.architecture")
    buf.Write(keyBuf.Bytes())
    binary.Write(&buf, binary.LittleEndian, uint32(gguftest.TypeString))
    binary.Write(&buf, binary.LittleEndian, uint64(len("exotic")))
    buf.WriteString("exotic")

    path := filepath.Join(t.TempDir(), "bogus.gguf")
    if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
        t.Fatal(err)
    }
    h, err := gguf.ReadHeaderWithTensors(path)
    if err != nil {
        t.Fatalf("expected no error from bogus tensor_count; got %v", err)
    }
    if h.ParamsCount != 0 {
        t.Errorf("ParamsCount=%d, want 0 (no tensor descriptors after kvs)", h.ParamsCount)
    }
}
```

Run: `go test ./internal/gguf/... -v` → expect all PASS.

- [ ] **Step 6: Verify + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/gguf/header.go internal/gguf/header_test.go internal/gguf/params_arch.go
git commit -m "$(cat <<'EOF'
feat(gguf): ReadHeaderWithTensors fallback scaffolding

Adds parser walk of the tensor info block when the kv-block paths leave
ParamsCount=0. Looks specifically for token_embd.weight; on hit, defers
to paramsBFromTokenEmbd[arch] for the formula. This commit leaves the
formula table empty (filled in Tasks 3-5). Header gains BlockCount field
populated from <arch>.block_count in the kv-block. Walk never aborts on
parse errors — falls through to kv-block value.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: `internal/gguf` — `llamaParams` formula + arch="llama" tests

**Spec:** §4.3 (per-arch formulas).

**Files:**
- Modify: `internal/gguf/params_arch.go`
- Test: `internal/gguf/params_arch_test.go`

The formula derivation is approximation; we calibrate against Llama-3-8B-Instruct as the reference fixture.

- [ ] **Step 1: Find reference Llama-3-8B values for calibration**

Llama-3-8B-Instruct reference values (from the model card on Hugging Face):
- Hidden dim: 4096
- Vocab size: 128256
- Block count: 32
- Actual parameter count: 8.03 B

Formula sketch (per spec §4.3 `llamaParams`):
- Embedding: vocab × hidden = 128256 × 4096 ≈ 0.525 B
- Per block: ~12 × hidden² (covers Q/K/V/O projection + 2-layer FFN gated; rough average)
  - 12 × 4096² × 32 ≈ 6.44 B
- Output head: vocab × hidden ≈ 0.525 B
- Total: ~7.49 B → ~7% below 8.03 B

That's outside our ±15% target. Better fit comes from including the GQA factor: Llama-3 uses 8 KV heads vs 32 Q heads, but the per-block formula already collapses that to a constant. The 7.49 → 8.03 gap suggests the constant should be higher. Empirical fit:

Solve `12k × 4096² × 32 + 2 × 128256 × 4096 = 8.03e9` → `12k ≈ 14.4` → use **14.5**.

Calibrate against Llama-3-70B too for sanity:
- hidden=8192, vocab=128256, blocks=80
- Formula: `14.5 × 8192² × 80 + 2 × 128256 × 8192 = 77.9 B + 2.1 B ≈ 80 B`
- Actual: 70.6 B → 13% overestimate. Within tolerance, but biased high.

Final formula constant for `llama`: **14.5** for per-block, with the standard untied-output assumption.

- [ ] **Step 2: Write the failing test**

`internal/gguf/params_arch_test.go` — create:

```go
package gguf_test

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/gregmundy/llamactl/internal/gguf"
    "github.com/gregmundy/llamactl/internal/gguftest"
)

func TestParamsArchLlama3_8B(t *testing.T) {
    raw := gguftest.BuildWithTensors(t, 3,
        []gguftest.Tensor{
            {Name: "token_embd.weight", Dims: []uint64{4096, 128256}, Type: 0, Offset: 0},
        },
        gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "llama"},
        gguftest.KV{Key: "llama.block_count", Type: gguftest.TypeU32, Value: uint32(32)},
    )
    path := filepath.Join(t.TempDir(), "llama-8b.gguf")
    if err := os.WriteFile(path, raw, 0o644); err != nil {
        t.Fatal(err)
    }
    h, err := gguf.ReadHeaderWithTensors(path)
    if err != nil {
        t.Fatal(err)
    }
    // Reference: Llama-3-8B-Instruct is 8.03 B.
    paramsB := float64(h.ParamsCount) / 1e9
    if paramsB < 8.03*0.85 || paramsB > 8.03*1.15 {
        t.Errorf("Llama-3-8B: paramsB=%.2f, want 8.03 ± 15%%", paramsB)
    }
}

func TestParamsArchLlama3_70B(t *testing.T) {
    raw := gguftest.BuildWithTensors(t, 3,
        []gguftest.Tensor{
            {Name: "token_embd.weight", Dims: []uint64{8192, 128256}, Type: 0, Offset: 0},
        },
        gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "llama"},
        gguftest.KV{Key: "llama.block_count", Type: gguftest.TypeU32, Value: uint32(80)},
    )
    path := filepath.Join(t.TempDir(), "llama-70b.gguf")
    if err := os.WriteFile(path, raw, 0o644); err != nil {
        t.Fatal(err)
    }
    h, err := gguf.ReadHeaderWithTensors(path)
    if err != nil {
        t.Fatal(err)
    }
    paramsB := float64(h.ParamsCount) / 1e9
    // Reference: Llama-3-70B is 70.6 B; allow ±15%.
    if paramsB < 70.6*0.85 || paramsB > 70.6*1.15 {
        t.Errorf("Llama-3-70B: paramsB=%.2f, want 70.6 ± 15%%", paramsB)
    }
}
```

Run: `go test ./internal/gguf/... -run TestParamsArchLlama -v` → expect FAIL (no formula registered).

- [ ] **Step 3: Implement `llamaParams` + register**

In `internal/gguf/params_arch.go`:

```go
package gguf

// llamaParams estimates total parameters for Llama-family models given:
//   hidden:  token_embd.weight dimension 0 (hidden_dim)
//   vocab:   token_embd.weight dimension 1 (vocab_size)
//   blocks:  <arch>.block_count
//
// Calibrated against Llama-3-8B (hidden=4096, vocab=128256, blocks=32 →
// reference 8.03 B) and Llama-3-70B (hidden=8192, vocab=128256, blocks=80 →
// reference 70.6 B). Accuracy ~10-15% across the family; overestimates large
// models slightly due to the constant per-block coefficient.
func llamaParams(hidden, vocab, blocks int) float64 {
    if hidden <= 0 || vocab <= 0 || blocks <= 0 {
        return 0
    }
    embedding := float64(vocab) * float64(hidden)
    perBlock := 14.5 * float64(hidden) * float64(hidden)
    output := float64(vocab) * float64(hidden) // untied; ~+10% when models use tied embeddings
    total := embedding + perBlock*float64(blocks) + output
    return total / 1e9
}

var paramsBFromTokenEmbd = map[string]func(hidden, vocab, blocks int) float64{
    "llama": llamaParams,
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/gguf/... -run TestParamsArchLlama -v
```

Expected: PASS (both 8B and 70B within ±15%).

If 70B fails: the model uses tied output embeddings (some Llama-3-70B fine-tunes do). Adjust `output` to 0 and re-test. **Don't ship without verifying against real fixtures during Task 18 live smoke.**

- [ ] **Step 5: Verify + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/gguf/params_arch.go internal/gguf/params_arch_test.go
git commit -m "$(cat <<'EOF'
feat(gguf): llamaParams formula for Llama-family ParamsB fallback

Calibrated against Llama-3-8B (8.03 B reference) and Llama-3-70B
(70.6 B reference). Per-block coefficient 14.5 covers attention + MLP
with the GQA collapse implicit in the constant. Untied-output assumption
biases the estimate ~10% high for tied-output fine-tunes — acceptable
within the ±15% tolerance documented in spec §7.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `internal/gguf` — qwen2 / qwen3 / gemma3 formulas

**Spec:** §4.3.

**Files:**
- Modify: `internal/gguf/params_arch.go`
- Test: `internal/gguf/params_arch_test.go`

Calibration references (from Hugging Face model cards):

- Qwen2.5-7B: hidden=3584, vocab=152064, blocks=28, paramsB=7.62
- Qwen2.5-32B: hidden=5120, vocab=152064, blocks=64, paramsB=32.76
- Qwen3-1.7B: hidden=2048, vocab=151936, blocks=28, paramsB=1.72
- Gemma-3-4B: hidden=2560, vocab=262144, blocks=34, paramsB=4.30
- Gemma-3-27B: hidden=5376, vocab=262144, blocks=62, paramsB=27.0

Qwen and Gemma have larger vocabularies than Llama (Qwen ~152k, Gemma ~262k vs Llama 128k). The per-block coefficient also differs slightly.

- [ ] **Step 1: Derive `qwen2Params` constant**

Solve for the coefficient using Qwen2.5-7B:
- embedding = 152064 × 3584 = 0.545 B
- output = 0.545 B
- per_block = X × 3584² × 28 = X × 360M
- Total = 7.62 B → per_block_total = 6.53 B → X = 18.1

Sanity-check with Qwen2.5-32B:
- 152064 × 5120 = 0.779 B (embedding)
- 0.779 B (output)
- 18.1 × 5120² × 64 = 30.4 B
- Total = 31.9 B vs reference 32.76 B → 2.6% low. Excellent.

Constant for `qwen2`: **18.1**.

- [ ] **Step 2: Derive `qwen3Params` constant**

Qwen3-1.7B reference:
- 151936 × 2048 = 0.311 B (embedding)
- 0.311 B (output)
- X × 2048² × 28 = X × 117.4M
- Total = 1.72 B → per_block = 1.10 B → X = 9.4

Hmm, that's quite different from qwen2's 18.1. Qwen3 uses much more aggressive MLP compression. Sanity-check with Qwen3-0.6B (hidden=1024, vocab=151936, blocks=28, paramsB=0.6):
- 151936 × 1024 = 0.156 B (×2 for embedding+output = 0.311 B)
- 9.4 × 1024² × 28 = 0.276 B
- Total = 0.59 B vs 0.6 reference → spot on.

Constant for `qwen3`: **9.4**.

- [ ] **Step 3: Derive `gemma3Params` constant**

Gemma-3-4B reference (hidden=2560, vocab=262144, blocks=34, paramsB=4.30):
- 262144 × 2560 = 0.671 B (×2 = 1.342 B)
- X × 2560² × 34 = X × 223M
- Total = 4.30 B → per_block_total = 2.96 B → X = 13.3

Sanity-check with Gemma-3-27B (hidden=5376, vocab=262144, blocks=62, paramsB=27.0):
- 262144 × 5376 = 1.409 B (×2 = 2.818 B)
- 13.3 × 5376² × 62 = 23.84 B
- Total = 26.66 B vs 27.0 → 1.3% low. Excellent.

Constant for `gemma3`: **13.3**.

Gemma may use tied embeddings (Gemma 1 did; verify against actual Gemma-3 model files during live smoke). If 4B test fails high, drop the `output` term and re-fit.

- [ ] **Step 4: Write the failing tests**

In `internal/gguf/params_arch_test.go`, append:

```go
func TestParamsArchQwen2_5_7B(t *testing.T) {
    paramsB := paramsBFor(t, "qwen2",
        []uint64{3584, 152064}, // token_embd dims (hidden, vocab)
        28,                      // blocks
    )
    if paramsB < 7.62*0.85 || paramsB > 7.62*1.15 {
        t.Errorf("Qwen2.5-7B: paramsB=%.2f, want 7.62 ± 15%%", paramsB)
    }
}

func TestParamsArchQwen2_5_32B(t *testing.T) {
    paramsB := paramsBFor(t, "qwen2",
        []uint64{5120, 152064}, 64)
    if paramsB < 32.76*0.85 || paramsB > 32.76*1.15 {
        t.Errorf("Qwen2.5-32B: paramsB=%.2f, want 32.76 ± 15%%", paramsB)
    }
}

func TestParamsArchQwen3_1_7B(t *testing.T) {
    paramsB := paramsBFor(t, "qwen3",
        []uint64{2048, 151936}, 28)
    if paramsB < 1.72*0.85 || paramsB > 1.72*1.15 {
        t.Errorf("Qwen3-1.7B: paramsB=%.2f, want 1.72 ± 15%%", paramsB)
    }
}

func TestParamsArchQwen3_0_6B(t *testing.T) {
    paramsB := paramsBFor(t, "qwen3",
        []uint64{1024, 151936}, 28)
    if paramsB < 0.6*0.85 || paramsB > 0.6*1.15 {
        t.Errorf("Qwen3-0.6B: paramsB=%.2f, want 0.6 ± 15%%", paramsB)
    }
}

func TestParamsArchGemma3_4B(t *testing.T) {
    paramsB := paramsBFor(t, "gemma3",
        []uint64{2560, 262144}, 34)
    if paramsB < 4.30*0.85 || paramsB > 4.30*1.15 {
        t.Errorf("Gemma-3-4B: paramsB=%.2f, want 4.30 ± 15%%", paramsB)
    }
}

func TestParamsArchGemma3_27B(t *testing.T) {
    paramsB := paramsBFor(t, "gemma3",
        []uint64{5376, 262144}, 62)
    if paramsB < 27.0*0.85 || paramsB > 27.0*1.15 {
        t.Errorf("Gemma-3-27B: paramsB=%.2f, want 27.0 ± 15%%", paramsB)
    }
}

// paramsBFor is a test helper that builds a synthetic fixture and runs
// ReadHeaderWithTensors against it, returning paramsB.
func paramsBFor(t *testing.T, arch string, tokenEmbdDims []uint64, blocks uint32) float64 {
    t.Helper()
    raw := gguftest.BuildWithTensors(t, 3,
        []gguftest.Tensor{
            {Name: "token_embd.weight", Dims: tokenEmbdDims, Type: 0, Offset: 0},
        },
        gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: arch},
        gguftest.KV{Key: arch + ".block_count", Type: gguftest.TypeU32, Value: blocks},
    )
    path := filepath.Join(t.TempDir(), arch+".gguf")
    if err := os.WriteFile(path, raw, 0o644); err != nil {
        t.Fatal(err)
    }
    h, err := gguf.ReadHeaderWithTensors(path)
    if err != nil {
        t.Fatal(err)
    }
    return float64(h.ParamsCount) / 1e9
}
```

Run: `go test ./internal/gguf/... -v` → expect the new qwen/gemma tests FAIL (formulas not registered yet).

- [ ] **Step 5: Implement the three formulas**

Append to `internal/gguf/params_arch.go`:

```go
// qwen2Params estimates total parameters for Qwen2.x-family models.
// Calibrated against Qwen2.5-7B (3584/152064/28 → 7.62 B reference) and
// Qwen2.5-32B (5120/152064/64 → 32.76 B reference). The 18.1 per-block
// coefficient reflects Qwen2.5's larger MLP-to-hidden ratio vs Llama.
func qwen2Params(hidden, vocab, blocks int) float64 {
    if hidden <= 0 || vocab <= 0 || blocks <= 0 {
        return 0
    }
    embedding := float64(vocab) * float64(hidden)
    perBlock := 18.1 * float64(hidden) * float64(hidden)
    output := float64(vocab) * float64(hidden)
    return (embedding + perBlock*float64(blocks) + output) / 1e9
}

// qwen3Params estimates Qwen3-family models. Calibrated against Qwen3-0.6B
// (1024/151936/28 → 0.6 B) and Qwen3-1.7B (2048/151936/28 → 1.72 B). The
// 9.4 coefficient is much lower than qwen2's 18.1 because Qwen3 uses more
// aggressive MLP compression with the same family size buckets.
func qwen3Params(hidden, vocab, blocks int) float64 {
    if hidden <= 0 || vocab <= 0 || blocks <= 0 {
        return 0
    }
    embedding := float64(vocab) * float64(hidden)
    perBlock := 9.4 * float64(hidden) * float64(hidden)
    output := float64(vocab) * float64(hidden)
    return (embedding + perBlock*float64(blocks) + output) / 1e9
}

// gemma3Params estimates Gemma3-family models. Calibrated against
// Gemma-3-4B (2560/262144/34 → 4.30 B) and Gemma-3-27B (5376/262144/62 →
// 27.0 B). Gemma's large vocabulary (262k tokens) makes embedding+output
// contribute disproportionately for smaller models.
func gemma3Params(hidden, vocab, blocks int) float64 {
    if hidden <= 0 || vocab <= 0 || blocks <= 0 {
        return 0
    }
    embedding := float64(vocab) * float64(hidden)
    perBlock := 13.3 * float64(hidden) * float64(hidden)
    output := float64(vocab) * float64(hidden)
    return (embedding + perBlock*float64(blocks) + output) / 1e9
}
```

Register them — replace the existing `paramsBFromTokenEmbd` declaration with:

```go
var paramsBFromTokenEmbd = map[string]func(hidden, vocab, blocks int) float64{
    "llama":  llamaParams,
    "qwen2":  qwen2Params,
    "qwen3":  qwen3Params,
    "gemma3": gemma3Params,
}
```

- [ ] **Step 6: Run all formula tests**

```bash
go test ./internal/gguf/... -v
```

Expected: ALL PASS.

If a particular calibration test fails outside the ±15% band:
- First, double-check the test's expected paramsB matches the model card (don't rebase coefficients on a typo).
- If real, adjust the per-block coefficient by re-solving against the failing reference. Update the inline calibration comment in the function.
- Re-run the whole arch's tests; the sibling reference should still pass.

- [ ] **Step 7: Verify + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/gguf/params_arch.go internal/gguf/params_arch_test.go
git commit -m "$(cat <<'EOF'
feat(gguf): qwen2 / qwen3 / gemma3 paramsB formulas

Per-arch coefficients calibrated against published reference paramsB:
- qwen2: 18.1 per-block (Qwen2.5-7B, Qwen2.5-32B)
- qwen3: 9.4 per-block (Qwen3-0.6B, Qwen3-1.7B; tighter MLP than qwen2)
- gemma3: 13.3 per-block (Gemma-3-4B, Gemma-3-27B; 262k vocab matters)

All within ±15% tolerance per spec §7. Formulas registered in
paramsBFromTokenEmbd; unknown arches fall through to ParamsCount=0.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: `internal/gguf` — `mistral` alias to `llamaParams`

**Spec:** §4.3 (mistral shares Llama layer structure).

**Files:**
- Modify: `internal/gguf/params_arch.go`
- Test: `internal/gguf/params_arch_test.go`

Mistral 7B uses 4096 hidden / 32000 vocab (older, pre-Llama-3) / 32 blocks. ParamsB reference: 7.24.

- [ ] **Step 1: Write the failing test**

In `internal/gguf/params_arch_test.go`, append:

```go
func TestParamsArchMistral7B(t *testing.T) {
    paramsB := paramsBFor(t, "mistral",
        []uint64{4096, 32000}, 32)
    if paramsB < 7.24*0.85 || paramsB > 7.24*1.15 {
        t.Errorf("Mistral-7B: paramsB=%.2f, want 7.24 ± 15%%", paramsB)
    }
}
```

Sanity-check the math with `llamaParams(4096, 32000, 32)`:
- embedding = 32000 × 4096 = 0.131 B
- output = 0.131 B
- per_block = 14.5 × 4096² × 32 = 7.78 B
- Total = 8.04 B vs 7.24 reference → 11% high, within ±15%.

Run: `go test ./internal/gguf/... -run TestParamsArchMistral -v` → expect FAIL (mistral not registered).

- [ ] **Step 2: Register the alias**

In `internal/gguf/params_arch.go`, extend the registration:

```go
var paramsBFromTokenEmbd = map[string]func(hidden, vocab, blocks int) float64{
    "llama":   llamaParams,
    "qwen2":   qwen2Params,
    "qwen3":   qwen3Params,
    "gemma3":  gemma3Params,
    "mistral": llamaParams, // Mistral 7B/8x7B share Llama layer structure
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/gguf/... -v
```

Expected: PASS.

- [ ] **Step 4: Verify + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/gguf/params_arch.go internal/gguf/params_arch_test.go
git commit -m "$(cat <<'EOF'
feat(gguf): mistral arch alias to llamaParams

Mistral 7B / 8x7B share the Llama layer structure; reusing llamaParams
avoids duplicating the formula. Calibrated against Mistral-7B-v0.1
(4096/32000/32 → 7.24 B reference; within ±15% at 8.04 estimate).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `cmd/gguf-inspect` — use `ReadHeaderWithTensors` + annotation

**Spec:** §4.5 (cmd/gguf-inspect annotation).

**Files:**
- Modify: `cmd/gguf-inspect/main.go`

The diagnostic tool should surface when the new fallback fires so users investigating "why doesn't this model show paramsB" can see whether the kv-block or the tensor walk produced the value.

- [ ] **Step 1: Read the current main.go**

```bash
cat cmd/gguf-inspect/main.go
```

Look for where it calls `gguf.ReadHeader(path)` and where it prints `ParamsCount`.

- [ ] **Step 2: Adapt the diff**

Switch the parse call to `ReadHeaderWithTensors`. For the annotation: call `ReadHeader` first, then `ReadHeaderWithTensors`. If the first returns ParamsCount=0 but the second produces a non-zero value, the value came from the tensor walk.

```go
// Before:
h, err := gguf.ReadHeader(path)

// After:
h0, err := gguf.ReadHeader(path)
if err != nil {
    // ...existing error handling...
}
h, err := gguf.ReadHeaderWithTensors(path)
if err != nil {
    fmt.Printf("  ParamsCount: %d (kv-block only; tensor walk failed: %v)\n", h0.ParamsCount, err)
} else if h.ParamsCount != h0.ParamsCount && h0.ParamsCount == 0 {
    fmt.Printf("  ParamsCount: %d (via tensor-shape fallback)\n", h.ParamsCount)
} else {
    fmt.Printf("  ParamsCount: %d\n", h.ParamsCount)
}
```

Adapt to the actual variable names + Printf format strings used in `main.go`. Don't change other lines (Arch, ContextLength, etc.).

- [ ] **Step 3: Build and verify the tool still runs**

```bash
go build ./cmd/gguf-inspect
./gguf-inspect $(ls ~/.local/share/llama-models/*/*.gguf | head -1)
rm gguf-inspect
```

Expected: prints architecture, paramsCount (likely from kv-block on installed models), context_length. No crashes.

- [ ] **Step 4: Commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add cmd/gguf-inspect/main.go
git commit -m "$(cat <<'EOF'
feat(gguf-inspect): annotate when tensor-shape fallback supplied paramsB

Switches the diagnostic tool to ReadHeaderWithTensors and prints
"(via tensor-shape fallback)" when the kv-block produced 0 but the
tensor walk recovered a value. Helps debugging older fine-tunes that
lack both general.parameter_count and general.size_label.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: `internal/cli/list.go` — self-heal uses `ReadHeaderWithTensors`

**Spec:** §4.5 (`internal/cli/list_test.go::TestListSelfHealsViaTensorShape`).

**Files:**
- Modify: `internal/cli/list.go`
- Test: `internal/cli/list_test.go`

Phase 6a wired list to re-parse GGUF headers when metadata ParamsB=0. That call uses `gguf.ReadHeader`. Switch to `ReadHeaderWithTensors` so the new fallback ships retroactively to the installed model library.

- [ ] **Step 1: Locate the self-heal callsite**

```bash
grep -n "ReadHeader\|self.heal\|ParamsB == 0" internal/cli/list.go
```

Expected: one line calling `gguf.ReadHeader(meta.GGUFPath)`.

- [ ] **Step 2: Write the failing test**

In `internal/cli/list_test.go`, append:

```go
func TestListSelfHealsViaTensorShape(t *testing.T) {
    tmp := t.TempDir()
    // Build a synthetic GGUF: arch=qwen2, no parameter_count, no size_label,
    // but with a token_embd.weight tensor descriptor.
    raw := gguftest.BuildWithTensors(t, 3,
        []gguftest.Tensor{
            {Name: "token_embd.weight", Dims: []uint64{3584, 152064}, Type: 0, Offset: 0},
        },
        gguftest.KV{Key: "general.architecture", Type: gguftest.TypeString, Value: "qwen2"},
        gguftest.KV{Key: "qwen2.block_count", Type: gguftest.TypeU32, Value: uint32(28)},
    )
    ggufPath := filepath.Join(tmp, "test.gguf")
    if err := os.WriteFile(ggufPath, raw, 0o644); err != nil {
        t.Fatal(err)
    }

    store := newFakeModelStore()
    store.put(models.Metadata{
        ID:       "qwen2.5-7b-test",
        Repo:     "test/repo",
        Quant:    models.Q4_K_M,
        GGUFPath: ggufPath,
        ParamsB:  0, // stale; should be healed
        Arch:     "qwen2",
    })

    deps := minimalListDeps(t, store)
    var out bytes.Buffer
    if err := runList(context.Background(), deps, &out); err != nil {
        t.Fatal(err)
    }
    healed, _ := store.Get(context.Background(), "qwen2.5-7b-test")
    if healed.ParamsB <= 0 || healed.ParamsB < 6.5 || healed.ParamsB > 9 {
        t.Errorf("expected ParamsB healed to ~7.5 B; got %.2f", healed.ParamsB)
    }
}
```

Adapt to the actual list test scaffolding — `newFakeModelStore` and `minimalListDeps` exist in the test file's helper section; if not, find the analogous setup in `list_test.go::TestListSelfHealsZeroParamsB` (Phase 6a #15) and clone its scaffolding.

Run: `go test ./internal/cli/... -run TestListSelfHealsViaTensorShape -v` → expect FAIL (current self-heal uses ReadHeader, which leaves ParamsB=0 for the synthetic fixture lacking both fallbacks).

- [ ] **Step 3: Switch the self-heal call**

In `internal/cli/list.go`, change the self-heal site:

```go
// Before:
h, err := gguf.ReadHeader(meta.GGUFPath)

// After:
h, err := gguf.ReadHeaderWithTensors(meta.GGUFPath)
```

That's the entire fix.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/cli/... -run TestListSelfHeals -v
```

Expected: PASS (both the existing Phase 6a #15 test and the new one).

- [ ] **Step 5: Verify + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/list.go internal/cli/list_test.go
git commit -m "$(cat <<'EOF'
feat(list): self-heal uses ReadHeaderWithTensors for tensor-shape fallback

Older fine-tunes lack both general.parameter_count and general.size_label;
Phase 6a's self-heal couldn't recover them. Switching to
ReadHeaderWithTensors lets the new token_embd.weight walk fill in
ParamsB for any installed model on next `llamactl list` invocation
(supported arches: llama, qwen2, qwen3, gemma3, mistral).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: `internal/models/speculative.go` — `SpeculativePair` + `PairVerdict`

**Spec:** §3.4 (eligibility logic).

**Files:**
- Create: `internal/models/speculative.go`
- Create: `internal/models/speculative_test.go`

Pure function; no I/O. Shared by `serve.go` (Task 10) and `fit.go` (Task 14).

- [ ] **Step 1: Find the hardware.Info struct**

```bash
grep -n "type Info" /Users/greg/Development/llamactl/internal/hardware/*.go
```

Note the field that gives usable VRAM (likely `UsableGB` or computed via `models.GpuAddressableGB(hw)`).

- [ ] **Step 2: Write the failing test**

Create `internal/models/speculative_test.go`:

```go
package models_test

import (
    "testing"

    "github.com/gregmundy/llamactl/internal/hardware"
    "github.com/gregmundy/llamactl/internal/models"
)

func TestSpeculativePairArchMismatch(t *testing.T) {
    main := models.Model{ID: "qwen2.5-32b", Arch: models.ArchQwen25, ParamsB: 32}
    draft := models.Model{ID: "llama-3-1b", Arch: models.ArchLlama3, ParamsB: 1}
    hw := hardware.Info{TotalMemoryGB: 64}

    v := models.SpeculativePair(main, draft, hw, "chat")
    if v.Ok {
        t.Errorf("expected Ok=false on arch mismatch; verdict=%+v", v)
    }
    if !contains(v.Reason, "arch") {
        t.Errorf("Reason should mention arch; got %q", v.Reason)
    }
}

func TestSpeculativePairRatioTooSmall(t *testing.T) {
    main := models.Model{ID: "qwen2.5-7b", Arch: models.ArchQwen25, ParamsB: 7}
    draft := models.Model{ID: "qwen2.5-7b-alt", Arch: models.ArchQwen25, ParamsB: 7}
    hw := hardware.Info{TotalMemoryGB: 64}

    v := models.SpeculativePair(main, draft, hw, "chat")
    if v.Ok {
        t.Errorf("expected Ok=false at ratio 1×; got %+v", v)
    }
}

func TestSpeculativePairRatioOk(t *testing.T) {
    main := models.Model{ID: "qwen2.5-32b", Arch: models.ArchQwen25, ParamsB: 32, MaxCtx: 32768}
    draft := models.Model{ID: "qwen2.5-3b", Arch: models.ArchQwen25, ParamsB: 3, MaxCtx: 32768}
    hw := hardware.Info{TotalMemoryGB: 64}

    v := models.SpeculativePair(main, draft, hw, "chat")
    if !v.Ok {
        t.Errorf("expected Ok=true at ratio ~10×; got %+v", v)
    }
    if v.Reason != "" {
        t.Errorf("expected no warning at ratio ~10×; Reason=%q", v.Reason)
    }
    if v.SizeRatio < 9 || v.SizeRatio > 12 {
        t.Errorf("SizeRatio=%.2f, want ~10.7", v.SizeRatio)
    }
}

func TestSpeculativePairRatioWarning(t *testing.T) {
    main := models.Model{ID: "qwen2.5-32b", Arch: models.ArchQwen25, ParamsB: 32, MaxCtx: 32768}
    draft := models.Model{ID: "qwen2.5-0.5b", Arch: models.ArchQwen25, ParamsB: 0.5, MaxCtx: 32768}
    hw := hardware.Info{TotalMemoryGB: 64}

    v := models.SpeculativePair(main, draft, hw, "chat")
    if !v.Ok {
        t.Errorf("ratio 64× should be Ok=true (warning only); got %+v", v)
    }
    if v.Reason == "" {
        t.Errorf("expected warning Reason at ratio 64×; got empty")
    }
}

func TestSpeculativePairCombinedRAMTooBig(t *testing.T) {
    main := models.Model{ID: "qwen2.5-70b", Arch: models.ArchQwen25, ParamsB: 70, MaxCtx: 8192}
    draft := models.Model{ID: "qwen2.5-7b", Arch: models.ArchQwen25, ParamsB: 7, MaxCtx: 8192}
    hw := hardware.Info{TotalMemoryGB: 32} // 32 GB host can't hold 70B + 7B in Q4

    v := models.SpeculativePair(main, draft, hw, "chat")
    if v.Ok {
        t.Errorf("expected Ok=false on RAM exhaustion; verdict=%+v", v)
    }
    if !contains(v.Reason, "RAM") {
        t.Errorf("Reason should mention RAM; got %q", v.Reason)
    }
}

func TestSpeculativePairZeroParamsB(t *testing.T) {
    main := models.Model{ID: "unknown", Arch: models.ArchQwen25, ParamsB: 0}
    draft := models.Model{ID: "qwen2.5-3b", Arch: models.ArchQwen25, ParamsB: 3}
    hw := hardware.Info{TotalMemoryGB: 64}

    v := models.SpeculativePair(main, draft, hw, "chat")
    if v.Ok {
        t.Errorf("expected Ok=false on zero paramsB; got %+v", v)
    }
}

func TestSpeculativePairUnknownArch(t *testing.T) {
    main := models.Model{ID: "exotic-1", Arch: models.Arch("exotic"), ParamsB: 10}
    draft := models.Model{ID: "exotic-2", Arch: models.Arch("exotic"), ParamsB: 1}
    hw := hardware.Info{TotalMemoryGB: 32}

    v := models.SpeculativePair(main, draft, hw, "chat")
    if !v.ArchMatch {
        t.Errorf("expected ArchMatch=true for same arch even if unknown")
    }
    // KVCachePerTokenKB falls back to a constant for unknown arch; the
    // exact verdict depends on quant assumptions, but the pair should at
    // least compute without panicking.
    _ = v.CombinedRAMGB
}

func contains(s, substr string) bool {
    return len(s) >= len(substr) && (s == substr ||
        len(substr) > 0 && (s[:len(substr)] == substr ||
            (len(s) > len(substr) && contains(s[1:], substr))))
}
```

Run: `go test ./internal/models/... -run TestSpeculativePair -v` → expect FAIL (`SpeculativePair` undefined).

- [ ] **Step 3: Implement the function**

Create `internal/models/speculative.go`:

```go
// Package models — speculative.go: SpeculativePair eligibility logic for
// llama-server's --model-draft pairing. Pure function; no I/O.
package models

import (
    "fmt"
    "math"

    "github.com/gregmundy/llamactl/internal/hardware"
)

// PairVerdict reports whether using draft as the speculative-decoding draft
// for main is viable on the given host. Reason is non-empty when !Ok (the
// refusal message) or when a warning applies (size ratio outside ideal).
type PairVerdict struct {
    Ok            bool
    Reason        string
    CombinedRAMGB float64
    SizeRatio     float64 // main.ParamsB / draft.ParamsB
    ArchMatch     bool
}

// Speculative-decoding constants.
const (
    speculativeMinRatio    = 2.0  // below this: draft is too close to main, no speedup
    speculativeWarnLowRatio  = 5.0  // below this: warn (overhead may eat speedup)
    speculativeWarnHighRatio = 15.0 // above this: warn (draft too small, alignment poor)
    speculativeHeadroomGB    = 4.0  // same as fit's headroom
)

// SpeculativePair returns the verdict for using draft as the speculative-
// decoding draft for main, on hw running the named recipe.
//
// Refusal conditions (Ok=false):
//   - main.ParamsB <= 0 or draft.ParamsB <= 0 (size unknown)
//   - draft.Arch != main.Arch (tokenizer compatibility cannot be assumed)
//   - SizeRatio < speculativeMinRatio (no speedup possible)
//   - CombinedRAMGB > UsableGB - speculativeHeadroomGB (too-big)
//
// Warning conditions (Ok=true, Reason non-empty):
//   - SizeRatio < speculativeWarnLowRatio (overhead may exceed speedup)
//   - SizeRatio > speculativeWarnHighRatio (alignment likely poor)
//
// recipe argument selects the ctx-size assumption for KV-cache math; today
// only "chat" is used (8192). Future recipes can adjust.
func SpeculativePair(main, draft Model, hw hardware.Info, recipe string) PairVerdict {
    v := PairVerdict{
        ArchMatch: main.Arch == draft.Arch,
    }

    if main.ParamsB <= 0 || draft.ParamsB <= 0 {
        v.Reason = fmt.Sprintf("paramsB unknown (main=%.2f, draft=%.2f); cannot compute eligibility",
            main.ParamsB, draft.ParamsB)
        return v
    }
    if !v.ArchMatch {
        v.Reason = fmt.Sprintf("arch mismatch: main=%s, draft=%s (must match for tokenizer compatibility)",
            main.Arch, draft.Arch)
        return v
    }

    v.SizeRatio = main.ParamsB / draft.ParamsB
    if v.SizeRatio < speculativeMinRatio {
        v.Reason = fmt.Sprintf("size ratio %.1f× too small (draft must be at least %.0f× smaller than main)",
            v.SizeRatio, speculativeMinRatio)
        return v
    }

    // Combined RAM math: weights + KV cache for each model.
    ctx := ctxForRecipe(recipe)
    v.CombinedRAMGB = approxWeightsGB(main) + approxWeightsGB(draft) +
        KVCacheGB(main.Arch, main.ParamsB, ctx) + KVCacheGB(draft.Arch, draft.ParamsB, ctx)

    usable := math.Min(float64(hw.TotalMemoryGB), GpuAddressableGB(hw))
    budget := usable - speculativeHeadroomGB
    if v.CombinedRAMGB > budget {
        v.Reason = fmt.Sprintf("combined weights + KV cache (%.1f GB) exceeds usable RAM (%.1f GB); free %.1f GB or pick a smaller draft",
            v.CombinedRAMGB, budget, v.CombinedRAMGB-budget)
        return v
    }

    v.Ok = true
    switch {
    case v.SizeRatio < speculativeWarnLowRatio:
        v.Reason = fmt.Sprintf("size ratio %.1f× below recommended 5-15× (overhead may eat speedup)", v.SizeRatio)
    case v.SizeRatio > speculativeWarnHighRatio:
        v.Reason = fmt.Sprintf("size ratio %.1f× above recommended 5-15× (draft alignment may be poor)", v.SizeRatio)
    }
    return v
}

// approxWeightsGB picks the Q4_K_M row from QuantSizeTable as a conservative
// default. Speculative pairing doesn't commit to a specific quant at
// eligibility time — the eventual `serve` call uses whatever quant is
// installed. Q4_K_M is the spec-decoding sweet spot for both main and draft.
func approxWeightsGB(m Model) float64 {
    bucket := int(math.Round(m.ParamsB))
    if row, ok := QuantSizeTable[bucket]; ok {
        if size, ok := row[Q4_K_M]; ok {
            return size
        }
    }
    // Unknown bucket: rough estimate of 0.6 GB per billion params at Q4_K_M.
    return m.ParamsB * 0.6
}

// ctxForRecipe maps a recipe name to its default ctx-size for the eligibility
// math. Matches the values in internal/recipes/recipes.go but inlined here
// to avoid an import cycle (models package is below recipes in the import
// graph). If recipe names diverge, update this map.
func ctxForRecipe(recipe string) int {
    switch recipe {
    case "long-context":
        return 32768
    case "low-memory":
        return 2048
    case "code", "chat":
        return 8192
    default:
        return 8192
    }
}
```

`KVCacheGB` already exists in `internal/models/quants.go` (Phase 5 helper); confirm with `grep -n "func KVCacheGB" internal/models/quants.go`.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/models/... -run TestSpeculativePair -v
```

Expected: ALL 7 PASS.

If `TestSpeculativePairCombinedRAMTooBig` fails because the `hardware.Info` budget computation is off, inspect `hardware.Info`'s actual field names and adjust `usable := math.Min(...)` accordingly. The intent: pick the smaller of total RAM and the GPU-addressable budget (Apple Silicon unified memory is bounded by `iogpu.wired_limit_mb`).

- [ ] **Step 5: Verify + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/models/speculative.go internal/models/speculative_test.go
git commit -m "$(cat <<'EOF'
feat(models): SpeculativePair eligibility for --model-draft pairing

Pure function: same arch + size ratio in [2, ∞) + combined weights/KV
cache fits budget. Warning band 5-15× ratio is the sweet spot;
outside that we proceed with a stderr warning. Refusal cases produce
ErrUserError-shaped Reason strings ready for serve.go to surface.

Shared by serve.go (--draft flag validation, Task 10) and fit.go
(--speculative discovery, Task 14).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: `internal/launchd/ports.go` — `HasDraft` helper

**Spec:** §3.5 (`HasDraft` mirror of `HasAPIKey`).

**Files:**
- Modify: `internal/launchd/ports.go`
- Test: `internal/launchd/ports_test.go`

One-line plist scanner returning the embedded `--model-draft` path (if any).

- [ ] **Step 1: Write the failing test**

In `internal/launchd/ports_test.go`, append:

```go
func TestHasDraftFindsEmbeddedPath(t *testing.T) {
    dir := t.TempDir()
    label := "com.llamactl.qwen2.5-32b-instruct"
    plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/llama-server</string>
    <string>--model</string>
    <string>/Users/greg/.local/share/llama-models/qwen2.5-32b-instruct/Q4_K_M.gguf</string>
    <string>--model-draft</string>
    <string>/Users/greg/.local/share/llama-models/qwen2.5-3b-instruct/Q4_K_M.gguf</string>
    <string>--ctx-size-draft</string>
    <string>8192</string>
  </array>
</dict>
</plist>`
    if err := os.WriteFile(filepath.Join(dir, label+".plist"), []byte(plist), 0o644); err != nil {
        t.Fatal(err)
    }
    path, ok := launchd.HasDraft(dir, label)
    if !ok {
        t.Fatalf("expected HasDraft to return ok=true")
    }
    want := "/Users/greg/.local/share/llama-models/qwen2.5-3b-instruct/Q4_K_M.gguf"
    if path != want {
        t.Errorf("HasDraft path = %q, want %q", path, want)
    }
}

func TestHasDraftAbsent(t *testing.T) {
    dir := t.TempDir()
    label := "com.llamactl.no-draft"
    plist := `<plist><dict><key>ProgramArguments</key><array>
    <string>/usr/local/bin/llama-server</string>
    <string>--model</string>
    <string>/path/main.gguf</string>
    </array></dict></plist>`
    if err := os.WriteFile(filepath.Join(dir, label+".plist"), []byte(plist), 0o644); err != nil {
        t.Fatal(err)
    }
    path, ok := launchd.HasDraft(dir, label)
    if ok || path != "" {
        t.Errorf("HasDraft = (%q, %v), want (\"\", false)", path, ok)
    }
}

func TestHasDraftMissingPlist(t *testing.T) {
    path, ok := launchd.HasDraft(t.TempDir(), "com.llamactl.does-not-exist")
    if ok || path != "" {
        t.Errorf("HasDraft on missing plist = (%q, %v), want (\"\", false)", path, ok)
    }
}
```

Run: `go test ./internal/launchd/... -run TestHasDraft -v` → expect FAIL (`HasDraft` undefined).

- [ ] **Step 2: Implement `HasDraft`**

In `internal/launchd/ports.go`, append after `HasPublicBind`:

```go
// HasDraft returns the path embedded in the plist's --model-draft arg, if
// any. Returns ("", false) when the plist is missing, the flag is absent,
// or the value <string> can't be parsed. Mirrors the scanning pattern of
// HasAPIKey / HasPublicBind.
func HasDraft(dir, label string) (string, bool) {
    data, err := os.ReadFile(filepath.Join(dir, label+".plist"))
    if err != nil {
        return "", false
    }
    s := string(data)
    idx := strings.Index(s, "<string>--model-draft</string>")
    if idx < 0 {
        return "", false
    }
    rest := s[idx+len("<string>--model-draft</string>"):]
    open := strings.Index(rest, "<string>")
    if open < 0 {
        return "", false
    }
    rest = rest[open+len("<string>"):]
    closeIdx := strings.Index(rest, "</string>")
    if closeIdx < 0 {
        return "", false
    }
    return strings.TrimSpace(rest[:closeIdx]), true
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/launchd/... -run TestHasDraft -v
```

Expected: PASS.

- [ ] **Step 4: Verify + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/launchd/ports.go internal/launchd/ports_test.go
git commit -m "$(cat <<'EOF'
feat(launchd): HasDraft plist scanner mirroring HasAPIKey

Returns the path embedded in --model-draft (if any) for a given service
label. One-line scan matching HasAPIKey / HasPublicBind precedent. Used
in serve_test for detached-mode coverage; foundation for a future doctor
check on draft availability (not specced in 6b).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: `internal/cli/serve.go` — `--draft` flag wiring + validation

**Spec:** §3.2.

**Files:**
- Modify: `internal/cli/serve.go`

This is the first behavior-bearing serve.go change. We add the cobra flag, plumb it through `runServe`, and call `SpeculativePair` for validation. No argv append yet — that's Task 11. Splitting keeps the diffs small.

- [ ] **Step 1: Add the cobra flag**

In `internal/cli/serve.go` `newServeCmd`:

```go
func newServeCmd(d *Deps) *cobra.Command {
    var port int
    var recipe string
    var detach bool
    var draftID string // NEW
    cmd := &cobra.Command{
        Use:   "serve <model-id>",
        Short: "Start llama-server for an installed model",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            return runServe(cmd.Context(), d, args[0], port, recipe, detach, draftID)
        },
    }
    cmd.Flags().IntVar(&port, "port", 8080, "TCP port for the OpenAI-compatible endpoint")
    cmd.Flags().StringVar(&recipe, "recipe", recipes.DefaultRecipe, "chat | code | long-context | low-memory")
    cmd.Flags().BoolVar(&detach, "detach", false, "register a launchd LaunchAgent and return")
    cmd.Flags().StringVar(&draftID, "draft", "", "draft model id for speculative decoding (must be installed)")
    return cmd
}
```

Update `runServe`'s signature to accept `draftID`:

```go
func runServe(ctx context.Context, d *Deps, id string, requestedPort int, recipeName string, detach bool, draftID string) error {
    // ... existing body ...
}
```

- [ ] **Step 2: Add the draft resolution + validation after the main model is resolved**

In `runServe`, after the existing recipe lookup block (after line ~81) and before `model := models.Model{...}`, add:

```go
// Resolve draft model if --draft was passed.
var draftMeta models.Metadata
var draftModel models.Model
hasDraft := draftID != ""
if hasDraft {
    var err error
    draftMeta, err = d.ModelStore.Get(ctx, draftID)
    if err != nil {
        return fmt.Errorf("%w: draft model %q is not installed; run `llamactl add %s` first",
            ErrUserError, draftID, draftID)
    }
    draftModel = models.Model{
        ID: draftMeta.ID, HFRepo: draftMeta.Repo, Arch: draftMeta.Arch,
        ParamsB: draftMeta.ParamsB, MaxCtx: lookupMaxCtx(draftMeta),
    }
}
```

After the existing `model := models.Model{...}` block (around line 86), validate the pair:

```go
if hasDraft {
    verdict := models.SpeculativePair(model, draftModel, hw, recipeName)
    if !verdict.Ok {
        return fmt.Errorf("%w: %s", ErrUserError, verdict.Reason)
    }
    if verdict.Reason != "" {
        // Warning (Ok=true but Reason non-empty): print to stderr, continue.
        fmt.Fprintf(d.Stderr, "llamactl: warning: %s\n", verdict.Reason)
    }
}
```

- [ ] **Step 3: Verify the build still passes**

```bash
go build ./...
```

Expected: clean build (no callers besides newServeCmd → runServe; main.go invokes via cobra).

- [ ] **Step 4: Verify by stubbing a test**

We need an existing `serve_test.go` that builds `runServe` calls — they're now broken because of the new param. Find and update them:

```bash
grep -n "runServe(" internal/cli/serve_test.go
```

Each call site: add `""` as the trailing `draftID` argument. Example:
```go
err := runServe(ctx, deps, "qwen2.5-3b", 8082, "chat", false)
// becomes
err := runServe(ctx, deps, "qwen2.5-3b", 8082, "chat", false, "")
```

Don't add the new test cases yet — that's Task 12.

- [ ] **Step 5: Run existing tests to confirm no regression**

```bash
go test ./internal/cli/... -race
```

Expected: PASS.

- [ ] **Step 6: Verify + commit**

```bash
gofmt -l . && go vet ./...
git add internal/cli/serve.go internal/cli/serve_test.go
git commit -m "$(cat <<'EOF'
feat(serve): wire --draft flag with SpeculativePair validation

Adds --draft <id> cobra flag. When set, resolves the draft model from
ModelStore (ErrUserError on not-found), then calls SpeculativePair to
validate arch match + ratio + combined-RAM budget. Refusal → ErrUserError
with the verdict's Reason. Warning band (ratio outside 5-15×) prints to
stderr but proceeds.

No argv append yet — Task 11 ships --model-draft / --ctx-size-draft
through to llama-server.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: `internal/cli/serve.go` — argv append + log line

**Spec:** §3.2 (post-recipe append).

**Files:**
- Modify: `internal/cli/serve.go`

Wire the actual `--model-draft <path>` and `--ctx-size-draft <N>` arguments into the argv slice. Match v1.3.0's `--api-key` append pattern (around line 130).

- [ ] **Step 1: Append the draft flags after the recipes.FlagsFor call**

In `runServe`, after the existing `argv := recipes.FlagsFor(...)` block and the api-key append, add:

```go
// Append --model-draft / --ctx-size-draft when --draft was passed and
// validated. Mirrors the --api-key append pattern from v1.3.0.
if hasDraft {
    draftCtx := lookupMaxCtx(draftMeta)
    if draftCtx == 0 || draftCtx > recipe.ContextLength(model) {
        draftCtx = recipe.ContextLength(model)
    }
    argv = append(argv, "--model-draft", draftMeta.GGUFPath)
    argv = append(argv, "--ctx-size-draft", fmt.Sprintf("%d", draftCtx))
}
```

The `recipe.ContextLength(model)` call mirrors how recipes already compute the main model's ctx-size. Confirm the actual API of `recipes.Recipe`:

```bash
grep -n "func.*ContextLength\|^type Recipe\|MaxCtx" internal/recipes/recipes.go
```

If there's no `ContextLength` method, fall back to using `model.MaxCtx` directly (which is what `recipes.FlagsFor` ultimately uses internally). Adjust the line:

```go
mainCtx := model.MaxCtx
if mainCtx == 0 {
    mainCtx = 8192 // recipe default
}
draftCtx := draftModel.MaxCtx
if draftCtx == 0 || draftCtx > mainCtx {
    draftCtx = mainCtx
}
```

- [ ] **Step 2: Add the log line just before the launch branch**

Find the existing `meta.LastServedAt = now()` block. Just before that, add:

```go
if hasDraft {
    fmt.Fprintf(d.Stdout, "speculative decoding enabled (draft=%s, ratio=%.1f×)\n",
        draftMeta.ID, verdict.SizeRatio)
}
```

Hmm — `verdict` is local to the validation block in Task 10. Move it to outer scope: change the validation block to declare `var verdict models.PairVerdict` at the runServe top, then assign inside the `if hasDraft` block. Adjust Task 10's code:

```go
// At top of runServe, after the existing var declarations:
var verdict models.PairVerdict

// ... inside if hasDraft branch:
verdict = models.SpeculativePair(model, draftModel, hw, recipeName)
```

Now the log line in Task 11 can reference `verdict.SizeRatio`.

- [ ] **Step 3: Verify build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/cli/... -race
```

Expected: existing tests still PASS (the new behavior is gated on `--draft`, which no current test exercises; Task 12 adds those tests).

- [ ] **Step 5: Verify + commit**

```bash
gofmt -l . && go vet ./...
git add internal/cli/serve.go
git commit -m "$(cat <<'EOF'
feat(serve): append --model-draft / --ctx-size-draft to llama-server argv

Post-recipe append mirroring v1.3.0's --api-key pattern. Draft ctx-size
is min(main_ctx, draft.MaxCtx) so the draft never exceeds its own
training context. Foreground log line prints
"speculative decoding enabled (draft=<id>, ratio=N.N×)" before launch.
Detached path inherits the argv via PlistSpec.Args (no template changes).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: `internal/cli/serve_test.go` — six new tests

**Spec:** §3.6 (`internal/cli/serve_test.go`).

**Files:**
- Modify: `internal/cli/serve_test.go`

Six new tests cover the happy path, every refusal branch, the warning branch, and the detached plist embedding. Reuse the existing test scaffolding (fake ModelStore, fake LaunchdService).

- [ ] **Step 1: Identify the test scaffolding pattern**

```bash
grep -n "func Test\|fakeModelStore\|minimalServeDeps\|fakeLaunchd" internal/cli/serve_test.go | head -20
```

Find the helper that builds a Deps with all the fakes wired. The existing tests use it; reuse it.

- [ ] **Step 2: Write the six tests**

In `internal/cli/serve_test.go`, append:

```go
// --- Phase 6b: --draft flag tests ---

func TestServeWithDraftAppendsModelDraftFlag(t *testing.T) {
    deps := newServeTestDeps(t)
    // Install both main and draft in the fake ModelStore.
    deps.ModelStore.(*fakeModelStore).put(models.Metadata{
        ID: "qwen2.5-7b-instruct", Arch: models.ArchQwen25, ParamsB: 7,
        GGUFPath: "/fake/main.gguf", Quant: models.Q4_K_M,
    })
    deps.ModelStore.(*fakeModelStore).put(models.Metadata{
        ID: "qwen2.5-0.5b-instruct", Arch: models.ArchQwen25, ParamsB: 0.5,
        GGUFPath: "/fake/draft.gguf", Quant: models.Q4_K_M,
    })

    // Run detached so we capture the plist; assert argv via plist contents.
    err := runServe(context.Background(), deps, "qwen2.5-7b-instruct",
        8082, "chat", true, "qwen2.5-0.5b-instruct")
    if err != nil {
        t.Fatalf("runServe: %v", err)
    }

    plist := readGeneratedPlist(t, deps, "com.llamactl.qwen2.5-7b-instruct")
    if !strings.Contains(plist, "<string>--model-draft</string>") {
        t.Errorf("plist missing --model-draft arg:\n%s", plist)
    }
    if !strings.Contains(plist, "<string>/fake/draft.gguf</string>") {
        t.Errorf("plist missing draft path:\n%s", plist)
    }
    if !strings.Contains(plist, "<string>--ctx-size-draft</string>") {
        t.Errorf("plist missing --ctx-size-draft arg:\n%s", plist)
    }
}

func TestServeDraftNotInstalled(t *testing.T) {
    deps := newServeTestDeps(t)
    deps.ModelStore.(*fakeModelStore).put(models.Metadata{
        ID: "qwen2.5-7b-instruct", Arch: models.ArchQwen25, ParamsB: 7,
        GGUFPath: "/fake/main.gguf", Quant: models.Q4_K_M,
    })
    // Note: NOT installing the draft.

    err := runServe(context.Background(), deps, "qwen2.5-7b-instruct",
        8082, "chat", true, "missing-draft-id")
    if err == nil || !errors.Is(err, ErrUserError) {
        t.Fatalf("expected ErrUserError; got %v", err)
    }
    if !strings.Contains(err.Error(), "missing-draft-id") {
        t.Errorf("error should name missing draft id; got %v", err)
    }
    if !strings.Contains(err.Error(), "llamactl add") {
        t.Errorf("error should suggest `llamactl add`; got %v", err)
    }
}

func TestServeDraftArchMismatch(t *testing.T) {
    deps := newServeTestDeps(t)
    deps.ModelStore.(*fakeModelStore).put(models.Metadata{
        ID: "qwen2.5-7b-instruct", Arch: models.ArchQwen25, ParamsB: 7,
        GGUFPath: "/fake/main.gguf", Quant: models.Q4_K_M,
    })
    deps.ModelStore.(*fakeModelStore).put(models.Metadata{
        ID: "llama-3-1b-instruct", Arch: models.ArchLlama3, ParamsB: 1,
        GGUFPath: "/fake/draft.gguf", Quant: models.Q4_K_M,
    })

    err := runServe(context.Background(), deps, "qwen2.5-7b-instruct",
        8082, "chat", true, "llama-3-1b-instruct")
    if err == nil || !errors.Is(err, ErrUserError) {
        t.Fatalf("expected ErrUserError; got %v", err)
    }
    if !strings.Contains(err.Error(), "arch") {
        t.Errorf("error should mention arch mismatch; got %v", err)
    }
}

func TestServeDraftCombinedRAMTooBig(t *testing.T) {
    deps := newServeTestDeps(t)
    // Tiny host: 8 GB total.
    deps.HardwareDetector = &fakeHardwareDetector{info: hardware.Info{TotalMemoryGB: 8}}

    deps.ModelStore.(*fakeModelStore).put(models.Metadata{
        ID: "qwen2.5-32b-instruct", Arch: models.ArchQwen25, ParamsB: 32,
        GGUFPath: "/fake/main.gguf", Quant: models.Q4_K_M,
    })
    deps.ModelStore.(*fakeModelStore).put(models.Metadata{
        ID: "qwen2.5-3b-instruct", Arch: models.ArchQwen25, ParamsB: 3,
        GGUFPath: "/fake/draft.gguf", Quant: models.Q4_K_M,
    })

    err := runServe(context.Background(), deps, "qwen2.5-32b-instruct",
        8082, "chat", true, "qwen2.5-3b-instruct")
    if err == nil || !errors.Is(err, ErrUserError) {
        t.Fatalf("expected ErrUserError on RAM exhaustion; got %v", err)
    }
    if !strings.Contains(err.Error(), "RAM") {
        t.Errorf("error should mention RAM; got %v", err)
    }
}

func TestServeDraftWarnsOnRatioOutsideRange(t *testing.T) {
    deps := newServeTestDeps(t)
    var stderr bytes.Buffer
    deps.Stderr = &stderr

    deps.ModelStore.(*fakeModelStore).put(models.Metadata{
        ID: "qwen2.5-32b-instruct", Arch: models.ArchQwen25, ParamsB: 32,
        GGUFPath: "/fake/main.gguf", Quant: models.Q4_K_M,
    })
    deps.ModelStore.(*fakeModelStore).put(models.Metadata{
        ID: "qwen2.5-0.5b-instruct", Arch: models.ArchQwen25, ParamsB: 0.5,
        GGUFPath: "/fake/draft.gguf", Quant: models.Q4_K_M,
    })

    err := runServe(context.Background(), deps, "qwen2.5-32b-instruct",
        8082, "chat", true, "qwen2.5-0.5b-instruct")
    if err != nil {
        t.Fatalf("expected no error (warning only); got %v", err)
    }
    if !strings.Contains(stderr.String(), "warning") {
        t.Errorf("expected stderr warning at ratio 64×; got: %s", stderr.String())
    }
}

func TestServeDetachedDraftEmbedsInPlist(t *testing.T) {
    deps := newServeTestDeps(t)
    deps.ModelStore.(*fakeModelStore).put(models.Metadata{
        ID: "qwen2.5-7b-instruct", Arch: models.ArchQwen25, ParamsB: 7,
        GGUFPath: "/fake/main.gguf", Quant: models.Q4_K_M,
    })
    deps.ModelStore.(*fakeModelStore).put(models.Metadata{
        ID: "qwen2.5-0.5b-instruct", Arch: models.ArchQwen25, ParamsB: 0.5,
        GGUFPath: "/fake/draft.gguf", Quant: models.Q4_K_M,
    })

    err := runServe(context.Background(), deps, "qwen2.5-7b-instruct",
        8082, "chat", true, "qwen2.5-0.5b-instruct")
    if err != nil {
        t.Fatal(err)
    }

    path, ok := launchd.HasDraft(deps.LaunchAgentsDir, "com.llamactl.qwen2.5-7b-instruct")
    if !ok {
        t.Fatalf("HasDraft returned ok=false for serve --draft")
    }
    if path != "/fake/draft.gguf" {
        t.Errorf("HasDraft path=%q, want /fake/draft.gguf", path)
    }
}
```

`newServeTestDeps`, `fakeModelStore.put`, `readGeneratedPlist`, `fakeHardwareDetector` — these helpers exist in the test file or in `fakes_test.go`. Find their existing definitions and reuse. If a needed shape doesn't exist, add it minimally in `fakes_test.go` (e.g., extend `fakeModelStore` with a `put` method that backs `Get`).

Run: `go test ./internal/cli/... -run "TestServeWith|TestServeDraft|TestServeDetachedDraft" -v` → expect all PASS.

- [ ] **Step 3: If a needed helper is missing, add it**

E.g., if `newServeTestDeps` doesn't exist, build one matching the existing patterns. Don't refactor existing tests — just add the helper.

- [ ] **Step 4: Verify + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/serve_test.go internal/cli/fakes_test.go
git commit -m "$(cat <<'EOF'
test(serve): six tests covering --draft happy path + refusals + warnings

- TestServeWithDraftAppendsModelDraftFlag — argv contains --model-draft
- TestServeDraftNotInstalled — ErrUserError, suggests llamactl add
- TestServeDraftArchMismatch — ErrUserError, names mismatch
- TestServeDraftCombinedRAMTooBig — ErrUserError, names RAM shortfall
- TestServeDraftWarnsOnRatioOutsideRange — stderr warning, continues
- TestServeDetachedDraftEmbedsInPlist — HasDraft round-trips

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: `internal/cli/fit.go` — `--speculative` flag + arg branch

**Spec:** §3.3 (fit speculative mode).

**Files:**
- Modify: `internal/cli/fit.go`

When `--speculative` is set, the positional arg becomes the MAIN model id (must be installed), HF search is skipped, and candidates come from `ModelStore.List`.

- [ ] **Step 1: Add the flag + adapt the cobra wiring**

In `newFitCmd`:

```go
func newFitCmd(d *Deps) *cobra.Command {
    var install bool
    var ctxSize int
    var limit int
    var asJSON bool
    var speculative bool // NEW
    cmd := &cobra.Command{
        Use:   "fit <query...>",
        Short: "Search HF and rank GGUF variants by fit on this host",
        Args:  cobra.MinimumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            if speculative {
                return runFitSpeculative(cmd.Context(), d, strings.Join(args, " "), limit)
            }
            return runFit(cmd.Context(), d, strings.Join(args, " "), install, ctxSize, limit, asJSON)
        },
    }
    cmd.Flags().BoolVar(&install, "install", false, "install the top-ranked OK row")
    cmd.Flags().IntVar(&ctxSize, "ctx", fitDefaultCtx, "context size for KV-cache estimation")
    cmd.Flags().IntVar(&limit, "limit", 10, "cap rows shown")
    cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
    cmd.Flags().BoolVar(&speculative, "speculative", false, "list installed draft candidates for the named main model")
    return cmd
}
```

- [ ] **Step 2: Stub `runFitSpeculative`**

Add at the bottom of `internal/cli/fit.go`:

```go
// runFitSpeculative is the --speculative branch of `llamactl fit`. The
// positional arg is the MAIN model id; candidates come from ModelStore.List.
// Implementation lands in Task 14; this stub exists so Task 13's flag wiring
// compiles in isolation.
func runFitSpeculative(ctx context.Context, d *Deps, mainID string, limit int) error {
    return fmt.Errorf("fit --speculative: not yet implemented")
}
```

- [ ] **Step 3: Verify the build**

```bash
go build ./...
go test ./internal/cli/... -race
```

Expected: clean build, existing tests PASS (the stub returns an error, but no existing test exercises `--speculative`).

- [ ] **Step 4: Verify + commit**

```bash
gofmt -l . && go vet ./...
git add internal/cli/fit.go
git commit -m "$(cat <<'EOF'
feat(fit): add --speculative flag + arg branch (stub)

Cobra wiring for `llamactl fit --speculative <main_id>`. The positional
arg is now interpreted as the main model id when --speculative is set;
HF search is skipped. Implementation lands in Task 14.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: `internal/cli/fit.go` — candidate enumeration

**Spec:** §3.3.

**Files:**
- Modify: `internal/cli/fit.go`

Resolve the main model from ModelStore (ErrUserError on not-found), iterate the installed list, drop arch-mismatches, call SpeculativePair per candidate.

- [ ] **Step 1: Replace the stub with the candidate loop**

In `internal/cli/fit.go`, replace `runFitSpeculative`:

```go
// specRow is a fit row in speculative mode. Reuses Verdict semantics from
// fit's main mode but the column meanings differ.
type specRow struct {
    DraftID       string  `json:"draft_id"`
    Arch          string  `json:"arch"`
    ParamsB       float64 `json:"params_b"`
    SizeRatio     float64 `json:"size_ratio"`
    CombinedRAMGB float64 `json:"combined_ram_gb"`
    Verdict       string  `json:"verdict"`        // "ok", "ratio-low", "ratio-high"
    Reason        string  `json:"reason,omitempty"`
}

func runFitSpeculative(ctx context.Context, d *Deps, mainID string, limit int) error {
    mainMeta, err := d.ModelStore.Get(ctx, mainID)
    if err != nil {
        return fmt.Errorf("%w: main model %q is not installed; run `llamactl add %s` first",
            ErrUserError, mainID, mainID)
    }
    hw, err := ensureHardware(ctx, d)
    if err != nil {
        return err
    }

    mainModel := models.Model{
        ID: mainMeta.ID, HFRepo: mainMeta.Repo, Arch: mainMeta.Arch,
        ParamsB: mainMeta.ParamsB, MaxCtx: lookupMaxCtx(mainMeta),
    }

    all, err := d.ModelStore.List(ctx)
    if err != nil {
        return fmt.Errorf("list installed models: %w", err)
    }

    var rows []specRow
    for _, candidate := range all {
        if candidate.ID == mainMeta.ID {
            continue // can't draft yourself
        }
        draftModel := models.Model{
            ID: candidate.ID, HFRepo: candidate.Repo, Arch: candidate.Arch,
            ParamsB: candidate.ParamsB, MaxCtx: lookupMaxCtx(candidate),
        }
        verdict := models.SpeculativePair(mainModel, draftModel, hw, "chat")
        if !verdict.ArchMatch {
            continue // omit arch-mismatches entirely (noise, not a candidate)
        }
        // Map verdict to a short verdict string.
        v := "ok"
        if !verdict.Ok {
            v = "refused"
        } else if verdict.SizeRatio < 5.0 {
            v = "ratio-low"
        } else if verdict.SizeRatio > 15.0 {
            v = "ratio-high"
        }
        rows = append(rows, specRow{
            DraftID:       candidate.ID,
            Arch:          string(candidate.Arch),
            ParamsB:       candidate.ParamsB,
            SizeRatio:     verdict.SizeRatio,
            CombinedRAMGB: verdict.CombinedRAMGB,
            Verdict:       v,
            Reason:        verdict.Reason,
        })
    }

    if len(rows) == 0 {
        fmt.Fprintf(d.Stdout,
            "no installed draft candidates for %s; run `llamactl fit %s` to find smaller variants of the same family\n",
            mainID, mainModel.Arch)
        return nil
    }

    // Sort + render: lands in Task 15.
    if limit > 0 && len(rows) > limit {
        rows = rows[:limit]
    }
    for _, r := range rows {
        fmt.Fprintf(d.Stdout, "%-40s %-8s %.1f B  ratio=%.1fx  RAM=%.1fGB  verdict=%s  %s\n",
            r.DraftID, r.Arch, r.ParamsB, r.SizeRatio, r.CombinedRAMGB, r.Verdict, r.Reason)
    }
    return nil
}
```

The bare `Fprintf` rendering is a placeholder so Task 14 produces visible output. Task 15 replaces it with the proper tabwriter table + sort.

- [ ] **Step 2: Verify the build**

```bash
go build ./...
go test ./internal/cli/... -race
```

Expected: clean.

- [ ] **Step 3: Verify + commit**

```bash
gofmt -l . && go vet ./...
git add internal/cli/fit.go
git commit -m "$(cat <<'EOF'
feat(fit): --speculative candidate enumeration via SpeculativePair

Resolves the main model from ModelStore (ErrUserError on not-found),
iterates ModelStore.List, drops arch-mismatches, calls SpeculativePair
per remaining candidate. Empty-candidates branch prints a helpful
"run `llamactl fit <arch>`" message and exits 0.

Output rendering is a placeholder; Task 15 adds the tabwriter table +
sort.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 15: `internal/cli/fit.go` — sort + tabwriter + footer

**Spec:** §3.3 (sort + columns + caveat footer).

**Files:**
- Modify: `internal/cli/fit.go`

Replace the placeholder Fprintf loop with a proper tabwriter, distance-from-midpoint sort, and footer caveat.

- [ ] **Step 1: Add the sort and tabwriter**

Replace the rendering portion of `runFitSpeculative` (the final loop):

```go
    // Sort: Ok rows first (sorted by |SizeRatio - 7.5| ascending — closest
    // to the ideal 5-15× midpoint rises first), then !Ok rows by Reason.
    sort.SliceStable(rows, func(i, j int) bool {
        ai := rows[i].Verdict == "refused"
        aj := rows[j].Verdict == "refused"
        if ai != aj {
            return !ai
        }
        if !ai {
            di := math.Abs(rows[i].SizeRatio - 7.5)
            dj := math.Abs(rows[j].SizeRatio - 7.5)
            return di < dj
        }
        return rows[i].Reason < rows[j].Reason
    })

    if limit > 0 && len(rows) > limit {
        rows = rows[:limit]
    }

    // Render via tabwriter.
    fmt.Fprintf(d.Stdout, "Draft candidates for %s (%g B, %s):\n\n",
        mainID, mainMeta.ParamsB, mainMeta.Arch)
    tw := tabwriter.NewWriter(d.Stdout, 0, 0, 2, ' ', 0)
    fmt.Fprintln(tw, "DRAFT ID\tARCH\tPARAMSB\tRATIO\tCOMBINED RAM\tVERDICT")
    for _, r := range rows {
        symbol := "✓ ok"
        if r.Verdict == "ratio-low" {
            symbol = "⚠ ratio-low"
        } else if r.Verdict == "ratio-high" {
            symbol = "⚠ ratio-high"
        } else if r.Verdict == "refused" {
            symbol = "✗ refused"
        }
        fmt.Fprintf(tw, "%s\t%s\t%g B\t%.1f×\t%.1f GB\t%s\n",
            r.DraftID, r.Arch, r.ParamsB, r.SizeRatio, r.CombinedRAMGB, symbol)
    }
    if err := tw.Flush(); err != nil {
        return fmt.Errorf("flush tabwriter: %w", err)
    }
    fmt.Fprintln(d.Stdout)
    fmt.Fprintln(d.Stdout, "Note: speculative decoding speedup depends on workload; ratio is a heuristic only.")
    return nil
```

Add `"math"` to the import block.

- [ ] **Step 2: Verify the build + run an existing fit_test**

```bash
go build ./...
go test ./internal/cli/... -run TestFit -race
```

Expected: clean. (No tests for `--speculative` yet; Task 16 adds them.)

- [ ] **Step 3: Verify + commit**

```bash
gofmt -l . && go vet ./...
git add internal/cli/fit.go
git commit -m "$(cat <<'EOF'
feat(fit): --speculative tabwriter rendering + distance-midpoint sort

Sort puts Ok rows first (closest to ratio=7.5 midpoint rises), refused
rows last by Reason. Tabwriter columns: DRAFT ID, ARCH, PARAMSB, RATIO,
COMBINED RAM, VERDICT. Footer notes the heuristic caveat.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 16: `internal/cli/fit_test.go` — four new tests

**Spec:** §3.6 (`internal/cli/fit_test.go`).

**Files:**
- Modify: `internal/cli/fit_test.go`

- [ ] **Step 1: Find the existing fit test scaffolding**

```bash
grep -n "func TestFit\|fakeHFClient\|newFitTestDeps" internal/cli/fit_test.go
```

If `newFitTestDeps` doesn't exist, build one inline in each test (following the existing pattern of constructing `Deps` field-by-field).

- [ ] **Step 2: Write the four tests**

In `internal/cli/fit_test.go`, append:

```go
// --- Phase 6b: --speculative tests ---

func TestFitSpeculativeListsInstalledDrafts(t *testing.T) {
    deps := newFitTestDeps(t)
    store := deps.ModelStore.(*fakeModelStore)
    // Main + three candidates: one arch-mismatch (dropped), two arch-match.
    store.put(models.Metadata{ID: "qwen2.5-7b-instruct", Arch: models.ArchQwen25, ParamsB: 7})
    store.put(models.Metadata{ID: "qwen2.5-0.5b-instruct", Arch: models.ArchQwen25, ParamsB: 0.5}) // ratio 14×
    store.put(models.Metadata{ID: "qwen2.5-1.5b-instruct", Arch: models.ArchQwen25, ParamsB: 1.5}) // ratio ~4.7×
    store.put(models.Metadata{ID: "llama-3-1b-instruct", Arch: models.ArchLlama3, ParamsB: 1})     // dropped

    var out bytes.Buffer
    deps.Stdout = &out
    err := runFitSpeculative(context.Background(), deps, "qwen2.5-7b-instruct", 10)
    if err != nil {
        t.Fatal(err)
    }
    s := out.String()
    if !strings.Contains(s, "qwen2.5-0.5b-instruct") || !strings.Contains(s, "qwen2.5-1.5b-instruct") {
        t.Errorf("expected both qwen2 drafts; output:\n%s", s)
    }
    if strings.Contains(s, "llama-3-1b-instruct") {
        t.Errorf("llama-arch draft should be dropped; output:\n%s", s)
    }
}

func TestFitSpeculativeMainNotInstalled(t *testing.T) {
    deps := newFitTestDeps(t)
    err := runFitSpeculative(context.Background(), deps, "nonexistent", 10)
    if err == nil || !errors.Is(err, ErrUserError) {
        t.Errorf("expected ErrUserError; got %v", err)
    }
}

func TestFitSpeculativeEmptyCandidates(t *testing.T) {
    deps := newFitTestDeps(t)
    store := deps.ModelStore.(*fakeModelStore)
    store.put(models.Metadata{ID: "qwen2.5-7b-instruct", Arch: models.ArchQwen25, ParamsB: 7})

    var out bytes.Buffer
    deps.Stdout = &out
    err := runFitSpeculative(context.Background(), deps, "qwen2.5-7b-instruct", 10)
    if err != nil {
        t.Fatalf("expected no error on empty candidates; got %v", err)
    }
    if !strings.Contains(out.String(), "no installed draft candidates") {
        t.Errorf("expected empty-candidates message; got:\n%s", out.String())
    }
}

func TestFitSpeculativeRatioOrder(t *testing.T) {
    deps := newFitTestDeps(t)
    store := deps.ModelStore.(*fakeModelStore)
    // Main + three candidates whose ratios bracket the 7.5 midpoint.
    store.put(models.Metadata{ID: "qwen2.5-32b-instruct", Arch: models.ArchQwen25, ParamsB: 32})
    store.put(models.Metadata{ID: "draft-a", Arch: models.ArchQwen25, ParamsB: 4})    // ratio 8 → closest
    store.put(models.Metadata{ID: "draft-b", Arch: models.ArchQwen25, ParamsB: 2.67}) // ratio 12
    store.put(models.Metadata{ID: "draft-c", Arch: models.ArchQwen25, ParamsB: 8})    // ratio 4

    var out bytes.Buffer
    deps.Stdout = &out
    err := runFitSpeculative(context.Background(), deps, "qwen2.5-32b-instruct", 10)
    if err != nil {
        t.Fatal(err)
    }

    s := out.String()
    aIdx := strings.Index(s, "draft-a")
    bIdx := strings.Index(s, "draft-b")
    cIdx := strings.Index(s, "draft-c")
    // Expected order: a (|8-7.5|=0.5) first, then c (|4-7.5|=3.5), then b (|12-7.5|=4.5).
    if !(aIdx < cIdx && cIdx < bIdx) {
        t.Errorf("rows out of order: a=%d, b=%d, c=%d (want a < c < b)\n%s", aIdx, bIdx, cIdx, s)
    }
}
```

`fakeModelStore.put` was added in Task 12; reuse.

Run: `go test ./internal/cli/... -run TestFitSpeculative -v` → expect all PASS.

- [ ] **Step 3: Verify + commit**

```bash
go test ./... -race && gofmt -l . && go vet ./...
git add internal/cli/fit_test.go
git commit -m "$(cat <<'EOF'
test(fit): four tests for --speculative discovery surface

- TestFitSpeculativeListsInstalledDrafts — arch-match yes, mismatch dropped
- TestFitSpeculativeMainNotInstalled — ErrUserError
- TestFitSpeculativeEmptyCandidates — helpful empty-state message; exit 0
- TestFitSpeculativeRatioOrder — distance-from-7.5-midpoint ordering

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 17: README + PRD documentation

**Spec:** §6 acceptance criteria reference.

**Files:**
- Modify: `README.md`
- Modify: `docs/llamactl-prd-v1.5.md`

- [ ] **Step 1: Add a "Speculative decoding" section to README**

Find a logical home — after "Using the endpoint" and before "Tips". Insert:

```markdown
## Speculative decoding

llamactl can wire llama-server's `--model-draft` flag for assisted decoding, where a smaller "draft" model proposes tokens that the main model verifies in parallel. Typical speedup: 1.5-3× depending on workload and draft quality.

### Discovery

```bash
llamactl fit --speculative <main-model>
```

Lists installed models that share the main's architecture, sorted by closest fit to the ideal 5-15× size ratio. Example:

```
Draft candidates for qwen2.5-32b-instruct (32 B, qwen2.5):

DRAFT ID                ARCH     PARAMSB  RATIO   COMBINED RAM   VERDICT
qwen2.5-3b-instruct     qwen2.5  3 B      10.7×   24.1 GB        ✓ ok
qwen2.5-0.5b-instruct   qwen2.5  0.5 B    64.0×   22.3 GB        ⚠ ratio-high
```

### Serving with a draft

```bash
llamactl serve qwen2.5-32b-instruct --draft qwen2.5-3b-instruct
```

The draft must be already installed. Mismatched architectures are refused (tokenizers diverge across families). Size ratios outside 5-15× warn but proceed.

Detached serves (`--detach`) embed `--model-draft` and `--ctx-size-draft` in the LaunchAgent plist, so the pairing persists across reboots. Re-running `serve --detach` without `--draft` clears the pairing.

### Limitations

- Both models must be installed via `add`; llamactl does not auto-download a draft.
- The draft's context window is capped at `min(main_ctx, draft.MaxCtx)` — exceeding the draft's training context degrades alignment.
- Speedup is workload-dependent. Batch size, temperature, and prompt structure all matter. The ratio heuristic is informational only.
```

- [ ] **Step 2: Mention the GGUF parser fallback in the existing "Adding models" or "Troubleshooting" section**

Find where `list` is documented; append:

```markdown
> **Note:** When `list` shows `?` for ParamsB, the model's GGUF lacks both `general.parameter_count` and `general.size_label`. Phase 6b's tensor-shape fallback handles most cases for llama / qwen2 / qwen3 / gemma3 / mistral architectures automatically on next `list` invocation. Unknown architectures still display `?`.
```

- [ ] **Step 3: Update the PRD**

Open `docs/llamactl-prd-v1.5.md`. Find the §Out of scope section (around line 398 — speculative decoding was listed as a post-v1 candidate). Move "speculative decoding" out of "post-v1" and into shipped features. Update the version table (or equivalent) noting v1.4.0.

Find the §CLI section. Add a line documenting the new `--draft` flag and `fit --speculative` mode.

- [ ] **Step 4: Verify markdown lint (manual)**

```bash
git diff README.md | head -80
git diff docs/llamactl-prd-v1.5.md | head -40
```

Confirm no broken table formatting, headings have one blank line above + below, code fences close.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/llamactl-prd-v1.5.md
git commit -m "$(cat <<'EOF'
docs(phase6b): README + PRD updates for --draft and fit --speculative

README:
- New 'Speculative decoding' section between 'Using the endpoint' and 'Tips'
- Troubleshooting note on GGUF parser fallback for older fine-tunes

PRD:
- Speculative decoding moved from §Out of scope (post-v1) to v1.4.0
- §CLI documents --draft flag on serve + --speculative on fit

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 18: Final review + live smoke + merge + tag + release

This is for the orchestrator (NOT a single implementer subagent — it spans the whole branch).

- [ ] **Step 1: Dispatch a final cross-cutting code review**

Use the `general-purpose` agent with model=opus. Prompt should:
- Diff `main...phase6b-speculative-and-parser`
- Verify the `SpeculativePair` PairVerdict is used consistently between `serve.go` (validation+warning split) and `fit.go` (verdict→display)
- Verify the per-arch formulas are registered correctly and the `paramsBFromTokenEmbd` map keys match what `general.architecture` actually emits for each family
- Verify no PlistSpec or plist template changes leaked in (the whole architectural promise of Task 9 is that the plist is untouched)
- Verify `Deps` field count is unchanged from v1.3.0
- Check that the README's Speculative section's example values match what `runFitSpeculative` actually prints (tabwriter symbols, footer text)

- [ ] **Step 2: Apply review fixes**

Each fix = one commit on `phase6b-speculative-and-parser`.

- [ ] **Step 3: Live smoke on Apple M5**

```bash
# Stop everything; build locally
llamactl status
llamactl stop --all 2>/dev/null
go install ./cmd/llamactl

# AC: GGUF parser tensor-shape fallback
# Pick an installed model showing ? in list (if any).
llamactl list                                                     # snapshot current state
# If a model shows ?, the next list invocation should self-heal.
llamactl list                                                     # second invocation may now show real paramsB

# Diagnostic via cmd/gguf-inspect:
go build ./cmd/gguf-inspect
./gguf-inspect ~/.local/share/llama-models/qwen2.5-7b-instruct/Q4_K_M.gguf
# Should show ParamsCount: 7620000000 (from kv-block, no fallback annotation)
# If you have an older fine-tune in the cache:
./gguf-inspect ~/.local/share/llama-models/<older-fine-tune>/*.gguf
# Should show "(via tensor-shape fallback)" annotation
rm gguf-inspect

# AC: --draft happy path (foreground)
llamactl add qwen2.5-7b-instruct                                 # if not already installed
llamactl add qwen2.5-0.5b-instruct                                # the draft
llamactl serve qwen2.5-7b-instruct --draft qwen2.5-0.5b-instruct &
SERVE_PID=$!
sleep 8
# Verify the log shows the speculative-decoding line:
tail -20 ~/Library/Logs/llamactl/qwen2.5-7b-instruct.log | grep -i speculative
# Quick generation test:
curl -s -d '{"model":"qwen2.5-7b-instruct","messages":[{"role":"user","content":"hi"}]}' \
     http://localhost:8080/v1/chat/completions | head -50
kill $SERVE_PID

# AC: --draft refusals
llamactl add llama-3-1b-instruct 2>/dev/null || true             # for arch-mismatch test
llamactl serve qwen2.5-7b-instruct --draft llama-3-1b-instruct
# Expected: ErrUserError, message names arch mismatch.

llamactl serve qwen2.5-7b-instruct --draft nonexistent-id
# Expected: ErrUserError, suggests llamactl add.

# AC: fit --speculative
llamactl fit --speculative qwen2.5-7b-instruct
# Expected: tabwriter listing arch=qwen2 candidates; llama-arch installs omitted.

# AC: detached serve embeds draft flags in plist
llamactl serve qwen2.5-7b-instruct --draft qwen2.5-0.5b-instruct --detach
sleep 5
grep -A1 "model-draft" ~/Library/LaunchAgents/com.llamactl.qwen2.5-7b-instruct.plist
grep -A1 "ctx-size-draft" ~/Library/LaunchAgents/com.llamactl.qwen2.5-7b-instruct.plist
llamactl stop qwen2.5-7b-instruct

# AC: existing 14 doctor checks all pass
llamactl doctor | grep -c '^[✓✗]'                                # 14
```

If any AC fails: STOP, investigate, fix on `phase6b-speculative-and-parser`. Don't merge until all green.

- [ ] **Step 4: Merge**

```bash
git checkout main
git merge --no-ff phase6b-speculative-and-parser -m "Merge phase6b-speculative-and-parser: speculative decoding + GGUF tensor-shape fallback (v1.4.0)"
```

- [ ] **Step 5: Tag and push**

```bash
git tag -a v1.4.0 -m "v1.4.0: speculative decoding + GGUF tensor-shape parser fallback"
git push origin main
git push origin v1.4.0
```

- [ ] **Step 6: Watch release pipeline**

```bash
gh run watch
# After green, verify cask published:
curl -s https://raw.githubusercontent.com/gregmundy/homebrew-tap/main/Casks/llamactl.rb | head -5
# Should show version "1.4.0"
```

- [ ] **Step 7: Verify brew upgrade in <10s**

```bash
time brew upgrade llamactl
/opt/homebrew/bin/llamactl --version          # → llamactl version v1.4.0
/opt/homebrew/bin/llamactl serve --help | grep -E "draft"
/opt/homebrew/bin/llamactl fit --help | grep -E "speculative"
```

---

## Task 19: Update project_state.md memory

**Files:**
- Modify: `/Users/greg/.claude/projects/-Users-greg-Development-llamactl/memory/project_state.md`
- Modify: `/Users/greg/.claude/projects/-Users-greg-Development-llamactl/memory/MEMORY.md`

- [ ] **Step 1: Add a Phase 6b section**

Append after the Phase 6a section in `project_state.md`. Record:
- Date shipped, tag (v1.4.0), merge commit
- Two headliners: `--draft` flag + `fit --speculative`; GGUF tensor-shape fallback
- Per-arch formulas list (llama / qwen2 / qwen3 / gemma3 / mistral)
- Architectural promise honored: zero PlistSpec changes, zero Deps additions, zero recipes changes
- Any live-smoke surprises caught
- Anything moved out of the deferred-concerns list:
  - Older fine-tune `?` ParamsB display closed for supported arches
  - `SpeculativePair` available as a public function for future callers
- Update "Next" line: Phase 6c (web UI + hot swap proxy) is the remaining track.

- [ ] **Step 2: Update MEMORY.md index**

```md
- [llamactl project state](project_state.md) — v1.4.0 shipped 2026-05-XX (speculative decoding + GGUF parser fallback); Phase 6c (web UI + hot swap proxy) next
```

- [ ] **Step 3: No git commit needed** — memory files live outside the repo.

---

## Self-review checklist (run before handing off to subagent-driven-development)

- **Spec coverage:** every section of `2026-05-12-phase6b-speculative-and-parser-design.md` maps to at least one task:
  - §2 architecture → file structure overview + Tasks 1, 2, 3, 8, 9, 10
  - §3.1 UX → Task 17 README (the example output)
  - §3.2 --draft flag on serve → Tasks 10, 11
  - §3.3 fit --speculative → Tasks 13, 14, 15
  - §3.4 SpeculativePair → Task 8
  - §3.5 plist embedding (now: HasDraft only) → Task 9
  - §3.6 tests → Tasks 8 (speculative_test), 9 (ports_test), 12 (serve_test), 16 (fit_test)
  - §4.2 ReadHeaderWithTensors → Task 2
  - §4.3 per-arch formulas → Tasks 3, 4, 5
  - §4.4 fall-through behavior → Task 2 truncation tests + Task 5 mistral alias
  - §4.5 cmd/gguf-inspect + list self-heal → Tasks 6, 7
  - §6 acceptance criteria → Task 18 live smoke
  - §7 non-goals → no tasks (these are explicit anti-features)

- **Placeholder scan:** no "TODO" / "TBD" remaining in the plan. The `blockCountFromHeader` placeholder noted in Task 2 is removed inline by the same task (BlockCount added to Header struct + populated in parseHeader).

- **Type consistency:**
  - `SpeculativePair` returns `PairVerdict` everywhere (Task 8 defines, Tasks 10, 14 consume).
  - `PairVerdict` fields: `Ok bool, Reason string, CombinedRAMGB float64, SizeRatio float64, ArchMatch bool` — consistent across plan body.
  - `ReadHeaderWithTensors` (Task 2) is the function name everywhere it appears (Tasks 6, 7).
  - `paramsBFromTokenEmbd` map keys are arch strings ("llama", "qwen2", "qwen3", "gemma3", "mistral") matching what `general.architecture` emits — verified during Task 4 calibration.
  - `--draft` is the cobra flag name; `draftID` is the variable; `DraftPath` / `DraftCtxSize` are NOT used (we don't add PlistSpec fields).
  - `runFitSpeculative` is the function name (Tasks 13, 14, 15).
  - `specRow` is the Phase 6b row struct (Task 14); separate from `fitRow` (the existing main-mode struct).
  - `HasDraft(dir, label) (string, bool)` signature consistent in Tasks 9, 12.

If issues found at implementation time: fix inline and continue. If a task reveals a missing requirement: add a sub-task rather than re-planning.

---

## Branch discipline reminder (read before dispatching any implementer)

Every implementer subagent prompt must end with:

> "You are on branch `phase6b-speculative-and-parser`. Do not `git checkout`, `git switch`, `git stash`, `git reset`, or any branch-changing operation. If `git status` shows unexpected files, stop and ask. Your task is exactly Task N below; do not start Task N+1."
