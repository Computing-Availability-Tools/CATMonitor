// Package lspci provides a data source that runs the external `lspci` command
// and caches PCI device descriptions. The output is static, so the first
// successful call caches all results permanently.
//
// The source is a process-wide singleton (Default). If lspci is not on PATH,
// all queries return empty strings (graceful degradation).
package lspci

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Source is the typed interface for the lspci data source.
type Source interface {
	// Available reports whether lspci is on PATH.
	Available() bool
	// Description returns the device description for a PCI address
	// (e.g. "0000:c1:00.0" -> "Mellanox Technologies MT28908 [ConnectX-6]").
	// Returns "" if lspci is unavailable or the address is not found.
	Description(pciAddr string) string
}

type defaultSource struct {
	once   sync.Once
	cache  map[string]string
}

var defaultSrc = &defaultSource{}

func Default() Source { return defaultSrc }

func (s *defaultSource) Available() bool {
	_, err := exec.LookPath("lspci")
	return err == nil
}

func (s *defaultSource) Description(pciAddr string) string {
	if pciAddr == "" {
		return ""
	}
	s.once.Do(s.load)
	if desc, ok := s.cache[pciAddr]; ok {
		return desc
	}
	// lspci output may omit the domain prefix "0000:", try without it.
	if strings.HasPrefix(pciAddr, "0000:") {
		if desc, ok := s.cache[pciAddr[5:]]; ok {
			return desc
		}
	}
	return ""
}

// load runs `lspci` once and parses all lines into a map of pciAddr -> description.
// lspci output format:
//   0000:c1:00.0 Ethernet controller: Mellanox Technologies MT28908 [ConnectX-6]
//   0000:c4:00.0 Ethernet controller: Intel Corporation X710 10GbE NIC
// The description is everything after the class name (the second colon-separated field).
func (s *defaultSource) load() {
	s.cache = make(map[string]string)
	if !s.Available() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lspci", "-q").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Split into address and rest: "0000:c1:00.0 Ethernet controller: description"
		// The address is everything before the first space.
		spaceIdx := strings.Index(line, " ")
		if spaceIdx < 0 {
			continue
		}
		addr := line[:spaceIdx]
		rest := line[spaceIdx+1:]
		// The rest is "Class: Description" — extract the description after the class.
		colonIdx := strings.Index(rest, ": ")
		if colonIdx < 0 {
			s.cache[addr] = rest
		} else {
			s.cache[addr] = strings.TrimSpace(rest[colonIdx+2:])
		}
	}
}
