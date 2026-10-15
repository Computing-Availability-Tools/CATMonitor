package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "catmonitor.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// An empty yaml keeps every default value.
func TestLoadEmptyKeepsDefaults(t *testing.T) {
	cfg, err := Load(writeCfg(t, "{}"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Type != "auto" {
		t.Errorf("Server.Type = %q, want auto", cfg.Server.Type)
	}
	if len(cfg.Collectors) != 7 {
		t.Errorf("Collectors = %d entries, want 7", len(cfg.Collectors))
	}
	for name, c := range cfg.Collectors {
		if !c.Enabled {
			t.Errorf("collector %q must default to enabled", name)
		}
	}
	if cfg.Collectors["disk"].Interval != 5*time.Second {
		t.Errorf("disk interval = %v, want 5s", cfg.Collectors["disk"].Interval)
	}
	if cfg.Storage.MaxFileAge != 168*time.Hour {
		t.Errorf("MaxFileAge = %v, want 168h", cfg.Storage.MaxFileAge)
	}
	if !cfg.Health.Enabled || cfg.Health.WeightScheme != "auto" {
		t.Errorf("Health = %+v, want enabled/auto", cfg.Health)
	}
	if cfg.FaultSub.Enabled {
		t.Error("faultsub must default to disabled")
	}
	if cfg.StragglerOutput.Enabled {
		t.Error("straggler_output must default to disabled")
	}
	if cfg.Snapshot.Enabled {
		t.Error("snapshot must default to disabled")
	}
}

// Missing or broken files surface as errors, never as silently-defaulted
// configs (Load only applies defaults on top of a parsed file).
func TestLoadAbsentFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Error("absent config file must return an error")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	if _, err := Load(writeCfg(t, "components: [not: valid: yaml:")); err == nil {
		t.Error("invalid yaml must return a parse error")
	}
}

// Partial overrides only touch the listed fields; unlisted sections keep
// their defaults.
func TestLoadPartialOverride(t *testing.T) {
	body := `server:
  type: cpu_only
collectors:
  cpu:
    enabled: false
    interval: 10s
collection:
  min_priority: high
features: [web, dfee]
faultsub:
  enabled: true
  rest_addr: ":19999"
  webhook_timeout: 2s
  webhook_retry: 3
  event_buffer: 8
snapshot:
  enabled: true
  dir: /data/snap
straggler_output:
  enabled: true
  data_dir: /data/straggler
  retention: 48h
  flush_interval: 30s
`
	cfg, err := Load(writeCfg(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Type != "cpu_only" {
		t.Errorf("Server.Type = %q", cfg.Server.Type)
	}
	if cfg.Collectors["cpu"].Enabled {
		t.Error("cpu must be disabled by override")
	}
	if cfg.Collectors["cpu"].Interval != 10*time.Second {
		t.Errorf("cpu interval = %v", cfg.Collectors["cpu"].Interval)
	}
	if !cfg.Collectors["memory"].Enabled {
		t.Error("memory must keep its default (enabled)")
	}
	if cfg.Collection.MinPriority != "high" {
		t.Errorf("min_priority = %q", cfg.Collection.MinPriority)
	}
	if len(cfg.Features) != 2 || cfg.Features[0] != "web" || cfg.Features[1] != "dfee" {
		t.Errorf("features = %v", cfg.Features)
	}
	if !cfg.FaultSub.Enabled || cfg.FaultSub.RestAddr != ":19999" ||
		cfg.FaultSub.WebhookTimeout != 2*time.Second || cfg.FaultSub.WebhookRetry != 3 ||
		cfg.FaultSub.EventBuffer != 8 {
		t.Errorf("faultsub = %+v", cfg.FaultSub)
	}
	if !cfg.Snapshot.Enabled || cfg.Snapshot.Dir != "/data/snap" {
		t.Errorf("snapshot = %+v", cfg.Snapshot)
	}
	if !cfg.StragglerOutput.Enabled || cfg.StragglerOutput.DataDir != "/data/straggler" ||
		cfg.StragglerOutput.Retention != 48*time.Hour || cfg.StragglerOutput.FlushInterval != 30*time.Second {
		t.Errorf("straggler_output = %+v", cfg.StragglerOutput)
	}
}

// A collector absent from the yaml disappears from the map (the daemon then
// falls back to that collector's DefaultEnabled).
func TestLoadCollectorRemovedWhenListed(t *testing.T) {
	body := `collectors:
  cpu:
    enabled: false
`
	cfg, err := Load(writeCfg(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := cfg.Collectors["cpu"]; !ok {
		t.Fatal("cpu entry should exist when explicitly listed")
	}
	if cfg.Collectors["cpu"].Enabled {
		t.Error("cpu must be disabled")
	}
}

func TestDefaultSnapshotAndStragglerDirs(t *testing.T) {
	cfg := Default()
	if cfg.Snapshot.Dir == "" || cfg.StragglerOutput.DataDir == "" {
		t.Error("default snapshot/straggler dirs must not be empty")
	}
	if cfg.FaultSub.WebhookTimeout != 5*time.Second || cfg.FaultSub.EventBuffer != 1024 {
		t.Errorf("faultsub defaults = %+v", cfg.FaultSub)
	}
	if cfg.StragglerOutput.Retention != 15*24*time.Hour {
		t.Errorf("retention = %v, want 15d", cfg.StragglerOutput.Retention)
	}
	if cfg.StragglerOutput.FlushInterval != 60*time.Second {
		t.Errorf("flush interval = %v, want 60s", cfg.StragglerOutput.FlushInterval)
	}
}
