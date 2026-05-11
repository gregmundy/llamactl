# llamactl

> Single-binary CLI for running llama.cpp on Apple Silicon.

**Status:** Phase 1 (foundation + introspection). `hardware` and `doctor` work; `add` and `serve` arrive in later phases. See `docs/llamactl-prd-v1.5.md` for the spec.

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

## Environment variables

| Variable | Effect |
|----------|--------|
| `LLAMACTL_LLAMA_SERVER_PATH` | Override llama-server discovery |
| `LLAMACTL_ALLOW_VM` | Permit running in a VM without Metal passthrough (NOT recommended) |

## Development

```bash
go test ./...
```
