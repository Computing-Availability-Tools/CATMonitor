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
