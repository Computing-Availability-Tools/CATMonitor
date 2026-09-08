//go:build linux

package disk

import (
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/dmesg"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/proc"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/smartctl"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/source/statfs"
)

func init() {
	if _, err := os.Stat("/host"); err == nil {
		proc.SetMountsPath("/proc/1/mounts")
		statfs.SetHostPrefix("/host")
	}
}

var deviceFilter = regexp.MustCompile(`^(sd[a-z]+|nvme\d+n\d+|vd[a-z]+|xvd[a-z]+)$`)

var virtualFS = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "tmpfs": true,
	"devpts": true, "overlay": true, "squashfs": true, "fusectl": true,
	"none": true, "cgroup": true, "cgroup2": true, "mqueue": true,
	"hugetlbfs": true, "rpc_pipefs": true, "binfmt_misc": true,
	"securityfs": true, "pstore": true, "bpf": true, "tracefs": true,
	"debugfs": true, "configfs": true, "autofs": true, "fuse": true,
	"fuse.gvfsd-fuse": true, "nfsd": true, "nsfs": true, "efivarfs": true,
	"selinuxfs": true,
}

func (c *DiskCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	var metrics []collector.Metric

	// space_usage per real device (deduplicated, one row per partition).
	if collector.AnyWanted("disk", []string{"space_usage", "space_detail"}) {
		mounts, err := proc.Default().Mounts()
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, m := range mounts {
			if virtualFS[m.Fstype] {
				continue
			}
			if seen[m.Device] {
				continue
			}
			seen[m.Device] = true
			spaceMetrics, err := c.collectSpaceUsage(m.Device, m.MountPoint, m.Fstype, now)
			if err != nil {
				continue
			}
			metrics = append(metrics, spaceMetrics...)
		}
	}

	if collector.AnyWanted("disk", []string{"iops", "throughput", "read_latency", "write_latency"}) {
		current, err := c.filteredDiskStats()
		if err == nil && c.hasPrevDiskStats {
			elapsed := now.Sub(c.prevDiskTime).Seconds()
			if elapsed <= 0 {
				elapsed = 5.0
			}
			if collector.AnyWanted("disk", []string{"iops"}) {
				metrics = append(metrics, c.computeIOPS(current, elapsed, now)...)
			}
			if collector.AnyWanted("disk", []string{"throughput"}) {
				metrics = append(metrics, c.computeThroughput(current, elapsed, now)...)
			}
			if collector.AnyWanted("disk", []string{"read_latency", "write_latency"}) {
				metrics = append(metrics, c.computeLatency(current, elapsed, now)...)
			}
		}
		c.prevDiskStats = current
		c.prevDiskTime = now
		c.hasPrevDiskStats = true
	}
	if collector.AnyWanted("disk", []string{"io_wait"}) {
		if ioWaitMetrics, err := c.collectIoWait(now); err == nil {
			metrics = append(metrics, ioWaitMetrics...)
		}
	}
	if collector.AnyWanted("disk", []string{"io_errors"}) {
		if ioErrMetrics, err := c.collectIoErrors(now); err == nil {
			metrics = append(metrics, ioErrMetrics...)
		}
	}
	if collector.AnyWanted("disk", []string{"smart_status", "smart_temperature"}) {
		if smartMetrics, err := c.collectSMART(now); err == nil {
			metrics = append(metrics, smartMetrics...)
		}
	}
	if collector.AnyWanted("disk", []string{"read_sectors_total", "written_sectors_total", "read_time_total", "write_time_total"}) {
		if rawMetrics, err := c.collectRawCounters(now); err == nil {
			metrics = append(metrics, rawMetrics...)
		}
	}

	return metrics, nil
}

func (c *DiskCollector) collectSpaceUsage(device, mountPoint, fstype string, now time.Time) ([]collector.Metric, error) {
	st, err := statfs.Default().Statfs(mountPoint)
	if err != nil {
		return nil, err
	}

	usage := 0.0
	if st.Total > 0 {
		usage = float64(st.Used) / float64(st.Total) * 100
	}

	labels := map[string]string{"device": device, "mount_point": mountPoint, "fstype": fstype}
	metrics := []collector.Metric{
		{Component: "disk", Name: "space_usage", Value: roundFloat(usage, 2), Unit: "%", Labels: labels, Timestamp: now},
		{Component: "disk", Name: "space_detail", Value: float64(st.Total) / (1024 * 1024), Unit: "MB", Labels: withField(labels, "total"), Timestamp: now},
		{Component: "disk", Name: "space_detail", Value: float64(st.Used) / (1024 * 1024), Unit: "MB", Labels: withField(labels, "used"), Timestamp: now},
		{Component: "disk", Name: "space_detail", Value: float64(st.Avail) / (1024 * 1024), Unit: "MB", Labels: withField(labels, "available"), Timestamp: now},
	}
	return metrics, nil
}

// filteredDiskStats reads /proc/diskstats via the proc source and applies the
// device filter, returning only real block devices.
func (c *DiskCollector) filteredDiskStats() (map[string]proc.DiskStat, error) {
	all, err := proc.Default().Diskstats()
	if err != nil {
		return nil, err
	}
	result := make(map[string]proc.DiskStat)
	for dev, s := range all {
		if !deviceFilter.MatchString(dev) {
			continue
		}
		result[dev] = s
	}
	return result, nil
}

func (c *DiskCollector) computeIOPS(current map[string]proc.DiskStat, elapsed float64, now time.Time) []collector.Metric {
	var metrics []collector.Metric
	for dev, curr := range current {
		if prev, ok := c.prevDiskStats[dev]; ok {
			readIops := float64(curr.ReadsCompleted-prev.ReadsCompleted) / elapsed
			writeIops := float64(curr.WritesCompleted-prev.WritesCompleted) / elapsed
			metrics = append(metrics, collector.Metric{
				Component: "disk", Name: "iops", Value: roundFloat(readIops, 0), Unit: "次/s",
				Labels: map[string]string{"device": dev, "direction": "read"}, Timestamp: now,
			})
			metrics = append(metrics, collector.Metric{
				Component: "disk", Name: "iops", Value: roundFloat(writeIops, 0), Unit: "次/s",
				Labels: map[string]string{"device": dev, "direction": "write"}, Timestamp: now,
			})
		}
	}
	return metrics
}

func (c *DiskCollector) computeThroughput(current map[string]proc.DiskStat, elapsed float64, now time.Time) []collector.Metric {
	var metrics []collector.Metric
	for dev, curr := range current {
		if prev, ok := c.prevDiskStats[dev]; ok {
			readMB := float64(curr.SectorsRead-prev.SectorsRead) * 512 / (1024 * 1024) / elapsed
			writeMB := float64(curr.SectorsWritten-prev.SectorsWritten) * 512 / (1024 * 1024) / elapsed
			metrics = append(metrics, collector.Metric{
				Component: "disk", Name: "throughput", Value: roundFloat(readMB, 2), Unit: "MB/s",
				Labels: map[string]string{"device": dev, "direction": "read"}, Timestamp: now,
			})
			metrics = append(metrics, collector.Metric{
				Component: "disk", Name: "throughput", Value: roundFloat(writeMB, 2), Unit: "MB/s",
				Labels: map[string]string{"device": dev, "direction": "write"}, Timestamp: now,
			})
		}
	}
	return metrics
}

func (c *DiskCollector) computeLatency(current map[string]proc.DiskStat, elapsed float64, now time.Time) []collector.Metric {
	var metrics []collector.Metric
	for dev, curr := range current {
		if prev, ok := c.prevDiskStats[dev]; ok {
			readLatency := float64(curr.ReadTime-prev.ReadTime) / elapsed
			writeLatency := float64(curr.WriteTime-prev.WriteTime) / elapsed
			metrics = append(metrics, collector.Metric{
				Component: "disk", Name: "read_latency", Value: roundFloat(readLatency, 2), Unit: "ms/s",
				Labels: map[string]string{"device": dev}, Timestamp: now,
			})
			metrics = append(metrics, collector.Metric{
				Component: "disk", Name: "write_latency", Value: roundFloat(writeLatency, 2), Unit: "ms/s",
				Labels: map[string]string{"device": dev}, Timestamp: now,
			})
		}
	}
	return metrics
}

func (c *DiskCollector) collectIoWait(now time.Time) ([]collector.Metric, error) {
	stat, err := proc.Default().Stat()
	if err != nil {
		return nil, err
	}
	curr, ok := stat.Cores["cpu"]
	if !ok {
		return nil, nil
	}
	var metrics []collector.Metric
	if c.hasPrevCPU {
		prevTotal := cpuStatTotal(c.prevCPU)
		currTotal := cpuStatTotal(curr)
		totalDelta := float64(currTotal - prevTotal)
		ioWaitDelta := float64(curr.Iowait - c.prevCPU.Iowait)
		if totalDelta > 0 {
			ioWaitPct := ioWaitDelta / totalDelta * 100
			metrics = append(metrics, collector.Metric{
				Component: "disk", Name: "io_wait", Value: roundFloat(ioWaitPct, 2), Unit: "%",
				Timestamp: now,
			})
		}
	}
	c.prevCPU = curr
	c.hasPrevCPU = true
	return metrics, nil
}

func (c *DiskCollector) collectIoErrors(now time.Time) ([]collector.Metric, error) {
	output, err := dmesg.Default().Text()
	if err != nil {
		return nil, err
	}
	count := 0
	for _, line := range strings.Split(output, "\n") {
		l := strings.ToLower(line)
		if strings.Contains(l, "i/o error") || strings.Contains(l, "blk_update_request") {
			count++
		}
	}
	return []collector.Metric{{
		Component: "disk", Name: "io_errors", Value: float64(count), Unit: "次", Timestamp: now,
	}}, nil
}

func (c *DiskCollector) collectSMART(now time.Time) ([]collector.Metric, error) {
	devs, err := c.filteredDiskStats()
	if err != nil {
		return nil, err
	}
	var metrics []collector.Metric
	for dev := range devs {
		output, err := smartctl.Default().Health(dev)
		if err != nil || output == "" {
			continue
		}
		metrics = append(metrics, parseSmartOutput(dev, output, now)...)
	}
	return metrics, nil
}

// collectRawCounters emits cumulative raw counters from /proc/diskstats:
// read_sectors_total, written_sectors_total, read_time_total (ms),
// write_time_total (ms). Only real block devices (filtered by deviceFilter).
func (c *DiskCollector) collectRawCounters(now time.Time) ([]collector.Metric, error) {
	current, err := c.filteredDiskStats()
	if err != nil {
		return nil, err
	}
	var metrics []collector.Metric
	for dev, s := range current {
		labels := map[string]string{"device": dev}
		metrics = append(metrics,
			collector.Metric{Component: "disk", Name: "read_sectors_total", Value: float64(s.SectorsRead), Unit: "", Labels: labels, Timestamp: now},
			collector.Metric{Component: "disk", Name: "written_sectors_total", Value: float64(s.SectorsWritten), Unit: "", Labels: labels, Timestamp: now},
			collector.Metric{Component: "disk", Name: "read_time_total", Value: float64(s.ReadTime), Unit: "ms", Labels: labels, Timestamp: now},
			collector.Metric{Component: "disk", Name: "write_time_total", Value: float64(s.WriteTime), Unit: "ms", Labels: labels, Timestamp: now},
		)
	}
	return metrics, nil
}
