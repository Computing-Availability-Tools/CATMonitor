//go:build windows

package cpu

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

var (
	kernel32DLL        = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemTimes = kernel32DLL.NewProc("GetSystemTimes")
)

type winFiletime struct {
	LowDateTime  uint32
	HighDateTime uint32
}

func getSystemCPUTimes() (idle, kernel, user uint64, err error) {
	var idleTime, kernelTime, userTime winFiletime
	r1, _, e1 := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleTime)),
		uintptr(unsafe.Pointer(&kernelTime)),
		uintptr(unsafe.Pointer(&userTime)),
	)
	if r1 == 0 {
		err = e1
		return
	}
	idle = uint64(idleTime.HighDateTime)<<32 | uint64(idleTime.LowDateTime)
	kernel = uint64(kernelTime.HighDateTime)<<32 | uint64(kernelTime.LowDateTime)
	user = uint64(userTime.HighDateTime)<<32 | uint64(userTime.LowDateTime)
	return
}

func (c *CPUCollector) collectCpuTimeStats(now time.Time) ([]collector.Metric, error) {
	idle, kernel, user, err := getSystemCPUTimes()
	if err != nil {
		return nil, err
	}

	total := kernel + user
	usage := 0.0

	if c.prevKernel != 0 || c.prevUser != 0 {
		prevTotal := c.prevKernel + c.prevUser
		deltaTotal := total - prevTotal
		deltaIdle := idle - c.prevIdle
		if deltaTotal > 0 {
			usage = float64(deltaTotal-deltaIdle) / float64(deltaTotal) * 100
		}
	}

	c.prevIdle = idle
	c.prevKernel = kernel
	c.prevUser = user

	// Windows GetSystemTimes only exposes idle/kernel/user, so the 8-field
	// time breakdown and per-state utilization metrics are unavailable and
	// omitted here (graceful degradation, decision H).
	return []collector.Metric{{
		Component: "cpu",
		Name:      "usage",
		Value:     roundFloat(usage, 2),
		Unit:      "%",
		Labels:    map[string]string{"core": "total"},
		Timestamp: now,
	}}, nil
}

func (c *CPUCollector) collectLoadAverage(now time.Time) ([]collector.Metric, error) {
	return nil, nil
}

func (c *CPUCollector) collectFrequency(now time.Time) ([]collector.Metric, error) {
	out, err := runPowerShell("(Get-CimInstance Win32_Processor | Select-Object -First 1).CurrentClockSpeed")
	if err != nil {
		slog.Debug("cpu: failed to get frequency", "error", err)
		return nil, nil
	}
	freq, err := strconv.ParseFloat(strings.TrimSpace(out), 64)
	if err != nil {
		return nil, nil
	}
	return []collector.Metric{{
		Component: "cpu",
		Name:      "frequency",
		Value:     roundFloat(freq, 0),
		Unit:      "MHz",
		Labels:    map[string]string{"core": "0"},
		Timestamp: now,
	}}, nil
}

func (c *CPUCollector) collectContextSwitches(now time.Time) ([]collector.Metric, error) {
	return nil, nil
}

func (c *CPUCollector) collectProcessCount(now time.Time) ([]collector.Metric, error) {
	out, err := runPowerShell("(Get-Process).Count")
	if err != nil {
		slog.Debug("cpu: failed to get process count", "error", err)
		return nil, nil
	}
	total, err := strconv.ParseFloat(strings.TrimSpace(out), 64)
	if err != nil {
		return nil, nil
	}
	return []collector.Metric{
		{Component: "cpu", Name: "process_count", Value: 0, Unit: "个", Labels: map[string]string{"type": "running"}, Timestamp: now},
		{Component: "cpu", Name: "process_count", Value: total, Unit: "个", Labels: map[string]string{"type": "total"}, Timestamp: now},
	}, nil
}

type winCPUInfo struct {
	Name             string `json:"Name"`
	NumberOfCores    int    `json:"NumberOfCores"`
	NumberOfLogicals int    `json:"NumberOfLogicalProcessors"`
	MaxClockSpeed    int    `json:"MaxClockSpeed"`
}

func (c *CPUCollector) collectModelInfo(now time.Time) ([]collector.Metric, error) {
	out, err := runPowerShell("Get-CimInstance Win32_Processor | Select-Object Name,NumberOfCores,NumberOfLogicalProcessors,MaxClockSpeed | ConvertTo-Json")
	if err != nil {
		slog.Debug("cpu: failed to get model info", "error", err)
		return nil, nil
	}

	var info winCPUInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return nil, nil
	}
	if info.Name == "" {
		return nil, nil
	}

	return []collector.Metric{{
		Component: "cpu",
		Name:      "model_info",
		Value:     float64(info.NumberOfCores),
		Unit:      "cores",
		Labels: map[string]string{
			"model_name":     info.Name,
			"logical_cores":  fmt.Sprintf("%d", info.NumberOfLogicals),
			"max_frequency":  fmt.Sprintf("%d MHz", info.MaxClockSpeed),
		},
		Timestamp: now,
	}}, nil
}

func runPowerShell(script string) (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
