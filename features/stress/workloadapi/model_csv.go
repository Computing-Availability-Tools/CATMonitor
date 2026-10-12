package workloadapi

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
)

// ValidateModelCSV verifies semantic evidence, not file presence or timestamps.
func ValidateModelCSV(reader io.Reader, result *ModelResult) error {
	if result == nil {
		return fmt.Errorf("missing model result")
	}
	csvReader := csv.NewReader(reader)
	header, err := csvReader.Read()
	if err != nil {
		return err
	}
	indexes := map[string]int{}
	for i, key := range header {
		if _, duplicate := indexes[key]; duplicate {
			return fmt.Errorf("duplicate CSV column")
		}
		indexes[key] = i
	}
	for _, key := range []string{"job_id", "action", "reference_id", "task", "device_id", "run_count", "result", "err_count", "exetime", "checked_steps", "saved_steps"} {
		if _, ok := indexes[key]; !ok {
			return fmt.Errorf("missing model CSV %s", key)
		}
	}
	seen := map[int]bool{}
	for {
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		field := func(key string) string { return row[indexes[key]] }
		if field("job_id") != result.JobID || field("action") != result.Action || field("reference_id") != result.ReferenceID || field("task") != result.Model || field("result") != "PASS" || field("err_count") != "0" {
			return fmt.Errorf("CSV is not a passing result of this model job")
		}
		id, err := strconv.Atoi(field("device_id"))
		if err != nil {
			return err
		}
		found := false
		for _, candidate := range result.Devices {
			found = found || id == candidate
		}
		if !found || seen[id] {
			return fmt.Errorf("CSV device coverage mismatch")
		}
		seen[id] = true
		count, err := strconv.Atoi(field("run_count"))
		if err != nil || count <= 0 || count > result.ExpectedSteps {
			return fmt.Errorf("invalid model run count")
		}
		checked, err := strconv.Atoi(field("checked_steps"))
		if err != nil {
			return err
		}
		saved, err := strconv.Atoi(field("saved_steps"))
		if err != nil {
			return err
		}
		if (result.Action == "compare" && (checked != result.ExpectedSteps || saved != 0)) || (result.Action == "create_reference" && (saved != result.ExpectedSteps || checked != 0)) {
			return fmt.Errorf("incomplete per-device model CSV step coverage")
		}
		elapsed, err := strconv.ParseFloat(field("exetime"), 64)
		if err != nil || math.IsNaN(elapsed) || math.IsInf(elapsed, 0) || elapsed < 0 {
			return fmt.Errorf("invalid model elapsed time")
		}
	}
	if len(seen) != len(result.Devices) {
		return fmt.Errorf("model CSV is missing selected devices")
	}
	return nil
}
