// Package stress implements CATMonitor's explicitly triggered reliability
// workload controller. Stress results are independent from health scoring.
package stress

import (
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

const (
	InitiatorCLI = "cli"
	InitiatorWeb = "web"
)

type Status string

const (
	StatusPending          Status = "pending"
	StatusRunning          Status = "running"
	StatusHealthy          Status = "healthy"
	StatusTimeLimitReached Status = "time_limit_reached"
	StatusUnhealthy        Status = "unhealthy"
	StatusUnavailable      Status = "unavailable"
	StatusUnsupported      Status = "unsupported"
	StatusCancelled        Status = "cancelled"
)

// Config is owned by the daemon. CLI and Web read it through the local control
// API and cannot override executor/container bindings.
type Config struct {
	Enabled           bool                       `yaml:"enabled" json:"enabled"`
	WebEnabled        bool                       `yaml:"web_enabled" json:"web_enabled"`
	ControlSocket     string                     `yaml:"control_socket" json:"control_socket"`
	ReportPath        string                     `yaml:"report_path" json:"report_path"`
	DefaultBenchmarks []string                   `yaml:"default_benchmarks" json:"default_benchmarks"`
	Executor          ExecutorConfig             `yaml:"executor" json:"executor"`
	Benchmarks        map[string]BenchmarkConfig `yaml:"benchmarks" json:"benchmarks"`
}

type ExecutorConfig struct {
	Type         string `yaml:"type" json:"type"`
	DockerBinary string `yaml:"docker_binary" json:"docker_binary"`
	DockerSocket string `yaml:"docker_socket" json:"docker_socket"`
}

type BenchmarkConfig struct {
	Enabled   bool          `yaml:"enabled" json:"enabled"`
	Plugin    string        `yaml:"plugin" json:"plugin"`
	Container string        `yaml:"container" json:"container"`
	User      string        `yaml:"user" json:"user"`
	Timeout   time.Duration `yaml:"timeout" json:"timeout"`
}

type RunOptions struct {
	Timeout   time.Duration
	Initiator string
}

type CheckStatus = workloadapi.CheckStatus

const (
	CheckPass        = workloadapi.CheckPass
	CheckWarn        = workloadapi.CheckWarn
	CheckFail        = workloadapi.CheckFail
	CheckUnsupported = workloadapi.CheckUnsupported
)

type ExecutionProfile = workloadapi.ExecutionProfile
type ProfileParameter = workloadapi.ProfileParameter
type ResourceProfile = workloadapi.ResourceProfile
type AssetCheck = workloadapi.AssetCheck
type MPICheck = workloadapi.MPICheck
type PreflightResult = workloadapi.PreflightResult
type BenchmarkResult struct {
	Name       string             `json:"name"`
	Status     Status             `json:"status"`
	Message    string             `json:"message"`
	StartedAt  time.Time          `json:"started_at"`
	FinishedAt time.Time          `json:"finished_at"`
	DurationMS int64              `json:"duration_ms"`
	Values     map[string]float64 `json:"values,omitempty"`
	Source     string             `json:"source,omitempty"`
	Output     string             `json:"output,omitempty"`
	Profile    *ExecutionProfile  `json:"profile,omitempty"`
}

type Report struct {
	JobID               string            `json:"job_id"`
	Initiator           string            `json:"initiator,omitempty"`
	Timestamp           time.Time         `json:"timestamp"`
	StartedAt           time.Time         `json:"started_at"`
	FinishedAt          time.Time         `json:"finished_at,omitempty"`
	Platform            string            `json:"platform"`
	TimeoutSeconds      int64             `json:"timeout_seconds,omitempty"`
	Status              Status            `json:"status"`
	ConfigurationSHA256 string            `json:"configuration_sha256,omitempty"`
	ReportError         string            `json:"report_error,omitempty"`
	Benchmarks          []BenchmarkResult `json:"benchmarks"`
	Cancellable         bool              `json:"cancellable,omitempty"`
}
