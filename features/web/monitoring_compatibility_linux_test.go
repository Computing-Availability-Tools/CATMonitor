//go:build linux

package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/snapshot"
	"github.com/Computing-Availability-Tools/CATMonitor/features/stress"
)

func TestLegacyWebFlags(t *testing.T) {
	options, err := parseWebOptions([]string{
		"-addr=:19322",
		"-snapshot-dir=/var/lib/catmonitor/snapshot",
		"-config=/etc/catmonitor/catmonitor.yaml",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.addr != ":19322" || options.snapshotDir != "/var/lib/catmonitor/snapshot" {
		t.Fatalf("unexpected monitoring options: %+v", options)
	}
	if options.legacyConfig != "/etc/catmonitor/catmonitor.yaml" {
		t.Fatalf("legacy -config was not accepted: %+v", options)
	}
}

func TestWebWithoutControlSocket(t *testing.T) {
	dir := t.TempDir()
	global := snapshot.GlobalSnapshot{
		SessionID:       "monitoring-only",
		Timestamp:       time.Now(),
		RefreshInterval: 3000,
	}
	data, err := json.Marshal(global)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	client, err := stress.NewControlClient(filepath.Join(dir, "missing-control.sock"))
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(dir, slog.Default(), client, ":19322").Routes()

	monitoring := httptest.NewRecorder()
	handler.ServeHTTP(monitoring, httptest.NewRequest(http.MethodGet, "/api/snapshot", nil))
	if monitoring.Code != http.StatusOK {
		t.Fatalf("monitoring status=%d body=%s", monitoring.Code, monitoring.Body.String())
	}

	stressResponse := httptest.NewRecorder()
	handler.ServeHTTP(stressResponse, httptest.NewRequest(http.MethodGet, "/api/stress/config", nil))
	if stressResponse.Code != http.StatusOK {
		t.Fatalf("stress status=%d body=%s, want 200", stressResponse.Code, stressResponse.Body.String())
	}
	var stressConfig map[string]any
	if err := json.Unmarshal(stressResponse.Body.Bytes(), &stressConfig); err != nil {
		t.Fatal(err)
	}
	if stressConfig["enabled"] != false || stressConfig["available"] != false {
		t.Fatalf("unexpected monitoring-only Stress config: %v", stressConfig)
	}

	runRequest := httptest.NewRequest(http.MethodPost, "/api/stress/runs", strings.NewReader(`{"benchmarks":["stream"]}`))
	runRequest.Header.Set("Content-Type", "application/json")
	runRequest.Header.Set("X-CATMonitor-Action", "stress")
	runResponse := httptest.NewRecorder()
	handler.ServeHTTP(runResponse, runRequest)
	if runResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("run status=%d body=%s, want 503", runResponse.Code, runResponse.Body.String())
	}

	cancelRequest := httptest.NewRequest(http.MethodPost, "/api/stress/runs/missing/cancel", strings.NewReader(`{}`))
	cancelRequest.Header.Set("Content-Type", "application/json")
	cancelRequest.Header.Set("X-CATMonitor-Action", "stress")
	cancelResponse := httptest.NewRecorder()
	handler.ServeHTTP(cancelResponse, cancelRequest)
	if cancelResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("cancel status=%d body=%s, want 503", cancelResponse.Code, cancelResponse.Body.String())
	}
}
