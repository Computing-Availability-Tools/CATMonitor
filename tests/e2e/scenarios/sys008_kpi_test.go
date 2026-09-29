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

// TestSYS008KpiOutput covers SYS-008 (KPI 文件输出).
//
// | TC   | Priority | Description                                        |
// |------|----------|----------------------------------------------------|
// | TC-013| P0       | KPI 文件按日产出                                    |
// | TC-042| P1       | 全局设备号(A3双芯片/掉卡空洞)——真机场景,本机验证结构 |
func TestSYS008KpiOutput(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{StragglerOutputEnabled: true})
	d.WaitComponentSnapshot(t, "cpu", 60*time.Second)

	t.Run("TC-013_kpi_daily_file", func(t *testing.T) {
		path := filepath.Join(d.StragglerDir(), "straggler_kpi_"+time.Now().Local().Format("2006-01-02")+".jsonl")
		e2e.WaitFileExists(t, path, 20*time.Second)

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read KPI file: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) < 1 {
			t.Fatalf("KPI file has 0 lines")
		}

		// Each line must be valid JSON with the expected keys.
		var sample map[string]json.RawMessage
		if err := json.Unmarshal([]byte(lines[0]), &sample); err != nil {
			t.Fatalf("KPI first line not valid JSON: %v", err)
		}
		if _, ok := sample["ts"]; !ok {
			t.Error("KPI line missing 'ts' key")
		}
		// vals and cpu_avg are omitempty — on a no-NPU host vals may be
		// absent but cpu_avg should be present (cpu metrics always collected).
		if _, ok := sample["cpu_avg"]; !ok {
			t.Error("KPI line missing 'cpu_avg' key (cpu metrics always available)")
		}
	})

	t.Run("TC-042_device_numbering_structure", func(t *testing.T) {
		// On real NPU hardware (A3 dual-chip), vals keys must be 0..15.
		// On this host (no NPU), we verify the structural contract:
		// vals, when present, maps deviceID → metric → value.
		path := filepath.Join(d.StragglerDir(), "straggler_kpi_"+time.Now().Local().Format("2006-01-02")+".jsonl")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("KPI file not readable: %v (hardware-dependent)", err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		hasVals := false
		for _, line := range lines {
			var sample map[string]json.RawMessage
			if err := json.Unmarshal([]byte(line), &sample); err != nil {
				continue
			}
			if valsRaw, ok := sample["vals"]; ok {
				var vals map[string]map[string]float64
				if err := json.Unmarshal(valsRaw, &vals); err != nil {
					t.Errorf("KPI 'vals' is not deviceID→metric→value: %v", err)
					return
				}
				hasVals = true
				for devID, metrics := range vals {
					if devID == "" {
						t.Error("KPI vals has empty device ID key")
					}
					for metric, v := range metrics {
						if metric == "" {
							t.Errorf("KPI vals[%s] has empty metric key", devID)
						}
						// Counter values must be non-negative (raw cumulative).
						if v < 0 {
							t.Errorf("KPI vals[%s][%s] = %v (negative counter)", devID, metric, v)
						}
					}
				}
			}
		}
		if !hasVals {
			t.Log("no NPU vals on this host (no NPU hardware); structural check skipped — full A3 dual-chip key range (0..15) requires real hardware")
		}
	})
}
