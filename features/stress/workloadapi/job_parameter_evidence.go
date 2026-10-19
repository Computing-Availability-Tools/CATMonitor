package workloadapi

import (
	"encoding/json"
	"fmt"
	"reflect"
)

func equalJSON(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	var x, y any
	if json.Unmarshal(left, &x) != nil || json.Unmarshal(right, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func ValidateNPUExecutionParameters(f *FrozenParameters, model *ModelRequest, operators *NPUOperatorParameters, result *ModelResult) error {
	if err := f.Validate("npu_burn"); err != nil {
		return err
	}
	var config struct {
		Cases []map[string]map[string]json.RawMessage `json:"cases"`
	}
	if err := json.Unmarshal(f.NPUConfig, &config); err != nil {
		return err
	}
	entries := map[string]map[string]json.RawMessage{}
	for _, group := range config.Cases {
		for name, data := range group {
			entries[name] = data
		}
	}
	if model != nil {
		if result == nil || result.EffectiveConfig == nil || result.SDCTolerances == nil || !equalJSON(entries[model.Name], result.EffectiveConfig) || !equalJSON(f.Effective.NPUBurn.SDCTolerances, result.SDCTolerances) {
			return fmt.Errorf("actual model config/tolerances differ from the frozen admission")
		}
		return nil
	}
	if operators == nil || len(operators.Operators) == 0 {
		return fmt.Errorf("runtime did not record actually loaded operator parameters")
	}
	if len(operators.Operators) != len(f.Effective.NPUBurn.Operators) {
		return fmt.Errorf("actual operator parameter coverage differs from frozen admission")
	}
	seen := map[string]bool{}
	for _, op := range operators.Operators {
		if _, ok := f.Effective.NPUBurn.Operators[op.Name]; !ok || seen[op.Name] {
			return fmt.Errorf("unexpected or duplicate actual operator %s", op.Name)
		}
		seen[op.Name] = true
		if !equalJSON(entries[op.Name], op.Config) {
			return fmt.Errorf("operator %s actually loaded parameters differ from frozen admission", op.Name)
		}
	}
	return nil
}
