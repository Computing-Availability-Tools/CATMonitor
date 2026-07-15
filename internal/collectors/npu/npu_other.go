//go:build !linux

// Non-Linux stubs: NPU (Huawei Ascend) is Linux-only. On other platforms
// the collector registers but produces no metrics (graceful degradation).

package npu

import (
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func (c *NPUCollector) ensureDevices() { c.devicesReady = true }

func (c *NPUCollector) collectStatic(now time.Time) ([]collector.Metric, error) {
	return nil, nil
}

func (c *NPUCollector) collectDevice(devID int, now time.Time) []collector.Metric {
	return nil
}
