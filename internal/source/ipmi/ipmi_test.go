package ipmi

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

func TestParseSDR(t *testing.T) {
	out := readMock(t, "../../../tests/testdata/ipmitool-sdr-output.txt")
	sensors := parseSDR(out)

	if len(sensors) != 6 {
		t.Fatalf("expected 6 sensors, got %d", len(sensors))
	}
	first := sensors[0]
	if first.Name != "CPU1 Temp" {
		t.Errorf("first name: expected 'CPU1 Temp', got %q", first.Name)
	}
	if first.Value != 65.0 {
		t.Errorf("CPU1 Temp value: expected 65.0, got %v", first.Value)
	}
	if first.Unit != "degrees C" {
		t.Errorf("CPU1 Temp unit: got %q", first.Unit)
	}
	if first.Status != "ok" {
		t.Errorf("CPU1 Temp status: got %q", first.Status)
	}

	var pwr *Sensor
	for i := range sensors {
		if sensors[i].Name == "CPU1 Pwr" {
			pwr = &sensors[i]
			break
		}
	}
	if pwr == nil {
		t.Fatal("missing CPU1 Pwr sensor")
	}
	if pwr.Value != 125.5 {
		t.Errorf("CPU1 Pwr value: expected 125.5, got %v", pwr.Value)
	}
	if pwr.Unit != "Watts" {
		t.Errorf("CPU1 Pwr unit: got %q", pwr.Unit)
	}
}

func TestSDRWithMock(t *testing.T) {
	SetMockSDR(readMock(t, "../../../tests/testdata/ipmitool-sdr-output.txt"))
	sensors, err := Default().SDR()
	if err != nil {
		t.Fatalf("SDR with mock failed: %v", err)
	}
	if len(sensors) != 6 {
		t.Fatalf("expected 6 sensors, got %d", len(sensors))
	}
}

func TestSDRCacheHitsWithinTTL(t *testing.T) {
	original := defaultSrc.cacheTTL
	SetCacheTTL(1 * time.Hour)
	defer SetCacheTTL(original)
	defer ResetFetcher()

	calls := 0
	defaultSrc.fetchSDR = func() (string, error) {
		calls++
		return readMock(t, "../../../tests/testdata/ipmitool-sdr-output.txt"), nil
	}
	// Reset cache state so the first call is a guaranteed miss (SDR keys
	// cache validity off cachedAt, not cached != nil).
	defaultSrc.cached = nil
	defaultSrc.cachedAt = time.Time{}

	if _, err := Default().SDR(); err != nil {
		t.Fatalf("first SDR failed: %v", err)
	}
	if _, err := Default().SDR(); err != nil {
		t.Fatalf("second SDR failed: %v", err)
	}
	if calls != 1 {
		t.Errorf("fetcher should be called once (2nd call served from cache), got %d", calls)
	}
}

func TestSDRCacheMissAfterTTL(t *testing.T) {
	SetCacheTTL(0) // expire immediately
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	calls := 0
	defaultSrc.fetchSDR = func() (string, error) {
		calls++
		return readMock(t, "../../../tests/testdata/ipmitool-sdr-output.txt"), nil
	}

	Default().SDR()
	Default().SDR()
	if calls != 2 {
		t.Errorf("with TTL=0 each call should re-fetch, expected 2 calls, got %d", calls)
	}
}

func TestSDRCachesFailure(t *testing.T) {
	// A failing fetcher (e.g. ipmitool installed but no BMC) must be cached:
	// the collector should not exec ipmitool on every 3s cycle.
	SetCacheTTL(1 * time.Hour)
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	calls := 0
	defaultSrc.fetchSDR = func() (string, error) {
		calls++
		return "", errTestFetch
	}
	if _, err := Default().SDR(); err != nil {
		t.Fatalf("failed SDR should return nil,nil (graceful), got err %v", err)
	}
	if _, err := Default().SDR(); err != nil {
		t.Fatalf("second SDR should not error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("failed fetcher should be cached (1 call), got %d", calls)
	}
}

var errTestFetch = &testErr{"simulated fetch failure"}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

func TestParsePowerReading(t *testing.T) {
	out := readMock(t, "../../../tests/testdata/ipmitool-dcmi-power.txt")
	p := parsePowerReading(out)
	if p != 125.5 {
		t.Errorf("expected 125.5 W, got %v", p)
	}
}

func TestPowerReadingMock(t *testing.T) {
	SetMockPower(readMock(t, "../../../tests/testdata/ipmitool-dcmi-power.txt"))
	p, err := Default().PowerReading()
	if err != nil {
		t.Fatalf("PowerReading with mock failed: %v", err)
	}
	if p != 125.5 {
		t.Errorf("expected 125.5, got %v", p)
	}
}

func TestParseSDREmpty(t *testing.T) {
	if got := parseSDR(""); len(got) != 0 {
		t.Errorf("expected 0 sensors for empty input, got %d", len(got))
	}
}

func TestParsePowerReadingMissing(t *testing.T) {
	if got := parsePowerReading("nothing relevant\n"); got != 0 {
		t.Errorf("expected 0 for missing power line, got %v", got)
	}
}
