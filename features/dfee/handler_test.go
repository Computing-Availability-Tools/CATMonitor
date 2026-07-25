package dfee

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// writeTestSnapshot writes a minimal snapshot JSON to a temp file.
func writeTestSnapshot(t *testing.T, path string, metrics []collector.Metric) {
	t.Helper()
	snap := snapshot{
		Timestamp:       time.Now(),
		RefreshInterval: 5000,
		Metrics:         metrics,
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestHandleAPI verifies the full API pipeline: snapshot → filter → CPU derive → group.
func TestHandleAPI(t *testing.T) {
	dir := t.TempDir()
	snapPath := filepath.Join(dir, "snapshot.json")

	writeTestSnapshot(t, snapPath, []collector.Metric{
		// NPU efficiency metrics.
		collector.Metric{Component: "npu", Name: "temperature", Value: 55, Unit: "°C", Labels: map[string]string{"npu_id": "0"}, Timestamp: time.Now()},
		collector.Metric{Component: "npu", Name: "aicore_freq", Value: 1200, Unit: "MHz", Labels: map[string]string{"npu_id": "0"}, Timestamp: time.Now()},
		// CPU time metrics (core=total).
		collector.Metric{Component: "cpu", Name: "user_time", Value: 1000, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "nice_time", Value: 10, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "system_time", Value: 500, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "idle_time", Value: 8000, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "iowait_time", Value: 50, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "irq_time", Value: 20, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "softirq_time", Value: 30, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "steal_time", Value: 5, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		// CPU power.
		collector.Metric{Component: "cpu", Name: "power", Value: 95, Unit: "W", Labels: map[string]string{"cpu": "0"}, Timestamp: time.Now()},
		// Non-efficiency metrics.
		collector.Metric{Component: "cpu", Name: "usage", Value: 12.3, Unit: "%", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "gpu", Name: "utilization", Value: 60, Unit: "%", Labels: map[string]string{"gpu_id": "0"}, Timestamp: time.Now()},
	})

	h := NewHandler(snapPath)

	// First call: CPU derivation has no prev → no derived metrics in cpu_utilization chart.
	resp1 := callAPI(t, h)
	if resp1.RefreshInterval != 5000 {
		t.Errorf("refresh_interval=%d, want 5000", resp1.RefreshInterval)
	}
	// 25 charts (was 14, disk_io→6, network→2, mixed-unit splits→+5).
	if len(resp1.Charts) != 25 {
		t.Errorf("expected 25 charts, got %d", len(resp1.Charts))
	}
	// CPU utilization chart should have 0 series on first call (no prev).
	cpuChart := findChart(resp1.Charts, "cpu_utilization")
	if cpuChart == nil {
		t.Fatal("cpu_utilization chart not found")
	}
	if len(cpuChart.Series) != 0 {
		t.Errorf("first call: expected 0 cpu_util series, got %d", len(cpuChart.Series))
	}
	// Raw CPU time metrics should NOT appear in any chart.
	for _, c := range resp1.Charts {
		for _, s := range c.Series {
			if s.ID == "user_time" || s.ID == "idle_time" {
				t.Errorf("raw CPU time metric leaked into chart %q: %s", c.ID, s.ID)
			}
		}
	}
	// NPU aicore_freq chart should have aicore_freq.
	npuFreq := findChart(resp1.Charts, "npu_aicore_freq")
	if npuFreq == nil {
		t.Fatal("npu_aicore_freq chart not found")
	}
	if len(npuFreq.Series) != 1 {
		t.Errorf("expected 1 npu_aicore_freq series, got %d", len(npuFreq.Series))
	}
	// CPU power chart should have power metric.
	cpuPower := findChart(resp1.Charts, "cpu_power")
	if cpuPower == nil {
		t.Fatal("cpu_power chart not found")
	}
	if len(cpuPower.Series) != 1 {
		t.Errorf("expected 1 cpu_power series, got %d", len(cpuPower.Series))
	}

	// Second call with updated CPU times: derived metrics should appear.
	writeTestSnapshot(t, snapPath, []collector.Metric{
		collector.Metric{Component: "cpu", Name: "user_time", Value: 1100, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "nice_time", Value: 20, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "system_time", Value: 600, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "idle_time", Value: 8100, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "iowait_time", Value: 60, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "irq_time", Value: 30, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "softirq_time", Value: 40, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		collector.Metric{Component: "cpu", Name: "steal_time", Value: 10, Unit: "jiffies", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
	})
	resp2 := callAPI(t, h)
	cpuChart2 := findChart(resp2.Charts, "cpu_utilization")
	if cpuChart2 == nil {
		t.Fatal("cpu_utilization chart not found in second call")
	}
	if len(cpuChart2.Series) != 7 {
		t.Fatalf("second call: expected 7 cpu_util series, got %d", len(cpuChart2.Series))
	}
	// idle_util + non_idle_util should ≈ 100.
	var idleU, nonIdleU float64
	for _, s := range cpuChart2.Series {
		if s.ID == "idle_util" {
			idleU = s.Value
		}
		if s.ID == "non_idle_util" {
			nonIdleU = s.Value
		}
	}
	if abs(idleU+nonIdleU-100) > 0.1 {
		t.Errorf("idle + non_idle = %.2f, expected ≈100", idleU+nonIdleU)
	}
}

// TestHandleAPI503 verifies 503 when snapshot is missing.
func TestHandleAPI503(t *testing.T) {
	h := NewHandler("/nonexistent/path/snapshot.json")
	req := httptest.NewRequest("GET", "/api/dfee", nil)
	w := httptest.NewRecorder()
	h.handleAPI(w, req)
	if w.Code != 503 {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

// TestHandleIndex verifies the SPA shell is served.
func TestHandleIndex(t *testing.T) {
	h := NewHandler("/dev/null")
	req := httptest.NewRequest("GET", "/dfee/", nil)
	w := httptest.NewRecorder()
	h.handleIndex(w, req)
	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("expected text/html, got %q", ct)
	}
}

// ---- helpers ----

func callAPI(t *testing.T, h *Handler) EfficiencyResponse {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/dfee", nil)
	w := httptest.NewRecorder()
	h.handleAPI(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp EfficiencyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func findChart(charts []chartData, id string) *chartData {
	for i := range charts {
		if charts[i].ID == id {
			return &charts[i]
		}
	}
	return nil
}
