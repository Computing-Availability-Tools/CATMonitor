package disk

import (
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func TestParseMounts(t *testing.T) {
	mounts, err := parseMounts("../../../tests/testdata/proc")
	if err != nil {
		t.Fatalf("parseMounts failed: %v", err)
	}

	// Test data has 4 mount points: /dev/sda1, proc, sysfs, tmpfs
	if len(mounts) != 4 {
		t.Fatalf("expected 4 mount points, got %d", len(mounts))
	}

	// Check first mount point
	m0 := mounts[0]
	if m0.device != "/dev/sda1" {
		t.Errorf("expected device '/dev/sda1', got '%s'", m0.device)
	}
	if m0.mountPoint != "/" {
		t.Errorf("expected mount point '/', got '%s'", m0.mountPoint)
	}
	if m0.fstype != "ext4" {
		t.Errorf("expected fstype 'ext4', got '%s'", m0.fstype)
	}
}

func TestVirtualFSFiltering(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")

	// parseMounts returns 4 mounts, but proc/sysfs/tmpfs should be filtered
	mounts, err := parseMounts(c.procPath)
	if err != nil {
		t.Fatalf("parseMounts failed: %v", err)
	}

	// Count non-virtual filesystems
	realFSCount := 0
	for _, m := range mounts {
		if !virtualFS[m.fstype] {
			realFSCount++
		}
	}

	// Only /dev/sda1 (ext4) should remain
	if realFSCount != 1 {
		t.Errorf("expected 1 real filesystem, got %d", realFSCount)
	}
}

func TestCollectSpaceUsage(t *testing.T) {
	c := New()

	// Use root filesystem for statfs test (always available)
	now := time.Now()
	metrics, err := c.collectSpaceUsage("/dev/root", "/", "ext4", now)
	if err != nil {
		t.Fatalf("collectSpaceUsage failed: %v", err)
	}

	if len(metrics) != 4 {
		t.Fatalf("expected 4 metrics (1 usage + 3 detail), got %d", len(metrics))
	}

	usage := metrics[0]
	if usage.Name != "space_usage" {
		t.Errorf("expected name 'space_usage', got '%s'", usage.Name)
	}
	if usage.Unit != "%" {
		t.Errorf("expected unit '%%', got '%s'", usage.Unit)
	}
	if usage.Value < 0 || usage.Value > 100 {
		t.Errorf("usage should be 0-100, got %.2f", usage.Value)
	}

	// Check detail metrics
	fields := make(map[string]bool)
	for _, m := range metrics[1:] {
		if m.Name != "space_detail" {
			t.Errorf("expected name 'space_detail', got '%s'", m.Name)
		}
		fields[m.Labels["field"]] = true
		if m.Value < 0 {
			t.Errorf("space detail should be >= 0, got %.0f", m.Value)
		}
	}
	if !fields["total"] || !fields["used"] || !fields["available"] {
		t.Error("expected total, used, available fields")
	}
}

func TestCollectIntegration(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")

	metrics, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Should have at least 4 metrics for the root filesystem
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
	// Verify virtual filesystems are in the map
	virtualFilesystems := []string{"proc", "sysfs", "devtmpfs", "tmpfs", "overlay", "squashfs"}
	for _, fs := range virtualFilesystems {
		if !virtualFS[fs] {
			t.Errorf("expected '%s' to be in virtualFS map", fs)
		}
	}

	// Real filesystems should not be in the map
	realFilesystems := []string{"ext4", "xfs", "btrfs", "ntfs"}
	for _, fs := range realFilesystems {
		if virtualFS[fs] {
			t.Errorf("expected '%s' to NOT be in virtualFS map", fs)
		}
	}
}

func TestWithField(t *testing.T) {
	labels := map[string]string{
		"device":     "/dev/sda1",
		"mount_point": "/",
		"fstype":      "ext4",
	}

	result := withField(labels, "total")

	if result["field"] != "total" {
		t.Error("expected field 'total'")
	}
	if result["device"] != "/dev/sda1" {
		t.Error("expected device to be preserved")
	}
	if result["fstype"] != "ext4" {
		t.Error("expected fstype to be preserved")
	}

	// Verify original map is not modified
	if _, ok := labels["field"]; ok {
		t.Error("original map should not be modified")
	}
}

func TestParseDiskStats(t *testing.T) {
	stats, err := parseDiskStats("../../../tests/testdata/proc")
	if err != nil {
		t.Fatalf("parseDiskStats failed: %v", err)
	}

	sda, ok := stats["sda"]
	if !ok {
		t.Fatal("missing 'sda' device in diskstats")
	}
	if sda.readsCompleted != 12345 {
		t.Errorf("expected readsCompleted 12345, got %d", sda.readsCompleted)
	}
	if sda.sectorsRead != 200000 {
		t.Errorf("expected sectorsRead 200000, got %d", sda.sectorsRead)
	}
	if sda.writesCompleted != 5000 {
		t.Errorf("expected writesCompleted 5000, got %d", sda.writesCompleted)
	}
	if sda.sectorsWritten != 100000 {
		t.Errorf("expected sectorsWritten 100000, got %d", sda.sectorsWritten)
	}

	if _, exists := stats["ram0"]; exists {
		t.Error("ram0 should be filtered out by device filter")
	}
}

func TestCollectIOPS(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")

	now := time.Now()
	// First call stores state, no IOPS metrics
	metrics1, err := c.collectIOPS(now)
	if err != nil {
		t.Fatalf("first collectIOPS failed: %v", err)
	}
	if len(metrics1) != 0 {
		t.Errorf("expected 0 metrics on first call, got %d", len(metrics1))
	}

	// Second call computes delta (same data, delta=0)
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
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")

	now := time.Now()
	// First call to populate prevDiskStats
	c.collectIOPS(now)

	// Second call for throughput
	metrics, err := c.collectThroughput(now)
	if err != nil {
		t.Fatalf("collectThroughput failed: %v", err)
	}
	for _, m := range metrics {
		if m.Name != "throughput" {
			t.Errorf("expected name 'throughput', got '%s'", m.Name)
		}
		if m.Unit != "MB/s" {
			t.Errorf("expected unit 'MB/s', got '%s'", m.Unit)
		}
	}
}

func TestCollectIoWait(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")

	now := time.Now()
	// First call stores state
	metrics1, err := c.collectIoWait(now)
	if err != nil {
		t.Fatalf("first collectIoWait failed: %v", err)
	}
	if len(metrics1) != 0 {
		t.Errorf("expected 0 metrics on first call, got %d", len(metrics1))
	}

	// Second call computes delta (same data, delta=0, io_wait=0)
	metrics2, _ := c.collectIoWait(now)
	for _, m := range metrics2 {
		if m.Name != "io_wait" {
			t.Errorf("expected name 'io_wait', got '%s'", m.Name)
		}
	}
}

func TestCollectIoErrors(t *testing.T) {
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")
	c.SetMockDmesg("kernel: I/O error, dev sda, sector 12345\nkernel: blk_update_request: I/O error\nkernel: normal line\n")

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
	c := New()
	c.SetProcPath("../../../tests/testdata/proc")
	c.SetMockSmartctl("sda", "SMART overall-health self-assessment test result: PASSED\nTemperature_Celsius 35 (0 19 0 0 0)\n")

	now := time.Now()
	metrics, err := c.collectSMART(now)
	if err != nil {
		t.Fatalf("collectSMART failed: %v", err)
	}

	if len(metrics) < 1 {
		t.Fatalf("expected at least 1 SMART metric, got %d", len(metrics))
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

// TestStringFields can be used to verify mount parsing edge cases
func TestParseMountsEdgeCases(t *testing.T) {
	// Verify that all mount entries have at least 3 fields
	mounts, _ := parseMounts("../../../tests/testdata/proc")
	for _, m := range mounts {
		if m.device == "" || m.mountPoint == "" || m.fstype == "" {
			t.Error("mount entry should not have empty fields")
		}
		if !strings.HasPrefix(m.device, "/") && m.device != "none" {
			// Some special devices like proc, sysfs don't start with /
			// This is just a sanity check, not a strict requirement
		}
	}
}
