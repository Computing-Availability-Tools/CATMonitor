//go:build linux

// Package runnerapi implements the private Unix-socket protocol used by the
// optional CATMonitor CPU stress runner. It intentionally exposes only the
// fixed STREAM, HPL and HPCG benchmark names; callers cannot submit commands,
// paths, arguments or environment variables.
package runnerapi

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
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const maxOutputBytes = 16 << 20

type Server struct {
	adapter string
	server  *http.Server
	runs    chan struct{}
}

func NewServer(adapter string) (*Server, error) {
	if !filepath.IsAbs(adapter) {
		return nil, fmt.Errorf("adapter path must be absolute")
	}
	info, err := os.Stat(adapter)
	if err != nil {
		return nil, fmt.Errorf("inspect adapter: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return nil, fmt.Errorf("adapter must be an executable regular file")
	}
	s := &Server{adapter: adapter, runs: make(chan struct{}, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", s.health)
	mux.HandleFunc("/v1/benchmarks/", s.describe)
	mux.HandleFunc("/v1/runs", s.run)
	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	return s, nil
}

func (s *Server) Serve(socketPath string, mode os.FileMode) error {
	if !filepath.IsAbs(socketPath) {
		return fmt.Errorf("socket path must be absolute")
	}
	if mode.Perm()&0007 != 0 {
		return fmt.Errorf("socket mode must not grant access to other users")
	}
	if err := prepareSocket(socketPath); err != nil {
		return err
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on Unix socket: %w", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()
	if err := os.Chmod(socketPath, mode.Perm()); err != nil {
		return fmt.Errorf("set Unix socket mode: %w", err)
	}
	if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error { return s.server.Shutdown(ctx) }

func prepareSocket(path string) error {
	directory := filepath.Dir(path)
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("inspect socket directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("socket parent is not a directory")
	}
	info, err = os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing socket path: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace non-socket path: %s", path)
	}
	connection, dialErr := net.DialTimeout("unix", path, 250*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return fmt.Errorf("Unix socket is already served: %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale Unix socket: %w", err)
	}
	return nil
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, `{"status":"ok","benchmarks":["stream","hpl","hpcg"]}`+"\n")
}

func (s *Server) describe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/v1/benchmarks/")
	if !supported(name) || strings.Contains(name, "/") {
		http.Error(w, "unsupported CPU benchmark", http.StatusNotFound)
		return
	}
	output, err := execute(r.Context(), s.adapter, "describe", name)
	if err != nil {
		writeExecutionError(w, output, err)
		return
	}
	if !json.Valid(output) {
		http.Error(w, "adapter returned invalid describe JSON", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(output)
}

type runRequest struct {
	Benchmark string `json:"benchmark"`
}

func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request runRequest
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid run request", http.StatusBadRequest)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		http.Error(w, "run request must contain one JSON object", http.StatusBadRequest)
		return
	}
	if !supported(request.Benchmark) {
		http.Error(w, "unsupported CPU benchmark", http.StatusBadRequest)
		return
	}
	select {
	case s.runs <- struct{}{}:
		defer func() { <-s.runs }()
	default:
		http.Error(w, "another CPU stress job is already running", http.StatusConflict)
		return
	}
	output, err := execute(r.Context(), s.adapter, request.Benchmark)
	if err != nil {
		writeExecutionError(w, output, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(output)
}

func supported(name string) bool {
	switch name {
	case "stream", "hpl", "hpcg":
		return true
	default:
		return false
	}
}

func methodNotAllowed(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", method)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func writeExecutionError(w http.ResponseWriter, output []byte, err error) {
	status := http.StatusUnprocessableEntity
	if errors.Is(err, errOutputLimit) {
		status = http.StatusBadGateway
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if len(output) > 0 {
		_, _ = w.Write(output)
		if output[len(output)-1] != '\n' {
			_, _ = io.WriteString(w, "\n")
		}
	}
	_, _ = fmt.Fprintf(w, "CPU runner: %v\n", err)
}

var errOutputLimit = errors.New("benchmark output exceeded 16 MiB limit")

type limitedBuffer struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	available := maxOutputBytes - b.buffer.Len()
	if available > 0 {
		write := len(p)
		if write > available {
			write = available
		}
		_, _ = b.buffer.Write(p[:write])
	}
	if len(p) > available {
		b.exceeded = true
	}
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buffer.Bytes())
}

func execute(ctx context.Context, adapter string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, adapter, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	buffer := &limitedBuffer{}
	cmd.Stdout = buffer
	cmd.Stderr = buffer
	err := cmd.Run()
	if buffer.exceeded {
		return buffer.Bytes(), errOutputLimit
	}
	return buffer.Bytes(), err
}
