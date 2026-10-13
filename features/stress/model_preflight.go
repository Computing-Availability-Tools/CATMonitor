package stress

import (
	"context"
	"runtime"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

// PreflightModel uses the same uncached admission as Start, but never creates a
// job, runs a workload, writes a reference/report, or calls Cancel. One bounded
// probe is permitted at a time; active jobs are never probed by this endpoint.
func (m *Manager) PreflightModel(ctx context.Context, request ModelPreflightRequest) ModelPreflightView {
	view := ModelPreflightView{Status: "failed", CheckedAt: time.Now(),
		NPUModel: workloadapi.CopyModel(request.NPUModel), NPUDevices: append(workloadapi.DeviceIDs(nil), request.NPUDevices...),
		Failures: []ModelPreflightFailure{}}
	block := func(code, label, message string) ModelPreflightView {
		view.Failures = append(view.Failures, ModelPreflightFailure{Code: code, Label: label, Message: message})
		return view
	}
	if err := request.NPUModel.Validate(); err != nil {
		return block("invalid_request", "模型请求", err.Error())
	}
	if err := workloadapi.ValidateDevices(request.NPUDevices); err != nil {
		return block("invalid_request", "设备选择", err.Error())
	}
	cfg := m.Config()
	benchmark := cfg.Benchmarks["npu_burn"]
	if !cfg.Enabled || !benchmark.Enabled || runtime.GOOS != "linux" {
		return block("model_disabled", "模型测试未启用", "当前部署未启用 Linux NPU Stress。")
	}
	allowed := false
	for _, name := range benchmark.AllowedNPUModels {
		allowed = allowed || name == request.NPUModel.Name
	}
	if !allowed {
		return block("model_not_allowed", "模型未授权", "所选模型不在管理员授权列表中。")
	}
	if request.NPUModel.Action == "create_reference" && !benchmark.AllowNPUReferenceGeneration {
		return block("reference_generation_denied", "参考生成未授权", "请管理员准备匹配参考，或显式授权生成参考。")
	}
	if !m.modelPreflightMu.TryLock() {
		view.Status = "not_checked"
		return block("preflight_busy", "预检正在执行", "请等待已有只读预检完成。")
	}
	defer m.modelPreflightMu.Unlock()
	m.mu.Lock()
	active := m.active != nil
	m.mu.Unlock()
	if active {
		view.Status = "not_checked"
		return block("controller_busy", "当前作业运行中", "未探测 NPU；请在当前作业结束后预检。")
	}
	ctx, cancel := context.WithTimeout(ctx, workloadapi.ModelPreflightTimeout+30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		view.Status = "not_checked"
		return block("preflight_interrupted", "预检未完成", err.Error())
	}
	profile, _, err := m.prepareNPUModel(RunOptions{NPUDevices: view.NPUDevices, NPUModel: view.NPUModel}, ctx)
	if interrupted := ctx.Err(); interrupted != nil {
		view.Status = "not_checked"
		return block("preflight_interrupted", "预检未完成", interrupted.Error())
	}
	if err != nil {
		return block("preflight_failed", "设备或运行环境预检", err.Error())
	}
	view.ConfigurationSHA256 = profile.ConfigurationSHA256
	for _, check := range profile.Assets {
		if !check.Required || (check.Status != CheckFail && check.Status != CheckUnsupported) {
			continue
		}
		// An earlier typed reference/dependency failure prevented the HBM
		// screen from running; do not mislabel that as insufficient memory.
		if check.Name == "model_memory_preflight" && parameterValue(profile, "model_preflight_code") != "" &&
			check.Message == "basic HBM screening was not completed" {
			continue
		}
		code := check.Name
		if check.Name == "model_reference_preflight" {
			if reason := parameterValue(profile, "model_preflight_code"); reason != "" {
				code = reason
			}
		}
		view.Failures = append(view.Failures, ModelPreflightFailure{Code: code, Label: modelPreflightLabel(code), Message: check.Message})
	}
	if (profile.Preflight.Status == CheckFail || profile.Preflight.Status == CheckUnsupported) && len(view.Failures) == 0 {
		return block("preflight_failed", "模型预检", profile.Preflight.Message)
	}
	m.mu.Lock()
	active = m.active != nil
	m.mu.Unlock()
	if active {
		view.Failures = []ModelPreflightFailure{}
		view.Status = "not_checked"
		return block("controller_busy", "已有新作业启动", "预检不是资源预约；请在作业结束后重新检查。")
	}
	if len(view.Failures) == 0 {
		view.Status = "ready"
	}
	view.CheckedAt = time.Now()
	return view
}

func modelPreflightLabel(code string) string {
	switch code {
	case "reference_missing":
		return "参考未准备"
	case "reference_mismatch":
		return "参考与当前参数不匹配"
	case "reference_incomplete":
		return "参考数据不完整"
	case "reference_exists":
		return "参考 ID 已使用"
	case "dependency_unavailable":
		return "模型依赖未就绪"
	case "model_reference_preflight":
		return "模型依赖或参考校验"
	case "model_memory_preflight":
		return "基础显存检查"
	case "idle_devices":
		return "设备空闲检查"
	default:
		return "运行环境检查（" + code + "）"
	}
}
