package main

import (
	"errors"
	"strings"
	"testing"
)

func TestModelOOMFailureMessage(t *testing.T) {
	failure := errors.New("exit status 1")
	output := "native diagnostics\nERROR: model SDC: NPU_HBM_OOM: model=gpt3, logical_device=1; NPU out of memory. Tried to allocate 162 MiB\n"
	message := modelFailureMessage(failure, output)
	for _, want := range []string{"NPU HBM OOM", "gpt3", "logical_device=1", "162 MiB"} {
		if !strings.Contains(message, want) {
			t.Fatalf("failure message missing %q: %s", want, message)
		}
	}
	if got := modelFailureMessage(failure, "unrelated text: NPU out of memory"); got != "workload command failed: exit status 1" {
		t.Fatalf("unmarked diagnostic must not be relabelled: %s", got)
	}
	if got := modelFailureMessage(nil, output); got != "" {
		t.Fatalf("successful process must not be converted to OOM: %s", got)
	}
}

func TestModelOOMMessageIsBounded(t *testing.T) {
	output := "ERROR: model SDC: NPU_HBM_OOM: " + strings.Repeat("x", 2048)
	message := modelFailureMessage(errors.New("exit status 1"), output)
	if len(message) > len("NPU HBM OOM: ")+1024 {
		t.Fatalf("unbounded OOM message length: %d", len(message))
	}
}
