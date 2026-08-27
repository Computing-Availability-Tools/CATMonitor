package config

import (
	"fmt"
	"os"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/platform"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server          ServerConfig            `yaml:"server"`
	Collectors      map[string]CollectorCfg `yaml:"collectors"`
	Storage         StorageConfig           `yaml:"storage"`
	Health          HealthConfig            `yaml:"health"`
	Stress          stress.Config           `yaml:"stress"`
	Collection      CollectionConfig        `yaml:"collection"`
	Features        []string                `yaml:"features"`
	FaultSub        FaultSubConfig          `yaml:"faultsub"`
	StragglerOutput StragglerOutputConfig   `yaml:"straggler_output"`
	Snapshot        SnapshotConfig          `yaml:"snapshot"`
}

type ServerConfig struct {
	Type string `yaml:"type"`
}
type CollectorCfg struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
}
type StorageConfig struct {
	DataDir    string        `yaml:"data_dir"`
	MaxFileAge time.Duration `yaml:"max_file_age"`
	Rotation   string        `yaml:"rotation"`
}
type HealthConfig struct {
	Enabled      bool   `yaml:"enabled"`
	WeightScheme string `yaml:"weight_scheme"`
}
type CollectionConfig struct {
	MinPriority string `yaml:"min_priority"`
}

type FaultSubConfig struct {
	Enabled        bool             `yaml:"enabled"`
	RestAddr       string           `yaml:"rest_addr"`
	WebhookTimeout time.Duration    `yaml:"webhook_timeout"`
	WebhookRetry   int              `yaml:"webhook_retry"`
	EventBuffer    int              `yaml:"event_buffer"`
	Defaults       FaultSubDefaults `yaml:"defaults"`
	Rules          map[string]bool  `yaml:"rules"`
}
type FaultSubDefaults struct {
	DebounceMs  int    `yaml:"debounce_ms"`
	MinSeverity string `yaml:"min_severity"`
}
type StragglerOutputConfig struct {
	Enabled       bool          `yaml:"enabled"`
	DataDir       string        `yaml:"data_dir"`
	Retention     time.Duration `yaml:"retention"`
	FlushInterval time.Duration `yaml:"flush_interval"`
	Metrics       []string      `yaml:"metrics"`
}
type SnapshotConfig struct {
	Enabled bool   `yaml:"enabled"`
	Dir     string `yaml:"dir"`
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{Type: "auto"},
		Collectors: map[string]CollectorCfg{
			"chassis": {Enabled: true, Interval: 3 * time.Second},
			"cpu":     {Enabled: true, Interval: 3 * time.Second},
			"memory":  {Enabled: true, Interval: 3 * time.Second},
			"disk":    {Enabled: true, Interval: 5 * time.Second},
			"gpu":     {Enabled: true, Interval: 3 * time.Second},
			"npu":     {Enabled: true, Interval: 3 * time.Second},
			"network": {Enabled: true, Interval: 3 * time.Second},
		},
		Storage: StorageConfig{DataDir: platform.DataDir(), MaxFileAge: 168 * time.Hour, Rotation: "daily"},
		Health:  HealthConfig{Enabled: true, WeightScheme: "auto"},
		Stress: stress.Config{
			Enabled: false, WebEnabled: false,
			ControlSocket:     "/run/catmonitor/control.sock",
			ReportPath:        platform.DataDir() + "/stress/stress-latest.json",
			DefaultBenchmarks: []string{"stream"},
			Executor:          stress.ExecutorConfig{Type: "docker_exec", DockerBinary: "/usr/bin/docker", DockerSocket: "/var/run/docker.sock"},
			Benchmarks: map[string]stress.BenchmarkConfig{
				"stream":   {Plugin: "stream", Container: "catmonitor-stress-cpu", User: "65532:65532", Timeout: time.Minute},
				"hpl":      {Plugin: "hpl", Container: "catmonitor-stress-cpu", User: "65532:65532", Timeout: 2 * time.Hour},
				"hpcg":     {Plugin: "hpcg", Container: "catmonitor-stress-cpu", User: "65532:65532", Timeout: 3 * time.Minute},
				"npu_burn": {Plugin: "npu_burn", Container: "catmonitor-stress-npu", Timeout: 30 * time.Minute},
			},
		},
		FaultSub: FaultSubConfig{
			Enabled: false, RestAddr: ":9101", WebhookTimeout: 5 * time.Second,
			WebhookRetry: 1, EventBuffer: 1024,
			Defaults: FaultSubDefaults{DebounceMs: 0, MinSeverity: "warning"},
		},
		StragglerOutput: StragglerOutputConfig{
			Enabled: false, DataDir: platform.DataDir() + "/straggler",
			Retention: 15 * 24 * time.Hour, FlushInterval: 60 * time.Second,
		},
		Snapshot: SnapshotConfig{Enabled: false, Dir: platform.DataDir() + "/snapshot"},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}
	if err := stress.ValidateConfig(cfg.Stress); err != nil {
		return nil, fmt.Errorf("invalid stress configuration: %w", err)
	}
	return cfg, nil
}
