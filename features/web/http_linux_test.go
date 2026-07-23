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

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"

	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/cpu"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/disk"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/gpu"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/memory"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/network"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/npu"
)

// TestHTTPAPISmoke drives the real HTTP layer end-to-end (in-process, no
// daemon): collects twice, serves via httptest, and exercises every documented
// route, asserting the v0.2.0 adaptation is visible through the API.
func TestHTTPAPISmoke(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Collector: CollectorCfg{RefreshInterval: 3 * time.Second, HistoryPoints: 60},
		Storage:   StorageCfg{SnapshotPath: filepath.Join(dir, "snapshot.json")},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dc := NewDataCollector(cfg, logger)
	// Seed a static spec so the HTTP layer has specs to serve regardless of
	// collector singleton state: other tests in this binary may have already
	// tripped the collectors' one-shot flags, so collectOnce may emit no
	// statics here. collectOnce preserves a pre-seeded stash when the current
	// cycle yields no static metrics.
	dc.staticStash = []collector.Metric{
		{Component: "cpu", Name: "model_info", Value: 4, Labels: map[string]string{"model_name": "TestCPU Xeon"}, Timestamp: time.Now()},
		{Component: "cpu", Name: "max_freq", Value: 2400, Unit: "MHz", Timestamp: time.Now()},
	}
	// Simulate the startup hardware-specs collection (hwinfo.go) with synthetic
	// deterministic data — the real collectHWSpecs is host-dependent.
	dc.SetHWSpecs([]collector.Metric{
		{Component: "system", Name: "device_model", Value: 1, Labels: map[string]string{"manufacturer": "TestCo", "product_name": "TestBox"}},
		{Component: "disk", Name: "disk_info", Value: 500, Unit: "GB", Labels: map[string]string{"device": "sda", "model": "TestSSD"}},
	})

	// Two cycles so delta-based metrics have a previous snapshot.
	dc.collectOnce()
	dc.collectOnce()

	srv := NewServer(cfg, dc, logger)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()
	client := ts.Client()

	// GET /api/collectors: 6 collectors, all expected components present.
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
		t.Errorf("collectors count=%d want 7 (system is not a periodic collector)", len(comps))
	}
	have := map[string]bool{}
	for _, c := range comps {
		have[c.Component] = true
	}
	for _, want := range []string{"chassis", "cpu", "memory", "disk", "gpu", "npu", "network"} {
		if !have[want] {
			t.Errorf("collector %q missing from /api/collectors", want)
		}
	}
	if have["system"] {
		t.Error("system should NOT be a registered collector (decoupled to hwinfo.go)")
	}

	// GET /api/snapshot: valid shape, /proc-backed series present, all history
	// keys honor the <component>_ prefix the frontend groups on.
	body, code = get(t, client, ts.URL+"/api/snapshot")
	if code != 200 {
		t.Fatalf("snapshot status=%d want 200", code)
	}
	var snap Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snap.Timestamp.IsZero() || len(snap.Metrics) == 0 {
		t.Error("snapshot missing timestamp or metrics")
	}
	for _, key := range []string{"cpu_usage", "cpu_load_average", "memory_usage"} {
		if arr, ok := snap.History[key]; !ok || len(arr) == 0 {
			t.Errorf("snapshot history[%q] missing/empty", key)
		}
	}
	for k := range snap.History {
		ok := false
		for _, comp := range []string{"cpu", "memory", "disk", "gpu", "npu", "network"} {
			p := comp + "_"
			if len(k) > len(p) && k[:len(p)] == p {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("history key %q lacks <component>_ prefix", k)
		}
	}

	// Specs field: stashed cpu/memory statics + startup hardware identity.
	if len(snap.Specs) == 0 {
		t.Error("snapshot.specs empty (static stash + hwSpecs not served over HTTP)")
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
		t.Error("snapshot.specs missing model_info (stashed static not served)")
	}
	if !hasDeviceModel {
		t.Error("snapshot.specs missing device_model (startup hwSpecs not served)")
	}

	// GET /api/config: reflects the configured interval/depth.
	body, code = get(t, client, ts.URL+"/api/config")
	if code != 200 {
		t.Fatalf("config status=%d want 200", code)
	}
	var cfgResp map[string]int
	if err := json.Unmarshal(body, &cfgResp); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfgResp["refresh_interval_ms"] != 3000 {
		t.Errorf("config refresh_interval_ms=%d want 3000", cfgResp["refresh_interval_ms"])
	}
	if cfgResp["history_points"] != 60 {
		t.Errorf("config history_points=%d want 60", cfgResp["history_points"])
	}

	// POST /api/config: hot-update interval; bad value rejected.
	post := func(t *testing.T, path string, payload string) (int, string) {
		t.Helper()
		resp, err := client.Post(ts.URL+path, "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("post %s: %v", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	code, _ = post(t, "/api/config", `{"refresh_interval_ms":2000}`)
	if code != 200 {
		t.Errorf("config POST 2000ms status=%d want 200", code)
	}
	if dc.Interval() != 2*time.Second {
		t.Errorf("interval not hot-updated: got %v want 2s", dc.Interval())
	}
	code, _ = post(t, "/api/config", `{"refresh_interval_ms:999}`) // malformed JSON
	if code != 400 {
		t.Errorf("config POST bad-json status=%d want 400", code)
	}
	code, _ = post(t, "/api/config", `{"refresh_interval_ms":500}`) // below minimum
	if code != 400 {
		t.Errorf("config POST 500ms status=%d want 400", code)
	}

	// POST /api/refresh: immediate collection accepted.
	code, _ = post(t, "/api/refresh", "")
	if code != 200 {
		t.Errorf("refresh status=%d want 200", code)
	}

	// GET /: SPA shell served as HTML.
	body, code = get(t, client, ts.URL+"/")
	if code != 200 {
		t.Fatalf("index status=%d want 200", code)
	}
	if !strings.Contains(string(body), "CATMonitor") && !strings.Contains(string(body), "catmonitor") {
		t.Error("index.html missing app title")
	}

	// GET /static/app.js: the shipped frontend must carry the v0.2.0 trend
	// labels we added (proves the adapted static bundle is embedded).
	body, code = get(t, client, ts.URL+"/static/app.js")
	if code != 200 {
		t.Fatalf("app.js status=%d want 200", code)
	}
	js := string(body)
	for _, needle := range []string{"cpu_temperature:", "cpu_power:", "memory_saturation:", "disk_iops:", "network_throughput:", "fragmentation:"} {
		if !strings.Contains(js, needle) {
			t.Errorf("static app.js missing %q (v0.2.0 adaptation not embedded)", needle)
		}
	}
	for _, needle := range []string{"device_model", "gpu_info", "npu_info", "disk_info", "net_info", "SPEC_DEFS", "openSpecsModal", "点击查看完整规格", "memoryTotalMB"} {
		if !strings.Contains(js, needle) {
			t.Errorf("static app.js missing %q (hardware-specs adaptation not embedded)", needle)
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
