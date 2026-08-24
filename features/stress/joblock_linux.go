//go:build linux

package stress

// Linux cross-process job coordination.
import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// acquireJobLock serializes stress jobs across CATMonitor processes on one
// Linux host. The lock file may remain on disk, but the kernel lock is released
// automatically if a process exits unexpectedly.
func acquireJobLock(reportPath string) (func() error, error) {
	if reportPath == "" {
		return func() error { return nil }, nil
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(reportPath+".lock", os.O_CREATE|os.O_RDWR, 0o640)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return func() error {
		unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		closeErr := file.Close()
		if unlockErr != nil {
			return unlockErr
		}
		return closeErr
	}, nil
}
