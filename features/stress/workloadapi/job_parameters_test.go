package workloadapi

import (
	"encoding/json"
	"math"
	"testing"
)

func pointer[T any](v T) *T { return &v }

func TestJobParameterStrictAndSelectedContract(t *testing.T) {
	for _, raw := range []string{`null`, `{"command":"id"}`, `{"stream":{"threads":1,"path":"/x"}}`, `{} {}`, `{"npu_burn":{"operators":{"matmul":{"exec_count":1}}}}`} {
		if _, err := DecodeJobParameters([]byte(raw)); err == nil {
			t.Fatalf("accepted unsafe/unknown input %s", raw)
		}
	}
	p, err := DecodeJobParameters([]byte(`{"stream":{"threads":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Validate([]string{"stream"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err = p.Validate([]string{"hpl"}, nil, nil); err == nil {
		t.Fatal("unselected parameter admitted")
	}
	*p.Stream.Threads = 0
	if p.Validate([]string{"stream"}, nil, nil) == nil {
		t.Fatal("explicit zero inherited")
	}
	for _, raw := range []string{`{"npu_burn":{"operators":{"matmul":{"shape":[[[2,3],[3,"4"]]]}}}}`, `{"npu_burn":{"operators":{"matmul":{"dimensions":{"path":1}}}}}`, `{"npu_burn":{"operators":{}}}`} {
		p, err := DecodeJobParameters([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if p.Validate([]string{"npu_burn"}, nil, CaseNames{"matmul"}) == nil {
			t.Fatal("invalid dimensions accepted", raw)
		}
	}
	p = &JobParameters{NPUBurn: &NPUBurnParameters{SDCTolerances: &SDCTolerances{Loss: pointer(math.NaN())}}}
	if p.Validate([]string{"npu_burn"}, &ModelRequest{}, nil) == nil {
		t.Fatal("nonfinite accepted")
	}
}

func TestFrozenParametersCompleteHashAndCopy(t *testing.T) {
	f := &FrozenParameters{SchemaVersion: 1, Benchmark: "stream", Effective: &JobParameters{Stream: &StreamParameters{Threads: pointer(2)}}}
	if err := f.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := f.Validate("stream"); err != nil {
		t.Fatal(err)
	}
	c := CopyFrozenParameters(f)
	*c.Effective.Stream.Threads = 3
	if *f.Effective.Stream.Threads != 2 || c.Validate("stream") == nil {
		t.Fatal("copy alias/hash mismatch undetected")
	}
	c.Effective.Stream.Threads = nil
	_ = c.Seal()
	if c.Validate("stream") == nil {
		t.Fatal("incomplete snapshot accepted")
	}
	r := &ExecutionParameters{SchemaVersion: 1, JobID: "job1", Benchmark: "stream", Frozen: CopyFrozenParameters(f)}
	if r.Validate("job1", "stream", f) != nil {
		t.Fatal("valid receipt rejected")
	}
	if r.Validate("job2", "stream", f) == nil {
		t.Fatal("old job receipt accepted")
	}
	*r.Frozen.Effective.Stream.Threads = 3
	_ = r.Frozen.Seal()
	if r.Validate("job1", "stream", f) == nil {
		t.Fatal("changed execution accepted")
	}
}

func TestHPCGRejectsSilentlyExpandedUserGrid(t *testing.T) {
	p := &JobParameters{HPCG: &HPCGParameters{NX: pointer(8)}}
	if p.Validate([]string{"hpcg"}, nil, nil) == nil {
		t.Fatal("requested grid silently expanded")
	}
}

func TestNPUActualLoadedConfigurationMustMatchFrozen(t *testing.T) {
	f := &FrozenParameters{SchemaVersion: 1, Benchmark: "npu_burn", NPUGeneration: "A2", NPUConfig: json.RawMessage(`{"cases":[{"matmul":{"run_count":1}}]}`), Effective: &JobParameters{NPUBurn: &NPUBurnParameters{Operators: map[string]OperatorParameters{"matmul": {RunCount: pointer(1)}}}}}
	_ = f.Seal()
	actual := &NPUOperatorParameters{Operators: []NPUOperatorConfig{{Name: "matmul", Config: map[string]json.RawMessage{"run_count": json.RawMessage(`1`)}}}}
	if err := ValidateNPUExecutionParameters(f, nil, actual, nil); err != nil {
		t.Fatal(err)
	}
	actual.Operators[0].Config["run_count"] = json.RawMessage(`2`)
	if ValidateNPUExecutionParameters(f, nil, actual, nil) == nil {
		t.Fatal("actual differs but passed")
	}
	actual.Operators = nil
	if ValidateNPUExecutionParameters(f, nil, actual, nil) == nil {
		t.Fatal("missing coverage passed")
	}
}
