package ipmi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTestErrMsg(t *testing.T) {
	e := &testErr{msg: "sensor not found: CPU1 Temp"}
	if got := e.Error(); got != "sensor not found: CPU1 Temp" {
		t.Errorf("Error() = %q", got)
	}
}

// PowerReading with an empty mock and no ipmitool on PATH must surface the
// exec error (the real-fetch branch), not panic or fake a value.
func TestPowerReadingRealFetchUnavailable(t *testing.T) {
	SetMockPower("")
	defer SetMockPower("")
	t.Setenv("PATH", t.TempDir()) // no ipmitool anywhere: exec fails fast

	if _, err := Default().PowerReading(); err == nil {
		t.Error("PowerReading must return the exec error when no mock and no ipmitool")
	}
}

// PowerReading with garbage mock text degrades to 0 (no matching line), nil error.
func TestPowerReadingGarbageMock(t *testing.T) {
	SetMockPower("nothing useful here\n")
	defer SetMockPower("")

	v, err := Default().PowerReading()
	if err != nil {
		t.Fatalf("garbage mock must not error, got %v", err)
	}
	if v != 0 {
		t.Errorf("garbage mock power = %v, want 0", v)
	}
}

// saveNameCache really writes the sensor-name map when a cacheDir is set
// (TestMain disables the dir globally; this test re-enables it on a temp dir
// to cover the write path, then restores the disabled state).
func TestSaveNameCacheWritesFile(t *testing.T) {
	cacheDir := t.TempDir()
	SetCacheDir(cacheDir)
	t.Cleanup(func() { SetCacheDir("") })

	SetMockSDR("Power | 1800 | Watts | ok\nInlet Temp | 28 | degrees C | ok\n")
	defer ResetFetcher()

	if _, err := Default().SDR(); err != nil {
		t.Fatalf("SDR: %v", err)
	}

	path := filepath.Join(cacheDir, sensorMapFilename)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("sensor map not written: %v", err)
	}
	var m struct {
		Updated string   `json:"updated"`
		Names   []string `json:"names"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("sensor map not valid JSON: %v", err)
	}
	if !strings.Contains(m.Updated, "T") { // RFC3339-ish timestamp
		t.Errorf("updated = %q", m.Updated)
	}
	if len(m.Names) != 2 || m.Names[0] != "Power" || m.Names[1] != "Inlet Temp" {
		t.Errorf("names = %v, want [Power Inlet Temp]", m.Names)
	}
}

// SetMockSensorGet drives the targeted-fetch path through the public hook.
// Under SWR the expired read serves the stale discovery values; the
// background refresh lands the targeted values. Waiting for quiescence
// before exiting also avoids leaking the refresh goroutine into later tests.
func TestSetMockSensorGetTargetedFetch(t *testing.T) {
	SetCacheTTL(0)
	defer SetCacheTTL(defaultCacheTTL)

	// Discovery to populate the name cache with Power + Inlet Temp.
	SetMockSDR("Power | 1800 | Watts | ok\nInlet Temp | 28 | degrees C | ok\n")
	defer ResetFetcher()
	if _, err := Default().SDR(); err != nil {
		t.Fatalf("discovery SDR: %v", err)
	}

	// Switch to targeted mode via the public hook.
	SetMockSensorGet(map[string]string{
		"Power":      "Sensor Reading         : 1824 (+/- 0) Watts\nStatus                 : ok\n",
		"Inlet Temp": "Sensor Reading         : 30 (+/- 0) degrees C\nStatus                 : ok\n",
	})

	sensors, err := Default().SDR()
	if err != nil {
		t.Fatalf("targeted SDR: %v", err)
	}
	if len(sensors) != 2 {
		t.Fatalf("targeted fetch returned %d sensors, want 2: %+v", len(sensors), sensors)
	}
	for _, s := range sensors {
		switch s.Name {
		case "Power":
			if s.Value != 1800 {
				t.Errorf("expired read Power = %v, want stale 1800", s.Value)
			}
		case "Inlet Temp":
			if s.Value != 28 {
				t.Errorf("expired read Inlet Temp = %v, want stale 28", s.Value)
			}
		default:
			t.Errorf("unexpected sensor %q", s.Name)
		}
	}

	if !waitFor(t, func() bool {
		defaultSrc.mu.Lock()
		defer defaultSrc.mu.Unlock()
		if defaultSrc.inflight {
			return false
		}
		for _, s := range defaultSrc.cached {
			switch s.Name {
			case "Power":
				if s.Value != 1824 {
					return false
				}
			case "Inlet Temp":
				if s.Value != 30 {
					return false
				}
			}
		}
		return true
	}) {
		t.Fatalf("background targeted refresh never landed: %+v", defaultSrc.cached)
	}
}
