// Package ipmi provides a data source that queries an external `ipmitool`
// command for sensor readings (SDR) and instantaneous power (DCMI).
//
// Two-level cache strategy:
//   - Result cache (10s TTL): avoids any ipmitool calls on every poll tick.
//   - Name cache (24h TTL): discovered sensor names from a full `ipmitool sensor`
//     scan. When valid, subsequent calls use `ipmitool sensor get "name"` per
//     sensor (<1s each) instead of the full scan (~40s).
//
// On first run (or name cache expired): full `ipmitool sensor` → parse →
// filter useful sensors → cache names → persist to disk.
// On subsequent runs: `ipmitool sensor get "name"` per cached name.
// If any targeted query fails: fall back to full scan and refresh names.
package ipmi

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sensor holds one SDR reading.
type Sensor struct {
	Name   string
	Value  float64
	Unit   string
	Status string
}

const (
	defaultCacheTTL    = 10 * time.Second
	nameCacheTTL       = 24 * time.Hour
	execTimeout        = 60 * time.Second
	sensorGetTimeout   = 5 * time.Second
	defaultCacheDir    = "features/web/data"
	sensorMapFilename  = "ipmi_sensor_map.json"
)

type Source interface {
	SDR() ([]Sensor, error)
	PowerReading() (float64, error)
	Available() bool
}

type sdrFetcher = func() (string, error)
type sensorGetFetcher = func(name string) (string, error)

func realFetchSDR() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ipmitool", "sensor").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func realFetchSensorGet(name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sensorGetTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ipmitool", "sensor", "get", name).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type defaultSource struct {
	mu             sync.Mutex
	cached         []Sensor
	cachedAt       time.Time
	cacheTTL       time.Duration
	fetchSDR       sdrFetcher
	fetchSensorGet sensorGetFetcher
	nameCache      []string
	nameCacheAt    time.Time
	cacheDir       string
	mockPower      string
}

var defaultSrc = &defaultSource{
	cacheTTL:       defaultCacheTTL,
	fetchSDR:       realFetchSDR,
	fetchSensorGet: realFetchSensorGet,
	cacheDir:       defaultCacheDir,
}

func Default() Source { return defaultSrc }

func SetCacheTTL(d time.Duration) { defaultSrc.cacheTTL = d }

func SetCacheDir(dir string) { defaultSrc.cacheDir = dir }

func SetMockSDR(s string) {
	defaultSrc.fetchSDR = func() (string, error) { return s, nil }
	defaultSrc.fetchSensorGet = nil
	defaultSrc.cached = nil
	defaultSrc.cachedAt = time.Time{}
	defaultSrc.nameCache = nil
	defaultSrc.nameCacheAt = time.Time{}
}

func SetMockSensorGet(m map[string]string) {
	defaultSrc.fetchSensorGet = func(name string) (string, error) {
		if out, ok := m[name]; ok {
			return out, nil
		}
		return "", &testErr{"sensor not found: " + name}
	}
}

func ResetFetcher() {
	defaultSrc.fetchSDR = realFetchSDR
	defaultSrc.fetchSensorGet = realFetchSensorGet
	defaultSrc.cached = nil
	defaultSrc.cachedAt = time.Time{}
	defaultSrc.nameCache = nil
	defaultSrc.nameCacheAt = time.Time{}
}

func SetMockPower(s string) { defaultSrc.mockPower = s }

func (s *defaultSource) Available() bool {
	_, err := exec.LookPath("ipmitool")
	return err == nil
}

func (s *defaultSource) SDR() ([]Sensor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Result cache valid → return immediately
	if !s.cachedAt.IsZero() && time.Since(s.cachedAt) < s.cacheTTL {
		return s.cached, nil
	}

	// 2. Name cache valid → targeted fetch
	if !s.nameCacheAt.IsZero() && time.Since(s.nameCacheAt) < nameCacheTTL && len(s.nameCache) > 0 {
		sensors := s.targetedFetch()
		if sensors != nil {
			s.cached = sensors
			s.cachedAt = time.Now()
			return s.cached, nil
		}
		// Targeted failed → fall through to discovery
	}

	// 3. Full discovery
	return s.discovery()
}

func (s *defaultSource) targetedFetch() []Sensor {
	if s.fetchSensorGet == nil {
		return nil
	}
	sensors := make([]Sensor, 0, len(s.nameCache))
	for _, name := range s.nameCache {
		out, err := s.fetchSensorGet(name)
		if err != nil {
			s.nameCache = nil
			s.nameCacheAt = time.Time{}
			return nil
		}
		sensors = append(sensors, parseSensorGet(name, out))
	}
	return sensors
}

func (s *defaultSource) discovery() ([]Sensor, error) {
	out, err := s.fetchSDR()
	s.cachedAt = time.Now()
	if err != nil {
		s.cached = nil
		return nil, nil
	}
	all := parseSDR(out)
	s.cached = all

	var names []string
	for _, sensor := range all {
		if isUsefulSensor(sensor.Name) {
			names = append(names, sensor.Name)
		}
	}
	s.nameCache = names
	s.nameCacheAt = time.Now()
	s.saveNameCache()

	return s.cached, nil
}

func (s *defaultSource) saveNameCache() {
	if s.cacheDir == "" || len(s.nameCache) == 0 {
		return
	}
	path := filepath.Join(s.cacheDir, sensorMapFilename)
	_ = os.MkdirAll(s.cacheDir, 0o755)
	m := struct {
		Updated string   `json:"updated"`
		Names   []string `json:"names"`
	}{
		Updated: s.nameCacheAt.Format(time.RFC3339),
		Names:   s.nameCache,
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(path, data, 0o644)
}

func (s *defaultSource) loadNameCache() {
	if s.cacheDir == "" {
		return
	}
	path := filepath.Join(s.cacheDir, sensorMapFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var m struct {
		Updated string   `json:"updated"`
		Names   []string `json:"names"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	t, err := time.Parse(time.RFC3339, m.Updated)
	if err != nil {
		return
	}
	if time.Since(t) < nameCacheTTL && len(m.Names) > 0 {
		s.nameCache = m.Names
		s.nameCacheAt = t
	}
}

func (s *defaultSource) PowerReading() (float64, error) {
	text := s.mockPower
	if text == "" {
		ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ipmitool", "dcmi", "power", "reading").Output()
		if err != nil {
			return 0, err
		}
		text = string(out)
	}
	return parsePowerReading(text), nil
}

// isUsefulSensor reports whether a sensor name matches any of the 9 ipmi
// metric patterns used by the chassis, cpu, and memory collectors.
func isUsefulSensor(name string) bool {
	l := strings.ToLower(name)
	switch {
	case l == "power":
		return true
	case l == "inlet temp":
		return true
	case l == "outlet temp":
		return true
	case strings.Contains(l, "fan") && strings.Contains(l, "speed"):
		return true
	case strings.Contains(l, "fan") && strings.Contains(l, "power"):
		return true
	case strings.Contains(l, "cpu") && strings.Contains(l, "temp"):
		return true
	case strings.Contains(l, "mem") && strings.Contains(l, "temp"):
		return true
	case strings.Contains(l, "cpu") && strings.Contains(l, "pwr"):
		return true
	case strings.Contains(l, "mem") && strings.Contains(l, "pwr"):
		return true
	}
	return false
}

// parseSensorGet parses `ipmitool sensor get "name"` output:
//
//	Sensor ID              : Power (0x25)
//	Sensor Reading         : 1824 (+/- 0) Watts
//	Status                 : ok
func parseSensorGet(name, output string) Sensor {
	s := Sensor{Name: name}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Sensor Reading") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) < 2 {
				continue
			}
			val := strings.TrimSpace(parts[1])
			fields := strings.Fields(val)
			if len(fields) >= 1 {
				if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
					s.Value = v
				}
			}
			if idx := strings.Index(val, ")"); idx >= 0 {
				s.Unit = strings.TrimSpace(val[idx+1:])
			}
		} else if strings.HasPrefix(line, "Status") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				s.Status = strings.TrimSpace(parts[1])
			}
		}
	}
	return s
}

func parseSDR(out string) []Sensor {
	var sensors []Sensor
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		reading := strings.Fields(parts[1])
		if len(reading) < 1 {
			continue
		}
		val, _ := strconv.ParseFloat(reading[0], 64)
		var unit, status string
		if len(parts) >= 4 {
			unit = strings.TrimSpace(parts[2])
			status = strings.TrimSpace(parts[3])
		} else {
			unit = strings.Join(reading[1:], " ")
			status = strings.TrimSpace(parts[2])
		}
		sensors = append(sensors, Sensor{Name: name, Value: val, Unit: unit, Status: status})
	}
	return sensors
}

func parsePowerReading(text string) float64 {
	for _, line := range strings.Split(text, "\n") {
		l := strings.ToLower(line)
		if !strings.Contains(l, "instantaneous power reading") {
			continue
		}
		fields := strings.Fields(line)
		for _, f := range fields {
			if v, err := strconv.ParseFloat(strings.TrimRight(f, ","), 64); err == nil {
				return v
			}
		}
	}
	return 0
}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }
