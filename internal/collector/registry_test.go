package collector

import (
	"testing"
	"time"
)

// fakeCollector is a minimal Collector test double.
type fakeCollector struct {
	name     string
	comp     string
	enabled  bool
	interval time.Duration
}

func (f *fakeCollector) Name() string            { return f.name }
func (f *fakeCollector) Component() string        { return f.comp }
func (f *fakeCollector) Collect() ([]Metric, error) { return nil, nil }
func (f *fakeCollector) Priority() Priority       { return PriorityMedium }
func (f *fakeCollector) DefaultInterval() time.Duration { return f.interval }
func (f *fakeCollector) DefaultEnabled() bool     { return f.enabled }

func TestRegistryEmpty(t *testing.T) {
	r := NewRegistry()
	if got := r.All(); len(got) != 0 {
		t.Errorf("empty registry All() = %d collectors, want 0", len(got))
	}
	if c := r.Get("missing"); c != nil {
		t.Errorf("Get on empty registry = %v, want nil", c)
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	c := &fakeCollector{name: "cpu", comp: "cpu", enabled: true}
	r.Register(c)
	if got := r.Get("cpu"); got != c {
		t.Errorf("Get(cpu) = %v, want the registered collector", got)
	}
	if got := r.Get("other"); got != nil {
		t.Errorf("Get(other) = %v, want nil", got)
	}
}

func TestRegistryRegisterSameNameReplaces(t *testing.T) {
	r := NewRegistry()
	first := &fakeCollector{name: "cpu", comp: "cpu"}
	second := &fakeCollector{name: "cpu", comp: "cpu"}
	r.Register(first)
	r.Register(second)
	if got := r.Get("cpu"); got != second {
		t.Error("re-registering the same name must replace the collector")
	}
	if len(r.All()) != 1 {
		t.Errorf("All() = %d collectors after replace, want 1", len(r.All()))
	}
}

func TestRegistryAllSortedByName(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"network", "cpu", "memory", "disk"} {
		r.Register(&fakeCollector{name: name, comp: name})
	}
	got := r.All()
	want := []string{"cpu", "disk", "memory", "network"}
	if len(got) != len(want) {
		t.Fatalf("All() = %d collectors, want %d", len(got), len(want))
	}
	for i, c := range got {
		if c.Name() != want[i] {
			t.Errorf("All()[%d] = %q, want %q", i, c.Name(), want[i])
		}
	}
}

func TestDefaultRegistryUsable(t *testing.T) {
	c := &fakeCollector{name: "registry-test-only", comp: "cpu"}
	DefaultRegistry.Register(c)
	if got := DefaultRegistry.Get("registry-test-only"); got != c {
		t.Error("DefaultRegistry must return the registered collector")
	}
}
