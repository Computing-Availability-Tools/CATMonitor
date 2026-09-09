package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Computing-Availability-Tools/CATMonitor/features/health"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/config"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(data)
}

func TestRenderScoreBar(t *testing.T) {
	cases := []struct {
		score, max, filled int
	}{
		{100, 100, 30},
		{0, 100, 0},
		{50, 100, 15},
		{15, 100, 4},  // 4.5 truncated to 4
		{150, 100, 30}, // capped at width
		{10, 0, 0},    // zero max -> empty bar, no divide-by-zero
	}
	for _, tc := range cases {
		bar := renderScoreBar(tc.score, tc.max)
		if got := utf8.RuneCountInString(bar); got != 32 { // 30 cells + 2 brackets
			t.Errorf("score=%d max=%d: bar length %d runes, want 32", tc.score, tc.max, got)
		}
		if got := strings.Count(bar, "█"); got != tc.filled {
			t.Errorf("score=%d max=%d: %d filled cells, want %d (%q)", tc.score, tc.max, got, tc.filled, bar)
		}
		if got := strings.Count(bar, "░"); got != 30-tc.filled {
			t.Errorf("score=%d max=%d: %d empty cells, want %d", tc.score, tc.max, got, 30-tc.filled)
		}
	}
}

func TestComponentStatus(t *testing.T) {
	cases := []struct {
		score, max int
		want       string
	}{
		{90, 100, "OK"},
		{89, 100, "Good"},
		{75, 100, "Good"},
		{74, 100, "Warning"},
		{60, 100, "Warning"},
		{59, 100, "Critical"},
		{0, 100, "Critical"},
		{0, 0, "N/A"},
		{7, 0, "N/A"}, // zero max must not divide by zero
	}
	for _, tc := range cases {
		if got := componentStatus(tc.score, tc.max); got != tc.want {
			t.Errorf("componentStatus(%d,%d) = %q, want %q", tc.score, tc.max, got, tc.want)
		}
	}
}

func TestFormatDeductions(t *testing.T) {
	if got := formatDeductions(nil); got != "" {
		t.Errorf("formatDeductions(nil) = %q, want empty", got)
	}
	one := []health.Deduction{{Rule: "cpu_usage_high", Penalty: 10}}
	if got := formatDeductions(one); got != "cpu_usage_high (-10)" {
		t.Errorf("formatDeductions one = %q", got)
	}
	two := []health.Deduction{{Rule: "a", Penalty: 10}, {Rule: "b", Penalty: 2.75}}
	if got := formatDeductions(two); got != "a (-10); b (-3)" {
		t.Errorf("formatDeductions two = %q", got)
	}
}

func TestIsCollectorEnabled(t *testing.T) {
	cfg := &config.Config{
		Collectors: map[string]config.CollectorCfg{
			"cpu": {Enabled: false},
			"gpu": {Enabled: true},
		},
	}
	if isCollectorEnabled(cfg, "cpu") {
		t.Error("cpu explicitly disabled")
	}
	if !isCollectorEnabled(cfg, "gpu") {
		t.Error("gpu explicitly enabled")
	}
	if !isCollectorEnabled(cfg, "memory") {
		t.Error("collector absent from config must default to enabled")
	}
}

func TestGetOutputFormat(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"catmonitor"}, "json"},
		{[]string{"catmonitor", "collect"}, "json"},
		{[]string{"catmonitor", "collect", "-o", "table"}, "table"},
		{[]string{"catmonitor", "collect", "--output", "table"}, "table"},
		{[]string{"catmonitor", "collect", "--output", "json"}, "json"},
		{[]string{"catmonitor", "collect", "-o"}, "json"}, // flag without value
		{[]string{"catmonitor", "-o", "table", "extra"}, "table"},
	}
	for _, tc := range cases {
		os.Args = tc.args
		if got := getOutputFormat(); got != tc.want {
			t.Errorf("getOutputFormat(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestPrintMetricsJSON(t *testing.T) {
	ts := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	in := []collector.Metric{
		{Component: "cpu", Name: "usage", Value: 42.5, Unit: "%", Labels: map[string]string{"core": "total"}, Timestamp: ts},
	}
	out := captureStdout(t, func() { printMetricsJSON(in) })
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("printed %d lines, want 1", len(lines))
	}
	var m collector.Metric
	if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, lines[0])
	}
	if m.Component != "cpu" || m.Name != "usage" || m.Value != 42.5 || m.Labels["core"] != "total" {
		t.Errorf("round trip lost fields: %+v", m)
	}
}

func TestPrintMetricsTable(t *testing.T) {
	in := []collector.Metric{
		{Component: "cpu", Name: "usage", Value: 42.5, Unit: "%", Labels: map[string]string{"core": "total"}},
		{Component: "npu", Name: "temperature", Value: 41, Unit: "°C"},
	}
	out := captureStdout(t, func() { printMetricsTable(in) })
	for _, want := range []string{"Component", "cpu", "usage", "42.50", "core=total", "npu", "temperature", "41.00"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintHealthTable(t *testing.T) {
	score := health.HealthScore{
		Score:      85,
		Grade:      "Good",
		ServerType: "cpu_only",
		Timestamp:  time.Date(2026, 9, 7, 12, 30, 0, 0, time.UTC),
		Components: map[string]health.ComponentScore{
			"cpu":    {Score: 8, Max: 10, Deductions: []health.Deduction{{Rule: "cpu_usage_high", Penalty: 2}}},
			"memory": {Score: 18, Max: 20},
		},
	}
	out := captureStdout(t, func() { printHealthTable(score) })
	for _, want := range []string{
		"CATMonitor Health Report",
		"85 / 100",
		"Good",
		"CPU",
		"MEMORY",
		"cpu_usage_high (-2)",
		"minor issues",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("health report missing %q:\n%s", want, out)
		}
	}

	critical := health.HealthScore{
		Score:     40,
		Grade:     "Critical",
		Timestamp: time.Now(),
		Components: map[string]health.ComponentScore{
			"cpu": {Score: 2, Max: 10},
		},
	}
	outCrit := captureStdout(t, func() { printHealthTable(critical) })
	if !strings.Contains(outCrit, "Critical issues detected") {
		t.Errorf("critical report missing the alert line:\n%s", outCrit)
	}
}

func TestPrintHealthJSON(t *testing.T) {
	score := health.HealthScore{
		Score:      85,
		Grade:      "Good",
		ServerType: "cpu_only",
		Timestamp:  time.Date(2026, 9, 7, 12, 30, 0, 0, time.UTC),
		Components: map[string]health.ComponentScore{
			"cpu": {Score: 8, Max: 10, Deductions: []health.Deduction{{Rule: "cpu_usage_high", Penalty: 2}}},
		},
	}
	out := captureStdout(t, func() { printHealthJSON(score) })
	var got health.HealthScore
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, out)
	}
	if got.Score != 85 || got.Grade != "Good" || got.ServerType != "cpu_only" {
		t.Errorf("round trip lost top-level fields: %+v", got)
	}
	comp, ok := got.Components["cpu"]
	if !ok {
		t.Fatalf("cpu component missing: %+v", got.Components)
	}
	if comp.Score != 8 || comp.Max != 10 || len(comp.Deductions) != 1 || comp.Deductions[0].Rule != "cpu_usage_high" {
		t.Errorf("cpu component round trip: %+v", comp)
	}
}

func TestPrintUsage(t *testing.T) {
	out := captureStdout(t, printUsage)
	for _, want := range []string{"Usage:", "daemon", "collect", "health", "stress", "list", "version"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}

// runList prints the registered collectors; the blank imports in main.go
// register all seven production collectors.
func TestRunList(t *testing.T) {
	out := captureStdout(t, runList)
	if !strings.Contains(out, "Name") {
		t.Errorf("list output missing header:\n%s", out)
	}
	for _, name := range []string{"cpu", "memory", "disk", "gpu", "npu", "network", "chassis"} {
		if !strings.Contains(out, name) {
			t.Errorf("list output missing collector %q:\n%s", name, out)
		}
	}
}
