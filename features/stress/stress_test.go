package stress

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestParseHPL(t *testing.T) {
	values, source, err := parseHPL("header\nT/V N NB P Q Time Gflops\nWR11C2R4 100 64 1 1 12.5 321.6\n")
	if err != nil {
		t.Fatal(err)
	}
	if source != "stdout" || values["time_seconds"] != 12.5 || values["gflops"] != 321.6 {
		t.Fatalf("unexpected HPL result: source=%q values=%v", source, values)
	}
}

func TestParseHPCGFallsBackToLatestResultFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "HPCG-Benchmark_001.txt")
	content := "Final Summary::HPCG result is VALID with a GFLOP/s rating of=123.45\nFinal Summary::Results are valid but execution time (sec) is=67.89\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	values, source, err := parseHPCG("ordinary command output", dir)
	if err != nil {
		t.Fatal(err)
	}
	if source != "result_file" || values["gflops"] != 123.45 || values["time_seconds"] != 67.89 {
		t.Fatalf("unexpected HPCG result: source=%q values=%v", source, values)
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
