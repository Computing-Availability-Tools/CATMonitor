//go:build linux

package workloadplugin

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

type npuCaseCatalog struct {
	Cases []struct {
		Name               string   `json:"name"`
		Dtypes             []string `json:"dtypes"`
		Selectable         bool     `json:"selectable"`
		Reason             string   `json:"reason"`
		HardwareValidation string   `json:"hardware_validation"`
	} `json:"cases"`
	Selected            workloadapi.CaseNames `json:"selected"`
	SelectionError      string                `json:"selection_error"`
	ConfigurationSHA256 string                `json:"configuration_sha256"`
}

func hasNPUCaseCatalog() bool {
	switch env("CATMONITOR_NPU_COMPAT_PROFILE") {
	case "portable-runtime-v1", "a2-portable-runtime-v1", "a2-upstream-base-v1",
		"model-sdc-v1", "a2-model-sdc-v1",
		"portable-model-sdc-memory-v1", "a2-upstream-model-sdc-memory-v1":
		return true
	default:
		return false
	}
}

func readNPUCaseCatalog(ctx context.Context) (*npuCaseCatalog, error) {
	path := envDefault("NPU_BURN_PREFLIGHT_EXECUTABLE", "/usr/local/bin/catmonitor-npu-burn-preflight")
	data, err := exec.CommandContext(ctx, path, "--cases-json").Output()
	if err != nil {
		return nil, fmt.Errorf("NPU operator catalog unavailable: %w", err)
	}
	// A failed selection legitimately has an empty selected list; validate it
	// only after reading selection_error, not with the request JSON decoder.
	var wire struct {
		Cases               json.RawMessage `json:"cases"`
		Selected            []string        `json:"selected"`
		SelectionError      string          `json:"selection_error"`
		ConfigurationSHA256 string          `json:"configuration_sha256"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("invalid NPU operator catalog: %w", err)
	}
	c := &npuCaseCatalog{Selected: wire.Selected, SelectionError: wire.SelectionError, ConfigurationSHA256: wire.ConfigurationSHA256}
	if hash, err := hex.DecodeString(c.ConfigurationSHA256); err != nil || len(hash) != 32 {
		return nil, fmt.Errorf("operator catalog has no valid configuration hash")
	}
	if err := json.Unmarshal(wire.Cases, &c.Cases); err != nil {
		return nil, err
	}
	var names []string
	for _, item := range c.Cases {
		names = append(names, item.Name)
	}
	if err := workloadapi.ValidateCases(names); err != nil {
		return nil, err
	}
	if c.SelectionError == "" {
		if err := workloadapi.ValidateCases(c.Selected); err != nil {
			return nil, err
		}
		for _, selected := range c.Selected {
			found := false
			for _, item := range c.Cases {
				found = found || (item.Name == selected && item.Selectable)
			}
			if !found {
				return nil, fmt.Errorf("operator catalog selected unavailable case %q", selected)
			}
		}
	}
	return c, nil
}

func npuCaseProfile(ctx context.Context) ([]workloadapi.ProfileParameter, *workloadapi.AssetCheck) {
	if !hasNPUCaseCatalog() {
		return nil, nil
	}
	check := &workloadapi.AssetCheck{Name: "operator_catalog", Path: "/opt/catmonitor/npuburn-python/ascend_npu_burn/config", Kind: "configuration", Required: true, Status: workloadapi.CheckPass, Message: "operator config/dtype admission passed; hardware validation is NOT_RUN"}
	catalog, err := readNPUCaseCatalog(ctx)
	if err != nil {
		check.Status, check.Message = workloadapi.CheckFail, err.Error()
		return nil, check
	}
	var available []string
	check.SHA256 = catalog.ConfigurationSHA256
	for _, item := range catalog.Cases {
		if item.Selectable {
			available = append(available, item.Name)
		}
	}
	data, _ := json.Marshal(catalog.Cases)
	params := []workloadapi.ProfileParameter{
		parameter("available_cases", "Selectable NPU operators (not hardware certification)", strings.Join(available, ","), ""),
		parameter("selected_cases", "Selected NPU operators", catalog.Selected.String(), ""),
		parameter("case_catalog", "NPU operator admission details", string(data), ""),
	}
	if catalog.SelectionError != "" {
		check.Status, check.Message = workloadapi.CheckFail, catalog.SelectionError
	}
	return params, check
}
