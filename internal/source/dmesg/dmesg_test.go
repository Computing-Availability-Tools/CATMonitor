package dmesg

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

func TestTextMockInject(t *testing.T) {
	want := readMock(t, "../../../tests/testdata/dmesg-oom-sample.txt")
	SetMock(want)
	defer ResetFetcher()

	got, err := Default().Text()
	if err != nil {
		t.Fatalf("Text with mock failed: %v", err)
	}
	if got != want {
		t.Errorf("Text mock: expected %d bytes, got %d", len(want), len(got))
	}
}

func TestTextCacheHitsWithinTTL(t *testing.T) {
	SetCacheTTL(1 * time.Hour)
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	calls := 0
	want := readMock(t, "../../../tests/testdata/dmesg-oom-sample.txt")
	defaultSrc.fetch = func() (string, error) {
		calls++
		return want, nil
	}
	defaultSrc.cachedAt = time.Time{}

	if _, err := Default().Text(); err != nil {
		t.Fatalf("first Text failed: %v", err)
	}
	if _, err := Default().Text(); err != nil {
		t.Fatalf("second Text failed: %v", err)
	}
	if calls != 1 {
		t.Errorf("fetcher should be called once (cache hit on 2nd), got %d", calls)
	}
}

func TestTextCacheMissAfterTTL(t *testing.T) {
	SetCacheTTL(0) // expire immediately
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	calls := 0
	want := readMock(t, "../../../tests/testdata/dmesg-oom-sample.txt")
	defaultSrc.fetch = func() (string, error) {
		calls++
		return want, nil
	}

	Default().Text()
	Default().Text()
	if calls != 2 {
		t.Errorf("with TTL=0 each call should re-fetch, expected 2, got %d", calls)
	}
}

func TestTextCachesFailure(t *testing.T) {
	// A failing fetcher (e.g. dmesg_restrict without root) must be cached:
	// the collector should not exec dmesg every cycle.
	SetCacheTTL(1 * time.Hour)
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	calls := 0
	defaultSrc.fetch = func() (string, error) {
		calls++
		return "", errFetchFail
	}
	defaultSrc.cachedAt = time.Time{}

	if _, err := Default().Text(); err != nil {
		t.Fatalf("failed fetch should return nil,nil (graceful), got %v", err)
	}
	if _, err := Default().Text(); err != nil {
		t.Fatalf("second Text should not error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("failed fetcher should be cached (1 call), got %d", calls)
	}
}

var errFetchFail = &testErr{"simulated dmesg failure"}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }
