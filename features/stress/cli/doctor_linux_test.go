//go:build linux

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestRunDoctorAcceptsFourBenchmarkFixture(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	scriptContent := `#!/usr/bin/env bash
CATMONITOR_STRESS_DESCRIBE_PROTOCOL=1
if [ "${1-}" != describe ]; then exit 2; fi
case "${2-}" in stream|hpl|hpcg|npu_burn) ;; *) exit 2 ;; esac
printf '{"protocol_version":1,"benchmark":"%s","parameters":[],"resources":{"mpi_processes":0,"threads_per_process":0,"total_workers":0,"runtime_seconds":0},"assets":[],"mpi":{"required":false,"implementation":"none","executable_abi":"not_applicable","status":"pass","message":"not required"},"preflight":{"status":"pass","message":"deployment precheck passed"}}\n' "$2"
`
	if err := os.WriteFile(script, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "catmonitor.yaml")
	configContent := fmt.Sprintf(`stress:
  enabled: true
  web_enabled: true
  script_path: %q
  report_path: %q
  default_benchmarks: [stream]
  benchmarks:
    stream: { enabled: true, timeout: 1m }
    hpl: { enabled: true, timeout: 10m }
    hpcg: { enabled: true, timeout: 3m, result_dir: %q }
    npu_burn: { enabled: true, timeout: 30m }
`, script, filepath.Join(dir, "stress-latest.json"), dir)
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&stderr, nil))
	if code := Run([]string{"doctor", "-c", configPath, "-o", "json"}, logger, &stdout, &stderr); code != 0 {
		t.Fatalf("doctor code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var result doctorResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "pass" || len(result.Benchmarks) != 4 {
		t.Fatalf("unexpected doctor result: %+v", result)
	}
	for _, item := range result.Benchmarks {
		if !item.Enabled || !item.Available {
			t.Fatalf("benchmark is not available: %+v", item)
		}
	}
}

func TestRunDoctorDoesNotDescribeDisabledFeature(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "described")
	script := filepath.Join(dir, "benchmark_check.sh")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\ntouch "+marker+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "catmonitor.yaml")
	configContent := fmt.Sprintf(`stress:
  enabled: false
  script_path: %q
  report_path: %q
  benchmarks:
    stream: { enabled: true, timeout: 1m }
`, script, filepath.Join(dir, "stress-latest.json"))
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"doctor", "-c", configPath}, slog.Default(), &stdout, &stderr); code != 1 {
		t.Fatalf("doctor code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("disabled feature triggered describe: %v", err)
	}
}
