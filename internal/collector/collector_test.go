package collector

import "testing"

func TestPriorityString(t *testing.T) {
	cases := map[Priority]string{
		PriorityHigh:   "High",
		PriorityMedium: "Medium",
		PriorityLow:    "Low",
		Priority(99):   "Unknown",
		Priority(-1):   "Unknown",
	}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("Priority(%d).String() = %q, want %q", int(p), got, want)
		}
	}
}

// AnyWanted defaults to true when no checker is installed (backward compatible).
func TestAnyWantedNilChecker(t *testing.T) {
	SetWantedChecker(nil)
	if !AnyWanted("cpu", []string{"anything"}) {
		t.Error("AnyWanted must default to true with no checker installed")
	}
}

func TestAnyWantedWithChecker(t *testing.T) {
	SetWantedChecker(func(component string, names []string) bool {
		return component == "cpu" && len(names) > 0 && names[0] == "usage"
	})
	defer SetWantedChecker(nil)

	if !AnyWanted("cpu", []string{"usage"}) {
		t.Error("checker allows cpu/usage")
	}
	if AnyWanted("cpu", []string{"user_time"}) {
		t.Error("checker rejects cpu/user_time")
	}
	if AnyWanted("gpu", []string{"usage"}) {
		t.Error("checker rejects non-cpu components")
	}
	if AnyWanted("cpu", nil) {
		t.Error("checker rejects an empty name list (len 0)")
	}
}
