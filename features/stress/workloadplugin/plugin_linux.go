//go:build linux

// Package workloadplugin implements the fixed, image-owned benchmark plugins
// used by catmonitor-stress-exec. No request can select an executable, command
// line, environment variable, working directory, or container transport.
package workloadplugin

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

// Command is a fully resolved workload invocation owned by an image plugin.
type Command struct {
	Path      string
	Args      []string
	Dir       string
	Env       []string
	ResultDir string
	Complete  func(string) (string, error)
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
		return Command{Path: launcher, Args: []string{"-np", strconv.Itoa(processes), executable}, Dir: dir, Env: extra}, nil
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
	assets := []workloadapi.AssetCheck{asset("executable", executable, "executable"), asset("working_directory", dir, "directory"), asset("input_file", input, "file"), asset("mpi_launcher", launcher, "executable")}
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
	executable, output := env("NPU_BURN_EXECUTABLE"), env("NPU_BURN_OUTPUT_DIR")
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
	assets := []workloadapi.AssetCheck{asset("executable", executable, "executable"), asset("output_directory", output, "directory")}
	deviceAsset := workloadapi.AssetCheck{Name: "logical_devices", Path: filepath.Join(envDefault("NPU_BURN_DEVICE_ROOT", "/dev"), "davinci[0-9]*"), Kind: "device_topology", Required: true, Status: workloadapi.CheckPass, Message: topologyMessage}
	if topologyErr != nil {
		deviceAsset.Status = workloadapi.CheckFail
		deviceAsset.Message = topologyErr.Error()
	}
	assets = append(assets, deviceAsset)
	failed += failedAssets(assets)
	params := []workloadapi.ProfileParameter{
		parameter("backend", "Execution backend", "workload_container", ""), parameter("executable", "Executable", executable, ""),
		parameter("output_mode", "Output mode", "upstream_default", ""), parameter("tool_output_directory", "Tool output directory", output, ""),
		parameter("result_directory", "Result directory", output, ""), parameter("selector", "Workload selector", selector, ""), parameter("workload", "Run case / group", workload, ""),
		parameter("devices", "NPU devices", env("NPU_BURN_DEVICE"), ""), parameter("device_namespace", "Device namespace", "npu_burn_logical", ""),
		parameter("device_node_ids", "Visible /dev/davinci node IDs", strings.Join(nodes, ","), ""), parameter("available_devices", "Available logical devices", strings.Join(devices, ","), ""),
		parameter("topology_source", "Topology source", "container_lspci", ""), parameter("pci_topology_devices", "PCI topology logical devices", strings.Join(topology, ","), ""),
		parameter("chip_generation", "Chip generation", env("NPU_BURN_CHIP_GENERATION"), ""), parameter("sdc_detection", "SDC detection", "enabled", ""),
		parameter("internal_timeout", "Per-case timeout", env("NPU_BURN_INTERNAL_TIMEOUT_SECONDS"), "seconds"),
	}
	return profile("npu_burn", params, workloadapi.ResourceProfile{RuntimeSeconds: seconds, ProblemSize: workload}, assets, mpiNone("MPI is not used by Ascend NPU Burn"), failed, 0)
}

func resolveNPUBurn() (Command, error) {
	executable, output := env("NPU_BURN_EXECUTABLE"), env("NPU_BURN_OUTPUT_DIR")
	if err := executableFile("Ascend NPU Burn", executable); err != nil {
		return Command{}, err
	}
	if err := directory("Ascend NPU Burn output directory", output); err != nil {
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
	devices, _, _, _, err := npuTopology(topologyCtx)
	if err != nil {
		return Command{}, err
	}
	if err := validateDeviceSelection(env("NPU_BURN_DEVICE"), devices); err != nil {
		return Command{}, err
	}
	args := []string{"--device", env("NPU_BURN_DEVICE"), "--sdc_detect", "--timeout", strconv.Itoa(seconds), "--chip_generation", generation}
	runCase, group := env("NPU_BURN_RUN_CASE"), env("NPU_BURN_GROUP")
	if (runCase == "") == (group == "") {
		return Command{}, errors.New("configure exactly one of NPU_BURN_RUN_CASE or NPU_BURN_GROUP")
	}
	if runCase != "" {
		args = append(args, "--run_case", runCase)
	} else {
		args = append(args, "--group", group)
	}
	csv := filepath.Join(output, "npu_burn_results.csv")
	before := signature(csv)
	return Command{Path: executable, Args: args, Complete: func(console string) (string, error) {
		if strings.Contains(console, "| FAIL |") {
			return console, errors.New("Ascend NPU Burn global device summary reported failure")
		}
		if before != "" && before == signature(csv) {
			return console, errors.New("Ascend NPU Burn did not update its result CSV during this run")
		}
		summary, err := summarizeCSV(csv)
		if err != nil {
			return console, err
		}
		return strings.TrimRight(console, "\n") + "\n" + summary + "\n", nil
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
func signature(path string) string {
	info, e := os.Stat(path)
	if e != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
}
func summarizeCSV(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", fmt.Errorf("Ascend NPU Burn result CSV is missing: %w", e)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	header := false
	cases, passed, failed, errorsCount := 0, 0, 0, 0
	devices := map[string]bool{}
	seconds := 0.0
	for s.Scan() {
		fields := strings.Split(s.Text(), ",")
		if len(fields) >= 8 && fields[0] == "task" && fields[7] == "result" {
			header = true
			continue
		}
		if len(fields) < 8 {
			return "", errors.New("Ascend NPU Burn result CSV has an invalid schema")
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		if _, e = strconv.Atoi(fields[1]); e != nil {
			return "", errors.New("Ascend NPU Burn result CSV has invalid device")
		}
		caseSeconds, e := strconv.ParseFloat(fields[5], 64)
		if e != nil {
			return "", errors.New("Ascend NPU Burn result CSV has invalid execution time")
		}
		errCount, e := strconv.Atoi(fields[6])
		if e != nil {
			return "", errors.New("Ascend NPU Burn result CSV has invalid error count")
		}
		if fields[7] != "PASS" && fields[7] != "FAIL" {
			return "", errors.New("Ascend NPU Burn result CSV has invalid result")
		}
		cases++
		devices[fields[1]] = true
		seconds += caseSeconds
		errorsCount += errCount
		if fields[7] == "PASS" && errCount == 0 {
			passed++
		} else {
			failed++
		}
	}
	if e := s.Err(); e != nil {
		return "", e
	}
	if !header || cases == 0 {
		return "", errors.New("Ascend NPU Burn result CSV has no result rows")
	}
	summary := fmt.Sprintf("CATMONITOR_NPU_BURN_SUMMARY devices=%d cases=%d passed=%d failed=%d errors=%d case_time_seconds=%.6f", len(devices), cases, passed, failed, errorsCount, seconds)
	if failed > 0 || errorsCount > 0 {
		return summary, errors.New("Ascend NPU Burn reported failed cases or SDC errors")
	}
	return summary, nil
}
