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
		"command -v",
		`-x OPENBLAS_NUM_THREADS`,
		`-x OMP_NUM_THREADS`,
		`-x OMP_DYNAMIC`,
		"--allow-run-as-root",
		"--map-by",
		"--bind-to",
		"-mca",
	} {
		if strings.Contains(script, forbidden) {
			t.Errorf("benchmark_check.sh contains host-specific or unsupported value %q", forbidden)
		}
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
