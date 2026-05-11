package config

import (
	"errors"
	"fmt"
	"os"

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
