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
	"strings"
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
	// Info returns parsed `smartctl -a /dev/<dev>` identity fields (model,
	// serial, firmware, size, interface). Cached per device for cacheTTL.
	Info(dev string) (*DiskInfo, error)
	// Available reports whether smartctl is on PATH.
	Available() bool
}

// DiskInfo holds the static identity of one block device parsed from
// `smartctl -a`. Fields are best-effort; absent fields are empty.
type DiskInfo struct {
	Model     string // Device Model / Model Number / Model Family fallback
	Serial    string
	Firmware  string
	Size      string // human-readable, e.g. "500 GB"
	Interface string // e.g. "SATA", "PCIe" / "NVMe"
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

func realInfoFetch(dev string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "smartctl", "-a", "/dev/"+dev).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type defaultSource struct {
	mu          sync.Mutex
	cache       map[string]string
	cachedAt    map[string]time.Time
	infoCache   map[string]string
	infoCachedAt map[string]time.Time
	cacheTTL    time.Duration
	fetch       fetcher
	infoFetch   fetcher
}

var defaultSrc = &defaultSource{
	cache:        make(map[string]string),
	cachedAt:     make(map[string]time.Time),
	infoCache:    make(map[string]string),
	infoCachedAt: make(map[string]time.Time),
	cacheTTL:     defaultCacheTTL,
	fetch:        realFetch,
	infoFetch:    realInfoFetch,
}

func Default() Source { return defaultSrc }

func SetCacheTTL(d time.Duration) { defaultSrc.cacheTTL = d }

func SetFetcher(f fetcher) {
	defaultSrc.fetch = f
	defaultSrc.cache = make(map[string]string)
	defaultSrc.cachedAt = make(map[string]time.Time)
}

// SetInfoFetcher swaps the smartctl -a fetcher for testing and clears the
// info cache.
func SetInfoFetcher(f fetcher) {
	defaultSrc.infoFetch = f
	defaultSrc.infoCache = make(map[string]string)
	defaultSrc.infoCachedAt = make(map[string]time.Time)
}

func ResetFetcher() {
	defaultSrc.fetch = realFetch
	defaultSrc.cache = make(map[string]string)
	defaultSrc.cachedAt = make(map[string]time.Time)
	defaultSrc.infoFetch = realInfoFetch
	defaultSrc.infoCache = make(map[string]string)
	defaultSrc.infoCachedAt = make(map[string]time.Time)
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

// Info runs `smartctl -a /dev/<dev>` (cached per device for cacheTTL) and
// returns parsed identity fields. On exec failure the result is negative-
// cached and nil is returned without error so the collector can degrade.
func (s *defaultSource) Info(dev string) (*DiskInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if at, ok := s.infoCachedAt[dev]; ok && !at.IsZero() && time.Since(at) < s.cacheTTL {
		return parseDiskInfo(s.infoCache[dev]), nil
	}
	out, err := s.infoFetch(dev)
	s.infoCachedAt[dev] = time.Now()
	if err != nil {
		s.infoCache[dev] = ""
		return nil, nil
	}
	s.infoCache[dev] = out
	return parseDiskInfo(out), nil
}

// parseDiskInfo extracts identity fields from `smartctl -a` output. It scans
// the INFORMATION SECTION lines. Field names vary by transport (SATA uses
// "Device Model", NVMe uses "Model Number"), so both are tried.
func parseDiskInfo(out string) *DiskInfo {
	var di DiskInfo
	hasModel := false
	for _, line := range strings.Split(out, "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "Device Model:") || strings.HasPrefix(trim, "Model Number:"):
			// Specific model wins over the generic Model Family.
			di.Model = strings.TrimSpace(strings.SplitN(trim, ":", 2)[1])
			hasModel = true
		case strings.HasPrefix(trim, "Model Family:"):
			// Only use the family when no specific model has been seen.
			if !hasModel {
				di.Model = strings.TrimSpace(strings.SplitN(trim, ":", 2)[1])
			}
		case strings.HasPrefix(trim, "Serial Number:"):
			di.Serial = strings.TrimSpace(strings.SplitN(trim, ":", 2)[1])
		case strings.HasPrefix(trim, "Firmware Version:"):
			di.Firmware = strings.TrimSpace(strings.SplitN(trim, ":", 2)[1])
		case strings.HasPrefix(trim, "User Capacity:"):
			di.Size = extractCapacity(trim)
		case strings.HasPrefix(trim, "SATA Version is"):
			di.Interface = "SATA"
		case strings.HasPrefix(trim, "PCIe") || strings.Contains(trim, "NVMe"):
			if di.Interface == "" {
				di.Interface = "PCIe"
			}
		}
	}
	return &di
}

// extractCapacity pulls the human-readable size in brackets from a line like
// "User Capacity:    500,107,862,016 bytes [500 GB]". Falls back to the raw
// field value when no bracket is present.
func extractCapacity(line string) string {
	if i := strings.IndexByte(line, '['); i >= 0 {
		if j := strings.IndexByte(line[i:], ']'); j > 0 {
			return strings.TrimSpace(line[i+1 : i+j])
		}
	}
	return strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
}
