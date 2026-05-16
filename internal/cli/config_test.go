package cli

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/config"
)

func TestConfigGetExistingKey(t *testing.T) {
	cfg := &config.Config{DefaultPort: 8080}
	var out bytes.Buffer
	d := &Deps{Config: cfg, Stdout: &out, Stderr: io.Discard}
	cmd := newConfigCmd(d)
	cmd.SetArgs([]string{"get", "default_port"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "8080" {
		t.Fatalf("got %q, want 8080", out.String())
	}
}

func TestConfigGetUnknownKeyErrors(t *testing.T) {
	d := &Deps{Config: &config.Config{}, Stdout: io.Discard, Stderr: io.Discard}
	cmd := newConfigCmd(d)
	cmd.SetArgs([]string{"get", "made_up_key"})
	err := cmd.Execute()
	if err == nil || !errors.Is(err, ErrUserError) {
		t.Fatalf("expected ErrUserError, got %v", err)
	}
}

func TestConfigSetWritesFile(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.yaml")
	d := &Deps{
		Config:     &config.Config{},
		ConfigPath: cfgPath,
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	}
	cmd := newConfigCmd(d)
	cmd.SetArgs([]string{"set", "api_key", "sk-test-xyz"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.APIKey != "sk-test-xyz" {
		t.Fatalf("APIKey=%q, want sk-test-xyz", loaded.APIKey)
	}
	// Also: in-memory cfg updated
	if d.Config.APIKey != "sk-test-xyz" {
		t.Fatalf("in-memory Config.APIKey not updated: %q", d.Config.APIKey)
	}
}

func TestConfigSetRejectsInvalidPort(t *testing.T) {
	tempDir := t.TempDir()
	d := &Deps{Config: &config.Config{}, ConfigPath: filepath.Join(tempDir, "config.yaml"), Stdout: io.Discard, Stderr: io.Discard}
	cmd := newConfigCmd(d)
	cmd.SetArgs([]string{"set", "default_port", "99999"})
	err := cmd.Execute()
	if err == nil || !errors.Is(err, ErrUserError) {
		t.Fatalf("expected ErrUserError for out-of-range port, got %v", err)
	}
}

func TestConfigSetRejectsInvalidLogLevel(t *testing.T) {
	tempDir := t.TempDir()
	d := &Deps{Config: &config.Config{}, ConfigPath: filepath.Join(tempDir, "config.yaml"), Stdout: io.Discard, Stderr: io.Discard}
	cmd := newConfigCmd(d)
	cmd.SetArgs([]string{"set", "log_level", "purple"})
	err := cmd.Execute()
	if err == nil || !errors.Is(err, ErrUserError) {
		t.Fatalf("expected ErrUserError, got %v", err)
	}
}

func TestConfigListRedactsSecrets(t *testing.T) {
	cfg := &config.Config{DefaultPort: 8080, APIKey: "sk-secret", HFToken: "hf_secret"}
	var out bytes.Buffer
	d := &Deps{Config: cfg, Stdout: &out, Stderr: io.Discard}
	cmd := newConfigCmd(d)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "sk-secret") {
		t.Fatalf("APIKey leaked:\n%s", s)
	}
	if strings.Contains(s, "hf_secret") {
		t.Fatalf("HFToken leaked:\n%s", s)
	}
	if !strings.Contains(s, "********  (set; redacted)") {
		t.Fatalf("missing exact redacted indicator:\n%s", s)
	}
	if !strings.Contains(s, "8080") {
		t.Fatalf("non-secret value missing:\n%s", s)
	}
}

func TestConfigSetCreatesFileIfMissing(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "subdir", "config.yaml")
	d := &Deps{Config: &config.Config{}, ConfigPath: cfgPath, Stdout: io.Discard, Stderr: io.Discard}
	cmd := newConfigCmd(d)
	cmd.SetArgs([]string{"set", "default_port", "8080"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(cfgPath); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	loaded, _ := config.Load(cfgPath)
	if loaded.DefaultPort != 8080 {
		t.Fatalf("DefaultPort not persisted: %d", loaded.DefaultPort)
	}
}

// TestConfigSetThenGet exercises the full set→get round-trip for every
// supported config key. For secret keys (api_key, hf_token), `get` returns the
// redacted sentinel because formatValue redacts secrets in all contexts.
func TestConfigSetThenGet(t *testing.T) {
	cases := []struct {
		key      string
		setValue string
		wantGet  string
	}{
		{"llama_server_path", "/usr/local/bin/llama-server", "/usr/local/bin/llama-server"},
		{"default_port", "11434", "11434"},
		{"models_dir", "/tmp/models", "/tmp/models"},
		{"hf_token", "hf_abc123", "********  (set; redacted)"},
		{"log_level", "debug", "debug"},
		{"api_key", "sk-round-trip", "********  (set; redacted)"},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			tempDir := t.TempDir()
			cfgPath := filepath.Join(tempDir, "config.yaml")
			d := &Deps{
				Config:     &config.Config{},
				ConfigPath: cfgPath,
				Stdout:     io.Discard,
				Stderr:     io.Discard,
			}

			// Set the value.
			setCmd := newConfigCmd(d)
			setCmd.SetArgs([]string{"set", tc.key, tc.setValue})
			if err := setCmd.Execute(); err != nil {
				t.Fatalf("set %s=%q: %v", tc.key, tc.setValue, err)
			}

			// Get it back.
			var out bytes.Buffer
			d.Stdout = &out
			getCmd := newConfigCmd(d)
			getCmd.SetArgs([]string{"get", tc.key})
			if err := getCmd.Execute(); err != nil {
				t.Fatalf("get %s: %v", tc.key, err)
			}

			got := strings.TrimSpace(out.String())
			if got != tc.wantGet {
				t.Fatalf("get %s = %q, want %q", tc.key, got, tc.wantGet)
			}
		})
	}
}

func TestConfigSetRejectsNegativePort(t *testing.T) {
	tempDir := t.TempDir()
	d := &Deps{Config: &config.Config{}, ConfigPath: filepath.Join(tempDir, "config.yaml"), Stdout: io.Discard, Stderr: io.Discard}
	cmd := newConfigCmd(d)
	// Use "--" to prevent cobra from interpreting "-1" as a shorthand flag.
	cmd.SetArgs([]string{"set", "default_port", "--", "-1"})
	err := cmd.Execute()
	if err == nil || !errors.Is(err, ErrUserError) {
		t.Fatalf("expected ErrUserError for negative port, got %v", err)
	}
}

func TestConfigSetTelemetryPortValidation(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.yaml")

	// Out-of-range port → ErrUserError.
	d := &Deps{Config: &config.Config{}, ConfigPath: cfgPath, Stdout: io.Discard, Stderr: io.Discard}
	cmd := newConfigCmd(d)
	cmd.SetArgs([]string{"set", "telemetry_port", "70000"})
	if err := cmd.Execute(); err == nil || !errors.Is(err, ErrUserError) {
		t.Fatalf("expected ErrUserError for port 70000, got %v", err)
	}

	// In-range port → persisted.
	d = &Deps{Config: &config.Config{}, ConfigPath: cfgPath, Stdout: io.Discard, Stderr: io.Discard}
	cmd = newConfigCmd(d)
	cmd.SetArgs([]string{"set", "telemetry_port", "18080"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TelemetryPort != 18080 {
		t.Errorf("TelemetryPort = %d, want 18080", loaded.TelemetryPort)
	}
}

func TestConfigSetTelemetryIntervalValidation(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.yaml")

	// Garbage → ErrUserError.
	d := &Deps{Config: &config.Config{}, ConfigPath: cfgPath, Stdout: io.Discard, Stderr: io.Discard}
	cmd := newConfigCmd(d)
	cmd.SetArgs([]string{"set", "telemetry_interval", "not-a-duration"})
	if err := cmd.Execute(); err == nil || !errors.Is(err, ErrUserError) {
		t.Fatalf("expected ErrUserError for garbage duration, got %v", err)
	}

	// Valid durations → persist.
	for _, val := range []string{"2s", "500ms", "1m"} {
		d := &Deps{Config: &config.Config{}, ConfigPath: cfgPath, Stdout: io.Discard, Stderr: io.Discard}
		cmd := newConfigCmd(d)
		cmd.SetArgs([]string{"set", "telemetry_interval", val})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error setting %q: %v", val, err)
		}
	}
}
