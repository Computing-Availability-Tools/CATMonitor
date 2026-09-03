// Package hccn_tool provides a data source that wraps the `hccn_tool` command
// for NPU network diagnostics (bandwidth, RoCE speed/link). It is exec-based
// (no CGo): singleton, fetcher seam, 5s timeout, per-device 30s cache.
// Safe for concurrent use: the mutex only guards the cache maps, and fetches
// run outside the lock so device-parallel callers do not serialize on execs.
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
	cacheTTL    = 30 * time.Second
)

// Bandwidth holds parsed bandwidth (MB/s) from `hccn_tool -i N -bandwidth -g`.
type Bandwidth struct {
	NetTX  float64
	NetRX  float64
	PcieTX float64
	PcieRX float64
}

// Source is the typed interface for the hccn_tool data source.
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
	fetch  fetcher
	mu     sync.Mutex
	cache  map[string]string // key: "devID:opt"
	at     map[string]time.Time
}

var defaultSrc = &defaultSource{
	fetch: realFetch,
	cache: make(map[string]string),
	at:    make(map[string]time.Time),
}

func Default() Source { return defaultSrc }

func SetMock(f fetcher) {
	defaultSrc.fetch = f
	defaultSrc.cache = make(map[string]string)
	defaultSrc.at = make(map[string]time.Time)
}

func ResetFetcher() {
	defaultSrc.fetch = realFetch
	defaultSrc.cache = make(map[string]string)
	defaultSrc.at = make(map[string]time.Time)
}

func (s *defaultSource) Available() bool {
	_, err := exec.LookPath("hccn_tool")
	return err == nil
}

// cached returns the cached output for (devID, opt), executing the fetch on
// miss. The mutex only guards the shared cache maps — the fetch itself runs
// WITHOUT the lock: callers are device-parallel goroutines with distinct
// cache keys (phyID:opt), so serializing execs would kill that parallelism
// while the same-key dedup benefit never materializes.
func (s *defaultSource) cached(devID int, opt string) (string, error) {
	key := strconv.Itoa(devID) + ":" + opt
	s.mu.Lock()
	if at, ok := s.at[key]; ok && time.Since(at) < cacheTTL {
		out := s.cache[key]
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()

	out, err := s.fetch(devID, opt)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.cache[key] = out
	s.at[key] = time.Now()
	s.mu.Unlock()
	return out, nil
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
