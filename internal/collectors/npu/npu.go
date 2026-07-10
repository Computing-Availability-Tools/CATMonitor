package npu

import (
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

type NPUCollector struct {
	smiPath    string
	available  bool
	mockOutput string
}

func New() *NPUCollector {
	c := &NPUCollector{
		smiPath:   "npu-smi",
		available: false,
	}
	if _, err := exec.LookPath("npu-smi"); err == nil {
		c.available = true
	}
	return c
}

func (c *NPUCollector) Name() string                 { return "npu" }
func (c *NPUCollector) Component() string            { return "npu" }
func (c *NPUCollector) Priority() collector.Priority { return collector.PriorityHigh }
func (c *NPUCollector) DefaultInterval() time.Duration {
	return 3 * time.Second
}
func (c *NPUCollector) DefaultEnabled() bool { return true }

func (c *NPUCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	if !c.available {
		return nil, nil
	}

	var output string
	if c.mockOutput != "" {
		output = c.mockOutput
	} else {
		cmd := exec.Command(c.smiPath, "info")
		out, err := cmd.Output()
		if err != nil {
			return nil, nil
		}
		output = string(out)
	}

	return parseOutput(output, now), nil
}

func parseOutput(output string, now time.Time) []collector.Metric {
	var metrics []collector.Metric
	lines := strings.Split(output, "\n")

	var dataLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if isNPUDataLine(line) {
			dataLines = append(dataLines, line)
		}
	}

	for i := 0; i+1 < len(dataLines); i += 2 {
		metrics = append(metrics, parseNPU(dataLines[i], dataLines[i+1], now)...)
	}

	return metrics
}

func parseNPU(line1, line2 string, now time.Time) []collector.Metric {
	seg1 := splitPipeFields(line1)
	if len(seg1) < 3 {
		return nil
	}

	npuFields := strings.Fields(seg1[0])
	if len(npuFields) < 1 {
		return nil
	}
	npuID := npuFields[0]
	healthStr := seg1[1]

	powerTempFields := strings.Fields(seg1[2])
	var power, temp float64
	if len(powerTempFields) >= 1 {
		power = parseFloat(powerTempFields[0])
	}
	if len(powerTempFields) >= 2 {
		temp = parseFloat(powerTempFields[1])
	}

	var aicore, memUsed, memTotal float64
	seg2 := splitPipeFields(line2)
	if len(seg2) >= 3 {
		memFields := strings.Fields(seg2[2])
		if len(memFields) >= 1 {
			aicore = parseFloat(memFields[0])
		}
		memUsed, memTotal = parseMemoryUsage(seg2[2])
	}

	healthVal := healthMap[healthStr]

	var metrics []collector.Metric

	metrics = append(metrics, collector.Metric{
		Component: "npu",
		Name:      "utilization",
		Value:     roundFloat(aicore, 2),
		Unit:      "%",
		Labels:    map[string]string{"npu_id": npuID},
		Timestamp: now,
	})

	memUsage := 0.0
	if memTotal > 0 {
		memUsage = memUsed / memTotal * 100
	}
	metrics = append(metrics, collector.Metric{
		Component: "npu",
		Name:      "memory_usage",
		Value:     roundFloat(memUsage, 2),
		Unit:      "%",
		Labels:    map[string]string{"npu_id": npuID},
		Timestamp: now,
	})
	metrics = append(metrics, collector.Metric{
		Component: "npu",
		Name:      "memory_detail",
		Value:     roundFloat(memUsed, 2),
		Unit:      "MB",
		Labels:    map[string]string{"npu_id": npuID, "field": "used"},
		Timestamp: now,
	})
	metrics = append(metrics, collector.Metric{
		Component: "npu",
		Name:      "memory_detail",
		Value:     roundFloat(memTotal, 2),
		Unit:      "MB",
		Labels:    map[string]string{"npu_id": npuID, "field": "total"},
		Timestamp: now,
	})

	metrics = append(metrics, collector.Metric{
		Component: "npu",
		Name:      "temperature",
		Value:     roundFloat(temp, 1),
		Unit:      "°C",
		Labels:    map[string]string{"npu_id": npuID},
		Timestamp: now,
	})
	metrics = append(metrics, collector.Metric{
		Component: "npu",
		Name:      "power_draw",
		Value:     roundFloat(power, 2),
		Unit:      "W",
		Labels:    map[string]string{"npu_id": npuID},
		Timestamp: now,
	})
	metrics = append(metrics, collector.Metric{
		Component: "npu",
		Name:      "health_status",
		Value:     healthVal,
		Unit:      "",
		Labels:    map[string]string{"npu_id": npuID, "status": healthStr},
		Timestamp: now,
	})

	return metrics
}

func isNPUDataLine(line string) bool {
	if !strings.HasPrefix(line, "| ") {
		return false
	}
	if len(line) < 3 {
		return false
	}
	c := line[2]
	return c >= '0' && c <= '9'
}

func splitPipeFields(line string) []string {
	parts := strings.Split(line, "|")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func parseMemoryUsage(s string) (used, total float64) {
	fields := strings.Fields(s)
	for i := 0; i < len(fields); i++ {
		if fields[i] == "/" {
			if i > 0 {
				used = parseFloat(fields[i-1])
			}
			if i+1 < len(fields) {
				total = parseFloat(fields[i+1])
			}
			break
		}
	}
	return
}

func parseFloat(s string) float64 {
	val, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return val
}

func roundFloat(val float64, precision int) float64 {
	multiplier := 1.0
	for i := 0; i < precision; i++ {
		multiplier *= 10
	}
	return float64(int64(val*multiplier+0.5)) / multiplier
}

var healthMap = map[string]float64{
	"OK":       1,
	"Warning":  2,
	"Alarm":    3,
	"Critical": 4,
}

func (c *NPUCollector) SetMockOutput(s string) {
	c.mockOutput = s
}

func (c *NPUCollector) SetAvailable(b bool) {
	c.available = b
}

func init() {
	collector.DefaultRegistry.Register(New())
}
