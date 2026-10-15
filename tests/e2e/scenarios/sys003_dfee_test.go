//go:build e2e

package scenarios

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	e2e "github.com/Computing-Availability-Tools/CATMonitor/tests/e2e/framework"
)

// TestSYS003DfeeDashboard covers SYS-003 (dfee 能效仪表盘).
//
// | TC   | Priority | Description                                        |
// |------|----------|----------------------------------------------------|
// | TC-007| P0       | /api/dfee 返回图表数据                              |
// | TC-011| P1       | -csv enabled → CSV 文件产出                        |
// | TC-012| P1       | -exporter enabled → :9333 可用                     |
// | TC-016| P1       | snapshot 未就绪→返回 503                           |
func TestSYS003DfeeDashboard(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{})
	df := e2e.StartDfee(t, bins, ws, d.SnapshotDir())
	d.WaitComponentSnapshot(t, "cpu", 60*time.Second)

	t.Run("TC-007_api_dfee", func(t *testing.T) {
		resp := df.API(t)
		if resp.Timestamp.IsZero() {
			t.Error("timestamp is zero")
		}
		if resp.RefreshInterval <= 0 {
			t.Errorf("refresh_interval_ms = %d, want > 0", resp.RefreshInterval)
		}
		if len(resp.Charts) == 0 {
			t.Error("charts is empty")
		}
		for _, ch := range resp.Charts {
			if ch.ID == "" || ch.Title == "" {
				t.Errorf("chart missing id or title: %+v", ch)
			}
		}
	})
}

// TestSYS003_011_CsvOutput covers TC-011 (dfee -csv enabled → CSV 产出).
func TestSYS003_011_CsvOutput(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{})
	d.WaitComponentSnapshot(t, "cpu", 60*time.Second)

	// Wait for :19323 to be free (previous test's dfee may still be shutting down).
	e2e.WaitPortFree(t, "127.0.0.1:19323", 15*time.Second)

	csvDir := filepath.Join(ws.Dir, "csv")
	dfeeProc := e2e.StartProc(t, "dfee-csv", filepath.Join(ws.Dir, "dfee-csv.log"), "", nil,
		bins.Dfee, "-addr", "127.0.0.1:19323", "-snapshot-dir", d.SnapshotDir(),
		"-csv", "enabled", "-csv-dir", csvDir, "-csv-interval", "1s")

	// Verify dfee started successfully.
	time.Sleep(2 * time.Second)
	if !dfeeProc.Alive() {
		dfeeProc.Stop()
		t.Fatal("dfee-csv process died (check dfee-csv.log)")
	}

	// Wait for a CSV file to appear.
	deadline := time.Now().Add(30 * time.Second)
	var csvPath string
	for time.Now().Before(deadline) {
		if entries, err := os.ReadDir(csvDir); err == nil && len(entries) > 0 {
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".csv") {
					csvPath = filepath.Join(csvDir, e.Name())
					break
				}
			}
			if csvPath != "" {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	dfeeProc.Stop()

	if csvPath == "" {
		t.Fatal("no CSV file produced within 30s")
	}
	data, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("CSV has %d lines, want >= 2 (header + data)", len(lines))
	}
	if lines[0] != "timestamp,metric_name,labels,value" {
		t.Errorf("CSV header = %q, want %q", lines[0], "timestamp,metric_name,labels,value")
	}
}

// TestSYS003_012_ExporterToggle covers TC-012 (-exporter enabled → :9333).
func TestSYS003_012_ExporterToggle(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{})
	d.WaitComponentSnapshot(t, "cpu", 60*time.Second)

	e2e.WaitPortFree(t, "127.0.0.1:19323", 15*time.Second)

	// Default: :9333 should NOT be listening.
	proc := e2e.StartProc(t, "dfee-default", filepath.Join(ws.Dir, "dfee-default.log"), "", nil,
		bins.Dfee, "-addr", "127.0.0.1:19323", "-snapshot-dir", d.SnapshotDir())
	time.Sleep(3 * time.Second)
	if conn, err := netDial("127.0.0.1:9333"); err == nil {
		conn.Close()
		t.Error(":9333 is listening with default (disabled) exporter")
	}
	proc.Stop()
	e2e.WaitPortFree(t, "127.0.0.1:19323", 15*time.Second)

	// Enabled: :9333 should serve metrics.
	proc2 := e2e.StartProc(t, "dfee-exporter", filepath.Join(ws.Dir, "dfee-exporter.log"), "", nil,
		bins.Dfee, "-addr", "127.0.0.1:19323", "-snapshot-dir", d.SnapshotDir(),
		"-exporter", "enabled")
	time.Sleep(2 * time.Second)
	if !proc2.Alive() {
		proc2.Stop()
		t.Fatal("dfee-exporter process died (check dfee-exporter.log)")
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := httpGet("http://127.0.0.1:9333/metrics")
		if err == nil {
			body := readAllBody(resp)
			resp.Body.Close()
			proc2.Stop()
			if !strings.Contains(body, "node_") && !strings.Contains(body, "static_") {
				t.Errorf(":9333/metrics lacks node_*/static_* metrics")
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	proc2.Stop()
	t.Fatal(":9333/metrics did not respond within 20s with exporter enabled")
}

// TestSYS003_016_NotReady503 covers TC-016 (dfee snapshot 未就绪→503).
func TestSYS003_016_NotReady503(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	emptyDir := filepath.Join(ws.Dir, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	proc := e2e.StartProc(t, "dfee-empty", filepath.Join(ws.Dir, "dfee-empty.log"), "", nil,
		bins.Dfee, "-addr", "127.0.0.1:19323", "-snapshot-dir", emptyDir)

	// Wait for dfee to start, then check /api/dfee returns 503.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := httpGet("http://127.0.0.1:19323/api/dfee")
		if err == nil {
			resp.Body.Close()
			proc.Stop()
			if resp.StatusCode != 503 {
				t.Errorf("status = %d, want 503", resp.StatusCode)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	proc.Stop()
	t.Fatal("dfee did not respond within 10s")
}
