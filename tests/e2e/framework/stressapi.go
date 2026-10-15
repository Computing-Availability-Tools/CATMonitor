//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// StressAPI is a black-box client for the stress endpoints the web binary
// proxies through the daemon's control socket. The POST endpoints are
// guarded by three soft defenses (Content-Type, X-CATMonitor-Action
// header, same-origin); the REAL authorization boundary is the control
// socket's file permission (0660, covered by TC-031).
type StressAPI struct{ base string }

// NewStressAPI binds a client to a web instance's stress endpoints.
func NewStressAPI() *StressAPI { return &StressAPI{base: "http://" + webAddr} }

func (c *StressAPI) do(t *testing.T, method, path, contentType, action string, body []byte) (int, string) {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if action != "" {
		req.Header.Set("X-CATMonitor-Action", action)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

// Config fetches GET /api/stress/config. Returns (status, body).
func (c *StressAPI) Config(t *testing.T) (int, string) {
	t.Helper()
	return c.do(t, http.MethodGet, "/api/stress/config", "", "", nil)
}

// StartRun POSTs a start request with all defense headers present.
func (c *StressAPI) StartRun(t *testing.T, benchmarks []string, timeoutSeconds int64) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"benchmarks":      benchmarks,
		"timeout_seconds": timeoutSeconds,
	})
	return c.do(t, http.MethodPost, "/api/stress/runs", "application/json", "stress", body)
}

// StartRunNoAction POSTs with the action header missing (defense probe).
func (c *StressAPI) StartRunNoAction(t *testing.T) (int, string) {
	t.Helper()
	return c.do(t, http.MethodPost, "/api/stress/runs", "application/json", "", []byte(`{"benchmarks":[]}`))
}

// StartRunWrongContentType POSTs with a non-JSON content type.
func (c *StressAPI) StartRunWrongContentType(t *testing.T) (int, string) {
	t.Helper()
	return c.do(t, http.MethodPost, "/api/stress/runs", "text/plain", "stress", []byte(`{"benchmarks":[]}`))
}

// StartRunForeignOrigin POSTs with a foreign Origin header.
func (c *StressAPI) StartRunForeignOrigin(t *testing.T) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, c.base+"/api/stress/runs", bytes.NewReader([]byte(`{"benchmarks":[]}`)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CATMonitor-Action", "stress")
	req.Header.Set("Origin", "http://evil.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

// WaitEndpoint polls GET /api/stress/config until it answers (any status).
func (c *StressAPI) WaitEndpoint(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, c.base+"/api/stress/config", nil)
		if err == nil {
			if resp, err := http.DefaultClient.Do(req); err == nil {
				resp.Body.Close()
				return
			}
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("web stress endpoints did not answer within %s", timeout)
}
