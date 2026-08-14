package cli

import (
	"bytes"
	"reflect"
	"strings"
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

func TestParseDoctorArgs(t *testing.T) {
	path, output, err := parseDoctorArgs([]string{"-c", "doctor.yaml", "-o", "table"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "doctor.yaml" || output != "table" {
		t.Fatalf("path=%q output=%q", path, output)
	}
	for _, args := range [][]string{{"--bench", "stream"}, {"-o", "yaml"}, {"unexpected"}} {
		if _, _, err := parseDoctorArgs(args); err == nil {
			t.Fatalf("args %v unexpectedly accepted", args)
		}
	}
}

func TestPrintDoctorTable(t *testing.T) {
	var output bytes.Buffer
	printDoctorTable(&output, doctorResult{
		Status: "pass",
		Benchmarks: []doctorItem{{
			Name: "stream", Enabled: true, Available: true,
			Status: stress.CheckPass, Message: "deployment precheck passed",
		}},
	})
	got := output.String()
	if !strings.Contains(got, "CATMonitor Stress Doctor  PASS") ||
		!strings.Contains(got, "stream") || !strings.Contains(got, "PASS") {
		t.Fatalf("unexpected doctor table:\n%s", got)
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
