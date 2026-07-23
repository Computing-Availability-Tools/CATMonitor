// Command slowNodeDetection is the straggler (slow-node) detection tool for
// AI training clusters. It reads Ascend PyTorch Profiler Level0 data (one
// SQLite .db file per NPU device), detects performance-degraded devices
// across four dimensions (compute, communication, CPU, NPU bubble), and
// outputs results as JSON and a human-readable text report.
//
// Optionally, a KPI resource CSV can be provided for lightweight NPU resource
// anomaly detection before the heavy Profiler analysis.
//
// Usage:
//
//	go run . path=/data/dir [degradation=0.3] [--kpi-csv=/path/to/kpi.csv]
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/Computing-Availability-Tools/CATMonitor/features/straggler/config"
	"github.com/Computing-Availability-Tools/CATMonitor/features/straggler/profiling/dataparse"
	"github.com/Computing-Availability-Tools/CATMonitor/features/straggler/profiling/detector"
	"github.com/Computing-Availability-Tools/CATMonitor/features/straggler/report"
	"github.com/Computing-Availability-Tools/CATMonitor/features/straggler/resource"
	"github.com/Computing-Availability-Tools/CATMonitor/features/straggler/utils"
)

func main() {
	// 1. Parse CLI arguments.
	var inputPath string
	var kpiCSVPath string
	degradation := 0.3

	for _, arg := range os.Args[1:] {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]
		switch key {
		case "path":
			inputPath = val
		case "degradation":
			if parsed, err := strconv.ParseFloat(val, 64); err == nil {
				if parsed < 0 {
					fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] WARNING: degradation < 0, using default 0.3\n")
				} else {
					if parsed > 1 {
						fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] WARNING: degradation > 1 may produce unexpected results\n")
					}
					degradation = parsed
				}
			} else {
				fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] WARNING: invalid degradation value, using default 0.3\n")
			}
		case "--kpi-csv":
			kpiCSVPath = val
		}
	}

	// ─────────────────────────────────────────────────────────────────
	// First line of defense: KPI resource anomaly detection (lightweight)
	// ─────────────────────────────────────────────────────────────────
	if kpiCSVPath != "" {
		kpiCfg := resource.DefaultDetectionConfig()
		kpiCfg.SpaceZThreshold = 1 + degradation  // tie to degradation param
		kpiCfg.TimeZThreshold = 1 + degradation*0.8

		fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] === KPI Resource Detection ===\n")
		kpiResult, err := resource.RunDetection(kpiCSVPath, kpiCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] KPI detection failed: %v\n", err)
			fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Falling through to Profiler detection...\n")
		} else {
			// Write KPI results.
			kpiOutputDir := ""
			if inputPath != "" {
				kpiOutputDir = inputPath
			} else {
				kpiOutputDir = "."
			}

			// JSON output.
			jsonPath := kpiOutputDir + "/npu_resource_detection_result.json"
			if err := resource.ExportJSON(kpiResult, jsonPath); err != nil {
				fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Failed to write KPI JSON: %v\n", err)
			}

			// Text report.
			reportDir := kpiOutputDir + "/analysis_result"
			reportContent, err := resource.WriteReport(kpiResult, reportDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Failed to write KPI report: %v\n", err)
			}
			fmt.Print(reportContent)

			// If confirmed anomalies found and we have no Profiler path, exit.
			if resource.HasConfirmedAnomaly(kpiResult.Results) && inputPath == "" {
				fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] KPI detection found confirmed anomalies. Done.\n")
				return
			}

			// If confirmed anomalies found with Profiler path, cross-validate.
			if resource.HasConfirmedAnomaly(kpiResult.Results) && inputPath != "" {
				fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] KPI found anomalies, proceeding to Profiler for cross-validation...\n")
			}

			// If no KPI anomalies and inputPath is set, fall through to Profiler.
			if !resource.HasConfirmedAnomaly(kpiResult.Results) && inputPath != "" {
				fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] KPI found no confirmed anomalies, falling back to Profiler...\n")
			}
		}
	}

	// ─────────────────────────────────────────────────────────────────
	// Second line of defense: Profiler slow-node detection (deep analysis)
	// ─────────────────────────────────────────────────────────────────
	if inputPath == "" {
		if kpiCSVPath == "" {
			fmt.Fprintf(os.Stderr, "Usage: slowNodeDetection path=/your/data/dir [degradation=0.3] [--kpi-csv=/path/to/kpi.csv]\n")
			fmt.Fprintf(os.Stderr, "ERROR: Missing required parameter: path=/your/data/dir\n")
			os.Exit(1)
		}
		// KPI-only mode: already done above.
		return
	}

	// Validate required path.
	if info, err := os.Stat(inputPath); err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "ERROR: Invalid directory: %s (err: %v)\n", inputPath, err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Input path: %s\n", inputPath)
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Degradation: %.2f\n", degradation)

	// 2. Initialize global configuration.
	config.FilePath = inputPath
	config.CalThreshold = 1 + degradation
	config.CommThreshold = 1 + degradation*5

	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] CalThreshold: %.2f, CommThreshold: %.2f\n",
		config.CalThreshold, config.CommThreshold)

	// 3. Data parsing: SQLite → CSV + JSON intermediates.
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Starting data parsing...\n")
	dataparse.DataParsing(inputPath)

	// 4. Get parallel topology from group_info JSON files.
	parallels, validRanks := detector.GetCurDetectionInfo(inputPath)
	if len(parallels) == 0 || len(validRanks) == 0 {
		fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] FATAL: Failed to get parallel domain info or valid ranks\n")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Valid ranks: %d, Parallel domains: %d\n",
		len(validRanks), len(parallels))

	// 5. Get single-snapshot step data from CSV files.
	stepData := detector.GetCurJobLastStepData(validRanks)
	if len(stepData) == 0 {
		fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] FATAL: No valid step data\n")
		os.Exit(1)
	}

	// 6. Run detection pipeline.
	result := detector.DelimitDetection(stepData, parallels, validRanks)
	if result == nil {
		fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] FATAL: Detection returned no results\n")
		os.Exit(1)
	}

	// 7. Write results.
	utils.Write_result(result, parallels)

	// 8. Generate text report.
	report.WriteReport(stepData, parallels, validRanks, inputPath, result, inputPath, degradation)

	fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Detection complete.\n")
}
