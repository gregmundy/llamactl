# llamactl

> Single-binary CLI for running llama.cpp on Apple Silicon.

**Status:**
- **Phase 1 (shipped 2026-05-10):** `hardware`, `doctor`
- **Phase 2 (this branch):** `search`, `add`, `list`, `remove`
- **Phase 3 (future):** `serve` + launchd, `status`, `stop`

See `docs/llamactl-prd-v1.5.md` for the spec.

## Requirements

- macOS 14+ on Apple Silicon
- A `llama-server` binary on PATH (via `brew install llama.cpp` or `brew install gregmundy/tap/llamavm && llamavm install latest`)

## Install (development)

```bash
git clone https://github.com/gregmundy/llamactl
cd llamactl
go build ./cmd/llamactl
./llamactl --help
```

## Commands

| Command | Description |
|---------|-------------|
| `llamactl hardware` | Detect chip, RAM, OS, iogpu cap, VM state. Writes `~/.config/llamactl/hardware.json`. |
| `llamactl doctor` | Verify bare-metal Apple Silicon, llama-server resolvable, version floor, iogpu cap. Exits 2 on any failure. |
| `llamactl search <query> [--refresh]` | List whitelisted models matching a HuggingFace query, with available quants. |
| `llamactl add <model-id> [--quant Q] [--ctx N]` | Auto-select the best quant for your host, download to `~/.local/share/llama-models/`, verify SHA256, write per-tool metadata. |
| `llamactl list` | Show installed models, sizes, and on-disk status. |
| `llamactl remove <model-id> [--purge]` | Drop per-tool metadata. With `--purge`, also delete the shared GGUF (best-effort cross-tool check). |

## Environment variables

| Variable | Effect |
|----------|--------|
| `LLAMACTL_LLAMA_SERVER_PATH` | Override llama-server discovery |
| `LLAMACTL_ALLOW_VM` | Permit running in a VM without Metal passthrough (NOT recommended) |
| `HF_TOKEN` / `LLAMACTL_HF_TOKEN` | Optional HuggingFace bearer token, sent on every API request. Useful if you hit anonymous rate limits. |

## Development

```bash
go test ./...
```
