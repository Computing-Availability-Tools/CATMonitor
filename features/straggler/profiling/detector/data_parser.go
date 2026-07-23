package detector

import (
	"encoding/csv"
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
// GetCurDetectionInfo — parse parallel topology from group_info JSON files
// ---------------------------------------------------------------------------

// GetCurDetectionInfo reads group_info_*.json files from op_metric/ and
// returns:
//   - parallels: domain name → sorted rank groups (e.g. "tp" → [[0,1],[2,3]])
//   - validRanks: sorted list of all rank IDs present in the data
func GetCurDetectionInfo(jobPath string) (map[string][][]int, []int) {
	metricDir := filepath.Join(jobPath, "op_metric")
	entries, err := os.ReadDir(metricDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] ERROR reading %s: %v\n", metricDir, err)
		return nil, nil
	}

	// Collect all rank IDs from group_info filenames.
	rankSet := make(map[int]bool)
	var jsonPaths []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "group_info_") && strings.HasSuffix(name, ".json") {
			// Extract rank: group_info_3.json → 3
			trimmed := strings.TrimPrefix(name, "group_info_")
			trimmed = strings.TrimSuffix(trimmed, ".json")
			if rank, err := strconv.Atoi(trimmed); err == nil {
				rankSet[rank] = true
			}
			jsonPaths = append(jsonPaths, filepath.Join(metricDir, name))
		}
	}

	if len(rankSet) == 0 {
		return nil, nil
	}

	validRanks := sortedKeys(rankSet)

	// Collect all domain names from all group_info files.
	domainSet := make(map[string]bool)
	for _, jp := range jsonPaths {
		topo := getCurRankTopo(jp)
		for _, v := range topo {
			if m, ok := v.(map[string]interface{}); ok {
				if gn, ok := m[dataFileFieldGroupName].(string); ok && gn != "" {
					domainSet[gn] = true
				}
			}
		}
	}

	// Build parallels map.
	parallels := make(map[string][][]int)
	for domain := range domainSet {
		groups := getDetectionJobParallelInfo(rankSet, jsonPaths, domain)
		// Filter: keep only groups with >1 cards.
		var filtered [][]int
		for _, g := range groups {
			if len(g) > 1 {
				filtered = append(filtered, g)
			}
		}
		if len(filtered) > 0 {
			parallels[domain] = filtered
		}
	}

	return parallels, validRanks
}

// getCurRankTopo reads a single group_info JSON file and returns the raw data.
func getCurRankTopo(filePath string) map[string]interface{} {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	return data
}

// getDetectionJobParallelInfo collects all rank groups for a given domain name
// across all rank topology files, with deduplication.
func getDetectionJobParallelInfo(rankSet map[int]bool, jsonPaths []string, target string) [][]int {
	var groups [][]int
	// parallelInfo tracks which ranks have already been assigned to a group.
	parallelInfo := make(map[int]map[int]bool)

	for _, jp := range jsonPaths {
		topo := getCurRankTopo(jp)
		if topo == nil {
			continue
		}
		for _, v := range topo {
			m, ok := v.(map[string]interface{})
			if !ok {
				continue
			}
			gn, _ := m[dataFileFieldGroupName].(string)
			if gn != target {
				continue
			}
			ranksRaw, ok := m[dataFileFieldGlobalRanks].([]interface{})
			if !ok {
				continue
			}
			npuGroup := make([]int, 0, len(ranksRaw))
			for _, r := range ranksRaw {
				switch n := r.(type) {
				case float64:
					npuGroup = append(npuGroup, int(n))
				case int:
					npuGroup = append(npuGroup, n)
				}
			}
			if len(npuGroup) == 0 {
				continue
			}
			// Dedup: if any rank in this group already appears in another group, skip.
			if checkRankParallelExist(&parallelInfo, npuGroup) {
				continue
			}
			// Mark these ranks.
			for _, rank := range npuGroup {
				if parallelInfo[rank] == nil {
					parallelInfo[rank] = make(map[int]bool)
				}
			}
			sort.Ints(npuGroup)
			groups = append(groups, npuGroup)
		}
	}
	return groups
}

// checkRankParallelExist returns true if any rank in npuGroup already appears
// in the parallelInfo tracking set.
func checkRankParallelExist(parallelInfo *map[int]map[int]bool, npuGroup []int) bool {
	for _, rank := range npuGroup {
		if (*parallelInfo)[rank] != nil && len((*parallelInfo)[rank]) > 0 {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// GetCurJobLastStepData — read CSV files into a single snapshot
// ---------------------------------------------------------------------------

// GetCurJobLastStepData reads global_rank_{N}.csv files and returns a
// unified snapshot map: metric_name → rank_id → value.
//
// When a CSV has more than one data row the second-to-last row is used
// (the last row may be incomplete).
func GetCurJobLastStepData(ranks []int) map[string]map[int]float64 {
	result := make(map[string]map[int]float64)

	for _, rank := range ranks {
		csvPath := filepath.Join(config.FilePath, "op_metric", "global_rank_"+strconv.Itoa(rank)+".csv")
		colData := readCSVDetectionDataAll(csvPath)
		if colData == nil {
			continue
		}

		for colName, values := range colData {
			if colName == stepIndex {
				continue
			}
			if len(values) == 0 {
				continue
			}

			// Second-to-last row, or the only row.
			var val float64
			if len(values) > 1 {
				val = values[len(values)-2]
			} else {
				val = values[0]
			}

			// Skip sentinel.
			if val == -99999.0 {
				continue
			}

			if result[colName] == nil {
				result[colName] = make(map[int]float64)
			}
			result[colName][rank] = val
		}
	}
	return result
}

// readCSVDetectionDataAll reads a CSV file and returns column-oriented data.
func readCSVDetectionDataAll(filePath string) map[string][]float64 {
	f, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil || len(records) < 2 {
		return nil
	}

	header := records[0]
	result := make(map[string][]float64)
	for _, col := range header {
		result[col] = nil
	}

	for _, row := range records[1:] {
		for i, cell := range row {
			if i >= len(header) {
				break
			}
			col := header[i]
			v, err := strconv.ParseFloat(strings.TrimSpace(cell), 64)
			if err != nil {
				result[col] = append(result[col], -99999.0)
			} else {
				result[col] = append(result[col], v)
			}
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sortedKeys(set map[int]bool) []int {
	keys := make([]int, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
