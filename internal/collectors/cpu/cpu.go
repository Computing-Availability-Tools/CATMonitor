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

// CPUCollector collects CPU metrics from /proc and /sys.
type CPUCollector struct {
	procPath           string
	sysPath            string
	prevStats          map[string][]uint64
	prevContextSwitches uint64
	modelInfoCollected  bool
}

func New() *CPUCollector {
	return &CPUCollector{
		procPath:  "/proc",
		sysPath:   "/sys",
		prevStats: make(map[string][]uint64),
	}
}

func (c *CPUCollector) Name() string                 { return "cpu" }
func (c *CPUCollector) Component() string            { return "cpu" }
func (c *CPUCollector) Priority() collector.Priority { return collector.PriorityHigh }
func (c *CPUCollector) DefaultInterval() time.Duration {
	return 3 * time.Second
}
func (c *CPUCollector) DefaultEnabled() bool { return true }

// Collect gathers all CPU metrics in a single call.
func (c *CPUCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	var metrics []collector.Metric

	// 1. CPU usage (requires two reads for delta calculation)
	usageMetrics, err := c.collectUsage(now)
	if err == nil {
		metrics = append(metrics, usageMetrics...)
	}

	// 2. Load average
	loadMetrics, err := c.collectLoadAverage(now)
	if err == nil {
		metrics = append(metrics, loadMetrics...)
	}

	// 3. Temperature
	tempMetrics, err := c.collectTemperature(now)
	if err == nil {
		metrics = append(metrics, tempMetrics...)
	}

	// 4. Frequency
	freqMetrics, err := c.collectFrequency(now)
	if err == nil {
		metrics = append(metrics, freqMetrics...)
	}

	// 5. Context switches
	ctxMetrics, err := c.collectContextSwitches(now)
	if err == nil {
		metrics = append(metrics, ctxMetrics...)
	}

	// 6. Process count
	procMetrics, err := c.collectProcessCount(now)
	if err == nil {
		metrics = append(metrics, procMetrics...)
	}

	// 7. Model info (collected once)
	if !c.modelInfoCollected {
		modelMetrics, err := c.collectModelInfo(now)
		if err == nil {
			metrics = append(metrics, modelMetrics...)
		}
		c.modelInfoCollected = true
	}

	return metrics, nil
}

func (c *CPUCollector) collectContextSwitches(now time.Time) ([]collector.Metric, error) {
	stats, err := parseCPUStat(c.procPath)
	if err != nil {
		return nil, err
	}

	var metrics []collector.Metric
	for cpuName, times := range stats {
		if cpuName != "cpu" {
			continue
		}
		if c.prevContextSwitches > 0 {
			_ = times // not used for context switches
		}
	}

	f, err := os.Open(filepath.Join(c.procPath, "stat"))
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
					metrics = append(metrics, collector.Metric{
						Component: "cpu",
						Name:      "context_switches",
						Value:     roundFloat(rate, 0),
						Unit:      "次/s",
						Timestamp: now,
					})
				}
			}
			break
		}
	}

	return metrics, nil
}

func (c *CPUCollector) collectProcessCount(now time.Time) ([]collector.Metric, error) {
	data, err := os.ReadFile(filepath.Join(c.procPath, "loadavg"))
	if err != nil {
		return nil, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 4 {
		return nil, nil
	}

	// Format: "running/total"
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
	f, err := os.Open(filepath.Join(c.procPath, "cpuinfo"))
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

// collectTemperature reads /sys/class/thermal/thermal_zone*/temp.
func (c *CPUCollector) collectTemperature(now time.Time) ([]collector.Metric, error) {
	thermalPath := filepath.Join(c.sysPath, "class", "thermal")
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

// collectFrequency reads /sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq.
func (c *CPUCollector) collectFrequency(now time.Time) ([]collector.Metric, error) {
	cpuPath := filepath.Join(c.sysPath, "devices", "system", "cpu")
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

// collectUsage reads /proc/stat and computes CPU usage percentage.
func (c *CPUCollector) collectUsage(now time.Time) ([]collector.Metric, error) {
	current, err := parseCPUStat(c.procPath)
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

	// Store current stats for next calculation.
	c.prevStats = current

	return metrics, nil
}

// collectLoadAverage reads /proc/loadavg.
func (c *CPUCollector) collectLoadAverage(now time.Time) ([]collector.Metric, error) {
	data, err := os.ReadFile(filepath.Join(c.procPath, "loadavg"))
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

// parseCPUStat reads /proc/stat and returns a map of cpu name -> time fields.
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

// calculateUsage computes CPU usage percentage from two snapshots.
// Formula: usage% = (total_delta - idle_delta) / total_delta * 100
func calculateUsage(prev, curr []uint64) float64 {
	if len(prev) < 4 || len(curr) < 4 {
		return 0
	}
	prevTotal := sumSlice(prev)
	currTotal := sumSlice(curr)
	prevIdle := prev[3] // idle is the 4th field
	currIdle := curr[3]

	totalDelta := float64(currTotal - prevTotal)
	idleDelta := float64(currIdle - prevIdle)

	if totalDelta == 0 {
		return 0
	}

	return (totalDelta - idleDelta) / totalDelta * 100
}

func sumSlice(s []uint64) uint64 {
	var total uint64
	for _, v := range s {
		total += v
	}
	return total
}

func roundFloat(val float64, precision int) float64 {
	multiplier := 1.0
	for i := 0; i < precision; i++ {
		multiplier *= 10
	}
	return float64(int64(val*multiplier+0.5)) / multiplier
}

// SetProcPath sets the proc filesystem path (for testing).
func (c *CPUCollector) SetProcPath(path string) {
	c.procPath = path
}

// SetSysPath sets the sys filesystem path (for testing).
func (c *CPUCollector) SetSysPath(path string) {
	c.sysPath = path
}

func init() {
	collector.DefaultRegistry.Register(New())
}
