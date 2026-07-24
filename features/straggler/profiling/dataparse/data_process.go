package dataparse

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Concurrency control
// ---------------------------------------------------------------------------

var (
	csvMutex sync.Mutex

	fileWriteOnce   = make(map[string]*sync.Once)
	fileWriteOnceMu sync.Mutex
)

// ---------------------------------------------------------------------------
// ProcessDatabase — per-file pipeline
// ---------------------------------------------------------------------------

// ProcessDatabase opens an SQLite database, extracts profiling data, computes
// metrics, and writes a single-line CSV to op_metric/.
func ProcessDatabase(dbFilePath string, outputDir string) error {
	db, err := sql.Open("sqlite", dbFilePath+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open db %s: %w", dbFilePath, err)
	}
	defer db.Close()

	// WAL mode
	db.Exec("PRAGMA journal_mode=WAL")

	// Create indices (idempotent).
	for _, idx := range []string{
		"CREATE INDEX IF NOT EXISTS idx_string_ids_value ON STRING_IDS(value)",
		"CREATE INDEX IF NOT EXISTS idx_device_op_time ON COMMUNICATION_OP(startNs, endNs)",
		"CREATE INDEX IF NOT EXISTS idx_task_time_type ON TASK(startNs, endNs, taskType)",
	} {
		if _, err := db.Exec(idx); err != nil {
			// Index creation failures are non-fatal (table may not exist yet).
			_ = err
		}
	}

	// Extract global rank from filename.
	rankStr, err := extractGlobalRankFromFilename(dbFilePath)
	if err != nil {
		return err
	}

	// Read parallel group info from META_DATA.
	groupInfo, xpToGroupName, err := readGroupInfo(db, rankStr, outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[DATA PROCESS] WARNING: %v\n", err)
	}

	// Get all step time ranges.
	steps, err := GetAllStepTimes(db)
	if err != nil || len(steps) == 0 {
		return fmt.Errorf("no step data in %s", dbFilePath)
	}

	// Merge all steps into one.
	minStart := math.MaxInt
	maxEnd := math.MinInt
	for _, s := range steps {
		if s.StartNs < minStart {
			minStart = s.StartNs
		}
		if s.EndNs > maxEnd {
			maxEnd = s.EndNs
		}
	}
	mergedStep := StepTime{ID: 0, StartNs: minStart, EndNs: maxEnd}

	// Compute metrics.
	metrics, err := TimeDiffForStep(db, xpToGroupName, mergedStep)
	if err != nil {
		return fmt.Errorf("metric calculation for rank %s: %w", rankStr, err)
	}
	metrics.StepIndex = mergedStep.ID
	metrics.StepDuration = mergedStep.EndNs - mergedStep.StartNs

	// Write CSV.
	csvPath := filepath.Join(outputDir, "op_metric", "global_rank_"+rankStr+".csv")
	if err := WriteResultsToCSV(csvPath, []PerformanceMetrics{metrics}); err != nil {
		return fmt.Errorf("write CSV %s: %w", csvPath, err)
	}

	_ = groupInfo
	return nil
}

// ---------------------------------------------------------------------------
// readGroupInfo
// ---------------------------------------------------------------------------

func readGroupInfo(db *sql.DB, rankStr, outputDir string) (map[string]interface{}, map[string]string, error) {
	row := db.QueryRow("SELECT value FROM META_DATA WHERE name = 'parallel_group_info'")
	var raw string
	if err := row.Scan(&raw); err != nil {
		return nil, nil, fmt.Errorf("parallel_group_info not found for rank %s: %w", rankStr, err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, nil, fmt.Errorf("parse parallel_group_info JSON: %w", err)
	}

	// Write group_info_{N}.json exactly once per unique filename.
	outDir := filepath.Join(outputDir, "op_metric")
	os.MkdirAll(outDir, 0755)
	jsonPath := filepath.Join(outDir, "group_info_"+rankStr+".json")

	fileWriteOnceMu.Lock()
	if fileWriteOnce[jsonPath] == nil {
		fileWriteOnce[jsonPath] = &sync.Once{}
	}
	once := fileWriteOnce[jsonPath]
	fileWriteOnceMu.Unlock()

	once.Do(func() {
		pretty, _ := json.MarshalIndent(data, "", "  ")
		os.WriteFile(jsonPath, pretty, 0644)
	})

	// Build xpToGroupName: smallerIndexName → original key (e.g. "tp" → "group_name_42")
	xpToGroupName := make(map[string]string)
	for key, val := range data {
		if m, ok := val.(map[string]interface{}); ok {
			if gn, ok := m["group_name"].(string); ok && gn != "" {
				xpToGroupName[gn] = key
			}
		}
	}
	return data, xpToGroupName, nil
}

// ---------------------------------------------------------------------------
// GetAllStepTimes — 3-level fallback
// ---------------------------------------------------------------------------

// GetAllStepTimes tries STEP_TIME first, then TASK+STRING_IDS+MSTX_EVENTS,
// finally a sentinel step spanning the entire time range.
func GetAllStepTimes(db *sql.DB) ([]StepTime, error) {
	exists, _ := tableExists(db, "STEP_TIME")
	if exists {
		steps, err := GetStepTimesFromSTEP_TIME(db)
		if err == nil && len(steps) > 0 {
			return steps, nil
		}
	}

	exists, _ = tableExists(db, "TASK")
	if exists {
		steps, err := GetStepTimesFromTASK(db)
		if err == nil && len(steps) > 0 {
			return steps, nil
		}
	}

	// Sentinel fallback.
	return []StepTime{{ID: -1, StartNs: math.MinInt, EndNs: math.MaxInt}}, nil
}

// GetStepTimesFromSTEP_TIME reads all step entries in descending ID order
// and reverses them to ascending.
func GetStepTimesFromSTEP_TIME(db *sql.DB) ([]StepTime, error) {
	rows, err := db.Query("SELECT id, startNs, endNs FROM STEP_TIME ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []StepTime
	for rows.Next() {
		var s StepTime
		if err := rows.Scan(&s.ID, &s.StartNs, &s.EndNs); err != nil {
			return nil, err
		}
		steps = append(steps, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse to ascending order.
	for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
		steps[i], steps[j] = steps[j], steps[i]
	}
	return steps, nil
}

var stepPattern = regexp.MustCompile(`^step\s+\d+$`)

// GetStepTimesFromTASK extracts step boundaries from TASK + STRING_IDS + MSTX_EVENTS.
func GetStepTimesFromTASK(db *sql.DB) ([]StepTime, error) {
	// Find all STRING_IDS entries matching "step N".
	sRows, err := db.Query("SELECT id, value FROM STRING_IDS")
	if err != nil {
		return nil, err
	}
	defer sRows.Close()

	type stepMsg struct{ sid, connID int }
	var msgs []stepMsg
	for sRows.Next() {
		var id int
		var val string
		if err := sRows.Scan(&id, &val); err != nil {
			continue
		}
		if !stepPattern.MatchString(strings.ToLower(val)) {
			continue
		}
		// Look up in MSTX_EVENTS.
		mRow := db.QueryRow("SELECT connectionId FROM MSTX_EVENTS WHERE message = ?", id)
		var cid int
		if err := mRow.Scan(&cid); err != nil {
			continue
		}
		msgs = append(msgs, stepMsg{sid: id, connID: cid})
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("no step markers found in TASK fallback")
	}

	var steps []StepTime
	for i, m := range msgs {
		tRow := db.QueryRow("SELECT startNs, endNs FROM TASK WHERE connectionId = ?", m.connID)
		var s StepTime
		s.ID = i
		if err := tRow.Scan(&s.StartNs, &s.EndNs); err != nil {
			continue
		}
		steps = append(steps, s)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("no step times from TASK")
	}
	return steps, nil
}

// ---------------------------------------------------------------------------
// TimeDiffForStep — main metric computation
// ---------------------------------------------------------------------------

// TimeDiffForStep extracts all performance metrics for a single (merged) step
// from the database.
func TimeDiffForStep(db *sql.DB, xpToGroupName map[string]string, stepTime StepTime) (PerformanceMetrics, error) {
	pm := PerformanceMetrics{
		Durations: make(map[string]int),
		Counts:    make(map[string]int),
	}

	// 1. Query DataLoader ID.
	dlID, _ := queryDataLoaderID(db)

	// 2. Query KERNEL_AICORE connection IDs for host-time fallback.
	kernelConnIDs, _ := getKernelAicoreTaskConnectionIDs(db, stepTime)

	// If no parallel group info, everything except ZP_Kernel and DataLoader is invalid.
	if len(xpToGroupName) == 0 {
		pm.ZPDevice = invalidData
		pm.ZPDuration = invalidData
		pm.ZPHost = invalidData
		pm.ZPBubble = invalidData
		pm.ZPCount = invalidData
		pm.ZPKernel, _ = GetAvgKernelTaskDuration(db, stepTime)
		pm.DataLoader, _ = queryDataLoaderDuration(db, dlID, stepTime)
		return pm, nil
	}

	// 3. Build groupName → string_id mapping.
	groupNames := make([]string, 0, len(xpToGroupName))
	for _, origKey := range xpToGroupName {
		groupNames = append(groupNames, origKey)
	}

	groupNameIDMap, err := batchQueryStringIDs(db, groupNames)
	if err != nil || len(groupNameIDMap) == 0 {
		pm.ZPDuration = invalidData
		pm.ZPDevice = invalidData
		pm.ZPBubble = invalidData
		pm.ZPCount = invalidData
		pm.ZPHost = invalidData
		pm.ZPKernel, _ = GetAvgKernelTaskDuration(db, stepTime)
		pm.DataLoader, _ = queryDataLoaderDuration(db, dlID, stepTime)
		return pm, nil
	}

	// Build reverse: STRING_IDS id → original group name.
	idToXp := make(map[int]string)
	for gn, sid := range groupNameIDMap {
		if xpName, ok := xpToGroupName[gn]; ok {
			idToXp[sid] = xpName
		}
	}

	// Build group name id list for SQL IN clause.
	groupNameIDs := make([]int, 0, len(groupNameIDMap))
	for _, sid := range groupNameIDMap {
		groupNameIDs = append(groupNameIDs, sid)
	}

	// 4. Query communication operators.
	devOps, err := getDeviceOpList(db, groupNameIDs, stepTime)
	if err != nil || len(devOps) == 0 {
		// No communication ops — invalidate all but ZP_Host (use kernel host fallback).
		pm.ZPDevice = invalidData
		pm.ZPDuration = invalidData
		pm.ZPBubble = invalidData
		pm.ZPCount = invalidData
		pm.ZPKernel, _ = GetAvgKernelTaskDuration(db, stepTime)
		pm.DataLoader, _ = queryDataLoaderDuration(db, dlID, stepTime)

		// ZP_Host fallback from KERNEL_AICORE host times.
		hostDurs := collectKernelHostDurations(db, kernelConnIDs)
		if len(hostDurs) > 0 {
			m, _ := CalculateMean(hostDurs)
			pm.ZPHost = m
		} else {
			pm.ZPHost = invalidData
		}
		return pm, nil
	}

	// 5. Collect unique connection IDs.
	connSet := make(map[int]bool)
	for _, op := range devOps {
		if op.ConnectionID > 0 {
			connSet[op.ConnectionID] = true
		}
	}
	connIDs := make([]int, 0, len(connSet))
	for cid := range connSet {
		connIDs = append(connIDs, cid)
	}

	// 6. Query host times from CANN_API and MSTX_EVENTS.
	hostMap := make(map[int]HostOp)
	for _, table := range []string{"CANN_API", "MSTX_EVENTS"} {
		hm, _ := getHostOpFromTable(db, table, connIDs)
		for k, v := range hm {
			hostMap[k] = v
		}
	}

	// Fill host times into device ops.
	var hostDurations []int
	var bubbleDurations []int
	var intervals []Interval

	for i := range devOps {
		if ho, ok := hostMap[devOps[i].ConnectionID]; ok {
			devOps[i].HStartNs = ho.StartNs
			devOps[i].HEndNs = ho.EndNs
		}
		// Host duration.
		if devOps[i].HStartNs > 0 && devOps[i].HEndNs >= devOps[i].HStartNs {
			hostDurations = append(hostDurations, devOps[i].HEndNs-devOps[i].HStartNs)
		}
		// Bubble duration.
		if devOps[i].HEndNs > 0 && devOps[i].StartNs > devOps[i].HEndNs {
			bubbleDurations = append(bubbleDurations, devOps[i].StartNs-devOps[i].HEndNs)
		}
		// Communication intervals.
		if devOps[i].StartNs > 0 && devOps[i].EndNs >= devOps[i].StartNs {
			intervals = append(intervals, Interval{Start: devOps[i].StartNs, End: devOps[i].EndNs})
		}
	}

	// Add kernel host durations.
	kernelHostDurs := collectKernelHostDurations(db, kernelConnIDs)
	hostDurations = append(hostDurations, kernelHostDurs...)

	// 7. Compute metrics.
	pm.ZPDuration = mergeIntervalsSimple(intervals)
	pm.ZPDevice = stepTime.EndNs - stepTime.StartNs - pm.ZPDuration
	if pm.ZPDevice < 0 {
		pm.ZPDevice = 0
		fmt.Fprintf(os.Stderr, "[WARN] Communication time exceeds step time, ZP_Device clamped to 0\n")
	}

	if m, err := CalculateMean(hostDurations); err == nil {
		pm.ZPHost = m
	} else {
		pm.ZPHost = invalidData
	}

	if m, err := CalculateMean(bubbleDurations); err == nil {
		pm.ZPBubble = m
	} else {
		pm.ZPBubble = invalidData
	}

	pm.ZPKernel, _ = GetAvgKernelTaskDuration(db, stepTime)
	pm.DataLoader, _ = queryDataLoaderDuration(db, dlID, stepTime)
	pm.ZPCount = invalidData

	// Per-domain metrics.
	domainOps := make(map[string][]OpStat)
	for _, op := range devOps {
		if xpName, ok := idToXp[op.DomainID]; ok {
			dur := op.EndNs - op.StartNs
			if dur > 0 {
				domainOps[xpName] = append(domainOps[xpName], OpStat{Duration: dur, Count: op.Count})
			}
		}
	}

	for domain, stats := range domainOps {
		dur, cnt, err := CalculateMidMeanPair(stats)
		if err == nil {
			pm.Durations[domain] = dur
			pm.Counts[domain] = cnt
		}
	}

	return pm, nil
}

// GetAvgKernelTaskDuration returns the average duration of KERNEL_AICORE tasks
// within the given step window.
func GetAvgKernelTaskDuration(db *sql.DB, stepTime StepTime) (int, error) {
	var avg sql.NullFloat64
	err := db.QueryRow(`
		SELECT AVG(t.endNs - t.startNs)
		FROM TASK t
		INNER JOIN STRING_IDS s ON t.taskType = s.id
		WHERE s.value IN ('KERNEL_AICORE')
		  AND t.startNs >= ?
		  AND t.endNs <= ?
	`, stepTime.StartNs, stepTime.EndNs).Scan(&avg)
	if err != nil || !avg.Valid {
		return 0, fmt.Errorf("kernel avg query failed: %w", err)
	}
	return int(math.Round(avg.Float64)), nil
}

// ---------------------------------------------------------------------------
// SQL helpers
// ---------------------------------------------------------------------------

func queryDataLoaderID(db *sql.DB) (int, error) {
	var id int
	err := db.QueryRow("SELECT id FROM STRING_IDS WHERE value = 'dataloader'").Scan(&id)
	return id, err
}

func queryDataLoaderDuration(db *sql.DB, dlID int, stepTime StepTime) (int, error) {
	var start, end int
	err := db.QueryRow(`
		SELECT startNs, endNs FROM MSTX_EVENTS
		WHERE message = ? AND startNs >= ? AND endNs <= ?
		LIMIT 1
	`, dlID, stepTime.StartNs, stepTime.EndNs).Scan(&start, &end)
	if err != nil {
		return 0, err
	}
	if end > start {
		return end - start, nil
	}
	return 0, nil
}

func batchQueryStringIDs(db *sql.DB, values []string) (map[string]int, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("no values")
	}
	args := make([]interface{}, len(values))
	for i, v := range values {
		args[i] = v
	}
	query := fmt.Sprintf("SELECT value, id FROM STRING_IDS WHERE value IN (%s)", placeholders(len(values)))
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var val string
		var id int
		if err := rows.Scan(&val, &id); err != nil {
			continue
		}
		result[val] = id
	}
	return result, rows.Err()
}

func getDeviceOpList(db *sql.DB, groupNameIDs []int, stepTime StepTime) ([]CommunicationOp, error) {
	if len(groupNameIDs) == 0 {
		return nil, nil
	}
	// Build IN clause parameters.
	placeholders := make([]string, len(groupNameIDs))
	args := make([]interface{}, 0, len(groupNameIDs)+2)
	for i, gid := range groupNameIDs {
		placeholders[i] = "?"
		args = append(args, gid)
	}
	args = append(args, stepTime.StartNs, stepTime.EndNs)

	query := fmt.Sprintf(`
		SELECT opName, startNs, endNs, connectionId, count, _rowid_, groupName
		FROM COMMUNICATION_OP
		WHERE groupName IN (%s)
		  AND startNs >= ?
		  AND endNs <= ?
		ORDER BY startNs ASC
	`, strings.Join(placeholders, ", "))

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ops []CommunicationOp
	for rows.Next() {
		var op CommunicationOp
		if err := rows.Scan(&op.OpName, &op.StartNs, &op.EndNs,
			&op.ConnectionID, &op.Count, &op.OpStreamIndex, &op.DomainID); err != nil {
			continue
		}
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

func getHostOpFromTable(db *sql.DB, tableName string, connIDs []int) (map[int]HostOp, error) {
	if len(connIDs) == 0 {
		return nil, nil
	}
	// Check table exists.
	exists, _ := tableExists(db, tableName)
	if !exists {
		return nil, nil
	}

	placeholders := make([]string, len(connIDs))
	args := make([]interface{}, len(connIDs))
	for i, cid := range connIDs {
		placeholders[i] = "?"
		args[i] = cid
	}

	query := fmt.Sprintf(
		"SELECT startNs, endNs, connectionId FROM %s WHERE connectionId IN (%s)",
		tableName, strings.Join(placeholders, ", "),
	)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int]HostOp)
	for rows.Next() {
		var ho HostOp
		var cid int
		if err := rows.Scan(&ho.StartNs, &ho.EndNs, &cid); err != nil {
			continue
		}
		result[cid] = ho
	}
	return result, rows.Err()
}

func getKernelAicoreTaskConnectionIDs(db *sql.DB, stepTime StepTime) ([]int, error) {
	rows, err := db.Query(`
		SELECT t.connectionId
		FROM TASK t
		INNER JOIN STRING_IDS s ON t.taskType = s.id
		WHERE s.value IN ('KERNEL_AICORE')
		  AND t.startNs >= ?
		  AND t.endNs <= ?
		  AND t.connectionId > 0
	`, stepTime.StartNs, stepTime.EndNs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var cid int
		if err := rows.Scan(&cid); err != nil {
			continue
		}
		ids = append(ids, cid)
	}
	return ids, rows.Err()
}

func collectKernelHostDurations(db *sql.DB, kernelConnIDs []int) []int {
	if len(kernelConnIDs) == 0 {
		return nil
	}
	var durs []int
	for _, table := range []string{"CANN_API", "MSTX_EVENTS"} {
		hostMap, err := getHostOpFromTable(db, table, kernelConnIDs)
		if err != nil {
			continue
		}
		for _, ho := range hostMap {
			if ho.StartNs > 0 && ho.EndNs >= ho.StartNs {
				durs = append(durs, ho.EndNs-ho.StartNs)
			}
		}
	}
	return durs
}

func tableExists(db *sql.DB, tableName string) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
		tableName,
	).Scan(&count)
	return count > 0, err
}

// CheckTableExists is the public wrapper.
func CheckTableExists(db *sql.DB, tableName string) (bool, error) {
	return tableExists(db, tableName)
}

// ---------------------------------------------------------------------------
// CSV I/O
// ---------------------------------------------------------------------------

// WriteResultsToCSV writes a slice of PerformanceMetrics to a CSV file.
// All rows in a single call share the same set of headers.
func WriteResultsToCSV(outputFile string, metrics []PerformanceMetrics) error {
	if len(metrics) == 0 {
		return nil
	}

	// Collect all domain names (sorted for determinism).
	domainSet := make(map[string]bool)
	for _, m := range metrics {
		for d := range m.Durations {
			domainSet[d] = true
		}
		for d := range m.Counts {
			domainSet[d] = true
		}
	}
	domains := make([]string, 0, len(domainSet))
	for d := range domainSet {
		domains = append(domains, d)
	}
	sort.Strings(domains)

	// Build header.
	header := []string{
		"StepIndex", "StepDuration", "ZP_Device", "ZP_Duration",
		"ZP_Host", "ZP_Bubble", "ZP_Count", "ZP_Kernel", "DataLoader",
	}
	for _, d := range domains {
		header = append(header, d+"_Duration", d+"_Count")
	}

	// Ensure output directory exists.
	os.MkdirAll(filepath.Dir(outputFile), 0755)

	csvMutex.Lock()
	defer csvMutex.Unlock()

	f, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return err
	}

	for _, m := range metrics {
		row := []string{
			strconv.Itoa(m.StepIndex),
			strconv.Itoa(m.StepDuration),
			strconv.Itoa(m.ZPDevice),
			strconv.Itoa(m.ZPDuration),
			strconv.Itoa(m.ZPHost),
			strconv.Itoa(m.ZPBubble),
			strconv.Itoa(m.ZPCount),
			strconv.Itoa(m.ZPKernel),
			strconv.Itoa(m.DataLoader),
		}
		for _, d := range domains {
			dur := m.Durations[d]
			cnt := m.Counts[d]
			row = append(row, strconv.Itoa(dur), strconv.Itoa(cnt))
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
