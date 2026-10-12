package stress

import (
	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
	"time"
)

// ControlConfigView is the read-only daemon-owned Stress configuration exposed
// to CLI and Web clients. It deliberately omits Docker socket and binary paths.
type ControlConfigView struct {
	Enabled           bool                   `json:"enabled"`
	FeatureEnabled    bool                   `json:"feature_enabled"`
	WebEnabled        bool                   `json:"web_enabled"`
	Platform          string                 `json:"platform"`
	Executor          string                 `json:"executor"`
	SharedReport      bool                   `json:"shared_report"`
	DefaultBenchmarks []string               `json:"default_benchmarks"`
	Benchmarks        []ControlBenchmarkView `json:"benchmarks"`
}

type ControlBenchmarkView struct {
	Name           string            `json:"name"`
	Plugin         string            `json:"plugin"`
	Container      string            `json:"container"`
	Enabled        bool              `json:"enabled"`
	Available      bool              `json:"available"`
	Message        string            `json:"message,omitempty"`
	TimeoutSeconds int64             `json:"timeout_seconds"`
	Profile        *ExecutionProfile `json:"profile,omitempty"`
	ProfileError   string            `json:"profile_error,omitempty"`
}

type ControlStartRequest struct {
	Benchmarks     []string                  `json:"benchmarks"`
	TimeoutSeconds int64                     `json:"timeout_seconds"`
	Initiator      string                    `json:"initiator"`
	NPUDevices     workloadapi.DeviceIDs     `json:"npu_devices,omitempty"`
	NPUCases       workloadapi.CaseNames     `json:"npu_cases,omitempty"`
	NPUModel       *workloadapi.ModelRequest `json:"npu_model,omitempty"`
}

func (r ControlStartRequest) RunOptions() RunOptions {
	return RunOptions{Timeout: time.Duration(r.TimeoutSeconds) * time.Second, Initiator: r.Initiator, NPUDevices: r.NPUDevices, NPUCases: r.NPUCases, NPUModel: workloadapi.CopyModel(r.NPUModel)}
}

type ControlAPIError struct {
	StatusCode int
	Message    string
}

func (e *ControlAPIError) Error() string { return e.Message }
