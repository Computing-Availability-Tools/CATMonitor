package mce

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

func TestParseMCE(t *testing.T) {
	text := readMock(t, "../../../tests/testdata/dmesg-mce-sample.txt")
	events := parseMCE(text)

	// Expected: CPU0 CE=3, CPU1 UCE=1
	want := map[string]map[string]uint64{
		"0": {"CE": 3},
		"1": {"UCE": 1},
	}
	got := map[string]map[string]uint64{}
	for _, e := range events {
		if got[e.Socket] == nil {
			got[e.Socket] = map[string]uint64{}
		}
		got[e.Socket][e.Kind] = e.Count
	}
	for sock, kinds := range want {
		for kind, cnt := range kinds {
			if got[sock][kind] != cnt {
				t.Errorf("socket %s %s: expected %d, got %d", sock, kind, cnt, got[sock][kind])
			}
		}
	}
	if len(events) != 2 {
		t.Errorf("expected 2 distinct (socket,kind) events, got %d", len(events))
	}
}

func TestParseMCEEmpty(t *testing.T) {
	events := parseMCE("nothing relevant here\n")
	if len(events) != 0 {
		t.Errorf("expected 0 events for non-MCE log, got %d", len(events))
	}
}

func TestParseMCENoSocketDefaultsToZero(t *testing.T) {
	events := parseMCE("Machine Check Exception: corrected error\n")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Socket != "0" {
		t.Errorf("expected default socket '0', got %q", events[0].Socket)
	}
	if events[0].Kind != "CE" {
		t.Errorf("expected kind CE, got %q", events[0].Kind)
	}
}

func TestEventsMockInject(t *testing.T) {
	SetMock(readMock(t, "../../../tests/testdata/dmesg-mce-sample.txt"))
	events, err := Default().Events()
	if err != nil {
		t.Fatalf("Events with mock failed: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 events via mock, got %d", len(events))
	}
}

func TestExtractSocket(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"Machine Check Exception on CPU 5: corrected", "5"},
		{"[Hardware Error]: CPU 12: uncorrectable", "12"},
		{"mce: Machine Check Exception: corrected", "0"},
	}
	for _, c := range cases {
		got := extractSocket(c.line)
		if got != c.want {
			t.Errorf("extractSocket(%q): expected %q, got %q", c.line, c.want, got)
		}
	}
}
