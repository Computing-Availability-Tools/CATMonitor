//go:build !linux

package stress

// Stress execution is unsupported outside Linux. Keep construction portable
// so configuration and unsupported-status behavior can still be tested.
func acquireJobLock(string) (func() error, error) {
	return func() error { return nil }, nil
}
