// Package cli implements the daemon-client catmonitor stress command.
package cli

import (
	"context"
	"encoding/json"
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

func Run(args []string, logger *slog.Logger, stdout, stderr io.Writer) int {
	_ = logger
	mode := "run"
	if len(args) > 0 {
		switch args[0] {
		case "run", "doctor", "status", "cancel":
			mode, args = args[0], args[1:]
		}
	}
	if helpRequested(args) {
		printUsage(stdout, mode)
		return 0
	}
	switch mode {
	case "doctor":
		return runDoctor(args, stdout, stderr)
	case "status":
		return runStatus(args, stdout, stderr)
	case "cancel":
		return runCancel(args, stdout, stderr)
	default:
		return runJob(args, stdout, stderr)
	}
}

func loadClient(configPath string) (*stress.ControlClient, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return stress.NewControlClient(cfg.Stress.ControlSocket)
}

func runJob(args []string, stdout, stderr io.Writer) int {
	configPath, names, output, timeout, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "stress:", err)
		return 2
	}
	client, err := loadClient(configPath)
	if err != nil {
		fmt.Fprintln(stderr, "stress:", err)
		return 1
	}
	ctx := context.Background()
	report, err := client.Start(ctx, stress.ControlStartRequest{Benchmarks: names, TimeoutSeconds: int64(timeout / time.Second), Initiator: stress.InitiatorCLI})
	if err != nil {
		fmt.Fprintln(stderr, "stress:", err)
		return 1
	}
	for report.Status == stress.StatusRunning {
		time.Sleep(250 * time.Millisecond)
		report, err = client.Job(ctx, report.JobID)
		if err != nil {
			fmt.Fprintln(stderr, "stress:", err)
			return 1
		}
	}
	printReport(stdout, report, output)
	if report.Status != stress.StatusHealthy {
		return 1
	}
	return 0
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	configPath, output, err := parseDoctorArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "stress doctor:", err)
		return 2
	}
	client, err := loadClient(configPath)
	if err != nil {
		fmt.Fprintln(stderr, "stress doctor:", err)
		return 1
	}
	view, err := client.Config(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "stress doctor:", err)
		return 1
	}
	result := doctorResult{Status: "pass", FeatureEnabled: view.FeatureEnabled, WebEnabled: view.WebEnabled}
	enabled := 0
	for _, benchmark := range view.Benchmarks {
		item := doctorItem{
			Name: benchmark.Name, Enabled: benchmark.Enabled, Available: benchmark.Available,
			Status: stress.CheckFail, Message: benchmark.Message, Profile: benchmark.Profile,
			ProfileError: benchmark.ProfileError,
		}
		if !benchmark.Enabled {
			item.Status = stress.CheckUnsupported
		} else {
			enabled++
			if benchmark.Available {
				item.Status = stress.CheckPass
				if benchmark.ProfileError != "" || (benchmark.Profile != nil && benchmark.Profile.Preflight.Status == stress.CheckWarn) {
					item.Status = stress.CheckWarn
				}
			} else {
				result.Status = "fail"
			}
		}
		result.Benchmarks = append(result.Benchmarks, item)
	}
	if !view.FeatureEnabled || enabled == 0 {
		result.Status = "fail"
	}
	if output == "table" {
		printDoctorTable(stdout, result)
	} else {
		printJSON(stdout, result)
	}
	if result.Status != "pass" {
		return 1
	}
	return 0
}

func runStatus(args []string, stdout, stderr io.Writer) int {
	configPath, jobID, output, err := parseJobArgs("stress status", args, false)
	if err != nil {
		fmt.Fprintln(stderr, "stress status:", err)
		return 2
	}
	client, err := loadClient(configPath)
	if err != nil {
		fmt.Fprintln(stderr, "stress status:", err)
		return 1
	}
	var report stress.Report
	if jobID == "" {
		report, err = client.Latest(context.Background())
	} else {
		report, err = client.Job(context.Background(), jobID)
	}
	if err != nil {
		fmt.Fprintln(stderr, "stress status:", err)
		return 1
	}
	printReport(stdout, report, output)
	return 0
}

func runCancel(args []string, stdout, stderr io.Writer) int {
	configPath, jobID, _, err := parseJobArgs("stress cancel", args, true)
	if err != nil {
		fmt.Fprintln(stderr, "stress cancel:", err)
		return 2
	}
	client, err := loadClient(configPath)
	if err != nil {
		fmt.Fprintln(stderr, "stress cancel:", err)
		return 1
	}
	if err := client.Cancel(context.Background(), jobID); err != nil {
		fmt.Fprintln(stderr, "stress cancel:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Cancellation accepted for stress job %s\n", jobID)
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

func printUsage(output io.Writer, mode string) {
	if mode == "doctor" {
		fmt.Fprintln(output, "Usage:\n  catmonitor stress doctor [-c config.yaml] [-o json|table]\n\nRead daemon-owned workload preflight; never starts a workload.")
		return
	}
	if mode == "status" || mode == "cancel" {
		fmt.Fprintf(output, "Usage:\n  catmonitor stress %s --job JOB_ID [-c config.yaml] [-o json|table]\n", mode)
		return
	}
	fmt.Fprintln(output, `Usage:
  catmonitor stress [run] [--bench hpl,hpcg,stream,npu_burn] [--timeout 30s] [-c config.yaml] [-o json|table]
  catmonitor stress doctor [-c config.yaml] [-o json|table]
  catmonitor stress status [--job JOB_ID] [-c config.yaml] [-o json|table]
  catmonitor stress cancel --job JOB_ID [-c config.yaml]

The CLI is a client of the local catmonitor daemon Stress Controller.`)
}

type doctorResult struct {
	Status         string       `json:"status"`
	FeatureEnabled bool         `json:"feature_enabled"`
	WebEnabled     bool         `json:"web_enabled"`
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

func parseDoctorArgs(args []string) (string, string, error) {
	fs := flag.NewFlagSet("stress doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath, output := platform.ConfigPath(), "json"
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
		return "", "", errorsOutput()
	}
	return configPath, output, nil
}

func parseArgs(args []string) (string, []string, string, time.Duration, error) {
	fs := flag.NewFlagSet("stress", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath, benchmarks, output := platform.ConfigPath(), "", "json"
	var timeout time.Duration
	fs.StringVar(&configPath, "config", configPath, "CATMonitor configuration file")
	fs.StringVar(&configPath, "c", configPath, "CATMonitor configuration file")
	fs.StringVar(&benchmarks, "bench", benchmarks, "comma-separated benchmarks")
	fs.StringVar(&benchmarks, "b", benchmarks, "comma-separated benchmarks")
	fs.StringVar(&output, "output", output, "json or table")
	fs.StringVar(&output, "o", output, "json or table")
	fs.DurationVar(&timeout, "timeout", 0, "single-job timeout shorter than the configured maximum")
	if err := fs.Parse(args); err != nil {
		return "", nil, "", 0, err
	}
	if fs.NArg() != 0 {
		return "", nil, "", 0, fmt.Errorf("unknown argument %q", fs.Arg(0))
	}
	if output != "json" && output != "table" {
		return "", nil, "", 0, errorsOutput()
	}
	var names []string
	if benchmarks != "" {
		for _, name := range strings.Split(benchmarks, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				return "", nil, "", 0, fmt.Errorf("benchmark names cannot be empty")
			}
			names = append(names, name)
		}
	}
	if timeout < 0 || (timeout > 0 && timeout < time.Second) {
		return "", nil, "", 0, fmt.Errorf("timeout must be zero or at least 1s")
	}
	return configPath, names, output, timeout, nil
}

func parseJobArgs(name string, args []string, requireJob bool) (string, string, string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath, jobID, output := platform.ConfigPath(), "", "json"
	fs.StringVar(&configPath, "config", configPath, "CATMonitor configuration file")
	fs.StringVar(&configPath, "c", configPath, "CATMonitor configuration file")
	fs.StringVar(&jobID, "job", jobID, "stress job id")
	fs.StringVar(&output, "output", output, "json or table")
	fs.StringVar(&output, "o", output, "json or table")
	if err := fs.Parse(args); err != nil {
		return "", "", "", err
	}
	if fs.NArg() != 0 {
		return "", "", "", fmt.Errorf("unknown argument %q", fs.Arg(0))
	}
	if requireJob && jobID == "" {
		return "", "", "", fmt.Errorf("--job is required")
	}
	if output != "json" && output != "table" {
		return "", "", "", errorsOutput()
	}
	return configPath, jobID, output, nil
}

func errorsOutput() error { return fmt.Errorf("output must be json or table") }

func printDoctorTable(output io.Writer, result doctorResult) {
	fmt.Fprintf(output, "\nCATMonitor Stress Doctor  %s\n", strings.ToUpper(result.Status))
	w := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Benchmark\tEnabled\tAvailable\tPreflight\tMessage")
	for _, item := range result.Benchmarks {
		fmt.Fprintf(w, "%s\t%t\t%t\t%s\t%s\n", item.Name, item.Enabled, item.Available, strings.ToUpper(string(item.Status)), item.Message)
	}
	_ = w.Flush()
}

func printReport(output io.Writer, report stress.Report, format string) {
	if format == "table" {
		printTable(output, report)
	} else {
		printJSON(output, report)
	}
}
func printJSON(output io.Writer, value any) {
	data, _ := json.MarshalIndent(value, "", "  ")
	fmt.Fprintln(output, string(data))
}

func printTable(output io.Writer, report stress.Report) {
	fmt.Fprintf(output, "\nCATMonitor Stress Report  %s\n", statusLabel(report.Status))
	w := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Benchmark\tStatus\tDuration\tMetric\tValue\tMessage")
	for _, result := range report.Benchmarks {
		keys := make([]string, 0, len(result.Values))
		for key := range result.Values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			fmt.Fprintf(w, "%s\t%s\t%s\t-\t-\t%s\n", result.Name, statusLabel(result.Status), formatDuration(result.DurationMS), result.Message)
			continue
		}
		for i, key := range keys {
			name, status, duration, message := "", "", "", ""
			if i == 0 {
				name, status, duration, message = result.Name, statusLabel(result.Status), formatDuration(result.DurationMS), result.Message
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", name, status, duration, key, formatValue(result.Values[key]), message)
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
