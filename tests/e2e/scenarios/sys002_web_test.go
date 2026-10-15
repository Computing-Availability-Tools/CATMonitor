//go:build e2e

package scenarios

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	e2e "github.com/Computing-Availability-Tools/CATMonitor/tests/e2e/framework"
)

// TestSYS002WebDashboard covers SYS-002 (Web 仪表盘展示实时监控数据).
// One shared daemon + web, 5 sub-tests; 3 dedicated tests for isolated
// web instances (empty dir / port fallback / read-only dir).
//
// | TC   | Priority | Description                                        |
// |------|----------|----------------------------------------------------|
// | TC-004| P0       | /api/snapshot 返回合并视图                          |
// | TC-005| P1       | /api/collectors 返回采集器元数据                    |
// | TC-006| P1       | /api/config 返回配置视图                            |
// | TC-014| P1       | 浏览器页面渲染(HTTP 级验证)                        |
// | TC-044| P1       | 并发请求不 panic                                    |
// | TC-015| P0       | snapshot 未就绪→503                                |
// | TC-017| P1       | 端口被占→自动+1 回退                                |
// | TC-043| P0       | web 只读契约(不写 snapshot 目录)                   |
func TestSYS002WebDashboard(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{})
	w := e2e.StartWeb(t, bins, ws, d.SnapshotDir())
	d.WaitComponentSnapshot(t, "cpu", 60*time.Second)

	t.Run("TC-004_api_snapshot", func(t *testing.T) {
		snap := w.Snapshot(t)
		if snap.SessionID == "" {
			t.Error("session_id is empty")
		}
		if snap.Health.Grade == "" {
			t.Error("health.grade is empty")
		}
		cpuMetrics := 0
		for _, m := range snap.Metrics {
			if m.Component == "cpu" {
				cpuMetrics++
			}
		}
		if cpuMetrics == 0 {
			t.Error("no cpu component metrics in merged view")
		}
	})

	t.Run("TC-005_api_collectors", func(t *testing.T) {
		body, _ := e2e.HTTPGet(t, "http://"+e2e.WebAddr()+"/api/collectors")
		if strings.Contains(body, `"system"`) {
			t.Error("collectors list contains 'system' (should not: not a registered collector)")
		}
		if !strings.Contains(body, `"cpu"`) || !strings.Contains(body, `"memory"`) {
			t.Error("collectors list missing cpu/memory")
		}
	})

	t.Run("TC-006_api_config", func(t *testing.T) {
		body, _ := e2e.HTTPGet(t, "http://"+e2e.WebAddr()+"/api/config")
		for _, want := range []string{"version", "started_at", "refresh_interval_ms", "history_points"} {
			if !strings.Contains(body, want) {
				t.Errorf("/api/config missing field %q", want)
			}
		}
	})

	t.Run("TC-014_page_rendering", func(t *testing.T) {
		// HTTP-level verification: the index page returns HTML that
		// references its static assets. Visual rendering is a manual/QA
		// concern (per the agreed handling of browser-dependent cases).
		body, contentType := e2e.HTTPGet(t, "http://"+e2e.WebAddr()+"/")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("index Content-Type = %q, want text/html", contentType)
		}
		if !strings.Contains(body, "<html") && !strings.Contains(body, "<!DOCTYPE") {
			t.Errorf("index body does not look like HTML")
		}
	})

	t.Run("TC-044_concurrent_requests", func(t *testing.T) {
		var wg sync.WaitGroup
		errs := make(chan error, 20)
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := http.Get("http://" + e2e.WebAddr() + "/api/snapshot")
				if err != nil {
					errs <- err
					return
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 200 {
					errs <- fmt.Errorf("status %d", resp.StatusCode)
					return
				}
				if !strings.Contains(string(body), "session_id") {
					errs <- fmt.Errorf("response missing session_id")
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
	})
}

// TestSYS002_015_NotReady503 covers TC-015 (snapshot 未就绪→503).
// Web pointed at an empty directory (no daemon running).
func TestSYS002_015_NotReady503(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	emptyDir := filepath.Join(ws.Dir, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = e2e.StartWeb(t, bins, ws, emptyDir) // lifecycle via t.Cleanup

	resp, err := http.Get("http://" + e2e.WebAddr() + "/api/snapshot")
	if err != nil {
		t.Fatalf("GET /api/snapshot: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// TestSYS002_017_PortFallback covers TC-017 (端口被占→自动+1 回退).
func TestSYS002_017_PortFallback(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)

	// Pre-occupy the default web port.
	blocker, err := net.Listen("tcp", "127.0.0.1:19322")
	if err != nil {
		t.Fatalf("pre-occupy 19322: %v", err)
	}
	defer blocker.Close()

	// Start web: it should fall back to 19323.
	proc := e2e.StartProc(t, "web-fallback", filepath.Join(ws.Dir, "web-fallback.log"), "", nil,
		bins.Web, "-addr", "127.0.0.1:19322", "-snapshot-dir", filepath.Join(ws.Dir, "snap"))

	// Wait for web to be reachable on the fallback port.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", "127.0.0.1:19323", 200*time.Millisecond); err == nil {
			conn.Close()
			proc.Stop()
			return // success: web bound 19323
		}
		time.Sleep(100 * time.Millisecond)
	}
	proc.Stop()
	t.Fatal("web did not bind :19323 within 15s (port fallback broken)")
}

// TestSYS002_043_ReadOnlyContract covers TC-043 (web 只读契约).
func TestSYS002_043_ReadOnlyContract(t *testing.T) {
	ws := e2e.NewWorkspace(t)
	bins := e2e.Build(t)
	d := e2e.StartDaemon(t, bins, ws, e2e.DaemonOpts{})
	d.WaitComponentSnapshot(t, "cpu", 60*time.Second)
	d.Stop() // stop daemon so web is alone with the read-only dir

	snapDir := d.SnapshotDir()
	if err := os.Chmod(snapDir, 0o555); err != nil {
		t.Fatalf("chmod 555: %v", err)
	}
	defer func() { _ = os.Chmod(snapDir, 0o755) }()

	before := countFiles(t, snapDir)

	w := e2e.StartWeb(t, bins, ws, snapDir)
	snap := w.Snapshot(t)
	if snap.SessionID == "" {
		t.Error("web cannot serve /api/snapshot from read-only dir")
	}

	after := countFiles(t, snapDir)
	if after != before {
		t.Errorf("web wrote to the snapshot directory: %d files before, %d after (read-only contract broken)", before, after)
	}
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}
