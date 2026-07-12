//go:build linux

package disk

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

var deviceFilter = regexp.MustCompile(`^(sd[a-z]+|nvme\d+n\d+|vd[a-z]+|xvd[a-z]+)$`)

var virtualFS = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "tmpfs": true,
	"devpts": true, "overlay": true, "squashfs": true, "fusectl": true,
	"none": true, "cgroup": true, "cgroup2": true, "mqueue": true,
	"hugetlbfs": true, "rpc_pipefs": true, "binfmt_misc": true,
	"securityfs": true, "pstore": true, "bpf": true, "tracefs": true,
	"debugfs": true, "configfs": true, "autofs": true, "fuse": true,
	"fuse.gvfsd-fuse": true,
}

var linuxDiskPaths = struct {
	procPath string
}{procPath: "/proc"}

func (c *DiskCollector) SetProcPath(path string) {
	linuxDiskPaths.procPath = path
	c.procPath = path
}

func (c *DiskCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	var metrics []collector.Metric

	mounts, err := parseMounts(linuxDiskPaths.procPath)
	if err != nil {
		return nil, err
	}

	for _, m := range mounts {
		if virtualFS[m.fstype] {
			continue
		}
		spaceMetrics, err := c.collectSpaceUsage(m.device, m.mountPoint, m.fstype, now)
		if err != nil {
			continue
		}
		metrics = append(metrics, spaceMetrics...)
	}

	iopsMetrics, _ := c.collectIOPS(now)
	metrics = append(metrics, iopsMetrics...)

	throughputMetrics, _ := c.collectThroughput(now)
	metrics = append(metrics, throughputMetrics...)

	ioWaitMetrics, _ := c.collectIoWait(now)
	metrics = append(metrics, ioWaitMetrics...)

	ioErrMetrics, _ := c.collectIoErrors(now)
	metrics = append(metrics, ioErrMetrics...)

	smartMetrics, _ := c.collectSMART(now)
	metrics = append(metrics, smartMetrics...)

	return metrics, nil
}

func (c *DiskCollector) collectSpaceUsage(device, mountPoint, fstype string, now time.Time) ([]collector.Metric, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(mountPoint, &stat); err != nil {
		return nil, err
	}

	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bfree * uint64(stat.Bsize)
	availBytes := stat.Bavail * uint64(stat.Bsize)
	usedBytes := totalBytes - freeBytes

	usage := 0.0
	if totalBytes > 0 {
		usage = float64(usedBytes) / float64(totalBytes) * 100
	}

	labels := map[string]string{"device": device, "mount_point": mountPoint, "fstype": fstype}
	metrics := []collector.Metric{
		{Component: "disk", Name: "space_usage", Value: roundFloat(usage, 2), Unit: "%", Labels: labels, Timestamp: now},
		{Component: "disk", Name: "space_detail", Value: float64(totalBytes) / (1024 * 1024), Unit: "MB", Labels: withField(labels, "total"), Timestamp: now},
		{Component: "disk", Name: "space_detail", Value: float64(usedBytes) / (1024 * 1024), Unit: "MB", Labels: withField(labels, "used"), Timestamp: now},
		{Component: "disk", Name: "space_detail", Value: float64(availBytes) / (1024 * 1024), Unit: "MB", Labels: withField(labels, "available"), Timestamp: now},
	}
	return metrics, nil
}

func (c *DiskCollector) collectIOPS(now time.Time) ([]collector.Metric, error) {
	current, err := parseDiskStats(linuxDiskPaths.procPath)
	if err != nil {
		return nil, err
	}

	var metrics []collector.Metric
	for dev, curr := range current {
		if prev, ok := c.prevDiskStats[dev]; ok {
			readIops := float64(curr.readsCompleted-prev.readsCompleted) / 5.0
			writeIops := float64(curr.writesCompleted-prev.writesCompleted) / 5.0
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
	c.prevDiskStats = current
	return metrics, nil
}

func (c *DiskCollector) collectThroughput(now time.Time) ([]collector.Metric, error) {
	current, err := parseDiskStats(linuxDiskPaths.procPath)
	if err != nil {
		return nil, err
	}

	var metrics []collector.Metric
	for dev, curr := range current {
		if prev, ok := c.prevDiskStats[dev]; ok {
			readMB := float64(curr.sectorsRead-prev.sectorsRead) * 512 / (1024 * 1024) / 5.0
			writeMB := float64(curr.sectorsWritten-prev.sectorsWritten) * 512 / (1024 * 1024) / 5.0
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
	return metrics, nil
}

func (c *DiskCollector) collectIoWait(now time.Time) ([]collector.Metric, error) {
	current, err := parseCPUStatForIoWait(linuxDiskPaths.procPath)
	if err != nil {
		return nil, err
	}

	var metrics []collector.Metric
	if c.prevCPUTimes != nil && len(current) >= 5 && len(c.prevCPUTimes) >= 5 {
		prevTotal := sumU64(c.prevCPUTimes)
		currTotal := sumU64(current)
		prevIoWait := c.prevCPUTimes[4]
		currIoWait := current[4]
		totalDelta := float64(currTotal - prevTotal)
		ioWaitDelta := float64(currIoWait - prevIoWait)
		if totalDelta > 0 {
			ioWaitPct := ioWaitDelta / totalDelta * 100
			metrics = append(metrics, collector.Metric{
				Component: "disk", Name: "io_wait", Value: roundFloat(ioWaitPct, 2), Unit: "%",
				Timestamp: now,
			})
		}
	}
	c.prevCPUTimes = current
	return metrics, nil
}

func (c *DiskCollector) collectIoErrors(now time.Time) ([]collector.Metric, error) {
	var output string
	if c.mockDmesg != "" {
		output = c.mockDmesg
	} else {
		cmd := exec.Command("dmesg")
		out, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		output = string(out)
	}

	count := 0
	lines := strings.Split(output, "\n")
	for _, line := range lines {
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
	stats, err := parseDiskStats(linuxDiskPaths.procPath)
	if err != nil {
		return nil, err
	}

	var metrics []collector.Metric
	for dev := range stats {
		if output, ok := c.mockSmartctl[dev]; ok {
			metrics = append(metrics, parseSmartOutput(dev, output, now)...)
			continue
		}
		cmd := exec.Command("smartctl", "-H", "/dev/"+dev)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		metrics = append(metrics, parseSmartOutput(dev, string(out), now)...)
	}
	return metrics, nil
}

func parseDiskStats(procPath string) (map[string]diskStats, error) {
	f, err := os.Open(filepath.Join(procPath, "diskstats"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]diskStats)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 11 {
			continue
		}
		dev := fields[2]
		if !deviceFilter.MatchString(dev) {
			continue
		}
		reads := parseU64(fields[3])
		sectorsRead := parseU64(fields[5])
		writes := parseU64(fields[7])
		sectorsWritten := parseU64(fields[9])
		result[dev] = diskStats{
			readsCompleted:  reads,
			sectorsRead:     sectorsRead,
			writesCompleted: writes,
			sectorsWritten:  sectorsWritten,
		}
	}
	return result, scanner.Err()
}

func parseCPUStatForIoWait(procPath string) ([]uint64, error) {
	f, err := os.Open(filepath.Join(procPath, "stat"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 6 {
				return nil, nil
			}
			times := make([]uint64, 0, len(fields)-1)
			for _, f := range fields[1:] {
				val, err := strconv.ParseUint(f, 10, 64)
				if err != nil {
					break
				}
				times = append(times, val)
			}
			return times, nil
		}
	}
	return nil, scanner.Err()
}

func parseMounts(procPath string) ([]MountInfo, error) {
	f, err := os.Open(filepath.Join(procPath, "mounts"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var mounts []MountInfo
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		mounts = append(mounts, MountInfo{device: fields[0], mountPoint: fields[1], fstype: fields[2]})
	}
	return mounts, scanner.Err()
}
