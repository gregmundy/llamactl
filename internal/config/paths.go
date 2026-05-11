// Package config owns llamactl's filesystem layout (XDG-aware) and the YAML
// config loader. Phase 1 is read-only; the `llamactl config` write command
// arrives in Phase 4.
package config

import (
	"os"
	"path/filepath"
)

// Paths resolves llamactl's config/cache/data directories. Construct via
// New() to pick up $HOME and XDG_* env vars at startup. Tests construct
// directly with their own Home.
type Paths struct {
	Home string
}

// New returns a Paths anchored to the calling user's home directory.
func New() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	return Paths{Home: home}, nil
}

// xdgDir returns $envVar/llamactl if envVar is set, else the fallback.
func (p Paths) xdgDir(envVar, fallback string) string {
	if v := os.Getenv(envVar); v != "" {
		return filepath.Join(v, "llamactl")
	}
	return fallback
}

func (p Paths) ConfigDir() string {
	return p.xdgDir("XDG_CONFIG_HOME", filepath.Join(p.Home, ".config", "llamactl"))
}

func (p Paths) CacheDir() string {
	return p.xdgDir("XDG_CACHE_HOME", filepath.Join(p.Home, ".cache", "llamactl"))
}

// DataDir is the SHARED model directory (note: not namespaced under "llamactl"
// because it's an open convention per PRD §4).
func (p Paths) DataDir() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "llama-models")
	}
	return filepath.Join(p.Home, ".local", "share", "llama-models")
}

func (p Paths) ConfigFile() string    { return filepath.Join(p.ConfigDir(), "config.yaml") }
func (p Paths) HardwareJSON() string  { return filepath.Join(p.ConfigDir(), "hardware.json") }
func (p Paths) ModelsMetaDir() string { return filepath.Join(p.ConfigDir(), "models") }
