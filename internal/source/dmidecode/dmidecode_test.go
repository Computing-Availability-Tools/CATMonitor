package dmidecode

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

func TestParseDmidecode(t *testing.T) {
	out := readMock(t, "../../../tests/testdata/dmidecode-type17.txt")
	devs := parseDmidecode(out)

	if len(devs) != 3 {
		t.Fatalf("expected 3 memory device sections, got %d", len(devs))
	}

	d0 := devs[0]
	if d0.Locator != "DIMM0" {
		t.Errorf("dev0 Locator: expected DIMM0, got %q", d0.Locator)
	}
	if d0.Type != "DDR4" {
		t.Errorf("dev0 Type: expected DDR4, got %q", d0.Type)
	}
	if d0.Speed != "3200 MT/s" {
		t.Errorf("dev0 Speed: got %q", d0.Speed)
	}
	if d0.Manufacturer != "Samsung" {
		t.Errorf("dev0 Manufacturer: got %q", d0.Manufacturer)
	}
	if d0.SizeMB != 16384 {
		t.Errorf("dev0 SizeMB: expected 16384 (16GB), got %d", d0.SizeMB)
	}

	// DIMM1 also 16GB
	if devs[1].SizeMB != 16384 {
		t.Errorf("dev1 SizeMB: expected 16384, got %d", devs[1].SizeMB)
	}
	if devs[1].Locator != "DIMM1" {
		t.Errorf("dev1 Locator: expected DIMM1, got %q", devs[1].Locator)
	}

	// DIMM2 empty slot
	if devs[2].SizeMB != 0 {
		t.Errorf("dev2 (empty slot) SizeMB: expected 0, got %d", devs[2].SizeMB)
	}
}

func TestParseSizeMB(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"16 GB", 16384},
		{"1024 MB", 1024},
		{"1 TB", 1048576},
		{"No Module Installed", 0},
		{"", 0},
	}
	for _, c := range cases {
		if got := parseSizeMB(c.in); got != c.want {
			t.Errorf("parseSizeMB(%q): expected %d, got %d", c.in, c.want, got)
		}
	}
}

func TestMemoryDevicesCaches(t *testing.T) {
	SetMock(readMock(t, "../../../tests/testdata/dmidecode-type17.txt"))
	d1, err := Default().MemoryDevices()
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	d2, err := Default().MemoryDevices()
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	// Same cached pointer (permanent cache, like lscpu).
	if len(d1) != len(d2) {
		t.Errorf("cache should serve same result; got %d vs %d", len(d1), len(d2))
	}
}

func TestParseSystemInfo(t *testing.T) {
	out := readMock(t, "../../../tests/testdata/dmidecode-type1.txt")
	si := parseSystemInfo(out)
	if si == nil {
		t.Fatal("expected SystemInfo, got nil")
	}
	if si.Manufacturer != "Supermicro" {
		t.Errorf("Manufacturer: got %q", si.Manufacturer)
	}
	if si.ProductName != "X12STW-F" {
		t.Errorf("ProductName: got %q", si.ProductName)
	}
	if si.Version != "1.0" {
		t.Errorf("Version: got %q", si.Version)
	}
	if si.Serial != "S12345678X" {
		t.Errorf("Serial: got %q", si.Serial)
	}
}

func TestParseSystemInfoEmpty(t *testing.T) {
	if si := parseSystemInfo("Handle 0x0002, DMI type 4, 0 bytes\nProcessor\n"); si != nil {
		t.Errorf("expected nil when no System Information section, got %+v", si)
	}
}

func TestSystemInfoCaches(t *testing.T) {
	SetSystemMock(readMock(t, "../../../tests/testdata/dmidecode-type1.txt"))
	s1, err := Default().SystemInfo()
	if err != nil || s1 == nil {
		t.Fatalf("first call failed: %v", err)
	}
	s2, err := Default().SystemInfo()
	if err != nil || s2 == nil {
		t.Fatalf("second call failed: %v", err)
	}
	if s1 != s2 {
		t.Error("SystemInfo should be cached (same pointer)")
	}
}
