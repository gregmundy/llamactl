package server

import (
	"bytes"
	"context"
	"fmt"
	"sync"
)

// Prober caches `llama-server --version` and `--help` output keyed by the
// binary path. Used by doctor, recipe-flag gating, and capability-aware
// argv emission.
type Prober struct {
	Runner CommandRunner

	mu        sync.Mutex
	versions  map[string]Version
	capsCache map[string]Capabilities
}

// Probe runs `<path> --version` (only on first call per path) and returns
// the parsed Version.
func (p *Prober) Probe(ctx context.Context, path string) (Version, error) {
	p.mu.Lock()
	if v, ok := p.versions[path]; ok {
		p.mu.Unlock()
		return v, nil
	}
	p.mu.Unlock()

	var combined bytes.Buffer
	if err := p.Runner.Run(ctx, path, []string{"--version"}, "", &combined, &combined); err != nil {
		return Version{}, fmt.Errorf("run %s --version: %w", path, err)
	}
	v, err := ParseVersion(combined.String())
	if err != nil {
		return Version{}, err
	}
	p.mu.Lock()
	if p.versions == nil {
		p.versions = make(map[string]Version)
	}
	p.versions[path] = v
	p.mu.Unlock()
	return v, nil
}

// Capabilities runs `<path> --help` (only on first call per path) and
// returns capability flags parsed from the help text. Returns the zero
// Capabilities (and a wrapped error) if the subprocess fails — callers
// should treat that as "assume legacy syntax."
func (p *Prober) Capabilities(ctx context.Context, path string) (Capabilities, error) {
	p.mu.Lock()
	if c, ok := p.capsCache[path]; ok {
		p.mu.Unlock()
		return c, nil
	}
	p.mu.Unlock()

	var combined bytes.Buffer
	if err := p.Runner.Run(ctx, path, []string{"--help"}, "", &combined, &combined); err != nil {
		return Capabilities{}, fmt.Errorf("run %s --help: %w", path, err)
	}
	c := parseHelpForCaps(combined.String())
	p.mu.Lock()
	if p.capsCache == nil {
		p.capsCache = make(map[string]Capabilities)
	}
	p.capsCache[path] = c
	p.mu.Unlock()
	return c, nil
}
