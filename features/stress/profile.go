package stress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

const (
	describeProtocolVersion = 1
	describeTimeout         = 2 * time.Second
	npuDescribeTimeout      = 30 * time.Second
	describeCacheTTL        = 10 * time.Second
	npuDescribeCacheTTL     = 60 * time.Second
)

type profileCacheEntry struct {
	profile   *ExecutionProfile
	err       error
	expiresAt time.Time
}

func (m *Manager) Describe(name string) (*ExecutionProfile, error) {
	return m.describeWithTimeout(name, 0)
}

func (m *Manager) describeWithTimeout(name string, timeoutOverride time.Duration) (*ExecutionProfile, error) {
	if !supportedBenchmark(name) {
		return nil, fmt.Errorf("unsupported benchmark %q", name)
	}
	m.profileMu.Lock()
	if cached, ok := m.profileCache[name]; ok && time.Now().Before(cached.expiresAt) {
		profile, err := copyExecutionProfile(cached.profile), cached.err
		m.profileMu.Unlock()
		return m.applyRunConfiguration(profile, name, timeoutOverride), err
	}
	m.profileMu.Unlock()

	timeout := describeTimeout
	cacheTTL := describeCacheTTL
	if name == "npu_burn" {
		timeout, cacheTTL = npuDescribeTimeout, npuDescribeCacheTTL
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	profile, describeErr := m.executor.Describe(ctx, m.binding(name))
	if describeErr == nil {
		if err := validateDescribeProfile(name, profile); err != nil {
			describeErr = err
		} else {
			profile = m.applyRunConfiguration(profile, name, 0)
		}
	}
	m.profileMu.Lock()
	m.profileCache[name] = profileCacheEntry{
		profile: copyExecutionProfile(profile), err: describeErr, expiresAt: time.Now().Add(cacheTTL),
	}
	m.profileMu.Unlock()
	if describeErr != nil {
		return nil, describeErr
	}
	return m.applyRunConfiguration(copyExecutionProfile(profile), name, timeoutOverride), nil
}

func validateDescribeProfile(name string, profile *ExecutionProfile) error {
	if profile == nil {
		return errors.New("empty describe profile")
	}
	if profile.ProtocolVersion != describeProtocolVersion {
		return fmt.Errorf("unsupported describe protocol version %d", profile.ProtocolVersion)
	}
	if profile.Benchmark != name {
		return fmt.Errorf("describe benchmark mismatch: got %q, want %q", profile.Benchmark, name)
	}
	if !validCheckStatus(profile.Preflight.Status) {
		return fmt.Errorf("invalid preflight status %q", profile.Preflight.Status)
	}
	if !validCheckStatus(profile.MPI.Status) || profile.MPI.Implementation == "" || profile.MPI.ExecutableABI == "" {
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
	profile.Executor = "docker_exec"
	profile.Container = benchmark.Container
	profile.Plugin = benchmark.Plugin
	if profile.Plugin == "" {
		profile.Plugin = name
	}
	profile.ConfigurationSHA256 = ""
	data, err := json.Marshal(profile)
	if err == nil {
		sum := sha256.Sum256(data)
		profile.ConfigurationSHA256 = hex.EncodeToString(sum[:])
	}
	return profile
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
	if profile.RuntimeIdentity != nil {
		copy.RuntimeIdentity = make(map[string]string, len(profile.RuntimeIdentity))
		for key, value := range profile.RuntimeIdentity {
			copy.RuntimeIdentity[key] = value
		}
	}
	return &copy
}
