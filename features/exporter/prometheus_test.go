package exporter

import (
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func TestIsCounter(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"user_time", true},
		{"idle_time", true},
		{"steal_time", true},
		{"rx_bytes_total", true},
		{"tx_bytes_total", true},
		{"usage", false},
		{"temperature", false},
		{"power_draw", false},
		{"iops", false},
		{"acg_count", false},
	}
	for _, tt := range tests {
		if got := isCounter(tt.name); got != tt.want {
			t.Errorf("isCounter(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPromName(t *testing.T) {
	tests := []struct {
		component, name, want string
	}{
		{"cpu", "usage", "catmonitor_cpu_usage"},
		{"npu", "temperature", "catmonitor_npu_temperature"},
		{"network", "rx_bytes_total", "catmonitor_network_rx_bytes_total"},
		{"disk", "read_latency", "catmonitor_disk_read_latency"},
		{"chassis", "power", "catmonitor_chassis_power"},
	}
	for _, tt := range tests {
		if got := promName(tt.component, tt.name); got != tt.want {
			t.Errorf("promName(%q,%q) = %q, want %q", tt.component, tt.name, got, tt.want)
		}
	}
}

func TestEncodeBasic(t *testing.T) {
	metrics := []collector.Metric{
		{Component: "cpu", Name: "usage", Value: 12.3, Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
	}
	out := string(Encode(metrics))
	if !strings.Contains(out, "# HELP catmonitor_cpu_usage") {
		t.Errorf("missing HELP line:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE catmonitor_cpu_usage gauge") {
		t.Errorf("missing TYPE gauge:\n%s", out)
	}
	if !strings.Contains(out, `catmonitor_cpu_usage{core="total"} 12.3`) {
		t.Errorf("missing data line:\n%s", out)
	}
}

func TestEncodeCounter(t *testing.T) {
	metrics := []collector.Metric{
		{Component: "cpu", Name: "user_time", Value: 3357, Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		{Component: "network", Name: "rx_bytes_total", Value: 21227412, Labels: map[string]string{"interface": "eth0"}, Timestamp: time.Now()},
	}
	out := string(Encode(metrics))
	if !strings.Contains(out, "# TYPE catmonitor_cpu_user_time counter") {
		t.Errorf("user_time should be counter:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE catmonitor_network_rx_bytes_total counter") {
		t.Errorf("rx_bytes_total should be counter:\n%s", out)
	}
}

func TestEncodeLabels(t *testing.T) {
	metrics := []collector.Metric{
		{Component: "npu", Name: "temperature", Value: 55, Labels: map[string]string{"npu_id": "0"}, Timestamp: time.Now()},
		{Component: "npu", Name: "temperature", Value: 60, Labels: map[string]string{"npu_id": "1"}, Timestamp: time.Now()},
		{Component: "disk", Name: "throughput", Value: 10.5, Labels: map[string]string{"device": "sda", "direction": "read"}, Timestamp: time.Now()},
	}
	out := string(Encode(metrics))
	if !strings.Contains(out, `catmonitor_npu_temperature{npu_id="0"} 55`) {
		t.Errorf("missing npu 0 line:\n%s", out)
	}
	if !strings.Contains(out, `catmonitor_npu_temperature{npu_id="1"} 60`) {
		t.Errorf("missing npu 1 line:\n%s", out)
	}
	if !strings.Contains(out, `catmonitor_disk_throughput{device="sda",direction="read"} 10.5`) {
		t.Errorf("missing disk line:\n%s", out)
	}
}

func TestEncodeEmpty(t *testing.T) {
	out := Encode(nil)
	if len(out) != 0 {
		t.Errorf("expected empty output, got %d bytes", len(out))
	}
}

func TestEncodeNoLabels(t *testing.T) {
	metrics := []collector.Metric{
		{Component: "chassis", Name: "power", Value: 350, Timestamp: time.Now()},
	}
	out := string(Encode(metrics))
	if !strings.Contains(out, "catmonitor_chassis_power 350") {
		t.Errorf("missing data line without labels:\n%s", out)
	}
}

func TestEncodeGrouping(t *testing.T) {
	// Multiple metrics with the same prometheus name should share HELP/TYPE.
	metrics := []collector.Metric{
		{Component: "cpu", Name: "usage", Value: 12.3, Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
		{Component: "cpu", Name: "usage", Value: 15.0, Labels: map[string]string{"core": "0"}, Timestamp: time.Now()},
		{Component: "cpu", Name: "usage", Value: 10.0, Labels: map[string]string{"core": "1"}, Timestamp: time.Now()},
	}
	out := string(Encode(metrics))
	helpCount := strings.Count(out, "# HELP catmonitor_cpu_usage")
	if helpCount != 1 {
		t.Errorf("expected 1 HELP line, got %d:\n%s", helpCount, out)
	}
	typeCount := strings.Count(out, "# TYPE catmonitor_cpu_usage")
	if typeCount != 1 {
		t.Errorf("expected 1 TYPE line, got %d:\n%s", typeCount, out)
	}
	dataCount := strings.Count(out, "catmonitor_cpu_usage{")
	if dataCount != 3 {
		t.Errorf("expected 3 data lines, got %d:\n%s", dataCount, out)
	}
}
