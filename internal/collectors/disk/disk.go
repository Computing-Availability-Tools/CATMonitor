package disk

import (
	"strconv"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
)

type DiskCollector struct {
	prevDiskStats map[string]proc.DiskStat
	prevCPU       proc.CPUStat
	hasPrevCPU    bool
}

func New() *DiskCollector {
	return &DiskCollector{
		prevDiskStats: make(map[string]proc.DiskStat),
	}
}

func (c *DiskCollector) Name() string                 { return "disk" }
func (c *DiskCollector) Component() string            { return "disk" }
func (c *DiskCollector) Priority() collector.Priority { return collector.PriorityHigh }
func (c *DiskCollector) DefaultInterval() time.Duration {
	return 5 * time.Second
}
func (c *DiskCollector) DefaultEnabled() bool { return true }

// parseSmartOutput parses `smartctl -H` output for a device, emitting
// smart_status (PASSED/FAILED) and smart_temperature when a temperature
// attribute line is present. Kept in the shared (non-build-tag) file because
// the parsing logic is platform-agnostic; only the smartctl invocation is
// abstracted into the smartctl source.
func parseSmartOutput(dev, output string, now time.Time) []collector.Metric {
	var metrics []collector.Metric
	lower := strings.ToLower(output)
	status := 0
	if strings.Contains(lower, "passed") {
		status = 1
	}
	statusStr := "FAILED"
	if status == 1 {
		statusStr = "PASSED"
	}
	metrics = append(metrics, collector.Metric{
		Component: "disk", Name: "smart_status", Value: float64(status), Unit: "",
		Labels:    map[string]string{"device": dev, "status": statusStr},
		Timestamp: now,
	})

	for _, line := range strings.Split(output, "\n") {
		l := strings.ToLower(line)
		if !strings.Contains(l, "temperature") {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if strings.Contains(strings.ToLower(f), "temp") && i+1 < len(fields) {
				temp, err := strconv.ParseFloat(fields[i+1], 64)
				if err == nil {
					metrics = append(metrics, collector.Metric{
						Component: "disk", Name: "smart_temperature", Value: temp, Unit: "°C",
						Labels: map[string]string{"device": dev}, Timestamp: now,
					})
					break
				}
			}
		}
	}
	return metrics
}

func withField(labels map[string]string, field string) map[string]string {
	result := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		result[k] = v
	}
	result["field"] = field
	return result
}

func roundFloat(val float64, precision int) float64 {
	multiplier := 1.0
	for i := 0; i < precision; i++ {
		multiplier *= 10
	}
	return float64(int64(val*multiplier+0.5)) / multiplier
}

// cpuStatTotal sums all 10 time fields of a CPUStat (the "total" jiffies),
// used by collectIoWait to compute the iowait share over total delta.
func cpuStatTotal(s proc.CPUStat) uint64 {
	return s.User + s.Nice + s.System + s.Idle + s.Iowait +
		s.Irq + s.Softirq + s.Steal + s.Guest + s.GuestNice
}

func init() {
	collector.DefaultRegistry.Register(New())
}
