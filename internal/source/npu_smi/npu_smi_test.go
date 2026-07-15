package npu_smi

import (
	"os"
	"strings"
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

func TestTopo(t *testing.T) {
	want := readMock(t, "../../../tests/testdata/npu-smi-topo-output.txt")
	SetMock(func(args ...string) (string, error) { return want, nil })
	defer ResetFetcher()

	got, err := Default().Topo()
	if err != nil {
		t.Fatalf("Topo failed: %v", err)
	}
	if got != strings.TrimSpace(want) {
		t.Errorf("Topo: expected %q, got %q", strings.TrimSpace(want), got)
	}
}

func TestTopoCachesAcrossCalls(t *testing.T) {
	calls := 0
	SetMock(func(args ...string) (string, error) {
		calls++
		return "topo", nil
	})
	defer ResetFetcher()

	Default().Topo()
	Default().Topo()
	if calls != 1 {
		t.Errorf("Topo should be called once (permanent cache), got %d", calls)
	}
}

func TestHccsBandwidth(t *testing.T) {
	out := readMock(t, "../../../tests/testdata/npu-smi-hccs-bw-output.txt")
	SetMock(func(args ...string) (string, error) { return out, nil })
	defer ResetFetcher()

	bw, err := Default().HccsBandwidth(0)
	if err != nil {
		t.Fatalf("HccsBandwidth failed: %v", err)
	}
	if bw.TxMB != 305.6 {
		t.Errorf("TxMB: expected 305.6, got %v", bw.TxMB)
	}
	if bw.RxMB != 280.3 {
		t.Errorf("RxMB: expected 280.3, got %v", bw.RxMB)
	}
}
