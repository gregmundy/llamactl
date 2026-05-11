package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
