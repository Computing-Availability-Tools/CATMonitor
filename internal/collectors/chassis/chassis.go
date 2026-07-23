// Package chassis provides a collector for server-level environmental metrics
// (system power, inlet/outlet temperature, chassis fan speed) sourced from
// ipmitool SDR. It shares the 30s SDR cache with the cpu and memory collectors.
//
// This is the first collector that is NOT tied to a specific hardware component
// (CPU/Memory/Disk/NPU/GPU/Network) — it covers chassis-level environmental
// sensors (BMC-managed fans, air temperature, total system power).
package chassis

import (
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/ipmi"
)

type ChassisCollector struct{}

func New() *ChassisCollector { return &ChassisCollector{} }

func (c *ChassisCollector) Name() string                 { return "chassis" }
func (c *ChassisCollector) Component() string            { return "chassis" }
func (c *ChassisCollector) Priority() collector.Priority { return collector.PriorityHigh }
func (c *ChassisCollector) DefaultInterval() time.Duration {
	return 3 * time.Second
}
func (c *ChassisCollector) DefaultEnabled() bool { return true }

func (c *ChassisCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	sensors, err := ipmi.Default().SDR()
	if err != nil {
		return nil, err
	}

	var metrics []collector.Metric
	for _, s := range sensors {
		name := strings.ToLower(s.Name)
		switch {
		// inlet_temp
		case strings.Contains(name, "inlet") && strings.Contains(name, "temp"):
			metrics = append(metrics, collector.Metric{
				Component: "chassis", Name: "inlet_temp", Value: roundFloat(s.Value, 1), Unit: "°C",
				Timestamp: now,
			})
		// outlet_temp
		case strings.Contains(name, "outlet") && strings.Contains(name, "temp"):
			metrics = append(metrics, collector.Metric{
				Component: "chassis", Name: "outlet_temp", Value: roundFloat(s.Value, 1), Unit: "°C",
				Timestamp: now,
			})
		// fan_power (FAN* Power) — must check before power to avoid false match
		case strings.Contains(name, "fan") && strings.Contains(name, "power"):
			fanNum, _ := parseFanName(s.Name)
			metrics = append(metrics, collector.Metric{
				Component: "chassis", Name: "fan_power", Value: roundFloat(s.Value, 2), Unit: "W",
				Labels:    map[string]string{"fan": fanNum},
				Timestamp: now,
			})
		// power (exact match "Power" — total system power, not PSU outputs like "Power1")
		case name == "power":
			metrics = append(metrics, collector.Metric{
				Component: "chassis", Name: "power", Value: roundFloat(s.Value, 2), Unit: "W",
				Timestamp: now,
			})
		// fan_speed (FAN* F/R Speed)
		case strings.Contains(name, "fan") && strings.Contains(name, "speed"):
			fanNum, direction := parseFanName(s.Name)
			metrics = append(metrics, collector.Metric{
				Component: "chassis", Name: "fan_speed", Value: roundFloat(s.Value, 0), Unit: "RPM",
				Labels:    map[string]string{"fan": fanNum, "direction": direction},
				Timestamp: now,
			})
		}
	}

	return metrics, nil
}

// parseFanName extracts the fan number and direction from SDR names like
// "FAN1 F Speed" → ("1", "F"), "FAN3 R Speed" → ("3", "R").
func parseFanName(name string) (fanNum, direction string) {
	fields := strings.Fields(name)
	for _, f := range fields {
		fl := strings.ToLower(f)
		if strings.HasPrefix(fl, "fan") && len(fl) > 3 {
			fanNum = fl[3:]
		}
		if f == "F" || f == "R" {
			direction = f
		}
	}
	if fanNum == "" {
		fanNum = "0"
	}
	if direction == "" {
		direction = "F"
	}
	return
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
