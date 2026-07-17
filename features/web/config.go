package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerCfg    `yaml:"server"`
	Collector CollectorCfg `yaml:"collector"`
	Storage   StorageCfg   `yaml:"storage"`
}

type ServerCfg struct {
	Addr string `yaml:"addr"`
}

type CollectorCfg struct {
	RefreshInterval   time.Duration `yaml:"refresh_interval"`
	HistoryPoints     int           `yaml:"history_points"`
	EnabledComponents []string      `yaml:"enabled_components"`
}

type StorageCfg struct {
	SnapshotPath string `yaml:"snapshot_path"`
	RuntimePath  string `yaml:"runtime_path"`
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerCfg{Addr: ":9527"},
		Collector: CollectorCfg{
			RefreshInterval: 5 * time.Second,
			HistoryPoints:   60,
		},
		Storage: StorageCfg{
			SnapshotPath: "features/web/data/snapshot.json",
			RuntimePath:  "features/web/data/runtime.json",
		},
	}
}

// LoadConfig loads YAML config (falling back to defaults when the file is
// missing) and overlays any UI-persisted runtime override.
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parse config %q: %w", path, err)
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config %q: %w", path, err)
		}
	}
	if err := loadRuntime(cfg); err != nil {
		// A corrupt runtime file must not block startup; just ignore it.
		_ = err
	}
	return cfg, nil
}

// runtimeState persists UI-adjusted settings across restarts.
type runtimeState struct {
	RefreshIntervalMS int `json:"refresh_interval_ms"`
}

func loadRuntime(cfg *Config) error {
	data, err := os.ReadFile(cfg.Storage.RuntimePath)
	if err != nil {
		return err
	}
	var rs runtimeState
	if err := json.Unmarshal(data, &rs); err != nil {
		return err
	}
	if rs.RefreshIntervalMS > 0 {
		cfg.Collector.RefreshInterval = time.Duration(rs.RefreshIntervalMS) * time.Millisecond
	}
	return nil
}

func saveRuntime(cfg *Config) error {
	rs := runtimeState{RefreshIntervalMS: int(cfg.Collector.RefreshInterval / time.Millisecond)}
	data, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(cfg.Storage.RuntimePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".runtime-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	tmp.Close()
	return os.Rename(tmpName, cfg.Storage.RuntimePath)
}
