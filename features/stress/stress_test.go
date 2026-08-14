package stress

// Manager, parser, persistence, lock, and shutdown regression tests.
import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseStream(t *testing.T) {
	values, source, err := parseStream("Copy: 1000.1\nScale: 900.2\nAdd: 800.3\nTriad: 700.4\n")
	if err != nil {
		t.Fatal(err)
	}
	if source != "stdout" || values["triad_mb_s"] != 700.4 {
		t.Fatalf("unexpected stream result: source=%q values=%v", source, values)
	}
}

func TestParseNPUBurnSummary(t *testing.T) {
	output := "CATMONITOR_NPU_BURN_SUMMARY devices=8 cases=24 passed=24 failed=0 errors=0 case_time_seconds=123.500000\n"
	values, source, err := parseNPUBurn(output)
	if err != nil {
		t.Fatal(err)
	}
	if source != "result_csv" || values["devices"] != 8 ||
		values["cases"] != 24 || values["passed"] != 24 ||
		values["failed"] != 0 || values["errors"] != 0 ||
		values["case_time_seconds"] != 123.5 {
		t.Fatalf("unexpected Ascend NPU Burn result: source=%q values=%v", source, values)
	}
}

func TestParseNPUBurnAcceptsSummaryGluedToPriorOutput(t *testing.T) {
	output := "path string is NULLpath string is NULLCATMONITOR_NPU_BURN_SUMMARY devices=1 cases=2 passed=2 failed=0 errors=0 case_time_seconds=1.647\n"
	values, _, err := parseNPUBurn(output)
	if err != nil || values["devices"] != 1 || values["cases"] != 2 || values["passed"] != 2 {
		t.Fatalf("real A3 glued summary was not parsed: err=%v values=%v", err, values)
	}
}

func TestParseNPUBurnRejectsInvalidProtocolOrIncompletePass(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantError string
	}{
		{name: "missing", output: "no validated summary\n", wantError: "validated summary not found"},
		{name: "malformed fields", output: "CATMONITOR_NPU_BURN_SUMMARY devices=1 cases=2 passed=2\n", wantError: "protocol error"},
		{name: "malformed integer", output: "CATMONITOR_NPU_BURN_SUMMARY devices=one cases=2 passed=2 failed=0 errors=0 case_time_seconds=1\n", wantError: "protocol error: invalid devices"},
		{name: "fractional integer", output: "CATMONITOR_NPU_BURN_SUMMARY devices=1 cases=1.5 passed=1 failed=0 errors=0 case_time_seconds=1\n", wantError: "protocol error: invalid cases"},
		{name: "malformed time", output: "CATMONITOR_NPU_BURN_SUMMARY devices=1 cases=1 passed=1 failed=0 errors=0 case_time_seconds=soon\n", wantError: "protocol error: invalid case_time_seconds"},
		{name: "negative time", output: "CATMONITOR_NPU_BURN_SUMMARY devices=1 cases=1 passed=1 failed=0 errors=0 case_time_seconds=-1\n", wantError: "protocol error: invalid case_time_seconds"},
		{name: "zero devices", output: "CATMONITOR_NPU_BURN_SUMMARY devices=0 cases=1 passed=1 failed=0 errors=0 case_time_seconds=0\n", wantError: "protocol error: devices must be at least 1"},
		{name: "zero cases", output: "CATMONITOR_NPU_BURN_SUMMARY devices=1 cases=0 passed=0 failed=0 errors=0 case_time_seconds=0\n", wantError: "protocol error: cases must be at least 1"},
		{name: "accounting mismatch", output: "CATMONITOR_NPU_BURN_SUMMARY devices=1 cases=3 passed=1 failed=1 errors=0 case_time_seconds=1\n", wantError: "protocol error: passed plus failed must equal cases"},
		{name: "failed case", output: "CATMONITOR_NPU_BURN_SUMMARY devices=1 cases=2 passed=1 failed=1 errors=0 case_time_seconds=1\n", wantError: "did not report a complete pass"},
		{name: "SDC error", output: "CATMONITOR_NPU_BURN_SUMMARY devices=1 cases=2 passed=2 failed=0 errors=1 case_time_seconds=1\n", wantError: "did not report a complete pass"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values, _, err := parseNPUBurn(test.output)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected %q, got %v", test.wantError, err)
			}
			if (test.name == "failed case" || test.name == "SDC error") && values == nil {
				t.Fatal("valid failure summary must retain metrics for unhealthy result rendering")
			}
		})
	}
}

func TestBoundedOutputKeepsTail(t *testing.T) {
	var output boundedOutput
	prefix := "DISCARDED PREFIX\n" + strings.Repeat("x", maxOutputBytes+128)
	if _, err := output.Write([]byte(prefix)); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("\nFINAL RESULT\n")); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.HasPrefix(got, "… output truncated") || !strings.Contains(got, "FINAL RESULT") {
		t.Fatalf("unexpected bounded output: length=%d tail=%q", len(got), got[len(got)-64:])
	}
	if strings.Contains(got, "DISCARDED PREFIX") {
		t.Fatal("bounded output retained discarded prefix")
	}
}

func TestParseHPL(t *testing.T) {
	values, source, err := parseHPL("header\nT/V N NB P Q Time Gflops\nWR00R2R4 20000 128 2 2 30.50 1.0000e+02\n1 tests completed and passed residual checks,\n0 tests completed and failed residual checks,\n")
	if err != nil {
		t.Fatal(err)
	}
	if source != "stdout" || values["time_seconds"] != 30.50 || values["gflops"] != 100 ||
		values["n"] != 20000 || values["nb"] != 128 || values["p"] != 2 ||
		values["q"] != 2 || values["process"] != 4 {
		t.Fatalf("unexpected HPL result: source=%q values=%v", source, values)
	}
}

func TestParseHPLRejectsFailedResidualCheck(t *testing.T) {
	output := "T/V N NB P Q Time Gflops\nWR00R2R4 20000 128 2 2 30.50 1.0000e+02\n1 tests completed and failed residual checks,\n"
	if _, _, err := parseHPL(output); err == nil || !strings.Contains(err.Error(), "failed residual") {
		t.Fatalf("expected failed residual check, got %v", err)
	}
}

func TestParseHPLRejectsExplicitFailedStatus(t *testing.T) {
	output := "T/V N NB P Q Time Gflops\nWR00R2R4 20000 128 2 2 30.50 1.0000e+02\nresidual check ...... FAILED\n"
	if _, _, err := parseHPL(output); err == nil || !strings.Contains(err.Error(), "FAILED") {
		t.Fatalf("expected explicit HPL failure, got %v", err)
	}
}

func TestBundledDispatcherIsGenericHostTemplate(t *testing.T) {
	data, err := os.ReadFile("benchmark_check.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`CATMONITOR_STRESS_DESCRIBE_PROTOCOL=1`,
		`STREAM_EXECUTABLE=""`,
		`STREAM_NUMACTL=""`,
		`HPL_EXECUTABLE=""`,
		`HPL_MPI_LAUNCHER=""`,
		`HPL_MPI_PROCESSES=0`,
		`HPCG_EXECUTABLE=""`,
		`HPCG_MPI_LAUNCHER=""`,
		`HPCG_MPI_PROCESSES=0`,
		`NPU_BURN_BACKEND="native"`,
		`NPU_BURN_EXECUTABLE=""`,
		`NPU_BURN_CONTAINER_RUNTIME=""`,
		`NPU_BURN_CONTAINER_NAME=""`,
		`NPU_BURN_CONTAINER_IMAGE=""`,
		`NPU_BURN_RUNTIME_CANN=""`,
		`NPU_BURN_RUNTIME_TORCH_NPU=""`,
		`NPU_BURN_SOC_MODEL=""`,
		`NPU_BURN_OUTPUT_DIR="${HOME}/.ascend_npu_burn/output"`,
		`NPU_BURN_RUN_CASE=""`,
		`NPU_BURN_GROUP=""`,
		`NPU_BURN_DEVICE=""`,
		`NPU_BURN_DEVICE_ROOT="/dev"`,
		`NPU_BURN_CHIP_GENERATION=""`,
		`require_absolute_executable`,
		`require_absolute_directory`,
		`require_nonnegative_integer "STREAM_THREADS"`,
		`hpl_input="$HPL_WORKDIR/HPL.dat"`,
		`require_positive_integer "HPCG_NX"`,
		`require_positive_integer "HPCG_RUNTIME_SECONDS"`,
		`exec "$STREAM_NUMACTL"`,
		`exec "$HPL_MPI_LAUNCHER"`,
		`exec "$HPCG_MPI_LAUNCHER"`,
		`-np "$HPL_MPI_PROCESSES"`,
		`export OPENBLAS_NUM_THREADS="$HPL_THREADS_PER_PROCESS"`,
		`export OMP_NUM_THREADS="$HPL_THREADS_PER_PROCESS"`,
		`export OMP_DYNAMIC=FALSE`,
		`-np "$HPCG_MPI_PROCESSES"`,
		`--nx="$HPCG_NX"`,
		`--rt="$HPCG_RUNTIME_SECONDS"`,
		`describe)`,
		`describe_stream`,
		`describe_hpl`,
		`describe_hpcg`,
		`describe_npu_burn`,
		`probe_npu_container`,
		`/usr/bin/test -x "$1"`,
		`lspci_path=$(command -v lspci)`,
		`summarize_npu_burn_csv`,
		`--sdc_detect`,
	} {
		if !strings.Contains(script, required) {
			t.Errorf("benchmark_check.sh missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"/root/",
		"Kunpeng",
		"validated host",
		"--report-bindings",
		"ppr:",
		"    osu)",
		"osu_alltoall",
		`HPL_INPUT=`,
		`-x OPENBLAS_NUM_THREADS`,
		`-x OMP_NUM_THREADS`,
		`-x OMP_DYNAMIC`,
		"--allow-run-as-root",
		"--map-by",
		"--bind-to",
		"-mca",
		`'test -x "$1"'`,
		`torch.npu.device_count`,
		`NPU_BURN_USE_DEFAULT_OUTPUT`,
		`npu_args+=(--output`,
	} {
		if strings.Contains(script, forbidden) {
			t.Errorf("benchmark_check.sh contains host-specific or unsupported value %q", forbidden)
		}
	}
}

func TestStandaloneUIExposesDeploymentFailureDetails(t *testing.T) {
	javascript, err := staticFiles.ReadFile("static/stress.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesheet, err := staticFiles.ReadFile("static/stress.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"benchmark-reason",
		"benchmark-deployment",
		"npuDeploymentSummary",
		"设备命名空间：",
		"可用 logical ID：",
		"拓扑来源：",
		"PCI logical ID：",
		"profileValue(asset.message, '失败')",
		"values.devices ?? values.device_count",
		"values.cases ?? values.case_count",
		"values.passed ?? values.passed_case_count",
		"values.failed ?? values.failed_case_count",
		"values.errors ?? values.error_count",
		"cases passed · ",
	} {
		if !strings.Contains(string(javascript), required) {
			t.Errorf("stress UI script does not expose %q", required)
		}
	}
	for _, required := range []string{".benchmark-reason", ".benchmark-deployment", ".primary-metric.bad"} {
		if !strings.Contains(string(stylesheet), required) {
			t.Errorf("stress UI stylesheet does not contain %q", required)
		}
	}
}

func TestBundledDispatcherValidatesAscendNPUBurnCSV(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dispatcher execution is Linux-only")
	}
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(dir, "npu-burn.args")
	npuBurn := writeExecutable(t, dir, "npu-burn", "#!/bin/bash\nset -eu\nprintf '%s\\n' \"$@\" > "+shellLiteral(argsFile)+"\nfor arg in \"$@\"; do [ \"$arg\" != --output ] || exit 9; [ \"$arg\" != --exec_count ] || exit 10; done\ngrep -Fxq -- --sdc_detect "+shellLiteral(argsFile)+" || exit 11\nprintf 'task,device_id,case_idx,run_count,stream_count,exetime,err_count,result,case_config\\nquant_matmul,0,0,100,1,12.5,0,PASS,shape=test\\nquant_matmul,1,0,100,1,13.5,0,PASS,shape=test\\n' > "+shellLiteral(filepath.Join(outputDir, "npu_burn_results.csv"))+"\n")
	script := configuredDispatcher(t, dir, map[string]string{
		"NPU_BURN_EXECUTABLE":               npuBurn,
		"NPU_BURN_OUTPUT_DIR":               outputDir,
		"NPU_BURN_RUN_CASE":                 "quant_matmul",
		"NPU_BURN_DEVICE":                   "0,1",
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "300",
		"NPU_BURN_CHIP_GENERATION":          "A3",
	})
	output, err := exec.Command("bash", script, "npu_burn").CombinedOutput()
	if err != nil {
		t.Fatalf("configured Ascend NPU Burn dispatcher failed: %v: %s", err, output)
	}
	values, _, err := parseNPUBurn(string(output))
	if err != nil || values["devices"] != 2 || values["cases"] != 2 ||
		values["case_time_seconds"] != 26 {
		t.Fatalf("unexpected validated NPU Burn output: err=%v values=%v output=%s", err, values, output)
	}
}

func TestBundledDispatcherRunsAscendNPUBurnWithPreparedContainer(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dispatcher execution is Linux-only")
	}
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(dir, "container-npu-burn.args")
	npuBurn := writeExecutable(t, dir, "npu-burn", "#!/bin/bash\nset -eu\nprintf '%s\\n' \"$@\" > "+shellLiteral(argsFile)+"\ngrep -Fxq -- --sdc_detect "+shellLiteral(argsFile)+" || exit 11\n! grep -Fxq -- --output "+shellLiteral(argsFile)+" || exit 12\nprintf 'task,device_id,case_idx,run_count,stream_count,exetime,err_count,result,case_config\\nmatmul,0,0,20,1,10.897049,0,PASS,shape=test\\n' > "+shellLiteral(filepath.Join(outputDir, "npu_burn_results.csv"))+"\n")
	docker := writeExecutable(t, dir, "docker", `#!/bin/bash
case "$1" in
  inspect) printf 'true|catmonitor/npuburn:a2-cann83\n' ;;
  exec)
    if printf '%s' "${5-}" | grep -Fq '/dev/davinci[0-9]*'; then
      printf '0\n'
    elif printf '%s' "${5-}" | grep -Fq 'lspci_path='; then
      printf '0\n'
    else
      shift 2
      exec "$@"
    fi
    ;;
  *) exit 98 ;;
esac
`)
	script := configuredDispatcher(t, dir, map[string]string{
		"NPU_BURN_BACKEND":                  "docker_exec",
		"NPU_BURN_EXECUTABLE":               npuBurn,
		"NPU_BURN_CONTAINER_RUNTIME":        docker,
		"NPU_BURN_CONTAINER_NAME":           "catmonitor-npuburn-a2",
		"NPU_BURN_CONTAINER_IMAGE":          "catmonitor/npuburn:a2-cann83",
		"NPU_BURN_OUTPUT_DIR":               outputDir,
		"NPU_BURN_RUN_CASE":                 "matmul",
		"NPU_BURN_DEVICE":                   "0",
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "120",
		"NPU_BURN_CHIP_GENERATION":          "A2",
	})
	output, err := exec.Command("bash", script, "npu_burn").CombinedOutput()
	if err != nil {
		t.Fatalf("prepared-container NPU Burn failed: %v: %s", err, output)
	}
	values, _, err := parseNPUBurn(string(output))
	if err != nil || values["devices"] != 1 || values["cases"] != 1 ||
		values["case_time_seconds"] != 10.897049 {
		t.Fatalf("unexpected container NPU Burn output: err=%v values=%v output=%s", err, values, output)
	}
}

func TestBundledDispatcherRejectsAscendNPUBurnFailureCSV(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dispatcher execution is Linux-only")
	}
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	npuBurn := writeExecutable(t, dir, "npu-burn", "#!/bin/bash\nprintf 'task,device_id,case_idx,run_count,stream_count,exetime,err_count,result,case_config\\nmatmul,0,0,100,1,12.5,1,FAIL,shape=test\\n' > "+shellLiteral(filepath.Join(outputDir, "npu_burn_results.csv"))+"\n")
	script := configuredDispatcher(t, dir, map[string]string{
		"NPU_BURN_EXECUTABLE":               npuBurn,
		"NPU_BURN_OUTPUT_DIR":               outputDir,
		"NPU_BURN_RUN_CASE":                 "matmul",
		"NPU_BURN_DEVICE":                   "all",
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "300",
		"NPU_BURN_CHIP_GENERATION":          "A5",
	})
	output, err := exec.Command("bash", script, "npu_burn").CombinedOutput()
	if err == nil || !strings.Contains(string(output), "reported failed cases or SDC errors") {
		t.Fatalf("failed NPU Burn CSV must fail dispatcher: err=%v output=%s", err, output)
	}
}

func TestBundledDispatcherRejectsStaleAscendNPUBurnCSV(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dispatcher execution is Linux-only")
	}
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	resultFile := filepath.Join(outputDir, "npu_burn_results.csv")
	if err := os.WriteFile(resultFile, []byte("task,device_id,case_idx,run_count,stream_count,exetime,err_count,result,case_config\nmatmul,0,0,20,1,10,0,PASS,shape=old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	npuBurn := writeExecutable(t, dir, "npu-burn", "#!/bin/bash\nexit 0\n")
	script := configuredDispatcher(t, dir, map[string]string{
		"NPU_BURN_EXECUTABLE":               npuBurn,
		"NPU_BURN_OUTPUT_DIR":               outputDir,
		"NPU_BURN_RUN_CASE":                 "matmul",
		"NPU_BURN_DEVICE":                   "0",
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "120",
		"NPU_BURN_CHIP_GENERATION":          "A2",
	})
	output, err := exec.Command("bash", script, "npu_burn").CombinedOutput()
	if err == nil || !strings.Contains(string(output), "did not update its result CSV") {
		t.Fatalf("stale NPU Burn result must fail: err=%v output=%s", err, output)
	}
}

func TestBundledDispatcherRejectsAscendNPUBurnGlobalFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dispatcher execution is Linux-only")
	}
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	npuBurn := writeExecutable(t, dir, "npu-burn", "#!/bin/bash\nprintf '| 0 | FAIL | worker exception |\\n'\nprintf 'task,device_id,case_idx,run_count,stream_count,exetime,err_count,result,case_config\\nmatmul,1,0,100,1,13.5,0,PASS,shape=test\\n' > "+shellLiteral(filepath.Join(outputDir, "npu_burn_results.csv"))+"\n")
	script := configuredDispatcher(t, dir, map[string]string{
		"NPU_BURN_EXECUTABLE":               npuBurn,
		"NPU_BURN_OUTPUT_DIR":               outputDir,
		"NPU_BURN_RUN_CASE":                 "matmul",
		"NPU_BURN_DEVICE":                   "all",
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "300",
		"NPU_BURN_CHIP_GENERATION":          "A3",
	})
	output, err := exec.Command("bash", script, "npu_burn").CombinedOutput()
	if err == nil || !strings.Contains(string(output), "global device summary reported failure") {
		t.Fatalf("global NPU Burn failure must override partial PASS CSV: err=%v output=%s", err, output)
	}
}

func TestBundledDispatcherRunsConfiguredStreamWithAbsolutePaths(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dispatcher execution is Linux-only")
	}
	dir := t.TempDir()
	streamExecutable := writeExecutable(t, dir, "stream_omp", "#!/bin/sh\nprintf 'Copy: 1\\nScale: 2\\nAdd: 3\\nTriad: 4\\n'\n")
	numactlExecutable := writeExecutable(t, dir, "numactl", "#!/bin/sh\ntest \"$1\" = --interleave=all\nshift\nexec \"$@\"\n")
	script := configuredDispatcher(t, dir, map[string]string{
		"STREAM_EXECUTABLE": streamExecutable,
		"STREAM_NUMACTL":    numactlExecutable,
		"STREAM_THREADS":    "2",
	})

	output, err := exec.Command("bash", script, "stream").CombinedOutput()
	if err != nil {
		t.Fatalf("configured STREAM dispatcher failed: %v: %s", err, output)
	}
	if !strings.Contains(string(output), "Triad: 4") {
		t.Fatalf("configured STREAM output=%s", output)
	}
}

func TestBundledDispatcherUsesWorkdirHPLDat(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dispatcher execution is Linux-only")
	}
	dir := t.TempDir()
	hplDir := filepath.Join(dir, "hpl")
	if err := os.Mkdir(hplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hplExecutable := writeExecutable(t, hplDir, "xhpl", "#!/bin/sh\nexit 0\n")
	mpiExecutable := writeExecutable(t, dir, "mpirun", "#!/bin/sh\nexit 0\n")
	script := configuredDispatcher(t, dir, map[string]string{
		"HPL_WORKDIR":             hplDir,
		"HPL_EXECUTABLE":          hplExecutable,
		"HPL_MPI_LAUNCHER":        mpiExecutable,
		"HPL_MPI_PROCESSES":       "1",
		"HPL_THREADS_PER_PROCESS": "1",
	})

	output, err := exec.Command("bash", script, "hpl").CombinedOutput()
	if err == nil || !strings.Contains(string(output), filepath.Join(hplDir, "HPL.dat")) {
		t.Fatalf("missing workdir HPL.dat should fail: err=%v output=%s", err, output)
	}
	if err := os.WriteFile(filepath.Join(hplDir, "HPL.dat"), []byte("test input\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err = exec.Command("bash", script, "hpl").CombinedOutput(); err != nil {
		t.Fatalf("configured HPL dispatcher failed after HPL.dat was installed: %v: %s", err, output)
	}
}

func TestBundledDispatcherUsesPortableMPIArgumentsAndExportedEnvironment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dispatcher execution is Linux-only")
	}
	dir := t.TempDir()
	mpiExecutable := writeExecutable(t, dir, "mpirun", `#!/bin/sh
printf 'args=%s\n' "$*"
printf 'openblas=%s omp=%s dynamic=%s\n' \
  "${OPENBLAS_NUM_THREADS-}" "${OMP_NUM_THREADS-}" "${OMP_DYNAMIC-}"
`)

	t.Run("hpl", func(t *testing.T) {
		hplDir := filepath.Join(dir, "hpl")
		if err := os.Mkdir(hplDir, 0o755); err != nil {
			t.Fatal(err)
		}
		hplExecutable := writeExecutable(t, hplDir, "xhpl", "#!/bin/sh\nexit 0\n")
		if err := os.WriteFile(filepath.Join(hplDir, "HPL.dat"), []byte("test input\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		script := configuredDispatcher(t, dir, map[string]string{
			"HPL_WORKDIR":             hplDir,
			"HPL_EXECUTABLE":          hplExecutable,
			"HPL_MPI_LAUNCHER":        mpiExecutable,
			"HPL_MPI_PROCESSES":       "8",
			"HPL_THREADS_PER_PROCESS": "12",
		})

		output, err := exec.Command("bash", script, "hpl").CombinedOutput()
		if err != nil {
			t.Fatalf("configured HPL dispatcher failed: %v: %s", err, output)
		}
		got := string(output)
		if !strings.Contains(got, "args=-np 8 "+hplExecutable) ||
			!strings.Contains(got, "openblas=12 omp=12 dynamic=") {
			t.Fatalf("unexpected portable HPL invocation: %s", got)
		}
	})

	t.Run("hpcg", func(t *testing.T) {
		hpcgDir := filepath.Join(dir, "hpcg")
		if err := os.Mkdir(hpcgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		hpcgExecutable := writeExecutable(t, hpcgDir, "xhpcg", "#!/bin/sh\nexit 0\n")
		script := configuredDispatcher(t, dir, map[string]string{
			"HPCG_WORKDIR":             hpcgDir,
			"HPCG_EXECUTABLE":          hpcgExecutable,
			"HPCG_MPI_LAUNCHER":        mpiExecutable,
			"HPCG_MPI_PROCESSES":       "96",
			"HPCG_THREADS_PER_PROCESS": "1",
		})

		output, err := exec.Command("bash", script, "hpcg").CombinedOutput()
		if err != nil {
			t.Fatalf("configured HPCG dispatcher failed: %v: %s", err, output)
		}
		got := string(output)
		wantArgs := "args=-np 96 " + hpcgExecutable + " --nx=32 --ny=32 --nz=32 --rt=60"
		if !strings.Contains(got, wantArgs) ||
			!strings.Contains(got, "openblas= omp=1 dynamic=FALSE") {
			t.Fatalf("unexpected portable HPCG invocation: %s", got)
		}
	})
}

func TestBundledDispatcherRejectsInvalidHPCGDimensions(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dispatcher execution is Linux-only")
	}
	dir := t.TempDir()
	hpcgExecutable := writeExecutable(t, dir, "xhpcg", "#!/bin/sh\nexit 0\n")
	mpiExecutable := writeExecutable(t, dir, "mpirun", "#!/bin/sh\nexit 0\n")
	script := configuredDispatcher(t, dir, map[string]string{
		"HPCG_WORKDIR":             dir,
		"HPCG_EXECUTABLE":          hpcgExecutable,
		"HPCG_MPI_LAUNCHER":        mpiExecutable,
		"HPCG_MPI_PROCESSES":       "1",
		"HPCG_THREADS_PER_PROCESS": "1",
		"HPCG_NX":                  "0",
	})

	output, err := exec.Command("bash", script, "hpcg").CombinedOutput()
	if err == nil || !strings.Contains(string(output), "HPCG_NX must be configured as a positive integer") {
		t.Fatalf("invalid HPCG dimension should fail: err=%v output=%s", err, output)
	}
}

func TestParseHPCGRequiresCurrentValidResultFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "HPCG-Benchmark_001.txt")
	content := "Final Summary::HPCG result is VALID with a GFLOP/s rating of=123.45\nFinal Summary::Results are valid but execution time (sec) is=67.89\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	values, source, err := parseHPCG("ordinary command output", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if source != "result_file" || values["gflops"] != 123.45 || values["time_seconds"] != 67.89 {
		t.Fatalf("unexpected HPCG result: source=%q values=%v", source, values)
	}
}

func TestParseHPCGRejectsValidStdoutWithoutResultFile(t *testing.T) {
	dir := t.TempDir()
	output := "Final Summary::HPCG result is VALID with a GFLOP/s rating of=12.5\nFinal Summary::Results are valid but execution time (sec) is=61\n"
	if _, _, err := parseHPCG(output, dir, nil); err == nil ||
		!strings.Contains(err.Error(), "no new or updated") {
		t.Fatalf("expected mandatory result file error, got %v", err)
	}
}

func TestParseHPCGRejectsInvalidCurrentResultFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "HPCG-Benchmark_3.1_invalid.txt")
	content := "Final Summary::HPCG result is INVALID\nFinal Summary::GFLOP/s rating of=12.5\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseHPCG("", dir, nil); err == nil ||
		!strings.Contains(err.Error(), "valid GFLOP/s and time not found") {
		t.Fatalf("expected invalid HPCG result error, got %v", err)
	}
}

func TestParseHPCGIgnoresNonBenchmarkTextFiles(t *testing.T) {
	dir := t.TempDir()
	decoy := filepath.Join(dir, "hpcg_notes.txt")
	valid := filepath.Join(dir, "HPCG-Benchmark_3.1_current.txt")
	decoyContent := "Final Summary::HPCG result is VALID with a GFLOP/s rating of=999\nFinal Summary::Results are valid but execution time (sec) is=1\n"
	validContent := "Final Summary::HPCG result is VALID with a GFLOP/s rating of=12.5\nFinal Summary::Results are valid but execution time (sec) is=61\n"
	if err := os.WriteFile(valid, []byte(validContent), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if err := os.WriteFile(decoy, []byte(decoyContent), 0o644); err != nil {
		t.Fatal(err)
	}
	values, source, err := parseHPCG("", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if source != "result_file" || values["gflops"] != 12.5 {
		t.Fatalf("non-benchmark text file was selected: source=%q values=%v", source, values)
	}
}

func TestParseHPCGRejectsUnchangedPreviousResult(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "HPCG-Benchmark_previous.txt")
	content := []byte("Final Summary::HPCG result is VALID with a GFLOP/s rating of=123.45\nFinal Summary::Results are valid but execution time (sec) is=67.89\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := snapshotHPCGResults(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseHPCG("ordinary command output", dir, before); err == nil ||
		!strings.Contains(err.Error(), "no new or updated") {
		t.Fatalf("expected stale result rejection, got %v", err)
	}
	if err := os.WriteFile(path, append(content, []byte("# current run\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	values, source, err := parseHPCG("ordinary command output", dir, before)
	if err != nil {
		t.Fatal(err)
	}
	if source != "result_file" || values["gflops"] != 123.45 {
		t.Fatalf("unexpected updated result: source=%q values=%v", source, values)
	}
}

func TestManagerRunsConfiguredStreamScript(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("script execution is Linux-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	streamOutput := "#!/bin/sh\necho 'Copy: 1000.1'\necho 'Scale: 900.2'\necho 'Add: 800.3'\necho 'Triad: 700.4'\n"
	if err := os.WriteFile(script, []byte(streamOutput), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{
		Enabled:           true,
		ScriptPath:        script,
		ReportPath:        filepath.Join(dir, "stress-latest.json"),
		DefaultBenchmarks: []string{"stream"},
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Second},
		},
	})
	report, err := manager.Start(nil)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); report.Status == StatusRunning && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
		report, err = manager.Job(report.JobID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if report.Status != StatusHealthy || report.HealthCondition != "Healthy" {
		t.Fatalf("unexpected report: %+v", report)
	}
	if got := report.Benchmarks[0].Values["copy_mb_s"]; got != 1000.1 {
		t.Fatalf("copy_mb_s=%v want 1000.1", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "stress-latest.json")); err != nil {
		t.Fatalf("report was not written: %v", err)
	}
}

func TestManagerRunsConfiguredHPLWithoutYAMLAssetPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("script execution is Linux-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	hplOutput := `#!/bin/sh
if [ "$#" -ne 1 ] || [ "$1" != "hpl" ]; then
    echo "unexpected arguments: $*"
    exit 9
fi
echo "T/V N NB P Q Time Gflops"
echo "WR00R2R4 20000 128 2 2 30.50 1.0000e+02"
echo "1 tests completed and passed residual checks,"
echo "0 tests completed and failed residual checks,"
`
	if err := os.WriteFile(script, []byte(hplOutput), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{
		Enabled:           true,
		ScriptPath:        script,
		ReportPath:        filepath.Join(dir, "stress-latest.json"),
		DefaultBenchmarks: []string{"hpl"},
		Benchmarks: map[string]BenchmarkConfig{
			"hpl": {Enabled: true, Timeout: time.Second},
		},
	})
	report, err := manager.Start(nil)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); report.Status == StatusRunning && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
		report, err = manager.Job(report.JobID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if report.Status != StatusHealthy || report.HealthCondition != "Healthy" {
		t.Fatalf("unexpected report: %+v", report)
	}
	values := report.Benchmarks[0].Values
	if values["gflops"] != 100 || values["process"] != 4 {
		t.Fatalf("unexpected HPL values: %v", values)
	}
}

func TestManagerRunsConfiguredHPCGWithoutYAMLAssetPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("script execution is Linux-only")
	}
	dir := t.TempDir()
	resultPath := filepath.Join(dir, "HPCG-Benchmark_3.1_current.txt")
	script := filepath.Join(dir, "benchmark_check.sh")
	hpcgOutput := `#!/bin/sh
if [ "$#" -ne 1 ] || [ "$1" != "hpcg" ]; then
    echo "unexpected arguments: $*"
    exit 9
fi
printf '%s\n' \
  'Final Summary::HPCG result is VALID with a GFLOP/s rating of=12.5' \
  'Final Summary::Results are valid but execution time (sec) is=61' \
  > '` + resultPath + `'
`
	if err := os.WriteFile(script, []byte(hpcgOutput), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{
		Enabled:           true,
		ScriptPath:        script,
		ReportPath:        filepath.Join(dir, "stress-latest.json"),
		DefaultBenchmarks: []string{"hpcg"},
		Benchmarks: map[string]BenchmarkConfig{
			"hpcg": {Enabled: true, ResultDir: dir, Timeout: time.Second},
		},
	})
	report, err := manager.Start(nil)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); report.Status == StatusRunning && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
		report, err = manager.Job(report.JobID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if report.Status != StatusHealthy || report.HealthCondition != "Healthy" {
		t.Fatalf("unexpected report: %+v", report)
	}
	result := report.Benchmarks[0]
	if result.Source != "result_file" || result.Values["gflops"] != 12.5 ||
		result.Values["time_seconds"] != 61 {
		t.Fatalf("unexpected HPCG result: %+v", result)
	}
}

func TestManagerRejectsDisabledBenchmark(t *testing.T) {
	manager := NewManager(Config{
		Enabled: true,
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: false},
		},
	})

	_, err := manager.Start([]string{"stream"})
	if err == nil || err.Error() != `benchmark "stream" is disabled in configuration` {
		t.Fatalf("expected disabled benchmark error, got %v", err)
	}
}

func TestManagerRejectsTimeoutExtension(t *testing.T) {
	manager := NewManager(Config{
		Enabled: true,
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Second},
		},
	})

	_, err := manager.StartWithOptions([]string{"stream"}, RunOptions{Timeout: 2 * time.Second})
	if err == nil || err.Error() != `requested timeout 2s exceeds configured maximum 1s for benchmark "stream"` {
		t.Fatalf("expected timeout extension error, got %v", err)
	}
}

func TestManagerTreatsConfiguredTimeLimitAsSuccessfulBenchmark(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("script execution is Linux-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile :; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{
		Enabled:           true,
		ScriptPath:        script,
		ReportPath:        filepath.Join(dir, "stress-latest.json"),
		DefaultBenchmarks: []string{"stream", "hpl", "hpcg"},
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: 50 * time.Millisecond},
			"hpl":    {Enabled: true, Timeout: 50 * time.Millisecond},
			"hpcg":   {Enabled: true, ResultDir: dir, Timeout: 50 * time.Millisecond},
		},
	})
	report, err := manager.Start(nil)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); report.Status == StatusRunning && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
		report, err = manager.Job(report.JobID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if report.Status != StatusHealthy || report.HealthCondition != "Healthy" {
		t.Fatalf("unexpected time-limit report: %+v", report)
	}
	for _, result := range report.Benchmarks {
		if result.Status != StatusTimeLimitReached || len(result.Values) != 0 {
			t.Fatalf("time-limited benchmark should pass without performance values: %+v", result)
		}
	}
}

func TestManagerRejectsAscendNPUBurnOuterTimeout(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("script execution is Linux-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile :; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{
		Enabled: true, ScriptPath: script, ReportPath: filepath.Join(dir, "stress-latest.json"),
		DefaultBenchmarks: []string{"npu_burn"},
		Benchmarks: map[string]BenchmarkConfig{
			"npu_burn": {Enabled: true, Timeout: 50 * time.Millisecond},
		},
	})
	report, err := manager.Start(nil)
	if err != nil {
		t.Fatal(err)
	}
	report = waitForJob(t, manager, report.JobID)
	if report.Status != StatusUnhealthy || len(report.Benchmarks) != 1 ||
		report.Benchmarks[0].Status != StatusUnhealthy ||
		!strings.Contains(report.Benchmarks[0].Message, "complete validated result") {
		t.Fatalf("NPU Burn timeout must not be accepted as pass: %+v", report)
	}
}

func TestManagerRetainsAscendNPUBurnFailureMetrics(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("script execution is Linux-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	content := "#!/bin/sh\nprintf '%s\\n' 'CATMONITOR_NPU_BURN_SUMMARY devices=1 cases=2 passed=1 failed=1 errors=1 case_time_seconds=4.5'\nexit 1\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{
		Enabled: true, ScriptPath: script, ReportPath: filepath.Join(dir, "stress-latest.json"),
		DefaultBenchmarks: []string{"npu_burn"},
		Benchmarks: map[string]BenchmarkConfig{
			"npu_burn": {Enabled: true, Timeout: time.Second},
		},
	})
	report, err := manager.Start(nil)
	if err != nil {
		t.Fatal(err)
	}
	report = waitForJob(t, manager, report.JobID)
	result := report.Benchmarks[0]
	if report.Status != StatusUnhealthy || result.Status != StatusUnhealthy ||
		result.Source != "result_csv" || result.Values["cases"] != 2 ||
		result.Values["passed"] != 1 || result.Values["failed"] != 1 ||
		result.Values["errors"] != 1 {
		t.Fatalf("NPU Burn failure metrics were not retained: %+v", report)
	}
}

func TestManagerRejectsUnwritableInitialReport(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("script execution is Linux-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	notDirectory := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(notDirectory, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{
		Enabled:    true,
		ScriptPath: script,
		ReportPath: filepath.Join(notDirectory, "stress-latest.json"),
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Second},
		},
	})
	if _, err := manager.Start([]string{"stream"}); err == nil ||
		!strings.Contains(err.Error(), "persist initial stress report") {
		t.Fatalf("expected report persistence error, got %v", err)
	}
}

func TestManagerReportsLaterPersistenceFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("script execution is Linux-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'Copy: 1\\nScale: 2\\nAdd: 3\\nTriad: 4\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{
		Enabled:    true,
		ScriptPath: script,
		ReportPath: filepath.Join(dir, "stress-latest.json"),
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Second},
		},
	})
	writes := 0
	manager.writeReport = func(Report) error {
		writes++
		if writes == 1 {
			return nil
		}
		return errors.New("simulated disk failure")
	}

	report, err := manager.Start([]string{"stream"})
	if err != nil {
		t.Fatal(err)
	}
	report = waitForJob(t, manager, report.JobID)
	if report.Status != StatusHealthy {
		t.Fatalf("benchmark result should remain healthy despite report failure: %+v", report)
	}
	if !strings.Contains(report.ReportError, "simulated disk failure") {
		t.Fatalf("missing later persistence error: %+v", report)
	}
}

func TestManagerRejectsConcurrentJobAndCancelsActiveJob(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("script execution is Linux-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile :; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{
		Enabled:    true,
		ScriptPath: script,
		ReportPath: filepath.Join(dir, "stress-latest.json"),
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Second},
		},
	})
	report, err := manager.Start([]string{"stream"})
	if err != nil {
		t.Fatal(err)
	}
	busyReport, err := manager.Start([]string{"stream"})
	if !errors.Is(err, ErrBusy) || busyReport.JobID != report.JobID {
		t.Fatalf("second job should return active report and ErrBusy: report=%+v err=%v", busyReport, err)
	}
	if err := manager.Cancel(report.JobID); err != nil {
		t.Fatal(err)
	}
	report = waitForJob(t, manager, report.JobID)
	if report.Status != StatusCancelled {
		t.Fatalf("cancelled job status=%q", report.Status)
	}
}

func TestManagerReloadsPersistedReport(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("script execution is Linux-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	reportPath := filepath.Join(dir, "stress-latest.json")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'Copy: 1\\nScale: 2\\nAdd: 3\\nTriad: 4\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Enabled:    true,
		ScriptPath: script,
		ReportPath: reportPath,
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Second},
		},
	}
	manager := NewManager(cfg)
	report, err := manager.Start([]string{"stream"})
	if err != nil {
		t.Fatal(err)
	}
	report = waitForJob(t, manager, report.JobID)

	restarted := NewManager(cfg)
	loaded, err := restarted.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.JobID != report.JobID || loaded.Status != StatusHealthy {
		t.Fatalf("reloaded report mismatch: got=%+v want=%+v", loaded, report)
	}
	history, err := restarted.History(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].JobID != report.JobID {
		t.Fatalf("reloaded history mismatch: %+v", history)
	}
}

func TestManagerHistoryIsBoundedNewestFirstAndOmitsOutput(t *testing.T) {
	dir := t.TempDir()
	reportPath := filepath.Join(dir, "stress-latest.json")
	manager := NewManager(Config{ReportPath: reportPath})
	existing := make([]Report, maxHistoryReports)
	for i := range existing {
		existing[i] = Report{
			JobID:     fmt.Sprintf("job-%03d", maxHistoryReports-i),
			StartedAt: time.Unix(int64(maxHistoryReports-i), 0),
			Status:    StatusHealthy,
		}
	}
	if err := writeJSONAtomic(historyPath(reportPath), existing); err != nil {
		t.Fatal(err)
	}
	latest := Report{
		JobID:     "job-latest",
		StartedAt: time.Unix(200, 0),
		Status:    StatusHealthy,
		Benchmarks: []BenchmarkResult{{
			Name:   "hpl",
			Status: StatusHealthy,
			Values: map[string]float64{"gflops": 205.13},
			Output: strings.Repeat("diagnostic output", 100),
		}},
	}
	if err := manager.appendHistoryFile(latest); err != nil {
		t.Fatal(err)
	}

	history, err := manager.History(maxHistoryReports)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != maxHistoryReports {
		t.Fatalf("history length=%d want=%d", len(history), maxHistoryReports)
	}
	if history[0].JobID != latest.JobID || history[len(history)-1].JobID != "job-002" {
		t.Fatalf("unexpected bounded history order: first=%q last=%q", history[0].JobID, history[len(history)-1].JobID)
	}
	if history[0].Benchmarks[0].Output != "" || history[0].Benchmarks[0].Values["gflops"] != 205.13 {
		t.Fatalf("history should retain metrics but omit command output: %+v", history[0].Benchmarks[0])
	}
	if got := historyPath(reportPath); got != filepath.Join(dir, "stress-history.json") {
		t.Fatalf("history path=%q", got)
	}
	limited, err := manager.History(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 3 {
		t.Fatalf("limited history length=%d", len(limited))
	}
}

func TestManagerRefreshesReportsWrittenByAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{ReportPath: filepath.Join(dir, "stress-latest.json")}
	writer := NewManager(cfg)
	observer := NewManager(cfg)

	first := Report{JobID: "cli-first", Initiator: InitiatorCLI, Status: StatusRunning}
	if err := writer.writeReportFile(first); err != nil {
		t.Fatal(err)
	}
	got, err := observer.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if got.JobID != first.JobID || got.Initiator != InitiatorCLI {
		t.Fatalf("first external report mismatch: %+v", got)
	}

	second := Report{JobID: "cli-second", Initiator: InitiatorCLI, Status: StatusHealthy}
	if err := writer.writeReportFile(second); err != nil {
		t.Fatal(err)
	}
	got, err = observer.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if got.JobID != second.JobID || got.Status != StatusHealthy {
		t.Fatalf("observer returned stale report: %+v", got)
	}
}

func TestManagersShareLinuxJobLockAndLiveReport(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("cross-process job lock is Linux-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile :; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Enabled:    true,
		ScriptPath: script,
		ReportPath: filepath.Join(dir, "stress-latest.json"),
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Second},
		},
	}
	cliManager := NewManager(cfg)
	webManager := NewManager(cfg)
	started, err := cliManager.StartWithOptions([]string{"stream"}, RunOptions{Initiator: InitiatorCLI})
	if err != nil {
		t.Fatal(err)
	}

	observed, err := webManager.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if observed.JobID != started.JobID || observed.Initiator != InitiatorCLI || observed.Cancellable {
		t.Fatalf("web observer did not see external CLI report: %+v", observed)
	}
	busy, err := webManager.StartWithOptions([]string{"stream"}, RunOptions{Initiator: InitiatorWeb})
	if !errors.Is(err, ErrBusy) || busy.JobID != started.JobID {
		t.Fatalf("second manager should be locked out: report=%+v err=%v", busy, err)
	}
	if webManager.CanCancel(started.JobID) {
		t.Fatal("observer must not cancel another process's job")
	}

	if err := cliManager.Cancel(started.JobID); err != nil {
		t.Fatal(err)
	}
	finished := waitForJob(t, cliManager, started.JobID)
	if finished.Status != StatusCancelled {
		t.Fatalf("cancelled CLI report=%+v", finished)
	}
	observed, err = webManager.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if observed.Status != StatusCancelled {
		t.Fatalf("observer did not refresh terminal report: %+v", observed)
	}
}

func TestLinuxJobLockAcrossProcesses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("cross-process job lock is Linux-only")
	}
	dir := t.TempDir()
	reportPath := filepath.Join(dir, "stress-latest.json")
	readyPath := filepath.Join(dir, "ready")
	releasePath := filepath.Join(dir, "release")
	var childOutput bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^TestJobLockHelperProcess$")
	cmd.Env = append(os.Environ(),
		"CATMONITOR_TEST_LOCK_REPORT="+reportPath,
		"CATMONITOR_TEST_LOCK_READY="+readyPath,
		"CATMONITOR_TEST_LOCK_RELEASE="+releasePath,
	)
	cmd.Stdout = &childOutput
	cmd.Stderr = &childOutput
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	childDone := make(chan error, 1)
	go func() { childDone <- cmd.Wait() }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper did not acquire lock: %s", childOutput.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	if release, err := acquireJobLock(reportPath); !errors.Is(err, ErrBusy) {
		if err == nil {
			_ = release()
		}
		t.Fatalf("parent acquired helper's lock: err=%v", err)
	}
	if err := os.WriteFile(releasePath, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-childDone:
		if err != nil {
			t.Fatalf("helper failed: %v output=%s", err, childOutput.String())
		}
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("helper did not release lock")
	}

	release, err := acquireJobLock(reportPath)
	if err != nil {
		t.Fatalf("lock unavailable after helper exit: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}

func TestJobLockHelperProcess(t *testing.T) {
	reportPath := os.Getenv("CATMONITOR_TEST_LOCK_REPORT")
	if reportPath == "" {
		return
	}
	release, err := acquireJobLock(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := os.WriteFile(os.Getenv("CATMONITOR_TEST_LOCK_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(os.Getenv("CATMONITOR_TEST_LOCK_RELEASE")); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for release")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestManagerShutdownCancelsActiveJob(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("real stress execution is Linux-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile :; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{
		Enabled:    true,
		ScriptPath: script,
		ReportPath: filepath.Join(dir, "stress-latest.json"),
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Second},
		},
	})
	started, err := manager.StartWithOptions([]string{"stream"}, RunOptions{Initiator: InitiatorWeb})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	report, err := manager.Job(started.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusCancelled {
		t.Fatalf("shutdown report=%+v", report)
	}

	second := NewManager(manager.Config())
	restarted, err := second.StartWithOptions([]string{"stream"}, RunOptions{Initiator: InitiatorCLI})
	if err != nil {
		t.Fatalf("shutdown did not release shared job lock: %v", err)
	}
	if err := second.Cancel(restarted.JobID); err != nil {
		t.Fatal(err)
	}
	_ = waitForJob(t, second, restarted.JobID)
}

func TestManagerDefensivelyCopiesConfigAndReports(t *testing.T) {
	cfg := Config{
		DefaultBenchmarks: []string{"stream"},
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true},
		},
	}
	manager := NewManager(cfg)
	cfg.DefaultBenchmarks[0] = "hpl"
	cfg.Benchmarks["stream"] = BenchmarkConfig{}

	got := manager.Config()
	if got.DefaultBenchmarks[0] != "stream" || !got.Benchmarks["stream"].Enabled {
		t.Fatalf("manager config was mutated through caller-owned data: %+v", got)
	}
	got.DefaultBenchmarks[0] = "hpcg"
	got.Benchmarks["stream"] = BenchmarkConfig{}
	again := manager.Config()
	if again.DefaultBenchmarks[0] != "stream" || !again.Benchmarks["stream"].Enabled {
		t.Fatalf("manager config was mutated through Config result: %+v", again)
	}

	report := Report{Benchmarks: []BenchmarkResult{{Values: map[string]float64{"copy": 1}}}}
	cloned := copyReport(report)
	cloned.Benchmarks[0].Values["copy"] = 2
	if report.Benchmarks[0].Values["copy"] != 1 {
		t.Fatal("copyReport shares benchmark values map")
	}
}

func TestManagerEmitsStructuredLifecycleLogs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("script execution is Linux-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'Copy: 1\\nScale: 2\\nAdd: 3\\nTriad: 4\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	manager := NewManagerWithLogger(Config{
		Enabled:    true,
		ScriptPath: script,
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Second},
		},
	}, logger)
	report, err := manager.Start([]string{"stream"})
	if err != nil {
		t.Fatal(err)
	}
	_ = waitForJob(t, manager, report.JobID)
	text := logs.String()
	for _, message := range []string{
		`"msg":"stress job started"`,
		`"msg":"stress benchmark started"`,
		`"msg":"stress benchmark finished"`,
		`"msg":"stress job finished"`,
		`"job_id":"` + report.JobID + `"`,
	} {
		if !strings.Contains(text, message) {
			t.Errorf("structured log missing %s: %s", message, text)
		}
	}
}

func waitForJob(t *testing.T, manager *Manager, jobID string) Report {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		report, err := manager.Job(jobID)
		if err != nil {
			t.Fatal(err)
		}
		if report.Status != StatusRunning {
			return report
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stress job %s did not finish", jobID)
	return Report{}
}

func configuredDispatcher(t *testing.T, dir string, values map[string]string) string {
	t.Helper()
	if deviceSelection, configured := values["NPU_BURN_DEVICE"]; configured &&
		values["NPU_BURN_BACKEND"] != "docker_exec" {
		if _, hasRoot := values["NPU_BURN_DEVICE_ROOT"]; !hasRoot {
			deviceRoot := filepath.Join(dir, "npu-devices")
			if err := os.MkdirAll(deviceRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			deviceIDs := strings.Split(deviceSelection, ",")
			if deviceSelection == "all" {
				deviceIDs = []string{"0"}
			}
			for _, deviceID := range deviceIDs {
				if _, err := strconv.Atoi(deviceID); err != nil {
					continue
				}
				if err := os.WriteFile(filepath.Join(deviceRoot, "davinci"+deviceID), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			values["NPU_BURN_DEVICE_ROOT"] = deviceRoot
		}
	}
	data, err := os.ReadFile("benchmark_check.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for name, value := range values {
		prefix := name + "="
		start := strings.Index(script, prefix)
		if start < 0 {
			t.Fatalf("dispatcher assignment %s not found", name)
		}
		end := strings.IndexByte(script[start:], '\n')
		if end < 0 {
			t.Fatalf("dispatcher assignment %s has no line ending", name)
		}
		replacement := prefix + shellLiteral(value)
		script = script[:start] + replacement + script[start+end:]
	}
	path := filepath.Join(dir, "benchmark_check.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
