package faultsub

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

// CardDropFaultCode is the DCMI error code meaning "card dropped/offline",
// matching the EEP fault manager's constant (0x40f84e00). Surfaced in
// error_code labels when the device reports it.
const CardDropFaultCode = "0x40f84e00"

// RuleConfig toggles each fault rule. A rule absent from the map defaults to
// enabled=true (fail-open: a new fault type still emits even if config omits
// it). The config layer maps yaml keys to these FaultType values.
type RuleConfig map[FaultType]bool

// Enabled reports whether a rule is on (default true when unset).
func (r RuleConfig) Enabled(t FaultType) bool {
	on, ok := r[t]
	return !ok || on
}

// FaultDetector evaluates a collected metric batch for fault conditions and
// emits FaultEvents. It is pure Go (no CGo) so it builds and tests everywhere.
//
// Events are transition-driven: a fault that newly appears emits one event,
// and a fault that clears emits a recovery event; a fault that persists
// across cycles emits nothing (the subscriber already knows). This keeps the
// stream quiet and gives each event a clear "something changed" meaning.
type FaultDetector struct {
	rules RuleConfig
	mu    sync.Mutex
	// prev[npuID][type] = true when the last batch saw this fault active.
	prev map[string]map[FaultType]bool
}

// NewDetector constructs a detector with the given rule toggles.
func NewDetector(rules RuleConfig) *FaultDetector {
	return &FaultDetector{
		rules: rules,
		prev:  make(map[string]map[FaultType]bool),
	}
}

// Detect inspects one collector.Metric batch (typically all metrics from one
// collector invocation) and returns the FaultEvents produced this cycle,
// including recovery events for faults that cleared.
//
// Only component=="npu" metrics are evaluated (and any component listed in a
// future rule set); other components are ignored so CPU/disk batches don't
// waste cycles.
func (d *FaultDetector) Detect(metrics []collector.Metric) []FaultEvent {
	// Group the relevant metrics by npu_id.
	type devState struct {
		health      *collector.Metric
		errorCode   *collector.Metric
		cardDrop    *collector.Metric
		hbmDouble   *collector.Metric
		ddrDouble   *collector.Metric
		roceStatus  *collector.Metric
		roceHealth  *collector.Metric
		driverHealth *collector.Metric
	}
	devs := make(map[string]*devState)

	for i := range metrics {
		m := &metrics[i]
		if m.Component != "npu" {
			continue
		}
		npuID := m.Labels["npu_id"]
		if npuID == "" {
			continue
		}
		st, ok := devs[npuID]
		if !ok {
			st = &devState{}
			devs[npuID] = st
		}
		switch m.Name {
		case "health_status":
			st.health = m
		case "error_code":
			st.errorCode = m
		case "card_drop":
			st.cardDrop = m
		case "hbm_double_ecc":
			st.hbmDouble = m
		case "ddr_double_ecc":
			st.ddrDouble = m
		case "roce_link_status":
			st.roceStatus = m
		case "roce_link_health":
			st.roceHealth = m
		case "driver_health":
			st.driverHealth = m
		}
	}

	now := time.Now()
	var events []FaultEvent
	d.mu.Lock()
	defer d.mu.Unlock()

	// activeThisCycle[npuID] = set of types currently firing.
	activeThisCycle := make(map[string]map[FaultType]bool, len(devs))
	prev := d.prev

	for npuID, st := range devs {
		if activeThisCycle[npuID] == nil {
			activeThisCycle[npuID] = make(map[FaultType]bool)
		}
		// fire records an active fault this cycle. It only emits an event
		// when the fault is newly active (a transition), not when it
		// persists from the previous cycle.
		fire := func(t FaultType, sev Severity, detail map[string]string) {
			activeThisCycle[npuID][t] = true
			wasActive := prev[npuID] != nil && prev[npuID][t]
			if wasActive {
				return // persistent fault: no new event
			}
			events = append(events, FaultEvent{
				EventID:   newEventID(),
				Type:      t,
				Component: "npu",
				NPUID:     npuID,
				Severity:  sev,
				Detail:    detail,
				Timestamp: now,
				Recovered: false,
			})
		}

		// card_drop: explicit metric ==1, or error_code labels contain the
		// card-drop code. Highest priority (critical).
		if d.rules.Enabled(FaultCardDrop) {
			dropped := false
			detail := map[string]string{}
			if st.cardDrop != nil && st.cardDrop.Value >= 1 {
				dropped = true
				detail["card_drop"] = "1"
			}
			if st.errorCode != nil {
				if codes := st.errorCode.Labels["error_codes"]; codes != "" {
					for _, c := range strings.Split(codes, ",") {
						c = strings.TrimSpace(c)
						if strings.EqualFold(c, CardDropFaultCode) {
							dropped = true
							detail["error_codes"] = codes
						}
					}
				}
			}
			if dropped {
				fire(FaultCardDrop, SeverityCritical, detail)
			}
		}

		// npu_health: Alarm/Critical.
		if d.rules.Enabled(FaultHealthState) && st.health != nil {
			status := st.health.Labels["status"]
			if status == "Alarm" || status == "Critical" {
				sev := SeverityWarning
				if status == "Critical" {
					sev = SeverityCritical
				}
				fire(FaultHealthState, sev, map[string]string{"health": status})
			}
		}

		// npu_error_code: any error code present (non-card-drop is warning).
		if d.rules.Enabled(FaultErrorCode) && st.errorCode != nil && st.errorCode.Value > 0 {
			codes := st.errorCode.Labels["error_codes"]
			if codes == "" {
				codes = strconv.FormatFloat(st.errorCode.Value, 'f', -1, 64)
			}
			fire(FaultErrorCode, SeverityWarning, map[string]string{"error_codes": codes})
		}

		// hbm_uce (HBM double-bit ECC, uncorrectable).
		if d.rules.Enabled(FaultHbmUCE) && st.hbmDouble != nil && st.hbmDouble.Value > 0 {
			fire(FaultHbmUCE, SeverityCritical, map[string]string{"ecc_count": formatFloat(st.hbmDouble.Value)})
		}

		// ddr_uce.
		if d.rules.Enabled(FaultDdrUCE) && st.ddrDouble != nil && st.ddrDouble.Value > 0 {
			fire(FaultDdrUCE, SeverityCritical, map[string]string{"ecc_count": formatFloat(st.ddrDouble.Value)})
		}

		// roce_link_down.
		if d.rules.Enabled(FaultRoceLinkDown) {
			down := false
			detail := map[string]string{}
			if st.roceStatus != nil {
				if st.roceStatus.Value == 0 || st.roceStatus.Labels["status"] == "down" {
					down = true
					detail["roce_link_status"] = "down"
				}
			}
			if st.roceHealth != nil {
				link := st.roceHealth.Labels["roce_link"]
				if link == "down" || link == "Down" {
					down = true
					detail["roce_link"] = link
				}
			}
			if down {
				fire(FaultRoceLinkDown, SeverityWarning, detail)
			}
		}

		// driver_unhealthy.
		if d.rules.Enabled(FaultDriverUnhealthy) && st.driverHealth != nil && st.driverHealth.Value != 0 {
			fire(FaultDriverUnhealthy, SeverityWarning, map[string]string{"driver_health": formatFloat(st.driverHealth.Value)})
		}
	}

	// Recovery: for every (npu,type) active last cycle but not this cycle,
	// emit a Recovered event.
	for npuID, prevTypes := range d.prev {
		cur := activeThisCycle[npuID]
		for t := range prevTypes {
			if !cur[t] {
				events = append(events, FaultEvent{
					EventID:   newEventID(),
					Type:      t,
					Component: "npu",
					NPUID:     npuID,
					Severity:  SeverityWarning,
					Detail:    map[string]string{},
					Timestamp: now,
					Recovered: true,
				})
			}
		}
	}

	// Persist this cycle's active set for next time.
	d.prev = activeThisCycle

	return events
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
