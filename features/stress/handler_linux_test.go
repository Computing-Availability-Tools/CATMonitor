//go:build linux

package stress

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func startControlFixture(t *testing.T, manager *Manager) (*ControlClient, context.CancelFunc) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.sock")
	server, err := ListenControl(path, manager, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	client, err := NewControlClient(path)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("control server shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("control server did not stop")
		}
	})
	return client, cancel
}

func TestUnifiedWebListenerProvidesReadAndMutationRoutes(t *testing.T) {
	manager := testManager(t, &fakeExecutor{})
	client, _ := startControlFixture(t, manager)
	mux := http.NewServeMux()
	Register(mux, client, ":19322", nil)

	configResponse := httptest.NewRecorder()
	mux.ServeHTTP(configResponse, httptest.NewRequest(http.MethodGet, "/api/stress/config", nil))
	if configResponse.Code != http.StatusOK {
		t.Fatalf("config status=%d body=%s", configResponse.Code, configResponse.Body.String())
	}
	var view map[string]any
	if err := json.Unmarshal(configResponse.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view["operator"] != true || view["enabled"] != true || view["security_debt_web_operator_auth"] != true {
		t.Fatalf("unified listener policy mismatch: %v", view)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/stress/runs", bytes.NewBufferString(`{"benchmarks":["stream"]}`))
	request.RemoteAddr = "192.0.2.1:41000"
	request.Host = "node.example:19322"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CATMonitor-Action", actionHeader)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("POST status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestUnifiedWebListenerRejectsCrossOriginMutation(t *testing.T) {
	manager := testManager(t, &fakeExecutor{})
	client, _ := startControlFixture(t, manager)
	mux := http.NewServeMux()
	Register(mux, client, ":19322", nil)
	req := httptest.NewRequest(http.MethodPost, "/api/stress/runs", bytes.NewBufferString(`{"benchmarks":["stream"]}`))
	req.RemoteAddr, req.Host = "192.0.2.1:4000", "node.example:19322"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CATMonitor-Action", actionHeader)
	req.Header.Set("Origin", "http://evil.example")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
