package stress

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

const maxOutputBytes = 16 * 1024

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
	cfg Config

	mu     sync.Mutex
	active *activeJob
	last   *Report
}

type activeJob struct {
	cancel context.CancelFunc
	report Report
}

func NewManager(cfg Config) *Manager { return &Manager{cfg: cfg} }

func (m *Manager) Config() Config { return m.cfg }

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
	now := time.Now()
	report := Report{JobID: newJobID(), Timestamp: now, StartedAt: now, Platform: runtime.GOOS, TimeoutSeconds: options.Timeout.Milliseconds() / 1000, Status: StatusRunning, HealthCondition: "Running"}
	for _, name := range selected {
		report.Benchmarks = append(report.Benchmarks, BenchmarkResult{Name: name, Status: StatusPending})
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.active = &activeJob{cancel: cancel, report: report}
	if err := m.persistReportLocked(&m.active.report); err != nil {
		cancel()
		m.active = nil
		return Report{}, fmt.Errorf("persist initial stress report: %w", err)
	}
	m.last = copyReport(m.active.report)
	startedReport := *copyReport(m.active.report)
	go m.run(ctx, selected, options.Timeout)
	return startedReport, nil
}

func (m *Manager) Latest() (Report, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.last != nil {
		return *copyReport(*m.last), nil
	}
	if m.cfg.ReportPath == "" {
		return Report{}, os.ErrNotExist
	}
	data, err := os.ReadFile(m.cfg.ReportPath)
	if err != nil {
		return Report{}, err
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return Report{}, err
	}
	m.last = &report
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
	m.active.cancel()
	return nil
}

func (m *Manager) run(ctx context.Context, names []string, timeoutOverride time.Duration) {
	for _, name := range names {
		m.setBenchmark(name, StatusRunning, "", nil, "", "", time.Time{}, false)
		result := m.runBenchmark(ctx, name, timeoutOverride)
		m.finishBenchmark(result)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return
	}
	report := m.active.report
	report.FinishedAt = time.Now()
	report.Timestamp = report.FinishedAt
	report.Status, report.HealthCondition = aggregateReportStatus(report.Benchmarks)
	m.active = nil
	_ = m.persistReportLocked(&report)
	m.last = copyReport(report)
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

func (m *Manager) runBenchmark(ctx context.Context, name string, timeoutOverride time.Duration) BenchmarkResult {
	started := time.Now()
	result := BenchmarkResult{Name: name, Status: StatusUnhealthy, StartedAt: started}
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

// Availability performs the deployment checks CATMonitor can know without
// executing the host-specific dispatcher. The dispatcher remains responsible
// for checking its concrete executable and MPI/NUMA environment.
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
	return true, "deployment precheck passed"
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
	if err := m.writeReportLocked(*report); err != nil {
		report.ReportError = err.Error()
		return err
	}
	return nil
}

func (m *Manager) writeReportLocked(report Report) error {
	if m.cfg.ReportPath == "" {
		return nil
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(m.cfg.ReportPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".stress-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if err == nil {
		err = os.Rename(name, m.cfg.ReportPath)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

func copyReport(report Report) *Report {
	copy := report
	copy.Benchmarks = append([]BenchmarkResult(nil), report.Benchmarks...)
	return &copy
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
