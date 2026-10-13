//go:build linux

package workloadplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

func environmentModel() *workloadapi.ModelRequest {
	return &workloadapi.ModelRequest{Name: env("CATMONITOR_NPU_MODEL"), Action: env("CATMONITOR_NPU_MODEL_ACTION"), ReferenceID: env("CATMONITOR_NPU_MODEL_REFERENCE")}
}

func modelArgs(m *workloadapi.ModelRequest, ids workloadapi.DeviceIDs) []string {
	return []string{"__model__", "--model", m.Name, "--action", m.Action, "--reference", m.ReferenceID, "--devices", ids.String()}
}

func describeNPUModel(ctx context.Context) *workloadapi.ExecutionProfile {
	p := describeNPUBurn(ctx) // Preserve runtime, mapping, idle, readonly-dir gates.
	// Models use direct synchronization, not the operator worker's timer.
	parameters := p.Parameters[:0]
	for _, item := range p.Parameters {
		if item.Key != "internal_timeout" {
			parameters = append(parameters, item)
		}
	}
	p.Parameters = parameters
	p.Resources.RuntimeSeconds = 0
	m := environmentModel()
	check := workloadapi.AssetCheck{Name: "model_reference_preflight", Path: env("NPU_BURN_EXECUTABLE"), Required: true, Kind: "model_sdc", Status: workloadapi.CheckPass, Message: "model imports/reference match passed; hardware validation is NOT_RUN"}
	memoryCheck := workloadapi.AssetCheck{Name: "model_memory_preflight", Path: env("NPU_BURN_EXECUTABLE"), Required: true, Kind: "memory", Status: workloadapi.CheckFail, Message: "basic HBM screening was not completed"}
	referenceConfirmed := false
	ids, err := workloadapi.ParseDevices(env("NPU_BURN_DEVICE"))
	if err == nil {
		err = m.Validate()
	}
	if err == nil && m.Action == "create_reference" && len(ids) != 1 {
		err = fmt.Errorf("reference generation requires one device")
	}
	if err == nil {
		args := append(modelArgs(m, ids), "--describe")
		probe := exec.CommandContext(ctx, env("NPU_BURN_EXECUTABLE"), args...)
		// Scope sanitization to metadata capture, not the training environment.
		probe.Env = append(os.Environ(), "LD_PRELOAD=")
		data, failure := probe.Output()
		if failure != nil {
			message := failure.Error()
			var diagnostic struct{ Preflight, Code, Message string }
			if json.Unmarshal(data, &diagnostic) == nil && diagnostic.Preflight == "fail" && diagnostic.Message != "" {
				switch diagnostic.Code {
				case "reference_missing", "reference_mismatch", "reference_incomplete", "reference_exists", "dependency_unavailable", "preflight_failed":
					p.Parameters = append(p.Parameters, parameter("model_preflight_code", "Model preflight reason", diagnostic.Code, ""))
					message = diagnostic.Message
				default:
					diagnostic.Preflight = "" // Unknown diagnostics use the ordinary error fallback.
				}
			}
			if exit, ok := failure.(*exec.ExitError); ok && len(exit.Stderr) > 0 {
				if diagnostic.Preflight != "fail" || diagnostic.Message == "" {
					message = truncate(string(exit.Stderr), 1024)
				}
			}
			err = fmt.Errorf("model dependency/reference preflight failed: %s", message)
		} else {
			var proof struct {
				Preflight   string                `json:"preflight"`
				Model       string                `json:"model"`
				Action      string                `json:"action"`
				ReferenceID string                `json:"reference_id"`
				Devices     workloadapi.DeviceIDs `json:"devices"`
				Memory      *modelMemoryProof     `json:"memory_preflight"`
			}
			if json.Unmarshal(data, &proof) != nil || proof.Model != m.Name || proof.Action != m.Action || proof.ReferenceID != m.ReferenceID || !workloadapi.SameDevices(ids, proof.Devices) {
				err = fmt.Errorf("model preflight JSON does not confirm request")
			} else if err = validateModelMemory(proof.Memory, ids); err != nil {
				referenceConfirmed = true
				memoryCheck.Message = err.Error()
			} else {
				memoryCheck.Status = workloadapi.CheckPass
				memoryCheck.Message = fmt.Sprintf("basic HBM screen passed (minimum %.2f GiB per device); full training peak is unknown and OOM remains possible", float64(proof.Memory.RequiredBytes)/(1<<30))
				p.Parameters = append(p.Parameters, parameter("model_basic_hbm_minimum", "Basic HBM minimum (not full training peak)", fmt.Sprintf("%.2f", float64(proof.Memory.RequiredBytes)/(1<<30)), "GiB"))
				if proof.Preflight != "pass" {
					err = fmt.Errorf("model preflight JSON reports failure")
				}
			}
		}
	}
	if err != nil {
		if !referenceConfirmed {
			check.Status, check.Message = workloadapi.CheckFail, err.Error()
		}
		p.Preflight.Status, p.Preflight.Message = workloadapi.CheckFail, err.Error()
	}
	p.Assets = append(p.Assets, check, memoryCheck)
	p.Parameters = append(p.Parameters, parameter("model", "Model", m.Name, ""), parameter("model_action", "Model action", m.Action, ""), parameter("model_reference", "Reference ID", m.ReferenceID, ""))
	p.Parameters = append(p.Parameters, parameter("model_device_strategy", "Model device strategy", "independent_sequential", ""))
	p.Resources.ProblemSize = m.Name
	return p
}

// ResolveRequest keeps models on the same executor/process-group/Cancel path.
func ResolveRequest(request workloadapi.Request) (Command, error) {
	if request.NPUModel == nil {
		return Resolve(request.Benchmark)
	}
	m := request.NPUModel
	if err := m.Validate(); err != nil {
		return Command{}, err
	}
	if request.Benchmark != "npu_burn" || request.NPUCases != nil {
		return Command{}, fmt.Errorf("invalid model benchmark/operator combination")
	}
	if err := workloadapi.ValidateDevices(request.NPUDevices); err != nil {
		return Command{}, err
	}
	if m.Action == "create_reference" && len(request.NPUDevices) != 1 {
		return Command{}, fmt.Errorf("reference generation requires one device")
	}
	bound := environmentModel()
	if *bound != *m {
		return Command{}, fmt.Errorf("model request differs from executor environment")
	}
	// The caller already verified request/environment device equality. Repeat
	// the complete runtime/model/idle preflight immediately before execution.
	ctx, cancel := context.WithTimeout(context.Background(), workloadapi.ModelPreflightTimeout)
	defer cancel()
	if p := describeNPUModel(ctx); p.Preflight.Status == workloadapi.CheckFail {
		return Command{}, fmt.Errorf("model preflight: %s", p.Preflight.Message)
	}
	output := env("NPU_BURN_OUTPUT_DIR")
	if err := writableDirectory("model output", output); err != nil {
		return Command{}, err
	}
	home, err := os.MkdirTemp(env("HOME"), "model-job-")
	if err != nil {
		return Command{}, err
	}
	resultPath := filepath.Join(output, request.JobID, "model-result.json")
	return Command{Path: env("NPU_BURN_EXECUTABLE"), Args: modelArgs(m, request.NPUDevices), Dir: home,
		Env:        []string{"HOME=" + home, "CATMONITOR_MODEL_JOB_ID=" + request.JobID, "OPENBLAS_NUM_THREADS=1", "OMP_NUM_THREADS=1", "MKL_NUM_THREADS=1", "NUMEXPR_NUM_THREADS=1", "BLIS_NUM_THREADS=1"},
		NPUDevices: append(workloadapi.DeviceIDs(nil), request.NPUDevices...), Cleanup: func() {
			_ = os.RemoveAll(home)
			// SIGKILL/timeout cannot run Python finally. Only this job's unpublished
			// staging tree is removed; published references are never deleted.
			if m.Action == "create_reference" {
				_ = os.RemoveAll(filepath.Join(envDefault("CATMONITOR_MODEL_REFERENCE_ROOT", "/var/lib/catmonitor/stress/model-references"), ".staging-"+request.JobID))
			}
		},
		ModelResult: func() (*workloadapi.ModelResult, error) {
			data, err := os.ReadFile(resultPath)
			if err != nil {
				return nil, err
			}
			var result workloadapi.ModelResult
			if err := json.Unmarshal(data, &result); err != nil {
				return nil, err
			}
			if err := result.Validate(m, request.NPUDevices, request.JobID); err != nil {
				return nil, err
			}
			if result.MemoryMode != envDefault("CATMONITOR_MODEL_MEMORY_MODE", "standard") {
				return nil, fmt.Errorf("model result memory mode differs from execution profile")
			}
			if !strings.HasPrefix(result.CSV, filepath.Dir(resultPath)+string(os.PathSeparator)) {
				return nil, fmt.Errorf("model CSV is outside this job directory")
			}
			if err := regularFile("model CSV", result.CSV); err != nil {
				return nil, err
			}
			file, err := os.Open(result.CSV)
			if err != nil {
				return nil, err
			}
			defer file.Close()
			if err := workloadapi.ValidateModelCSV(file, &result); err != nil {
				return nil, err
			}
			return &result, nil
		}}, nil
}
