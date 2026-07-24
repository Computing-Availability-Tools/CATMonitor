// Package dataparse parses Ascend PyTorch Profiler Level0 SQLite databases
// into per-device CSV + JSON intermediate files.
package dataparse

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Data structures
// ---------------------------------------------------------------------------

// StepTime represents a single profiler step time window.
type StepTime struct {
	ID      int
	StartNs int
	EndNs   int
}

// CommunicationOp stores parsed data for a single communication operator.
type CommunicationOp struct {
	OpStreamIndex int
	OpName        int
	StartNs       int
	EndNs         int
	HStartNs      int
	HEndNs        int
	Count         int
	ConnectionID  int
	DomainID      int
}

// PerformanceMetrics collects all metrics for one device snapshot.
type PerformanceMetrics struct {
	StepIndex    int
	StepDuration int
	ZPDevice     int
	ZPDuration   int
	ZPHost       int
	ZPBubble     int
	ZPCount      int
	ZPKernel     int
	DataLoader   int
	Durations    map[string]int
	Counts       map[string]int
}

// OpStat pairs a duration with a count.
type OpStat struct {
	Duration int
	Count    int
}

// Interval is a [Start, End] time range.
type Interval struct {
	Start int
	End   int
}

// HostOp stores host-side operation timing.
type HostOp struct {
	StartNs int
	EndNs   int
}

// ---------------------------------------------------------------------------
// Sentinel
// ---------------------------------------------------------------------------

const invalidData = -99999

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// CalculateMean computes the integer mean of a slice of ints, filtering out
// negative values.
func CalculateMean(values []int) (int, error) {
	var sum int
	var count int
	for _, v := range values {
		if v >= 0 {
			sum += v
			count++
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("no valid values for mean calculation")
	}
	return int(math.Round(float64(sum) / float64(count))), nil
}

// CalculateMidMeanPair computes the mid-mean duration and count from a slice
// of OpStat entries (discards the single largest and smallest by duration).
func CalculateMidMeanPair(stats []OpStat) (int, int, error) {
	if len(stats) == 0 {
		return 0, 0, fmt.Errorf("empty OpStat slice")
	}

	if len(stats) == 1 {
		return stats[0].Duration, stats[0].Count, nil
	}

	sort.Slice(stats, func(i, j int) bool { return stats[i].Duration < stats[j].Duration })

	trimmed := stats[1 : len(stats)-1]
	if len(trimmed) == 0 {
		trimmed = stats
	}

	var sumDuration, sumCount int
	for _, s := range trimmed {
		sumDuration += s.Duration
		sumCount += s.Count
	}
	meanDuration := int(math.Round(float64(sumDuration) / float64(len(trimmed))))
	meanCount := int(math.Round(float64(sumCount) / float64(len(trimmed))))
	return meanDuration, meanCount, nil
}

// mergeIntervalsSimple computes the total duration covered by a set of
// intervals, merging overlapping ranges.
func mergeIntervalsSimple(intervals []Interval) int {
	if len(intervals) == 0 {
		return 0
	}

	sort.Slice(intervals, func(i, j int) bool { return intervals[i].Start < intervals[j].Start })

	total := 0
	currentEnd := intervals[0].Start

	for _, iv := range intervals {
		if iv.Start > currentEnd {
			total += iv.End - iv.Start
			currentEnd = iv.End
		} else if iv.End > currentEnd {
			total += iv.End - currentEnd
			currentEnd = iv.End
		}
	}
	return total
}

func placeholders(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

func extractGlobalRankFromFilename(filePath string) (string, error) {
	base := filePath
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, "\\"); idx >= 0 {
		base = base[idx+1:]
	}

	const prefix = "ascend_pytorch_profiler_"
	const suffix = ".db"

	if !strings.HasPrefix(base, prefix) || !strings.HasSuffix(base, suffix) {
		return "", fmt.Errorf("filename %q does not match pattern ascend_pytorch_profiler_*.db", base)
	}

	rank := base[len(prefix) : len(base)-len(suffix)]
	if rank == "" {
		return "", fmt.Errorf("could not extract rank from filename %q", base)
	}
	return rank, nil
}
