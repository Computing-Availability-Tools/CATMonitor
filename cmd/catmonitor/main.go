package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/exporter"
	"github.com/Computing-Availability-Tools/CATMonitor/features/faultsub"
	"github.com/Computing-Availability-Tools/CATMonitor/features/health"
	"github.com/Computing-Availability-Tools/CATMonitor/features/snapshot"
	"github.com/Computing-Availability-Tools/CATMonitor/features/stragglerout"
	stresscli "github.com/Computing-Availability-Tools/CATMonitor/features/stress/cli"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/config"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/metrics"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/platform"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/storage"

	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/chassis"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/cpu"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/disk"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/gpu"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/memory"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/network"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/npu"
)

const version = "0.3.3"

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
	case "stress":
		if code := stresscli.Run(os.Args[2:], setupLogger(), os.Stdout, os.Stderr); code != 0 {
			os.Exit(code)
		}
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
  stress       Run an explicit stress benchmark job
  list         List all registered collectors
  version      Show version information

Flags:
  -c, --config      Config file path (default: ` + platform.ConfigPath() + `)
  -o, --output      Output format: json|table (default: json)
  -h, --help        Show help (parsed, then exit)`)
}

func loadConfig() *config.Config {
	fs := flag.NewFlagSet("catmonitor", flag.ContinueOnError)
	configPath := fs.String("config", platform.ConfigPath(), "Config file path")
	fs.String("c", platform.ConfigPath(), "Config file path (short)")
	fs.String("o", "", "Output format: json|table")
	fs.String("output", "", "Output format: json|table")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(0)
	}

	// Load the metric catalog: env CATMONITOR_METRICS (a file) > a metrics.yaml
	// next to the catmonitor config > dev fallback configs/metrics.yaml.
	metricsPaths := []string{
		os.Getenv("CATMONITOR_METRICS"),
		filepath.Join(filepath.Dir(*configPath), "metrics.yaml"),
		"configs/metrics.yaml",
	}
	if err := metrics.Init(metricsPaths...); err != nil {
		slog.Error("metrics catalog init failed", "error", err)
		os.Exit(1)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config, using defaults", "error", err)
		return config.Default()
	}
	// Load each enabled feature's metrics.yaml override (priority/selectivity):
	// unions the metrics each feature needs so they survive metrics.Filter.
	// Intervals are parsed separately in runDaemon (LoadModuleOverride is
	// last-wins, not min).
	featurePaths := make([]string, 0, len(cfg.Features))
	for _, f := range cfg.Features {
		p := filepath.Join("features", f, "metrics.yaml")
		featurePaths = append(featurePaths, p)
		if err := metrics.LoadModuleOverride(p); err != nil {
			slog.Error("feature metrics override failed", "feature", f, "path", p, "error", err)
		}
	}
	// Scoped collection: when features non-empty, only metrics listed by some
	// enabled feature (AND priority >= min_priority) are collected. Empty -> unscoped.
	metrics.SetFeatureScope(featurePaths)
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

	// Set collection threshold based on config (default: low = collect all).
	metrics.SetCollectionThreshold(cfg.Collection.MinPriority)
	collector.SetWantedChecker(metrics.AnyWanted)

	store, err := storage.New(cfg.Storage.DataDir)
	if err != nil {
		logger.Error("failed to create storage", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	cacheStore := exporter.NewCachingStorage(store)

	// Build collector configs
	collectorCfgs := make(map[string]collector.CollectorConfig)
	for name, c := range cfg.Collectors {
		collectorCfgs[name] = collector.CollectorConfig{
			Enabled:  c.Enabled,
			Interval: c.Interval,
		}
	}

	// Derive per-component collection cadence (C_comp) from enabled features:
	// for each component, take the min interval declared across the features'
	// metrics.yaml; components no feature declares keep catmonitor.yaml's
	// collectors.<name>.interval. This makes feature interval the collection
	// cadence (collection == per-component snapshot refresh). Empty features
	// list => catmonitor.yaml collectors.interval throughout.
	if len(cfg.Features) > 0 {
		declared := map[string]time.Duration{} // comp -> min across features
		for _, f := range cfg.Features {
			fi, _ := metrics.ComponentIntervals(filepath.Join("features", f, "metrics.yaml"))
			for comp, dur := range fi {
				if prev, ok := declared[comp]; !ok || dur < prev {
					declared[comp] = dur
				}
			}
		}
		for name, cc := range collectorCfgs {
			// Collector name == component (1:1); override interval where a
			// feature declared one.
			if dur, ok := declared[name]; ok {
				cc.Interval = dur
				collectorCfgs[name] = cc
			}
		}
		if len(declared) > 0 {
			logger.Info("derived per-component cadence from features", "features", cfg.Features, "declared_components", len(declared))
		}
	}

	// Optionally wrap the storage chain with the fault-subscription tap.
	// When faultsub is disabled, sink stays as the exporter's CachingStorage
	// and daemon behavior is unchanged.
	var sink collector.Storage = cacheStore
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if cfg.StragglerOutput.Enabled {
		kpiw := stragglerout.NewKPIWriter(cfg.StragglerOutput.DataDir, cfg.StragglerOutput.Retention, logger)
		sstore := stragglerout.NewStragglerStorage(cacheStore, stragglerout.NewKPIMapper(), kpiw, cfg.StragglerOutput.FlushInterval, logger)
		go func() {
			<-ctx.Done()
			sstore.Flush(time.Now())
		}()
		sink = sstore
		logger.Info("straggler_output enabled", "data_dir", cfg.StragglerOutput.DataDir)
	}
	if cfg.FaultSub.Enabled {
		rules := faultsub.RuleConfig{}
		for k, v := range cfg.FaultSub.Rules {
			rules[faultsub.FaultType(k)] = v
		}
		det := faultsub.NewDetector(rules)
		wh := faultsub.NewWebhook(cfg.FaultSub.WebhookTimeout, logger)
		disp := faultsub.NewDispatcher(wh, faultsub.NewSubscriptionManager(),
			cfg.FaultSub.WebhookRetry, cfg.FaultSub.EventBuffer, logger)
		fstore := faultsub.NewFaultStorage(cacheStore, det, disp, logger)
		go faultsub.ServeAPI(ctx, cfg.FaultSub.RestAddr, disp, fstore, logger)
		sink = fstore
		logger.Info("faultsub enabled", "rest_addr", cfg.FaultSub.RestAddr)
	}

	// Snapshot production (per-component files snapshot_<comp>.json + one
	// global snapshot.json), consumed by read-only features (web/dfee). Opt-in:
	// when disabled the daemon writes no snapshot files and behaves as before.
	// When enabled the daemon is the sole snapshot producer; web must run as a
	// read-only consumer (no self-collection).
	if cfg.Snapshot.Enabled {
		pcw := snapshot.NewPerCompWriter(sink, cfg.Snapshot.Dir, 60, logger)
		sink = pcw // scheduler writes through the per-component snapshot writer

		// Build collector metadata + per-component intervals for the global snapshot.
		regAll := collector.DefaultRegistry.All()
		collectors := make([]snapshot.CollectorInfo, 0, len(regAll))
		intervals := make(map[string]int, len(regAll))
		for _, c := range regAll {
			interval := c.DefaultInterval()
			if cc, ok := collectorCfgs[c.Name()]; ok {
				interval = cc.Interval
			}
			collectors = append(collectors, snapshot.CollectorInfo{
				Name:      c.Name(),
				Component: c.Component(),
				Priority:  c.Priority().String(),
				Interval:  interval.String(),
				Enabled:   c.DefaultEnabled(),
			})
			intervals[c.Component()] = int(interval / time.Millisecond)
		}
		// Global cadence C_global: min over per-component cadences.
		minMS := 0
		for _, ms := range intervals {
			if minMS == 0 || ms < minMS {
				minMS = ms
			}
		}
		refresh := time.Duration(minMS) * time.Millisecond
		gw := snapshot.NewGlobalWriter(cacheStore, cfg.Snapshot.Dir, refresh, logger)
		gw.SetCollectors(collectors)
		gw.SetIntervals(intervals)
		go gw.Run(ctx)

		// One-shot hardware identity at startup: system specs -> global writer,
		// per-component specs (gpu_info/npu_info/...) -> per-comp writer.
		go func() {
			specs := snapshot.CollectHWSpecs()
			var sys []collector.Metric
			byComp := map[string][]collector.Metric{}
			for _, m := range specs {
				if m.Component == "system" {
					sys = append(sys, m)
				} else {
					byComp[m.Component] = append(byComp[m.Component], m)
				}
			}
			gw.SetSystemSpecs(sys)
			for comp, ms := range byComp {
				pcw.SetCompSpecs(comp, ms)
			}
			logger.Info("hardware specs distributed to snapshot writers", "count", len(specs))
		}()

		logger.Info("snapshot production enabled", "dir", cfg.Snapshot.Dir, "refresh", refresh)
	}

	scheduler := collector.NewScheduler(collector.DefaultRegistry, sink, logger)
	scheduler.SetFilter(metrics.Filter)

	// Prometheus exporter endpoint
	go exporter.ServeMetrics(":9100", cacheStore, logger)

	scheduler.Start(ctx, collectorCfgs)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	logger.Info("CATMonitor daemon started", "version", version)
	sig := <-sigCh
	logger.Info("received signal, shutting down", "signal", sig)
	cancel()
	scheduler.Stop()
}

func runCollect() {
	cfg := loadConfig()
	output := getOutputFormat()

	// Set collection threshold + inject wanted checker (same as daemon).
	metrics.SetCollectionThreshold(cfg.Collection.MinPriority)
	collector.SetWantedChecker(metrics.AnyWanted)

	var allMetrics []collector.Metric
	for _, c := range collector.DefaultRegistry.All() {
		if !isCollectorEnabled(cfg, c.Name()) {
			continue
		}
		collected, err := c.Collect()
		if err != nil {
			continue
		}
		allMetrics = append(allMetrics, collected...)
	}

	allMetrics = metrics.Filter(allMetrics)

	if output == "table" {
		printMetricsTable(allMetrics)
	} else {
		printMetricsJSON(allMetrics)
	}
}

func runHealth() {
	cfg := loadConfig()
	// Health module reads its own metrics.yaml first (merged over the default).
	if err := metrics.LoadModuleOverride("features/health/metrics.yaml"); err != nil {
		slog.Error("health metrics override failed", "error", err)
		os.Exit(1)
	}
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
		collected, err := c.Collect()
		if err != nil {
			continue
		}
		allMetrics = append(allMetrics, collected...)
	}

	allMetrics = metrics.Filter(allMetrics)

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
