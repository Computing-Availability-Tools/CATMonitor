package resultparse

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseStandardBenchmarks(t *testing.T) {
	tests := []struct {
		name, output, source string
		key                  string
		want                 float64
	}{
		{"stream", "Copy: 100.1\nScale: 200.2\nAdd: 300.3\nTriad: 400.4\n", "stdout", "triad_mb_s", 400.4},
		{"hpl", "T/V                N    NB     P     Q               Time                 Gflops\nWR00C2R4       50000   256     4     2             150.60             5.5337e+02\n", "stdout", "gflops", 553.37},
		{"npu_burn", "CATMONITOR_NPU_BURN_SUMMARY devices=2 cases=4 passed=4 failed=0 errors=0 case_time_seconds=12.5\n", "result_csv", "passed", 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values, source, err := Parse(test.name, test.output, "", Snapshot{})
			if err != nil {
				t.Fatal(err)
			}
			if source != test.source || values[test.key] != test.want {
				t.Fatalf("source=%q values=%v", source, values)
			}
		})
	}
}

func TestHPCGRequiresFreshResult(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "HPCG-Benchmark_3.1_test.txt")
	old := "HPCG result is VALID with a GFLOP/s rating of=1.0\nResults are valid but execution time (sec) is=2.0\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := Capture("hpcg", dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Parse("hpcg", "", dir, before); err == nil {
		t.Fatal("stale HPCG result was accepted")
	}
	fresh := "HPCG result is VALID with a GFLOP/s rating of=22.1496\nResults are valid but execution time (sec) is=62.2467\n"
	if err := os.WriteFile(path, []byte(fresh), 0o600); err != nil {
		t.Fatal(err)
	}
	values, source, err := Parse("hpcg", "", dir, before)
	if err != nil {
		t.Fatal(err)
	}
	if source != "result_file" || values["gflops"] != 22.1496 || values["time_seconds"] != 62.2467 {
		t.Fatalf("source=%q values=%v", source, values)
	}
}

func TestNPUBurnRejectsIncompleteSummary(t *testing.T) {
	values, source, err := Parse("npu_burn", "CATMONITOR_NPU_BURN_SUMMARY devices=2 cases=4 passed=3 failed=1 errors=0 case_time_seconds=12.5\n", "", Snapshot{})
	if err == nil || source != "result_csv" || values["failed"] != 1 {
		t.Fatalf("err=%v source=%q values=%v", err, source, values)
	}
}
