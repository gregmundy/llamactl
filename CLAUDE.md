# CLAUDE.md — Working on llamactl

Operational notes for agents working in this repo. Not a user manual (see `docs/MANUAL.md`). Not a spec (see `docs/superpowers/specs/`).

## Project status

**Feature-complete as of v1.4.0.** Maintenance-only going forward. Phase 6c (web UI / hot swap proxy / daemon) was deliberately cancelled — a daemon conflicts with the product's one-shot CLI identity. Don't propose features that turn llamactl into a long-running process.

The full version history is in `~/.claude/projects/-Users-greg-Development-llamactl/memory/project_state.md`. Always read that first when starting work — it has the architectural decisions, deferred concerns, and what each phase shipped.

## What llamactl is

Single-binary Go CLI that orchestrates `llama-server` on Apple Silicon. Uses launchd for service supervision, never owns a daemon itself. Static binary distributed via Homebrew cask at `gregmundy/tap/llamactl`. Module: `github.com/gregmundy/llamactl`. Go 1.26.2.

## Layout

```
cmd/llamactl/main.go         Wiring only — Deps construction + cobra root
cmd/gguf-inspect/main.go     Diagnostic tool for GGUF parser issues
internal/cli/                Every subcommand lives here (one file per cmd)
internal/cli/deps.go         The Deps struct + every narrow interface
internal/config/             ~/.config/llamactl/config.yaml load/save
internal/download/           HTTP range downloads with flock dedupe
internal/gguf/               GGUF header parser; params_arch.go has the formulas
internal/gguftest/           Test helper: BuildWithTensors generates fixtures
internal/hardware/           sysctl/system_profiler probes
internal/hf/                 HuggingFace API client (search/repo info/range fetch)
internal/launchd/            Plist render + launchctl wrapper
internal/models/             PreferredIDs, Arch constants, quant selector, SpeculativePair
internal/proc/               Port allocation + ps/etime parsing
internal/recipes/            chat/code/long-context/low-memory/agent flag bundles
internal/runner/             os/exec seam
internal/server/             llama-server version + capability probe
docs/MANUAL.md               User-facing manual (PM sign-off doc)
docs/llamactl-prd-v1.5.md    Source-of-truth PRD
docs/superpowers/specs/      Approved design specs per phase
docs/superpowers/plans/      Implementation plans per phase
```

## Architectural conventions (do not relitigate)

These are load-bearing — they're documented because past iterations broke when they were violated.

1. **`cmd/llamactl/main.go` is wiring only.** Command logic lives in `internal/cli`. main.go constructs `cli.Deps` and hands it to the cobra root. Putting logic in main.go makes it untestable.

2. **`cli.Deps` + narrow interfaces.** Every external dependency goes through a small interface in `internal/cli/deps.go` (`HardwareDetector`, `ServerResolver`, `HFClient`, `ModelStore`, `LaunchdService`, `PortAllocator`, etc.). Production adapters live in `adapters_*.go`. Tests construct `&Deps{...}` with fakes. Adding a `Deps` field is fine; changing an interface signature breaks every fake — only do it when you mean it.

3. **`runner.CommandRunner` is the os/exec seam.** Leaf packages (`launchd`, `proc`) re-declare narrow interfaces locally via Go structural typing. Don't shell out via `exec.Command` directly outside `runner` and `cmd/llamactl/main.go`.

4. **`ErrUserError` sentinel → exit 2; generic error → exit 1.** Foreground `serve` additionally propagates llama-server's exit code via `errors.As(*exec.ExitError)` in `main.go`. User-facing errors must wrap `ErrUserError`.

5. **Storage paths are fixed:**
   - `~/.local/share/llama-models/<id>/<quant>.gguf` — shared with llamavm (don't move it)
   - `~/.config/llamactl/models/<id>.json` — per-tool metadata (we own this)
   - `~/Library/LaunchAgents/com.llamactl.<id>.plist` — one per detached serve
   - `~/Library/Logs/llamactl/<id>.log` — rotated at 10 MiB
   - `~/.cache/llamactl/hf-{search,repo}/` — HF API cache (30d TTL)

6. **`models.PreferredIDs` is not a gate.** `add Author/Repo --quant Q4_K_M` accepts any HF GGUF repo. PreferredIDs exists for ergonomics + curated metadata.

7. **`Arch` strings must match GGUF reality.** `general.architecture` is what shows up in the file. We learned this twice: `qwen2.5` (v1.4.0 fix) and `mistral` (v1.4.1 fix). When adding a family, verify `gguf-inspect` against a real file first, not the marketing name. `models.NormalizeArch` migrates legacy metadata strings to canonical values via `list` self-heal.

## Branch discipline (subagent prompts)

Every implementer subagent prompt **must** include this primer, verbatim:

> "You are on branch `<branch-name>`. Do NOT `git checkout`, `git switch`, `git stash`, `git reset`, or any branch-changing operation. If `git status` shows unexpected files, stop and ask. Your task is exactly Task N below; do not start Task N+1."

Phase 3 lost an afternoon to silent branch switches before this primer existed. Phase 6a + 6b had zero branch issues across 50+ implementer dispatches with it. Cost is 60 characters; benefit is large.

## TDD + verification

Each task = one commit. Per task:

1. Write failing test
2. Run it; confirm it fails for the right reason (not a compile error masking the real intent)
3. Implement
4. Run full suite with `-race`: `go test ./... -race`
5. `gofmt -l .` + `go vet ./...` must be clean
6. Commit with a HEREDOC-formatted message ending with `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>`

Skill modernization diagnostics (e.g. "use `strings.Cut`", "tagged switch") are informational — don't churn unrelated code chasing them.

## Live smoke (before tagging)

Every release goes through a smoke pass on Apple M5 before tag/push:

1. `go install ./cmd/llamactl` — locally-built binary in `~/go/bin/llamactl`
2. Exercise the new feature against real HF + real Metal
3. Confirm `llamactl doctor` still reports 14/14
4. Run the relevant pre-existing flows that touch the changed code

Live smoke has caught real bugs that synthetic tests missed in every phase since v1.0.0. Don't skip it.

## Release flow

```bash
# After feature branch is green and smoke passes:
git checkout main
git merge --no-ff <branch> -m "Merge <branch>: <summary> (vX.Y.Z)"
git tag -a vX.Y.Z -m "vX.Y.Z: <summary>"
git push origin main
git push origin vX.Y.Z

gh run watch <release-run-id> --exit-status
# GoReleaser builds darwin/arm64, publishes the cask to homebrew-tap.

brew update && time brew upgrade llamactl
/opt/homebrew/bin/llamactl --version  # confirm new tag
```

CI uses `actions/checkout@v5`, `actions/setup-go@v6`, `goreleaser/goreleaser-action@v7` (Node 24). Don't downgrade.

After release: update `project_state.md` memory with what shipped + any new deferred concerns. Update `MEMORY.md` index pointer. Update `docs/MANUAL.md` version table.

## Local `go install` vs Homebrew

`go install ./cmd/llamactl` produces a `dev`-versioned binary at `~/go/bin/llamactl`. The Homebrew cask embeds the tag via `-ldflags '-X main.llamactlVersion=v...'`. If `~/go/bin/` precedes `/opt/homebrew/bin/` in `$PATH`, the dev binary wins and `--version` shows `dev`. To use the brew binary unqualified, `rm ~/go/bin/llamactl` after smoke testing.

`dev` builds short-circuit the version-check + update paths (so a developer working on the version code doesn't see misleading "update available: vdev → vX.Y.Z" messages).

## Configuration

Six keys in `~/.config/llamactl/config.yaml`:
- `llama_server_path` — path override for discovery
- `default_port` — int 1-65535
- `models_dir` — path override for the GGUF directory
- `hf_token` — Hugging Face API token (precedence: `LLAMACTL_HF_TOKEN` env > `HF_TOKEN` env > config)
- `log_level` — debug/info/warn/error
- `api_key` — endpoint auth token (precedence: `LLAMACTL_API_KEY` env > config)

`config.Save` is atomic temp+rename with mode `0600`. Secrets (`api_key`, `hf_token`) redact as `********` in `config list`.

## Don't do these things

- **Don't add a daemon.** Phase 6c was cancelled for this exact reason. The product identity is one-shot CLI.
- **Don't propose a web UI.** Same.
- **Don't propose hot model swap.** Required a daemon to do cleanly; without one, the few-second port-race window isn't acceptable.
- **Don't add new `Deps` interface methods without need.** Additive fields are fine; signature changes cascade.
- **Don't shell out via raw `exec.Command`.** Use `runner.CommandRunner`.
- **Don't commit binaries.** `gguf-inspect` and `llamactl` build outputs land in repo root via `go build`; both are in `.gitignore`. Verify before `git add -A`.
- **Don't commit `.claude/` directory contents.** Harness state. `.gitignore` covers it.
- **Don't skip the live smoke step before tagging.** Every phase has surfaced bugs that synthetic tests missed.
- **Don't `git push --force` to main.** Ever.

## Auto-memory

This project has persistent memory at `~/.claude/projects/-Users-greg-Development-llamactl/memory/`. Read `MEMORY.md` and `project_state.md` at session start. Update `project_state.md` after each release with what shipped + new deferred concerns. The format is well-established — follow the existing structure.
