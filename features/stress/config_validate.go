package stress

import (
	"errors"
	"fmt"
	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
	"path/filepath"
	"strings"
)

// ValidateConfig enforces the daemon-owned execution boundary before the
// control socket starts. A run request can select only bindings accepted here.
func ValidateConfig(cfg Config) error {
	// Monitoring-only deployments do not instantiate a Stress Controller or
	// executor. Ignore legacy/partial execution fields while the feature is
	// disabled so existing monitoring YAML remains loadable without Docker.
	if !cfg.Enabled {
		return nil
	}
	if cfg.ControlSocket == "" || !filepath.IsAbs(cfg.ControlSocket) {
		return errors.New("stress.control_socket must be an absolute path")
	}
	if cfg.ReportPath == "" || !filepath.IsAbs(cfg.ReportPath) {
		return errors.New("stress.report_path must be an absolute path")
	}
	if cfg.Executor.Type != "" && cfg.Executor.Type != "docker_exec" {
		return errors.New("stress.executor.type must be docker_exec")
	}
	if cfg.Executor.DockerBinary == "" || !filepath.IsAbs(cfg.Executor.DockerBinary) {
		return errors.New("stress.executor.docker_binary must be an absolute path")
	}
	if cfg.Executor.DockerSocket == "" || !filepath.IsAbs(cfg.Executor.DockerSocket) {
		return errors.New("stress.executor.docker_socket must be an absolute path")
	}

	for name, benchmark := range cfg.Benchmarks {
		if benchmark.AllowedNPUModels != nil || benchmark.AllowNPUReferenceGeneration {
			if name != "npu_burn" {
				return errors.New("model authorization is only valid for npu_burn")
			}
			seen := map[string]bool{}
			for _, model := range benchmark.AllowedNPUModels {
				if (model != "gpt3" && model != "llama2") || seen[model] {
					return errors.New("allowed_npu_models must contain unique gpt3/llama2 names")
				}
				seen[model] = true
			}
			if benchmark.AllowNPUReferenceGeneration && len(benchmark.AllowedNPUModels) == 0 {
				return errors.New("reference generation requires allowed_npu_models")
			}
		}
		if benchmark.AllowedNPUDevices != nil {
			if name != "npu_burn" {
				return errors.New("allowed_npu_devices is only valid for npu_burn")
			}
			if err := workloadapi.ValidateDevices(benchmark.AllowedNPUDevices); err != nil {
				return err
			}
		}
		if !supportedBenchmark(name) {
			return fmt.Errorf("stress benchmark %q is unsupported", name)
		}
		plugin := strings.TrimSpace(benchmark.Plugin)
		if plugin == "" {
			plugin = name
		}
		if plugin != name {
			return fmt.Errorf("stress benchmark %q has invalid plugin %q", name, benchmark.Plugin)
		}
		if !containerNamePattern.MatchString(benchmark.Container) {
			return fmt.Errorf("stress benchmark %q has invalid container name", name)
		}
		if benchmark.User != "" && !containerUserPattern.MatchString(benchmark.User) {
			return fmt.Errorf("stress benchmark %q has invalid user", name)
		}
		if benchmark.Timeout <= 0 {
			return fmt.Errorf("stress benchmark %q timeout must be positive", name)
		}
	}

	if len(cfg.DefaultBenchmarks) == 0 {
		return errors.New("stress.default_benchmarks must not be empty when stress is enabled")
	}
	seen := make(map[string]bool, len(cfg.DefaultBenchmarks))
	for _, name := range cfg.DefaultBenchmarks {
		if seen[name] {
			return fmt.Errorf("stress.default_benchmarks contains duplicate %q", name)
		}
		seen[name] = true
		benchmark, ok := cfg.Benchmarks[name]
		if !ok || !benchmark.Enabled {
			return fmt.Errorf("stress default benchmark %q is not enabled", name)
		}
	}
	return nil
}
