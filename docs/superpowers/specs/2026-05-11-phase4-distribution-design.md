# llamactl Phase 4: Homebrew Distribution + Polish — Design Spec

**Status:** Approved 2026-05-11
**Ships as:** `v1.0.0`
**Covers:** PRD AC#1 (`brew install gregmundy/tap/llamactl` succeeds in under 30 s)
**Branch:** `phase4-polish-and-tap`

---

## 1. Goal

Take llamactl from "passes 15/16 acceptance criteria locally" to "publicly installable v1.0.0
that meets all 16." Three polish items address concerns the Phase 3 live smoke surfaced; the
distribution layer adds GitHub repo + GoReleaser + Homebrew tap + CI/release workflows.

Out of scope (deferred to Phase 5+): `update`, `config <key> <value>` writes, log rotation,
endpoint auth, hot model swap, status for foreground serves.

---

## 2. Architecture overview

Two concerns on one branch:

1. **Polish** (three code changes inside `internal/`):
   - flash-attn capability detection via `server.Prober.Capabilities`
   - foreground subprocess graceful shutdown via `Cmd.Cancel` + `Cmd.WaitDelay`
   - Gemma 4 PARAMS investigation (diagnose then fix or document)
2. **Distribution** (new files outside `internal/`):
   - `gregmundy/llamactl` GitHub repo (public)
   - `.goreleaser.yml` + `.github/workflows/{ci,release}.yml`
   - Cask auto-published to `gregmundy/homebrew-tap`

After merge to `main`, the `v1.0.0` tag triggers the release workflow.

---

## 3. Polish #1 — flash-attn capability detection

### Why

Phase 3 emits `--flash-attn on` (tristate syntax). Old Homebrew builds (≤ ~6300) expect bare
`--flash-attn`. Today's choice is to fail loudly on old builds; we want robust detection.

### Design

Extend `internal/server.Prober`. Today it caches `Version` per binary path; add a parallel cache
for `Capabilities`. No new package, no new `Deps` interface.

```go
// internal/server/types.go (or wherever Version lives)
type Capabilities struct {
    FlashAttnTristate bool // true if `--help` shows "--flash-attn [on|off|auto]"
}

// internal/server/prober.go
func (p *Prober) Capabilities(ctx context.Context, path string) (Capabilities, error)
```

`Capabilities` runs `<llama-server> --help`, scans combined stdout+stderr for the substring
`--flash-attn` followed by `[on` (anywhere within ~64 chars). Cache by binary path; same
lifecycle as the existing version cache.

`recipes.FlagsFor` signature gains `caps server.Capabilities`:

```go
func FlagsFor(r Recipe, m models.Model, _ models.Quant, ggufPath string,
    hw hardware.Info, ver server.Version, caps server.Capabilities,
    sizeGB float64, port int) []string
```

Emission rule:
- `caps.FlashAttnTristate` true → `--flash-attn on`
- false → bare `--flash-attn`
- Skip entirely if `shouldAddFlashAttn(ver)` returns false (unchanged behavior).

`cli/serve.go` calls `Prober.Capabilities` after `Probe` (both cached, both cheap on second use).

### Tests

In `internal/server/prober_test.go`:
- `TestCapabilitiesTristateDetection` — fake runner returns a help blob containing
  `--flash-attn [on|off|auto]` → `caps.FlashAttnTristate == true`
- `TestCapabilitiesLegacyDetection` — fake runner returns a help blob with `--flash-attn` only
  → `caps.FlashAttnTristate == false`
- `TestCapabilitiesCachedSecondCall` — second call to same path does not re-invoke runner

In `internal/recipes/recipes_test.go`:
- Existing flash-attn tests pass `server.Capabilities{FlashAttnTristate: true}` so the asserted
  output is `--flash-attn on` (current behavior preserved)
- New `TestFlagsFor_FlashAttnLegacySyntax` — passes `Capabilities{FlashAttnTristate: false}`,
  asserts argv contains bare `--flash-attn` (no `on` value)

In `internal/cli/serve_test.go`:
- `Deps.ServerProber` fake adds a `Capabilities` method returning a tristate-true result

---

## 4. Polish #2 — foreground graceful shutdown

### Why

`exec.CommandContext`'s default Cancel sends SIGKILL when the context is canceled. Phase 3 smoke
confirmed this: Ctrl-C on `serve` → llamactl → llama-server SIGKILLed → "Error: signal: killed"
+ exit 255. No graceful flush.

### Design

In `internal/cli/serve.go` `runServeForeground`, override the Cancel and WaitDelay:

```go
cmd := exec.CommandContext(ctx, llamaServer, argv...)
cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
cmd.WaitDelay = 5 * time.Second
cmd.Stdout = io.MultiWriter(logFile, d.Stdout)
cmd.Stderr = io.MultiWriter(logFile, d.Stderr)
return cmd.Run()
```

`exec.CommandContext` calls `Cmd.Cancel` automatically when ctx is canceled. If the process
doesn't exit within `WaitDelay`, Go follows up with SIGKILL. We do not need a separate goroutine.

Detached path (`runServeDetached`) is untouched — launchd owns child lifecycle there.

The polish-fix from Phase 3 in `cmd/llamactl/main.go` (`errors.As(*exec.ExitError)` →
`os.Exit(exitErr.ExitCode())`) already propagates the child's exit code correctly. When SIGTERM
succeeds the child exits cleanly (code 0); when it doesn't and we SIGKILL, exit propagates as 255.

### Tests

The integration test `TestIntegrationPhase3ForegroundServe` is currently missing (carryover from
Phase 3 review). Phase 4 adds it:

- Spawn the fake llama-server binary (already at `testdata/fakellamaserver/main.go`, already
  honors SIGTERM via `signal.Notify`)
- Run `runRoot(... "serve" ...)` in a goroutine with a cancellable context
- After the fake reports "loaded model", cancel the context
- Assert: process exits within 5s, log file contains "shutting down" line emitted by the fake
  on SIGTERM (proving SIGTERM was delivered, not SIGKILL)

---

## 5. Polish #3 — Gemma 4 PARAMS investigation

### Why

`list` shows blank PARAMS for `gemma-4-e4b-it`. Either the GGUF header lacks
`general.parameter_count`, or our parser doesn't read this variant's encoding.

### Design

Two tasks:

**Task A (diagnostic):** Add a one-shot debug command `internal/gguf/cmd-inspect/main.go` (or
inline it as a `_test.go` that runs against `~/.local/share/llama-models/gemma-4-e4b-it/Q4_K_M.gguf`
when present, skipping otherwise). Output: all top-level metadata keys + values + GGUF types.
Compare to `qwen2.5-3b-instruct`'s GGUF which we know parses correctly.

**Task B (fix):** Based on Task A findings, one of:
- Field absent → make `cli/list.go` display `"?"` instead of `""` for `ParamsB == 0` AND for
  any case where the GGUF didn't report param count. (Distinguishes "unknown" from "this is a
  legitimately small model.")
- Field present but unsupported value type → extend `internal/gguf.ReadHeader` to handle the type.
- Integer truncation (sub-1B precision issue resurfacing for E4B which may report ~4B exactly
  via a different field) → switch `Metadata.ParamsB` to float-with-one-decimal display.

The plan keeps Task B's exact subtasks open until Task A produces evidence.

### Tests

Driven by Task A findings. At minimum:
- `internal/cli/list_test.go` gains a case for `ParamsB == 0` → output column shows `?` (or
  current blank, depending on Task B path)

---

## 6. Distribution — repo bootstrap

`~/Development/llamactl` is local-only today (verified: `git remote -v` returns nothing). After
all polish commits land on `main`:

```bash
gh repo create gregmundy/llamactl \
  --public \
  --source ~/Development/llamactl \
  --remote origin \
  --description "Run llama.cpp on Apple Silicon" \
  --push
```

This creates the public repo, sets `origin`, and pushes all of `main`'s history (~76 commits
pre-Phase 4 across Phase 1, 2, 2.5, 3 + this Phase 4 series).

**`.gitignore` audit before push:** ensure the local-build artifact `/llamactl` at repo root is
ignored so prebuilt binaries don't leak into git. (Check existing `.gitignore`; add the entry if
missing.)

After push, set the per-repo secret:
```bash
gh secret set HOMEBREW_TAP_GITHUB_TOKEN --repo gregmundy/llamactl
```
Reuses the existing PAT from `gregmundy/llamavm` (Contents:write on `gregmundy/homebrew-tap`).
User pastes value interactively; not stored anywhere in the repo.

---

## 7. Distribution — GoReleaser config

`.goreleaser.yml` at repo root, modeled on llamavm's working config:

```yaml
version: 2
project_name: llamactl

before:
  hooks:
    - go mod tidy

builds:
  - id: llamactl
    main: ./cmd/llamactl
    binary: llamactl
    env:
      - CGO_ENABLED=0
    goos: [darwin]
    goarch: [arm64]
    ldflags:
      - -s -w -X main.llamactlVersion={{.Version}}

archives:
  - id: default
    ids: [llamactl]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format: tar.gz
    files:
      - LICENSE*
      - README.md

checksum:
  name_template: "checksums.txt"

snapshot:
  version_template: "{{ incpatch .Version }}-next"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^chore:"
      - "^test:"

homebrew_casks:
  - name: llamactl
    binaries:
      - llamactl
    repository:
      owner: gregmundy
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}"
    homepage: "https://github.com/gregmundy/llamactl"
    description: "Run llama.cpp on Apple Silicon"
    license: "MIT"
    hooks:
      post:
        install: |
          if OS.mac?
            system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", staged_path]
          end
    caveats: |
      Verify your environment after install:

        llamactl doctor

      Add your first model:

        llamactl add qwen2.5-3b-instruct
```

`cmd/llamactl/main.go` already declares `var llamactlVersion = "dev"` — the `-X` ldflags
injection replaces this at build time. No code change needed.

---

## 8. Distribution — GitHub Actions workflows

### `.github/workflows/ci.yml`

```yaml
name: ci

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: macos-14
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
          cache: true
      - run: go vet ./...
      - run: go build ./...
      - run: go test ./... -race
```

### `.github/workflows/release.yml`

Identical shape to llamavm's, only the project name differs:

```yaml
name: release

on:
  push:
    tags: ['v*']

permissions:
  contents: write

jobs:
  goreleaser:
    runs-on: macos-14
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
          cache: true
      - uses: goreleaser/goreleaser-action@v6
        with:
          distribution: goreleaser
          version: '~> v2'
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}
```

---

## 9. Distribution — README refresh

The existing README predates Phase 3. Update to:

- List all 9 subcommands (current README likely shows only Phase 1+2).
- Replace "build from source" instructions with `brew install gregmundy/tap/llamactl`.
- Add a "Quick start" with `doctor` → `add` → `serve --detach` → `curl` → `stop`.
- Keep the existing "About / why exists" narrative.

Not a full rewrite — a section-by-section refresh. Estimated +60 lines.

---

## 10. Sequence + tagging

On `phase4-polish-and-tap` branch:

1. **Polish #1**: `internal/server/prober.go` — add `Capabilities` + test. Change
   `internal/recipes/recipes.go` `FlagsFor` signature. Update all callers + tests. One commit.
2. **Polish #2**: `internal/cli/serve.go` graceful shutdown + integration test
   `TestIntegrationPhase3ForegroundServe`. One commit.
3. **Polish #3a**: GGUF inspector + run against gemma-4-e4b-it.gguf locally. Capture findings
   in commit message body. One commit.
4. **Polish #3b**: Fix or document based on findings. One commit.
5. **README refresh**. One commit.
6. **`.gitignore` audit + `.goreleaser.yml`**. One commit.
7. **`.github/workflows/ci.yml` + `release.yml`**. One commit.
8. Merge `phase4-polish-and-tap` → `main` via `--no-ff` (consistent with prior phases).
9. `gh repo create gregmundy/llamactl --public --source ~/Development/llamactl --remote origin --push --description "Run llama.cpp on Apple Silicon"`
10. `gh secret set HOMEBREW_TAP_GITHUB_TOKEN --repo gregmundy/llamactl` (paste PAT).
11. **`git tag v1.0.0 -m "v1.0.0: MVP (PRD AC#1-16)"` then `git push --tags`.**
12. Watch the release workflow at `https://github.com/gregmundy/llamactl/actions`. On green:
    - Verify GitHub release page lists `llamactl_1.0.0_darwin_arm64.tar.gz` + `checksums.txt`.
    - Verify auto-PR/commit at `gregmundy/homebrew-tap` adds `Casks/llamactl.rb`.
    - Run `brew untap gregmundy/tap 2>/dev/null; brew tap gregmundy/tap; brew install llamactl` on
      a clean shell, confirm `llamactl --help` works and install completed in under 30 s
      (matches AC#1).
13. Update `project_state.md` memory with Phase 4 ship summary.

---

## 11. Risks + non-goals

- **PAT scope:** the `HOMEBREW_TAP_GITHUB_TOKEN` reused from llamavm needs Contents:write on
  `gregmundy/homebrew-tap`. If revoked or expired, release fails. Not a fresh risk; same as
  llamavm today.
- **Cask vs Formula:** we ship a Cask (prebuilt unsigned binary) not a Formula (build from
  source). Cask handles unsigned binaries via the `xattr -dr com.apple.quarantine` postflight
  hook (same as llamavm). Building from source via Formula would take ~30 s defeating AC#1's
  install budget.
- **darwin/arm64 only:** matches PRD targeting. Intel Macs and Linux are deferred per PRD
  "Out of scope, post-v1 candidates."
- **flash-attn detection regex brittleness:** the substring scan looks for `--flash-attn` near
  `[on`. If llama.cpp changes the help text wording, the detection could regress. Probe is
  re-run when binary mtime changes (handled by Prober's caching strategy), so users get a
  chance to recover by reinstalling llama-server. Document this in the recipes package doc.

---

## 12. Acceptance criteria

Phase 4 is complete when:

- ✅ Polish #1 tests pass; `recipes.FlagsFor` correctly emits both syntaxes based on detected caps
- ✅ Polish #2 integration test confirms SIGTERM (not SIGKILL) delivered to llama-server
  on context cancel, child exits cleanly within 5s
- ✅ Polish #3 either makes PARAMS column populate for gemma-4-e4b-it OR explicitly documents
  why it can't (with a sensible "?" fallback in list output)
- ✅ `gregmundy/llamactl` public on GitHub; `main` matches local
- ✅ `v1.0.0` tag triggers release workflow; release page populated; cask published to
  `gregmundy/homebrew-tap`
- ✅ `brew install gregmundy/tap/llamactl` succeeds in under 30 s on a clean machine; resulting
  `llamactl --help` lists all 9 subcommands; `llamactl doctor` runs to completion
- ✅ `project_state.md` memory updated
