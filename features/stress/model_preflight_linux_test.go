//go:build linux

package stress

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

type readonlyModelExecutor struct {
	modelExecutor
	failCode string
	status   CheckStatus
	calls    atomic.Int32
	entered  chan struct{}
	release  chan struct{}
}

func (f *readonlyModelExecutor) Describe(ctx context.Context, binding Binding) (*ExecutionProfile, error) {
	f.calls.Add(1)
	if f.entered != nil && binding.NPUModel != nil {
		select {
		case f.entered <- struct{}{}:
		default:
		}
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	profile, err := f.modelExecutor.Describe(ctx, binding)
	if err != nil || binding.NPUModel == nil {
		return profile, err
	}
	profile.Assets = []AssetCheck{{Name: "model_reference_preflight", Path: "/model", Required: true, Status: CheckPass, Message: "matched"}}
	if f.status != "" {
		profile.Preflight.Status = f.status
	}
	if f.failCode != "" {
		profile.Preflight = PreflightResult{Status: CheckFail, Message: "fixture blocker"}
		profile.Assets[0].Status, profile.Assets[0].Message = CheckFail, "only this check failed"
		profile.Parameters = append(profile.Parameters, ProfileParameter{Key: "model_preflight_code", Label: "Reason", Value: f.failCode})
		profile.Assets = append(profile.Assets,
			AssetCheck{Name: "model_memory_preflight", Path: "/model", Required: true, Status: CheckFail, Message: "basic HBM screening was not completed"},
			AssetCheck{Name: "optional", Path: "/optional", Required: false, Status: CheckFail, Message: "not required"})
	}
	return profile, nil
}

func readonlyModelManager(t *testing.T, executor *readonlyModelExecutor) *Manager {
	m := testManager(t, executor)
	m.cfg.Benchmarks["npu_burn"] = BenchmarkConfig{Enabled: true, Plugin: "npu_burn", Container: "npu", Timeout: time.Minute,
		AllowedNPUDevices: []int{0, 1}, AllowedNPUModels: []string{"gpt3", "llama2"}, AllowNPUReferenceGeneration: true}
	return m
}
func modelPreflightRequest() ModelPreflightRequest {
	return ModelPreflightRequest{NPUDevices: workloadapi.DeviceIDs{0}, NPUModel: &workloadapi.ModelRequest{Name: "gpt3", Action: "compare", ReferenceID: "ref1"}}
}

func TestModelPreflightReadonlyAndOnlyFailedChecks(t *testing.T) {
	for _, code := range []string{"", "reference_missing", "reference_mismatch", "reference_incomplete", "reference_exists", "dependency_unavailable"} {
		t.Run(code, func(t *testing.T) {
			executor := &readonlyModelExecutor{failCode: code}
			m := readonlyModelManager(t, executor)
			view := m.PreflightModel(context.Background(), modelPreflightRequest())
			if code == "" {
				if view.Status != "ready" || len(view.Failures) != 0 {
					t.Fatalf("%+v", view)
				}
			} else if view.Status != "failed" || len(view.Failures) != 1 || view.Failures[0].Code != code {
				t.Fatalf("only actual failure, not skipped HBM/optional checks: %+v", view)
			}
			if view.NPUModel.ReferenceID != "ref1" || !workloadapi.SameDevices(view.NPUDevices, []int{0}) || view.ConfigurationSHA256 == "" {
				t.Fatalf("selection identity missing: %+v", view)
			}
			if executor.runCalls != 0 || m.active != nil || m.last != nil {
				t.Fatal("preflight created/executed a job")
			}
			if _, err := os.Stat(m.cfg.ReportPath); !os.IsNotExist(err) {
				t.Fatalf("preflight wrote a report: %v", err)
			}
			history, err := m.History(10)
			if err != nil || len(history) != 0 {
				t.Fatalf("preflight wrote history: %+v %v", history, err)
			}
		})
	}
}

func TestModelPreflightRejectsUnsupportedAndInvalidSelection(t *testing.T) {
	executor := &readonlyModelExecutor{status: CheckUnsupported}
	m := readonlyModelManager(t, executor)
	if view := m.PreflightModel(context.Background(), modelPreflightRequest()); view.Status != "failed" {
		t.Fatalf("unsupported became ready: %+v", view)
	}
	executor.status = CheckWarn
	if view := m.PreflightModel(context.Background(), modelPreflightRequest()); view.Status != "ready" || len(view.Failures) != 0 {
		t.Fatalf("warning must not be relabelled as failure: %+v", view)
	}
	executor.status = ""
	for _, mutate := range []func(*ModelPreflightRequest){
		func(r *ModelPreflightRequest) { r.NPUModel = nil },
		func(r *ModelPreflightRequest) { r.NPUDevices = workloadapi.DeviceIDs{} },
		func(r *ModelPreflightRequest) { r.NPUDevices = workloadapi.DeviceIDs{2} },
		func(r *ModelPreflightRequest) { r.NPUModel.ReferenceID = "../unsafe" },
		func(r *ModelPreflightRequest) {
			r.NPUModel.Action = "create_reference"
			r.NPUDevices = workloadapi.DeviceIDs{0, 1}
		},
	} {
		r := modelPreflightRequest()
		mutate(&r)
		if view := m.PreflightModel(context.Background(), r); view.Status != "failed" {
			t.Fatalf("invalid request accepted: %+v", view)
		}
	}
	b := m.cfg.Benchmarks["npu_burn"]
	b.AllowedNPUModels = nil
	m.cfg.Benchmarks["npu_burn"] = b
	before := executor.calls.Load()
	if view := m.PreflightModel(context.Background(), modelPreflightRequest()); view.Failures[0].Code != "model_not_allowed" {
		t.Fatalf("%+v", view)
	}
	if executor.calls.Load() != before {
		t.Fatal("unauthorized request probed hardware")
	}
	b.AllowedNPUModels = []string{"gpt3"}
	b.AllowNPUReferenceGeneration = false
	m.cfg.Benchmarks["npu_burn"] = b
	r := modelPreflightRequest()
	r.NPUModel.Action = "create_reference"
	if view := m.PreflightModel(context.Background(), r); view.Failures[0].Code != "reference_generation_denied" {
		t.Fatalf("%+v", view)
	}
}

func TestModelPreflightDoesNotProbeActiveJobAndBoundsConcurrentProbe(t *testing.T) {
	executor := &readonlyModelExecutor{entered: make(chan struct{}, 1), release: make(chan struct{})}
	m := readonlyModelManager(t, executor)
	t.Cleanup(func() { m.mu.Lock(); m.active = nil; m.mu.Unlock() })
	m.mu.Lock()
	m.active = &activeJob{}
	m.mu.Unlock()
	view := m.PreflightModel(context.Background(), modelPreflightRequest())
	if view.Status != "not_checked" || view.Failures[0].Code != "controller_busy" || executor.calls.Load() != 0 {
		t.Fatalf("%+v", view)
	}
	m.mu.Lock()
	m.active = nil
	m.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan ModelPreflightView, 1)
	go func() { done <- m.PreflightModel(ctx, modelPreflightRequest()) }()
	select {
	case <-executor.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not enter")
	}
	before := executor.calls.Load()
	view = m.PreflightModel(context.Background(), modelPreflightRequest())
	if view.Status != "not_checked" || view.Failures[0].Code != "preflight_busy" || executor.calls.Load() != before {
		t.Fatalf("overlap: %+v", view)
	}
	cancel()
	select {
	case view = <-done:
		if view.Status != "not_checked" || view.Failures[0].Code != "preflight_interrupted" {
			t.Fatalf("cancelled metadata query became ready: %+v", view)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not honor caller cancellation")
	}
}

func TestModelPreflightWebUsesControllerAndExistingRequestPolicy(t *testing.T) {
	executor := &readonlyModelExecutor{failCode: "reference_missing"}
	m := readonlyModelManager(t, executor)
	client, _ := startControlFixture(t, m)
	mux := http.NewServeMux()
	Register(mux, client, ":19322", nil)
	body := `{"npu_devices":[0],"npu_model":{"name":"gpt3","action":"compare","reference_id":"ref1"}}`
	for _, tc := range []struct {
		name, body, content, action, origin string
		status                              int
	}{
		{"valid", body, "application/json", "stress", "http://node.example:19322", 200},
		{"no_header", body, "application/json", "", "", 403},
		{"cross_origin", body, "application/json", "stress", "http://evil.example", 403},
		{"wrong_content", body, "text/plain", "stress", "", 415},
		{"unknown_field", strings.TrimSuffix(body, "}") + `,"environment":{}}`, "application/json", "stress", "", 400},
		{"extra_json", body + ` {}`, "application/json", "stress", "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/stress/models/preflight", strings.NewReader(tc.body))
			r.Host = "node.example:19322"
			r.Header.Set("Content-Type", tc.content)
			r.Header.Set("X-CATMonitor-Action", tc.action)
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if w.Code == 200 {
				var view ModelPreflightView
				if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.Status != "failed" || len(view.Failures) != 1 || view.Failures[0].Code != "reference_missing" {
					t.Fatalf("%s %v", w.Body.String(), err)
				}
			}
		})
	}
	if executor.runCalls != 0 {
		t.Fatal("Web preflight ran a workload")
	}
	view, err := client.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range view.Benchmarks {
		if b.Name == "npu_burn" && (b.ModelCapabilities == nil || len(b.ModelCapabilities.AllowedModels) != 2 || !b.ModelCapabilities.AllowReferenceGeneration) {
			t.Fatalf("admin model capability missing: %+v", b)
		}
	}
}
