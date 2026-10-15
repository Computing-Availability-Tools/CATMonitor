//go:build linux

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/snapshot"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"log/slog"
)

// TestWebReadOnlyDir verifies web's read-only contract on the snapshot
// directory: with the directory chmod 0555 (read-only for everyone), all
// web API endpoints still serve correctly — proving web never writes to
// the snapshot directory it consumes.
//
// Design ref: web Excel TC-018 (P0, 只读契约-目录只读可运行).
func TestWebReadOnlyDir(t *testing.T) {
	dir := t.TempDir()

	// Produce a realistic snapshot layout: global + one per-component file.
	now := time.Now()
	g := &snapshot.GlobalSnapshot{SessionID: "ro-test", Timestamp: now, RefreshInterval: 3000}
	if err := snapshot.WriteJSONAtomic(filepath.Join(dir, "snapshot.json"), g); err != nil {
		t.Fatalf("write global snapshot: %v", err)
	}
	comp := &snapshot.CompSnapshot{
		Component: "cpu", Timestamp: now,
		Metrics: []collector.Metric{
			{Component: "cpu", Name: "usage", Value: 42, Labels: map[string]string{"core": "total"}, Timestamp: now},
		},
		History: map[string][]float64{"usage": {40, 41, 42}},
	}
	if err := snapshot.WriteJSONAtomic(filepath.Join(dir, "snapshot_cpu.json"), comp); err != nil {
		t.Fatalf("write cpu snapshot: %v", err)
	}

	// Make the directory read-only — web must still function.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod 555: %v", err)
	}
	// Restore permissions so t.TempDir cleanup can remove the files.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	server := NewServer(dir, slog.Default(), nil, ":19322")

	tests := []struct {
		name   string
		target string
		check  func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:   "snapshot serves data",
			target: "/api/snapshot",
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if rec.Code != http.StatusOK {
					t.Fatalf("/api/snapshot: status %d, body %s", rec.Code, rec.Body.String())
				}
				var snap map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
					t.Fatalf("/api/snapshot: decode: %v", err)
				}
				if snap["session_id"] != "ro-test" {
					t.Errorf("/api/snapshot: session_id=%v, want ro-test", snap["session_id"])
				}
			},
		},
		{
			name:   "collectors metadata",
			target: "/api/collectors",
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if rec.Code != http.StatusOK {
					t.Errorf("/api/collectors: status %d", rec.Code)
				}
			},
		},
		{
			name:   "config view",
			target: "/api/config",
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if rec.Code != http.StatusOK {
					t.Errorf("/api/config: status %d", rec.Code)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			rec := httptest.NewRecorder()
			server.Routes().ServeHTTP(rec, req)
			tc.check(t, rec)
		})
	}
}
