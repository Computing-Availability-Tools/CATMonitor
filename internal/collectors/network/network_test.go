//go:build linux

package network

import (
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/sys"
)

const (
	testdataProc = "../../../tests/testdata/proc"
	testdataSys  = "../../../tests/testdata/sys"
)

func useTestdata(t *testing.T) {
	t.Helper()
	proc.SetRoot(testdataProc)
	sys.SetRoot(testdataSys)
	t.Cleanup(func() {
		proc.SetRoot("/proc")
		sys.SetRoot("/sys")
	})
}

func TestCollectIntegration(t *testing.T) {
	useTestdata(t)
	c := New()

	// First call: total bytes and error_count but no throughput/packet_count.
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

	// Second call: should have throughput and packet_count.
	metrics2, err := c.Collect()
	if err != nil {
		t.Fatalf("second Collect failed: %v", err)
	}
	hasThroughput, hasPacketCount := false, false
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

	// lo should be filtered out.
	for _, m := range metrics2 {
		if m.Labels["interface"] == "lo" {
			t.Error("lo interface should be filtered out")
		}
	}
}

func TestCollectInterfaceStatus(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()

	metrics, err := c.collectInterfaceStatus(now)
	if err != nil {
		t.Fatalf("collectInterfaceStatus failed: %v", err)
	}
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
	useTestdata(t)
	c := New()
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
