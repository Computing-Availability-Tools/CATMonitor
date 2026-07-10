package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the full CATMonitor configuration.
type Config struct {
	Server     ServerConfig            `yaml:"server"`
	Collectors map[string]CollectorCfg `yaml:"collectors"`
	Storage    StorageConfig           `yaml:"storage"`
	Health     HealthConfig            `yaml:"health"`
}

// ServerConfig holds server-level configuration.
type ServerConfig struct {
	Type string `yaml:"type"` // auto | cpu_only | accelerated
}

// CollectorCfg holds per-collector configuration.
type CollectorCfg struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
}

// StorageConfig holds data storage configuration.
type StorageConfig struct {
	DataDir    string        `yaml:"data_dir"`
	MaxFileAge time.Duration `yaml:"max_file_age"`
	Rotation   string        `yaml:"rotation"`
}

// HealthConfig holds health evaluation configuration.
type HealthConfig struct {
	Enabled      bool          `yaml:"enabled"`
	Interval     time.Duration `yaml:"interval"`
	WeightScheme string        `yaml:"weight_scheme"` // auto | cpu_only | accelerated_8card | accelerated_4card
}

// Default returns the default configuration.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Type: "auto",
		},
		Collectors: map[string]CollectorCfg{
			"cpu":     {Enabled: true, Interval: 3 * time.Second},
			"memory":  {Enabled: true, Interval: 3 * time.Second},
			"disk":    {Enabled: true, Interval: 5 * time.Second},
			"gpu":     {Enabled: true, Interval: 3 * time.Second},
			"npu":     {Enabled: true, Interval: 3 * time.Second},
			"network": {Enabled: true, Interval: 3 * time.Second},
		},
		Storage: StorageConfig{
			DataDir:    "/var/lib/catmonitor/data",
			MaxFileAge: 168 * time.Hour,
			Rotation:   "daily",
		},
		Health: HealthConfig{
			Enabled:      true,
			Interval:     5 * time.Second,
			WeightScheme: "auto",
		},
	}
}

// Load reads configuration from a YAML file. If the file does not exist,
// default configuration is returned.
func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	return cfg, nil
}
