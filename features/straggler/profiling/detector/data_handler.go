package detector

import (
	"sort"

	"github.com/Computing-Availability-Tools/CATMonitor/features/straggler/config"
)

// ---------------------------------------------------------------------------
// Slow compute detection
// ---------------------------------------------------------------------------

// getSlowCalculateRanks detects compute-straggler ranks by testing each group
// of the primary detection domain.
func getSlowCalculateRanks(detectionGroups [][]int, alignedData map[string]map[int]float64, detectionParallel string, localResult config.DegradationData) error {
	for _, npuGroup := range detectionGroups {
		abnormalRanks, degradations := detCalForOneGroup(alignedData, npuGroup)
		for i, rank := range abnormalRanks {
			localResult.AddSingle("cal", rank, degradations[i])
		}
	}
	return nil
}

// detCalForOneGroup runs homogeneous clustering on a single compute group.
// It prefers ZP_Kernel (direction "max"); falls back to ZP_Duration ("min").
func detCalForOneGroup(alignedData map[string]map[int]float64, npuGroup []int) ([]int, []float64) {
	if len(npuGroup) < minRanksInGroup {
		return nil, nil
	}

	// Determine whether ZP_Kernel is available for ALL ranks in this group.
	useKernel := true
	for _, npuID := range npuGroup {
		kernelMap, ok := alignedData[zpKernelColumn]
		if !ok {
			useKernel = false
			break
		}
		val, ok := kernelMap[npuID]
		if !ok || val <= 0 {
			useKernel = false
			break
		}
	}

	var metricName, direction string
	if useKernel {
		metricName = zpKernelColumn
		direction = "max"
	} else {
		metricName = zpDurationColumn
		direction = "min"
	}

	// Build data arrays aligned by rank.
	metricMap, ok := alignedData[metricName]
	if !ok {
		return nil, nil
	}

	ranks := make([]int, 0, len(npuGroup))
	values := make([]float64, 0, len(npuGroup))
	for _, npuID := range npuGroup {
		v, ok := metricMap[npuID]
		if !ok || v == 0 {
			continue
		}
		ranks = append(ranks, npuID)
		values = append(values, v)
	}

	if len(ranks) < minRanksInGroup {
		return nil, nil
	}

	return HomogenizationComparisonFunc(ranks, values, config.CalThreshold, direction)
}

// ---------------------------------------------------------------------------
// NPU Bubble detection
// ---------------------------------------------------------------------------

// detectionZpBubbleData applies a fixed threshold (< 5000 ns) to flag ranks
// with insufficient NPU idle time.
func detectionZpBubbleData(npuData map[int]float64, localResult config.DegradationData) {
	const bubbleThreshold = 5000.0 // 5 µs

	for npuID, value := range npuData {
		if value <= 0 {
			continue
		}
		if value < bubbleThreshold {
			localResult.AddSingle("npu_bubble", npuID, value)
		}
	}
}

// ---------------------------------------------------------------------------
// Slow communication detection
// ---------------------------------------------------------------------------

// detectionAllCommunicationParallel runs communication-domain detection for
// every parallel domain (except pp and embd).
func detectionAllCommunicationParallel(
	parallels map[string][][]int,
	calDetectionGroup [][]int,
	curNpus []int,
	data map[string]map[int]float64,
	localResult config.DegradationData,
) error {
	ppStageNum := 1
	if pp, ok := parallels[ppParallelDomainName]; ok && len(pp) > 0 && len(pp[0]) > 0 {
		ppStageNum = len(pp[0])
	}

	for domainName, domainGroups := range parallels {
		if domainName == ppParallelDomainName || domainName == "embd" {
			continue
		}
		if !checkParallelDomainIsExist(domainGroups) {
			continue
		}
		xpData := getSlowCommunicationDetectionData(domainName, data)
		if len(xpData) == 0 {
			continue
		}

		abnormalGroups, degradations := HomogenizationForSlowCommunication(domainGroups, xpData, config.CommThreshold, ppStageNum)
		for i, group := range abnormalGroups {
			localResult.AddGroup("comm", group, degradations[i])
		}
	}
	return nil
}

// getSlowCommunicationDetectionData extracts {domain}_Duration values from the
// step data snapshot.
func getSlowCommunicationDetectionData(parallelName string, allData map[string]map[int]float64) map[int]float64 {
	colName := parallelName + "_Duration"
	dataMap, ok := allData[colName]
	if !ok {
		return nil
	}
	// Make a copy.
	result := make(map[int]float64, len(dataMap))
	for k, v := range dataMap {
		result[k] = v
	}
	return result
}

// HomogenizationForSlowCommunication applies homogeneous clustering across
// communication domain groups, with PP-stage-aware bucketing.
//
// Steps:
//  1. Sort each sub-group internally and sort groups lexicographically.
//  2. Find the minimum-time card per group (representative).
//  3. Bucket representatives by PP stage.
//  4. Cluster within each PP stage bucket.
//  5. Map detected anomalous representatives back to their full groups.
func HomogenizationForSlowCommunication(
	detectionDomains [][]int,
	detectionData map[int]float64,
	degradationPercent float64,
	ppStageNum int,
) ([][]int, []float64) {
	if len(detectionDomains) == 0 {
		return nil, nil
	}

	// 1. Sort each group internally, then sort groups lexicographically.
	sortedGroups := make([][]int, len(detectionDomains))
	for i, g := range detectionDomains {
		sg := make([]int, len(g))
		copy(sg, g)
		sort.Ints(sg)
		sortedGroups[i] = sg
	}
	sort.Slice(sortedGroups, func(i, j int) bool {
		minLen := len(sortedGroups[i])
		if len(sortedGroups[j]) < minLen {
			minLen = len(sortedGroups[j])
		}
		for k := 0; k < minLen; k++ {
			if sortedGroups[i][k] != sortedGroups[j][k] {
				return sortedGroups[i][k] < sortedGroups[j][k]
			}
		}
		return len(sortedGroups[i]) < len(sortedGroups[j])
	})

	// 2. Find the minimum-time card in each group.
	detectionCards := make([]int, 0, len(sortedGroups))
	rank2Group := make(map[int][]int)
	for _, group := range sortedGroups {
		minCard := -1
		var minVal float64 = 1e18
		for _, card := range group {
			if v, ok := detectionData[card]; ok && v < minVal {
				minVal = v
				minCard = card
			}
		}
		if minCard >= 0 {
			detectionCards = append(detectionCards, minCard)
			rank2Group[minCard] = group
		}
	}

	if len(detectionCards) < 2 {
		return nil, nil
	}

	// 3. Bucket by PP stage.
	if ppStageNum <= 0 {
		ppStageNum = 1
	}
	interval := len(detectionCards) / ppStageNum
	if interval < 1 {
		interval = 1
	}

	cardGroups := make([][]int, 0)
	for i := 0; i < len(detectionCards); i += interval {
		end := i + interval
		if end > len(detectionCards) {
			end = len(detectionCards)
		}
		cardGroups = append(cardGroups, detectionCards[i:end])
	}

	// 4. Cluster each PP stage bucket.
	var abnormalGroups [][]int
	var degradations []float64

	for _, cg := range cardGroups {
		if len(cg) < 2 {
			continue
		}
		vals := make([]float64, len(cg))
		for i, card := range cg {
			if v, ok := detectionData[card]; ok {
				vals[i] = v
			}
		}
		abnormalCards, degs := HomogenizationComparisonFunc(cg, vals, degradationPercent, "max")
		for i, ac := range abnormalCards {
			if fullGroup, ok := rank2Group[ac]; ok {
				abnormalGroups = append(abnormalGroups, fullGroup)
				degradations = append(degradations, degs[i])
			}
		}
	}

	return abnormalGroups, degradations
}

// ---------------------------------------------------------------------------
// Slow CPU detection
// ---------------------------------------------------------------------------

// getSlowHostRanksByHomogenize detects CPU-straggler ranks using ZP_Host data
// preprocessed with 4-card trimmed means.
func getSlowHostRanksByHomogenize(npus []int, detectionData map[int]float64, localResult config.DegradationData) []int {
	var haveDataRanks []int
	var ranksData []float64

	for _, npuID := range npus {
		if v, ok := detectionData[npuID]; ok {
			haveDataRanks = append(haveDataRanks, npuID)
			ranksData = append(ranksData, v)
		}
	}

	if len(haveDataRanks) < minRanksInGroup {
		return nil
	}

	// Preprocess: 4-card trimmed mean.
	processCPUData(ranksData)

	abnormalRanks, degradations := HomogenizationComparisonFunc(haveDataRanks, ranksData, config.CalThreshold, "max")
	for i, rank := range abnormalRanks {
		localResult.AddSingle("cpu", rank, degradations[i])
	}
	return abnormalRanks
}

// processCPUData replaces each 4-card group's values with the trimmed mean
// (discard min and max, average the rest). This smooths intra-machine variance.
func processCPUData(ranksData []float64) {
	const groupSize = 4
	i := 0
	for i < len(ranksData) {
		end := i + groupSize
		if end > len(ranksData) {
			end = len(ranksData)
		}
		group := ranksData[i:end]

		var mean float64
		if len(group) > 2 {
			sorted := make([]float64, len(group))
			copy(sorted, group)
			sort.Float64s(sorted)
			trimmed := sorted[1 : len(sorted)-1]
			var sum float64
			for _, v := range trimmed {
				sum += v
			}
			mean = sum / float64(len(trimmed))
		} else {
			var sum float64
			for _, v := range group {
				sum += v
			}
			mean = sum / float64(len(group))
		}

		for k := i; k < end; k++ {
			ranksData[k] = mean
		}
		i = end
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// checkParallelDomainIsExist validates a parallel domain: all sub-groups must
// have the same size, and at least one group must have >1 card.
func checkParallelDomainIsExist(parallel [][]int) bool {
	if len(parallel) == 0 {
		return false
	}
	size := len(parallel[0])
	hasMulti := size > 1
	for _, g := range parallel[1:] {
		if len(g) != size {
			return false
		}
		if len(g) > 1 {
			hasMulti = true
		}
	}
	return hasMulti
}
