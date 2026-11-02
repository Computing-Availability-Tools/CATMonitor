package workloadapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

// JobParameters contains data only. Executables, paths, environment, container
// bindings, device authorization and maximum timeouts remain administrator-owned.
// A nil field means inherit; an explicitly supplied zero/empty value is validated.
type JobParameters struct {
	HPL     *HPLParameters     `json:"hpl,omitempty"`
	HPCG    *HPCGParameters    `json:"hpcg,omitempty"`
	Stream  *StreamParameters  `json:"stream,omitempty"`
	NPUBurn *NPUBurnParameters `json:"npu_burn,omitempty"`
}

type HPLParameters struct {
	N                 *int `json:"n,omitempty"`
	NB                *int `json:"nb,omitempty"`
	P                 *int `json:"p,omitempty"`
	Q                 *int `json:"q,omitempty"`
	MPIProcesses      *int `json:"mpi_processes,omitempty"`
	ThreadsPerProcess *int `json:"threads_per_process,omitempty"`
}

type HPCGParameters struct {
	NX                *int `json:"nx,omitempty"`
	NY                *int `json:"ny,omitempty"`
	NZ                *int `json:"nz,omitempty"`
	RuntimeSeconds    *int `json:"runtime_seconds,omitempty"`
	MPIProcesses      *int `json:"mpi_processes,omitempty"`
	ThreadsPerProcess *int `json:"threads_per_process,omitempty"`
}

type StreamParameters struct {
	Threads *int `json:"threads,omitempty"`
}

type NPUBurnParameters struct {
	Operators     map[string]OperatorParameters `json:"operators,omitempty"`
	Model         *ModelTrainingParameters      `json:"model,omitempty"`
	SDCTolerances *SDCTolerances                `json:"sdc_tolerances,omitempty"`
}

type OperatorParameters struct {
	Dtype      []string                   `json:"dtype,omitempty"`
	Shape      json.RawMessage            `json:"shape,omitempty"`
	Dimensions map[string]json.RawMessage `json:"dimensions,omitempty"`
	Pattern    []string                   `json:"pattern,omitempty"`
	RunCount   *int                       `json:"run_count,omitempty"`
}

type ModelTrainingParameters struct {
	RunCount          *int     `json:"run_count,omitempty"`
	NumWarmup         *int     `json:"num_warmup,omitempty"`
	BatchSize         *int     `json:"batch_size,omitempty"`
	SeqLen            *int     `json:"seq_len,omitempty"`
	RandomSeed        *int     `json:"random_seed,omitempty"`
	SamplePool        *int     `json:"sample_pool,omitempty"`
	NumClasses        *int     `json:"num_classes,omitempty"`
	VocabSize         *int     `json:"vocab_size,omitempty"`
	HiddenSize        *int     `json:"hidden_size,omitempty"`
	NumHiddenLayers   *int     `json:"num_hidden_layers,omitempty"`
	NumAttentionHeads *int     `json:"num_attention_heads,omitempty"`
	IntermediateSize  *int     `json:"intermediate_size,omitempty"`
	LearningRate      *float64 `json:"learning_rate,omitempty"`
	Dtype             *string  `json:"dtype,omitempty"`
}

// These are part of reference identity, not mutable comparison relaxations.
// Changing either requires an independently generated matching reference.
type SDCTolerances struct {
	Loss     *float64 `json:"loss,omitempty"`
	Grad     *float64 `json:"grad,omitempty"`
	Checksum *float64 `json:"checksum,omitempty"`
}

// FrozenParameters is produced by the workload Describe operation, not accepted
// from frontend API requests. It preserves the complete effective HPL input or
// NPU configuration, including defaults, against later shared-config changes.
type FrozenParameters struct {
	SchemaVersion int             `json:"schema_version"`
	Benchmark     string          `json:"benchmark"`
	Effective     *JobParameters  `json:"effective"`
	HPLInput      string          `json:"hpl_input,omitempty"`
	NPUGeneration string          `json:"npu_generation,omitempty"`
	NPUConfig     json.RawMessage `json:"npu_config,omitempty"`
	SHA256        string          `json:"sha256,omitempty"`
}

// ExecutionParameters is emitted only after an execution process starts.
// NPU actual loaded cases/model config are additionally checked by the plugin.
type ExecutionParameters struct {
	SchemaVersion int               `json:"schema_version"`
	JobID         string            `json:"job_id"`
	Benchmark     string            `json:"benchmark"`
	Frozen        *FrozenParameters `json:"frozen"`
}

type DescribeRequest struct {
	Parameters *JobParameters `json:"parameters,omitempty"`
}

func DecodeJobParameters(data []byte) (*JobParameters, error) {
	if len(data) == 0 || len(data) > 24<<10 {
		return nil, fmt.Errorf("job parameters must contain 1..24576 bytes")
	}
	var result JobParameters
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, fmt.Errorf("job parameters must be a JSON object")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&result); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("job parameters must contain one JSON object")
	}
	return &result, nil
}

func (p *JobParameters) For(name string) *JobParameters {
	if p == nil {
		return nil
	}
	result := &JobParameters{}
	switch name {
	case "hpl":
		result.HPL = p.HPL
	case "hpcg":
		result.HPCG = p.HPCG
	case "stream":
		result.Stream = p.Stream
	case "npu_burn":
		result.NPUBurn = p.NPUBurn
	}
	if result.HPL == nil && result.HPCG == nil && result.Stream == nil && result.NPUBurn == nil {
		return nil
	}
	return CopyJobParameters(result)
}

func (p *JobParameters) Validate(selected []string, model *ModelRequest, cases CaseNames) error {
	if p == nil {
		return nil
	}
	data, err := json.Marshal(p)
	if err != nil || len(data) > 24<<10 {
		return fmt.Errorf("job parameters exceed 24 KiB")
	}
	allowed := map[string]bool{}
	for _, name := range selected {
		allowed[name] = true
	}
	for _, name := range []string{"hpl", "hpcg", "stream", "npu_burn"} {
		if p.For(name) != nil && !allowed[name] {
			return fmt.Errorf("parameters for unselected benchmark %s", name)
		}
	}
	positive := func(name string, value *int, maximum int) error {
		if value != nil && (*value < 1 || *value > maximum) {
			return fmt.Errorf("%s must be 1..%d", name, maximum)
		}
		return nil
	}
	if h := p.HPL; h != nil {
		for name, value := range map[string]*int{"hpl.n": h.N, "hpl.nb": h.NB, "hpl.p": h.P, "hpl.q": h.Q, "hpl.mpi_processes": h.MPIProcesses, "hpl.threads_per_process": h.ThreadsPerProcess} {
			if err := positive(name, value, 2147483647); err != nil {
				return err
			}
		}
	}
	if h := p.HPCG; h != nil {
		for name, value := range map[string]*int{"hpcg.nx": h.NX, "hpcg.ny": h.NY, "hpcg.nz": h.NZ, "hpcg.runtime_seconds": h.RuntimeSeconds, "hpcg.mpi_processes": h.MPIProcesses, "hpcg.threads_per_process": h.ThreadsPerProcess} {
			if err := positive(name, value, 1048576); err != nil {
				return err
			}
		}
		if h.RuntimeSeconds != nil && *h.RuntimeSeconds > 86400 {
			return fmt.Errorf("HPCG runtime exceeds 24 hours")
		}
		for _, dimension := range []*int{h.NX, h.NY, h.NZ} {
			if dimension != nil && *dimension < 16 {
				return fmt.Errorf("HPCG dimensions must be at least 16; upstream silently expands smaller grids")
			}
		}
	}
	if s := p.Stream; s != nil {
		if err := positive("stream.threads", s.Threads, 65536); err != nil {
			return err
		}
	}
	if n := p.NPUBurn; n != nil {
		if n.Operators != nil && (len(n.Operators) == 0 || len(n.Operators) > 32) {
			return fmt.Errorf("operators must contain 1..32 selected cases")
		}
		if len(n.Operators) > 0 && model != nil {
			return fmt.Errorf("operator and model parameters cannot be combined")
		}
		for name, op := range n.Operators {
			if err := ValidateCases(CaseNames{name}); err != nil {
				return err
			}
			if cases != nil && !containsCase(cases, name) {
				return fmt.Errorf("parameters for unselected operator %s", name)
			}
			if err := positive("operator run_count", op.RunCount, 1000000); err != nil {
				return err
			}
			for _, items := range [][]string{op.Dtype, op.Pattern} {
				if items != nil && (len(items) == 0 || len(items) > 32) {
					return fmt.Errorf("dtype/pattern selection must not be empty or exceed 32 items")
				}
				seen := map[string]bool{}
				for _, item := range items {
					if item == "" || len(item) > 64 || seen[item] {
						return fmt.Errorf("invalid or duplicate dtype/pattern")
					}
					seen[item] = true
				}
			}
			if op.Shape != nil {
				if err := validateDimensions(op.Shape); err != nil {
					return err
				}
			}
			for key, value := range op.Dimensions {
				if !dimensionKeys[key] {
					return fmt.Errorf("unsupported tensor dimension %s", key)
				}
				if key == "shape" && op.Shape != nil {
					return fmt.Errorf("shape provided twice")
				}
				if err := validateDimensions(value); err != nil {
					return err
				}
			}
		}
		if (n.Model != nil || n.SDCTolerances != nil) && model == nil {
			return fmt.Errorf("training/SDC parameters require a model job")
		}
		if m := n.Model; m != nil {
			for name, value := range map[string]*int{"run_count": m.RunCount, "batch_size": m.BatchSize, "seq_len": m.SeqLen, "sample_pool": m.SamplePool, "num_classes": m.NumClasses, "vocab_size": m.VocabSize, "hidden_size": m.HiddenSize, "num_hidden_layers": m.NumHiddenLayers, "num_attention_heads": m.NumAttentionHeads, "intermediate_size": m.IntermediateSize} {
				if err := positive("model."+name, value, 1000000); err != nil {
					return err
				}
			}
			for name, value := range map[string]*int{"num_warmup": m.NumWarmup, "random_seed": m.RandomSeed} {
				if value != nil && (*value < 0 || *value > 2147483647) {
					return fmt.Errorf("model.%s must be a non-negative integer", name)
				}
			}
			if m.Dtype != nil && *m.Dtype != "bfloat16" {
				return fmt.Errorf("models require bfloat16; no dtype substitution")
			}
			if m.LearningRate != nil && (!finite(*m.LearningRate) || *m.LearningRate <= 0 || *m.LearningRate > 1) {
				return fmt.Errorf("learning_rate must be finite and in (0,1]")
			}
		}
		if t := n.SDCTolerances; t != nil {
			for _, value := range []*float64{t.Loss, t.Grad, t.Checksum} {
				if value != nil && (!finite(*value) || *value < 0 || *value > 1) {
					return fmt.Errorf("SDC tolerance must be finite and in [0,1]")
				}
			}
		}
	}
	return nil
}

var dimensionKeys = map[string]bool{"shape": true, "MKN": true, "B": true, "N": true, "K": true, "k": true, "C": true, "H": true, "W": true, "input_data": true, "weight_data": true, "BSHN": true, "BNSH": true, "seq_len": true, "in.out": true, "batch_size": true, "head_num": true, "head_dim": true, "ESKN": true, "ND": true}

func containsCase(cases CaseNames, name string) bool {
	for _, v := range cases {
		if v == name {
			return true
		}
	}
	return false
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func validateDimensions(data json.RawMessage) error {
	var value any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return err
	}
	count := 0
	var visit func(any, int) error
	visit = func(v any, depth int) error {
		if depth > 5 {
			return fmt.Errorf("tensor dimensions are too deeply nested")
		}
		switch item := v.(type) {
		case json.Number:
			n, e := item.Int64()
			count++
			if e != nil || n <= 0 || n > 1048576 || count > 1024 {
				return fmt.Errorf("tensor dimensions must contain bounded positive integers")
			}
		case []any:
			if len(item) == 0 || len(item) > 128 {
				return fmt.Errorf("tensor dimension list must contain 1..128 values")
			}
			for _, child := range item {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("tensor dimensions cannot contain strings, booleans, objects or null")
		}
		return nil
	}
	return visit(value, 0)
}

func CopyJobParameters(p *JobParameters) *JobParameters {
	if p == nil {
		return nil
	}
	data, err := json.Marshal(p)
	if err != nil {
		return p
	} // Preserve invalid input for the caller's validator; never silently erase it.
	var result JobParameters
	_ = json.Unmarshal(data, &result)
	return &result
}
func CopyFrozenParameters(p *FrozenParameters) *FrozenParameters {
	if p == nil {
		return nil
	}
	data, _ := json.Marshal(p)
	var result FrozenParameters
	_ = json.Unmarshal(data, &result)
	return &result
}
func CopyExecutionParameters(p *ExecutionParameters) *ExecutionParameters {
	if p == nil {
		return nil
	}
	result := *p
	result.Frozen = CopyFrozenParameters(p.Frozen)
	return &result
}

func (p *FrozenParameters) Seal() error {
	p.SHA256 = ""
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	p.SHA256 = hex.EncodeToString(sum[:])
	return nil
}
func (p *FrozenParameters) Validate(name string) error {
	if p == nil || p.SchemaVersion != 1 || p.Benchmark != name || p.Effective == nil || p.Effective.For(name) == nil {
		return fmt.Errorf("missing/invalid frozen workload parameters")
	}
	data, err := json.Marshal(p)
	if err != nil || len(data) > 48<<10 {
		return fmt.Errorf("frozen parameters exceed 48 KiB")
	}
	copy := CopyFrozenParameters(p)
	hash := copy.SHA256
	_ = copy.Seal()
	if hash == "" || hash != copy.SHA256 {
		return fmt.Errorf("frozen workload parameter hash mismatch")
	}
	if name != "hpl" && p.HPLInput != "" {
		return fmt.Errorf("unexpected HPL input")
	}
	if name != "npu_burn" && (p.NPUConfig != nil || p.NPUGeneration != "") {
		return fmt.Errorf("unexpected NPU configuration")
	}
	var model *ModelRequest
	if p.Effective.NPUBurn != nil && p.Effective.NPUBurn.Model != nil {
		model = &ModelRequest{}
	}
	if err := p.Effective.Validate([]string{name}, model, nil); err != nil {
		return err
	}
	required := []*int{}
	switch name {
	case "stream":
		required = []*int{p.Effective.Stream.Threads}
	case "hpl":
		h := p.Effective.HPL
		required = []*int{h.N, h.NB, h.P, h.Q, h.MPIProcesses, h.ThreadsPerProcess}
		if p.HPLInput == "" || len(p.HPLInput) > 16<<10 {
			return fmt.Errorf("missing or oversized frozen HPL input")
		}
	case "hpcg":
		h := p.Effective.HPCG
		required = []*int{h.NX, h.NY, h.NZ, h.RuntimeSeconds, h.MPIProcesses, h.ThreadsPerProcess}
	case "npu_burn":
		if (p.NPUGeneration != "A2" && p.NPUGeneration != "A3" && p.NPUGeneration != "A5") || !json.Valid(p.NPUConfig) || bytes.Equal(bytes.TrimSpace(p.NPUConfig), []byte("null")) {
			return fmt.Errorf("missing frozen NPU configuration/generation")
		}
		if n := p.Effective.NPUBurn; n.Model == nil && len(n.Operators) == 0 || n.Model != nil && (n.SDCTolerances == nil || n.SDCTolerances.Loss == nil || n.SDCTolerances.Grad == nil || n.SDCTolerances.Checksum == nil) {
			return fmt.Errorf("incomplete frozen NPU parameters")
		}
	default:
		return fmt.Errorf("unsupported frozen benchmark")
	}
	for _, v := range required {
		if v == nil || *v <= 0 {
			return fmt.Errorf("incomplete frozen effective parameters")
		}
	}
	return nil
}
func (p *ExecutionParameters) Validate(jobID, name string, frozen *FrozenParameters) error {
	if p == nil || p.SchemaVersion != 1 || p.JobID != jobID || p.Benchmark != name {
		return fmt.Errorf("invalid execution parameter receipt")
	}
	if err := p.Frozen.Validate(name); err != nil {
		return err
	}
	if frozen == nil || p.Frozen.SHA256 != frozen.SHA256 {
		return fmt.Errorf("execution parameters differ from admitted frozen parameters")
	}
	return nil
}
