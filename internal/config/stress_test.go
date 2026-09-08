package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDaemonOwnedStressConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catmonitor.yaml")
	content := []byte(`stress:
  enabled: true
  web_enabled: true
  control_socket: /run/catmonitor/control.sock
  report_path: /var/lib/catmonitor/stress/stress-latest.json
  default_benchmarks: [stream]
  executor:
    type: docker_exec
    docker_binary: /usr/bin/docker
    docker_socket: /var/run/docker.sock
  benchmarks:
    stream:
      enabled: true
      plugin: stream
      container: catmonitor-stress-cpu
      user: "65532:65532"
      timeout: 1m
    npu_burn:
      enabled: true
      plugin: npu_burn
      container: catmonitor-stress-npu
      timeout: 30m
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Stress.Enabled || !cfg.Stress.WebEnabled || cfg.Stress.ControlSocket != "/run/catmonitor/control.sock" {
		t.Fatalf("unexpected stress config: %+v", cfg.Stress)
	}
	stream := cfg.Stress.Benchmarks["stream"]
	if stream.Timeout != time.Minute || stream.User != "65532:65532" || stream.Container != "catmonitor-stress-cpu" {
		t.Fatalf("unexpected stream binding: %+v", stream)
	}
	if cfg.Stress.Benchmarks["npu_burn"].Timeout != 30*time.Minute {
		t.Fatalf("unexpected NPU config: %+v", cfg.Stress.Benchmarks["npu_burn"])
	}
}

func TestDefaultStressConfigIsSafeAndDeterministic(t *testing.T) {
	cfg := Default().Stress
	if cfg.Enabled || cfg.WebEnabled {
		t.Fatalf("stress defaults must be disabled: %+v", cfg)
	}
	if cfg.ControlSocket != "/run/catmonitor/control.sock" || cfg.Executor.Type != "docker_exec" {
		t.Fatalf("unexpected default controller transport: %+v", cfg)
	}
	if cfg.Benchmarks["stream"].User != "65532:65532" || cfg.Benchmarks["npu_burn"].User != "" {
		t.Fatalf("unexpected default workload users: %+v", cfg.Benchmarks)
	}
}
