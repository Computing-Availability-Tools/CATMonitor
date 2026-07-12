//go:build linux

package memory

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

var linuxMemPaths = struct {
	procPath string
	sysPath  string
}{procPath: "/proc", sysPath: "/sys"}

func (c *MemoryCollector) SetProcPath(path string) { linuxMemPaths.procPath = path }
func (c *MemoryCollector) SetSysPath(path string)  { linuxMemPaths.sysPath = path }
func (c *MemoryCollector) SetMockDmesg(s string)   { c.mockDmesg = s }

func (c *MemoryCollector) collectUsage(now time.Time) ([]collector.Metric, error) {
	meminfo, err := parseMeminfo(linuxMemPaths.procPath)
	if err != nil {
		return nil, err
	}

	var metrics []collector.Metric

	memTotal, ok1 := meminfo["MemTotal"]
	memAvail, ok2 := meminfo["MemAvailable"]
	if !ok1 || !ok2 {
		return nil, nil
	}

	usage := 0.0
	if memTotal > 0 {
		usage = float64(memTotal-memAvail) / float64(memTotal) * 100
	}

	metrics = append(metrics, collector.Metric{
		Component: "memory",
		Name:      "usage",
		Value:     roundFloat(usage, 2),
		Unit:      "%",
		Timestamp: now,
	})

	metrics = append(metrics, collector.Metric{
		Component: "memory",
		Name:      "usage_detail",
		Value:     float64(memTotal) / 1024,
		Unit:      "MB",
		Labels:    map[string]string{"field": "total"},
		Timestamp: now,
	})
	metrics = append(metrics, collector.Metric{
		Component: "memory",
		Name:      "usage_detail",
		Value:     float64(memTotal-memAvail) / 1024,
		Unit:      "MB",
		Labels:    map[string]string{"field": "used"},
		Timestamp: now,
	})
	metrics = append(metrics, collector.Metric{
		Component: "memory",
		Name:      "usage_detail",
		Value:     float64(memAvail) / 1024,
		Unit:      "MB",
		Labels:    map[string]string{"field": "available"},
		Timestamp: now,
	})

	return metrics, nil
}

func (c *MemoryCollector) collectSwapUsage(now time.Time) ([]collector.Metric, error) {
	meminfo, err := parseMeminfo(linuxMemPaths.procPath)
	if err != nil {
		return nil, err
	}

	swapTotal, ok1 := meminfo["SwapTotal"]
	swapFree, ok2 := meminfo["SwapFree"]
	if !ok1 || !ok2 {
		return nil, nil
	}

	usage := 0.0
	if swapTotal > 0 {
		usage = float64(swapTotal-swapFree) / float64(swapTotal) * 100
	}

	return []collector.Metric{{
		Component: "memory",
		Name:      "swap_usage",
		Value:     roundFloat(usage, 2),
		Unit:      "%",
		Timestamp: now,
	}}, nil
}

func (c *MemoryCollector) collectECCErrors(filename, metricName string, now time.Time) ([]collector.Metric, error) {
	edacPath := filepath.Join(linuxMemPaths.sysPath, "devices", "system", "edac", "mc")

	entries, err := os.ReadDir(edacPath)
	if err != nil {
		return nil, nil
	}

	var metrics []collector.Metric
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "mc") {
			continue
		}
		countPath := filepath.Join(edacPath, entry.Name(), filename)
		data, err := os.ReadFile(countPath)
		if err != nil {
			continue
		}
		val := strings.TrimSpace(string(data))
		count, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			continue
		}
		metrics = append(metrics, collector.Metric{
			Component: "memory",
			Name:      metricName,
			Value:     float64(count),
			Unit:      "次",
			Labels:    map[string]string{"mc": entry.Name()},
			Timestamp: now,
		})
	}

	return metrics, nil
}

func parseMeminfo(procPath string) (map[string]uint64, error) {
	data, err := os.ReadFile(filepath.Join(procPath, "meminfo"))
	if err != nil {
		return nil, err
	}

	result := make(map[string]uint64)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.TrimSpace(parts[1])
		valStr = strings.TrimSuffix(valStr, "kB")
		valStr = strings.TrimSpace(valStr)
		val, err := strconv.ParseUint(valStr, 10, 64)
		if err != nil {
			continue
		}
		result[key] = val
	}

	return result, nil
}

func (c *MemoryCollector) collectPageFaults(now time.Time) ([]collector.Metric, error) {
	data, err := os.ReadFile(filepath.Join(linuxMemPaths.procPath, "vmstat"))
	if err != nil {
		return nil, err
	}

	var pgfault, pgmajfault uint64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "pgfault":
			pgfault, _ = strconv.ParseUint(fields[1], 10, 64)
		case "pgmajfault":
			pgmajfault, _ = strconv.ParseUint(fields[1], 10, 64)
		}
	}

	var metrics []collector.Metric
	if c.prevPageFaults > 0 {
		minorRate := float64(pgfault-c.prevPageFaults) / 3.0
		metrics = append(metrics, collector.Metric{
			Component: "memory", Name: "page_faults", Value: roundFloat(minorRate, 0), Unit: "次/s",
			Labels: map[string]string{"type": "minor"}, Timestamp: now,
		})
	}
	if c.prevMajorFaults > 0 {
		majorRate := float64(pgmajfault-c.prevMajorFaults) / 3.0
		metrics = append(metrics, collector.Metric{
			Component: "memory", Name: "page_faults", Value: roundFloat(majorRate, 0), Unit: "次/s",
			Labels: map[string]string{"type": "major"}, Timestamp: now,
		})
	}
	c.prevPageFaults = pgfault
	c.prevMajorFaults = pgmajfault
	return metrics, nil
}

func (c *MemoryCollector) collectOOMCount(now time.Time) ([]collector.Metric, error) {
	var output string

	if c.mockDmesg != "" {
		output = c.mockDmesg
	} else {
		cmd := exec.Command("dmesg")
		out, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		output = string(out)
	}

	count := 0
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		l := strings.ToLower(line)
		if strings.Contains(l, "out of memory") || strings.Contains(l, "killed process") {
			count++
		}
	}

	return []collector.Metric{{
		Component: "memory",
		Name:      "oom_count",
		Value:     float64(count),
		Unit:      "次",
		Timestamp: now,
	}}, nil
}
