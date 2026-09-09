package main

import (
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func netMetric(name string, value float64, iface string) collector.Metric {
	return collector.Metric{
		Component: "network", Name: name, Value: value,
		Labels: map[string]string{"interface": iface},
	}
}

// First call (hasPrev=false): all deltas are zero, but prev is captured.
func TestDeriveNetworkDeltaFirstCall(t *testing.T) {
	in := []collector.Metric{
		netMetric("rx_bytes_total", 1000, "eth0"),
		netMetric("tx_bytes_total", 2000, "eth0"),
		netMetric("rx_bytes_total", 500, "eth1"),
		{Component: "cpu", Name: "usage", Value: 42}, // not network: untouched
	}
	out, prev := deriveNetworkDelta(in, nil, false)
	for _, m := range out[:3] {
		if m.Value != 0 {
			t.Errorf("%s/%s: first call delta = %v, want 0", m.Labels["interface"], m.Name, m.Value)
		}
	}
	if len(prev) != 3 {
		t.Errorf("prev captured %d series, want 3: %v", len(prev), prev)
	}
	if out[3].Value != 42 {
		t.Errorf("non-network metric must be untouched, got %v", out[3].Value)
	}
}

// Second call: values are replaced by the delta since prev.
func TestDeriveNetworkDeltaSecondCall(t *testing.T) {
	first := []collector.Metric{
		netMetric("rx_bytes_total", 1000, "eth0"),
		netMetric("tx_bytes_total", 2000, "eth0"),
	}
	_, prev := deriveNetworkDelta(first, nil, false)

	second := []collector.Metric{
		netMetric("rx_bytes_total", 1500, "eth0"), // delta 500
		netMetric("tx_bytes_total", 2600, "eth0"), // delta 600
	}
	out, newPrev := deriveNetworkDelta(second, prev, true)
	if out[0].Value != 500 {
		t.Errorf("rx delta = %v, want 500", out[0].Value)
	}
	if out[1].Value != 600 {
		t.Errorf("tx delta = %v, want 600", out[1].Value)
	}
	if len(newPrev) != 2 {
		t.Errorf("new prev = %v, want 2 series", newPrev)
	}
}

// Counter reset (curr < prev) clamps the delta to zero.
func TestDeriveNetworkDeltaCounterReset(t *testing.T) {
	first := []collector.Metric{netMetric("rx_bytes_total", 5000, "eth0")}
	_, prev := deriveNetworkDelta(first, nil, false)

	second := []collector.Metric{netMetric("rx_bytes_total", 100, "eth0")} // reboot/reset
	out, _ := deriveNetworkDelta(second, prev, true)
	if out[0].Value != 0 {
		t.Errorf("counter reset delta = %v, want 0 (clamped)", out[0].Value)
	}
}

// hasPrev=true but the series was not in prev (new interface): delta 0.
func TestDeriveNetworkDeltaNewSeriesMidStream(t *testing.T) {
	first := []collector.Metric{netMetric("rx_bytes_total", 100, "eth0")}
	_, prev := deriveNetworkDelta(first, nil, false)

	second := []collector.Metric{
		netMetric("rx_bytes_total", 200, "eth0"), // known: delta 100
		netMetric("rx_bytes_total", 900, "eth1"), // unknown series: delta 0
	}
	out, _ := deriveNetworkDelta(second, prev, true)
	if out[0].Value != 100 {
		t.Errorf("known series delta = %v, want 100", out[0].Value)
	}
	if out[1].Value != 0 {
		t.Errorf("unknown series delta = %v, want 0", out[1].Value)
	}
}

// Non-rx/tx network metrics are ignored entirely (no prev entries, untouched).
func TestDeriveNetworkDeltaIgnoresOtherNetworkMetrics(t *testing.T) {
	in := []collector.Metric{
		netMetric("interface_status", 1, "eth0"),
		netMetric("rx_bytes_total", 100, "eth0"),
	}
	out, prev := deriveNetworkDelta(in, nil, false)
	if len(prev) != 1 {
		t.Errorf("prev = %v, want only the rx series", prev)
	}
	if out[0].Value != 1 {
		t.Errorf("interface_status must be untouched, got %v", out[0].Value)
	}
}
