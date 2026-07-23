//go:build linux

package npu

import (
	"strconv"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/dcmi"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/hccn_tool"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/npu_smi"
)

func joinStrings(parts []string, sep string) string {
	return strings.Join(parts, sep)
}

// DCMI constants (from dcmi_interface_api.h). Values verified from the enum
// definitions in the header file; unit semantics (mV vs V, etc.) marked
// 待实测 per Q4 decision.
const (
	// dcmi_freq_type
	freqDDR             = 1
	freqCTRLCPU         = 2
	freqHBM             = 6
	freqAICoreCurrent   = 7
	freqAICoreMax       = 9
	freqVectorCoreCurrent = 12

	// dcmi_utilization_rate
	rateDDR          uint = 1
	rateAICore       uint = 2
	rateAICPU        uint = 3
	rateCTRLCPU      uint = 4
	rateDDRBandwidth  uint = 5
	rateHBM          uint = 6
	rateHBMBandwidth  uint = 10
	rateVectorCore   uint = 12
	rateNPU          uint = 13

	// dcmi_device_type (for ECC)
	devTypeDDR = 0
	devTypeHBM = 2

	// dcmi_manager_sensor_id
	sensorCluster  = 0
	sensorPeri     = 1
	sensorAICore0  = 2
	sensorAICore1  = 3
	sensorNTC      = 10
	sensorSOC      = 11
	sensorFP       = 12
	sensorNDie     = 13
	sensorHBM      = 14

	// dcmi_main_cmd
	mainCmdDVPP = 0
	mainCmdLP   = 8 // DCMI_MAIN_CMD_LP (enum position 8, after DVPP=0,ISP=1,TS_GROUP=2,CAN=3,UART=4,UPGRADE=5,UFS=6,OS_POWER=7,LP=8)

	// dcmi_lp_sub_cmd
	lpSubAICoreVoltage = 0
	lpSubHybridVoltage = 1
	lpSubCpuVoltage   = 2
	lpSubDdrVoltage   = 3
	lpSubACG           = 4
)

// ensureDevices populates deviceIDs from DCMI CardList at first call.
func (c *NPUCollector) ensureDevices() {
	if c.devicesReady {
		return
	}
	c.devicesReady = true
	src := dcmi.Default()
	if !src.Available() {
		return
	}
	_, cards, err := src.CardList()
	if err != nil {
		return
	}
	c.deviceIDs = cards
}

// collectStatic collects global/static metrics once: npu_num, comm_topo,
// driver_version, chip_type (per device).
func (c *NPUCollector) collectStatic(now time.Time) ([]collector.Metric, error) {
	var metrics []collector.Metric

	// npu_num
	metrics = append(metrics, collector.Metric{
		Component: "npu", Name: "npu_num", Value: float64(len(c.deviceIDs)), Unit: "个",
		Timestamp: now,
	})

	// comm_topo (npu-smi info -t topo)
	if topo, err := npu_smi.Default().Topo(); err == nil && topo != "" {
		metrics = append(metrics, collector.Metric{
			Component: "npu", Name: "comm_topo", Value: 0, Unit: "",
			Labels: map[string]string{"topo": topo}, Timestamp: now,
		})
	}

	// driver_version (global)
	src := dcmi.Default()
	if src.Available() {
		if ver, err := src.DriverVersion(); err == nil && ver != "" {
			metrics = append(metrics, collector.Metric{
				Component: "npu", Name: "driver_version", Value: 0, Unit: "",
				Labels: map[string]string{"driver_version": ver}, Timestamp: now,
			})
		}
	}

	// chip_type per device (static)
	for _, dev := range c.deviceIDs {
		if chip, err := src.ChipInfo(0, dev); err == nil && chip != nil {
			metrics = append(metrics, collector.Metric{
				Component: "npu", Name: "chip_type", Value: 0, Unit: "",
				Labels: map[string]string{"npu_id": strconv.Itoa(dev), "chip_type": chip.ChipType},
				Timestamp: now,
			})
		}
	}

	return metrics, nil
}

// collectDevice collects all per-device metrics for one NPU. Called in a
// separate goroutine per device (device-parallel). Errors per metric are
// silently skipped (graceful degradation).
func (c *NPUCollector) collectDevice(devID int, now time.Time) []collector.Metric {
	var metrics []collector.Metric
	card := 0 // single-card assumption; multi-card needs card_id per device
	label := map[string]string{"npu_id": strconv.Itoa(devID)}
	src := dcmi.Default()

	// --- 5.1 utilization (AICore %) ---
	if src.Available() {
		if v, err := src.UtilizationRate(card, devID, rateAICore); err == nil {
			metrics = append(metrics, collector.Metric{
				Component: "npu", Name: "utilization", Value: float64(v), Unit: "%", Labels: label, Timestamp: now,
			})
		}
	}

	// --- 5.2 memory_usage (HBM %) ---
	if src.Available() {
		if hbm, err := src.HbmInfo(card, devID); err == nil && hbm != nil {
			usage := 0.0
			if hbm.MemorySize > 0 {
				usage = float64(hbm.MemoryUsage) / float64(hbm.MemorySize) * 100
			}
			metrics = append(metrics, collector.Metric{
				Component: "npu", Name: "memory_usage", Value: roundFloat(usage, 2), Unit: "%", Labels: label, Timestamp: now,
			})
			// 5.53 hbm_total_memory, 5.54 hbm_used_memory
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "hbm_total_memory", Value: float64(hbm.MemorySize), Unit: "MB", Labels: label, Timestamp: now})
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "hbm_used_memory", Value: float64(hbm.MemoryUsage), Unit: "MB", Labels: label, Timestamp: now})
			// 5.21 hbm_temp
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "hbm_temp", Value: float64(hbm.Temp), Unit: "°C", Labels: label, Timestamp: now})
		}
	}

	// --- 5.3 temperature ---
	if src.Available() {
		if v, err := src.Temperature(card, devID); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "temperature", Value: float64(v), Unit: "°C", Labels: label, Timestamp: now})
		}
	}

	// --- 5.4 power_draw ---
	if src.Available() {
		if v, err := src.Power(card, devID); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "power_draw", Value: float64(v), Unit: "W", Labels: label, Timestamp: now})
		}
	}

	// --- 5.5 health_status ---
	if src.Available() {
		if v, err := src.Health(card, devID); err == nil {
			statusStr := "OK"
			switch v {
			case 1:
				statusStr = "Warning"
			case 2:
				statusStr = "Alarm"
			case 3:
				statusStr = "Critical"
			}
			metrics = append(metrics, collector.Metric{
				Component: "npu", Name: "health_status", Value: float64(v + 1), Unit: "",
				Labels: map[string]string{"npu_id": strconv.Itoa(devID), "status": statusStr}, Timestamp: now,
			})
		}
	}

	// --- 5.9 driver_health ---
	if src.Available() {
		if v, err := src.DriverHealth(); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "driver_health", Value: float64(v), Unit: "", Labels: label, Timestamp: now})
		}
	}

	// --- 5.10 error_code ---
	if src.Available() {
		if v, err := src.ErrorCodeV2(card, devID); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "error_code", Value: float64(v), Unit: "", Labels: label, Timestamp: now})
		}
	}

	// --- 5.14 voltage ---
	if src.Available() {
		if v, err := src.Voltage(card, devID); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "voltage", Value: float64(v), Unit: "V", Labels: label, Timestamp: now})
		}
	}

	// --- 5.22 cluster_temp (representative sensor) ---
	if src.Available() {
		if v, err := src.SensorInfo(card, devID, sensorCluster); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "cluster_temp", Value: float64(v), Unit: "°C", Labels: label, Timestamp: now})
		}
	}

	// --- 5.23 peri_temp ---
	if src.Available() {
		if v, err := src.SensorInfo(card, devID, sensorPeri); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "peri_temp", Value: float64(v), Unit: "°C", Labels: label, Timestamp: now})
		}
	}

	// --- 5.24 aicore0_temp ---
	if src.Available() {
		if v, err := src.SensorInfo(card, devID, sensorAICore0); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "aicore0_temp", Value: float64(v), Unit: "°C", Labels: map[string]string{"npu_id": label["npu_id"], "aicore": "0"}, Timestamp: now})
		}
	}

	// --- 5.25 aicore1_temp ---
	if src.Available() {
		if v, err := src.SensorInfo(card, devID, sensorAICore1); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "aicore1_temp", Value: float64(v), Unit: "°C", Labels: map[string]string{"npu_id": label["npu_id"], "aicore": "1"}, Timestamp: now})
		}
	}

	// --- 5.26-5.29 ntc1-4_temp ---
	if src.Available() {
		if ntc, err := src.SensorNTC(card, devID); err == nil {
			for i, v := range ntc {
				metrics = append(metrics, collector.Metric{
					Component: "npu", Name: "ntc" + strconv.Itoa(i+1) + "_temp", Value: float64(v), Unit: "°C",
					Labels: map[string]string{"npu_id": label["npu_id"], "ntc": strconv.Itoa(i + 1)}, Timestamp: now,
				})
			}
		}
	}

	// --- 5.30 soc_max_temp ---
	if src.Available() {
		if v, err := src.SensorInfo(card, devID, sensorSOC); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "soc_max_temp", Value: float64(v), Unit: "°C", Labels: label, Timestamp: now})
		}
	}

	// --- 5.31 fp_max_temp ---
	if src.Available() {
		if v, err := src.SensorInfo(card, devID, sensorFP); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "fp_max_temp", Value: float64(v), Unit: "°C", Labels: label, Timestamp: now})
		}
	}

	// --- 5.32 ndie_temp ---
	if src.Available() {
		if v, err := src.SensorInfo(card, devID, sensorNDie); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "ndie_temp", Value: float64(v), Unit: "°C", Labels: label, Timestamp: now})
		}
	}

	// --- 5.33 hbm_max_temp ---
	if src.Available() {
		if v, err := src.SensorInfo(card, devID, sensorHBM); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "hbm_max_temp", Value: float64(v), Unit: "°C", Labels: label, Timestamp: now})
		}
	}

	// --- 5.15-5.18 aicore/hybrid/cpu/ddr voltage ---
	if src.Available() {
		if v, err := src.DeviceInfo(card, devID, mainCmdLP, lpSubAICoreVoltage); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "aicore_voltage", Value: float64(v), Unit: "V", Labels: label, Timestamp: now})
		}
		if v, err := src.DeviceInfo(card, devID, mainCmdLP, lpSubHybridVoltage); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "hybrid_voltage", Value: float64(v), Unit: "V", Labels: label, Timestamp: now})
		}
		if v, err := src.DeviceInfo(card, devID, mainCmdLP, lpSubCpuVoltage); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "cpu_voltage", Value: float64(v), Unit: "V", Labels: label, Timestamp: now})
		}
		if v, err := src.DeviceInfo(card, devID, mainCmdLP, lpSubDdrVoltage); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "ddr_voltage", Value: float64(v), Unit: "V", Labels: label, Timestamp: now})
		}
	}

	// --- 5.19 acg_count ---
	if src.Available() {
		if v, err := src.DeviceInfo(card, devID, mainCmdLP, lpSubACG); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "acg_count", Value: float64(v), Unit: "次", Labels: label, Timestamp: now})
		}
	}

	// --- 5.20 fan_speed ---
	if src.Available() {
		if count, err := src.FanCount(card, devID); err == nil {
			for i := 0; i < count; i++ {
				if speed, err := src.FanSpeed(card, devID, i); err == nil {
					metrics = append(metrics, collector.Metric{
						Component: "npu", Name: "fan_speed", Value: float64(speed), Unit: "%",
						Labels: map[string]string{"npu_id": label["npu_id"], "fan": strconv.Itoa(i)}, Timestamp: now,
					})
				}
			}
		}
	}

	// --- 5.34 aicpu_freq ---
	if src.Available() {
		if ai, err := src.AicpuInfo(card, devID); err == nil && ai != nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "aicpu_freq", Value: float64(ai.CurFreq), Unit: "MHz", Labels: label, Timestamp: now})
		}
	}

	// --- 5.35 aicore_rated_freq ---
	if src.Available() {
		if v, err := src.Frequency(card, devID, freqAICoreMax); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "aicore_rated_freq", Value: float64(v), Unit: "MHz", Labels: label, Timestamp: now})
		}
	}

	// --- 5.37 ctrlcpu_freq ---
	if src.Available() {
		if v, err := src.Frequency(card, devID, freqCTRLCPU); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "ctrlcpu_freq", Value: float64(v), Unit: "MHz", Labels: label, Timestamp: now})
		}
	}

	// --- 5.38 vector_core_freq ---
	if src.Available() {
		if v, err := src.Frequency(card, devID, freqVectorCoreCurrent); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "vector_core_freq", Value: float64(v), Unit: "MHz", Labels: label, Timestamp: now})
		}
	}

	// --- 5.39 hbm_freq ---
	if src.Available() {
		if v, err := src.Frequency(card, devID, freqHBM); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "hbm_freq", Value: float64(v), Unit: "MHz", Labels: label, Timestamp: now})
		}
	}

	// --- 5.40 ddr_freq ---
	if src.Available() {
		if v, err := src.Frequency(card, devID, freqDDR); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "ddr_freq", Value: float64(v), Unit: "MHz", Labels: label, Timestamp: now})
		}
	}

	// --- 5.42-5.47 utilization rates ---
	if src.Available() {
		if v, err := src.UtilizationRate(card, devID, rateAICPU); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "aicpu_util", Value: float64(v), Unit: "%", Labels: label, Timestamp: now})
		}
		if v, err := src.UtilizationRate(card, devID, rateCTRLCPU); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "ctrlcpu_util", Value: float64(v), Unit: "%", Labels: label, Timestamp: now})
		}
		if v, err := src.UtilizationRate(card, devID, rateVectorCore); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "vector_core_util", Value: float64(v), Unit: "%", Labels: label, Timestamp: now})
		}
		if v, err := src.UtilizationRate(card, devID, rateHBMBandwidth); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "hbm_bandwidth_util", Value: float64(v), Unit: "%", Labels: label, Timestamp: now})
		}
		if v, err := src.UtilizationRate(card, devID, rateDDR); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "ddr_util", Value: float64(v), Unit: "%", Labels: label, Timestamp: now})
		}
		if v, err := src.UtilizationRate(card, devID, rateDDRBandwidth); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "ddr_bandwidth_util", Value: float64(v), Unit: "%", Labels: label, Timestamp: now})
		}
	}

	// --- 5.48-5.52 DVPP utilization ---
	if src.Available() {
		if dvpp, err := src.DvppRatio(card, devID); err == nil && dvpp != nil {
			metrics = append(metrics,
				collector.Metric{Component: "npu", Name: "vdec_util", Value: float64(dvpp.VdecRatio), Unit: "%", Labels: label, Timestamp: now},
				collector.Metric{Component: "npu", Name: "vpc_util", Value: float64(dvpp.VpcRatio), Unit: "%", Labels: label, Timestamp: now},
				collector.Metric{Component: "npu", Name: "venc_util", Value: float64(dvpp.VencRatio), Unit: "%", Labels: label, Timestamp: now},
				collector.Metric{Component: "npu", Name: "jpege_util", Value: float64(dvpp.JpegeRatio), Unit: "%", Labels: label, Timestamp: now},
				collector.Metric{Component: "npu", Name: "jpegd_util", Value: float64(dvpp.JpegdRatio), Unit: "%", Labels: label, Timestamp: now},
			)
		}
	}

	// --- 5.11-5.12 process info ---
	if src.Available() {
		if pids, err := src.ResourceInfoFull(card, devID); err == nil {
			pidStrs := make([]string, len(pids))
			for i, p := range pids {
				pidStrs[i] = strconv.FormatUint(uint64(p), 10)
			}
			metrics = append(metrics,
				collector.Metric{Component: "npu", Name: "process_info", Value: float64(len(pids)), Unit: "个",
					Labels: map[string]string{"npu_id": label["npu_id"], "process_pids": joinStrings(pidStrs, ",")}, Timestamp: now},
				collector.Metric{Component: "npu", Name: "process_total", Value: float64(len(pids)), Unit: "个", Labels: label, Timestamp: now},
			)
		}
	}

	// --- 5.36 aicore_freq ---
	if src.Available() {
		if v, err := src.Frequency(card, devID, freqAICoreCurrent); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "aicore_freq", Value: float64(v), Unit: "MHz", Labels: label, Timestamp: now})
		}
	}

	// --- 5.41 npu_util (overall) ---
	if src.Available() {
		if v, err := src.UtilizationRate(card, devID, rateNPU); err == nil {
			metrics = append(metrics, collector.Metric{Component: "npu", Name: "npu_util", Value: float64(v), Unit: "%", Labels: label, Timestamp: now})
		}
	}

	// --- 5.55-5.58 HBM ECC (delta) ---
	if src.Available() {
		if ecc, err := src.EccInfo(card, devID, devTypeHBM); err == nil && ecc != nil {
			c.emitEccMetrics(&metrics, devID, "hbm", ecc, now)
		}
	}

	// --- 5.59-5.62 DDR ECC (delta) ---
	if src.Available() {
		if ecc, err := src.EccInfo(card, devID, devTypeDDR); err == nil && ecc != nil {
			c.emitEccMetrics(&metrics, devID, "ddr", ecc, now)
		}
	}

	// --- 5.63-5.65 LLC perf ---
	if src.Available() {
		if llc, err := src.LlcPerf(card, devID); err == nil && llc != nil {
			metrics = append(metrics,
				collector.Metric{Component: "npu", Name: "llc_write_hit_rate", Value: float64(llc.WrHitRate), Unit: "%", Labels: label, Timestamp: now},
				collector.Metric{Component: "npu", Name: "llc_read_hit_rate", Value: float64(llc.RdHitRate), Unit: "%", Labels: label, Timestamp: now},
				collector.Metric{Component: "npu", Name: "llc_throughput", Value: float64(llc.Throughput), Unit: "MB/s", Labels: label, Timestamp: now},
			)
		}
	}

	// --- 5.68 roce_link_status (DCMI) ---
	if src.Available() {
		if v, err := src.NetworkHealth(card, devID); err == nil {
			statusStr := "down"
			if v > 0 {
				statusStr = "up"
			}
			metrics = append(metrics, collector.Metric{
				Component: "npu", Name: "roce_link_status", Value: float64(v), Unit: "",
				Labels: map[string]string{"npu_id": strconv.Itoa(devID), "status": statusStr}, Timestamp: now,
			})
		}
	}

	// --- Command-based metrics (npu_smi / hccn_tool) ---

	// 5.66-5.67, 5.71-5.72 net/pcie bandwidth (hccn_tool)
	if bw, err := hccn_tool.Default().Bandwidth(devID); err == nil && bw != nil {
		metrics = append(metrics,
			collector.Metric{Component: "npu", Name: "net_tx_bandwidth", Value: bw.NetTX, Unit: "MB/s", Labels: map[string]string{"npu_id": strconv.Itoa(devID), "direction": "tx"}, Timestamp: now},
			collector.Metric{Component: "npu", Name: "net_rx_bandwidth", Value: bw.NetRX, Unit: "MB/s", Labels: map[string]string{"npu_id": strconv.Itoa(devID), "direction": "rx"}, Timestamp: now},
			collector.Metric{Component: "npu", Name: "pcie_tx_bandwidth", Value: bw.PcieTX, Unit: "MB/s", Labels: map[string]string{"npu_id": strconv.Itoa(devID), "direction": "tx"}, Timestamp: now},
			collector.Metric{Component: "npu", Name: "pcie_rx_bandwidth", Value: bw.PcieRX, Unit: "MB/s", Labels: map[string]string{"npu_id": strconv.Itoa(devID), "direction": "rx"}, Timestamp: now},
		)
	}
	// 5.69 roce_speed_status, 5.70 roce_link_health
	if speed, err := hccn_tool.Default().Speed(devID); err == nil && speed != "" {
		metrics = append(metrics, collector.Metric{Component: "npu", Name: "roce_speed_status", Value: 0, Unit: "", Labels: map[string]string{"npu_id": strconv.Itoa(devID), "roce_speed": speed}, Timestamp: now})
	}
	if link, err := hccn_tool.Default().Link(devID); err == nil && link != "" {
		metrics = append(metrics, collector.Metric{Component: "npu", Name: "roce_link_health", Value: 0, Unit: "", Labels: map[string]string{"npu_id": strconv.Itoa(devID), "roce_link": link}, Timestamp: now})
	}

	// 5.73-5.74 hccs bandwidth (npu-smi -t hccs-bw)
	if bw, err := npu_smi.Default().HccsBandwidth(devID); err == nil && bw != nil {
		metrics = append(metrics,
			collector.Metric{Component: "npu", Name: "hccs_tx_bandwidth", Value: bw.TxMB, Unit: "MB/s", Labels: map[string]string{"npu_id": strconv.Itoa(devID), "direction": "tx"}, Timestamp: now},
			collector.Metric{Component: "npu", Name: "hccs_rx_bandwidth", Value: bw.RxMB, Unit: "MB/s", Labels: map[string]string{"npu_id": strconv.Itoa(devID), "direction": "rx"}, Timestamp: now},
		)
	}

	// 5.75-6.19 hccn_tool statistics (45 metrics: MAC/ROCE/NIC packet counters)
	if stats, err := hccn_tool.Default().Statistics(devID); err == nil {
		for name, val := range stats {
			unit := "个"
			if strings.Contains(name, "_oct_") {
				unit = "bytes"
			}
			metrics = append(metrics, collector.Metric{
				Component: "npu", Name: name, Value: float64(val), Unit: unit,
				Labels: label, Timestamp: now,
			})
		}
	}

	return metrics
}

// emitEccMetrics emits 4 ECC metrics (single/double errors + isolated pages)
// for a device+type. Error counts are delta-based (cumulative → per-cycle).
func (c *NPUCollector) emitEccMetrics(metrics *[]collector.Metric, devID int, devType string, ecc *dcmi.EccInfo, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := strconv.Itoa(devID)

	// single_bit errors (delta)
	singleKey := id + ":" + devType + ":single"
	singleDelta := uint64(0)
	if prev, ok := c.prevEcc[singleKey]; ok && prev > 0 {
		singleDelta = uint64(ecc.SingleBitErrorCnt) - prev
	}
	c.prevEcc[singleKey] = uint64(ecc.SingleBitErrorCnt)
	*metrics = append(*metrics, collector.Metric{
		Component: "npu", Name: devType + "_single_ecc", Value: float64(singleDelta), Unit: "次",
		Labels: map[string]string{"npu_id": id, "device_type": devType, "kind": "single"}, Timestamp: now,
	})

	// double_bit errors (delta)
	doubleKey := id + ":" + devType + ":double"
	doubleDelta := uint64(0)
	if prev, ok := c.prevEcc[doubleKey]; ok && prev > 0 {
		doubleDelta = uint64(ecc.DoubleBitErrorCnt) - prev
	}
	c.prevEcc[doubleKey] = uint64(ecc.DoubleBitErrorCnt)
	*metrics = append(*metrics, collector.Metric{
		Component: "npu", Name: devType + "_double_ecc", Value: float64(doubleDelta), Unit: "次",
		Labels: map[string]string{"npu_id": id, "device_type": devType, "kind": "double"}, Timestamp: now,
	})

	// isolated pages (instantaneous)
	*metrics = append(*metrics,
		collector.Metric{Component: "npu", Name: devType + "_single_ecc_isolated", Value: float64(ecc.SingleBitIsolatedPagesCnt), Unit: "个",
			Labels: map[string]string{"npu_id": id, "device_type": devType}, Timestamp: now},
		collector.Metric{Component: "npu", Name: devType + "_double_ecc_isolated", Value: float64(ecc.DoubleBitIsolatedPagesCnt), Unit: "个",
			Labels: map[string]string{"npu_id": id, "device_type": devType}, Timestamp: now},
	)
}
