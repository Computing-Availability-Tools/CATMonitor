//go:build linux

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

func TestRequestContractRejectsCommandLikeOptions(t *testing.T) {
	t.Setenv("CATMONITOR_STRESS_BENCHMARKS", "stream,hpl")
	valid := workloadapi.Request{ProtocolVersion: 1, JobID: "aabb", Benchmark: "stream", TimeoutSeconds: 5}
	if err := validateRequest(valid); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*workloadapi.Request){
		"unknown benchmark": func(r *workloadapi.Request) { r.Benchmark = "shell" },
		"invalid job id":    func(r *workloadapi.Request) { r.JobID = "../job" },
		"invalid protocol":  func(r *workloadapi.Request) { r.ProtocolVersion = 2 },
		"unbounded timeout": func(r *workloadapi.Request) { r.TimeoutSeconds = 86401 },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			mutate(&request)
			if err := validateRequest(request); err == nil {
				t.Fatal("invalid request was accepted")
			}
		})
	}
	valid.Options = map[string]json.RawMessage{"command": json.RawMessage(`"rm"`)}
	if err := validateRequest(valid); err != nil {
		t.Fatalf("shape validation should leave options policy to run: %v", err)
	}
}

func TestStateRootMustBeAbsolute(t *testing.T) {
	t.Setenv("CATMONITOR_STRESS_STATE_ROOT", "relative")
	if _, err := stateRoot(); err == nil {
		t.Fatal("relative state root was accepted")
	}
}

func TestAtomicWriteJSONDoesNotLeaveTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := atomicWriteJSON(path, workloadapi.State{ProtocolVersion: 1, JobID: "aabb", Status: workloadapi.StatusRunning}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("unexpected state directory entries: %v", entries)
	}
}
