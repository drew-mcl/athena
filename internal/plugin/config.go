package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	defaultConfigDirName  = ".config"
	defaultProductDirName = "athena"
	pluginConfigFileName  = "plugins.json"
)

// Config stores user plugin enablement state.
type Config struct {
	Enabled map[string]bool `json:"enabled"`
}

// DefaultConfigPath returns ~/.config/athena/plugins.json.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return pluginConfigFileName
	}
	return filepath.Join(home, defaultConfigDirName, defaultProductDirName, pluginConfigFileName)
}

// LoadConfig reads plugin config from disk. Missing files return an empty config.
func LoadConfig() (*Config, error) {
	path := DefaultConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Enabled: make(map[string]bool)}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Enabled == nil {
		cfg.Enabled = make(map[string]bool)
	}
	return &cfg, nil
}

// SaveConfig writes plugin config to disk.
func SaveConfig(cfg *Config) error {
	path := DefaultConfigPath()
	if cfg == nil {
		cfg = &Config{Enabled: make(map[string]bool)}
	} else if cfg.Enabled == nil {
		cfg.Enabled = make(map[string]bool)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// IsEnabled reports whether a plugin is enabled.
func (c *Config) IsEnabled(name string) bool {
	if c == nil || c.Enabled == nil {
		return false
	}
	return c.Enabled[name]
}

// ApplyConfig updates registry plugin states from a config map.
func (r *Registry) ApplyConfig(cfg *Config) {
	for _, p := range r.List() {
		p.SetEnabled(cfg != nil && cfg.IsEnabled(p.Name()))
	}
}

// RefreshRegistryFromDisk reloads persisted plugin config and applies it.
func RefreshRegistryFromDisk(r *Registry) error {
	if r == nil {
		return nil
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	r.ApplyConfig(cfg)
	return nil
}
