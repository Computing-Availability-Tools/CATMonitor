//go:build !linux

package stress

// Unsupported-platform command regression tests.
import (
	"testing"
	"time"
)

func TestNonLinuxManagerReturnsUnsupported(t *testing.T) {
	manager := NewManager(Config{
		Enabled: true,
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Second},
		},
	})
	report, err := manager.Start([]string{"stream"})
	if err != nil {
		t.Fatal(err)
	}
	report = waitForJob(t, manager, report.JobID)
	if report.Status != StatusUnsupported ||
		len(report.Benchmarks) != 1 ||
		report.Benchmarks[0].Status != StatusUnsupported {
		t.Fatalf("non-Linux stress result should be unsupported: %+v", report)
	}
}
