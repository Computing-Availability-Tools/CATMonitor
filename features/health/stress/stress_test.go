package stress

import (
	"os"
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
		`STREAM_EXECUTABLE=""`,
		`HPL_EXECUTABLE=""`,
		`HPL_INPUT=""`,
		`HPL_MPI_PROCESSES=0`,
		`HPCG_EXECUTABLE=""`,
		`HPCG_MPI_PROCESSES=0`,
		`require_absolute_executable`,
		`-x OPENBLAS_NUM_THREADS`,
		`-x OMP_NUM_THREADS`,
		`-np "$HPL_MPI_PROCESSES"`,
		`export OMP_DYNAMIC=FALSE`,
		`--map-by core`,
		`--bind-to core`,
		`-np "$HPCG_MPI_PROCESSES"`,
		`--nx="$HPCG_NX"`,
		`--rt="$HPCG_RUNTIME_SECONDS"`,
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
	} {
		if strings.Contains(script, forbidden) {
			t.Errorf("benchmark_check.sh contains host-specific or unsupported value %q", forbidden)
		}
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
