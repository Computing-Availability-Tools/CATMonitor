// Package cli implements the catmonitor stress command adapter.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/config"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/platform"
)

// Run parses and executes the top-level catmonitor stress command.
func Run(args []string, logger *slog.Logger, stdout, stderr io.Writer) int {
	if helpRequested(args) {
		printUsage(stdout)
		return 0
	}

	configPath, names, output, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "stress:", err)
		return 2
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, "stress: load config:", err)
		return 1
	}

	manager := stress.NewManagerWithLogger(cfg.Stress, logger)
	report, err := manager.StartWithOptions(names, stress.RunOptions{Initiator: stress.InitiatorCLI})
	if err != nil {
		if errors.Is(err, stress.ErrBusy) && report.JobID != "" {
			fmt.Fprintf(stderr, "stress: %v (job_id=%s initiator=%s)\n", err, report.JobID, report.Initiator)
			return 1
		}
		fmt.Fprintln(stderr, "stress:", err)
		return 1
	}
	for report.Status == stress.StatusRunning {
		time.Sleep(200 * time.Millisecond)
		report, err = manager.Job(report.JobID)
		if err != nil {
			fmt.Fprintln(stderr, "stress:", err)
			return 1
		}
	}

	if output == "table" {
		printTable(stdout, report)
	} else {
		data, _ := json.MarshalIndent(report, "", "  ")
		fmt.Fprintln(stdout, string(data))
	}
	if report.Status != stress.StatusHealthy {
		return 1
	}
	return 0
}

func helpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, `Usage:
  catmonitor stress [--bench hpl,hpcg,stream] [-c config.yaml] [-o json|table]

Run explicitly enabled Linux stress benchmarks.
Without --bench, run default_benchmarks from the CATMonitor configuration.
Without --config, load CATMONITOR_CONFIG or the platform default path.

Options:
  -b, --bench       Comma-separated benchmark names
  -c, --config      CATMonitor configuration file path
  -o, --output      json (default) or table
  -h, --help        Show this help`)
}

func parseArgs(args []string) (string, []string, string, error) {
	fs := flag.NewFlagSet("stress", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := platform.ConfigPath()
	benchmarks := ""
	output := "json"
	fs.StringVar(&configPath, "config", configPath, "CATMonitor configuration file")
	fs.StringVar(&configPath, "c", configPath, "CATMonitor configuration file")
	fs.StringVar(&benchmarks, "bench", benchmarks, "comma-separated benchmarks")
	fs.StringVar(&benchmarks, "b", benchmarks, "comma-separated benchmarks")
	fs.StringVar(&output, "output", output, "json or table")
	fs.StringVar(&output, "o", output, "json or table")
	if err := fs.Parse(args); err != nil {
		return "", nil, "", err
	}
	if fs.NArg() != 0 {
		return "", nil, "", fmt.Errorf("unknown argument %q", fs.Arg(0))
	}
	if output != "json" && output != "table" {
		return "", nil, "", fmt.Errorf("output must be json or table")
	}

	var names []string
	if benchmarks != "" {
		for _, name := range strings.Split(benchmarks, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				return "", nil, "", fmt.Errorf("benchmark names cannot be empty")
			}
			names = append(names, name)
		}
	}
	return configPath, names, output, nil
}

func printTable(output io.Writer, report stress.Report) {
	fmt.Fprintf(output, "\nCATMonitor Stress Report  %s\n", report.HealthCondition)
	w := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Benchmark\tStatus\tDuration\tMetric\tValue\tMessage")
	for _, result := range report.Benchmarks {
		keys := make([]string, 0, len(result.Values))
		for key := range result.Values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			fmt.Fprintf(w, "%s\t%s\t%s\t-\t-\t%s\n",
				result.Name, statusLabel(result.Status), formatDuration(result.DurationMS), result.Message)
			continue
		}
		for i, key := range keys {
			name, status, duration, message := "", "", "", ""
			if i == 0 {
				name = result.Name
				status = statusLabel(result.Status)
				duration = formatDuration(result.DurationMS)
				message = result.Message
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				name, status, duration, key, formatValue(result.Values[key]), message)
		}
	}
	_ = w.Flush()
}

func statusLabel(status stress.Status) string {
	switch status {
	case stress.StatusHealthy:
		return "OK"
	case stress.StatusTimeLimitReached:
		return "OK (time limit)"
	case stress.StatusUnhealthy:
		return "FAILED"
	default:
		return strings.ToUpper(string(status))
	}
}

func formatDuration(milliseconds int64) string {
	return (time.Duration(milliseconds) * time.Millisecond).String()
}

func formatValue(value float64) string {
	if math.Trunc(value) == value {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%.2f", value)
}
