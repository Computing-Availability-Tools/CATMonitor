//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/snapshot"
)

// DaemonOpts tunes how StartDaemon configures the daemon under test.
type DaemonOpts struct {
	// Features is the feature scope (config `features:`), resolved
	// relative to the repo root by the daemon. Default [web,dfee].
	Features []string
	// MinPriority is the collection threshold (low|medium|high). Default
	// empty = no threshold.
	MinPriority string
	// CPUInterval / MemoryInterval speed up collection. Note: a non-empty
	// feature scope derives per-component cadence from the features'
	// metrics.yaml and overrides these; the scheduler still collects once
	// immediately on start. Defaults 500ms.
	CPUInterval    time.Duration
	MemoryInterval time.Duration
	// FaultSubEnabled turns on the fault subscription feature (REST API
	// on FaultSubRestAddr + webhook dispatching). Default off.
	FaultSubEnabled  bool
	FaultSubRestAddr string // default 127.0.0.1:19321
	// StressEnabled turns on the stress controller (control socket at
	// ControlSocket, mode 0660). Default off.
	StressEnabled bool
	ControlSocket string // default <workspace>/control.sock
	// StragglerOutputEnabled turns on the straggler KPI tap. Default off.
	StragglerOutputEnabled bool
	StragglerDataDir       string        // default <workspace>/straggler
	StragglerFlushInterval time.Duration // default 500ms
	// EnvlessProc starts the daemon with an empty PATH (for degradation
	// scenarios: no nvidia-smi/npu-smi/ipmitool reachable — TC-025).
	EnvlessProc bool
}

func (o DaemonOpts) withDefaults(ws *Workspace) DaemonOpts {
	if o.Features == nil {
		o.Features = []string{"web", "dfee"}
	}
	if o.CPUInterval <= 0 {
		o.CPUInterval = 500 * time.Millisecond
	}
	if o.MemoryInterval <= 0 {
		o.MemoryInterval = 500 * time.Millisecond
	}
	if o.FaultSubRestAddr == "" {
		o.FaultSubRestAddr = "127.0.0.1:19321"
	}
	if o.ControlSocket == "" && ws != nil {
		o.ControlSocket = filepath.Join(ws.Dir, "control.sock")
	}
	if o.StragglerDataDir == "" && ws != nil {
		o.StragglerDataDir = filepath.Join(ws.Dir, "straggler")
	}
	if o.StragglerFlushInterval <= 0 {
		o.StragglerFlushInterval = 500 * time.Millisecond
	}
	return o
}

// DaemonProc is a supervised daemon plus the paths it writes.
type DaemonProc struct {
	proc          *Proc
	root          string
	configPath    string
	snapshotDir   string
	dataDir       string
	faultSubAddr  string
	controlSocket string
	stragglerDir  string
	bins          *Binaries
	ws            *Workspace
	opts          DaemonOpts
}

// StartDaemon launches a daemon configured for the e2e scenario. The test
// fails unless the exporter reports ready AND the global snapshot.json has
// been produced within the timeout.
func StartDaemon(t *testing.T, bins *Binaries, ws *Workspace, opts DaemonOpts) *DaemonProc {
	t.Helper()
	opts = opts.withDefaults(ws)
	root := RepoRoot(t)
	requirePortFree(t, daemonExporterAddr, "daemon exporter /metrics (hardcoded :19320)")

	snapDir := filepath.Join(ws.Dir, "snapshot")
	dataDir := filepath.Join(ws.Dir, "data")
	cfgPath := filepath.Join(ws.Dir, "daemon.yaml")

	// Build the YAML config: each optional block contributes its own
	// section; the order follows the shipped catmonitor.yaml layout.
	var b strings.Builder
	fmt.Fprintf(&b, "collectors:\n  cpu: { enabled: true, interval: %s }\n  memory: { enabled: true, interval: %s }\n",
		opts.CPUInterval, opts.MemoryInterval)
	fmt.Fprintf(&b, "storage:\n  data_dir: %s\n", dataDir)
	fmt.Fprintf(&b, "features: [%s]\n", strings.Join(opts.Features, ", "))
	if opts.MinPriority != "" {
		fmt.Fprintf(&b, "collection:\n  min_priority: %s\n", opts.MinPriority)
	}
	if opts.FaultSubEnabled {
		requirePortFree(t, opts.FaultSubRestAddr, "faultsub REST API")
		fmt.Fprintf(&b, "faultsub:\n  enabled: true\n  rest_addr: %q\n", opts.FaultSubRestAddr)
	} else {
		b.WriteString("faultsub:\n  enabled: false\n")
	}
	if opts.StressEnabled {
		fmt.Fprintf(&b, `stress:
  enabled: true
  control_socket: %q
  benchmarks:
    stream: { enabled: true, plugin: stream, container: catmonitor-stress-cpu, user: "65532:65532", timeout: 1m }
`, opts.ControlSocket)
	} else {
		b.WriteString("stress:\n  enabled: false\n")
	}
	fmt.Fprintf(&b, "snapshot:\n  enabled: true\n  dir: %s\n", snapDir)
	if opts.StragglerOutputEnabled {
		fmt.Fprintf(&b, "straggler_output:\n  enabled: true\n  data_dir: %q\n  flush_interval: %s\n",
			opts.StragglerDataDir, opts.StragglerFlushInterval)
	} else {
		b.WriteString("straggler_output:\n  enabled: false\n")
	}

	if err := os.WriteFile(cfgPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write daemon config: %v", err)
	}

	d := &DaemonProc{
		root:          root,
		configPath:    cfgPath,
		snapshotDir:   snapDir,
		dataDir:       dataDir,
		faultSubAddr:  opts.FaultSubRestAddr,
		controlSocket: opts.ControlSocket,
		stragglerDir:  opts.StragglerDataDir,
		bins:          bins,
		ws:            ws,
		opts:          opts,
	}
	d.proc = d.launch(t)
	waitFileExists(t, d.GlobalPath(), readyTimeout)
	waitHTTPReady(t, "http://"+daemonExporterAddr+"/-/ready", readyTimeout)

	// Ensure the snapshot tree is world-readable for privilege-dropped
	// consumers (TC-031): the daemon may create it with restrictive umask.
	_ = os.Chmod(snapDir, 0o755)
	if entries, err := os.ReadDir(snapDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				_ = os.Chmod(filepath.Join(snapDir, e.Name()), 0o644)
			}
		}
	}
	return d
}

func (d *DaemonProc) launch(t *testing.T) *Proc {
	t.Helper()
	env := []string{"CATMONITOR_METRICS=" + filepath.Join(d.root, "configs", "metrics.yaml")}
	if d.opts.EnvlessProc {
		env = append(env, "PATH="+t.TempDir())
	}
	return StartProcSpec(t, ProcSpec{
		Name: "daemon", LogPath: filepath.Join(d.ws.Dir, "daemon.log"), Dir: d.root,
		Env:  env,
		Argv: []string{d.bins.Daemon, "daemon", "-config", d.configPath},
	})
}

// SnapshotDir is the directory the daemon writes snapshot files into.
func (d *DaemonProc) SnapshotDir() string { return d.snapshotDir }

// DataDir is the daemon's JSONL data directory.
func (d *DaemonProc) DataDir() string { return d.dataDir }

// GlobalPath is the daemon's global snapshot.json path.
func (d *DaemonProc) GlobalPath() string { return filepath.Join(d.snapshotDir, "snapshot.json") }

// MetricsURL is the daemon's Prometheus-format metrics endpoint.
func (d *DaemonProc) MetricsURL() string { return "http://" + daemonExporterAddr + "/metrics" }

// FaultSubAddr is the configured faultsub REST address.
func (d *DaemonProc) FaultSubAddr() string { return d.faultSubAddr }

// ControlSocket is the stress control socket path.
func (d *DaemonProc) ControlSocket() string { return d.controlSocket }

// StragglerDir is the straggler KPI output directory.
func (d *DaemonProc) StragglerDir() string { return d.stragglerDir }

// Global reads the current global snapshot from disk.
func (d *DaemonProc) Global(t *testing.T) *snapshot.GlobalSnapshot {
	t.Helper()
	g, err := snapshot.ReadGlobal(d.GlobalPath())
	if err != nil {
		t.Fatalf("read global snapshot: %v", err)
	}
	return g
}

// Stop terminates the daemon. Idempotent.
func (d *DaemonProc) Stop() { d.proc.Stop() }

// Alive reports whether the daemon process is still running.
func (d *DaemonProc) Alive() bool { return d.proc.Alive() }

// Restart stops the daemon and starts a fresh instance with the same
// config, then waits until the global snapshot carries a NEW session_id.
func (d *DaemonProc) Restart(t *testing.T) *snapshot.GlobalSnapshot {
	t.Helper()
	oldID := d.Global(t).SessionID
	d.Stop()
	requirePortFree(t, daemonExporterAddr, "daemon exporter after restart")
	d.proc = d.launch(t)

	deadline := time.Now().Add(readyTimeout)
	for time.Now().Before(deadline) {
		if g, err := snapshot.ReadGlobal(d.GlobalPath()); err == nil && g.SessionID != "" && g.SessionID != oldID {
			waitHTTPReady(t, "http://"+daemonExporterAddr+"/-/ready", readyTimeout)
			return g
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("daemon restart: no new session_id in %s within %s", d.GlobalPath(), readyTimeout)
	return nil
}

// WaitComponentSnapshot blocks until the daemon has written the
// per-component snapshot file (e.g. snapshot_cpu.json).
//
// This is the STRONGEST readiness gate for consumer-side assertions:
// PerCompWriter delegates to the inner storage chain (exporter cache +
// JSONL) BEFORE writing its own file, so the file's existence implies the
// whole chain already holds the same batch. On hosts with a slow BMC,
// `ipmitool sensor list` can take ~10s, delaying the first ipmi-dependent
// collector cycle (cpu/memory/chassis) even though disk/network write
// immediately — the plain ready gate is NOT enough before asserting on
// specific components.
func (d *DaemonProc) WaitComponentSnapshot(t *testing.T, component string, timeout time.Duration) {
	t.Helper()
	waitFileExists(t, filepath.Join(d.snapshotDir, "snapshot_"+component+".json"), timeout)
}

// WaitMetricsFamily blocks until the daemon's /metrics output contains the
// given metric-family prefix. Prefer WaitComponentSnapshot when assertions
// read snapshot files (cache-vs-file race, see above).
func (d *DaemonProc) WaitMetricsFamily(t *testing.T, family string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(d.MetricsURL())
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if strings.Contains(string(body), family) {
				return
			}
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("%s did not expose %q within %s (slow BMC? collector failure? see the daemon log dump)",
		d.MetricsURL(), family, timeout)
}

// WaitGlobalSnapshot blocks until the global snapshot.json exists in the
// given snapshot directory (used by scenarios that manage the daemon
// process manually, e.g. EX-1's disk-full setup).
func WaitGlobalSnapshot(t *testing.T, snapshotDir string, timeout time.Duration) {
	t.Helper()
	waitFileExists(t, filepath.Join(snapshotDir, "snapshot.json"), timeout)
}

// GlobalSnapshotPath returns <snapshotDir>/snapshot.json.
func GlobalSnapshotPath(snapshotDir string) string {
	return filepath.Join(snapshotDir, "snapshot.json")
}
