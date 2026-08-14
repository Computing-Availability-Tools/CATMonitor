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
	defer ResetFetcher()
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
	defaultSrc.fetchSensorGet = nil
	defaultSrc.cached = nil
	defaultSrc.cachedAt = time.Time{}
	defaultSrc.nameCache = nil
	defaultSrc.nameCacheAt = time.Time{}

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
	SetCacheTTL(0)
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	calls := 0
	defaultSrc.fetchSDR = func() (string, error) {
		calls++
		return readMock(t, "../../../tests/testdata/ipmitool-sdr-output.txt"), nil
	}
	defaultSrc.fetchSensorGet = nil

	Default().SDR()
	Default().SDR()
	if calls != 2 {
		t.Errorf("with TTL=0 each call should re-fetch, expected 2 calls, got %d", calls)
	}
}

func TestSDRCachesFailure(t *testing.T) {
	SetCacheTTL(1 * time.Hour)
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	calls := 0
	defaultSrc.fetchSDR = func() (string, error) {
		calls++
		return "", errTestFetch
	}
	defaultSrc.fetchSensorGet = nil
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

func TestParseSensorGet(t *testing.T) {
	out := `Locating sensor record...
Sensor ID              : Power (0x25)
Entity ID              : 7.96
Sensor Type (Threshold): Other
Sensor Reading         : 1824 (+/- 0) Watts
Status                 : ok
Lower Non-Recoverable  : na
`
	s := parseSensorGet("Power", out)
	if s.Name != "Power" {
		t.Errorf("name: expected 'Power', got %q", s.Name)
	}
	if s.Value != 1824 {
		t.Errorf("value: expected 1824, got %v", s.Value)
	}
	if s.Unit != "Watts" {
		t.Errorf("unit: expected 'Watts', got %q", s.Unit)
	}
	if s.Status != "ok" {
		t.Errorf("status: expected 'ok', got %q", s.Status)
	}
}

func TestParseSensorGetDegreesC(t *testing.T) {
	out := `Sensor Reading         : 28 (+/- 0) degrees C
Status                 : ok
`
	s := parseSensorGet("Inlet Temp", out)
	if s.Value != 28 {
		t.Errorf("value: expected 28, got %v", s.Value)
	}
	if s.Unit != "degrees C" {
		t.Errorf("unit: expected 'degrees C', got %q", s.Unit)
	}
}

func TestParseSensorGetRPM(t *testing.T) {
	out := `Sensor Reading         : 9450 (+/- 0) RPM
Status                 : ok
`
	s := parseSensorGet("FAN1 F Speed", out)
	if s.Value != 9450 {
		t.Errorf("value: expected 9450, got %v", s.Value)
	}
	if s.Unit != "RPM" {
		t.Errorf("unit: expected 'RPM', got %q", s.Unit)
	}
}

func TestParseSensorGetNA(t *testing.T) {
	out := `Sensor Reading         : na
Status                 : na
`
	s := parseSensorGet("Missing", out)
	if s.Value != 0 {
		t.Errorf("value: expected 0, got %v", s.Value)
	}
}

func TestIsUsefulSensor(t *testing.T) {
	useful := []string{
		"Power", "Inlet Temp", "Outlet Temp",
		"FAN1 F Speed", "FAN1 R Speed", "FAN8 Speed",
		"CPU1 Temp", "CPU1 MEM Temp", "CPU1 Pwr", "MEM1 Pwr",
	}
	for _, name := range useful {
		if !isUsefulSensor(name) {
			t.Errorf("expected %q to be useful", name)
		}
	}
	notUseful := []string{
		"Power Supply 1", "System Fan", "CPU1 Core Rem",
		"Chassis", "PSU1 Status", "random sensor",
	}
	for _, name := range notUseful {
		if isUsefulSensor(name) {
			t.Errorf("expected %q to NOT be useful", name)
		}
	}
}

func TestSDRTargetedFetch(t *testing.T) {
	SetCacheTTL(0) // force re-fetch every call
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	// First call: discovery populates name cache
	discoveryCalls := 0
	defaultSrc.fetchSDR = func() (string, error) {
		discoveryCalls++
		return "Power | 1800 | Watts | ok\nInlet Temp | 28 | degrees C | ok\n", nil
	}
	defaultSrc.fetchSensorGet = nil // disable targeted on first call

	Default().SDR()
	if discoveryCalls != 1 {
		t.Fatalf("first call should trigger discovery, got %d", discoveryCalls)
	}

	// Now enable targeted fetch
	getCalls := 0
	defaultSrc.fetchSensorGet = func(name string) (string, error) {
		getCalls++
		switch name {
		case "Power":
			return "Sensor Reading         : 1824 (+/- 0) Watts\nStatus                 : ok\n", nil
		case "Inlet Temp":
			return "Sensor Reading         : 30 (+/- 0) degrees C\nStatus                 : ok\n", nil
		default:
			return "", &testErr{"not found"}
		}
	}

	sensors, err := Default().SDR()
	if err != nil {
		t.Fatalf("targeted SDR failed: %v", err)
	}
	if getCalls != 2 {
		t.Errorf("expected 2 sensor get calls, got %d", getCalls)
	}
	if discoveryCalls != 1 {
		t.Errorf("discovery should not be called again, got %d", discoveryCalls)
	}
	if len(sensors) != 2 {
		t.Fatalf("expected 2 sensors, got %d", len(sensors))
	}
	if sensors[0].Name != "Power" || sensors[0].Value != 1824 {
		t.Errorf("Power sensor: got %+v", sensors[0])
	}
}

func TestSDRFallbackOnSensorGetFailure(t *testing.T) {
	SetCacheTTL(0)
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	// Discovery populates name cache
	defaultSrc.fetchSDR = func() (string, error) {
		return "Power | 1800 | Watts | ok\n", nil
	}
	defaultSrc.fetchSensorGet = nil
	Default().SDR()

	// Targeted fetch fails → fallback to discovery
	discoveryCalls := 0
	defaultSrc.fetchSDR = func() (string, error) {
		discoveryCalls++
		return "Power | 1900 | Watts | ok\n", nil
	}
	defaultSrc.fetchSensorGet = func(name string) (string, error) {
		return "", &testErr{"sensor not found"}
	}

	sensors, err := Default().SDR()
	if err != nil {
		t.Fatalf("fallback SDR failed: %v", err)
	}
	if discoveryCalls != 1 {
		t.Errorf("fallback should trigger discovery once, got %d", discoveryCalls)
	}
	if len(sensors) != 1 {
		t.Fatalf("expected 1 sensor from fallback, got %d", len(sensors))
	}
	if sensors[0].Value != 1900 {
		t.Errorf("expected updated value 1900, got %v", sensors[0].Value)
	}
}
