package health

import (
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// mkComp 构造指定部件的一条健康指标。
func mkComp(component, name string, value float64, labels map[string]string) collector.Metric {
	if labels == nil {
		labels = map[string]string{}
	}
	return collector.Metric{Component: component, Name: name, Value: value, Labels: labels, Timestamp: time.Now()}
}

func chassisMetrics() []collector.Metric {
	return []collector.Metric{
		mkComp("chassis", "inlet_temp", 25, map[string]string{"sensor": "inlet"}),
		mkComp("chassis", "outlet_temp", 30, map[string]string{"sensor": "outlet"}),
	}
}

// 无 BMC（无 chassis 指标）时 Chassis 权重并入 CPU：
// cpu_only/2card/4card/8card 下 CPU 有效满额变为 35/30/25/25（WEIGHT_SPEC §2.4）。
func TestNoBMCWeightTransfer(t *testing.T) {
	cases := []struct {
		scheme      WeightScheme
		wantCPUMax  int
		wantChasMax int
	}{
		{CPUOnlyScheme, 35, 0},
		{Accelerated2CardScheme, 30, 0},
		{Accelerated4CardScheme, 25, 0},
		{Accelerated8CardScheme, 25, 0},
	}
	for _, tc := range cases {
		// 输入不含任何 chassis 指标。
		metrics := []collector.Metric{
			mkComp("cpu", "usage", 45, map[string]string{"core": "total"}),
			mkComp("memory", "usage", 50, nil),
			mkComp("disk", "space_usage", 40, nil),
			mkComp("network", "error_count", 0, nil),
		}
		score := NewEvaluator(tc.scheme).Evaluate(metrics)
		comp, ok := score.Components["cpu"]
		if !ok || comp.Max != tc.wantCPUMax {
			t.Errorf("scheme %+v 无 BMC: cpu Max = %v(ok=%v), want %d", tc.scheme, comp, ok, tc.wantCPUMax)
		}
		if chas, ok := score.Components["chassis"]; ok && chas.Max != tc.wantChasMax {
			t.Errorf("scheme %+v 无 BMC: chassis Max = %v, want %d", tc.scheme, chas.Max, tc.wantChasMax)
		}
	}
}

// 有 chassis 指标时权重不转移（四方案原值）。
func TestNoTransferWhenChassisPresent(t *testing.T) {
	cases := []struct {
		scheme     WeightScheme
		wantCPUMax int
	}{
		{CPUOnlyScheme, 25},
		{Accelerated8CardScheme, 15},
	}
	for _, tc := range cases {
		metrics := []collector.Metric{
			mkComp("cpu", "usage", 45, map[string]string{"core": "total"}),
			mkComp("memory", "usage", 50, nil),
			mkComp("disk", "space_usage", 40, nil),
		}
		metrics = append(metrics, chassisMetrics()...)
		score := NewEvaluator(tc.scheme).Evaluate(metrics)
		if comp, ok := score.Components["cpu"]; !ok || comp.Max != tc.wantCPUMax {
			t.Errorf("scheme %+v 有 chassis: cpu Max = %v(ok=%v), want %d", tc.scheme, comp, ok, tc.wantCPUMax)
		}
		if chas, ok := score.Components["chassis"]; !ok || chas.Max != 10 {
			t.Errorf("scheme %+v 有 chassis: chassis Max = %v(ok=%v), want 10", tc.scheme, chas, ok)
		}
	}
}

// 转移仅取决于 chassis 分组存在性，与指标值无关：
// 有指标（哪怕值为 0）即不转移；分组为空（配置启用采集器但指标缺失）即转移。
func TestTransferGatedByChassisGroupPresence(t *testing.T) {
	withZero := []collector.Metric{
		mkComp("cpu", "usage", 45, map[string]string{"core": "total"}),
		mkComp("chassis", "inlet_temp", 0, map[string]string{"sensor": "inlet"}),
	}
	score := NewEvaluator(CPUOnlyScheme).Evaluate(withZero)
	if comp, ok := score.Components["cpu"]; !ok || comp.Max != 25 {
		t.Errorf("chassis 值为 0 但分组存在: cpu Max = %v(ok=%v), want 25（不转移）", comp, ok)
	}

	emptyGroup := []collector.Metric{
		mkComp("cpu", "usage", 45, map[string]string{"core": "total"}),
		mkComp("memory", "usage", 50, nil),
	}
	score = NewEvaluator(CPUOnlyScheme).Evaluate(emptyGroup)
	if comp, ok := score.Components["cpu"]; !ok || comp.Max != 35 {
		t.Errorf("chassis 分组为空: cpu Max = %v(ok=%v), want 35（转移）", comp, ok)
	}
}

// 检测到 GPU/NPU 指标时自动检测结果优先于传入方案（Evaluate 内部覆盖），
// 且调用幂等（纯函数，两次结果一致）。
func TestAutoDetectOverridesPassedScheme(t *testing.T) {
	metrics := []collector.Metric{
		mkComp("cpu", "usage", 45, map[string]string{"core": "total"}),
		mkComp("memory", "usage", 50, nil),
		mkComp("disk", "space_usage", 40, nil),
		// 4 卡 NPU → 自动检测应选 accelerated_4card（GPU 满额 30）。
		mkComp("npu", "temperature", 40, map[string]string{"npu_id": "0"}),
		mkComp("npu", "temperature", 40, map[string]string{"npu_id": "1"}),
		mkComp("npu", "temperature", 40, map[string]string{"npu_id": "2"}),
		mkComp("npu", "temperature", 40, map[string]string{"npu_id": "3"}),
	}
	e := NewEvaluator(CPUOnlyScheme) // 显式传入 cpu_only，但有 NPU 指标。
	s1 := e.Evaluate(metrics)
	s2 := e.Evaluate(metrics)

	if comp, ok := s1.Components["npu"]; !ok || comp.Max != 30 {
		t.Errorf("auto 覆盖传入方案: npu Max = %v(ok=%v), want 30（4card 档）", comp, ok)
	}
	if s1.Score != s2.Score || len(s1.Components) != len(s2.Components) {
		t.Error("Evaluate 应幂等：两次结果不一致")
	}
}

// npu_num/2 fallback 结果为 0 时取 1（如 npu_num=1 → 1-2 卡档，GPU 满额 20）。
func TestFallbackCardCountZeroBecomesOne(t *testing.T) {
	metrics := []collector.Metric{
		mkComp("cpu", "usage", 45, map[string]string{"core": "total"}),
		mkComp("memory", "usage", 50, nil),
		// npu_num=1 且无 npu_id 标签：1/2=0 → 取 1。
		mkComp("npu", "npu_num", 1, nil),
	}
	score := NewEvaluator(CPUOnlyScheme).Evaluate(metrics)
	comp, ok := score.Components["npu"]
	if !ok || comp.Max != 20 {
		t.Errorf("npu_num=1 fallback: npu Max = %v(ok=%v), want 20（1-2 卡档）", comp, ok)
	}
}
