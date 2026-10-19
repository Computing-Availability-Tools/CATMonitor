//go:build linux

package workloadplugin

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
)

func readOperatorParameters(path, jobID, generation string, selected workloadapi.CaseNames) (*workloadapi.NPUOperatorParameters, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	} // Older images have no execution receipt.
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<10 {
		return nil, fmt.Errorf("operator parameter receipt exceeds 64 KiB")
	}
	var proof workloadapi.NPUOperatorParameters
	if err := json.Unmarshal(data, &proof); err != nil {
		return nil, err
	}
	if err := proof.Validate(jobID, selected); err != nil {
		return nil, err
	}
	if proof.ChipGeneration != generation {
		return nil, fmt.Errorf("operator parameter receipt has a different chip generation")
	}
	return &proof, nil
}
