package exporter

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

type serveMemStorage struct{ metrics []collector.Metric }

func (m *serveMemStorage) Write(ms []collector.Metric) error {
	m.metrics = append(m.metrics, ms...)
	return nil
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return "127.0.0.1:" + strconv.Itoa(port)
}

// ServeMetrics exposes /metrics, /-/healthy and /-/ready with the documented
// status semantics.
func TestServeMetricsEndpoints(t *testing.T) {
	store := NewCachingStorage(&serveMemStorage{})
	addr := freePort(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	go ServeMetrics(addr, store, logger)

	base := "http://" + addr
	client := &http.Client{Timeout: 2 * time.Second}

	// Wait for the server to come up.
	var resp *http.Response
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		resp, err = client.Get(base + "/-/healthy")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if resp == nil {
		t.Fatalf("exporter did not come up on %s", addr)
	}
	resp.Body.Close()

	// Before the first write: ready must be 503, /metrics empty.
	resp, err := client.Get(base + "/-/ready")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("/-/ready before first write = %d, want 503", resp.StatusCode)
	}
	resp, err = client.Get(base + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q", ct)
	}
	if body := readAll(t, resp); body != "" {
		t.Errorf("/metrics before first write = %q, want empty", body)
	}

	// After a write: ready 200 and the metric appears.
	ts := time.Now()
	if err := store.Write([]collector.Metric{
		{Component: "cpu", Name: "usage", Value: 42.5, Unit: "%", Labels: map[string]string{"core": "total"}, Timestamp: ts},
		{Component: "network", Name: "rx_bytes_total", Value: 1024, Labels: map[string]string{"interface": "eth0"}, Timestamp: ts},
	}); err != nil {
		t.Fatal(err)
	}

	resp, err = client.Get(base + "/-/ready")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/-/ready after write = %d, want 200", resp.StatusCode)
	}

	resp, err = client.Get(base + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	for _, want := range []string{
		"# HELP catmonitor_cpu_usage cpu/usage",
		"# TYPE catmonitor_cpu_usage gauge",
		`catmonitor_cpu_usage{core="total"} 42.5`,
		"# TYPE catmonitor_network_rx_bytes_total counter",
		`catmonitor_network_rx_bytes_total{interface="eth0"} 1024`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics body missing %q:\n%s", want, body)
		}
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

// The daemon wires JSONL storage inside the cache; both layers stay in sync.
func TestCachingStorageWrapsInner(t *testing.T) {
	inner := &serveMemStorage{}
	store := NewCachingStorage(inner)

	if store.Ready() {
		t.Error("Ready() must be false before any write")
	}
	if err := store.Write([]collector.Metric{{Component: "cpu", Name: "usage", Value: 1, Timestamp: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if !store.Ready() {
		t.Error("Ready() must be true after a write")
	}
	if got := store.AllMetrics(); len(got) != 1 || got[0].Name != "usage" {
		t.Errorf("AllMetrics = %+v", got)
	}
	if len(inner.metrics) != 1 {
		t.Errorf("inner storage got %d metrics, want 1 (cache must delegate)", len(inner.metrics))
	}
}
