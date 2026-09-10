//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/snapshot"
)

// WebProc is a supervised catmonitor-web read-only consumer.
type WebProc struct {
	proc *Proc
	base string
}

// WebSpec describes how to launch one web instance.
type WebSpec struct {
	SnapshotDir   string
	ControlSocket string // "" uses /run/catmonitor/control.sock
	RunAsUser     string  // "" = current user; requires root to set
}

// StartWeb launches web on 127.0.0.1:19322 consuming snapshotDir.
func StartWeb(t *testing.T, bins *Binaries, ws *Workspace, snapshotDir string) *WebProc {
	t.Helper()
	return StartWebSpec(t, bins, ws, WebSpec{SnapshotDir: snapshotDir})
}

// StartWebSpec launches a web instance honoring the full spec.
func StartWebSpec(t *testing.T, bins *Binaries, ws *Workspace, spec WebSpec) *WebProc {
	t.Helper()
	requirePortFree(t, webAddr, "web listener (no silent fallback to :19323 wanted)")
	controlSocket := spec.ControlSocket
	if controlSocket == "" {
		controlSocket = "/run/catmonitor/control.sock"
	}
	logPath := filepath.Join(ws.Dir, "web.log")
	if spec.RunAsUser != "" {
		logPath = logPath + "-" + spec.RunAsUser + ".log"
	}
	proc := StartProcSpec(t, ProcSpec{
		Name: "web", LogPath: logPath, RunAsUser: spec.RunAsUser,
		Argv: []string{bins.Web, "-addr", webAddr, "-snapshot-dir", spec.SnapshotDir, "-control-socket", controlSocket},
	})
	waitHTTPReady(t, "http://"+webAddr+"/", readyTimeout)
	return &WebProc{proc: proc, base: "http://" + webAddr}
}

// StartWebAs launches web as the given user with the given control socket.
func StartWebAs(t *testing.T, bins *Binaries, ws *Workspace, snapshotDir, user, controlSocket string) *WebProc {
	t.Helper()
	return StartWebSpec(t, bins, ws, WebSpec{
		SnapshotDir:   snapshotDir,
		ControlSocket: controlSocket,
		RunAsUser:     user,
	})
}

// Snapshot fetches /api/snapshot (typed with the product's model).
func (w *WebProc) Snapshot(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	var snap snapshot.Snapshot
	getJSON(t, w.base+"/api/snapshot", &snap)
	return &snap
}

// Stop terminates the web process. Idempotent.
func (w *WebProc) Stop() { w.proc.Stop() }

// WebAddr is the web listener address used by the framework.
func WebAddr() string { return webAddr }

// DfeeProc is a supervised catmonitor-dfee read-only consumer.
type DfeeProc struct {
	proc *Proc
	base string
}

// StartDfee launches dfee on 127.0.0.1:19323 consuming snapshotDir.
func StartDfee(t *testing.T, bins *Binaries, ws *Workspace, snapshotDir string) *DfeeProc {
	t.Helper()
	requirePortFree(t, dfeeAddr, "dfee listener")
	proc := StartProc(t, "dfee", filepath.Join(ws.Dir, "dfee.log"), "", nil,
		bins.Dfee, "-addr", dfeeAddr, "-snapshot-dir", snapshotDir)
	waitHTTPReady(t, "http://"+dfeeAddr+"/api/dfee", readyTimeout)
	return &DfeeProc{proc: proc, base: "http://" + dfeeAddr}
}

// DfeeResponse is the subset of /api/dfee that scenarios assert on.
type DfeeResponse struct {
	Timestamp       time.Time   `json:"timestamp"`
	RefreshInterval int         `json:"refresh_interval_ms"`
	Charts          []DfeeChart `json:"charts"`
}

// DfeeChart is one chart group of the dfee model.
type DfeeChart struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	YUnit  string     `json:"y_unit"`
	Series []DfeeSery `json:"series"`
}

// DfeeSery is one series of a chart group.
type DfeeSery struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// API fetches /api/dfee.
func (d *DfeeProc) API(t *testing.T) DfeeResponse {
	t.Helper()
	var resp DfeeResponse
	getJSON(t, d.base+"/api/dfee", &resp)
	return resp
}

// Stop terminates the dfee process. Idempotent.
func (d *DfeeProc) Stop() { d.proc.Stop() }

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("GET %s: decode json: %v", url, err)
	}
}
