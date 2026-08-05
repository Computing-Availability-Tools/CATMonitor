package stress

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	describeProtocolVersion = 1
	describeTimeout         = 2 * time.Second
	describeCacheTTL        = 10 * time.Second
	maxDescribeBytes        = 256 * 1024
	describeProtocolMarker  = "CATMONITOR_STRESS_DESCRIBE_PROTOCOL=1"
)

type profileCacheEntry struct {
	profile   *ExecutionProfile
	err       error
	expiresAt time.Time
	scriptMod time.Time
	scriptLen int64
}

// Describe returns the effective, read-only workload profile for a benchmark.
// The dispatcher contract is:
//
//	bash benchmark_check.sh describe <stream|hpl|hpcg>
//
// It must write exactly one JSON object to stdout and must not launch a
// benchmark. Results are cached briefly because the Web UI polls configuration.
func (m *Manager) Describe(name string) (*ExecutionProfile, error) {
	return m.describeWithTimeout(name, 0)
}

func (m *Manager) describeWithTimeout(name string, timeoutOverride time.Duration) (*ExecutionProfile, error) {
	if runtime.GOOS != "linux" {
		return fallbackProfile(m, name, timeoutOverride, "describe is supported on Linux only"),
			errors.New("describe is supported on Linux only")
	}
	if !supportedBenchmark(name) {
		return nil, fmt.Errorf("unsupported benchmark %q", name)
	}
	info, err := os.Stat(m.cfg.ScriptPath)
	if err != nil {
		return fallbackProfile(m, name, timeoutOverride, "benchmark dispatcher script is unavailable"), err
	}

	m.profileMu.Lock()
	cached, ok := m.profileCache[name]
	if ok && time.Now().Before(cached.expiresAt) &&
		cached.scriptMod.Equal(info.ModTime()) && cached.scriptLen == info.Size() {
		profile := copyExecutionProfile(cached.profile)
		cachedErr := cached.err
		m.profileMu.Unlock()
		return m.applyRunConfiguration(profile, name, timeoutOverride), cachedErr
	}
	m.profileMu.Unlock()

	profile, describeErr := m.readDescribeProfile(name)
	if describeErr != nil {
		profile = fallbackProfile(m, name, 0, describeErr.Error())
	}
	profile = m.applyRunConfiguration(profile, name, 0)

	m.profileMu.Lock()
	m.profileCache[name] = profileCacheEntry{
		profile: copyExecutionProfile(profile), err: describeErr,
		expiresAt: time.Now().Add(describeCacheTTL),
		scriptMod: info.ModTime(), scriptLen: info.Size(),
	}
	m.profileMu.Unlock()
	return m.applyRunConfiguration(copyExecutionProfile(profile), name, timeoutOverride), describeErr
}

func (m *Manager) readDescribeProfile(name string) (*ExecutionProfile, error) {
	script, err := os.ReadFile(m.cfg.ScriptPath)
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(script, []byte(describeProtocolMarker)) {
		return nil, errors.New("dispatcher does not declare describe protocol version 1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), describeTimeout)
	defer cancel()
	cmd := benchmarkCommand(ctx, "bash", m.cfg.ScriptPath, "describe", name)
	cmd.Dir = filepath.Dir(m.cfg.ScriptPath)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("describe protocol timed out")
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if len(message) > 256 {
			message = message[:256]
		}
		return nil, fmt.Errorf("describe command failed: %v: %s", err, message)
	}
	if stdout.Len() > maxDescribeBytes {
		return nil, fmt.Errorf("describe response exceeds %d bytes", maxDescribeBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	var profile ExecutionProfile
	if err := decoder.Decode(&profile); err != nil {
		return nil, fmt.Errorf("decode describe response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("decode describe response: multiple JSON values")
		}
		return nil, fmt.Errorf("decode describe response: %w", err)
	}
	if err := validateDescribeProfile(name, &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

func validateDescribeProfile(name string, profile *ExecutionProfile) error {
	if profile.ProtocolVersion != describeProtocolVersion {
		return fmt.Errorf("unsupported describe protocol version %d", profile.ProtocolVersion)
	}
	if profile.Benchmark != name {
		return fmt.Errorf("describe benchmark mismatch: got %q, want %q", profile.Benchmark, name)
	}
	if !validCheckStatus(profile.Preflight.Status) {
		return fmt.Errorf("invalid preflight status %q", profile.Preflight.Status)
	}
	if !validCheckStatus(profile.MPI.Status) ||
		profile.MPI.Implementation == "" || profile.MPI.ExecutableABI == "" {
		return fmt.Errorf("invalid MPI check status %q", profile.MPI.Status)
	}
	if profile.Resources.MPIProcesses < 0 || profile.Resources.ThreadsPerProcess < 0 ||
		profile.Resources.TotalWorkers < 0 || profile.Resources.RuntimeSeconds < 0 {
		return errors.New("describe resources must be non-negative")
	}
	if profile.Resources.MPIProcesses > 0 && profile.Resources.ThreadsPerProcess > 0 &&
		profile.Resources.TotalWorkers != profile.Resources.MPIProcesses*profile.Resources.ThreadsPerProcess {
		return errors.New("describe total_workers does not match MPI processes multiplied by threads")
	}
	seen := make(map[string]bool, len(profile.Parameters))
	for _, parameter := range profile.Parameters {
		if parameter.Key == "" || parameter.Label == "" || seen[parameter.Key] {
			return errors.New("describe parameters require unique non-empty keys and labels")
		}
		seen[parameter.Key] = true
	}
	for _, asset := range profile.Assets {
		if asset.Name == "" || asset.Path == "" || !validCheckStatus(asset.Status) {
			return errors.New("describe assets require name, path, and a valid status")
		}
		if asset.SHA256 != "" && !validSHA256(asset.SHA256) {
			return fmt.Errorf("describe asset %q has an invalid SHA-256", asset.Name)
		}
		if asset.Required && asset.Status == CheckFail && profile.Preflight.Status != CheckFail {
			return errors.New("failed required asset requires failed preflight status")
		}
	}
	if profile.MPI.Status == CheckFail && profile.Preflight.Status != CheckFail {
		return errors.New("failed MPI compatibility requires failed preflight status")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validCheckStatus(status CheckStatus) bool {
	switch status {
	case CheckPass, CheckWarn, CheckFail:
		return true
	default:
		return false
	}
}

func fallbackProfile(m *Manager, name string, timeoutOverride time.Duration, message string) *ExecutionProfile {
	profile := &ExecutionProfile{
		ProtocolVersion:          0,
		Benchmark:                name,
		DescribeProtocolFallback: true,
		MPI: MPICheck{
			Implementation: "unknown", ExecutableABI: "unknown",
			Status: CheckUnsupported, Message: "describe protocol unavailable",
		},
		Preflight: PreflightResult{
			Status:  CheckUnsupported,
			Message: "only basic deployment checks are available: " + message,
		},
	}
	return m.applyRunConfiguration(profile, name, timeoutOverride)
}

func (m *Manager) applyRunConfiguration(profile *ExecutionProfile, name string, timeoutOverride time.Duration) *ExecutionProfile {
	if profile == nil {
		return nil
	}
	benchmark := m.cfg.Benchmarks[name]
	timeout := effectiveTimeout(benchmark.Timeout)
	if timeoutOverride > 0 && timeoutOverride < timeout {
		timeout = timeoutOverride
	}
	profile.TimeoutSeconds = int64(timeout / time.Second)
	profile.ResultDirectory = benchmark.ResultDir
	profile.ScriptSHA256, _ = fileSHA256(m.cfg.ScriptPath)
	profile.ConfigurationSHA256 = ""
	data, err := json.Marshal(profile)
	if err == nil {
		sum := sha256.Sum256(data)
		profile.ConfigurationSHA256 = hex.EncodeToString(sum[:])
	}
	return profile
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func aggregateConfigurationSHA256(names []string, profiles map[string]*ExecutionProfile) string {
	type entry struct {
		Name string `json:"name"`
		Hash string `json:"sha256"`
	}
	entries := make([]entry, 0, len(names))
	for _, name := range names {
		hash := ""
		if profile := profiles[name]; profile != nil {
			hash = profile.ConfigurationSHA256
		}
		entries = append(entries, entry{Name: name, Hash: hash})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	data, _ := json.Marshal(entries)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func copyExecutionProfile(profile *ExecutionProfile) *ExecutionProfile {
	if profile == nil {
		return nil
	}
	copy := *profile
	copy.Parameters = append([]ProfileParameter(nil), profile.Parameters...)
	copy.Assets = append([]AssetCheck(nil), profile.Assets...)
	return &copy
}
