package snapshot

import (
	"encoding/json"
	"os"
)

// ReadGlobal loads the global snapshot (health/collectors/intervals/system
// specs) written by the daemon's GlobalWriter.
func ReadGlobal(path string) (*GlobalSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s GlobalSnapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ReadComp loads a per-component snapshot (metrics/history/specs) written by
// the daemon's PerCompWriter.
func ReadComp(path string) (*CompSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s CompSnapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
