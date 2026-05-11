# llamactl

> Single-binary CLI for running llama.cpp on Apple Silicon.

Currently under construction. See `docs/llamactl-prd-v1.5.md` for the spec.

## Requirements

- macOS 14+ on Apple Silicon
- A `llama-server` binary on PATH (via `brew install llama.cpp` or `brew install gregmundy/tap/llamavm && llamavm install latest`)

## Development

```bash
go build ./cmd/llamactl
./llamactl --help
go test ./...
```
