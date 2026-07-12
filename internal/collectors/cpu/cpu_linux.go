//go:build linux

package cpu

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

type cpuLinux struct {
	procPath string
	sysPath  string
}

var linuxPaths = cpuLinux{procPath: "/proc", sysPath: "/sys"}

func (c *CPUCollector) collectUsage(now time.Time) ([]collector.Metric, error) {
	current, err := parseCPUStat(linuxPaths.procPath)
	if err != nil {
		return nil, err
	}

	var metrics []collector.Metric

	for cpuName, times := range current {
		usage := 0.0
		if prev, ok := c.prevStats[cpuName]; ok {
			usage = calculateUsage(prev, times)
		}

		labels := map[string]string{"core": cpuName}
		if cpuName == "cpu" {
			labels["core"] = "total"
		}

		metrics = append(metrics, collector.Metric{
			Component: "cpu",
			Name:      "usage",
			Value:     roundFloat(usage, 2),
			Unit:      "%",
			Labels:    labels,
			Timestamp: now,
		})
	}

	c.prevStats = current
	return metrics, nil
}

func (c *CPUCollector) collectLoadAverage(now time.Time) ([]collector.Metric, error) {
	data, err := os.ReadFile(filepath.Join(linuxPaths.procPath, "loadavg"))
	if err != nil {
		return nil, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid loadavg format")
	}

	var metrics []collector.Metric
	intervals := []string{"1m", "5m", "15m"}

	for i, interval := range intervals {
		val, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			continue
		}
		metrics = append(metrics, collector.Metric{
			Component: "cpu",
			Name:      "load_average",
			Value:     roundFloat(val, 2),
			Unit:      "",
			Labels:    map[string]string{"interval": interval},
			Timestamp: now,
		})
	}

	return metrics, nil
}

func (c *CPUCollector) collectTemperature(now time.Time) ([]collector.Metric, error) {
	thermalPath := filepath.Join(linuxPaths.sysPath, "class", "thermal")
	entries, err := os.ReadDir(thermalPath)
	if err != nil {
		return nil, err
	}

	var metrics []collector.Metric
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "thermal_zone") {
			continue
		}
		tempPath := filepath.Join(thermalPath, entry.Name(), "temp")
		data, err := os.ReadFile(tempPath)
		if err != nil {
			continue
		}
		val := strings.TrimSpace(string(data))
		tempMilli, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			continue
		}
		metrics = append(metrics, collector.Metric{
			Component: "cpu",
			Name:      "temperature",
			Value:     roundFloat(float64(tempMilli)/1000.0, 1),
			Unit:      "°C",
			Labels:    map[string]string{"zone": entry.Name()},
			Timestamp: now,
		})
	}

	return metrics, nil
}

func (c *CPUCollector) collectFrequency(now time.Time) ([]collector.Metric, error) {
	cpuPath := filepath.Join(linuxPaths.sysPath, "devices", "system", "cpu")
	entries, err := os.ReadDir(cpuPath)
	if err != nil {
		return nil, err
	}

	var metrics []collector.Metric
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "cpu") {
			continue
		}
		if entry.Name() == "cpuidle" || entry.Name() == "cpufreq" {
			continue
		}
		freqPath := filepath.Join(cpuPath, entry.Name(), "cpufreq", "scaling_cur_freq")
		data, err := os.ReadFile(freqPath)
		if err != nil {
			continue
		}
		val := strings.TrimSpace(string(data))
		freqKHz, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			continue
		}
		coreName := strings.TrimPrefix(entry.Name(), "cpu")
		metrics = append(metrics, collector.Metric{
			Component: "cpu",
			Name:      "frequency",
			Value:     roundFloat(float64(freqKHz)/1000.0, 0),
			Unit:      "MHz",
			Labels:    map[string]string{"core": coreName},
			Timestamp: now,
		})
	}

	return metrics, nil
}

func (c *CPUCollector) collectContextSwitches(now time.Time) ([]collector.Metric, error) {
	f, err := os.Open(filepath.Join(linuxPaths.procPath, "stat"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ctxt ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				val, err := strconv.ParseUint(fields[1], 10, 64)
				if err == nil {
					rate := 0.0
					if c.prevContextSwitches > 0 {
						rate = float64(val-c.prevContextSwitches) / 3.0
					}
					c.prevContextSwitches = val
					return []collector.Metric{{
						Component: "cpu",
						Name:      "context_switches",
						Value:     roundFloat(rate, 0),
						Unit:      "次/s",
						Timestamp: now,
					}}, nil
				}
			}
			break
		}
	}

	return nil, nil
}

func (c *CPUCollector) collectProcessCount(now time.Time) ([]collector.Metric, error) {
	data, err := os.ReadFile(filepath.Join(linuxPaths.procPath, "loadavg"))
	if err != nil {
		return nil, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 4 {
		return nil, nil
	}

	parts := strings.Split(fields[3], "/")
	if len(parts) < 2 {
		return nil, nil
	}

	running, _ := strconv.ParseFloat(parts[0], 64)
	total, _ := strconv.ParseFloat(parts[1], 64)

	return []collector.Metric{
		{Component: "cpu", Name: "process_count", Value: running, Unit: "个", Labels: map[string]string{"type": "running"}, Timestamp: now},
		{Component: "cpu", Name: "process_count", Value: total, Unit: "个", Labels: map[string]string{"type": "total"}, Timestamp: now},
	}, nil
}

func (c *CPUCollector) collectModelInfo(now time.Time) ([]collector.Metric, error) {
	f, err := os.Open(filepath.Join(linuxPaths.procPath, "cpuinfo"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var modelName string
	var coreCount int
	var cacheSize string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) >= 2 {
				modelName = strings.TrimSpace(parts[1])
			}
		}
		if strings.HasPrefix(line, "cpu cores") && coreCount == 0 {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) >= 2 {
				val, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				coreCount = val
			}
		}
		if strings.HasPrefix(line, "cache size") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) >= 2 {
				cacheSize = strings.TrimSpace(parts[1])
			}
		}
	}

	if modelName == "" {
		return nil, nil
	}

	return []collector.Metric{
		{Component: "cpu", Name: "model_info", Value: float64(coreCount), Unit: "cores", Labels: map[string]string{"model_name": modelName, "cache_size": cacheSize}, Timestamp: now},
	}, nil
}

func parseCPUStat(procPath string) (map[string][]uint64, error) {
	f, err := os.Open(filepath.Join(procPath, "stat"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string][]uint64)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		cpuName := fields[0]
		times := make([]uint64, 0, 10)
		for _, f := range fields[1:] {
			val, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				break
			}
			times = append(times, val)
		}
		if len(times) > 0 {
			result[cpuName] = times
		}
	}

	return result, scanner.Err()
}

func (c *CPUCollector) SetProcPath(path string) {
	linuxPaths.procPath = path
	c.procPath = path
}

func (c *CPUCollector) SetSysPath(path string) {
	linuxPaths.sysPath = path
	c.sysPath = path
}
