# llamactl — PRD v1.5

## Summary

A single-binary CLI that handles the boilerplate of running llama.cpp on
Apple Silicon. Pick the right quantization for available hardware,
download it, generate appropriate llama-server flags, manage the daemon
via launchd, expose an OpenAI-compatible endpoint over the local network.

Built primarily for the author's Hermes deployment (private email triage
on an Apple Silicon Mac mini, accessed over Tailscale). MIT-licensed,
others welcome to use it; no adoption goals.

llamactl pairs with llamavm (the author's llama.cpp version manager) but
does not require it. It uses whatever `llama-server` it finds on PATH —
Homebrew's llama.cpp, llamavm-managed builds, or a manually compiled
binary all work. llamavm is recommended (not required) when no llama.cpp
install is detected.

## Problem

Running llama.cpp on Apple Silicon currently requires:
- Picking a quantization that fits available unified memory while leaving
  room for context, OS, and KV cache (under-documented heuristics)
- Identifying and downloading the right GGUF file from HuggingFace
  (inconsistent naming, split files, no automated verification)
- Running llama-server with model- and hardware-appropriate flags
  (long flag set, easy to misconfigure)
- Keeping it running (no built-in process supervision)
- Discovering and fixing macOS-specific gotchas (iogpu.wired_limit_mb)

The current setup takes hours and spans multiple documentation sources.
This tool compresses it to one command per phase.

## Goals (v1)

1. One-line install via the author's existing Homebrew tap (gregmundy/tap)
2. Hardware introspection: detect chip, RAM, GPU-addressable memory, OS
3. Model installation: `llamactl add <model>` picks the right quant for
   the host and downloads it to a predictable shared location
4. Daemon launch: `llamactl serve <model> --detach` runs as a launchd
   service, exposes OpenAI-compatible endpoint
5. Live status: `llamactl status` shows model, memory, endpoint, tok/s
6. Diagnostics: `llamactl doctor` detects common issues, offers fixes
7. From Hermes on a different host, the endpoint is reachable via Tailscale
8. Works with any llama.cpp install (Homebrew, llamavm, manual); no
   tight coupling to a specific install method

## Non-goals (v1)

- Linux, Windows, WSL support
- NVIDIA, AMD, Intel GPU support
- Web UI or dashboard
- Multi-model orchestration (one model per `serve` command is fine)
- Authentication on the endpoint (relies on Tailscale for access control)
- Local quantization conversion (download pre-quantized only)
- Telemetry, usage analytics, update notifications
- Speculative decoding auto-config
- Hot model swapping
- Hard dependency on llamavm or any specific llama.cpp install method

## Target environment

- macOS 14+ on any Apple Silicon Mac (all M-series chips)
- 8 GB minimum RAM, 16 GB recommended
- Homebrew installed
- Bare-metal only (running under a hypervisor without Metal passthrough
  is rejected by `doctor` to prevent silent CPU fallback)
- Internet access for model downloads
- A working `llama-server` binary, found via PATH or configured location.
  Acceptable sources include:
  - Homebrew (`brew install llama.cpp`)
  - llamavm (`brew install gregmundy/tap/llamavm`, then `llamavm install latest`)
  - Manually compiled with Metal support
  - Any custom path set via `llamactl config llama_server_path`

## Preferred model IDs (v1)

These short-ids ship with pre-tuned `ParamsB`, `Arch`, and `MaxCtx`
metadata, enabling `add <short-id>` to auto-select a quant via §6.1:

- Qwen 2.5 (3B, 7B, 14B Instruct + Coder)
- Llama 3.1 (8B), 3.2 (3B), 3.3 (70B — only on 32GB+ hosts)
- Mistral 7B Instruct v0.3

The preferred-IDs table lives in source as a Go map; adding entries is
a code change. **However, `add` is not gated by this table** — any
HuggingFace GGUF repo path (form: `<Org>/<Repo>`) is accepted with an
explicit `--quant`. The downloaded GGUF's header supplies `ParamsB` and
`Arch` for `list` display and future selector use.

## llama-server discovery

llamactl resolves the `llama-server` binary in this order:

1. `LLAMACTL_LLAMA_SERVER_PATH` environment variable
2. `llama_server_path` from `~/.config/llamactl/config.yaml`
3. `llama-server` on `$PATH` (covers Homebrew and llamavm shims)
4. `~/.llamavm/shims/llama-server` (direct probe if PATH missed it)
5. `$(brew --prefix llama.cpp)/bin/llama-server`

If none resolve, `llamactl doctor` and any command that would invoke
llama-server prints:

> No `llama-server` found. Install via one of:
>   brew install llama.cpp
>   brew install gregmundy/tap/llamavm && llamavm install latest
> Or set `llamactl config llama_server_path /path/to/llama-server`.

llamavm is mentioned as one option, never the only option.

llamactl runs `llama-server --version` once on startup, caches the
version string, and uses it to gate flags that require a minimum version
(e.g., flash-attention support varies by build). If the detected version
is below the documented floor, llamactl warns and falls back to a
conservative recipe.

## Storage convention

Model files (GGUFs) live in a tool-agnostic shared directory:

`~/.local/share/llama-models/<model-id>/<quant>.gguf`

This is not a coordinated interop contract — it's an open convention.
Other tools (manual `llama-cli` invocations, future tools by the author,
third-party tools) can read from and write to this location if they
choose. llamactl does not assume any other tool is present.

llamactl's per-tool metadata lives at
`~/.config/llamactl/models/<model-id>.json` and references the shared
file path plus expected SHA256.

**Concurrent access:**
- Before downloading, llamactl checks the shared path. If a file exists
  with a matching SHA256 (from any source), skip the download.
- Partial downloads use `<quant>.gguf.partial` with a file lock; second
  process waits, then validates.
- `llamactl remove` deletes only its metadata by default. With `--purge`,
  also deletes the shared GGUF after warning if the file might be in use
  elsewhere (best-effort detection by checking other known metadata dirs).

**Cache** (HuggingFace API responses) lives at `~/.cache/llamactl/` and
is per-tool, not shared.

## CLI surface

```
llamactl hardware
  Detects chip, RAM, GPU memory, OS version, llama.cpp version (via
  whichever llama-server is resolved). Writes hardware.json. Idempotent.

llamactl search <query> [--refresh]
  Searches HuggingFace for GGUF repos matching the query. Preferred
  short-ids (see §4) are listed first and marked with `*`; other HF
  results follow. Results capped at 25. Cached 24h; --refresh bypasses
  cache.

llamactl add <input> [--quant <preset>] [--ctx <int>]
  Two input forms:
    - Preferred short-id (e.g. qwen2.5-7b-instruct): auto-selects quant
      via the §6.1 algorithm, downloads from the canonical HF repo,
      verifies SHA256, persists metadata with pre-tuned ParamsB/Arch.
    - HuggingFace repo path (e.g. Qwen/Qwen3-8B-Instruct-GGUF, detected
      by presence of "/"): requires explicit --quant. Downloads, verifies
      SHA256, parses the GGUF header to derive ParamsB/Arch, persists
      metadata under a derived id (basename lowercased, "-gguf" stripped).

  If the file already exists at the shared path with a matching SHA256,
  skips download and only writes llamactl metadata.

  --quant: override automatic selection (Q4_K_M, Q5_K_M, etc.). Required
           for HF-path inputs.
  --ctx:   target context size, affects quant calculation. Default 8192.
           Ignored for HF-path inputs (no auto-selection).

llamactl list
  Lists installed models: id, quant, size on disk, last served.

llamactl remove <model-id> [--purge]
  Removes llamactl metadata. With --purge, also deletes the shared
  GGUF file after best-effort check for use by other tools.

llamactl serve <model-id> [--port <int>] [--recipe <name>] [--detach]
  Starts llama-server (resolved per discovery order) with hardware-tuned
  flags. Default port 8080, default recipe chat.
  Recipes: chat, code, long-context, low-memory.
  --detach: registers a launchd LaunchAgent and returns. Otherwise
            foreground.
  Endpoint: http://0.0.0.0:<port>/v1/* (OpenAI-compatible, native to
  llama-server).

llamactl stop [<model-id>]
  Stops detached service. No argument stops all. Removes the launchd
  plist; preserves model files.

llamactl status [--json]
  Lists running services: model-id, port, memory used, uptime,
  tok/s (rolling 60s), endpoint URL.

llamactl doctor
  Checks: llama-server resolvable, llama.cpp version meets floor,
  bare-metal Apple Silicon (refuse if running under hypervisor without
  real Metal device), iogpu.wired_limit_mb appropriate for largest
  installed model, no port conflicts, model files match metadata,
  orphaned metadata, disk space available, Tailscale running if
  configured.
  Each check: ✓ OK or ✗ with cause and suggested fix.
  For sudo-required fixes, outputs the exact command for the user to
  run; never escalates silently.

llamactl update
  Self-update via Homebrew. Does not touch llama.cpp or models.

llamactl config <key> [<value>]
  Get/set keys: default_port, models_dir, hf_token, log_level,
  llama_server_path.
```

## Core logic specifications

### Quantization selection algorithm

Inputs:
- `model_size_b` (parameter count in billions, from preferred-IDs table or GGUF header)
- `model_arch` (qwen2.5 | llama3 | mistral)
- `target_ctx` (tokens; from --ctx or default 8192)
- `available_memory_gb` (gpu_addressable_memory − os_overhead − headroom)

Constants:
- `os_overhead_gb` = 4
- `headroom_gb` = 2
- `kv_cache_per_token_kb` = lookup table by `(model_arch, kv_quant)`
- `quant_size_table` = lookup table mapping `(model_size_b, quant)` → GB

Algorithm:
1. `usable = available_memory_gb − os_overhead_gb − headroom_gb`
2. `kv_cache_gb = target_ctx × kv_cache_per_token_kb(arch, q8_0) / 1024²`
3. `model_budget = usable − kv_cache_gb`
4. For each quant in `[Q5_K_M, Q4_K_M, Q4_K_S, IQ4_XS, IQ3_M, IQ3_XS, Q2_K]`:
   - If `quant_size_table(model_size_b, quant) ≤ model_budget`: return quant
5. None fit: return error with explanation and suggestion (smaller model
   or shorter context).

### Recipe → flag mapping

All recipes set `--n-gpu-layers 999` (Apple Silicon, no penalty for full
offload). `--flash-attn` is added only if the resolved llama-server
version supports it.

```
chat (default):
  --ctx-size       min(model_max_ctx, 8192)
  --cache-type-k   f16
  --cache-type-v   f16
  --threads        max(1, cpu_cores − 2)
  --mlock          if usable_memory > model_size + 4GB

long-context:
  --ctx-size       min(model_max_ctx, 32768)
  --cache-type-k   q8_0
  --cache-type-v   q8_0
  (else as chat)

code:
  --ctx-size       min(model_max_ctx, 16384)
  (else as chat; temperature is API-time, not flag)

low-memory:
  --ctx-size       4096
  --cache-type-k   q4_0
  --cache-type-v   q4_0
  --mlock          false
```

### Bare-metal enforcement

llamactl refuses to operate when running under a hypervisor without a
real Metal device. The check runs at startup and as part of `doctor`.

Algorithm:
1. Read `sysctl kern.hv_vmm_present`. If `0`, skip remaining checks
   (genuine bare metal).
2. If `1`, run `system_profiler SPDisplaysDataType -json` and inspect
   for an actual Metal device entry (presence of GPU hardware
   accessible via Metal).
3. If hypervisor flag is set AND no Metal device is present: refuse to
   proceed. Print:
   > Detected Mac VM without Metal passthrough. llamactl requires
   > bare-metal Apple Silicon — running here would silently fall back
   > to CPU and lose 5-10x performance. If you have GPU passthrough
   > configured, this check can be overridden via
   > LLAMACTL_ALLOW_VM=1 (not recommended).

### Caching

HuggingFace API responses are cached at `~/.cache/llamactl/`.

- Search results: TTL 24 hours
- Per-repo metadata: TTL 7 days
- Per-quant SHA256: cached forever (immutable once published)
- Cache key includes endpoint, query parameters, and hardware fingerprint
- `--refresh` flag on relevant commands forces cache bypass
- GGUF files themselves are not cached separately — the shared model
  directory serves that role

## File layout

```
~/.config/llamactl/
  config.yaml                 user settings (incl. llama_server_path)
  hardware.json               detected hardware
  models/<model-id>.json      per-model metadata

~/.local/share/llama-models/  open convention for any tool
  <model-id>/<quant>.gguf
  <model-id>/<quant>.gguf.partial  in-progress (file-locked)

~/.cache/llamactl/            per-tool, not shared
  hf-search/<key>.json
  hf-repo/<repo>.json

~/Library/LaunchAgents/
  com.llamactl.<model-id>.plist

~/Library/Logs/llamactl/
  <model-id>.log
```

## Acceptance criteria

MVP is complete when, on the author's M-series Mac mini, all of the
following hold:

1. `brew install gregmundy/tap/llamactl` succeeds and installs in under
   30 seconds, with no automatic install of llamavm or llama.cpp
2. `llamactl doctor` on a system with no llama.cpp installed reports
   the absence and suggests both `brew install llama.cpp` and llamavm
   as installation options, without preferring one
3. `llamactl doctor` on a system with Homebrew's llama.cpp passes the
   resolution check, reports the version
4. `llamactl doctor` on a system with llamavm passes the resolution
   check, reports the active llamavm version
5. `llamactl hardware` correctly identifies chip generation, total RAM,
   and GPU-addressable memory regardless of llama.cpp install method
6. `llamactl add qwen2.5-7b` selects Q4_K_M on a 16 GB host, downloads
   the GGUF, verifies SHA256, completes in under 10 minutes on a 100
   Mbps connection
7. If the same GGUF is already present at the shared path with a
   matching SHA256, `add` detects it, skips download, and only writes
   metadata
8. `llamactl serve qwen2.5-7b --detach` registers a launchd service
   and returns within 5 seconds, with a working endpoint at
   `http://localhost:8080`
9. A standard OpenAI chat-completions POST to that endpoint returns a
   valid response with non-zero token count
10. `llamactl status` shows the running service with memory usage
    accurate within 10% of `ps`/Activity Monitor and a recent tok/s
    measurement
11. `llamactl doctor` detects an unset or low `iogpu.wired_limit_mb`
    and outputs the exact `sudo sysctl` command for the user to run
12. `llamactl doctor` detects when running on a Mac VM without Metal
    passthrough (`sysctl kern.hv_vmm_present` set + no real Metal
    device) and refuses with a clear error message before any model
    operation
13. After a clean reboot, the launchd service auto-starts and the
    endpoint becomes available within 60 seconds
14. From a separate host on the same Tailnet, an OpenAI client
    configured with `base_url=http://llm-mini.tailnet.ts.net:8080/v1`
    successfully sends a chat completion
15. `llamactl stop qwen2.5-7b` cleanly stops the service, unloads the
    launchd plist, and the endpoint stops responding within 10 seconds
16. Switching the active llamavm version (on a system using llamavm)
    causes the next `llamactl serve` invocation to use the new
    `llama-server` binary, with no llamactl config changes required

## Implementation notes

- Language: Go. Single static binary, easy cross-compilation later.
- CLI: `spf13/cobra`. Config: `spf13/viper` or stdlib YAML, author's call.
- llama-server invocation: `os/exec` with the resolved path. Never
  hardcode `/opt/homebrew/...` or any specific install location.
- Version detection: run `llama-server --version`, parse, cache. Document
  the minimum supported version in the README and check at startup.
- Bare-metal detection: `sysctl kern.hv_vmm_present` plus
  `system_profiler SPDisplaysDataType -json` parse. Override via
  `LLAMACTL_ALLOW_VM=1` env var for users who genuinely have GPU
  passthrough configured.
- HuggingFace API: thin wrapper around HF REST endpoints. Resumable
  downloads.
- Hardware introspection: `system_profiler SPHardwareDataType -json`,
  `sysctl hw.memsize`, `sysctl iogpu.wired_limit_mb`.
- launchd plist: `text/template`, no external library. Templates embed
  the resolved llama-server path at plist-write time, so plists become
  stale if the user reinstalls llama.cpp at a different path. `doctor`
  detects stale plists and offers to regenerate.
- Distribution: gregmundy/tap, separate Formula from llamavm.

## Out of scope, post-v1 candidates

- Linux + NVIDIA support (separate code paths, share quant-selection logic)
- Multi-platform binary distribution
- Local quantization pipeline (pull original weights, convert, quantize
  via dedicated subcommand; brings Python toolchain dependency)
- Speculative decoding auto-config
- Web UI for management
- Authentication on endpoint
- Performance benchmarking and reporting
- Coordinated metadata format with future tools
