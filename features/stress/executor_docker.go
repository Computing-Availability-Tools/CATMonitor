package stress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

const (
	workloadEntrypoint = "/usr/local/bin/catmonitor-stress-exec"
	maxExecutorOutput  = 256 * 1024
	cancelGracePeriod  = 10 * time.Second
)

var (
	containerNamePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	containerUserPattern    = regexp.MustCompile(`^[0-9]+(?::[0-9]+)?$`)
	dockerAPIVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
)

type DockerExecExecutor struct {
	binary string
	socket string

	apiVersionMu sync.Mutex
	apiVersion   string
}

func NewDockerExecExecutor(cfg ExecutorConfig) (*DockerExecExecutor, error) {
	binary := strings.TrimSpace(cfg.DockerBinary)
	if binary == "" {
		binary = "/usr/bin/docker"
	}
	if !strings.HasPrefix(binary, "/") {
		return nil, errors.New("docker_binary must be an absolute path")
	}
	socket := strings.TrimSpace(cfg.DockerSocket)
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	if !strings.HasPrefix(socket, "/") {
		return nil, errors.New("docker_socket must be an absolute path")
	}
	return &DockerExecExecutor{binary: binary, socket: socket}, nil
}

func (e *DockerExecExecutor) dockerArgs(args ...string) []string {
	base := []string{"--host", "unix://" + e.socket}
	return append(base, args...)
}

func (e *DockerExecExecutor) workloadArgs(binding Binding, interactive bool, command ...string) []string {
	args := []string{"exec"}
	if interactive {
		args = append(args, "-i")
	}
	if binding.User != "" {
		args = append(args, "--user", binding.User)
	}
	args = append(args, binding.Container, workloadEntrypoint)
	args = append(args, command...)
	return e.dockerArgs(args...)
}

func validateBinding(binding Binding) error {
	if !supportedBenchmark(binding.Benchmark) {
		return fmt.Errorf("unsupported benchmark %q", binding.Benchmark)
	}
	if binding.Plugin == "" || binding.Plugin != binding.Benchmark {
		return fmt.Errorf("benchmark %q has invalid plugin %q", binding.Benchmark, binding.Plugin)
	}
	if !containerNamePattern.MatchString(binding.Container) {
		return fmt.Errorf("benchmark %q has invalid container name", binding.Benchmark)
	}
	if binding.User != "" && !containerUserPattern.MatchString(binding.User) {
		return fmt.Errorf("benchmark %q has invalid container user", binding.Benchmark)
	}
	return nil
}

func (e *DockerExecExecutor) Describe(ctx context.Context, binding Binding) (*ExecutionProfile, error) {
	if err := validateBinding(binding); err != nil {
		return nil, err
	}
	args := e.workloadArgs(binding, false, "describe", "--benchmark", binding.Plugin, "--json")
	stdout, stderr, err := e.runCommand(ctx, nil, args...)
	if err != nil {
		return nil, transportError("describe", binding.Container, err, stderr, stdout)
	}
	var profile ExecutionProfile
	if err := decodeStrictJSON(stdout, &profile); err != nil {
		return nil, fmt.Errorf("decode workload describe response: %w", err)
	}
	if err := validateDescribeProfile(binding.Benchmark, &profile); err != nil {
		return nil, err
	}
	profile.Executor = "docker_exec"
	profile.Container = binding.Container
	profile.Plugin = binding.Plugin
	return &profile, nil
}

func (e *DockerExecExecutor) Run(ctx context.Context, binding Binding, request workloadapi.Request) (workloadapi.Result, error) {
	if err := validateBinding(binding); err != nil {
		return workloadapi.Result{}, err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return workloadapi.Result{}, err
	}
	args := e.workloadArgs(binding, true, "run", "--request", "-")
	env, err := e.dockerCommandEnv(ctx)
	if err != nil {
		return workloadapi.Result{}, err
	}
	cmd := exec.Command(e.binary, args...)
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr cappedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return workloadapi.Result{}, transportError("run", binding.Container, err, stderr.Bytes(), stdout.Bytes())
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err = <-done:
	case <-ctx.Done():
		cancelCtx, cancel := context.WithTimeout(context.Background(), cancelGracePeriod)
		cancelErr := e.Cancel(cancelCtx, binding, request.JobID)
		cancel()
		select {
		case err = <-done:
		case <-time.After(cancelGracePeriod):
			_ = cmd.Process.Kill()
			err = <-done
		}
		if cancelErr != nil {
			return workloadapi.Result{}, fmt.Errorf("cancel workload after controller context ended: %w", cancelErr)
		}
	}
	if err != nil {
		return workloadapi.Result{}, transportError("run", binding.Container, err, stderr.Bytes(), stdout.Bytes())
	}
	var result workloadapi.Result
	if err := decodeStrictJSON(stdout.Bytes(), &result); err != nil {
		return workloadapi.Result{}, fmt.Errorf("decode workload result: %w", err)
	}
	if result.ProtocolVersion != workloadapi.ProtocolVersion || result.JobID != request.JobID ||
		result.Benchmark != request.Benchmark || !workloadapi.ValidTerminalStatus(result.Status) {
		return workloadapi.Result{}, errors.New("workload returned an invalid result envelope")
	}
	return result, nil
}

func (e *DockerExecExecutor) Cancel(ctx context.Context, binding Binding, jobID string) error {
	if err := validateBinding(binding); err != nil {
		return err
	}
	args := e.workloadArgs(binding, false, "cancel", "--job-id", jobID)
	stdout, stderr, err := e.runCommand(ctx, nil, args...)
	if err != nil {
		return transportError("cancel", binding.Container, err, stderr, stdout)
	}
	var response workloadapi.CancelResponse
	if err := decodeStrictJSON(stdout, &response); err != nil {
		return fmt.Errorf("decode workload cancel response: %w", err)
	}
	if response.ProtocolVersion != workloadapi.ProtocolVersion || response.JobID != jobID || !response.Accepted {
		return errors.New("workload rejected cancellation")
	}
	return nil
}

func (e *DockerExecExecutor) Status(ctx context.Context, binding Binding, jobID string) (workloadapi.State, error) {
	if err := validateBinding(binding); err != nil {
		return workloadapi.State{}, err
	}
	args := e.workloadArgs(binding, false, "status", "--job-id", jobID)
	stdout, stderr, err := e.runCommand(ctx, nil, args...)
	if err != nil {
		return workloadapi.State{}, transportError("status", binding.Container, err, stderr, stdout)
	}
	var state workloadapi.State
	if err := decodeStrictJSON(stdout, &state); err != nil {
		return workloadapi.State{}, fmt.Errorf("decode workload status response: %w", err)
	}
	if state.ProtocolVersion != workloadapi.ProtocolVersion || state.JobID != jobID {
		return workloadapi.State{}, errors.New("workload returned an invalid state envelope")
	}
	return state, nil
}

func (e *DockerExecExecutor) runCommand(ctx context.Context, stdin io.Reader, args ...string) ([]byte, []byte, error) {
	env, err := e.dockerCommandEnv(ctx)
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.CommandContext(ctx, e.binary, args...)
	cmd.Env = env
	cmd.Stdin = stdin
	var stdout, stderr cappedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func (e *DockerExecExecutor) dockerCommandEnv(ctx context.Context) ([]string, error) {
	apiVersion, err := e.dockerAPIVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect Docker daemon API version: %w", err)
	}
	env := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "DOCKER_API_VERSION=") {
			env = append(env, item)
		}
	}
	return append(env, "DOCKER_API_VERSION="+apiVersion), nil
}

func (e *DockerExecExecutor) dockerAPIVersion(ctx context.Context) (string, error) {
	e.apiVersionMu.Lock()
	defer e.apiVersionMu.Unlock()
	if e.apiVersion != "" {
		return e.apiVersion, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", e.socket)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, "http://docker/version", nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Docker version endpoint returned HTTP %d", response.StatusCode)
	}
	var version struct {
		APIVersion string `json:"ApiVersion"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64*1024))
	if err := decoder.Decode(&version); err != nil {
		return "", fmt.Errorf("decode Docker version response: %w", err)
	}
	if !dockerAPIVersionPattern.MatchString(version.APIVersion) {
		return "", fmt.Errorf("Docker version endpoint returned invalid API version %q", version.APIVersion)
	}
	e.apiVersion = version.APIVersion
	return e.apiVersion, nil
}

func transportError(operation, container string, err error, stderr, stdout []byte) error {
	message := strings.TrimSpace(string(stderr))
	if message == "" {
		message = strings.TrimSpace(string(stdout))
	}
	if len(message) > 512 {
		message = message[len(message)-512:]
	}
	if message == "" {
		return fmt.Errorf("docker exec %s in %s: %w", operation, container, err)
	}
	return fmt.Errorf("docker exec %s in %s: %w: %s", operation, container, err, message)
}

func decodeStrictJSON(data []byte, value any) error {
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

type cappedBuffer struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(p) >= maxExecutorOutput {
		b.data = append(b.data[:0], p[len(p)-maxExecutorOutput:]...)
		b.truncated = true
		return n, nil
	}
	if overflow := len(b.data) + len(p) - maxExecutorOutput; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
		b.truncated = true
	}
	b.data = append(b.data, p...)
	return n, nil
}

func (b *cappedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	data := append([]byte(nil), b.data...)
	if b.truncated {
		data = append([]byte("... output truncated; showing tail\n"), data...)
	}
	return data
}
