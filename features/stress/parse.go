package stress

// Benchmark output and result-file parsing.
import (
	"crypto/sha256"
	"fmt"
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
)

type fileSignature struct {
	size   int64
	modNS  int64
	digest [sha256.Size]byte
}

func parseBenchmark(name, output, resultDir string, hpcgBefore map[string]fileSignature) (map[string]float64, string, error) {
	switch name {
	case "stream":
		return parseStream(output)
	case "hpl":
		return parseHPL(output)
	case "hpcg":
		return parseHPCG(output, resultDir, hpcgBefore)
	default:
		return nil, "", fmt.Errorf("unsupported benchmark %q", name)
	}
}

func parseStream(output string) (map[string]float64, string, error) {
	values := make(map[string]float64)
	for _, match := range streamLine.FindAllStringSubmatch(output, -1) {
		v, _ := strconv.ParseFloat(match[2], 64)
		values[strings.ToLower(match[1])+"_mb_s"] = v
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
		timeValue, err1 := strconv.ParseFloat(fields[len(fields)-2], 64)
		gflops, err2 := strconv.ParseFloat(fields[len(fields)-1], 64)
		if errN == nil && errNB == nil && errP == nil && errQ == nil && err1 == nil && err2 == nil {
			return map[string]float64{
				"n":            n,
				"nb":           nb,
				"p":            p,
				"q":            q,
				"process":      p * q,
				"time_seconds": timeValue,
				"gflops":       gflops,
			}, "stdout", nil
		}
	}
	return nil, "", fmt.Errorf("HPL Time/Gflops row not found")
}

func parseHPCG(_ string, resultDir string, before map[string]fileSignature) (map[string]float64, string, error) {
	path, err := latestHPCGResult(resultDir, before)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read HPCG result %q: %w", path, err)
	}
	if values, ok := parseHPCGText(string(data)); ok {
		return values, "result_file", nil
	}
	return nil, "", fmt.Errorf("HPCG valid GFLOP/s and time not found in %q", path)
}

func parseHPCGText(text string) (map[string]float64, bool) {
	gflopsMatch := hpcgGFLOPS.FindStringSubmatch(text)
	timeMatch := hpcgTime.FindStringSubmatch(text)
	if len(gflopsMatch) != 2 || len(timeMatch) != 2 {
		return nil, false
	}
	gflops, err1 := strconv.ParseFloat(gflopsMatch[1], 64)
	timeValue, err2 := strconv.ParseFloat(timeMatch[1], 64)
	if err1 != nil || err2 != nil {
		return nil, false
	}
	return map[string]float64{"gflops": gflops, "time_seconds": timeValue}, true
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
		if entry.IsDir() || !strings.HasPrefix(lowerName, "hpcg-benchmark") ||
			!strings.HasSuffix(lowerName, ".txt") {
			continue
		}
		path := filepath.Join(dir, name)
		info, infoErr := entry.Info()
		data, readErr := os.ReadFile(path)
		if infoErr == nil && readErr == nil {
			candidates = append(candidates, hpcgResultCandidate{
				path: path,
				mod:  info.ModTime(),
				signature: fileSignature{
					size:   info.Size(),
					modNS:  info.ModTime().UnixNano(),
					digest: sha256.Sum256(data),
				},
			})
		}
	}
	return candidates, nil
}
