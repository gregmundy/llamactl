package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/gregmundy/llamactl/internal/download"
	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/launchd"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/runner"
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

// HFClient is the HuggingFace API + bytes seam.
type HFClient interface {
	Search(ctx context.Context, query string) ([]hf.SearchHit, error)
	SearchRefresh(ctx context.Context, query string) ([]hf.SearchHit, error)
	RepoInfo(ctx context.Context, repoID string) (hf.Repo, error)
	FetchRange(ctx context.Context, repoID, file string, offset, end int64, w io.Writer) error
}

// Downloader resolves a single Request → on-disk GGUF.
type Downloader interface {
	Get(ctx context.Context, req download.Request) error
}

// QuantSelector picks the best-fitting quant for a model on a host.
type QuantSelector interface {
	Select(model models.Model, hw hardware.Info, targetCtx int) (models.Quant, error)
}

// ModelStore is per-tool metadata storage.
type ModelStore interface {
	List(ctx context.Context) ([]models.Metadata, error)
	Get(ctx context.Context, id string) (models.Metadata, error)
	Put(ctx context.Context, m models.Metadata) error
	Delete(ctx context.Context, id string) error
}

// FileSystem is a narrow seam for the disk operations cli/add/list/remove need.
type FileSystem interface {
	Stat(path string) (os.FileInfo, error)
	Remove(path string) error
	MkdirAll(path string, perm os.FileMode) error
}

// LaunchdService wraps launchctl operations.
type LaunchdService interface {
	Load(ctx context.Context, plistPath string) error
	Bootout(ctx context.Context, label string) error
	Print(ctx context.Context, label string) (launchd.ServiceInfo, error)
	List(ctx context.Context) ([]launchd.ServiceInfo, error)
}

// PortAllocator returns a bindable TCP port.
type PortAllocator interface {
	Free(preferred int) (int, error)
}

// ProcInspector queries the kernel about a running pid.
type ProcInspector interface {
	RSS(pid int) (int64, error)
	Uptime(pid int) (time.Duration, error)
}

// TokRateReader computes tokens/sec from a per-model log file.
type TokRateReader interface {
	Rate(logPath string, window time.Duration, now time.Time) (float64, error)
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

	HFClient      HFClient
	Downloader    Downloader
	QuantSelector QuantSelector
	ModelStore    ModelStore
	FS            FileSystem

	ModelsConfigDir string
	SharedModelsDir string
	HFCacheDir      string

	LaunchdService LaunchdService
	PortAllocator  PortAllocator
	ProcInspector  ProcInspector
	TokRateReader  TokRateReader

	// Runner is the shared subprocess seam (Phase 1's runner.CommandRunner).
	// Doctor's Tailscale check shells out via this; future commands can too.
	Runner runner.CommandRunner

	LaunchAgentsDir string // ~/Library/LaunchAgents
	LogsDir         string // ~/Library/Logs/llamactl

	LookPath func(name string) (string, error)
	Getenv   func(key string) string
	Now      func() time.Time
}

// OSFileSystem is the production FileSystem backed by package os.
type OSFileSystem struct{}

func (OSFileSystem) Stat(p string) (os.FileInfo, error)     { return os.Stat(p) }
func (OSFileSystem) Remove(p string) error                  { return os.Remove(p) }
func (OSFileSystem) MkdirAll(p string, m os.FileMode) error { return os.MkdirAll(p, m) }

// SelectorAdapter wraps the package-level models.SelectQuant in the
// QuantSelector interface.
type SelectorAdapter struct{}

func (SelectorAdapter) Select(m models.Model, hi hardware.Info, ctx int) (models.Quant, error) {
	return models.SelectQuant(m, hi, ctx)
}
