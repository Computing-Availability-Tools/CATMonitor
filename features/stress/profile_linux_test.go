//go:build linux

package stress

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDispatcherDescribeStreamIsJSONAndDoesNotLaunchWorkload(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "launched")
	stream := writeExecutable(t, dir, "stream_omp", "#!/bin/sh\ntouch "+shellLiteral(marker)+"\n")
	numactl := writeExecutable(t, dir, "numactl", "#!/bin/sh\ntouch "+shellLiteral(marker)+"\n")
	script := configuredDispatcher(t, dir, map[string]string{
		"STREAM_EXECUTABLE": stream,
		"STREAM_NUMACTL":    numactl,
		"STREAM_THREADS":    "32",
	})

	output, err := exec.Command("bash", script, "describe", "stream").Output()
	if err != nil {
		t.Fatal(err)
	}
	var profile ExecutionProfile
	if err := json.Unmarshal(output, &profile); err != nil {
		t.Fatalf("describe output is not JSON: %v: %s", err, output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("describe launched the benchmark or NUMA wrapper: %v", err)
	}
	if profile.ProtocolVersion != 1 || profile.Benchmark != "stream" ||
		profile.Preflight.Status != CheckPass || profile.Resources.TotalWorkers != 32 ||
		len(profile.Assets) != 2 || profile.Assets[0].SHA256 == "" {
		t.Fatalf("unexpected STREAM profile: %+v", profile)
	}
}

func TestDispatcherDescribeHPLDetectsMPIABIMismatch(t *testing.T) {
	dir := t.TempDir()
	hplDir := filepath.Join(dir, "hpl")
	if err := os.Mkdir(hplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hpl := writeExecutable(t, hplDir, "xhpl", "#!/bin/sh\nexit 0\n")
	mpirun := writeExecutable(t, dir, "mpirun", "#!/bin/sh\necho 'HYDRA build details MPICH'\n")
	writeExecutable(t, dir, "ldd", "#!/bin/sh\necho 'libopen-rte.so.40 => /test/libopen-rte.so.40'\n")
	hplDat := strings.Join([]string{
		"HPLinpack benchmark input file",
		"CATMonitor HPL stress",
		"HPL.out",
		"6",
		"1",
		"50000",
		"1",
		"256",
		"0",
		"1",
		"4",
		"2",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(hplDir, "HPL.dat"), []byte(hplDat), 0o644); err != nil {
		t.Fatal(err)
	}
	script := configuredDispatcher(t, dir, map[string]string{
		"HPL_WORKDIR":             hplDir,
		"HPL_EXECUTABLE":          hpl,
		"HPL_MPI_LAUNCHER":        mpirun,
		"HPL_MPI_PROCESSES":       "8",
		"HPL_THREADS_PER_PROCESS": "12",
	})
	command := exec.Command("bash", script, "describe", "hpl")
	command.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var profile ExecutionProfile
	if err := json.Unmarshal(output, &profile); err != nil {
		t.Fatalf("describe output is not JSON: %v: %s", err, output)
	}
	if profile.MPI.Implementation != "mpich" || profile.MPI.ExecutableABI != "openmpi" ||
		profile.MPI.Status != CheckFail || profile.Preflight.Status != CheckFail {
		t.Fatalf("MPI ABI mismatch was not detected: %+v", profile.MPI)
	}
	if profile.Resources.TotalWorkers != 96 || profile.Resources.ProblemSize != "50000" {
		t.Fatalf("unexpected HPL resources: %+v", profile.Resources)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	manager := NewManager(Config{
		Enabled: true, ScriptPath: script,
		Benchmarks: map[string]BenchmarkConfig{"hpl": {Enabled: true, Timeout: time.Minute}},
	})
	available, message := manager.Availability("hpl")
	if available || !strings.Contains(message, "preflight") {
		t.Fatalf("explicit MPI ABI mismatch must block execution: available=%v message=%q", available, message)
	}
}

func TestManagerPersistsProfileAndConfigurationHash(t *testing.T) {
	dir := t.TempDir()
	stream := writeExecutable(t, dir, "stream_omp", "#!/bin/sh\nprintf 'Copy: 1\\nScale: 2\\nAdd: 3\\nTriad: 4\\n'\n")
	numactl := writeExecutable(t, dir, "numactl", "#!/bin/sh\nshift\nexec \"$@\"\n")
	script := configuredDispatcher(t, dir, map[string]string{
		"STREAM_EXECUTABLE": stream,
		"STREAM_NUMACTL":    numactl,
		"STREAM_THREADS":    "4",
	})
	manager := NewManager(Config{
		Enabled: true, ScriptPath: script,
		ReportPath: filepath.Join(dir, "stress-latest.json"),
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Minute},
		},
	})
	defaultProfile, err := manager.Describe("stream")
	if err != nil {
		t.Fatal(err)
	}
	started, err := manager.StartWithOptions([]string{"stream"}, RunOptions{
		Initiator: InitiatorCLI, Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForJob(t, manager, started.JobID)
	if finished.ConfigurationSHA256 == "" || len(finished.ConfigurationSHA256) != 64 ||
		finished.Benchmarks[0].Profile == nil ||
		finished.Benchmarks[0].Profile.ConfigurationSHA256 == "" ||
		finished.Benchmarks[0].Profile.TimeoutSeconds != 30 ||
		finished.Benchmarks[0].Profile.ConfigurationSHA256 == defaultProfile.ConfigurationSHA256 {
		t.Fatalf("profile snapshot was not persisted: %+v", finished)
	}
	history, err := manager.History(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ConfigurationSHA256 != finished.ConfigurationSHA256 ||
		history[0].Benchmarks[0].Profile == nil {
		t.Fatalf("history did not retain the profile: %+v", history)
	}
}

func TestManagerFallsBackForLegacyDispatcher(t *testing.T) {
	dir := t.TempDir()
	script := writeExecutable(t, dir, "benchmark_check.sh", "#!/bin/sh\nexit 1\n")
	manager := NewManager(Config{
		Enabled: true, ScriptPath: script,
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Minute},
		},
	})
	profile, err := manager.Describe("stream")
	if err == nil || profile == nil || !profile.DescribeProtocolFallback ||
		profile.Preflight.Status != CheckUnsupported || profile.ConfigurationSHA256 == "" {
		t.Fatalf("unexpected legacy fallback: profile=%+v err=%v", profile, err)
	}
	available, message := manager.Availability("stream")
	if !available || !strings.Contains(message, "describe protocol unavailable") {
		t.Fatalf("legacy dispatcher should remain runnable with a warning: %v %q", available, message)
	}
}

func TestManagerRejectsMalformedDescribeJSON(t *testing.T) {
	dir := t.TempDir()
	script := writeExecutable(t, dir, "benchmark_check.sh", `#!/bin/bash
CATMONITOR_STRESS_DESCRIBE_PROTOCOL=1
printf '{"protocol_version":1,"benchmark":"stream","unknown":true}\n'
`)
	manager := NewManager(Config{
		Enabled: true, ScriptPath: script,
		Benchmarks: map[string]BenchmarkConfig{"stream": {Enabled: true, Timeout: time.Minute}},
	})
	profile, err := manager.Describe("stream")
	if err == nil || profile == nil || !profile.DescribeProtocolFallback ||
		!strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("malformed describe JSON was not rejected: profile=%+v err=%v", profile, err)
	}
}

func TestManagerDescribeTimeoutKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	childPID := filepath.Join(dir, "child.pid")
	script := writeExecutable(t, dir, "benchmark_check.sh", `#!/bin/bash
CATMONITOR_STRESS_DESCRIBE_PROTOCOL=1
sleep 30 &
printf '%s\n' "$!" > "$CATMONITOR_TEST_CHILD_PID"
wait
`)
	manager := NewManager(Config{
		Enabled: true, ScriptPath: script,
		Benchmarks: map[string]BenchmarkConfig{"stream": {Enabled: true, Timeout: time.Minute}},
	})
	started := time.Now()
	oldValue, hadValue := os.LookupEnv("CATMONITOR_TEST_CHILD_PID")
	if err := os.Setenv("CATMONITOR_TEST_CHILD_PID", childPID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if hadValue {
			_ = os.Setenv("CATMONITOR_TEST_CHILD_PID", oldValue)
		} else {
			_ = os.Unsetenv("CATMONITOR_TEST_CHILD_PID")
		}
	}()
	_, err := manager.Describe("stream")
	if err == nil || !strings.Contains(err.Error(), "timed out") ||
		time.Since(started) > 4*time.Second {
		t.Fatalf("describe timeout was not bounded: elapsed=%s err=%v", time.Since(started), err)
	}
	data, readErr := os.ReadFile(childPID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("describe child process %d survived timeout", pid)
}
