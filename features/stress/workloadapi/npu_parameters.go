package workloadapi

import (
	"encoding/json"
	"fmt"
)

// NPUOperatorParameters is a receipt of the TestCase configurations handed to
// the engines, not today's deployment defaults or evidence of a successful run.
type NPUOperatorParameters struct {
	SchemaVersion  int                 `json:"schema_version"`
	JobID          string              `json:"job_id"`
	ChipGeneration string              `json:"chip_generation"`
	Operators      []NPUOperatorConfig `json:"operators"`
}

type NPUOperatorConfig struct {
	Name   string                     `json:"name"`
	Config map[string]json.RawMessage `json:"config"`
}

func (p *NPUOperatorParameters) Validate(jobID string, selected CaseNames) error {
	if p == nil || p.SchemaVersion != 1 || p.JobID != jobID || len(p.Operators) == 0 {
		return fmt.Errorf("operator parameter receipt does not belong to this job")
	}
	if p.ChipGeneration != "A2" && p.ChipGeneration != "A3" && p.ChipGeneration != "A5" {
		return fmt.Errorf("invalid parameter receipt generation")
	}
	names := CaseNames{}
	for _, op := range p.Operators {
		if op.Name == "gpt3" || op.Name == "llama2" || len(op.Config) == 0 {
			return fmt.Errorf("invalid operator parameter receipt")
		}
		var count int64
		if json.Unmarshal(op.Config["run_count"], &count) != nil || count <= 0 {
			return fmt.Errorf("operator receipt has no valid run_count")
		}
		for _, value := range op.Config {
			if !json.Valid(value) {
				return fmt.Errorf("invalid operator JSON parameter")
			}
		}
		names = append(names, op.Name)
	}
	if err := ValidateCases(names); err != nil {
		return err
	}
	if len(selected) > 0 && !SameCases(names, selected) {
		return fmt.Errorf("operator parameter receipt differs from this job's selection")
	}
	return nil
}

func CopyNPUParameters(p *NPUOperatorParameters) *NPUOperatorParameters {
	if p == nil {
		return nil
	}
	copy := *p
	copy.Operators = make([]NPUOperatorConfig, len(p.Operators))
	for i, op := range p.Operators {
		copy.Operators[i] = NPUOperatorConfig{Name: op.Name, Config: map[string]json.RawMessage{}}
		for key, value := range op.Config {
			copy.Operators[i].Config[key] = append(json.RawMessage(nil), value...)
		}
	}
	return &copy
}
