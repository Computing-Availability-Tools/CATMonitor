package cpufreq

import (
	"os"
	"path/filepath"
	"testing"
)

const testdataSys = "../../../tests/testdata/sys"

func TestCores(t *testing.T) {
	s := New(testdataSys)
	cores, err := s.Cores()
	if err != nil {
		t.Fatalf("Cores failed: %v", err)
	}
	if len(cores) != 2 {
		t.Fatalf("expected 2 cores, got %d (%v)", len(cores), cores)
	}
	got := map[string]bool{}
	for _, c := range cores {
		got[c] = true
	}
	if !got["cpu0"] || !got["cpu1"] {
		t.Errorf("expected cpu0 and cpu1, got %v", cores)
	}
}

func TestInfoMinMaxFreq(t *testing.T) {
	s := New(testdataSys)
	min, err := s.InfoMinFreq()
	if err != nil {
		t.Fatalf("InfoMinFreq: %v", err)
	}
	if min != 800000 {
		t.Errorf("InfoMinFreq: want 800000, got %d", min)
	}
	max, err := s.InfoMaxFreq()
	if err != nil {
		t.Fatalf("InfoMaxFreq: %v", err)
	}
	if max != 3500000 {
		t.Errorf("InfoMaxFreq: want 3500000, got %d", max)
	}
}

func TestCurMinMaxFreqAndGovernor(t *testing.T) {
	s := New(testdataSys)
	if v, _ := s.CurMinFreq("cpu0"); v != 800000 {
		t.Errorf("CurMinFreq(cpu0): want 800000, got %d", v)
	}
	if v, _ := s.CurMaxFreq("cpu0"); v != 3500000 {
		t.Errorf("CurMaxFreq(cpu0): want 3500000, got %d", v)
	}
	if v, _ := s.CurFreq("cpu0"); v != 2400000 {
		t.Errorf("CurFreq(cpu0): want 2400000, got %d", v)
	}
	if g, _ := s.Governor("cpu0"); g != "powersave" {
		t.Errorf("Governor(cpu0): want powersave, got %q", g)
	}
}

func TestAvailable_TrueOnTestdata(t *testing.T) {
	s := New(testdataSys)
	if !s.Available() {
		t.Error("Available() want true on testdata tree")
	}
}

func TestAvailable_FalseOnEmpty(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if s.Available() {
		t.Error("Available() want false on empty temp dir")
	}
}

// TestWriteRoundTrip exercises the real write path against a temp fs fixture
// (cannot write to /sys in CI). It builds a minimal cpu0/cpufreq tree, writes
// scaling_min/max_freq, then reads back to verify content + the read path
// reflects the write.
func TestWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cpu0 := filepath.Join(dir, cpuBase, "cpu0", "cpufreq")
	if err := os.MkdirAll(cpu0, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed cpuinfo_min_freq so Available()==true and InfoMinFreq works.
	if err := os.WriteFile(filepath.Join(cpu0, "cpuinfo_min_freq"), []byte("800000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cpu0, "cpuinfo_max_freq"), []byte("3500000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Initial scaling files (current min=max=max).
	if err := os.WriteFile(filepath.Join(cpu0, "scaling_min_freq"), []byte("800000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cpu0, "scaling_max_freq"), []byte("3500000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cpu0, "scaling_governor"), []byte("schedutil\n"), 0644); err != nil {
		t.Fatal(err)
	}

	s := New(dir)
	if !s.Available() {
		t.Fatal("Available() want true on temp fixture")
	}
	// Pin to min: min first then max.
	if err := s.SetMinFreq("cpu0", 800000); err != nil {
		t.Fatalf("SetMinFreq: %v", err)
	}
	if err := s.SetMaxFreq("cpu0", 800000); err != nil {
		t.Fatalf("SetMaxFreq: %v", err)
	}
	if v, _ := s.CurMaxFreq("cpu0"); v != 800000 {
		t.Errorf("after pin, CurMaxFreq want 800000, got %d", v)
	}
	// Governor write round-trip.
	if err := s.SetGovernor("cpu0", "powersave"); err != nil {
		t.Fatalf("SetGovernor: %v", err)
	}
	if g, _ := s.Governor("cpu0"); g != "powersave" {
		t.Errorf("after SetGovernor, Governor want powersave, got %q", g)
	}
}

func TestMockSource(t *testing.T) {
	m := NewMockSource(
		[]string{"cpu0", "cpu1"},
		800000, 3500000,
		map[string]uint64{"cpu0": 800000, "cpu1": 800000},
		map[string]uint64{"cpu0": 3500000, "cpu1": 3500000},
		map[string]string{"cpu0": "powersave", "cpu1": "powersave"},
	)
	if !m.Available() {
		t.Fatal("mock Available want true")
	}
	if err := m.SetMinFreq("cpu0", 800000); err != nil {
		t.Fatalf("SetMinFreq: %v", err)
	}
	if err := m.SetMaxFreq("cpu0", 800000); err != nil {
		t.Fatalf("SetMaxFreq: %v", err)
	}
	if len(m.SetMinCalls) != 1 || m.SetMinCalls[0].KHz != 800000 {
		t.Errorf("SetMinCalls = %+v", m.SetMinCalls)
	}
	if len(m.SetMaxCalls) != 1 {
		t.Errorf("SetMaxCalls = %+v", m.SetMaxCalls)
	}
	// Fail path still recorded.
	m.FailMax("cpu1")
	if err := m.SetMaxFreq("cpu1", 800000); err == nil {
		t.Error("SetMaxFreq(cpu1) want error after FailMax")
	}
	if len(m.SetMaxCalls) != 2 {
		t.Errorf("SetMaxCalls want 2 (recorded even on fail), got %d", len(m.SetMaxCalls))
	}
}
