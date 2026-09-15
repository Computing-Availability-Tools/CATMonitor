package ipmi

import (
	"fmt"
	"os"
	"sync"
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

	if len(sensors) != 14 {
		t.Fatalf("expected 14 sensors, got %d", len(sensors))
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
	if len(sensors) != 14 {
		t.Fatalf("expected 14 sensors, got %d", len(sensors))
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
	SetCacheTTL(0) // force expiry: every read is stale-while-revalidate
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	var mu sync.Mutex
	calls := 0
	defaultSrc.fetchSDR = func() (string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return readMock(t, "../../../tests/testdata/ipmitool-sdr-output.txt"), nil
	}
	defaultSrc.fetchSensorGet = nil

	Default().SDR() // cold start: synchronous fetch
	Default().SDR() // expired: serves stale immediately, refresh runs in background

	// The background refresh must land.
	if !waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls >= 2
	}) {
		t.Errorf("background refresh should re-fetch after TTL=0, calls=%d", calls)
	}
	// No goroutine may outlive the test: wait for inflight to drain.
	drainInflight(t)
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
		"CPU1 Core Rem", "CPU2 Core Rem",
	}
	for _, name := range useful {
		if !isUsefulSensor(name) {
			t.Errorf("expected %q to be useful", name)
		}
	}
	notUseful := []string{
		"Power Supply 1", "System Fan",
		"Chassis", "PSU1 Status", "random sensor",
		"CPU1 VRD Temp", "CPU2 VDDQ Temp", "CPU1 VRM Temp",
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

	// TTL=0: expired read serves the stale (discovery) value immediately and
	// refreshes via targeted gets in the background.
	sensors, err := Default().SDR()
	if err != nil {
		t.Fatalf("expired SDR failed: %v", err)
	}
	if sensors[0].Value != 1800 {
		t.Fatalf("expired read should serve the stale value 1800, got %v", sensors[0].Value)
	}

	// Wait for the background targeted refresh to land.
	deadline := time.Now().Add(2 * time.Second)
	for {
		defaultSrc.mu.Lock()
		val := defaultSrc.cached[0].Value
		done := !defaultSrc.inflight && val == 1824
		defaultSrc.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("background targeted refresh never landed (value=%v)", val)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if getCalls != 2 {
		t.Errorf("expected 2 sensor get calls, got %d", getCalls)
	}
	if discoveryCalls != 1 {
		t.Errorf("discovery should not be called again, got %d", discoveryCalls)
	}

	// Next read serves the refreshed targeted values.
	sensors, err = Default().SDR()
	if err != nil {
		t.Fatalf("post-refresh SDR failed: %v", err)
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

	// TTL=0: expired read serves the stale value, the background refresh
	// falls back from all-failed targeted gets to discovery.
	sensors, err := Default().SDR()
	if err != nil {
		t.Fatalf("fallback SDR failed: %v", err)
	}
	if sensors[0].Value != 1800 {
		t.Fatalf("expired read should serve stale value 1800, got %v", sensors[0].Value)
	}

	// Wait for the background refresh (targeted all-fail → discovery) to land.
	deadline := time.Now().Add(2 * time.Second)
	for {
		defaultSrc.mu.Lock()
		val := defaultSrc.cached[0].Value
		done := !defaultSrc.inflight && val == 1900
		defaultSrc.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("background fallback refresh never landed (value=%v)", val)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if discoveryCalls != 1 {
		t.Errorf("fallback should trigger discovery once, got %d", discoveryCalls)
	}
}

// --- SWR (stale-while-revalidate) behaviour ---

// waitFor polls until cond holds or the deadline passes. cond must manage
// its own locking (the source mutex is NOT reentrant). Returns false on
// timeout.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func drainInflight(t *testing.T) {
	t.Helper()
	if !waitFor(t, func() bool {
		defaultSrc.mu.Lock()
		defer defaultSrc.mu.Unlock()
		return !defaultSrc.inflight
	}) {
		t.Fatal("background refresh never finished")
	}
}

// TestSDRStaleWhileRevalidate: an expired read returns the stale value
// immediately (not blocked on the fetch) and a single background refresh
// eventually updates the cache.
func TestSDRStaleWhileRevalidate(t *testing.T) {
	SetCacheTTL(1 * time.Hour)
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	var mu sync.Mutex
	calls := 0
	defaultSrc.fetchSDR = func() (string, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		time.Sleep(150 * time.Millisecond)
		return fmt.Sprintf("Power | %d | Watts | ok\n", 1700+n), nil
	}
	defaultSrc.fetchSensorGet = nil

	// Cold start: synchronous, first value (1701).
	if _, err := Default().SDR(); err != nil {
		t.Fatalf("cold start: %v", err)
	}

	// Force expiry.
	defaultSrc.mu.Lock()
	defaultSrc.cachedAt = time.Now().Add(-time.Hour)
	defaultSrc.mu.Unlock()

	// Expired read must return the stale value WITHOUT blocking on the
	// 150ms fetch.
	start := time.Now()
	sensors, err := Default().SDR()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expired read failed: %v", err)
	}
	if elapsed >= 100*time.Millisecond {
		t.Errorf("expired read blocked on fetch: %v (want <100ms)", elapsed)
	}
	if sensors[0].Value != 1701 {
		t.Errorf("expired read: expected stale 1701, got %v", sensors[0].Value)
	}

	// The background refresh must land with the next value (1702).
	if !waitFor(t, func() bool {
		defaultSrc.mu.Lock()
		defer defaultSrc.mu.Unlock()
		return len(defaultSrc.cached) > 0 && defaultSrc.cached[0].Value == 1702
	}) {
		t.Fatal("background refresh never landed")
	}
	drainInflight(t)
}

// TestSDRInflightDedup: while a slow refresh is in flight, further expired
// reads must not spawn additional refreshes.
func TestSDRInflightDedup(t *testing.T) {
	SetCacheTTL(0) // every read is expired
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	var mu sync.Mutex
	calls := 0
	release := make(chan struct{})
	defaultSrc.fetchSDR = func() (string, error) {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if !first {
			<-release // only the background refresh blocks
		}
		return "Power | 1800 | Watts | ok\n", nil
	}
	defaultSrc.fetchSensorGet = nil

	if _, err := Default().SDR(); err != nil {
		t.Fatalf("cold start: %v", err)
	}

	// Several expired reads while the first background refresh is running.
	for i := 0; i < 5; i++ {
		Default().SDR()
	}

	// Wait for the one background refresh to actually start, then verify no
	// extra fetches were spawned by the other expired reads.
	if !waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls >= 2
	}) {
		mu.Lock()
		n := calls
		mu.Unlock()
		t.Fatalf("background refresh never started, calls=%d", n)
	}
	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 2 { // cold start + exactly one background refresh
		t.Errorf("expected 2 fetches (cold + 1 refresh), got %d", n)
	}

	close(release)
	drainInflight(t)
}

// TestSDRColdStartSingleFlight: concurrent cold starts share ONE fetch.
func TestSDRColdStartSingleFlight(t *testing.T) {
	defer ResetFetcher()

	var mu sync.Mutex
	calls := 0
	defaultSrc.fetchSDR = func() (string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		time.Sleep(150 * time.Millisecond)
		return "Power | 1800 | Watts | ok\n", nil
	}
	defaultSrc.fetchSensorGet = nil

	const n = 8
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sensors, err := Default().SDR()
			if err != nil {
				t.Errorf("cold start SDR: %v", err)
			}
			if len(sensors) != 1 {
				t.Errorf("expected 1 sensor, got %d", len(sensors))
			}
		}()
	}
	wg.Wait()
	// One 150ms fetch shared by 8 callers: total well under 8×150ms.
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Errorf("cold starts serialized: %v", elapsed)
	}

	mu.Lock()
	fetches := calls
	mu.Unlock()
	if fetches != 1 {
		t.Errorf("expected exactly 1 fetch for %d concurrent cold starts, got %d", n, fetches)
	}
}

// TestSDRPartialSensorGetFailure: a single failing sensor get is skipped —
// the rest of the sensors still serve and no discovery fallback runs.
func TestSDRPartialSensorGetFailure(t *testing.T) {
	SetCacheTTL(0)
	defer SetCacheTTL(defaultCacheTTL)
	defer ResetFetcher()

	// Discovery populates the name cache with two sensors.
	defaultSrc.fetchSDR = func() (string, error) {
		return "Power | 1800 | Watts | ok\nInlet Temp | 28 | degrees C | ok\n", nil
	}
	defaultSrc.fetchSensorGet = nil
	Default().SDR()

	// Targeted fetch: one of the two sensors fails.
	discoveryCalls := 0
	defaultSrc.fetchSDR = func() (string, error) {
		discoveryCalls++
		return "Power | 9999 | Watts | ok\n", nil
	}
	defaultSrc.fetchSensorGet = func(name string) (string, error) {
		if name == "Power" {
			return "Sensor Reading         : 1824 (+/- 0) Watts\nStatus                 : ok\n", nil
		}
		return "", &testErr{"timeout"}
	}

	// Expired read: stale first, then the partial refresh lands.
	Default().SDR()
	if !waitFor(t, func() bool {
		defaultSrc.mu.Lock()
		defer defaultSrc.mu.Unlock()
		return len(defaultSrc.cached) == 1 && defaultSrc.cached[0].Value == 1824
	}) {
		t.Fatal("partial refresh never landed")
	}
	drainInflight(t)

	if discoveryCalls != 0 {
		t.Errorf("one failed sensor must not trigger discovery fallback, got %d calls", discoveryCalls)
	}

	// The name cache must stay intact (not wiped by the single failure).
	defaultSrc.mu.Lock()
	names := len(defaultSrc.nameCache)
	defaultSrc.mu.Unlock()
	if names != 2 {
		t.Errorf("name cache should keep both sensors, got %d", names)
	}
}
