package lscpu

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

func TestParseLscpu(t *testing.T) {
	out := readMock(t, "../../../tests/testdata/lscpu-output.txt")
	topo := parseLscpu(out)

	if topo.Cores != 28 {
		t.Errorf("Cores: expected 28, got %d", topo.Cores)
	}
	if topo.Sockets != 2 {
		t.Errorf("Sockets: expected 2, got %d", topo.Sockets)
	}
	if topo.CoresPerSocket != 14 {
		t.Errorf("CoresPerSocket: expected 14, got %d", topo.CoresPerSocket)
	}
	if topo.DiesPerSocket != 1 {
		t.Errorf("DiesPerSocket: expected 1, got %d", topo.DiesPerSocket)
	}
	if len(topo.NumaNodes) != 2 {
		t.Fatalf("NumaNodes: expected 2, got %d", len(topo.NumaNodes))
	}
	if topo.NumaCPU["0"] != "0-13" {
		t.Errorf("NumaCPU[0]: expected '0-13', got %q", topo.NumaCPU["0"])
	}
	if topo.NumaCPU["1"] != "14-27" {
		t.Errorf("NumaCPU[1]: expected '14-27', got %q", topo.NumaCPU["1"])
	}
}

func TestParseLscpuMissingDies(t *testing.T) {
	// lscpu without "Die(s) per socket" line: DiesPerSocket should default to 1.
	out := "CPU(s): 8\nSocket(s): 1\nCore(s) per socket: 8\nNUMA node(s): 1\n"
	topo := parseLscpu(out)
	if topo.DiesPerSocket != 1 {
		t.Errorf("DiesPerSocket should default to 1, got %d", topo.DiesPerSocket)
	}
}

func TestTopologyCachesAcrossCalls(t *testing.T) {
	SetMock(readMock(t, "../../../tests/testdata/lscpu-output.txt"))
	s := Default()

	t1, err := s.Topology()
	if err != nil {
		t.Fatalf("first Topology failed: %v", err)
	}
	t2, err := s.Topology()
	if err != nil {
		t.Fatalf("second Topology failed: %v", err)
	}
	if t1 != t2 {
		t.Error("Topology should return the same cached pointer across calls")
	}
}

func TestTopologyMockInject(t *testing.T) {
	SetMock("CPU(s): 64\nSocket(s): 4\n")
	topo, err := Default().Topology()
	if err != nil {
		t.Fatalf("Topology with mock failed: %v", err)
	}
	if topo.Cores != 64 || topo.Sockets != 4 {
		t.Errorf("mock parse: expected 64 cores/4 sockets, got %d/%d", topo.Cores, topo.Sockets)
	}
}
