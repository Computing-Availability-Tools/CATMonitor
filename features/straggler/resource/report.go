package resource

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// =============================================================================
// Detection Pipeline (orchestrator)
// =============================================================================

// RunDetection executes the full KPI detection pipeline and returns the result.
//
// Pipeline:
//  1. ParseCSV
//  2. AggregateByMinute
//  3. SplitWindows
//  4. BuildBaselines
//  5. Space detection (peer comparison) on detection window
//  6. Time detection (self comparison) against baselines
//  7. FuseAndSummarize (compute-first 2D cross-validation)
//  8. BoundRootCause
//  9. CrossCardCorrelation
func RunDetection(csvPath string, cfg DetectionConfig) (*DetectionResult, error) {
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] KPI detection starting: %s\n", csvPath)

	// 1. Parse CSV.
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Step 1/9: Parsing CSV...\n")
	rawData, err := ParseCSV(csvPath)
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Parsed %d raw rows, %d cards\n",
		len(rawData.Rows), len(rawData.CardIDs))

	// 2. Aggregate by minute.
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Step 2/9: Aggregating by minute (trimmed mean)...\n")
	aggregated, err := AggregateByMinute(rawData.RawRows, rawData.CardIDs, cfg)
	if err != nil {
		return nil, fmt.Errorf("aggregate: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Aggregated to %d minute-level rows\n", len(aggregated))

	rawData.Rows = aggregated

	// 3. Split windows.
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Step 3/9: Splitting windows (baseline=%.0fh, detection=%.0fh)...\n",
		cfg.BaselineHours, cfg.DetectionHours)
	baselineRows, detectionRows := SplitWindows(aggregated, cfg)
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Baseline: %d rows, Detection: %d rows\n",
		len(baselineRows), len(detectionRows))

	if len(detectionRows) == 0 {
		return nil, fmt.Errorf("no data in detection window (need at least 1 minute in the last %.0fh)", cfg.DetectionHours)
	}

	// 4. Build baselines.
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Step 4/9: Building historical baselines...\n")
	baselines := BuildBaselines(baselineRows, rawData.CardIDs)
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Built baselines for %d cards\n", len(baselines))

	// 5. Space detection.
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Step 5/9: Space (peer) detection...\n")
	spaceResult := detectSpaceAnomalies(detectionRows, rawData.CardIDs, cfg)
	spaceDetails := aggregateSpaceScores(spaceResult, rawData.CardIDs, cfg)

	// 6. Time detection.
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Step 6/9: Time (self) detection...\n")
	timeResult := detectTimeAnomalies(detectionRows, baselines, rawData.CardIDs, cfg)
	timeDetails := aggregateTimeScores(timeResult, detectionRows, baselines, rawData.CardIDs, cfg)

	// 7. Fuse + compute-first ordering.
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Step 7/9: 2D cross-validation (compute-first)...\n")
	trends := detectTrends(aggregated, rawData.CardIDs, cfg)
	summaries := FuseAndSummarize(spaceDetails, timeDetails, trends, rawData.CardIDs, cfg)

	// 8. Root cause.
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Step 8/9: Root-cause bounding...\n")
	rootCauses := BoundRootCause(summaries)

	// 9. Cross-card correlation.
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Step 9/9: Cross-card correlation...\n")
	correlations := CrossCardCorrelation(rootCauses, summaries)

	// Build result.
	confirmed := 0
	earlyDeg := 0
	indiv := 0
	normal := 0
	for _, s := range summaries {
		switch s.Quadrant {
		case QuadConfirmedAnomaly:
			confirmed++
		case QuadEarlyDegradation:
			earlyDeg++
		case QuadIndividualVariance:
			indiv++
		default:
			normal++
		}
	}

	result := &DetectionResult{
		Summary: DetectionSummary{
			TotalCards:         len(rawData.CardIDs),
			ConfirmedAnomalies: confirmed,
			EarlyDegradation:   earlyDeg,
			IndividualVariance: indiv,
			Normal:             normal,
			KPICSV:             csvPath,
			TotalTimePoints:    len(aggregated),
			BaselineWindow:     fmt.Sprintf("%.0fh", cfg.BaselineHours),
			DetectionWindow:    fmt.Sprintf("%.0fh", cfg.DetectionHours),
			DetectionMethod:    string(cfg.SpaceMethod),
		},
		Results:      summaries,
		RootCauses:   rootCauses,
		Correlations: correlations,
	}

	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] KPI detection complete: confirmed=%d early=%d variance=%d normal=%d\n",
		confirmed, earlyDeg, indiv, normal)

	return result, nil
}

// =============================================================================
// JSON Export
// =============================================================================

// ExportJSON writes the detection result as JSON.
func ExportJSON(result *DetectionResult, outputPath string) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] KPI result JSON written to %s\n", outputPath)
	return nil
}

// =============================================================================
// Text Report
// =============================================================================

// WriteReport generates a human-readable text report.
func WriteReport(result *DetectionResult, outputDir string) (string, error) {
	var b strings.Builder

	b.WriteString("================================================================================\n")
	b.WriteString("  NPU 资源 KPI 异常检测报告\n")
	b.WriteString("================================================================================\n\n")

	// Summary.
	b.WriteString("[SUMMARY]\n")
	fmt.Fprintf(&b, "  CSV:        %s\n", result.Summary.KPICSV)
	fmt.Fprintf(&b, "  数据点:     %d (分钟级)\n", result.Summary.TotalTimePoints)
	fmt.Fprintf(&b, "  基线窗口:   %s\n", result.Summary.BaselineWindow)
	fmt.Fprintf(&b, "  检测窗口:   %s\n", result.Summary.DetectionWindow)
	fmt.Fprintf(&b, "  检测方法:   %s\n", result.Summary.DetectionMethod)
	fmt.Fprintf(&b, "  总卡数:     %d\n", result.Summary.TotalCards)
	fmt.Fprintf(&b, "  ✓ 正常:     %d\n", result.Summary.Normal)
	fmt.Fprintf(&b, "  ✗ 确认异常: %d\n", result.Summary.ConfirmedAnomalies)
	fmt.Fprintf(&b, "  ⚡ 早期劣化: %d\n", result.Summary.EarlyDegradation)
	fmt.Fprintf(&b, "  ◇ 个体差异: %d\n", result.Summary.IndividualVariance)
	b.WriteString("\n")

	// Confirmed anomalies.
	if len(result.RootCauses) > 0 {
		b.WriteString("================================================================================\n")
		b.WriteString("  确认异常详情\n")
		b.WriteString("================================================================================\n\n")

		for _, rc := range result.RootCauses {
			fmt.Fprintf(&b, "  Card %d | %s | 置信度: %s\n", rc.CardID, rc.Category, rc.Confidence)
			fmt.Fprintf(&b, "  建议: %s\n", rc.Suggestion)
			if len(rc.Evidence) > 0 {
				b.WriteString("  异常指标:\n")
				for _, e := range rc.Evidence {
					fmt.Fprintf(&b, "    %-20s space=%.1f time=%.1f quadrant=%s current=%.1f baseline=%.1f\n",
						e.Metric, e.SpaceScore, e.TimeScore, e.Quadrant, e.CurrentMean, e.BaselineMean)
				}
			}
			b.WriteString("\n")
		}
	}

	// Early degradation (watch list).
	earlyCards := filterByQuadrant(result.Results, QuadEarlyDegradation)
	if len(earlyCards) > 0 {
		b.WriteString("================================================================================\n")
		b.WriteString("  早期劣化（关注列表）\n")
		b.WriteString("================================================================================\n\n")
		for _, s := range earlyCards {
			fmt.Fprintf(&b, "  Card %d | score=%.2f\n", s.CardID, s.CompositeScore)
			for _, d := range s.AnomalyDetails {
				if d.TimeAbnormal {
					fmt.Fprintf(&b, "    %-20s time_z=%.1f current=%.1f baseline=%.1f±%.1f\n",
						d.Metric, d.TimeScore, d.CurrentMean, d.BaselineMean, 0.0)
				}
			}
			if len(s.TrendFindings) > 0 {
				for _, t := range s.TrendFindings {
					fmt.Fprintf(&b, "    [趋势] %s (R²=%.2f)\n", t.Desc, t.RSquared)
				}
			}
			b.WriteString("\n")
		}
	}

	// Correlations.
	if len(result.Correlations) > 0 {
		b.WriteString("================================================================================\n")
		b.WriteString("  跨卡关联分析\n")
		b.WriteString("================================================================================\n\n")
		for _, c := range result.Correlations {
			fmt.Fprintf(&b, "  [%s] %s (置信度: %s)\n", c.Type, c.Description, c.Confidence)
			fmt.Fprintf(&b, "  涉及卡: %v\n\n", c.CardIDs)
		}
	}

	b.WriteString("================================================================================\n")
	b.WriteString("  报告结束\n")
	b.WriteString("================================================================================\n")

	reportContent := b.String()

	// Write to file.
	outPath := filepath.Join(outputDir, "npu_resource_detection_report.log")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return reportContent, fmt.Errorf("create output dir: %w", err)
	}
	if err := os.WriteFile(outPath, []byte(reportContent), 0644); err != nil {
		return reportContent, fmt.Errorf("write report: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] KPI report written to %s\n", outPath)

	return reportContent, nil
}

// =============================================================================
// Helpers
// =============================================================================

func filterByQuadrant(summaries []CardDetectionSummary, q Quadrant) []CardDetectionSummary {
	var result []CardDetectionSummary
	for _, s := range summaries {
		if s.Quadrant == q {
			result = append(result, s)
		}
	}
	return result
}
