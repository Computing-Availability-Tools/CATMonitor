package resource

import "fmt"

// =============================================================================
// Root-Cause Bounding
// =============================================================================

// BoundRootCause diagnoses the root cause for each anomalous card based on
// the pattern of anomalous metrics. Compute-class rules take priority;
// communication rules only fire when compute is clean.
func BoundRootCause(summaries []CardDetectionSummary) []RootCauseResult {
	var results []RootCauseResult

	for _, s := range summaries {
		if s.Quadrant == QuadNormal {
			continue
		}
		if len(s.AnomalyDetails) == 0 && len(s.SecondaryCommAnomalies) == 0 {
			continue
		}

		result := boundOne(s)
		results = append(results, result)
	}

	return results
}

// boundOne diagnoses one card.
func boundOne(s CardDetectionSummary) RootCauseResult {
	switch s.AnomalyCategory {
	case CatCompute:
		return boundCompute(s)
	case CatCommunication:
		return boundCommunication(s)
	default:
		// Some anomalies present but neither compute nor communication classified.
		return boundUnknown(s)
	}
}

// =============================================================================
// Compute Root-Cause Rules (C1-C10)
// =============================================================================

// computeRule defines a root-cause matching rule for compute-class anomalies.
type computeRule struct {
	category   RootCauseCategory
	confidence Confidence
	suggestion string
	// match returns true if the anomaly pattern matches this rule.
	match func(anom map[MetricName]*MetricAnomalyDetail) bool
}

var computeRules = []computeRule{
	// C1: TEMP↑ + FREQ↓ → thermal throttling.
	{
		category: RcThermalThrottle, confidence: ConfHigh,
		suggestion: "热降频。检查风扇转速/风道堵塞/机房环境温度",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			return isAbnormal(a, MetricTemp, true) && isAbnormal(a, MetricAICoreFreq, true)
		},
	},
	// C2: TEMP↑ + POWER↑ + FREQ normal → cooling insufficient.
	{
		category: RcCoolingInsufficient, confidence: ConfHigh,
		suggestion: "散热能力不足。检查散热器接触/硅脂老化/风扇故障",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			return isAbnormal(a, MetricTemp, true) && isAbnormal(a, MetricPower, true) &&
				!isAbnormal(a, MetricAICoreFreq, true)
		},
	},
	// C3: FREQ↓ + TEMP normal → forced downclock (non-thermal).
	{
		category: RcForcedDownclock, confidence: ConfMedium,
		suggestion: "强制降频（非热）。检查驱动/固件的频率策略配置",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			return isAbnormal(a, MetricAICoreFreq, true) && !isAbnormal(a, MetricTemp, true)
		},
	},
	// C4: POWER↓ + AICORE_UTIL↓ + HBM_UTIL↓ → straggler.
	{
		category: RcStraggler, confidence: ConfHigh,
		suggestion: "Straggler（卡空闲等待）。该卡可能在等通信/等数据，建议触发Profiling精查",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			return isAbnormal(a, MetricPower, true) && isAbnormal(a, MetricAICoreUtil, true) &&
				isAbnormal(a, MetricHBMUtil, true)
		},
	},
	// C5: AICORE_UTIL↓ + HBM_UTIL normal → load imbalance.
	{
		category: RcLoadImbalance, confidence: ConfMedium,
		suggestion: "计算负载不均。检查数据分发策略/模型并行切分是否均衡",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			return isAbnormal(a, MetricAICoreUtil, true) && !isAbnormal(a, MetricHBMUtil, true)
		},
	},
	// C6: HBM_UTIL↓ + AICORE_UTIL normal → memory bandwidth bottleneck.
	{
		category: RcMemBottleneck, confidence: ConfLow,
		suggestion: "内存带宽瓶颈。检查HBM访问模式/是否有大量cache miss",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			return isAbnormal(a, MetricHBMUtil, true) && !isAbnormal(a, MetricAICoreUtil, true)
		},
	},
	// C7: TEMP↑ + POWER normal + FREQ normal → temp sensor drift.
	{
		category: RcTempSensorFault, confidence: ConfMedium,
		suggestion: "温度传感器漂移。交叉验证功率数据（真发热必伴随功率↑）",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			return isAbnormal(a, MetricTemp, true) && !isAbnormal(a, MetricPower, true) &&
				!isAbnormal(a, MetricAICoreFreq, true)
		},
	},
	// C8: ≥4 metrics abnormal → comprehensive hardware fault.
	{
		category: RcHardwareFault, confidence: ConfHigh,
		suggestion: "多指标综合异常，建议隔离该卡，安排硬件诊断/更换",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			count := 0
			for _, m := range AllMetrics {
				if IsComputeMetric(m) && isAbnormal(a, m, true) {
					count++
				}
			}
			return count >= 4
		},
	},
	// C9: Isolated TEMP↑ → local hotspot / sensor variance.
	{
		category: RcTempSensorFault, confidence: ConfLow,
		suggestion: "单项温度偏高（可能为局部热点/传感器个体差异）。持续观察，若升级为双维异常则进一步排查",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			return isAbnormal(a, MetricTemp, true) &&
				countAbnormalCompute(a) == 1
		},
	},
	// C10: Isolated POWER↑ → power measurement bias.
	{
		category: RcUnknown, confidence: ConfLow,
		suggestion: "单项功耗偏高。交叉验证：功率↑应伴随温度↑，否则可能是计量误差",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			return isAbnormal(a, MetricPower, true) &&
				!isAbnormal(a, MetricTemp, true) &&
				countAbnormalCompute(a) == 1
		},
	},
}

// =============================================================================
// Communication Root-Cause Rules (N1-N4)
// =============================================================================

// commRule defines a root-cause matching rule for communication-class anomalies.
type commRule struct {
	category   RootCauseCategory
	confidence Confidence
	suggestion string
	match      func(anom map[MetricName]*MetricAnomalyDetail) bool
}

var commRules = []commRule{
	// N1: ERR_PKT↑ → physical link fault.
	{
		category: RcNetworkLinkIssue, confidence: ConfHigh,
		suggestion: "网络物理链路故障。检查光模块/光纤/交换机端口CRC错误",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			return isAbnormal(a, MetricRocETxErrPkt, true)
		},
	},
	// N2: PFC_PKT↑ → network congestion (PFC storm).
	{
		category: RcNetworkCongestion, confidence: ConfHigh,
		suggestion: "网络拥塞（PFC风暴）。检查交换机PFC配置/队列buffer/ECN标记",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			return isAbnormal(a, MetricRXPfcPkt, true)
		},
	},
	// N3: OUT_OF_ORDER↑ + RETRY↑ → packet loss/disorder.
	{
		category: RcNetworkPacketLoss, confidence: ConfHigh,
		suggestion: "RoCE网络丢包乱序。检查RoCE路径ECN配置/DCQCN参数",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			return isAbnormal(a, MetricRocEOutOfOrder, true) && isAbnormal(a, MetricRocENewPktRty, true)
		},
	},
	// N4: TX_BANDWIDTH↓ + AICORE_UTIL normal → bandwidth limited.
	{
		category: RcBandwidthLimited, confidence: ConfMedium,
		suggestion: "通信带宽受限。检查网卡协商速率/PCIe带宽/光模块型号",
		match: func(a map[MetricName]*MetricAnomalyDetail) bool {
			return isAbnormal(a, MetricTXBandwidth, true) && !isAbnormal(a, MetricAICoreUtil, true)
		},
	},
}

// =============================================================================
// Rule Matching
// =============================================================================

func boundCompute(s CardDetectionSummary) RootCauseResult {
	anomMap := detailsToMap(s.AnomalyDetails)

	// Try rules in order (C1 through C10).
	for _, rule := range computeRules {
		if rule.match(anomMap) {
			return RootCauseResult{
				CardID:     s.CardID,
				Category:   rule.category,
				Confidence: rule.confidence,
				Evidence:   s.AnomalyDetails,
				Suggestion: rule.suggestion,
			}
		}
	}

	// Fallback: unknown compute issue.
	return RootCauseResult{
		CardID:     s.CardID,
		Category:   RcUnknown,
		Confidence: ConfMedium,
		Evidence:   s.AnomalyDetails,
		Suggestion: fmt.Sprintf("计算类异常（%d个指标），但无法精确定界，建议人工分析", len(s.AnomalyDetails)),
	}
}

func boundCommunication(s CardDetectionSummary) RootCauseResult {
	anomMap := detailsToMap(s.AnomalyDetails)

	for _, rule := range commRules {
		if rule.match(anomMap) {
			ev := s.AnomalyDetails
			if len(ev) == 0 {
				ev = s.SecondaryCommAnomalies
			}
			return RootCauseResult{
				CardID:     s.CardID,
				Category:   rule.category,
				Confidence: rule.confidence,
				Evidence:   ev,
				Suggestion: rule.suggestion,
			}
		}
	}

	// Fallback.
	ev := s.AnomalyDetails
	if len(ev) == 0 {
		ev = s.SecondaryCommAnomalies
	}
	return RootCauseResult{
		CardID:     s.CardID,
		Category:   RcUnknown,
		Confidence: ConfLow,
		Evidence:   ev,
		Suggestion: "通信类异常，但无法精确定界，建议人工分析",
	}
}

func boundUnknown(s CardDetectionSummary) RootCauseResult {
	ev := s.AnomalyDetails
	if len(ev) == 0 {
		ev = s.SecondaryCommAnomalies
	}
	return RootCauseResult{
		CardID:     s.CardID,
		Category:   RcUnknown,
		Confidence: ConfLow,
		Evidence:   ev,
		Suggestion: "异常指标不足以归类，建议人工分析",
	}
}

// =============================================================================
// Helpers
// =============================================================================

// detailsToMap converts a slice of MetricAnomalyDetail to a lookup map.
func detailsToMap(details []MetricAnomalyDetail) map[MetricName]*MetricAnomalyDetail {
	m := make(map[MetricName]*MetricAnomalyDetail)
	for i := range details {
		m[details[i].Metric] = &details[i]
	}
	return m
}

// isAbnormal checks if a metric is abnormal (space or time).
// If 'any' is true, either dimension counts. Otherwise both must be true.
func isAbnormal(a map[MetricName]*MetricAnomalyDetail, m MetricName, any bool) bool {
	d, ok := a[m]
	if !ok {
		return false
	}
	if any {
		return d.SpaceAbnormal || d.TimeAbnormal
	}
	return d.SpaceAbnormal && d.TimeAbnormal
}

// countAbnormalCompute counts how many compute metrics are abnormal.
func countAbnormalCompute(a map[MetricName]*MetricAnomalyDetail) int {
	count := 0
	for m, d := range a {
		if IsComputeMetric(m) && (d.SpaceAbnormal || d.TimeAbnormal) {
			count++
		}
	}
	return count
}

// =============================================================================
// Cross-Card Correlation Analysis
// =============================================================================

// CrossCardCorrelation analyzes whether anomalous cards are correlated
// (same node, same network domain, all cards, etc.).
func CrossCardCorrelation(_ []RootCauseResult, summaries []CardDetectionSummary) []CorrelationResult {
	var results []CorrelationResult

	anomalousCards := make([]int, 0)
	for _, s := range summaries {
		if s.Quadrant == QuadConfirmedAnomaly {
			anomalousCards = append(anomalousCards, s.CardID)
		}
	}

	n := len(anomalousCards)
	total := len(summaries)

	if n == 0 {
		return nil
	}

	// Job-level: all cards anomalous.
	if n == total && total > 1 {
		results = append(results, CorrelationResult{
			Type:        "job_level",
			Description: "所有卡均出现异常，可能是任务级故障（训练hang/机房环境问题）",
			CardIDs:     anomalousCards,
			Confidence:  ConfMedium,
		})
		return results
	}

	// Card-level: isolated cards.
	if n == 1 {
		results = append(results, CorrelationResult{
			Type:        "card_level",
			Description: "仅单卡异常，板卡级故障可能性大",
			CardIDs:     anomalousCards,
			Confidence:  ConfHigh,
		})
		return results
	}

	// Multiple but not all: card-level.
	results = append(results, CorrelationResult{
		Type:        "card_level",
		Description: fmt.Sprintf("%d/%d张卡异常，需逐卡排查", n, total),
		CardIDs:     anomalousCards,
		Confidence:  ConfMedium,
	})

	return results
}
