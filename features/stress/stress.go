// Package stress runs explicitly requested, high-load benchmark jobs.
//
// It is deliberately separate from health: health scores collected hardware
// metrics, while stress executes externally deployed benchmark assets.
package stress

import "time"

// Status is the stress job and benchmark execution state. A terminal
// StatusHealthy means the command exited successfully and all required result
// values were parsed; it does not compare a performance threshold.
//
// This type intentionally does not reuse health.HealthScore.Grade: a health
// grade is a 0--100 hardware score, while Status is an explicit benchmark job
// lifecycle/outcome.
type Status string

const (
	StatusPending     Status = "pending"
	StatusRunning     Status = "running"
	StatusHealthy     Status = "healthy"
	StatusUnhealthy   Status = "unhealthy"
	StatusTimeout     Status = "timeout"
	StatusUnavailable Status = "unavailable"
	StatusUnsupported Status = "unsupported"
	StatusCancelled   Status = "cancelled"
)

// Config is shared by the CLI and Web job manager. Paths are deployment
// configuration, never accepted from a Web request.
type Config struct {
	Enabled           bool                       `yaml:"enabled" json:"enabled"`
	WebEnabled        bool                       `yaml:"web_enabled" json:"web_enabled"`
	ScriptPath        string                     `yaml:"script_path" json:"script_path"`
	ReportPath        string                     `yaml:"report_path" json:"report_path"`
	DefaultBenchmarks []string                   `yaml:"default_benchmarks" json:"default_benchmarks"`
	Benchmarks        map[string]BenchmarkConfig `yaml:"benchmarks" json:"benchmarks"`
}

type BenchmarkConfig struct {
	Enabled   bool          `yaml:"enabled" json:"enabled"`
	Path      string        `yaml:"path" json:"path"`
	Timeout   time.Duration `yaml:"timeout" json:"timeout"`
	ResultDir string        `yaml:"result_dir" json:"result_dir"`
}

// RunOptions applies only to one submitted job. It is never persisted back to
// YAML. Timeout can only shorten the configured per-benchmark limit.
type RunOptions struct {
	Timeout time.Duration
}

type BenchmarkResult struct {
	// Name is the configured benchmark identifier. Status describes execution
	// and parsing success; Values contains the benchmark-specific measurements.
	Name       string             `json:"name"`
	Status     Status             `json:"status"`
	Message    string             `json:"message"`
	StartedAt  time.Time          `json:"started_at"`
	FinishedAt time.Time          `json:"finished_at"`
	DurationMS int64              `json:"duration_ms"`
	Values     map[string]float64 `json:"values,omitempty"`
	Source     string             `json:"source,omitempty"`
	Output     string             `json:"output,omitempty"`
}

type Report struct {
	JobID          string    `json:"job_id"`
	Timestamp      time.Time `json:"timestamp"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
	Platform       string    `json:"platform"`
	TimeoutSeconds int64     `json:"timeout_seconds,omitempty"`
	Status         Status    `json:"status"`
	// HealthCondition is the legacy, derived aggregate view of Benchmarks. New
	// callers should use Status for job state and each BenchmarkResult.Status
	// for the reason a specific benchmark did or did not complete.
	HealthCondition string            `json:"health_condition"`
	Benchmarks      []BenchmarkResult `json:"benchmarks"`
}
