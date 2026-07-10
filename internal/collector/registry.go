package collector

import (
	"sort"
	"sync"
)

// Registry is a thread-safe collector registry.
type Registry struct {
	mu         sync.RWMutex
	collectors map[string]Collector
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		collectors: make(map[string]Collector),
	}
}

// Register adds a collector to the registry.
// If a collector with the same name already exists, it will be replaced.
func (r *Registry) Register(c Collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collectors[c.Name()] = c
}

// Get returns a collector by name, or nil if not found.
func (r *Registry) Get(name string) Collector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.collectors[name]
}

// All returns all registered collectors, sorted by name.
func (r *Registry) All() []Collector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Collector, 0, len(r.collectors))
	for _, c := range r.collectors {
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name() < result[j].Name()
	})
	return result
}

// DefaultRegistry is the global collector registry instance.
var DefaultRegistry = NewRegistry()
