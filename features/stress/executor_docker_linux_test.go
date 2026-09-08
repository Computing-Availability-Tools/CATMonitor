//go:build linux

package stress

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerCommandEnvNegotiatesDaemonAPIVersion(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ApiVersion":"1.39"}`))
	})}
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})
	go func() { _ = server.Serve(listener) }()

	t.Setenv("DOCKER_API_VERSION", "1.54")
	executor, err := NewDockerExecExecutor(ExecutorConfig{DockerSocket: socket, DockerBinary: "/usr/bin/docker"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := executor.dockerCommandEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range env {
		if strings.HasPrefix(item, "DOCKER_API_VERSION=") {
			count++
			if item != "DOCKER_API_VERSION=1.39" {
				t.Fatalf("Docker API environment=%q, want negotiated 1.39", item)
			}
		}
	}
	if count != 1 {
		t.Fatalf("Docker API environment count=%d, want 1", count)
	}
}

func TestDockerAPIVersionRejectsInvalidResponse(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ApiVersion":"latest"}`))
	})}
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})
	go func() { _ = server.Serve(listener) }()

	executor, err := NewDockerExecExecutor(ExecutorConfig{DockerSocket: socket, DockerBinary: "/usr/bin/docker"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.dockerAPIVersion(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid API version") {
		t.Fatalf("dockerAPIVersion error=%v, want invalid API version", err)
	}
}
