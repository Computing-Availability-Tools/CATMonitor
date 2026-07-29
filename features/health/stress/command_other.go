//go:build !linux

package stress

import (
	"context"
	"os/exec"
)

func benchmarkCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
