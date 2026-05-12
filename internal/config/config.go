package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk representation of ~/.config/llamactl/config.yaml.
// Fields are camel-cased in YAML to match `llamactl config <key>` argument
// names. Zero values mean "unset, fall back to defaults".
type Config struct {
	LlamaServerPath string `yaml:"llama_server_path"`
	DefaultPort     int    `yaml:"default_port"`
	ModelsDir       string `yaml:"models_dir"`
	HFToken         string `yaml:"hf_token"`
	LogLevel        string `yaml:"log_level"`
	APIKey          string `yaml:"api_key"`
}

// Load reads path and returns the parsed Config. A missing file is not an
// error — Load returns the zero Config. Malformed YAML is.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// Save marshals cfg to YAML and atomically writes it to path. The parent
// directory is created (mode 0o755) if it does not exist. The file is written
// to a sibling .tmp file first and then renamed so that readers never see a
// partially written file. The file mode is 0o600 because the config may
// contain tokens (HFToken, APIKey).
func Save(path string, cfg Config) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write temp config %s: %w", tmp, err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename temp config to %s: %w", path, err)
	}

	return nil
}
