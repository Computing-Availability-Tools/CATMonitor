// Package nvidia_smi provides a data source that wraps the `nvidia-smi` command
// for NVIDIA GPU metrics. It is exec-based (no CGo) and mirrors the
// npu_smi/smartctl/ipmi pattern: singleton, fetcher seam for tests, 5s timeout.
//
// One Query() call runs `nvidia-smi --query-gpu=... --format=csv,noheader,nounits`
// and returns all GPUs' parsed data in one shot (efficient — single exec for
// all metrics, same as the original collector).
package nvidia_smi

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const execTimeout = 5 * time.Second

// GPU holds parsed data for one NVIDIA GPU from a single nvidia-smi query call.
type GPU struct {
	Index       string  // "0", "1", ...
	Utilization float64 // %
	MemUsed     float64 // MB
	MemTotal    float64 // MB
	Temperature float64 // °C
	Power       float64 // W
	FanSpeed    float64 // %
	EccErrors   float64 // count
	ClockFreq   float64 // MHz
}

// Source is the typed interface for the nvidia-smi data source.
type Source interface {
	// Query runs nvidia-smi once and returns parsed data for all GPUs.
	Query() ([]GPU, error)
	// Available reports whether nvidia-smi is on PATH.
	Available() bool
}

type fetcher = func() (string, error)

func realFetch() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=index,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,fan.speed,ecc.errors.uncorrected.volatile.total,clocks.gr",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type defaultSource struct {
	fetch fetcher
}

var defaultSrc = &defaultSource{fetch: realFetch}

func Default() Source { return defaultSrc }

func SetMock(out string) { defaultSrc.fetch = func() (string, error) { return out, nil } }

func ResetFetcher() { defaultSrc.fetch = realFetch }

func (s *defaultSource) Available() bool {
	_, err := exec.LookPath("nvidia-smi")
	return err == nil
}

func (s *defaultSource) Query() ([]GPU, error) {
	out, err := s.fetch()
	if err != nil {
		return nil, err
	}
	return parseOutput(out), nil
}

// parseOutput parses `nvidia-smi --query-gpu=... --format=csv,noheader,nounits`
// output. Each line = one GPU, 9 comma-separated fields.
func parseOutput(out string) []GPU {
	var gpus []GPU
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := parseCSVLine(line)
		if len(fields) < 9 {
			continue
		}
		gpus = append(gpus, GPU{
			Index:       fields[0],
			Utilization: parseFloat(fields[1]),
			MemUsed:     parseFloat(fields[2]),
			MemTotal:    parseFloat(fields[3]),
			Temperature: parseFloat(fields[4]),
			Power:       parseFloat(fields[5]),
			FanSpeed:    parseFloat(fields[6]),
			EccErrors:   parseFloat(fields[7]),
			ClockFreq:   parseFloat(fields[8]),
		})
	}
	return gpus
}

func parseCSVLine(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		result = append(result, strings.TrimSpace(p))
	}
	return result
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}
