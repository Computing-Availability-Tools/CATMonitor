package main

import (
	"os"
	"strings"
	"testing"
)

func TestFormatCSVValue(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0, "0"},
		{67, "67"},
		{171344930, "171344930"},
		{1.16, "1.16"},
		{6248.58, "6248.58"},
		{0.897, "0.897"},
		{1.52204e11, "1.52204E+11"},
		{9.99775e11, "9.99775E+11"},
		{76084470008, "76084470008"},
		{-5, "-5"},
	}
	for _, tt := range tests {
		got := formatCSVValue(tt.input)
		if got != tt.want {
			t.Errorf("formatCSVValue(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatCSVLabels(t *testing.T) {
	// No labels
	got := formatCSVLabels(nil)
	if got != "" {
		t.Errorf("empty labels: got %q", got)
	}

	// Single label
	got = formatCSVLabels(map[string]string{"mode": "user"})
	if got != `mode="user"` {
		t.Errorf("single label: got %q", got)
	}

	// Multiple labels (sorted)
	got = formatCSVLabels(map[string]string{"interface": "eth0", "direction": "rx"})
	if got != `direction="rx",interface="eth0"` {
		t.Errorf("multi label: got %q", got)
	}
}

func TestCSVEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`simple`, `simple`},
		{`no,comma`, `"no,comma"`},
		{`has"quote`, `"has""quote"`},
		{`plain123`, `plain123`},
	}
	for _, tt := range tests {
		got := csvEscape(tt.input)
		if got != tt.want {
			t.Errorf("csvEscape(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCSVFileOutput(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "dfee-csv-test")
	defer os.RemoveAll(tmpDir)

	// Create a CSVWriter-like file manually to test format.
	path := tmpDir + "/dfee_metrics_test.csv"
	f, _ := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	defer f.Close()

	w := &CSVWriter{file: f}
	w.writeFileRow("timestamp", "metric_name", "labels", "value")
	w.writeFileRow("1785308522", "node_cpu_seconds_total", `mode="user"`, "171344930")
	w.writeFileRow("1785308522", "node_load1", "", "1.16")
	w.writeFileRow("1785308522", "static_hardware_info", `product_name="A",cpu_info="B,C"`, "1")

	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}

	// Header
	if lines[0] != "timestamp,metric_name,labels,value" {
		t.Errorf("header: got %q", lines[0])
	}

	// Simple label (contains quotes → CSV escaped)
	if lines[1] != `1785308522,node_cpu_seconds_total,"mode=""user""",171344930` {
		t.Errorf("line 1: got %q", lines[1])
	}

	// Empty labels
	if lines[2] != `1785308522,node_load1,,1.16` {
		t.Errorf("line 2: got %q", lines[2])
	}

	// Labels with comma and quotes (both must be escaped)
	if lines[3] != `1785308522,static_hardware_info,"product_name=""A"",cpu_info=""B,C""",1` {
		t.Errorf("line 3: got %q", lines[3])
	}
}
