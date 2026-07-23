package hccn_tool

import (
	"os"
	"strconv"
	"testing"
)

func readMock(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(data)
}

func TestBandwidth(t *testing.T) {
	out := readMock(t, "../../../tests/testdata/hccn-tool-bandwidth-output.txt")
	SetMock(func(devID int, opt string) (string, error) { return out, nil })
	defer ResetFetcher()

	bw, err := Default().Bandwidth(0)
	if err != nil {
		t.Fatalf("Bandwidth failed: %v", err)
	}
	if bw.NetTX != 1250.0 {
		t.Errorf("NetTX: expected 1250, got %v", bw.NetTX)
	}
	if bw.NetRX != 980.0 {
		t.Errorf("NetRX: expected 980, got %v", bw.NetRX)
	}
	if bw.PcieTX != 2500.0 {
		t.Errorf("PcieTX: expected 2500, got %v", bw.PcieTX)
	}
	if bw.PcieRX != 2100.0 {
		t.Errorf("PcieRX: expected 2100, got %v", bw.PcieRX)
	}
}

func TestSpeed(t *testing.T) {
	out := readMock(t, "../../../tests/testdata/hccn-tool-speed-output.txt")
	SetMock(func(devID int, opt string) (string, error) { return out, nil })
	defer ResetFetcher()

	speed, err := Default().Speed(0)
	if err != nil {
		t.Fatalf("Speed failed: %v", err)
	}
	if speed != "100Gbps" {
		t.Errorf("Speed: expected '100Gbps', got %q", speed)
	}
}

func TestLink(t *testing.T) {
	out := readMock(t, "../../../tests/testdata/hccn-tool-link-output.txt")
	SetMock(func(devID int, opt string) (string, error) { return out, nil })
	defer ResetFetcher()

	link, err := Default().Link(0)
	if err != nil {
		t.Fatalf("Link failed: %v", err)
	}
	if link != "ACTIVE" {
		t.Errorf("Link: expected 'ACTIVE', got %q", link)
	}
}

func TestStatistics(t *testing.T) {
	out := readMock(t, "../../../tests/testdata/hccn-tool-stat-output.txt")
	SetMock(func(devID int, opt string) (string, error) { return out, nil })
	defer ResetFetcher()

	stats, err := Default().Statistics(2)
	if err != nil {
		t.Fatalf("Statistics failed: %v", err)
	}
	// Should have 45 metrics.
	if len(stats) != 45 {
		t.Fatalf("expected 45 statistics, got %d", len(stats))
	}
	// Verify specific values.
	if stats["mac_tx_total_pkt_num"] != 12345 {
		t.Errorf("mac_tx_total_pkt_num: expected 12345, got %d", stats["mac_tx_total_pkt_num"])
	}
	if stats["mac_rx_total_oct_num"] != 54321098 {
		t.Errorf("mac_rx_total_oct_num: expected 54321098, got %d", stats["mac_rx_total_oct_num"])
	}
	if stats["roce_cqe_num"] != 5000 {
		t.Errorf("roce_cqe_num: expected 5000, got %d", stats["roce_cqe_num"])
	}
	if stats["nic_tx_all_oct_num"] != 12345678 {
		t.Errorf("nic_tx_all_oct_num: expected 12345678, got %d", stats["nic_tx_all_oct_num"])
	}
	// Verify PFC priority metrics.
	for i := 0; i <= 7; i++ {
		key := "mac_tx_pfc_pri" + strconv.Itoa(i) + "_pkt_num"
		if _, ok := stats[key]; !ok {
			t.Errorf("missing %s", key)
		}
	}
}
