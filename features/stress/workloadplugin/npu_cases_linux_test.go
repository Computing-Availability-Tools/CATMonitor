//go:build linux

package workloadplugin

import (
	"context"
	"encoding/json"
	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/workloadapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNPUCaseCatalogAdmission(t *testing.T) {
	t.Setenv("CATMONITOR_NPU_COMPAT_PROFILE", "a2-portable-runtime-v1")
	for _, tc := range []struct {
		data string
		ok   bool
	}{
		{`{"cases":[{"name":"matmul","selectable":true}],"selected":["matmul"],"selection_error":""}`, true},
		{`{"cases":[{"name":"matmul","selectable":true}],"selected":[],"selection_error":"unsupported dtype"}`, false},
		{`{"cases":[],"selected":[]}`, false},
		{`not-json`, false},
	} {
		var data map[string]any
		if json.Unmarshal([]byte(tc.data), &data) == nil {
			data["configuration_sha256"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			encoded, _ := json.Marshal(data)
			tc.data = string(encoded)
		}
		p := executableFixture(t, t.TempDir(), "preflight", "#!/bin/sh\n[ \"$1\" = --cases-json ] || exit 1\nprintf '%s\\n' '"+tc.data+"'\n")
		t.Setenv("NPU_BURN_PREFLIGHT_EXECUTABLE", p)
		params, check := npuCaseProfile(context.Background())
		if (check.Status == workloadapi.CheckPass) != tc.ok {
			t.Fatalf("%+v %s", check, tc.data)
		}
		if tc.ok && len(params) != 3 {
			t.Fatal(params)
		}
	}
}

func TestFullBaseA2CatalogProfile(t *testing.T) {
	t.Setenv("CATMONITOR_NPU_COMPAT_PROFILE", "a2-upstream-base-v1")
	if !hasNPUCaseCatalog() {
		t.Fatal("full-base profile must retain dtype and generation admission")
	}
}

func TestNPUEachSelectedOperatorMustCoverEachDevice(t *testing.T) {
	header := "task,device_id,case_idx,run_count,stream_count,exetime,err_count,result\n"
	rows := []string{"matmul,0,0,20,1,1,0,PASS\n", "matmul,1,0,20,1,1,0,PASS\n", "conv2d,0,0,20,1,1,0,PASS\n", "conv2d,1,0,20,1,1,0,PASS\n"}
	for omit := -1; omit < len(rows); omit++ {
		data := header
		for i, row := range rows {
			if i != omit {
				data += row
			}
		}
		p := filepath.Join(t.TempDir(), "result.csv")
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := summarizeSelectedCSV(p, []string{"matmul", "conv2d"}, "0", "1")
		if (err == nil) != (omit == -1) {
			t.Fatalf("omit=%d err=%v", omit, err)
		}
	}
}

func TestNPUCSVGroupsCountRowsNotOperatorsOrIterations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.csv")
	data := "task,device_id,case_idx,run_count,stream_count,exetime,err_count,result\n" +
		"matmul_atomic,1,0,100,1,2.5,0,PASS\n" +
		"matmul,0,0,100,1,1.25,0,PASS\n" +
		"matmul,0,1,100,1,0.75,0,PASS\n" +
		"matmul_atomic,0,0,100,1,3,0,PASS\n" +
		"matmul,1,0,100,1,4,0,PASS\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := summarizeSelectedCSV(path, []string{"matmul", "matmul_atomic"}, "0", "1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "devices=2 cases=5 passed=5 failed=0 errors=0 case_time_seconds=11.500000\n") {
		t.Fatal(output)
	}
	const prefix = "CATMONITOR_NPU_BURN_CASE_GROUPS="
	parts := strings.Split(output, "\n"+prefix)
	if len(parts) != 2 {
		t.Fatal(output)
	}
	var groups []npuCSVGroup
	if err := json.Unmarshal([]byte(parts[1]), &groups); err != nil {
		t.Fatal(err)
	}
	want := []npuCSVGroup{
		{Device: 0, Operator: "matmul", Cases: 2, Passed: 2, CaseTimeSeconds: 2},
		{Device: 0, Operator: "matmul_atomic", Cases: 1, Passed: 1, CaseTimeSeconds: 3},
		{Device: 1, Operator: "matmul", Cases: 1, Passed: 1, CaseTimeSeconds: 4},
		{Device: 1, Operator: "matmul_atomic", Cases: 1, Passed: 1, CaseTimeSeconds: 2.5},
	}
	if len(groups) != len(want) {
		t.Fatal(groups)
	}
	for i := range want {
		if groups[i] != want[i] {
			t.Fatalf("group %d=%+v, want %+v", i, groups[i], want[i])
		}
	}
	// SDC errors and FAIL rows remain failures, even with a readable breakdown.
	data = strings.Replace(data, "2.5,0,PASS", "2.5,2,FAIL", 1)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	output, err = summarizeSelectedCSV(path, []string{"matmul", "matmul_atomic"}, "0", "1")
	if err == nil || !strings.Contains(output, `"device":1,"operator":"matmul_atomic","cases":1,"passed":0,"failed":1,"errors":2`) {
		t.Fatalf("output=%s, err=%v", output, err)
	}
}
