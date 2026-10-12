package workloadapi

import (
	"strings"
	"testing"
)

func TestModelRequestAndResult(t *testing.T) {
	m := &ModelRequest{Name: "gpt3", Action: "compare", ReferenceID: "ref1"}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	r := &ModelResult{JobID: "job1", Model: "gpt3", Action: "compare", ReferenceID: "ref1", Devices: DeviceIDs{0, 1}, Outcome: "sdc_passed", ExpectedSteps: 3, CheckedSteps: 6, CSV: "/results/current.csv"}
	if err := r.Validate(m, DeviceIDs{0, 1}, "job1"); err != nil {
		t.Fatal(err)
	}
	r.MemoryMode = "auto"
	if r.Validate(m, DeviceIDs{0, 1}, "job1") == nil {
		t.Fatal("unrecorded automatic memory downgrade accepted")
	}
	r.MemoryMode = "checkpointed"
	if err := r.Validate(m, DeviceIDs{0, 1}, "job1"); err != nil {
		t.Fatal(err)
	}
	r.CheckedSteps = 3
	if err := r.Validate(m, DeviceIDs{0, 1}, "job1"); err == nil {
		t.Fatal("partial comparison accepted")
	}
	for _, id := range []string{"", "../ref", "/root/ref", "a;b"} {
		m.ReferenceID = id
		if m.Validate() == nil {
			t.Fatal("unsafe reference accepted")
		}
	}
	m.ReferenceID = "ref1"
	m.Action = "create_reference"
	r.Action = "create_reference"
	r.Outcome = "reference_generated"
	r.CheckedSteps = 0
	r.SavedSteps = 3
	r.Devices = DeviceIDs{0}
	if err := r.Validate(m, DeviceIDs{0}, "job1"); err != nil {
		t.Fatal(err)
	}
	r.Outcome = "sdc_passed"
	if err := r.Validate(m, DeviceIDs{0}, "job1"); err == nil {
		t.Fatal("reference generation mislabeled as SDC pass")
	}
}

func TestModelCSVBelongsToJobAndCoversEveryDevice(t *testing.T) {
	r := &ModelResult{JobID: "job1", Model: "gpt3", Action: "compare", ReferenceID: "ref1", Devices: DeviceIDs{0, 1}, ExpectedSteps: 3}
	header := "job_id,action,reference_id,task,device_id,run_count,result,err_count,exetime,checked_steps,saved_steps\n"
	rows := "job1,compare,ref1,gpt3,0,2,PASS,0,1,3,0\njob1,compare,ref1,gpt3,1,2,PASS,0,1,3,0\n"
	if err := ValidateModelCSV(strings.NewReader(header+rows), r); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{header, header + strings.ReplaceAll(rows, "job1", "stale"), header + strings.ReplaceAll(rows, "PASS", "FAIL"), header + strings.ReplaceAll(rows, ",1,3,0", ",1,1,0"), header + strings.Split(rows, "\n")[0] + "\n"} {
		if err := ValidateModelCSV(strings.NewReader(body), r); err == nil {
			t.Fatal("invalid CSV accepted")
		}
	}
}
