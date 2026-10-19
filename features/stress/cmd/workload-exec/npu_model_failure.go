package main

import (
	"fmt"
	"strings"
)

func modelFailureMessage(failure error, output string) string {
	if failure == nil {
		return ""
	}
	const marker = "ERROR: model SDC: NPU_HBM_OOM:"
	if index := strings.LastIndex(output, marker); index >= 0 {
		detail := strings.TrimSpace(strings.SplitN(output[index+len(marker):], "\n", 2)[0])
		if len(detail) > 1024 {
			detail = detail[:1024]
		}
		return "NPU HBM OOM: " + detail
	}
	return fmt.Sprintf("workload command failed: %v", failure)
}
