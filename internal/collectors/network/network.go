package network

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// NetworkCollector collects network metrics.
type NetworkCollector struct {
	procPath  string
	sysPath   string
	prevStats map[string]netDevStats
}

// netDevStats holds parsed /proc/net/dev values for a single interface.
type netDevStats struct {
	rxBytes   uint64
	rxPackets uint64
	rxErrs    uint64
	rxDrop    uint64
	txBytes   uint64
	txPackets uint64
	txErrs    uint64
	txDrop    uint64
}

// New creates a new Network collector.
func New() *NetworkCollector {
	return &NetworkCollector{
		procPath:  "/proc",
		sysPath:   "/sys",
		prevStats: make(map[string]netDevStats),
	}
}

func (c *NetworkCollector) Name() string                 { return "network" }
func (c *NetworkCollector) Component() string            { return "network" }
func (c *NetworkCollector) Priority() collector.Priority { return collector.PriorityHigh }
func (c *NetworkCollector) DefaultInterval() time.Duration {
	return 3 * time.Second
}
func (c *NetworkCollector) DefaultEnabled() bool { return true }

// Collect gathers all network metrics.
func (c *NetworkCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	var metrics []collector.Metric

	current, err := parseNetDev(c.procPath)
	if err != nil {
		return nil, err
	}

	for iface, currStats := range current {
		if iface == "lo" {
			continue
		}

		// Throughput (delta)
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

			// Packet count (delta)
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

		// Error count (current values)
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

		// Total bytes (current values)
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

	// Interface status
	ifStatusMetrics, err := c.collectInterfaceStatus(now)
	if err == nil {
		metrics = append(metrics, ifStatusMetrics...)
	}

	// Connection count
	connMetrics, err := c.collectConnectionCount(now)
	if err == nil {
		metrics = append(metrics, connMetrics...)
	}

	return metrics, nil
}

func (c *NetworkCollector) collectConnectionCount(now time.Time) ([]collector.Metric, error) {
	stateMap := map[string]string{
		"01": "ESTABLISHED", "02": "SYN_SENT", "03": "SYN_RECV",
		"04": "FIN_WAIT1", "05": "FIN_WAIT2", "06": "TIME_WAIT",
		"07": "CLOSE", "08": "CLOSE_WAIT", "09": "LAST_ACK",
		"0A": "LISTEN", "0B": "CLOSING",
	}

	counts := make(map[string]int)
	for _, filename := range []string{"tcp", "tcp6"} {
		path := filepath.Join(c.procPath, "net", filename)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		firstLine := true
		for scanner.Scan() {
			if firstLine {
				firstLine = false
				continue
			}
			fields := strings.Fields(scanner.Text())
			if len(fields) < 4 {
				continue
			}
			state := fields[3]
			if name, ok := stateMap[state]; ok {
				counts[name]++
			}
		}
		f.Close()
	}

	var metrics []collector.Metric
	for state, count := range counts {
		metrics = append(metrics, collector.Metric{
			Component: "network", Name: "connection_count", Value: float64(count), Unit: "个",
			Labels:    map[string]string{"state": state},
			Timestamp: now,
		})
	}

	return metrics, nil
}

// collectInterfaceStatus reads /sys/class/net/*/operstate.
func (c *NetworkCollector) collectInterfaceStatus(now time.Time) ([]collector.Metric, error) {
	netPath := filepath.Join(c.sysPath, "class", "net")
	entries, err := os.ReadDir(netPath)
	if err != nil {
		return nil, err
	}

	var metrics []collector.Metric
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		iface := entry.Name()
		if iface == "lo" {
			continue
		}
		statePath := filepath.Join(netPath, iface, "operstate")
		data, err := os.ReadFile(statePath)
		if err != nil {
			continue
		}
		state := strings.TrimSpace(string(data))
		statusVal := 0
		if state == "up" {
			statusVal = 1
		}
		metrics = append(metrics, collector.Metric{
			Component: "network",
			Name:      "interface_status",
			Value:     float64(statusVal),
			Unit:      "",
			Labels:    map[string]string{"interface": iface, "status": state},
			Timestamp: now,
		})
	}

	return metrics, nil
}

// parseNetDev reads /proc/net/dev and returns a map of interface -> stats.
func parseNetDev(procPath string) (map[string]netDevStats, error) {
	f, err := os.Open(filepath.Join(procPath, "net", "dev"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]netDevStats)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= 2 {
			continue
		}
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}
		stats := netDevStats{}
		stats.rxBytes = parseUint(fields[0])
		stats.rxPackets = parseUint(fields[1])
		stats.rxErrs = parseUint(fields[2])
		stats.rxDrop = parseUint(fields[3])
		stats.txBytes = parseUint(fields[8])
		stats.txPackets = parseUint(fields[9])
		stats.txErrs = parseUint(fields[10])
		stats.txDrop = parseUint(fields[11])
		result[iface] = stats
	}

	return result, scanner.Err()
}

func parseUint(s string) uint64 {
	val, _ := strconv.ParseUint(s, 10, 64)
	return val
}

func roundFloat(val float64, precision int) float64 {
	multiplier := 1.0
	for i := 0; i < precision; i++ {
		multiplier *= 10
	}
	return float64(int64(val*multiplier+0.5)) / multiplier
}

func (c *NetworkCollector) SetProcPath(path string) { c.procPath = path }
func (c *NetworkCollector) SetSysPath(path string)  { c.sysPath = path }

func init() {
	collector.DefaultRegistry.Register(New())
}
