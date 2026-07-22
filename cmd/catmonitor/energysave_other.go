//go:build !linux

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/config"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/storage"
)

// startEnergysave is a no-op on non-Linux: cpufreq sysfs actuation is
// Linux-only. Matches the linux signature so main.go can call it
// unconditionally.
func startEnergysave(_ context.Context, _ *config.Config, _ *collector.Scheduler, _ *storage.JSONLStorage, _ *slog.Logger) {
}

// stopEnergysave is a no-op on non-Linux.
func stopEnergysave() {}

// runEnergysaveCLI reports that the feature is unsupported on non-Linux.
func runEnergysaveCLI(_ *config.Config, _ *slog.Logger) {
	fmt.Println("energysave: not supported on this platform (Linux-only cpufreq actuation)")
}
