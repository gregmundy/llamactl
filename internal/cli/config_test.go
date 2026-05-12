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
	if !strings.Contains(s, "********") {
		t.Fatalf("missing redacted indicator:\n%s", s)
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
}
