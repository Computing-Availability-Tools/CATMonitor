// Package dmesg provides a data source that returns the kernel ring buffer
// text via the external `dmesg` command. It exists so the Memory collector's
// oom_count metric can read the kernel log through the source layer (instead
// of inline exec), consistent with the proc/sys/ipmi pattern.
//
// dmesg is an external subprocess and may need root (kernel.dmesg_restrict),
// so the result is cached for a configurable TTL (default 30s): multiple
// callers (e.g. oom now, mce later) within the window share one exec. A
// 5s timeout caps exec time. The source is a process-wide singleton; tests
// inject canned output via SetMock.
package dmesg

import (
	"context"
	"os/exec"
	"sync"
	"time"
)

const (
	defaultCacheTTL = 30 * time.Second
	execTimeout     = 5 * time.Second
)

// Source is the typed interface for the dmesg data source.
type Source interface {
	// Text returns the raw `dmesg` output. Cached for cacheTTL; a failing
	// exec is also cached so the collector does not re-spawn dmesg every
	// cycle (mirrors ipmi negative caching).
	Text() (string, error)
	// Available reports whether dmesg is on PATH. Note: read permission is
	// verified at call time, not here.
	Available() bool
}

// fetcher is a swappable seam returning raw dmesg text. Tests swap it to
// drive the cache path without the real dmesg binary (same idiom as ipmi).
type fetcher = func() (string, error)

func realFetch() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "dmesg").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type defaultSource struct {
	mu        sync.Mutex
	cached    string
	cachedAt  time.Time
	cacheTTL  time.Duration
	fetch     fetcher
	mockText  string
}

var defaultSrc = &defaultSource{cacheTTL: defaultCacheTTL, fetch: realFetch}

func Default() Source { return defaultSrc }

// SetCacheTTL adjusts the dmesg cache window of the singleton.
func SetCacheTTL(d time.Duration) { defaultSrc.cacheTTL = d }

// SetMock injects canned dmesg output (bypasses exec and cache; used by tests).
func SetMock(s string) {
	defaultSrc.mockText = s
	defaultSrc.cached = ""
	defaultSrc.cachedAt = time.Time{}
}

// ResetFetcher restores the real dmesg fetcher and clears the cache.
func ResetFetcher() {
	defaultSrc.fetch = realFetch
	defaultSrc.mockText = ""
	defaultSrc.cached = ""
	defaultSrc.cachedAt = time.Time{}
}

func (s *defaultSource) Available() bool {
	_, err := exec.LookPath("dmesg")
	return err == nil
}

func (s *defaultSource) Text() (string, error) {
	if s.mockText != "" {
		return s.mockText, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cachedAt.IsZero() && time.Since(s.cachedAt) < s.cacheTTL {
		return s.cached, nil
	}
	out, err := s.fetch()
	s.cachedAt = time.Now()
	if err != nil {
		// Negative cache: avoid re-spawning dmesg on every cycle when it
		// consistently fails (e.g. dmesg_restrict without root).
		s.cached = ""
		return "", nil
	}
	s.cached = out
	return out, nil
}
