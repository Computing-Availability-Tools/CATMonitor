//go:build linux

// Package workloadplugin implements the fixed, image-owned benchmark plugins
// used by catmonitor-stress-exec. No request can select an executable, command
// line, environment variable, working directory, or container transport.
package workloadplugin

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

// Command is a fully resolved workload invocation owned by an image plugin.
type Command struct {
	Path               string
	Args               []string
	Dir                string
	Env                []string
	ResultDir          string
	Complete           func(string) (string, error)
	NPUDevices         workloadapi.DeviceIDs
	NPUCases           workloadapi.CaseNames
	Cleanup            func()
	ModelResult        func() (*workloadapi.ModelResult, error)
	OperatorParameters func(string) (*workloadapi.NPUOperatorParameters, error)
}

func Describe(ctx context.Context, name string) (*workloadapi.ExecutionProfile, error) {
	switch name {
	case "stream":
		return describeStream(), nil
	case "hpl":
		return describeHPL(ctx), nil
	case "hpcg":
		return describeHPCG(ctx), nil
	case "npu_burn":
		if env("CATMONITOR_NPU_MODEL") != "" {
			return describeNPUModel(ctx), nil
		}
		return describeNPUBurn(ctx), nil
	default:
		return nil, fmt.Errorf("unsupported workload plugin %q", name)
	}
}

func Resolve(name string) (Command, error) {
	switch name {
	case "stream":
		executable, numa := env("STREAM_EXECUTABLE"), env("STREAM_NUMACTL")
		threads, err := nonnegative("STREAM_THREADS")
		if err != nil {
			return Command{}, err
		}
		if err := executableFile("STREAM", executable); err != nil {
			return Command{}, err
		}
		if err := executableFile("STREAM NUMA launcher", numa); err != nil {
			return Command{}, err
		}
		extra := []string{}
		if threads > 0 {
			extra = append(extra, fmt.Sprintf("OMP_NUM_THREADS=%d", threads))
		}
		return Command{Path: numa, Args: []string{"--interleave=all", executable}, Env: extra}, nil
	case "hpl":
		executable, launcher, dir := env("HPL_EXECUTABLE"), env("HPL_MPI_LAUNCHER"), env("HPL_WORKDIR")
		processes, err := positive("HPL_MPI_PROCESSES")
		if err != nil {
			return Command{}, err
		}
		threads, err := positive("HPL_THREADS_PER_PROCESS")
		if err != nil {
			return Command{}, err
		}
		if err := executableFile("HPL", executable); err != nil {
			return Command{}, err
		}
		if err := executableFile("HPL MPI launcher", launcher); err != nil {
			return Command{}, err
		}
		if err := directory("HPL working directory", dir); err != nil {
			return Command{}, err
		}
		if err := regularFile("HPL input file", filepath.Join(dir, "HPL.dat")); err != nil {
			return Command{}, err
		}
		if err := hplResourceCheck(filepath.Join(dir, "HPL.dat"), processes, threads); err != nil {
			return Command{}, err
		}
		extra := []string{fmt.Sprintf("OPENBLAS_NUM_THREADS=%d", threads), fmt.Sprintf("OMP_NUM_THREADS=%d", threads)}
		if library := env("HPL_LIBRARY_DIR"); library != "" {
			if err := directory("HPL library directory", library); err != nil {
				return Command{}, err
			}
			value := library
			if existing := os.Getenv("LD_LIBRARY_PATH"); existing != "" {
				value += ":" + existing
			}
			extra = append(extra, "LD_LIBRARY_PATH="+value)
		}
		data, err := hplInput(filepath.Join(dir, "HPL.dat"))
		if err != nil {
			return Command{}, err
		}
		runDir, err := os.MkdirTemp(dir, "job-")
		if err != nil {
			return Command{}, err
		}
		cleanup := func() { _ = os.RemoveAll(runDir) }
		if err := os.WriteFile(filepath.Join(runDir, "HPL.dat"), data, 0o600); err != nil {
			cleanup()
			return Command{}, err
		}
		return Command{Path: launcher, Args: []string{"-np", strconv.Itoa(processes), executable}, Dir: runDir, Env: extra, Cleanup: cleanup}, nil
	case "hpcg":
		executable, launcher, dir := env("HPCG_EXECUTABLE"), env("HPCG_MPI_LAUNCHER"), env("HPCG_WORKDIR")
		processes, err := positive("HPCG_MPI_PROCESSES")
		if err != nil {
			return Command{}, err
		}
		threads, err := positive("HPCG_THREADS_PER_PROCESS")
		if err != nil {
			return Command{}, err
		}
		nx, err := positive("HPCG_NX")
		if err != nil {
			return Command{}, err
		}
		ny, err := positive("HPCG_NY")
		if err != nil {
			return Command{}, err
		}
		nz, err := positive("HPCG_NZ")
		if err != nil {
			return Command{}, err
		}
		runtimeSeconds, err := positive("HPCG_RUNTIME_SECONDS")
		if err != nil {
			return Command{}, err
		}
		if err := executableFile("HPCG", executable); err != nil {
			return Command{}, err
		}
		if err := executableFile("HPCG MPI launcher", launcher); err != nil {
			return Command{}, err
		}
		if err := directory("HPCG working directory", dir); err != nil {
			return Command{}, err
		}
		return Command{Path: launcher, Args: []string{"-np", strconv.Itoa(processes), executable,
			fmt.Sprintf("--nx=%d", nx), fmt.Sprintf("--ny=%d", ny), fmt.Sprintf("--nz=%d", nz), fmt.Sprintf("--rt=%d", runtimeSeconds)},
			Dir: dir, Env: []string{fmt.Sprintf("OMP_NUM_THREADS=%d", threads), "OMP_DYNAMIC=FALSE"}, ResultDir: dir}, nil
	case "npu_burn":
		return resolveNPUBurn()
	default:
		return Command{}, fmt.Errorf("unsupported workload plugin %q", name)
	}
}

func describeStream() *workloadapi.ExecutionProfile {
	executable, numa := env("STREAM_EXECUTABLE"), env("STREAM_NUMACTL")
	threads, numberErr := nonnegative("STREAM_THREADS")
	assets := []workloadapi.AssetCheck{asset("executable", executable, "executable"), asset("numa_launcher", numa, "executable")}
	failed := failedAssets(assets)
	if numberErr != nil {
		failed++
	}
	return profile("stream", []workloadapi.ProfileParameter{
		parameter("execution_backend", "Execution backend", envDefault("CPU_EXECUTION_PROFILE", "workload_container"), ""),
		parameter("executable", "Executable", executable, ""), parameter("threads", "OpenMP threads", env("STREAM_THREADS"), "threads"),
		parameter("numa_policy", "NUMA policy", "interleave_all", ""),
	}, workloadapi.ResourceProfile{ThreadsPerProcess: threads, TotalWorkers: threads}, assets, mpiNone("MPI is not used by STREAM"), failed, 0)
}

func describeHPL(ctx context.Context) *workloadapi.ExecutionProfile {
	executable, launcher, dir := env("HPL_EXECUTABLE"), env("HPL_MPI_LAUNCHER"), env("HPL_WORKDIR")
	processes, processErr := positive("HPL_MPI_PROCESSES")
	threads, threadErr := positive("HPL_THREADS_PER_PROCESS")
	input := filepath.Join(dir, "HPL.dat")
	n, nb, p, q := hplDimensions(input)
	if env("HPL_N") != "" {
		n, nb, p, q = env("HPL_N"), env("HPL_NB"), env("HPL_P"), env("HPL_Q")
	}
	assets := []workloadapi.AssetCheck{asset("executable", executable, "executable"), asset("working_directory", dir, "directory"), asset("input_file", input, "file"), asset("mpi_launcher", launcher, "executable")}
	capacity := workloadapi.AssetCheck{Name: "resource_capacity", Path: input, Kind: "runtime_capacity", Required: true, Status: workloadapi.CheckPass, Message: "P x Q, online affinity, CPU quota and memory budget passed"}
	if err := hplResourceCheck(input, processes, threads); err != nil {
		capacity.Status = workloadapi.CheckFail
		capacity.Message = err.Error()
	}
	assets = append(assets, capacity)
	if library := env("HPL_LIBRARY_DIR"); library != "" {
		assets = append(assets, asset("library_directory", library, "directory"))
	}
	mpi := probeMPI(ctx, launcher, executable)
	failed, warned := failedAssets(assets), 0
	if processErr != nil {
		failed++
		processes = 0
	}
	if threadErr != nil {
		failed++
		threads = 0
	}
	if mpi.Status == workloadapi.CheckFail {
		failed++
	} else if mpi.Status == workloadapi.CheckWarn {
		warned++
	}
	return profile("hpl", []workloadapi.ProfileParameter{
		parameter("execution_backend", "Execution backend", envDefault("CPU_EXECUTION_PROFILE", "workload_container"), ""), parameter("executable", "Executable", executable, ""),
		parameter("mpi_processes", "MPI processes", env("HPL_MPI_PROCESSES"), "ranks"), parameter("threads_per_process", "Threads per process", env("HPL_THREADS_PER_PROCESS"), "threads"),
		parameter("n", "Problem size N", n, ""), parameter("nb", "Block size NB", nb, ""), parameter("process_grid", "Process grid P x Q", grid(p, q), ""),
	}, workloadapi.ResourceProfile{MPIProcesses: processes, ThreadsPerProcess: threads, TotalWorkers: processes * threads, ProblemSize: valueOr(n, "unknown")}, assets, mpi, failed, warned)
}

func describeHPCG(ctx context.Context) *workloadapi.ExecutionProfile {
	executable, launcher, dir := env("HPCG_EXECUTABLE"), env("HPCG_MPI_LAUNCHER"), env("HPCG_WORKDIR")
	processes, e1 := positive("HPCG_MPI_PROCESSES")
	threads, e2 := positive("HPCG_THREADS_PER_PROCESS")
	nx, e3 := positive("HPCG_NX")
	ny, e4 := positive("HPCG_NY")
	nz, e5 := positive("HPCG_NZ")
	seconds, e6 := positive("HPCG_RUNTIME_SECONDS")
	assets := []workloadapi.AssetCheck{asset("executable", executable, "executable"), asset("working_directory", dir, "directory"), asset("mpi_launcher", launcher, "executable")}
	mpi := probeMPI(ctx, launcher, executable)
	failed, warned := failedAssets(assets), 0
	for _, err := range []error{e1, e2, e3, e4, e5, e6} {
		if err != nil {
			failed++
		}
	}
	if e1 != nil {
		processes = 0
	}
	if e2 != nil {
		threads = 0
	}
	if e3 != nil {
		nx = 0
	}
	if e4 != nil {
		ny = 0
	}
	if e5 != nil {
		nz = 0
	}
	if e6 != nil {
		seconds = 0
	}
	if mpi.Status == workloadapi.CheckFail {
		failed++
	} else if mpi.Status == workloadapi.CheckWarn {
		warned++
	}
	grid := fmt.Sprintf("%dx%dx%d", nx, ny, nz)
	return profile("hpcg", []workloadapi.ProfileParameter{
		parameter("execution_backend", "Execution backend", envDefault("CPU_EXECUTION_PROFILE", "workload_container"), ""), parameter("executable", "Executable", executable, ""),
		parameter("mpi_processes", "MPI processes", env("HPCG_MPI_PROCESSES"), "ranks"), parameter("threads_per_process", "Threads per process", env("HPCG_THREADS_PER_PROCESS"), "threads"),
		parameter("local_grid", "Local grid", grid, ""), parameter("target_runtime", "Target runtime", env("HPCG_RUNTIME_SECONDS"), "seconds"),
	}, workloadapi.ResourceProfile{MPIProcesses: processes, ThreadsPerProcess: threads, TotalWorkers: processes * threads, RuntimeSeconds: seconds, ProblemSize: grid}, assets, mpi, failed, warned)
}

func describeNPUBurn(ctx context.Context) *workloadapi.ExecutionProfile {
	executable, output, logDir, runtimeHome := env("NPU_BURN_EXECUTABLE"), env("NPU_BURN_OUTPUT_DIR"), env("NPU_BURN_LOG_DIR"), env("HOME")
	seconds, secondsErr := positive("NPU_BURN_INTERNAL_TIMEOUT_SECONDS")
	devices, nodes, topology, topologyMessage, topologyErr := npuTopology(ctx)
	selector, workload := "run_case", env("NPU_BURN_RUN_CASE")
	if group := env("NPU_BURN_GROUP"); group != "" {
		selector, workload = "group", group
	}
	failed := 0
	if secondsErr != nil {
		failed++
		seconds = 0
	}
	if (env("NPU_BURN_RUN_CASE") == "") == (env("NPU_BURN_GROUP") == "") {
		failed++
	}
	if generation := env("NPU_BURN_CHIP_GENERATION"); generation != "A2" && generation != "A3" && generation != "A5" {
		failed++
	}
	assets := []workloadapi.AssetCheck{
		asset("executable", executable, "executable"),
		npuRuntimePreflight(ctx),
		writableAsset("output_directory", output),
		writableAsset("log_directory", logDir),
		writableAsset("runtime_home", runtimeHome),
	}
	deviceAsset := workloadapi.AssetCheck{Name: "logical_devices", Path: filepath.Join(envDefault("NPU_BURN_DEVICE_ROOT", "/dev"), "davinci[0-9]*"), Kind: "device_topology", Required: true, Status: workloadapi.CheckPass, Message: topologyMessage}
	if topologyErr != nil {
		deviceAsset.Status = workloadapi.CheckFail
		deviceAsset.Message = topologyErr.Error()
	}
	assets = append(assets, deviceAsset)
	if env("CATMONITOR_NPU_REQUIRE_IDLE") == "1" {
		idle := workloadapi.AssetCheck{Name: "idle_devices", Path: "npu-smi info", Kind: "process_inventory", Required: true, Status: workloadapi.CheckPass, Message: "node-wide process inventory reports no running NPU processes; not an exclusive reservation"}
		if err := checkNPUIdle(ctx, len(nodes)); err != nil {
			idle.Status = workloadapi.CheckFail
			idle.Message = err.Error()
		}
		assets = append(assets, idle)
	}
	caseParams, caseCheck := npuCaseProfile(ctx)
	if caseCheck != nil {
		assets = append(assets, *caseCheck)
	}
	failed += failedAssets(assets)
	params := []workloadapi.ProfileParameter{
		parameter("backend", "Execution backend", "workload_container", ""), parameter("executable", "Executable", executable, ""),
		parameter("output_mode", "Output mode", "upstream_default", ""), parameter("tool_output_directory", "Tool output directory", output, ""),
		parameter("result_directory", "Result directory", output, ""), parameter("log_directory", "Log directory", logDir, ""), parameter("runtime_home", "Runtime home", runtimeHome, ""),
		parameter("selector", "Workload selector", selector, ""), parameter("workload", "Run case / group", workload, ""),
		parameter("devices", "NPU devices", env("NPU_BURN_DEVICE"), ""), parameter("device_namespace", "Device namespace", "npu_burn_logical", ""),
		parameter("device_mode", "Device mapping mode", envDefault("CATMONITOR_NPU_DEVICE_MODE", "full"), ""),
		parameter("blas_threads", "BLAS/OpenMP thread budget", env("OPENBLAS_NUM_THREADS"), "threads"),
		parameter("device_node_ids", "Visible /dev/davinci node IDs", strings.Join(nodes, ","), ""), parameter("available_devices", "Available logical devices", strings.Join(devices, ","), ""),
		parameter("topology_source", "Topology source", envDefault("CATMONITOR_NPU_DEVICE_MODE", "full"), ""), parameter("pci_topology_devices", "PCI inventory (full: ordinal IDs; single: BDFs)", strings.Join(topology, ","), ""),
		parameter("chip_generation", "Chip generation", env("NPU_BURN_CHIP_GENERATION"), ""), parameter("sdc_detection", "SDC detection", "enabled", ""),
		parameter("internal_timeout", "Internal stream synchronization timeout", env("NPU_BURN_INTERNAL_TIMEOUT_SECONDS"), "seconds"),
		parameter("model_memory_mode", "Model memory mode", envDefault("CATMONITOR_MODEL_MEMORY_MODE", "standard"), ""),
	}
	params = append(params, caseParams...)
	return profile("npu_burn", params, workloadapi.ResourceProfile{RuntimeSeconds: seconds, ProblemSize: workload}, assets, mpiNone("MPI is not used by Ascend NPU Burn"), failed, 0)
}

func npuRuntimePreflight(ctx context.Context) workloadapi.AssetCheck {
	path := envDefault("NPU_BURN_PREFLIGHT_EXECUTABLE", "/usr/local/bin/catmonitor-npu-burn-preflight")
	check := workloadapi.AssetCheck{Name: "runtime_preflight", Path: path, Kind: "runtime_preflight", Required: true, Status: workloadapi.CheckPass, Message: "CANN, torch_npu, custom ops, and mapped device count passed"}
	if err := executableFile("Ascend NPU runtime preflight", path); err != nil {
		check.Status = workloadapi.CheckFail
		check.Message = err.Error()
		return check
	}
	out, err := exec.CommandContext(ctx, path).CombinedOutput()
	if err != nil {
		check.Status = workloadapi.CheckFail
		message := strings.TrimSpace(truncate(string(out), 1024))
		if message == "" {
			message = err.Error()
		}
		check.Message = "Ascend NPU runtime preflight failed: " + message
	}
	return check
}

func resolveNPUBurn() (Command, error) {
	executable, output, logDir, runtimeHome := env("NPU_BURN_EXECUTABLE"), env("NPU_BURN_OUTPUT_DIR"), env("NPU_BURN_LOG_DIR"), env("HOME")
	if err := executableFile("Ascend NPU Burn", executable); err != nil {
		return Command{}, err
	}
	if err := writableDirectory("Ascend NPU Burn output directory", output); err != nil {
		return Command{}, err
	}
	if err := writableDirectory("Ascend NPU Burn log directory", logDir); err != nil {
		return Command{}, err
	}
	if err := writableDirectory("Ascend NPU Burn runtime home", runtimeHome); err != nil {
		return Command{}, err
	}
	seconds, err := positive("NPU_BURN_INTERNAL_TIMEOUT_SECONDS")
	if err != nil {
		return Command{}, err
	}
	generation := env("NPU_BURN_CHIP_GENERATION")
	if generation != "A2" && generation != "A3" && generation != "A5" {
		return Command{}, errors.New("NPU_BURN_CHIP_GENERATION must be A2, A3, or A5")
	}
	topologyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	devices, nodes, _, _, err := npuTopology(topologyCtx)
	if err != nil {
		return Command{}, err
	}
	if err := validateDeviceSelection(env("NPU_BURN_DEVICE"), devices); err != nil {
		return Command{}, err
	}
	if err := checkNPUIdle(topologyCtx, len(nodes)); err != nil {
		return Command{}, err
	}
	args := []string{"--device", env("NPU_BURN_DEVICE"), "--sdc_detect", "--timeout", strconv.Itoa(seconds), "--chip_generation", generation}
	runCase, group := env("NPU_BURN_RUN_CASE"), env("NPU_BURN_GROUP")
	var selectedCases workloadapi.CaseNames
	if hasNPUCaseCatalog() {
		catalog, err := readNPUCaseCatalog(topologyCtx)
		if err != nil {
			return Command{}, err
		}
		if catalog.SelectionError != "" {
			return Command{}, errors.New(catalog.SelectionError)
		}
		selectedCases = catalog.Selected
		runCase, group = selectedCases.String(), ""
	}
	if (runCase == "") == (group == "") {
		return Command{}, errors.New("configure exactly one of NPU_BURN_RUN_CASE or NPU_BURN_GROUP")
	}
	if runCase != "" {
		args = append(args, "--run_case", runCase)
	} else {
		args = append(args, "--group", group)
	}
	// Preserve the deployment's tmpfs HOME budget while isolating persistent
	// CSV output. Upstream's default output derives from HOME; the default-path
	// symlink also works with the pristine CLI's inverted explicit-output check.
	runOutput, err := os.MkdirTemp(output, "run-")
	if err != nil {
		return Command{}, err
	}
	runHome, err := os.MkdirTemp(runtimeHome, "job-")
	if err != nil {
		_ = os.Remove(runOutput)
		return Command{}, err
	}
	cleanup := func() { _ = os.RemoveAll(runHome) }
	if err := os.Mkdir(filepath.Join(runHome, ".ascend_npu_burn"), 0o750); err != nil {
		cleanup()
		_ = os.Remove(runOutput)
		return Command{}, err
	}
	if err := os.Symlink(runOutput, filepath.Join(runHome, ".ascend_npu_burn", "output")); err != nil {
		cleanup()
		_ = os.Remove(runOutput)
		return Command{}, err
	}
	csv := filepath.Join(runOutput, "npu_burn_results.csv")
	parametersPath := filepath.Join(runOutput, "operator-parameters.json")
	selected := devices
	if env("NPU_BURN_DEVICE") != "all" {
		selected = strings.Split(env("NPU_BURN_DEVICE"), ",")
	}
	selectedIDs, err := workloadapi.ParseDevices(strings.Join(selected, ","))
	if err != nil {
		cleanup()
		_ = os.Remove(runOutput)
		return Command{}, err
	}
	// CANN JIT also writes relative to cwd (kernel_meta), not only HOME.
	// Keep those files inside the deployment's bounded writable tmpfs, and
	// remove them with this job. Never make the image root writable.
	return Command{Path: executable, Args: args, Dir: runHome, NPUDevices: selectedIDs, NPUCases: selectedCases, Cleanup: cleanup,
		Env: []string{"HOME=" + runHome, "NPU_BURN_OUTPUT_DIR=" + runOutput, "CATMONITOR_NPU_OPERATOR_PARAMETERS_PATH=" + parametersPath},
		OperatorParameters: func(jobID string) (*workloadapi.NPUOperatorParameters, error) {
			return readOperatorParameters(parametersPath, jobID, generation, selectedCases)
		}, Complete: func(console string) (string, error) {
			if strings.Contains(console, "| FAIL |") {
				return console, errors.New("Ascend NPU Burn global device summary reported failure")
			}
			summary, err := summarizeSelectedCSV(csv, selectedCases, selected...)
			if err != nil {
				return console, err
			}
			return strings.TrimRight(console, "\n") + "\n" + summary + "\nCATMONITOR_NPU_BURN_RESULT_CSV=" + csv + "\n", nil
		}}, nil
}

func profile(name string, parameters []workloadapi.ProfileParameter, resources workloadapi.ResourceProfile, assets []workloadapi.AssetCheck, mpi workloadapi.MPICheck, failed, warned int) *workloadapi.ExecutionProfile {
	status, message := workloadapi.CheckPass, "required assets and compatibility checks passed"
	if failed > 0 {
		status = workloadapi.CheckFail
		message = fmt.Sprintf("%d required preflight check(s) failed", failed)
	} else if warned > 0 {
		status = workloadapi.CheckWarn
		message = fmt.Sprintf("required assets are available; %d compatibility check(s) need review", warned)
	}
	return &workloadapi.ExecutionProfile{ProtocolVersion: 1, Benchmark: name, Parameters: parameters, Resources: resources, Assets: assets, MPI: mpi, Preflight: workloadapi.PreflightResult{Status: status, Message: message}}
}
func parameter(key, label, value, unit string) workloadapi.ProfileParameter {
	return workloadapi.ProfileParameter{Key: key, Label: label, Value: value, Unit: unit}
}
func mpiNone(message string) workloadapi.MPICheck {
	return workloadapi.MPICheck{Required: false, Implementation: "none", ExecutableABI: "none", Status: workloadapi.CheckPass, Message: message}
}
func asset(name, path, kind string) workloadapi.AssetCheck {
	a := workloadapi.AssetCheck{Name: name, Path: path, Kind: kind, Required: true, Status: workloadapi.CheckPass, Message: "available"}
	var info os.FileInfo
	var err error
	if !filepath.IsAbs(path) {
		err = errors.New("path is not absolute")
	} else {
		info, err = os.Stat(path)
	}
	if err == nil && kind == "executable" && info.Mode()&0111 == 0 {
		err = errors.New("executable is unavailable")
	}
	if err == nil && kind == "directory" && !info.IsDir() {
		err = errors.New("directory is unavailable")
	}
	if err == nil && kind == "file" && !info.Mode().IsRegular() {
		err = errors.New("file is unavailable")
	}
	if err != nil {
		a.Status = workloadapi.CheckFail
		a.Message = err.Error()
		return a
	}
	if info.Mode().IsRegular() {
		if data, readErr := os.ReadFile(path); readErr == nil {
			sum := sha256.Sum256(data)
			a.SHA256 = hex.EncodeToString(sum[:])
		}
	}
	return a
}
func writableAsset(name, path string) workloadapi.AssetCheck {
	a := workloadapi.AssetCheck{Name: name, Path: path, Kind: "writable_directory", Required: true, Status: workloadapi.CheckPass, Message: "available and writable"}
	if err := writableDirectory(name, path); err != nil {
		a.Status = workloadapi.CheckFail
		a.Message = err.Error()
	}
	return a
}
func failedAssets(assets []workloadapi.AssetCheck) int {
	n := 0
	for _, a := range assets {
		if a.Required && a.Status == workloadapi.CheckFail {
			n++
		}
	}
	return n
}
func env(name string) string { return strings.TrimSpace(os.Getenv(name)) }
func envDefault(name, value string) string {
	if v := env(name); v != "" {
		return v
	}
	return value
}
func positive(name string) (int, error) {
	v, err := strconv.Atoi(env(name))
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return v, nil
}
func nonnegative(name string) (int, error) {
	v, err := strconv.Atoi(env(name))
	if err != nil || v < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return v, nil
}
func executableFile(name, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s executable is not configured with an absolute path", name)
	}
	i, e := os.Stat(path)
	if e != nil || !i.Mode().IsRegular() || i.Mode()&0111 == 0 {
		return fmt.Errorf("%s executable is unavailable: %s", name, path)
	}
	return nil
}
func regularFile(name, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s is not configured with an absolute path", name)
	}
	i, e := os.Stat(path)
	if e != nil || !i.Mode().IsRegular() {
		return fmt.Errorf("%s is unavailable: %s", name, path)
	}
	return nil
}
func directory(name, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s is not configured with an absolute path", name)
	}
	i, e := os.Stat(path)
	if e != nil || !i.IsDir() {
		return fmt.Errorf("%s is unavailable: %s", name, path)
	}
	return nil
}
func writableDirectory(name, path string) error {
	if err := directory(name, path); err != nil {
		return err
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return fmt.Errorf("cannot inspect %s filesystem: %w", name, err)
	}
	const statfsReadOnly = 1 // Linux ST_RDONLY from statfs(2).
	if stat.Flags&statfsReadOnly != 0 {
		return fmt.Errorf("%s is on a read-only filesystem: %s", name, path)
	}
	return nil
}
func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
func grid(p, q string) string {
	if p == "" || q == "" {
		return ""
	}
	return p + "x" + q
}

func hplDimensions(path string) (string, string, string, string) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", "", ""
	}
	defer f.Close()
	values := []string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) > 0 {
			values = append(values, fields[0])
		}
	}
	if len(values) < 12 {
		return "", "", "", ""
	}
	for _, i := range []int{5, 7, 10, 11} {
		if _, e := strconv.Atoi(values[i]); e != nil {
			return "", "", "", ""
		}
	}
	return values[5], values[7], values[10], values[11]
}

func probeMPI(ctx context.Context, launcher, executable string) workloadapi.MPICheck {
	m := workloadapi.MPICheck{Required: true, Launcher: launcher, Implementation: "unknown", ExecutableABI: "unknown", Status: workloadapi.CheckWarn, Message: "MPI implementation or executable ABI could not be identified"}
	if executableFile("MPI launcher", launcher) != nil {
		m.Status = workloadapi.CheckFail
		m.Message = "MPI launcher is unavailable"
		return m
	}
	out, err := exec.CommandContext(ctx, launcher, "--version").CombinedOutput()
	m.Version = truncate(string(out), 1024)
	if err != nil {
		m.Status = workloadapi.CheckFail
		m.Message = "MPI launcher version probe failed"
		return m
	}
	lower := strings.ToLower(m.Version)
	if strings.Contains(lower, "open mpi") || strings.Contains(lower, "openrte") {
		m.Implementation = "openmpi"
	} else if strings.Contains(lower, "mpich") || strings.Contains(lower, "hydra") {
		m.Implementation = "mpich"
	}
	if ldd, err := exec.LookPath("ldd"); err == nil {
		if out, err := exec.CommandContext(ctx, ldd, executable).CombinedOutput(); err == nil {
			lower = strings.ToLower(string(out))
			if strings.Contains(lower, "libmpich") {
				m.ExecutableABI = "mpich"
			} else if strings.Contains(lower, "libmpi_usempif") || strings.Contains(lower, "libmpi_mpifh") || strings.Contains(lower, "libopen-rte") || strings.Contains(lower, "libopen-pal") {
				m.ExecutableABI = "openmpi"
			}
		}
	}
	if m.Implementation != "unknown" && m.ExecutableABI != "unknown" {
		if m.Implementation == m.ExecutableABI {
			m.Status = workloadapi.CheckPass
			m.Message = "launcher implementation matches executable MPI ABI"
		} else {
			m.Status = workloadapi.CheckFail
			m.Message = "launcher implementation does not match executable MPI ABI"
		}
	} else if m.Implementation != "unknown" {
		m.Message = "launcher identified; executable MPI ABI is static or could not be identified"
	}
	return m
}
func truncate(v string, n int) string {
	if len(v) > n {
		return v[:n]
	}
	return v
}

func npuTopology(ctx context.Context) (logical, nodeIDs, pciIDs []string, message string, err error) {
	root := envDefault("NPU_BURN_DEVICE_ROOT", "/dev")
	entries, e := filepath.Glob(filepath.Join(root, "davinci[0-9]*"))
	if e != nil {
		return nil, nil, nil, "", e
	}
	ids := []int{}
	for _, p := range entries {
		id, e := strconv.Atoi(strings.TrimPrefix(filepath.Base(p), "davinci"))
		if e == nil {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	for _, id := range ids {
		nodeIDs = append(nodeIDs, strconv.Itoa(id))
	}
	if len(nodeIDs) == 0 {
		return nil, nil, nil, "", errors.New("no /dev/davinciN device nodes are available")
	}
	mode := envDefault("CATMONITOR_NPU_DEVICE_MODE", "full")
	if mode == "single" {
		if len(nodeIDs) != 1 || env("NPU_BURN_DEVICE") != "0" {
			return nil, nodeIDs, nil, "", errors.New("single mode requires one mapped node and explicit logical device 0")
		}
		path := envDefault("NPU_BURN_PREFLIGHT_EXECUTABLE", "/usr/local/bin/catmonitor-npu-burn-preflight")
		out, e := exec.CommandContext(ctx, path, "--topology-json").Output()
		if e != nil {
			var exit *exec.ExitError
			if errors.As(e, &exit) && len(exit.Stderr) > 0 {
				return nil, nodeIDs, nil, "", fmt.Errorf("single-device mapping UNVERIFIED: %s", truncate(strings.TrimSpace(string(exit.Stderr)), 1024))
			}
			return nil, nodeIDs, nil, "", fmt.Errorf("single-device mapping verification failed: %w", e)
		}
		var proof struct {
			RuntimeCount int               `json:"runtime_count"`
			NodeIDs      []string          `json:"node_ids"`
			PCI          []string          `json:"pci_addresses"`
			NUMA         map[string][]int  `json:"numa_to_logical"`
			NodePCI      map[string]string `json:"node_pci"`
			RuntimePCI   map[string]string `json:"runtime_pci"`
			Source       string            `json:"mapping_source"`
		}
		if e = json.Unmarshal(out, &proof); e != nil || proof.RuntimeCount != 1 || len(proof.NodeIDs) != 1 || proof.NodeIDs[0] != nodeIDs[0] || proof.Source != "runtime_kernel_pci" || len(proof.PCI) == 0 || len(proof.NUMA) != 1 {
			return nil, nodeIDs, nil, "", errors.New("single-device mapping proof is missing or inconsistent")
		}
		bdf := proof.NodePCI[nodeIDs[0]]
		known := false
		for _, p := range proof.PCI {
			if p == bdf {
				known = true
			}
		}
		if !known || bdf == "" || len(proof.NodePCI) != 1 || len(proof.RuntimePCI) != 1 || proof.RuntimePCI["0"] != bdf {
			return nil, nodeIDs, nil, "", errors.New("runtime and kernel device PCI identities disagree or are unverified")
		}
		for node, ids := range proof.NUMA {
			n, e := strconv.Atoi(node)
			if e != nil || n < 0 || len(ids) != 1 || ids[0] != 0 {
				return nil, nodeIDs, nil, "", errors.New("single-device NUMA proof is invalid")
			}
		}
		return []string{"0"}, nodeIDs, proof.PCI, "runtime logical 0 PCI identity agrees with mapped character device kernel PCI/NUMA ancestry", nil
	}
	if mode != "full" {
		return nil, nodeIDs, nil, "", errors.New("unknown NPU device mode")
	}
	lspci := envDefault("CATMONITOR_LSPCI", "/usr/bin/lspci")
	if err := executableFile("lspci", lspci); err != nil {
		return nil, nodeIDs, nil, "", err
	}
	out, e := exec.CommandContext(ctx, lspci, "-D", "-d", "19e5:").CombinedOutput()
	if e != nil {
		return nil, nodeIDs, nil, "", fmt.Errorf("cannot enumerate NPU Burn PCI topology: %w", e)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "Processing accelerators") && strings.Contains(line, "Device") {
			pciIDs = append(pciIDs, strconv.Itoa(len(pciIDs)))
		}
	}
	if len(pciIDs) == 0 {
		return nil, nodeIDs, nil, "", errors.New("lspci found no Ascend 19e5 Processing accelerators; refusing the upstream eight-device fallback")
	}
	if len(nodeIDs) != len(pciIDs) {
		return nil, nodeIDs, pciIDs, "", fmt.Errorf("device node count (%d: %s) does not match NPU Burn lspci topology count (%d: %s)", len(nodeIDs), strings.Join(nodeIDs, ","), len(pciIDs), strings.Join(pciIDs, ","))
	}
	logical = append(logical, pciIDs...)
	if e := validateDeviceSelection(env("NPU_BURN_DEVICE"), logical); e != nil {
		return logical, nodeIDs, pciIDs, "", e
	}
	return logical, nodeIDs, pciIDs, "selected NPU Burn logical devices are available: " + env("NPU_BURN_DEVICE"), nil
}
func validateDeviceSelection(selected string, available []string) error {
	if selected == "all" {
		return nil
	}
	if selected == "" {
		return errors.New("NPU_BURN_DEVICE must explicitly select one or more logical device IDs")
	}
	valid := map[string]bool{}
	for _, id := range available {
		valid[id] = true
	}
	seen := map[string]bool{}
	for _, id := range strings.Split(selected, ",") {
		if _, e := strconv.Atoi(id); e != nil || id == "" {
			return errors.New("NPU_BURN_DEVICE must be all or comma-separated logical IDs")
		}
		if seen[id] {
			return fmt.Errorf("NPU_BURN_DEVICE contains duplicate logical device %s", id)
		}
		if !valid[id] {
			return fmt.Errorf("NPU Burn logical device %s is unavailable; valid logical devices: %s", id, strings.Join(available, ","))
		}
		seen[id] = true
	}
	return nil
}
func summarizeCSV(path string, expectedDevices ...string) (string, error) {
	return summarizeSelectedCSV(path, nil, expectedDevices...)
}

// Counts are CSV result rows, not operator/device counts or run_count totals.
// This read-only breakdown travels with the existing report output so older
// controllers and stored reports do not require a protocol migration.
type npuCSVGroup struct {
	Device          int     `json:"device"`
	Operator        string  `json:"operator"`
	Cases           int     `json:"cases"`
	Passed          int     `json:"passed"`
	Failed          int     `json:"failed"`
	Errors          int     `json:"errors"`
	CaseTimeSeconds float64 `json:"case_time_seconds"`
}

func summarizeSelectedCSV(path string, expectedCases []string, expectedDevices ...string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", fmt.Errorf("Ascend NPU Burn result CSV is missing: %w", e)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	header, e := r.Read()
	if e != nil || len(header) < 8 || strings.Join(header[:8], ",") != "task,device_id,case_idx,run_count,stream_count,exetime,err_count,result" {
		return "", errors.New("Ascend NPU Burn result CSV has an invalid header")
	}
	cases, passed, failed, errorsCount := 0, 0, 0, 0
	devices := map[string]bool{}
	coverage := map[string]map[string]bool{}
	groups := map[string]*npuCSVGroup{}
	seconds := 0.0
	for {
		fields, e := r.Read()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return "", fmt.Errorf("invalid CSV: %w", e)
		}
		if len(fields) < 8 {
			return "", errors.New("Ascend NPU Burn result CSV has an invalid schema")
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		if id, err := strconv.Atoi(fields[1]); err != nil || id < 0 {
			return "", errors.New("Ascend NPU Burn result CSV has invalid device")
		}
		caseSeconds, e := strconv.ParseFloat(fields[5], 64)
		if e != nil || caseSeconds < 0 || math.IsNaN(caseSeconds) || math.IsInf(caseSeconds, 0) {
			return "", errors.New("Ascend NPU Burn result CSV has invalid execution time")
		}
		errCount, e := strconv.Atoi(fields[6])
		if e != nil || errCount < 0 {
			return "", errors.New("Ascend NPU Burn result CSV has invalid error count")
		}
		if fields[7] != "PASS" && fields[7] != "FAIL" {
			return "", errors.New("Ascend NPU Burn result CSV has invalid result")
		}
		if runs, err := strconv.Atoi(fields[3]); err != nil || runs <= 0 {
			return "", errors.New("Ascend NPU Burn result CSV has no actual executions")
		}
		cases++
		devices[fields[1]] = true
		if coverage[fields[1]] == nil {
			coverage[fields[1]] = map[string]bool{}
		}
		coverage[fields[1]][fields[0]] = true
		key := fields[1] + ":" + fields[0]
		group := groups[key]
		if group == nil {
			id, _ := strconv.Atoi(fields[1]) // Already validated above.
			group = &npuCSVGroup{Device: id, Operator: fields[0]}
			groups[key] = group
		}
		group.Cases++
		group.Errors += errCount
		group.CaseTimeSeconds += caseSeconds
		seconds += caseSeconds
		errorsCount += errCount
		if fields[7] == "PASS" && errCount == 0 {
			passed++
			group.Passed++
		} else {
			failed++
			group.Failed++
		}
	}
	if cases == 0 {
		return "", errors.New("Ascend NPU Burn result CSV has no result rows")
	}
	if len(expectedDevices) > 0 {
		if len(devices) != len(expectedDevices) {
			return "", errors.New("CSV device coverage differs from selected devices")
		}
		for _, id := range expectedDevices {
			if !devices[id] {
				return "", fmt.Errorf("CSV missing selected logical device %s", id)
			}
		}
	}
	summary := fmt.Sprintf("CATMONITOR_NPU_BURN_SUMMARY devices=%d cases=%d passed=%d failed=%d errors=%d case_time_seconds=%.6f", len(devices), cases, passed, failed, errorsCount, seconds)
	if len(expectedCases) > 0 {
		for id, tasks := range coverage {
			if len(tasks) != len(expectedCases) {
				return "", fmt.Errorf("CSV operator coverage differs on logical device %s", id)
			}
			for _, name := range expectedCases {
				if !tasks[name] {
					return "", fmt.Errorf("CSV missing operator %s on logical device %s", name, id)
				}
			}
		}
	}
	breakdown := make([]npuCSVGroup, 0, len(groups))
	for _, group := range groups {
		breakdown = append(breakdown, *group)
	}
	sort.Slice(breakdown, func(i, j int) bool {
		if breakdown[i].Device != breakdown[j].Device {
			return breakdown[i].Device < breakdown[j].Device
		}
		return breakdown[i].Operator < breakdown[j].Operator
	})
	data, err := json.Marshal(breakdown)
	if err != nil {
		return "", fmt.Errorf("encode CSV breakdown: %w", err)
	}
	summary += "\nCATMONITOR_NPU_BURN_CASE_GROUPS=" + string(data)
	if failed > 0 || errorsCount > 0 {
		return summary, errors.New("Ascend NPU Burn reported failed cases or SDC errors")
	}
	return summary, nil
}
