package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

func extractJobParametersArgs(args []string) ([]string, *workloadapi.JobParameters, error) {
	p := &workloadapi.JobParameters{}
	remaining := []string{}
	changed := false
	whole := false
	ensureHPL := func() {
		if p.HPL == nil {
			p.HPL = &workloadapi.HPLParameters{}
		}
	}
	ensureHPCG := func() {
		if p.HPCG == nil {
			p.HPCG = &workloadapi.HPCGParameters{}
		}
	}
	ensureNPU := func() {
		if p.NPUBurn == nil {
			p.NPUBurn = &workloadapi.NPUBurnParameters{}
		}
	}
	ensureModel := func() {
		ensureNPU()
		if p.NPUBurn.Model == nil {
			p.NPUBurn.Model = &workloadapi.ModelTrainingParameters{}
		}
	}
	integers := map[string]func(int){
		"hpl-n": func(v int) { ensureHPL(); p.HPL.N = &v }, "hpl-nb": func(v int) { ensureHPL(); p.HPL.NB = &v }, "hpl-p": func(v int) { ensureHPL(); p.HPL.P = &v }, "hpl-q": func(v int) { ensureHPL(); p.HPL.Q = &v },
		"hpl-mpi-processes": func(v int) { ensureHPL(); p.HPL.MPIProcesses = &v }, "hpl-threads-per-process": func(v int) { ensureHPL(); p.HPL.ThreadsPerProcess = &v },
		"hpcg-nx": func(v int) { ensureHPCG(); p.HPCG.NX = &v }, "hpcg-ny": func(v int) { ensureHPCG(); p.HPCG.NY = &v }, "hpcg-nz": func(v int) { ensureHPCG(); p.HPCG.NZ = &v },
		"hpcg-runtime-seconds": func(v int) { ensureHPCG(); p.HPCG.RuntimeSeconds = &v }, "hpcg-mpi-processes": func(v int) { ensureHPCG(); p.HPCG.MPIProcesses = &v }, "hpcg-threads-per-process": func(v int) { ensureHPCG(); p.HPCG.ThreadsPerProcess = &v },
		"stream-threads": func(v int) {
			if p.Stream == nil {
				p.Stream = &workloadapi.StreamParameters{}
			}
			p.Stream.Threads = &v
		},
		"model-run-count": func(v int) { ensureModel(); p.NPUBurn.Model.RunCount = &v }, "model-warmup": func(v int) { ensureModel(); p.NPUBurn.Model.NumWarmup = &v },
		"model-batch-size": func(v int) { ensureModel(); p.NPUBurn.Model.BatchSize = &v }, "model-seq-len": func(v int) { ensureModel(); p.NPUBurn.Model.SeqLen = &v },
		"model-hidden-size": func(v int) { ensureModel(); p.NPUBurn.Model.HiddenSize = &v }, "model-layers": func(v int) { ensureModel(); p.NPUBurn.Model.NumHiddenLayers = &v },
		"model-attention-heads": func(v int) { ensureModel(); p.NPUBurn.Model.NumAttentionHeads = &v }, "model-intermediate-size": func(v int) { ensureModel(); p.NPUBurn.Model.IntermediateSize = &v },
		"model-vocab-size": func(v int) { ensureModel(); p.NPUBurn.Model.VocabSize = &v }, "model-sample-pool": func(v int) { ensureModel(); p.NPUBurn.Model.SamplePool = &v },
		"model-random-seed": func(v int) { ensureModel(); p.NPUBurn.Model.RandomSeed = &v }, "model-num-classes": func(v int) { ensureModel(); p.NPUBurn.Model.NumClasses = &v },
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		argument := args[i]
		flag, value, inline := strings.Cut(strings.TrimPrefix(argument, "--"), "=")
		setter, isInt := integers[flag]
		recognized := strings.HasPrefix(argument, "--") && (isInt || flag == "job-parameters" || flag == "job-parameters-file" || flag == "npu-operator-parameters" || flag == "model-parameters" || flag == "model-learning-rate" || strings.HasPrefix(flag, "sdc-") && (flag == "sdc-loss-tolerance" || flag == "sdc-grad-tolerance" || flag == "sdc-checksum-tolerance"))
		if !recognized {
			remaining = append(remaining, argument)
			continue
		}
		if seen[flag] {
			return nil, nil, fmt.Errorf("duplicate --%s", flag)
		}
		seen[flag] = true
		if !inline {
			i++
			if i >= len(args) {
				return nil, nil, fmt.Errorf("--%s requires a value", flag)
			}
			value = args[i]
		}
		if value == "" {
			return nil, nil, fmt.Errorf("--%s requires a nonempty value", flag)
		}
		if flag == "job-parameters" || flag == "job-parameters-file" {
			if changed || whole {
				return nil, nil, fmt.Errorf("choose one JSON parameter input or per-field flags, not both")
			}
			data := []byte(value)
			if flag == "job-parameters-file" {
				reader := io.Reader(os.Stdin)
				var file *os.File
				if value != "-" {
					var err error
					file, err = os.Open(value)
					if err != nil {
						return nil, nil, err
					}
					reader = file
				}
				var err error
				data, err = io.ReadAll(io.LimitReader(reader, (24<<10)+1))
				if file != nil {
					_ = file.Close()
				}
				if err != nil {
					return nil, nil, err
				}
			}
			var err error
			p, err = workloadapi.DecodeJobParameters(data)
			if err != nil {
				return nil, nil, err
			}
			whole = true
			continue
		}
		if whole {
			return nil, nil, fmt.Errorf("choose one JSON parameter input or per-field flags, not both")
		}
		changed = true
		if strings.HasPrefix(flag, "model-") && flag != "model-parameters" && seen["model-parameters"] {
			return nil, nil, fmt.Errorf("model JSON and model per-field flags cannot be combined")
		}
		if isInt {
			v, err := strconv.Atoi(value)
			if err != nil {
				return nil, nil, fmt.Errorf("--%s requires an integer", flag)
			}
			setter(v)
			continue
		}
		switch flag {
		case "npu-operator-parameters", "model-parameters":
			key := "operators"
			if flag == "model-parameters" {
				key = "model"
			}
			data, marshalErr := json.Marshal(map[string]any{"npu_burn": map[string]json.RawMessage{key: json.RawMessage(value)}})
			if marshalErr != nil {
				return nil, nil, marshalErr
			}
			part, err := workloadapi.DecodeJobParameters(data)
			if err != nil {
				return nil, nil, err
			}
			ensureNPU()
			if key == "operators" {
				p.NPUBurn.Operators = part.NPUBurn.Operators
			} else {
				if p.NPUBurn.Model != nil {
					return nil, nil, fmt.Errorf("model JSON and model per-field flags cannot be combined")
				}
				p.NPUBurn.Model = part.NPUBurn.Model
			}
		default:
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, nil, fmt.Errorf("--%s requires a number", flag)
			}
			if flag == "model-learning-rate" {
				ensureModel()
				p.NPUBurn.Model.LearningRate = &v
				continue
			}
			ensureNPU()
			if p.NPUBurn.SDCTolerances == nil {
				p.NPUBurn.SDCTolerances = &workloadapi.SDCTolerances{}
			}
			switch flag {
			case "sdc-loss-tolerance":
				p.NPUBurn.SDCTolerances.Loss = &v
			case "sdc-grad-tolerance":
				p.NPUBurn.SDCTolerances.Grad = &v
			case "sdc-checksum-tolerance":
				p.NPUBurn.SDCTolerances.Checksum = &v
			}
		}
	}
	if !changed && !whole {
		return remaining, nil, nil
	}
	return remaining, p, nil
}
