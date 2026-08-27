// Package resultparse converts benchmark-specific output into the normalized
// values returned by the workload execution protocol. Parsing runs inside the
// workload container so file-producing benchmarks never expose private work
// directories to the CATMonitor controller.
package resultparse

import (
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	streamLine        = regexp.MustCompile(`(?im)^\s*(copy|scale|add|triad):\s*([0-9]+(?:\.[0-9]+)?)`)
	hplFailedCount    = regexp.MustCompile(`(?im)^\s*([0-9]+)\s+tests?\s+completed\s+and\s+failed\s+residual\s+checks`)
	hplExplicitFailed = regexp.MustCompile(`(?im)\bFAILED\s*$`)
	hpcgGFLOPS        = regexp.MustCompile(`HPCG result is VALID with a GFLOP/s rating of=\s*([0-9]+(?:\.[0-9]+)?)`)
	hpcgTime          = regexp.MustCompile(`Results are valid but execution time \(sec\) is=\s*([0-9]+(?:\.[0-9]+)?)`)
	npuBurnSummary    = regexp.MustCompile(`^CATMONITOR_NPU_BURN_SUMMARY\s+devices=(\S+)\s+cases=(\S+)\s+passed=(\S+)\s+failed=(\S+)\s+errors=(\S+)\s+case_time_seconds=(\S+)[ \t]*(?:\r?\n|$)`)
)

const npuBurnSummaryToken = "CATMONITOR_NPU_BURN_SUMMARY"

type fileSignature struct {
	size   int64
	modNS  int64
	digest [sha256.Size]byte
}

// Snapshot is an opaque pre-run view used to reject stale HPCG result files.
type Snapshot struct {
	hpcg map[string]fileSignature
}

// Capture records any benchmark-specific pre-run state.
func Capture(name, resultDir string) (Snapshot, error) {
	if name != "hpcg" {
		return Snapshot{}, nil
	}
	files, err := snapshotHPCGResults(resultDir)
	return Snapshot{hpcg: files}, err
}

// Parse validates a successful workload and returns normalized numeric values.
func Parse(name, output, resultDir string, before Snapshot) (map[string]float64, string, error) {
	switch name {
	case "stream":
		return parseStream(output)
	case "hpl":
		return parseHPL(output)
	case "hpcg":
		return parseHPCG(resultDir, before.hpcg)
	case "npu_burn":
		return parseNPUBurn(output)
	default:
		return nil, "", fmt.Errorf("unsupported benchmark %q", name)
	}
}

func parseNPUBurn(output string) (map[string]float64, string, error) {
	summaryIndex := strings.LastIndex(output, npuBurnSummaryToken)
	if summaryIndex < 0 {
		return nil, "", fmt.Errorf("Ascend NPU Burn validated summary not found")
	}
	match := npuBurnSummary.FindStringSubmatch(output[summaryIndex:])
	if len(match) != 7 {
		return nil, "", fmt.Errorf("Ascend NPU Burn summary protocol error: malformed fields")
	}

	integerKeys := []string{"devices", "cases", "passed", "failed", "errors"}
	values := make(map[string]float64, 6)
	for index, key := range integerKeys {
		value, err := strconv.ParseUint(match[index+1], 10, 64)
		if err != nil {
			return nil, "", fmt.Errorf("Ascend NPU Burn summary protocol error: invalid %s", key)
		}
		values[key] = float64(value)
	}
	caseTime, err := strconv.ParseFloat(match[6], 64)
	if err != nil || math.IsNaN(caseTime) || math.IsInf(caseTime, 0) || caseTime < 0 {
		return nil, "", fmt.Errorf("Ascend NPU Burn summary protocol error: invalid case_time_seconds")
	}
	values["case_time_seconds"] = caseTime

	if values["devices"] < 1 || values["cases"] < 1 {
		return nil, "", fmt.Errorf("Ascend NPU Burn summary protocol error: devices and cases must be at least 1")
	}
	if values["passed"]+values["failed"] != values["cases"] {
		return nil, "", fmt.Errorf("Ascend NPU Burn summary protocol error: passed plus failed must equal cases")
	}
	if values["passed"] != values["cases"] || values["failed"] != 0 || values["errors"] != 0 {
		return values, "result_csv", fmt.Errorf("Ascend NPU Burn summary did not report a complete pass")
	}
	return values, "result_csv", nil
}

func parseStream(output string) (map[string]float64, string, error) {
	values := make(map[string]float64)
	for _, match := range streamLine.FindAllStringSubmatch(output, -1) {
		value, _ := strconv.ParseFloat(match[2], 64)
		values[strings.ToLower(match[1])+"_mb_s"] = value
	}
	for _, key := range []string{"copy_mb_s", "scale_mb_s", "add_mb_s", "triad_mb_s"} {
		if _, ok := values[key]; !ok {
			return nil, "", fmt.Errorf("STREAM result missing %s", key)
		}
	}
	return values, "stdout", nil
}

func parseHPL(output string) (map[string]float64, string, error) {
	if match := hplFailedCount.FindStringSubmatch(output); len(match) == 2 {
		failed, _ := strconv.Atoi(match[1])
		if failed > 0 {
			return nil, "", fmt.Errorf("HPL reported %d failed residual check(s)", failed)
		}
	}
	if hplExplicitFailed.MatchString(output) {
		return nil, "", fmt.Errorf("HPL reported FAILED")
	}

	seenHeader := false
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "T/V") && strings.Contains(line, "Time") && strings.Contains(line, "Gflops") {
			seenHeader = true
			continue
		}
		if !seenHeader {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		n, errN := strconv.ParseFloat(fields[len(fields)-6], 64)
		nb, errNB := strconv.ParseFloat(fields[len(fields)-5], 64)
		p, errP := strconv.ParseFloat(fields[len(fields)-4], 64)
		q, errQ := strconv.ParseFloat(fields[len(fields)-3], 64)
		timeValue, errTime := strconv.ParseFloat(fields[len(fields)-2], 64)
		gflops, errGFLOPS := strconv.ParseFloat(fields[len(fields)-1], 64)
		if errN == nil && errNB == nil && errP == nil && errQ == nil && errTime == nil && errGFLOPS == nil {
			return map[string]float64{
				"n": n, "nb": nb, "p": p, "q": q, "process": p * q,
				"time_seconds": timeValue, "gflops": gflops,
			}, "stdout", nil
		}
	}
	return nil, "", fmt.Errorf("HPL Time/Gflops row not found")
}

func parseHPCG(resultDir string, before map[string]fileSignature) (map[string]float64, string, error) {
	path, err := latestHPCGResult(resultDir, before)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read HPCG result %q: %w", path, err)
	}
	gflopsMatch := hpcgGFLOPS.FindStringSubmatch(string(data))
	timeMatch := hpcgTime.FindStringSubmatch(string(data))
	if len(gflopsMatch) != 2 || len(timeMatch) != 2 {
		return nil, "", fmt.Errorf("HPCG valid GFLOP/s and time not found in %q", path)
	}
	gflops, errGFLOPS := strconv.ParseFloat(gflopsMatch[1], 64)
	timeValue, errTime := strconv.ParseFloat(timeMatch[1], 64)
	if errGFLOPS != nil || errTime != nil {
		return nil, "", fmt.Errorf("HPCG result contains invalid numeric values in %q", path)
	}
	return map[string]float64{"gflops": gflops, "time_seconds": timeValue}, "result_file", nil
}

func snapshotHPCGResults(dir string) (map[string]fileSignature, error) {
	candidates, err := hpcgResultCandidates(dir)
	if err != nil {
		return nil, err
	}
	snapshot := make(map[string]fileSignature, len(candidates))
	for _, candidate := range candidates {
		snapshot[candidate.path] = candidate.signature
	}
	return snapshot, nil
}

func latestHPCGResult(dir string, before map[string]fileSignature) (string, error) {
	candidates, err := hpcgResultCandidates(dir)
	if err != nil {
		return "", err
	}
	changed := candidates[:0]
	for _, candidate := range candidates {
		if previous, existed := before[candidate.path]; existed && previous == candidate.signature {
			continue
		}
		changed = append(changed, candidate)
	}
	if len(changed) == 0 {
		return "", fmt.Errorf("no new or updated HPCG-Benchmark*.txt result found in %q", dir)
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].mod.After(changed[j].mod) })
	return changed[0].path, nil
}

type hpcgResultCandidate struct {
	path      string
	mod       time.Time
	signature fileSignature
}

func hpcgResultCandidates(dir string) ([]hpcgResultCandidate, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("HPCG result directory unavailable: %w", err)
	}
	var candidates []hpcgResultCandidate
	for _, entry := range entries {
		name := entry.Name()
		lowerName := strings.ToLower(name)
		if entry.IsDir() || !strings.HasPrefix(lowerName, "hpcg-benchmark") || !strings.HasSuffix(lowerName, ".txt") {
			continue
		}
		path := filepath.Join(dir, name)
		info, infoErr := entry.Info()
		data, readErr := os.ReadFile(path)
		if infoErr == nil && readErr == nil {
			candidates = append(candidates, hpcgResultCandidate{
				path:      path,
				mod:       info.ModTime(),
				signature: fileSignature{size: info.Size(), modNS: info.ModTime().UnixNano(), digest: sha256.Sum256(data)},
			})
		}
	}
	return candidates, nil
}
