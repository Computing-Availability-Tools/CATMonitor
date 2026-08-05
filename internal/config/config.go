package config

import (
	"fmt"
	"os"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/platform"
	"gopkg.in/yaml.v3"
)

// Config represents the full CATMonitor configuration.
type Config struct {
	Server          ServerConfig            `yaml:"server"`
	Collectors      map[string]CollectorCfg `yaml:"collectors"`
	Storage         StorageConfig           `yaml:"storage"`
	Health          HealthConfig            `yaml:"health"`
	Stress          stress.Config           `yaml:"stress"`
	Collection      CollectionConfig        `yaml:"collection"`
	Features        []string                `yaml:"features"` // enabled features; daemon loads features/<name>/metrics.yaml overrides + derives C_comp from their intervals
	FaultSub        FaultSubConfig          `yaml:"faultsub"`
	StragglerOutput StragglerOutputConfig   `yaml:"straggler_output"`
	Snapshot        SnapshotConfig          `yaml:"snapshot"`
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

// HealthConfig holds health evaluation configuration. (Health is evaluated by
// the snapshot global writer at C_global; no separate health interval — removed
// dead config.) WeightScheme selects the scoring weights ("auto" detects
// gpu/npu).
type HealthConfig struct {
	Enabled      bool   `yaml:"enabled"`
	WeightScheme string `yaml:"weight_scheme"` // auto | cpu_only | accelerated_8card | accelerated_4card
}

// CollectionConfig controls which metrics are collected (pre-filter by priority).
type CollectionConfig struct {
	MinPriority string `yaml:"min_priority"` // low | medium | high
}

// FaultSubConfig controls the fault subscription & push mechanism (features/faultsub).
// When Enabled is false (the default) the daemon skips the feature entirely
// and behaves exactly as before.
type FaultSubConfig struct {
	Enabled        bool             `yaml:"enabled"`         // opt-in switch
	RestAddr       string           `yaml:"rest_addr"`       // subscription REST API listen address
	WebhookTimeout time.Duration    `yaml:"webhook_timeout"` // per-request webhook timeout
	WebhookRetry   int              `yaml:"webhook_retry"`   // failed-webhook retry count
	EventBuffer    int              `yaml:"event_buffer"`    // recent-event ring buffer size
	Defaults       FaultSubDefaults `yaml:"defaults"`
	Rules          map[string]bool  `yaml:"rules"`
}

// FaultSubDefaults holds subscription defaults applied when a subscriber
// omits the corresponding field.
type FaultSubDefaults struct {
	DebounceMs  int    `yaml:"debounce_ms"`
	MinSeverity string `yaml:"min_severity"`
}

// StragglerOutputConfig controls the straggler-dedicated KPI file output
// (features/stragglerout). When Enabled is false (the default) the daemon
// skips the feature and no KPI file is produced.
type StragglerOutputConfig struct {
	Enabled       bool          `yaml:"enabled"`        // opt-in switch
	DataDir       string        `yaml:"data_dir"`       // KPI file directory
	Retention     time.Duration `yaml:"retention"`      // file retention (default 15d)
	FlushInterval time.Duration `yaml:"flush_interval"` // in-memory buffer flush cadence
	Metrics       []string      `yaml:"metrics"`        // which straggler fields to emit (empty=all)
}

// SnapshotConfig controls daemon-side snapshot production (per-component
// files snapshot_<comp>.json + one global snapshot.json), consumed by read-only
// features (web/dfee). When Enabled is false (the default) the daemon writes
// no snapshot files and behaves exactly as before — this is the migration
// switch. When enabled, the daemon is the sole snapshot producer and web must
// run as a read-only consumer (no self-collection). The per-component history
// ring depth is fixed at 60 (not configurable).
type SnapshotConfig struct {
	Enabled bool   `yaml:"enabled"`
	Dir     string `yaml:"dir"` // directory for snapshot_<comp>.json + snapshot.json
}

// Default returns the default configuration.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Type: "auto",
		},
		Collectors: map[string]CollectorCfg{
			"chassis": {Enabled: true, Interval: 3 * time.Second},
			"cpu":     {Enabled: true, Interval: 3 * time.Second},
			"memory":  {Enabled: true, Interval: 3 * time.Second},
			"disk":    {Enabled: true, Interval: 5 * time.Second},
			"gpu":     {Enabled: true, Interval: 3 * time.Second},
			"npu":     {Enabled: true, Interval: 3 * time.Second},
			"network": {Enabled: true, Interval: 3 * time.Second},
		},
		Storage: StorageConfig{
			DataDir:    platform.DataDir(),
			MaxFileAge: 168 * time.Hour,
			Rotation:   "daily",
		},
		Health: HealthConfig{
			Enabled:      true,
			WeightScheme: "auto",
		},
		FaultSub: FaultSubConfig{
			Enabled:        false, // opt-in; daemon unchanged when off
			RestAddr:       ":9101",
			WebhookTimeout: 5 * time.Second,
			WebhookRetry:   1,
			EventBuffer:    1024,
			Defaults: FaultSubDefaults{
				DebounceMs:  0,
				MinSeverity: "warning",
			},
		},
		StragglerOutput: StragglerOutputConfig{
			Enabled:       false, // opt-in; no KPI file when off
			DataDir:       platform.DataDir() + "/straggler",
			Retention:     15 * 24 * time.Hour,
			FlushInterval: 60 * time.Second,
		},
		Snapshot: SnapshotConfig{
			Enabled: false, // opt-in; daemon unchanged when off
			Dir:     platform.DataDir() + "/snapshot",
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
