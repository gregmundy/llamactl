package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/config"
	"github.com/gregmundy/llamactl/internal/launchd"
)

func newTelemetryTestDeps(t *testing.T) (*Deps, *fakeLaunchdService) {
	t.Helper()
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "LaunchAgents")
	logsDir := filepath.Join(dir, "Logs")
	svc := &fakeLaunchdService{Services: map[string]launchd.ServiceInfo{}}
	cfg := &config.Config{TelemetryPort: 18080, TelemetryHost: "127.0.0.1"}
	d := &Deps{
		Stdout:          &bytes.Buffer{},
		Stderr:          io.Discard,
		Config:          cfg,
		ConfigPath:      filepath.Join(dir, "config.yaml"),
		LaunchAgentsDir: agentsDir,
		LogsDir:         logsDir,
		LaunchdService:  svc,
		Getenv:          func(string) string { return "" },
		UserHomeDir:     func() (string, error) { return dir, nil },
		LookPath: func(name string) (string, error) {
			// Return a fake binary path so resolveTelemetrydBinary
			// doesn't fall through to os.Executable.
			return filepath.Join(dir, name), nil
		},
	}
	return d, svc
}

func TestTelemetryEnable_RefusesPublicWithoutAPIKey(t *testing.T) {
	d, _ := newTelemetryTestDeps(t)
	d.Config.TelemetryHost = "0.0.0.0"
	d.Config.APIKey = ""
	cmd := newTelemetryCmd(d)
	cmd.SetArgs([]string{"enable"})
	err := cmd.Execute()
	if err == nil || !errors.Is(err, ErrUserError) {
		t.Fatalf("expected ErrUserError, got %v", err)
	}
}

func TestTelemetryEnable_PublicWithAPIKeyOK(t *testing.T) {
	d, svc := newTelemetryTestDeps(t)
	d.Config.TelemetryHost = "0.0.0.0"
	d.Config.APIKey = "sk-test"
	cmd := newTelemetryCmd(d)
	cmd.SetArgs([]string{"enable"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	plistPath := filepath.Join(d.LaunchAgentsDir, launchd.TelemetrydLabel+".plist")
	if _, err := os.Stat(plistPath); err != nil {
		t.Errorf("expected plist at %s: %v", plistPath, err)
	}
	if len(svc.Loaded) == 0 {
		t.Error("expected Load to be called")
	}
}

func TestTelemetryEnable_LocalNoAPIKeyOK(t *testing.T) {
	d, _ := newTelemetryTestDeps(t)
	// Default from newTelemetryTestDeps is 127.0.0.1 + no api_key.
	cmd := newTelemetryCmd(d)
	cmd.SetArgs([]string{"enable"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	plistPath := filepath.Join(d.LaunchAgentsDir, launchd.TelemetrydLabel+".plist")
	if _, err := os.Stat(plistPath); err != nil {
		t.Errorf("expected plist at %s: %v", plistPath, err)
	}
}

func TestTelemetryDisable_RemovesPlist(t *testing.T) {
	d, svc := newTelemetryTestDeps(t)
	plistPath := filepath.Join(d.LaunchAgentsDir, launchd.TelemetrydLabel+".plist")
	if err := os.MkdirAll(d.LaunchAgentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plistPath, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newTelemetryCmd(d)
	cmd.SetArgs([]string{"disable"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Error("expected plist to be removed")
	}
	if len(svc.Booted) == 0 {
		t.Error("expected Bootout to be called")
	}
}

func TestTelemetryDisable_IdempotentWhenStopped(t *testing.T) {
	d, _ := newTelemetryTestDeps(t)
	cmd := newTelemetryCmd(d)
	cmd.SetArgs([]string{"disable"})
	if err := cmd.Execute(); err != nil {
		t.Errorf("disable when stopped should not error, got: %v", err)
	}
}

func TestTelemetryStatus_ReportsStopped(t *testing.T) {
	d, _ := newTelemetryTestDeps(t)
	out := &bytes.Buffer{}
	d.Stdout = out
	cmd := newTelemetryCmd(d)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stopped") {
		t.Errorf("expected 'stopped' in output, got %q", out.String())
	}
}

func TestTelemetryStatus_ReportsRunning(t *testing.T) {
	d, svc := newTelemetryTestDeps(t)
	svc.Services[launchd.TelemetrydLabel] = launchd.ServiceInfo{
		Label: launchd.TelemetrydLabel, PID: 4242, State: "running",
	}
	out := &bytes.Buffer{}
	d.Stdout = out
	cmd := newTelemetryCmd(d)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "running") || !strings.Contains(s, "4242") {
		t.Errorf("expected running+pid in output, got %q", s)
	}
}

// silence unused import warning while we're in early dev
var _ = context.Background
