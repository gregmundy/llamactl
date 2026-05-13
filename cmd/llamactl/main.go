package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
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

// hfHTTPClient returns the *http.Client used for every HF API + download
// request. Transport-level timeouts protect against indefinite hangs when
// the upstream is slow or unreachable, without capping body-read duration
// (so multi-GB GGUF downloads are not artificially time-bounded).
//
//   - DialContext.Timeout: how long to wait for TCP connection setup
//   - TLSHandshakeTimeout: how long to wait for TLS negotiation
//   - ResponseHeaderTimeout: how long to wait between sending the request
//     and receiving the response headers. This is the key fix — without
//     it, a single hanging HF response stalls `fit` indefinitely.
//   - IdleConnTimeout: how long to keep idle keep-alive connections open
//
// http.Client.Timeout itself is left unset because it covers the full
// request including body read; setting it would kill ongoing GGUF
// downloads at the timeout, not just stuck headers.
func hfHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	}
	return &http.Client{Transport: transport}
}

var llamactlVersion = "dev"

func main() {
	paths, err := config.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "llamactl: cannot resolve home directory:", err)
		os.Exit(1)
	}
	run := runner.ExecRunner{}

	resolver := &server.Resolver{
		Getenv:     os.Getenv,
		LookPath:   exec.LookPath,
		HomeDir:    paths.Home,
		ConfigPath: paths.ConfigFile(),
		Runner:     run,
	}

	configPath := paths.ConfigFile()
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "llamactl: warning: load config: %v\n", err)
	}

	deps := &cli.Deps{
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
		HardwareDetector: &hardware.Detector{Runner: run},
		HardwareJSONPath: paths.HardwareJSON(),
		ServerResolver:   resolver,
		ServerProber:     &server.Prober{Runner: run},
		Config:           &cfg,
		ConfigPath:       configPath,
		LookPath:         exec.LookPath,
		Getenv:           os.Getenv,
		Now:              time.Now,
		Sleep:            time.After,
		UserHomeDir:      os.UserHomeDir,
	}

	// Phase 2: wire HFClient, Downloader, QuantSelector, ModelStore
	hfCache := hf.NewCache(paths.CacheDir())
	hfClient := hf.NewClient("https://huggingface.co", hfCache, hfHTTPClient())
	if tok := resolveHFToken(os.Getenv, &cfg); tok != "" {
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

	deps.LlamactlVersion = llamactlVersion

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

// resolveHFToken returns the Hugging Face API token to use for every HF
// call, with this precedence (highest wins):
//
//  1. LLAMACTL_HF_TOKEN env var
//  2. HF_TOKEN env var
//  3. config.yaml `hf_token` key
//  4. "" (no token — calls are anonymous)
//
// Matches the precedence shape used for `api_key` in serve.go.
//
// Before v1.4.4 the config path was dead code: `llamactl config set
// hf_token X` persisted the value but main.go never read it. This helper
// closes that gap and exists outside main() so tests can drive it without
// process-global env state.
func resolveHFToken(getenv func(string) string, cfg *config.Config) string {
	for _, k := range []string{"LLAMACTL_HF_TOKEN", "HF_TOKEN"} {
		if v := getenv(k); v != "" {
			return v
		}
	}
	if cfg != nil && cfg.HFToken != "" {
		return cfg.HFToken
	}
	return ""
}
