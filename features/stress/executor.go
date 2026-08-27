package stress

import (
	"context"
	"errors"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

var ErrExecutorUnavailable = errors.New("stress executor is unavailable")

// Binding selects a preconfigured plugin inside a preconfigured workload
// container. Values come from the shared CATMonitor configuration, never from
// a CLI or Web run request.
type Binding struct {
	Benchmark string
	Plugin    string
	Container string
	User      string
}

// Executor owns transport and lifecycle only. Benchmark semantics and result
// parsing remain inside workload plugins.
type Executor interface {
	Describe(context.Context, Binding) (*ExecutionProfile, error)
	Run(context.Context, Binding, workloadapi.Request) (workloadapi.Result, error)
	Cancel(context.Context, Binding, string) error
	Status(context.Context, Binding, string) (workloadapi.State, error)
}

type unavailableExecutor struct{ err error }

func (e unavailableExecutor) Describe(context.Context, Binding) (*ExecutionProfile, error) {
	return nil, e.err
}
func (e unavailableExecutor) Run(context.Context, Binding, workloadapi.Request) (workloadapi.Result, error) {
	return workloadapi.Result{}, e.err
}
func (e unavailableExecutor) Cancel(context.Context, Binding, string) error { return e.err }
func (e unavailableExecutor) Status(context.Context, Binding, string) (workloadapi.State, error) {
	return workloadapi.State{}, e.err
}
