package resource

import (
	"fmt"
	"os"
	"sort"
)

// =============================================================================
// Fusion: 2D Cross-Validation + Compute-First Ordering
// =============================================================================

// FuseAndSummarize merges space and time detection results, applies 2D cross-
// validation to assign each card a quadrant, and enforces the "compute first,
// communication second" detection order.
//
// For each card:
//  1. Check compute metrics first.
//  2. If compute anomaly found → category=compute, communication metrics
//     are checked but flagged as "secondary" (possibly consequential).
//  3. If compute is clean → check communication metrics → if anomalous,
//     category=communication (independent network issue).
func FuseAndSummarize(
	spaceDetails map[int]map[MetricName]*MetricAnomalyDetail,
	timeDetails map[int]map[MetricName]*MetricAnomalyDetail,
	trends map[int][]TrendFinding,
	cardIDs []int,
	cfg DetectionConfig,
) []CardDetectionSummary {
	var summaries []CardDetectionSummary

	for _, cid := range cardIDs {
		summary := fuseOneCard(cid, spaceDetails[cid], timeDetails[cid], trends[cid], cardIDs, cfg)
		summaries = append(summaries, summary)
	}

	// Sort: confirmed anomalies first, then by composite score descending.
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Quadrant != summaries[j].Quadrant {
			// Confirmed anomalies first.
			qi := quadrantOrder(summaries[i].Quadrant)
			qj := quadrantOrder(summaries[j].Quadrant)
			return qi < qj
		}
		return summaries[i].CompositeScore > summaries[j].CompositeScore
	})

	return summaries
}

// quadrantOrder returns a sort order for quadrants (lower = more severe).
func quadrantOrder(q Quadrant) int {
	switch q {
	case QuadConfirmedAnomaly:
		return 0
	case QuadEarlyDegradation:
		return 1
	case QuadIndividualVariance:
		return 2
	default:
		return 3
	}
}

// fuseOneCard fuses space+time for a single card with compute-first logic.
func fuseOneCard(
	cid int,
	spaceM map[MetricName]*MetricAnomalyDetail,
	timeM map[MetricName]*MetricAnomalyDetail,
	trends []TrendFinding,
	cardIDs []int,
	cfg DetectionConfig,
) CardDetectionSummary {
	// Merge space and time details.
	merged := mergeDetails(spaceM, timeM, cfg)

	// Step 1: Check compute metrics.
	hasComputeAnomaly := false
	for _, metric := range AllMetrics {
		if IsComputeMetric(metric) {
			if d, ok := merged[metric]; ok && (d.SpaceAbnormal || d.TimeAbnormal) {
				hasComputeAnomaly = true
			}
		}
	}

	var summary CardDetectionSummary
	summary.CardID = cid
	summary.TrendFindings = trends

	if hasComputeAnomaly {
		// Compute anomaly: category=compute.
		// Check communication metrics but flag as secondary.
		summary.AnomalyCategory = CatCompute

		for _, metric := range AllMetrics {
			d := merged[metric]
			d.determineQuadrant()
			if IsComputeMetric(metric) {
				if d.SpaceAbnormal || d.TimeAbnormal {
					summary.AnomalyDetails = append(summary.AnomalyDetails, *d)
				}
			} else if IsCommunicationMetric(metric) {
				if d.SpaceAbnormal || d.TimeAbnormal {
					// Communication anomaly on a compute-anomalous card:
					// likely secondary — flag separately.
					summary.SecondaryCommAnomalies = append(summary.SecondaryCommAnomalies, *d)
				}
			}
		}

		// Determine overall quadrant from compute metrics only.
		summary.Quadrant = worstQuadrant(summary.AnomalyDetails)
		summary.CompositeScore = compositeScore(summary.AnomalyDetails, cfg)
		summary.Severity = determineSeverity(summary.Quadrant, summary.CompositeScore)

	} else {
		// Compute clean → check communication.
		summary.AnomalyCategory = CatNone

		// Check all metrics.
		for _, metric := range AllMetrics {
			d := merged[metric]
			d.determineQuadrant()
			if d.SpaceAbnormal || d.TimeAbnormal {
				summary.AnomalyDetails = append(summary.AnomalyDetails, *d)
			}
		}

		// Determine category from anomalous metrics.
		hasCommAnomaly := false
		for _, d := range summary.AnomalyDetails {
			if IsCommunicationMetric(d.Metric) {
				hasCommAnomaly = true
			}
		}
		if hasCommAnomaly {
			summary.AnomalyCategory = CatCommunication
		}

		summary.Quadrant = worstQuadrant(summary.AnomalyDetails)
		summary.CompositeScore = compositeScore(summary.AnomalyDetails, cfg)
		summary.Severity = determineSeverity(summary.Quadrant, summary.CompositeScore)
	}

	if len(summary.AnomalyDetails) == 0 && len(summary.SecondaryCommAnomalies) > 0 {
		// Only secondary comm anomalies: still flag as compute-related.
		summary.Quadrant = QuadConfirmedAnomaly
		summary.CompositeScore = compositeScore(summary.SecondaryCommAnomalies, cfg)
		summary.Severity = SevWarning
	}

	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Card %d: category=%s quadrant=%s score=%.2f anomalies=%d secondary=%d\n",
		cid, summary.AnomalyCategory, summary.Quadrant, summary.CompositeScore,
		len(summary.AnomalyDetails), len(summary.SecondaryCommAnomalies))

	return summary
}

// =============================================================================
// Detail Helpers
// =============================================================================

// mergeDetails combines space and time detection details into one.
func mergeDetails(
	spaceM map[MetricName]*MetricAnomalyDetail,
	timeM map[MetricName]*MetricAnomalyDetail,
	cfg DetectionConfig,
) map[MetricName]*MetricAnomalyDetail {
	merged := make(map[MetricName]*MetricAnomalyDetail)

	for _, metric := range AllMetrics {
		sd := spaceM[metric]
		td := timeM[metric]

		d := &MetricAnomalyDetail{Metric: metric}
		if sd != nil {
			d.SpaceScore = sd.SpaceScore
			d.SpaceAbnormal = sd.SpaceAbnormal
		}
		if td != nil {
			d.TimeScore = td.TimeScore
			d.TimeAbnormal = td.TimeAbnormal
			d.CurrentMean = td.CurrentMean
			d.BaselineMean = td.BaselineMean
			d.PeerMean = td.PeerMean
		}

		// Compute fusion score.
		d.FusionScore = cfg.TimeWeight*d.TimeScore + cfg.SpaceWeight*d.SpaceScore
		merged[metric] = d
	}

	return merged
}

// determineQuadrant classifies the metric into the 2×2 quadrant.
func (d *MetricAnomalyDetail) determineQuadrant() {
	switch {
	case d.SpaceAbnormal && d.TimeAbnormal:
		d.Quadrant = QuadConfirmedAnomaly
	case d.SpaceAbnormal && !d.TimeAbnormal:
		d.Quadrant = QuadIndividualVariance
	case !d.SpaceAbnormal && d.TimeAbnormal:
		d.Quadrant = QuadEarlyDegradation
	default:
		d.Quadrant = QuadNormal
	}
}

// worstQuadrant returns the most severe quadrant from a list of details.
func worstQuadrant(details []MetricAnomalyDetail) Quadrant {
	worst := QuadNormal
	for _, d := range details {
		if quadrantOrder(d.Quadrant) < quadrantOrder(worst) {
			worst = d.Quadrant
		}
	}
	return worst
}

// compositeScore computes the weighted anomaly score across all provided details.
func compositeScore(details []MetricAnomalyDetail, cfg DetectionConfig) float64 {
	if len(details) == 0 {
		return 0
	}
	var sum float64
	for _, d := range details {
		sum += d.FusionScore
	}
	return sum / float64(len(details))
}

// determineSeverity maps quadrant + score to severity.
func determineSeverity(q Quadrant, score float64) Severity {
	switch q {
	case QuadConfirmedAnomaly:
		if score >= 5 {
			return SevCritical
		}
		return SevWarning
	case QuadEarlyDegradation:
		return SevInfo
	case QuadIndividualVariance:
		return SevInfo
	default:
		return SevInfo
	}
}
