package cli

import (
	"fmt"
	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
	"strings"
)

func extractModelArgs(args []string) ([]string, *workloadapi.ModelRequest, error) {
	rest := []string{}
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(args[i], "=")
		if name != "--npu-model" && name != "--model-action" && name != "--reference-id" {
			rest = append(rest, args[i])
			continue
		}
		if _, exists := values[name]; exists {
			return nil, nil, fmt.Errorf("duplicate %s", name)
		}
		if !inline {
			i++
			if i >= len(args) {
				return nil, nil, fmt.Errorf("%s requires a value", name)
			}
			value = args[i]
		}
		values[name] = value
	}
	if len(values) == 0 {
		return rest, nil, nil
	}
	model := &workloadapi.ModelRequest{Name: values["--npu-model"], Action: values["--model-action"], ReferenceID: values["--reference-id"]}
	if err := model.Validate(); err != nil {
		return nil, nil, err
	}
	return rest, model, nil
}
