//go:build linux

package stress

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

type selectionExecutor struct {
	fakeExecutor
	selectionMu  sync.Mutex
	bindings     []Binding
	mismatch     bool
	busy         bool
	catalog      bool
	caseMismatch bool
}

func (f *selectionExecutor) Describe(_ context.Context, b Binding) (*ExecutionProfile, error) {
	p := validProfile(b.Benchmark)
	device := "0"
	if b.NPUDevices != nil {
		device = b.NPUDevices.String()
	}
	p.Parameters = []ProfileParameter{{Key: "devices", Label: "Devices", Value: device}, {Key: "available_devices", Label: "Available", Value: "0,1"}}
	if f.catalog {
		cases := "matmul"
		if b.NPUCases != nil {
			cases = b.NPUCases.String()
		}
		p.Parameters = append(p.Parameters, ProfileParameter{Key: "selected_cases", Label: "Selected operators", Value: cases}, ProfileParameter{Key: "available_cases", Label: "Available operators", Value: "matmul,conv2d"})
	}
	if f.busy && b.NPUDevices != nil {
		p.Preflight = PreflightResult{Status: CheckFail, Message: "device busy"}
	}
	return p, nil
}
func (f *selectionExecutor) Run(ctx context.Context, b Binding, r workloadapi.Request) (workloadapi.Result, error) {
	f.selectionMu.Lock()
	f.bindings = append(f.bindings, b)
	f.selectionMu.Unlock()
	out, err := f.fakeExecutor.Run(ctx, b, r)
	out.NPUDevices = append(workloadapi.DeviceIDs(nil), r.NPUDevices...)
	out.NPUCases = append(workloadapi.CaseNames(nil), r.NPUCases...)
	if f.caseMismatch {
		out.NPUCases = workloadapi.CaseNames{"matmul"}
	}
	if f.mismatch {
		out.NPUDevices = workloadapi.DeviceIDs{0}
	}
	return out, err
}
func selectionManager(t *testing.T, f *selectionExecutor) *Manager {
	m := testManager(t, f)
	m.cfg.Benchmarks["npu_burn"] = BenchmarkConfig{Enabled: true, Plugin: "npu_burn", Container: "npu", Timeout: time.Minute, AllowedNPUDevices: []int{0, 1}}
	return m
}

func TestNPUJobSelectionPinnedAndArchived(t *testing.T) {
	f := &selectionExecutor{}
	m := selectionManager(t, f)
	ids := workloadapi.DeviceIDs{1, 0}
	started, err := m.StartWithOptions([]string{"npu_burn"}, RunOptions{NPUDevices: ids})
	if err != nil {
		t.Fatal(err)
	}
	ids[0] = 999 // caller cannot mutate the frozen request
	r := waitForTerminal(t, m, started.JobID)
	if r.Status != StatusHealthy || !workloadapi.SameDevices(r.Benchmarks[0].NPUSelection.Actual, []int{0, 1}) {
		t.Fatalf("report=%+v", r)
	}
	history, err := m.History(1)
	if err != nil || !workloadapi.SameDevices(history[0].Benchmarks[0].NPUSelection.Requested, []int{1, 0}) {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	firstHash := r.ConfigurationSHA256
	profile := m.applyRunConfiguration(copyExecutionProfile(r.Benchmarks[0].Profile), "npu_burn", 10*time.Second)
	if profile.TimeoutSeconds != 10 || parameterValue(profile, "maximum_job_timeout") != "60" {
		t.Fatal("configured and effective timeout were conflated")
	}
	second, err := m.Start([]string{"npu_burn"})
	if err != nil {
		t.Fatal(err)
	}
	r = waitForTerminal(t, m, second.JobID)
	if !workloadapi.SameDevices(r.Benchmarks[0].NPUSelection.Selected, []int{0}) || r.ConfigurationSHA256 == firstHash {
		t.Fatal("job selection leaked into next job/default/hash")
	}
}

func TestNPUSelectionAdmissionAndCoverage(t *testing.T) {
	for _, ids := range []workloadapi.DeviceIDs{{}, {-1}, {1, 1}, {2}} {
		m := selectionManager(t, &selectionExecutor{})
		if _, err := m.StartWithOptions([]string{"npu_burn"}, RunOptions{NPUDevices: ids}); err == nil {
			t.Fatalf("accepted %v", ids)
		}
	}
	m := selectionManager(t, &selectionExecutor{})
	b := m.cfg.Benchmarks["npu_burn"]
	b.AllowedNPUDevices = nil
	m.cfg.Benchmarks["npu_burn"] = b
	if _, err := m.StartWithOptions([]string{"npu_burn"}, RunOptions{NPUDevices: workloadapi.DeviceIDs{1}}); err == nil {
		t.Fatal("visibility granted permission")
	}
	if _, err := m.StartWithOptions([]string{"stream"}, RunOptions{NPUDevices: workloadapi.DeviceIDs{0}}); err == nil {
		t.Fatal("CPU accepted NPU option")
	}
	m = selectionManager(t, &selectionExecutor{busy: true})
	if _, err := m.Start([]string{"npu_burn"}); err == nil {
		t.Fatal("busy accepted")
	}
	m = selectionManager(t, &selectionExecutor{mismatch: true})
	r, err := m.StartWithOptions([]string{"npu_burn"}, RunOptions{NPUDevices: workloadapi.DeviceIDs{0, 1}})
	if err != nil {
		t.Fatal(err)
	}
	if waitForTerminal(t, m, r.JobID).Status != StatusUnhealthy {
		t.Fatal("incomplete device coverage passed")
	}
}

func TestNPUWebCLIShareSelectionAndCancel(t *testing.T) {
	m := selectionManager(t, &selectionExecutor{fakeExecutor: fakeExecutor{block: true}, catalog: true})
	client, _ := startControlFixture(t, m)
	mux := http.NewServeMux()
	Register(mux, client, ":19322", nil)
	req := httptest.NewRequest("POST", "/api/stress/runs", strings.NewReader(`{"benchmarks":["npu_burn"],"npu_devices":[1],"npu_cases":["conv2d"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CATMonitor-Action", "stress")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var started Report
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	r, err := client.Job(context.Background(), started.JobID)
	if err != nil || r.Initiator != InitiatorWeb || r.Benchmarks[0].NPUSelection.Selected[0] != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if !workloadapi.SameCases(r.Benchmarks[0].NPUSelection.SelectedCases, []string{"conv2d"}) {
		t.Fatal("Web operator selection lost")
	}
	if _, err := m.Start([]string{"stream"}); !errors.Is(err, ErrBusy) {
		t.Fatal("overlap accepted")
	}
	if err := client.Cancel(context.Background(), r.JobID); err != nil {
		t.Fatal(err)
	}
	if waitForTerminal(t, m, r.JobID).Status != StatusCancelled {
		t.Fatal("cross-entry cancel failed")
	}
}

func TestNPURequestRejectsExplicitEmptyNullAndBadTypes(t *testing.T) {
	for _, value := range []string{`[]`, `null`, `[1,1]`, `[-1]`, `[1.1]`, `["1"]`, `"all"`} {
		var r ControlStartRequest
		if err := json.Unmarshal([]byte(`{"npu_devices":`+value+`}`), &r); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	var r ControlStartRequest
	if err := json.Unmarshal([]byte(`{"benchmarks":["npu_burn"]}`), &r); err != nil || r.NPUDevices != nil {
		t.Fatal("omitted should preserve default")
	}
}
