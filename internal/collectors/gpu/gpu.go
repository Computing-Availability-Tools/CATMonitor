package gpu

import (
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

type GPUCollector struct {
	smiPath    string
	available  bool
	mockOutput string
}

func New() *GPUCollector {
	c := &GPUCollector{
		smiPath:   "nvidia-smi",
		available: false,
	}
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		c.available = true
	}
	return c
}

func (c *GPUCollector) Name() string                 { return "gpu" }
func (c *GPUCollector) Component() string            { return "gpu" }
func (c *GPUCollector) Priority() collector.Priority { return collector.PriorityHigh }
func (c *GPUCollector) DefaultInterval() time.Duration {
	return 3 * time.Second
}
func (c *GPUCollector) DefaultEnabled() bool { return true }

func (c *GPUCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	if !c.available {
		return nil, nil
	}

	var output string
	if c.mockOutput != "" {
		output = c.mockOutput
	} else {
		cmd := exec.Command(c.smiPath,
			"--query-gpu=index,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,fan.speed,ecc.errors.uncorrected.volatile.total,clocks.gr",
			"--format=csv,noheader,nounits")
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
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := parseCSVLine(line)
		if len(fields) < 9 {
			continue
		}

		gpuID := fields[0]
		utilization := parseFloat(fields[1])
		memUsed := parseFloat(fields[2])
		memTotal := parseFloat(fields[3])
		temperature := parseFloat(fields[4])
		power := parseFloat(fields[5])
		fan := parseFloat(fields[6])
		ecc := parseFloat(fields[7])
		clock := parseFloat(fields[8])

		metrics = append(metrics, collector.Metric{
			Component: "gpu",
			Name:      "utilization",
			Value:     roundFloat(utilization, 2),
			Unit:      "%",
			Labels:    map[string]string{"gpu_id": gpuID},
			Timestamp: now,
		})

		memUsage := 0.0
		if memTotal > 0 {
			memUsage = memUsed / memTotal * 100
		}
		metrics = append(metrics, collector.Metric{
			Component: "gpu",
			Name:      "memory_usage",
			Value:     roundFloat(memUsage, 2),
			Unit:      "%",
			Labels:    map[string]string{"gpu_id": gpuID},
			Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "gpu",
			Name:      "memory_detail",
			Value:     roundFloat(memUsed, 2),
			Unit:      "MB",
			Labels:    map[string]string{"gpu_id": gpuID, "field": "used"},
			Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "gpu",
			Name:      "memory_detail",
			Value:     roundFloat(memTotal, 2),
			Unit:      "MB",
			Labels:    map[string]string{"gpu_id": gpuID, "field": "total"},
			Timestamp: now,
		})

		metrics = append(metrics, collector.Metric{
			Component: "gpu",
			Name:      "temperature",
			Value:     roundFloat(temperature, 1),
			Unit:      "°C",
			Labels:    map[string]string{"gpu_id": gpuID},
			Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "gpu",
			Name:      "power_draw",
			Value:     roundFloat(power, 2),
			Unit:      "W",
			Labels:    map[string]string{"gpu_id": gpuID},
			Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "gpu",
			Name:      "fan_speed",
			Value:     roundFloat(fan, 2),
			Unit:      "%",
			Labels:    map[string]string{"gpu_id": gpuID},
			Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "gpu",
			Name:      "ecc_errors",
			Value:     roundFloat(ecc, 0),
			Unit:      "次",
			Labels:    map[string]string{"gpu_id": gpuID},
			Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "gpu",
			Name:      "clock_frequency",
			Value:     roundFloat(clock, 0),
			Unit:      "MHz",
			Labels:    map[string]string{"gpu_id": gpuID},
			Timestamp: now,
		})
	}

	return metrics
}

func parseCSVLine(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		result = append(result, strings.TrimSpace(p))
	}
	return result
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

func (c *GPUCollector) SetMockOutput(s string) {
	c.mockOutput = s
}

func (c *GPUCollector) SetAvailable(b bool) {
	c.available = b
}

func init() {
	collector.DefaultRegistry.Register(New())
}
