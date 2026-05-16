# llamactl

A single-binary Go CLI for running llama.cpp on Apple Silicon. Manages model downloads with hardware-aware quantization selection, recipe-based `llama-server` invocation, launchd lifecycle, and a doctor that catches the configuration mistakes people actually make.

**Requirements:** Apple Silicon M-series Mac. macOS only. Linux and Intel are out of scope for v1.

## Why llamactl

`llama.cpp` is excellent. The orchestration around it is the bottleneck:

- Picking the right quantization for your RAM is guesswork.
- `llama-server` flags need tuning per workload (context, KV cache, flash-attn).
- Running it persistently means hand-writing launchd plists.
- Diagnosing why it won't bind, won't start, or runs slow means digging through llama.cpp's logs.

llamactl handles all of that with one command per intent. `add` downloads and picks a quant. `serve` runs `llama-server` with sensible defaults. `doctor` tells you what's wrong with your config.

## Install

```sh
brew install gregmundy/tap/llamactl
```

llamactl is a Homebrew Cask (prebuilt unsigned binary, ~3 MB). It does NOT bundle `llama-server`. Install one of:

```sh
brew install llama.cpp                 # the simple option
brew install gregmundy/tap/llamavm     # manages multiple llama.cpp builds
```

Then verify your environment:

```sh
llamactl doctor
```

## Quick start

```sh
llamactl add qwen2.5-3b-instruct          # downloads, picks the best quant for your RAM
llamactl serve qwen2.5-3b-instruct --detach
curl http://localhost:8080/v1/chat/completions \
  -d '{"model":"llamactl","messages":[{"role":"user","content":"hello"}]}'

llamactl status                           # MEM, UPTIME, TOK/S
llamactl stop qwen2.5-3b-instruct
```

## Commands

| Command | What it does |
| --- | --- |
| `hardware` | Detect chip, RAM, GPU memory, OS version; cache to `hardware.json` |
| `doctor` | Run 14 health checks; exits 2 on any failure |
| `search` | Search HuggingFace for GGUF repos (preferred IDs marked `*`) |
| `fit` | Rank HF GGUF variants by fit on this host; `--install` picks top ✓ |
| `add` | Download a preferred short-id or any HF GGUF repo |
| `list` | List installed models with PARAMS, SIZE, ADDED, LAST-SERVED |
| `remove` | Remove metadata (use `--purge` to also delete the GGUF) |
| `serve` | Run `llama-server` (foreground, or `--detach` to launchd) |
| `status` | Show running detached services (table or `--json`) |
| `stop` | Stop a service (or all services if no id) |
| `cache` | `cache prune [--all]`: clear stale HuggingFace API cache |
| `config` | `config get/set/list <key> [<value>]`: manage llamactl settings |
| `update` | Upgrade llamactl via Homebrew (`--refresh` bypasses 24h cache) |

## Recipes

`serve` flags are assembled from a named recipe. Default is `chat`.

| Recipe | Context | KV cache | Notes |
| --- | --- | --- | --- |
| `chat` | 8 K | f16 | Default |
| `code` | 16 K | f16 | Longer context for code work |
| `long-context` | 32 K | q8_0 | Memory-efficient large context |
| `low-memory` | 4 K | q4_0 | Minimal footprint |

## Speculative decoding

llamactl can wire `llama-server`'s `--model-draft` flag for assisted decoding, where a smaller draft model proposes tokens that the main model verifies in parallel. Typical speedup: 1.5-3× depending on workload and draft quality.

### Finding draft candidates

```sh
llamactl fit --speculative <main-model>
```

Lists installed models that share the main's architecture, sorted by closest fit to the ideal 5-15× size ratio:

```
Draft candidates for qwen2.5-32b-instruct (32 B, qwen2.5):

DRAFT ID                ARCH     PARAMSB  RATIO   COMBINED RAM   VERDICT
qwen2.5-3b-instruct     qwen2.5  3 B      10.7×   24.1 GB        ✓ ok
qwen2.5-0.5b-instruct   qwen2.5  0.5 B    64.0×   22.3 GB        ⚠ ratio-high
```

### Serving with a draft

```sh
llamactl serve qwen2.5-32b-instruct --draft qwen2.5-3b-instruct
```

The draft must already be installed. Mismatched architectures are refused (tokenizers diverge across families). Size ratios outside 5-15× warn but proceed.

Detached serves (`--detach`) embed `--model-draft` and `--ctx-size-draft` in the LaunchAgent plist, so the pairing persists across reboots. Re-running `serve --detach` without `--draft` clears the pairing.

### Caveats

- Both models must be installed via `add`. llamactl does not auto-download a draft.
- The draft's context window is capped at `min(main_ctx, draft.MaxCtx)`. Exceeding the draft's training context degrades alignment.
- Speedup is workload-dependent. Batch size, temperature, and prompt structure all matter. The ratio heuristic is informational.

## Using the API

`llama-server` exposes an OpenAI-compatible API. Any client that accepts a base URL works.

### Python

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8080/v1", api_key="not-needed")
resp = client.chat.completions.create(
    model="llamactl",
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

Aider, Continue, Cursor, and Zed all accept a custom OpenAI base URL. Set the provider to OpenAI-compatible and the base URL to `http://localhost:8080/v1` (or whichever port `llamactl status` reports).

### Remote access

`llama-server` binds to `0.0.0.0`, so any host on your Tailnet can reach it:

```
base_url=http://llm-mini.tailnet.ts.net:8080/v1
```

## Authentication

By default, llamactl serves unauthenticated. Anyone on your Tailnet can reach the endpoint. Enable opt-in token authentication:

```sh
llamactl config set api_key sk-your-token-here    # persisted
export LLAMACTL_API_KEY=sk-your-token-here        # or env var (takes precedence)
llamactl serve qwen2.5-3b-instruct --detach       # plist embeds --api-key
```

Clients then pass `Authorization: Bearer sk-your-token-here`:

```sh
curl -H "Authorization: Bearer sk-your-token-here" \
  http://localhost:8080/v1/chat/completions \
  -d '{"model":"llamactl","messages":[{"role":"user","content":"hello"}]}'
```

`llamactl doctor` warns when a service binds publicly (`0.0.0.0`) without an `api_key` configured. `llamactl config list` redacts `api_key` and `hf_token` as `********`.

## Telemetry API

`llamactl-telemetryd` is an optional sidecar daemon that exposes a JSON endpoint summarizing installed and running models. Useful for personal dashboards, status widgets, or any external API that wants visibility into what the host is doing.

Enable it (requires `api_key` when binding publicly):

```sh
llamactl config set api_key sk-your-token-here
llamactl telemetry enable
```

The daemon listens on `0.0.0.0:18080` by default. Override per-key with `telemetry_host`, `telemetry_port`, `telemetry_interval`.

Fetch the current snapshot:

```sh
curl -H "Authorization: Bearer sk-your-token-here" \
  http://llm-mini.tailnet.ts.net:18080/v1/telemetry
```

Response shape:

```json
{
  "generated_at": "2026-05-16T14:23:11Z",
  "installed": [
    {"id":"qwen2.5-3b-instruct","params_b":3.0,"quant":"Q5_K_M","size_bytes":2469606195}
  ],
  "running": [
    {
      "id":"qwen2.5-3b-instruct",
      "port":8082,
      "recipe":"chat",
      "size_bytes":2469606195,
      "memory_bytes":695894016,
      "state":"idle",
      "tokens_per_second":0.0,
      "tokens_predicted_total":1280,
      "uptime_seconds":3621
    }
  ]
}
```

`tokens_per_second` is a rolling rate computed over the most recent `telemetry_interval` (default 2 s). It is `null` immediately after enable (no prior sample to delta against), and `0.0` when no generation has happened between two polls.

`state` enumerates `idle | active | loading | metrics_disabled | unreachable`.

Management:

```sh
llamactl telemetry status     # show pid/port/host
llamactl telemetry disable    # stop and remove the plist
```

`llamactl doctor` flags telemetry misconfigurations as part of its 15 checks.

## Reference

### Storage paths

| Path | Contents |
| --- | --- |
| `~/.local/share/llama-models/<id>/<quant>.gguf` | Model weights (shared with llamavm) |
| `~/.config/llamactl/models/<id>.json` | Per-model metadata (llamactl-specific) |
| `~/.cache/llamactl/hf-*` | HuggingFace API cache |
| `~/Library/LaunchAgents/com.llamactl.<id>.plist` | Detached-service plists |
| `~/Library/Logs/llamactl/<id>.log` | Per-model server logs |

### Environment variables

| Variable | Effect |
| --- | --- |
| `LLAMACTL_LLAMA_SERVER_PATH` | Override `llama-server` discovery |
| `LLAMACTL_ALLOW_VM` | Permit running in a VM without Metal passthrough (not recommended) |
| `LLAMACTL_API_KEY` | API token for the server; takes precedence over `config set api_key` |
| `HF_TOKEN` / `LLAMACTL_HF_TOKEN` | HuggingFace bearer token. Useful if you hit anonymous rate limits. |

### Build from source

```sh
git clone https://github.com/gregmundy/llamactl
cd llamactl
go build -o llamactl ./cmd/llamactl
./llamactl --help
```

## Tips

- Default port is 8080. If it's busy, `serve` shifts to the next free port in `[8080, 8180)`. `status` shows the actual port.
- Per-model logs live at `~/Library/Logs/llamactl/<id>.log`. `tail -f` for live debugging. Rotated automatically at 10 MiB (3 generations kept).
- Detached services survive reboot (launchd `RunAtLoad` + `KeepAlive`). Run `stop` to free GPU memory; `serve --detach` to bring it back.
- `llamactl fit <query>` ranks new HuggingFace models against your host before download.
- `llamactl cache prune` clears stale HuggingFace API cache (auto-pruned at 30 days, but the command lets you force it).

## Troubleshooting

When `list` shows `?` for `ParamsB`, the model's GGUF is missing both `general.parameter_count` and `general.size_label`. The tensor-shape fallback handles most cases for `llama`, `qwen2`, `qwen3`, `gemma3`, and `mistral` architectures automatically on the next `list` invocation. Unknown architectures still display `?`.
