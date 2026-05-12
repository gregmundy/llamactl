// Package server resolves and probes the llama-server binary. The resolver
// follows PRD §4 discovery order; the probe runs `llama-server --version`
// once and caches the parsed output. Both are constructed in main.go and
// passed to cli via narrow interfaces.
package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Source identifies which discovery step produced the resolved path. Useful
// for `doctor` to tell the user where the binary came from.
type Source int

const (
	SourceUnknown Source = iota
	SourceEnv
	SourceConfig
	SourcePATH
	SourceLlamavmShim
	SourceBrew
)

func (s Source) String() string {
	switch s {
	case SourceEnv:
		return "LLAMACTL_LLAMA_SERVER_PATH"
	case SourceConfig:
		return "config.yaml (llama_server_path)"
	case SourcePATH:
		return "$PATH"
	case SourceLlamavmShim:
		return "~/.llamavm/shims/llama-server"
	case SourceBrew:
		return "brew --prefix llama.cpp"
	}
	return "unknown"
}

// Resolution is the outcome of a successful Resolve.
type Resolution struct {
	Path   string
	Source Source
}

// ErrNotFound is returned by Resolve when none of the discovery steps locate
// a llama-server binary.
var ErrNotFound = errors.New("no llama-server found")

// CommandRunner is the runner.CommandRunner shape, redeclared here so this
// package has no dependency on internal/runner.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
}

// Resolver finds llama-server using the PRD §4 priority order. All
// dependencies are injectable so tests don't touch the real filesystem
// outside their TempDir.
//
// Resolve memoizes successful resolutions; mu/cached are internal state.
// Use a pointer (*Resolver) at call sites that need the cache to be
// shared across calls (the production wiring does).
type Resolver struct {
	Getenv     func(string) string
	LookPath   func(string) (string, error)
	HomeDir    string // typically os.UserHomeDir() at startup
	ConfigPath string // typically config.Paths{}.ConfigFile()
	Runner     CommandRunner

	mu     sync.Mutex
	cached *Resolution
}

// Resolve walks the five-step discovery order and returns the first match.
// A path counts as a match only if it points to an existing file (except for
// LookPath, which already guarantees the file exists and is executable).
//
// Successful resolutions are cached; failed resolutions (ErrNotFound) are
// not, so a transient install can be picked up on a later call. Mirrors
// the Prober memoization pattern — doctor's resolvable + version-floor
// checks each invoke Resolve and would otherwise pay 2x the discovery cost.
func (r *Resolver) Resolve(ctx context.Context) (Resolution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cached != nil {
		return *r.cached, nil
	}
	res, err := r.resolveUncached(ctx)
	if err != nil {
		return Resolution{}, err
	}
	r.cached = &res
	return res, nil
}

func (r *Resolver) resolveUncached(ctx context.Context) (Resolution, error) {
	// Step 1: LLAMACTL_LLAMA_SERVER_PATH env var
	if p := r.Getenv("LLAMACTL_LLAMA_SERVER_PATH"); p != "" {
		if exists(p) {
			return Resolution{Path: p, Source: SourceEnv}, nil
		}
	}

	// Step 2: llama_server_path field in config.yaml
	if p := r.fromConfig(); p != "" && exists(p) {
		return Resolution{Path: p, Source: SourceConfig}, nil
	}

	// Step 3: llama-server on $PATH — LookPath already verifies existence
	if p, err := r.LookPath("llama-server"); err == nil {
		return Resolution{Path: p, Source: SourcePATH}, nil
	}

	// Step 4: ~/.llamavm/shims/llama-server (llamavm-managed)
	shim := filepath.Join(r.HomeDir, ".llamavm", "shims", "llama-server")
	if exists(shim) {
		return Resolution{Path: shim, Source: SourceLlamavmShim}, nil
	}

	// Step 5: $(brew --prefix llama.cpp)/bin/llama-server
	if p, ok := r.fromBrew(ctx); ok && exists(p) {
		return Resolution{Path: p, Source: SourceBrew}, nil
	}

	return Resolution{}, ErrNotFound
}

func (r *Resolver) fromConfig() string {
	b, err := os.ReadFile(r.ConfigPath)
	if err != nil {
		return ""
	}
	var doc struct {
		LlamaServerPath string `yaml:"llama_server_path"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return ""
	}
	return doc.LlamaServerPath
}

func (r *Resolver) fromBrew(ctx context.Context) (string, bool) {
	var stdout bytes.Buffer
	if err := r.Runner.Run(ctx, "brew", []string{"--prefix", "llama.cpp"}, "", &stdout, io.Discard); err != nil {
		return "", false
	}
	prefix := strings.TrimSpace(stdout.String())
	if prefix == "" {
		return "", false
	}
	return filepath.Join(prefix, "bin", "llama-server"), true
}

func exists(p string) bool {
	if p == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
