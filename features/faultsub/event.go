// Package faultsub implements CATMonitor's fault subscription & push mechanism.
//
// faultsub is a Storage plugin (like exporter.CachingStorage): it wraps the
// inner collector.Storage, taps every collected metric batch, runs fault
// detection rules, and pushes FaultEvents to registered subscribers via HTTP
// webhook (net/http, zero new dependencies). A REST API lets subscribers
// declare what fault types / NPUs / debounce / callback URL they want.
//
// The module is opt-in: when catmonitor.yaml has faultsub.enabled=false (the
// default), daemon wiring skips it entirely and behavior is unchanged.
package faultsub

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// FaultType enumerates the fault conditions FaultDetector can emit.
type FaultType string

const (
	FaultCardDrop        FaultType = "card_drop"         // NPU card offline / device not ready
	FaultHealthState     FaultType = "npu_health"        // health_status non-OK (Alarm/Critical)
	FaultErrorCode       FaultType = "npu_error_code"   // device reports error codes
	FaultHbmUCE          FaultType = "hbm_uce"           // HBM double-bit (uncorrectable) ECC
	FaultDdrUCE          FaultType = "ddr_uce"           // DDR double-bit ECC
	FaultRoceLinkDown    FaultType = "roce_link_down"    // RoCE link down / unhealthy
	FaultDriverUnhealthy FaultType = "driver_unhealthy" // NPU driver health non-zero
	FaultStragglerDetected FaultType = "straggler_detected" // slow-node (straggler) detection hit (ingested from external detector)
)

// AllFaultTypes returns every known FaultType, used by the REST discovery
// endpoint GET /faultsub/types so subscribers can learn the capability set.
func AllFaultTypes() []FaultType {
	return []FaultType{
		FaultCardDrop,
		FaultHealthState,
		FaultErrorCode,
		FaultHbmUCE,
		FaultDdrUCE,
		FaultRoceLinkDown,
		FaultDriverUnhealthy,
		FaultStragglerDetected,
	}
}

// Severity rates a fault's urgency. Subscribers filter by MinSeverity.
type Severity string

const (
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// AtLeast reports whether s meets the given minimum severity threshold.
// An empty threshold means "accept anything".
func (s Severity) AtLeast(min string) bool {
	if min == "" {
		return true
	}
	v := func(x Severity) int {
		if x == SeverityCritical {
			return 2
		}
		if x == SeverityWarning {
			return 1
		}
		return 0
	}
	return v(s) >= v(Severity(min))
}

// FaultEvent is the unit pushed to subscribers and stored for REST retrieval.
// It is produced by FaultDetector from a collected metric batch.
type FaultEvent struct {
	// EventID is a globally unique identifier (random hex) used for dedup.
	EventID string `json:"event_id"`
	// Type is the fault condition that fired.
	Type FaultType `json:"type"`
	// Component is the metric component ("npu").
	Component string `json:"component"`
	// NPUID is the affected device id (from metric.Labels["npu_id"]).
	NPUID string `json:"npu_id"`
	// Severity is warning or critical.
	Severity Severity `json:"severity"`
	// Detail carries the raw evidence (error_codes, health, ecc_count, ...).
	Detail map[string]string `json:"detail"`
	// Timestamp is when the fault was detected.
	Timestamp time.Time `json:"timestamp"`
	// Recovered is true for a transition from fault -> healthy.
	Recovered bool `json:"recovered"`
}

// newEventID returns a 16-byte random hex string. Used for EventID.
func newEventID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
