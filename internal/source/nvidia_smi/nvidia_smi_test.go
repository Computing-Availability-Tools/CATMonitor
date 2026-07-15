package nvidia_smi

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

func TestParseCSVLine(t *testing.T) {
	fields := parseCSVLine("0, 82, 16384, 24576, 72, 250.50, 65, 0, 1545")
	if len(fields) != 9 {
		t.Fatalf("expected 9 fields, got %d", len(fields))
	}
	if fields[0] != "0" {
		t.Errorf("field 0: expected '0', got '%s'", fields[0])
	}
	if fields[5] != "250.50" {
		t.Errorf("field 5: expected '250.50', got '%s'", fields[5])
	}
}

func TestParseOutput(t *testing.T) {
	out := readMock(t, "../../../tests/testdata/nvidia-smi-output.txt")
	gpus := parseOutput(out)
	if len(gpus) != 2 {
		t.Fatalf("expected 2 GPUs, got %d", len(gpus))
	}
	g0 := gpus[0]
	if g0.Index != "0" {
		t.Errorf("gpu0 Index: expected '0', got '%s'", g0.Index)
	}
	if g0.Utilization != 82 {
		t.Errorf("gpu0 Utilization: expected 82, got %v", g0.Utilization)
	}
	if g0.MemUsed != 16384 {
		t.Errorf("gpu0 MemUsed: expected 16384, got %v", g0.MemUsed)
	}
	if g0.MemTotal != 24576 {
		t.Errorf("gpu0 MemTotal: expected 24576, got %v", g0.MemTotal)
	}
	if g0.Temperature != 72 {
		t.Errorf("gpu0 Temperature: expected 72, got %v", g0.Temperature)
	}
	if g0.Power != 250.5 {
		t.Errorf("gpu0 Power: expected 250.5, got %v", g0.Power)
	}
	if g0.EccErrors != 0 {
		t.Errorf("gpu0 EccErrors: expected 0, got %v", g0.EccErrors)
	}
	if g0.ClockFreq != 1545 {
		t.Errorf("gpu0 ClockFreq: expected 1545, got %v", g0.ClockFreq)
	}
}

func TestQueryWithMock(t *testing.T) {
	out := readMock(t, "../../../tests/testdata/nvidia-smi-output.txt")
	SetMock(out)
	defer ResetFetcher()

	gpus, err := Default().Query()
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(gpus) != 2 {
		t.Fatalf("expected 2 GPUs, got %d", len(gpus))
	}
	for _, g := range gpus {
		if g.Index == "" {
			t.Error("GPU Index should not be empty")
		}
	}
}

func TestQueryEmpty(t *testing.T) {
	SetMock("")
	defer ResetFetcher()

	gpus, err := Default().Query()
	if err != nil {
		t.Fatalf("Query with empty mock should not error: %v", err)
	}
	if len(gpus) != 0 {
		t.Errorf("expected 0 GPUs for empty input, got %d", len(gpus))
	}
}
