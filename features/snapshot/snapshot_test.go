package snapshot

import (
	"path/filepath"
	"testing"
	"time"
)

// TestSnapshotRoundTrip writes a Snapshot (the frontend-facing response shape
// web assembles from global + per-comp files) and reads it back, covering the
// atomic-write/read decoupling boundary used by the HTTP layer.
func TestSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	in := &Snapshot{
		Timestamp:       time.Now(),
		RefreshInterval: 5000,
		HistoryPoints:   60,
		History:         map[string][]float64{"cpu_temperature": {55, 60}},
	}
	if err := WriteAtomic(path, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := Read(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.RefreshInterval != in.RefreshInterval || out.HistoryPoints != in.HistoryPoints {
		t.Error("snapshot fields mismatch on round trip")
	}
	if got := out.History["cpu_temperature"]; len(got) != 2 || got[1] != 60 {
		t.Errorf("history round trip = %v want [55 60]", got)
	}
}
