//go:build windows

package network

import (
	"encoding/json"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

type winNetAdapterStat struct {
	Name               string  `json:"Name"`
	ReceivedBytes      float64 `json:"ReceivedBytes"`
	SentBytes          float64 `json:"SentBytes"`
	ReceivedPackets    float64 `json:"ReceivedUnicastPackets"`
	SentPackets        float64 `json:"SentUnicastPackets"`
	ReceivedErrors     float64 `json:"ReceivedPacketErrors"`
	SentErrors         float64 `json:"SentPacketErrors"`
	ReceivedDiscards   float64 `json:"ReceivedDiscardedPackets"`
	SentDiscards       float64 `json:"SentDiscardedPackets"`
}

type winNetAdapterStatus struct {
	Name   string `json:"Name"`
	Status string `json:"Status"`
}

type winTCPState struct {
	Name  string `json:"Name"`
	Count int    `json:"Count"`
}

func runPowerShell(script string) (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func ensureJSONArray(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '{' {
		return "[" + raw + "]"
	}
	if len(raw) == 0 {
		return "[]"
	}
	return raw
}

func (c *NetworkCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	var metrics []collector.Metric

	out, err := runPowerShell("Get-NetAdapterStatistics | Select-Object Name,ReceivedBytes,SentBytes,ReceivedUnicastPackets,SentUnicastPackets,ReceivedPacketErrors,SentPacketErrors,ReceivedDiscardedPackets,SentDiscardedPackets | ConvertTo-Json")
	if err != nil {
		slog.Debug("network: failed to get adapter stats", "error", err)
		return metrics, nil
	}

	var stats []winNetAdapterStat
	if err := json.Unmarshal([]byte(ensureJSONArray(out)), &stats); err != nil {
		slog.Debug("network: failed to parse adapter stats", "error", err)
		return metrics, nil
	}

	current := make(map[string]netDevStats)
	for _, s := range stats {
		current[s.Name] = netDevStats{
			rxBytes:   uint64(s.ReceivedBytes),
			rxPackets: uint64(s.ReceivedPackets),
			rxErrs:    uint64(s.ReceivedErrors),
			rxDrop:    uint64(s.ReceivedDiscards),
			txBytes:   uint64(s.SentBytes),
			txPackets: uint64(s.SentPackets),
			txErrs:    uint64(s.SentErrors),
			txDrop:    uint64(s.SentDiscards),
		}
	}

	for iface, currStats := range current {
		if prev, ok := c.prevStats[iface]; ok {
			rxThroughput := float64(currStats.rxBytes-prev.rxBytes) / 3.0
			txThroughput := float64(currStats.txBytes-prev.txBytes) / 3.0

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

			rxPkts := float64(currStats.rxPackets-prev.rxPackets) / 3.0
			txPkts := float64(currStats.txPackets-prev.txPackets) / 3.0
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

		metrics = append(metrics, collector.Metric{
			Component: "network", Name: "error_count",
			Value: float64(currStats.rxErrs), Unit: "次",
			Labels:    map[string]string{"interface": iface, "type": "rx_err"},
			Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "network", Name: "error_count",
			Value: float64(currStats.rxDrop), Unit: "次",
			Labels:    map[string]string{"interface": iface, "type": "rx_drop"},
			Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "network", Name: "error_count",
			Value: float64(currStats.txErrs), Unit: "次",
			Labels:    map[string]string{"interface": iface, "type": "tx_err"},
			Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "network", Name: "error_count",
			Value: float64(currStats.txDrop), Unit: "次",
			Labels:    map[string]string{"interface": iface, "type": "tx_drop"},
			Timestamp: now,
		})

		metrics = append(metrics, collector.Metric{
			Component: "network", Name: "rx_bytes_total",
			Value: float64(currStats.rxBytes), Unit: "bytes",
			Labels:    map[string]string{"interface": iface},
			Timestamp: now,
		})
		metrics = append(metrics, collector.Metric{
			Component: "network", Name: "tx_bytes_total",
			Value: float64(currStats.txBytes), Unit: "bytes",
			Labels:    map[string]string{"interface": iface},
			Timestamp: now,
		})
	}

	c.prevStats = current

	if ifStatusMetrics, err := c.collectInterfaceStatus(now); err == nil {
		metrics = append(metrics, ifStatusMetrics...)
	}

	if connMetrics, err := c.collectConnectionCount(now); err == nil {
		metrics = append(metrics, connMetrics...)
	}

	return metrics, nil
}

func (c *NetworkCollector) collectInterfaceStatus(now time.Time) ([]collector.Metric, error) {
	out, err := runPowerShell("Get-NetAdapter | Select-Object Name,Status | ConvertTo-Json")
	if err != nil {
		return nil, nil
	}

	var statuses []winNetAdapterStatus
	if err := json.Unmarshal([]byte(ensureJSONArray(out)), &statuses); err != nil {
		return nil, nil
	}

	var metrics []collector.Metric
	for _, s := range statuses {
		statusVal := 0
		if s.Status == "Up" {
			statusVal = 1
		}
		metrics = append(metrics, collector.Metric{
			Component: "network",
			Name:      "interface_status",
			Value:     float64(statusVal),
			Unit:      "",
			Labels:    map[string]string{"interface": s.Name, "status": strings.ToLower(s.Status)},
			Timestamp: now,
		})
	}
	return metrics, nil
}

func (c *NetworkCollector) collectConnectionCount(now time.Time) ([]collector.Metric, error) {
	out, err := runPowerShell("Get-NetTCPConnection | Group-Object State | Select-Object Name,Count | ConvertTo-Json")
	if err != nil {
		return nil, nil
	}

	var states []winTCPState
	if err := json.Unmarshal([]byte(ensureJSONArray(out)), &states); err != nil {
		return nil, nil
	}

	var metrics []collector.Metric
	for _, s := range states {
		metrics = append(metrics, collector.Metric{
			Component: "network", Name: "connection_count", Value: float64(s.Count), Unit: "个",
			Labels:    map[string]string{"state": s.Name},
			Timestamp: now,
		})
	}
	return metrics, nil
}
