// Package smartctl provides a data source that wraps the external `smartctl`
// command (from smartmontools) to query disk SMART health.
//
// smartctl is an expensive subprocess, so results are cached per device for
// a configurable TTL (default 60s). A failing exec is also cached (negative
// cache) so the collector does not re-spawn smartctl every cycle when no
// smartmontools or no permission is available. The source is a process-wide
// singleton; tests inject a fake fetcher via SetFetcher.
package smartctl

import (
	"context"
	"os/exec"
	"sync"
	"time"
)

const (
	defaultCacheTTL = 60 * time.Second
	execTimeout     = 5 * time.Second
)

// Source is the typed interface for the smartctl data source.
type Source interface {
	// Health returns the raw `smartctl -H /dev/<dev>` output for the given
	// device name (e.g. "sda"). Cached per device for cacheTTL.
	Health(dev string) (string, error)
	// Available reports whether smartctl is on PATH.
	Available() bool
}

// fetcher is a swappable seam returning the raw smartctl -H output for a
// device. Tests swap it to drive the cache path without the real binary.
type fetcher = func(dev string) (string, error)

func realFetch(dev string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "smartctl", "-H", "/dev/"+dev).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type defaultSource struct {
	mu        sync.Mutex
	cache     map[string]string
	cachedAt  map[string]time.Time
	cacheTTL  time.Duration
	fetch     fetcher
}

var defaultSrc = &defaultSource{
	cache:    make(map[string]string),
	cachedAt: make(map[string]time.Time),
	cacheTTL: defaultCacheTTL,
	fetch:    realFetch,
}

func Default() Source { return defaultSrc }

func SetCacheTTL(d time.Duration) { defaultSrc.cacheTTL = d }

func SetFetcher(f fetcher) {
	defaultSrc.fetch = f
	defaultSrc.cache = make(map[string]string)
	defaultSrc.cachedAt = make(map[string]time.Time)
}

func ResetFetcher() {
	defaultSrc.fetch = realFetch
	defaultSrc.cache = make(map[string]string)
	defaultSrc.cachedAt = make(map[string]time.Time)
}

func (s *defaultSource) Available() bool {
	_, err := exec.LookPath("smartctl")
	return err == nil
}

func (s *defaultSource) Health(dev string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if at, ok := s.cachedAt[dev]; ok && !at.IsZero() && time.Since(at) < s.cacheTTL {
		return s.cache[dev], nil
	}
	out, err := s.fetch(dev)
	s.cachedAt[dev] = time.Now()
	if err != nil {
		// Negative cache: avoid re-spawning smartctl every cycle.
		s.cache[dev] = ""
		return "", nil
	}
	s.cache[dev] = out
	return out, nil
}
