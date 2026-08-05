//go:build linux

package stress

// Linux process-group integration for the top-level stress feature.
import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// benchmarkCommand places the shell and every MPI/benchmark child in a
// dedicated process group. Cancelling the context then releases the complete
// high-load job instead of only killing the wrapper shell.
func benchmarkCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return cmd
}
