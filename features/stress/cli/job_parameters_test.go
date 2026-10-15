package cli

import (
	"reflect"
	"testing"
)

func TestPerJobCLIParametersAndAmbiguousInputs(t *testing.T) {
	args, p, err := extractJobParametersArgs([]string{"--bench", "hpl", "--hpl-n=1024", "--hpl-nb", "64", "--hpl-mpi-processes", "1"})
	if err != nil || !reflect.DeepEqual(args, []string{"--bench", "hpl"}) || *p.HPL.N != 1024 || *p.HPL.NB != 64 {
		t.Fatalf("%v %+v %v", args, p, err)
	}
	for _, args := range [][]string{
		{"--job-parameters", "{}", "--stream-threads", "1"},
		{"--stream-threads", "1", "--job-parameters", "{}"},
		{"--model-parameters", "{}", "--model-run-count", "1"},
		{"--model-run-count", "1", "--model-parameters", "{}"},
		{"--stream-threads", "1", "--stream-threads", "2"},
		{"--npu-operator-parameters", `{"matmul":{}},"stream":{"threads":1}`},
	} {
		if _, _, err := extractJobParametersArgs(args); err == nil {
			t.Fatal("ambiguous/injected input accepted", args)
		}
	}
	_, p, err = extractJobParametersArgs([]string{"--npu-operator-parameters", `{"matmul":{"run_count":2,"dtype":["float16"]}}`, "--sdc-loss-tolerance", "0.02"})
	if err != nil || *p.NPUBurn.Operators["matmul"].RunCount != 2 {
		t.Fatal(p, err)
	}
}
