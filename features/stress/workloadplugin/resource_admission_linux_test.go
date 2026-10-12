//go:build linux

package workloadplugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHPLResolvedInputMatchesDescribeAndIsIsolated(t *testing.T) {
	root := t.TempDir()
	original := "HPL\nexample\nHPL.out\n6\n1\n10000\n1\n256\n0\n1\n2\n4\n"
	input := filepath.Join(root, "HPL.dat")
	if err := os.WriteFile(input, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HPL_EXECUTABLE", executableFixture(t, root, "xhpl", "#!/bin/sh\nexit 0\n"))
	t.Setenv("HPL_MPI_LAUNCHER", executableFixture(t, root, "mpirun", "#!/bin/sh\necho MPICH\n"))
	t.Setenv("HPL_WORKDIR", root)
	t.Setenv("HPL_LIBRARY_DIR", "")
	for key, value := range map[string]string{"HPL_N": "32", "HPL_NB": "16", "HPL_P": "1", "HPL_Q": "1", "HPL_MPI_PROCESSES": "1", "HPL_THREADS_PER_PROCESS": "1"} {
		t.Setenv(key, value)
	}
	profile := describeHPL(context.Background())
	found := false
	for _, asset := range profile.Assets {
		if asset.Name == "resource_capacity" {
			found = true
			if asset.Status != "pass" {
				t.Fatal(asset)
			}
		}
	}
	if !found {
		t.Fatal("missing capacity preflight")
	}
	spec, err := Resolve("hpl")
	if err != nil {
		t.Fatal(err)
	}
	defer spec.Cleanup()
	data, err := os.ReadFile(filepath.Join(spec.Dir, "HPL.dat"))
	if err != nil || !strings.Contains(string(data), "\n32\n1\n16\n") || spec.Dir == root {
		t.Fatalf("%s %v", data, err)
	}
	data, err = os.ReadFile(input)
	if err != nil || string(data) != original {
		t.Fatal("shared input was changed")
	}
	t.Setenv("HPL_P", "2")
	if _, err := Resolve("hpl"); err == nil {
		t.Fatal("mismatched P x Q accepted at run")
	}
	profile = describeHPL(context.Background())
	if profile.Preflight.Status != "fail" {
		t.Fatal("mismatched P x Q accepted by describe")
	}
}

func TestHPLCapacityAdmission(t *testing.T) {
	input := []byte("HPL\nexample\nHPL.out\n6\n1\n4096\n1\n256\n0\n1\n1\n2\n")
	if err := validateHPLResources(input, 2, 2, 4, 1<<30); err != nil {
		t.Fatal(err)
	}
	for _, args := range []struct {
		r, t int
		c    float64
		m    int64
	}{{1, 1, 4, 1 << 30}, {2, 4, 4, 1 << 30}, {2, 2, 3.5, 1 << 30}, {2, 2, 4, 1 << 20}} {
		if err := validateHPLResources(input, args.r, args.t, args.c, args.m); err == nil {
			t.Fatalf("accepted %+v", args)
		}
	}
}

func TestEffectiveResourcesSparseOfflineQuotaMemory(t *testing.T) {
	root := t.TempDir()
	proc, sys := filepath.Join(root, "proc"), filepath.Join(root, "sys")
	write := func(p, v string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(v), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(proc, "self/status"), "Cpus_allowed_list:\t2,5-7\n")
	write(filepath.Join(proc, "self/cgroup"), "0::/job\n")
	write(filepath.Join(proc, "meminfo"), "MemAvailable: 8388608 kB\n")
	write(filepath.Join(sys, "devices/system/cpu/online"), "0-5,7-9")
	write(filepath.Join(sys, "fs/cgroup/job/cpu.max"), "150000 100000")
	write(filepath.Join(sys, "fs/cgroup/job/memory.max"), "1073741824")
	write(filepath.Join(sys, "fs/cgroup/job/memory.current"), "536870912")
	c, m, err := readResources(proc, sys)
	if err != nil || c != 1.5 || m != 536870912 {
		t.Fatalf("%v %v %v", c, m, err)
	}
	write(filepath.Join(proc, "self/cgroup"), "3:cpu,cpuacct:/job\n4:memory:/job\n")
	write(filepath.Join(sys, "fs/cgroup/cpu,cpuacct/job/cpu.cfs_quota_us"), "200000")
	write(filepath.Join(sys, "fs/cgroup/cpu,cpuacct/job/cpu.cfs_period_us"), "100000")
	write(filepath.Join(sys, "fs/cgroup/memory/job/memory.limit_in_bytes"), "1073741824")
	write(filepath.Join(sys, "fs/cgroup/memory/job/memory.usage_in_bytes"), "1")
	c, m, err = readResources(proc, sys)
	if err != nil || c != 2 || m != 1073741823 {
		t.Fatalf("v1 %v %v %v", c, m, err)
	}
}

func TestNPUIdleFailsClosed(t *testing.T) {
	output := "| 2 910B4 | OK |\n| 5 Ascend910B4 | OK |\n| NPU Chip | Process id | Process name | Process memory(MB) |\n+===+\n| No running processes found in NPU 2 |\n+===+\n| No running processes found in NPU 5 |\n+===+\n"
	if err := parseNPUIdle(output, 2); err != nil {
		t.Fatal(err)
	}
	if err := parseNPUIdle(output, 4); err != nil {
		t.Fatal("card count is not die count", err)
	}
	for _, bad := range []string{"", output + "| 5 0 | 123 | VLLMWorker | 100 |", "Process id\n| No running processes found in NPU 2 |", output + "Warning: truncated inventory"} {
		if err := parseNPUIdle(bad, 2); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
