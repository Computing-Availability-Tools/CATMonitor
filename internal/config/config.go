package config

import (
	"fmt"
	"os"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/platform"
	"gopkg.in/yaml.v3"
)

// Config represents the full CATMonitor configuration.
type Config struct {
	Server     ServerConfig            `yaml:"server"`
	Collectors map[string]CollectorCfg `yaml:"collectors"`
	Storage    StorageConfig           `yaml:"storage"`
	Health     HealthConfig           `yaml:"health"`
	Energysave EnergysaveConfig        `yaml:"energysave"`
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

// EnergysaveConfig holds the cpugov (CPU-senses-NPU) actuator configuration
// (SPEC: features/cpugov/cpugov_SPEC.md). Writes to sysfs require root; the
// feature is off by default and starts in dry_run (read-only) mode.
type EnergysaveConfig struct {
	Enabled             bool          `yaml:"enabled"`                // default false
	Interval            time.Duration `yaml:"interval"`               // control loop period
	CpuIdleThresholdPct float64       `yaml:"cpu_idle_threshold_pct"` // idle% >= this => idle sample
	ObserveWindow       time.Duration `yaml:"observe_window"`         // x seconds sustained idle to confirm
	NonIdleBreak        int           `yaml:"non_idle_break"`          // consecutive non-idle to abort
	DryRun              bool          `yaml:"dry_run"`                 // true = judge+log only, no sysfs write
	MinFreqOverride     uint64        `yaml:"min_freq_override"`      // 0 = use cpuinfo_min_freq
	NpuStaleSec         int           `yaml:"npu_stale_sec"`          // NPU data staleness threshold (sec)
}

// Default returns the default configuration.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Type: "auto",
		},
		Collectors: map[string]CollectorCfg{
			"chassis": {Enabled: true, Interval: 3 * time.Second},
			"cpu":      {Enabled: true, Interval: 3 * time.Second},
			"memory":   {Enabled: true, Interval: 3 * time.Second},
			"disk":     {Enabled: true, Interval: 5 * time.Second},
			"gpu":      {Enabled: true, Interval: 3 * time.Second},
			"npu":      {Enabled: true, Interval: 3 * time.Second},
			"network":  {Enabled: true, Interval: 3 * time.Second},
		},
		Storage: StorageConfig{
			DataDir:    platform.DataDir(),
			MaxFileAge: 168 * time.Hour,
			Rotation:   "daily",
		},
		Health: HealthConfig{
			Enabled:      true,
			Interval:     5 * time.Second,
			WeightScheme: "auto",
		},
		Energysave: EnergysaveConfig{
			Enabled:             false,
			Interval:            3 * time.Second,
			CpuIdleThresholdPct: 97,
			ObserveWindow:       120 * time.Second,
			NonIdleBreak:        2,
			DryRun:              true,
			MinFreqOverride:     0,
			NpuStaleSec:         6,
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
