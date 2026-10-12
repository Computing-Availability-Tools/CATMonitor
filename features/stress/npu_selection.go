package stress

import (
	"context"
	"fmt"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

func parameterValue(profile *ExecutionProfile, key string) string {
	for _, p := range profile.Parameters {
		if p.Key == key {
			return p.Value
		}
	}
	return ""
}

// Job admission is deliberately uncached: a previous doctor's profile is not
// proof that the current selection or the current workload resources are safe.
func (m *Manager) describeJob(binding Binding, timeout time.Duration) (*ExecutionProfile, error) {
	budget := describeTimeout
	if binding.Benchmark == "npu_burn" {
		budget = npuDescribeTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	profile, err := m.executor.Describe(ctx, binding)
	if err != nil {
		return nil, err
	}
	if err := validateDescribeProfile(binding.Benchmark, profile); err != nil {
		return nil, err
	}
	return m.applyRunConfiguration(profile, binding.Benchmark, timeout), nil
}

func (m *Manager) prepareNPU(requested workloadapi.DeviceIDs, cases workloadapi.CaseNames, timeout time.Duration) (*ExecutionProfile, *NPUSelection, error) {
	binding := m.binding("npu_burn")
	base, err := m.describeJob(binding, timeout)
	if err != nil {
		return nil, nil, err
	}
	available, err := workloadapi.ParseDevices(parameterValue(base, "available_devices"))
	if err != nil {
		return nil, nil, fmt.Errorf("NPU logical namespace is unverified: %w", err)
	}
	defaults := available
	if value := parameterValue(base, "devices"); value != "all" {
		defaults, err = workloadapi.ParseDevices(value)
		if err != nil {
			return nil, nil, err
		}
	}
	selected := defaults
	if requested != nil {
		selected = requested
	}
	if err := workloadapi.ValidateDevices(selected); err != nil {
		return nil, nil, err
	}
	allowed := m.cfg.Benchmarks["npu_burn"].AllowedNPUDevices
	// Existing deployments keep their defaults; changing cards is opt-in by
	// the administrator, not automatically authorized by device visibility.
	if allowed == nil {
		allowed = defaults
	}
	for _, id := range selected {
		if !containsDevice(available, id) {
			return nil, nil, fmt.Errorf("NPU logical device %d is not visible", id)
		}
		if !containsDevice(allowed, id) {
			return nil, nil, fmt.Errorf("NPU logical device %d is not in the administrator allowlist", id)
		}
	}
	binding.NPUDevices = append(workloadapi.DeviceIDs(nil), selected...)
	var defaultCases, selectedCases workloadapi.CaseNames
	if value := parameterValue(base, "selected_cases"); value != "" {
		defaultCases, err = workloadapi.ParseCases(value)
		if err != nil {
			return nil, nil, err
		}
		selectedCases = defaultCases
	}
	if cases != nil {
		if err := workloadapi.ValidateCases(cases); err != nil {
			return nil, nil, err
		}
		availableCases, err := workloadapi.ParseCases(parameterValue(base, "available_cases"))
		if err != nil {
			return nil, nil, fmt.Errorf("workload image has no verified operator catalog; rebuild with the portable compatibility profile")
		}
		for _, name := range cases {
			found := false
			for _, candidate := range availableCases {
				found = found || candidate == name
			}
			if !found {
				return nil, nil, fmt.Errorf("NPU operator %q is unavailable for this image/profile", name)
			}
		}
		selectedCases = cases
	}
	binding.NPUCases = append(workloadapi.CaseNames(nil), selectedCases...)
	profile, err := m.describeJob(binding, timeout)
	if err != nil {
		return nil, nil, err
	}
	resolved, err := workloadapi.ParseDevices(parameterValue(profile, "devices"))
	if err != nil || !workloadapi.SameDevices(resolved, selected) {
		return nil, nil, fmt.Errorf("workload describe did not confirm the requested NPU selection")
	}
	if selectedCases != nil {
		confirmed, err := workloadapi.ParseCases(parameterValue(profile, "selected_cases"))
		if err != nil || !workloadapi.SameCases(confirmed, selectedCases) {
			return nil, nil, fmt.Errorf("workload describe did not confirm the requested NPU operators")
		}
	}
	return profile, &NPUSelection{Default: append([]int(nil), defaults...), Requested: append([]int(nil), requested...), Selected: append([]int(nil), selected...), DefaultCases: append([]string(nil), defaultCases...), RequestedCases: append([]string(nil), cases...), SelectedCases: append([]string(nil), selectedCases...)}, nil
}

func containsDevice(ids []int, id int) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func copyNPUSelection(s *NPUSelection) *NPUSelection {
	if s == nil {
		return nil
	}
	return &NPUSelection{Model: workloadapi.CopyModel(s.Model), Default: append([]int(nil), s.Default...), Requested: append([]int(nil), s.Requested...), Selected: append([]int(nil), s.Selected...), Actual: append([]int(nil), s.Actual...), DefaultCases: append([]string(nil), s.DefaultCases...), RequestedCases: append([]string(nil), s.RequestedCases...), SelectedCases: append([]string(nil), s.SelectedCases...), ActualCases: append([]string(nil), s.ActualCases...)}
}

func (m *Manager) prepareNPUModel(options RunOptions) (*ExecutionProfile, *NPUSelection, error) {
	model := workloadapi.CopyModel(options.NPUModel)
	if err := model.Validate(); err != nil {
		return nil, nil, err
	}
	if options.NPUCases != nil {
		return nil, nil, fmt.Errorf("model and operator selection cannot be combined")
	}
	if options.NPUDevices == nil {
		return nil, nil, fmt.Errorf("model jobs require explicit logical devices")
	}
	if model.Action == "create_reference" && len(options.NPUDevices) != 1 {
		return nil, nil, fmt.Errorf("reference generation requires exactly one logical device")
	}
	allowed := false
	cfg := m.cfg.Benchmarks["npu_burn"]
	for _, name := range cfg.AllowedNPUModels {
		allowed = allowed || name == model.Name
	}
	if !allowed {
		return nil, nil, fmt.Errorf("model is not in administrator allowed_npu_models")
	}
	if model.Action == "create_reference" && !cfg.AllowNPUReferenceGeneration {
		return nil, nil, fmt.Errorf("administrator has not enabled reference generation")
	}
	// Preserve device/idle admission, then perform a model-specific dependency
	// and immutable-reference admission using the same bound device selection.
	_, selection, err := m.prepareNPU(options.NPUDevices, nil, options.Timeout)
	if err != nil {
		return nil, nil, err
	}
	binding := m.binding("npu_burn")
	binding.NPUDevices = append(workloadapi.DeviceIDs(nil), selection.Selected...)
	binding.NPUModel = model
	profile, err := m.describeJob(binding, options.Timeout)
	if err != nil {
		return nil, nil, err
	}
	resolved, err := workloadapi.ParseDevices(parameterValue(profile, "devices"))
	if err != nil || !workloadapi.SameDevices(resolved, selection.Selected) || parameterValue(profile, "model") != model.Name || parameterValue(profile, "model_action") != model.Action || parameterValue(profile, "model_reference") != model.ReferenceID {
		return nil, nil, fmt.Errorf("model describe does not confirm this job request")
	}
	selection.Model = model
	selection.SelectedCases, selection.RequestedCases, selection.DefaultCases = nil, nil, nil
	return profile, selection, nil
}
