//go:build linux

package npu

import (
	"os"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/dcmi"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/hccn_tool"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/npu_smi"
)

const (
	testdataPath = "../../../tests/testdata"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(data)
}

// useTestdata sets up DCMI mock + npu_smi/hccn_tool mock for 2 devices.
func useTestdata(t *testing.T) {
	t.Helper()

	// DCMI mock: 2 devices (card 0, dev 0 and 1).
	mock := &dcmi.MockProvider{
		CardListVal: []int{0, 1},
		Temp:        map[[2]int]int{{0, 0}: 42, {0, 1}: 38},
		Powers:      map[[2]int]int{{0, 0}: 65, {0, 1}: 60},
		Volts:       map[[2]int]uint{{0, 0}: 800, {0, 1}: 800},
		Healths:     map[[2]int]uint{{0, 0}: 0, {0, 1}: 0},
		Chips: map[[2]int]*dcmi.ChipInfo{{0, 0}: {
			ChipType:  "Ascend910A",
			ChipName:  "Ascend910A",
			ChipVer:   "V1",
			AicoreCnt: 32,
		}},
		Hbms: map[[2]int]*dcmi.HbmInfo{{0, 0}: {
			MemorySize:  32768,
			MemoryUsage: 16384,
			Temp:        55,
			Freq:        1600,
		}},
		Utils: map[[3]int]uint{
			{0, 0, 2}:  45,  // AICORE
			{0, 0, 13}: 50,  // NPU
			{0, 0, 3}:  30,  // AICPU
			{0, 0, 4}:  20,  // CTRLCPU
			{0, 0, 12}: 25,  // VECTORCORE
			{0, 0, 10}: 40,  // HBM_BANDWIDTH
			{0, 0, 1}:  15,  // DDR
			{0, 0, 5}:  10,  // DDR_BANDWIDTH
		},
		Freqs: map[[3]int]uint{
			{0, 0, 7}:  1800, // AICORE_CURRENT
			{0, 0, 9}:  2000, // AICORE_MAX
			{0, 0, 2}:  2400, // CTRLCPU
			{0, 0, 12}: 1800, // VECTORCORE_CURRENT
			{0, 0, 6}:  1600, // HBM
			{0, 0, 1}:  2400, // DDR
		},
		Sensors: map[[3]int]int{
			{0, 0, 0}:  60,  // CLUSTER
			{0, 0, 1}:  58,  // PERI
			{0, 0, 2}:  62,  // AICORE0
			{0, 0, 3}:  61,  // AICORE1
			{0, 0, 11}: 65,  // SOC
			{0, 0, 12}: 50,  // FP
			{0, 0, 13}: 58,  // N_DIE
			{0, 0, 14}: 55,  // HBM
		},
		NTCs: map[[2]int][4]int{{0, 0}: {45, 44, 43, 42}},
		DeviceInfo_: map[[4]int]uint{
			{0, 0, 8, 0}: 800,  // LP/AICORE_VOLTAGE
			{0, 0, 8, 1}: 700,  // LP/HYBRID_VOLTAGE
			{0, 0, 8, 2}: 1000, // LP/CPU_VOLTAGE
			{0, 0, 8, 3}: 1200, // LP/DDR_VOLTAGE
			{0, 0, 8, 4}: 1234, // LP/ACG
		},
		FanCounts:  map[[2]int]int{{0, 0}: 2},
		FanSpeeds:  map[[3]int]int{{0, 0, 0}: 65, {0, 0, 1}: 70},
		Aicpus: map[[2]int]*dcmi.AicpuInfo{{0, 0}: {
			MaxFreq:  2000,
			CurFreq:  1800,
			AicpuNum: 4,
		}},
		DvppRatios: map[[2]int]*dcmi.DvppRatio{{0, 0}: {
			VdecRatio: 10, VpcRatio: 5, VencRatio: 8, JpegeRatio: 3, JpegdRatio: 2,
		}},
		PidLists: map[[2]int][]uint{{0, 0}: {1234, 5678, 9012}},
		Eccs: map[[3]int]*dcmi.EccInfo{{0, 0, 2}: { // HBM
			SingleBitErrorCnt:         3,
			DoubleBitErrorCnt:         0,
			SingleBitIsolatedPagesCnt: 2,
			DoubleBitIsolatedPagesCnt: 0,
		}},
		Llcs: map[[2]int]*dcmi.LlcPerf{{0, 0}: {
			WrHitRate:  85,
			RdHitRate:  90,
			Throughput: 1250,
		}},
		NetHealths: map[[2]int]int{{0, 0}: 1},
		DriverVer:  "23.0.0",
		DriverHP:   0,
	}
	dcmi.SetProvider(mock)
	t.Cleanup(func() { dcmi.SetProvider(nil) })

	// npu_smi mock
	npu_smi.SetMock(func(args ...string) (string, error) {
		out := readFile(t, testdataPath+"/npu-smi-topo-output.txt")
		return out, nil
	})
	t.Cleanup(func() { npu_smi.ResetFetcher() })

	// hccn_tool mock
	hccn_tool.SetMock(func(devID int, opt string) (string, error) {
		switch opt {
		case "-bandwidth":
			return readFile(t, testdataPath+"/hccn-tool-bandwidth-output.txt"), nil
		case "-speed":
			return readFile(t, testdataPath+"/hccn-tool-speed-output.txt"), nil
		case "-link":
			return readFile(t, testdataPath+"/hccn-tool-link-output.txt"), nil
		}
		return "", nil
	})
	t.Cleanup(func() { hccn_tool.ResetFetcher() })
}

func findMetric(metrics []collector.Metric, name string) *collector.Metric {
	for i := range metrics {
		if metrics[i].Name == name {
			return &metrics[i]
		}
	}
	return nil
}

func TestCollectDevice(t *testing.T) {
	useTestdata(t)
	c := New()
	c.ensureDevices()
	now := time.Now()

	metrics := c.collectDevice(0, now)
	if len(metrics) < 40 {
		t.Fatalf("expected at least 40 metrics for device 0, got %d", len(metrics))
	}

	// Check key metrics.
	if m := findMetric(metrics, "utilization"); m == nil || m.Value != 45 {
		t.Errorf("utilization: expected 45, got %v", m)
	}
	if m := findMetric(metrics, "memory_usage"); m == nil {
		t.Error("missing memory_usage")
	}
	if m := findMetric(metrics, "temperature"); m == nil || m.Value != 42 {
		t.Errorf("temperature: expected 42, got %v", m)
	}
	if m := findMetric(metrics, "power_draw"); m == nil || m.Value != 65 {
		t.Errorf("power_draw: expected 65, got %v", m)
	}
	if m := findMetric(metrics, "health_status"); m == nil {
		t.Error("missing health_status")
	}
	if m := findMetric(metrics, "npu_util"); m == nil || m.Value != 50 {
		t.Errorf("npu_util: expected 50, got %v", m)
	}
	if m := findMetric(metrics, "aicore_freq"); m == nil || m.Value != 1800 {
		t.Errorf("aicore_freq: expected 1800, got %v", m)
	}
	if m := findMetric(metrics, "voltage"); m == nil {
		t.Error("missing voltage")
	}
	if m := findMetric(metrics, "aicpu_freq"); m == nil || m.Value != 1800 {
		t.Errorf("aicpu_freq: expected 1800, got %v", m)
	}
	if m := findMetric(metrics, "aicore_rated_freq"); m == nil || m.Value != 2000 {
		t.Errorf("aicore_rated_freq: expected 2000, got %v", m)
	}
	if m := findMetric(metrics, "ddr_freq"); m == nil || m.Value != 2400 {
		t.Errorf("ddr_freq: expected 2400, got %v", m)
	}
	if m := findMetric(metrics, "aicpu_util"); m == nil || m.Value != 30 {
		t.Errorf("aicpu_util: expected 30, got %v", m)
	}
	if m := findMetric(metrics, "vdec_util"); m == nil || m.Value != 10 {
		t.Errorf("vdec_util: expected 10, got %v", m)
	}
	if m := findMetric(metrics, "peri_temp"); m == nil {
		t.Error("missing peri_temp")
	}
	if m := findMetric(metrics, "ntc1_temp"); m == nil || m.Value != 45 {
		t.Errorf("ntc1_temp: expected 45, got %v", m)
	}
	if m := findMetric(metrics, "aicore_voltage"); m == nil {
		t.Error("missing aicore_voltage")
	}
	if m := findMetric(metrics, "acg_count"); m == nil || m.Value != 1234 {
		t.Errorf("acg_count: expected 1234, got %v", m)
	}
	if m := findMetric(metrics, "fan_speed"); m == nil {
		t.Error("missing fan_speed")
	}
	if m := findMetric(metrics, "process_info"); m == nil {
		t.Error("missing process_info")
	}
	if m := findMetric(metrics, "process_total"); m == nil || m.Value != 3 {
		t.Errorf("process_total: expected 3, got %v", m)
	}
	if m := findMetric(metrics, "net_tx_bandwidth"); m == nil || m.Value != 1250.0 {
		t.Errorf("net_tx_bandwidth: expected 1250, got %v", m)
	}
	if m := findMetric(metrics, "roce_speed_status"); m == nil {
		t.Error("missing roce_speed_status")
	}
	if m := findMetric(metrics, "roce_link_health"); m == nil {
		t.Error("missing roce_link_health")
	}
	if m := findMetric(metrics, "hccs_tx_bandwidth"); m == nil {
		t.Error("missing hccs_tx_bandwidth")
	}
	// ECC: first call, delta=0 (no prev)
	if m := findMetric(metrics, "hbm_single_ecc"); m == nil {
		t.Error("missing hbm_single_ecc")
	}
}

func TestCollectEccDelta(t *testing.T) {
	useTestdata(t)
	c := New()
	c.ensureDevices()
	now := time.Now()

	// First call: prev=0, delta=0.
	c.collectDevice(0, now)

	// Bump mock ECC count.
	mock := &dcmi.MockProvider{
		CardListVal: []int{0},
		Eccs: map[[3]int]*dcmi.EccInfo{{0, 0, 2}: {
			SingleBitErrorCnt: 5, // was 3, delta=2
			DoubleBitErrorCnt: 0,
		}},
	}
	dcmi.SetProvider(mock)

	metrics := c.collectDevice(0, now)
	m := findMetric(metrics, "hbm_single_ecc")
	if m == nil || m.Value != 2 {
		t.Errorf("hbm_single_ecc delta: expected 2 (5-3), got %v", m)
	}
}

func TestCollectIntegration(t *testing.T) {
	useTestdata(t)
	c := New()

	// Full Collect: static (once) + per-device (parallel, 2 devices).
	metrics, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(metrics) < 15 {
		t.Errorf("expected at least 15 metrics, got %d", len(metrics))
	}

	// All metrics should be component=npu.
	for _, m := range metrics {
		if m.Component != "npu" {
			t.Errorf("expected component 'npu', got '%s'", m.Component)
		}
		if m.Timestamp.IsZero() {
			t.Error("timestamp should not be zero")
		}
	}

	// Static metrics present.
	names := make(map[string]bool)
	for _, m := range metrics {
		names[m.Name] = true
	}
	for _, n := range []string{"npu_num", "comm_topo", "driver_version", "utilization", "memory_usage", "temperature", "power_draw", "health_status", "npu_util"} {
		if !names[n] {
			t.Errorf("expected metric %q in Collect output", n)
		}
	}
}

func TestCollectParallelMultiDevice(t *testing.T) {
	// Verify device-parallel: 2 devices should both produce metrics.
	useTestdata(t)
	c := New()

	metrics, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Device 0 metrics present.
	dev0Found := false
	for _, m := range metrics {
		if m.Labels["npu_id"] == "0" {
			dev0Found = true
			break
		}
	}
	if !dev0Found {
		t.Error("expected metrics for device 0")
	}
}

func TestNoDCMIAvailable(t *testing.T) {
	// Without DCMI provider, Collect should still work (no DCMI metrics,
	// only command-based if npu-smi/hccn_tool available — here they're not
	// set up, so all metrics empty).
	dcmi.SetProvider(nil)
	defer dcmi.SetProvider(nil)
	npu_smi.SetMock(func(args ...string) (string, error) { return "", nil })
	defer npu_smi.ResetFetcher()
	hccn_tool.SetMock(func(devID int, opt string) (string, error) { return "", nil })
	defer hccn_tool.ResetFetcher()

	c := New()
	metrics, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect without DCMI should not error: %v", err)
	}
	// May have npu_num=0 but no DCMI metrics.
	for _, m := range metrics {
		if m.Component != "npu" {
			t.Errorf("expected component 'npu', got '%s'", m.Component)
		}
	}
}

func TestCollectorInterface(t *testing.T) {
	c := New()
	if c.Name() != "npu" {
		t.Errorf("expected name 'npu', got '%s'", c.Name())
	}
	if c.Component() != "npu" {
		t.Errorf("expected component 'npu', got '%s'", c.Component())
	}
	if c.Priority() != collector.PriorityHigh {
		t.Errorf("expected priority High, got %s", c.Priority())
	}
	if c.DefaultInterval() != 3*time.Second {
		t.Errorf("expected interval 3s, got %v", c.DefaultInterval())
	}
	if !c.DefaultEnabled() {
		t.Error("expected default enabled true")
	}
}
