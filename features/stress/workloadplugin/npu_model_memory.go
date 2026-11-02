package workloadplugin

import (
	"fmt"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

// This proof is a static lower-bound screen, never a hardware training PASS.
type modelMemoryProof struct {
	Status        string `json:"status"`
	Message       string `json:"message"`
	Scope         string `json:"scope"`
	RequiredBytes int64  `json:"required_bytes"`
	FullPeakKnown bool   `json:"full_peak_known"`
	Devices       []struct {
		Device     int    `json:"device"`
		FreeBytes  int64  `json:"free_bytes"`
		TotalBytes int64  `json:"total_bytes"`
		Status     string `json:"status"`
	} `json:"devices"`
}

func validateModelMemory(proof *modelMemoryProof, ids workloadapi.DeviceIDs) error {
	if proof == nil || len(ids) == 0 || proof.Scope != "basic_lower_bound" || proof.RequiredBytes <= 0 || proof.FullPeakKnown || len(proof.Devices) != len(ids) {
		return fmt.Errorf("model basic HBM screening is missing or invalid")
	}
	seen := map[int]bool{}
	for _, row := range proof.Devices {
		selected := false
		for _, id := range ids {
			selected = selected || row.Device == id
		}
		if !selected || seen[row.Device] || row.FreeBytes < 0 || row.TotalBytes <= 0 || row.FreeBytes > row.TotalBytes {
			return fmt.Errorf("invalid model HBM capacity proof for logical device %d", row.Device)
		}
		seen[row.Device] = true
		if row.FreeBytes < proof.RequiredBytes {
			return fmt.Errorf("insufficient basic NPU HBM on logical device %d: free %.2f GiB, basic minimum %.2f GiB", row.Device, float64(row.FreeBytes)/(1<<30), float64(proof.RequiredBytes)/(1<<30))
		}
		if row.Status != "pass" {
			return fmt.Errorf("model basic HBM screening did not pass on logical device %d", row.Device)
		}
	}
	if proof.Status != "pass" {
		return fmt.Errorf("model basic HBM screening did not pass")
	}
	return nil
}
