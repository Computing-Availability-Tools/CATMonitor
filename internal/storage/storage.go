package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// JSONLStorage writes metrics to JSONL files (one JSON object per line),
// one file per component per calendar day (cpu_2026-09-15.jsonl). Files
// older than maxAge are deleted at startup and once per day rollover.
type JSONLStorage struct {
	dataDir string
	maxAge  time.Duration // 0 = keep forever (no retention cleanup)

	// mu guards the shared files map and the retention cleanup only.
	// The per-metric file writes themselves run WITHOUT the lock: each
	// component's file is written only by that component's collector
	// goroutine (the scheduler runs one goroutine per collector), so
	// per-file writes are naturally serialized and a large CPU batch
	// cannot starve npu/network writes waiting on a global lock.
	mu      sync.Mutex
	files   map[string]*os.File
	lastDay string // date (2006-01-02) of the last retention cleanup
}

// jsonlDateRe extracts the date suffix from "<component>_<YYYY-MM-DD>.jsonl".
var jsonlDateRe = regexp.MustCompile(`^(.+)_(\d{4}-\d{2}-\d{2})\.jsonl$`)

// New creates a new JSONLStorage with the given data directory. When
// maxAge > 0, JSONL files whose embedded date is older than maxAge are
// deleted immediately (startup cleanup) and again on each calendar-day
// rollover (triggered by the first Write of a new day).
func New(dataDir string, maxAge time.Duration) (*JSONLStorage, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}
	s := &JSONLStorage{
		dataDir: dataDir,
		maxAge:  maxAge,
		files:   make(map[string]*os.File),
	}
	if maxAge > 0 {
		s.cleanupExpired()
	}
	s.mu.Lock()
	s.lastDay = time.Now().Format("2006-01-02")
	s.mu.Unlock()
	return s, nil
}

// Write appends metrics to the per-component JSONL files, grouped by
// component. See the struct comment for the locking model.
func (s *JSONLStorage) Write(metrics []collector.Metric) error {
	now := time.Now()
	dateStr := now.Format("2006-01-02")
	s.maybeCleanup(dateStr)

	for _, m := range metrics {
		filename := fmt.Sprintf("%s_%s.jsonl", m.Component, dateStr)
		path := filepath.Join(s.dataDir, filename)

		f, err := s.getFile(path)
		if err != nil {
			return err
		}

		data, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("failed to marshal metric: %w", err)
		}
		data = append(data, '\n')

		if _, err := f.Write(data); err != nil {
			return fmt.Errorf("failed to write metric: %w", err)
		}
	}

	return nil
}

// maybeCleanup runs the retention cleanup once per calendar-day change.
// Concurrent triggers are safe: the day check runs under s.mu and the
// cleanup itself is idempotent.
func (s *JSONLStorage) maybeCleanup(dateStr string) {
	if s.maxAge <= 0 {
		return
	}
	s.mu.Lock()
	if s.lastDay == dateStr {
		s.mu.Unlock()
		return
	}
	s.lastDay = dateStr
	s.mu.Unlock()
	s.cleanupExpired()
}

// cleanupExpired deletes JSONL files whose embedded date is older than
// maxAge. Open handles for deleted files are closed and evicted from the
// files map first (the current day's file is never expired, so active
// writers just re-open their fresh file).
func (s *JSONLStorage) cleanupExpired() {
	if s.maxAge <= 0 {
		return
	}
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		match := jsonlDateRe.FindStringSubmatch(e.Name())
		if match == nil {
			continue // not a dated JSONL file; leave it alone
		}
		fileDate, err := time.ParseInLocation("2006-01-02", match[2], time.Local)
		if err != nil {
			continue
		}
		if fileDate.Add(s.maxAge).Before(now) {
			path := filepath.Join(s.dataDir, e.Name())
			if f, ok := s.files[path]; ok {
				_ = f.Close()
				delete(s.files, path)
			}
			_ = os.Remove(path)
		}
	}
}

// getFile returns the open file handle for path, opening it if necessary.
// The lock is held only for the map lookup / open, not for any writes.
func (s *JSONLStorage) getFile(path string) (*os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.files[path]; ok {
		return f, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open data file: %w", err)
	}
	s.files[path] = f
	return f, nil
}

// Close closes all open file handles.
func (s *JSONLStorage) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.files {
		_ = f.Close()
	}
	s.files = make(map[string]*os.File)
}
