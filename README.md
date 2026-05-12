# llamactl

Single-binary Go CLI for running llama.cpp on Apple Silicon. Handles model download,
hardware-aware quantization selection, recipe-based llama-server invocation, launchd
lifecycle, and a doctor that catches the configuration mistakes people actually make.

Built and tested on M-series Macs. Linux and Intel are out of scope for v1.

## Install

```bash
brew install gregmundy/tap/llamactl
```

llamactl is a Homebrew Cask (prebuilt unsigned binary, ~3 MB). It does NOT auto-install
`llama-server` — you bring your own:

```bash
brew install llama.cpp                          # one option
brew install gregmundy/tap/llamavm              # the other; manages multiple builds
```

Then check your environment:

```bash
llamactl doctor
```

## Quick start

```bash
llamactl add qwen2.5-3b-instruct                # downloads, auto-selects quant
llamactl serve qwen2.5-3b-instruct --detach     # registers a launchd service
curl http://localhost:8080/v1/chat/completions  # OpenAI-compatible endpoint
llamactl status                                 # MEM, UPTIME, TOK/S
llamactl stop qwen2.5-3b-instruct               # bootout + plist removal
```

## Commands

| Command   | What it does                                                     |
|-----------|------------------------------------------------------------------|
| `hardware`| Detect chip, RAM, GPU memory, OS version; cache to hardware.json |
| `doctor`  | Run 10 health checks; exits 2 on any failure                     |
| `search`  | Search HuggingFace for GGUF repos (preferred IDs marked `*`)     |
| `add`     | Download a preferred short-id or any HF GGUF repo                |
| `list`    | List installed models with PARAMS, SIZE, ADDED, LAST-SERVED      |
| `remove`  | Remove metadata (use `--purge` to also delete the GGUF)          |
| `serve`   | Run llama-server (foreground or `--detach` to launchd)           |
| `status`  | Show running detached services (table or `--json`)               |
| `stop`    | Stop a service (or all services if no id)                        |

## Recipes

`serve` flags are assembled from a named recipe. Default is `chat`.

| Recipe         | Context  | KV cache    | Notes                          |
|----------------|----------|-------------|--------------------------------|
| `chat`         | 8 K      | f16         | Default                        |
| `code`         | 16 K     | f16         | Longer context for code work   |
| `long-context` | 32 K     | q8_0        | Memory-efficient large context |
| `low-memory`   | 4 K      | q4_0        | Minimal footprint              |

## Storage

| Path                                              | What lives there                          |
|---------------------------------------------------|-------------------------------------------|
| `~/.local/share/llama-models/<id>/<quant>.gguf`   | Model weights (shared with llamavm)       |
| `~/.config/llamactl/models/<id>.json`             | Per-model metadata (llamactl-specific)    |
| `~/.cache/llamactl/hf-*`                          | HuggingFace API cache                     |
| `~/Library/LaunchAgents/com.llamactl.<id>.plist`  | Detached-service plists                   |
| `~/Library/Logs/llamactl/<id>.log`                | Per-model server logs                     |

## Environment variables

| Variable | Effect |
|----------|--------|
| `LLAMACTL_LLAMA_SERVER_PATH` | Override llama-server discovery |
| `LLAMACTL_ALLOW_VM` | Permit running in a VM without Metal passthrough (NOT recommended) |
| `HF_TOKEN` / `LLAMACTL_HF_TOKEN` | Optional HuggingFace bearer token, sent on every API request. Useful if you hit anonymous rate limits. |

## Build from source

```bash
git clone https://github.com/gregmundy/llamactl
cd llamactl
go build -o llamactl ./cmd/llamactl
./llamactl --help
```

## License

MIT — see [LICENSE](LICENSE).
