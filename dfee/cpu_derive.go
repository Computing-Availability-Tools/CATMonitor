package dfee

import (
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// cpuTimeSnapshot holds the 8 cumulative jiffie counters from /proc/stat
// (core=total aggregate). Used to compute delta → utilization percentages.
type cpuTimeSnapshot struct {
	user, nice, system, idle    float64
	iowait, irq, softirq, steal float64
	valid                       bool
}

// extractCPUTimes reads the 8 CPU time metrics (core=total) from the given
// metrics slice and returns them as a cpuTimeSnapshot. Returns ok=false if
// any of the 8 values are missing.
func extractCPUTimes(metrics []collector.Metric) (cpuTimeSnapshot, bool) {
	var ts cpuTimeSnapshot
	needed := map[string]*float64{
		"user_time": &ts.user, "nice_time": &ts.nice,
		"system_time": &ts.system, "idle_time": &ts.idle,
		"iowait_time": &ts.iowait, "irq_time": &ts.irq,
		"softirq_time": &ts.softirq, "steal_time": &ts.steal,
	}
	found := 0
	for _, m := range metrics {
		if m.Component != "cpu" {
			continue
		}
		if m.Labels["core"] != "total" {
			continue
		}
		ptr, ok := needed[m.Name]
		if !ok {
			continue
		}
		*ptr = m.Value
		found++
	}
	ts.valid = found == 8
	return ts, ts.valid
}

// cpuTotal returns the sum of all 8 jiffie counters.
func (ts cpuTimeSnapshot) total() float64 {
	return ts.user + ts.nice + ts.system + ts.idle +
		ts.iowait + ts.irq + ts.softirq + ts.steal
}

// delta returns the per-field difference between two snapshots, clamping
// negative values to 0 (counters can reset on reboot).
func (ts cpuTimeSnapshot) delta(prev cpuTimeSnapshot) cpuTimeSnapshot {
	d := func(a, b float64) float64 {
		if a < b {
			return 0
		}
		return a - b
	}
	return cpuTimeSnapshot{
		user: d(ts.user, prev.user), nice: d(ts.nice, prev.nice),
		system: d(ts.system, prev.system), idle: d(ts.idle, prev.idle),
		iowait: d(ts.iowait, prev.iowait), irq: d(ts.irq, prev.irq),
		softirq: d(ts.softirq, prev.softirq), steal: d(ts.steal, prev.steal),
		valid: true,
	}
}

// derivedMetric is one of the 7 utilization percentages computed from the
// 8 raw CPU time deltas.
type derivedMetric struct {
	name  string
	value float64
}

// deriveCPUUtil computes 7 utilization percentages from the delta between
// current and previous CPU time snapshots. Returns empty slice if prev is
// invalid or total delta is zero.
func deriveCPUUtil(prev, curr cpuTimeSnapshot) []derivedMetric {
	if !prev.valid || !curr.valid {
		return nil
	}
	d := curr.delta(prev)
	total := d.total()
	if total <= 0 {
		return nil
	}
	pct := func(v float64) float64 {
		if v < 0 {
			v = 0
		}
		return v / total * 100
	}
	return []derivedMetric{
		{"idle_util", pct(d.idle)},
		{"non_idle_util", pct(d.user + d.nice + d.system + d.iowait + d.irq + d.softirq + d.steal)},
		{"user_util", pct(d.user + d.nice)},
		{"system_util", pct(d.system)},
		{"iowait_util", pct(d.iowait)},
		{"irq_util", pct(d.irq + d.softirq)},
		{"steal_util", pct(d.steal)},
	}
}

// derivedToMetrics converts derived metrics to collector.Metric instances
// with the given timestamp, so they can be grouped by chartGroups.
func derivedToMetrics(dm []derivedMetric, now time.Time) []collector.Metric {
	out := make([]collector.Metric, 0, len(dm))
	for _, d := range dm {
		out = append(out, collector.Metric{
			Component: "cpu", Name: d.name, Value: d.value, Unit: "%",
			Labels: map[string]string{"core": "total"}, Timestamp: now,
		})
	}
	return out
}
