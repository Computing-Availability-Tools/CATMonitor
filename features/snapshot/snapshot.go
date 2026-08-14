package snapshot

import (
	"encoding/json"
	"os"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/health"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// Snapshot is the single cached view written by the collector goroutine and
// read by the HTTP layer. It is the decoupling boundary: the web side never
// calls collectors directly, it only reads this file.
type Snapshot struct {
	SessionID       string               `json:"session_id"`
	Timestamp       time.Time            `json:"timestamp"`
	RefreshInterval int                  `json:"refresh_interval_ms"`
	HistoryPoints   int                  `json:"history_points"`
	Health          health.HealthScore   `json:"health"`
	Metrics         []collector.Metric   `json:"metrics"`
	History         map[string][]float64 `json:"history"`
	// Specs holds stashed static device specs (CPU model, frequency, cache,
	// topology, memory modules). Collectors emit these once at startup then
	// suppress them via flags; without this stash the snapshot would lose all
	// device specs after the first cycle. Populated by collectOnce from the
	// first cycle that yields any static metric, then re-injected every cycle.
	Specs []collector.Metric `json:"specs,omitempty"`
}

// WriteAtomic writes the snapshot to disk atomically: write to a temp file in
// the same directory, then rename. Readers never see a half-written file.
func WriteAtomic(path string, s *Snapshot) error {
	return WriteJSONAtomic(path, s)
}

// Read loads the snapshot from disk.
func Read(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
