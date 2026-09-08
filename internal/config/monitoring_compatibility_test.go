package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyMonitoringConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catmonitor.yaml")
	content := []byte(`server:
  type: auto
collectors:
  cpu: { enabled: true, interval: 3s }
  memory: { enabled: true, interval: 3s }
storage:
  data_dir: /var/lib/catmonitor/data
  max_file_age: 168h
  rotation: daily
health:
  enabled: true
  weight_scheme: auto
stress:
  enabled: false
  web_enabled: false
  script_path: /opt/catmonitor/stress/benchmark_check.sh
  report_path: /var/lib/catmonitor/stress/stress-latest.json
  default_benchmarks: [stream]
  benchmarks:
    stream: { enabled: false, timeout: 1m }
    hpl: { enabled: false, timeout: 2h }
    hpcg: { enabled: false, result_dir: "", timeout: 3m }
snapshot:
  enabled: true
  dir: /var/lib/catmonitor/snapshot
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("legacy monitoring config must remain loadable: %v", err)
	}
	if cfg.Stress.Enabled {
		t.Fatalf("legacy disabled stress block must not enable V2 Stress: %+v", cfg.Stress)
	}
	if !cfg.Snapshot.Enabled || !cfg.Collectors["cpu"].Enabled {
		t.Fatalf("monitoring fields were not preserved: %+v", cfg)
	}
}

func TestLegacyMonitoringConfigWithoutStressSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catmonitor.yaml")
	content := []byte(`server:
  type: auto
collectors:
  cpu: { enabled: true, interval: 3s }
  memory: { enabled: true, interval: 3s }
storage:
  data_dir: /var/lib/catmonitor/data
  max_file_age: 168h
  rotation: daily
health:
  enabled: true
  weight_scheme: auto
snapshot:
  enabled: true
  dir: /var/lib/catmonitor/snapshot
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("legacy monitoring config without stress section must remain loadable: %v", err)
	}
	if cfg.Stress.Enabled {
		t.Fatalf("missing stress section must keep V2 Stress disabled: %+v", cfg.Stress)
	}
	if !cfg.Snapshot.Enabled || !cfg.Collectors["cpu"].Enabled {
		t.Fatalf("monitoring fields were not preserved: %+v", cfg)
	}
}

func TestEnabledLegacyStressConfigRequiresMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catmonitor.yaml")
	content := []byte(`stress:
  enabled: true
  web_enabled: true
  script_path: /opt/catmonitor/stress/benchmark_check.sh
  report_path: /var/lib/catmonitor/stress/stress-latest.json
  default_benchmarks: [stream]
  benchmarks:
    stream: { enabled: true, timeout: 1m }
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "legacy stress configuration is not supported") {
		t.Fatalf("enabled V1 stress config must require migration, got %v", err)
	}
}

func TestLegacyHealthStressConfigRequiresMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catmonitor.yaml")
	content := []byte(`health:
  enabled: true
  stress:
    enabled: true
    script_path: /opt/catmonitor/stress/benchmark_check.sh
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "legacy health.stress configuration is not supported") {
		t.Fatalf("enabled health.stress config must require migration, got %v", err)
	}
}
