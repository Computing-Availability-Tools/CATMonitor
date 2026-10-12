package workloadapi

import "testing"

func TestNPUFailureClassification(t *testing.T) {
	f := ClassifyNPUFailure("RuntimeError: NPU out of memory. Tried to allocate 162.00 MiB")
	if f == nil || f.Code != "npu_hbm_oom" || f.Resource != "npu_hbm" {
		t.Fatalf("explicit HBM allocation failure not classified: %+v", f)
	}
	for _, text := range []string{"aclInit error 507899", "MemoryError", "workload cancelled", "CUDA out of memory", "PASS", "Advice: NPU out of memory may occur"} {
		if ClassifyNPUFailure(text) != nil {
			t.Fatalf("unproven HBM failure inferred from %q", text)
		}
	}
}
