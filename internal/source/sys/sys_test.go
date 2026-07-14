package sys

import (
	"testing"
)

const testdataSys = "../../../tests/testdata/sys"

func TestCpuFreqs(t *testing.T) {
	s := New(testdataSys)
	freqs, err := s.CpuFreqs()
	if err != nil {
		t.Fatalf("CpuFreqs failed: %v", err)
	}
	if freqs["cpu0"] != 2400000 {
		t.Errorf("cpu0: expected 2400000 kHz, got %d", freqs["cpu0"])
	}
	if freqs["cpu1"] != 1800000 {
		t.Errorf("cpu1: expected 1800000 kHz, got %d", freqs["cpu1"])
	}
	if len(freqs) != 2 {
		t.Errorf("expected 2 cores, got %d", len(freqs))
	}
}

func TestCpuInfoMinMaxFreq(t *testing.T) {
	s := New(testdataSys)
	min, err := s.CpuInfoMinFreq()
	if err != nil {
		t.Fatalf("CpuInfoMinFreq failed: %v", err)
	}
	if min != 800000 {
		t.Errorf("min: expected 800000, got %d", min)
	}
	max, err := s.CpuInfoMaxFreq()
	if err != nil {
		t.Fatalf("CpuInfoMaxFreq failed: %v", err)
	}
	if max != 3500000 {
		t.Errorf("max: expected 3500000, got %d", max)
	}
}

func TestCacheInfos(t *testing.T) {
	s := New(testdataSys)
	caches, err := s.CacheInfos("cpu0")
	if err != nil {
		t.Fatalf("CacheInfos failed: %v", err)
	}
	if len(caches) != 4 {
		t.Fatalf("expected 4 cache indexes, got %d", len(caches))
	}
	byLevelType := map[string]CacheInfo{}
	for _, c := range caches {
		byLevelType[levelTypeKey(c.Level, c.Type)] = c
	}
	l1d, ok := byLevelType["1-Data"]
	if !ok {
		t.Fatal("missing L1 Data cache")
	}
	if l1d.SizeKB != 32 {
		t.Errorf("L1d size: expected 32 KB, got %d", l1d.SizeKB)
	}
	l3, ok := byLevelType["3-Unified"]
	if !ok {
		t.Fatal("missing L3 Unified cache")
	}
	if l3.SizeKB != 35840 {
		t.Errorf("L3 size: expected 35840 KB, got %d", l3.SizeKB)
	}
}

func levelTypeKey(level int, typ string) string {
	return intToStr(level) + "-" + typ
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	if i < 0 {
		return "-" + intToStr(-i)
	}
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestParseCPUList(t *testing.T) {
	cases := []struct {
		in   string
		want []int
	}{
		{"0-3,5,7-9", []int{0, 1, 2, 3, 5, 7, 8, 9}},
		{"4,6", []int{4, 6}},
		{"8-9", []int{8, 9}},
		{"0", []int{0}},
		{"", nil},
		{"0-0", []int{0}},
	}
	for _, c := range cases {
		got := parseCPUList(c.in)
		if !equalInts(got, c.want) {
			t.Errorf("parseCPUList(%q): expected %v, got %v", c.in, c.want, got)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCpuOnlineOfflineIsolated(t *testing.T) {
	s := New(testdataSys)
	online, err := s.CpuOnline()
	if err != nil {
		t.Fatalf("CpuOnline failed: %v", err)
	}
	if len(online) != 8 {
		t.Errorf("online count: expected 8, got %d", len(online))
	}
	offline, err := s.CpuOffline()
	if err != nil {
		t.Fatalf("CpuOffline failed: %v", err)
	}
	if len(offline) != 2 {
		t.Errorf("offline count: expected 2, got %d", len(offline))
	}
	isolated, err := s.CpuIsolated()
	if err != nil {
		t.Fatalf("CpuIsolated failed: %v", err)
	}
	if len(isolated) != 2 {
		t.Errorf("isolated count: expected 2, got %d", len(isolated))
	}
}

func TestNodes(t *testing.T) {
	s := New(testdataSys)
	nodes, err := s.Nodes()
	if err != nil {
		t.Fatalf("Nodes failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 NUMA nodes, got %d", len(nodes))
	}
	if nodes[0] != "0" || nodes[1] != "1" {
		t.Errorf("nodes: expected [0 1], got %v", nodes)
	}
}

func TestEdac(t *testing.T) {
	s := New(testdataSys)
	edacs, err := s.Edac()
	if err != nil {
		t.Fatalf("Edac failed: %v", err)
	}
	if len(edacs) != 2 {
		t.Fatalf("expected 2 mc, got %d", len(edacs))
	}
	var mc0 *EdacMC
	for i := range edacs {
		if edacs[i].Name == "mc0" {
			mc0 = &edacs[i]
			break
		}
	}
	if mc0 == nil {
		t.Fatal("missing mc0")
	}
	if mc0.CECount != 3 {
		t.Errorf("mc0 CECount: expected 3, got %d", mc0.CECount)
	}
	if mc0.UECount != 0 {
		t.Errorf("mc0 UECount: expected 0, got %d", mc0.UECount)
	}
}

func TestNetOperstate(t *testing.T) {
	s := New(testdataSys)
	state, err := s.NetOperstate("eth0")
	if err != nil {
		t.Fatalf("NetOperstate failed: %v", err)
	}
	if state != "up" {
		t.Errorf("eth0 state: expected 'up', got %q", state)
	}
}

func TestNetInterfaces(t *testing.T) {
	s := New(testdataSys)
	ifaces, err := s.NetInterfaces()
	if err != nil {
		t.Fatalf("NetInterfaces failed: %v", err)
	}
	// testdata has eth0 and lo
	have := map[string]bool{}
	for _, n := range ifaces {
		have[n] = true
	}
	if !have["eth0"] || !have["lo"] {
		t.Errorf("expected eth0 and lo, got %v", ifaces)
	}
}

func TestNetInterfaceInfo(t *testing.T) {
	s := New(testdataSys)
	info, err := s.NetInterfaceInfo("eth0")
	if err != nil || info == nil {
		t.Fatalf("NetInterfaceInfo failed: %v", err)
	}
	if info.Name != "eth0" {
		t.Errorf("Name: got %q", info.Name)
	}
	if info.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC: got %q", info.MAC)
	}
	if info.MTU != 1500 {
		t.Errorf("MTU: got %d", info.MTU)
	}
	if info.Speed != 1000 {
		t.Errorf("Speed: got %d want 1000", info.Speed)
	}
	if info.Driver != "e1000" {
		t.Errorf("Driver: got %q want e1000", info.Driver)
	}
}

func TestNetInterfaceInfoVirtual(t *testing.T) {
	// "lo" has address/mtu but no device/driver symlink; must not error.
	s := New(testdataSys)
	info, err := s.NetInterfaceInfo("lo")
	if err != nil || info == nil {
		t.Fatalf("NetInterfaceInfo(lo) failed: %v", err)
	}
	if info.Driver != "" {
		t.Errorf("lo Driver should be empty, got %q", info.Driver)
	}
	if info.Speed != -1 {
		t.Errorf("lo Speed should be -1 (no speed file), got %d", info.Speed)
	}
}

func TestBlockDevices(t *testing.T) {
	s := New(testdataSys)
	devs, err := s.BlockDevices()
	if err != nil {
		t.Fatalf("BlockDevices failed: %v", err)
	}
	byName := map[string]BlockDev{}
	for _, d := range devs {
		byName[d.Name] = d
	}
	// sda + sdb are real; loop0 must be filtered out.
	if _, ok := byName["loop0"]; ok {
		t.Error("loop0 should be filtered out (virtual device)")
	}
	if len(byName) < 2 {
		t.Fatalf("expected sda+sdb, got %v", byName)
	}
	sda := byName["sda"]
	if sda.Model != "Virtual Disk" {
		t.Errorf("sda Model: got %q", sda.Model)
	}
	if sda.SizeBytes != 730960*512 {
		t.Errorf("sda SizeBytes: got %d want %d", sda.SizeBytes, 730960*512)
	}
	sdb := byName["sdb"]
	if sdb.SizeBytes != 2147483648*512 {
		t.Errorf("sdb SizeBytes: got %d", sdb.SizeBytes)
	}
}

func TestThermal(t *testing.T) {
	s := New(testdataSys)
	zones, err := s.Thermal()
	if err != nil {
		t.Fatalf("Thermal failed: %v", err)
	}
	if len(zones) != 2 {
		t.Fatalf("expected 2 thermal zones, got %d", len(zones))
	}
	byName := map[string]int{}
	for _, z := range zones {
		byName[z.Name] = z.TempMilliC
	}
	if byName["thermal_zone0"] != 65000 {
		t.Errorf("thermal_zone0: expected 65000 milli-C, got %d", byName["thermal_zone0"])
	}
	if byName["thermal_zone1"] != 55000 {
		t.Errorf("thermal_zone1: expected 55000 milli-C, got %d", byName["thermal_zone1"])
	}
}

func TestSetRootRedirectsDefault(t *testing.T) {
	original := root
	SetRoot(testdataSys)
	defer SetRoot(original)
	nodes, err := Default().Nodes()
	if err != nil {
		t.Fatalf("Default after SetRoot failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("Default should read testdata after SetRoot; got %d nodes", len(nodes))
	}
}

func TestMissingRoot(t *testing.T) {
	s := New("/nonexistent/sys")
	if _, err := s.CpuFreqs(); err == nil {
		t.Error("CpuFreqs on missing root should return error")
	}
	if _, err := s.Nodes(); err == nil {
		t.Error("Nodes on missing root should return error")
	}
}
