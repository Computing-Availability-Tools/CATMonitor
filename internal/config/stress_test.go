package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadTopLevelStressConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catmonitor.yaml")
	content := []byte(`stress:
  enabled: true
  web_enabled: true
  script_path: /etc/catmonitor/benchmark_check.sh
  report_path: /var/lib/catmonitor/stress-latest.json
  default_benchmarks: [stream]
  benchmarks:
    stream: { enabled: true, timeout: 1m }
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Stress.Enabled || !cfg.Stress.WebEnabled || cfg.Stress.Benchmarks["stream"].Timeout != time.Minute {
		t.Fatalf("unexpected stress config: %+v", cfg.Stress)
	}
}

func TestDefaultStressConfigIsSafe(t *testing.T) {
	cfg := Default().Stress
	if cfg.Enabled || cfg.WebEnabled {
		t.Fatalf("stress defaults must be disabled: %+v", cfg)
	}
	if cfg.ScriptPath != "" || cfg.ReportPath != "" {
		t.Fatalf("disabled stress defaults must not assume deployment paths: %+v", cfg)
	}
}
