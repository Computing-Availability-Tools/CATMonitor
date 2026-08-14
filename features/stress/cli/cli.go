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
	mode := "run"
	if len(args) > 0 && args[0] == "doctor" {
		mode = "doctor"
		args = args[1:]
	}
	if helpRequested(args) {
		if mode == "doctor" {
			printDoctorUsage(stdout)
		} else {
			printUsage(stdout)
		}
		return 0
	}
	if mode == "doctor" {
		return runDoctor(args, logger, stdout, stderr)
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
  catmonitor stress [--bench hpl,hpcg,stream,npu_burn] [-c config.yaml] [-o json|table]
  catmonitor stress doctor [-c config.yaml] [-o json|table]

Run explicitly enabled Linux stress benchmarks.
Without --bench, run default_benchmarks from the CATMonitor configuration.
Without --config, load CATMONITOR_CONFIG or the platform default path.

Options:
  -b, --bench       Comma-separated benchmark names
  -c, --config      CATMonitor configuration file path
  -o, --output      json (default) or table
  -h, --help        Show this help`)
}

func printDoctorUsage(output io.Writer) {
	fmt.Fprintln(output, `Usage:
  catmonitor stress doctor [-c config.yaml] [-o json|table]

Run read-only deployment checks for every configured stress benchmark.
The command never starts a benchmark, container, or MPI workload.

Options:
  -c, --config      CATMonitor configuration file path
  -o, --output      json (default) or table
  -h, --help        Show this help`)
}

type doctorResult struct {
	Status         string       `json:"status"`
	FeatureEnabled bool         `json:"feature_enabled"`
	WebEnabled     bool         `json:"web_enabled"`
	ScriptPath     string       `json:"script_path,omitempty"`
	ReportPath     string       `json:"report_path,omitempty"`
	Benchmarks     []doctorItem `json:"benchmarks"`
}

type doctorItem struct {
	Name         string                   `json:"name"`
	Enabled      bool                     `json:"enabled"`
	Available    bool                     `json:"available"`
	Status       stress.CheckStatus       `json:"status"`
	Message      string                   `json:"message"`
	Profile      *stress.ExecutionProfile `json:"profile,omitempty"`
	ProfileError string                   `json:"profile_error,omitempty"`
}

func runDoctor(args []string, logger *slog.Logger, stdout, stderr io.Writer) int {
	configPath, output, err := parseDoctorArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "stress doctor:", err)
		return 2
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, "stress doctor: load config:", err)
		return 1
	}

	manager := stress.NewManagerWithLogger(cfg.Stress, logger)
	result := doctorResult{
		Status:         "pass",
		FeatureEnabled: cfg.Stress.Enabled,
		WebEnabled:     cfg.Stress.WebEnabled,
		ScriptPath:     cfg.Stress.ScriptPath,
		ReportPath:     cfg.Stress.ReportPath,
	}
	names := make([]string, 0, len(cfg.Stress.Benchmarks))
	for name := range cfg.Stress.Benchmarks {
		names = append(names, name)
	}
	sort.Strings(names)
	enabledCount := 0
	for _, name := range names {
		benchmark := cfg.Stress.Benchmarks[name]
		available, message := manager.Availability(name)
		item := doctorItem{
			Name: name, Enabled: benchmark.Enabled, Available: available,
			Status: stress.CheckFail, Message: message,
		}
		if !benchmark.Enabled {
			item.Status = stress.CheckUnsupported
		} else {
			enabledCount++
			if cfg.Stress.Enabled {
				profile, profileErr := manager.Describe(name)
				item.Profile = profile
				if profileErr != nil {
					item.ProfileError = profileErr.Error()
				}
				if available {
					item.Status = stress.CheckPass
					if profileErr != nil || (profile != nil && profile.Preflight.Status == stress.CheckWarn) {
						item.Status = stress.CheckWarn
					}
				}
			}
			if !available {
				result.Status = "fail"
			}
		}
		result.Benchmarks = append(result.Benchmarks, item)
	}
	if !cfg.Stress.Enabled || enabledCount == 0 {
		result.Status = "fail"
	}

	if output == "table" {
		printDoctorTable(stdout, result)
	} else {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(stdout, string(data))
	}
	if result.Status != "pass" {
		return 1
	}
	return 0
}

func parseDoctorArgs(args []string) (string, string, error) {
	fs := flag.NewFlagSet("stress doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := platform.ConfigPath()
	output := "json"
	fs.StringVar(&configPath, "config", configPath, "CATMonitor configuration file")
	fs.StringVar(&configPath, "c", configPath, "CATMonitor configuration file")
	fs.StringVar(&output, "output", output, "json or table")
	fs.StringVar(&output, "o", output, "json or table")
	if err := fs.Parse(args); err != nil {
		return "", "", err
	}
	if fs.NArg() != 0 {
		return "", "", fmt.Errorf("unknown argument %q", fs.Arg(0))
	}
	if output != "json" && output != "table" {
		return "", "", fmt.Errorf("output must be json or table")
	}
	return configPath, output, nil
}

func printDoctorTable(output io.Writer, result doctorResult) {
	fmt.Fprintf(output, "\nCATMonitor Stress Doctor  %s\n", strings.ToUpper(result.Status))
	w := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Benchmark\tEnabled\tAvailable\tPreflight\tMessage")
	for _, item := range result.Benchmarks {
		fmt.Fprintf(w, "%s\t%t\t%t\t%s\t%s\n",
			item.Name, item.Enabled, item.Available, strings.ToUpper(string(item.Status)), item.Message)
	}
	_ = w.Flush()
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
