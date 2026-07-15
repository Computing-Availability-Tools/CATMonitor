package chassis

import (
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/ipmi"
)

const mockSDR = `Inlet Temp        | 28.000     | degrees C  | ok
Outlet Temp       | 42.000     | degrees C  | ok
Power             | 1848.000   | Watts      | ok
CPU1 Temp         | 65.000     | degrees C  | ok
MEM1 Temp         | 42.000     | degrees C  | ok
MEM1 Pwr          | 12.500     | Watts      | na
CPU1 Pwr          | 125.500    | Watts      | na
FAN1 F Speed      | 9375.000   | RPM        | ok
FAN1 R Speed      | 9300.000   | RPM        | ok
FAN2 F Speed      | 9450.000   | RPM        | ok
FAN2 R Speed      | 9300.000   | RPM        | ok
FAN3 F Speed      | 9375.000   | RPM        | ok
FAN3 R Speed      | 9300.000   | RPM        | ok
FAN4 F Speed      | 9375.000   | RPM        | ok
FAN4 R Speed      | 9300.000   | RPM        | ok
FAN5 F Speed      | 9450.000   | RPM        | ok
FAN5 R Speed      | 9225.000   | RPM        | ok
FAN6 F Speed      | 9450.000   | RPM        | ok
FAN6 R Speed      | 9225.000   | RPM        | ok
FAN7 F Speed      | 9375.000   | RPM        | ok
FAN7 R Speed      | 9300.000   | RPM        | ok
FAN8 F Speed      | 9450.000   | RPM        | ok
FAN8 R Speed      | 9300.000   | RPM        | ok
FAN1 Power        | 8.500      | Watts      | ok
FAN2 Power        | 8.200      | Watts      | ok
FAN3 Power        | 8.700      | Watts      | ok
`

func setupMock(t *testing.T) {
	t.Helper()
	ipmi.SetMockSDR(mockSDR)
	t.Cleanup(func() { ipmi.ResetFetcher() })
}

func findMetric(metrics []collector.Metric, name string) *collector.Metric {
	for i := range metrics {
		if metrics[i].Name == name {
			return &metrics[i]
		}
	}
	return nil
}

func TestCollectChassis(t *testing.T) {
	setupMock(t)
	c := New()
	metrics, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// 1 power + 1 inlet_temp + 1 outlet_temp + 16 fan_speed (8×2) + 3 fan_power = 22
	if len(metrics) != 22 {
		t.Fatalf("expected 22 metrics, got %d", len(metrics))
	}

	// power
	if m := findMetric(metrics, "power"); m == nil || m.Value != 1848.0 {
		t.Errorf("power: expected 1848, got %v", m)
	}

	// inlet_temp
	if m := findMetric(metrics, "inlet_temp"); m == nil || m.Value != 28.0 {
		t.Errorf("inlet_temp: expected 28, got %v", m)
	}

	// outlet_temp
	if m := findMetric(metrics, "outlet_temp"); m == nil || m.Value != 42.0 {
		t.Errorf("outlet_temp: expected 42, got %v", m)
	}
}

func TestFanSpeed(t *testing.T) {
	setupMock(t)
	c := New()
	metrics, _ := c.Collect()

	// Count fan_speed metrics
	fanCount := 0
	for _, m := range metrics {
		if m.Name == "fan_speed" {
			fanCount++
			if m.Unit != "RPM" {
				t.Errorf("fan_speed unit: expected RPM, got %s", m.Unit)
			}
			if m.Labels["fan"] == "" || m.Labels["direction"] == "" {
				t.Errorf("fan_speed labels missing: %+v", m.Labels)
			}
		}
	}
	if fanCount != 16 {
		t.Errorf("expected 16 fan_speed metrics (8 fans × F/R), got %d", fanCount)
	}

	// Verify FAN1 F = 9375
	for _, m := range metrics {
		if m.Name == "fan_speed" && m.Labels["fan"] == "1" && m.Labels["direction"] == "F" {
			if m.Value != 9375 {
				t.Errorf("FAN1 F Speed: expected 9375, got %v", m.Value)
			}
		}
	}
}

func TestFanPower(t *testing.T) {
	setupMock(t)
	c := New()
	metrics, _ := c.Collect()

	powerCount := 0
	for _, m := range metrics {
		if m.Name == "fan_power" {
			powerCount++
			if m.Unit != "W" {
				t.Errorf("fan_power unit: expected W, got %s", m.Unit)
			}
			if m.Labels["fan"] == "" {
				t.Errorf("fan_power fan label missing: %+v", m.Labels)
			}
		}
	}
	if powerCount != 3 {
		t.Errorf("expected 3 fan_power metrics, got %d", powerCount)
	}
	// FAN1 Power = 8.5
	for _, m := range metrics {
		if m.Name == "fan_power" && m.Labels["fan"] == "1" {
			if m.Value != 8.5 {
				t.Errorf("FAN1 Power: expected 8.5, got %v", m.Value)
			}
		}
	}
}

func TestParseFanName(t *testing.T) {
	cases := []struct {
		in            string
		wantFan, wantDir string
	}{
		{"FAN1 F Speed", "1", "F"},
		{"FAN3 R Speed", "3", "R"},
		{"FAN8 F Speed", "8", "F"},
	}
	for _, c := range cases {
		fan, dir := parseFanName(c.in)
		if fan != c.wantFan || dir != c.wantDir {
			t.Errorf("parseFanName(%q): expected fan=%s dir=%s, got fan=%s dir=%s", c.in, c.wantFan, c.wantDir, fan, dir)
		}
	}
}

func TestCollectorInterface(t *testing.T) {
	c := New()
	if c.Name() != "chassis" {
		t.Errorf("expected name 'chassis', got '%s'", c.Name())
	}
	if c.Component() != "chassis" {
		t.Errorf("expected component 'chassis', got '%s'", c.Component())
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
