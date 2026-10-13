package workloadapi

import (
	"encoding/json"
	"testing"
)

func parametersFixture() *NPUOperatorParameters {
	return &NPUOperatorParameters{SchemaVersion: 1, JobID: "aabb", ChipGeneration: "A2", Operators: []NPUOperatorConfig{{Name: "matmul", Config: map[string]json.RawMessage{
		"run_count": json.RawMessage(`13`), "dtype": json.RawMessage(`["float16"]`), "shape": json.RawMessage(`[[[4,8],[8,16]]]`), "pattern": json.RawMessage(`["gauss_random"]`),
	}}}}
}

func TestOperatorParametersIdentityAndCopy(t *testing.T) {
	proof := parametersFixture()
	if err := proof.Validate("aabb", CaseNames{"matmul"}); err != nil {
		t.Fatal(err)
	}
	copy := CopyNPUParameters(proof)
	copy.Operators[0].Config["run_count"][0] = '9'
	copy.Operators[0].Name = "conv2d"
	if string(proof.Operators[0].Config["run_count"]) != "13" || proof.Operators[0].Name != "matmul" {
		t.Fatal("snapshot copy aliases mutable data")
	}
	for _, mutate := range []func(*NPUOperatorParameters){
		func(p *NPUOperatorParameters) { p.JobID = "different" },
		func(p *NPUOperatorParameters) { p.ChipGeneration = "unknown" },
		func(p *NPUOperatorParameters) { p.Operators[0].Name = "conv2d" },
		func(p *NPUOperatorParameters) { p.Operators = append(p.Operators, p.Operators[0]) },
		func(p *NPUOperatorParameters) { p.Operators[0].Config["run_count"] = json.RawMessage(`0`) },
		func(p *NPUOperatorParameters) { p.Operators[0].Config["run_count"] = json.RawMessage(`"13"`) },
		func(p *NPUOperatorParameters) { p.Operators[0].Config["shape"] = json.RawMessage(`broken-json`) },
	} {
		p := parametersFixture()
		mutate(p)
		if p.Validate("aabb", CaseNames{"matmul"}) == nil {
			t.Fatalf("invalid actual receipt accepted: %+v", p)
		}
	}
}
