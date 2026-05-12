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

## Using the endpoint

llama-server's OpenAI-compatible API works with any client that takes a `base_url`.

### Python

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8080/v1", api_key="not-needed")
resp = client.chat.completions.create(
    model="llamactl",  # any non-empty string
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

Aider, Continue, Cursor, and Zed all accept a custom OpenAI base URL. Set the model
provider to OpenAI-compatible and the base URL to `http://localhost:8080/v1` (or the
port shown by `llamactl status`).

### From another machine on your Tailnet

llama-server binds to `0.0.0.0`, so any host on your Tailnet can reach it:

```
base_url=http://llm-mini.tailnet.ts.net:8080/v1
```

## Commands

| Command    | What it does                                                       |
|------------|--------------------------------------------------------------------|
| `hardware` | Detect chip, RAM, GPU memory, OS version; cache to hardware.json   |
| `doctor`   | Run 12 health checks; exits 2 on any failure                       |
| `search`   | Search HuggingFace for GGUF repos (preferred IDs marked `*`)       |
| `fit`      | Rank HF GGUF variants by fit on this host; `--install` picks top ✓ |
| `add`      | Download a preferred short-id or any HF GGUF repo                  |
| `list`     | List installed models with PARAMS, SIZE, ADDED, LAST-SERVED        |
| `remove`   | Remove metadata (use `--purge` to also delete the GGUF)            |
| `serve`    | Run llama-server (foreground or `--detach` to launchd)             |
| `status`   | Show running detached services (table or `--json`)                 |
| `stop`     | Stop a service (or all services if no id)                          |
| `cache`    | `cache prune [--all]` — clear stale HuggingFace API cache          |

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

## Tips

- Default port is 8080. If it's busy, `serve` shifts to the next free port in `[8080, 8180)`; `status` shows the actual port.
- Per-model logs live at `~/Library/Logs/llamactl/<id>.log` — `tail -f` for live debugging. Rotated automatically at 10 MiB (3 generations kept).
- Detached services survive reboot (launchd `RunAtLoad` + `KeepAlive`). Run `stop` to free GPU memory; `serve --detach` to bring it back.
- `llamactl fit <query>` ranks new HuggingFace models against your host before download.
- `llamactl cache prune` clears stale HuggingFace API cache (auto-pruned at 30 days but the command lets you force it).

## License

MIT — see [LICENSE](LICENSE).
