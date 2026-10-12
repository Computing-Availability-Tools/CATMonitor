package workloadapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// CaseNames contains explicit upstream operator names, not shell arguments,
// groups, numeric aliases or an implicit "all" selection.
type CaseNames []string

func (c *CaseNames) UnmarshalJSON(data []byte) error {
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return err
	}
	if err := ValidateCases(names); err != nil {
		return err
	}
	*c = names
	return nil
}

func ValidateCases(names []string) error {
	if len(names) == 0 || len(names) > 128 {
		return fmt.Errorf("npu_cases requires 1..128 explicit operator names")
	}
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || name == "all" || len(name) > 64 || seen[name] ||
			name[0] < 'a' || name[0] > 'z' || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" {
			return fmt.Errorf("invalid or duplicate NPU operator %q", name)
		}
		seen[name] = true
	}
	return nil
}

func ParseCases(text string) (CaseNames, error) {
	names := CaseNames(strings.Split(text, ","))
	for i := range names {
		names[i] = strings.TrimSpace(names[i])
	}
	return names, ValidateCases(names)
}

func (c CaseNames) String() string { return strings.Join(c, ",") }

func SameCases(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a, b = append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
