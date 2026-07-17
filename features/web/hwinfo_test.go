package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/dmidecode"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/smartctl"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/sys"
)

// testdata lives two levels up from features/web/ (at repo root /tests/testdata).
const hwTestdataSys = "../../tests/testdata/sys"

func readHWMock(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func newTestHW() *hwCollector { return newHWCollector() }

func TestParseNPUStatic(t *testing.T) {
	out := readHWMock(t, "../../tests/testdata/npu-smi-output.txt")
	var data []string
	for _, l := range strings.Split(out, "\n") {
		if isNPUDataLine(l) {
			data = append(data, l)
		}
	}
	if len(data) < 2 {
		t.Fatalf("expected >=2 data lines, got %d", len(data))
	}
	id, name, bus := parseNPUStatic(data[0], data[1])
	if id != "0" || name != "910A" || bus != "0000:01:00.0" {
		t.Errorf("got id=%q name=%q bus=%q", id, name, bus)
	}
}

func TestHWGpuInfo(t *testing.T) {
	c := newTestHW()
	c.setNvidiaMock("0, Tesla T4, GPU-abc-123, 535.129\n1, Tesla V100, GPU-def-456, 535.129\n")
	m := c.gpuInfo(time.Now())
	if len(m) != 2 {
		t.Fatalf("expected 2 gpu_info, got %d", len(m))
	}
	if m[0].Component != "gpu" || m[0].Name != "gpu_info" {
		t.Errorf("metric: %s/%s", m[0].Component, m[0].Name)
	}
	if m[0].Labels["name"] != "Tesla T4" || m[0].Labels["uuid"] != "GPU-abc-123" || m[0].Labels["driver_version"] != "535.129" {
		t.Errorf("labels: %+v", m[0].Labels)
	}
}

func TestHWNpuInfo(t *testing.T) {
	c := newTestHW()
	c.setNpuMock(readHWMock(t, "../../tests/testdata/npu-smi-output.txt"))
	m := c.npuInfo(time.Now())
	if len(m) != 2 {
		t.Fatalf("expected 2 npu_info, got %d", len(m))
	}
	if m[0].Labels["name"] != "910A" || m[0].Labels["bus_id"] != "0000:01:00.0" {
		t.Errorf("npu0: %+v", m[0].Labels)
	}
	if m[1].Labels["npu_id"] != "1" {
		t.Errorf("npu1 id: %q", m[1].Labels["npu_id"])
	}
}

func TestHWDeviceModel(t *testing.T) {
	dmidecode.SetSystemMock(readHWMock(t, "../../tests/testdata/dmidecode-type1.txt"))
	c := newTestHW()
	m := c.deviceModel(time.Now())
	if m == nil {
		t.Fatal("expected device_model, got nil")
	}
	if m.Component != "system" || m.Name != "device_model" {
		t.Errorf("metric: %s/%s", m.Component, m.Name)
	}
	if m.Labels["manufacturer"] != "Supermicro" || m.Labels["product_name"] != "X12STW-F" || m.Labels["serial_number"] != "S12345678X" {
		t.Errorf("labels: %+v", m.Labels)
	}
}

func TestHWNetInfo(t *testing.T) {
	// Redirect the global sys root to testdata (isolated per test binary).
	sys.SetRoot(hwTestdataSys)
	defer sys.SetRoot("/sys")
	c := newTestHW()
	m := c.netInfo(time.Now())
	if len(m) != 1 {
		t.Fatalf("expected 1 net_info (eth0, lo skipped), got %d", len(m))
	}
	lb := m[0].Labels
	if lb["interface"] != "eth0" || lb["mac"] != "aa:bb:cc:dd:ee:ff" || lb["mtu"] != "1500" || lb["speed"] != "1000" || lb["driver"] != "e1000" {
		t.Errorf("net_info labels: %+v", lb)
	}
}

func TestHWDiskInfo(t *testing.T) {
	sys.SetRoot(hwTestdataSys)
	defer sys.SetRoot("/sys")
	smartctl.SetInfoFetcher(func(dev string) (string, error) {
		return readHWMock(t, "../../tests/testdata/smartctl-info-output.txt"), nil
	})
	defer smartctl.ResetFetcher()
	c := newTestHW()
	m := c.diskInfo(time.Now())
	if len(m) != 2 {
		t.Fatalf("expected 2 disks (sda+sdb), got %d", len(m))
	}
	sda := m[0]
	if sda.Component != "disk" || sda.Name != "disk_info" || sda.Unit != "GB" {
		t.Errorf("metric: %s/%s unit=%q", sda.Component, sda.Name, sda.Unit)
	}
	if sda.Labels["device"] != "sda" || sda.Labels["model"] != "Virtual Disk" {
		t.Errorf("sda labels: %+v", sda.Labels)
	}
	// smartctl enriches serial/firmware/interface.
	if sda.Labels["serial"] != "S4P2NX0K123456A" || sda.Labels["firmware"] != "2B2QEXE7" || sda.Labels["interface"] != "PCIe" {
		t.Errorf("sda enrich labels: %+v", sda.Labels)
	}
	if sda.Value < 0.3 || sda.Value > 0.5 {
		t.Errorf("sda size(GB): got %v want ~0.4", sda.Value)
	}
	if m[1].Value < 1099 || m[1].Value > 1100 {
		t.Errorf("sdb size(GB): got %v want ~1099.5", m[1].Value)
	}
}

// TestCollectHWSpecsSmoke runs the real entry point on this host. It must not
// panic and must only emit the 6 known identity metric names (or none when the
// hardware/tools are absent).
func TestCollectHWSpecsSmoke(t *testing.T) {
	m := collectHWSpecs()
	known := map[string]bool{
		"device_model": true, "os_info": true, "gpu_info": true, "npu_info": true,
		"disk_info": true, "net_info": true,
	}
	for _, mm := range m {
		if !known[mm.Name] {
			t.Errorf("unexpected metric %q from collectHWSpecs", mm.Name)
		}
	}
}

// TestHWOSInfo verifies os_info is collected on Linux (PRETTY_NAME from
// /etc/os-release, kernel from `uname -r`); skips gracefully when /etc/os-release
// is absent (e.g. minimal containers).
func TestHWOSInfo(t *testing.T) {
	c := newHWCollector()
	m := c.osInfo(time.Now())
	if m == nil {
		t.Skip("os_info unavailable (no /etc/os-release and uname absent)")
	}
	if m.Component != "system" || m.Name != "os_info" {
		t.Fatalf("got component=%s name=%s, want system/os_info", m.Component, m.Name)
	}
	if lb := m.Labels; lb != nil {
		if lb["pretty_name"] == "" {
			t.Error("pretty_name label empty")
		}
	}
}

// compile-time: hwCollector is used.
var _ = (*hwCollector)(nil)
var _ collector.Metric
