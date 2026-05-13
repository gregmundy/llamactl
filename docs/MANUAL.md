# llamactl — User Manual

**Version:** v1.4.1
**Date:** 2026-05-12
**Audience:** Product manager sign-off + end users.

This document describes every user-facing capability of `llamactl`: every command, every flag, every configuration key, every doctor check, and every storage location. It maps the implemented surface to the PRD v1.5 acceptance criteria so reviewers can confirm scope was met.

---

## 1. What llamactl is

`llamactl` is a single-binary Go CLI for running [llama.cpp](https://github.com/ggerganov/llama.cpp) on Apple Silicon. It manages the full lifecycle of local LLMs:

- Hardware detection (chip generation, RAM, GPU-addressable memory)
- Health checks ("doctor")
- Model discovery via Hugging Face search
- Per-host fit ranking (quant selection by available memory)
- Model downloads with SHA verification
- Launching `llama-server` foregrounded or under launchd
- Service status, stop, and log management
- Configuration, authentication, updates

Llamactl does **not** include a daemon, a web UI, or its own quantization pipeline. It is a thin orchestration layer over `llama-server` plus macOS primitives (launchd, sysctl).

### Design principles

| Principle | Consequence |
|---|---|
| One-shot CLI | Every invocation exits when done. No persistent process owned by llamactl itself. |
| macOS native | Uses launchd for service management, not a custom supervisor. |
| Apple Silicon first | M1/M2/M3/M4/M5 hosts are the target; Intel deprioritized. |
| Bring your own `llama-server` | Resolves via `$PATH`, llamavm, or `--llama-server-path`; never bundles. |
| Single shared GGUF directory | `~/.local/share/llama-models/<id>/<quant>.gguf` is shared with other tools (llamavm). |
| Per-tool metadata | `~/.config/llamactl/models/<id>.json` is the only file llamactl owns per-model. |
| Tailnet as the auth boundary by default | Authentication is opt-in; the host's Tailscale boundary is the trust boundary unless `api_key` is set. |

### Audience

End users are developers on Apple Silicon Macs running local LLMs for chat, coding, or batch inference. Llamactl assumes familiarity with the terminal and basic OpenAI-compatible API conventions; it does not assume familiarity with `llama-server` or GGUF internals.

---

## 2. Installation

### Homebrew (recommended)

```bash
brew install gregmundy/tap/llamactl
```

The cask installs the static binary at `/opt/homebrew/bin/llamactl` (Apple Silicon) or `/usr/local/bin/llamactl` (Intel; deprecated). Installation completes in <5 seconds on a fresh tap.

Verify:
```bash
llamactl --version
# llamactl version v1.4.1
```

### From source

```bash
go install github.com/gregmundy/llamactl/cmd/llamactl@latest
```

This produces a `dev` build (no version embedded; `llamactl --version` prints `dev`). The Homebrew installation embeds the tag via `-ldflags`.

### Prerequisites

llamactl needs a working `llama-server` binary on `$PATH`, available via [llamavm](https://github.com/gregmundy/llamavm), or pointed to via `config set llama_server_path <path>`. `llamactl doctor` will tell you which is in use.

---

## 3. Quick start

```bash
# 1. One-time setup: cache hardware detection.
llamactl hardware

# 2. Check everything looks right.
llamactl doctor

# 3. See what's worth downloading for your machine.
llamactl fit qwen 2.5 3b

# 4. Install a preferred-id model.
llamactl add qwen2.5-3b-instruct

# 5. Serve it in the background.
llamactl serve qwen2.5-3b-instruct --detach

# 6. Use it.
curl -d '{"model":"qwen2.5-3b-instruct",
         "messages":[{"role":"user","content":"hi"}]}' \
     http://localhost:8080/v1/chat/completions

# 7. Stop it.
llamactl stop qwen2.5-3b-instruct
```

Total elapsed time on a fresh M-series Mac: ~30 seconds plus model download.

---

## 4. Command reference

Llamactl has 13 user-facing commands plus `help` / `completion`. Each section below documents every flag and shows typical output.

### 4.1 `llamactl hardware`

Detect chip, RAM, GPU memory, OS version; cache to `~/.config/llamactl/hardware.json`.

```
$ llamactl hardware
Chip:               Apple M5
Chip generation:    M5
RAM:                24 GiB
iogpu.wired_limit:  not set (default ~75% of RAM)
Hypervisor:         absent
Metal:              detected
macOS version:      26.0
Wrote /Users/greg/.config/llamactl/hardware.json
```

Best-effort: if a probe fails, the field is left zero. `doctor` translates zero values into actionable messages. The cache is consulted by `add`, `serve`, and `fit` so they don't re-probe every invocation.

### 4.2 `llamactl doctor`

Diagnose the environment. Runs 14 checks. Each check prints `✓` (pass) or `✗` (fail) followed by a short description and an optional remediation hint.

```
$ llamactl doctor
✓ Bare-metal Apple Silicon — no hypervisor detected
✓ llama-server is resolvable — /opt/homebrew/bin/llama-server (via Homebrew)
✓ llama-server version meets floor — b4500 (...)
✓ iogpu.wired_limit_mb is appropriate — host has 24576 MB RAM
✓ port conflicts
✓ model files match metadata
✓ orphaned metadata
✓ disk space — 702 GiB free
✓ tailscale — online
✓ stale plists
✓ Log files within size limit (10 MiB)
✓ HuggingFace API cache size (<500 MiB)
✓ Public-bound endpoints have api_key set
✓ llamactl version — on latest (v1.4.1)
OK
```

All 14 checks pass-through soft when a dependency is absent (e.g., `tailscale` not installed → soft pass with `(not installed)`). The exit code is 0 if everything passes, 1 if any check fails. See §11 for the complete check inventory.

### 4.3 `llamactl search <query>`

Search Hugging Face for GGUF repos. Returns up to 25 results. Preferred IDs (see §6) are prefixed with `*`.

```
$ llamactl search qwen 2.5 7b
* qwen2.5-7b-instruct          Qwen/Qwen2.5-7B-Instruct-GGUF       (Downloads: 1.2M)
  bartowski/Qwen2.5-7B-Instruct-GGUF                                (Downloads: 0.8M)
  lmstudio-community/Qwen2.5-7B-Instruct-GGUF                       (Downloads: 0.4M)
  ...
```

**Flags:**
- `--refresh` — bypass the search-result cache (~/.cache/llamactl/hf-search/)

### 4.4 `llamactl fit <query>`

Search HF and rank GGUF variants by fit on this host. Combines HF search with per-quant size estimates and verdicts.

```
$ llamactl fit qwen 2.5 3b
REPO                                  QUANT    SIZE     FREE     VERDICT  NOTE
Qwen/Qwen2.5-3B-Instruct-GGUF         Q5_K_M   2.2 GB   12.5 GB  ✓ ok
Qwen/Qwen2.5-3B-Instruct-GGUF         Q4_K_M   1.9 GB   12.8 GB  ✓ ok
bartowski/Qwen2.5-3B-Instruct-GGUF    Q5_K_M   2.2 GB   12.5 GB  ✓ ok
...
```

**Verdicts:**
- `✓ ok` — fits with headroom (default 4 GB free after weights + KV cache)
- `⚠ tight` — fits but uses most of the budget
- `✗ too-big` — doesn't fit on this host

**Sorting:**
- Within `ok`: popularity-weighted (Hugging Face download count)
- Per-repo dedupe with 60/40 bucketing (when `--limit >= 5`): 60% of slots for primary rows (best quant per repo), 40% for alternate quants of the surfaced top repos

**Flags:**
- `--install` — install the top-ranked OK row automatically (skips the manual `add` step)
- `--ctx <int>` — context size for KV-cache estimation (default 8192)
- `--limit <int>` — cap rows shown (default 10)
- `--json` — emit JSON instead of the human table
- `--speculative` — list installed draft candidates for the named main model (see §9)

### 4.5 `llamactl fit --speculative <main-model>`

Discovery surface for speculative decoding. Lists installed models compatible as drafts for the named main model.

```
$ llamactl fit --speculative qwen2.5-3b-instruct
Draft candidates for qwen2.5-3b-instruct (3 B, qwen2):

DRAFT ID               ARCH   PARAMSB  RATIO  COMBINED RAM  VERDICT
qwen2.5-0.5b-instruct  qwen2  0.63 B   4.8×   2.5 GB        ⚠ ratio-low

Note: speculative decoding speedup depends on workload; ratio is a heuristic only.
```

Drafts must already be installed. Arch-mismatched candidates are omitted. Sorted by closeness to the ideal 7.5× ratio. See §9 for the speculative-decoding workflow.

### 4.6 `llamactl add <input>`

Download a preferred short-id (see §6) or any Hugging Face GGUF repo path.

```
$ llamactl add qwen2.5-3b-instruct
selected Q5_K_M
downloading Q5_K_M.gguf (2.3 GiB) [=========>          ] 47%
verifying SHA256… ok
installed qwen2.5-3b-instruct (Q5_K_M, 2.3 GiB) ->
  /Users/greg/.local/share/llama-models/qwen2.5-3b-instruct/Q5_K_M.gguf
```

**Two input modes:**
- Preferred-id: `llamactl add qwen2.5-3b-instruct` — uses the curated short-id table
- HF path: `llamactl add Qwen/Qwen2.5-3B-Instruct-GGUF --quant Q5_K_M` — any HF GGUF repo

**Flags:**
- `--quant <name>` — override automatic quant selection. Allowed: `Q5_K_M`, `Q4_K_M`, `Q4_K_S`, `IQ4_XS`, `IQ3_M`, `IQ3_XS`, `Q2_K`
- `--ctx <int>` — target context size for quant calculation (default 8192)

**SHA dedupe:** if the same GGUF (matching SHA256) is already on disk at the canonical path, `add` skips the download and only writes the metadata.

**File lock:** concurrent `add` invocations on the same repo serialize via `flock` on the GGUF directory. A blocked caller logs `another llamactl instance is downloading <repo>; waiting…` to stderr.

### 4.7 `llamactl list`

List installed models. Reads metadata from `~/.config/llamactl/models/*.json` and stats each GGUF on disk.

```
$ llamactl list
MODEL-ID               QUANT   PARAMS  SIZE       PATH                                                                     ADDED       LAST-SERVED
gemma-4-e4b-it         Q4_K_M  7.5 B   4.6 GiB    /Users/greg/.local/share/llama-models/gemma-4-e4b-it/Q4_K_M.gguf         2026-05-11  2026-05-12
qwen2.5-0.5b-instruct  Q4_K_M  0.63 B  468.6 MiB  /Users/greg/.local/share/llama-models/qwen2.5-0.5b-instruct/Q4_K_M.gguf  2026-05-12  
qwen2.5-3b-instruct    Q5_K_M  3 B     2.3 GiB    /Users/greg/.local/share/llama-models/qwen2.5-3b-instruct/Q5_K_M.gguf    2026-05-11  2026-05-12
```

**Self-healing behavior** (runs silently per invocation):
- If `ParamsB == 0` but the GGUF exists, re-parses the header to fill in params and arch.
- If `Arch` is a legacy string (`"qwen2.5"`, `"mistral"`), normalizes to the current canonical value via a string-substitution table (no GGUF re-parse).
- Updates the metadata file when either heals fire.

**Size column:**
- Live file size if the file exists.
- `(missing)` if the path doesn't exist.
- `(stat err)` for unreadable files.

### 4.8 `llamactl remove <model-id>`

Remove the metadata for a model. By default the shared GGUF on disk is preserved (it may be in use by other tools).

```
$ llamactl remove qwen2.5-3b-instruct
removed qwen2.5-3b-instruct metadata (GGUF preserved at
  /Users/greg/.local/share/llama-models/qwen2.5-3b-instruct/Q5_K_M.gguf)
```

**Flags:**
- `--purge` — also delete the GGUF file. Runs a best-effort check for other tools (llamavm) that might be using the file; refuses if a known consumer is present.

### 4.9 `llamactl serve <model-id>`

Start `llama-server` for an installed model. Two modes: foreground (default) and detached via launchd.

```
$ llamactl serve qwen2.5-3b-instruct
starting llama-server (recipe=chat, port=8080)…
[llama-server log streamed to stdout + ~/Library/Logs/llamactl/qwen2.5-3b-instruct.log]
```

Press Ctrl-C to stop. The serve binary handles SIGTERM gracefully (5-second grace, then SIGKILL).

Detached:
```
$ llamactl serve qwen2.5-3b-instruct --detach
bound to :8082 (:8080 was in use)
service qwen2.5-3b-instruct started (pid=4914, recipe=chat); endpoint http://localhost:8082
```

The detached path writes `~/Library/LaunchAgents/com.llamactl.<model-id>.plist`, loads it via `launchctl bootstrap gui/<UID>`, and polls up to 5 seconds for the process to come up.

**Flags:**
- `--port <int>` — TCP port (default 8080). If occupied, llamactl scans `[port, port+100)` for a free one.
- `--recipe <name>` — `chat` / `code` / `long-context` / `low-memory` / `agent` (default `chat`). See §5.
- `--detach` — register as a launchd service and return.
- `--draft <id>` — draft model id for speculative decoding (must be installed). See §9.

**Port allocation:** sibling `com.llamactl.*` plists are scanned; their ports are in the skip-list so concurrent detached serves on different models pick distinct ports. Re-serving the same model reuses its existing port.

**Authentication:** if `api_key` is set (via `config` or `LLAMACTL_API_KEY` env), `--api-key <token>` is appended to llama-server's argv. The env var wins over the config file.

### 4.10 `llamactl stop [<model-id>]`

Stop a detached service. Without an argument, stops all running llamactl-managed services.

```
$ llamactl stop qwen2.5-3b-instruct
stopped qwen2.5-3b-instruct and removed
  /Users/greg/Library/LaunchAgents/com.llamactl.qwen2.5-3b-instruct.plist

$ llamactl stop
stopped 2 services
```

`stop` runs `launchctl bootout` and removes the plist file. The endpoint becomes unresponsive within seconds.

### 4.11 `llamactl status`

List detached llamactl services with live memory, uptime, and recent tok/s.

```
$ llamactl status
MODEL-ID             PORT  STATE    MEM      UPTIME  TOK/S     ENDPOINT
qwen2.5-3b-instruct  8082  running  4.6 GiB  21s     72.0 t/s  http://localhost:8082
qwen3-1.7b           8083  running  1.4 GiB  3m12s   45.2 t/s  http://localhost:8083
```

Memory comes from `ps -o rss=`. tok/s comes from parsing the last 256 KiB of the log file for `eval time`/`prompt eval time` lines.

**Flags:**
- `--json` — emit machine-readable JSON

### 4.12 `llamactl config`

Inspect and update llamactl configuration. Three sub-commands.

**Six allowed keys:**

| Key | Type | Purpose |
|---|---|---|
| `llama_server_path` | path | Override discovery; absolute path to a `llama-server` binary |
| `default_port` | int (1-65535) | Override the default 8080 for `serve` |
| `models_dir` | path | Override `~/.local/share/llama-models` |
| `hf_token` | secret | Hugging Face API token for private repos |
| `log_level` | enum | `debug` / `info` / `warn` / `error` |
| `api_key` | secret | OpenAI-API authentication token for endpoint protection |

Config file: `~/.config/llamactl/config.yaml`. Written atomically (temp + rename) with mode `0600`.

**`llamactl config get <key>`** — print current value. Unknown key → exit 2.

```
$ llamactl config get default_port
8080
```

**`llamactl config set <key> <value>`** — update value and persist.

```
$ llamactl config set api_key sk-abc123
api_key updated
$ llamactl config set default_port 99999
Error: user error: default_port must be 1-65535
```

Setting an empty value clears the key.

**`llamactl config list`** — tabular view of all six keys. Secrets (`api_key`, `hf_token`) display as `********` if set, `(unset)` if zero.

```
$ llamactl config list
api_key              ********  (set; redacted)
default_port         8080
hf_token             (unset)
llama_server_path    (unset)
log_level            (unset)
models_dir           (unset)
```

### 4.13 `llamactl cache prune`

Remove stale Hugging Face API cache entries from `~/.cache/llamactl/hf-*/`. By default, entries older than 30 days are removed.

```
$ llamactl cache prune
removed 1001 cache files (520.4 MiB freed)
```

**Flags:**
- `--all` — remove every cache entry (full reset), not just stale ones

Also removes empty namespace subdirectories left over from cache-version bumps.

### 4.14 `llamactl update`

Upgrade to the latest published version via Homebrew. Detects whether the running binary is a Homebrew install (`/opt/homebrew/Caskroom/llamactl/` or `/opt/homebrew/bin/llamactl`); shells out to `brew update && brew upgrade gregmundy/tap/llamactl` if so.

```
$ llamactl update
current: v1.4.0
latest:  v1.4.1
==> brew update
==> brew upgrade gregmundy/tap/llamactl
🍺  llamactl was successfully upgraded!
done.

$ llamactl update              # already current
already on latest (v1.4.1)
```

Non-Homebrew installs (e.g., `go install`) print a helpful message and exit 0 without acting.

**Latest version source:** parses `version "X.Y.Z"` from the cask file at `https://raw.githubusercontent.com/gregmundy/homebrew-tap/main/Casks/llamactl.rb`. Cached for 24 hours at `~/.cache/llamactl/last-version-check.json`.

**Flags:**
- `--refresh` — bypass the 24-hour version-check cache

---

## 5. Recipes

A recipe is a named tuning bundle that maps to `llama-server` flags. Five ship as of v1.4.5:

| Recipe | Context | KV cache K/V | Mlock policy | Intended for |
|---|---|---|---|---|
| `chat` | 8 192 | f16 / f16 | auto | Interactive chat; default |
| `code` | 16 384 | f16 / f16 | auto | Code assistance; doubled context |
| `long-context` | 32 768 | q8_0 / q8_0 | auto | RAG, long-document QA; quantized KV for footprint |
| `low-memory` | 4 096 | q4_0 / q4_0 | off | Constrained hosts (8 GB hosts); aggressive KV quantization |
| `agent` | 8 192 | f16 / f16 | auto | Deterministic, non-interactive utility workloads (summarize / extract / classify / rewrite / agent offload) |

**Mlock auto** adds `--mlock` when usable RAM is at least 4 GB greater than the model's weight size. **Mlock off** never adds `--mlock` regardless of headroom (used by `low-memory`).

The recipe also drives:
- `--flash-attn on` (tristate) when llama-server build supports it; bare `--flash-attn` for older builds.
- `--cache-type-k` / `--cache-type-v` selection.
- Max context clamping (recipe ctx clamped against the model family's MaxCtx).

Recipes are pure-function over the main model only — speculative decoding's `--draft` is appended post-recipe in `serve.go` and does not require its own recipe variant.

### `agent` recipe — additional flags

`agent` pins sampling and reasoning behavior at server startup so output is deterministic and reasoning-capable models don't burn the generation budget on internal thinking.

| Flag | Value | Why |
|---|---|---|
| `--temp` | `0` | Deterministic output; repeatable across identical prompts |
| `--top-p` | `1.0` | No nucleus filter; let `temp 0` do the work |
| `--top-k` | `0` | Disabled |
| `--predict` | `2048` | Bounded generation; bounded enough to fail-fast on runaway, generous enough for rich outputs |
| `--reasoning` | `off` | Disables thinking server-wide on reasoning-capable models (Qwen3, DeepSeek-R1, etc.). Without this, those models can spend their entire generation budget inside `<think>` blocks and return empty `content`. |

Callers can override any of these per-request via the OpenAI chat-completions body fields (`temperature`, `top_p`, `max_tokens`). Recipe settings are *defaults*, not enforcements.

Pair `agent` with a small fast model (e.g. `qwen2.5-3b-instruct`, `qwen3-1.7b`) for offload duty. Larger models work fine too but the recipe was tuned around sub-3B utility workloads.

---

## 6. Preferred model IDs

11 short-ids ship in v1.4.1's curated table. Each entry maps to an HF repo + canonical `Arch` / `ParamsB` / `MaxCtx`. Using a preferred-id with `llamactl add` skips the `--quant` flag (selector picks automatically based on host RAM).

| Short ID | HF Repo | Family | ParamsB | MaxCtx |
|---|---|---|---|---|
| `qwen3-0.6b` | `Qwen/Qwen3-0.6B-GGUF` | qwen3 | 0.6 | 32 768 |
| `qwen3-1.7b` | `Qwen/Qwen3-1.7B-GGUF` | qwen3 | 1.7 | 32 768 |
| `qwen2.5-3b-instruct` | `Qwen/Qwen2.5-3B-Instruct-GGUF` | qwen2 | 3.0 | 32 768 |
| `qwen2.5-7b-instruct` | `Qwen/Qwen2.5-7B-Instruct-GGUF` | qwen2 | 7.0 | 32 768 |
| `qwen2.5-14b-instruct` | `Qwen/Qwen2.5-14B-Instruct-GGUF` | qwen2 | 14.0 | 32 768 |
| `qwen2.5-coder-3b` | `Qwen/Qwen2.5-Coder-3B-Instruct-GGUF` | qwen2 | 3.0 | 32 768 |
| `qwen2.5-coder-7b` | `Qwen/Qwen2.5-Coder-7B-Instruct-GGUF` | qwen2 | 7.0 | 32 768 |
| `qwen2.5-coder-14b` | `Qwen/Qwen2.5-Coder-14B-Instruct-GGUF` | qwen2 | 14.0 | 32 768 |
| `llama3.1-8b` | `bartowski/Meta-Llama-3.1-8B-Instruct-GGUF` | llama3 | 8.0 | 131 072 |
| `llama3.2-3b` | `bartowski/Llama-3.2-3B-Instruct-GGUF` | llama3 | 3.0 | 131 072 |
| `llama3.3-70b` | `bartowski/Llama-3.3-70B-Instruct-GGUF` | llama3 | 70.0 | 131 072 |
| `mistral-7b-v0.3` | `bartowski/Mistral-7B-Instruct-v0.3-GGUF` | llama3 | 7.0 | 32 768 |

PreferredIDs is **not a gate**. `add Author/Repo-GGUF --quant Q4_K_M` accepts any HF GGUF repo path. Preferred-IDs exist for ergonomics and pre-tuned metadata.

---

## 7. Authentication

Authentication is **opt-in**. Without an API key, llamactl serves endpoints unauthenticated, and the host's Tailnet boundary is the trust boundary.

### Enabling

Either via config:
```bash
llamactl config set api_key sk-abc123
```

Or via env var (wins over config):
```bash
export LLAMACTL_API_KEY=sk-abc123
```

### Behavior

When set, `serve` appends `--api-key <token>` to llama-server's argv. The detached plist embeds the same arg, so the key persists across reboots.

Use:
```bash
curl -H "Authorization: Bearer sk-abc123" \
  http://localhost:8080/v1/chat/completions ...
```

Without the header, llama-server returns 401.

### Doctor check

The `Public-bound endpoints have api_key set` check flags a `✗` warning when a service binds publicly (`--host 0.0.0.0` or default-bind) and no `--api-key` argument is present in the plist. The check never blocks operations, only warns.

**Known limitation:** llama-server's `/v1/models` endpoint is intentionally unauthenticated upstream. Only `/v1/chat/completions` and other inference endpoints enforce `--api-key`.

### Hugging Face authentication (`hf_token`)

`hf_token` is independent of `api_key`. It controls llamactl's outbound API calls to Hugging Face (for `search`, `fit`, `add`). Without a token, llamactl makes anonymous HF calls — sufficient for the entire preferred-ID table and any public GGUF repo.

You only need a token for:

- **Gated official-vendor models** like `meta-llama/Llama-3.1-8B-Instruct` or `google/gemma-3-27b-it`. The preferred-ID table uses community re-hosts (e.g. `bartowski/Meta-Llama-3.1-8B-Instruct-GGUF`) to avoid this.
- **High-volume scripted use**. Anonymous HF API allows ~5000 req/hour. A `fit` invocation is 1 search + up to 25 RepoInfo = ~26 calls, so ~190 invocations per hour anonymous. Realistically not a concern for interactive use.

Resolution order (highest wins):

```
LLAMACTL_HF_TOKEN env  →  HF_TOKEN env  →  config hf_token  →  anonymous
```

Set via:

```bash
llamactl config set hf_token hf_abc123      # persisted
export HF_TOKEN=hf_abc123                    # session-scoped
export LLAMACTL_HF_TOKEN=hf_abc123            # session-scoped, wins above
```

Per v1.4.4, the config path is wired correctly — earlier versions persisted the config value but never read it. If you upgraded from v1.4.3 or earlier and already had `hf_token` set in `config.yaml`, no action needed; it will now take effect.

---

## 8. Configuration files

```
~/.config/llamactl/
├── config.yaml              # The six config keys; mode 0600
├── hardware.json            # Cached hardware detection
└── models/
    ├── qwen2.5-3b-instruct.json
    ├── qwen2.5-0.5b-instruct.json
    └── gemma-4-e4b-it.json

~/.local/share/llama-models/  # Shared with other tools (llamavm)
├── qwen2.5-3b-instruct/
│   └── Q5_K_M.gguf
└── ...

~/.cache/llamactl/
├── hf-search-v1/            # HF search-result cache (24h TTL)
├── hf-repo-v2/              # HF repo-info cache (24h TTL)
└── last-version-check.json  # Update-check cache (24h TTL)

~/Library/LaunchAgents/
└── com.llamactl.<model-id>.plist  # One per detached service

~/Library/Logs/llamactl/
└── <model-id>.log           # Rotated at 10 MiB; up to 3 backups (.log.1, .log.2, .log.3)
```

### Metadata format

Each `models/<id>.json` is:
```json
{
  "id": "qwen2.5-3b-instruct",
  "repo": "Qwen/Qwen2.5-3B-Instruct-GGUF",
  "quant": "Q5_K_M",
  "sha256": "2c63dde5f2c9ab1fd64d47dee2d34dade6ba9ff62442d1d20b5342310c982081",
  "gguf_path": "/Users/greg/.local/share/llama-models/qwen2.5-3b-instruct/Q5_K_M.gguf",
  "size_bytes": 2438740384,
  "added_at": "2026-05-11T23:47:22.900174-04:00",
  "params_b": 3,
  "arch": "qwen2",
  "last_served_at": "2026-05-12T14:12:21.484085-04:00"
}
```

---

## 9. Speculative decoding workflow

Speculative decoding pairs a small "draft" model with a larger "main" model. The draft proposes tokens; the main model verifies in parallel. Typical speedup is 1.5–3× depending on workload.

### Step 1: install both models

Both main and draft must be locally installed. Llamactl does not auto-download a draft when serving.

```bash
llamactl add qwen2.5-3b-instruct                       # the main
llamactl add Qwen/Qwen2.5-0.5B-Instruct-GGUF --quant Q4_K_M  # the draft
```

### Step 2: discover compatible drafts

```bash
$ llamactl fit --speculative qwen2.5-3b-instruct
Draft candidates for qwen2.5-3b-instruct (3 B, qwen2):

DRAFT ID               ARCH   PARAMSB  RATIO  COMBINED RAM  VERDICT
qwen2.5-0.5b-instruct  qwen2  0.63 B   4.8×   2.5 GB        ⚠ ratio-low
```

Eligibility rules:
- Same `general.architecture` (tokenizer compatibility cannot be assumed across families).
- Size ratio between 2× (minimum for any speedup) and `usable_RAM - 4 GB headroom`.
- Sorted by closeness to the 7.5× ideal sweet spot.

Verdicts:
- `✓ ok` — ratio in [5, 15]
- `⚠ ratio-low` — ratio in [2, 5) — overhead may eat speedup
- `⚠ ratio-high` — ratio > 15 — alignment may be poor
- `✗ refused` — ratio < 2 or combined RAM exceeds budget

### Step 3: serve with the draft

```bash
$ llamactl serve qwen2.5-3b-instruct --draft qwen2.5-0.5b-instruct --detach
bound to :8082 (:8080 was in use)
speculative decoding enabled (draft=qwen2.5-0.5b-instruct, ratio=4.8×)
service qwen2.5-3b-instruct started (pid=4914, recipe=chat); endpoint http://localhost:8082
```

Llamactl validates the pair before launching:
- Missing draft → `ErrUserError` exit 2 with `"run \`llamactl add <id>\` first"`
- Arch mismatch → `ErrUserError` exit 2 naming both archs
- Combined RAM exceeds budget → `ErrUserError` exit 2 with shortfall
- Ratio outside 5–15× → stderr warning, serve continues

Detached services embed `--model-draft` and `--ctx-size-draft` in the plist, so the pairing persists across reboots. Re-running `serve --detach` without `--draft` clears the pairing.

### Caveats

- Speedup is workload-dependent. Batch size, temperature, and prompt structure all matter.
- The ratio heuristic is informational; some pairs slow generation rather than speeding it.
- The draft's context is capped at `min(main_ctx, draft.MaxCtx)`.
- Tokenizer compatibility is not validated by llamactl — same architecture is the proxy. `llama-server` errors at startup if tokenizers actually diverge.

---

## 10. Hardware → quant selection

The selector is a pure function:

```
usable_GB     = GPU-addressable RAM - 4 GB (OS overhead) - 2 GB (headroom)
kv_cache_GB   = KVCachePerTokenKB[arch][Q8_0] × ctx_size / 1024 / 1024
budget_GB     = usable - kv_cache_GB
```

For each quant in descending quality order — `Q5_K_M`, `Q4_K_M`, `Q4_K_S`, `IQ4_XS`, `IQ3_M`, `IQ3_XS`, `Q2_K` — return the first that fits the budget.

GPU-addressable RAM is determined by:
- `hw.iogpu_wired_limit_mb` if explicitly set (via `sudo sysctl iogpu.wired_limit_mb=...`), OR
- 67% of total RAM (the empirical macOS default).

Sub-1B models (ParamsB < 0.5) round to bucket 1 in `QuantSizeTable`. Unknown ParamsB buckets fall through to a rough 0.6 GB/B Q4_K_M estimate (used by speculative-pair RAM math).

---

## 11. Doctor checks (14 total)

| # | Check | Pass condition |
|---|---|---|
| 1 | Bare-metal Apple Silicon | `sysctl kern.hv_vmm_present` is 0 |
| 2 | llama-server resolvable | Found on `$PATH`, via llamavm, or at `llama_server_path` config |
| 3 | llama-server version meets floor | Reports a build number; >= `MinLlamaServerBuild=1` |
| 4 | iogpu.wired_limit_mb appropriate | Either explicitly set, or default ratio sufficient for RAM size |
| 5 | port conflicts | No process binding a port a llamactl plist claims |
| 6 | model files match metadata | Every metadata entry's `gguf_path` exists on disk |
| 7 | orphaned metadata | No GGUF file lacks a metadata entry |
| 8 | disk space | At least 10 GiB free on the models partition |
| 9 | tailscale | If installed, reports `online` (otherwise soft pass `(not installed)`) |
| 10 | stale plists | No `com.llamactl.*.plist` file references a missing GGUF |
| 11 | Log files within size limit | Each `~/Library/Logs/llamactl/<id>.log` ≤ 10 MiB |
| 12 | HuggingFace API cache size | `~/.cache/llamactl/hf-*` total ≤ 500 MiB |
| 13 | Public-bound endpoints have api_key set | Every plist binding publicly contains `--api-key` |
| 14 | llamactl version | Either on latest, on `dev` build, or check soft-passes when offline |

Checks 1, 2, 11–14 may report informational messages on a `✓` pass (e.g., "(dev build; skipping version check)"). Check 1 is hard-fail — running on a Mac VM without Metal passthrough refuses model operations before any download.

---

## 12. PRD v1.5 acceptance criteria — status

The PRD lists 16 acceptance criteria. All 16 are met as of v1.4.1.

| AC # | Description | Status |
|---|---|---|
| 1 | `brew install gregmundy/tap/llamactl` succeeds in <30 s | ✅ Verified: 7.6 s (v1.0.0); ~3 s on every subsequent upgrade |
| 2 | `doctor` on a system with no llama.cpp suggests both `brew install llama.cpp` and llamavm | ✅ Verified in Phase 3 smoke |
| 3 | `doctor` on Homebrew's llama.cpp passes resolution + reports version | ✅ Verified in Phase 3 + Phase 4 smokes |
| 4 | `doctor` on llamavm passes resolution + reports active llamavm version | ✅ Verified in Phase 3 |
| 5 | `hardware` correctly identifies chip generation, total RAM, GPU-addressable memory | ✅ Verified on M5 / 24 GB |
| 6 | `add qwen2.5-7b` selects Q4_K_M on 16 GB host, downloads, SHA-verifies in <10 min on 100 Mbps | ✅ Verified |
| 7 | Same GGUF already present + matching SHA → skip download, write metadata only | ✅ Verified ("already present (matched SHA)" path) |
| 8 | `serve qwen2.5-7b --detach` returns within 5 s with working endpoint | ✅ Verified (detach poll deadline = 5 s) |
| 9 | OpenAI chat-completions POST returns valid response with non-zero token count | ✅ Verified in every phase smoke |
| 10 | `status` shows running service with memory ±10% of `ps` and recent tok/s | ✅ Verified |
| 11 | `doctor` detects unset/low `iogpu.wired_limit_mb` and outputs the exact `sudo sysctl` command | ✅ Verified |
| 12 | `doctor` refuses on Mac VM without Metal passthrough before any model operation | ✅ Verified (hard-fail check #1) |
| 13 | After clean reboot, launchd service auto-starts; endpoint available within 60 s | ✅ Verified (Phase 4 reboot test) |
| 14 | OpenAI client from a separate Tailnet host can chat-complete | ✅ Verified |
| 15 | `stop qwen2.5-7b` cleanly stops service, unloads plist, endpoint dies within 10 s | ✅ Verified |
| 16 | Switching active llamavm version causes next `serve` to use the new binary, no config changes | ✅ Verified |

---

## 13. PRD v1.5 non-goals — status

The PRD called out the following as **out of scope** for v1. Re-elevation in later phases is noted.

| Non-goal | Status |
|---|---|
| Cross-platform support (Windows, Linux) | Still out of scope |
| Local quantization | Still out of scope (we consume pre-quantized GGUF) |
| Multi-model concurrent serving (single port) | Still out of scope (single-model per serve invocation; multiple serves on distinct ports work) |
| Embeddings / vector DB integration | Still out of scope |
| Cloud / API key brokerage | Still out of scope |
| Authentication on local endpoints | **Re-elevated in Phase 6a (v1.3.0)** — opt-in via `api_key` config + `LLAMACTL_API_KEY` env |
| Web UI / dashboard | Cancelled (conflicts with one-shot CLI nature; everything a UI would show is reachable via `status`/`list`/`fit`) |
| Hot model swap | Deferred to Phase 6c (web UI); now perma-deferred since 6c was cancelled — the only clean path was via the proxy substrate that 6c would have built |
| Speculative decoding | **Re-elevated in Phase 6b (v1.4.0)** — explicit `--draft` flag + `fit --speculative` discovery |

---

## 14. Version history

| Version | Date | Highlights |
|---|---|---|
| v1.0.0 | 2026-05-11 | MVP. PRD AC#1–16 complete. Hardware/doctor/search/add/list/remove/serve/stop/status. Homebrew cask published. |
| v1.2.0 | 2026-05-12 | `fit` command. `cache prune`. 14 doctor checks (was 10). 19 backlog items drained. |
| v1.2.1 | 2026-05-12 | Hotfix: `PortAllocator` race when concurrent detached serves were starting simultaneously. |
| v1.3.0 | 2026-05-12 | `update` + `config` commands. Opt-in endpoint auth via `api_key`. ParamsB `int → float64` migration for sub-1B precision. 13 backlog items. |
| v1.4.0 | 2026-05-12 | Speculative decoding (`--draft`, `fit --speculative`). GGUF tensor-shape parser fallback (closes the `?` ParamsB hole for supported arches). |
| v1.4.1 | 2026-05-12 | Cleanup: exported speculative thresholds, dropped `ArchMistral`, `list` self-heals legacy Arch strings, `fit` 60/40 dedupe bucketing. |
| v1.4.2 | 2026-05-12 | Hotfix: `fit` no longer hangs when an HF API response stalls. Added transport-level `ResponseHeaderTimeout` (30 s) to the HTTP client. Downloads unaffected (no global `Timeout`). |
| v1.4.3 | 2026-05-12 | `fit` parallelizes its `RepoInfo` loop (8 concurrent) — `fit gemma` drops from ~60 s to ~10 s. TTY progress line `"fetching repo info (N/M)…"` updates in place during the wait; suppressed for non-TTY output so pipes stay clean. |
| v1.4.4 | 2026-05-12 | Fix: `hf_token` set via `config` was being silently ignored (env-only resolution path). Now `LLAMACTL_HF_TOKEN > HF_TOKEN > config hf_token > anonymous`. |

---

## 15. Appendix

### 15.1 Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic error (network, file system, internal) — prefixed `llamactl:` |
| 2 | User error (bad input, validation failure, missing prerequisite) — produced via the `ErrUserError` sentinel |
| Other | Foreground `serve` propagates `llama-server`'s exit code on its non-zero exit |

### 15.2 Environment variables

| Variable | Purpose |
|---|---|
| `LLAMACTL_API_KEY` | Endpoint auth token; wins over `config api_key` |
| `HF_TOKEN` | Hugging Face API token used on every HF call. Wins over `config set hf_token`. |
| `LLAMACTL_HF_TOKEN` | Same as `HF_TOKEN` but takes precedence when both are set. |
| `PATH` | Used to discover `llama-server` |
| `HOME` | Used to derive every storage path |

### 15.3 Port allocation rules

1. `--port N` (or `default_port` config; or 8080 if unset) is the preferred port.
2. If preferred port is bound by any process, scan `[preferred, preferred+100)` for the first free port.
3. Sibling `com.llamactl.*` plists' ports are added to the skip list before binding (prevents two simultaneous detached serves from racing onto the same port during model load).
4. Re-serving the same model id excludes its own current port from the skip list (keeps the same port across re-serves).

### 15.4 File paths reference

```
/opt/homebrew/bin/llamactl                       # Apple Silicon brew install
/opt/homebrew/Caskroom/llamactl/<version>/       # Apple Silicon cask root
/usr/local/bin/llamactl                          # Intel brew install (deprecated)
~/.config/llamactl/config.yaml                   # Config (mode 0600)
~/.config/llamactl/hardware.json                 # Hardware cache
~/.config/llamactl/models/<id>.json              # Per-model metadata
~/.local/share/llama-models/<id>/<quant>.gguf    # Shared GGUF directory
~/.cache/llamactl/hf-search-v1/                  # HF search cache
~/.cache/llamactl/hf-repo-v2/                    # HF repo-info cache
~/.cache/llamactl/last-version-check.json        # Update-check cache
~/Library/LaunchAgents/com.llamactl.<id>.plist   # Per-service launchd plist
~/Library/Logs/llamactl/<id>.log                 # Per-service log (10 MiB rotation)
```

### 15.5 Glossary

- **GGUF** — the binary model file format used by `llama.cpp`. Each file contains a metadata header (kv-block) and tensor data.
- **Quant** — quantization preset. Smaller quants mean smaller files but reduced model quality. The selector picks the highest-quality quant that fits the host's memory budget.
- **KV cache** — per-token attention state held in memory during generation. Grows with context size; the recipe's `cache_type_k`/`cache_type_v` controls its precision.
- **Recipe** — a named bundle of `llama-server` flags (ctx, KV-cache type, mlock policy).
- **Detached serve** — a `serve` invocation that registers a launchd LaunchAgent and returns. The service persists across reboots.
- **Speculative decoding** — pairing a small draft model with a larger main model to speed up generation via parallel verification.
- **Tailnet** — Tailscale's overlay network. Used as the default trust boundary for unauthenticated endpoints.

---

## 16. Sign-off checklist

For the product manager reviewing:

- [ ] PRD v1.5 acceptance criteria 1–16 all marked ✅ in §12.
- [ ] Non-goals respected per §13 (web UI cancelled; speculative decoding + auth re-elevated as documented).
- [ ] Every command in §4 has at least one observed live invocation against my Apple Silicon host.
- [ ] Configuration keys in §4.12 cover the originally-scoped set with `api_key` added per v1.3.0.
- [ ] Storage layout in §8 matches the PRD §Storage convention.
- [ ] Recipe set in §5 matches the PRD §Recipe → flag mapping.
- [ ] Preferred-ID table in §6 includes the originally-scoped families plus Qwen3 / Qwen2.5-Coder additions.
- [ ] Doctor checks in §11 total 14 (was 10 at v1.0; +2 in Phase 5, +2 in Phase 6a).

Sign-off: ______________________________________ Date: ______________
