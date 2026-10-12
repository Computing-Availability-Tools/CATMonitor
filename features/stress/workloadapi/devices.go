package workloadapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// DeviceIDs is an explicit logical selection. Nil means omitted; [] and null
// on the wire are errors, never aliases for all devices.
type DeviceIDs []int

func (d *DeviceIDs) UnmarshalJSON(data []byte) error {
	var ids []int
	if err := json.Unmarshal(data, &ids); err != nil {
		return err
	}
	if err := ValidateDevices(ids); err != nil {
		return err
	}
	*d = ids
	return nil
}

func ValidateDevices(ids []int) error {
	if len(ids) == 0 || len(ids) > 1024 {
		return fmt.Errorf("npu_devices requires 1..1024 explicit logical IDs")
	}
	seen := map[int]bool{}
	for _, id := range ids {
		if id < 0 || id > 1048575 || seen[id] {
			return fmt.Errorf("invalid or duplicate NPU logical ID %d", id)
		}
		seen[id] = true
	}
	return nil
}

func ParseDevices(text string) (DeviceIDs, error) {
	var ids DeviceIDs
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return nil, fmt.Errorf("expected comma-separated logical IDs, got %q", text)
		}
		id, err := strconv.Atoi(part)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, ValidateDevices(ids)
}

func (d DeviceIDs) String() string {
	parts := make([]string, len(d))
	for i, id := range d {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

func SameDevices(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	a, b = append([]int(nil), a...), append([]int(nil), b...)
	sort.Ints(a)
	sort.Ints(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
