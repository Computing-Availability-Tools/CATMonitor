//go:build linux

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress"
)

func startCLIControlFixture(t *testing.T) (string, func()) {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "control.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/stress/config":
			_ = json.NewEncoder(w).Encode(stress.ControlConfigView{
				Enabled: true, FeatureEnabled: true, WebEnabled: true, Platform: "linux", Executor: "docker_exec", SharedReport: true,
				DefaultBenchmarks: []string{"stream"},
				Benchmarks:        []stress.ControlBenchmarkView{{Name: "stream", Plugin: "stream", Container: "catmonitor-stress-cpu", Enabled: true, Available: true, TimeoutSeconds: 60, Profile: validCLIProfile()}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/stress/jobs":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(stress.Report{JobID: "aabb", Status: stress.StatusRunning, Platform: "linux"})
		case r.Method == http.MethodGet && r.URL.Path == "/stress/jobs/aabb":
			_ = json.NewEncoder(w).Encode(stress.Report{JobID: "aabb", Status: stress.StatusHealthy, Platform: "linux", Benchmarks: []stress.BenchmarkResult{{Name: "stream", Status: stress.StatusHealthy, Values: map[string]float64{"triad_mb_s": 10}}}})
		case r.Method == http.MethodPost && r.URL.Path == "/stress/jobs/aabb/cancel":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	})
	server := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() { _ = server.Serve(listener); close(done) }()
	return socket, func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		<-done
	}
}

func validCLIProfile() *stress.ExecutionProfile {
	return &stress.ExecutionProfile{
		ProtocolVersion: 1, Benchmark: "stream", Executor: "docker_exec", Container: "catmonitor-stress-cpu", Plugin: "stream",
		MPI:       stress.MPICheck{Status: stress.CheckPass, Implementation: "not_required", ExecutableABI: "not_required"},
		Preflight: stress.PreflightResult{Status: stress.CheckPass, Message: "ready"},
	}
}

func writeCLIConfig(t *testing.T, socket string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catmonitor.yaml")
	content := "stress:\n  control_socket: " + socket + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunAndDoctorUseDaemonControlAPI(t *testing.T) {
	socket, stop := startCLIControlFixture(t)
	defer stop()
	config := writeCLIConfig(t, socket)
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"doctor": {[]string{"doctor", "-c", config, "-o", "json"}, `"status": "pass"`},
		"run":    {[]string{"run", "--bench", "stream", "-c", config, "-o", "json"}, `"status": "healthy"`},
		"cancel": {[]string{"cancel", "--job", "aabb", "-c", config}, "Cancellation accepted"},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, nil, &stdout, &stderr); code != 0 {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("stdout=%s, want %q", stdout.String(), tc.want)
			}
		})
	}
}

func TestHelpDoesNotContactDaemon(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"run", "--help"}, nil, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "daemon Stress Controller") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
