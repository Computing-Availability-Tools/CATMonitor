//go:build linux

package runnerapi

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServerDescribeRunAllowlistAndBusy(t *testing.T) {
	temp := t.TempDir()
	started := filepath.Join(temp, "started")
	adapter := filepath.Join(temp, "adapter.sh")
	script := `#!/bin/sh
set -eu
case "$1" in
  describe) printf '{"protocol_version":1,"benchmark":"%s","parameters":[],"resources":{},"assets":[],"mpi":{},"preflight":{"status":"pass","message":"ok"}}\n' "$2" ;;
  stream) echo 'Copy: 1.0 0 0 0'; echo 'Scale: 2.0 0 0 0'; echo 'Add: 3.0 0 0 0'; echo 'Triad: 4.0 0 0 0' ;;
  hpl) touch "` + started + `"; sleep 30 ;;
  hpcg) echo hpcg-ok ;;
  *) exit 9 ;;
esac
`
	if err := os.WriteFile(adapter, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(adapter)
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(temp, "runner.sock")
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(socket, 0660) }()
	waitForPath(t, socket)
	client := unixClient(socket)

	response, err := client.Get("http://runner/v1/benchmarks/stream")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `"benchmark":"stream"`) {
		t.Fatalf("unexpected describe response %d: %s", response.StatusCode, body)
	}
	response, err = client.Get("http://runner/v1/benchmarks/npu_burn")
	if err != nil {
		t.Fatal(err)
	}
	_ = readBody(t, response)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("NPU benchmark unexpectedly accepted: %d", response.StatusCode)
	}

	runBody := bytes.NewBufferString(`{"benchmark":"stream"}`)
	response, err = client.Post("http://runner/v1/runs", "application/json", runBody)
	if err != nil {
		t.Fatal(err)
	}
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Triad: 4.0") {
		t.Fatalf("unexpected run response %d: %s", response.StatusCode, body)
	}

	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://runner/v1/runs", strings.NewReader(`{"benchmark":"hpl"}`))
		response, requestErr := client.Do(request)
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	waitForPath(t, started)
	response, err = client.Post("http://runner/v1/runs", "application/json", strings.NewReader(`{"benchmark":"hpcg"}`))
	if err != nil {
		t.Fatal(err)
	}
	body = readBody(t, response)
	if response.StatusCode != http.StatusConflict || !strings.Contains(body, "already running") {
		t.Fatalf("expected busy response, got %d: %s", response.StatusCode, body)
	}
	cancel()
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled benchmark request did not stop")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}

func TestServerRejectsUnknownFieldsAndUnsafeSocketPath(t *testing.T) {
	temp := t.TempDir()
	adapter := filepath.Join(temp, "adapter.sh")
	if err := os.WriteFile(adapter, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(adapter)
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(temp, "runner.sock")
	done := make(chan error, 1)
	go func() { done <- server.Serve(socket, 0660) }()
	waitForPath(t, socket)
	second, err := NewServer(adapter)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Serve(socket, 0660); err == nil || !strings.Contains(err.Error(), "already served") {
		t.Fatalf("active socket was unexpectedly replaced: %v", err)
	}
	client := unixClient(socket)
	response, err := client.Post("http://runner/v1/runs", "application/json", strings.NewReader(`{"benchmark":"stream","command":"id"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = readBody(t, response)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown field unexpectedly accepted: %d", response.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	<-done

	regular := filepath.Join(temp, "not-a-socket")
	if err := os.WriteFile(regular, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	server, err = NewServer(adapter)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Serve(regular, 0660); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("expected non-socket refusal, got %v", err)
	}
	content, err := os.ReadFile(regular)
	if err != nil || string(content) != "preserve" {
		t.Fatalf("non-socket path was modified: %q, %v", content, err)
	}
}

func unixClient(socket string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}}
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("path did not become ready: %s", path)
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
