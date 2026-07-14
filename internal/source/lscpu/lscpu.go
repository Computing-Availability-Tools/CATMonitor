// Package lscpu provides a data source that runs the external `lscpu` command
// and parses its output into a CPU topology struct.
//
// The topology is essentially static, so the first successful call to
// Topology() caches the result permanently and subsequent calls return the
// cached value without re-invoking lscpu (mirrors the project's
// "modelInfoCollected" startup-once caching idiom, decision A).
//
// The source is a process-wide singleton (Default). Tests inject mock output
// via SetMock.
package lscpu

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// Topology holds the static CPU topology parsed from lscpu.
type Topology struct {
	Cores         int               // logical CPU count ("CPU(s)")
	Sockets       int               // physical CPU / socket count ("Socket(s)")
	CoresPerSocket int              // ("Core(s) per socket")
	DiesPerSocket int               // ("Die(s) per socket", default 1)
	NumaNodes     []string          // NUMA node ids, e.g. ["0","1"]
	NumaCPU       map[string]string // node id -> cpu list string, e.g. "0"->"0-13"
}

// Source is the typed interface for the lscpu data source.
type Source interface {
	// Topology returns the cached CPU topology, running lscpu on first call.
	// Returns nil, error if lscpu is unavailable or parsing fails.
	Topology() (*Topology, error)
	// Available reports whether lscpu is on PATH.
	Available() bool
}

type defaultSource struct {
	once    sync.Once
	cached  *Topology
	mockOut string
}

var defaultSrc = &defaultSource{}

func Default() Source { return defaultSrc }

// SetMock injects canned lscpu output for testing (clears the cache so the
// next Topology() call re-parses the mock).
func SetMock(out string) {
	defaultSrc.mockOut = out
	defaultSrc.once = sync.Once{}
	defaultSrc.cached = nil
}

func (s *defaultSource) Available() bool {
	_, err := exec.LookPath("lscpu")
	return err == nil
}

func (s *defaultSource) Topology() (*Topology, error) {
	var perr error
	s.once.Do(func() {
		var out string
		if s.mockOut != "" {
			out = s.mockOut
		} else {
			o, err := exec.Command("lscpu").Output()
			if err != nil {
				perr = err
				return
			}
			out = string(o)
		}
		s.cached = parseLscpu(out)
	})
	if perr != nil {
		return nil, perr
	}
	return s.cached, nil
}

// parseLscpu converts lscpu text output into a Topology. Unknown/missing
// fields are left zero-valued (DiesPerSocket defaults to 1 when absent).
func parseLscpu(out string) *Topology {
	t := &Topology{DiesPerSocket: 1, NumaCPU: map[string]string{}}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(line, ":", 2)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSpace(fields[0])
		val := strings.TrimSpace(fields[1])
		switch key {
		case "CPU(s)":
			t.Cores, _ = strconv.Atoi(val)
		case "Socket(s)":
			t.Sockets, _ = strconv.Atoi(val)
		case "Core(s) per socket":
			t.CoresPerSocket, _ = strconv.Atoi(val)
		case "Die(s) per socket":
			if v, err := strconv.Atoi(val); err == nil && v > 0 {
				t.DiesPerSocket = v
			}
		case "NUMA node(s)":
			if v, err := strconv.Atoi(val); err == nil {
				for i := 0; i < v; i++ {
					t.NumaNodes = append(t.NumaNodes, strconv.Itoa(i))
				}
			}
		default:
			if strings.HasPrefix(key, "NUMA node") && strings.HasSuffix(key, " CPU(s)") {
				node := strings.TrimSuffix(strings.TrimPrefix(key, "NUMA node"), " CPU(s)")
				if node != "" {
					t.NumaCPU[node] = val
					if !contains(t.NumaNodes, node) {
						t.NumaNodes = append(t.NumaNodes, node)
					}
				}
			}
		}
	}
	return t
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
