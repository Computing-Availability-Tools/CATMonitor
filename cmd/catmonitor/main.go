package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/config"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/health"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/platform"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/storage"

	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/cpu"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/disk"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/gpu"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/memory"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/network"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/npu"
)

const version = "0.1.1"

func main() {
	if len(os.Args) < 2 {
		runDaemon()
		return
	}

	switch os.Args[1] {
	case "daemon":
		runDaemon()
	case "collect":
		runCollect()
	case "health":
		runHealth()
	case "list":
		runList()
	case "version":
		fmt.Printf("CATMonitor v%s (Go %s)\n", version, "1.23+")
	default:
		printUsage()
	}
}

func printUsage() {
	fmt.Println(`CATMonitor - Computing Availability Tools Monitor

Usage:
  catmonitor [command] [flags]

Commands:
  daemon       Start daemon process (default)
  collect      Collect metrics once and print
  health       Run health check and print report
  list         List all registered collectors
  version      Show version information

Flags:
  -c, --config      Config file path (default: ` + platform.ConfigPath() + `)
  -d, --data-dir    Data output directory (default: ` + platform.DataDir() + `)
  -o, --output      Output format: json|table (default: json)
  -v, --verbose     Verbose logging`)
}

func loadConfig() *config.Config {
	fs := flag.NewFlagSet("catmonitor", flag.ContinueOnError)
	configPath := fs.String("config", platform.ConfigPath(), "Config file path")
	fs.String("c", platform.ConfigPath(), "Config file path (short)")
	fs.String("o", "", "Output format: json|table")
	fs.String("output", "", "Output format: json|table")
	fs.Parse(os.Args[2:])

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config, using defaults", "error", err)
		return config.Default()
	}
	return cfg
}

func setupLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

func runDaemon() {
	cfg := loadConfig()
	logger := setupLogger()

	store, err := storage.New(cfg.Storage.DataDir)
	if err != nil {
		logger.Error("failed to create storage", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	// Build collector configs
	collectorCfgs := make(map[string]collector.CollectorConfig)
	for name, c := range cfg.Collectors {
		collectorCfgs[name] = collector.CollectorConfig{
			Enabled:  c.Enabled,
			Interval: c.Interval,
		}
	}

	scheduler := collector.NewScheduler(collector.DefaultRegistry, store, logger)

	// Set up health evaluator
	var healthEval *health.Evaluator
	if cfg.Health.Enabled {
		scheme := health.GetScheme(cfg.Health.WeightScheme)
		healthEval = health.NewEvaluator(scheme)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler.Start(ctx, collectorCfgs)

	// Health check goroutine
	if healthEval != nil {
		go func() {
			ticker := time.NewTicker(cfg.Health.Interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					runHealthCheck(scheduler, healthEval, store, logger)
				}
			}
		}()
	}

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	logger.Info("CATMonitor daemon started", "version", version)
	sig := <-sigCh
	logger.Info("received signal, shutting down", "signal", sig)
	cancel()
	scheduler.Stop()
}

func runHealthCheck(scheduler *collector.Scheduler, eval *health.Evaluator, store *storage.JSONLStorage, logger *slog.Logger) {
	// Collect all metrics
	var allMetrics []collector.Metric
	for _, c := range collector.DefaultRegistry.All() {
		metrics, err := c.Collect()
		if err != nil {
			logger.Error("collection failed", "collector", c.Name(), "error", err)
			continue
		}
		allMetrics = append(allMetrics, metrics...)
	}

	score := eval.Evaluate(allMetrics)
	logger.Info("health check", "score", score.Score, "grade", score.Grade)
}

func runCollect() {
	cfg := loadConfig()
	output := getOutputFormat()

	var allMetrics []collector.Metric
	for _, c := range collector.DefaultRegistry.All() {
		if !isCollectorEnabled(cfg, c.Name()) {
			continue
		}
		metrics, err := c.Collect()
		if err != nil {
			continue
		}
		allMetrics = append(allMetrics, metrics...)
	}

	if output == "table" {
		printMetricsTable(allMetrics)
	} else {
		printMetricsJSON(allMetrics)
	}
}

func runHealth() {
	cfg := loadConfig()
	output := "table"
	for i, arg := range os.Args {
		if (arg == "-o" || arg == "--output") && i+1 < len(os.Args) {
			output = os.Args[i+1]
			break
		}
	}

	var allMetrics []collector.Metric
	for _, c := range collector.DefaultRegistry.All() {
		if !isCollectorEnabled(cfg, c.Name()) {
			continue
		}
		metrics, err := c.Collect()
		if err != nil {
			continue
		}
		allMetrics = append(allMetrics, metrics...)
	}

	scheme := health.GetScheme(cfg.Health.WeightScheme)
	eval := health.NewEvaluator(scheme)
	score := eval.Evaluate(allMetrics)

	if output == "table" {
		printHealthTable(score)
	} else {
		printHealthJSON(score)
	}
}

func runList() {
	collectors := collector.DefaultRegistry.All()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Name\tComponent\tPriority\tInterval\tEnabled\t")
	for _, c := range collectors {
		fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%v\t\n",
			c.Name(), c.Component(), c.Priority(),
			c.DefaultInterval(), c.DefaultEnabled())
	}
	w.Flush()
}

func getOutputFormat() string {
	for i, arg := range os.Args {
		if arg == "-o" || arg == "--output" {
			if i+1 < len(os.Args) {
				return os.Args[i+1]
			}
		}
	}
	return "json"
}

func isCollectorEnabled(cfg *config.Config, name string) bool {
	if c, ok := cfg.Collectors[name]; ok {
		return c.Enabled
	}
	return true
}

func printMetricsJSON(metrics []collector.Metric) {
	for _, m := range metrics {
		data, _ := json.Marshal(m)
		fmt.Println(string(data))
	}
}

func printMetricsTable(metrics []collector.Metric) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Component\tMetric\tValue\tUnit\tLabels\t")
	for _, m := range metrics {
		labels := ""
		for k, v := range m.Labels {
			if labels != "" {
				labels += ","
			}
			labels += k + "=" + v
		}
		fmt.Fprintf(w, "%s\t%s\t%.2f\t%s\t%s\t\n", m.Component, m.Name, m.Value, m.Unit, labels)
	}
	w.Flush()
}

func printHealthJSON(score health.HealthScore) {
	data, _ := json.MarshalIndent(score, "", "  ")
	fmt.Println(string(data))
}

func printHealthTable(score health.HealthScore) {
	fmt.Println()
	fmt.Println("CATMonitor Health Report")
	fmt.Println("======================================================================")
	fmt.Println()

	bar := renderScoreBar(score.Score, 100)
	fmt.Printf("  Overall Score:  %s  %d / 100   [ %s ]\n", bar, score.Score, score.Grade)
	fmt.Printf("  Server Type:    %s\n", score.ServerType)
	fmt.Printf("  Check Time:     %s\n", score.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Println()

	fmt.Println("  ----------------------------------------------------------------------")
	fmt.Println("  Component        Score / Max    Status       Deductions")
	fmt.Println("  ----------------------------------------------------------------------")

	order := []string{"cpu", "memory", "disk", "gpu", "npu"}
	for _, name := range order {
		if comp, ok := score.Components[name]; ok {
			status := componentStatus(comp.Score, comp.Max)
			deductions := formatDeductions(comp.Deductions)
			if deductions == "" {
				deductions = "-"
			}
			fmt.Printf("  %-16s  %3d / %-3d      %-8s     %s\n", strings.ToUpper(name), comp.Score, comp.Max, status, deductions)
		}
	}

	fmt.Println("  ----------------------------------------------------------------------")
	fmt.Printf("  %-16s  %3d / %-3d      %s\n", "TOTAL", score.Score, 100, score.Grade)
	fmt.Println("  ----------------------------------------------------------------------")
	fmt.Println()

	switch {
	case score.Score >= 90:
		fmt.Println("  [OK]    All systems are healthy.")
	case score.Score >= 75:
		fmt.Println("  [OK]    System is operating with minor issues.")
	case score.Score >= 60:
		fmt.Println("  [!]     System has warnings that may need attention.")
	default:
		fmt.Println("  [X]     Critical issues detected - immediate attention required!")
	}
	fmt.Println()
}

func renderScoreBar(score, max int) string {
	width := 30
	filled := 0
	if max > 0 {
		filled = int(float64(width) * float64(score) / float64(max))
	}
	if filled > width {
		filled = width
	}
	bar := ""
	for i := 0; i < filled; i++ {
		bar += "█"
	}
	for i := filled; i < width; i++ {
		bar += "░"
	}
	return "[" + bar + "]"
}

func componentStatus(score, max int) string {
	if max == 0 {
		return "N/A"
	}
	ratio := float64(score) / float64(max)
	switch {
	case ratio >= 0.9:
		return "OK"
	case ratio >= 0.75:
		return "Good"
	case ratio >= 0.6:
		return "Warning"
	default:
		return "Critical"
	}
}

func formatDeductions(deductions []health.Deduction) string {
	if len(deductions) == 0 {
		return ""
	}
	parts := make([]string, len(deductions))
	for i, d := range deductions {
		parts[i] = fmt.Sprintf("%s (-%.0f)", d.Rule, d.Penalty)
	}
	return strings.Join(parts, "; ")
}
