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

// MinLlamaServerBuild is the lowest llama.cpp build number llamactl supports.
// Below this, doctor warns and recipes fall back to a conservative flag set.
const MinLlamaServerBuild = 3500

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
