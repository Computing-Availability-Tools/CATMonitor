// Package hccn_tool provides a data source that wraps the `hccn_tool` command
// for NPU network diagnostics (bandwidth, RoCE speed/link, NIC statistics).
// It is exec-based (no CGo): singleton, fetcher seam, 5s exec timeout.
// Caching is stale-while-revalidate (SWR) with a 6s TTL: expired reads return
// the stale value immediately and trigger a single background refresh, so
// callers never block on an exec after the initial cold start. Safe for
// concurrent use: the mutex only guards the cache maps; fetches run outside
// the lock so device-parallel callers do not serialize.
package hccn_tool

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	execTimeout = 5 * time.Second
	cacheTTL    = 6 * time.Second
)

// Bandwidth holds parsed bandwidth (MB/s) from `hccn_tool -i N -bandwidth -g`.
type Bandwidth struct {
	NetTX  float64
	NetRX  float64
	PcieTX float64
	PcieRX float64
}

// Source is the typed interface for the hccn_tool data source. All reads are
// served from an SWR cache (see package comment); methods block only on the
// initial cold start of each (device, command) pair.
type Source interface {
	Bandwidth(devID int) (*Bandwidth, error)
	Speed(devID int) (string, error)
	Link(devID int) (string, error)
	Statistics(devID int) (map[string]uint64, error)
	Available() bool
}

type fetcher = func(devID int, opt string) (string, error)

func realFetch(devID int, opt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "hccn_tool", "-i", strconv.Itoa(devID), opt, "-g").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type defaultSource struct {
	fetch    fetcher
	mu       sync.Mutex
	cache    map[string]string // key: "devID:opt"
	at       map[string]time.Time
	inflight map[string]bool // key → a background SWR refresh is running
	gen      uint64          // bumped on reset; in-flight refreshes discard their writes
}

var defaultSrc = &defaultSource{
	fetch:    realFetch,
	cache:    make(map[string]string),
	at:       make(map[string]time.Time),
	inflight: make(map[string]bool),
}

func Default() Source { return defaultSrc }

func SetMock(f fetcher) {
	defaultSrc.mu.Lock()
	defer defaultSrc.mu.Unlock()
	defaultSrc.fetch = f
	defaultSrc.resetLocked()
}

func ResetFetcher() {
	defaultSrc.mu.Lock()
	defer defaultSrc.mu.Unlock()
	defaultSrc.fetch = realFetch
	defaultSrc.resetLocked()
}

// resetLocked swaps the cache state and invalidates refreshes that are still
// in flight (they check gen on completion and discard their writes). Callers
// must hold s.mu.
func (s *defaultSource) resetLocked() {
	s.gen++
	s.cache = make(map[string]string)
	s.at = make(map[string]time.Time)
	s.inflight = make(map[string]bool)
}

func (s *defaultSource) Available() bool {
	_, err := exec.LookPath("hccn_tool")
	return err == nil
}

// cached returns the cached output for (devID, opt). Fresh entries return
// immediately. Expired entries are served stale while a single background
// refresh runs (stale-while-revalidate), so callers never block on an exec
// after cold start. Cold starts (no cached value yet) fetch synchronously —
// the only blocking path, paid once per key at daemon start.
func (s *defaultSource) cached(devID int, opt string) (string, error) {
	key := strconv.Itoa(devID) + ":" + opt

	s.mu.Lock()
	if at, ok := s.at[key]; ok {
		out := s.cache[key]
		if time.Since(at) < cacheTTL {
			s.mu.Unlock()
			return out, nil
		}
		// Expired (SWR): serve the stale value now; spawn one background
		// refresh unless one is already in flight for this key.
		if !s.inflight[key] {
			s.inflight[key] = true
			gen := s.gen
			s.mu.Unlock()
			go s.refresh(devID, opt, key, gen)
		} else {
			s.mu.Unlock()
		}
		return out, nil
	}
	gen := s.gen
	s.mu.Unlock()

	// Cold start: no value to serve — fetch synchronously.
	out, err := s.fetch(devID, opt)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	if s.gen == gen {
		s.cache[key] = out
		s.at[key] = time.Now()
	}
	s.mu.Unlock()
	return out, nil
}

// refresh performs one background SWR refresh for key. At most one refresh
// goroutine per key runs at a time (guarded by s.inflight). On failure the
// stale value is kept and the next expired read retries.
func (s *defaultSource) refresh(devID int, opt, key string, gen uint64) {
	out, err := s.fetch(devID, opt)
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen {
		return // source was reset mid-flight: discard the write
	}
	if err == nil {
		s.cache[key] = out
		s.at[key] = time.Now()
	}
	delete(s.inflight, key)
}

func (s *defaultSource) Bandwidth(devID int) (*Bandwidth, error) {
	out, err := s.cached(devID, "-bandwidth")
	if err != nil {
		return nil, err
	}
	return parseBandwidth(out), nil
}

func (s *defaultSource) Speed(devID int) (string, error) {
	out, err := s.cached(devID, "-speed")
	if err != nil {
		return "", err
	}
	return parseValue(out, "Speed:"), nil
}

func (s *defaultSource) Link(devID int) (string, error) {
	out, err := s.cached(devID, "-link")
	if err != nil {
		return "", err
	}
	return parseValue(out, "link status"), nil
}

func (s *defaultSource) Statistics(devID int) (map[string]uint64, error) {
	out, err := s.cached(devID, "-stat")
	if err != nil {
		return nil, err
	}
	return parseStatistics(out), nil
}

func parseBandwidth(out string) *Bandwidth {
	bw := &Bandwidth{}
	for _, line := range strings.Split(out, "\n") {
		l := strings.ToLower(line)
		switch {
		case strings.Contains(l, "net") && strings.Contains(l, "tx"):
			bw.NetTX = parseFirstFloat(line)
		case strings.Contains(l, "net") && strings.Contains(l, "rx"):
			bw.NetRX = parseFirstFloat(line)
		case strings.Contains(l, "pcie") && strings.Contains(l, "tx"):
			bw.PcieTX = parseFirstFloat(line)
		case strings.Contains(l, "pcie") && strings.Contains(l, "rx"):
			bw.PcieRX = parseFirstFloat(line)
		}
	}
	return bw
}

func parseValue(out, prefix string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(strings.ToLower(line), strings.ToLower(prefix)) {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func parseFirstFloat(line string) float64 {
	fields := strings.Fields(line)
	for _, f := range fields {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			return v
		}
	}
	return 0
}

// parseStatistics parses the output of `hccn_tool -i N -stat -g` into a
// map of metric_name → cumulative counter value. The output format is:
//
//	packet statistics:
//	mac_tx_mac_pause_num:0
//	mac_rx_mac_pause_num:0
//	...
//
// The header line is skipped; remaining lines are split on the first ':'.
func parseStatistics(out string) map[string]uint64 {
	result := make(map[string]uint64)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "statistics:") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		v, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			continue
		}
		result[name] = v
	}
	return result
}
