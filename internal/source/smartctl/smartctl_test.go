package smartctl

import (
	"os"
	"testing"
	"time"
)

func readMock(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(data)
}

func TestHealthMockInject(t *testing.T) {
	want := readMock(t, "../../../tests/testdata/smartctl-health-output.txt")
	SetFetcher(func(dev string) (string, error) { return want, nil })
	defer ResetFetcher()

	got, err := Default().Health("sda")
	if err != nil {
		t.Fatalf("Health with mock failed: %v", err)
	}
	if got != want {
		t.Errorf("expected %d bytes, got %d", len(want), len(got))
	}
}

func TestHealthCacheHitsWithinTTL(t *testing.T) {
	SetCacheTTL(1 * time.Hour)
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	calls := 0
	want := readMock(t, "../../../tests/testdata/smartctl-health-output.txt")
	SetFetcher(func(dev string) (string, error) {
		calls++
		return want, nil
	})

	Default().Health("sda")
	Default().Health("sda")
	if calls != 1 {
		t.Errorf("fetcher should be called once per device (cache hit), got %d", calls)
	}
}

func TestHealthCachePerDevice(t *testing.T) {
	SetCacheTTL(1 * time.Hour)
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	calls := map[string]int{}
	want := readMock(t, "../../../tests/testdata/smartctl-health-output.txt")
	SetFetcher(func(dev string) (string, error) {
		calls[dev]++
		return want, nil
	})

	Default().Health("sda")
	Default().Health("sdb")
	Default().Health("sda")
	if calls["sda"] != 1 || calls["sdb"] != 1 {
		t.Errorf("expected 1 call per device, got sda=%d sdb=%d", calls["sda"], calls["sdb"])
	}
}

func TestHealthCacheMissAfterTTL(t *testing.T) {
	SetCacheTTL(0)
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	calls := 0
	want := readMock(t, "../../../tests/testdata/smartctl-health-output.txt")
	SetFetcher(func(dev string) (string, error) {
		calls++
		return want, nil
	})

	Default().Health("sda")
	Default().Health("sda")
	if calls != 2 {
		t.Errorf("with TTL=0 each call should re-fetch, expected 2, got %d", calls)
	}
}

func TestHealthCachesFailure(t *testing.T) {
	SetCacheTTL(1 * time.Hour)
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	calls := 0
	SetFetcher(func(dev string) (string, error) {
		calls++
		return "", errFail
	})
	if _, err := Default().Health("sda"); err != nil {
		t.Fatalf("failed fetch should return nil,nil (graceful), got %v", err)
	}
	if _, err := Default().Health("sda"); err != nil {
		t.Fatalf("second call should not error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("failed fetcher should be cached (1 call), got %d", calls)
	}
}

var errFail = &testErr{"simulated smartctl failure"}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }
