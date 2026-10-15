package stragglerout

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

// StragglerFields returns the documented KPI field list in stable order.
func TestStragglerFieldsStableOrder(t *testing.T) {
	first := StragglerFields()
	if len(first) != 11 {
		t.Fatalf("StragglerFields() = %d fields, want 11: %v", len(first), first)
	}
	if first[0] != "temp" || first[len(first)-1] != "roce_new_pkt_rty" {
		t.Errorf("unexpected boundary fields: %v", first)
	}
	seen := map[string]bool{}
	for _, f := range first {
		if seen[f] {
			t.Errorf("duplicate field %q", f)
		}
		seen[f] = true
	}
	// Stable across calls.
	second := StragglerFields()
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("order changed between calls: %v vs %v", first, second)
		}
	}
}

// DataDir returns the configured directory; an empty dir falls back to the
// module default.
func TestKPIWriterDataDir(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := NewKPIWriter("/data/kpi", 24*time.Hour, logger)
	if got := w.DataDir(); got != "/data/kpi" {
		t.Errorf("DataDir() = %q, want /data/kpi", got)
	}
	w = NewKPIWriter("", 24*time.Hour, logger)
	if got := w.DataDir(); got != "straggler" {
		t.Errorf("DataDir() = %q, want default straggler", got)
	}
}
