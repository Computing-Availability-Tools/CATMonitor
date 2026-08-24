//go:build !linux

package stress

// Portable unsupported-platform command construction.
import (
	"context"
	"os/exec"
)

func benchmarkCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
