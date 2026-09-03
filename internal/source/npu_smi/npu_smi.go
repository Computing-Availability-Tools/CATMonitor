// Package npu_smi provides a data source that wraps the `npu-smi info -t`
// subcommands (topology, HCCS bandwidth). It is exec-based (no CGo) and
// mirrors the ipmi/smartctl pattern: singleton, fetcher seam for tests,
// 5s exec timeout, per-method caching.
package npu_smi

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const execTimeout = 5 * time.Second

// HccsBw holds HCCS send/receive bandwidth parsed from
// `npu-smi info -t hccs-bw -i N -c 0 -time 50`.
type HccsBw struct {
	TxMB float64
	RxMB float64
}

// Source is the typed interface for the npu-smi data source.
type Source interface {
	// Topo returns the NPU communication topology string. Cached permanently
	// (static).
	Topo() (string, error)
	// HccsBandwidth returns HCCS TX/RX bandwidth (MB/s) for a device.
	// Not cached: the command samples live bandwidth (-time 50), so every
	// call executes npu-smi.
	HccsBandwidth(card, chip int) (*HccsBw, error)
	// Available reports whether npu-smi is on PATH.
	Available() bool
}

type fetcher = func(args ...string) (string, error)

func realFetch(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "npu-smi", args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type defaultSource struct {
	fetch   fetcher
	mock    string
	topoOnce sync.Once
	topoCache string
}

var defaultSrc = &defaultSource{fetch: realFetch}

func Default() Source { return defaultSrc }

func SetMock(f fetcher) {
	defaultSrc.fetch = f
	defaultSrc.topoOnce = sync.Once{}
	defaultSrc.topoCache = ""
}

func ResetFetcher() {
	defaultSrc.fetch = realFetch
	defaultSrc.topoOnce = sync.Once{}
	defaultSrc.topoCache = ""
}

func (s *defaultSource) Available() bool {
	_, err := exec.LookPath("npu-smi")
	return err == nil
}

func (s *defaultSource) Topo() (string, error) {
	var perr error
	s.topoOnce.Do(func() {
		out, err := s.fetch("info", "-t", "topo")
		if err != nil {
			perr = err
			return
		}
		s.topoCache = strings.TrimSpace(out)
	})
	return s.topoCache, perr
}

func (s *defaultSource) HccsBandwidth(card, chip int) (*HccsBw, error) {
	out, err := s.fetch("info", "-t", "hccs-bw", "-i", strconv.Itoa(card), "-c", strconv.Itoa(chip), "-time", "50")
	if err != nil {
		return nil, err
	}
	if strings.Contains(strings.ToLower(out), "does not support") {
		return nil, fmt.Errorf("npu-smi: device %d chip %d does not support hccs-bw", card, chip)
	}
	return parseHccsBw(out), nil
}

func parseHccsBw(out string) *HccsBw {
	bw := &HccsBw{}
	for _, line := range strings.Split(out, "\n") {
		l := strings.ToLower(line)
		if strings.Contains(l, "tx") {
			bw.TxMB = parseFirstFloat(line)
		}
		if strings.Contains(l, "rx") {
			bw.RxMB = parseFirstFloat(line)
		}
	}
	return bw
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
