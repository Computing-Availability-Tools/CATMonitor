//go:build linux

package workloadplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

func intPointer(v int) *int { return &v }
func inherited(value *int, fallback int) int {
	if value != nil {
		return *value
	}
	return fallback
}

// PrepareJobParameters is scoped to this disposable workload-exec process. It
// maps typed numeric data to fixed plugin-owned environment keys and temporary
// inputs; it never changes the container config or a shared deployment file.
func PrepareJobParameters(ctx context.Context, name string, requested *workloadapi.JobParameters, frozen *workloadapi.FrozenParameters) (*workloadapi.FrozenParameters, func(), error) {
	cleanup := func() {}
	if frozen == nil {
		model := (*workloadapi.ModelRequest)(nil)
		if env("CATMONITOR_NPU_MODEL") != "" {
			model = environmentModel()
		}
		if err := requested.Validate([]string{name}, model, nil); err != nil {
			return nil, cleanup, err
		}
		var err error
		frozen, err = freezeParameters(ctx, name, requested)
		if err != nil {
			return nil, cleanup, err
		}
	} else {
		frozen = workloadapi.CopyFrozenParameters(frozen)
		if err := frozen.Validate(name); err != nil {
			return nil, cleanup, err
		}
	}
	dir, err := os.MkdirTemp("", "catmonitor-job-parameters-")
	if err != nil {
		return nil, cleanup, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	if err := applyFrozenParameters(frozen, dir); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return frozen, cleanup, nil
}

func freezeParameters(ctx context.Context, name string, requested *workloadapi.JobParameters) (*workloadapi.FrozenParameters, error) {
	p := workloadapi.CopyJobParameters(requested)
	if p == nil {
		p = &workloadapi.JobParameters{}
	}
	f := &workloadapi.FrozenParameters{SchemaVersion: 1, Benchmark: name, Effective: &workloadapi.JobParameters{}}
	switch name {
	case "stream":
		threads, err := nonnegative("STREAM_THREADS")
		if err != nil {
			return nil, err
		}
		if p.Stream != nil && p.Stream.Threads != nil {
			threads = *p.Stream.Threads
		}
		if threads == 0 {
			if value := env("OMP_NUM_THREADS"); value != "" {
				threads, err = strconv.Atoi(value)
				if err != nil || threads <= 0 {
					return nil, fmt.Errorf("invalid inherited OMP thread budget")
				}
			}
			if threads == 0 {
				cpus, _, e := effectiveResources()
				if e != nil {
					return nil, e
				}
				threads = int(cpus)
				if threads < 1 {
					threads = 1
				}
			}
		}
		f.Effective.Stream = &workloadapi.StreamParameters{Threads: intPointer(threads)}
	case "hpl":
		data, err := hplInput(filepath.Join(env("HPL_WORKDIR"), "HPL.dat"))
		if err != nil {
			return nil, err
		}
		if len(data) > 16<<10 {
			return nil, fmt.Errorf("HPL input exceeds 16 KiB")
		}
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		if len(lines) < 12 {
			return nil, fmt.Errorf("HPL input is incomplete")
		}
		values := make([]int, 4)
		for i, line := range []int{5, 7, 10, 11} {
			n, e := hplNumbers(lines[line], 1)
			if e != nil {
				return nil, e
			}
			values[i] = int(n[0])
		}
		ranks, err := positive("HPL_MPI_PROCESSES")
		if err != nil {
			return nil, err
		}
		threads, err := positive("HPL_THREADS_PER_PROCESS")
		if err != nil {
			return nil, err
		}
		h := p.HPL
		if h == nil {
			h = &workloadapi.HPLParameters{}
		}
		values[0] = inherited(h.N, values[0])
		values[1] = inherited(h.NB, values[1])
		values[2] = inherited(h.P, values[2])
		values[3] = inherited(h.Q, values[3])
		if h.N != nil || h.NB != nil || h.P != nil || h.Q != nil {
			for i, line := range []int{5, 7, 10, 11} {
				lines[line] = strconv.Itoa(values[i])
			}
			lines[4], lines[6], lines[9] = "1", "1", "1"
			data = []byte(strings.Join(lines, "\n"))
		}
		f.HPLInput = string(data)
		f.Effective.HPL = &workloadapi.HPLParameters{N: intPointer(values[0]), NB: intPointer(values[1]), P: intPointer(values[2]), Q: intPointer(values[3]), MPIProcesses: intPointer(inherited(h.MPIProcesses, ranks)), ThreadsPerProcess: intPointer(inherited(h.ThreadsPerProcess, threads))}
	case "hpcg":
		h := p.HPCG
		if h == nil {
			h = &workloadapi.HPCGParameters{}
		}
		fields := []struct {
			key      string
			override *int
		}{{"HPCG_NX", h.NX}, {"HPCG_NY", h.NY}, {"HPCG_NZ", h.NZ}, {"HPCG_RUNTIME_SECONDS", h.RuntimeSeconds}, {"HPCG_MPI_PROCESSES", h.MPIProcesses}, {"HPCG_THREADS_PER_PROCESS", h.ThreadsPerProcess}}
		values := make([]*int, len(fields))
		for i, field := range fields {
			n, e := positive(field.key)
			if e != nil {
				return nil, e
			}
			values[i] = intPointer(inherited(field.override, n))
		}
		// Preserve historical deployment behavior while recording the actual
		// upstream init.cpp grid. Explicit job values <16 were rejected above.
		largest := 16
		for _, v := range values[:3] {
			if *v > largest {
				largest = *v
			}
		}
		for _, v := range values[:3] {
			if *v < 16 {
				*v = largest
			}
		}
		f.Effective.HPCG = &workloadapi.HPCGParameters{NX: values[0], NY: values[1], NZ: values[2], RuntimeSeconds: values[3], MPIProcesses: values[4], ThreadsPerProcess: values[5]}
	case "npu_burn":
		input := map[string]any{"generation": env("NPU_BURN_CHIP_GENERATION"), "run_case": env("NPU_BURN_RUN_CASE"), "group": env("NPU_BURN_GROUP"), "model": env("CATMONITOR_NPU_MODEL"), "directory": env("NPU_BURN_CONFIG_DIR"), "parameters": p.NPUBurn}
		data, _ := json.Marshal(input)
		cmd := exec.CommandContext(ctx, "python3", "-s", "/usr/local/libexec/catmonitor/job_config.py")
		cmd.Env = append(os.Environ(), "LD_PRELOAD=")
		cmd.Stdin = bytes.NewReader(data)
		output, err := cmd.Output()
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				return nil, fmt.Errorf("NPU job parameters: %s", truncate(string(e.Stderr), 1200))
			}
			return nil, err
		}
		if len(output) > 40<<10 {
			return nil, fmt.Errorf("NPU effective configuration exceeds 40 KiB")
		}
		var result struct {
			Config     json.RawMessage                `json:"config"`
			Generation string                         `json:"generation"`
			Effective  *workloadapi.NPUBurnParameters `json:"effective"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			return nil, err
		}
		if result.Generation != env("NPU_BURN_CHIP_GENERATION") || result.Effective == nil || len(result.Config) == 0 {
			return nil, fmt.Errorf("NPU resolver did not confirm generation and effective parameters")
		}
		f.NPUGeneration, f.NPUConfig, f.Effective.NPUBurn = result.Generation, result.Config, result.Effective
	default:
		return nil, fmt.Errorf("unsupported benchmark parameters")
	}
	if err := f.Seal(); err != nil {
		return nil, err
	}
	if err := f.Validate(name); err != nil {
		return nil, err
	}
	return f, nil
}

func applyFrozenParameters(f *workloadapi.FrozenParameters, dir string) error {
	values := map[string]string{}
	p := f.Effective
	switch f.Benchmark {
	case "stream":
		if p.Stream == nil || p.Stream.Threads == nil {
			return fmt.Errorf("incomplete STREAM snapshot")
		}
		values["STREAM_THREADS"] = strconv.Itoa(*p.Stream.Threads)
	case "hpl":
		h := p.HPL
		if h == nil || h.MPIProcesses == nil || h.ThreadsPerProcess == nil || f.HPLInput == "" {
			return fmt.Errorf("incomplete HPL snapshot")
		}
		if err := os.WriteFile(filepath.Join(dir, "HPL.dat"), []byte(f.HPLInput), 0o600); err != nil {
			return err
		}
		values["HPL_WORKDIR"] = dir
		values["HPL_MPI_PROCESSES"] = strconv.Itoa(*h.MPIProcesses)
		values["HPL_THREADS_PER_PROCESS"] = strconv.Itoa(*h.ThreadsPerProcess)
		// Frozen input already contains the deployment/job override. Never apply
		// a later inherited HPL_N/... override to it a second time.
		for _, key := range []string{"HPL_N", "HPL_NB", "HPL_P", "HPL_Q"} {
			values[key] = ""
		}
	case "hpcg":
		h := p.HPCG
		if h == nil {
			return fmt.Errorf("incomplete HPCG snapshot")
		}
		for key, value := range map[string]*int{"HPCG_NX": h.NX, "HPCG_NY": h.NY, "HPCG_NZ": h.NZ, "HPCG_RUNTIME_SECONDS": h.RuntimeSeconds, "HPCG_MPI_PROCESSES": h.MPIProcesses, "HPCG_THREADS_PER_PROCESS": h.ThreadsPerProcess} {
			if value == nil {
				return fmt.Errorf("incomplete HPCG snapshot")
			}
			values[key] = strconv.Itoa(*value)
		}
		values["HPCG_WORKDIR"] = dir
	case "npu_burn":
		if f.NPUConfig == nil || (f.NPUGeneration != "A2" && f.NPUGeneration != "A3" && f.NPUGeneration != "A5") {
			return fmt.Errorf("incomplete NPU snapshot")
		}
		data, err := json.Marshal(map[string]any{"generation": f.NPUGeneration, "config": f.NPUConfig})
		if err != nil {
			return err
		}
		path := filepath.Join(dir, "npu-frozen-config.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
		values["CATMONITOR_NPU_FROZEN_CONFIG_FILE"] = path
		if t := p.NPUBurn.SDCTolerances; t != nil {
			if t.Loss == nil || t.Grad == nil || t.Checksum == nil {
				return fmt.Errorf("incomplete SDC tolerance snapshot")
			}
			values["CATMONITOR_MODEL_LOSS_TOLERANCE"] = strconv.FormatFloat(*t.Loss, 'g', -1, 64)
			values["CATMONITOR_MODEL_GRAD_TOLERANCE"] = strconv.FormatFloat(*t.Grad, 'g', -1, 64)
			values["CATMONITOR_MODEL_CHECKSUM_TOLERANCE"] = strconv.FormatFloat(*t.Checksum, 'g', -1, 64)
		}
	}
	for key, value := range values {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

func ValidateJobCapacity(name string, f *workloadapi.FrozenParameters) error {
	if err := f.Validate(name); err != nil {
		return err
	}
	if name == "npu_burn" {
		return nil
	}
	cpus, memory, err := effectiveResources()
	if err != nil {
		return err
	}
	switch name {
	case "stream":
		if float64(*f.Effective.Stream.Threads) > cpus {
			return fmt.Errorf("STREAM threads exceed effective online/affinity/quota CPU capacity %.2f", cpus)
		}
	case "hpl":
		h := f.Effective.HPL
		return validateHPLResources([]byte(f.HPLInput), *h.MPIProcesses, *h.ThreadsPerProcess, cpus, memory)
	case "hpcg":
		h := f.Effective.HPCG
		workers := float64(*h.MPIProcesses) * float64(*h.ThreadsPerProcess)
		if workers > cpus {
			return fmt.Errorf("HPCG ranks x threads exceeds effective CPU capacity %.2f", cpus)
		}
		// A conservative lower-bound screen, not a peak-memory guarantee.
		required := float64(*h.NX)*float64(*h.NY)*float64(*h.NZ)*float64(*h.MPIProcesses)*512 + float64(*h.MPIProcesses)*(64<<20)
		if required > float64(memory) || memory <= 0 {
			return fmt.Errorf("HPCG local grid estimated memory %.0f exceeds available budget %d", required, memory)
		}
	}
	return nil
}

func DescribeJob(ctx context.Context, name string, requested *workloadapi.JobParameters) (*workloadapi.ExecutionProfile, error) {
	if name == "npu_burn" && !hasNPUJobParameters() {
		if requested != nil {
			return nil, fmt.Errorf("NPU job parameters require the explicit job-parameters-v1 compatibility patch/profile")
		}
		return Describe(ctx, name)
	}
	f, cleanup, err := PrepareJobParameters(ctx, name, requested, nil)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	p, err := Describe(ctx, name)
	if err != nil {
		return nil, err
	}
	path := "/sys/devices/system/cpu/online"
	if name == "npu_burn" {
		path = "/usr/local/share/catmonitor/job-parameter-base.json"
	}
	check := workloadapi.AssetCheck{Name: "job_parameter_capacity", Path: path, Kind: "runtime_capacity", Required: true, Status: workloadapi.CheckPass, Message: "effective job parameters validated; capacity is not an exclusive reservation"}
	if err := ValidateJobCapacity(name, f); err != nil {
		check.Status = workloadapi.CheckFail
		check.Message = err.Error()
		p.Preflight.Status = workloadapi.CheckFail
		p.Preflight.Message = err.Error()
	}
	p.Assets = append(p.Assets, check)
	p.FrozenParameters = workloadapi.CopyFrozenParameters(f)
	p.ConfigurationSHA256 = f.SHA256
	return p, nil
}

func hasNPUJobParameters() bool {
	switch env("CATMONITOR_NPU_COMPAT_PROFILE") {
	case "portable-model-sdc-memory-job-v1", "a2-upstream-model-sdc-memory-job-v1":
		return true
	default:
		return false
	}
}
