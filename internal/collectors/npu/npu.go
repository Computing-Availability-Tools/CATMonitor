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
	phyID  int
}

// deviceMetricNames lists every per-device metric collectDevice (npu_linux.go)
// can produce. Collect()'s Phase-2 gate uses it: if none is wanted, the whole
// per-device collection is skipped. Keep in sync with collectDevice.
var deviceMetricNames = []string{
	// utilization & memory
	"utilization", "memory_usage", "hbm_total_memory", "hbm_used_memory",
	"npu_util", "aicpu_util", "ctrlcpu_util", "vector_core_util",
	"hbm_bandwidth_util", "ddr_util", "ddr_bandwidth_util",
	"vdec_util", "vpc_util", "venc_util", "jpege_util", "jpegd_util",
	// temperature
	"temperature", "hbm_temp", "cluster_temp", "peri_temp",
	"aicore0_temp", "aicore1_temp",
	"ntc1_temp", "ntc2_temp", "ntc3_temp", "ntc4_temp",
	"soc_max_temp", "fp_max_temp", "ndie_temp", "hbm_max_temp",
	// power, voltage, health
	"power_draw", "voltage", "aicore_voltage", "hybrid_voltage",
	"cpu_voltage", "ddr_voltage", "acg_count",
	"health_status", "driver_health", "error_code", "card_drop",
	// frequency
	"aicore_freq", "aicore_rated_freq", "aicpu_freq", "ctrlcpu_freq",
	"vector_core_freq", "hbm_freq", "ddr_freq",
	// fan, process
	"fan_speed", "process_info", "process_total",
	// ECC (emitEccMetrics: devType ∈ {hbm, ddr})
	"hbm_single_ecc", "hbm_double_ecc",
	"hbm_single_ecc_isolated", "hbm_double_ecc_isolated",
	"ddr_single_ecc", "ddr_double_ecc",
	"ddr_single_ecc_isolated", "ddr_double_ecc_isolated",
	// LLC
	"llc_write_hit_rate", "llc_read_hit_rate", "llc_throughput",
	// RoCE & bandwidth
	"roce_link_status", "roce_speed_status", "roce_link_health",
	"net_tx_bandwidth", "net_rx_bandwidth",
	"pcie_tx_bandwidth", "pcie_rx_bandwidth",
	"hccs_tx_bandwidth", "hccs_rx_bandwidth",
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

	// Phase 1: global/static metrics (once). Skip when no devices to avoid
	// emitting npu_num=0 which would falsely trigger accelerated server type.
	if !c.staticCollected {
		c.staticCollected = true
		if len(c.devices) > 0 && collector.AnyWanted("npu", []string{"npu_num", "driver_version", "chip_type", "comm_topo"}) {
			if m, err := c.collectStatic(now); err == nil {
				allMetrics = append(allMetrics, m...)
			}
		}
	}

	// Phase 2: per-device metrics (parallel).
	if len(c.devices) > 0 && collector.AnyWanted("npu", deviceMetricNames) {
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
