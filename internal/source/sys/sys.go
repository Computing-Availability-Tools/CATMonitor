// Package sys provides a data source that reads and parses the Linux /sys
// virtual filesystem. It returns typed structs; the parsing logic (file tree
// traversal + string -> struct) is centralized here so collectors stay thin.
//
// Like proc, sys is a process-wide singleton (Default) backed by a swappable
// root path (SetRoot) for testing, with no caching (decision C).
package sys

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CacheInfo describes one cache level of a CPU core, parsed from
// /sys/devices/system/cpu/<core>/cache/index*/.
type CacheInfo struct {
	Level  int    // 1, 2, 3
	Type   string // "Data", "Instruction", "Unified"
	SizeKB int    // cache size in KB (parsed from "32K" style content)
}

// EdacMC holds the ECC error counters of one memory controller, parsed from
// /sys/devices/system/edac/mc/<mc>/. Provided for the future memory collector
// migration (decision G); not used by the CPU collector.
type EdacMC struct {
	Name    string // "mc0", "mc1"
	CECount uint64  // correctable error count
	UECount uint64  // uncorrectable error count
}

// ThermalZone holds one /sys/class/thermal/thermal_zone* reading. Provided
// so the CPU collector can read CPU temperature via the source layer during
// the migration; the temperature metric later switches to ipmi (decision E),
// after which this method is unused but retained like Edac/NetOperstate.
type ThermalZone struct {
	Name       string // "thermal_zone0"
	TempMilliC int    // temperature in millidegrees Celsius
}

// Source is the typed interface for the /sys data source.
type Source interface {
	// CpuFreqs returns scaling_cur_freq (kHz) per online core that exposes a
	// cpufreq policy. Keyed by core name ("cpu0", "cpu1", ...).
	CpuFreqs() (map[string]uint64, error)
	// CpuInfoMinFreq returns the hardware minimum frequency (kHz) read from
	// cpuinfo_min_freq of the first available core. Static value.
	CpuInfoMinFreq() (uint64, error)
	// CpuInfoMaxFreq returns the hardware maximum frequency (kHz) read from
	// cpuinfo_max_freq of the first available core. Static value.
	CpuInfoMaxFreq() (uint64, error)
	// CacheInfos returns the cache descriptors of one core.
	CacheInfos(core string) ([]CacheInfo, error)
	// CpuOnline / CpuOffline / CpuIsolated parse the comma/range CPU lists
	// (e.g. "0-3,5,7-9") into a slice of core numbers.
	CpuOnline() ([]int, error)
	CpuOffline() ([]int, error)
	CpuIsolated() ([]int, error)
	// Nodes returns the NUMA node identifiers (e.g. ["0","1"]) by listing
	// /sys/devices/system/node/node*. Empty on UMA / containers.
	Nodes() ([]string, error)
	// Edac returns per-memory-controller ECC counters. Provided for the
	// future memory collector migration.
	Edac() ([]EdacMC, error)
	// Thermal returns the thermal-zone readings from
	// /sys/class/thermal/thermal_zone*/temp.
	Thermal() ([]ThermalZone, error)
	// NetOperstate returns the operational state ("up"/"down"/...) of a
	// network interface. Provided for the future network collector migration.
	NetOperstate(iface string) (string, error)
	// NetInterfaces lists all network interface names under
	// /sys/class/net/ (including "lo"); callers filter as needed.
	NetInterfaces() ([]string, error)
}

type defaultSource struct {
	root string
}

var root = "/sys"

func New(r string) Source { return &defaultSource{root: r} }

func Default() Source { return &defaultSource{root: root} }

func SetRoot(r string) { root = r }

func (s *defaultSource) CpuFreqs() (map[string]uint64, error) {
	cpuDir := filepath.Join(s.root, "devices", "system", "cpu")
	entries, err := os.ReadDir(cpuDir)
	if err != nil {
		return nil, err
	}
	result := make(map[string]uint64)
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, "cpu") {
			continue
		}
		if name == "cpuidle" || name == "cpufreq" {
			continue
		}
		freqPath := filepath.Join(cpuDir, name, "cpufreq", "scaling_cur_freq")
		data, err := os.ReadFile(freqPath)
		if err != nil {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			continue
		}
		result[name] = v
	}
	return result, nil
}

func (s *defaultSource) readFirstCoreFreqFile(name string) (uint64, error) {
	cpuDir := filepath.Join(s.root, "devices", "system", "cpu")
	entries, err := os.ReadDir(cpuDir)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() || !strings.HasPrefix(n, "cpu") || n == "cpuidle" || n == "cpufreq" {
			continue
		}
		p := filepath.Join(cpuDir, n, "cpufreq", name)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			continue
		}
		return v, nil
	}
	// No core exposes the requested cpufreq file (e.g. WSL/virtualized
	// guests without cpufreq). Return an error so the caller skips the
	// metric rather than emitting a misleading 0.
	return 0, os.ErrNotExist
}

func (s *defaultSource) CpuInfoMinFreq() (uint64, error) {
	return s.readFirstCoreFreqFile("cpuinfo_min_freq")
}

func (s *defaultSource) CpuInfoMaxFreq() (uint64, error) {
	return s.readFirstCoreFreqFile("cpuinfo_max_freq")
}

func (s *defaultSource) CacheInfos(core string) ([]CacheInfo, error) {
	cacheDir := filepath.Join(s.root, "devices", "system", "cpu", core, "cache")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil, err
	}
	var result []CacheInfo
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "index") {
			continue
		}
		idxDir := filepath.Join(cacheDir, e.Name())
		levelBytes, err := os.ReadFile(filepath.Join(idxDir, "level"))
		if err != nil {
			continue
		}
		level, err := strconv.Atoi(strings.TrimSpace(string(levelBytes)))
		if err != nil {
			continue
		}
		typ, err := os.ReadFile(filepath.Join(idxDir, "type"))
		if err != nil {
			continue
		}
		sizeBytes, err := os.ReadFile(filepath.Join(idxDir, "size"))
		if err != nil {
			continue
		}
		result = append(result, CacheInfo{
			Level:  level,
			Type:   strings.TrimSpace(string(typ)),
			SizeKB: parseCacheSize(string(sizeBytes)),
		})
	}
	return result, nil
}

// parseCacheSize parses a /sys cache size string like "32K" or "1024K" into
// an integer KB value. Returns 0 if it cannot be parsed.
func parseCacheSize(s string) int {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "K")
	s = strings.TrimSpace(s)
	v, _ := strconv.Atoi(s)
	return v
}

func (s *defaultSource) cpuListFile(name string) ([]int, error) {
	p := filepath.Join(s.root, "devices", "system", "cpu", name)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return parseCPUList(strings.TrimSpace(string(data))), nil
}

func (s *defaultSource) CpuOnline() ([]int, error)    { return s.cpuListFile("online") }
func (s *defaultSource) CpuOffline() ([]int, error)  { return s.cpuListFile("offline") }
func (s *defaultSource) CpuIsolated() ([]int, error)  { return s.cpuListFile("isolated") }

// parseCPUList expands a Linux CPU list string such as "0-3,5,7-9" into the
// explicit slice [0 1 2 3 5 7 8 9]. Empty input yields an empty slice.
func parseCPUList(s string) []int {
	var result []int
	if s == "" {
		return result
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if dash := strings.Index(part, "-"); dash >= 0 {
			lo, err1 := strconv.Atoi(part[:dash])
			hi, err2 := strconv.Atoi(part[dash+1:])
			if err1 != nil || err2 != nil || lo > hi {
				continue
			}
			for i := lo; i <= hi; i++ {
				result = append(result, i)
			}
		} else {
			if v, err := strconv.Atoi(part); err == nil {
				result = append(result, v)
			}
		}
	}
	return result
}

func (s *defaultSource) Nodes() ([]string, error) {
	nodeDir := filepath.Join(s.root, "devices", "system", "node")
	entries, err := os.ReadDir(nodeDir)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "node") {
			continue
		}
		result = append(result, strings.TrimPrefix(e.Name(), "node"))
	}
	return result, nil
}

func (s *defaultSource) Edac() ([]EdacMC, error) {
	edacDir := filepath.Join(s.root, "devices", "system", "edac", "mc")
	entries, err := os.ReadDir(edacDir)
	if err != nil {
		return nil, nil
	}
	var result []EdacMC
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "mc") {
			continue
		}
		mc := EdacMC{Name: e.Name()}
		if data, err := os.ReadFile(filepath.Join(edacDir, e.Name(), "ce_count")); err == nil {
			mc.CECount, _ = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		}
		if data, err := os.ReadFile(filepath.Join(edacDir, e.Name(), "ue_count")); err == nil {
			mc.UECount, _ = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		}
		result = append(result, mc)
	}
	return result, nil
}

func (s *defaultSource) NetOperstate(iface string) (string, error) {
	p := filepath.Join(s.root, "class", "net", iface, "operstate")
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (s *defaultSource) NetInterfaces() ([]string, error) {
	netDir := filepath.Join(s.root, "class", "net")
	entries, err := os.ReadDir(netDir)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, e := range entries {
		// /sys/class/net/* are symlinks to /sys/devices/...; IsDir() is false
		// for symlinks, so accept both directories and symlinks.
		if e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			result = append(result, e.Name())
		}
	}
	return result, nil
}

func (s *defaultSource) Thermal() ([]ThermalZone, error) {
	thermalDir := filepath.Join(s.root, "class", "thermal")
	entries, err := os.ReadDir(thermalDir)
	if err != nil {
		return nil, err
	}
	var result []ThermalZone
	for _, e := range entries {
		// /sys/class/thermal/* are symlinks; accept dirs and symlinks.
		if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			continue
		}
		if !strings.HasPrefix(e.Name(), "thermal_zone") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(thermalDir, e.Name(), "temp"))
		if err != nil {
			continue
		}
		milli, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			continue
		}
		result = append(result, ThermalZone{Name: e.Name(), TempMilliC: int(milli)})
	}
	return result, nil
}
