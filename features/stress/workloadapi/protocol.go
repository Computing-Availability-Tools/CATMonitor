// Package workloadapi defines the transport-neutral protocol shared by
// CATMonitor's CPU and accelerator workload containers.
package workloadapi

import (
	"encoding/json"
	"time"
)

const ProtocolVersion = 1

const (
	StatusPending          = "pending"
	StatusRunning          = "running"
	StatusHealthy          = "healthy"
	StatusTimeLimitReached = "time_limit_reached"
	StatusUnhealthy        = "unhealthy"
	StatusUnavailable      = "unavailable"
	StatusCancelled        = "cancelled"
)

// Request is the only payload accepted by a workload container. It contains
// no command, executable path, environment or untyped argument vector.
type Request struct {
	ProtocolVersion int                        `json:"protocol_version"`
	JobID           string                     `json:"job_id"`
	Benchmark       string                     `json:"benchmark"`
	TimeoutSeconds  int64                      `json:"timeout_seconds"`
	Options         map[string]json.RawMessage `json:"options"`
}

// Result is the common CPU/NPU terminal envelope returned by
// catmonitor-stress-exec.
type Result struct {
	ProtocolVersion int                `json:"protocol_version"`
	JobID           string             `json:"job_id"`
	Benchmark       string             `json:"benchmark"`
	Status          string             `json:"status"`
	StartedAt       time.Time          `json:"started_at"`
	FinishedAt      time.Time          `json:"finished_at"`
	DurationMS      int64              `json:"duration_ms"`
	Message         string             `json:"message"`
	Values          map[string]float64 `json:"values,omitempty"`
	Source          string             `json:"source,omitempty"`
	Output          string             `json:"output,omitempty"`
}

// State is returned by the workload status operation.
type State struct {
	ProtocolVersion int       `json:"protocol_version"`
	JobID           string    `json:"job_id"`
	Benchmark       string    `json:"benchmark,omitempty"`
	Status          string    `json:"status"`
	PID             int       `json:"pid,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
	Message         string    `json:"message,omitempty"`
}

type CancelResponse struct {
	ProtocolVersion int    `json:"protocol_version"`
	JobID           string `json:"job_id"`
	Accepted        bool   `json:"accepted"`
	Message         string `json:"message,omitempty"`
}

func ValidTerminalStatus(status string) bool {
	switch status {
	case StatusHealthy, StatusTimeLimitReached, StatusUnhealthy, StatusUnavailable, StatusCancelled:
		return true
	default:
		return false
	}
}

// CheckStatus and profile types are the shared describe contract returned by
// CPU and NPU workload plugins. They intentionally live in workloadapi so the
// workload image does not import the daemon/controller package.
type CheckStatus string

const (
	CheckPass        CheckStatus = "pass"
	CheckWarn        CheckStatus = "warn"
	CheckFail        CheckStatus = "fail"
	CheckUnsupported CheckStatus = "unsupported"
)

type ExecutionProfile struct {
	ProtocolVersion     int                `json:"protocol_version"`
	Benchmark           string             `json:"benchmark"`
	Executor            string             `json:"executor,omitempty"`
	Container           string             `json:"container,omitempty"`
	Plugin              string             `json:"plugin,omitempty"`
	RuntimeIdentity     map[string]string  `json:"runtime_identity,omitempty"`
	Parameters          []ProfileParameter `json:"parameters"`
	Resources           ResourceProfile    `json:"resources"`
	Assets              []AssetCheck       `json:"assets"`
	MPI                 MPICheck           `json:"mpi"`
	Preflight           PreflightResult    `json:"preflight"`
	TimeoutSeconds      int64              `json:"timeout_seconds"`
	ScriptSHA256        string             `json:"script_sha256,omitempty"`
	ConfigurationSHA256 string             `json:"configuration_sha256"`
}

type ProfileParameter struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}
type ResourceProfile struct {
	MPIProcesses      int    `json:"mpi_processes"`
	ThreadsPerProcess int    `json:"threads_per_process"`
	TotalWorkers      int    `json:"total_workers"`
	RuntimeSeconds    int    `json:"runtime_seconds"`
	ProblemSize       string `json:"problem_size,omitempty"`
}
type AssetCheck struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Kind     string      `json:"kind"`
	Required bool        `json:"required"`
	Status   CheckStatus `json:"status"`
	Message  string      `json:"message"`
	SHA256   string      `json:"sha256,omitempty"`
}
type MPICheck struct {
	Required       bool        `json:"required"`
	Launcher       string      `json:"launcher,omitempty"`
	Implementation string      `json:"implementation"`
	Version        string      `json:"version,omitempty"`
	ExecutableABI  string      `json:"executable_abi"`
	Status         CheckStatus `json:"status"`
	Message        string      `json:"message"`
}
type PreflightResult struct {
	Status  CheckStatus `json:"status"`
	Message string      `json:"message"`
}
