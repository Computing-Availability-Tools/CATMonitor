package hccn_tool

import (
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
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
	if link != "UP" {
		t.Errorf("Link: expected 'UP', got %q", link)
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

// TestCachedDoesNotHoldLockAcrossFetch is a regression test for the lock
// scope: the mutex must NOT be held while fetch runs. Callers are
// device-parallel goroutines with distinct cache keys; if execs serialized
// on the lock, an N-device collection would take N×fetch instead of ~fetch.
// The mock fetch sleeps 100ms; 8 concurrent misses for 8 distinct devices
// must finish in well under 8×100ms.
func TestCachedDoesNotHoldLockAcrossFetch(t *testing.T) {
	SetMock(func(devID int, opt string) (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "out-" + strconv.Itoa(devID), nil
	})
	defer ResetFetcher()

	const devices = 8
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < devices; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if _, err := Default().Statistics(idx); err != nil {
				t.Errorf("Statistics(%d) failed: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	// Serialized: 8 × 100ms = 800ms. Parallel: ~100ms. Allow generous
	// headroom for CI jitter; anything ≥ 400ms means execs serialized.
	if elapsed >= 400*time.Millisecond {
		t.Errorf("concurrent fetches serialized: %d misses took %v (want <400ms, ideal ~100ms)", devices, elapsed)
	}

	// Cache must be populated: a second round of the same keys is all hits
	// and therefore near-instant even though the fetch still sleeps 100ms.
	start = time.Now()
	for i := 0; i < devices; i++ {
		if _, err := Default().Statistics(i); err != nil {
			t.Errorf("cached Statistics(%d) failed: %v", i, err)
		}
	}
	if cached := time.Since(start); cached >= 100*time.Millisecond {
		t.Errorf("cache hits blocked by fetch: 8 hits took %v (want <100ms)", cached)
	}
}
