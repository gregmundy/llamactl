package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
)

// Prober caches the parsed `llama-server --version` output keyed by the
// binary path. Used by doctor and by Phase 3's recipe-flag gating.
type Prober struct {
	Runner CommandRunner

	mu    sync.Mutex
	cache map[string]Version
}

// Probe runs `<path> --version` (only on first call per path) and returns
// the parsed Version.
func (p *Prober) Probe(ctx context.Context, path string) (Version, error) {
	p.mu.Lock()
	if v, ok := p.cache[path]; ok {
		p.mu.Unlock()
		return v, nil
	}
	p.mu.Unlock()

	var stdout bytes.Buffer
	if err := p.Runner.Run(ctx, path, []string{"--version"}, "", &stdout, io.Discard); err != nil {
		return Version{}, fmt.Errorf("run %s --version: %w", path, err)
	}
	v, err := ParseVersion(stdout.String())
	if err != nil {
		return Version{}, err
	}
	p.mu.Lock()
	if p.cache == nil {
		p.cache = make(map[string]Version)
	}
	p.cache[path] = v
	p.mu.Unlock()
	return v, nil
}
