// Package ipmi provides a data source that queries an external `ipmitool`
// command for sensor readings (SDR) and instantaneous power (DCMI).
//
// Caching is stale-while-revalidate (SWR) with a 10s TTL: expired reads
// return the stale value immediately and trigger a single background
// refresh, so callers (the chassis, cpu and memory collectors poll every
// few seconds) never block on an ipmitool exec after the initial cold
// start. A failed refresh keeps the stale value; the next expired read
// retries. Safe for concurrent use: the mutex only guards the cache
// fields; execs run outside the lock.
//
// Fetch strategy: a name cache (24h TTL, persisted to disk) holds the
// useful sensor names discovered by one full `ipmitool sensor` scan.
// While valid, refreshes use one `ipmitool sensor get "name"` per sensor
// (skipping individual failures); a full discovery scan runs only when
// the name cache is empty or every targeted get failed.
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
	execTimeout        = 120 * time.Second
	// sensorGetTimeout bounds one `ipmitool sensor get` call. Slow BMCs can
	// take 5-7s per sensor (measured on a 910B4 host under load); 5s killed
	// healthy sensors and forced a full-scan fallback every cycle.
	sensorGetTimeout   = 15 * time.Second
	defaultCacheDir    = "/var/lib/catmonitor"
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

	inflight bool         // a background refresh or cold-start fetch is running
	coldDone chan struct{} // closed when the in-flight cold start finishes
	gen      uint64       // bumped on reset; in-flight fetches discard their writes
}

var defaultSrc = &defaultSource{
	cacheTTL:       defaultCacheTTL,
	fetchSDR:       realFetchSDR,
	fetchSensorGet: realFetchSensorGet,
	cacheDir:       defaultCacheDir,
}

func Default() Source { return defaultSrc }

func SetCacheTTL(d time.Duration) {
	defaultSrc.mu.Lock()
	defer defaultSrc.mu.Unlock()
	defaultSrc.cacheTTL = d
}

func SetCacheDir(dir string) {
	defaultSrc.mu.Lock()
	defer defaultSrc.mu.Unlock()
	defaultSrc.cacheDir = dir
}

func SetMockSDR(s string) {
	defaultSrc.mu.Lock()
	defer defaultSrc.mu.Unlock()
	defaultSrc.fetchSDR = func() (string, error) { return s, nil }
	defaultSrc.fetchSensorGet = nil
	defaultSrc.resetLocked()
}

func SetMockSensorGet(m map[string]string) {
	defaultSrc.mu.Lock()
	defer defaultSrc.mu.Unlock()
	defaultSrc.fetchSensorGet = func(name string) (string, error) {
		if out, ok := m[name]; ok {
			return out, nil
		}
		return "", &testErr{"sensor not found: " + name}
	}
}

func ResetFetcher() {
	defaultSrc.mu.Lock()
	defer defaultSrc.mu.Unlock()
	defaultSrc.fetchSDR = realFetchSDR
	defaultSrc.fetchSensorGet = realFetchSensorGet
	defaultSrc.resetLocked()
}

// resetLocked swaps the cache state and invalidates fetches still in flight
// (they check gen on completion and discard their writes). Callers must hold
// s.mu.
func (s *defaultSource) resetLocked() {
	s.gen++
	s.cached = nil
	s.cachedAt = time.Time{}
	s.nameCache = nil
	s.nameCacheAt = time.Time{}
	s.inflight = false
	s.coldDone = nil
}

func SetMockPower(s string) { defaultSrc.mockPower = s }

func (s *defaultSource) Available() bool {
	_, err := exec.LookPath("ipmitool")
	return err == nil
}

// SDR returns the current sensor readings. Fresh cache hits return
// immediately. Expired entries are served stale while a single background
// refresh runs (stale-while-revalidate), so callers never block on an
// ipmitool exec after the initial cold start. Cold starts (nothing cached
// yet) fetch synchronously, single-flight: concurrent first callers share
// one fetch instead of racing N ipmitool scans. A cold-start failure caches
// an empty result for one TTL (graceful, like the pre-SWR behaviour).
func (s *defaultSource) SDR() ([]Sensor, error) {
	s.mu.Lock()
	if !s.cachedAt.IsZero() {
		if time.Since(s.cachedAt) < s.cacheTTL {
			sensors := s.cached
			s.mu.Unlock()
			return sensors, nil
		}
		// Expired (SWR): serve the stale value now; one background refresh.
		if !s.inflight {
			s.inflight = true
			gen := s.gen
			s.mu.Unlock()
			go s.refresh(gen)
		} else {
			s.mu.Unlock()
		}
		return s.cached, nil
	}

	// Cold start: nothing to serve — single-flight synchronous fetch.
	if s.inflight {
		ch := s.coldDone
		s.mu.Unlock()
		<-ch
		s.mu.Lock()
		sensors := s.cached
		s.mu.Unlock()
		return sensors, nil
	}
	s.inflight = true
	ch := make(chan struct{})
	s.coldDone = ch
	gen := s.gen
	s.mu.Unlock()

	sensors := s.fetchFresh()

	s.mu.Lock()
	if gen == s.gen {
		s.cached = sensors // nil on total failure: cached for one TTL
		s.cachedAt = time.Now()
		s.inflight = false
		s.coldDone = nil
	}
	close(ch) // always release cold-start waiters
	s.mu.Unlock()
	return sensors, nil
}

// refresh performs one background SWR refresh. At most one refresh runs at
// a time (guarded by s.inflight). A failed refresh keeps the stale value;
// the next expired read retries.
func (s *defaultSource) refresh(gen uint64) {
	sensors := s.fetchFresh()
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen {
		return // source was reset mid-flight: discard the write
	}
	if sensors != nil {
		s.cached = sensors
		s.cachedAt = time.Now()
	}
	s.inflight = false
}

// fetchFresh performs an uncached read: per-sensor `sensor get` calls while
// the name cache is valid (skipping individual failures), falling back to a
// full discovery scan when the name cache is empty or every get failed.
// Discovery refreshes and persists the name cache. Callers must NOT hold
// s.mu; the lock is taken only to read/write shared fields, never across
// exec or file I/O.
func (s *defaultSource) fetchFresh() []Sensor {
	s.mu.Lock()
	names := append([]string(nil), s.nameCache...)
	namesValid := !s.nameCacheAt.IsZero() && time.Since(s.nameCacheAt) < nameCacheTTL && len(names) > 0
	get, full := s.fetchSensorGet, s.fetchSDR
	s.mu.Unlock()

	if namesValid && get != nil {
		sensors := make([]Sensor, 0, len(names))
		for _, name := range names {
			out, err := get(name)
			if err != nil {
				continue // skip the failed sensor; the rest still serve
			}
			sensors = append(sensors, parseSensorGet(name, out))
		}
		if len(sensors) > 0 {
			return sensors
		}
		// Every get failed (BMC reset? renamed sensors?) → full discovery.
	}

	out, err := full()
	if err != nil {
		return nil
	}
	all := parseSDR(out)

	var useful []string
	for _, sensor := range all {
		if isUsefulSensor(sensor.Name) {
			useful = append(useful, sensor.Name)
		}
	}
	at := time.Now()

	s.mu.Lock()
	s.nameCache = useful
	s.nameCacheAt = at
	dir := s.cacheDir
	s.mu.Unlock()
	s.saveNameCache(dir, useful, at)

	return all
}

// saveNameCache persists the name cache to disk. It must be called without
// s.mu held (file I/O).
func (s *defaultSource) saveNameCache(dir string, names []string, at time.Time) {
	if dir == "" || len(names) == 0 {
		return
	}
	path := filepath.Join(dir, sensorMapFilename)
	_ = os.MkdirAll(dir, 0o755)
	m := struct {
		Updated string   `json:"updated"`
		Names   []string `json:"names"`
	}{
		Updated: at.Format(time.RFC3339),
		Names:   names,
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
	case l == "power" || l == "chassispower":
		return true
	case l == "inlet temp":
		return true
	case l == "outlet temp":
		return true
	case strings.HasPrefix(l, "fan") && strings.Contains(l, "speed"):
		return true
	case strings.HasPrefix(l, "fan") && strings.Contains(l, "power"):
		return true
	case strings.Contains(l, "cpu") && strings.Contains(l, "temp") &&
		!strings.Contains(l, "vrd") && !strings.Contains(l, "vddq") && !strings.Contains(l, "vrm"):
		// CPU temps, excluding voltage regulator temps (VRD/VDDQ/VRM) —
		// they are not core temps and no collector wants them.
		return true
	case strings.Contains(l, "cpu") && strings.Contains(l, "core"):
		// "CPU1 Core Rem" — core temp naming on BMCs that omit "Temp"
		// from the sensor name.
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
