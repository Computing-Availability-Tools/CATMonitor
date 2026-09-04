//go:build linux

package cli

import "testing"

func TestCLIRejectsUnknownArgumentsAndOutput(t *testing.T) {
	if _, _, err := parseDoctorArgs([]string{"unexpected"}); err == nil {
		t.Fatal("doctor should reject positional arguments")
	}
	if _, _, _, _, err := parseArgs([]string{"-o", "xml"}); err == nil {
		t.Fatal("run should reject unsupported output format")
	}
	if _, _, _, err := parseJobArgs("stress cancel", []string{}, true); err == nil {
		t.Fatal("cancel should require a job id")
	}
}
