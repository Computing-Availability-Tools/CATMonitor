// Package proc provides a data source that reads and parses the Linux /proc
// virtual filesystem. It returns typed structs so that the parsing logic
// (string -> struct) is centralized here and can be shared by multiple
// collectors (e.g. cpu and disk both consume /proc/stat).
//
// The source is a process-wide singleton (Default) backed by a swappable
// root path (SetRoot) for testing. It performs no caching: every call reads
// the file fresh (decision C) — /proc is an in-memory filesystem so reads
// are cheap and freshness is preferred over coalescing.
package proc

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CPUStat holds the 10 time fields of a single "cpu" / "cpuN" line in
// /proc/stat. All values are cumulative jiffies (USER_HZ, 1/100s on x86).
type CPUStat struct {
	User      uint64
	Nice      uint64
	System    uint64
	Idle      uint64
	Iowait    uint64
	Irq       uint64
	Softirq   uint64
	Steal     uint64
	Guest     uint64
	GuestNice uint64
}

// Stat is the parsed result of /proc/stat: per-core cumulative time fields
// plus the total context-switch counter (the "ctxt" line).
//
// Cores is keyed by the cpu name ("cpu" for the aggregate total, "cpu0" ..
// "cpuN" for individual cores). Bundling ctxt here lets a caller that needs
// both usage (from cpu times) and context_switches read /proc/stat once.
type Stat struct {
	Cores            map[string]CPUStat
	ContextSwitches  uint64
}

// Loadavg is the parsed /proc/loadavg content.
type Loadavg struct {
	One     float64 // 1-minute load average
	Five    float64 // 5-minute load average
	Fifteen float64 // 15-minute load average
	Running int     // number of running processes
	Total   int     // total number of processes
}

// DiskStat holds per-device counters from /proc/diskstats. The source
// returns ALL devices unfiltered; collectors apply their own device filter.
type DiskStat struct {
	ReadsCompleted  uint64
	SectorsRead     uint64
	WritesCompleted uint64
	SectorsWritten  uint64
	ReadTime        uint64 // field 7: time spent reading (ms)
	WriteTime       uint64 // field 11: time spent writing (ms)
}

// NetDevStat holds per-interface counters from /proc/net/dev. The source
// returns ALL interfaces (including "lo"); collectors filter as needed.
type NetDevStat struct {
	RxBytes   uint64
	RxPackets uint64
	RxErrs    uint64
	RxDrop    uint64
	TxBytes   uint64
	TxPackets uint64
	TxErrs    uint64
	TxDrop    uint64
}

// CPUInfo holds static CPU model information from /proc/cpuinfo.
type CPUInfo struct {
	ModelName string
	Cores     int
	CacheSize string
}

// BuddyInfo holds one line of /proc/buddyinfo: the free-block count for each
// buddy order of a (node, zone) pair.
type BuddyInfo struct {
	Node   string
	Zone   string
	Orders []uint64 // free blocks per order (index 0 = order 0)
}

// Mount holds one /proc/mounts entry.
type Mount struct {
	Device     string
	MountPoint string
	Fstype     string
}

// PressureLine holds one PSI line (some/full) from /proc/pressure/{memory,cpu,io}.
// Avg10/Avg60/Avg300 are the % of time tasks were stalled over the last
// 10s/60s/300s; Total is the cumulative microseconds of stall.
type PressureLine struct {
	Avg10  float64
	Avg60  float64
	Avg300 float64
	Total  uint64
}

// Pressure holds the parsed "some" and "full" PSI lines for one resource.
type Pressure struct {
	Some PressureLine
	Full PressureLine
}

// tcpStateMap maps the /proc/net/tcp state code to its symbolic name.
var tcpStateMap = map[string]string{
	"01": "ESTABLISHED", "02": "SYN_SENT", "03": "SYN_RECV",
	"04": "FIN_WAIT1", "05": "FIN_WAIT2", "06": "TIME_WAIT",
	"07": "CLOSE", "08": "CLOSE_WAIT", "09": "LAST_ACK",
	"0A": "LISTEN", "0B": "CLOSING",
}

// Source is the typed interface for the /proc data source. Collectors depend
// on this; the default singleton (Default) is a *defaultSource whose root
// path can be swapped via SetRoot for testing.
type Source interface {
	Stat() (*Stat, error)
	Loadavg() (*Loadavg, error)
	Meminfo() (map[string]uint64, error)
	Diskstats() (map[string]DiskStat, error)
	NetDev() (map[string]NetDevStat, error)
	Vmstat() (map[string]uint64, error)
	Cpuinfo() (*CPUInfo, error)
	Buddyinfo() ([]BuddyInfo, error)
	Mounts() ([]Mount, error)
	// NetTCPStates returns the count of TCP connections grouped by state
	// name (e.g. "ESTABLISHED"). Reads /proc/net/tcp and /proc/net/tcp6.
	NetTCPStates() (map[string]int, error)
	// Pressure returns the PSI (Pressure Stall Information) for the given
	// resource ("memory", "cpu", or "io") from /proc/pressure/<resource>.
	// Returns nil, error if PSI is not enabled (file absent).
	Pressure(resource string) (*Pressure, error)
}

// defaultSource reads /proc files from a configurable root path.
type defaultSource struct {
	root string
}

// New returns a Source reading from the given root directory (e.g. "/proc"
// in production, or a testdata path in tests).
func New(root string) Source {
	return &defaultSource{root: root}
}

// root is the swappable root path of the singleton.
var root = "/proc"

// Default returns the process-wide Source singleton backed by the current
// root path (default "/proc").
func Default() Source {
	return &defaultSource{root: root}
}

// SetRoot redirects the Default singleton to read from the given root path.
// Used by tests to point at testdata fixtures.
func SetRoot(r string) {
	root = r
}

func (s *defaultSource) readFile(name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(s.root, name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *defaultSource) Stat() (*Stat, error) {
	f, err := os.Open(filepath.Join(s.root, "stat"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := &Stat{Cores: make(map[string]CPUStat)}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch {
		case strings.HasPrefix(fields[0], "cpu"):
			result.Cores[fields[0]] = parseCPUStatLine(fields[1:])
		case fields[0] == "ctxt":
			if v, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				result.ContextSwitches = v
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// parseCPUStatLine maps the numeric fields following a "cpu"/"cpuN" label
// into a CPUStat by position. Missing trailing fields (older kernels) are
// left zero.
func parseCPUStatLine(fields []string) CPUStat {
	vars := make([]uint64, len(fields))
	for i, f := range fields {
		v, _ := strconv.ParseUint(f, 10, 64)
		vars[i] = v
	}
	var st CPUStat
	switch len(vars) {
	case 0:
	case 1:
		st.User = vars[0]
	case 2:
		st.User, st.Nice = vars[0], vars[1]
	case 3:
		st.User, st.Nice, st.System = vars[0], vars[1], vars[2]
	case 4:
		st.User, st.Nice, st.System, st.Idle = vars[0], vars[1], vars[2], vars[3]
	case 5:
		st.User, st.Nice, st.System, st.Idle, st.Iowait = vars[0], vars[1], vars[2], vars[3], vars[4]
	case 6:
		st.User, st.Nice, st.System, st.Idle, st.Iowait, st.Irq = vars[0], vars[1], vars[2], vars[3], vars[4], vars[5]
	case 7:
		st.User, st.Nice, st.System, st.Idle, st.Iowait, st.Irq, st.Softirq = vars[0], vars[1], vars[2], vars[3], vars[4], vars[5], vars[6]
	case 8:
		st.User, st.Nice, st.System, st.Idle, st.Iowait, st.Irq, st.Softirq, st.Steal = vars[0], vars[1], vars[2], vars[3], vars[4], vars[5], vars[6], vars[7]
	case 9:
		st.User, st.Nice, st.System, st.Idle, st.Iowait, st.Irq, st.Softirq, st.Steal, st.Guest = vars[0], vars[1], vars[2], vars[3], vars[4], vars[5], vars[6], vars[7], vars[8]
	default:
		st.User, st.Nice, st.System, st.Idle, st.Iowait, st.Irq, st.Softirq, st.Steal, st.Guest, st.GuestNice = vars[0], vars[1], vars[2], vars[3], vars[4], vars[5], vars[6], vars[7], vars[8], vars[9]
	}
	return st
}

func (s *defaultSource) Loadavg() (*Loadavg, error) {
	data, err := s.readFile("loadavg")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(data)
	if len(fields) < 4 {
		return nil, nil
	}
	var la Loadavg
	la.One, _ = strconv.ParseFloat(fields[0], 64)
	la.Five, _ = strconv.ParseFloat(fields[1], 64)
	la.Fifteen, _ = strconv.ParseFloat(fields[2], 64)
	rtParts := strings.Split(fields[3], "/")
	if len(rtParts) == 2 {
		la.Running, _ = strconv.Atoi(rtParts[0])
		la.Total, _ = strconv.Atoi(rtParts[1])
	}
	return &la, nil
}

func (s *defaultSource) Meminfo() (map[string]uint64, error) {
	data, err := s.readFile("meminfo")
	if err != nil {
		return nil, err
	}
	result := make(map[string]uint64)
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.TrimSpace(parts[1])
		valStr = strings.TrimSuffix(valStr, "kB")
		valStr = strings.TrimSpace(valStr)
		val, err := strconv.ParseUint(valStr, 10, 64)
		if err != nil {
			continue
		}
		result[key] = val
	}
	return result, nil
}

func (s *defaultSource) Diskstats() (map[string]DiskStat, error) {
	f, err := os.Open(filepath.Join(s.root, "diskstats"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]DiskStat)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 11 {
			continue
		}
		dev := fields[2]
		result[dev] = DiskStat{
			ReadsCompleted:  parseUint(fields[3]),
			SectorsRead:     parseUint(fields[5]),
			WritesCompleted: parseUint(fields[7]),
			SectorsWritten:  parseUint(fields[9]),
			ReadTime:        parseUint(fields[6]),
			WriteTime:       parseUint(fields[10]),
		}
	}
	return result, scanner.Err()
}

func (s *defaultSource) NetDev() (map[string]NetDevStat, error) {
	f, err := os.Open(filepath.Join(s.root, "net", "dev"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]NetDevStat)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= 2 { // skip the two header lines
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
		result[iface] = NetDevStat{
			RxBytes:   parseUint(fields[0]),
			RxPackets: parseUint(fields[1]),
			RxErrs:    parseUint(fields[2]),
			RxDrop:    parseUint(fields[3]),
			TxBytes:   parseUint(fields[8]),
			TxPackets: parseUint(fields[9]),
			TxErrs:    parseUint(fields[10]),
			TxDrop:    parseUint(fields[11]),
		}
	}
	return result, scanner.Err()
}

func (s *defaultSource) Vmstat() (map[string]uint64, error) {
	data, err := s.readFile("vmstat")
	if err != nil {
		return nil, err
	}
	result := make(map[string]uint64)
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		result[fields[0]] = val
	}
	return result, nil
}

func (s *defaultSource) Cpuinfo() (*CPUInfo, error) {
	f, err := os.Open(filepath.Join(s.root, "cpuinfo"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var info CPUInfo
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "model name") && info.ModelName == "":
			info.ModelName = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		case strings.HasPrefix(line, "cpu cores") && info.Cores == 0:
			if v, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(line, ":", 2)[1])); err == nil {
				info.Cores = v
			}
		case strings.HasPrefix(line, "cache size"):
			info.CacheSize = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if info.ModelName == "" {
		return nil, nil
	}
	return &info, nil
}

func (s *defaultSource) Buddyinfo() ([]BuddyInfo, error) {
	f, err := os.Open(filepath.Join(s.root, "buddyinfo"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var result []BuddyInfo
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		nodeZone, rest, ok := strings.Cut(line, ",")
		if !ok {
			continue
		}
		fields := strings.Fields(nodeZone)
		var bi BuddyInfo
		if len(fields) >= 2 {
			bi.Node = fields[1]
		}
		restFields := strings.Fields(rest)
		if len(restFields) >= 2 {
			bi.Zone = restFields[1]
			for _, f := range restFields[2:] {
				v, _ := strconv.ParseUint(f, 10, 64)
				bi.Orders = append(bi.Orders, v)
			}
		}
		if bi.Node != "" {
			result = append(result, bi)
		}
	}
	return result, scanner.Err()
}

func (s *defaultSource) Mounts() ([]Mount, error) {
	f, err := os.Open(filepath.Join(s.root, "mounts"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var result []Mount
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		result = append(result, Mount{Device: fields[0], MountPoint: fields[1], Fstype: fields[2]})
	}
	return result, scanner.Err()
}

func (s *defaultSource) NetTCPStates() (map[string]int, error) {
	counts := make(map[string]int)
	for _, name := range []string{"net/tcp", "net/tcp6"} {
		f, err := os.Open(filepath.Join(s.root, name))
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
			if stateName, ok := tcpStateMap[fields[3]]; ok {
				counts[stateName]++
			}
		}
		f.Close()
	}
	return counts, nil
}

func parseUint(s string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v
}

func (s *defaultSource) Pressure(resource string) (*Pressure, error) {
	data, err := s.readFile(filepath.Join("pressure", resource))
	if err != nil {
		return nil, err
	}
	p := &Pressure{}
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		var dst *PressureLine
		switch fields[0] {
		case "some":
			dst = &p.Some
		case "full":
			dst = &p.Full
		default:
			continue
		}
		for _, kv := range fields[1:] {
			key, val, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			switch key {
			case "avg10":
				dst.Avg10, _ = strconv.ParseFloat(val, 64)
			case "avg60":
				dst.Avg60, _ = strconv.ParseFloat(val, 64)
			case "avg300":
				dst.Avg300, _ = strconv.ParseFloat(val, 64)
			case "total":
				dst.Total, _ = strconv.ParseUint(val, 10, 64)
			}
		}
	}
	return p, nil
}
