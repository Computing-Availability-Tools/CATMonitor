package collector

import "time"

// Priority represents the collection priority of a metric.
type Priority int

const (
	PriorityLow Priority = iota
	PriorityMedium
	PriorityHigh
)

func (p Priority) String() string {
	switch p {
	case PriorityHigh:
		return "High"
	case PriorityMedium:
		return "Medium"
	case PriorityLow:
		return "Low"
	default:
		return "Unknown"
	}
}

// Metric represents a single collected metric data point.
type Metric struct {
	Component string            `json:"component"`
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Unit      string            `json:"unit"`
	Labels    map[string]string `json:"labels,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// Collector is the interface that all metric collectors must implement.
type Collector interface {
	Name() string
	Component() string
	Collect() ([]Metric, error)
	Priority() Priority
	DefaultInterval() time.Duration
	DefaultEnabled() bool
}

// wantedChecker is injected by the caller (e.g. metrics.AnyWanted) to avoid
// an import cycle between collector and metrics. nil = collect everything.
var wantedChecker func(string, []string) bool

// SetWantedChecker installs a function that reports whether any of the given
// metrics should be collected. Called by collectors via AnyWanted before
// expensive sub-methods.
func SetWantedChecker(fn func(string, []string) bool) {
	wantedChecker = fn
}

// AnyWanted reports whether any of the given metrics should be collected.
// Returns true when no checker is installed (backward compatible).
func AnyWanted(component string, names []string) bool {
	if wantedChecker == nil {
		return true
	}
	return wantedChecker(component, names)
}
