package network

import (
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func TestParseNetDev(t *testing.T) {
	stats, err := parseNetDev("../../../tests/testdata/proc")
	if err != nil {
		t.Fatalf("parseNetDev failed: %v", err)
	}

	if len(stats) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(stats))
	}

	eth0, ok := stats["eth0"]
	if !ok {
		t.Fatal("missing 'eth0' interface")
	}
	if eth0.rxBytes != 5000000 {
		t.Errorf("expected rxBytes 5000000, got %d", eth0.rxBytes)
	}
	if eth0.txBytes != 3000000 {
		t.Errorf("expected txBytes 3000000, got %d", eth0.txBytes)
	}
	if eth0.rxErrs != 2 {
		t.Errorf("expected rxErrs 2, got %d", eth0.rxErrs)
	}
	if eth0.rxDrop != 1 {
		t.Errorf("expected rxDrop 1, got %d", eth0.rxDrop)
	}
}

func TestCollectIntegration(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")

	// First call - total bytes and error_count but no throughput/packet_count
	metrics, err := c.Collect()
	if err != nil {
		t.Fatalf("first Collect failed: %v", err)
	}

	// eth0: 2 total + 4 error_count = 6 metrics (no throughput/packet on first call)
	eth0Count := 0
	for _, m := range metrics {
		if m.Labels["interface"] == "eth0" {
			eth0Count++
		}
	}
	if eth0Count < 6 {
		t.Errorf("expected at least 6 eth0 metrics on first call, got %d", eth0Count)
	}

	// Verify error_count values
	for _, m := range metrics {
		if m.Name == "error_count" && m.Labels["interface"] == "eth0" {
			switch m.Labels["type"] {
			case "rx_err":
				if m.Value != 2 {
					t.Errorf("expected rx_err=2, got %.0f", m.Value)
				}
			case "rx_drop":
				if m.Value != 1 {
					t.Errorf("expected rx_drop=1, got %.0f", m.Value)
				}
			}
		}
	}

	// Second call - should have throughput and packet_count
	metrics2, err := c.Collect()
	if err != nil {
		t.Fatalf("second Collect failed: %v", err)
	}

	hasThroughput := false
	hasPacketCount := false
	for _, m := range metrics2 {
		if m.Name == "throughput" && m.Labels["interface"] == "eth0" {
			hasThroughput = true
		}
		if m.Name == "packet_count" && m.Labels["interface"] == "eth0" {
			hasPacketCount = true
		}
	}
	if !hasThroughput {
		t.Error("expected throughput metrics on second call")
	}
	if !hasPacketCount {
		t.Error("expected packet_count metrics on second call")
	}

	// Verify lo is filtered out
	for _, m := range metrics2 {
		if m.Labels["interface"] == "lo" {
			t.Error("lo interface should be filtered out")
		}
	}
}

func TestCollectInterfaceStatus(t *testing.T) {
	c := New()
	c.SetSysPath("../../../tests/testdata/sys")

	now := time.Now()
	metrics, err := c.collectInterfaceStatus(now)
	if err != nil {
		t.Fatalf("collectInterfaceStatus failed: %v", err)
	}

	// Should have eth0 (up), lo is filtered
	foundEth0 := false
	for _, m := range metrics {
		if m.Labels["interface"] == "eth0" {
			foundEth0 = true
			if m.Value != 1 {
				t.Errorf("expected eth0 status=1 (up), got %.0f", m.Value)
			}
			if m.Labels["status"] != "up" {
				t.Errorf("expected status 'up', got '%s'", m.Labels["status"])
			}
		}
		if m.Labels["interface"] == "lo" {
			t.Error("lo should be filtered from interface_status")
		}
	}
	if !foundEth0 {
		t.Error("expected eth0 interface_status metric")
	}
}

func TestCollectConnectionCount(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")
	now := time.Now()

	metrics, err := c.collectConnectionCount(now)
	if err != nil {
		t.Fatalf("collectConnectionCount failed: %v", err)
	}

	if len(metrics) == 0 {
		t.Fatal("expected at least 1 connection_count metric")
	}

	statesFound := map[string]bool{}
	for _, m := range metrics {
		if m.Name != "connection_count" {
			t.Errorf("expected name 'connection_count', got '%s'", m.Name)
		}
		statesFound[m.Labels["state"]] = true
	}

	if !statesFound["LISTEN"] {
		t.Error("expected LISTEN state")
	}
	if !statesFound["ESTABLISHED"] {
		t.Error("expected ESTABLISHED state")
	}
	if !statesFound["TIME_WAIT"] {
		t.Error("expected TIME_WAIT state")
	}
}

func TestCollectorInterface(t *testing.T) {
	c := New()

	if c.Name() != "network" {
		t.Errorf("expected name 'network', got '%s'", c.Name())
	}
	if c.Component() != "network" {
		t.Errorf("expected component 'network', got '%s'", c.Component())
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

func TestParseUint(t *testing.T) {
	if v := parseUint("12345"); v != 12345 {
		t.Errorf("expected 12345, got %d", v)
	}
	if v := parseUint("invalid"); v != 0 {
		t.Errorf("expected 0 for invalid input, got %d", v)
	}
	if v := parseUint(""); v != 0 {
		t.Errorf("expected 0 for empty input, got %d", v)
	}
}
