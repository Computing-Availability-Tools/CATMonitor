//go:build e2e

package scenarios

import (
	"os"
	"strings"
	"testing"
	"time"

	e2e "github.com/Computing-Availability-Tools/CATMonitor/tests/e2e/framework"
)

// TestSYS006StressSecurity covers SYS-006 (stress 操作安全门控).
//
// | TC   | Priority | Description                                        |
// |------|----------|----------------------------------------------------|
// | TC-031| P0       | control socket 非 root 进程无法连接                |
// | TC-032| P0       | stress 端点三道防线行为                            |
// | TC-033| P1       | stress socket 不可达→优雅降级                      |
func TestSYS006StressSecurity(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{StressEnabled: true})
	e2e.WaitFileExists(t, d.ControlSocket(), 10*time.Second)

	// TC-032: same-user web with socket access → three defenses.
	w := e2e.StartWebSpec(t, bins, ws, e2e.WebSpec{
		SnapshotDir:   d.SnapshotDir(),
		ControlSocket: d.ControlSocket(),
	})
	api := e2e.NewStressAPI()
	api.WaitEndpoint(t, 10*time.Second)

	t.Run("TC-032_three_defenses", func(t *testing.T) {
		if status, _ := api.StartRunNoAction(t); status != 403 {
			t.Errorf("missing X-CATMonitor-Action: status %d (want 403)", status)
		}
		if status, _ := api.StartRunWrongContentType(t); status != 415 {
			t.Errorf("wrong Content-Type: status %d (want 415)", status)
		}
		if status, _ := api.StartRunForeignOrigin(t); status != 403 {
			t.Errorf("foreign Origin: status %d (want 403)", status)
		}
		// Legitimate shape: all defenses satisfied.
		status, body := api.StartRun(t, []string{}, 1)
		switch {
		case status == 202:
			// Controller accepted.
		case status >= 400 && status < 600:
			// Proxy error from controller (executor unavailable is valid).
			if !strings.Contains(body, "error") {
				t.Errorf("legit start: status %d body %q — expected structured error", status, body)
			}
		default:
			t.Errorf("legit start: unexpected status %d", status)
		}
	})

	// TC-031: non-root web cannot reach the socket.
	w.Stop()
	e2e.WaitPortFree(t, e2e.WebAddr(), 10*time.Second)
	nr := e2e.StartWebAs(t, bins, ws, d.SnapshotDir(), "nobody", d.ControlSocket())
	napi := e2e.NewStressAPI()
	napi.WaitEndpoint(t, 10*time.Second)

	t.Run("TC-031_socket_permission", func(t *testing.T) {
		// Socket mode is 0660 root:root.
		info, err := os.Lstat(d.ControlSocket())
		if err != nil {
			t.Fatalf("stat control socket: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o660 {
			t.Fatalf("control socket mode = %o, want 0660", got)
		}

		// Non-root web: stress config reports available:false.
		if status, body := napi.Config(t); status == 200 && strings.Contains(body, `"available":true`) {
			t.Error("non-root web reached stress config through 0660 root socket")
		}

		// Snapshot read path still works.
		if snap := nr.Snapshot(t); snap.SessionID == "" {
			t.Error("non-root web cannot serve /api/snapshot (snapshot read path broken)")
		}
	})
}

// TestSYS006_033_GracefulDegradation covers TC-033 (socket 不可达→优雅降级).
// Daemon WITHOUT stress → web stress endpoints degrade but web works.
func TestSYS006_033_GracefulDegradation(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{}) // no stress
	w := e2e.StartWeb(t, bins, ws, d.SnapshotDir())
	_ = w

	api := e2e.NewStressAPI()
	api.WaitEndpoint(t, 10*time.Second)

	// Stress config returns 200 with available:false (graceful degradation).
	status, body := api.Config(t)
	if status != 200 {
		t.Errorf("stress config: status %d, want 200 (graceful)", status)
	}
	if strings.Contains(body, `"available":true`) {
		t.Error("available should be false when stress controller is not running")
	}

	// Web snapshot still works.
	snapURL := "http://" + e2e.WebAddr() + "/api/snapshot"
	if resp, err := httpGet(snapURL); err == nil {
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("/api/snapshot: status %d (should be unaffected)", resp.StatusCode)
		}
	}
}
