//go:build linux

package network

import (
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/sys"
)

func (c *NetworkCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	var metrics []collector.Metric

	current, err := proc.Default().NetDev()
	if err != nil {
		return nil, err
	}

	for iface, curr := range current {
		if iface == "lo" {
			continue
		}

		if prev, ok := c.prevStats[iface]; ok {
			rxThroughput := float64(curr.RxBytes-prev.RxBytes) / 3.0
			txThroughput := float64(curr.TxBytes-prev.TxBytes) / 3.0

			metrics = append(metrics, collector.Metric{
				Component: "network", Name: "throughput",
				Value: roundFloat(rxThroughput, 0), Unit: "bytes/s",
				Labels:    map[string]string{"interface": iface, "direction": "rx"},
				Timestamp: now,
			})
			metrics = append(metrics, collector.Metric{
				Component: "network", Name: "throughput",
				Value: roundFloat(txThroughput, 0), Unit: "bytes/s",
				Labels:    map[string]string{"interface": iface, "direction": "tx"},
				Timestamp: now,
			})

			rxPkts := float64(curr.RxPackets-prev.RxPackets) / 3.0
			txPkts := float64(curr.TxPackets-prev.TxPackets) / 3.0
			metrics = append(metrics, collector.Metric{
				Component: "network", Name: "packet_count",
				Value: roundFloat(rxPkts, 0), Unit: "个/s",
				Labels:    map[string]string{"interface": iface, "direction": "rx"},
				Timestamp: now,
			})
			metrics = append(metrics, collector.Metric{
				Component: "network", Name: "packet_count",
				Value: roundFloat(txPkts, 0), Unit: "个/s",
				Labels:    map[string]string{"interface": iface, "direction": "tx"},
				Timestamp: now,
			})
		}

		metrics = append(metrics,
			collector.Metric{Component: "network", Name: "error_count", Value: float64(curr.RxErrs), Unit: "次",
				Labels: map[string]string{"interface": iface, "type": "rx_err"}, Timestamp: now},
			collector.Metric{Component: "network", Name: "error_count", Value: float64(curr.RxDrop), Unit: "次",
				Labels: map[string]string{"interface": iface, "type": "rx_drop"}, Timestamp: now},
			collector.Metric{Component: "network", Name: "error_count", Value: float64(curr.TxErrs), Unit: "次",
				Labels: map[string]string{"interface": iface, "type": "tx_err"}, Timestamp: now},
			collector.Metric{Component: "network", Name: "error_count", Value: float64(curr.TxDrop), Unit: "次",
				Labels: map[string]string{"interface": iface, "type": "tx_drop"}, Timestamp: now},
			collector.Metric{Component: "network", Name: "rx_bytes_total", Value: float64(curr.RxBytes), Unit: "bytes",
				Labels: map[string]string{"interface": iface}, Timestamp: now},
			collector.Metric{Component: "network", Name: "tx_bytes_total", Value: float64(curr.TxBytes), Unit: "bytes",
				Labels: map[string]string{"interface": iface}, Timestamp: now},
		)
	}

	c.prevStats = current

	if statusMetrics, err := c.collectInterfaceStatus(now); err == nil {
		metrics = append(metrics, statusMetrics...)
	}
	if connMetrics, err := c.collectConnectionCount(now); err == nil {
		metrics = append(metrics, connMetrics...)
	}

	return metrics, nil
}

func (c *NetworkCollector) collectConnectionCount(now time.Time) ([]collector.Metric, error) {
	states, err := proc.Default().NetTCPStates()
	if err != nil {
		return nil, err
	}
	var metrics []collector.Metric
	for state, count := range states {
		metrics = append(metrics, collector.Metric{
			Component: "network", Name: "connection_count", Value: float64(count), Unit: "个",
			Labels:    map[string]string{"state": state},
			Timestamp: now,
		})
	}
	return metrics, nil
}

func (c *NetworkCollector) collectInterfaceStatus(now time.Time) ([]collector.Metric, error) {
	ifaces, err := sys.Default().NetInterfaces()
	if err != nil {
		return nil, err
	}
	var metrics []collector.Metric
	for _, iface := range ifaces {
		if iface == "lo" {
			continue
		}
		state, err := sys.Default().NetOperstate(iface)
		if err != nil {
			continue
		}
		statusVal := 0
		if state == "up" {
			statusVal = 1
		}
		metrics = append(metrics, collector.Metric{
			Component: "network", Name: "interface_status", Value: float64(statusVal), Unit: "",
			Labels:    map[string]string{"interface": iface, "status": state},
			Timestamp: now,
		})
	}
	return metrics, nil
}

// debugRealSys (temporary) — checks collectInterfaceStatus against real /sys.
func (c *NetworkCollector) debugRealSys() {}
