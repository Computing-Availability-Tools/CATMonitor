package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CSVWriter periodically writes metrics to a CSV file.
// It reuses Exporter.buildMetrics() to get the same metrics as the
// Prometheus exporter, then formats them as standard CSV rows.
type CSVWriter struct {
	exporter *Exporter
	dir      string
	interval time.Duration
	file     *os.File
	mu       sync.Mutex
}

// NewCSVWriter creates a CSVWriter that writes to dir every interval.
func NewCSVWriter(exporter *Exporter, dir string, interval time.Duration) *CSVWriter {
	return &CSVWriter{
		exporter: exporter,
		dir:      dir,
		interval: interval,
	}
}

// Run starts the periodic CSV write loop until ctx is canceled.
func (w *CSVWriter) Run(ctx context.Context) {
	_ = os.MkdirAll(w.dir, 0o755)
	filename := "dfee_metrics_" + time.Now().Format("20060102_150405") + ".csv"
	path := filepath.Join(w.dir, filename)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	w.mu.Lock()
	w.file = f
	w.mu.Unlock()

	// Write header once.
	w.writeFileRow("timestamp", "metric_name", "labels", "value")

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Write immediately on start.
	w.writeCycle()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.writeCycle()
		}
	}
}

// Close closes the CSV file.
func (w *CSVWriter) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
}

// writeCycle collects metrics and writes them to the CSV file.
func (w *CSVWriter) writeCycle() {
	metrics := w.exporter.buildMetrics()
	ts := time.Now().Unix()

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return
	}
	for _, m := range metrics {
		w.writeFileRow(
			strconv.FormatInt(ts, 10),
			m.name,
			formatCSVLabels(m.labels),
			formatCSVValue(m.value),
		)
	}
}

// writeFileRow writes one CSV row. All fields are CSV-escaped (double-quoted
// if they contain comma, quote, or newline).
func (w *CSVWriter) writeFileRow(timestamp, name, labels, value string) {
	row := csvEscape(timestamp) + "," +
		csvEscape(name) + "," +
		csvEscape(labels) + "," +
		csvEscape(value) + "\n"
	_, _ = w.file.WriteString(row)
}

// formatCSVLabels formats labels as key="value",key2="value2".
// Returns empty string if no labels.
func formatCSVLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	// Sort for deterministic output.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+`="`+labels[k]+`"`)
	}
	return strings.Join(parts, ",")
}

// formatCSVValue formats a float for CSV output.
// - Integer < 1e11: integer format (e.g. 171344930)
// - >= 1e11: scientific notation %.5E (e.g. 1.52204E+11)
// - Non-integer: compact decimal (e.g. 1.16, 6248.58)
func formatCSVValue(v float64) string {
	if v == float64(int64(v)) {
		// Exact integer
		abs := v
		if abs < 0 {
			abs = -abs
		}
		if abs >= 1e11 {
			return strconv.FormatFloat(v, 'E', 5, 64)
		}
		return strconv.FormatInt(int64(v), 10)
	}
	// Non-integer: compact decimal
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// csvEscape wraps a field in double quotes if it contains comma, quote, or
// newline. Inner double quotes are doubled (standard CSV escaping).
func csvEscape(s string) string {
	if !strings.ContainsAny(s, `,"`+"\n\r") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
