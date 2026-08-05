//go:build linux

package stress

// Linux process-group regression tests.
import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTimeLimitKillsBenchmarkProcessGroup(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	childPID := filepath.Join(dir, "child.pid")
	content := "#!/bin/sh\nsleep 30 &\necho $! > '" + childPID + "'\nwait\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{
		Enabled:    true,
		ScriptPath: script,
		Benchmarks: map[string]BenchmarkConfig{
			"hpl": {Enabled: true, Timeout: 100 * time.Millisecond},
		},
	})
	report, err := manager.Start([]string{"hpl"})
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
	if report.Status != StatusHealthy || report.Benchmarks[0].Status != StatusTimeLimitReached {
		t.Fatalf("unexpected report: %+v", report)
	}
	pidData, err := os.ReadFile(childPID)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		err = syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) || processIsZombie(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("benchmark child process %d is still running: %v", pid, err)
}

func processIsZombie(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(data))
	return len(fields) > 2 && fields[2] == "Z"
}
