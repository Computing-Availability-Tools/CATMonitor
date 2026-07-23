// Package spacedetector implements the homogeneous clustering algorithm — a
// recursive 1-D clustering detector that finds abnormal data points by
// locating the largest gap in a sorted list of values.
//
// It is the single anomaly-detection primitive used by all four detection
// categories (slow compute, slow communication, slow CPU, NPU bubble).
package detector

import (
	"math"
	"sort"
)

// IndexAndValue ties a data value to its original index in the input slice.
type IndexAndValue struct {
	Index int
	Value float64
}

// HomogenizationComparisonFunc is the public entry point.
//
// Parameters:
//   - fileRanks:           original rank IDs (e.g. [1, 5, 9, 13])
//   - alignedData:         data values, one per rank
//   - degradationPercent:  threshold (e.g. 1.3 for compute, 2.5 for communication)
//   - abnormalType:        "max" (bigger is worse) or "min" (smaller is worse)
//
// Returns:
//   - abnormal rank IDs
//   - corresponding degradation scores (value / baseline for "max", baseline / value for "min")
func HomogenizationComparisonFunc(
	fileRanks []int,
	alignedData []float64,
	degradationPercent float64,
	abnormalType string,
) ([]int, []float64) {
	if len(alignedData) == 0 || len(fileRanks) == 0 || len(alignedData) != len(fileRanks) {
		return nil, nil
	}

	// 1. Find baseline value.
	var baseVal float64
	switch abnormalType {
	case "max":
		baseVal = min(alignedData)
	case "min":
		baseVal = max(alignedData)
	default:
		baseVal = min(alignedData)
	}
	if baseVal == 0 {
		baseVal = math.SmallestNonzeroFloat64
	}

	// 2. Recursively cluster until no further split is possible.
	abnormalIndices := recurseDimensionalClusteringWithDegradation(alignedData, degradationPercent, abnormalType)
	if len(abnormalIndices) == 0 {
		return nil, nil
	}

	// 3. Map abnormal indices back to rank IDs and compute degradation scores.
	abnormalRanks := make([]int, 0, len(abnormalIndices))
	degradations := make([]float64, 0, len(abnormalIndices))
	for _, idx := range abnormalIndices {
		if idx < 0 || idx >= len(fileRanks) {
			continue
		}
		value := alignedData[idx]
		var deg float64
		if abnormalType == "max" {
			deg = value / baseVal
		} else {
			deg = baseVal / value
		}
		abnormalRanks = append(abnormalRanks, fileRanks[idx])
		degradations = append(degradations, deg)
	}
	return abnormalRanks, degradations
}

// recurseDimensionalClusteringWithDegradation repeatedly applies one-dimensional
// clustering, mapping local indices back to the original dataList on each
// iteration until no further split can be made.
func recurseDimensionalClusteringWithDegradation(
	dataList []float64,
	degradationPercent float64,
	abnormalType string,
) []int {
	if len(dataList) < 2 {
		return nil
	}

	var result []int
	input := dataList

	for {
		tmpResult, nextList := oneDimensionalClustering(input, degradationPercent, abnormalType)
		if len(tmpResult) == 0 {
			break
		}

		input = nextList

		if result == nil {
			// First round: store the local indices of the abnormal group.
			result = tmpResult
		} else {
			// Subsequent rounds: map local indices back through the previous result.
			var newResult []int
			for _, localIdx := range tmpResult {
				if localIdx >= 0 && localIdx < len(result) {
					origIdx := result[localIdx]
					if origIdx >= 0 && origIdx < len(dataList) {
						newResult = append(newResult, origIdx)
					}
				}
			}
			result = newResult
		}
	}
	return result
}

// oneDimensionalClustering performs a single round of clustering.
//
// Steps:
//  1. Sort by value (preserving original indices).
//  2. Compute adjacent differences.
//  3. Find the largest gap.
//  4. Check gap condition 1: maxDiff >= totalDiffSum / 2.
//  5. Check gap condition 2: bigMean / littleMean >= degradationPercent.
//  6. Return the abnormal side according to abnormalType.
func oneDimensionalClustering(
	dataList []float64,
	degradationPercent float64,
	abnormalType string,
) ([]int, []float64) {
	if len(dataList) < 2 {
		return nil, nil
	}

	// 1. Build sorted index-value pairs.
	sorted := sortDataByIndexAndValue(dataList)

	// 2. Compute adjacent differences.
	diffs, totalDiff := calculateDifferences(sorted)
	if len(diffs) == 0 {
		return nil, nil
	}

	// 3. Find the index of the maximum difference.
	maxDiffIdx := 0
	for i := 1; i < len(diffs); i++ {
		if diffs[i] > diffs[maxDiffIdx] {
			maxDiffIdx = i
		}
	}

	// 4. Condition 1: max gap must be at least half of the total sum of gaps.
	if totalDiff == 0 || diffs[maxDiffIdx] < totalDiff/2.0 {
		return nil, nil
	}

	// 5. Condition 2: ratio of means must meet the degradation threshold.
	//    After maxDiffIdx, indices [0..maxDiffIdx] are the "little" group,
	//    indices [maxDiffIdx+1..] are the "big" group.
	littleGroup := sorted[:maxDiffIdx+1]
	bigGroup := sorted[maxDiffIdx+1:]

	if len(littleGroup) == 0 || len(bigGroup) == 0 {
		return nil, nil
	}

	littleMean := calculateMean(littleGroup)
	bigMean := calculateMean(bigGroup)

	if littleMean == 0 {
		return nil, nil
	}
	if bigMean/littleMean < degradationPercent {
		return nil, nil
	}

	// 6. Return the abnormal side.
	if abnormalType == "max" {
		// Larger values are abnormal.
		indices, values := collectIndices(bigGroup)
		return indices, values
	}
	// Smaller values are abnormal.
	indices, values := collectIndices(littleGroup)
	return indices, values
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sortDataByIndexAndValue(dataList []float64) []IndexAndValue {
	ivs := make([]IndexAndValue, len(dataList))
	for i, v := range dataList {
		ivs[i] = IndexAndValue{Index: i, Value: v}
	}
	sort.Slice(ivs, func(i, j int) bool { return ivs[i].Value < ivs[j].Value })
	return ivs
}

func calculateDifferences(ivs []IndexAndValue) ([]float64, float64) {
	if len(ivs) < 2 {
		return nil, 0
	}
	diffs := make([]float64, len(ivs)-1)
	var total float64
	for i := 0; i < len(ivs)-1; i++ {
		d := ivs[i+1].Value - ivs[i].Value
		diffs[i] = d
		total += d
	}
	return diffs, total
}

func calculateMean(ivs []IndexAndValue) float64 {
	if len(ivs) == 0 {
		return 0
	}
	var sum float64
	for _, iv := range ivs {
		sum += iv.Value
	}
	return sum / float64(len(ivs))
}

func collectIndices(ivs []IndexAndValue) ([]int, []float64) {
	indices := make([]int, len(ivs))
	values := make([]float64, len(ivs))
	for i, iv := range ivs {
		indices[i] = iv.Index
		values[i] = iv.Value
	}
	return indices, values
}

func min(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func max(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
