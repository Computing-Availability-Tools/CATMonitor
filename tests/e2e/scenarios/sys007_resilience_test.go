//go:build e2e

package scenarios

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	e2e "github.com/Computing-Availability-Tools/CATMonitor/tests/e2e/framework"
)

// TestSYS007_025_ToolsMissing covers TC-025 (硬件/工具缺失→部件指标空).
func TestSYS007_025_ToolsMissing(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	// EnvlessProc: daemon starts with empty PATH (no external tools).
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{EnvlessProc: true})
	d.WaitComponentSnapshot(t, "cpu", 60*time.Second)

	body, _ := e2e.HTTPGet(t, d.MetricsURL())

	// Daemon is alive.
	if !d.Alive() {
		t.Fatal("daemon died with empty PATH")
	}

	// CPU metrics are present (from /proc, no external tool needed).
	if !strings.Contains(body, "catmonitor_cpu_usage") {
		t.Error("cpu metrics missing (should work from /proc without tools)")
	}

	// Memory metrics are present (from /proc).
	if !strings.Contains(body, "catmonitor_memory_usage") {
		t.Error("memory metrics missing (should work from /proc)")
	}

	// GPU metrics should be ABSENT (no nvidia-smi with empty PATH).
	if strings.Contains(body, "catmonitor_gpu_") {
		t.Error("gpu metrics present with empty PATH (nvidia-smi unreachable)")
	}
}

// TestSYS007_027_DiskFull covers TC-027 (JSONL 目录写满→daemon 不崩且自愈).
// Requires root (tmpfs mount); skips otherwise.
func TestSYS007_027_DiskFull(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("tmpfs mount requires root; rerun as root")
	}
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)

	// A tmpfs as the daemon's data_dir.
	fullDir := filepath.Join(ws.Dir, "fulldisk")
	if err := os.MkdirAll(fullDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mount := exec.Command("mount", "-t", "tmpfs", "-o", "size=1m", "tmpfs", fullDir)
	if out, err := mount.CombinedOutput(); err != nil {
		t.Skipf("tmpfs mount failed (%v: %s)", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("umount", fullDir).Run() })

	// Start daemon with the tmpfs as data_dir.
	snapDir := filepath.Join(ws.Dir, "snapshot")
	cfg := "collectors:\n  cpu: { enabled: true, interval: 500ms }\n" +
		"storage:\n  data_dir: " + fullDir + "\n" +
		"features: [web]\n" +
		"snapshot:\n  enabled: true\n  dir: " + snapDir + "\n" +
		"faultsub: { enabled: false }\nstress: { enabled: false }\nstraggler_output: { enabled: false }\n"
	cfgPath := filepath.Join(ws.Dir, "daemon.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	root := e2e.RepoRoot(t)
	d := e2e.StartProcSpec(t, e2e.ProcSpec{
		Name: "daemon", LogPath: filepath.Join(ws.Dir, "daemon.log"), Dir: root,
		Env: []string{"CATMONITOR_METRICS=" + filepath.Join(root, "configs", "metrics.yaml")},
		Argv: []string{bins.Daemon, "daemon", "-config", cfgPath},
	})
	e2e.WaitGlobalSnapshot(t, snapDir, 30*time.Second)

	// Baseline: JSONL is being written.
	jsonlPath := filepath.Join(fullDir, "cpu_"+time.Now().Format("2006-01-02")+".jsonl")
	e2e.WaitFileExists(t, jsonlPath, 15*time.Second)

	// Fill the disk.
	filler, err := os.OpenFile(filepath.Join(fullDir, "filler"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("create filler: %v", err)
	}
	chunk := make([]byte, 64*1024)
	for i := 0; i < 64; i++ {
		if _, err := filler.Write(chunk); err != nil {
			break
		}
	}
	filler.Close()

	// Daemon survives with a full disk.
	time.Sleep(3 * time.Second)
	if !d.Alive() {
		t.Fatal("daemon died when data dir filled up")
	}

	// /metrics still serves from the in-memory cache.
	body, _ := e2e.HTTPGet(t, "http://127.0.0.1:19320/metrics")
	if !strings.Contains(body, "catmonitor_cpu") {
		t.Error("/metrics lost cpu metrics during disk-full")
	}

	// Snapshot (independent dir) still updates.
	staleStat, _ := os.Stat(e2e.GlobalSnapshotPath(snapDir))
	time.Sleep(3 * time.Second)
	freshStat, err := os.Stat(e2e.GlobalSnapshotPath(snapDir))
	if err != nil {
		t.Fatalf("snapshot.json vanished: %v", err)
	}
	if !freshStat.ModTime().After(staleStat.ModTime()) {
		t.Error("snapshot.json stopped updating during disk-full")
	}

	// Free space and verify JSONL self-heals.
	os.Remove(filepath.Join(fullDir, "filler"))
	before := lineCount(t, jsonlPath)
	time.Sleep(5 * time.Second)
	after := lineCount(t, jsonlPath)
	if after <= before {
		t.Errorf("JSONL did not resume: before=%d after=%d lines", before, after)
	}
}

func lineCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "\n")
}
