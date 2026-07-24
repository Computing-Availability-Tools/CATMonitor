package resource

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// =============================================================================
// CSV Parsing
// =============================================================================

// ParseCSV reads a KPI CSV file and returns the parsed time-series data.
//
// The CSV format is:
//   timestamp,NPU_CARD_POWER,NPU_CARD_TEMP,...,CPU_average
//   1784547926,"{""0"":1628,...}","{""0"":47,...}",...,"{""cpu1"":""4.26"",...}"
func ParseCSV(filePath string) (*TimeSeriesData, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot open CSV %s: %w", filePath, err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("cannot read CSV %s: %w", filePath, err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("CSV %s has no data rows (only header)", filePath)
	}

	// Header row: identify column indices by name.
	header := records[0]
	colIdx, err := buildColumnIndex(header)
	if err != nil {
		return nil, fmt.Errorf("CSV %s: %w", filePath, err)
	}

	// Parse data rows.
	rows := make([]CSVRow, 0, len(records)-1)
	cardIDSet := make(map[int]bool)

	for i := 1; i < len(records); i++ {
		rec := records[i]
		row, err := parseRow(rec, colIdx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] [WARN] skipping row %d in %s: %v\n", i, filePath, err)
			continue
		}
		rows = append(rows, row)

		// Collect all card IDs.
		for cid := range row.Power {
			cardIDSet[cid] = true
		}
		for cid := range row.Temp {
			cardIDSet[cid] = true
		}
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("CSV %s: no valid data rows parsed", filePath)
	}

	// Sort rows by timestamp.
	sort.Slice(rows, func(i, j int) bool { return rows[i].Timestamp < rows[j].Timestamp })

	// Build sorted card ID list.
	cardIDs := make([]int, 0, len(cardIDSet))
	for cid := range cardIDSet {
		cardIDs = append(cardIDs, cid)
	}
	sort.Ints(cardIDs)

	return &TimeSeriesData{
		Rows:    rows,
		RawRows: rows, // keep raw for counter metric processing
		CardIDs: cardIDs,
	}, nil
}

// =============================================================================
// Column Index Mapping
// =============================================================================

type columnIndex struct {
	timestamp     int
	power         int
	temp          int
	aicoreFreq    int
	aicoreUtil    int
	hbmUtil       int
	txBandwidth   int
	rxPfcPkt      int
	roceTxErrPkt  int
	roceOutOfOrder int
	roceNewPktRty int
	nicRxAllPkg   int
	cpuAvg        int
}

func buildColumnIndex(header []string) (columnIndex, error) {
	ci := columnIndex{
		timestamp:     -1,
		power:         -1,
		temp:          -1,
		aicoreFreq:    -1,
		aicoreUtil:    -1,
		hbmUtil:       -1,
		txBandwidth:   -1,
		rxPfcPkt:      -1,
		roceTxErrPkt:  -1,
		roceOutOfOrder: -1,
		roceNewPktRty: -1,
		nicRxAllPkg:   -1,
		cpuAvg:        -1,
	}

	nameToIdx := map[string]*int{
		"timestamp":              &ci.timestamp,
		"NPU_CARD_POWER":         &ci.power,
		"NPU_CARD_TEMP":          &ci.temp,
		"NPU_CARD_AICORE_FREQ":   &ci.aicoreFreq,
		"NPU_CARD_AICORE_UTIL":   &ci.aicoreUtil,
		"NPU_CARD_HBM_UTIL":      &ci.hbmUtil,
		"NPU_TX_BANDWIDTH":       &ci.txBandwidth,
		"NPU_RX_PFC_PKT":         &ci.rxPfcPkt,
		"NPU_ROCE_TX_ERR_PKT":    &ci.roceTxErrPkt,
		"NPU_ROCE_OUT_OF_ORDER":  &ci.roceOutOfOrder,
		"NPU_ROCE_NEW_PKT_RTY":   &ci.roceNewPktRty,
		"NPU_NIC_RX_ALL_PKG":     &ci.nicRxAllPkg,
		"CPU_average":            &ci.cpuAvg,
	}

	for i, h := range header {
		h = strings.TrimSpace(h)
		if ptr, ok := nameToIdx[h]; ok {
			*ptr = i
		}
	}

	// Only timestamp is strictly required; metrics use column-order fallback.
	if ci.timestamp < 0 {
		return ci, fmt.Errorf("missing required column: timestamp")
	}

	// Warn about missing metric columns (non-fatal).
	for name, ptr := range nameToIdx {
		if name != "timestamp" && *ptr < 0 {
			fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] [WARN] column '%s' not found in CSV header, will be empty\n", name)
		}
	}

	return ci, nil
}

// =============================================================================
// Row Parsing
// =============================================================================

func parseRow(rec []string, ci columnIndex) (CSVRow, error) {
	row := CSVRow{}

	// Timestamp.
	if ci.timestamp >= len(rec) || rec[ci.timestamp] == "" {
		return row, fmt.Errorf("missing timestamp")
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(rec[ci.timestamp]), 10, 64)
	if err != nil {
		return row, fmt.Errorf("invalid timestamp %q: %w", rec[ci.timestamp], err)
	}
	row.Timestamp = ts

	// Parse each metric column as JSON dict.
	row.Power = parseMetricJSON(rec, ci.power, "NPU_CARD_POWER")
	row.Temp = parseMetricJSON(rec, ci.temp, "NPU_CARD_TEMP")
	row.AICoreFreq = parseMetricJSON(rec, ci.aicoreFreq, "NPU_CARD_AICORE_FREQ")
	row.AICoreUtil = parseMetricJSON(rec, ci.aicoreUtil, "NPU_CARD_AICORE_UTIL")
	row.HBMUtil = parseMetricJSON(rec, ci.hbmUtil, "NPU_CARD_HBM_UTIL")
	row.TXBandwidth = parseMetricJSON(rec, ci.txBandwidth, "NPU_TX_BANDWIDTH")
	row.RXPfcPkt = parseMetricJSON(rec, ci.rxPfcPkt, "NPU_RX_PFC_PKT")
	row.RocETxErrPkt = parseMetricJSON(rec, ci.roceTxErrPkt, "NPU_ROCE_TX_ERR_PKT")
	row.RocEOutOfOrder = parseMetricJSON(rec, ci.roceOutOfOrder, "NPU_ROCE_OUT_OF_ORDER")
	row.RocENewPktRty = parseMetricJSON(rec, ci.roceNewPktRty, "NPU_ROCE_NEW_PKT_RTY")
	row.NICRxAllPkg = parseMetricJSON(rec, ci.nicRxAllPkg, "NPU_NIC_RX_ALL_PKG")

	// CPU_average has string values (e.g. "4.26") so parse separately.
	row.CPUAvg = parseCPUJSON(rec, ci.cpuAvg)

	return row, nil
}

// parseMetricJSON parses a JSON dict like {"0":1628,"1":1747,...} → map[int]float64.
func parseMetricJSON(rec []string, idx int, name string) map[int]float64 {
	if idx < 0 || idx >= len(rec) || rec[idx] == "" {
		return nil
	}
	raw := rec[idx]
	// The CSV quoting may double-quote the JSON. Un-double if needed.
	// e.g. "{""0"":1628}" → {"0":1628}
	raw = strings.ReplaceAll(raw, `""`, `"`)

	var rawMap map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &rawMap); err != nil {
		fmt.Fprintf(os.Stderr, "[SLOWNODE ALGO] [WARN] cannot parse %s JSON: %v (raw: %.80s...)\n", name, err, raw)
		return nil
	}

	result := make(map[int]float64, len(rawMap))
	for k, v := range rawMap {
		cardID, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		switch n := v.(type) {
		case float64:
			result[cardID] = n
		case string:
			if f, err := strconv.ParseFloat(n, 64); err == nil {
				result[cardID] = f
			}
		}
	}
	return result
}

// parseCPUJSON parses CPU_average column: {"cpu1":"4.26","cpu2":"3.41",...}.
func parseCPUJSON(rec []string, idx int) map[string]string {
	if idx < 0 || idx >= len(rec) || rec[idx] == "" {
		return nil
	}
	raw := rec[idx]
	raw = strings.ReplaceAll(raw, `""`, `"`)

	var rawMap map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &rawMap); err != nil {
		return nil
	}

	result := make(map[string]string, len(rawMap))
	for k, v := range rawMap {
		switch s := v.(type) {
		case string:
			result[k] = s
		case float64:
			result[k] = strconv.FormatFloat(s, 'f', -1, 64)
		}
	}
	return result
}

// =============================================================================
// Metric Value Accessors
// =============================================================================

// getMetricValues returns the values for a given metric from a CSVRow for the specified cards.
func getMetricValues(row CSVRow, metric MetricName, cardIDs []int) []float64 {
	dict := getMetricDict(row, metric)
	if dict == nil {
		return nil
	}
	vals := make([]float64, len(cardIDs))
	for i, cid := range cardIDs {
		if v, ok := dict[cid]; ok {
			vals[i] = v
		} else {
			vals[i] = 0 // missing = 0 (will be filtered by caller)
		}
	}
	return vals
}

// getMetricDict returns the raw map for a metric from a row.
func getMetricDict(row CSVRow, metric MetricName) map[int]float64 {
	switch metric {
	case MetricTemp:
		return row.Temp
	case MetricPower:
		return row.Power
	case MetricAICoreFreq:
		return row.AICoreFreq
	case MetricAICoreUtil:
		return row.AICoreUtil
	case MetricHBMUtil:
		return row.HBMUtil
	case MetricTXBandwidth:
		return row.TXBandwidth
	case MetricRXPfcPkt:
		return row.RXPfcPkt
	case MetricRocETxErrPkt:
		return row.RocETxErrPkt
	case MetricRocEOutOfOrder:
		return row.RocEOutOfOrder
	case MetricRocENewPktRty:
		return row.RocENewPktRty
	default:
		return nil
	}
}

// setMetricDict sets the value for a metric in a row (used by aggregator).
func setMetricDict(row *CSVRow, metric MetricName, vals map[int]float64) {
	switch metric {
	case MetricTemp:
		row.Temp = vals
	case MetricPower:
		row.Power = vals
	case MetricAICoreFreq:
		row.AICoreFreq = vals
	case MetricAICoreUtil:
		row.AICoreUtil = vals
	case MetricHBMUtil:
		row.HBMUtil = vals
	case MetricTXBandwidth:
		row.TXBandwidth = vals
	case MetricRXPfcPkt:
		row.RXPfcPkt = vals
	case MetricRocETxErrPkt:
		row.RocETxErrPkt = vals
	case MetricRocEOutOfOrder:
		row.RocEOutOfOrder = vals
	case MetricRocENewPktRty:
		row.RocENewPktRty = vals
	case MetricNICRxAllPkg:
		row.NICRxAllPkg = vals
	}
}

// getMetricDictFromRaw is like getMetricDict but uses RawRows for counter metric processing.
// MetricNICRxAllPkg is added for completeness.
const MetricNICRxAllPkg MetricName = "nic_rx_all_pkg"
