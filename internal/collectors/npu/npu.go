package npu

import (
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// npuDevice holds the (card_id, device_id) pair for one NPU chip.
type npuDevice struct {
	cardID int
	devID  int
}

// NPUCollector collects metrics from Huawei Ascend NPUs via DCMI (CGo) and
// npu-smi/hccn_tool commands. Collection is device-parallel: each NPU's
// metrics are collected in a separate goroutine, so 8-card latency ≈ 1-card.
type NPUCollector struct {
	mu              sync.Mutex
	devices         []npuDevice    // populated at startup from CardList + DeviceNumInCard
	devicesReady    bool
	prevEcc         map[string]uint64 // key "dev:type:kind" → cumulative count for delta
	staticCollected bool              // topo, npu_num, driver_version, chip_type, comm_topo
}

func New() *NPUCollector {
	return &NPUCollector{
		prevEcc: make(map[string]uint64),
	}
}

func (c *NPUCollector) Name() string                 { return "npu" }
func (c *NPUCollector) Component() string            { return "npu" }
func (c *NPUCollector) Priority() collector.Priority { return collector.PriorityHigh }
func (c *NPUCollector) DefaultInterval() time.Duration {
	return 3 * time.Second
}
func (c *NPUCollector) DefaultEnabled() bool { return true }

// Collect runs device-parallel collection. Each device gets its own goroutine;
// single-card failure does not affect others. Static/global metrics (topo,
// device count) are collected once before the parallel phase.
func (c *NPUCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	var allMetrics []collector.Metric

	// Ensure device list is populated.
	c.ensureDevices()

	// Phase 1: global/static metrics (once).
	if !c.staticCollected {
		if m, err := c.collectStatic(now); err == nil {
			allMetrics = append(allMetrics, m...)
		}
		c.staticCollected = true
	}

	// Phase 2: per-device metrics (parallel).
	if len(c.devices) > 0 {
		var wg sync.WaitGroup
		results := make([][]collector.Metric, len(c.devices))
		for i, d := range c.devices {
			wg.Add(1)
			go func(idx int, dev npuDevice) {
				defer wg.Done()
				results[idx] = c.collectDevice(dev, now)
			}(i, d)
		}
		wg.Wait()
		for _, m := range results {
			allMetrics = append(allMetrics, m...)
		}
	}

	return allMetrics, nil
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
