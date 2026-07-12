//go:build windows

package disk

import (
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

var (
	kernel32DLL_disk       = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceEx = kernel32DLL_disk.NewProc("GetDiskFreeSpaceExW")
	procGetLogicalDrives   = kernel32DLL_disk.NewProc("GetLogicalDrives")
	procGetDriveType       = kernel32DLL_disk.NewProc("GetDriveTypeW")
)

func getLogicalDrives() []string {
	r1, _, _ := procGetLogicalDrives.Call()
	if r1 == 0 {
		return nil
	}
	bitmask := uint32(r1)
	var drives []string
	for i := 0; i < 26; i++ {
		if bitmask&(1<<i) != 0 {
			drives = append(drives, string(rune('A'+i))+":\\")
		}
	}
	return drives
}

func isFixedDrive(path string) bool {
	pathPtr, _ := syscall.UTF16PtrFromString(path)
	r1, _, _ := procGetDriveType.Call(uintptr(unsafe.Pointer(pathPtr)))
	return r1 == 3 // DRIVE_FIXED
}

func getDiskSpace(path string) (totalBytes, freeBytes, availBytes uint64, err error) {
	pathPtr, e1 := syscall.UTF16PtrFromString(path)
	if e1 != nil {
		return 0, 0, 0, e1
	}
	var freeAvailable, total, totalFree int64
	r1, _, e2 := procGetDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeAvailable)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r1 == 0 {
		return 0, 0, 0, e2
	}
	return uint64(total), uint64(totalFree), uint64(freeAvailable), nil
}

func getVolumeName(path string) string {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetVolumeInformationW")

	pathPtr, _ := syscall.UTF16PtrFromString(path)
	var volName [256]uint16
	var volSerial, maxCompLen, flags uint32
	var fsName [256]uint16

	proc.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&volName[0])),
		uintptr(len(volName)),
		uintptr(unsafe.Pointer(&volSerial)),
		uintptr(unsafe.Pointer(&maxCompLen)),
		uintptr(unsafe.Pointer(&flags)),
		uintptr(unsafe.Pointer(&fsName[0])),
		uintptr(len(fsName)),
	)
	return strings.TrimRight(string(utf16.Decode(fsName[:])), "\x00")
}

func (c *DiskCollector) Collect() ([]collector.Metric, error) {
	now := time.Now()
	var metrics []collector.Metric

	drives := getLogicalDrives()
	for _, drive := range drives {
		if !isFixedDrive(drive) {
			continue
		}
		fstype := getVolumeName(drive + "\\")
		if fstype == "" {
			fstype = "unknown"
		}
		spaceMetrics, err := c.collectSpaceUsage(drive, drive, fstype, now)
		if err != nil {
			continue
		}
		metrics = append(metrics, spaceMetrics...)
	}

	return metrics, nil
}

func (c *DiskCollector) collectSpaceUsage(device, mountPoint, fstype string, now time.Time) ([]collector.Metric, error) {
	totalBytes, freeBytes, availBytes, err := getDiskSpace(mountPoint)
	if err != nil {
		return nil, err
	}

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
