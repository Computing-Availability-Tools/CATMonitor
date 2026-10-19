package stress

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNPUCSVHistoryPreservesBreakdownWithoutVerboseOutput(t *testing.T) {
	output := "large workload log\nCATMONITOR_NPU_BURN_CASE_GROUPS=" +
		`[{"device":0,"operator":"conv2d","cases":6,"passed":6,"failed":0,"errors":0,"case_time_seconds":22.626}]` +
		"\nCATMONITOR_NPU_BURN_RESULT_CSV=/results/current.csv\n"
	values := map[string]float64{"devices": 1, "cases": 6, "passed": 6, "failed": 0, "errors": 0, "case_time_seconds": 22.626}
	csv := readNPUCSV(output, values)
	if csv == nil || csv.Path != "/results/current.csv" || len(csv.Groups) != 1 || csv.Groups[0].Cases != 6 {
		t.Fatal(csv)
	}
	report := Report{JobID: "aabb", Benchmarks: []BenchmarkResult{{Name: "npu_burn", Output: output, NPUCSV: csv}}}
	copy := copyReport(report)
	copy.Benchmarks[0].NPUCSV.Groups[0].Cases = 99
	if csv.Groups[0].Cases != 6 {
		t.Fatal("copied report aliases mutable CSV metadata")
	}
	m := &Manager{cfg: Config{ReportPath: filepath.Join(t.TempDir(), "stress-latest.json")}}
	if err := m.appendHistoryFile(report); err != nil {
		t.Fatal(err)
	}
	history, err := m.History(20)
	if err != nil || len(history) != 1 {
		t.Fatalf("history=%v err=%v", history, err)
	}
	result := history[0].Benchmarks[0]
	if result.Output != "" || result.NPUCSV == nil || result.NPUCSV.Groups[0].Passed != 6 || result.NPUCSV.Path != csv.Path {
		t.Fatalf("history lost CSV identity/breakdown or retained verbose output: %+v", result)
	}
	for _, bad := range []string{
		"old plugin output without breakdown",
		strings.Replace(output, `"cases":6`, `"cases":7`, 1),
		strings.Replace(output, `"errors":0`, `"errors":-1`, 1),
		strings.Replace(output, `"case_time_seconds":22.626`, `"case_time_seconds":99`, 1),
	} {
		if readNPUCSV(bad, values) != nil {
			t.Fatal("accepted missing/invalid/mismatched CSV metadata")
		}
	}
}
