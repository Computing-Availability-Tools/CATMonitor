//go:build e2e

package scenarios

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	e2e "github.com/Computing-Availability-Tools/CATMonitor/tests/e2e/framework"
)

// TestSYS001DaemonOutput covers SYS-001 (Daemon 启动并产出监控数据).
// One shared daemon, 4 sub-tests + 1 dedicated (SIGTERM needs its own).
//
// | TC   | Priority | Description                                        |
// |------|----------|----------------------------------------------------|
// | TC-001| P0       | /metrics 端点输出 catmonitor 指标                  |
// | TC-002| P0       | snapshot 文件落盘                                  |
// | TC-003| P1       | JSONL 历史文件按部件写入                            |
// | TC-024| P1       | counter 类型判定(_time/_total 后缀)               |
// | TC-047| P1       | daemon 优雅退出(SIGTERM)                          |
func TestSYS001DaemonOutput(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{})
	// Wait for the slowest first component (cpu, delayed by ipmi on slow BMC).
	d.WaitComponentSnapshot(t, "cpu", 60*time.Second)

	t.Run("TC-001_metrics_endpoint", func(t *testing.T) {
		body, contentType := e2e.HTTPGet(t, d.MetricsURL())
		if !strings.Contains(contentType, "text/plain") {
			t.Errorf("Content-Type = %q, want text/plain", contentType)
		}
		if !strings.Contains(contentType, "version=0.0.4") {
			t.Errorf("Content-Type = %q, want version=0.0.4", contentType)
		}
		if !strings.Contains(body, "catmonitor_cpu_usage") {
			t.Errorf("/metrics lacks catmonitor_cpu_usage lines")
		}
		if !strings.Contains(body, "# HELP") || !strings.Contains(body, "# TYPE") {
			t.Errorf("/metrics lacks HELP/TYPE comment lines")
		}
	})

	t.Run("TC-002_snapshot_files", func(t *testing.T) {
		// Global snapshot exists (already waited for in StartDaemon).
		g := d.Global(t)
		if g.SessionID == "" {
			t.Error("snapshot.json missing session_id")
		}
		if g.RefreshInterval <= 0 {
			t.Errorf("snapshot.json refresh_interval_ms = %d, want > 0", g.RefreshInterval)
		}
		// Per-component snapshot exists (already waited for cpu).
		comp, err := os.Stat(filepath.Join(d.SnapshotDir(), "snapshot_cpu.json"))
		if err != nil {
			t.Fatalf("snapshot_cpu.json not found: %v", err)
		}
		if comp.Size() == 0 {
			t.Error("snapshot_cpu.json is empty")
		}
	})

	t.Run("TC-003_jsonl_files", func(t *testing.T) {
		path := filepath.Join(d.DataDir(), "cpu_"+time.Now().Format("2006-01-02")+".jsonl")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("cpu JSONL not found: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) < 1 {
			t.Fatalf("cpu JSONL has %d lines, want >= 1", len(lines))
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
			t.Errorf("first JSONL line is not valid JSON: %v", err)
		}
		if m["component"] != "cpu" {
			t.Errorf("JSONL component = %v, want cpu", m["component"])
		}
	})

	t.Run("TC-024_counter_types", func(t *testing.T) {
		body, _ := e2e.HTTPGet(t, d.MetricsURL())
		if !strings.Contains(body, "# TYPE catmonitor_cpu_user_time counter") {
			t.Errorf("user_time should be counter type")
		}
		if !strings.Contains(body, "# TYPE catmonitor_cpu_usage gauge") {
			t.Errorf("usage should be gauge type")
		}
	})
}

// TestSYS001_047_SigtermGracefulExit covers TC-047 (daemon 优雅退出).
// Needs a dedicated daemon because it kills the process. The graceful
// exit can take up to ~10s on slow-BMC hosts (scheduler.Stop() waits for
// in-flight ipmitool calls); the assertion allows 15s before declaring a
// hang.
func TestSYS001_047_SigtermGracefulExit(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{})

	// Send SIGTERM and time the exit.
	start := time.Now()
	d.Stop()
	elapsed := time.Since(start)

	if elapsed > 15*time.Second {
		t.Errorf("daemon took %v to exit gracefully (want < 15s; slow BMC can legitimately need ~10s)", elapsed)
	}
	if d.Alive() {
		t.Error("daemon still alive after Stop")
	}
}
