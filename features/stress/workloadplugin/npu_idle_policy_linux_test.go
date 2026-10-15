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

func npuIdlePolicyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "davinci5"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	exe := executableFixture(t, root, "burn", "#!/bin/sh\ntouch \"$HOME/model-probed\"\nexit 9\n")
	pci := executableFixture(t, root, "lspci", "#!/bin/sh\necho '0000:81:00.0 Processing accelerators: Huawei Device d803'\n")
	preflight := executableFixture(t, root, "preflight", "#!/bin/sh\nexit 0\n")
	smi := executableFixture(t, root, "npu-smi", "#!/bin/sh\ntouch \"$HOME/idle-probed\"\nif [ \"$TEST_NPU_BUSY\" = 1 ]; then\n printf '%s\\n' '| 5 910B4 | OK |' '| NPU Chip | Process id | Process name |' '| 5 0 | 12345 | | 100 |'\nelse\n printf '%s\\n' '| 5 910B4 | OK |' '| NPU Chip | Process id | Process name |' '| No running processes found in NPU 5 |'\nfi\n")
	for key, value := range map[string]string{
		"NPU_BURN_EXECUTABLE": exe, "CATMONITOR_LSPCI": pci, "NPU_SMI_EXECUTABLE": smi,
		"NPU_BURN_PREFLIGHT_EXECUTABLE": preflight, "NPU_BURN_DEVICE_ROOT": root,
		"NPU_BURN_OUTPUT_DIR": root, "NPU_BURN_LOG_DIR": root, "HOME": root,
		"NPU_BURN_INTERNAL_TIMEOUT_SECONDS": "300", "NPU_BURN_CHIP_GENERATION": "A2",
		"NPU_BURN_DEVICE": "0", "NPU_BURN_RUN_CASE": "matmul", "NPU_BURN_GROUP": "",
		"CATMONITOR_NPU_DEVICE_MODE": "full", "CATMONITOR_NPU_REQUIRE_IDLE": "0",
		"CATMONITOR_NPU_COMPAT_PROFILE": "none", "CATMONITOR_NPU_MODEL": "", "TEST_NPU_BUSY": "1",
	} {
		t.Setenv(key, value)
	}
	return root
}

func TestNPUReadinessSkipsOccupancyButNotRuntimeFailures(t *testing.T) {
	root := npuIdlePolicyFixture(t)
	p := describeNPUBurn(context.Background())
	if p.Preflight.Status != workloadapi.CheckPass {
		t.Fatalf("busy must not fail deployment readiness: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(root, "idle-probed")); !os.IsNotExist(err) {
		t.Fatalf("readiness queried occupancy: %v", err)
	}
	for _, check := range p.Assets {
		if check.Name == "idle_devices" {
			t.Fatal("readiness must not claim idle evidence")
		}
	}
	t.Setenv("CATMONITOR_NPU_REQUIRE_IDLE", "1")
	p = describeNPUBurn(context.Background())
	if p.Preflight.Status != workloadapi.CheckFail {
		t.Fatal("job admission accepted busy device")
	}
	t.Setenv("CATMONITOR_NPU_REQUIRE_IDLE", "0")
	t.Setenv("NPU_BURN_PREFLIGHT_EXECUTABLE", executableFixture(t, root, "broken-runtime", "#!/bin/sh\nexit 7\n"))
	if p := describeNPUBurn(context.Background()); p.Preflight.Status != workloadapi.CheckFail {
		t.Fatal("deployment-only policy hid runtime failure")
	}
}

func TestNPURunAlwaysRechecksIdleRegardlessOfDeploymentPolicy(t *testing.T) {
	root := npuIdlePolicyFixture(t)
	for _, policy := range []string{"0", "", "1"} {
		t.Setenv("CATMONITOR_NPU_REQUIRE_IDLE", policy)
		if _, err := resolveNPUBurn(); err == nil || !strings.Contains(err.Error(), "NPU busy") {
			t.Fatalf("REQUIRE_IDLE=%q bypassed run gate: %v", policy, err)
		}
	}
	// An earlier idle admission is not a reservation. Run must query again.
	t.Setenv("TEST_NPU_BUSY", "0")
	if p := describeNPUBurnWithIdle(context.Background(), true); p.Preflight.Status != workloadapi.CheckPass {
		t.Fatal(p.Preflight)
	}
	t.Setenv("TEST_NPU_BUSY", "1")
	if _, err := resolveNPUBurn(); err == nil {
		t.Fatal("cached idle evidence authorized execution")
	}
	t.Setenv("TEST_NPU_BUSY", "0")
	spec, err := resolveNPUBurn()
	if err != nil {
		t.Fatal(err)
	}
	spec.Cleanup()
	// Missing/unreadable process inventory is not permission to start.
	t.Setenv("NPU_SMI_EXECUTABLE", filepath.Join(root, "missing-smi"))
	if _, err := resolveNPUBurn(); err == nil || !strings.Contains(err.Error(), "cannot verify idle") {
		t.Fatalf("unverified device accepted: %v", err)
	}
}

func TestNPUModelRunCannotBypassIdleWithDeploymentPolicy(t *testing.T) {
	root := npuIdlePolicyFixture(t)
	t.Setenv("CATMONITOR_NPU_MODEL", "gpt3")
	t.Setenv("CATMONITOR_NPU_MODEL_ACTION", "create_reference")
	t.Setenv("CATMONITOR_NPU_MODEL_REFERENCE", "ref1")
	_, err := ResolveRequest(workloadapi.Request{Benchmark: "npu_burn", JobID: "aabb", NPUDevices: workloadapi.DeviceIDs{0},
		NPUModel: &workloadapi.ModelRequest{Name: "gpt3", Action: "create_reference", ReferenceID: "ref1"}})
	if err == nil || !strings.Contains(err.Error(), "idle_devices") {
		t.Fatalf("model run bypassed idle gate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "model-probed")); !os.IsNotExist(err) {
		t.Fatalf("model initialization ran on busy device: %v", err)
	}
}
