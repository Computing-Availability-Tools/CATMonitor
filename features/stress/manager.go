package stress

// Manager implements the explicit benchmark job lifecycle.
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
)

var (
	ErrDisabled = errors.New("stress testing is disabled")
	ErrBusy     = errors.New("a stress job is already running")
	ErrNotFound = errors.New("stress job not found")
)

const (
	maxOutputBytes     = 16 * 1024
	maxHistoryReports  = 100
	defaultHistoryRead = 20
)

// boundedOutput keeps only the tail of combined stdout/stderr. HPL emits its
// result row and residual summary near the end, while STREAM output is small
// and HPCG is parsed from its result file. This bounds memory during execution
// instead of collecting unbounded output and truncating only afterwards.
type boundedOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(p)
	if len(p) >= maxOutputBytes {
		b.data = append(b.data[:0], p[len(p)-maxOutputBytes:]...)
		b.truncated = true
		return written, nil
	}
	if overflow := len(b.data) + len(p) - maxOutputBytes; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
		b.truncated = true
	}
	b.data = append(b.data, p...)
	return written, nil
}

func (b *boundedOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.truncated {
		return string(b.data)
	}
	return "… output truncated; showing tail\n" + string(b.data)
}

type Manager struct {
	cfg         Config
	logger      *slog.Logger
	writeReport func(Report) error

	mu           sync.Mutex
	active       *activeJob
	last         *Report
	profileMu    sync.Mutex
	profileCache map[string]profileCacheEntry
}

type activeJob struct {
	cancel      context.CancelFunc
	done        chan struct{}
	releaseLock func() error
	report      Report
}

func NewManager(cfg Config) *Manager {
	return NewManagerWithLogger(cfg, nil)
}

func NewManagerWithLogger(cfg Config, logger *slog.Logger) *Manager {
	manager := &Manager{
		cfg:          copyConfig(cfg),
		logger:       logger,
		profileCache: make(map[string]profileCacheEntry),
	}
	manager.writeReport = manager.writeReportFile
	return manager
}

func (m *Manager) Config() Config { return copyConfig(m.cfg) }

func (m *Manager) Start(names []string) (Report, error) {
	return m.StartWithOptions(names, RunOptions{})
}

func (m *Manager) StartWithOptions(names []string, options RunOptions) (Report, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.cfg.Enabled {
		return Report{}, ErrDisabled
	}
	if m.active != nil {
		m.logWarn("stress job rejected because another job is running", "job_id", m.active.report.JobID)
		return *copyReport(m.active.report), ErrBusy
	}
	selected, err := m.selected(names)
	if err != nil {
		return Report{}, err
	}
	if err := m.validateTimeout(selected, options.Timeout); err != nil {
		return Report{}, err
	}
	if runtime.GOOS == "linux" {
		for _, name := range selected {
			if available, message := m.Availability(name); !available {
				return Report{}, fmt.Errorf("benchmark %q is unavailable: %s", name, message)
			}
		}
	}
	profiles := make(map[string]*ExecutionProfile, len(selected))
	for _, name := range selected {
		profile, _ := m.describeWithTimeout(name, options.Timeout)
		profiles[name] = profile
	}
	releaseLock, err := acquireJobLock(m.cfg.ReportPath)
	if errors.Is(err, ErrBusy) {
		report, readErr := m.readReportFile()
		if readErr == nil {
			m.last = copyReport(report)
			m.logWarn("stress job rejected because another process is running", "job_id", report.JobID, "initiator", report.Initiator)
			return *copyReport(report), ErrBusy
		}
		m.logWarn("stress job rejected because another process is running", "report_error", readErr)
		return Report{}, ErrBusy
	}
	if err != nil {
		return Report{}, fmt.Errorf("persist initial stress report coordination: %w", err)
	}
	now := time.Now()
	report := Report{
		JobID:               newJobID(),
		Initiator:           options.Initiator,
		Timestamp:           now,
		StartedAt:           now,
		Platform:            runtime.GOOS,
		TimeoutSeconds:      options.Timeout.Milliseconds() / 1000,
		Status:              StatusRunning,
		HealthCondition:     "Running",
		ConfigurationSHA256: aggregateConfigurationSHA256(selected, profiles),
	}
	for _, name := range selected {
		report.Benchmarks = append(report.Benchmarks, BenchmarkResult{
			Name: name, Status: StatusPending, Profile: copyExecutionProfile(profiles[name]),
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.active = &activeJob{cancel: cancel, done: make(chan struct{}), releaseLock: releaseLock, report: report}
	if err := m.persistReportLocked(&m.active.report); err != nil {
		cancel()
		_ = releaseLock()
		m.active = nil
		return Report{}, fmt.Errorf("persist initial stress report: %w", err)
	}
	m.last = copyReport(m.active.report)
	startedReport := *copyReport(m.active.report)
	m.logInfo("stress job started", "job_id", report.JobID, "initiator", report.Initiator, "benchmarks", selected, "timeout_seconds", report.TimeoutSeconds)
	go m.run(ctx, report.JobID, selected, options.Timeout, profiles)
	return startedReport, nil
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

// Shutdown cancels the job owned by this Manager and waits for its final report
// and cross-process lock release. Jobs owned by another process are never
// cancelled.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if m.active == nil {
		m.mu.Unlock()
		return nil
	}
	jobID := m.active.report.JobID
	cancel := m.active.cancel
	done := m.active.done
	m.logInfo("stress manager shutting down active job", "job_id", jobID)
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
		m.setBenchmark(name, StatusRunning, "", nil, "", "", time.Time{}, false)
		m.logInfo("stress benchmark started", "job_id", jobID, "benchmark", name)
		result := m.runBenchmark(ctx, name, timeoutOverride, profiles[name])
		m.finishBenchmark(result)
		m.logInfo("stress benchmark finished", "job_id", jobID, "benchmark", name, "status", result.Status, "duration_ms", result.DurationMS)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return
	}
	job := m.active
	report := job.report
	report.FinishedAt = time.Now()
	report.Timestamp = report.FinishedAt
	report.Status, report.HealthCondition = aggregateReportStatus(report.Benchmarks)
	_ = m.persistReportLocked(&report)
	if err := m.appendHistoryFile(report); err != nil {
		m.logError("stress history persistence failed", "job_id", report.JobID, "error", err)
	}
	m.last = copyReport(report)
	m.active = nil
	if err := job.releaseLock(); err != nil {
		m.logError("stress job lock release failed", "job_id", report.JobID, "error", err)
	}
	close(job.done)
	m.logInfo("stress job finished", "job_id", report.JobID, "initiator", report.Initiator, "status", report.Status, "duration_ms", report.FinishedAt.Sub(report.StartedAt).Milliseconds(), "report_error", report.ReportError)
}

func aggregateReportStatus(benchmarks []BenchmarkResult) (Status, string) {
	allHealthy := len(benchmarks) > 0
	hasTimeout, hasCancelled := false, false
	hasUnavailable, hasUnsupported := false, false
	for _, benchmark := range benchmarks {
		switch benchmark.Status {
		case StatusHealthy, StatusTimeLimitReached:
			continue
		case StatusUnhealthy:
			return StatusUnhealthy, "Unhealthy"
		case StatusTimeout:
			hasTimeout = true
		case StatusCancelled:
			hasCancelled = true
		case StatusUnavailable:
			hasUnavailable = true
		case StatusUnsupported:
			hasUnsupported = true
		default:
			return StatusUnhealthy, "Unhealthy"
		}
		allHealthy = false
	}
	if allHealthy {
		return StatusHealthy, "Healthy"
	}
	if hasTimeout {
		return StatusTimeout, "Incomplete"
	}
	if hasCancelled {
		return StatusCancelled, "Incomplete"
	}
	if hasUnavailable {
		return StatusUnavailable, "Unavailable"
	}
	if hasUnsupported {
		return StatusUnsupported, "Unsupported"
	}
	return StatusUnhealthy, "Unhealthy"
}

func (m *Manager) runBenchmark(ctx context.Context, name string, timeoutOverride time.Duration, profile *ExecutionProfile) BenchmarkResult {
	started := time.Now()
	result := BenchmarkResult{
		Name: name, Status: StatusUnhealthy, StartedAt: started,
		Profile: copyExecutionProfile(profile),
	}
	finish := func(status Status, message string) BenchmarkResult {
		result.Status = status
		result.Message = message
		result.FinishedAt = time.Now()
		result.DurationMS = result.FinishedAt.Sub(started).Milliseconds()
		return result
	}
	if runtime.GOOS != "linux" {
		return finish(StatusUnsupported, "stress execution is supported on Linux only")
	}
	benchmark, ok := m.cfg.Benchmarks[name]
	if !ok || !benchmark.Enabled {
		return finish(StatusUnavailable, "benchmark is not enabled in configuration")
	}
	if m.cfg.ScriptPath == "" || !isRegularFile(m.cfg.ScriptPath) {
		return finish(StatusUnavailable, "benchmark script is unavailable")
	}
	resultDir := benchmark.ResultDir
	if name == "hpcg" && (resultDir == "" || !isDir(resultDir)) {
		return finish(StatusUnavailable, "HPCG result directory is unavailable")
	}
	if resultDir == "" {
		resultDir = filepath.Dir(m.cfg.ScriptPath)
	}
	var hpcgBefore map[string]fileSignature
	if name == "hpcg" {
		var err error
		hpcgBefore, err = snapshotHPCGResults(resultDir)
		if err != nil {
			return finish(StatusUnavailable, err.Error())
		}
	}
	timeout := effectiveTimeout(benchmark.Timeout)
	if timeoutOverride > 0 && timeoutOverride < timeout {
		timeout = timeoutOverride
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := []string{m.cfg.ScriptPath, name}
	cmd := benchmarkCommand(runCtx, "bash", args...)
	cmd.Dir = filepath.Dir(m.cfg.ScriptPath)
	cmd.Env = os.Environ()
	var output boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	outputText := output.String()
	result.Output = outputText
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return finish(StatusTimeLimitReached, "configured time limit reached; benchmark stopped as planned (final performance values were not produced)")
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return finish(StatusCancelled, "benchmark cancelled")
	}
	if err != nil {
		return finish(StatusUnhealthy, fmt.Sprintf("benchmark command failed: %v", err))
	}
	values, source, err := parseBenchmark(name, outputText, resultDir, hpcgBefore)
	if err != nil {
		return finish(StatusUnhealthy, err.Error())
	}
	result.Values = values
	result.Source = source
	return finish(StatusHealthy, "command completed and required values parsed")
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

// Availability combines the basic CATMonitor deployment checks with the
// dispatcher's read-only describe/preflight protocol. Legacy host adapters
// remain runnable but are reported with a compatibility warning until they
// implement describe.
func (m *Manager) Availability(name string) (bool, string) {
	if runtime.GOOS != "linux" {
		return false, "stress execution is supported on Linux only"
	}
	if !supportedBenchmark(name) {
		return false, "unsupported benchmark name"
	}
	benchmark, ok := m.cfg.Benchmarks[name]
	if !ok {
		return false, "benchmark is not configured"
	}
	if !benchmark.Enabled {
		return false, "benchmark is disabled in configuration"
	}
	if m.cfg.ScriptPath == "" || !isRegularFile(m.cfg.ScriptPath) {
		return false, "benchmark dispatcher script is unavailable"
	}
	if name == "hpcg" && (benchmark.ResultDir == "" || !isDir(benchmark.ResultDir)) {
		return false, "HPCG result directory is unavailable"
	}
	profile, err := m.Describe(name)
	if err != nil {
		return true, "basic deployment precheck passed; describe protocol unavailable: " + err.Error()
	}
	switch profile.Preflight.Status {
	case CheckFail:
		return false, profile.Preflight.Message
	case CheckWarn:
		return true, profile.Preflight.Message
	default:
		return true, "deployment precheck passed"
	}
}

func (m *Manager) setBenchmark(name string, status Status, message string, values map[string]float64, source, output string, finished time.Time, complete bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return
	}
	for i := range m.active.report.Benchmarks {
		result := &m.active.report.Benchmarks[i]
		if result.Name != name {
			continue
		}
		result.Status, result.Message, result.Values, result.Source, result.Output = status, message, values, source, output
		if status == StatusRunning {
			result.StartedAt = time.Now()
		}
		if complete {
			result.FinishedAt = finished
			result.DurationMS = finished.Sub(result.StartedAt).Milliseconds()
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
	seen := make(map[string]bool, len(names))
	selected := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" || seen[name] {
			continue
		}
		if !supportedBenchmark(name) {
			return nil, fmt.Errorf("benchmark %q is not supported", name)
		}
		if _, ok := m.cfg.Benchmarks[name]; !ok {
			return nil, fmt.Errorf("benchmark %q is not configured", name)
		}
		if !m.cfg.Benchmarks[name].Enabled {
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
	case "stream", "hpl", "hpcg":
		return true
	default:
		return false
	}
}

func (m *Manager) persistReportLocked(report *Report) error {
	report.ReportError = ""
	if err := m.writeReport(*report); err != nil {
		report.ReportError = err.Error()
		m.logError("stress report persistence failed", "job_id", report.JobID, "status", report.Status, "error", err)
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

// History returns final reports ordered newest first. The latest report remains
// the source of truth for running state; history is a bounded operational view.
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
		// The bounded command tail is useful in the latest diagnostic report,
		// but retaining it for every run would make history unnecessarily large.
		archived.Benchmarks[i].Output = ""
	}
	filtered := make([]Report, 0, len(reports)+1)
	filtered = append(filtered, archived)
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
	dir := filepath.Dir(reportPath)
	ext := filepath.Ext(reportPath)
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
	closeErr := tmp.Close()
	if err == nil {
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
		if report.Benchmarks[i].Values == nil {
			continue
		}
		copy.Benchmarks[i].Values = make(map[string]float64, len(report.Benchmarks[i].Values))
		for key, value := range report.Benchmarks[i].Values {
			copy.Benchmarks[i].Values[key] = value
		}
	}
	return &copy
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

func (m *Manager) logInfo(message string, args ...any) {
	if m.logger != nil {
		m.logger.Info(message, args...)
	}
}

func (m *Manager) logWarn(message string, args ...any) {
	if m.logger != nil {
		m.logger.Warn(message, args...)
	}
}

func (m *Manager) logError(message string, args ...any) {
	if m.logger != nil {
		m.logger.Error(message, args...)
	}
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
func isDir(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }

func newJobID() string {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
