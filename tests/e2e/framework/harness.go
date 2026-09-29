//go:build e2e

package e2e

import (
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	// daemonExporterAddr is hardcoded in cmd/catmonitor (v0.3.6): the e2e
	// framework cannot reconfigure it, so it must be free before start.
	daemonExporterAddr = "127.0.0.1:19320"
	webAddr            = "127.0.0.1:19322"
	dfeeAddr           = "127.0.0.1:19323"

	// readyTimeout bounds every readiness poll (snapshot.json, /-/ready).
	readyTimeout = 60 * time.Second
	// pollInterval is the retry cadence of the readiness helpers.
	pollInterval = 50 * time.Millisecond
	// logTailLines is how many log lines are dumped on test failure.
	logTailLines = 50
)

// GoBin returns the Go toolchain used to build the binaries under test.
func GoBin() string {
	if v := os.Getenv("GO_BIN"); v != "" {
		return v
	}
	return "go"
}

// RepoRoot locates the repository root by walking up from the working
// directory until a go.mod declaring the CATMonitor module is found.
func RepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(data), "CATMonitor") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found walking up from %s", dir)
		}
		dir = parent
	}
}

// Workspace is a per-scenario scratch directory (auto-removed on test end).
type Workspace struct{ Dir string }

// NewWorkspace allocates a fresh workspace, making the t.TempDir() chain
// traversable by non-root users (for RunAsUser scenarios).
func NewWorkspace(t *testing.T) *Workspace {
	t.Helper()
	ws := &Workspace{Dir: t.TempDir()}
	chmodTestTempChain(ws.Dir)
	return ws
}

// chmodTestTempChain makes dir and its Go-test temp ancestors (matching
// /tmp/Test*) traversable (0755). It stops before /tmp itself.
func chmodTestTempChain(dir string) {
	for p := dir; p != "/" && p != "."; p = filepath.Dir(p) {
		if !strings.HasPrefix(p, "/tmp/Test") {
			break
		}
		if err := os.Chmod(p, 0o755); err != nil {
			break
		}
	}
}

// Binaries holds the production executables compiled for one scenario.
type Binaries struct {
	Dir    string
	Daemon string
	Web    string
	Dfee   string
}

// Build compiles the three production binaries (daemon, web, dfee) into a
// per-test directory, then makes them executable by non-root users.
func Build(t *testing.T) *Binaries {
	t.Helper()
	root := RepoRoot(t)
	dir := t.TempDir()
	b := &Binaries{
		Dir:    dir,
		Daemon: filepath.Join(dir, "catmonitor"),
		Web:    filepath.Join(dir, "catmonitor-web"),
		Dfee:   filepath.Join(dir, "catmonitor-dfee"),
	}
	targets := []struct{ out, pkg string }{
		{b.Daemon, "./cmd/catmonitor"},
		{b.Web, "./features/web"},
		{b.Dfee, "./features/dfee"},
	}
	for _, tg := range targets {
		cmd := exec.Command(GoBin(), "build", "-o", tg.out, tg.pkg)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", tg.pkg, err, out)
		}
	}
	_ = os.Chmod(dir, 0o755)
	for _, tg := range targets {
		_ = os.Chmod(tg.out, 0o755)
	}
	return b
}

// Proc is a supervised child process whose output is captured to a log
// file. On test failure the log tail is dumped automatically; the process
// is always stopped via the test cleanup.
type Proc struct {
	Name    string
	LogPath string

	cmd      *exec.Cmd
	logFile  *os.File
	stopOnce sync.Once
}

// ProcSpec describes how to launch one supervised process.
type ProcSpec struct {
	Name      string
	LogPath   string
	Dir       string
	Env       []string
	RunAsUser string // drops to that user's uid/gid; requires root
	Argv      []string
}

// StartProc launches a supervised process with the default credentials.
func StartProc(t *testing.T, name, logPath, dir string, env []string, argv ...string) *Proc {
	t.Helper()
	return StartProcSpec(t, ProcSpec{Name: name, LogPath: logPath, Dir: dir, Env: env, Argv: argv})
}

// StartProcSpec launches a supervised process honoring the full spec,
// including optional privilege dropping.
func StartProcSpec(t *testing.T, spec ProcSpec) *Proc {
	t.Helper()
	logFile, err := os.Create(spec.LogPath)
	if err != nil {
		t.Fatalf("create %s log: %v", spec.Name, err)
	}
	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if spec.RunAsUser != "" {
		if os.Geteuid() != 0 {
			logFile.Close()
			t.Skipf("dropping to user %q requires root; current euid=%d", spec.RunAsUser, os.Geteuid())
		}
		u, err := user.Lookup(spec.RunAsUser)
		if err != nil {
			logFile.Close()
			t.Fatalf("lookup user %s: %v", spec.RunAsUser, err)
		}
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},
		}
	}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatalf("start %s: %v", spec.Name, err)
	}
	p := &Proc{Name: spec.Name, LogPath: spec.LogPath, cmd: cmd, logFile: logFile}
	t.Cleanup(func() {
		p.Stop()
		if t.Failed() {
			p.dumpTail(t)
		}
	})
	return p
}

// Stop terminates the process (SIGTERM, escalating to SIGKILL after 15s)
// and waits for it to exit. Idempotent. The 15s window accommodates the
// daemon's graceful shutdown, which waits for in-flight collections that
// can be slow on BMC-delayed hosts (ipmitool ~10s).
func (p *Proc) Stop() {
	p.stopOnce.Do(func() {
		if p.cmd.Process == nil {
			return
		}
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			_ = p.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = p.cmd.Process.Kill()
			<-done
		}
		p.logFile.Close()
	})
}

// Alive reports whether the process has not exited yet.
func (p *Proc) Alive() bool {
	if p.cmd.Process == nil {
		return false
	}
	return p.cmd.Process.Signal(syscall.Signal(0)) == nil
}

func (p *Proc) dumpTail(t *testing.T) {
	data, err := os.ReadFile(p.LogPath)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > logTailLines {
		lines = lines[len(lines)-logTailLines:]
	}
	t.Logf("--- %s.log (last %d lines) ---\n%s", p.Name, len(lines), strings.Join(lines, "\n"))
}

// requirePortFree fails the test fast when addr is already listening.
func requirePortFree(t *testing.T, addr, purpose string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return
	}
	conn.Close()
	t.Fatalf("%s needs %s but it is already in use; free the port or rerun on a clean environment", purpose, addr)
}

// WaitPortFree polls until addr is free (no listener) or timeout.
func WaitPortFree(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			return
		}
		conn.Close()
		time.Sleep(pollInterval)
	}
	t.Fatalf("port %s still in use after %s", addr, timeout)
}

// WaitFileExists polls until path exists or the timeout elapses.
func WaitFileExists(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	waitFileExists(t, path, timeout)
}

func waitFileExists(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("file %s not created within %s", path, timeout)
}

func waitHTTPReady(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("GET %s did not return 200 within %s", url, timeout)
}

// HTTPGet fetches url once, requires HTTP 200, and returns the body and
// Content-Type header.
func HTTPGet(t *testing.T, url string) (string, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: read body: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d, body: %s", url, resp.StatusCode, body)
	}
	return string(body), resp.Header.Get("Content-Type")
}
