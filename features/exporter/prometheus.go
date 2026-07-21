package exporter

import (
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// metricPrefix is prepended to all exported Prometheus metric names.
const metricPrefix = "catmonitor_"

// isCounter returns true for cumulative metrics whose names indicate they
// only increase over time (CPU jiffies, network bytes). Prometheus uses this
// type hint so users can apply rate()/increase() in PromQL.
func isCounter(name string) bool {
	return strings.HasSuffix(name, "_time") ||
		strings.HasSuffix(name, "_total")
}

// promName builds the Prometheus metric name: catmonitor_{component}_{name}.
// Special characters (-, /, .) are replaced with _.
func promName(component, name string) string {
	s := metricPrefix + component + "_" + name
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ".", "_")
	return s
}

// formatLabels converts a metric's Labels map to Prometheus label syntax.
// Returns an empty string if the map is empty.
func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+strconv.Quote(labels[k]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// Encode converts a slice of collector.Metrics to Prometheus text format.
// Metrics are grouped by Prometheus name; each group gets one HELP + TYPE
// line followed by data lines.
func Encode(metrics []collector.Metric) []byte {
	var buf strings.Builder

	// Group by prometheus metric name for HELP/TYPE ordering.
	type group struct {
		component string
		name      string
		metrics   []collector.Metric
	}
	groups := make(map[string]*group)
	var order []string

	for _, m := range metrics {
		pn := promName(m.Component, m.Name)
		g, ok := groups[pn]
		if !ok {
			g = &group{component: m.Component, name: m.Name, metrics: nil}
			groups[pn] = g
			order = append(order, pn)
		}
		g.metrics = append(g.metrics, m)
	}

	for _, pn := range order {
		g := groups[pn]
		// HELP line
		buf.WriteString("# HELP " + pn + " " + g.component + "/" + g.name + "\n")
		// TYPE line
		typ := "gauge"
		if isCounter(g.name) {
			typ = "counter"
		}
		buf.WriteString("# TYPE " + pn + " " + typ + "\n")
		// Data lines
		for _, m := range g.metrics {
			buf.WriteString(pn)
			buf.WriteString(formatLabels(m.Labels))
			buf.WriteString(" ")
			buf.WriteString(strconv.FormatFloat(m.Value, 'f', -1, 64))
			buf.WriteString("\n")
		}
	}

	return []byte(buf.String())
}

// ServeMetrics starts an HTTP server exposing the cached metrics in
// Prometheus text format. Blocks until the server stops (intended to run
// in a goroutine alongside the daemon's main loop).
func ServeMetrics(addr string, store *CachingStorage, logger *slog.Logger) {
	mux := http.NewServeMux()

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		metrics := store.AllMetrics()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Write(Encode(metrics))
	})

	mux.HandleFunc("/-/healthy", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/-/ready", func(w http.ResponseWriter, r *http.Request) {
		if store.Ready() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})

	logger.Info("exporter listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("exporter server error", "error", err)
	}
}
