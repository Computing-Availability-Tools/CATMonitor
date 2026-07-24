// Package report generates a human-readable detection report with bar charts.
package report

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	barChar     = "█"
	barMaxWidth = 40
	topN        = 30
	bottomN     = 5
	reportSep   = 80
)

// ---------------------------------------------------------------------------
// WriteReport — public entry point
// ---------------------------------------------------------------------------

// WriteReport generates a text report and writes it to
// analysis_result/detection_report.log under outputDir.
// Returns the path to the written report file.
func WriteReport(
	stepData map[string]map[int]float64,
	parallels map[string][][]int,
	validRanks []int,
	outputDir string,
	detectionResult map[string]map[string]float64,
	inputPath string,
	degradation float64,
) string {
	report := GenerateReport(stepData, parallels, validRanks, detectionResult, inputPath, degradation)

	outDir := filepath.Join(outputDir, "analysis_result")
	os.MkdirAll(outDir, 0755)
	outPath := filepath.Join(outDir, "detection_report.log")
	if err := os.WriteFile(outPath, []byte(report), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[REPORT] Failed to write report: %v\n", err)
		return ""
	}
	fmt.Fprintf(os.Stderr, "[REPORT] Report written to %s\n", outPath)
	return outPath
}

// GenerateReport builds the full report text and returns it as a string.
func GenerateReport(
	stepData map[string]map[int]float64,
	parallels map[string][][]int,
	validRanks []int,
	detectionResult map[string]map[string]float64,
	inputPath string,
	degradation float64,
) string {
	var sb strings.Builder

	// Header.
	sb.WriteString(sepLine("慢节点检测报告", reportSep))
	sb.WriteString(fmt.Sprintf("\n  数据目录: %s\n", inputPath))
	sb.WriteString(fmt.Sprintf("  生成时间: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("  有效 Rank 数: %d\n\n", len(validRanks)))

	// Topology summary.
	sb.WriteString(sepLine("并行域拓扑", 60))
	for domain, groups := range parallels {
		sb.WriteString(fmt.Sprintf("  %s: %d 个 Group\n", domain, len(groups)))
	}
	sb.WriteString("\n")

	// Detection summary.
	sb.WriteString(sepLine("检测结果摘要", reportSep))
	sb.WriteString(detectionSummary(detectionResult, validRanks, degradation))
	sb.WriteString("\n")

	// ZP_Kernel section.
	if kernelData, ok := stepData["ZP_Kernel"]; ok {
		abnormal := abnormalSingleRanks(detectionResult["cal"])
		sb.WriteString(metricSection("ZP_Kernel 耗时排序", kernelData, abnormal))
		sb.WriteString("\n")
	}

	// ZP_Host section.
	if hostData, ok := stepData["ZP_Host"]; ok {
		abnormal := abnormalSingleRanks(detectionResult["cpu"])
		sb.WriteString(metricSection("ZP_Host 耗时排序", hostData, abnormal))
		sb.WriteString("\n")
	}

	// Communication sections.
	sb.WriteString(commTotalSection(stepData, parallels))
	sb.WriteString("\n")

	for domain, groups := range parallels {
		if domain == "pp" || domain == "embd" {
			continue
		}
		colName := domain + "_Duration"
		if commData, ok := stepData[colName]; ok {
			abnormalGroups := abnormalCommGroups(detectionResult["comm"])
			sb.WriteString(commSection(domain, groups, commData, abnormalGroups))
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// Section builders
// ---------------------------------------------------------------------------

func metricSection(metricName string, data map[int]float64, abnormalRanks map[int]bool) string {
	valid := filterValid(data)
	if len(valid) == 0 {
		return ""
	}

	// Sort by value descending.
	type kv struct {
		rank  int
		value float64
	}
	var sorted []kv
	for rank, v := range valid {
		sorted = append(sorted, kv{rank, value: v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].value > sorted[j].value })

	maxVal := sorted[0].value

	var sb strings.Builder
	sb.WriteString(sepLine(metricName, 80))

	top := sorted
	bottom := sorted[max(0, len(sorted)-bottomN):]
	if len(sorted) > topN+bottomN {
		top = sorted[:topN]
	}

	var values []float64
	for _, kv := range sorted {
		values = append(values, kv.value)
	}
	med := median(values)

	sb.WriteString(fmt.Sprintf("  Max: %s  Min: %s  Mean: %s  Median: %s\n\n",
		fmtNs(maxVal), fmtNs(sorted[len(sorted)-1].value),
		fmtNs(mean(values)), fmtNs(med)))

	sb.WriteString(fmt.Sprintf("  %-4s  %-6s  %12s  %8s  %s\n", "#", "Rank", "耗时", "劣化指数", "柱状图"))
	sb.WriteString(fmt.Sprintf("  %-4s  %-6s  %12s  %8s  %s\n", "---", "------", "----------", "--------", strings.Repeat("-", barMaxWidth)))

	printRank := func(idx int, kv kv) {
		deg := kv.value / valid[minValue(valid)]
		if deg == 0 {
			deg = 1
		}
		marker := ""
		if abnormalRanks[kv.rank] {
			marker = " ***"
		}
		sb.WriteString(fmt.Sprintf("  %-4d  %-6d  %12s  %7.2fx  %s%s\n",
			idx+1, kv.rank, fmtNs(kv.value), deg, bar(kv.value, maxVal), marker))
	}

	for i, kv := range top {
		printRank(i, kv)
	}

	if len(sorted) > topN+bottomN {
		sb.WriteString(fmt.Sprintf("  ...  (省略 %d 个)\n", len(sorted)-topN-bottomN))
	}

	for i, kv := range bottom {
		printRank(len(sorted)-len(bottom)+i, kv)
	}

	return sb.String()
}

func commSection(domainName string, domainGroups [][]int, commData map[int]float64, abnormalGroups map[string]bool) string {
	var sb strings.Builder
	sb.WriteString(sepLine(domainName+" 通信域分组对比", 80))

	// Sort groups lexicographically.
	sortedGroups := make([][]int, len(domainGroups))
	for i, g := range domainGroups {
		sg := make([]int, len(g))
		copy(sg, g)
		sort.Ints(sg)
		sortedGroups[i] = sg
	}
	sort.Slice(sortedGroups, func(i, j int) bool {
		for k := 0; k < len(sortedGroups[i]) && k < len(sortedGroups[j]); k++ {
			if sortedGroups[i][k] != sortedGroups[j][k] {
				return sortedGroups[i][k] < sortedGroups[j][k]
			}
		}
		return len(sortedGroups[i]) < len(sortedGroups[j])
	})

	var maxMean float64
	type groupStat struct {
		group    []int
		min, max float64
		mean     float64
		abnormal bool
	}
	var stats []groupStat

	for _, g := range sortedGroups {
		var vals []float64
		for _, r := range g {
			if v, ok := commData[r]; ok {
				vals = append(vals, v)
			}
		}
		if len(vals) == 0 {
			continue
		}
		sort.Float64s(vals)
		mn := vals[0]
		mx := vals[len(vals)-1]
		av := mean(vals)

		// Check if abnormal.
		key := joinInts(g, ",")
		ab := abnormalGroups[key]

		stats = append(stats, groupStat{group: g, min: mn, max: mx, mean: av, abnormal: ab})
		if av > maxMean {
			maxMean = av
		}
	}

	if maxMean == 0 {
		maxMean = 1
	}

	sb.WriteString(fmt.Sprintf("  %-20s  %8s  %8s  %8s  %s\n", "Group", "Min", "Mean", "Max", "柱状图"))
	sb.WriteString(fmt.Sprintf("  %-20s  %8s  %8s  %8s  %s\n", strings.Repeat("-", 20), strings.Repeat("-", 8), strings.Repeat("-", 8), strings.Repeat("-", 8), strings.Repeat("-", barMaxWidth)))

	for _, st := range stats {
		marker := ""
		if st.abnormal {
			marker = " ***"
		}
		groupLabel := "[" + joinInts(st.group, ", ") + "]"
		sb.WriteString(fmt.Sprintf("  %-20s  %8s  %8s  %8s  %s%s\n",
			groupLabel, fmtNs(st.min), fmtNs(st.mean), fmtNs(st.max), bar(st.mean, maxMean), marker))
	}

	return sb.String()
}

func commTotalSection(stepData map[string]map[int]float64, parallels map[string][][]int) string {
	// Try to use ZP_Duration or individual domain durations.
	data, ok := stepData["ZP_Duration"]
	if !ok {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(metricSection("总通信时间 (ZP_Duration)", data, nil))
	_ = parallels
	return sb.String()
}

func detectionSummary(
	detectionResult map[string]map[string]float64,
	validRanks []int,
	degradation float64,
) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("  劣化阈值: Cal=%.2f, Comm=%.2f\n\n", 1+degradation, 1+degradation*5))

	sb.WriteString(fmt.Sprintf("  %-22s  %-8s  %-8s  %s\n", "检测类型", "状态", "异常数", "异常详情"))
	sb.WriteString(fmt.Sprintf("  %-22s  %-8s  %-8s  %s\n", strings.Repeat("-", 22), strings.Repeat("-", 8), strings.Repeat("-", 8), strings.Repeat("-", 30)))

	// Ordered categories.
	categories := []struct {
		key, label string
	}{
		{"cal", "慢计算 (cal)"},
		{"comm", "慢通信 (comm)"},
		{"cpu", "慢CPU (cpu)"},
		{"npu_bubble", "Bubble (npu_bubble)"},
	}

	for _, cat := range categories {
		data := detectionResult[cat.key]
		status := "正常"
		count := 0
		var details []string

		if len(data) > 0 {
			status = "异常"
			count = len(data)
			// Build detail strings (truncated).
			for k, v := range data {
				details = append(details, fmt.Sprintf("%s: %.2fx", k, v))
				if len(details) >= 5 {
					break
				}
			}
		}

		detailStr := "-"
		if len(details) > 0 {
			detailStr = strings.Join(details, "; ")
			if len(data) > 5 {
				detailStr += fmt.Sprintf(" ... +%d more", len(data)-5)
			}
		}

		sb.WriteString(fmt.Sprintf("  %-22s  %-8s  %-8d  %s\n", cat.label, status, count, detailStr))
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func fmtNs(value float64) string {
	abs := math.Abs(value)
	switch {
	case abs >= 1e9:
		return fmt.Sprintf("%.2fs", value/1e9)
	case abs >= 1e6:
		return fmt.Sprintf("%.2fms", value/1e6)
	case abs >= 1e3:
		return fmt.Sprintf("%.2fus", value/1e3)
	default:
		return fmt.Sprintf("%.0fns", value)
	}
}

func filterValid(data map[int]float64) map[int]float64 {
	result := make(map[int]float64)
	for k, v := range data {
		if v > 0 && v != -99999.0 {
			result[k] = v
		}
	}
	return result
}

func bar(value, maxValue float64) string {
	if maxValue == 0 {
		return ""
	}
	width := int(math.Round(value / maxValue * float64(barMaxWidth)))
	if width > barMaxWidth {
		width = barMaxWidth
	}
	if width < 1 {
		width = 1
	}
	return strings.Repeat(barChar, width)
}

func sepLine(title string, width int) string {
	prefix := strings.Repeat("=", (width-len(title)-2)/2)
	suffix := strings.Repeat("=", width-len(title)-2-len(prefix))
	return fmt.Sprintf("\n%s %s %s\n", prefix, title, suffix)
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func median(sortedVals []float64) float64 {
	if len(sortedVals) == 0 {
		return 0
	}
	n := len(sortedVals)
	if n%2 == 0 {
		return (sortedVals[n/2-1] + sortedVals[n/2]) / 2
	}
	return sortedVals[n/2]
}

func joinInts(ints []int, sep string) string {
	parts := make([]string, len(ints))
	for i, v := range ints {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, sep)
}

func minValue(data map[int]float64) int {
	var minKey int
	minSet := false
	var minVal float64
	for k, v := range data {
		if !minSet || v < minVal {
			minVal = v
			minKey = k
			minSet = true
		}
	}
	return minKey
}

func abnormalSingleRanks(data map[string]float64) map[int]bool {
	result := make(map[int]bool)
	for k := range data {
		if r, err := strconv.Atoi(k); err == nil {
			result[r] = true
		}
	}
	return result
}

func abnormalCommGroups(data map[string]float64) map[string]bool {
	result := make(map[string]bool)
	for k := range data {
		result[k] = true
	}
	return result
}
