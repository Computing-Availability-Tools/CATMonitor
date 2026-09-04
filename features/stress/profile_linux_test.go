//go:build linux

package stress

import "testing"

func TestDescribeUsesDaemonProfileCache(t *testing.T) {
	executor := &fakeExecutor{}
	manager := testManager(t, executor)
	first, err := manager.Describe("stream")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Describe("stream")
	if err != nil {
		t.Fatal(err)
	}
	executor.mu.Lock()
	calls := executor.describeCalls
	executor.mu.Unlock()
	if calls != 1 {
		t.Fatalf("describe calls=%d, want 1", calls)
	}
	if first.ConfigurationSHA256 == "" || first.ConfigurationSHA256 != second.ConfigurationSHA256 {
		t.Fatalf("profile configuration hash is not stable: first=%q second=%q", first.ConfigurationSHA256, second.ConfigurationSHA256)
	}
	if first.Executor == "" || first.Container != "catmonitor-stress-cpu" || first.Plugin != "stream" {
		t.Fatalf("deployment binding missing from profile: %+v", first)
	}
}

func TestValidateDescribeRejectsFailedRequiredAssetWithPassingPreflight(t *testing.T) {
	profile := validProfile("stream")
	profile.Assets = []AssetCheck{{Name: "binary", Path: "/fixed", Kind: "executable", Required: true, Status: CheckFail}}
	if err := validateDescribeProfile("stream", profile); err == nil {
		t.Fatal("expected invalid profile to be rejected")
	}
}
