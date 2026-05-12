package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gregmundy/llamactl/internal/cli"
	"github.com/gregmundy/llamactl/internal/config"
	"github.com/gregmundy/llamactl/internal/download"
	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/launchd"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/proc"
	"github.com/gregmundy/llamactl/internal/runner"
	"github.com/gregmundy/llamactl/internal/server"
)

var llamactlVersion = "dev"

func main() {
	paths, err := config.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "llamactl: cannot resolve home directory:", err)
		os.Exit(1)
	}
	run := runner.ExecRunner{}

	resolver := server.Resolver{
		Getenv:     os.Getenv,
		LookPath:   exec.LookPath,
		HomeDir:    paths.Home,
		ConfigPath: paths.ConfigFile(),
		Runner:     run,
	}

	deps := &cli.Deps{
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
		HardwareDetector: &hardware.Detector{Runner: run},
		HardwareJSONPath: paths.HardwareJSON(),
		ServerResolver:   resolver,
		ServerProber:     &server.Prober{Runner: run},
		LookPath:         exec.LookPath,
		Getenv:           os.Getenv,
		Now:              time.Now,
	}

	// Phase 2: wire HFClient, Downloader, QuantSelector, ModelStore
	hfCache := hf.NewCache(paths.CacheDir())
	hfClient := hf.NewClient("https://huggingface.co", hfCache, nil)
	if tok := firstNonEmptyEnv("LLAMACTL_HF_TOKEN", "HF_TOKEN"); tok != "" {
		hfClient = hfClient.WithToken(tok)
	}

	deps.HFClient = hfClient
	deps.Downloader = &download.Downloader{Ranger: hfClient, Stderr: os.Stderr}
	deps.QuantSelector = cli.SelectorAdapter{}
	deps.ModelStore = models.NewFileStore(paths.ModelsMetaDir())
	deps.FS = cli.OSFileSystem{}
	deps.ModelsConfigDir = paths.ModelsMetaDir()
	deps.SharedModelsDir = paths.DataDir()
	deps.HFCacheDir = paths.CacheDir()

	// Phase 3 wiring.
	launchAgentsDir := filepath.Join(paths.Home, "Library", "LaunchAgents")
	logsDir := filepath.Join(paths.Home, "Library", "Logs", "llamactl")

	launchdSvc := &launchd.Service{Runner: run, UID: os.Getuid()}
	deps.LaunchdService = &cli.LaunchdServiceAdapter{Service: launchdSvc, AgentsDir: launchAgentsDir}
	deps.PortAllocator = proc.Allocator{}
	deps.ProcInspector = &proc.Inspector{Runner: run}
	deps.TokRateReader = &proc.TailRate{}
	deps.Runner = run
	deps.LaunchAgentsDir = launchAgentsDir
	deps.LogsDir = logsDir

	root := cli.NewRoot(deps, llamactlVersion)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, cli.ErrUserError) {
			os.Exit(2)
		}
		// Foreground `serve` propagates llama-server's own exit code (PRD §9)
		// so scripts can react to crashes.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "llamactl:", err)
		os.Exit(1)
	}
}

func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
