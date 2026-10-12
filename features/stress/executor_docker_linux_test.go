//go:build linux

package stress

import (
	"context"
	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNPUDescribeRecordsImmutableRuntimeIdentity(t *testing.T) {
	dir := t.TempDir()
	id, imageID := strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64)
	binary := filepath.Join(dir, "docker")
	script := "#!/bin/sh\nshift 2\ncase $1 in\ninspect) printf '%s\\n' '{\"id\":\"" + id + "\",\"image\":\"" + imageID + "\"}' ;;\nexec)\n" +
		"test \"$2\" = '" + id + "' || exit 7\n" +
		"printf '%s\\n' '{\"protocol_version\":1,\"benchmark\":\"npu_burn\",\"preflight\":{\"status\":\"pass\"},\"mpi\":{\"status\":\"pass\",\"implementation\":\"none\",\"executable_abi\":\"none\"}}' ;;\n*) exit 8 ;;\nesac\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	e := &DockerExecExecutor{binary: binary, socket: "/not-used.sock", apiVersion: "1.39"}
	p, err := e.Describe(context.Background(), Binding{Benchmark: "npu_burn", Plugin: "npu_burn", Container: "npu"})
	if err != nil {
		t.Fatal(err)
	}
	if parameterValue(p, "workload_container_id") != id || parameterValue(p, "workload_image_id") != imageID || p.Container != "npu" {
		t.Fatalf("identity missing: %+v", p)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' '{\"id\":\"npu\",\"image\":\"latest\"}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Describe(context.Background(), Binding{Benchmark: "npu_burn", Plugin: "npu_burn", Container: "npu"}); err == nil {
		t.Fatal("mutable aliases accepted as evidence identity")
	}
}

func TestDockerNPUSelectionUsesOnlyTypedEnvironment(t *testing.T) {
	e := &DockerExecExecutor{socket: "/run/docker.sock"}
	b := Binding{Benchmark: "npu_burn", Plugin: "npu_burn", Container: "npu", NPUDevices: workloadapi.DeviceIDs{1, 0}}
	if err := validateBinding(b); err != nil {
		t.Fatal(err)
	}
	args := e.workloadArgs(b, false, "describe", "--json")
	got := strings.Join(args, " ")
	if !strings.Contains(got, "--env NPU_BURN_DEVICE=1,0 --env CATMONITOR_NPU_REQUIRE_IDLE=1 npu ") {
		t.Fatal(got)
	}
	if strings.Contains(got, " sh ") || strings.Contains(got, "bash") {
		t.Fatal("shell transport introduced")
	}
	b.NPUCases = workloadapi.CaseNames{"matmul", "conv2d"}
	got = strings.Join(e.workloadArgs(b, true, "run"), " ")
	if !strings.Contains(got, "--env NPU_BURN_RUN_CASE=matmul,conv2d --env NPU_BURN_GROUP= npu ") {
		t.Fatal(got)
	}
	b.NPUCases = workloadapi.CaseNames{"matmul;id"}
	if err := validateBinding(b); err == nil {
		t.Fatal("invalid operator accepted")
	}
	b.NPUCases = nil
	b.NPUDevices = workloadapi.DeviceIDs{1, 1}
	if err := validateBinding(b); err == nil {
		t.Fatal("duplicate accepted")
	}
}

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
