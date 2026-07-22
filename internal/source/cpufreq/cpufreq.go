// Package cpufreq provides a data source that reads and writes the Linux
// cpufreq sysfs interface under /sys/devices/system/cpu/<core>/cpufreq/.
//
// Unlike proc/sys which are read-only data sources, cpufreq is an *actuator*
// source: in addition to reading cpuinfo_min_freq / scaling_min_freq etc. it
// writes scaling_min_freq / scaling_max_freq to pin core frequencies. Writes
// require root (CAP_SYS_ADMIN); on virtualized hosts without a cpufreq driver
// the source reports Available()==false and the caller no-ops.
//
// The source is a process-wide singleton (Default) backed by a swappable
// root path (SetRoot) for testing. Feature-layer callers (cpugov) receive a
// Source via DI so tests inject a MockSource without touching the singleton.
package cpufreq

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Source is the typed interface for the cpufreq data source.
type Source interface {
	// Available reports whether any online core exposes a readable
	// cpuinfo_min_freq (i.e. the host has a cpufreq driver). Virtualized
	// guests / WSL usually return false.
	Available() bool
	// Cores returns online core names that own a cpufreq directory
	// ("cpu0", "cpu1", ...). Order matches directory listing.
	Cores() ([]string, error)
	// InfoMinFreq returns the hardware minimum frequency (kHz) read from
	// cpuinfo_min_freq of the first available core. Static value.
	InfoMinFreq() (uint64, error)
	// InfoMaxFreq returns the hardware maximum frequency (kHz) read from
	// cpuinfo_max_freq of the first available core. Static value.
	InfoMaxFreq() (uint64, error)
	// CurMinFreq returns the current scaling_min_freq (kHz) of a core.
	CurMinFreq(core string) (uint64, error)
	// CurMaxFreq returns the current scaling_max_freq (kHz) of a core.
	CurMaxFreq(core string) (uint64, error)
	// CurFreq returns the current scaling_cur_freq (kHz) of a core
	// (the live frequency the core is running at). Read-only, for
	// observability of whether the pin actually took effect.
	CurFreq(core string) (uint64, error)
	// Governor returns the current scaling_governor of a core.
	Governor(core string) (string, error)
	// SetMinFreq writes scaling_min_freq (kHz) for a core.
	SetMinFreq(core string, kHz uint64) error
	// SetMaxFreq writes scaling_max_freq (kHz) for a core.
	SetMaxFreq(core string, kHz uint64) error
	// SetGovernor writes scaling_governor for a core.
	SetGovernor(core string, gov string) error
}

type defaultSource struct {
	root string
}

// cpuBase is the sub-path under root for the cpu device tree.
const cpuBase = "devices/system/cpu"

var root = "/sys"

// New returns a Source rooted at r (used for tests / SetRoot indirection).
func New(r string) Source { return &defaultSource{root: r} }

// Default returns the process-wide source backed by /sys.
func Default() Source { return &defaultSource{root: root} }

// SetRoot redirects the Default() source's root path (test seam).
func SetRoot(r string) { root = r }

func (s *defaultSource) cpuDir() string { return filepath.Join(s.root, cpuBase) }

// Cores lists online cores that own a cpufreq directory. "cpuidle" and
// "cpufreq" global entries under the cpu tree are skipped.
func (s *defaultSource) Cores() ([]string, error) {
	entries, err := os.ReadDir(s.cpuDir())
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, "cpu") {
			continue
		}
		if name == "cpuidle" || name == "cpufreq" {
			continue
		}
		// Skip aggregate "cpu" (no core number) — it has no cpufreq dir of
		// its own; per-core entries are "cpu0", "cpu1", ...
		if name == "cpu" {
			continue
		}
		fi, err := os.Stat(filepath.Join(s.cpuDir(), name, "cpufreq"))
		if err != nil || !fi.IsDir() {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

// Available reports whether Cores() is non-empty and the first core exposes a
// readable cpuinfo_min_freq. Cheap enough to call every control tick.
func (s *defaultSource) Available() bool {
	cores, err := s.Cores()
	if err != nil || len(cores) == 0 {
		return false
	}
	_, err = s.readCoreFreq(cores[0], "cpuinfo_min_freq")
	return err == nil
}

func (s *defaultSource) InfoMinFreq() (uint64, error) {
	cores, err := s.Cores()
	if err != nil {
		return 0, err
	}
	if len(cores) == 0 {
		return 0, os.ErrNotExist
	}
	return s.readCoreFreq(cores[0], "cpuinfo_min_freq")
}

func (s *defaultSource) InfoMaxFreq() (uint64, error) {
	cores, err := s.Cores()
	if err != nil {
		return 0, err
	}
	if len(cores) == 0 {
		return 0, os.ErrNotExist
	}
	return s.readCoreFreq(cores[0], "cpuinfo_max_freq")
}

func (s *defaultSource) CurMinFreq(core string) (uint64, error) {
	return s.readCoreFreq(core, "scaling_min_freq")
}

func (s *defaultSource) CurMaxFreq(core string) (uint64, error) {
	return s.readCoreFreq(core, "scaling_max_freq")
}

func (s *defaultSource) CurFreq(core string) (uint64, error) {
	return s.readCoreFreq(core, "scaling_cur_freq")
}

func (s *defaultSource) Governor(core string) (string, error) {
	p := filepath.Join(s.cpuDir(), core, "cpufreq", "scaling_governor")
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (s *defaultSource) SetMinFreq(core string, kHz uint64) error {
	return s.writeCoreFreq(core, "scaling_min_freq", kHz)
}

func (s *defaultSource) SetMaxFreq(core string, kHz uint64) error {
	return s.writeCoreFreq(core, "scaling_max_freq", kHz)
}

func (s *defaultSource) SetGovernor(core string, gov string) error {
	p := filepath.Join(s.cpuDir(), core, "cpufreq", "scaling_governor")
	return os.WriteFile(p, []byte(gov+"\n"), 0644)
}

// readCoreFreq reads a single kHz-valued cpufreq file for a core and parses
// the leading integer (whitespace-trimmed).
func (s *defaultSource) readCoreFreq(core, name string) (uint64, error) {
	p := filepath.Join(s.cpuDir(), core, "cpufreq", name)
	data, err := os.ReadFile(p)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// writeCoreFreq writes a kHz value to a cpufreq file. A trailing newline is
// appended to mirror `echo N > file` behavior (kernel cpufreq parsers accept
// optional trailing whitespace). On real /sys the file mode is owned by the
// kernel; the 0644 mode only applies to test fixtures on a regular fs.
func (s *defaultSource) writeCoreFreq(core, name string, kHz uint64) error {
	p := filepath.Join(s.cpuDir(), core, "cpufreq", name)
	return os.WriteFile(p, []byte(strconv.FormatUint(kHz, 10)+"\n"), 0644)
}
