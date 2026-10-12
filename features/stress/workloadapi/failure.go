package workloadapi

import "strings"

// Failure is durable diagnostic evidence, not a hardware health verdict.
type Failure struct {
	Code     string `json:"code"`
	Resource string `json:"resource"`
}

// ClassifyNPUFailure recognizes an explicit allocator error only. Driver
// errors, cancellation and generic failures must not be labelled HBM OOM.
func ClassifyNPUFailure(output string) *Failure {
	if strings.Contains(output, "RuntimeError: NPU out of memory") || strings.Contains(output, "OutOfMemoryError: NPU out of memory") || strings.Contains(output, "NPU out of memory. Tried to allocate") {
		return &Failure{Code: "npu_hbm_oom", Resource: "npu_hbm"}
	}
	return nil
}
