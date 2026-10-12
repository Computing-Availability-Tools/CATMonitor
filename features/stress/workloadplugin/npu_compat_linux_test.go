//go:build linux

package workloadplugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSingleDeviceAdmissionRequiresRuntimeProof(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "davinci14"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NPU_BURN_DEVICE_ROOT", root)
	t.Setenv("CATMONITOR_NPU_DEVICE_MODE", "single")
	t.Setenv("NPU_BURN_DEVICE", "0")
	good := `{"runtime_count":1,"node_ids":["14"],"pci_addresses":["0000:01:00.0","0000:81:00.0"],"numa_to_logical":{"7":[0]},"node_pci":{"14":"0000:81:00.0"},"runtime_pci":{"0":"0000:81:00.0"},"mapping_source":"runtime_kernel_pci"}`
	cases := []struct {
		name, proof string
		ok          bool
	}{
		{"valid", good, true},
		{"missing", "{}", false},
		{"wrong_runtime", strings.Replace(good, `"0":"0000:81:00.0"`, `"0":"0000:01:00.0"`, 1), false},
		{"runtime_count", strings.Replace(good, `"runtime_count":1`, `"runtime_count":2`, 1), false},
		{"wrong_node", strings.Replace(good, `["14"]`, `["5"]`, 1), false},
		{"unknown_numa", strings.Replace(good, `"7":[0]`, `"-1":[0]`, 1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := executableFixture(t, root, "preflight", fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s'\n", tc.proof))
			t.Setenv("NPU_BURN_PREFLIGHT_EXECUTABLE", path)
			ids, _, _, _, err := npuTopology(context.Background())
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v", err)
			}
			if tc.ok && strings.Join(ids, ",") != "0" {
				t.Fatal(ids)
			}
		})
	}
	t.Setenv("NPU_BURN_DEVICE", "14")
	if _, _, _, _, err := npuTopology(context.Background()); err == nil {
		t.Fatal("host ID accepted as runtime index")
	}
}

func TestNPUCSVSemanticAndDeviceCoverage(t *testing.T) {
	header := "task,device_id,case_idx,run_count,stream_count,exetime,err_count,result,case_config\n"
	for _, row := range []string{
		"matmul,0,0,0,1,1,0,PASS,x\n",
		"matmul,0,0,1,1,1,-1,PASS,x\n",
		"matmul,0,0,1,1,NaN,0,PASS,x\n",
		"matmul,0,0,1,1,1,0,FAIL,x\n",
		"matmul,1,0,1,1,1,0,PASS,x\n",
	} {
		p := filepath.Join(t.TempDir(), "results.csv")
		if err := os.WriteFile(p, []byte(header+row), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := summarizeCSV(p, "0"); err == nil {
			t.Fatalf("accepted %q", row)
		}
	}
	p := filepath.Join(t.TempDir(), "results.csv")
	if err := os.WriteFile(p, []byte(header+"matmul,0,0,10,1,1,0,PASS,x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := summarizeCSV(p, "0"); err != nil {
		t.Fatal(err)
	}
	if _, err := summarizeCSV(p, "0", "1"); err == nil {
		t.Fatal("incomplete coverage accepted")
	}
}

func TestNPURunDoesNotReusePreviousCSV(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"dev", "output", "log", "home"} {
		if err := os.Mkdir(filepath.Join(root, name), 0750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "dev", "davinci5"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	exe := executableFixture(t, root, "burn", "#!/bin/sh\nexit 0\n")
	pci := executableFixture(t, root, "lspci", "#!/bin/sh\necho '0000:01:00.0 Processing accelerators: Huawei Device d803'\n")
	for k, v := range map[string]string{
		"NPU_BURN_EXECUTABLE": exe, "CATMONITOR_LSPCI": pci,
		"NPU_BURN_DEVICE_ROOT": filepath.Join(root, "dev"), "NPU_BURN_OUTPUT_DIR": filepath.Join(root, "output"),
		"NPU_BURN_LOG_DIR": filepath.Join(root, "log"), "HOME": filepath.Join(root, "home"),
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "10", "NPU_BURN_CHIP_GENERATION": "A3",
		"NPU_BURN_DEVICE": "0", "NPU_BURN_RUN_CASE": "matmul", "NPU_BURN_GROUP": "",
		"CATMONITOR_NPU_DEVICE_MODE": "full",
	} {
		t.Setenv(k, v)
	}
	row := "task,device_id,case_idx,run_count,stream_count,exetime,err_count,result,case_config\nmatmul,0,0,10,1,1,0,PASS,x\n"
	if err := os.WriteFile(filepath.Join(root, "output", "npu_burn_results.csv"), []byte(row), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := resolveNPUBurn()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Cleanup()
	if _, err = first.Complete(""); err == nil {
		t.Fatal("old CSV accepted")
	}
	var output, home string
	for _, v := range first.Env {
		if strings.HasPrefix(v, "NPU_BURN_OUTPUT_DIR=") {
			output = strings.TrimPrefix(v, "NPU_BURN_OUTPUT_DIR=")
		}
		if strings.HasPrefix(v, "HOME=") {
			home = strings.TrimPrefix(v, "HOME=")
		}
	}
	if !strings.HasPrefix(home, filepath.Join(root, "home")+string(os.PathSeparator)) {
		t.Fatal("job HOME escaped the deployment's tmpfs budget")
	}
	if first.Dir != home {
		t.Fatalf("CANN cwd=%q, want per-job tmpfs HOME=%q", first.Dir, home)
	}
	// Reproduce the relative CANN JIT write without hardware or a writable
	// image /workspace. Execute in exactly the directory returned by Resolve.
	jit := exec.Command("sh", "-c", "mkdir kernel_meta && printf compiled > kernel_meta/op.o")
	jit.Dir = first.Dir
	if out, err := jit.CombinedOutput(); err != nil {
		t.Fatalf("per-job JIT directory: %v: %s", err, out)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(home, ".ascend_npu_burn/output"))
	if err != nil || resolved != output {
		t.Fatal("default output does not resolve to current CSV directory", err)
	}
	if err := os.WriteFile(filepath.Join(output, "npu_burn_results.csv"), []byte(row), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = first.Complete(""); err != nil {
		t.Fatal(err)
	}
	first.Cleanup()
	if _, err := os.Stat(first.Dir); !os.IsNotExist(err) {
		t.Fatalf("per-job JIT cache not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(output, "npu_burn_results.csv")); err != nil {
		t.Fatal("HOME cleanup removed persistent CSV", err)
	}
	second, err := resolveNPUBurn()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Cleanup()
	if _, err = second.Complete(""); err == nil {
		t.Fatal("previous invocation's CSV accepted")
	}
}
