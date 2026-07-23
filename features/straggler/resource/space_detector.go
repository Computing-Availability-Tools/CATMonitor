package resource

import (
	"math"
	"sort"
)

// =============================================================================
// Space (Peer-Comparison) Dimension Detection
// =============================================================================

// detectSpaceAnomalies computes per-time-point space Z-Scores for all cards
// across all metrics over the detection window.
//
// Returns: cardID → metric → []zscore (one per detection-window time point).
func detectSpaceAnomalies(detectionRows []CSVRow, cardIDs []int, cfg DetectionConfig) *SpaceDetectionResult {
	result := &SpaceDetectionResult{
		Scores: make(map[int]map[MetricName][]float64),
	}

	// Init.
	for _, cid := range cardIDs {
		result.Scores[cid] = make(map[MetricName][]float64)
		for _, metric := range AllMetrics {
			result.Scores[cid][metric] = make([]float64, 0, len(detectionRows))
		}
	}

	// For each time point, for each metric, compute Z-Scores.
	for _, row := range detectionRows {
		for _, metric := range AllMetrics {
			meta := MetricMetaRegistry[metric]
			vals := getMetricValues(row, metric, cardIDs)

			// Filter to valid cards present at this time point.
			present := make([]int, 0, len(cardIDs))
			presentVals := make([]float64, 0, len(cardIDs))
			for i, cid := range cardIDs {
				dict := getMetricDict(row, metric)
				if dict == nil {
					continue
				}
				if _, ok := dict[cid]; ok {
					present = append(present, i)
					presentVals = append(presentVals, vals[i])
				}
			}

			if len(presentVals) < 2 {
				// Need at least 2 cards for peer comparison.
				for _, cid := range cardIDs {
					result.Scores[cid][metric] = append(result.Scores[cid][metric], 0)
				}
				continue
			}

			// Compute Z-Scores based on method.
			switch meta.SpaceMethod {
			case MethodAbsolute:
				// Absolute threshold: > threshold → anomaly.
				for i, cid := range cardIDs {
					z := 0.0
					if vals[i] > meta.AbsThreshold {
						z = 999 // sentinel for "absolute anomaly"
					}
					result.Scores[cid][metric] = append(result.Scores[cid][metric], z)
				}

			case MethodDirect:
				// Direct comparison (for freq): below min of others → anomaly.
				allVals := presentVals
				sort.Float64s(allVals)
				minVal := allVals[0]
				for i, cid := range cardIDs {
					z := 0.0
					if vals[i] < minVal || (vals[i] < minVal+cfg.FreqDownclockGap) {
						// If significantly below the minimum peer value.
						if vals[i] < minVal {
							z = 999
						}
					}
					result.Scores[cid][metric] = append(result.Scores[cid][metric], z)
				}

			case MethodIQR:
				sorted := make([]float64, len(presentVals))
				copy(sorted, presentVals)
				sort.Float64s(sorted)
				q1 := Percentile(sorted, 0.25)
				q3 := Percentile(sorted, 0.75)
				iqr := q3 - q1
				lower := q1 - cfg.SpaceIQRMult*iqr
				upper := q3 + cfg.SpaceIQRMult*iqr

				for i, cid := range cardIDs {
					z := 0.0
					if vals[i] < lower || vals[i] > upper {
						z = 999
					}
					result.Scores[cid][metric] = append(result.Scores[cid][metric], z)
				}

			default: // MethodZScore
				mean, std := MeanStd(presentVals)
				for i, cid := range cardIDs {
					z := 0.0
					if std > 0 {
						z = math.Abs(vals[i]-mean) / std
					}
					result.Scores[cid][metric] = append(result.Scores[cid][metric], z)
				}
			}
		}
	}

	return result
}

// =============================================================================
// Space Score Aggregation
// =============================================================================

// aggregateSpaceScores reduces per-time-point space scores to per-card
// aggregate space scores.
//
// For each card+metric:
//   spaceScore = mean of Z-Scores across detection window
//   spaceAbnormal = mean Z-Score > threshold
func aggregateSpaceScores(space *SpaceDetectionResult, cardIDs []int, cfg DetectionConfig) map[int]map[MetricName]*MetricAnomalyDetail {
	result := make(map[int]map[MetricName]*MetricAnomalyDetail)

	for _, cid := range cardIDs {
		result[cid] = make(map[MetricName]*MetricAnomalyDetail)
		for _, metric := range AllMetrics {
			zscores := space.Scores[cid][metric]
			if len(zscores) == 0 {
				result[cid][metric] = &MetricAnomalyDetail{
					Metric:     metric,
					SpaceScore: 0,
				}
				continue
			}

			// For absolute/direct methods, consider "abnormal" if any point had sentinel value.
			meta := MetricMetaRegistry[metric]
			isSentinel := meta.SpaceMethod == MethodAbsolute || meta.SpaceMethod == MethodDirect

			var sum float64
			abnormalCount := 0
			for _, z := range zscores {
				if isSentinel {
					if z >= 999 {
						abnormalCount++
					}
				} else {
					sum += z
					if z > cfg.SpaceZThreshold {
						abnormalCount++
					}
				}
			}

			var spaceScore float64
			var spaceAbnormal bool
			if isSentinel {
				// For absolute/direct: abnormal if >50% of points flagged.
				spaceScore = float64(abnormalCount) / float64(len(zscores))
				spaceAbnormal = spaceScore > 0.5
			} else {
				spaceScore = sum / float64(len(zscores))
				spaceAbnormal = spaceScore > cfg.SpaceZThreshold
			}

			result[cid][metric] = &MetricAnomalyDetail{
				Metric:        metric,
				SpaceScore:    spaceScore,
				SpaceAbnormal: spaceAbnormal,
			}
		}
	}

	return result
}
