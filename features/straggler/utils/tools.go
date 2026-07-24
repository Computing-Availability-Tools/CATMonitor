// Package utils provides result-writing and utility functions for the
// straggler detection system.
package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Computing-Availability-Tools/CATMonitor/features/straggler/config"
)

// ---------------------------------------------------------------------------
// Result types for JSON output
// ---------------------------------------------------------------------------

// DetectionEntry is a single detection result line in the JSON output.
type DetectionEntry struct {
	DisplayKey  string  `json:"display_key"`
	MetricValue float64 `json:"metric_value"`
	IsAbnormal  bool    `json:"is_abnormal"`
}

// DetectionResult is the top-level JSON structure written to
// straggler_detection_result.json.
type DetectionResult struct {
	Cal       []DetectionEntry `json:"cal"`
	Comm      []DetectionEntry `json:"comm"`
	CPU       []DetectionEntry `json:"cpu"`
	NPUBubble []DetectionEntry `json:"npu_bubble"`
}

// ---------------------------------------------------------------------------
// Write_result — stdout + JSON file
// ---------------------------------------------------------------------------

// Write_result prints detection results to stdout (human-readable) and writes
// straggler_detection_result.json to config.FilePath.
func Write_result(finalResult map[string]map[string]float64, parallels map[string][][]int) {
	dr := DetectionResult{
		Cal:       buildEntries(finalResult["cal"], "cal", parallels),
		Comm:      buildEntries(finalResult["comm"], "comm", parallels),
		CPU:       buildEntries(finalResult["cpu"], "cpu", parallels),
		NPUBubble: buildEntries(finalResult["npu_bubble"], "npu_bubble", parallels),
	}

	// Print to stdout
	printStdout(dr)

	// Write JSON file
	outPath := filepath.Join(config.FilePath, "straggler_detection_result.json")
	data, err := json.MarshalIndent(dr, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Failed to marshal JSON: %v\n", err)
		return
	}
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] Failed to write result file: %v\n", err)
		return
	}
	fmt.Printf("[SLOWNODE ALGO] Result written to %s\n", outPath)
}

// ---------------------------------------------------------------------------
// Entry builders
// ---------------------------------------------------------------------------

type kv struct {
	key   string
	value float64
}

// buildEntries converts a DegradationData inner map to a sorted slice of
// DetectionEntry values.
//
// Sorting rules:
//   - npu_bubble: ascending  (smaller value = worse bubble)
//   - all others: descending (larger value = worse degradation)
func buildEntries(data map[string]float64, category string, parallels map[string][][]int) []DetectionEntry {
	if len(data) == 0 {
		return nil
	}

	kvs := make([]kv, 0, len(data))
	for k, v := range data {
		kvs = append(kvs, kv{key: k, value: v})
	}

	ascending := category == "npu_bubble"
	sort.Slice(kvs, func(i, j int) bool {
		if ascending {
			return kvs[i].value < kvs[j].value
		}
		return kvs[i].value > kvs[j].value
	})

	entries := make([]DetectionEntry, 0, len(kvs))
	for _, item := range kvs {
		dk := buildDisplayKey(item.key, category, parallels)
		entries = append(entries, DetectionEntry{
			DisplayKey:  dk,
			MetricValue: item.value,
			IsAbnormal:  true,
		})
	}
	return entries
}

// buildDisplayKey converts an internal key into a human-readable display key.
//
//   - For comm: "0,1,2,3" → "tp[0, 1, 2, 3]"
//   - For others: "5" → "5"
func buildDisplayKey(rawKey, category string, parallels map[string][][]int) string {
	if category != "comm" {
		return rawKey
	}
	ranks := stringToRanks(rawKey)
	if len(ranks) == 0 {
		return rawKey
	}
	domainName := findDomainForRanks(ranks, parallels)
	if domainName == "" {
		// Fallback: use the raw key.
		return "[" + rawKey + "]"
	}
	parts := make([]string, len(ranks))
	for i, r := range ranks {
		parts[i] = strconv.Itoa(r)
	}
	return domainName + "[" + strings.Join(parts, ", ") + "]"
}

// findDomainForRanks finds which parallel domain a sorted rank set belongs to.
func findDomainForRanks(ranks []int, parallels map[string][][]int) string {
	for domain, groups := range parallels {
		for _, group := range groups {
			if intSlicesEqual(sortInts(group), ranks) {
				return domain
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Print helpers
// ---------------------------------------------------------------------------

func printStdout(dr DetectionResult) {
	printCategory("慢计算 (cal)", dr.Cal)
	printCategory("慢通信 (comm)", dr.Comm)
	printCategory("慢CPU (cpu)", dr.CPU)
	printCategory("NPU Bubble (npu_bubble)", dr.NPUBubble)
}

func printCategory(label string, entries []DetectionEntry) {
	if len(entries) == 0 {
		fmt.Printf("%s: 无异常\n", label)
		return
	}
	fmt.Printf("%s: 发现 %d 个异常\n", label, len(entries))
	for _, e := range entries {
		fmt.Printf("  %-30s %8.2f\n", e.DisplayKey, e.MetricValue)
	}
}

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

// CheckFileOrDirectoryReadMode returns true if the path exists and is readable.
func CheckFileOrDirectoryReadMode(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	_ = info
	return true
}

// CheckFileOrDirectoryIsSoftLink returns true if path is a symbolic link.
func CheckFileOrDirectoryIsSoftLink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

// TransferFloatArrayToInt converts []interface{} containing float64 values
// (typical of JSON unmarshalling) to []int.
func TransferFloatArrayToInt(ids []interface{}) []int {
	result := make([]int, 0, len(ids))
	for _, v := range ids {
		switch n := v.(type) {
		case float64:
			result = append(result, int(n))
		case int:
			result = append(result, n)
		}
	}
	return result
}

// ReadFile reads an entire file and returns its content.
func ReadFile(filePath string) ([]byte, error) {
	return os.ReadFile(filePath)
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

// stringToRanks parses a comma-separated rank key (e.g. "0,2,4") into []int.
func stringToRanks(s string) []int {
	parts := strings.Split(s, ",")
	ranks := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		ranks = append(ranks, n)
	}
	return ranks
}

func sortInts(a []int) []int {
	sorted := make([]int, len(a))
	copy(sorted, a)
	sort.Ints(sorted)
	return sorted
}

func intSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
