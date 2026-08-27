//go:build linux

package workloadplugin

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func executableFixture(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStreamPluginResolvesFixedImageEnvironment(t *testing.T) {
	dir := t.TempDir()
	stream := executableFixture(t, dir, "stream", "#!/bin/sh\nexit 0\n")
	numactl := executableFixture(t, dir, "numactl", "#!/bin/sh\nexit 0\n")
	t.Setenv("STREAM_EXECUTABLE", stream)
	t.Setenv("STREAM_NUMACTL", numactl)
	t.Setenv("STREAM_THREADS", "4")
	spec, err := Resolve("stream")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != numactl || !reflect.DeepEqual(spec.Args, []string{"--interleave=all", stream}) || !reflect.DeepEqual(spec.Env, []string{"OMP_NUM_THREADS=4"}) {
		t.Fatalf("unexpected stream command: %+v", spec)
	}
}

func TestNPUTopologyMapsSparseDeviceNodesToContiguousLogicalIDs(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"2", "5"} {
		if err := os.WriteFile(filepath.Join(root, "davinci"+id), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	lspci := executableFixture(t, root, "lspci", "#!/bin/sh\nprintf '%s\\n' '0000:01:00.0 Processing accelerators: Huawei Device d803' '0000:02:00.0 Processing accelerators: Huawei Device d803'\n")
	t.Setenv("CATMONITOR_LSPCI", lspci)
	t.Setenv("NPU_BURN_DEVICE_ROOT", root)
	t.Setenv("NPU_BURN_DEVICE", "1")
	logical, nodes, pci, _, err := npuTopology(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(logical, ",") != "0,1" || strings.Join(nodes, ",") != "2,5" || strings.Join(pci, ",") != "0,1" {
		t.Fatalf("logical=%v nodes=%v pci=%v", logical, nodes, pci)
	}
}

func TestNPUSummaryRequiresCompletePass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "npu_burn_results.csv")
	data := "task,device,x,y,z,exetime,error,result\nmatmul,0,1,1,1,2.5,0,PASS\nmatmul,1,1,1,1,3.5,0,PASS\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	summary, err := summarizeCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "devices=2") || !strings.Contains(summary, "passed=2") || !strings.Contains(summary, "case_time_seconds=6.000000") {
		t.Fatalf("unexpected summary: %s", summary)
	}
}
