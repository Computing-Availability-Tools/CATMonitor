package resource

import (
	"fmt"
	"math"
	"os"
	"sort"
)

// =============================================================================
// 1-Minute Trimmed-Mean Aggregation
// =============================================================================

// AggregateByMinute groups raw rows into 1-minute buckets and computes a single
// aggregated row per minute.
//
// Continuous metrics (TEMP, POWER, FREQ, UTIL, BANDWIDTH, NIC_RX):
//   sort → trim top/bottom 25% → mean of middle 50% (midmean)
//
// Counter metrics (ERR_PKT, RETRY, OUT_OF_ORDER, PFC_PKT):
//   per-card increment within the 1-minute bucket (handles counter wrap)
func AggregateByMinute(rawRows []CSVRow, cardIDs []int, cfg DetectionConfig) ([]CSVRow, error) {
	if len(rawRows) == 0 {
		return nil, fmt.Errorf("no rows to aggregate")
	}

	windowSec := int64(cfg.AggregationWindowSec)
	if windowSec <= 0 {
		windowSec = 60
	}

	// Group rows by minute bucket.
	buckets := make(map[int64][]CSVRow)
	for _, row := range rawRows {
		bucketTS := (row.Timestamp / windowSec) * windowSec
		buckets[bucketTS] = append(buckets[bucketTS], row)
	}

	// Sort bucket timestamps.
	bucketTSs := make([]int64, 0, len(buckets))
	for ts := range buckets {
		bucketTSs = append(bucketTSs, ts)
	}
	sort.Slice(bucketTSs, func(i, j int) bool { return bucketTSs[i] < bucketTSs[j] })

	// Aggregate each bucket into one row.
	aggregated := make([]CSVRow, 0, len(buckets))
	for _, bucketTS := range bucketTSs {
		bucket := buckets[bucketTS]
		row, err := aggregateBucket(bucket, bucketTS, cardIDs, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] [WARN] skipping bucket %d: %v\n", bucketTS, err)
			continue
		}
		aggregated = append(aggregated, row)
	}

	if len(aggregated) == 0 {
		return nil, fmt.Errorf("no valid aggregated rows after processing %d buckets", len(buckets))
	}

	return aggregated, nil
}

// aggregateBucket computes one aggregated row from all samples in a 1-minute bucket.
func aggregateBucket(bucket []CSVRow, bucketTS int64, cardIDs []int, cfg DetectionConfig) (CSVRow, error) {
	row := CSVRow{
		Timestamp: bucketTS,
		CPUAvg:    bucket[len(bucket)-1].CPUAvg, // take last CPU value
	}

	// Per-card: for each metric, collect all sample values in the bucket.
	for _, metric := range AllMetrics {
		cardVals := make(map[int][]float64)
		for _, r := range bucket {
			dict := getMetricDict(r, metric)
			if dict == nil {
				continue
			}
			for cid, val := range dict {
				cardVals[cid] = append(cardVals[cid], val)
			}
		}

		aggregatedVals := make(map[int]float64)
		for cid, vals := range cardVals {
			if len(vals) == 0 {
				continue
			}
			if IsCounterMetric(metric) {
				// Counter metric: compute increment (last - first), handle wrap.
				aggregatedVals[cid] = counterDelta(vals)
			} else {
				// Continuous metric: trimmed mean.
				aggregatedVals[cid] = midmean(vals, cfg.TrimRatio, cfg.MinSamplesForTrim)
			}
		}

		setMetricDict(&row, metric, aggregatedVals)
	}

	// NIC_RX_ALL_PKG: treated as continuous.
	nicVals := make(map[int][]float64)
	for _, r := range bucket {
		for cid, val := range r.NICRxAllPkg {
			nicVals[cid] = append(nicVals[cid], val)
		}
	}
	row.NICRxAllPkg = make(map[int]float64)
	for cid, vals := range nicVals {
		row.NICRxAllPkg[cid] = midmean(vals, cfg.TrimRatio, cfg.MinSamplesForTrim)
	}

	return row, nil
}

// =============================================================================
// Midmean (Trimmed Mean)
// =============================================================================

// midmean computes the trimmed mean: sort → trim top/bottom trimRatio → mean of middle.
// If N < minSamples, falls back to plain mean.
func midmean(values []float64, trimRatio float64, minSamples int) float64 {
	if len(values) == 0 {
		return 0
	}
	if len(values) < minSamples {
		// Fall back to plain mean for very small samples.
		var sum float64
		for _, v := range values {
			sum += v
		}
		return sum / float64(len(values))
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	trim := int(math.Floor(float64(len(sorted)) * trimRatio))
	if trim == 0 {
		trim = 1
	}

	keep := sorted[trim : len(sorted)-trim]
	if len(keep) < 2 {
		// After trimming, not enough data — fall back to median.
		return sorted[len(sorted)/2]
	}

	var sum float64
	for _, v := range keep {
		sum += v
	}
	return sum / float64(len(keep))
}

// =============================================================================
// Counter Delta
// =============================================================================

// counterDelta computes the increment of a cumulative counter over a bucket.
// Handles 64-bit counter wrap.
func counterDelta(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	first := vals[0]
	last := vals[len(vals)-1]

	delta := last - first
	if delta < 0 {
		// Counter wrapped: assume 64-bit wrap.
		delta += math.MaxUint64
		if delta < 0 {
			// Still negative after wrap correction → data error.
			return 0
		}
	}
	return delta
}

// =============================================================================
// Window Split
// =============================================================================

// SplitWindows divides aggregated rows into baseline and detection windows.
//
// Returns (baselineRows, detectionRows).
// Detection window = last DetectionHours of data.
// Baseline window = everything before that.
func SplitWindows(rows []CSVRow, cfg DetectionConfig) (baseline, detection []CSVRow) {
	if len(rows) == 0 {
		return nil, nil
	}

	detectionSec := int64(cfg.DetectionHours * 3600)
	if detectionSec <= 0 {
		detectionSec = 3600 // default 1 hour
	}

	lastTS := rows[len(rows)-1].Timestamp
	cutoffTS := lastTS - detectionSec

	for _, row := range rows {
		if row.Timestamp >= cutoffTS {
			detection = append(detection, row)
		} else {
			baseline = append(baseline, row)
		}
	}

	return baseline, detection
}
