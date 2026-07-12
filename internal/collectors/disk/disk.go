package disk

import (
	"strconv"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

type DiskCollector struct {
	procPath        string
	prevDiskStats   map[string]diskStats
	prevCPUTimes    []uint64
	mockDmesg       string
	mockSmartctl    map[string]string
}

type diskStats struct {
	readsCompleted  uint64
	sectorsRead     uint64
	writesCompleted uint64
	sectorsWritten  uint64
}

type MountInfo struct {
	device     string
	mountPoint string
	fstype     string
}

func New() *DiskCollector {
	return &DiskCollector{
		prevDiskStats: make(map[string]diskStats),
		mockSmartctl:  make(map[string]string),
	}
}

func (c *DiskCollector) Name() string                 { return "disk" }
func (c *DiskCollector) Component() string            { return "disk" }
func (c *DiskCollector) Priority() collector.Priority { return collector.PriorityHigh }
func (c *DiskCollector) DefaultInterval() time.Duration {
	return 5 * time.Second
}
func (c *DiskCollector) DefaultEnabled() bool { return true }

func (c *DiskCollector) SetMockDmesg(s string)              { c.mockDmesg = s }
func (c *DiskCollector) SetMockSmartctl(dev, output string) { c.mockSmartctl[dev] = output }

func parseSmartOutput(dev, output string, now time.Time) []collector.Metric {
	var metrics []collector.Metric
	lower := strings.ToLower(output)
	status := 0
	if strings.Contains(lower, "passed") {
		status = 1
	}
	metrics = append(metrics, collector.Metric{
		Component: "disk", Name: "smart_status", Value: float64(status), Unit: "",
		Labels: map[string]string{"device": dev, "status": map[bool]string{true: "PASSED", false: "FAILED"}[status == 1]},
		Timestamp: now,
	})

	for _, line := range strings.Split(output, "\n") {
		l := strings.ToLower(line)
		if strings.Contains(l, "temperature_celsius") || strings.Contains(l, "temperature") {
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
	}
	return metrics
}

func parseU64(s string) uint64 {
	val, _ := strconv.ParseUint(s, 10, 64)
	return val
}

func withField(labels map[string]string, field string) map[string]string {
	result := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		result[k] = v
	}
	result["field"] = field
	return result
}

func sumU64(s []uint64) uint64 {
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

func init() {
	collector.DefaultRegistry.Register(New())
}
