//go:build linux

package disk

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/dmesg"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/smartctl"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/statfs"
)

const (
	testdataProc = "../../../tests/testdata/proc"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(data)
}

// useTestdata redirects proc source to testdata, mocks dmesg (for io_errors)
// and smartctl (for SMART). statfs is left real so space_usage tests hit the
// actual root filesystem (always available).
func useTestdata(t *testing.T) {
	t.Helper()
	proc.SetRoot(testdataProc)
	dmesg.SetMock(readFile(t, "../../../tests/testdata/dmesg-oom-sample.txt"))
	smartctl.SetFetcher(func(dev string) (string, error) {
		return "SMART overall-health self-assessment test result: PASSED\nTemperature_Celsius 35\n", nil
	})
	t.Cleanup(func() {
		proc.SetRoot("/proc")
		dmesg.ResetFetcher()
		smartctl.ResetFetcher()
		statfs.ResetFetcher()
	})
}

func TestVirtualFSFiltering(t *testing.T) {
	useTestdata(t)
	mounts, err := proc.Default().Mounts()
	if err != nil {
		t.Fatalf("Mounts failed: %v", err)
	}
	realFSCount := 0
	for _, m := range mounts {
		if !virtualFS[m.Fstype] {
			realFSCount++
		}
	}
	// testdata mounts: /dev/sda1(ext4), proc, sysfs, tmpfs → only sda1 real.
	if realFSCount != 1 {
		t.Errorf("expected 1 real filesystem, got %d", realFSCount)
	}
}

func TestCollectSpaceUsage(t *testing.T) {
	c := New()
	now := time.Now()
	// Real root filesystem (statfs always available).
	metrics, err := c.collectSpaceUsage("/dev/root", "/", "ext4", now)
	if err != nil {
		t.Fatalf("collectSpaceUsage failed: %v", err)
	}
	if len(metrics) != 4 {
		t.Fatalf("expected 4 metrics (1 usage + 3 detail), got %d", len(metrics))
	}
	if metrics[0].Name != "space_usage" || metrics[0].Unit != "%" {
		t.Errorf("space_usage: %+v", metrics[0])
	}
	if metrics[0].Value < 0 || metrics[0].Value > 100 {
		t.Errorf("usage should be 0-100, got %.2f", metrics[0].Value)
	}
	fields := make(map[string]bool)
	for _, m := range metrics[1:] {
		if m.Name != "space_detail" {
			t.Errorf("expected space_detail, got %s", m.Name)
		}
		fields[m.Labels["field"]] = true
	}
	if !fields["total"] || !fields["used"] || !fields["available"] {
		t.Error("expected total/used/available fields")
	}
}

func TestCollectIOPS(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	// First call stores state, no IOPS metrics.
	metrics1, err := c.collectIOPS(now)
	if err != nil {
		t.Fatalf("first collectIOPS failed: %v", err)
	}
	if len(metrics1) != 0 {
		t.Errorf("expected 0 metrics on first call, got %d", len(metrics1))
	}
	// Second call computes delta (same data, delta=0).
	metrics2, _ := c.collectIOPS(now)
	for _, m := range metrics2 {
		if m.Name != "iops" {
			t.Errorf("expected name 'iops', got '%s'", m.Name)
		}
		if m.Value != 0 {
			t.Errorf("expected 0 IOPS (no change), got %.0f", m.Value)
		}
	}
}

func TestCollectThroughput(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	// Populate prevDiskStats via collectIOPS first.
	c.collectIOPS(now)
	metrics, err := c.collectThroughput(now)
	if err != nil {
		t.Fatalf("collectThroughput failed: %v", err)
	}
	for _, m := range metrics {
		if m.Name != "throughput" || m.Unit != "MB/s" {
			t.Errorf("expected throughput MB/s, got %s %s", m.Name, m.Unit)
		}
	}
}

func TestCollectIoWait(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	metrics1, err := c.collectIoWait(now)
	if err != nil {
		t.Fatalf("first collectIoWait failed: %v", err)
	}
	if len(metrics1) != 0 {
		t.Errorf("expected 0 metrics on first call, got %d", len(metrics1))
	}
	metrics2, _ := c.collectIoWait(now)
	for _, m := range metrics2 {
		if m.Name != "io_wait" {
			t.Errorf("expected name 'io_wait', got '%s'", m.Name)
		}
	}
}

func TestCollectIoErrors(t *testing.T) {
	useTestdata(t)
	c := New()
	// Override dmesg mock with IO-error-specific content.
	dmesg.SetMock("kernel: I/O error, dev sda, sector 12345\nkernel: blk_update_request: I/O error\nkernel: normal line\n")
	now := time.Now()
	metrics, err := c.collectIoErrors(now)
	if err != nil {
		t.Fatalf("collectIoErrors failed: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Value != 2 {
		t.Errorf("expected 2 IO errors, got %.0f", metrics[0].Value)
	}
	if metrics[0].Name != "io_errors" {
		t.Errorf("expected name 'io_errors', got '%s'", metrics[0].Name)
	}
}

func TestCollectSMART(t *testing.T) {
	useTestdata(t)
	c := New()
	now := time.Now()
	metrics, err := c.collectSMART(now)
	if err != nil {
		t.Fatalf("collectSMART failed: %v", err)
	}
	// testdata diskstats has sda, sdb (ram0 filtered) → 2 devices × (status + temp) = 4
	if len(metrics) < 2 {
		t.Fatalf("expected at least 2 SMART metrics, got %d", len(metrics))
	}
	hasStatus := false
	for _, m := range metrics {
		switch m.Name {
		case "smart_status":
			hasStatus = true
			if m.Labels["status"] != "PASSED" {
				t.Errorf("expected PASSED, got '%s'", m.Labels["status"])
			}
		case "smart_temperature":
			if m.Value < 0 {
				t.Errorf("expected non-negative temperature, got %.0f", m.Value)
			}
		}
	}
	if !hasStatus {
		t.Error("expected smart_status metric")
	}
}

func TestCollectIntegration(t *testing.T) {
	useTestdata(t)
	c := New()

	metrics, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(metrics) < 4 {
		t.Errorf("expected at least 4 metrics, got %d", len(metrics))
	}
	for _, m := range metrics {
		if m.Component != "disk" {
			t.Errorf("expected component 'disk', got '%s'", m.Component)
		}
		if m.Timestamp.IsZero() {
			t.Error("timestamp should not be zero")
		}
	}
}

func TestVirtualFSMap(t *testing.T) {
	virtualFilesystems := []string{"proc", "sysfs", "devtmpfs", "tmpfs", "overlay", "squashfs"}
	for _, fs := range virtualFilesystems {
		if !virtualFS[fs] {
			t.Errorf("expected '%s' to be in virtualFS map", fs)
		}
	}
	realFilesystems := []string{"ext4", "xfs", "btrfs", "ntfs"}
	for _, fs := range realFilesystems {
		if virtualFS[fs] {
			t.Errorf("expected '%s' to NOT be in virtualFS map", fs)
		}
	}
}

func TestWithField(t *testing.T) {
	labels := map[string]string{"device": "/dev/sda1", "mount_point": "/", "fstype": "ext4"}
	result := withField(labels, "total")
	if result["field"] != "total" || result["device"] != "/dev/sda1" || result["fstype"] != "ext4" {
		t.Error("withField did not preserve/copy fields as expected")
	}
	if _, ok := labels["field"]; ok {
		t.Error("original map should not be modified")
	}
}

func TestParseSmartOutput(t *testing.T) {
	now := time.Now()
	metrics := parseSmartOutput("sda", "SMART overall-health self-assessment test result: PASSED\nTemperature_Celsius 35\n", now)
	if len(metrics) < 1 {
		t.Fatal("expected at least 1 SMART metric")
	}
	hasStatus, hasTemp := false, false
	for _, m := range metrics {
		switch m.Name {
		case "smart_status":
			hasStatus = true
			if m.Labels["status"] != "PASSED" {
				t.Errorf("expected PASSED, got '%s'", m.Labels["status"])
			}
		case "smart_temperature":
			hasTemp = true
			if m.Value != 35 {
				t.Errorf("expected temp 35, got %.0f", m.Value)
			}
		}
	}
	if !hasStatus || !hasTemp {
		t.Errorf("expected smart_status and smart_temperature, got status=%v temp=%v", hasStatus, hasTemp)
	}
}

func TestCollectorInterface(t *testing.T) {
	c := New()
	if c.Name() != "disk" {
		t.Errorf("expected name 'disk', got '%s'", c.Name())
	}
	if c.Component() != "disk" {
		t.Errorf("expected component 'disk', got '%s'", c.Component())
	}
	if c.Priority() != collector.PriorityHigh {
		t.Errorf("expected priority High, got %s", c.Priority())
	}
	if c.DefaultInterval() != 5*time.Second {
		t.Errorf("expected interval 5s, got %v", c.DefaultInterval())
	}
	if !c.DefaultEnabled() {
		t.Error("expected default enabled true")
	}
}

func TestRoundFloat(t *testing.T) {
	if v := roundFloat(37.555, 2); v != 37.56 {
		t.Errorf("expected 37.56, got %.2f", v)
	}
	if v := roundFloat(0.0, 2); v != 0 {
		t.Errorf("expected 0, got %.2f", v)
	}
	if v := roundFloat(100.0, 2); v != 100 {
		t.Errorf("expected 100, got %.2f", v)
	}
}

func TestParseMountsEdgeCases(t *testing.T) {
	useTestdata(t)
	mounts, _ := proc.Default().Mounts()
	for _, m := range mounts {
		if m.Device == "" || m.MountPoint == "" || m.Fstype == "" {
			t.Error("mount entry should not have empty fields")
		}
		if !strings.HasPrefix(m.Device, "/") && m.Device != "none" {
			// special devices (proc, sysfs) don't start with / — sanity only
		}
	}
}
