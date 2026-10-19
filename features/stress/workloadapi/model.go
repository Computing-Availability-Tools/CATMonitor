package workloadapi

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

// ModelPreflightTimeout includes runtime, operator catalog and real model/HBM
// imports. It is not a training duration or an unbounded readiness wait.
const ModelPreflightTimeout = 60 * time.Second

func PreflightTimeout(benchmark string, model bool) time.Duration {
	if benchmark == "npu_burn" {
		if model {
			return ModelPreflightTimeout
		}
		return 30 * time.Second
	}
	return 2 * time.Second
}

var modelReferenceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// ModelRequest is distinct from operator selection and accepts no user paths.
type ModelRequest struct {
	Name        string `json:"name"`
	Action      string `json:"action"`
	ReferenceID string `json:"reference_id"`
}

func (m *ModelRequest) Validate() error {
	if m == nil {
		return errors.New("model request is missing")
	}
	if m.Name != "gpt3" && m.Name != "llama2" {
		return errors.New("model must be gpt3 or llama2")
	}
	if m.Action != "create_reference" && m.Action != "compare" {
		return errors.New("model action must be create_reference or compare")
	}
	if !modelReferenceID.MatchString(m.ReferenceID) {
		return errors.New("reference_id must be a safe name, not a path")
	}
	return nil
}

func CopyModel(m *ModelRequest) *ModelRequest {
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}

type ModelResult struct {
	JobID           string                     `json:"job_id"`
	Model           string                     `json:"model"`
	Action          string                     `json:"action"`
	ReferenceID     string                     `json:"reference_id"`
	Devices         DeviceIDs                  `json:"devices"`
	Outcome         string                     `json:"outcome"`
	CheckedSteps    int                        `json:"checked_steps"`
	ExpectedSteps   int                        `json:"expected_steps"`
	SavedSteps      int                        `json:"saved_steps"`
	Errors          int                        `json:"errors"`
	CSV             string                     `json:"csv"`
	MemoryMode      string                     `json:"memory_mode,omitempty"`
	EffectiveConfig map[string]json.RawMessage `json:"effective_config,omitempty"`
	SDCTolerances   *SDCTolerances             `json:"sdc_tolerances,omitempty"`
}

func (r *ModelResult) Validate(m *ModelRequest, devices DeviceIDs, jobID string) error {
	if r != nil && r.MemoryMode != "" && r.MemoryMode != "standard" && r.MemoryMode != "checkpointed" {
		return errors.New("invalid model memory mode")
	}
	if r == nil || m == nil || r.Model != m.Name || r.Action != m.Action || r.ReferenceID != m.ReferenceID || r.JobID != jobID || !SameDevices(r.Devices, devices) {
		return errors.New("model result does not belong to this job/request")
	}
	if r.Errors != 0 || r.CSV == "" || r.ExpectedSteps <= 0 {
		return errors.New("model errors, missing CSV or missing step contract")
	}
	if m.Action == "create_reference" {
		if r.Outcome != "reference_generated" || r.SavedSteps != r.ExpectedSteps || r.CheckedSteps != 0 || len(devices) != 1 {
			return errors.New("invalid reference generation result")
		}
	} else if r.Outcome != "sdc_passed" || r.CheckedSteps != r.ExpectedSteps*len(devices) || r.SavedSteps != 0 {
		return errors.New("model did not perform SDC comparison")
	}
	return nil
}
