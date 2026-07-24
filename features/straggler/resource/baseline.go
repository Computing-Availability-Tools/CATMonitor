package resource

import "sort"

// =============================================================================
// Historical Baseline Building
// =============================================================================

// BuildBaselines computes per-card per-metric statistical baselines from
// historical (baseline-window) data.
func BuildBaselines(baselineRows []CSVRow, cardIDs []int) map[int]map[MetricName]*CardBaseline {
	result := make(map[int]map[MetricName]*CardBaseline)

	for _, cid := range cardIDs {
		result[cid] = make(map[MetricName]*CardBaseline)
		for _, metric := range AllMetrics {
			// Collect all values for this card+metric from baseline rows.
			var values []float64
			for _, row := range baselineRows {
				dict := getMetricDict(row, metric)
				if dict == nil {
					continue
				}
				if v, ok := dict[cid]; ok {
					values = append(values, v)
				}
			}

			if len(values) < 2 {
				// Not enough data for a meaningful baseline.
				result[cid][metric] = &CardBaseline{
					CardID: cid,
					Metric: metric,
					N:      len(values),
				}
				continue
			}

			mean, std := MeanStd(values)

			sorted := make([]float64, len(values))
			copy(sorted, values)
			sort.Float64s(sorted)

			baseline := &CardBaseline{
				CardID: cid,
				Metric: metric,
				Mean:   mean,
				StdDev: std,
				P50:    Percentile(sorted, 0.50),
				P95:    Percentile(sorted, 0.95),
				P99:    Percentile(sorted, 0.99),
				N:      len(values),
			}
			result[cid][metric] = baseline
		}
	}

	return result
}
