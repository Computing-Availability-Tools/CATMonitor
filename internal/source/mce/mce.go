// Package mce provides a data source that counts Machine Check Exception (MCE)
// events from the kernel log, distinguishing Corrected (CE) and Uncorrected
// (UCE/UE) errors per CPU socket.
//
// It prefers /var/log/mcelog (maintained by the mcelog daemon) and falls back
// to `dmesg` when mcelog is absent. CE/UCE here are CPU-level hardware errors
// (MCA), distinct from the Memory collector's EDAC ecc_ce_errors (memory ECC).
//
// The source is a process-wide singleton (Default). Tests inject mock log
// text via SetMock. No caching: each call reads the latest log (decision C —
// deltas are computed by the collector, not the source).
package mce

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Event holds the count of MCE events of one kind on one CPU socket.
type Event struct {
	Socket string // socket id, e.g. "0"
	Kind   string // "CE" or "UCE"
	Count  uint64
}

// Source is the typed interface for the mce data source.
type Source interface {
	// Events returns the cumulative MCE counts per (socket, kind) found in the
	// current log text.
	Events() ([]Event, error)
	// Available reports whether mcelog or dmesg is reachable.
	Available() bool
}

type defaultSource struct {
	mockLog string
}

var defaultSrc = &defaultSource{}

func Default() Source { return defaultSrc }

// SetMock injects canned log text (dmesg-style) for testing.
func SetMock(s string) { defaultSrc.mockLog = s }

const mcelogPath = "/var/log/mcelog"

func (s *defaultSource) Available() bool {
	if _, err := os.Stat(mcelogPath); err == nil {
		return true
	}
	if _, err := exec.LookPath("dmesg"); err == nil {
		return true
	}
	return false
}

func (s *defaultSource) Events() ([]Event, error) {
	text, err := s.readLog()
	if err != nil {
		return nil, err
	}
	return parseMCE(text), nil
}

func (s *defaultSource) readLog() (string, error) {
	if s.mockLog != "" {
		return s.mockLog, nil
	}
	if data, err := os.ReadFile(mcelogPath); err == nil {
		return string(data), nil
	}
	out, err := exec.Command("dmesg").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// parseMCE scans log lines for Machine Check events, classifies each as CE
// (corrected) or UCE (uncorrectable/fatal), extracts the CPU socket, and
// returns aggregated counts per (socket, kind).
func parseMCE(text string) []Event {
	type key struct{ socket, kind string }
	agg := map[key]uint64{}

	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "Machine Check") {
			continue
		}
		socket := extractSocket(line)
		var kind string
		switch {
		case strings.Contains(line, "uncorrect"),
			strings.Contains(line, "Uncorrect"),
			strings.Contains(line, "fatal"),
			strings.Contains(line, "Fatal"):
			kind = "UCE"
		case strings.Contains(line, "corrected"),
			strings.Contains(line, "Corrected"):
			kind = "CE"
		default:
			continue
		}
		agg[key{socket, kind}]++
	}

	var events []Event
	for k, v := range agg {
		events = append(events, Event{Socket: k.socket, Kind: k.kind, Count: v})
	}
	return events
}

// extractSocket finds the first "CPU <n>" token in a line and returns "<n>".
// Returns "0" if no CPU token is found (a reasonable default for single-socket).
func extractSocket(line string) string {
	fields := strings.Fields(line)
	for i, f := range fields {
		if (strings.EqualFold(f, "CPU") || f == "CPU") && i+1 < len(fields) {
			n := strings.TrimRight(fields[i+1], ",:;")
			if _, err := strconv.Atoi(n); err == nil {
				return n
			}
		}
	}
	return "0"
}
