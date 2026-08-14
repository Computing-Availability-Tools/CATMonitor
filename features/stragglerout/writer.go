package stragglerout

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// KPIWriter appends KPI samples to a daily JSONL file and prunes files older
// than the retention window. One file per day:
//
//	{dataDir}/straggler_kpi_{YYYY-MM-DD}.jsonl
//
// Each line is one KPISample JSON. The daily split mirrors CATMonitor's own
// JSONL rotation and lets the straggler reader pick a date range for its
// baseline/detection windows.
type KPIWriter struct {
	dataDir   string
	retention time.Duration
	mu        sync.Mutex
	logger    *slog.Logger
	lastPrune time.Time
}

// NewKPIWriter creates a writer. dataDir is created if absent.
func NewKPIWriter(dataDir string, retention time.Duration, logger *slog.Logger) *KPIWriter {
	if logger == nil {
		logger = slog.Default()
	}
	if dataDir == "" {
		dataDir = "straggler"
	}
	if retention <= 0 {
		retention = 15 * 24 * time.Hour
	}
	_ = os.MkdirAll(dataDir, 0o755)
	return &KPIWriter{dataDir: dataDir, retention: retention, logger: logger}
}

// Append writes one sample to the daily file named by the sample's timestamp.
func (w *KPIWriter) Append(s *KPISample) error {
	if s == nil {
		return nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal kpi sample: %w", err)
	}
	data = append(data, '\n')
	path := w.pathFor(sampleTime(s.Timestamp))
	w.mu.Lock()
	defer w.mu.Unlock()
	// O_APPEND|O_CREATE is atomic per-write on POSIX; safe for concurrent
	// Append from the storage tap goroutine.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open kpi file %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write kpi file %s: %w", path, err)
	}
	return nil
}

// pathFor returns the daily file path for the given time (local date).
func (w *KPIWriter) pathFor(t time.Time) string {
	return filepath.Join(w.dataDir, "straggler_kpi_"+t.Local().Format("2006-01-02")+".jsonl")
}

// Prune deletes daily files whose modification time is older than retention.
// Called opportunistically from the storage tap at most once per hour to keep
// the dir bounded; cheap because it only stats existing files.
func (w *KPIWriter) Prune(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.lastPrune.IsZero() {
		w.lastPrune = now
		return
	}
	if now.Sub(w.lastPrune) < time.Hour {
		return
	}
	w.lastPrune = now
	cutoff := now.Add(-w.retention)
	entries, err := os.ReadDir(w.dataDir)
	if err != nil {
		w.logger.Error("stragglerout prune: read dir", "error", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(w.dataDir, e.Name())); err != nil {
				w.logger.Error("stragglerout prune: remove", "file", e.Name(), "error", err)
			}
		}
	}
}

// DataDir returns the configured data directory (for inspection/tests).
func (w *KPIWriter) DataDir() string { return w.dataDir }
