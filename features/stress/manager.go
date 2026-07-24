package stress

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
		return m.active.report, ErrBusy
	}
	selected, err := m.selected(names)
	if err != nil {
		return Report{}, err
	}
	if err := m.validateTimeout(selected, options.Timeout); err != nil {
		return Report{}, err
	}
	now := time.Now()
	report := Report{JobID: newJobID(), Timestamp: now, StartedAt: now, Platform: runtime.GOOS, TimeoutSeconds: options.Timeout.Milliseconds() / 1000, Status: StatusRunning, HealthCondition: "Running"}
	for _, name := range selected {
		report.Benchmarks = append(report.Benchmarks, BenchmarkResult{Name: name, Status: StatusPending})
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.active = &activeJob{cancel: cancel, report: report}
	m.last = copyReport(report)
	m.writeReportLocked(report)
	go m.run(ctx, selected, options.Timeout)
	return report, nil
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
	report.Status = StatusHealthy
	report.HealthCondition = "Healthy"
	for _, benchmark := range report.Benchmarks {
		if benchmark.Status != StatusHealthy {
			report.Status = StatusUnhealthy
			report.HealthCondition = "Unhealthy"
			break
		}
	}
	m.active = nil
	m.last = copyReport(report)
	m.writeReportLocked(report)
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
	if name != "stream" && (benchmark.Path == "" || !isDir(benchmark.Path)) {
		return finish(StatusUnavailable, "benchmark asset directory is unavailable")
	}
	timeout := effectiveTimeout(benchmark.Timeout)
	if timeoutOverride > 0 && timeoutOverride < timeout {
		timeout = timeoutOverride
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := []string{m.cfg.ScriptPath, name, benchmark.Path}
	cmd := exec.CommandContext(runCtx, "bash", args...)
	cmd.Dir = filepath.Dir(m.cfg.ScriptPath)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	result.Output = truncateOutput(string(output))
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return finish(StatusTimeout, "benchmark timed out")
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return finish(StatusCancelled, "benchmark cancelled")
	}
	if err != nil {
		return finish(StatusUnhealthy, fmt.Sprintf("benchmark command failed: %v", err))
	}
	resultDir := benchmark.ResultDir
	if resultDir == "" {
		resultDir = cmd.Dir
	}
	values, source, err := parseBenchmark(name, string(output), resultDir)
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
	m.last = copyReport(m.active.report)
	m.writeReportLocked(m.active.report)
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
	m.last = copyReport(m.active.report)
	m.writeReportLocked(m.active.report)
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

func (m *Manager) writeReportLocked(report Report) {
	if m.cfg.ReportPath == "" {
		return
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(m.cfg.ReportPath)
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".stress-*.tmp")
	if err != nil {
		return
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

func truncateOutput(output string) string {
	if len(output) <= maxOutputBytes {
		return output
	}
	return output[:maxOutputBytes] + "\n… output truncated"
}

func newJobID() string {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
