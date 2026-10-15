//go:build linux

package workloadplugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

func trackParameterEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"HPL_WORKDIR", "HPL_N", "HPL_NB", "HPL_P", "HPL_Q", "HPL_MPI_PROCESSES", "HPL_THREADS_PER_PROCESS", "HPCG_WORKDIR", "HPCG_NX", "HPCG_NY", "HPCG_NZ", "HPCG_RUNTIME_SECONDS", "HPCG_MPI_PROCESSES", "HPCG_THREADS_PER_PROCESS", "STREAM_THREADS", "OMP_NUM_THREADS"} {
		t.Setenv(key, "")
	}
}

func TestHPLJobFreezeInheritsAndDoesNotModifySharedInput(t *testing.T) {
	trackParameterEnv(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "HPL.dat")
	data := "HPL\nfixture\nHPL.out\n6\n2\n256 512\n1\n32\n0\n1\n1\n1\n16\n"
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HPL_WORKDIR", dir)
	t.Setenv("HPL_MPI_PROCESSES", "1")
	t.Setenv("HPL_THREADS_PER_PROCESS", "1")
	f, err := freezeParameters(context.Background(), "hpl", nil)
	if err != nil || f.HPLInput != data {
		t.Fatalf("full default input lost: %v", err)
	}
	n := 1024
	f, err = freezeParameters(context.Background(), "hpl", &workloadapi.JobParameters{HPL: &workloadapi.HPLParameters{N: &n}})
	if err != nil {
		t.Fatal(err)
	}
	if *f.Effective.HPL.N != 1024 || *f.Effective.HPL.NB != 32 || strings.Split(f.HPLInput, "\n")[4] != "1" {
		t.Fatal("partial inherit failed", f)
	}
	original, _ := os.ReadFile(file)
	if string(original) != data {
		t.Fatal("shared input modified")
	}
	if err := os.WriteFile(file, []byte("changed after admission"), 0600); err != nil {
		t.Fatal(err)
	}
	_, cleanup, err := PrepareJobParameters(context.Background(), "hpl", nil, f)
	if err != nil {
		t.Fatal(err)
	}
	temp := env("HPL_WORKDIR")
	loaded, _ := os.ReadFile(filepath.Join(temp, "HPL.dat"))
	if string(loaded) != f.HPLInput {
		t.Fatal("run reread shared deployment")
	}
	cleanup()
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatal("temporary input leaked")
	}
	if err := ValidateJobCapacity("hpl", f); err != nil {
		t.Fatal(err)
	}
	q := 2
	f.Effective.HPL.Q = &q
	lines := strings.Split(f.HPLInput, "\n")
	lines[11] = "2"
	f.HPLInput = strings.Join(lines, "\n")
	_ = f.Seal()
	if ValidateJobCapacity("hpl", f) == nil {
		t.Fatal("P*Q != ranks admitted")
	}
}

func TestJobThreadsAndHPCGCapacityFailClosed(t *testing.T) {
	trackParameterEnv(t)
	t.Setenv("STREAM_THREADS", "1")
	threads := 65536
	f, err := freezeParameters(context.Background(), "stream", &workloadapi.JobParameters{Stream: &workloadapi.StreamParameters{Threads: &threads}})
	if err != nil {
		t.Fatal(err)
	}
	if ValidateJobCapacity("stream", f) == nil {
		t.Fatal("CPU oversubscription admitted")
	}
	for key, value := range map[string]string{"HPCG_NX": "16", "HPCG_NY": "16", "HPCG_NZ": "16", "HPCG_RUNTIME_SECONDS": "2", "HPCG_MPI_PROCESSES": "1", "HPCG_THREADS_PER_PROCESS": "1"} {
		t.Setenv(key, value)
	}
	f, err = freezeParameters(context.Background(), "hpcg", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ValidateJobCapacity("hpcg", f) != nil {
		t.Fatal("small inherited grid rejected")
	}
	t.Setenv("HPCG_NX", "8")
	t.Setenv("HPCG_NY", "32")
	legacy, err := freezeParameters(context.Background(), "hpcg", nil)
	if err != nil || *legacy.Effective.HPCG.NX != 32 {
		t.Fatal("legacy grid did not record upstream normalization", err)
	}
	huge := 1048576
	f.Effective.HPCG.NX = &huge
	f.Effective.HPCG.NY = &huge
	f.Effective.HPCG.NZ = &huge
	_ = f.Seal()
	if ValidateJobCapacity("hpcg", f) == nil {
		t.Fatal("memory oversubscription admitted")
	}
}

func TestJobDescribePreservesRequiredAssetWireContract(t *testing.T) {
	trackParameterEnv(t)
	root := t.TempDir()
	t.Setenv("STREAM_EXECUTABLE", executableFixture(t, root, "stream", "#!/bin/sh\nexit 0\n"))
	t.Setenv("STREAM_NUMACTL", executableFixture(t, root, "numactl", "#!/bin/sh\nexit 0\n"))
	t.Setenv("STREAM_THREADS", "1")
	p, err := DescribeJob(context.Background(), "stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range p.Assets {
		if a.Name == "job_parameter_capacity" {
			found = true
			if a.Path == "" || a.Status != workloadapi.CheckPass {
				t.Fatal("invalid required asset envelope", a)
			}
		}
	}
	if !found {
		t.Fatal("missing capacity gate")
	}
}
