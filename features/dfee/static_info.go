package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// SWStaticInfo holds static software information as label key-value pairs.
type SWStaticInfo struct {
	OSVersion          string
	NPUDriverVersion   string
	NPUFirmwareVersion string
	CANNVersion        string
	PythonVersion      string
	TorchVersion       string
	TorchNPUVersion    string
	TransformersVersion string
	MindSpeedVersion   string
	VLLMVersion        string
	VLLMAscendVersion  string
	SGLangVersion      string
	MindIEVersion      string
	VerLVersion        string
	VerLNPUVersion     string
	GPUDriverVersion   string
	CUDAVersion        string
}

// collectSWStaticInfo collects static software info. Commands marked as
// container-mode run via docker exec when dockerContainer is non-empty.
func collectSWStaticInfo(dockerContainer string) SWStaticInfo {
	return SWStaticInfo{
		OSVersion:          collectOSVersion(),
		NPUDriverVersion:   readFileValue("/usr/local/Ascend/driver/version.info"),
		NPUFirmwareVersion: readFileValue("/usr/local/Ascend/firmware/version.info"),
		CANNVersion:        collectCANNVersion(dockerContainer),
		PythonVersion:      collectPythonVersion(dockerContainer),
		TorchVersion:       collectPipPackage("torch", dockerContainer),
		TorchNPUVersion:    collectPipPackage("torch_npu", dockerContainer),
		TransformersVersion: collectPipPackage("transformers", dockerContainer),
		MindSpeedVersion:   collectPipPackage("mindspeed", dockerContainer),
		VLLMVersion:        collectPipPackage("vllm", dockerContainer),
		VLLMAscendVersion:  collectPipPackage("vllm_ascend", dockerContainer),
		SGLangVersion:      collectPipPackage("sglang", dockerContainer),
		MindIEVersion:      collectMindIEVersion(),
		VerLVersion:        collectPipPackage("verl", dockerContainer),
		VerLNPUVersion:     collectPipPackage("verl_npu", dockerContainer),
		GPUDriverVersion:   collectGPUDriverVersion(),
		CUDAVersion:        collectCUDAVersion(),
	}
}

// collectOSVersion reads /etc/os-release and extracts PRETTY_NAME.
func collectOSVersion() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			val := strings.TrimPrefix(line, "PRETTY_NAME=")
			val = strings.Trim(val, `"`)
			return strings.TrimSpace(val)
		}
	}
	return ""
}

// readFileValue reads a version.info file and extracts the Version= value.
func readFileValue(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Version=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Version="))
		}
	}
	return ""
}

// collectCANNVersion probes CANN toolkit install info files.
// When dockerContainer is set, runs grep inside the container.
func collectCANNVersion(dockerContainer string) string {
	paths := []string{
		"/usr/local/Ascend/ascend-toolkit/latest/aarch64-linux/ascend_toolkit_install.info",
		"/usr/local/Ascend/ascend-toolkit/latest/x86_64-linux/ascend_toolkit_install.info",
	}
	for _, p := range paths {
		var out []byte
		var err error
		if dockerContainer != "" {
			out, err = exec.Command("docker", "exec", dockerContainer, "sh", "-c",
				`grep "^version=" `+p+` | cut -d'=' -f2`).Output()
		} else {
			out, err = exec.Command("sh", "-c",
				`grep "^version=" `+p+` | cut -d'=' -f2`).Output()
		}
		if err == nil {
			val := strings.TrimSpace(string(out))
			if val != "" {
				return val
			}
		}
	}
	return ""
}

// collectPythonVersion runs `python -V` or `python3 -V`.
// When dockerContainer is set, runs inside the container.
func collectPythonVersion(dockerContainer string) string {
	for _, py := range []string{"python3", "python"} {
		var out []byte
		var err error
		if dockerContainer != "" {
			out, err = exec.Command("docker", "exec", dockerContainer, py, "-V").Output()
		} else {
			out, err = exec.Command(py, "-V").Output()
		}
		if err == nil {
			line := strings.TrimSpace(string(out))
			return strings.TrimPrefix(line, "Python ")
		}
	}
	return ""
}

// collectPipPackage runs `pip list` (or pip3) and looks for a package name.
// When dockerContainer is set, runs inside the container.
func collectPipPackage(pkg, dockerContainer string) string {
	for _, pip := range []string{"pip3", "pip"} {
		var out []byte
		var err error
		if dockerContainer != "" {
			out, err = exec.Command("docker", "exec", dockerContainer, pip, "list").Output()
		} else {
			out, err = exec.Command(pip, "list").Output()
		}
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == pkg {
				return fields[1]
			}
		}
	}
	return ""
}

// collectMindIEVersion reads mindie version.info file.
func collectMindIEVersion() string {
	out, err := exec.Command("sh", "-c",
		`grep "^Ascend-mindie" /usr/local/Ascend/mindie/latest/version.info | cut -d':' -f2 | sed 's/^ *//'`).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// collectGPUDriverVersion runs `nvidia-smi --query-gpu=driver_version`.
func collectGPUDriverVersion() string {
	out, err := exec.Command("nvidia-smi", "--query-gpu=driver_version", "--format=csv,noheader").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return ""
}

// collectCUDAVersion runs `nvcc --version` and extracts the release line.
func collectCUDAVersion() string {
	out, err := exec.Command("nvcc", "--version").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "release") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// HWStaticInfo holds static hardware information as label key-value pairs.
type HWStaticInfo struct {
	ProductName  string
	CPUInfo      string
	MemoryInfo   string
	DiskInfo     string
	GPUType      string
	NPUChipName  string
	PSUInfo      string
}

// collectHWStaticInfo collects static hardware info via exec.Command.
// Each field has its own fallback (empty string on failure).
func collectHWStaticInfo() HWStaticInfo {
	return HWStaticInfo{
		ProductName: collectProductName(),
		CPUInfo:     collectCPUInfo(),
		MemoryInfo:  collectMemoryInfo(),
		DiskInfo:    collectDiskInfo(),
		GPUType:     collectGPUType(),
		NPUChipName: collectNPUChipName(),
		PSUInfo:     collectPSUInfo(),
	}
}

// collectProductName runs `ipmitool fru print` and extracts the first Product Name.
func collectProductName() string {
	out, err := exec.Command("ipmitool", "fru", "print").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.TrimSpace(line)
		if strings.Contains(l, "Product Name") {
			parts := strings.SplitN(l, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// collectCPUInfo runs `lscpu` and formats as "Sockets*Model".
func collectCPUInfo() string {
	out, err := exec.Command("lscpu").Output()
	if err != nil {
		return ""
	}
	sockets := ""
	model := ""
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "Socket(s):") {
			parts := strings.SplitN(l, ":", 2)
			if len(parts) == 2 {
				sockets = strings.TrimSpace(parts[1])
			}
		}
		if strings.HasPrefix(l, "Model name:") {
			parts := strings.SplitN(l, ":", 2)
			if len(parts) == 2 {
				model = strings.TrimSpace(parts[1])
			}
		}
	}
	if model == "" {
		return ""
	}
	if sockets == "" {
		return model
	}
	return sockets + "*" + model
}

// collectMemoryInfo runs `dmidecode -t 17` and formats as "Count*Type Size".
func collectMemoryInfo() string {
	out, err := exec.Command("dmidecode", "-t", "17").Output()
	if err != nil {
		return ""
	}
	count := 0
	size := ""
	memType := ""
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "Size:") && strings.Contains(l, "GB") {
			count++
			parts := strings.SplitN(l, ":", 2)
			if len(parts) == 2 {
				size = strings.TrimSpace(parts[1])
			}
		}
		if strings.HasPrefix(l, "Type:") {
			lower := strings.ToLower(l)
			if !strings.Contains(lower, "unknown") {
				parts := strings.SplitN(l, ":", 2)
				if len(parts) == 2 {
					memType = strings.TrimSpace(parts[1])
				}
			}
		}
	}
	if count == 0 {
		return ""
	}
	if memType != "" && size != "" {
		return strconv.Itoa(count) + "*" + memType + " " + size
	}
	return ""
}

// collectDiskInfo runs `lsblk -d -o NAME,SIZE -n` and formats as "name size, name size".
// Uses strings.Fields to normalize variable whitespace between columns.
func collectDiskInfo() string {
	out, err := exec.Command("lsblk", "-d", "-o", "NAME,SIZE", "-n").Output()
	if err != nil {
		return ""
	}
	var parts []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			parts = append(parts, fields[0]+" "+fields[1])
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

// collectGPUType runs `nvidia-smi --query-gpu=name --format=csv,noheader` and takes the first line.
func collectGPUType() string {
	out, err := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 0 && lines[0] != "" {
		return strings.TrimSpace(lines[0])
	}
	return ""
}

// collectNPUChipName parses `npu-smi info` output to extract the chip name
// from the first data row after the "===" separator.
// Output format:
//   +======================+=================+...
//   | 0     910B3          | OK              |...
//   | 0                    | 0000:C1:00.0    |...
func collectNPUChipName() string {
	out, err := exec.Command("npu-smi", "info").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	foundSeparator := false
	for _, line := range lines {
		if strings.Contains(line, "===") {
			foundSeparator = true
			continue
		}
		if foundSeparator && strings.HasPrefix(strings.TrimSpace(line), "|") {
			fields := strings.Split(line, "|")
			if len(fields) >= 2 {
				// fields[1] = " 0     910B3          "
				parts := strings.Fields(fields[1])
				if len(parts) >= 2 {
					return parts[1]
				}
			}
		}
	}
	return ""
}

// collectPSUInfo runs `ipmitool fru print` and extracts PSU product names.
// Collects all Product Name lines, counts occurrences of the first name,
// formats as "Count*Name".
func collectPSUInfo() string {
	out, err := exec.Command("ipmitool", "fru", "print").Output()
	if err != nil {
		return ""
	}
	var productNames []string
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.TrimSpace(line)
		if strings.Contains(l, "Product Name") {
			parts := strings.SplitN(l, ":", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(parts[1])
				if name != "" {
					productNames = append(productNames, name)
				}
			}
		}
	}
	if len(productNames) == 0 {
		return ""
	}
	firstName := productNames[0]
	count := 0
	for _, name := range productNames {
		if name == firstName {
			count++
		}
	}
	return strconv.Itoa(count) + "*" + firstName
}
