// Package dmidecode provides a data source that runs the external `dmidecode
// --type 17` command and parses its SMBIOS Memory Device entries into typed
// structs describing installed DIMMs.
//
// DIMM inventory is static, so the first successful call to MemoryDevices()
// caches the result permanently (mirrors the lscpu source's sync.Once idiom).
// The source is a process-wide singleton (Default); tests inject mock output
// via SetMock. dmidecode typically requires root; when unavailable or
// permission-denied, Available() returns false and MemoryDevices() returns
// an error so the caller can gracefully degrade.
package dmidecode

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// MemoryDevice holds one SMBIOS Memory Device (type 17) entry.
type MemoryDevice struct {
	Locator     string // e.g. "DIMM0"
	Type        string // e.g. "DDR4"; empty for empty slot
	Speed       string // e.g. "3200 MT/s"
	Manufacturer string
	SizeMB      int // 0 for "No Module Installed"
}

// SystemInfo holds the SMBIOS System Information (type 1) entry describing the
// server/device identity. Static; cached permanently after the first call.
type SystemInfo struct {
	Manufacturer string // e.g. "Supermicro"
	ProductName string // e.g. "X12STW-F" (board/product); "To be filled by O.E.M." when unset
	Version      string // product version / board revision
	Serial       string // system serial number
}

// Source is the typed interface for the dmidecode data source.
type Source interface {
	// MemoryDevices returns all DIMM entries (including empty slots, which
	// have SizeMB=0). Cached after the first successful call.
	MemoryDevices() ([]MemoryDevice, error)
	// SystemInfo returns the SMBIOS type 1 (System Information) entry.
	// Cached after the first successful call; nil if dmidecode is unavailable.
	SystemInfo() (*SystemInfo, error)
	// Available reports whether dmidecode is on PATH. Note: root permission
	// to actually read SMBIOS is verified at call time, not here.
	Available() bool
}

type defaultSource struct {
	once        sync.Once
	cached      []MemoryDevice
	systemOnce  sync.Once
	cachedSys   *SystemInfo
	mockOut     string
	mockSysOut  string
}

var defaultSrc = &defaultSource{}

func Default() Source { return defaultSrc }

// SetMock injects canned `dmidecode --type 17` output for testing (clears the
// cache so the next call re-parses the mock).
func SetMock(out string) {
	defaultSrc.mockOut = out
	defaultSrc.once = sync.Once{}
	defaultSrc.cached = nil
}

// SetSystemMock injects canned `dmidecode --type 1` output for testing.
func SetSystemMock(out string) {
	defaultSrc.mockSysOut = out
	defaultSrc.systemOnce = sync.Once{}
	defaultSrc.cachedSys = nil
}

func (s *defaultSource) Available() bool {
	_, err := exec.LookPath("dmidecode")
	return err == nil
}

func (s *defaultSource) MemoryDevices() ([]MemoryDevice, error) {
	var perr error
	s.once.Do(func() {
		var out string
		if s.mockOut != "" {
			out = s.mockOut
		} else {
			o, err := exec.Command("dmidecode", "--type", "17").Output()
			if err != nil {
				perr = err
				return
			}
			out = string(o)
		}
		s.cached = parseDmidecode(out)
	})
	if perr != nil {
		return nil, perr
	}
	return s.cached, nil
}

func (s *defaultSource) SystemInfo() (*SystemInfo, error) {
	var perr error
	s.systemOnce.Do(func() {
		var out string
		if s.mockSysOut != "" {
			out = s.mockSysOut
		} else {
			o, err := exec.Command("dmidecode", "--type", "1").Output()
			if err != nil {
				perr = err
				return
			}
			out = string(o)
		}
		s.cachedSys = parseSystemInfo(out)
	})
	if perr != nil {
		return nil, perr
	}
	return s.cachedSys, nil
}

// parseSystemInfo walks `dmidecode --type 1` output and extracts the System
// Information fields. Returns nil if no System Information section is found.
func parseSystemInfo(out string) *SystemInfo {
	inSystem := false
	var si SystemInfo
	found := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "Handle "):
			inSystem = false
		case strings.HasPrefix(line, "System Information"):
			inSystem = true
			found = true
		case strings.HasPrefix(line, "\t"):
			if !inSystem {
				continue
			}
			kv := strings.SplitN(strings.TrimSpace(line), ":", 2)
			if len(kv) < 2 {
				continue
			}
			key := strings.TrimSpace(kv[0])
			val := strings.TrimSpace(kv[1])
			switch key {
			case "Manufacturer":
				si.Manufacturer = val
			case "Product Name":
				si.ProductName = val
			case "Version":
				si.Version = val
			case "Serial Number":
				si.Serial = val
			}
		}
	}
	if !found {
		return nil
	}
	return &si
}

// parseDmidecode walks the dmidecode --type 17 output, splitting on "Handle"
// header lines and extracting fields from each Memory Device section.
func parseDmidecode(out string) []MemoryDevice {
	var devices []MemoryDevice
	var cur *MemoryDevice
	flush := func() {
		if cur != nil {
			devices = append(devices, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "Handle "):
			// New section starts; flush the previous one (only Memory Device
			// sections are type 17 in this output, so each Handle is a DIMM).
			flush()
			cur = &MemoryDevice{}
		case strings.HasPrefix(line, "\t"):
			if cur == nil {
				continue
			}
			kv := strings.SplitN(strings.TrimSpace(line), ":", 2)
			if len(kv) < 2 {
				continue
			}
			key := strings.TrimSpace(kv[0])
			val := strings.TrimSpace(kv[1])
			switch key {
			case "Locator":
				cur.Locator = val
			case "Type":
				cur.Type = val
			case "Speed":
				cur.Speed = val
			case "Manufacturer":
				cur.Manufacturer = val
			case "Size":
				cur.SizeMB = parseSizeMB(val)
			}
		}
	}
	flush()
	return devices
}

// parseSizeMB converts a dmidecode Size value to MB. Examples:
//   - "16 GB"   -> 16384
//   - "1024 MB" -> 1024
//   - "No Module Installed" -> 0
func parseSizeMB(s string) int {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return 0
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0
	}
	switch strings.ToUpper(fields[1]) {
	case "GB":
		return n * 1024
	case "MB":
		return n
	case "TB":
		return n * 1024 * 1024
	}
	return 0
}
