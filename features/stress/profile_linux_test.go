//go:build linux

package stress

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDispatcherDescribeStreamIsJSONAndDoesNotLaunchWorkload(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "launched")
	stream := writeExecutable(t, dir, "stream_omp", "#!/bin/sh\ntouch "+shellLiteral(marker)+"\n")
	numactl := writeExecutable(t, dir, "numactl", "#!/bin/sh\ntouch "+shellLiteral(marker)+"\n")
	script := configuredDispatcher(t, dir, map[string]string{
		"STREAM_EXECUTABLE": stream,
		"STREAM_NUMACTL":    numactl,
		"STREAM_THREADS":    "32",
	})

	output, err := exec.Command("bash", script, "describe", "stream").Output()
	if err != nil {
		t.Fatal(err)
	}
	var profile ExecutionProfile
	if err := json.Unmarshal(output, &profile); err != nil {
		t.Fatalf("describe output is not JSON: %v: %s", err, output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("describe launched the benchmark or NUMA wrapper: %v", err)
	}
	if profile.ProtocolVersion != 1 || profile.Benchmark != "stream" ||
		profile.Preflight.Status != CheckPass || profile.Resources.TotalWorkers != 32 ||
		len(profile.Assets) != 2 || profile.Assets[0].SHA256 == "" {
		t.Fatalf("unexpected STREAM profile: %+v", profile)
	}
}

func TestDispatcherDescribeAscendNPUBurnIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "launched")
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	npuBurn := writeExecutable(t, dir, "npu-burn", "#!/bin/sh\ntouch "+shellLiteral(marker)+"\n")
	script := configuredDispatcher(t, dir, map[string]string{
		"NPU_BURN_EXECUTABLE":               npuBurn,
		"NPU_BURN_OUTPUT_DIR":               outputDir,
		"NPU_BURN_GROUP":                    "group_basic",
		"NPU_BURN_DEVICE":                   "0,1,2,3",
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "300",
		"NPU_BURN_CHIP_GENERATION":          "A3",
	})
	output, err := exec.Command("bash", script, "describe", "npu_burn").Output()
	if err != nil {
		t.Fatal(err)
	}
	var profile ExecutionProfile
	if err := json.Unmarshal(output, &profile); err != nil {
		t.Fatalf("describe output is not JSON: %v: %s", err, output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("describe launched Ascend NPU Burn: %v", err)
	}
	if profile.Benchmark != "npu_burn" || profile.Preflight.Status != CheckPass ||
		profile.Resources.RuntimeSeconds != 300 || profile.Resources.ProblemSize != "group_basic" ||
		len(profile.Assets) != 3 || profile.MPI.Required {
		t.Fatalf("unexpected Ascend NPU Burn profile: %+v", profile)
	}
}

func TestDispatcherDescribeAscendNPUBurnDockerExecProfile(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "launched")
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	npuBurn := writeExecutable(t, dir, "npu-burn", "#!/bin/sh\ntouch "+shellLiteral(marker)+"\n")
	docker := writeExecutable(t, dir, "docker", `#!/bin/bash
case "$1" in
  inspect) printf 'true|catmonitor/npuburn:a2-cann83\n' ;;
  exec)
    if printf '%s' "${5-}" | grep -Fq '/dev/davinci[0-9]*'; then
      printf '0\n1\n2\n'
    elif printf '%s' "${5-}" | grep -Fq 'lspci_path='; then
      printf '0\n1\n2\n'
    else
      [ "$5" = '/usr/bin/test -x "$1"' ] || exit 97
      shift 2
      exec "$@"
    fi
    ;;
  *) exit 98 ;;
esac
`)
	script := configuredDispatcher(t, dir, map[string]string{
		"NPU_BURN_BACKEND":                  "docker_exec",
		"NPU_BURN_EXECUTABLE":               npuBurn,
		"NPU_BURN_CONTAINER_RUNTIME":        docker,
		"NPU_BURN_CONTAINER_NAME":           "catmonitor-npuburn-a2",
		"NPU_BURN_CONTAINER_IMAGE":          "catmonitor/npuburn:a2-cann83",
		"NPU_BURN_RUNTIME_CANN":             "8.3.RC2",
		"NPU_BURN_RUNTIME_TORCH_NPU":        "2.8.0",
		"NPU_BURN_SOC_MODEL":                "Ascend 910B4",
		"NPU_BURN_OUTPUT_DIR":               outputDir,
		"NPU_BURN_RUN_CASE":                 "matmul",
		"NPU_BURN_DEVICE":                   "0",
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "120",
		"NPU_BURN_CHIP_GENERATION":          "A2",
	})
	output, err := exec.Command("bash", script, "describe", "npu_burn").Output()
	if err != nil {
		t.Fatal(err)
	}
	var profile ExecutionProfile
	if err := json.Unmarshal(output, &profile); err != nil {
		t.Fatalf("describe output is not JSON: %v: %s", err, output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("describe launched Ascend NPU Burn: %v", err)
	}
	parameters := make(map[string]string, len(profile.Parameters))
	for _, parameter := range profile.Parameters {
		parameters[parameter.Key] = parameter.Value
	}
	if profile.Preflight.Status != CheckPass || len(profile.Assets) != 4 ||
		parameters["backend"] != "docker_exec" ||
		parameters["image"] != "catmonitor/npuburn:a2-cann83" ||
		parameters["cann"] != "8.3.RC2" || parameters["torch_npu"] != "2.8.0" ||
		parameters["soc"] != "Ascend 910B4" || parameters["chip_generation"] != "A2" ||
		parameters["output_mode"] != "upstream_default" ||
		parameters["tool_output_directory"] != "/opt/catmonitor/npuburn-home/.ascend_npu_burn/output" ||
		parameters["result_directory"] != outputDir || parameters["sdc_detection"] != "enabled" ||
		parameters["device_namespace"] != "npu_burn_logical" ||
		parameters["available_devices"] != "0,1,2" ||
		parameters["topology_source"] != "container_lspci" ||
		parameters["pci_topology_devices"] != "0,1,2" {
		t.Fatalf("unexpected container NPU Burn profile: %+v parameters=%v", profile, parameters)
	}
	if _, exists := parameters["execution_count"]; exists {
		t.Fatalf("dead upstream exec_count must not be exposed: parameters=%v", parameters)
	}
}

func TestDispatcherRejectsDeviceOutsideSixteenDevicePCITopologyBeforeWorkload(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "workload-launched")
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	npuBurn := writeExecutable(t, dir, "npu-burn", "#!/bin/sh\ntouch "+shellLiteral(marker)+"\n")
	docker := writeExecutable(t, dir, "docker", `#!/bin/bash
case "$1" in
  inspect) printf 'true|catmonitor/npuburn:a3-v3\n' ;;
  exec)
    if printf '%s' "${5-}" | grep -Fq '/dev/davinci[0-9]*'; then
      printf '0\n1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n'
    elif printf '%s' "${5-}" | grep -Fq 'lspci_path='; then
      printf '0\n1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n'
    else
      shift 2
      exec "$@"
    fi
    ;;
  *) exit 98 ;;
esac
`)
	script := configuredDispatcher(t, dir, map[string]string{
		"NPU_BURN_BACKEND":                  "docker_exec",
		"NPU_BURN_EXECUTABLE":               npuBurn,
		"NPU_BURN_CONTAINER_RUNTIME":        docker,
		"NPU_BURN_CONTAINER_NAME":           "catmonitor-npuburn-a3",
		"NPU_BURN_CONTAINER_IMAGE":          "catmonitor/npuburn:a3-v3",
		"NPU_BURN_OUTPUT_DIR":               outputDir,
		"NPU_BURN_RUN_CASE":                 "quant_matmul",
		"NPU_BURN_DEVICE":                   "16",
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "120",
		"NPU_BURN_CHIP_GENERATION":          "A3",
	})

	output, err := exec.Command("bash", script, "describe", "npu_burn").Output()
	if err != nil {
		t.Fatal(err)
	}
	var profile ExecutionProfile
	if err := json.Unmarshal(output, &profile); err != nil {
		t.Fatalf("describe output is not JSON: %v: %s", err, output)
	}
	parameters := make(map[string]string, len(profile.Parameters))
	for _, parameter := range profile.Parameters {
		parameters[parameter.Key] = parameter.Value
	}
	var topology *AssetCheck
	for index := range profile.Assets {
		if profile.Assets[index].Name == "logical_devices" {
			topology = &profile.Assets[index]
			break
		}
	}
	if profile.Preflight.Status != CheckFail || topology == nil || topology.Status != CheckFail ||
		parameters["device_namespace"] != "npu_burn_logical" ||
		parameters["available_devices"] != "0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15" ||
		parameters["pci_topology_devices"] != "0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15" ||
		!strings.Contains(topology.Message, "logical device 16 is unavailable") ||
		!strings.Contains(topology.Message, "Do not use npu-smi Phy-ID") {
		t.Fatalf("physical ID was not rejected by describe: profile=%+v parameters=%v", profile, parameters)
	}
	manager := NewManager(Config{
		Enabled:    true,
		ScriptPath: script,
		Benchmarks: map[string]BenchmarkConfig{
			"npu_burn": {Enabled: true, Timeout: time.Minute},
		},
	})
	available, message := manager.Availability("npu_burn")
	if available || !strings.Contains(message, "logical_devices (/dev/davinci[0-9]*)") ||
		!strings.Contains(message, "logical device 16 is unavailable") {
		t.Fatalf("manager did not surface logical device preflight: available=%v message=%q", available, message)
	}

	runOutput, err := exec.Command("bash", script, "npu_burn").CombinedOutput()
	if err == nil || !strings.Contains(string(runOutput), "valid logical devices: 0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15") {
		t.Fatalf("invalid device must fail before workload: err=%v output=%s", err, runOutput)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("invalid device launched NPU Burn workload: %v", err)
	}
}

func TestDispatcherAcceptsDevice14OnSixteenDevicePCITopology(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var lspciOutput strings.Builder
	for id := 0; id < 16; id++ {
		fmt.Fprintf(&lspciOutput, "0000:%02x:00.0 Processing accelerators: Huawei Technologies Co., Ltd. Device d803\n", id)
	}
	writeExecutable(t, dir, "lspci", "#!/bin/sh\nprintf '%s' "+shellLiteral(lspciOutput.String())+"\n")
	docker := writeExecutable(t, dir, "docker", `#!/bin/bash
case "$1" in
  inspect) printf 'true|catmonitor/npuburn:a3-v5\n' ;;
  exec)
    if printf '%s' "${5-}" | grep -Fq '/dev/davinci[0-9]*'; then
      seq 0 15
    elif printf '%s' "${5-}" | grep -Fq 'lspci_path='; then
      PATH=`+shellLiteral(dir)+`:"$PATH" /bin/sh -c "$5"
    elif [ "${5-}" = '/usr/bin/test -x "$1"' ]; then
      exit 0
    else
      exit 97
    fi
    ;;
  *) exit 98 ;;
esac
`)
	script := configuredDispatcher(t, dir, map[string]string{
		"NPU_BURN_BACKEND":                  "docker_exec",
		"NPU_BURN_EXECUTABLE":               "/usr/local/bin/catmonitor-npu-burn",
		"NPU_BURN_CONTAINER_RUNTIME":        docker,
		"NPU_BURN_CONTAINER_NAME":           "catmonitor-npuburn-a3-v5",
		"NPU_BURN_CONTAINER_IMAGE":          "catmonitor/npuburn:a3-v5",
		"NPU_BURN_RUNTIME_CANN":             "9.0.1",
		"NPU_BURN_RUNTIME_TORCH_NPU":        "2.10.0.post2",
		"NPU_BURN_SOC_MODEL":                "Ascend910_9382",
		"NPU_BURN_OUTPUT_DIR":               outputDir,
		"NPU_BURN_RUN_CASE":                 "quant_matmul",
		"NPU_BURN_DEVICE":                   "14",
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "120",
		"NPU_BURN_CHIP_GENERATION":          "A3",
	})
	output, err := exec.Command("bash", script, "describe", "npu_burn").Output()
	if err != nil {
		t.Fatal(err)
	}
	var profile ExecutionProfile
	if err := json.Unmarshal(output, &profile); err != nil {
		t.Fatalf("describe output is not JSON: %v: %s", err, output)
	}
	parameters := make(map[string]string, len(profile.Parameters))
	for _, parameter := range profile.Parameters {
		parameters[parameter.Key] = parameter.Value
	}
	if profile.Preflight.Status != CheckPass || parameters["devices"] != "14" ||
		parameters["available_devices"] != "0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15" ||
		parameters["pci_topology_devices"] != parameters["available_devices"] {
		t.Fatalf("device 14 was not accepted on matching 16-device topology: profile=%+v parameters=%v", profile, parameters)
	}
}

func TestDispatcherValidatesNPULogicalDeviceLists(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	deviceRoot := filepath.Join(dir, "devices")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(deviceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"0", "2", "7"} {
		if err := os.WriteFile(filepath.Join(deviceRoot, "davinci"+id), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	npuBurn := writeExecutable(t, dir, "npu-burn", "#!/bin/sh\nexit 0\n")
	tests := []struct {
		name      string
		selection string
		wantPass  bool
		message   string
	}{
		{name: "non_contiguous_list", selection: "0,2,7", wantPass: true},
		{name: "single_device", selection: "7", wantPass: true},
		{name: "duplicate", selection: "7,7", message: "duplicate logical device 7"},
		{name: "whitespace", selection: "0, 2, 7", message: "without whitespace"},
		{name: "negative", selection: "-1", message: "without whitespace"},
		{name: "non_numeric", selection: "foo", message: "without whitespace"},
		{name: "unavailable", selection: "8", message: "logical device 8 is unavailable"},
		{name: "empty", selection: "", message: "must explicitly select"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			script := configuredDispatcher(t, dir, map[string]string{
				"NPU_BURN_EXECUTABLE":               npuBurn,
				"NPU_BURN_OUTPUT_DIR":               outputDir,
				"NPU_BURN_RUN_CASE":                 "quant_matmul",
				"NPU_BURN_DEVICE":                   test.selection,
				"NPU_BURN_DEVICE_ROOT":              deviceRoot,
				"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "120",
				"NPU_BURN_CHIP_GENERATION":          "A3",
			})
			output, err := exec.Command("bash", script, "describe", "npu_burn").Output()
			if err != nil {
				t.Fatal(err)
			}
			var profile ExecutionProfile
			if err := json.Unmarshal(output, &profile); err != nil {
				t.Fatalf("describe output is not JSON: %v: %s", err, output)
			}
			parameters := make(map[string]string, len(profile.Parameters))
			for _, parameter := range profile.Parameters {
				parameters[parameter.Key] = parameter.Value
			}
			if parameters["available_devices"] != "0,2,7" {
				t.Fatalf("non-contiguous topology was changed: parameters=%v", parameters)
			}
			var topology *AssetCheck
			for index := range profile.Assets {
				if profile.Assets[index].Name == "logical_devices" {
					topology = &profile.Assets[index]
					break
				}
			}
			if topology == nil {
				t.Fatal("logical_devices asset is missing")
			}
			if test.wantPass {
				if profile.Preflight.Status != CheckPass || topology.Status != CheckPass {
					t.Fatalf("valid selection failed: profile=%+v", profile)
				}
			} else if profile.Preflight.Status != CheckFail || topology.Status != CheckFail ||
				!strings.Contains(topology.Message, test.message) {
				t.Fatalf("invalid selection was not rejected as expected: profile=%+v want message %q", profile, test.message)
			}
		})
	}
}

func TestDispatcherDockerDeviceProbeDoesNotFallBackToHost(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	hostDeviceRoot := filepath.Join(dir, "host-devices")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(hostDeviceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostDeviceRoot, "davinci7"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	docker := writeExecutable(t, dir, "docker", `#!/bin/bash
case "$1" in
  inspect) printf 'true|catmonitor/npuburn:a3-v4\n' ;;
  exec)
    if [ "${5-}" = '/usr/bin/test -x "$1"' ]; then exit 0; fi
    exit 42
    ;;
  *) exit 98 ;;
esac
`)
	script := configuredDispatcher(t, dir, map[string]string{
		"NPU_BURN_BACKEND":                  "docker_exec",
		"NPU_BURN_EXECUTABLE":               "/usr/local/bin/catmonitor-npu-burn",
		"NPU_BURN_CONTAINER_RUNTIME":        docker,
		"NPU_BURN_CONTAINER_NAME":           "catmonitor-npuburn-a3",
		"NPU_BURN_CONTAINER_IMAGE":          "catmonitor/npuburn:a3-v4",
		"NPU_BURN_OUTPUT_DIR":               outputDir,
		"NPU_BURN_RUN_CASE":                 "quant_matmul",
		"NPU_BURN_DEVICE":                   "7",
		"NPU_BURN_DEVICE_ROOT":              hostDeviceRoot,
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "120",
		"NPU_BURN_CHIP_GENERATION":          "A3",
	})
	output, err := exec.Command("bash", script, "describe", "npu_burn").Output()
	if err != nil {
		t.Fatal(err)
	}
	var profile ExecutionProfile
	if err := json.Unmarshal(output, &profile); err != nil {
		t.Fatalf("describe output is not JSON: %v: %s", err, output)
	}
	parameters := make(map[string]string, len(profile.Parameters))
	for _, parameter := range profile.Parameters {
		parameters[parameter.Key] = parameter.Value
	}
	var topology *AssetCheck
	for index := range profile.Assets {
		if profile.Assets[index].Name == "logical_devices" {
			topology = &profile.Assets[index]
			break
		}
	}
	if profile.Preflight.Status != CheckFail || topology == nil || topology.Status != CheckFail ||
		parameters["available_devices"] != "" ||
		!strings.Contains(topology.Message, "cannot inspect /dev/davinciN in the fixed container") {
		t.Fatalf("docker topology probe incorrectly fell back to host devices: profile=%+v parameters=%v", profile, parameters)
	}
}

func TestDispatcherRejectsContainerAndPCITopologyMismatch(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	docker := writeExecutable(t, dir, "docker", `#!/bin/bash
case "$1" in
  inspect) printf 'true|catmonitor/npuburn:a3-v4\n' ;;
  exec)
    if printf '%s' "${5-}" | grep -Fq '/dev/davinci[0-9]*'; then
      seq 0 15
    elif printf '%s' "${5-}" | grep -Fq 'lspci_path='; then
      seq 0 7
    elif [ "${5-}" = '/usr/bin/test -x "$1"' ]; then
      exit 0
    else
      exit 97
    fi
    ;;
  *) exit 98 ;;
esac
`)
	script := configuredDispatcher(t, dir, map[string]string{
		"NPU_BURN_BACKEND":                  "docker_exec",
		"NPU_BURN_EXECUTABLE":               "/usr/local/bin/catmonitor-npu-burn",
		"NPU_BURN_CONTAINER_RUNTIME":        docker,
		"NPU_BURN_CONTAINER_NAME":           "catmonitor-npuburn-a3-v4",
		"NPU_BURN_CONTAINER_IMAGE":          "catmonitor/npuburn:a3-v4",
		"NPU_BURN_OUTPUT_DIR":               outputDir,
		"NPU_BURN_RUN_CASE":                 "quant_matmul",
		"NPU_BURN_DEVICE":                   "14",
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "120",
		"NPU_BURN_CHIP_GENERATION":          "A3",
	})
	output, err := exec.Command("bash", script, "describe", "npu_burn").Output()
	if err != nil {
		t.Fatal(err)
	}
	var profile ExecutionProfile
	if err := json.Unmarshal(output, &profile); err != nil {
		t.Fatalf("describe output is not JSON: %v: %s", err, output)
	}
	parameters := make(map[string]string, len(profile.Parameters))
	for _, parameter := range profile.Parameters {
		parameters[parameter.Key] = parameter.Value
	}
	var topology *AssetCheck
	for index := range profile.Assets {
		if profile.Assets[index].Name == "logical_devices" {
			topology = &profile.Assets[index]
			break
		}
	}
	if profile.Preflight.Status != CheckFail || topology == nil || topology.Status != CheckFail ||
		parameters["available_devices"] != "0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15" ||
		parameters["pci_topology_devices"] != "0,1,2,3,4,5,6,7" ||
		!strings.Contains(topology.Message, "do not match NPU Burn lspci topology") {
		t.Fatalf("container/lspci topology mismatch was not rejected: profile=%+v parameters=%v", profile, parameters)
	}
}

func TestDispatcherDescribeAscendNPUBurnRejectsMissingContainerExecutable(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	docker := writeExecutable(t, dir, "docker", `#!/bin/bash
case "$1" in
  inspect) printf 'true|catmonitor/npuburn:a3-candidate\n' ;;
  exec)
    [ "$5" = '/usr/bin/test -x "$1"' ] || exit 97
    shift 2
    exec "$@"
    ;;
  *) exit 98 ;;
esac
`)
	script := configuredDispatcher(t, dir, map[string]string{
		"NPU_BURN_BACKEND":                  "docker_exec",
		"NPU_BURN_EXECUTABLE":               "/missing/catmonitor-npu-burn",
		"NPU_BURN_CONTAINER_RUNTIME":        docker,
		"NPU_BURN_CONTAINER_NAME":           "catmonitor-npuburn-a3",
		"NPU_BURN_CONTAINER_IMAGE":          "catmonitor/npuburn:a3-candidate",
		"NPU_BURN_RUNTIME_CANN":             "9.0.1",
		"NPU_BURN_RUNTIME_TORCH_NPU":        "2.10.0.post2",
		"NPU_BURN_SOC_MODEL":                "Ascend910_9382",
		"NPU_BURN_OUTPUT_DIR":               outputDir,
		"NPU_BURN_RUN_CASE":                 "quant_matmul",
		"NPU_BURN_DEVICE":                   "0",
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "120",
		"NPU_BURN_CHIP_GENERATION":          "A3",
	})
	output, err := exec.Command("bash", script, "describe", "npu_burn").Output()
	if err != nil {
		t.Fatal(err)
	}
	var profile ExecutionProfile
	if err := json.Unmarshal(output, &profile); err != nil {
		t.Fatalf("describe output is not JSON: %v: %s", err, output)
	}
	var containerAsset *AssetCheck
	for index := range profile.Assets {
		if profile.Assets[index].Name == "container" {
			containerAsset = &profile.Assets[index]
			break
		}
	}
	if profile.Preflight.Status != CheckFail || containerAsset == nil ||
		containerAsset.Status != CheckFail ||
		!strings.Contains(containerAsset.Message, "container executable is unavailable") {
		t.Fatalf("missing container executable was not rejected: %+v", profile)
	}
}

func TestManagerAvailabilityReportsContainerPreflightFailures(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	missingRuntime := filepath.Join(dir, "missing-docker")
	script := configuredDispatcher(t, dir, map[string]string{
		"NPU_BURN_BACKEND":                  "docker_exec",
		"NPU_BURN_EXECUTABLE":               "/opt/npuburn/bin/npu-burn",
		"NPU_BURN_CONTAINER_RUNTIME":        missingRuntime,
		"NPU_BURN_CONTAINER_NAME":           "catmonitor-npuburn-a2",
		"NPU_BURN_CONTAINER_IMAGE":          "catmonitor/npuburn:a2-cann83",
		"NPU_BURN_RUNTIME_CANN":             "8.3.RC2",
		"NPU_BURN_RUNTIME_TORCH_NPU":        "2.8.0",
		"NPU_BURN_SOC_MODEL":                "Ascend 910B4",
		"NPU_BURN_OUTPUT_DIR":               outputDir,
		"NPU_BURN_RUN_CASE":                 "matmul",
		"NPU_BURN_DEVICE":                   "0",
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "120",
		"NPU_BURN_CHIP_GENERATION":          "A2",
	})
	manager := NewManager(Config{
		Enabled: true, ScriptPath: script,
		Benchmarks: map[string]BenchmarkConfig{
			"npu_burn": {Enabled: true, Timeout: time.Minute},
		},
	})
	available, message := manager.Availability("npu_burn")
	if available || !strings.Contains(message, "container_runtime ("+missingRuntime+"): executable is unavailable") ||
		!strings.Contains(message, "container (catmonitor-npuburn-a2): container runtime is unavailable") {
		t.Fatalf("container failure was not explained: available=%v message=%q", available, message)
	}
}

func TestDispatcherDescribeHPLDetectsMPIABIMismatch(t *testing.T) {
	dir := t.TempDir()
	hplDir := filepath.Join(dir, "hpl")
	if err := os.Mkdir(hplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hpl := writeExecutable(t, hplDir, "xhpl", "#!/bin/sh\nexit 0\n")
	mpirun := writeExecutable(t, dir, "mpirun", "#!/bin/sh\necho 'HYDRA build details MPICH'\n")
	writeExecutable(t, dir, "ldd", "#!/bin/sh\necho 'libopen-rte.so.40 => /test/libopen-rte.so.40'\n")
	hplDat := strings.Join([]string{
		"HPLinpack benchmark input file",
		"CATMonitor HPL stress",
		"HPL.out",
		"6",
		"1",
		"50000",
		"1",
		"256",
		"0",
		"1",
		"4",
		"2",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(hplDir, "HPL.dat"), []byte(hplDat), 0o644); err != nil {
		t.Fatal(err)
	}
	script := configuredDispatcher(t, dir, map[string]string{
		"HPL_WORKDIR":             hplDir,
		"HPL_EXECUTABLE":          hpl,
		"HPL_MPI_LAUNCHER":        mpirun,
		"HPL_MPI_PROCESSES":       "8",
		"HPL_THREADS_PER_PROCESS": "12",
	})
	command := exec.Command("bash", script, "describe", "hpl")
	command.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var profile ExecutionProfile
	if err := json.Unmarshal(output, &profile); err != nil {
		t.Fatalf("describe output is not JSON: %v: %s", err, output)
	}
	if profile.MPI.Implementation != "mpich" || profile.MPI.ExecutableABI != "openmpi" ||
		profile.MPI.Status != CheckFail || profile.Preflight.Status != CheckFail {
		t.Fatalf("MPI ABI mismatch was not detected: %+v", profile.MPI)
	}
	if profile.Resources.TotalWorkers != 96 || profile.Resources.ProblemSize != "50000" {
		t.Fatalf("unexpected HPL resources: %+v", profile.Resources)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	manager := NewManager(Config{
		Enabled: true, ScriptPath: script,
		Benchmarks: map[string]BenchmarkConfig{"hpl": {Enabled: true, Timeout: time.Minute}},
	})
	available, message := manager.Availability("hpl")
	if available || !strings.Contains(message, "preflight") {
		t.Fatalf("explicit MPI ABI mismatch must block execution: available=%v message=%q", available, message)
	}
}

func TestManagerPersistsProfileAndConfigurationHash(t *testing.T) {
	dir := t.TempDir()
	stream := writeExecutable(t, dir, "stream_omp", "#!/bin/sh\nprintf 'Copy: 1\\nScale: 2\\nAdd: 3\\nTriad: 4\\n'\n")
	numactl := writeExecutable(t, dir, "numactl", "#!/bin/sh\nshift\nexec \"$@\"\n")
	script := configuredDispatcher(t, dir, map[string]string{
		"STREAM_EXECUTABLE": stream,
		"STREAM_NUMACTL":    numactl,
		"STREAM_THREADS":    "4",
	})
	manager := NewManager(Config{
		Enabled: true, ScriptPath: script,
		ReportPath: filepath.Join(dir, "stress-latest.json"),
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Minute},
		},
	})
	defaultProfile, err := manager.Describe("stream")
	if err != nil {
		t.Fatal(err)
	}
	started, err := manager.StartWithOptions([]string{"stream"}, RunOptions{
		Initiator: InitiatorCLI, Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForJob(t, manager, started.JobID)
	if finished.ConfigurationSHA256 == "" || len(finished.ConfigurationSHA256) != 64 ||
		finished.Benchmarks[0].Profile == nil ||
		finished.Benchmarks[0].Profile.ConfigurationSHA256 == "" ||
		finished.Benchmarks[0].Profile.TimeoutSeconds != 30 ||
		finished.Benchmarks[0].Profile.ConfigurationSHA256 == defaultProfile.ConfigurationSHA256 {
		t.Fatalf("profile snapshot was not persisted: %+v", finished)
	}
	history, err := manager.History(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ConfigurationSHA256 != finished.ConfigurationSHA256 ||
		history[0].Benchmarks[0].Profile == nil {
		t.Fatalf("history did not retain the profile: %+v", history)
	}
}

func TestManagerRejectsDispatcherWithoutDescribeProtocol(t *testing.T) {
	dir := t.TempDir()
	script := writeExecutable(t, dir, "benchmark_check.sh", "#!/bin/sh\nexit 1\n")
	manager := NewManager(Config{
		Enabled: true, ScriptPath: script,
		Benchmarks: map[string]BenchmarkConfig{
			"stream": {Enabled: true, Timeout: time.Minute},
		},
	})
	profile, err := manager.Describe("stream")
	if err == nil || profile != nil || !strings.Contains(err.Error(), "does not declare describe protocol") {
		t.Fatalf("dispatcher without describe protocol was not rejected: profile=%+v err=%v", profile, err)
	}
	available, message := manager.Availability("stream")
	if available || !strings.Contains(message, "describe/preflight failed") {
		t.Fatalf("dispatcher without describe protocol should be unavailable: %v %q", available, message)
	}
	if _, err := manager.Start([]string{"stream"}); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("dispatcher without describe protocol should not start: %v", err)
	}
}

func TestManagerRejectsMalformedDescribeJSON(t *testing.T) {
	dir := t.TempDir()
	script := writeExecutable(t, dir, "benchmark_check.sh", `#!/bin/bash
CATMONITOR_STRESS_DESCRIBE_PROTOCOL=1
printf '{"protocol_version":1,"benchmark":"stream","unknown":true}\n'
`)
	manager := NewManager(Config{
		Enabled: true, ScriptPath: script,
		Benchmarks: map[string]BenchmarkConfig{"stream": {Enabled: true, Timeout: time.Minute}},
	})
	profile, err := manager.Describe("stream")
	if err == nil || profile != nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("malformed describe JSON was not rejected: profile=%+v err=%v", profile, err)
	}
	available, message := manager.Availability("stream")
	if available || !strings.Contains(message, "describe/preflight failed") {
		t.Fatalf("malformed describe JSON should block availability: %v %q", available, message)
	}
}

func TestManagerDescribeTimeoutKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	childPID := filepath.Join(dir, "child.pid")
	script := writeExecutable(t, dir, "benchmark_check.sh", `#!/bin/bash
CATMONITOR_STRESS_DESCRIBE_PROTOCOL=1
sleep 30 &
printf '%s\n' "$!" > "$CATMONITOR_TEST_CHILD_PID"
wait
`)
	manager := NewManager(Config{
		Enabled: true, ScriptPath: script,
		Benchmarks: map[string]BenchmarkConfig{"stream": {Enabled: true, Timeout: time.Minute}},
	})
	started := time.Now()
	oldValue, hadValue := os.LookupEnv("CATMONITOR_TEST_CHILD_PID")
	if err := os.Setenv("CATMONITOR_TEST_CHILD_PID", childPID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if hadValue {
			_ = os.Setenv("CATMONITOR_TEST_CHILD_PID", oldValue)
		} else {
			_ = os.Unsetenv("CATMONITOR_TEST_CHILD_PID")
		}
	}()
	_, err := manager.Describe("stream")
	if err == nil || !strings.Contains(err.Error(), "timed out") ||
		time.Since(started) > 4*time.Second {
		t.Fatalf("describe timeout was not bounded: elapsed=%s err=%v", time.Since(started), err)
	}
	data, readErr := os.ReadFile(childPID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("describe child process %d survived timeout", pid)
}
