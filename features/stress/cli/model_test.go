package cli

import "testing"

func TestExtractModelArgs(t *testing.T) {
	args, m, err := extractModelArgs([]string{"--bench", "npu_burn", "--npu-model=gpt3", "--model-action", "compare", "--reference-id", "ref1"})
	if err != nil || len(args) != 2 || m.Name != "gpt3" || m.Action != "compare" {
		t.Fatalf("%v %v %v", args, m, err)
	}
	for _, input := range [][]string{{"--npu-model"}, {"--npu-model", "llama2"}, {"--npu-model", "gpt3", "--npu-model", "llama2"}, {"--npu-model=gpt3", "--model-action=compare", "--reference-id=../ref"}} {
		if _, _, err := extractModelArgs(input); err == nil {
			t.Fatal("invalid model request accepted", input)
		}
	}
}
