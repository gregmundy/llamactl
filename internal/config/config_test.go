package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "config.yaml")
	orig := Config{
		LlamaServerPath: "/path/to/llama",
		DefaultPort:     8080,
		APIKey:          "sk-test-123",
	}
	if err := Save(path, orig); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != orig {
		t.Fatalf("round-trip mismatch:\nwant %+v\ngot  %+v", orig, loaded)
	}
}

func TestSaveAtomicNoPartialOnError(t *testing.T) {
	tempDir := t.TempDir()
	readOnly := filepath.Join(tempDir, "ro")
	if err := os.Mkdir(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(readOnly, "config.yaml")
	err := Save(path, Config{DefaultPort: 8080})
	if err == nil {
		t.Fatal("expected error writing to read-only dir")
	}
	entries, _ := os.ReadDir(readOnly)
	if len(entries) > 0 {
		t.Fatalf("partial tmp file left behind: %v", entries)
	}
}

func TestSaveSecretFilePerms(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "config.yaml")
	if err := Save(path, Config{APIKey: "sk-test"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config file mode = %o, want 0600 (holds tokens)", info.Mode().Perm())
	}
}

func TestPaths_RespectXDGOverrides(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	t.Setenv("XDG_CACHE_HOME", "/tmp/cache")
	t.Setenv("XDG_DATA_HOME", "/tmp/data")
	p := Paths{Home: "/home/u"}
	if got := p.ConfigDir(); got != "/tmp/cfg/llamactl" {
		t.Errorf("ConfigDir = %q", got)
	}
	if got := p.CacheDir(); got != "/tmp/cache/llamactl" {
		t.Errorf("CacheDir = %q", got)
	}
	if got := p.DataDir(); got != "/tmp/data/llama-models" {
		t.Errorf("DataDir = %q", got)
	}
}

func TestPaths_DefaultsWhenNoXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	p := Paths{Home: "/home/u"}
	if got := p.ConfigDir(); got != "/home/u/.config/llamactl" {
		t.Errorf("ConfigDir = %q", got)
	}
	if got := p.CacheDir(); got != "/home/u/.cache/llamactl" {
		t.Errorf("CacheDir = %q", got)
	}
	if got := p.DataDir(); got != "/home/u/.local/share/llama-models" {
		t.Errorf("DataDir = %q", got)
	}
	if got := p.HardwareJSON(); got != "/home/u/.config/llamactl/hardware.json" {
		t.Errorf("HardwareJSON = %q", got)
	}
}

func TestLoad_MissingFileReturnsZeroConfig(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if cfg.LlamaServerPath != "" {
		t.Errorf("expected empty LlamaServerPath, got %q", cfg.LlamaServerPath)
	}
}

func TestLoad_ParsesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := "llama_server_path: /opt/custom/llama-server\ndefault_port: 9090\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LlamaServerPath != "/opt/custom/llama-server" {
		t.Errorf("LlamaServerPath = %q", cfg.LlamaServerPath)
	}
	if cfg.DefaultPort != 9090 {
		t.Errorf("DefaultPort = %d", cfg.DefaultPort)
	}
}

func TestSaveLoadTelemetryFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := Config{
		TelemetryPort:     18080,
		TelemetryHost:     "0.0.0.0",
		TelemetryInterval: "2s",
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.TelemetryPort != 18080 {
		t.Errorf("TelemetryPort = %d, want 18080", got.TelemetryPort)
	}
	if got.TelemetryHost != "0.0.0.0" {
		t.Errorf("TelemetryHost = %q, want 0.0.0.0", got.TelemetryHost)
	}
	if got.TelemetryInterval != "2s" {
		t.Errorf("TelemetryInterval = %q, want 2s", got.TelemetryInterval)
	}
}

func TestLoad_RejectsMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("::: not yaml :::"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error, got %v", err)
	}
}
