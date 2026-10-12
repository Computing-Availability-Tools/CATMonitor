//go:build linux

package workloadplugin

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var idleNPU = regexp.MustCompile(`^\|\s*No running processes found in NPU ([0-9]+)\s*\|$`)
var inventoryNPU = regexp.MustCompile(`^\|\s*([0-9]+)\s+(?:Ascend)?[0-9][A-Za-z0-9_-]*\s*\|`)

// Conservative node-wide check. npu-smi card/chip IDs are NOT interpreted as
// torch indices: any process, unknown table, or incomplete inventory rejects.
// This is a preflight snapshot, not a reservation against external schedulers.
func checkNPUIdle(ctx context.Context, mappedCount int) error {
	if env("CATMONITOR_NPU_REQUIRE_IDLE") != "1" {
		return nil
	}
	out, err := exec.CommandContext(ctx, envDefault("NPU_SMI_EXECUTABLE", "/usr/local/bin/npu-smi"), "info").CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot verify idle NPU state: %w", err)
	}
	return parseNPUIdle(string(out), mappedCount)
}

func parseNPUIdle(output string, mappedCount int) error {
	start := strings.Index(output, "Process id")
	if start < 0 {
		return fmt.Errorf("NPU process inventory is unverified: no process table")
	}
	// Card IDs are used only to prove table completeness, never to select
	// logical runtime devices. One card may contain several NPU dies.
	cards := map[string]bool{}
	for _, line := range strings.Split(output[:start], "\n") {
		if match := inventoryNPU.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			cards[match[1]] = true
		}
	}
	lines := strings.Split(output[start:], "\n")
	seen := map[string]bool{}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "+") {
			continue
		}
		if match := idleNPU.FindStringSubmatch(line); match != nil {
			if seen[match[1]] {
				return fmt.Errorf("duplicate NPU process inventory row")
			}
			seen[match[1]] = true
			continue
		}
		return fmt.Errorf("NPU busy or process inventory unverified; no workload started (node-wide idle policy)")
	}
	if mappedCount <= 0 || len(cards) == 0 || len(seen) != len(cards) {
		return fmt.Errorf("NPU idle inventory is incomplete; no workload started")
	}
	for id := range cards {
		if !seen[id] {
			return fmt.Errorf("NPU idle inventory is incomplete; no workload started")
		}
	}
	return nil
}
