package cli

import (
	"reflect"
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress"
)

func TestParseArgs(t *testing.T) {
	path, names, output, err := parseArgs([]string{
		"--bench", "stream,hpcg", "-c", "test.yaml", "-o", "table",
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "test.yaml" || output != "table" || !reflect.DeepEqual(names, []string{"stream", "hpcg"}) {
		t.Fatalf("path=%q names=%v output=%q", path, names, output)
	}
}

func TestParseArgsRejectsInvalidInput(t *testing.T) {
	for _, args := range [][]string{
		{"--bench", "stream,", "-o", "json"},
		{"--bench", "stream", "-o", "yaml"},
		{"run"},
		{"unexpected"},
	} {
		if _, _, _, err := parseArgs(args); err == nil {
			t.Fatalf("args %v unexpectedly accepted", args)
		}
	}
}

func TestParseArgsUsesDefaults(t *testing.T) {
	path, names, output, err := parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" || len(names) != 0 || output != "json" {
		t.Fatalf("path=%q names=%v output=%q", path, names, output)
	}
}

func TestStatusAndValueFormatting(t *testing.T) {
	if got := statusLabel(stress.StatusHealthy); got != "OK" {
		t.Fatalf("healthy label=%q", got)
	}
	if got := statusLabel(stress.StatusTimeLimitReached); got != "OK (time limit)" {
		t.Fatalf("time-limit label=%q", got)
	}
	if got := formatValue(12); got != "12" {
		t.Fatalf("integer value=%q", got)
	}
	if got := formatValue(12.345); got != "12.35" {
		t.Fatalf("decimal value=%q", got)
	}
}
