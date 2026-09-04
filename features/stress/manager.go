package stress

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

var (
	ErrDisabled = errors.New("stress testing is disabled")
	ErrBusy     = errors.New("a stress job is already running")
	ErrNotFound = errors.New("stress job not found")
)

const (
	maxHistoryReports  = 100
	defaultHistoryRead = 20
	transportGrace     = 30 * time.Second
)

// Manager is the daemon-owned Stress Controller. CLI and Web use the daemon
// control API and never construct an executing Manager.
type Manager struct {
	cfg         Config
	logger      *slog.Logger
	executor    Executor
	writeReport func(Report) error

	mu           sync.Mutex
	active       *activeJob
	last         *Report
	profileMu    sync.Mutex
	profileCache map[string]profileCacheEntry
}

type activeJob struct {
	cancel context.CancelFunc
	done   chan struct{}
	report Report
}

func NewManager(cfg Config) *Manager { return NewManagerWithLogger(cfg, nil) }

func NewManagerWithLogger(cfg Config, logger *slog.Logger) *Manager {
	executor, err := newConfiguredExecutor(cfg.Executor)
	if err != nil {
		executor = unavailableExecutor{err: fmt.Errorf("%w: %v", ErrExecutorUnavailable, err)}
	}
	return NewManagerWithExecutor(cfg, logger, executor)
}

func NewManagerWithExecutor(cfg Config, logger *slog.Logger, executor Executor) *Manager {
	if executor == nil {
		executor = unavailableExecutor{err: ErrExecutorUnavailable}
	}
	m := &Manager{
		cfg: copyConfig(cfg), logger: logger, executor: executor,
		profileCache: make(map[string]profileCacheEntry),
	}
	m.writeReport = m.writeReportFile
	return m
}

func newConfiguredExecutor(cfg ExecutorConfig) (Executor, error) {
	if cfg.Type == "" {
		cfg.Type = "docker_exec"
	}
	if cfg.Type != "docker_exec" {
		return nil, fmt.Errorf("unsupported executor type %q", cfg.Type)
	}
	return NewDockerExecExecutor(cfg)
}

func (m *Manager) Config() Config { return copyConfig(m.cfg) }

func (m *Manager) Start(names []string) (Report, error) {
	return m.StartWithOptions(names, RunOptions{})
}

func (m *Manager) StartWithOptions(names []string, options RunOptions) (Report, error) {
	m.mu.Lock()
	if !m.cfg.Enabled {
		m.mu.Unlock()
		return Report{}, ErrDisabled
	}
	if m.active != nil {
		active := *copyReport(m.active.report)
		m.mu.Unlock()
		return active, ErrBusy
	}
	m.mu.Unlock()

	selected, err := m.selected(names)
	if err != nil {
		return Report{}, err
	}
	if err := m.validateTimeout(selected, options.Timeout); err != nil {
		return Report{}, err
	}
	if runtime.GOOS != "linux" {
		return Report{}, errors.New("stress execution is supported on Linux only")
	}

	// Describe may invoke an external workload container. Keep it outside the
	// Manager lock so read-only control requests remain responsive.
	profiles := make(map[string]*ExecutionProfile, len(selected))
	for _, name := range selected {
		profile, describeErr := m.describeWithTimeout(name, options.Timeout)
		if describeErr != nil {
			return Report{}, fmt.Errorf("benchmark %q describe/preflight failed: %w", name, describeErr)
		}
		if profile.Preflight.Status == CheckFail {
			return Report{}, fmt.Errorf("benchmark %q is unavailable: %s", name, failedPreflightMessage(profile))
		}
		profiles[name] = profile
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		return *copyReport(m.active.report), ErrBusy
	}

	now := time.Now()
	report := Report{
		JobID: newJobID(), Initiator: options.Initiator, Timestamp: now,
		StartedAt: now, Platform: runtime.GOOS, Status: StatusRunning,
		TimeoutSeconds:      options.Timeout.Milliseconds() / 1000,
		ConfigurationSHA256: aggregateConfigurationSHA256(selected, profiles),
	}
	for _, name := range selected {
		report.Benchmarks = append(report.Benchmarks, BenchmarkResult{
			Name: name, Status: StatusPending, Profile: copyExecutionProfile(profiles[name]),
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.active = &activeJob{cancel: cancel, done: make(chan struct{}), report: report}
	if err := m.persistReportLocked(&m.active.report); err != nil {
		cancel()
		m.active = nil
		return Report{}, fmt.Errorf("persist initial stress report: %w", err)
	}
	m.last = copyReport(m.active.report)
	started := *copyReport(m.active.report)
	m.logInfo("stress job started", "job_id", report.JobID, "initiator", report.Initiator, "benchmarks", selected)
	go m.run(ctx, report.JobID, selected, options.Timeout, profiles)
	return started, nil
}

func (m *Manager) Latest() (Report, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		report := *copyReport(m.active.report)
		report.Cancellable = true
		return report, nil
	}
	if m.cfg.ReportPath == "" {
		if m.last != nil {
			return *copyReport(*m.last), nil
		}
		return Report{}, os.ErrNotExist
	}
	report, err := m.readReportFile()
	if err != nil {
		if m.last != nil {
			return *copyReport(*m.last), nil
		}
		return Report{}, err
	}
	m.last = copyReport(report)
	return *copyReport(report), nil
}

func (m *Manager) Job(id string) (Report, error) {
	report, err := m.Latest()
	if err != nil || report.JobID != id {
		return Report{}, ErrNotFound
	}
	return report, nil
}

func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil || m.active.report.JobID != id {
		return ErrNotFound
	}
	m.logInfo("stress job cancellation requested", "job_id", id)
	m.active.cancel()
	return nil
}

func (m *Manager) CanCancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active != nil && m.active.report.JobID == id
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if m.active == nil {
		m.mu.Unlock()
		return nil
	}
	jobID, cancel, done := m.active.report.JobID, m.active.cancel, m.active.done
	cancel()
	m.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for stress job %s shutdown: %w", jobID, ctx.Err())
	}
}

func (m *Manager) run(ctx context.Context, jobID string, names []string, timeoutOverride time.Duration, profiles map[string]*ExecutionProfile) {
	for _, name := range names {
		if ctx.Err() != nil {
			m.setBenchmark(name, StatusCancelled, "benchmark cancelled", nil, "", "", time.Now(), true)
			continue
		}
		m.setBenchmark(name, StatusRunning, "", nil, "", "", time.Time{}, false)
		result := m.runBenchmark(ctx, jobID, name, timeoutOverride, profiles[name])
		m.finishBenchmark(result)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil || m.active.report.JobID != jobID {
		return
	}
	job := m.active
	report := job.report
	report.FinishedAt = time.Now()
	report.Timestamp = report.FinishedAt
	report.Status = aggregateReportStatus(report.Benchmarks)
	_ = m.persistReportLocked(&report)
	if err := m.appendHistoryFile(report); err != nil {
		m.logError("stress history persistence failed", "job_id", report.JobID, "error", err)
	}
	m.last = copyReport(report)
	m.active = nil
	close(job.done)
	m.logInfo("stress job finished", "job_id", report.JobID, "status", report.Status)
}

func (m *Manager) runBenchmark(ctx context.Context, jobID, name string, timeoutOverride time.Duration, profile *ExecutionProfile) BenchmarkResult {
	started := time.Now()
	result := BenchmarkResult{Name: name, Status: StatusUnhealthy, StartedAt: started, Profile: copyExecutionProfile(profile)}
	finish := func(status Status, message string) BenchmarkResult {
		result.Status, result.Message, result.FinishedAt = status, message, time.Now()
		result.DurationMS = result.FinishedAt.Sub(started).Milliseconds()
		return result
	}

	benchmark, ok := m.cfg.Benchmarks[name]
	if !ok || !benchmark.Enabled {
		return finish(StatusUnavailable, "benchmark is not enabled in configuration")
	}
	timeout := effectiveTimeout(benchmark.Timeout)
	if timeoutOverride > 0 && timeoutOverride < timeout {
		timeout = timeoutOverride
	}
	binding := m.binding(name)
	request := workloadapi.Request{
		ProtocolVersion: workloadapi.ProtocolVersion, JobID: jobID,
		Benchmark: name, TimeoutSeconds: int64(timeout / time.Second),
		Options: map[string]json.RawMessage{},
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout+transportGrace)
	defer cancel()
	envelope, err := m.executor.Run(runCtx, binding, request)
	if errors.Is(ctx.Err(), context.Canceled) {
		return finish(StatusCancelled, "benchmark cancelled")
	}
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return finish(StatusUnhealthy, "workload transport did not return after the configured time limit")
		}
		return finish(StatusUnhealthy, fmt.Sprintf("workload executor failed: %v", err))
	}
	result.Output, result.Source = envelope.Output, envelope.Source
	result.Values = copyValues(envelope.Values)
	status := Status(envelope.Status)
	switch status {
	case StatusCancelled:
		return finish(StatusCancelled, envelope.Message)
	case StatusUnavailable:
		return finish(StatusUnavailable, envelope.Message)
	case StatusUnhealthy:
		return finish(StatusUnhealthy, envelope.Message)
	case StatusTimeLimitReached:
		if name == "npu_burn" {
			return finish(StatusUnhealthy, "configured time limit reached before Ascend NPU Burn produced a complete validated result")
		}
		return finish(StatusTimeLimitReached, envelope.Message)
	case StatusHealthy:
	default:
		return finish(StatusUnhealthy, "workload returned an invalid status")
	}
	if len(result.Values) == 0 || result.Source == "" {
		return finish(StatusUnhealthy, "workload protocol returned no normalized result values")
	}
	message := envelope.Message
	if message == "" {
		message = "workload completed and required values parsed"
	}
	return finish(StatusHealthy, message)
}

func (m *Manager) binding(name string) Binding {
	benchmark := m.cfg.Benchmarks[name]
	plugin := benchmark.Plugin
	if plugin == "" {
		plugin = name
	}
	return Binding{Benchmark: name, Plugin: plugin, Container: benchmark.Container, User: benchmark.User}
}

func (m *Manager) Availability(name string) (bool, string) {
	if runtime.GOOS != "linux" {
		return false, "stress execution is supported on Linux only"
	}
	if !m.cfg.Enabled {
		return false, "stress testing is disabled"
	}
	benchmark, ok := m.cfg.Benchmarks[name]
	if !ok {
		return false, "benchmark is not configured"
	}
	if !benchmark.Enabled {
		return false, "benchmark is disabled in configuration"
	}
	if err := validateBinding(m.binding(name)); err != nil {
		return false, err.Error()
	}
	profile, err := m.Describe(name)
	if err != nil {
		return false, "describe/preflight failed: " + err.Error()
	}
	if profile.Preflight.Status == CheckFail {
		return false, failedPreflightMessage(profile)
	}
	if profile.Preflight.Status == CheckWarn {
		return true, profile.Preflight.Message
	}
	return true, "deployment precheck passed"
}

func failedPreflightMessage(profile *ExecutionProfile) string {
	if profile == nil {
		return "deployment preflight failed"
	}
	reasons := make([]string, 0, len(profile.Assets)+1)
	for _, asset := range profile.Assets {
		if asset.Status == CheckFail {
			reasons = append(reasons, asset.Name+": "+asset.Message)
		}
	}
	if profile.MPI.Status == CheckFail {
		reasons = append(reasons, "MPI: "+profile.MPI.Message)
	}
	if len(reasons) == 0 {
		return profile.Preflight.Message
	}
	return "deployment preflight failed: " + strings.Join(reasons, "; ")
}

func (m *Manager) validateTimeout(selected []string, requested time.Duration) error {
	if requested == 0 {
		return nil
	}
	if requested < 0 {
		return errors.New("requested timeout must be positive")
	}
	for _, name := range selected {
		maximum := effectiveTimeout(m.cfg.Benchmarks[name].Timeout)
		if requested > maximum {
			return fmt.Errorf("requested timeout %s exceeds configured maximum %s for benchmark %q", requested, maximum, name)
		}
	}
	return nil
}

func effectiveTimeout(configured time.Duration) time.Duration {
	if configured <= 0 {
		return time.Hour
	}
	return configured
}

func aggregateReportStatus(benchmarks []BenchmarkResult) Status {
	allHealthy, hasCancelled, hasUnavailable, hasUnsupported := len(benchmarks) > 0, false, false, false
	for _, benchmark := range benchmarks {
		switch benchmark.Status {
		case StatusHealthy, StatusTimeLimitReached:
			continue
		case StatusUnhealthy:
			return StatusUnhealthy
		case StatusCancelled:
			hasCancelled = true
		case StatusUnavailable:
			hasUnavailable = true
		case StatusUnsupported:
			hasUnsupported = true
		default:
			return StatusUnhealthy
		}
		allHealthy = false
	}
	if allHealthy {
		return StatusHealthy
	}
	if hasCancelled {
		return StatusCancelled
	}
	if hasUnavailable {
		return StatusUnavailable
	}
	if hasUnsupported {
		return StatusUnsupported
	}
	return StatusUnhealthy
}

func (m *Manager) setBenchmark(name string, status Status, message string, values map[string]float64, source, output string, finished time.Time, complete bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return
	}
	for i := range m.active.report.Benchmarks {
		item := &m.active.report.Benchmarks[i]
		if item.Name != name {
			continue
		}
		item.Status, item.Message, item.Values, item.Source, item.Output = status, message, values, source, output
		if status == StatusRunning {
			item.StartedAt = time.Now()
		}
		if complete {
			item.FinishedAt, item.DurationMS = finished, finished.Sub(item.StartedAt).Milliseconds()
		}
		break
	}
	_ = m.persistReportLocked(&m.active.report)
	m.last = copyReport(m.active.report)
}

func (m *Manager) finishBenchmark(result BenchmarkResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return
	}
	for i := range m.active.report.Benchmarks {
		if m.active.report.Benchmarks[i].Name == result.Name {
			m.active.report.Benchmarks[i] = result
			break
		}
	}
	_ = m.persistReportLocked(&m.active.report)
	m.last = copyReport(m.active.report)
}

func (m *Manager) selected(requested []string) ([]string, error) {
	names := requested
	if len(names) == 0 {
		names = m.cfg.DefaultBenchmarks
	}
	if len(names) == 0 {
		return nil, errors.New("no stress benchmarks configured")
	}
	seen := make(map[string]bool)
	selected := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" || seen[name] {
			continue
		}
		if !supportedBenchmark(name) {
			return nil, fmt.Errorf("benchmark %q is not supported", name)
		}
		benchmark, ok := m.cfg.Benchmarks[name]
		if !ok {
			return nil, fmt.Errorf("benchmark %q is not configured", name)
		}
		if !benchmark.Enabled {
			return nil, fmt.Errorf("benchmark %q is disabled in configuration", name)
		}
		seen[name] = true
		selected = append(selected, name)
	}
	if len(selected) == 0 {
		return nil, errors.New("no stress benchmarks selected")
	}
	return selected, nil
}

func supportedBenchmark(name string) bool {
	switch name {
	case "stream", "hpl", "hpcg", "npu_burn":
		return true
	default:
		return false
	}
}

func (m *Manager) History(limit int) ([]Report, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.ReportPath == "" {
		return []Report{}, nil
	}
	reports, err := m.readHistoryFile()
	if os.IsNotExist(err) {
		return []Report{}, nil
	}
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultHistoryRead
	}
	if limit > maxHistoryReports {
		limit = maxHistoryReports
	}
	if len(reports) > limit {
		reports = reports[:limit]
	}
	result := make([]Report, len(reports))
	for i := range reports {
		result[i] = *copyReport(reports[i])
	}
	return result, nil
}

func (m *Manager) readReportFile() (Report, error) {
	data, err := os.ReadFile(m.cfg.ReportPath)
	if err != nil {
		return Report{}, err
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return Report{}, err
	}
	return report, nil
}

func (m *Manager) persistReportLocked(report *Report) error {
	report.ReportError = ""
	if err := m.writeReport(*report); err != nil {
		report.ReportError = err.Error()
		return err
	}
	return nil
}

func (m *Manager) writeReportFile(report Report) error {
	if m.cfg.ReportPath == "" {
		return nil
	}
	return writeJSONAtomic(m.cfg.ReportPath, report)
}

func (m *Manager) appendHistoryFile(report Report) error {
	if m.cfg.ReportPath == "" {
		return nil
	}
	reports, err := m.readHistoryFile()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	archived := *copyReport(report)
	archived.Cancellable = false
	for i := range archived.Benchmarks {
		archived.Benchmarks[i].Output = ""
	}
	filtered := []Report{archived}
	for _, item := range reports {
		if item.JobID != archived.JobID {
			filtered = append(filtered, item)
		}
		if len(filtered) == maxHistoryReports {
			break
		}
	}
	return writeJSONAtomic(historyPath(m.cfg.ReportPath), filtered)
}

func (m *Manager) readHistoryFile() ([]Report, error) {
	data, err := os.ReadFile(historyPath(m.cfg.ReportPath))
	if err != nil {
		return nil, err
	}
	var reports []Report
	if err := json.Unmarshal(data, &reports); err != nil {
		return nil, err
	}
	if reports == nil {
		reports = []Report{}
	}
	return reports, nil
}

func historyPath(reportPath string) string {
	dir, ext := filepath.Dir(reportPath), filepath.Ext(reportPath)
	if ext == "" {
		ext = ".json"
	}
	base := strings.TrimSuffix(filepath.Base(reportPath), filepath.Ext(reportPath))
	base = strings.TrimSuffix(base, "-latest")
	if base == "" {
		base = "stress"
	}
	return filepath.Join(dir, base+"-history"+ext)
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".stress-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

func copyReport(report Report) *Report {
	copy := report
	copy.Benchmarks = append([]BenchmarkResult(nil), report.Benchmarks...)
	for i := range copy.Benchmarks {
		copy.Benchmarks[i].Profile = copyExecutionProfile(report.Benchmarks[i].Profile)
		copy.Benchmarks[i].Values = copyValues(report.Benchmarks[i].Values)
	}
	return &copy
}

func copyValues(values map[string]float64) map[string]float64 {
	if values == nil {
		return nil
	}
	copy := make(map[string]float64, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func copyConfig(cfg Config) Config {
	copy := cfg
	copy.DefaultBenchmarks = append([]string(nil), cfg.DefaultBenchmarks...)
	if cfg.Benchmarks != nil {
		copy.Benchmarks = make(map[string]BenchmarkConfig, len(cfg.Benchmarks))
		for name, benchmark := range cfg.Benchmarks {
			copy.Benchmarks[name] = benchmark
		}
	}
	return copy
}

func newJobID() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func (m *Manager) logInfo(message string, args ...any) {
	if m.logger != nil {
		m.logger.Info(message, args...)
	}
}
func (m *Manager) logError(message string, args ...any) {
	if m.logger != nil {
		m.logger.Error(message, args...)
	}
}
