// Command llamactl-telemetryd is the sidecar daemon that aggregates
// installed-model and running-model telemetry and serves it over HTTP.
// Invoked exclusively by launchd via `llamactl telemetry enable`.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregmundy/llamactl/internal/config"
	"github.com/gregmundy/llamactl/internal/launchd"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/proc"
	"github.com/gregmundy/llamactl/internal/runner"
	"github.com/gregmundy/llamactl/internal/telemetry"
)

var telemetrydVersion = "dev"

const (
	defaultPort     = 18080
	defaultHost     = "0.0.0.0"
	defaultInterval = 2 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "llamactl-telemetryd:", err)
		os.Exit(1)
	}
}

func run() error {
	paths, err := config.New()
	if err != nil {
		return fmt.Errorf("resolve paths: %w", err)
	}
	cfg, err := config.Load(paths.ConfigFile())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	host := strings.TrimSpace(cfg.TelemetryHost)
	if host == "" {
		host = defaultHost
	}
	port := cfg.TelemetryPort
	if port == 0 {
		port = defaultPort
	}
	interval := defaultInterval
	if cfg.TelemetryInterval != "" {
		if d, err := time.ParseDuration(cfg.TelemetryInterval); err == nil {
			interval = d
		}
	}

	apiKey := os.Getenv("LLAMACTL_API_KEY")
	if apiKey == "" {
		apiKey = cfg.APIKey
	}

	if isPublicHost(host) && apiKey == "" {
		return errors.New(
			"telemetryd would bind " + host + " without authentication;\n" +
				"set api_key via `llamactl config set api_key <token>` or\n" +
				"bind locally via `llamactl config set telemetry_host 127.0.0.1`")
	}

	logsDir := filepath.Join(paths.Home, "Library", "Logs", "llamactl")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir logs: %w", err)
	}
	logPath := filepath.Join(logsDir, "telemetryd.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log %s: %w", logPath, err)
	}
	defer logFile.Close()
	logger := slog.New(slog.NewTextHandler(io.MultiWriter(logFile, os.Stderr), &slog.HandlerOptions{
		Level: slogLevel(cfg.LogLevel),
	}))

	logger.Info("telemetryd starting", "version", telemetrydVersion, "host", host, "port", port, "interval", interval)

	state := telemetry.NewState()
	launchAgentsDir := filepath.Join(paths.Home, "Library", "LaunchAgents")
	modelsDir := paths.ModelsMetaDir()

	httpClient := &http.Client{} // per-request timeout enforced in Scrape via context
	run := runner.ExecRunner{}
	poller := &telemetry.Poller{
		State:          state,
		Lister:         telemetry.LaunchdLister{},
		LaunchdService: &launchd.Service{Runner: run, UID: os.Getuid()},
		ProcInspector:  &proc.Inspector{Runner: run},
		PlistDir:       launchAgentsDir,
		HTTPClient:     httpClient,
		Interval:       interval,
		BaseURLFn:      telemetry.DefaultBaseURL,
	}

	listInstalled := func() []models.Metadata {
		store := models.NewFileStore(modelsDir)
		got, err := store.List(context.Background())
		if err != nil {
			logger.Warn("list installed", "err", err)
			return nil
		}
		return got
	}

	handler := telemetry.NewHandler(state, listInstalled, apiKey, time.Now)

	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", host, port),
		Handler: handler,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go poller.Run(ctx)

	go func() {
		<-ctx.Done()
		shutdown, sCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer sCancel()
		_ = srv.Shutdown(shutdown)
	}()

	logger.Info("listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", err)
	}
	logger.Info("telemetryd stopped")
	return nil
}

func isPublicHost(h string) bool {
	switch h {
	case "127.0.0.1", "::1", "localhost":
		return false
	}
	return true
}

func slogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
