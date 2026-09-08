//go:build linux

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress"
)

func startWebControlFixture(t *testing.T) *stress.ControlClient {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "control.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/stress/config" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(stress.ControlConfigView{
			Enabled: true, FeatureEnabled: true, WebEnabled: true, Platform: "linux", Executor: "docker_exec", SharedReport: true,
			DefaultBenchmarks: []string{"stream"},
		})
	})}
	done := make(chan struct{})
	go func() { _ = server.Serve(listener); close(done) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		<-done
	})
	client, err := stress.NewControlClient(socket)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestWebMountsUnifiedStressView(t *testing.T) {
	client := startWebControlFixture(t)
	server := NewServer(t.TempDir(), slog.Default(), client, ":19322")
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/stress/config", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["enabled"] != true || body["operator"] != true || body["security_debt_web_operator_auth"] != true {
		t.Fatalf("unexpected stress policy: %v", body)
	}
}
