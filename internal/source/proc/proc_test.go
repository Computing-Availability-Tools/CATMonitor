package proc

import (
	"testing"
)

const testdataProc = "../../../tests/testdata/proc"

func testSource(t *testing.T) Source {
	t.Helper()
	return New(testdataProc)
}

func TestStat(t *testing.T) {
	s := New(testdataProc)
	stat, err := s.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	total, ok := stat.Cores["cpu"]
	if !ok {
		t.Fatal("missing aggregate 'cpu' core")
	}
	if total.User != 3357 {
		t.Errorf("total.User: expected 3357, got %d", total.User)
	}
	if total.System != 4313 {
		t.Errorf("total.System: expected 4313, got %d", total.System)
	}
	if total.Idle != 1362393 {
		t.Errorf("total.Idle: expected 1362393, got %d", total.Idle)
	}
	if total.GuestNice != 0 {
		t.Errorf("total.GuestNice: expected 0, got %d", total.GuestNice)
	}

	if _, ok := stat.Cores["cpu0"]; !ok {
		t.Error("missing cpu0 core")
	}
	if len(stat.Cores) != 5 { // cpu + cpu0..cpu3
		t.Errorf("expected 5 cores, got %d", len(stat.Cores))
	}

	if stat.ContextSwitches != 1148605 {
		t.Errorf("ContextSwitches: expected 1148605, got %d", stat.ContextSwitches)
	}
}

func TestLoadavg(t *testing.T) {
	s := New(testdataProc)
	la, err := s.Loadavg()
	if err != nil {
		t.Fatalf("Loadavg failed: %v", err)
	}
	if la == nil {
		t.Fatal("Loadavg returned nil")
	}
	if la.One != 0.35 {
		t.Errorf("One: expected 0.35, got %v", la.One)
	}
	if la.Five != 0.25 {
		t.Errorf("Five: expected 0.25, got %v", la.Five)
	}
	if la.Fifteen != 0.15 {
		t.Errorf("Fifteen: expected 0.15, got %v", la.Fifteen)
	}
	if la.Running != 2 {
		t.Errorf("Running: expected 2, got %d", la.Running)
	}
	if la.Total != 287 {
		t.Errorf("Total: expected 287, got %d", la.Total)
	}
}

func TestMeminfo(t *testing.T) {
	s := New(testdataProc)
	mi, err := s.Meminfo()
	if err != nil {
		t.Fatalf("Meminfo failed: %v", err)
	}
	if mi["MemTotal"] != 16384000 {
		t.Errorf("MemTotal: expected 16384000, got %d", mi["MemTotal"])
	}
	if mi["MemAvailable"] != 10240000 {
		t.Errorf("MemAvailable: expected 10240000, got %d", mi["MemAvailable"])
	}
	if mi["SwapTotal"] != 8192000 {
		t.Errorf("SwapTotal: expected 8192000, got %d", mi["SwapTotal"])
	}
	if mi["SwapFree"] != 8000000 {
		t.Errorf("SwapFree: expected 8000000, got %d", mi["SwapFree"])
	}
}

func TestDiskstats(t *testing.T) {
	s := New(testdataProc)
	ds, err := s.Diskstats()
	if err != nil {
		t.Fatalf("Diskstats failed: %v", err)
	}
	sda, ok := ds["sda"]
	if !ok {
		t.Fatal("missing sda device")
	}
	if sda.ReadsCompleted != 12345 {
		t.Errorf("sda.ReadsCompleted: expected 12345, got %d", sda.ReadsCompleted)
	}
	if sda.SectorsWritten != 100000 {
		t.Errorf("sda.SectorsWritten: expected 100000, got %d", sda.SectorsWritten)
	}
	if _, ok := ds["ram0"]; !ok {
		t.Error("source should return ALL devices incl ram0 (filtering is collector's job)")
	}
}

func TestNetDev(t *testing.T) {
	s := New(testdataProc)
	nd, err := s.NetDev()
	if err != nil {
		t.Fatalf("NetDev failed: %v", err)
	}
	eth0, ok := nd["eth0"]
	if !ok {
		t.Fatal("missing eth0 interface")
	}
	if eth0.RxBytes != 5000000 {
		t.Errorf("eth0.RxBytes: expected 5000000, got %d", eth0.RxBytes)
	}
	if eth0.TxPackets != 3000 {
		t.Errorf("eth0.TxPackets: expected 3000, got %d", eth0.TxPackets)
	}
	if eth0.RxErrs != 2 {
		t.Errorf("eth0.RxErrs: expected 2, got %d", eth0.RxErrs)
	}
	if _, ok := nd["lo"]; !ok {
		t.Error("source should return ALL interfaces incl lo (filtering is collector's job)")
	}
}

func TestVmstat(t *testing.T) {
	s := New(testdataProc)
	vs, err := s.Vmstat()
	if err != nil {
		t.Fatalf("Vmstat failed: %v", err)
	}
	if vs["pgfault"] != 2836432 {
		t.Errorf("pgfault: expected 2836432, got %d", vs["pgfault"])
	}
	if vs["pgmajfault"] != 1234 {
		t.Errorf("pgmajfault: expected 1234, got %d", vs["pgmajfault"])
	}
}

func TestCpuinfo(t *testing.T) {
	s := New(testdataProc)
	info, err := s.Cpuinfo()
	if err != nil {
		t.Fatalf("Cpuinfo failed: %v", err)
	}
	if info == nil {
		t.Fatal("Cpuinfo returned nil")
	}
	if info.ModelName != "Intel(R) Xeon(R) Gold 6248R CPU @ 3.00GHz" {
		t.Errorf("ModelName: got %q", info.ModelName)
	}
	if info.Cores != 4 {
		t.Errorf("Cores: expected 4, got %d", info.Cores)
	}
	if info.CacheSize != "35840 KB" {
		t.Errorf("CacheSize: got %q", info.CacheSize)
	}
}

func TestBuddyinfo(t *testing.T) {
	s := New(testdataProc)
	bi, err := s.Buddyinfo()
	if err != nil {
		t.Fatalf("Buddyinfo failed: %v", err)
	}
	if len(bi) != 5 {
		t.Fatalf("expected 5 (node,zone) entries, got %d", len(bi))
	}

	var normal0 *BuddyInfo
	for i := range bi {
		if bi[i].Node == "0" && bi[i].Zone == "Normal" {
			normal0 = &bi[i]
			break
		}
	}
	if normal0 == nil {
		t.Fatal("missing node 0 zone Normal")
	}
	if len(normal0.Orders) != 11 {
		t.Errorf("node0 Normal orders: expected 11, got %d", len(normal0.Orders))
	}
	if normal0.Orders[0] != 1234 {
		t.Errorf("node0 Normal order0: expected 1234, got %d", normal0.Orders[0])
	}
}

func TestMounts(t *testing.T) {
	s := New(testdataProc)
	ms, err := s.Mounts()
	if err != nil {
		t.Fatalf("Mounts failed: %v", err)
	}
	if len(ms) != 4 {
		t.Fatalf("expected 4 mounts, got %d", len(ms))
	}
	var sda1 *Mount
	for i := range ms {
		if ms[i].Device == "/dev/sda1" {
			sda1 = &ms[i]
			break
		}
	}
	if sda1 == nil {
		t.Fatal("missing /dev/sda1 mount")
	}
	if sda1.MountPoint != "/" || sda1.Fstype != "ext4" {
		t.Errorf("sda1: got %+v", sda1)
	}
}

func TestNetTCPStates(t *testing.T) {
	s := New(testdataProc)
	states, err := s.NetTCPStates()
	if err != nil {
		t.Fatalf("NetTCPStates failed: %v", err)
	}
	// tcp sample: 0A(LISTEN)=1, 01(ESTABLISHED)=2, 06(TIME_WAIT)=1
	if states["LISTEN"] != 1 {
		t.Errorf("LISTEN: expected 1, got %d", states["LISTEN"])
	}
	if states["ESTABLISHED"] != 2 {
		t.Errorf("ESTABLISHED: expected 2, got %d", states["ESTABLISHED"])
	}
	if states["TIME_WAIT"] != 1 {
		t.Errorf("TIME_WAIT: expected 1, got %d", states["TIME_WAIT"])
	}
}

func TestSetRootRedirectsDefault(t *testing.T) {
	original := root
	SetRoot(testdataProc)
	defer SetRoot(original)

	stat, err := Default().Stat()
	if err != nil {
		t.Fatalf("Default after SetRoot failed: %v", err)
	}
	if stat.ContextSwitches != 1148605 {
		t.Errorf("Default should read from testdata after SetRoot; got ctxt=%d", stat.ContextSwitches)
	}
}

func TestMissingFile(t *testing.T) {
	// A root with no stat file yields an error from Stat.
	s := New("/nonexistent/proc")
	if _, err := s.Stat(); err == nil {
		t.Error("Stat on missing root should return error")
	}
	if _, err := s.Loadavg(); err == nil {
		t.Error("Loadavg on missing root should return error")
	}
}

func TestPressure(t *testing.T) {
	s := New(testdataProc)
	p, err := s.Pressure("memory")
	if err != nil {
		t.Fatalf("Pressure failed: %v", err)
	}
	if p == nil {
		t.Fatal("Pressure returned nil")
	}
	// some avg10=0.06 avg60=0.01 avg300=0.00 total=12345
	if p.Some.Avg10 != 0.06 {
		t.Errorf("Some.Avg10: expected 0.06, got %v", p.Some.Avg10)
	}
	if p.Some.Avg60 != 0.01 {
		t.Errorf("Some.Avg60: expected 0.01, got %v", p.Some.Avg60)
	}
	if p.Some.Avg300 != 0.00 {
		t.Errorf("Some.Avg300: expected 0.00, got %v", p.Some.Avg300)
	}
	if p.Some.Total != 12345 {
		t.Errorf("Some.Total: expected 12345, got %d", p.Some.Total)
	}
	// full line all zero in sample
	if p.Full.Avg10 != 0 {
		t.Errorf("Full.Avg10: expected 0, got %v", p.Full.Avg10)
	}
}

func TestPressureMissing(t *testing.T) {
	s := New("/nonexistent/proc")
	if _, err := s.Pressure("memory"); err == nil {
		t.Error("Pressure on missing root should return error")
	}
}
