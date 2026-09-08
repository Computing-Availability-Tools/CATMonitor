//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/resultparse"
	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadplugin"
)

const maxOutputBytes = 16 * 1024

var jobIDPattern = regexp.MustCompile(`^[0-9a-f]{1,64}$`)

func main() {
	if len(os.Args) < 2 {
		fatal("operation is required")
	}
	var err error
	switch os.Args[1] {
	case "describe":
		err = describe(os.Args[2:])
	case "run":
		err = run(os.Args[2:])
	case "cancel":
		err = cancel(os.Args[2:])
	case "status":
		err = status(os.Args[2:])
	default:
		err = fmt.Errorf("unknown operation %q", os.Args[1])
	}
	if err != nil {
		fatal(err.Error())
	}
}

func describe(args []string) error {
	fs := flag.NewFlagSet("describe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	benchmark, jsonOutput := "", false
	fs.StringVar(&benchmark, "benchmark", "", "benchmark name")
	fs.BoolVar(&jsonOutput, "json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !jsonOutput {
		return errors.New("describe requires --json and no positional arguments")
	}
	if benchmark == "" {
		capabilities := map[string]any{"protocol_version": workloadapi.ProtocolVersion, "benchmarks": allowedBenchmarks()}
		return json.NewEncoder(os.Stdout).Encode(capabilities)
	}
	if !allowedBenchmark(benchmark) {
		return fmt.Errorf("benchmark %q is not supported by this container", benchmark)
	}
	timeout := 2 * time.Second
	if benchmark == "npu_burn" {
		timeout = 30 * time.Second
	}
	ctx, done := context.WithTimeout(context.Background(), timeout)
	defer done()
	profile, err := workloadplugin.Describe(ctx, benchmark)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("describe timed out")
		}
		return fmt.Errorf("describe failed: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(profile)
}

func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	requestPath := ""
	fs.StringVar(&requestPath, "request", "", "request file or -")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || requestPath == "" {
		return errors.New("run requires --request")
	}
	var reader io.Reader
	if requestPath == "-" {
		reader = io.LimitReader(os.Stdin, 64<<10)
	} else {
		if !filepath.IsAbs(requestPath) {
			return errors.New("request path must be absolute or -")
		}
		file, err := os.Open(requestPath)
		if err != nil {
			return err
		}
		defer file.Close()
		reader = io.LimitReader(file, 64<<10)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	var request workloadapi.Request
	if err := decodeStrict(data, &request); err != nil {
		return fmt.Errorf("invalid workload request: %w", err)
	}
	if err := validateRequest(request); err != nil {
		return err
	}
	if len(request.Options) != 0 {
		return errors.New("this workload profile does not accept per-job options")
	}

	root, err := stateRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return err
	}
	active := filepath.Join(root, "active")
	if err := os.Mkdir(active, 0o750); err != nil {
		if os.IsExist(err) {
			return errors.New("another workload job is already running in this container")
		}
		return err
	}
	activeOwned := true
	defer func() {
		if activeOwned {
			_ = os.RemoveAll(active)
		}
	}()
	if err := atomicWrite(filepath.Join(active, "job_id"), []byte(request.JobID+"\n"), 0o640); err != nil {
		return err
	}
	jobDir := filepath.Join(root, "workload-jobs", request.JobID)
	if err := os.MkdirAll(jobDir, 0o750); err != nil {
		return err
	}
	if err := atomicWriteJSON(filepath.Join(jobDir, "request.json"), request); err != nil {
		return err
	}
	started := time.Now()
	state := workloadapi.State{ProtocolVersion: workloadapi.ProtocolVersion, JobID: request.JobID, Benchmark: request.Benchmark, Status: workloadapi.StatusRunning, UpdatedAt: started}
	if err := atomicWriteJSON(filepath.Join(jobDir, "state.json"), state); err != nil {
		return err
	}

	spec, err := workloadplugin.Resolve(request.Benchmark)
	if err != nil {
		return writeRunFailure(jobDir, request, started, "", err)
	}
	resultDir := spec.ResultDir
	before, err := resultparse.Capture(request.Benchmark, resultDir)
	if err != nil {
		return writeRunFailure(jobDir, request, started, "", fmt.Errorf("capture pre-run result state: %w", err))
	}
	ctx, timeoutCancel := context.WithTimeout(context.Background(), time.Duration(request.TimeoutSeconds)*time.Second)
	defer timeoutCancel()
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process, syscall.SIGKILL) }
	var output limitedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		return writeRunFailure(jobDir, request, started, output.String(), fmt.Errorf("start workload: %w", err))
	}
	pgid := cmd.Process.Pid
	if err := atomicWrite(filepath.Join(jobDir, "pgid"), []byte(strconv.Itoa(pgid)+"\n"), 0o640); err != nil {
		_ = killGroup(cmd.Process, syscall.SIGKILL)
		_ = cmd.Wait()
		return err
	}
	if _, err := os.Stat(filepath.Join(jobDir, "cancel.requested")); err == nil {
		_ = killProcessGroup(pgid, syscall.SIGTERM)
	}
	runErr := cmd.Wait()
	resultOutput := output.String()
	if runErr == nil && spec.Complete != nil {
		resultOutput, runErr = spec.Complete(resultOutput)
	}
	finished := time.Now()
	result := workloadapi.Result{
		ProtocolVersion: workloadapi.ProtocolVersion, JobID: request.JobID, Benchmark: request.Benchmark,
		StartedAt: started, FinishedAt: finished, DurationMS: finished.Sub(started).Milliseconds(), Output: resultOutput,
	}
	cancelled := fileExists(filepath.Join(jobDir, "cancel.requested"))
	switch {
	case cancelled:
		result.Status, result.Message = workloadapi.StatusCancelled, "workload cancelled"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		result.Status, result.Message = workloadapi.StatusTimeLimitReached, "configured time limit reached; workload process group stopped"
	case runErr != nil:
		result.Status, result.Message = workloadapi.StatusUnhealthy, fmt.Sprintf("workload command failed: %v", runErr)
	default:
		values, source, parseErr := resultparse.Parse(request.Benchmark, result.Output, resultDir, before)
		result.Values, result.Source = values, source
		if parseErr != nil {
			result.Status, result.Message = workloadapi.StatusUnhealthy, parseErr.Error()
		} else {
			result.Status, result.Message = workloadapi.StatusHealthy, "workload command completed and required values parsed"
		}
	}
	state.Status, state.UpdatedAt, state.Message = result.Status, finished, result.Message
	_ = os.Remove(filepath.Join(jobDir, "pgid"))
	if err := atomicWriteJSON(filepath.Join(jobDir, "result.json"), result); err != nil {
		return err
	}
	if err := atomicWriteJSON(filepath.Join(jobDir, "state.json"), state); err != nil {
		return err
	}
	activeOwned = false
	if err := removeActiveIfOwner(active, request.JobID); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func writeRunFailure(jobDir string, request workloadapi.Request, started time.Time, output string, failure error) error {
	finished := time.Now()
	result := workloadapi.Result{ProtocolVersion: workloadapi.ProtocolVersion, JobID: request.JobID, Benchmark: request.Benchmark, Status: workloadapi.StatusUnavailable, StartedAt: started, FinishedAt: finished, DurationMS: finished.Sub(started).Milliseconds(), Message: failure.Error(), Output: output}
	_ = atomicWriteJSON(filepath.Join(jobDir, "result.json"), result)
	_ = atomicWriteJSON(filepath.Join(jobDir, "state.json"), workloadapi.State{ProtocolVersion: workloadapi.ProtocolVersion, JobID: request.JobID, Benchmark: request.Benchmark, Status: result.Status, UpdatedAt: finished, Message: result.Message})
	return json.NewEncoder(os.Stdout).Encode(result)
}

func cancel(args []string) error {
	jobID, err := parseJobIDArgs("cancel", args)
	if err != nil {
		return err
	}
	root, err := stateRoot()
	if err != nil {
		return err
	}
	active := filepath.Join(root, "active")
	owner, err := os.ReadFile(filepath.Join(active, "job_id"))
	if err != nil || strings.TrimSpace(string(owner)) != jobID {
		return errors.New("active workload job not found")
	}
	jobDir := filepath.Join(root, "workload-jobs", jobID)
	if err := atomicWrite(filepath.Join(jobDir, "cancel.requested"), []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o640); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(jobDir, "pgid"))
	if err != nil {
		return json.NewEncoder(os.Stdout).Encode(workloadapi.CancelResponse{ProtocolVersion: workloadapi.ProtocolVersion, JobID: jobID, Accepted: true, Message: "cancellation recorded before workload process start"})
	}
	pgid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pgid <= 1 {
		return errors.New("invalid workload process group")
	}
	_ = killProcessGroup(pgid, syscall.SIGTERM)
	deadline := time.Now().Add(3 * time.Second)
	for processGroupExists(pgid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processGroupExists(pgid) {
		_ = killProcessGroup(pgid, syscall.SIGKILL)
	}
	return json.NewEncoder(os.Stdout).Encode(workloadapi.CancelResponse{ProtocolVersion: workloadapi.ProtocolVersion, JobID: jobID, Accepted: true, Message: "workload process group cancellation requested"})
}

func status(args []string) error {
	jobID, err := parseJobIDArgs("status", args)
	if err != nil {
		return err
	}
	root, err := stateRoot()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, "workload-jobs", jobID, "state.json"))
	if err != nil {
		return errors.New("workload job not found")
	}
	var state workloadapi.State
	if err := decodeStrict(data, &state); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(state)
}

func validateRequest(request workloadapi.Request) error {
	if request.ProtocolVersion != workloadapi.ProtocolVersion {
		return fmt.Errorf("unsupported protocol version %d", request.ProtocolVersion)
	}
	if !jobIDPattern.MatchString(request.JobID) {
		return errors.New("job_id must be 1-64 lowercase hexadecimal characters")
	}
	if !allowedBenchmark(request.Benchmark) {
		return fmt.Errorf("benchmark %q is not supported by this container", request.Benchmark)
	}
	if request.TimeoutSeconds <= 0 || request.TimeoutSeconds > int64((24*time.Hour)/time.Second) {
		return errors.New("timeout_seconds must be between 1 and 86400")
	}
	return nil
}

func allowedBenchmarks() []string {
	raw := strings.Split(os.Getenv("CATMONITOR_STRESS_BENCHMARKS"), ",")
	result, seen := []string{}, map[string]bool{}
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
func allowedBenchmark(name string) bool {
	for _, allowed := range allowedBenchmarks() {
		if name == allowed {
			return true
		}
	}
	return false
}

func stateRoot() (string, error) {
	root := os.Getenv("CATMONITOR_STRESS_STATE_ROOT")
	if root == "" {
		root = "/var/lib/catmonitor/stress"
	}
	if !filepath.IsAbs(root) {
		return "", errors.New("stress state root must be absolute")
	}
	return root, nil
}

func parseJobIDArgs(name string, args []string) (string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jobID := ""
	fs.StringVar(&jobID, "job-id", "", "job id")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if fs.NArg() != 0 || !jobIDPattern.MatchString(jobID) {
		return "", errors.New("valid --job-id is required")
	}
	return jobID, nil
}
func decodeStrict(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func atomicWriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data, 0o640)
}
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".stress-exec-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
func removeActiveIfOwner(active, jobID string) error {
	data, err := os.ReadFile(filepath.Join(active, "job_id"))
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(data)) != jobID {
		return errors.New("active workload ownership changed")
	}
	return os.RemoveAll(active)
}
func killGroup(process *os.Process, signal syscall.Signal) error {
	if process == nil {
		return os.ErrProcessDone
	}
	return killProcessGroup(process.Pid, signal)
}
func killProcessGroup(pgid int, signal syscall.Signal) error {
	err := syscall.Kill(-pgid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
func processGroupExists(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

type limitedBuffer struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(p) >= maxOutputBytes {
		b.data = append(b.data[:0], p[len(p)-maxOutputBytes:]...)
		b.truncated = true
		return n, nil
	}
	if overflow := len(b.data) + len(p) - maxOutputBytes; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
		b.truncated = true
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (b *limitedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...)
}
func (b *limitedBuffer) String() string {
	value := string(b.Bytes())
	if b.truncated {
		return "... output truncated; showing tail\n" + value
	}
	return value
}
func fatal(message string) { fmt.Fprintln(os.Stderr, "catmonitor-stress-exec:", message); os.Exit(1) }
