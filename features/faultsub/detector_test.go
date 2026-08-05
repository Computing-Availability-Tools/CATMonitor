package faultsub

import (
	"testing"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
)

func mkNPU(name string, val float64, labels map[string]string) collector.Metric {
	return collector.Metric{
		Component: "npu", Name: name, Value: val, Unit: "",
		Labels: labels, Timestamp: time.Now(),
	}
}

// hasType reports whether events contain one of the given type.
func hasType(events []FaultEvent, t FaultType) bool {
	for _, e := range events {
		if e.Type == t {
			return true
		}
	}
	return false
}

func TestCardDropViaMetric(t *testing.T) {
	d := NewDetector(nil)
	ev := d.Detect([]collector.Metric{
		mkNPU("card_drop", 1, map[string]string{"npu_id": "3"}),
	})
	if !hasType(ev, FaultCardDrop) {
		t.Fatalf("expected card_drop, got %+v", ev)
	}
	if ev[0].Severity != SeverityCritical {
		t.Errorf("expected critical, got %s", ev[0].Severity)
	}
}

func TestCardDropViaErrorCodeLabel(t *testing.T) {
	d := NewDetector(nil)
	ev := d.Detect([]collector.Metric{
		mkNPU("error_code", 1, map[string]string{"npu_id": "3", "error_codes": "0x40f84e00,0x12345678"}),
	})
	if !hasType(ev, FaultCardDrop) {
		t.Fatalf("expected card_drop from error code label, got %+v", ev)
	}
}

func TestCardDropCodeCaseInsensitive(t *testing.T) {
	d := NewDetector(nil)
	ev := d.Detect([]collector.Metric{
		mkNPU("error_code", 1, map[string]string{"npu_id": "3", "error_codes": "0X40F84E00"}),
	})
	if !hasType(ev, FaultCardDrop) {
		t.Fatalf("expected case-insensitive card_drop match, got %+v", ev)
	}
}

func TestHealthStateAlarm(t *testing.T) {
	d := NewDetector(nil)
	ev := d.Detect([]collector.Metric{
		mkNPU("health_status", 3, map[string]string{"npu_id": "1", "status": "Alarm"}),
	})
	if !hasType(ev, FaultHealthState) {
		t.Fatalf("expected npu_health, got %+v", ev)
	}
	if ev[0].Severity != SeverityWarning {
		t.Errorf("Alarm should be warning, got %s", ev[0].Severity)
	}
}

func TestHealthStateCritical(t *testing.T) {
	d := NewDetector(nil)
	ev := d.Detect([]collector.Metric{
		mkNPU("health_status", 4, map[string]string{"npu_id": "1", "status": "Critical"}),
	})
	if !hasType(ev, FaultHealthState) {
		t.Fatalf("expected npu_health, got %+v", ev)
	}
	if ev[0].Severity != SeverityCritical {
		t.Errorf("Critical should be critical, got %s", ev[0].Severity)
	}
}

func TestHealthStateOKNoEvent(t *testing.T) {
	d := NewDetector(nil)
	ev := d.Detect([]collector.Metric{
		mkNPU("health_status", 1, map[string]string{"npu_id": "1", "status": "OK"}),
	})
	if len(ev) != 0 {
		t.Fatalf("expected no events for OK health, got %+v", ev)
	}
}

func TestErrorCodePresent(t *testing.T) {
	d := NewDetector(nil)
	// error_code without card-drop code -> only npu_error_code (warning)
	ev := d.Detect([]collector.Metric{
		mkNPU("error_code", 2, map[string]string{"npu_id": "2", "error_codes": "0x12345678"}),
	})
	if !hasType(ev, FaultErrorCode) {
		t.Fatalf("expected npu_error_code, got %+v", ev)
	}
	// Should NOT also emit card_drop since codes don't contain 0x40f84e00.
	if hasType(ev, FaultCardDrop) {
		t.Errorf("should not emit card_drop for non-drop codes")
	}
}

func TestHbmUCE(t *testing.T) {
	d := NewDetector(nil)
	ev := d.Detect([]collector.Metric{
		mkNPU("hbm_double_ecc", 2, map[string]string{"npu_id": "0"}),
	})
	if !hasType(ev, FaultHbmUCE) {
		t.Fatalf("expected hbm_uce, got %+v", ev)
	}
	if ev[0].Severity != SeverityCritical {
		t.Errorf("UCE should be critical, got %s", ev[0].Severity)
	}
}

func TestDdrUCE(t *testing.T) {
	d := NewDetector(nil)
	ev := d.Detect([]collector.Metric{
		mkNPU("ddr_double_ecc", 1, map[string]string{"npu_id": "0"}),
	})
	if !hasType(ev, FaultDdrUCE) {
		t.Fatalf("expected ddr_uce, got %+v", ev)
	}
}

func TestRoceLinkDown(t *testing.T) {
	d := NewDetector(nil)
	ev := d.Detect([]collector.Metric{
		mkNPU("roce_link_status", 0, map[string]string{"npu_id": "5", "status": "down"}),
	})
	if !hasType(ev, FaultRoceLinkDown) {
		t.Fatalf("expected roce_link_down, got %+v", ev)
	}
}

func TestRoceLinkUpNoEvent(t *testing.T) {
	d := NewDetector(nil)
	ev := d.Detect([]collector.Metric{
		mkNPU("roce_link_status", 1, map[string]string{"npu_id": "5", "status": "up"}),
	})
	if len(ev) != 0 {
		t.Fatalf("expected no events for up link, got %+v", ev)
	}
}

func TestDriverUnhealthy(t *testing.T) {
	d := NewDetector(nil)
	ev := d.Detect([]collector.Metric{
		mkNPU("driver_health", 1, map[string]string{"npu_id": "0"}),
	})
	if !hasType(ev, FaultDriverUnhealthy) {
		t.Fatalf("expected driver_unhealthy, got %+v", ev)
	}
}

func TestRuleDisabled(t *testing.T) {
	d := NewDetector(RuleConfig{FaultHbmUCE: false})
	ev := d.Detect([]collector.Metric{
		mkNPU("hbm_double_ecc", 2, map[string]string{"npu_id": "0"}),
	})
	if hasType(ev, FaultHbmUCE) {
		t.Fatalf("rule disabled but event emitted: %+v", ev)
	}
}

func TestRuleAbsentDefaultsEnabled(t *testing.T) {
	// Empty RuleConfig: all rules enabled by default.
	d := NewDetector(RuleConfig{})
	ev := d.Detect([]collector.Metric{
		mkNPU("hbm_double_ecc", 1, map[string]string{"npu_id": "0"}),
	})
	if !hasType(ev, FaultHbmUCE) {
		t.Fatalf("absent rule should default enabled, got %+v", ev)
	}
}

func TestNonNPUComponentIgnored(t *testing.T) {
	d := NewDetector(nil)
	ev := d.Detect([]collector.Metric{
		{Component: "cpu", Name: "usage", Value: 99, Labels: map[string]string{"npu_id": "0"}},
	})
	if len(ev) != 0 {
		t.Fatalf("cpu batch should produce no fault events, got %+v", ev)
	}
}

func TestRecoveryEvent(t *testing.T) {
	d := NewDetector(nil)

	// Cycle 1: NPU 3 has card_drop.
	c1 := d.Detect([]collector.Metric{
		mkNPU("card_drop", 1, map[string]string{"npu_id": "3"}),
	})
	if len(c1) != 1 || c1[0].Type != FaultCardDrop {
		t.Fatalf("cycle1: expected 1 card_drop, got %+v", c1)
	}

	// Cycle 2: NPU 3 healthy -> recovery event for card_drop.
	c2 := d.Detect([]collector.Metric{
		mkNPU("card_drop", 0, map[string]string{"npu_id": "3"}),
	})
	if len(c2) != 1 || !c2[0].Recovered || c2[0].Type != FaultCardDrop {
		t.Fatalf("cycle2: expected 1 recovery card_drop, got %+v", c2)
	}
}

func TestPersistentFaultNoReemit(t *testing.T) {
	// Transition-driven: a fault that persists emits only once (on appear),
	// not every cycle. This keeps the stream quiet for subscribers.
	d := NewDetector(nil)

	c1 := d.Detect([]collector.Metric{
		mkNPU("card_drop", 1, map[string]string{"npu_id": "3"}),
	})
	if len(c1) != 1 {
		t.Fatalf("cycle1: expected 1 event, got %d", len(c1))
	}

	// Same fault still active next cycle -> no new event.
	c2 := d.Detect([]collector.Metric{
		mkNPU("card_drop", 1, map[string]string{"npu_id": "3"}),
	})
	if len(c2) != 0 {
		t.Fatalf("cycle2: persistent fault should not re-emit, got %+v", c2)
	}
}

func TestRecoveryNoSpuriousRepeat(t *testing.T) {
	d := NewDetector(nil)

	// Cycle 1: fault.
	d.Detect([]collector.Metric{
		mkNPU("card_drop", 1, map[string]string{"npu_id": "3"}),
	})
	// Cycle 2: healthy -> one recovery.
	d.Detect([]collector.Metric{
		mkNPU("card_drop", 0, map[string]string{"npu_id": "3"}),
	})
	// Cycle 3: still healthy -> no recovery (already recovered last cycle).
	c3 := d.Detect([]collector.Metric{
		mkNPU("card_drop", 0, map[string]string{"npu_id": "3"}),
	})
	if len(c3) != 0 {
		t.Fatalf("cycle3: expected no events, got %+v", c3)
	}
}

func TestMultipleNPURecovery(t *testing.T) {
	d := NewDetector(nil)
	// Two NPUs faulted.
	d.Detect([]collector.Metric{
		mkNPU("card_drop", 1, map[string]string{"npu_id": "1"}),
		mkNPU("card_drop", 1, map[string]string{"npu_id": "2"}),
	})
	// Only NPU 1 recovers; NPU 2 stays faulted.
	ev := d.Detect([]collector.Metric{
		mkNPU("card_drop", 0, map[string]string{"npu_id": "1"}),
		mkNPU("card_drop", 1, map[string]string{"npu_id": "2"}),
	})
	if len(ev) != 1 {
		t.Fatalf("expected 1 recovery event, got %d: %+v", len(ev), ev)
	}
	if ev[0].NPUID != "1" || !ev[0].Recovered {
		t.Errorf("expected recovery for NPU 1, got %+v", ev[0])
	}
}

func TestConcurrentDetect(t *testing.T) {
	// Recovery state map must be safe under concurrent access (the detector
	// is called from the scheduler goroutine, but the snapshot/dispatcher
	// may also iterate). At minimum Detect itself must not race itself.
	d := NewDetector(nil)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			d.Detect([]collector.Metric{
				mkNPU("card_drop", float64(i % 2), map[string]string{"npu_id": "0"}),
			})
		}
		close(done)
	}()
	<-done
}
