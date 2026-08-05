//go:build linux

package stress

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandlerOwnsStandaloneUIAndAPI(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "benchmark_check.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Enabled: true, WebEnabled: true, ScriptPath: script,
		ReportPath:        filepath.Join(dir, "stress-latest.json"),
		DefaultBenchmarks: []string{"stream"},
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Minute},
		},
	}
	manager := NewManager(cfg)
	mux := http.NewServeMux()
	Register(mux, manager, "127.0.0.1:9527", slog.Default())
	server := httptest.NewServer(mux)
	defer server.Close()

	body, status := handlerGet(t, server.Client(), server.URL+"/stress/")
	if status != http.StatusOK ||
		!strings.Contains(string(body), "CATMonitor 可靠性压测") ||
		!strings.Contains(string(body), "本次执行参数与资源规模") {
		t.Fatalf("standalone UI status=%d body=%s", status, body)
	}
	body, status = handlerGet(t, server.Client(), server.URL+"/api/stress/config")
	if status != http.StatusOK {
		t.Fatalf("config status=%d body=%s", status, body)
	}
	var apiConfig struct {
		Enabled    bool `json:"enabled"`
		Benchmarks []struct {
			Name    string            `json:"name"`
			Profile *ExecutionProfile `json:"profile"`
		} `json:"benchmarks"`
	}
	if err := json.Unmarshal(body, &apiConfig); err != nil || !apiConfig.Enabled ||
		len(apiConfig.Benchmarks) != 1 || apiConfig.Benchmarks[0].Profile == nil {
		t.Fatalf("config response=%s err=%v", body, err)
	}

	withoutAction, err := http.NewRequest(http.MethodPost, server.URL+"/api/stress/runs", strings.NewReader(`{"benchmarks":["stream"]}`))
	if err != nil {
		t.Fatal(err)
	}
	withoutAction.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(withoutAction)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("missing action header status=%d", response.StatusCode)
	}

	legacyAction, err := http.NewRequest(http.MethodPost, server.URL+"/api/stress/runs", strings.NewReader(`{"benchmarks":["stream"]}`))
	if err != nil {
		t.Fatal(err)
	}
	legacyAction.Header.Set("Content-Type", "application/json")
	legacyAction.Header.Set("X-CATMonitor-Action", "health-stress")
	response, err = server.Client().Do(legacyAction)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("unpublished legacy action status=%d", response.StatusCode)
	}

	_, status = handlerGet(t, server.Client(), server.URL+"/api/health/stress/config")
	if status != http.StatusNotFound {
		t.Fatalf("unpublished legacy API status=%d", status)
	}

	start := handlerMutation(t, server.URL+"/api/stress/runs", `{"benchmarks":["stream"],"timeout_seconds":1}`)
	response, err = server.Client().Do(start)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("start status=%d body=%s", response.StatusCode, body)
	}
	var report Report
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		body, status = handlerGet(t, server.Client(), server.URL+"/api/stress/runs/"+report.JobID)
		if status != http.StatusOK {
			t.Fatalf("job status=%d body=%s", status, body)
		}
		if err := json.Unmarshal(body, &report); err != nil {
			t.Fatal(err)
		}
		if report.Status != StatusRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if report.Status != StatusHealthy || report.Benchmarks[0].Status != StatusTimeLimitReached {
		t.Fatalf("time-limited report=%+v", report)
	}
	body, status = handlerGet(t, server.Client(), server.URL+"/api/stress/history?limit=10")
	if status != http.StatusOK {
		t.Fatalf("history status=%d body=%s", status, body)
	}
	var history []Report
	if err := json.Unmarshal(body, &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].JobID != report.JobID || history[0].Cancellable {
		t.Fatalf("unexpected history response: %+v", history)
	}

	body, status = handlerGet(t, server.Client(), server.URL+"/api/stress/history?limit=101")
	if status != http.StatusBadRequest {
		t.Fatalf("invalid history limit status=%d body=%s", status, body)
	}
}

func handlerMutation(t *testing.T, url, body string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CATMonitor-Action", "stress")
	return request
}

func handlerGet(t *testing.T, client *http.Client, url string) ([]byte, int) {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body, response.StatusCode
}
