package health

import "github.com/Computing-Availability-Tools/CATMonitor/internal/collector"

// findMetric finds a metric by component, name, and optional label key-value.
func findMetric(metrics []collector.Metric, component, name, labelKey, labelVal string) *collector.Metric {
	for i := range metrics {
		m := &metrics[i]
		if m.Component == component && m.Name == name {
			if labelKey == "" {
				return m
			}
			if v, ok := m.Labels[labelKey]; ok && v == labelVal {
				return m
			}
		}
	}
	return nil
}

// worstValue returns the maximum Value among metrics matching the given name,
// or ok=false if none. "Worst" = max because higher is worse for temperature,
// utilization, memory_usage and error counts.
func worstValue(metrics []collector.Metric, name string) (float64, bool) {
	found := false
	var worst float64
	for _, m := range metrics {
		if m.Name == name {
			if !found || m.Value > worst {
				worst = m.Value
				found = true
			}
		}
	}
	return worst, found
}

// hasAnyPositive reports whether any metric with the given name has value > 0.
func hasAnyPositive(metrics []collector.Metric, name string) bool {
	for _, m := range metrics {
		if m.Name == name && m.Value > 0 {
			return true
		}
	}
	return false
}
