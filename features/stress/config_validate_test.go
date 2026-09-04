package stress

import (
	"strings"
	"testing"
	"time"
)

func validConfigForTest() Config {
	return Config{
		Enabled: true, ControlSocket: "/run/catmonitor/control.sock",
		ReportPath:        "/var/lib/catmonitor/stress/latest.json",
		DefaultBenchmarks: []string{"stream"},
		Executor:          ExecutorConfig{Type: "docker_exec", DockerBinary: "/usr/bin/docker", DockerSocket: "/var/run/docker.sock"},
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Plugin: "stream", Container: "catmonitor-stress-cpu", User: "65532:65532", Timeout: time.Minute},
		},
	}
}

func TestValidateConfigAcceptsFixedBinding(t *testing.T) {
	if err := ValidateConfig(validConfigForTest()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateConfigIgnoresDisabledExecutionFields(t *testing.T) {
	cfg := Config{
		Enabled:  false,
		Executor: ExecutorConfig{Type: "legacy-shell"},
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: false},
		},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("disabled Stress must not require an execution plane: %v", err)
	}
}

func TestValidateConfigRejectsExpandedExecutionSurface(t *testing.T) {
	tests := map[string]func(*Config){
		"relative socket": func(c *Config) { c.ControlSocket = "control.sock" },
		"wrong executor":  func(c *Config) { c.Executor.Type = "shell" },
		"plugin path":     func(c *Config) { b := c.Benchmarks["stream"]; b.Plugin = "/bin/sh"; c.Benchmarks["stream"] = b },
		"container option": func(c *Config) {
			b := c.Benchmarks["stream"]
			b.Container = "--privileged"
			c.Benchmarks["stream"] = b
		},
		"named user":       func(c *Config) { b := c.Benchmarks["stream"]; b.User = "root"; c.Benchmarks["stream"] = b },
		"zero timeout":     func(c *Config) { b := c.Benchmarks["stream"]; b.Timeout = 0; c.Benchmarks["stream"] = b },
		"disabled default": func(c *Config) { b := c.Benchmarks["stream"]; b.Enabled = false; c.Benchmarks["stream"] = b },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := validConfigForTest()
			mutate(&cfg)
			if err := ValidateConfig(cfg); err == nil {
				t.Fatal("invalid config was accepted")
			} else if strings.TrimSpace(err.Error()) == "" {
				t.Fatal("validation returned an empty error")
			}
		})
	}
}
