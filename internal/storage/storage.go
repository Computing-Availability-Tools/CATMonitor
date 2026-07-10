package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// JSONLStorage writes metrics to JSONL files (one JSON object per line).
type JSONLStorage struct {
	dataDir string
	mu      sync.Mutex
	files   map[string]*os.File
}

// New creates a new JSONLStorage with the given data directory.
func New(dataDir string) (*JSONLStorage, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}
	return &JSONLStorage{
		dataDir: dataDir,
		files:   make(map[string]*os.File),
	}, nil
}

// Write appends metrics to the appropriate JSONL files, grouped by component.
func (s *JSONLStorage) Write(metrics []collector.Metric) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dateStr := time.Now().Format("2006-01-02")

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

// getFile returns an open file handle for the given path, opening it if necessary.
func (s *JSONLStorage) getFile(path string) (*os.File, error) {
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
