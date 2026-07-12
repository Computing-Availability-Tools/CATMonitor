//go:build windows

package memory

import (
	"syscall"
	"time"
	"unsafe"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

var (
	kernel32DLL_mem       = syscall.NewLazyDLL("kernel32.dll")
	procGlobalMemoryEx    = kernel32DLL_mem.NewProc("GlobalMemoryStatusEx")
)

type memoryStatusEx struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

func getMemoryStatus() (*memoryStatusEx, error) {
	var memStat memoryStatusEx
	memStat.dwLength = uint32(unsafe.Sizeof(memStat))
	r1, _, e1 := procGlobalMemoryEx.Call(uintptr(unsafe.Pointer(&memStat)))
	if r1 == 0 {
		return nil, e1
	}
	return &memStat, nil
}

func (c *MemoryCollector) collectUsage(now time.Time) ([]collector.Metric, error) {
	memStat, err := getMemoryStatus()
	if err != nil {
		return nil, err
	}

	totalPhys := memStat.ullTotalPhys
	availPhys := memStat.ullAvailPhys
	usage := float64(memStat.dwMemoryLoad)

	var metrics []collector.Metric
	metrics = append(metrics, collector.Metric{
		Component: "memory",
		Name:      "usage",
		Value:     roundFloat(usage, 2),
		Unit:      "%",
		Timestamp: now,
	})

	metrics = append(metrics, collector.Metric{
		Component: "memory",
		Name:      "usage_detail",
		Value:     roundFloat(float64(totalPhys)/1024/1024, 1),
		Unit:      "MB",
		Labels:    map[string]string{"field": "total"},
		Timestamp: now,
	})
	metrics = append(metrics, collector.Metric{
		Component: "memory",
		Name:      "usage_detail",
		Value:     roundFloat(float64(totalPhys-availPhys)/1024/1024, 1),
		Unit:      "MB",
		Labels:    map[string]string{"field": "used"},
		Timestamp: now,
	})
	metrics = append(metrics, collector.Metric{
		Component: "memory",
		Name:      "usage_detail",
		Value:     roundFloat(float64(availPhys)/1024/1024, 1),
		Unit:      "MB",
		Labels:    map[string]string{"field": "available"},
		Timestamp: now,
	})

	return metrics, nil
}

func (c *MemoryCollector) collectSwapUsage(now time.Time) ([]collector.Metric, error) {
	memStat, err := getMemoryStatus()
	if err != nil {
		return nil, err
	}

	totalPage := memStat.ullTotalPageFile
	availPage := memStat.ullAvailPageFile

	usage := 0.0
	if totalPage > 0 {
		usage = float64(totalPage-availPage) / float64(totalPage) * 100
	}

	return []collector.Metric{{
		Component: "memory",
		Name:      "swap_usage",
		Value:     roundFloat(usage, 2),
		Unit:      "%",
		Timestamp: now,
	}}, nil
}

func (c *MemoryCollector) collectECCErrors(filename, metricName string, now time.Time) ([]collector.Metric, error) {
	return nil, nil
}

func (c *MemoryCollector) collectPageFaults(now time.Time) ([]collector.Metric, error) {
	return nil, nil
}

func (c *MemoryCollector) collectOOMCount(now time.Time) ([]collector.Metric, error) {
	return nil, nil
}

func (c *MemoryCollector) SetMockDmesg(s string) { c.mockDmesg = s }
