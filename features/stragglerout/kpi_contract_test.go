package stragglerout

import (
	"encoding/json"
	"testing"
	"time"
)

// TestKPISampleJSONContract verifies the serialized KPISample format is
// parseable by the straggler slow-node detector's JSON reader (the
// TimeSeriesData consumer): field names (`ts`/`vals`/`cpu_avg`), the
// nested deviceID→metric→value mapping, and round-trip fidelity.
// A renamed JSON tag here would silently break the straggler CLI.
//
// Design ref: stragglerout Excel TC-025 (P0, JSON reader 重建-CSVRow 契约).
func TestKPISampleJSONContract(t *testing.T) {
	sample := &KPISample{
		Timestamp: time.Now().Unix(),
		Vals: map[string]map[string]float64{
			"0": {"temp": 47, "power": 1628, "aicore_freq": 1800, "aicore_util": 45,
				"hbm_util": 50, "tx_bandwidth": 1250, "rx_pfc_pkt": 0,
				"roce_tx_err_pkt": 0, "roce_out_of_order": 0, "roce_new_pkt_rty": 0},
			"1": {"temp": 52, "power": 2051, "hbm_util": 4.4},
		},
		CPUAvg: map[string]string{"cpu0": "4.26", "cpu1": "3.98"},
	}

	data, err := json.Marshal(sample)
	if err != nil {
		t.Fatalf("marshal KPISample: %v", err)
	}

	// Verify the raw JSON keys.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	for _, key := range []string{"ts", "vals", "cpu_avg"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("KPISample JSON missing key %q (straggler reader depends on it)", key)
		}
	}

	// Verify the nested vals structure: deviceID → metric → value.
	var vals map[string]map[string]float64
	if err := json.Unmarshal(raw["vals"], &vals); err != nil {
		t.Fatalf("unmarshal vals: %v", err)
	}
	if len(vals) != 2 {
		t.Errorf("vals has %d devices, want 2", len(vals))
	}
	dev0, ok := vals["0"]
	if !ok {
		t.Fatal("vals missing device '0'")
	}
	expectedFields := []string{"temp", "power", "aicore_freq", "aicore_util", "hbm_util",
		"tx_bandwidth", "rx_pfc_pkt", "roce_tx_err_pkt", "roce_out_of_order", "roce_new_pkt_rty"}
	for _, f := range expectedFields {
		if _, ok := dev0[f]; !ok {
			t.Errorf("vals['0'] missing KPI field %q (straggler CSVRow expects it)", f)
		}
	}
	if dev0["temp"] != 47 {
		t.Errorf("vals['0']['temp'] = %v, want 47", dev0["temp"])
	}

	// Verify cpu_avg structure.
	var cpuAvg map[string]string
	if err := json.Unmarshal(raw["cpu_avg"], &cpuAvg); err != nil {
		t.Fatalf("unmarshal cpu_avg: %v", err)
	}
	if cpuAvg["cpu0"] != "4.26" {
		t.Errorf("cpu_avg['cpu0'] = %q, want '4.26'", cpuAvg["cpu0"])
	}

	// Round-trip: the JSON line written to the KPI file must be
	// parseable as a complete KPISample (simulating the straggler reader).
	var line KPISample
	if err := json.Unmarshal(data, &line); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if line.Timestamp != sample.Timestamp {
		t.Errorf("round-trip ts: got %d, want %d", line.Timestamp, sample.Timestamp)
	}
	if line.Vals["0"]["power"] != 1628 {
		t.Errorf("round-trip vals['0']['power']: got %v, want 1628", line.Vals["0"]["power"])
	}
}
