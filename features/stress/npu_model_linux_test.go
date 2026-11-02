//go:build linux

package stress

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadplugin"
)

func TestRealModelDescribeProfileMatchesControllerContract(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string, mode os.FileMode) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("davinci5", "", 0600)
	pci := write("lspci", "#!/bin/sh\necho '0000:81:00.0 Processing accelerators: Huawei Device d803'\n", 0755)
	preflight := write("preflight", "#!/bin/sh\nexit 0\n", 0755)
	for key, value := range map[string]string{
		"NPU_BURN_DEVICE_ROOT": root, "CATMONITOR_LSPCI": pci,
		"NPU_BURN_PREFLIGHT_EXECUTABLE": preflight, "NPU_BURN_DEVICE": "0",
		"NPU_BURN_OUTPUT_DIR": root, "NPU_BURN_LOG_DIR": root, "HOME": root,
		"NPU_BURN_CHIP_GENERATION": "A2", "NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "300",
		"NPU_BURN_RUN_CASE": "matmul", "NPU_BURN_GROUP": "",
		"CATMONITOR_NPU_DEVICE_MODE": "full", "CATMONITOR_NPU_REQUIRE_IDLE": "0",
		"CATMONITOR_NPU_COMPAT_PROFILE": "none", "CATMONITOR_NPU_MODEL": "gpt3",
		"CATMONITOR_NPU_MODEL_ACTION": "create_reference", "CATMONITOR_NPU_MODEL_REFERENCE": "ref1",
	} {
		t.Setenv(key, value)
	}
	for _, capacity := range []struct {
		status string
		free   string
		want   CheckStatus
	}{{"pass", "16", CheckPass}, {"fail", "4", CheckFail}} {
		body := `{"model":"gpt3","action":"create_reference","reference_id":"ref1","devices":[0],"preflight":"` + capacity.status + `","memory_preflight":{"status":"` + capacity.status + `","scope":"basic_lower_bound","required_bytes":8,"full_peak_known":false,"devices":[{"device":0,"free_bytes":` + capacity.free + `,"total_bytes":32,"status":"` + capacity.status + `"}]}}`
		exe := write("model-adapter", "#!/bin/sh\nprintf '%s\\n' '"+body+"'\nexit 0\n", 0755)
		t.Setenv("NPU_BURN_EXECUTABLE", exe)
		profile, err := workloadplugin.Describe(context.Background(), "npu_burn")
		if err != nil {
			t.Fatal(err)
		}
		if err := validateDescribeProfile("npu_burn", profile); err != nil {
			t.Fatalf("real plugin profile violates Controller contract: %v", err)
		}
		if profile.Preflight.Status != capacity.want {
			t.Fatalf("RC=0 must not override JSON preflight: %s, want %s", profile.Preflight.Status, capacity.want)
		}
		if capacity.want == CheckFail {
			for _, asset := range profile.Assets {
				if asset.Name == "model_reference_preflight" && asset.Status != CheckPass {
					t.Fatalf("HBM failure must not be labelled missing reference: %+v", asset)
				}
			}
		}
	}
	for _, reason := range []string{"reference_missing", "reference_mismatch", "dependency_unavailable"} {
		exe := write("model-adapter", "#!/bin/sh\nprintf '%s\\n' '{\"preflight\":\"fail\",\"code\":\""+reason+"\",\"message\":\"synthetic readonly blocker\"}'\nexit 1\n", 0755)
		t.Setenv("NPU_BURN_EXECUTABLE", exe)
		profile, err := workloadplugin.Describe(context.Background(), "npu_burn")
		if err != nil || profile.Preflight.Status != CheckFail || parameterValue(profile, "model_preflight_code") != reason {
			t.Fatalf("failed Describe must retain typed reason: %+v %v", profile, err)
		}
	}
}

type modelExecutor struct {
	selectionExecutor
	modelBudget time.Duration
}

func (f *modelExecutor) Describe(ctx context.Context, b Binding) (*ExecutionProfile, error) {
	p, err := f.selectionExecutor.Describe(ctx, b)
	if b.NPUModel != nil {
		deadline, ok := ctx.Deadline()
		if ok {
			f.modelBudget = time.Until(deadline)
		}
		p.Parameters = append(p.Parameters, ProfileParameter{Key: "model", Label: "Model", Value: b.NPUModel.Name}, ProfileParameter{Key: "model_action", Label: "Model action", Value: b.NPUModel.Action}, ProfileParameter{Key: "model_reference", Label: "Reference ID", Value: b.NPUModel.ReferenceID})
	}
	return p, err
}
func (f *modelExecutor) Run(ctx context.Context, b Binding, r workloadapi.Request) (workloadapi.Result, error) {
	out, err := f.selectionExecutor.Run(ctx, b, r)
	if r.NPUModel != nil {
		out.NPUModel = &workloadapi.ModelResult{JobID: r.JobID, Model: r.NPUModel.Name, Action: r.NPUModel.Action, ReferenceID: r.NPUModel.ReferenceID, Devices: r.NPUDevices, ExpectedSteps: 3, CSV: "/results/current.csv", Outcome: "sdc_passed", CheckedSteps: 3 * len(r.NPUDevices)}
		if r.NPUModel.Action == "create_reference" {
			out.NPUModel.Outcome = "reference_generated"
			out.NPUModel.CheckedSteps = 0
			out.NPUModel.SavedSteps = 3
		}
		out.Source = "model_sdc"
	}
	return out, err
}
func TestNPUOperatorAndModelTwoHourJobLimit(t *testing.T) {
	for _, job := range []struct {
		name  string
		model *workloadapi.ModelRequest
	}{
		{name: "operator"},
		{name: "generate_reference", model: &workloadapi.ModelRequest{Name: "gpt3", Action: "create_reference", ReferenceID: "ref1"}},
		{name: "sdc_compare", model: &workloadapi.ModelRequest{Name: "llama2", Action: "compare", ReferenceID: "ref1"}},
	} {
		for _, timeout := range []time.Duration{0, 2 * time.Hour, 2*time.Hour + time.Second} {
			t.Run(job.name+"/"+timeout.String(), func(t *testing.T) {
				f := &modelExecutor{}
				m := testManager(t, f)
				m.cfg.Benchmarks["npu_burn"] = BenchmarkConfig{
					Enabled: true, Plugin: "npu_burn", Container: "npu", Timeout: 2 * time.Hour,
					AllowedNPUDevices: []int{0}, AllowedNPUModels: []string{"gpt3", "llama2"},
					AllowNPUReferenceGeneration: true,
				}
				started, err := m.StartWithOptions([]string{"npu_burn"}, RunOptions{
					Timeout: timeout, NPUDevices: workloadapi.DeviceIDs{0}, NPUModel: workloadapi.CopyModel(job.model),
				})
				if timeout > 2*time.Hour {
					if err == nil || !strings.Contains(err.Error(), "exceeds configured maximum 2h0m0s") {
						t.Fatalf("7201s must be rejected before workload dispatch: %v", err)
					}
					f.selectionMu.Lock()
					defer f.selectionMu.Unlock()
					if len(f.bindings) != 0 {
						t.Fatal("over-budget job was dispatched")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				report := waitForTerminal(t, m, started.JobID)
				profile := report.Benchmarks[0].Profile
				if report.Status != StatusHealthy || profile.TimeoutSeconds != 7200 || parameterValue(profile, "maximum_job_timeout") != "7200" {
					t.Fatalf("job must use and archive the two-hour limit: %+v", report)
				}
			})
		}
	}
}

func TestModelJobAuthorizationAndHistory(t *testing.T) {
	f := &modelExecutor{}
	m := testManager(t, f)
	m.cfg.Benchmarks["npu_burn"] = BenchmarkConfig{Enabled: true, Plugin: "npu_burn", Container: "npu", Timeout: 60000000000, AllowedNPUDevices: []int{0, 1}, AllowedNPUModels: []string{"gpt3"}}
	req := RunOptions{NPUDevices: workloadapi.DeviceIDs{0}, NPUModel: &workloadapi.ModelRequest{Name: "gpt3", Action: "create_reference", ReferenceID: "ref1"}}
	if _, err := m.StartWithOptions([]string{"npu_burn"}, req); err == nil {
		t.Fatal("reference generation allowed without admin authorization")
	}
	cfg := m.cfg.Benchmarks["npu_burn"]
	cfg.AllowNPUReferenceGeneration = true
	m.cfg.Benchmarks["npu_burn"] = cfg
	started, err := m.StartWithOptions([]string{"npu_burn"}, req)
	if err != nil {
		t.Fatal(err)
	}
	if f.modelBudget < workloadapi.ModelPreflightTimeout || f.modelBudget > workloadapi.ModelPreflightTimeout+15*time.Second {
		t.Fatalf("model transport budget must allow the bounded inner probe: %s", f.modelBudget)
	}
	req.NPUModel.Name = "llama2"
	r := waitForTerminal(t, m, started.JobID)
	if r.Status != StatusHealthy || r.Benchmarks[0].NPUModel.Outcome != "reference_generated" || r.Benchmarks[0].NPUSelection.Model.Name != "gpt3" {
		t.Fatalf("%+v", r)
	}
	history, err := m.History(1)
	if err != nil || history[0].Benchmarks[0].NPUModel.SavedSteps != 3 {
		t.Fatalf("%+v %v", history, err)
	}
}

func TestNPUModelPreflightRejectsBusyAfterDeploymentPass(t *testing.T) {
	f := &modelExecutor{selectionExecutor: selectionExecutor{busy: true}}
	m := testManager(t, f)
	m.cfg.Benchmarks["npu_burn"] = BenchmarkConfig{Enabled: true, Plugin: "npu_burn", Container: "npu", Timeout: time.Minute,
		AllowedNPUDevices: []int{0, 1}, AllowedNPUModels: []string{"gpt3"}, AllowNPUReferenceGeneration: true}
	if available, reason := m.Availability("npu_burn"); !available {
		t.Fatalf("deployment is ready despite business occupancy: %s", reason)
	}
	request := ModelPreflightRequest{NPUDevices: workloadapi.DeviceIDs{0}, NPUModel: &workloadapi.ModelRequest{Name: "gpt3", Action: "create_reference", ReferenceID: "ref1"}}
	view := m.PreflightModel(context.Background(), request)
	if view.Status != "failed" || len(view.Failures) != 1 || view.Failures[0].Code != "idle_devices" || !strings.Contains(view.Failures[0].Message, "device busy") {
		t.Fatalf("model admission skipped idle check: %+v", view)
	}
	if f.modelBudget != 0 {
		t.Fatal("model/HBM probe ran despite known busy devices")
	}
}
