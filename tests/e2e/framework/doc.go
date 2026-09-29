// Package e2e provides the black-box end-to-end test framework for
// CATMonitor: workspace isolation, supervised processes with log capture,
// ready gating, and typed API clients.
//
// All real framework code is guarded by the "e2e" build tag so that the
// default `go vet ./...` / `go test ./...` gates stay fast and hermetic;
// this file keeps the package valid without the tag. Run scenarios with
// `make test-e2e`.
//
// Each scenario references the test cases in tests/e2e/testcases.xlsx by
// TC number (sub-test name) for bidirectional traceability.
package e2e
