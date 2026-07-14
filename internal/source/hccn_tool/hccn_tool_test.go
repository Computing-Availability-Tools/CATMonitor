package hccn_tool

import (
	"os"
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
