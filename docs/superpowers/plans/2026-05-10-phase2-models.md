# llamactl Phase 2: Models (search / add / list / remove) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver `llamactl search`, `add`, `list`, `remove` — enough to discover a whitelisted model, auto-select the best quant for the host, download with SHA-verified resumable file-locked transfer, and manage per-tool metadata. Covers PRD v1.5 acceptance criteria **6** and **7**.

**Architecture:** Four new packages (`internal/models`, `internal/hf`, `internal/download`, extended `internal/cli`) plus a concrete `models.FileStore`. New seams are added to the existing `Deps` struct as narrow interfaces (mirrors Phase 1). All external I/O — HTTP, filesystem, hardware probes — goes through interfaces so tests use fakes and `httptest`. No real HuggingFace calls, no real `system_profiler` invocations, no real multi-GB downloads in the test suite.

**Tech Stack:** Go 1.26.2 (unchanged from Phase 1). New deps: `golang.org/x/sys/unix` (flock), `golang.org/x/term` (TTY detection). No other additions.

**Working branch:** `phase2-models`. **Every implementer prompt must state this branch and forbid `git checkout/switch/stash/branch`.** The Phase 1 retro logged a Task 3 implementer silently committing to `main` and stashing uncommitted controller work — see `~/.claude/.../feedback_subagent_branch_safety.md`.

---

## Spec coverage (Phase 2 → PRD acceptance criteria)

This plan covers PRD acceptance criteria **6** and **7** and §3 (search/add/list/remove), §5 (storage convention), §6.1 (quant selection), §8 (caching).

| AC | Requirement | Covered by |
|----|-------------|------------|
| 6  | `add qwen2.5-7b` on 16 GB selects Q4_K_M, downloads, verifies SHA, completes in <10 min on 100 Mbps | Tasks 1, 2, 4–7, 11 |
| 7  | Already-present GGUF with matching SHA → `add` skips download, only writes metadata | Task 11 (dedupe fast path) |

Out of scope here: `serve`, `stop`, `status`, launchd, `config write`, `update`, Tailscale, the bare-metal gate (those belong to Phase 3+).

---

## File Structure

```
llamactl/
├── internal/
│   ├── models/                          NEW — pure data + pure logic
│   │   ├── quants.go                    Quant type, Arch type, tables, preference order, constants
│   │   ├── quants_test.go               table presence + monotonicity assertions
│   │   ├── whitelist.go                 Model struct, var Whitelist map[string]Model
│   │   ├── whitelist_test.go            every entry well-formed; LookupOrSuggest fn + tests
│   │   ├── metadata.go                  Metadata struct + JSON tags
│   │   ├── selector.go                  func SelectQuant(model, hw, ctx) (Quant, error)
│   │   ├── selector_test.go             table-driven, walks PRD §6.1 worked examples
│   │   ├── filestore.go                 FileStore (concrete ModelStore): List/Get/Put/Delete
│   │   └── filestore_test.go            uses t.TempDir()
│   │
│   ├── hf/                              NEW — HuggingFace API + cache
│   │   ├── types.go                     Repo, File (Sibling), LFSInfo, SearchHit DTOs
│   │   ├── cache.go                     filesystem cache with TTL envelopes
│   │   ├── cache_test.go                t.TempDir() based
│   │   ├── client.go                    Client { Search, RepoInfo, FetchRange }
│   │   └── client_test.go               httptest-backed
│   │
│   ├── download/                        NEW — resumable, locked, SHA-verified
│   │   ├── progress.go                  TTY-only carriage-return writer
│   │   ├── progress_test.go             fake clock + bytes.Buffer
│   │   ├── download.go                  Downloader.Get(ctx, Request) error
│   │   └── download_test.go             httptest range responses + flock contention
│   │
│   ├── config/
│   │   └── paths.go                     MODIFIED — add ModelsConfigDir, SharedModelsDir, HFCacheDir
│   │
│   └── cli/
│       ├── deps.go                      MODIFIED — add HFClient, Downloader, QuantSelector,
│       │                                            ModelStore, FS interfaces + scalar paths
│       ├── root.go                      MODIFIED — register search/add/list/remove
│       ├── search.go                    NEW
│       ├── search_test.go               NEW
│       ├── add.go                       NEW
│       ├── add_test.go                  NEW
│       ├── list.go                      NEW
│       ├── list_test.go                 NEW
│       ├── remove.go                    NEW
│       ├── remove_test.go               NEW
│       └── integration_test.go          MODIFIED — extend with add→list→remove flow
│
└── cmd/llamactl/main.go                 MODIFIED — construct concrete hf.Client, download.Downloader,
                                                    models.Selector, models.FileStore
```

**Decomposition note:** `internal/models` holds the FileStore even though it's an I/O-bearing struct — it's the natural home for "things that operate on `models.Metadata`". Keeping it out of `internal/cli` lets `cli/list.go` and `cli/remove.go` consume a small interface.

---

## Task 0: Create the feature branch

**Files:** none (git only)

- [ ] **Step 1: Confirm starting state**

Run from `/Users/greg/Development/llamactl`:
```bash
git status && git branch --show-current
```
Expected: working tree clean, on `main` at commit `da8f890` (spec) or later.

- [ ] **Step 2: Create and switch to the feature branch**

```bash
git checkout -b phase2-models
```
Expected: `Switched to a new branch 'phase2-models'`.

- [ ] **Step 3: Verify**

```bash
git branch --show-current
```
Expected output: `phase2-models`. Subagent prompts will name this branch.

---

## Task 1: `internal/models` — Quants, Arch, tables, constants

**Files:**
- Create: `internal/models/quants.go`
- Create: `internal/models/quants_test.go`

- [ ] **Step 1: Write the failing test**

`internal/models/quants_test.go`:
```go
package models

import "testing"

func TestPreferenceOrder(t *testing.T) {
	want := []Quant{Q5_K_M, Q4_K_M, Q4_K_S, IQ4_XS, IQ3_M, IQ3_XS, Q2_K}
	if len(PreferenceOrder) != len(want) {
		t.Fatalf("PreferenceOrder length = %d, want %d", len(PreferenceOrder), len(want))
	}
	for i, q := range want {
		if PreferenceOrder[i] != q {
			t.Errorf("PreferenceOrder[%d] = %s, want %s", i, PreferenceOrder[i], q)
		}
	}
}

func TestQuantSizeTableMonotonic(t *testing.T) {
	for params, row := range QuantSizeTable {
		var prev float64 = 1e9
		for _, q := range PreferenceOrder {
			size, ok := row[q]
			if !ok {
				t.Errorf("QuantSizeTable[%d] missing %s", params, q)
				continue
			}
			if size > prev {
				t.Errorf("QuantSizeTable[%d][%s]=%.2f > previous %.2f (preference order should be larger->smaller)",
					params, q, size, prev)
			}
			prev = size
		}
	}
}

func TestKVCacheTablesPopulated(t *testing.T) {
	for _, arch := range []Arch{ArchQwen25, ArchLlama3, ArchMistral} {
		row, ok := KVCachePerTokenKB[arch]
		if !ok {
			t.Errorf("KVCachePerTokenKB missing arch %s", arch)
			continue
		}
		if _, ok := row[Q8_0]; !ok {
			t.Errorf("KVCachePerTokenKB[%s] missing Q8_0", arch)
		}
	}
}
```

- [ ] **Step 2: Run, confirm it fails**

```bash
go test ./internal/models/...
```
Expected: `undefined: Quant` and similar — the package doesn't exist yet.

- [ ] **Step 3: Write `internal/models/quants.go`**

```go
// Package models holds the curated whitelist, quantization tables, and the
// pure quant-selection algorithm. No I/O. No clocks. No env reads. The
// FileStore in this package is the one exception — it operates on Metadata
// and is the natural home for it.
package models

// Quant is a GGUF quantization preset name (e.g., "Q4_K_M").
type Quant string

const (
	Q5_K_M Quant = "Q5_K_M"
	Q4_K_M Quant = "Q4_K_M"
	Q4_K_S Quant = "Q4_K_S"
	IQ4_XS Quant = "IQ4_XS"
	IQ3_M  Quant = "IQ3_M"
	IQ3_XS Quant = "IQ3_XS"
	Q2_K   Quant = "Q2_K"

	// Q8_0 is only used as a KV-cache lookup key; never appears in
	// PreferenceOrder because the spec doesn't ship 8-bit weights as a
	// fallback (too large for the host classes we target).
	Q8_0 Quant = "Q8_0"
)

// PreferenceOrder is the descending-quality fallback chain from PRD §6.1.
// SelectQuant walks this list and returns the first quant whose size fits
// the computed model budget.
var PreferenceOrder = []Quant{Q5_K_M, Q4_K_M, Q4_K_S, IQ4_XS, IQ3_M, IQ3_XS, Q2_K}

// Arch is a model family tag used as a KV-cache lookup key.
type Arch string

const (
	ArchQwen25  Arch = "qwen2.5"
	ArchLlama3  Arch = "llama3"
	ArchMistral Arch = "mistral"
)

// Selector constants from PRD §6.1.
const (
	OSOverheadGB = 4.0
	HeadroomGB   = 2.0

	// DefaultIogpuRatio is the fraction of total RAM macOS will allow the
	// GPU to wire when iogpu.wired_limit_mb is not explicitly set. The
	// real default is dynamic; 0.67 is the empirical mean that makes PRD
	// AC#6 produce Q4_K_M on a 16 GB host with qwen2.5-7b, and it matches
	// what `sudo sysctl iogpu.wired_limit_mb` users typically observe
	// before overriding.
	DefaultIogpuRatio = 0.67
)

// QuantSizeTable[paramsB][quant] is approximate on-disk GGUF size in
// gigabytes. Numbers are starting estimates from llama.cpp's GGUF
// model-size docs + measured filesizes for the v1 whitelist. The
// implementer should re-validate against HF file sizes during Task 11.
var QuantSizeTable = map[int]map[Quant]float64{
	3: {Q5_K_M: 2.2, Q4_K_M: 1.9, Q4_K_S: 1.8, IQ4_XS: 1.7, IQ3_M: 1.5, IQ3_XS: 1.4, Q2_K: 1.3},
	7: {Q5_K_M: 5.1, Q4_K_M: 4.4, Q4_K_S: 4.1, IQ4_XS: 3.8, IQ3_M: 3.3, IQ3_XS: 3.1, Q2_K: 2.7},
	8: {Q5_K_M: 5.7, Q4_K_M: 4.9, Q4_K_S: 4.6, IQ4_XS: 4.3, IQ3_M: 3.8, IQ3_XS: 3.5, Q2_K: 3.0},
	14: {Q5_K_M: 10.4, Q4_K_M: 8.9, Q4_K_S: 8.4, IQ4_XS: 7.8, IQ3_M: 6.9, IQ3_XS: 6.4, Q2_K: 5.5},
	70: {Q5_K_M: 49.9, Q4_K_M: 42.5, Q4_K_S: 40.3, IQ4_XS: 37.7, IQ3_M: 32.9, IQ3_XS: 30.8, Q2_K: 26.4},
}

// KVCachePerTokenKB[arch][kvQuant] is the combined K+V cache size per token
// in kilobytes. A single conservative number per arch covers the largest
// supported model in that family — overestimates KV for smaller models in
// the same family, biasing the selector slightly toward smaller weight
// quants. Acceptable for v1; refine when adding new models.
var KVCachePerTokenKB = map[Arch]map[Quant]float64{
	ArchQwen25:  {Q8_0: 0.5},
	ArchLlama3:  {Q8_0: 0.5},
	ArchMistral: {Q8_0: 0.5},
}
```

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/models/...
```
Expected: `ok  github.com/gregmundy/llamactl/internal/models`.

- [ ] **Step 5: Commit**

```bash
git add internal/models/quants.go internal/models/quants_test.go
git commit -m "feat(models): quant + arch types, size + KV-cache tables, constants"
```

---

## Task 2: `internal/models` — Whitelist + LookupOrSuggest

**Files:**
- Create: `internal/models/whitelist.go`
- Create: `internal/models/whitelist_test.go`

- [ ] **Step 1: Write the failing test**

`internal/models/whitelist_test.go`:
```go
package models

import (
	"strings"
	"testing"
)

func TestWhitelistEntriesWellFormed(t *testing.T) {
	if len(Whitelist) == 0 {
		t.Fatal("Whitelist is empty")
	}
	for id, m := range Whitelist {
		if m.ID != id {
			t.Errorf("Whitelist[%q].ID = %q (must equal map key)", id, m.ID)
		}
		if m.HFRepo == "" {
			t.Errorf("Whitelist[%q].HFRepo empty", id)
		}
		if m.ParamsB <= 0 {
			t.Errorf("Whitelist[%q].ParamsB = %d", id, m.ParamsB)
		}
		if m.MaxCtx <= 0 {
			t.Errorf("Whitelist[%q].MaxCtx = %d", id, m.MaxCtx)
		}
		if _, ok := QuantSizeTable[m.ParamsB]; !ok {
			t.Errorf("Whitelist[%q].ParamsB = %d has no QuantSizeTable row", id, m.ParamsB)
		}
		switch m.Arch {
		case ArchQwen25, ArchLlama3, ArchMistral:
		default:
			t.Errorf("Whitelist[%q].Arch = %q (not a known Arch)", id, m.Arch)
		}
	}
}

func TestLookupOrSuggestHit(t *testing.T) {
	m, err := LookupOrSuggest("qwen2.5-7b-instruct")
	if err != nil {
		t.Fatalf("LookupOrSuggest returned error: %v", err)
	}
	if m.ID != "qwen2.5-7b-instruct" {
		t.Errorf("ID = %q", m.ID)
	}
}

func TestLookupOrSuggestMiss(t *testing.T) {
	_, err := LookupOrSuggest("not-a-real-model")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "qwen2.5-7b-instruct") {
		t.Errorf("error should list valid IDs, got: %s", msg)
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/models/... -run Whitelist
go test ./internal/models/... -run LookupOrSuggest
```
Expected: undefined symbols.

- [ ] **Step 3: Write `internal/models/whitelist.go`**

```go
package models

import (
	"fmt"
	"sort"
	"strings"
)

// Model is a whitelisted model entry. The Whitelist map's key matches Model.ID.
type Model struct {
	ID      string // canonical llamactl id, e.g. "qwen2.5-7b-instruct"
	HFRepo  string // HuggingFace repo, e.g. "Qwen/Qwen2.5-7B-Instruct-GGUF"
	Arch    Arch
	ParamsB int // parameter count in billions, must have a row in QuantSizeTable
	MaxCtx  int // maximum context tokens supported by the model family
}

// Whitelist is the curated set of models llamactl supports in v1.
// Expanding it is a code change, per PRD §4.
var Whitelist = map[string]Model{
	"qwen2.5-3b-instruct":  {ID: "qwen2.5-3b-instruct", HFRepo: "Qwen/Qwen2.5-3B-Instruct-GGUF", Arch: ArchQwen25, ParamsB: 3, MaxCtx: 32768},
	"qwen2.5-7b-instruct":  {ID: "qwen2.5-7b-instruct", HFRepo: "Qwen/Qwen2.5-7B-Instruct-GGUF", Arch: ArchQwen25, ParamsB: 7, MaxCtx: 32768},
	"qwen2.5-14b-instruct": {ID: "qwen2.5-14b-instruct", HFRepo: "Qwen/Qwen2.5-14B-Instruct-GGUF", Arch: ArchQwen25, ParamsB: 14, MaxCtx: 32768},
	"qwen2.5-coder-3b":     {ID: "qwen2.5-coder-3b", HFRepo: "Qwen/Qwen2.5-Coder-3B-Instruct-GGUF", Arch: ArchQwen25, ParamsB: 3, MaxCtx: 32768},
	"qwen2.5-coder-7b":     {ID: "qwen2.5-coder-7b", HFRepo: "Qwen/Qwen2.5-Coder-7B-Instruct-GGUF", Arch: ArchQwen25, ParamsB: 7, MaxCtx: 32768},
	"qwen2.5-coder-14b":    {ID: "qwen2.5-coder-14b", HFRepo: "Qwen/Qwen2.5-Coder-14B-Instruct-GGUF", Arch: ArchQwen25, ParamsB: 14, MaxCtx: 32768},
	"llama3.1-8b":          {ID: "llama3.1-8b", HFRepo: "bartowski/Meta-Llama-3.1-8B-Instruct-GGUF", Arch: ArchLlama3, ParamsB: 8, MaxCtx: 131072},
	"llama3.2-3b":          {ID: "llama3.2-3b", HFRepo: "bartowski/Llama-3.2-3B-Instruct-GGUF", Arch: ArchLlama3, ParamsB: 3, MaxCtx: 131072},
	"llama3.3-70b":         {ID: "llama3.3-70b", HFRepo: "bartowski/Llama-3.3-70B-Instruct-GGUF", Arch: ArchLlama3, ParamsB: 70, MaxCtx: 131072},
	"mistral-7b-v0.3":      {ID: "mistral-7b-v0.3", HFRepo: "bartowski/Mistral-7B-Instruct-v0.3-GGUF", Arch: ArchMistral, ParamsB: 7, MaxCtx: 32768},
}

// LookupOrSuggest returns the whitelist entry for id, or an error listing
// available ids if it isn't whitelisted. Error message is suitable for
// printing to the user verbatim (no further formatting needed by callers).
func LookupOrSuggest(id string) (Model, error) {
	if m, ok := Whitelist[id]; ok {
		return m, nil
	}
	ids := make([]string, 0, len(Whitelist))
	for k := range Whitelist {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	return Model{}, fmt.Errorf("unknown model %q; available: %s", id, strings.Join(ids, ", "))
}
```

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/models/...
```

- [ ] **Step 5: Commit**

```bash
git add internal/models/whitelist.go internal/models/whitelist_test.go
git commit -m "feat(models): whitelist map + LookupOrSuggest"
```

---

## Task 3: `internal/models` — Metadata struct + JSON shape

**Files:**
- Create: `internal/models/metadata.go`

- [ ] **Step 1: Write the file (no test needed — pure types verified by FileStore test in Task 4)**

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
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/models/...
```
Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/models/metadata.go
git commit -m "feat(models): Metadata JSON shape"
```

---

## Task 4: `internal/models` — Selector (pure quant-selection algorithm)

**Files:**
- Create: `internal/models/selector.go`
- Create: `internal/models/selector_test.go`

- [ ] **Step 1: Write the failing test**

`internal/models/selector_test.go`:
```go
package models

import (
	"errors"
	"testing"

	"github.com/gregmundy/llamactl/internal/hardware"
)

func hw(ramGB int, iogpuMB int) hardware.Info {
	return hardware.Info{
		RAMBytes:          uint64(ramGB) * (1 << 30),
		IogpuWiredLimitMB: iogpuMB,
	}
}

func TestSelectQuant_PRDExample16GBQwen7B(t *testing.T) {
	m := Whitelist["qwen2.5-7b-instruct"]
	got, err := SelectQuant(m, hw(16, 0), 8192)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != Q4_K_M {
		t.Errorf("got %s, want Q4_K_M (PRD AC#6)", got)
	}
}

func TestSelectQuant_NoneFit(t *testing.T) {
	m := Whitelist["llama3.3-70b"]
	_, err := SelectQuant(m, hw(8, 0), 8192)
	if !errors.Is(err, ErrNoQuantFits) {
		t.Fatalf("got %v, want ErrNoQuantFits", err)
	}
}

func TestSelectQuant_HighRAMPicksHighestQuant(t *testing.T) {
	m := Whitelist["qwen2.5-7b-instruct"]
	got, err := SelectQuant(m, hw(64, 0), 8192)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != Q5_K_M {
		t.Errorf("got %s, want Q5_K_M", got)
	}
}

func TestSelectQuant_IogpuOverrideUsed(t *testing.T) {
	// 64 GB RAM but iogpu pinned to 8 GB → tight budget.
	m := Whitelist["qwen2.5-7b-instruct"]
	got, err := SelectQuant(m, hw(64, 8*1024), 8192)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// usable = 8 - 4 - 2 = 2 GB → only Q2_K (2.7 GB) > 2 → none fit
	if !errors.Is(err, ErrNoQuantFits) && got != Q2_K {
		// budget 2 < Q2_K 2.7 → ErrNoQuantFits expected
		t.Errorf("got (%s, %v); expected ErrNoQuantFits", got, err)
	}
}

func TestSelectQuant_UnknownParamsBRow(t *testing.T) {
	m := Model{ID: "fake", HFRepo: "x/y", Arch: ArchQwen25, ParamsB: 999, MaxCtx: 4096}
	_, err := SelectQuant(m, hw(64, 0), 8192)
	if err == nil {
		t.Fatal("expected error for missing QuantSizeTable row")
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/models/... -run SelectQuant
```
Expected: `undefined: SelectQuant`.

- [ ] **Step 3: Write `internal/models/selector.go`**

```go
package models

import (
	"errors"
	"fmt"

	"github.com/gregmundy/llamactl/internal/hardware"
)

// ErrNoQuantFits is returned when even the smallest quant in
// PreferenceOrder does not fit the computed model budget.
var ErrNoQuantFits = errors.New("no quant fits available memory")

// gpuAddressableGB returns the GPU-addressable memory in gigabytes, derived
// from hw.IogpuWiredLimitMB if explicitly set, else from RAMBytes scaled by
// DefaultIogpuRatio (the empirical macOS default — see quants.go).
func gpuAddressableGB(hw hardware.Info) float64 {
	if hw.IogpuWiredLimitMB > 0 {
		return float64(hw.IogpuWiredLimitMB) / 1024.0
	}
	return float64(hw.RAMBytes) / (1 << 30) * DefaultIogpuRatio
}

// SelectQuant implements PRD §6.1. Pure function: no I/O.
func SelectQuant(model Model, info hardware.Info, targetCtx int) (Quant, error) {
	sizeRow, ok := QuantSizeTable[model.ParamsB]
	if !ok {
		return "", fmt.Errorf("no QuantSizeTable row for ParamsB=%d (model %q)", model.ParamsB, model.ID)
	}
	kvRow, ok := KVCachePerTokenKB[model.Arch]
	if !ok {
		return "", fmt.Errorf("no KVCachePerTokenKB row for Arch=%s (model %q)", model.Arch, model.ID)
	}
	kvPerTok, ok := kvRow[Q8_0]
	if !ok {
		return "", fmt.Errorf("no Q8_0 entry in KVCachePerTokenKB[%s]", model.Arch)
	}

	usable := gpuAddressableGB(info) - OSOverheadGB - HeadroomGB
	kvCacheGB := float64(targetCtx) * kvPerTok / (1024.0 * 1024.0)
	budget := usable - kvCacheGB
	if budget <= 0 {
		return "", fmt.Errorf("%w: usable memory after OS+headroom+KV is %.2f GB", ErrNoQuantFits, budget)
	}

	for _, q := range PreferenceOrder {
		size, ok := sizeRow[q]
		if !ok {
			continue
		}
		if size <= budget {
			return q, nil
		}
	}
	return "", fmt.Errorf("%w: smallest available quant (%s, %.2f GB) exceeds budget %.2f GB; try a smaller model or shorter --ctx",
		ErrNoQuantFits, PreferenceOrder[len(PreferenceOrder)-1], sizeRow[PreferenceOrder[len(PreferenceOrder)-1]], budget)
}
```

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/models/...
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/models/selector.go internal/models/selector_test.go
git commit -m "feat(models): SelectQuant per PRD §6.1"
```

---

## Task 5: `internal/models` — FileStore (concrete ModelStore)

**Files:**
- Create: `internal/models/filestore.go`
- Create: `internal/models/filestore_test.go`

- [ ] **Step 1: Write the failing test**

`internal/models/filestore_test.go`:
```go
package models

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFileStorePutGetListDelete(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)
	ctx := context.Background()

	m := Metadata{
		ID:        "qwen2.5-7b-instruct",
		Repo:      "Qwen/Qwen2.5-7B-Instruct-GGUF",
		Quant:     Q4_K_M,
		SHA256:    "deadbeef",
		GGUFPath:  "/tmp/x.gguf",
		SizeBytes: 4_000_000_000,
		AddedAt:   time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	}
	if err := s.Put(ctx, m); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(ctx, m.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SHA256 != m.SHA256 || got.Quant != m.Quant {
		t.Errorf("Get returned %+v, want fields matching %+v", got, m)
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}

	if err := s.Delete(ctx, m.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: err = %v, want ErrNotFound", err)
	}
}

func TestFileStoreGetMissingReturnsErrNotFound(t *testing.T) {
	s := NewFileStore(t.TempDir())
	_, err := s.Get(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestFileStoreListEmpty(t *testing.T) {
	s := NewFileStore(t.TempDir())
	list, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("List len = %d, want 0", len(list))
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/models/... -run FileStore
```
Expected: `undefined: NewFileStore`, `undefined: ErrNotFound`.

- [ ] **Step 3: Write `internal/models/filestore.go`**

```go
package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNotFound is returned by FileStore.Get/Delete when no metadata file
// exists for the given id.
var ErrNotFound = errors.New("model metadata not found")

// FileStore is the on-disk implementation of cli.ModelStore. Each model id
// maps to one JSON file under dir.
type FileStore struct {
	dir string
}

// NewFileStore returns a FileStore writing to dir. The directory is created
// lazily on first Put.
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

func (s *FileStore) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// List returns all stored Metadata sorted by ID.
func (s *FileStore) List(_ context.Context) ([]Metadata, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read models dir %s: %w", s.dir, err)
	}
	var out []Metadata
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		m, err := s.Get(context.Background(), id)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Get returns the Metadata for id or ErrNotFound.
func (s *FileStore) Get(_ context.Context, id string) (Metadata, error) {
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return Metadata{}, ErrNotFound
	}
	if err != nil {
		return Metadata{}, fmt.Errorf("read metadata %s: %w", id, err)
	}
	var m Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return Metadata{}, fmt.Errorf("decode metadata %s: %w", id, err)
	}
	return m, nil
}

// Put writes the metadata atomically (write to tmp + rename).
func (s *FileStore) Put(_ context.Context, m Metadata) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", s.dir, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode metadata %s: %w", m.ID, err)
	}
	final := s.path(m.ID)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, final, err)
	}
	return nil
}

// Delete removes the metadata file. Missing -> ErrNotFound.
func (s *FileStore) Delete(_ context.Context, id string) error {
	err := os.Remove(s.path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("remove metadata %s: %w", id, err)
	}
	return nil
}
```

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/models/...
```

- [ ] **Step 5: Commit**

```bash
git add internal/models/filestore.go internal/models/filestore_test.go
git commit -m "feat(models): FileStore — JSON CRUD for per-tool metadata"
```

---

## Task 6: `internal/hf` — DTOs + filesystem cache

**Files:**
- Create: `internal/hf/types.go`
- Create: `internal/hf/cache.go`
- Create: `internal/hf/cache_test.go`

- [ ] **Step 1: Write the failing test**

`internal/hf/cache_test.go`:
```go
package hf

import (
	"testing"
	"time"
)

func TestCachePutGet(t *testing.T) {
	c := NewCache(t.TempDir())
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	payload := []byte(`{"hello":"world"}`)
	if err := c.Put("ns", "key", payload); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, fresh, err := c.Get("ns", "key", time.Hour)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !fresh {
		t.Errorf("fresh = false; want true (just written)")
	}
	if string(got) != string(payload) {
		t.Errorf("payload = %q, want %q", string(got), string(payload))
	}
}

func TestCacheStale(t *testing.T) {
	c := NewCache(t.TempDir())
	c.now = func() time.Time { return time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC) }
	if err := c.Put("ns", "key", []byte(`{}`)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	c.now = func() time.Time { return time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC) }
	got, fresh, err := c.Get("ns", "key", time.Hour)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fresh {
		t.Errorf("fresh = true; want false (24h elapsed, TTL 1h)")
	}
	if got == nil {
		t.Errorf("payload still returned even when stale (caller decides)")
	}
}

func TestCacheMiss(t *testing.T) {
	c := NewCache(t.TempDir())
	got, fresh, err := c.Get("ns", "missing", time.Hour)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil || fresh {
		t.Errorf("miss should return nil/false, got (%v, %v)", got, fresh)
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/hf/...
```
Expected: package does not exist yet.

- [ ] **Step 3: Write `internal/hf/types.go`**

```go
// Package hf is a thin client for the HuggingFace Hub HTTP API plus a
// filesystem-backed cache. It owns net/http for this project — no command
// in internal/cli should import net/http directly.
package hf

// SearchHit is a HuggingFace /api/models?search=... entry.
type SearchHit struct {
	ID         string `json:"id"`        // e.g. "Qwen/Qwen2.5-7B-Instruct-GGUF"
	Downloads  int    `json:"downloads"`
	Likes      int    `json:"likes"`
	LastModified string `json:"lastModified"`
}

// LFSInfo is the per-file LFS metadata exposed by HF's /api/models/<repo>.
type LFSInfo struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// File is a sibling entry in an HF repo listing.
type File struct {
	RFilename string  `json:"rfilename"`
	LFS       *LFSInfo `json:"lfs,omitempty"`
}

// Repo is a HuggingFace /api/models/<repo> response (subset we care about).
type Repo struct {
	ID       string `json:"id"`
	Siblings []File `json:"siblings"`
}
```

- [ ] **Step 4: Write `internal/hf/cache.go`**

```go
package hf

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Cache is a filesystem-backed cache with TTL envelopes. Entries are stored
// under <root>/<namespace>/<sha1(key)>.json.
//
// Get returns (payload, fresh, error). A stale hit still returns payload —
// the caller decides whether to use it (offline fallback) or refresh.
type Cache struct {
	root string
	now  func() time.Time
}

// NewCache returns a Cache rooted at dir. Directory creation is lazy.
func NewCache(dir string) *Cache {
	return &Cache{root: dir, now: time.Now}
}

type envelope struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Payload   json.RawMessage `json:"payload"`
}

func (c *Cache) path(ns, key string) string {
	sum := sha1.Sum([]byte(key))
	return filepath.Join(c.root, ns, hex.EncodeToString(sum[:])+".json")
}

func (c *Cache) Put(ns, key string, payload []byte) error {
	p := c.path(ns, key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("mkdir cache: %w", err)
	}
	env := envelope{FetchedAt: c.now(), Payload: payload}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (c *Cache) Get(ns, key string, ttl time.Duration) ([]byte, bool, error) {
	data, err := os.ReadFile(c.path(ns, key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read cache: %w", err)
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		// Corrupt entry — treat as miss.
		return nil, false, nil
	}
	fresh := c.now().Sub(env.FetchedAt) <= ttl
	return env.Payload, fresh, nil
}
```

- [ ] **Step 5: Run, confirm pass**

```bash
go test ./internal/hf/...
```

- [ ] **Step 6: Commit**

```bash
git add internal/hf/types.go internal/hf/cache.go internal/hf/cache_test.go
git commit -m "feat(hf): DTOs and filesystem cache with TTL envelopes"
```

---

## Task 7: `internal/hf` — Client (Search, RepoInfo, FetchRange)

**Files:**
- Create: `internal/hf/client.go`
- Create: `internal/hf/client_test.go`

- [ ] **Step 1: Write the failing test**

`internal/hf/client_test.go`:
```go
package hf

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientSearchAndCache(t *testing.T) {
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !strings.HasPrefix(r.URL.Path, "/api/models") {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"Qwen/Qwen2.5-7B-Instruct-GGUF","downloads":1,"likes":1}]`))
	}))
	t.Cleanup(ts.Close)

	c := NewClient(ts.URL, NewCache(t.TempDir()), nil)
	got, err := c.Search(context.Background(), "qwen2.5")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].ID != "Qwen/Qwen2.5-7B-Instruct-GGUF" {
		t.Errorf("got %+v", got)
	}

	// Second call should hit cache.
	if _, err := c.Search(context.Background(), "qwen2.5"); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("server hits = %d, want 1 (second call should be cached)", hits)
	}
}

func TestClientSearchRefreshBypassesCache(t *testing.T) {
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(`[]`))
	}))
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL, NewCache(t.TempDir()), nil)
	if _, err := c.Search(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SearchRefresh(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2", hits)
	}
}

func TestClientRepoInfo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models/Qwen/Qwen2.5-7B-Instruct-GGUF" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(`{
			"id":"Qwen/Qwen2.5-7B-Instruct-GGUF",
			"siblings":[
				{"rfilename":"qwen2.5-7b-instruct-q4_k_m.gguf","lfs":{"sha256":"deadbeef","size":4500000000}}
			]
		}`))
	}))
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL, NewCache(t.TempDir()), nil)
	repo, err := c.RepoInfo(context.Background(), "Qwen/Qwen2.5-7B-Instruct-GGUF")
	if err != nil {
		t.Fatalf("RepoInfo: %v", err)
	}
	if len(repo.Siblings) != 1 || repo.Siblings[0].LFS == nil || repo.Siblings[0].LFS.SHA256 != "deadbeef" {
		t.Errorf("repo = %+v", repo)
	}
}

func TestClientFetchRangeHonorsRangeHeader(t *testing.T) {
	body := []byte("0123456789abcdef")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rng := r.Header.Get("Range")
		if rng != "bytes=4-" {
			t.Errorf("Range = %q", rng)
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write(body[4:])
	}))
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL, NewCache(t.TempDir()), nil)
	var buf bytes.Buffer
	if err := c.FetchRange(context.Background(), "Qwen/Repo", "file.gguf", 4, 0, &buf); err != nil {
		t.Fatalf("FetchRange: %v", err)
	}
	if buf.String() != "456789abcdef" {
		t.Errorf("got %q", buf.String())
	}
}

func TestClientFetchRangeServerIgnoresRangeReturns200(t *testing.T) {
	body := []byte("0123456789abcdef")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL, NewCache(t.TempDir()), nil)
	var buf bytes.Buffer
	err := c.FetchRange(context.Background(), "r", "f", 4, 0, &buf)
	if err == nil {
		t.Fatal("expected ErrRangeNotSupported")
	}
	// Caller (Downloader) detects this and restarts from offset 0.
}

func TestClientRetries5xxAndFails(t *testing.T) {
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(503)
	}))
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL, NewCache(t.TempDir()), nil)
	c.retrySleep = func(time.Duration) {} // no real delays
	_, err := c.Search(context.Background(), "q")
	if err == nil {
		t.Fatal("expected error")
	}
	if hits != 3 {
		t.Errorf("hits = %d, want 3", hits)
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/hf/...
```

- [ ] **Step 3: Write `internal/hf/client.go`**

```go
package hf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	SearchTTL = 24 * time.Hour
	RepoTTL   = 7 * 24 * time.Hour
)

// ErrRangeNotSupported is returned by FetchRange when the offset is non-zero
// but the server responds with 200 (full body) instead of 206 (partial).
// The caller (Downloader) handles this by restarting from offset 0.
var ErrRangeNotSupported = errors.New("server did not honor Range header")

// Client talks to the HuggingFace Hub. Search and RepoInfo are cached;
// FetchRange streams bytes.
type Client struct {
	baseURL    string
	cache      *Cache
	httpClient *http.Client
	token      string // from HF_TOKEN / LLAMACTL_HF_TOKEN; empty if not set
	retrySleep func(time.Duration)
}

// NewClient returns a Client. If httpClient is nil, http.DefaultClient is used.
func NewClient(baseURL string, cache *Cache, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:    baseURL,
		cache:      cache,
		httpClient: httpClient,
		retrySleep: time.Sleep,
	}
}

// WithToken sets the bearer token used on every request.
func (c *Client) WithToken(t string) *Client { c.token = t; return c }

// Search returns whitelisted-candidate hits (caller filters against the
// internal models whitelist). Cached with SearchTTL.
func (c *Client) Search(ctx context.Context, query string) ([]SearchHit, error) {
	return c.search(ctx, query, false)
}

// SearchRefresh bypasses the cache (used for --refresh).
func (c *Client) SearchRefresh(ctx context.Context, query string) ([]SearchHit, error) {
	return c.search(ctx, query, true)
}

func (c *Client) search(ctx context.Context, query string, refresh bool) ([]SearchHit, error) {
	if !refresh {
		if data, fresh, err := c.cache.Get("hf-search", query, SearchTTL); err == nil && fresh && data != nil {
			var hits []SearchHit
			if jerr := json.Unmarshal(data, &hits); jerr == nil {
				return hits, nil
			}
			// fall through on decode failure
		}
	}
	endpoint := c.baseURL + "/api/models?search=" + url.QueryEscape(query)
	data, err := c.doJSON(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var hits []SearchHit
	if err := json.Unmarshal(data, &hits); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	_ = c.cache.Put("hf-search", query, data)
	return hits, nil
}

// RepoInfo fetches /api/models/<repoID>. Cached with RepoTTL.
func (c *Client) RepoInfo(ctx context.Context, repoID string) (Repo, error) {
	if data, fresh, err := c.cache.Get("hf-repo", repoID, RepoTTL); err == nil && fresh && data != nil {
		var r Repo
		if jerr := json.Unmarshal(data, &r); jerr == nil {
			return r, nil
		}
	}
	endpoint := c.baseURL + "/api/models/" + repoID
	data, err := c.doJSON(ctx, endpoint)
	if err != nil {
		return Repo{}, err
	}
	var r Repo
	if err := json.Unmarshal(data, &r); err != nil {
		return Repo{}, fmt.Errorf("decode repo response: %w", err)
	}
	_ = c.cache.Put("hf-repo", repoID, data)
	return r, nil
}

// FetchRange streams [offset, end) of repoID/file into w. end == 0 means EOF.
// If offset > 0 and the server returns 200 (instead of 206), returns
// ErrRangeNotSupported without writing anything to w.
func (c *Client) FetchRange(ctx context.Context, repoID, file string, offset, end int64, w io.Writer) error {
	endpoint := c.baseURL + "/" + repoID + "/resolve/main/" + file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if offset > 0 || end > 0 {
		if end > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, end-1))
		} else {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if offset > 0 && resp.StatusCode == http.StatusOK {
		return ErrRangeNotSupported
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("fetch %s: status %d", endpoint, resp.StatusCode)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("fetch body %s: %w", endpoint, err)
	}
	return nil
}

// doJSON does a GET with up to 3 attempts on 5xx + transport errors.
func (c *Client) doJSON(ctx context.Context, endpoint string) ([]byte, error) {
	delays := []time.Duration{0, time.Second, 2 * time.Second}
	var lastErr error
	for _, d := range delays {
		if d > 0 {
			c.retrySleep(d)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("GET %s: %w", endpoint, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch {
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("GET %s: status %d", endpoint, resp.StatusCode)
			continue
		case resp.StatusCode >= 400:
			return nil, fmt.Errorf("GET %s: status %d: %s", endpoint, resp.StatusCode, string(body))
		}
		return body, nil
	}
	return nil, lastErr
}
```

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/hf/...
```

- [ ] **Step 5: Commit**

```bash
git add internal/hf/client.go internal/hf/client_test.go
git commit -m "feat(hf): Client with Search/RepoInfo/FetchRange + retry"
```

---

## Task 8: `internal/download` — Progress writer

**Files:**
- Create: `internal/download/progress.go`
- Create: `internal/download/progress_test.go`

- [ ] **Step 1: Write the failing test**

`internal/download/progress_test.go`:
```go
package download

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestProgressEmitsCarriageReturnUpdates(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	p := &Progress{
		Out:    &buf,
		Total:  10 * 1024 * 1024, // 10 MiB
		Now:    func() time.Time { now = now.Add(250 * time.Millisecond); return now },
		IsTTY:  true,
		MinInterval: 200 * time.Millisecond,
	}
	for i := 0; i < 10; i++ {
		_, _ = p.Write(make([]byte, 1024*1024)) // 1 MiB each
	}
	p.Finish()
	out := buf.String()
	if !strings.Contains(out, "\r") {
		t.Errorf("output should contain CR updates; got %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("Finish should emit trailing newline; got %q", out[len(out)-5:])
	}
}

func TestProgressSuppressedWhenNotTTY(t *testing.T) {
	var buf bytes.Buffer
	p := &Progress{Out: &buf, Total: 1024, IsTTY: false}
	_, _ = p.Write(make([]byte, 1024))
	p.Finish()
	if buf.Len() != 0 {
		t.Errorf("expected no output when not TTY; got %q", buf.String())
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/download/...
```

- [ ] **Step 3: Write `internal/download/progress.go`**

```go
// Package download orchestrates resumable, file-locked, SHA-verified
// downloads of single GGUF files. It depends on internal/hf for the
// network seam.
package download

import (
	"fmt"
	"io"
	"time"
)

// Progress is an io.Writer wrapper that emits a single-line progress
// indicator with carriage-return updates. No-op when IsTTY is false.
type Progress struct {
	Out         io.Writer
	Total       int64
	Initial     int64                // bytes already on disk before this run
	Now         func() time.Time     // for tests; defaults to time.Now
	IsTTY       bool
	MinInterval time.Duration        // suppress updates faster than this

	written int64
	start   time.Time
	last    time.Time
}

func (p *Progress) ensureInit() {
	if p.Now == nil {
		p.Now = time.Now
	}
	if p.MinInterval == 0 {
		p.MinInterval = 250 * time.Millisecond
	}
	if p.start.IsZero() {
		p.start = p.Now()
		p.last = p.start.Add(-p.MinInterval) // force first update
	}
}

func (p *Progress) Write(b []byte) (int, error) {
	p.ensureInit()
	p.written += int64(len(b))
	if !p.IsTTY {
		return len(b), nil
	}
	now := p.Now()
	if now.Sub(p.last) < p.MinInterval {
		return len(b), nil
	}
	p.last = now
	p.emit(now)
	return len(b), nil
}

// Finish flushes a final update and a newline.
func (p *Progress) Finish() {
	if !p.IsTTY {
		return
	}
	p.ensureInit()
	p.emit(p.Now())
	fmt.Fprintln(p.Out)
}

func (p *Progress) emit(now time.Time) {
	done := p.Initial + p.written
	pct := 0.0
	if p.Total > 0 {
		pct = float64(done) / float64(p.Total) * 100
	}
	elapsed := now.Sub(p.start).Seconds()
	speedMiBs := 0.0
	if elapsed > 0 {
		speedMiBs = float64(p.written) / elapsed / (1024 * 1024)
	}
	etaStr := "--:--:--"
	if speedMiBs > 0 && p.Total > done {
		remBytes := float64(p.Total - done)
		etaSec := int(remBytes / (speedMiBs * 1024 * 1024))
		etaStr = fmt.Sprintf("%02d:%02d:%02d", etaSec/3600, (etaSec%3600)/60, etaSec%60)
	}
	fmt.Fprintf(p.Out, "\r%5.1f%%  %d/%d MiB  %.1f MiB/s  ETA %s",
		pct, done/(1024*1024), p.Total/(1024*1024), speedMiBs, etaStr)
}
```

- [ ] **Step 4: Run, confirm pass**

```bash
go test ./internal/download/...
```

- [ ] **Step 5: Commit**

```bash
git add internal/download/progress.go internal/download/progress_test.go
git commit -m "feat(download): TTY-aware progress writer"
```

---

## Task 9: `internal/download` — Downloader (flock + resume + SHA verify + atomic rename)

**Files:**
- Create: `internal/download/download.go`
- Create: `internal/download/download_test.go`

- [ ] **Step 1: Write the failing test**

`internal/download/download_test.go`:
```go
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/hf"
)

func mkBody(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// fakeRanger satisfies the Ranger interface for tests without a real HTTP server.
type fakeRanger struct {
	body []byte
	noRange bool
}

func (f *fakeRanger) FetchRange(ctx context.Context, repo, file string, offset, end int64, w io.Writer) error {
	if offset > 0 && f.noRange {
		return hf.ErrRangeNotSupported
	}
	if end == 0 || end > int64(len(f.body)) {
		end = int64(len(f.body))
	}
	_, err := w.Write(f.body[offset:end])
	return err
}

func TestDownloadHappyPath(t *testing.T) {
	dir := t.TempDir()
	body := mkBody(1024)
	dl := &Downloader{Ranger: &fakeRanger{body: body}}
	req := Request{
		RepoID:         "x/y",
		File:           "f.gguf",
		DestPath:       filepath.Join(dir, "f.gguf"),
		ExpectedSHA256: sha256hex(body),
		TotalSize:      int64(len(body)),
	}
	if err := dl.Get(context.Background(), req); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := os.ReadFile(req.DestPath)
	if err != nil || string(got) != string(body) {
		t.Errorf("dest mismatch (err=%v)", err)
	}
	if _, err := os.Stat(req.DestPath + ".partial"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".partial should be cleaned up; got err=%v", err)
	}
}

func TestDownloadResumeFromPartial(t *testing.T) {
	dir := t.TempDir()
	body := mkBody(1024)
	// Pre-seed partial with first 512 bytes.
	partial := filepath.Join(dir, "f.gguf.partial")
	if err := os.WriteFile(partial, body[:512], 0o644); err != nil {
		t.Fatal(err)
	}
	dl := &Downloader{Ranger: &fakeRanger{body: body}}
	req := Request{
		RepoID: "x/y", File: "f.gguf",
		DestPath:       filepath.Join(dir, "f.gguf"),
		ExpectedSHA256: sha256hex(body),
		TotalSize:      int64(len(body)),
	}
	if err := dl.Get(context.Background(), req); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := os.ReadFile(req.DestPath)
	if string(got) != string(body) {
		t.Errorf("resume produced wrong final content")
	}
}

func TestDownloadSHAMismatchUnlinksPartial(t *testing.T) {
	dir := t.TempDir()
	body := mkBody(64)
	dl := &Downloader{Ranger: &fakeRanger{body: body}}
	req := Request{
		RepoID: "x/y", File: "f.gguf",
		DestPath:       filepath.Join(dir, "f.gguf"),
		ExpectedSHA256: strings.Repeat("0", 64), // wrong
		TotalSize:      int64(len(body)),
	}
	err := dl.Get(context.Background(), req)
	if err == nil {
		t.Fatal("expected SHA mismatch error")
	}
	if _, err := os.Stat(req.DestPath + ".partial"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".partial should be unlinked after SHA mismatch")
	}
	if _, err := os.Stat(req.DestPath); err == nil {
		t.Errorf("dest should not exist after SHA mismatch")
	}
}

func TestDownloadNoRangeRestartsFromZero(t *testing.T) {
	dir := t.TempDir()
	body := mkBody(256)
	partial := filepath.Join(dir, "f.gguf.partial")
	_ = os.WriteFile(partial, body[:128], 0o644) // garbage from a previous run

	dl := &Downloader{Ranger: &fakeRanger{body: body, noRange: true}}
	req := Request{
		RepoID: "x/y", File: "f.gguf",
		DestPath:       filepath.Join(dir, "f.gguf"),
		ExpectedSHA256: sha256hex(body),
		TotalSize:      int64(len(body)),
	}
	if err := dl.Get(context.Background(), req); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := os.ReadFile(req.DestPath)
	if string(got) != string(body) {
		t.Errorf("after no-range restart, content mismatch")
	}
}

func TestDownloadFlockSerializesTwoCallers(t *testing.T) {
	// Two concurrent Get() calls for the same dest must serialize: the
	// second blocks on flock, then sees the final file with matching SHA
	// and short-circuits (dedupe fast path inside Get).
	dir := t.TempDir()
	body := mkBody(2048)
	// Slow Ranger: blocks on a channel so test can sequence the race.
	gate := make(chan struct{})
	slow := &slowRanger{body: body, gate: gate}
	dl := &Downloader{Ranger: slow}
	req := Request{
		RepoID: "x/y", File: "f.gguf",
		DestPath:       filepath.Join(dir, "f.gguf"),
		ExpectedSHA256: sha256hex(body),
		TotalSize:      int64(len(body)),
	}

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make([]error, 2)
	go func() { defer wg.Done(); errs[0] = dl.Get(context.Background(), req) }()
	// Brief wait so caller 0 grabs the lock first.
	time.Sleep(50 * time.Millisecond)
	go func() { defer wg.Done(); errs[1] = dl.Get(context.Background(), req) }()
	// Let caller 0 finish writing.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()
	if errs[0] != nil {
		t.Errorf("caller 0: %v", errs[0])
	}
	if errs[1] != nil {
		t.Errorf("caller 1: %v", errs[1])
	}
	if got, _ := os.ReadFile(req.DestPath); string(got) != string(body) {
		t.Errorf("final file content mismatch")
	}
}

// slowRanger blocks on gate before writing, so caller-1 has time to contend.
type slowRanger struct {
	body []byte
	gate chan struct{}
}

func (s *slowRanger) FetchRange(ctx context.Context, repo, file string, offset, end int64, w io.Writer) error {
	<-s.gate
	if end == 0 {
		end = int64(len(s.body))
	}
	_, err := w.Write(s.body[offset:end])
	return err
}

// Sanity test that httptest + Downloader integrate over real HTTP (one minimal case).
func TestDownloadOverHTTPTest(t *testing.T) {
	body := mkBody(2048)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ignore Range; serve full body. Real client wraps hf.Client which
		// detects this and restarts; this fake leaves that path covered
		// by the fakeRanger test above.
		w.WriteHeader(200)
		w.Write(body)
	}))
	t.Cleanup(ts.Close)
	c := hf.NewClient(ts.URL, hf.NewCache(t.TempDir()), nil)
	dl := &Downloader{Ranger: c}
	dir := t.TempDir()
	req := Request{
		RepoID: "x/y", File: "f.gguf",
		DestPath:       filepath.Join(dir, "f.gguf"),
		ExpectedSHA256: sha256hex(body),
		TotalSize:      int64(len(body)),
	}
	if err := dl.Get(context.Background(), req); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if size, _ := fileSize(req.DestPath); size != int64(len(body)) {
		t.Errorf("got size %d, want %d", size, len(body))
	}
}

func fileSize(p string) (int64, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/download/...
```
Expected: undefined symbols.

- [ ] **Step 3: Write `internal/download/download.go`**

```go
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gregmundy/llamactl/internal/hf"
	"golang.org/x/sys/unix"
)

// Ranger is the network seam — satisfied by *hf.Client. Declared locally so
// internal/download does not depend on the broader cli.HFClient interface.
type Ranger interface {
	FetchRange(ctx context.Context, repoID, file string, offset, end int64, w io.Writer) error
}

// Request is one download job.
type Request struct {
	RepoID         string
	File           string // .gguf filename on HF
	DestPath       string // final on-disk path
	ExpectedSHA256 string // hex
	TotalSize      int64  // for progress; 0 disables
	Progress       *Progress // optional
}

// Downloader orchestrates a single Get: lock -> resume -> stream -> verify -> rename.
type Downloader struct {
	Ranger Ranger
}

// Get fetches DestPath from RepoID/File, resuming a .partial if present,
// verifying SHA256, and atomically renaming on success.
//
// If DestPath already exists and its on-disk SHA matches ExpectedSHA256,
// returns nil immediately (dedupe fast path — PRD AC#7).
func (d *Downloader) Get(ctx context.Context, req Request) error {
	if existing, err := verifyExisting(req.DestPath, req.ExpectedSHA256); err == nil && existing {
		return nil
	}

	partial := req.DestPath + ".partial"
	if err := os.MkdirAll(filepath.Dir(req.DestPath), 0o755); err != nil {
		return fmt.Errorf("mkdir dest: %w", err)
	}
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open partial: %w", err)
	}
	defer f.Close()

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("flock partial: %w", err)
	}
	// Release happens on close via defer.

	// Another process may have just finished while we waited on the lock.
	if existing, err := verifyExisting(req.DestPath, req.ExpectedSHA256); err == nil && existing {
		return nil
	}

	// Compute resume offset = current partial size.
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat partial: %w", err)
	}
	resumeOffset := fi.Size()

	h := sha256.New()
	if resumeOffset > 0 {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("seek partial: %w", err)
		}
		if _, err := io.CopyN(h, f, resumeOffset); err != nil {
			return fmt.Errorf("re-hash partial: %w", err)
		}
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return fmt.Errorf("seek to end: %w", err)
		}
	}

	if req.Progress != nil {
		req.Progress.Initial = resumeOffset
	}

	writers := []io.Writer{f, h}
	if req.Progress != nil {
		writers = append(writers, req.Progress)
	}
	mw := io.MultiWriter(writers...)

	err = d.Ranger.FetchRange(ctx, req.RepoID, req.File, resumeOffset, 0, mw)
	if errors.Is(err, hf.ErrRangeNotSupported) {
		// Restart from zero: truncate partial, reset hash, re-stream.
		if err := f.Truncate(0); err != nil {
			return fmt.Errorf("truncate for restart: %w", err)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		h.Reset()
		if req.Progress != nil {
			req.Progress.Initial = 0
		}
		err = d.Ranger.FetchRange(ctx, req.RepoID, req.File, 0, 0, io.MultiWriter(f, h))
	}
	if err != nil {
		return fmt.Errorf("fetch range: %w", err)
	}

	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync partial: %w", err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	if got != req.ExpectedSHA256 {
		_ = os.Remove(partial)
		return fmt.Errorf("sha256 mismatch: got %s, want %s", got, req.ExpectedSHA256)
	}

	// Close (releasing the lock) before rename so any contending process
	// sees a clean state.
	if err := f.Close(); err != nil {
		return fmt.Errorf("close partial: %w", err)
	}
	if err := os.Rename(partial, req.DestPath); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", partial, req.DestPath, err)
	}
	return nil
}

// verifyExisting returns (true, nil) if path exists and its sha256 == expected.
// Returns (false, nil) if missing. Other errors propagate.
func verifyExisting(path, expectedHex string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	got := hex.EncodeToString(h.Sum(nil))
	return got == expectedHex, nil
}
```

- [ ] **Step 4: Add `golang.org/x/sys` dependency**

```bash
go get golang.org/x/sys/unix
```

- [ ] **Step 5: Run, confirm pass**

```bash
go test ./internal/download/...
```

- [ ] **Step 6: Commit**

```bash
git add internal/download/download.go internal/download/download_test.go go.mod go.sum
git commit -m "feat(download): flock + resume + SHA verify + atomic rename"
```

---

## Task 10: Extend `internal/config/paths.go` + `internal/cli/deps.go`

**Files:**
- Modify: `internal/config/paths.go`
- Modify: `internal/cli/deps.go`

- [ ] **Step 1: Read existing files**

```bash
cat internal/config/paths.go
cat internal/cli/deps.go
```

Phase 1's `config.Paths` already covers everything Phase 2 needs:
- `Paths.DataDir()` → `~/.local/share/llama-models` (shared GGUF dir, PRD §5)
- `Paths.ModelsMetaDir()` → `~/.config/llamactl/models` (per-tool metadata)
- `Paths.CacheDir()` → `~/.cache/llamactl` (HF API cache)
- `Paths.HardwareJSON()` → `~/.config/llamactl/hardware.json`

**No new path methods are needed.** Skip to Step 3.

- [ ] **Step 3: Write the failing test for new Deps fields**

Add to `internal/cli/deps.go`'s test file (or create a tiny test verifying the struct exists):

`internal/cli/deps_test.go`:
```go
package cli

import "testing"

func TestDepsHasPhase2Fields(t *testing.T) {
	d := Deps{}
	// Compile-time check: these field accesses must compile.
	_ = d.HFClient
	_ = d.Downloader
	_ = d.QuantSelector
	_ = d.ModelStore
	_ = d.FS
	_ = d.ModelsConfigDir
	_ = d.SharedModelsDir
	_ = d.HFCacheDir
}
```

- [ ] **Step 4: Extend `internal/cli/deps.go`**

Add new imports:
```go
import (
	// ... existing imports ...
	"os"
	"github.com/gregmundy/llamactl/internal/download"
	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/models"
)
```

Add interfaces below the existing `ServerProber`:

```go
// HFClient is the HuggingFace API + bytes seam.
type HFClient interface {
	Search(ctx context.Context, query string) ([]hf.SearchHit, error)
	SearchRefresh(ctx context.Context, query string) ([]hf.SearchHit, error)
	RepoInfo(ctx context.Context, repoID string) (hf.Repo, error)
	FetchRange(ctx context.Context, repoID, file string, offset, end int64, w io.Writer) error
}

// Downloader resolves a single Request → on-disk GGUF.
type Downloader interface {
	Get(ctx context.Context, req download.Request) error
}

// QuantSelector picks the best-fitting quant for a model on a host.
type QuantSelector interface {
	Select(model models.Model, hw hardware.Info, targetCtx int) (models.Quant, error)
}

// ModelStore is per-tool metadata storage.
type ModelStore interface {
	List(ctx context.Context) ([]models.Metadata, error)
	Get(ctx context.Context, id string) (models.Metadata, error)
	Put(ctx context.Context, m models.Metadata) error
	Delete(ctx context.Context, id string) error
}

// FileSystem is a narrow seam for the disk operations cli/add/list/remove need.
type FileSystem interface {
	Stat(path string) (os.FileInfo, error)
	Remove(path string) error
	MkdirAll(path string, perm os.FileMode) error
}
```

Add `hardware.Info` import if not present:
```go
import "github.com/gregmundy/llamactl/internal/hardware"
```

Extend the `Deps` struct (append fields, don't reorder):

```go
type Deps struct {
	// ... existing fields ...

	HFClient      HFClient
	Downloader    Downloader
	QuantSelector QuantSelector
	ModelStore    ModelStore
	FS            FileSystem

	ModelsConfigDir string
	SharedModelsDir string
	HFCacheDir      string
}
```

Also add a tiny adapter type — `osFS` — at the bottom of `deps.go` so `main.go` can wire a real filesystem:

```go
// OSFileSystem is the production FileSystem backed by package os.
type OSFileSystem struct{}

func (OSFileSystem) Stat(p string) (os.FileInfo, error)    { return os.Stat(p) }
func (OSFileSystem) Remove(p string) error                 { return os.Remove(p) }
func (OSFileSystem) MkdirAll(p string, m os.FileMode) error { return os.MkdirAll(p, m) }

// SelectorAdapter wraps the package-level models.SelectQuant in the
// QuantSelector interface.
type SelectorAdapter struct{}

func (SelectorAdapter) Select(m models.Model, hi hardware.Info, ctx int) (models.Quant, error) {
	return models.SelectQuant(m, hi, ctx)
}
```

- [ ] **Step 5: Run all tests**

```bash
go build ./...
go test ./...
```
Expected: still passing — no command consumes the new fields yet.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/deps.go internal/cli/deps_test.go
git commit -m "feat(cli): Deps interfaces for Phase 2 commands"
```

---

## Task 11: `internal/cli` — `add` command

This is the largest task. Decomposed into substeps with TDD.

**Files:**
- Create: `internal/cli/add.go`
- Create: `internal/cli/add_test.go`

- [ ] **Step 1: Write fakes shared with later test files**

Create `internal/cli/fakes_test.go`:
```go
package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/gregmundy/llamactl/internal/download"
	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/models"
)

// fakeHFClient is a controllable HFClient for tests.
type fakeHFClient struct {
	SearchHits map[string][]hf.SearchHit
	Repos      map[string]hf.Repo
	Bytes      map[string][]byte // key = repoID + "/" + file
	FetchCalls int
}

func (f *fakeHFClient) Search(ctx context.Context, q string) ([]hf.SearchHit, error) {
	return f.SearchHits[q], nil
}
func (f *fakeHFClient) SearchRefresh(ctx context.Context, q string) ([]hf.SearchHit, error) {
	return f.SearchHits[q], nil
}
func (f *fakeHFClient) RepoInfo(ctx context.Context, repoID string) (hf.Repo, error) {
	r, ok := f.Repos[repoID]
	if !ok {
		return hf.Repo{}, errors.New("404")
	}
	return r, nil
}
func (f *fakeHFClient) FetchRange(ctx context.Context, repoID, file string, off, end int64, w io.Writer) error {
	f.FetchCalls++
	b, ok := f.Bytes[repoID+"/"+file]
	if !ok {
		return errors.New("404")
	}
	if end == 0 {
		end = int64(len(b))
	}
	_, err := w.Write(b[off:end])
	return err
}

// fakeDownloader does its own (in-process) "download" by calling the
// underlying fakeHFClient and writing the file directly. We exercise the
// real Downloader path in integration_test.go via httptest, so this fake
// just lets us assert "Downloader.Get was called with X" or "skipped".
type fakeDownloader struct {
	HFClient *fakeHFClient
	Calls    []download.Request
}

func (f *fakeDownloader) Get(ctx context.Context, req download.Request) error {
	f.Calls = append(f.Calls, req)
	if f.HFClient == nil {
		return nil
	}
	body, ok := f.HFClient.Bytes[req.RepoID+"/"+req.File]
	if !ok {
		return errors.New("fakeDownloader: no body for " + req.RepoID + "/" + req.File)
	}
	if err := os.MkdirAll(filepathDir(req.DestPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(req.DestPath, body, 0o644)
}

func filepathDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}

// fakeHardwareDetector returns a pinned Info.
type fakeHardwareDetector struct{ Info hardware.Info }

func (f fakeHardwareDetector) Detect(ctx context.Context) (hardware.Info, error) {
	return f.Info, nil
}

// fakeClock for deterministic AddedAt.
func fakeNow() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) }

// fakeModelStore is an in-memory ModelStore.
type fakeModelStore struct {
	M map[string]models.Metadata
}

func newFakeModelStore() *fakeModelStore { return &fakeModelStore{M: map[string]models.Metadata{}} }

func (s *fakeModelStore) List(_ context.Context) ([]models.Metadata, error) {
	out := make([]models.Metadata, 0, len(s.M))
	for _, m := range s.M {
		out = append(out, m)
	}
	return out, nil
}
func (s *fakeModelStore) Get(_ context.Context, id string) (models.Metadata, error) {
	m, ok := s.M[id]
	if !ok {
		return models.Metadata{}, models.ErrNotFound
	}
	return m, nil
}
func (s *fakeModelStore) Put(_ context.Context, m models.Metadata) error {
	s.M[m.ID] = m
	return nil
}
func (s *fakeModelStore) Delete(_ context.Context, id string) error {
	if _, ok := s.M[id]; !ok {
		return models.ErrNotFound
	}
	delete(s.M, id)
	return nil
}
```

- [ ] **Step 2: Write the failing `add_test.go`**

```go
package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/models"
)

func makeDeps(t *testing.T) (*Deps, *fakeHFClient, *fakeDownloader, *fakeModelStore, string) {
	t.Helper()
	shared := t.TempDir()
	configDir := t.TempDir()
	body := []byte("fake gguf bytes for testing")
	sum := sha256.Sum256(body)
	shaHex := hex.EncodeToString(sum[:])

	hfc := &fakeHFClient{
		Repos: map[string]hf.Repo{
			"Qwen/Qwen2.5-7B-Instruct-GGUF": {
				ID: "Qwen/Qwen2.5-7B-Instruct-GGUF",
				Siblings: []hf.File{
					{RFilename: "qwen2.5-7b-instruct-q4_k_m.gguf", LFS: &hf.LFSInfo{SHA256: shaHex, Size: int64(len(body))}},
				},
			},
		},
		Bytes: map[string][]byte{
			"Qwen/Qwen2.5-7B-Instruct-GGUF/qwen2.5-7b-instruct-q4_k_m.gguf": body,
		},
	}
	dl := &fakeDownloader{HFClient: hfc}
	store := newFakeModelStore()

	d := &Deps{
		Stdout:           &out,
		Stderr:           &out,
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 16 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(configDir, "hardware.json"),
		HFClient:         hfc,
		Downloader:       dl,
		QuantSelector:    SelectorAdapter{},
		ModelStore:       store,
		FS:               OSFileSystem{},
		ModelsConfigDir:  filepath.Join(configDir, "models"),
		SharedModelsDir:  shared,
		Now:              fakeNow,
	}
	return d, hfc, dl, store, shared
}

func TestAddHappyPath(t *testing.T) {
	d, _, dl, store, shared := makeDeps(t)
	if _, _, err := runRoot(t, d, "add", "qwen2.5-7b-instruct"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(dl.Calls) != 1 {
		t.Errorf("Downloader.Get call count = %d, want 1", len(dl.Calls))
	}
	if _, ok := store.M["qwen2.5-7b-instruct"]; !ok {
		t.Errorf("Metadata not persisted")
	}
	want := filepath.Join(shared, "qwen2.5-7b-instruct", "Q4_K_M.gguf")
	if dl.Calls[0].DestPath != want {
		t.Errorf("DestPath = %q, want %q", dl.Calls[0].DestPath, want)
	}
}

func TestAddDedupesIfFileAlreadyPresent(t *testing.T) {
	d, hfc, dl, _, shared := makeDeps(t)
	body := hfc.Bytes["Qwen/Qwen2.5-7B-Instruct-GGUF/qwen2.5-7b-instruct-q4_k_m.gguf"]
	dest := filepath.Join(shared, "qwen2.5-7b-instruct", "Q4_K_M.gguf")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runRoot(t, d, "add", "qwen2.5-7b-instruct"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(dl.Calls) != 0 {
		t.Errorf("Downloader should not be called when SHA matches; got %d calls", len(dl.Calls))
	}
}

func TestAddUnknownModelErrors(t *testing.T) {
	d, _, _, _, _ := makeDeps(t)
	_, _, err := runRoot(t, d, "add", "nope")
	if err == nil || !strings.Contains(err.Error(), "available") {
		t.Fatalf("err = %v", err)
	}
}

func TestAddQuantOverride(t *testing.T) {
	d, hfc, dl, _, _ := makeDeps(t)
	// Add a second sibling so --quant Q5_K_M resolves.
	body := []byte("alt bytes")
	sum := sha256.Sum256(body)
	hfc.Repos["Qwen/Qwen2.5-7B-Instruct-GGUF"] = hf.Repo{
		ID: "Qwen/Qwen2.5-7B-Instruct-GGUF",
		Siblings: []hf.File{
			{RFilename: "qwen2.5-7b-instruct-q4_k_m.gguf", LFS: &hf.LFSInfo{SHA256: "0", Size: 1}},
			{RFilename: "qwen2.5-7b-instruct-q5_k_m.gguf", LFS: &hf.LFSInfo{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))}},
		},
	}
	hfc.Bytes["Qwen/Qwen2.5-7B-Instruct-GGUF/qwen2.5-7b-instruct-q5_k_m.gguf"] = body
	if _, _, err := runRoot(t, d, "add", "qwen2.5-7b-instruct", "--quant", "Q5_K_M"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(dl.Calls) != 1 || !strings.Contains(dl.Calls[0].File, "q5_k_m") {
		t.Errorf("expected Q5_K_M file; got calls=%+v", dl.Calls)
	}
}

func TestAddNoQuantFitsErrors(t *testing.T) {
	d, _, _, _, _ := makeDeps(t)
	// 8 GB host can't fit llama3.3-70b.
	d.HardwareDetector = fakeHardwareDetector{Info: hardware.Info{RAMBytes: 8 * (1 << 30)}}
	_, _, err := runRoot(t, d, "add", "llama3.3-70b")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "quant") {
		t.Fatalf("err = %v", err)
	}
}

func TestAddBootstrapsHardwareJSON(t *testing.T) {
	d, _, _, _, _ := makeDeps(t)
	if _, err := os.Stat(d.HardwareJSONPath); err == nil {
		t.Fatal("precondition: hardware.json should not exist yet")
	}
	if _, _, err := runRoot(t, d, "add", "qwen2.5-7b-instruct"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := os.Stat(d.HardwareJSONPath); err != nil {
		t.Errorf("hardware.json should be auto-written; err=%v", err)
	}
}

var _ = models.Q4_K_M // touch the package so the import is exercised
```

- [ ] **Step 3: Run, confirm fail**

```bash
go test ./internal/cli/... -run TestAdd
```
Expected: `unknown command "add"` or similar.

- [ ] **Step 4: Write `internal/cli/add.go`**

```go
package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregmundy/llamactl/internal/download"
	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newAddCmd(d *Deps) *cobra.Command {
	var quantOverride string
	var targetCtx int
	cmd := &cobra.Command{
		Use:   "add <model-id>",
		Short: "Download a whitelisted model and write metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd.Context(), d, args[0], quantOverride, targetCtx)
		},
	}
	cmd.Flags().StringVar(&quantOverride, "quant", "", "override automatic quant selection")
	cmd.Flags().IntVar(&targetCtx, "ctx", 8192, "target context size for quant calculation")
	return cmd
}

func runAdd(ctx context.Context, d *Deps, id, quantOverride string, targetCtx int) error {
	model, err := models.LookupOrSuggest(id)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUserError, err)
	}

	hw, err := ensureHardware(ctx, d)
	if err != nil {
		return err
	}

	var quant models.Quant
	if quantOverride != "" {
		if !isKnownQuant(quantOverride) {
			return fmt.Errorf("%w: unknown --quant %q (known: %s)", ErrUserError, quantOverride, knownQuantsList())
		}
		quant = models.Quant(quantOverride)
	} else {
		quant, err = d.QuantSelector.Select(model, hw, targetCtx)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrUserError, err)
		}
	}

	repo, err := d.HFClient.RepoInfo(ctx, model.HFRepo)
	if err != nil {
		return fmt.Errorf("fetch HF repo info: %w", err)
	}
	file, expectedSHA, totalSize, err := findQuantFile(repo, quant)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUserError, err)
	}

	destDir := filepath.Join(d.SharedModelsDir, model.ID)
	destPath := filepath.Join(destDir, string(quant)+".gguf")

	// Dedupe fast path: file present + on-disk SHA matches → no download.
	if existing, _ := sha256OfFileIfExists(destPath); existing == expectedSHA {
		fmt.Fprintf(d.Stdout, "already present (matched SHA): %s\n", destPath)
	} else {
		req := download.Request{
			RepoID:         model.HFRepo,
			File:           file,
			DestPath:       destPath,
			ExpectedSHA256: expectedSHA,
			TotalSize:      totalSize,
			Progress:       newProgress(d, totalSize),
		}
		if err := d.Downloader.Get(ctx, req); err != nil {
			return fmt.Errorf("download: %w", err)
		}
	}

	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	meta := models.Metadata{
		ID:        model.ID,
		Repo:      model.HFRepo,
		Quant:     quant,
		SHA256:    expectedSHA,
		GGUFPath:  destPath,
		SizeBytes: totalSize,
		AddedAt:   now(),
	}
	if err := d.ModelStore.Put(ctx, meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	fmt.Fprintf(d.Stdout, "installed %s (%s, %s) -> %s\n",
		model.ID, quant, humanBytes(totalSize), destPath)
	return nil
}

// ensureHardware reads hardware.json if present, else runs the detector and
// persists the result (auto-bootstrap, per the spec). Mirrors the marshal +
// write logic used by internal/cli/hardware.go in Phase 1; we intentionally
// duplicate the few lines rather than introduce a config.WriteHardware
// helper that no other caller would use.
func ensureHardware(ctx context.Context, d *Deps) (hardware.Info, error) {
	data, err := os.ReadFile(d.HardwareJSONPath)
	if err == nil {
		var info hardware.Info
		if jerr := json.Unmarshal(data, &info); jerr == nil {
			return info, nil
		}
		// fall through on decode error and re-detect
	} else if !errors.Is(err, fs.ErrNotExist) {
		return hardware.Info{}, fmt.Errorf("read hardware.json: %w", err)
	}
	info, derr := d.HardwareDetector.Detect(ctx)
	if derr != nil {
		return hardware.Info{}, fmt.Errorf("detect hardware: %w", derr)
	}
	if err := os.MkdirAll(filepath.Dir(d.HardwareJSONPath), 0o755); err != nil {
		fmt.Fprintf(d.Stderr, "llamactl: warning: mkdir for hardware.json: %v\n", err)
		return info, nil
	}
	b, mErr := json.MarshalIndent(info, "", "  ")
	if mErr != nil {
		fmt.Fprintf(d.Stderr, "llamactl: warning: marshal hardware.json: %v\n", mErr)
		return info, nil
	}
	if werr := os.WriteFile(d.HardwareJSONPath, b, 0o644); werr != nil {
		fmt.Fprintf(d.Stderr, "llamactl: warning: persist hardware.json: %v\n", werr)
	}
	return info, nil
}

// findQuantFile looks for a sibling whose filename contains the quant
// (case-insensitive) and is a .gguf file. Rejects multi-shard (-N-of-M).
func findQuantFile(repo hf.Repo, quant models.Quant) (file, sha string, size int64, err error) {
	qLower := strings.ToLower(string(quant))
	available := make([]string, 0, len(repo.Siblings))
	for _, s := range repo.Siblings {
		if !strings.HasSuffix(strings.ToLower(s.RFilename), ".gguf") {
			continue
		}
		// Capture quants seen for the error message.
		available = append(available, s.RFilename)
		if !strings.Contains(strings.ToLower(s.RFilename), qLower) {
			continue
		}
		if strings.Contains(s.RFilename, "-of-") {
			return "", "", 0, fmt.Errorf("multi-shard GGUF (%s) not supported in v1", s.RFilename)
		}
		if s.LFS == nil || s.LFS.SHA256 == "" {
			return "", "", 0, fmt.Errorf("HF sibling %s missing lfs.sha256", s.RFilename)
		}
		return s.RFilename, s.LFS.SHA256, s.LFS.Size, nil
	}
	return "", "", 0, fmt.Errorf("no %s file in %s; available: %s", quant, repo.ID, strings.Join(available, ", "))
}

func isKnownQuant(q string) bool {
	for _, p := range models.PreferenceOrder {
		if string(p) == q {
			return true
		}
	}
	return false
}

func knownQuantsList() string {
	out := make([]string, 0, len(models.PreferenceOrder))
	for _, q := range models.PreferenceOrder {
		out = append(out, string(q))
	}
	return strings.Join(out, ", ")
}

func sha256OfFileIfExists(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func humanBytes(n int64) string {
	const u = 1024.0
	if n < int64(u) {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := u, 0
	for x := float64(n) / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/div, "KMGTPE"[exp])
}

// newProgress returns a Progress configured for the current stderr. Returns
// nil when stderr is not a TTY OR when totalSize is 0 (unknown).
func newProgress(d *Deps, totalSize int64) *download.Progress {
	if totalSize <= 0 {
		return nil
	}
	f, ok := d.Stderr.(*os.File)
	isTTY := ok && term.IsTerminal(int(f.Fd()))
	if !isTTY {
		return nil
	}
	return &download.Progress{Out: d.Stderr, Total: totalSize, IsTTY: true}
}

```

- [ ] **Step 5: Register `add` in `internal/cli/root.go`**

Find the existing `NewRoot` constructor; add to the list of `AddCommand` calls:
```go
root.AddCommand(newAddCmd(d))
```

- [ ] **Step 6: Run, confirm pass**

```bash
go test ./internal/cli/... -run TestAdd
```

- [ ] **Step 7: Commit**

```bash
git add internal/cli/add.go internal/cli/add_test.go internal/cli/fakes_test.go internal/cli/root.go internal/cli/deps.go
git commit -m "feat(cli): add command with auto-bootstrap, dedupe fast path, quant override"
```

---

## Task 12: `internal/cli` — `search` command

**Files:**
- Create: `internal/cli/search.go`
- Create: `internal/cli/search_test.go`

- [ ] **Step 1: Write the failing test**

```go
package cli

import (
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/hf"
)

func TestSearchFiltersToWhitelistAndFormatsTable(t *testing.T) {
	hfc := &fakeHFClient{
		SearchHits: map[string][]hf.SearchHit{
			"qwen": {
				{ID: "Qwen/Qwen2.5-7B-Instruct-GGUF"},
				{ID: "Qwen/SomeOtherRepo-NotWhitelisted"},
			},
		},
		Repos: map[string]hf.Repo{
			"Qwen/Qwen2.5-7B-Instruct-GGUF": {
				ID: "Qwen/Qwen2.5-7B-Instruct-GGUF",
				Siblings: []hf.File{
					{RFilename: "qwen2.5-7b-instruct-q4_k_m.gguf"},
					{RFilename: "qwen2.5-7b-instruct-q5_k_m.gguf"},
				},
			},
		},
	}
	d := &Deps{HFClient: hfc}

	out, _, err := runRoot(t, d, "search", "qwen")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "qwen2.5-7b-instruct") {
		t.Errorf("output missing whitelisted id; got:\n%s", out)
	}
	if strings.Contains(out, "SomeOtherRepo") {
		t.Errorf("output included non-whitelisted repo:\n%s", out)
	}
	if !strings.Contains(out, "Q4_K_M") || !strings.Contains(out, "Q5_K_M") {
		t.Errorf("output missing quants:\n%s", out)
	}
}

func TestSearchEmptyOK(t *testing.T) {
	hfc := &fakeHFClient{SearchHits: map[string][]hf.SearchHit{"x": {}}}
	d := &Deps{HFClient: hfc}
	out, _, err := runRoot(t, d, "search", "x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "no matches") {
		t.Errorf("expected 'no matches', got: %s", out)
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/cli/... -run TestSearch
```

- [ ] **Step 3: Write `internal/cli/search.go`**

```go
package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/spf13/cobra"
)

func newSearchCmd(d *Deps) *cobra.Command {
	var refresh bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search whitelisted models on HuggingFace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd.Context(), d, args[0], refresh)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "bypass the search cache")
	return cmd
}

func runSearch(ctx context.Context, d *Deps, query string, refresh bool) error {
	var hits []hf.SearchHit
	var err error
	if refresh {
		hits, err = d.HFClient.SearchRefresh(ctx, query)
	} else {
		hits, err = d.HFClient.Search(ctx, query)
	}
	if err != nil {
		return err
	}

	// Filter to whitelisted repos. Build a reverse index repo -> model.ID once.
	byRepo := make(map[string]models.Model, len(models.Whitelist))
	for _, m := range models.Whitelist {
		byRepo[m.HFRepo] = m
	}
	matched := make([]models.Model, 0, len(hits))
	seen := make(map[string]bool)
	for _, h := range hits {
		if m, ok := byRepo[h.ID]; ok && !seen[m.ID] {
			matched = append(matched, m)
			seen[m.ID] = true
		}
	}
	if len(matched) == 0 {
		fmt.Fprintln(d.Stdout, "no matches")
		return nil
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })

	tw := tabwriter.NewWriter(d.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL-ID\tPARAMS\tQUANTS\tREPO")
	for _, m := range matched {
		repo, rerr := d.HFClient.RepoInfo(ctx, m.HFRepo)
		quants := "(unknown)"
		if rerr == nil {
			quants = strings.Join(availableQuantsFromSiblings(repo.Siblings), ",")
		}
		fmt.Fprintf(tw, "%s\t%dB\t%s\t%s\n", m.ID, m.ParamsB, quants, m.HFRepo)
	}
	return tw.Flush()
}

// availableQuantsFromSiblings extracts which canonical quants appear in the
// repo's .gguf siblings (case-insensitive match). Returned in PreferenceOrder.
func availableQuantsFromSiblings(files []hf.File) []string {
	found := make(map[models.Quant]bool)
	for _, f := range files {
		low := strings.ToLower(f.RFilename)
		if !strings.HasSuffix(low, ".gguf") {
			continue
		}
		for _, q := range models.PreferenceOrder {
			if strings.Contains(low, strings.ToLower(string(q))) {
				found[q] = true
			}
		}
	}
	out := make([]string, 0, len(found))
	for _, q := range models.PreferenceOrder {
		if found[q] {
			out = append(out, string(q))
		}
	}
	return out
}
```

- [ ] **Step 4: Register in `internal/cli/root.go`**

Append to the `AddCommand` block: `root.AddCommand(newSearchCmd(d))`.

- [ ] **Step 5: Run, confirm pass**

```bash
go test ./internal/cli/...
```

- [ ] **Step 6: Commit**

```bash
git add internal/cli/search.go internal/cli/search_test.go internal/cli/root.go
git commit -m "feat(cli): search command with whitelist filter and tabwriter output"
```

---

## Task 13: `internal/cli` — `list` command

**Files:**
- Create: `internal/cli/list.go`
- Create: `internal/cli/list_test.go`

- [ ] **Step 1: Write the failing test**

`internal/cli/list_test.go`:
```go
package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/models"
)

func TestListShowsAllEntriesWithStatus(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "qwen.gguf")
	if err := os.WriteFile(existing, []byte("xxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID: "qwen2.5-7b-instruct", Quant: models.Q4_K_M, SHA256: "abc",
		GGUFPath: existing, SizeBytes: 3, AddedAt: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	})
	_ = store.Put(context.Background(), models.Metadata{
		ID: "llama3.1-8b", Quant: models.Q4_K_M, SHA256: "def",
		GGUFPath: filepath.Join(dir, "missing.gguf"), SizeBytes: 1, AddedAt: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	})

	d := &Deps{ModelStore: store, FS: OSFileSystem{}}
	out, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "qwen2.5-7b-instruct") || !strings.Contains(out, "llama3.1-8b") {
		t.Errorf("output missing models:\n%s", out)
	}
	if !strings.Contains(out, "(missing)") {
		t.Errorf("output should mark missing GGUF:\n%s", out)
	}
}

func TestListEmpty(t *testing.T) {
	d := &Deps{ModelStore: newFakeModelStore(), FS: OSFileSystem{}}
	out, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "no models installed") {
		t.Errorf("output:\n%s", out)
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/cli/... -run TestList
```

- [ ] **Step 3: Write `internal/cli/list.go`**

```go
package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newListCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed models",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), d)
		},
	}
}

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
	fmt.Fprintln(tw, "MODEL-ID\tQUANT\tSIZE\tPATH\tADDED")
	for _, m := range entries {
		size := humanBytes(m.SizeBytes)
		fi, statErr := d.FS.Stat(m.GGUFPath)
		switch {
		case statErr == nil:
			size = humanBytes(fi.Size())
		case errors.Is(statErr, fs.ErrNotExist):
			size = "(missing)"
		default:
			size = "(stat err)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			m.ID, m.Quant, size, m.GGUFPath, m.AddedAt.Format("2006-01-02"))
	}
	return tw.Flush()
}
```

- [ ] **Step 4: Register in `internal/cli/root.go`**

Append: `root.AddCommand(newListCmd(d))`.

- [ ] **Step 5: Run, confirm pass**

```bash
go test ./internal/cli/... -run TestList
```

- [ ] **Step 6: Commit**

```bash
git add internal/cli/list.go internal/cli/list_test.go internal/cli/root.go
git commit -m "feat(cli): list command with stat-driven (missing) marker"
```

---

## Task 14: `internal/cli` — `remove` command

**Files:**
- Create: `internal/cli/remove.go`
- Create: `internal/cli/remove_test.go`

- [ ] **Step 1: Write the failing test**

`internal/cli/remove_test.go`:
```go
package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/models"
)

func setupRemove(t *testing.T) (*Deps, *fakeModelStore, string) {
	t.Helper()
	dir := t.TempDir()
	gguf := filepath.Join(dir, "qwen.gguf")
	if err := os.WriteFile(gguf, []byte("xxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeModelStore()
	_ = store.Put(context.Background(), models.Metadata{
		ID: "qwen2.5-7b-instruct", Quant: models.Q4_K_M, GGUFPath: gguf, SizeBytes: 3,
	})
	d := &Deps{ModelStore: store, FS: OSFileSystem{}}
	return d, store, gguf
}

func TestRemoveMetadataOnly(t *testing.T) {
	d, store, gguf := setupRemove(t)
	if _, _, err := runRoot(t, d, "remove", "qwen2.5-7b-instruct"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := store.Get(context.Background(), "qwen2.5-7b-instruct"); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("metadata still present")
	}
	if _, err := os.Stat(gguf); err != nil {
		t.Errorf("GGUF should remain (no --purge); err=%v", err)
	}
}

func TestRemovePurgeDeletesGGUF(t *testing.T) {
	d, store, gguf := setupRemove(t)
	if _, _, err := runRoot(t, d, "remove", "qwen2.5-7b-instruct", "--purge"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := os.Stat(gguf); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GGUF should be deleted; err=%v", err)
	}
	if _, err := store.Get(context.Background(), "qwen2.5-7b-instruct"); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("metadata still present")
	}
}

func TestRemovePurgeRefusesIfPartialExists(t *testing.T) {
	d, _, gguf := setupRemove(t)
	if err := os.WriteFile(gguf+".partial", []byte("incomplete"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := runRoot(t, d, "remove", "qwen2.5-7b-instruct", "--purge")
	if err == nil || !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("err = %v, want 'in progress'", err)
	}
}

func TestRemoveUnknownModelErrors(t *testing.T) {
	d, _, _ := setupRemove(t)
	_, _, err := runRoot(t, d, "remove", "nope")
	if err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Run, confirm fail**

```bash
go test ./internal/cli/... -run TestRemove
```

- [ ] **Step 3: Write `internal/cli/remove.go`**

```go
package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/gregmundy/llamactl/internal/models"
	"github.com/spf13/cobra"
)

func newRemoveCmd(d *Deps) *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use:   "remove <model-id>",
		Short: "Remove llamactl metadata for a model (use --purge to also delete the GGUF)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(cmd.Context(), d, args[0], purge)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete the shared GGUF file (best-effort cross-tool check)")
	return cmd
}

func runRemove(ctx context.Context, d *Deps, id string, purge bool) error {
	m, err := d.ModelStore.Get(ctx, id)
	if errors.Is(err, models.ErrNotFound) {
		return fmt.Errorf("%w: model %q is not installed", ErrUserError, id)
	}
	if err != nil {
		return err
	}

	if purge {
		if _, statErr := d.FS.Stat(m.GGUFPath + ".partial"); statErr == nil {
			return fmt.Errorf("%w: %s.partial exists — download in progress; aborting --purge", ErrUserError, m.GGUFPath)
		}
		fmt.Fprintf(d.Stderr,
			"llamactl: best-effort: cannot detect other tools' use of %s; deleting anyway\n",
			m.GGUFPath)
		if err := d.FS.Remove(m.GGUFPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove gguf: %w", err)
		}
		// Try to remove the (now likely empty) parent directory; ignore failure.
		_ = d.FS.Remove(filepath.Dir(m.GGUFPath))
	}

	if err := d.ModelStore.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete metadata: %w", err)
	}

	if purge {
		fmt.Fprintf(d.Stdout, "removed %s and deleted %s\n", id, m.GGUFPath)
	} else {
		fmt.Fprintf(d.Stdout, "removed %s metadata (GGUF preserved at %s)\n", id, m.GGUFPath)
	}
	return nil
}
```

- [ ] **Step 4: Register in `internal/cli/root.go`**

Append: `root.AddCommand(newRemoveCmd(d))`.

- [ ] **Step 5: Run, confirm pass**

```bash
go test ./internal/cli/... -run TestRemove
```

- [ ] **Step 6: Commit**

```bash
git add internal/cli/remove.go internal/cli/remove_test.go internal/cli/root.go
git commit -m "feat(cli): remove command with --purge and partial-file guard"
```

---

## Task 15: Wire concrete dependencies in `cmd/llamactl/main.go`

**Files:**
- Modify: `cmd/llamactl/main.go`

- [ ] **Step 1: Read existing main.go**

```bash
cat cmd/llamactl/main.go
```

- [ ] **Step 2: Add Phase 2 concrete construction**

Phase 1's `cmd/llamactl/main.go` already constructs `paths, err := config.New()` and uses its methods. **First, read main.go to confirm the actual variable name (likely `paths` or `cfgPaths`)** — adjust the references below accordingly.

After the existing `Deps` initialization (where `HardwareDetector`, `ServerResolver`, etc. are set), add:

```go
// Phase 2 wiring. `paths` is the *config.Paths Phase 1 already constructed.
hfCache := hf.NewCache(paths.CacheDir())
hfClient := hf.NewClient("https://huggingface.co", hfCache, nil)
if tok := firstNonEmptyEnv("LLAMACTL_HF_TOKEN", "HF_TOKEN"); tok != "" {
    hfClient = hfClient.WithToken(tok)
}

deps.HFClient = hfClient
deps.Downloader = &download.Downloader{Ranger: hfClient}
deps.QuantSelector = cli.SelectorAdapter{}
deps.ModelStore = models.NewFileStore(paths.ModelsMetaDir())
deps.FS = cli.OSFileSystem{}
deps.ModelsConfigDir = paths.ModelsMetaDir()
deps.SharedModelsDir = paths.DataDir() // ~/.local/share/llama-models per PRD §5
deps.HFCacheDir = paths.CacheDir()
```

Add imports:
```go
"github.com/gregmundy/llamactl/internal/download"
"github.com/gregmundy/llamactl/internal/hf"
"github.com/gregmundy/llamactl/internal/models"
```

(`internal/cli` and `internal/config` are already imported by Phase 1.)

Add the helper at the bottom of main.go:
```go
func firstNonEmptyEnv(keys ...string) string {
    for _, k := range keys {
        if v := os.Getenv(k); v != "" {
            return v
        }
    }
    return ""
}
```

- [ ] **Step 3: Build**

```bash
go build ./cmd/llamactl
```
Expected: no output, binary `./llamactl` exists.

- [ ] **Step 4: Smoke-test the wired binary**

```bash
./llamactl --help
```
Expected: lists `add`, `search`, `list`, `remove` alongside Phase 1 commands.

```bash
./llamactl add nope 2>&1; echo "exit=$?"
```
Expected: error containing "available", exit 2.

- [ ] **Step 5: Commit**

```bash
git add cmd/llamactl/main.go
git commit -m "feat(cli): wire HFClient, Downloader, QuantSelector, ModelStore in main"
```

---

## Task 16: End-to-end integration test

**Files:**
- Modify: `internal/cli/integration_test.go`

- [ ] **Step 1: Append the Phase 2 flow to integration_test.go**

```go
func TestIntegrationPhase2AddListRemove(t *testing.T) {
	body := []byte("integration bytes")
	sum := sha256.Sum256(body)
	shaHex := hex.EncodeToString(sum[:])

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/models/Qwen/Qwen2.5-7B-Instruct-GGUF"):
			fmt.Fprintf(w, `{"id":"Qwen/Qwen2.5-7B-Instruct-GGUF","siblings":[{"rfilename":"qwen2.5-7b-instruct-q4_k_m.gguf","lfs":{"sha256":"%s","size":%d}}]}`, shaHex, len(body))
		case strings.Contains(r.URL.Path, "/resolve/main/"):
			rng := r.Header.Get("Range")
			off := int64(0)
			if rng != "" {
				_, _ = fmt.Sscanf(rng, "bytes=%d-", &off)
				w.WriteHeader(http.StatusPartialContent)
			}
			w.Write(body[off:])
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(ts.Close)

	configDir := t.TempDir()
	sharedDir := t.TempDir()
	cacheDir := t.TempDir()

	hfClient := hf.NewClient(ts.URL, hf.NewCache(cacheDir), nil)
	store := models.NewFileStore(filepath.Join(configDir, "models"))

	d := &Deps{
		HardwareDetector: fakeHardwareDetector{Info: hardware.Info{RAMBytes: 16 * (1 << 30)}},
		HardwareJSONPath: filepath.Join(configDir, "hardware.json"),
		HFClient:         hfClient,
		Downloader:       &download.Downloader{Ranger: hfClient},
		QuantSelector:    SelectorAdapter{},
		ModelStore:       store,
		FS:               OSFileSystem{},
		ModelsConfigDir:  filepath.Join(configDir, "models"),
		SharedModelsDir:  sharedDir,
		Now:              fakeNow,
	}

	if _, _, err := runRoot(t, d, "add", "qwen2.5-7b-instruct"); err != nil {
		t.Fatalf("add: %v", err)
	}
	listOut, _, err := runRoot(t, d, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listOut, "qwen2.5-7b-instruct") {
		t.Errorf("list output missing model:\n%s", listOut)
	}
	// Verify dedupe on re-add: no second on-disk download (file size stable).
	gguf := filepath.Join(sharedDir, "qwen2.5-7b-instruct", "Q4_K_M.gguf")
	fi1, _ := os.Stat(gguf)
	if _, _, err := runRoot(t, d, "add", "qwen2.5-7b-instruct"); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	fi2, _ := os.Stat(gguf)
	if fi1.ModTime() != fi2.ModTime() {
		t.Errorf("re-add should not rewrite the file (dedupe fast path)")
	}
	// Remove --purge.
	if _, _, err := runRoot(t, d, "remove", "qwen2.5-7b-instruct", "--purge"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(gguf); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GGUF should be gone after --purge; err=%v", err)
	}
}
```

(Add the necessary imports at the top of `integration_test.go`: `crypto/sha256`, `encoding/hex`, `errors`, `fmt`, `net/http`, `net/http/httptest`, `os`, `path/filepath`, `strings`, plus `download`, `hf`, `models`, `hardware`.)

- [ ] **Step 2: Run, confirm pass**

```bash
go test ./internal/cli/... -run TestIntegrationPhase2 -v
```

- [ ] **Step 3: Full suite + vet**

```bash
go vet ./...
go test ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/cli/integration_test.go
git commit -m "test(cli): end-to-end add/list/remove integration with httptest"
```

---

## Task 17: Update README — Phase 2 status

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Read existing README**

```bash
cat README.md
```

- [ ] **Step 2: Replace the "Currently under construction" line with a status block**

```markdown
## Status

- **Phase 1 (shipped 2026-05-10):** `llamactl hardware`, `llamactl doctor`
- **Phase 2 (this branch):** `llamactl search`, `llamactl add`, `llamactl list`, `llamactl remove`
- **Phase 3 (future):** `llamactl serve` + launchd, `llamactl status`, `llamactl stop`

See `docs/llamactl-prd-v1.5.md` for the full spec.

### Phase 2 usage

```bash
llamactl search qwen2.5             # list whitelisted matches with available quants
llamactl add qwen2.5-7b-instruct    # auto-select quant for your host, download, verify SHA
llamactl list                       # show installed models
llamactl remove qwen2.5-7b-instruct --purge  # drop metadata + delete the shared GGUF
```

Set `HF_TOKEN` (or `LLAMACTL_HF_TOKEN`) if you hit HuggingFace rate limits.
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: README Phase 2 status + usage"
```

---

## Final verification

- [ ] **All tests pass**

```bash
go test ./...
go vet ./...
```

- [ ] **Binary works end-to-end (with network — manual smoke test)**

```bash
go build ./cmd/llamactl
./llamactl search qwen2.5
./llamactl add qwen2.5-3b-instruct      # smallest model, fastest smoke test
./llamactl list
./llamactl remove qwen2.5-3b-instruct --purge
```

- [ ] **Branch is ready for review/merge**

```bash
git log main..phase2-models --oneline
git diff main..phase2-models --stat
```

Expected: ~17 commits, all on `phase2-models`. None on `main`. Working tree clean.

---

## Notes for the executing agent

1. **Branch:** All work happens on `phase2-models`. Never `git checkout main`, never stash, never branch. If you find yourself on the wrong branch, stop and report — do not "fix" it.
2. **Spec is authoritative:** `docs/superpowers/specs/2026-05-10-phase2-models-design.md` is the contract. If a task instruction contradicts the spec, the spec wins — flag it and stop.
3. **Per-task verification:** After each task, run that task's tests and then the full suite (`go test ./...`). Both should pass before committing.
4. **Two-stage review:** For substantive tasks (4, 7, 9, 11, 16), the controller will dispatch a spec+quality review subagent before accepting the commit. Trivial tasks (1, 2, 3, 8, 13, 14, 17) are verified by direct file inspection.
5. **Type drift watch:** The interface names in `Deps` (`HFClient`, `Downloader`, `QuantSelector`, `ModelStore`, `FileSystem`) and the package-level type names (`hf.Client`, `download.Downloader`, etc.) deliberately differ — interfaces in `cli` are abstractions; concrete types live in their packages. Don't conflate them.

