package stragglerout

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func mkMetric(comp, name string, val float64, labels map[string]string) collector.Metric {
	return collector.Metric{
		Component: comp, Name: name, Value: val, Unit: "",
		Labels: labels, Timestamp: time.Unix(1784547926, 0),
	}
}

func TestExtractNPUMetrics(t *testing.T) {
	m := NewKPIMapper()
	sample := m.Extract([]collector.Metric{
		mkMetric("npu", "temperature", 47, map[string]string{"npu_id": "0"}),
		mkMetric("npu", "power_draw", 1628, map[string]string{"npu_id": "0"}),
		mkMetric("npu", "aicore_freq", 1800, map[string]string{"npu_id": "0"}),
		mkMetric("npu", "utilization", 45, map[string]string{"npu_id": "0"}),
		mkMetric("npu", "memory_usage", 50, map[string]string{"npu_id": "0"}),
		mkMetric("npu", "net_tx_bandwidth", 1250, map[string]string{"npu_id": "0"}),
		mkMetric("npu", "mac_rx_pfc_pkt_num", 0, map[string]string{"npu_id": "0"}),
		mkMetric("npu", "roce_tx_err_pkt_num", 0, map[string]string{"npu_id": "0"}),
		mkMetric("npu", "roce_out_of_order_num", 0, map[string]string{"npu_id": "0"}),
		mkMetric("npu", "roce_new_pkt_rty", 0, map[string]string{"npu_id": "0"}),
		// same metrics for card 1
		mkMetric("npu", "temperature", 50, map[string]string{"npu_id": "1"}),
		mkMetric("npu", "power_draw", 1700, map[string]string{"npu_id": "1"}),
		// a non-KPI npu metric (must be ignored)
		mkMetric("npu", "fan_speed", 65, map[string]string{"npu_id": "0"}),
	})
	if sample == nil {
		t.Fatal("expected non-nil sample")
	}
	if len(sample.Vals) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(sample.Vals))
	}
	c0 := sample.Vals["0"]
	if c0["temp"] != 47 || c0["power"] != 1628 || c0["aicore_freq"] != 1800 {
		t.Errorf("card 0 vals wrong: %+v", c0)
	}
	if c0["aicore_util"] != 45 || c0["hbm_util"] != 50 || c0["tx_bandwidth"] != 1250 {
		t.Errorf("card 0 mapped fields wrong: %+v", c0)
	}
	if c0["rx_pfc_pkt"] != 0 || c0["roce_tx_err_pkt"] != 0 || c0["roce_out_of_order"] != 0 || c0["roce_new_pkt_rty"] != 0 {
		t.Errorf("card 0 counter fields wrong: %+v", c0)
	}
	// non-KPI npu metric (fan_speed) must be ignored — not present in the sample
	if _, ok := c0["fan_speed"]; ok {
		t.Errorf("non-KPI metric leaked into sample: %+v", c0)
	}
	if sample.Vals["1"]["temp"] != 50 {
		t.Errorf("card 1 temp: %v", sample.Vals["1"]["temp"])
	}
	if sample.Timestamp != 1784547926 {
		t.Errorf("timestamp: %d", sample.Timestamp)
	}
}

func TestExtractRocENewPktRtyAlias(t *testing.T) {
	// If hccn_tool emits "roce_retrans_pkt_num" instead of "roce_new_pkt_rty",
	// the mapper must still map it to the straggler field.
	m := NewKPIMapper()
	sample := m.Extract([]collector.Metric{
		mkMetric("npu", "roce_retrans_pkt_num", 7, map[string]string{"npu_id": "2"}),
	})
	if sample == nil {
		t.Fatal("expected non-nil sample")
	}
	if sample.Vals["2"]["roce_new_pkt_rty"] != 7 {
		t.Errorf("alias not mapped: %+v", sample.Vals["2"])
	}
}

func TestExtractCPUMetrics(t *testing.T) {
	m := NewKPIMapper()
	sample := m.Extract([]collector.Metric{
		mkMetric("cpu", "usage", 4.26, map[string]string{"cpu": "cpu1"}),
		mkMetric("cpu", "usage", 3.41, map[string]string{"cpu": "cpu2"}),
		mkMetric("cpu", "usage", 99, map[string]string{"core": "total"}), // ignored
	})
	if sample == nil {
		t.Fatal("expected non-nil sample")
	}
	if sample.CPUAvg["cpu1"] != "4.26" || sample.CPUAvg["cpu2"] != "3.41" {
		t.Errorf("cpu_avg wrong: %+v", sample.CPUAvg)
	}
	if _, ok := sample.CPUAvg["total"]; ok {
		t.Errorf("total should be ignored: %+v", sample.CPUAvg)
	}
}

func TestExtractIgnoresIrrelevantBatch(t *testing.T) {
	m := NewKPIMapper()
	sample := m.Extract([]collector.Metric{
		mkMetric("disk", "iops", 100, nil),
		mkMetric("memory", "usage", 70, nil),
	})
	if sample != nil {
		t.Fatalf("non-KPI batch should produce nil sample, got %+v", sample)
	}
}

func TestExtractDualChipPerDevice(t *testing.T) {
	// A3 dual-chip: one card (npu_id=0) with two chips (chip_id 0/1). The
	// mapper must derive per-chip global device ids from the labels alone
	// (device_id = npu_id*2 + chip_id) so both chips survive as separate vals
	// entries (no last-write-wins collapse).
	m := NewKPIMapper()
	sample := m.Extract([]collector.Metric{
		mkMetric("npu", "temperature", 47, map[string]string{"npu_id": "0", "chip_id": "0"}),
		mkMetric("npu", "power_draw", 1628, map[string]string{"npu_id": "0", "chip_id": "0"}),
		mkMetric("npu", "temperature", 52, map[string]string{"npu_id": "0", "chip_id": "1"}),
		mkMetric("npu", "power_draw", 2051, map[string]string{"npu_id": "0", "chip_id": "1"}),
		mkMetric("npu", "temperature", 50, map[string]string{"npu_id": "1", "chip_id": "0"}),
	})
	if sample == nil {
		t.Fatal("expected non-nil sample")
	}
	if len(sample.Vals) != 3 {
		t.Fatalf("expected 3 per-chip entries (2 chips of card 0 + 1 chip of card 1), got %d: %+v", len(sample.Vals), sample.Vals)
	}
	if v := sample.Vals["0"]; v == nil || v["temp"] != 47 || v["power"] != 1628 {
		t.Errorf("chip device 0 (card 0 chip 0) wrong: %+v", v)
	}
	if v := sample.Vals["1"]; v == nil || v["temp"] != 52 || v["power"] != 2051 {
		t.Errorf("chip device 1 (card 0 chip 1) wrong: %+v", v)
	}
	if v := sample.Vals["2"]; v == nil || v["temp"] != 50 {
		t.Errorf("chip device 2 (card 1 chip 0) wrong: %+v", v)
	}
}

func TestExtractDualChipKeyStableWhenCardDrops(t *testing.T) {
	// The chip stride must not shrink when a later batch misses chips (card
	// drop): device_id for card 2 chip 0 stays 4 (2*2+0), not 2.
	m := NewKPIMapper()
	// First batch: full dual-chip cards 0 and 2 → stride learned as 2.
	m.Extract([]collector.Metric{
		mkMetric("npu", "temperature", 47, map[string]string{"npu_id": "0", "chip_id": "0"}),
		mkMetric("npu", "temperature", 52, map[string]string{"npu_id": "0", "chip_id": "1"}),
		mkMetric("npu", "temperature", 50, map[string]string{"npu_id": "2", "chip_id": "0"}),
		mkMetric("npu", "temperature", 53, map[string]string{"npu_id": "2", "chip_id": "1"}),
	})
	// Second batch: card 0 dropped; only card 2 chip 0 remains. Key must
	// still be 4 (stable slot), not 2.
	sample := m.Extract([]collector.Metric{
		mkMetric("npu", "temperature", 50, map[string]string{"npu_id": "2", "chip_id": "0"}),
	})
	if sample == nil || sample.Vals["4"] == nil || sample.Vals["4"]["temp"] != 50 {
		t.Errorf("card 2 chip 0 must stay keyed as 4 after card 0 drops, got %+v", sample.Vals)
	}
}

func TestExtractFallsBackToNpuID(t *testing.T) {
	// Card-level metrics without chip_id (e.g. hccn_tool's net_tx_bandwidth)
	// must keep npu_id as the key — base behavior preserved.
	m := NewKPIMapper()
	sample := m.Extract([]collector.Metric{
		mkMetric("npu", "temperature", 47, map[string]string{"npu_id": "3"}),
		mkMetric("npu", "power_draw", 1600, map[string]string{"npu_id": "3"}),
	})
	if sample == nil {
		t.Fatal("expected non-nil sample")
	}
	if len(sample.Vals) != 1 {
		t.Fatalf("expected 1 entry keyed by npu_id, got %d: %+v", len(sample.Vals), sample.Vals)
	}
	if v := sample.Vals["3"]; v == nil || v["temp"] != 47 || v["power"] != 1600 {
		t.Errorf("npu_id fallback entry wrong: %+v", v)
	}
}

func TestKPIWriterAppendAndPrune(t *testing.T) {
	dir := t.TempDir()
	w := NewKPIWriter(dir, 1*time.Hour, nil)

	// Today's sample.
	now := time.Now()
	sample := &KPISample{Timestamp: now.Unix(), Vals: map[string]map[string]float64{
		"0": {"temp": 47},
	}}
	if err := w.Append(sample); err != nil {
		t.Fatalf("append: %v", err)
	}
	// A sample from 2 days ago (older than retention=1h).
	old := &KPISample{Timestamp: now.Add(-48 * time.Hour).Unix(), Vals: map[string]map[string]float64{
		"0": {"temp": 40},
	}}
	if err := w.Append(old); err != nil {
		t.Fatalf("append old: %v", err)
	}
	// Prune: the old file should be removed.
	w.Prune(now)
	// Today's file must remain; old file removed.
	entries, _ := os.ReadDir(dir)
	remaining := map[string]bool{}
	for _, e := range entries {
		remaining[e.Name()] = true
	}
	if !remaining["straggler_kpi_"+now.Local().Format("2006-01-02")+".jsonl"] {
		t.Errorf("today's file pruned: %v", remaining)
	}
}

func TestStragglerStorageDelegatesAndTaps(t *testing.T) {
	dir := t.TempDir()
	w := NewKPIWriter(dir, 1*time.Hour, nil)
	m := NewKPIMapper()
	mock := &mockInner{}
	ss := NewStragglerStorage(mock, m, w, 0, nil) // flushInterval 0 → flush every write

	// non-KPI batch: no sample, inner still written.
	ss.Write([]collector.Metric{mkMetric("disk", "iops", 100, nil)})
	if len(mock.written) != 1 {
		t.Fatalf("inner should get 1 write, got %d", len(mock.written))
	}

	// KPI batch: sample buffered+flushed (interval 0).
	ss.Write([]collector.Metric{
		mkMetric("npu", "temperature", 47, map[string]string{"npu_id": "0"}),
	})
	ss.Flush(time.Now())
	// File should contain the KPI sample.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 kpi file, got %d", len(entries))
	}
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if len(data) == 0 {
		t.Fatal("kpi file empty")
	}
}

type mockInner struct {
	written [][]collector.Metric
}

func (m *mockInner) Write(metrics []collector.Metric) error {
	cp := make([]collector.Metric, len(metrics))
	copy(cp, metrics)
	m.written = append(m.written, cp)
	return nil
}
