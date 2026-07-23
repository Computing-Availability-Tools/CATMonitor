// Package ipmi provides a data source that queries an external `ipmitool`
// command for sensor readings (SDR) and instantaneous power (DCMI).
//
// ipmitool is an expensive subprocess (IPMI bus round-trip, hundreds of
// milliseconds to seconds), so SDR results are cached for a configurable TTL
// (default 30s, decision D). A single cached SDR call serves the temperature,
// mem_temperature and power metrics, so the collector can call SDR() on its
// 3s cadence without spawning ipmitool more than once per cache window.
//
// exec uses exec.CommandContext with a 5s timeout to avoid a hung BMC bus
// blocking the collector goroutine.
//
// The source is a process-wide singleton (Default). Tests inject canned
// output via SetMockSDR / SetMockPower (decision B style).
package ipmi

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sensor holds one SDR reading parsed from `ipmitool sdr`.
type Sensor struct {
	Name   string  // sensor label, e.g. "CPU1 Temp"
	Value  float64 // numeric reading
	Unit   string  // "degrees C", "Watts", "", ...
	Status string  // "ok", "na", "ns", ...
}

// defaultCacheTTL is the SDR cache window (decision D = 30s).
const defaultCacheTTL = 30 * time.Second

// execTimeout caps how long a single ipmitool invocation may take.
const execTimeout = 5 * time.Second

// Source is the typed interface for the ipmi data source.
type Source interface {
	// SDR returns all sensors from `ipmitool sdr`. Cached for cacheTTL.
	SDR() ([]Sensor, error)
	// PowerReading returns the instantaneous system power (Watts) from
	// `ipmitool dcmi power reading`.
	PowerReading() (float64, error)
	// Available reports whether ipmitool is on PATH.
	Available() bool
}

// sdrFetcher returns the raw `ipmitool sdr` text. It is a swappable seam so
// tests can drive the cache path without a real ipmitool binary (consistent
// with decision B's swap-a-package-var style, not constructor injection).
type sdrFetcher = func() (string, error)

// realFetchSDR runs `ipmitool sdr` with a 5s timeout.
func realFetchSDR() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ipmitool", "sdr").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type defaultSource struct {
	mu        sync.Mutex
	cached    []Sensor
	cachedAt  time.Time
	cacheTTL  time.Duration
	fetchSDR  sdrFetcher // swappable; defaults to realFetchSDR
	mockPower string
}

var defaultSrc = &defaultSource{cacheTTL: defaultCacheTTL, fetchSDR: realFetchSDR}

func Default() Source { return defaultSrc }

// SetCacheTTL adjusts the SDR cache window of the singleton.
func SetCacheTTL(d time.Duration) { defaultSrc.cacheTTL = d }

// SetMockSDR injects canned `ipmitool sdr` output by swapping the fetcher to
// one that returns the mock text, and invalidating the cache so the next call
// re-fetches (from the mock).
func SetMockSDR(s string) {
	defaultSrc.fetchSDR = func() (string, error) { return s, nil }
	defaultSrc.cached = nil
	defaultSrc.cachedAt = time.Time{}
}

// ResetFetcher restores the real ipmitool fetcher and clears the cache.
// Used by tests to restore default singleton state.
func ResetFetcher() {
	defaultSrc.fetchSDR = realFetchSDR
	defaultSrc.cached = nil
	defaultSrc.cachedAt = time.Time{}
}

// SetMockPower injects canned `ipmitool dcmi power reading` output.
func SetMockPower(s string) { defaultSrc.mockPower = s }

func (s *defaultSource) Available() bool {
	_, err := exec.LookPath("ipmitool")
	return err == nil
}

func (s *defaultSource) SDR() ([]Sensor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Within the cache window, return the cached result without re-invoking
	// ipmitool. This caches BOTH successes and failures: a machine whose
	// ipmitool is installed but has no BMC fails fast, and without negative
	// caching the collector would exec ipmitool on every 3s cycle.
	if !s.cachedAt.IsZero() && time.Since(s.cachedAt) < s.cacheTTL {
		return s.cached, nil
	}
	out, err := s.fetchSDR()
	s.cachedAt = time.Now() // mark the attempt regardless of outcome
	if err != nil {
		s.cached = nil
		// Graceful: return no sensors and no error so the caller simply
		// produces no metrics (and doesn't log errors every cycle).
		return nil, nil
	}
	s.cached = parseSDR(out)
	return s.cached, nil
}

func (s *defaultSource) PowerReading() (float64, error) {
	text := s.mockPower
	if text == "" {
		ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ipmitool", "dcmi", "power", "reading").Output()
		if err != nil {
			return 0, err
		}
		text = string(out)
	}
	return parsePowerReading(text), nil
}

// parseSDR converts `ipmitool sdr` text output into a slice of Sensors.
// Each line is pipe-delimited: "<name> | <value> <unit> | <status>".
// Lines without 3 pipe fields are skipped.
func parseSDR(out string) []Sensor {
	var sensors []Sensor
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		reading := strings.Fields(parts[1])
		if len(reading) < 1 {
			continue
		}
		val, _ := strconv.ParseFloat(reading[0], 64)
		unit := strings.Join(reading[1:], " ")
		status := strings.TrimSpace(parts[2])
		sensors = append(sensors, Sensor{Name: name, Value: val, Unit: unit, Status: status})
	}
	return sensors
}

// parsePowerReading extracts the instantaneous power value (Watts) from
// `ipmitool dcmi power reading` output, e.g. a line beginning with
// "Instantaneous power reading:".
func parsePowerReading(text string) float64 {
	for _, line := range strings.Split(text, "\n") {
		l := strings.ToLower(line)
		if !strings.Contains(l, "instantaneous power reading") {
			continue
		}
		fields := strings.Fields(line)
		for _, f := range fields {
			if v, err := strconv.ParseFloat(strings.TrimRight(f, ","), 64); err == nil {
				return v
			}
		}
	}
	return 0
}
