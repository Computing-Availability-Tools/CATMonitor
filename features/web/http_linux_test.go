//go:build linux

package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/health"
	"github.com/Computing-Availability-Tools/CATMonitor/features/snapshot"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// writeGlobal writes a daemon-style global snapshot for testing.
func writeGlobal(t *testing.T, dir string) {
	t.Helper()
	g := &snapshot.GlobalSnapshot{
		SessionID:       "test-session",
		Timestamp:       time.Now(),
		RefreshInterval: 1000,
		Intervals:       map[string]int{"cpu": 1000, "memory": 1000},
		Health:          health.HealthScore{Score: 97, Grade: "Excellent", ServerType: "cpu_only", Components: map[string]health.ComponentScore{"cpu": {Score: 30, Max: 30}}},
		Collectors: []snapshot.CollectorInfo{
			{Name: "chassis", Component: "chassis", Priority: "High", Interval: "3s", Enabled: true},
			{Name: "cpu", Component: "cpu", Priority: "High", Interval: "1s", Enabled: true},
			{Name: "disk", Component: "disk", Priority: "High", Interval: "2s", Enabled: true},
			{Name: "gpu", Component: "gpu", Priority: "High", Interval: "3s", Enabled: true},
			{Name: "memory", Component: "memory", Priority: "High", Interval: "1s", Enabled: true},
			{Name: "network", Component: "network", Priority: "High", Interval: "1s", Enabled: true},
			{Name: "npu", Component: "npu", Priority: "High", Interval: "3s", Enabled: true},
		},
		SystemSpecs: []collector.Metric{
			{Component: "system", Name: "device_model", Value: 1, Labels: map[string]string{"product_name": "TestBox"}},
		},
	}
	if err := snapshot.WriteJSONAtomic(filepath.Join(dir, "snapshot.json"), g); err != nil {
		t.Fatalf("write global: %v", err)
	}
}

func writeComp(t *testing.T, dir, comp string, metrics []collector.Metric, hist map[string][]float64, specs []collector.Metric) {
	t.Helper()
	c := &snapshot.CompSnapshot{Component: comp, Timestamp: time.Now(), Metrics: metrics, History: hist, Specs: specs}
	if err := snapshot.WriteJSONAtomic(filepath.Join(dir, "snapshot_"+comp+".json"), c); err != nil {
		t.Fatalf("write %s: %v", comp, err)
	}
}

// TestHTTPAPIReadOnlyConsumer drives the web HTTP layer against daemon-produced
// snapshot files (no collection in-process): asserts the assembled snapshot,
// collectors, read-only config, and that /api/refresh is gone.
func TestHTTPAPIReadOnlyConsumer(t *testing.T) {
	dir := t.TempDir()
	writeGlobal(t, dir)
	writeComp(t, dir, "cpu",
		[]collector.Metric{
			{Component: "cpu", Name: "usage", Value: 12.3, Unit: "%", Labels: map[string]string{"core": "total"}, Timestamp: time.Now()},
			{Component: "cpu", Name: "model_info", Value: 4, Labels: map[string]string{"model_name": "Xeon"}, Timestamp: time.Now()},
		},
		map[string][]float64{"cpu_usage": {12.3}},
		[]collector.Metric{{Component: "cpu", Name: "model_info", Value: 4, Labels: map[string]string{"model_name": "Xeon"}, Timestamp: time.Now()}},
	)
	writeComp(t, dir, "memory",
		[]collector.Metric{{Component: "memory", Name: "usage", Value: 30, Unit: "%", Timestamp: time.Now()}},
		map[string][]float64{"memory_usage": {30.0}},
		nil,
	)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(dir, logger, nil, "")
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()
	client := ts.Client()

	// GET /api/collectors: 7 collectors from the global snapshot.
	body, code := get(t, client, ts.URL+"/api/collectors")
	if code != 200 {
		t.Fatalf("collectors status=%d want 200", code)
	}
	var comps []struct {
		Component string `json:"component"`
	}
	if err := json.Unmarshal(body, &comps); err != nil {
		t.Fatalf("decode collectors: %v", err)
	}
	if len(comps) != 7 {
		t.Errorf("collectors count=%d want 7", len(comps))
	}

	// GET /api/snapshot: assembled view — health + metrics (cpu+memory) + history + specs (model_info + device_model).
	body, code = get(t, client, ts.URL+"/api/snapshot")
	if code != 200 {
		t.Fatalf("snapshot status=%d want 200", code)
	}
	var snap snapshot.Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snap.Health.Score != 97 {
		t.Errorf("health score=%d want 97", snap.Health.Score)
	}
	if len(snap.Metrics) != 3 {
		t.Errorf("metrics len=%d want 3 (cpu usage+model_info + memory usage)", len(snap.Metrics))
	}
	if arr, ok := snap.History["cpu_usage"]; !ok || len(arr) != 1 || arr[0] != 12.3 {
		t.Errorf("history[cpu_usage]=%v want [12.3]", arr)
	}
	hasModel, hasDeviceModel := false, false
	for _, m := range snap.Specs {
		if m.Name == "model_info" {
			hasModel = true
		}
		if m.Name == "device_model" {
			hasDeviceModel = true
		}
	}
	if !hasModel {
		t.Error("specs missing model_info (from per-comp file)")
	}
	if !hasDeviceModel {
		t.Error("specs missing device_model (from global SystemSpecs)")
	}

	// GET /api/config: read-only, reflects daemon cadence + configured depth.
	body, code = get(t, client, ts.URL+"/api/config")
	if code != 200 {
		t.Fatalf("config status=%d want 200", code)
	}
	var cfgResp map[string]int
	if err := json.Unmarshal(body, &cfgResp); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfgResp["refresh_interval_ms"] != 1000 {
		t.Errorf("refresh_interval_ms=%d want 1000", cfgResp["refresh_interval_ms"])
	}
	if cfgResp["history_points"] != 60 {
		t.Errorf("history_points=%d want 60", cfgResp["history_points"])
	}

	// POST /api/config: read-only now (405).
	post := func(t *testing.T, path, payload string) int {
		t.Helper()
		resp, err := client.Post(ts.URL+path, "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("post %s: %v", path, err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(t, "/api/config", `{"refresh_interval_ms":2000}`); code != http.StatusMethodNotAllowed {
		t.Errorf("config POST status=%d want 405 (read-only)", code)
	}

	// POST /api/refresh: removed route -> 404.
	if code := post(t, "/api/refresh", ""); code != http.StatusNotFound {
		t.Errorf("refresh status=%d want 404 (removed)", code)
	}

	// GET /: SPA shell served as HTML.
	body, code = get(t, client, ts.URL+"/")
	if code != 200 || !strings.Contains(string(body), "CATMonitor") && !strings.Contains(string(body), "catmonitor") {
		t.Errorf("index status=%d body has no title", code)
	}

	// GET /static/app.js: embedded frontend still carries the v0.2.0 labels.
	body, code = get(t, client, ts.URL+"/static/app.js")
	if code != 200 {
		t.Fatalf("app.js status=%d want 200", code)
	}
	for _, needle := range []string{"cpu_temperature:", "memory_saturation:", "disk_iops:", "device_model"} {
		if !strings.Contains(string(body), needle) {
			t.Errorf("app.js missing %q", needle)
		}
	}
}

func get(t *testing.T, c *http.Client, url string) ([]byte, int) {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b, resp.StatusCode
}
