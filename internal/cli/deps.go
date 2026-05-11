package cli

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/server"
)

var ErrUserError = errors.New("user error")

type HardwareDetector interface {
	Detect(ctx context.Context) (hardware.Info, error)
}

// ServerResolver locates the llama-server binary.
type ServerResolver interface {
	Resolve(ctx context.Context) (server.Resolution, error)
}

// ServerProber runs `llama-server --version` and caches the result.
type ServerProber interface {
	Probe(ctx context.Context, path string) (server.Version, error)
}

// MinLlamaServerBuild is the lowest llama.cpp build number llamactl will
// accept. Set to 1 (any parseable, positive build) because Homebrew uses
// upstream release tags (~4500+) while llamavm-managed builds use cmake's
// own per-build counter starting from 1 — there's no single integer floor
// that distinguishes "too old" from "modern custom build". Until we have
// a feature-detection mechanism that reads, say, --help for flag support,
// the parse-ability of `--version` is the only reliable gate.
const MinLlamaServerBuild = 1

type Deps struct {
	Stdout io.Writer
	Stderr io.Writer

	HardwareDetector HardwareDetector
	HardwareJSONPath string

	ServerResolver ServerResolver
	ServerProber   ServerProber

	LookPath func(name string) (string, error)
	Getenv   func(key string) string
	Now      func() time.Time
}
