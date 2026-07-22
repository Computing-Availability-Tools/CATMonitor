//go:build linux

// Package cpugov implements the CPU-senses-NPU energy-saving actuator
// (SPEC: features/cpugov/cpugov_SPEC.md).
//
// It watches CPU usage (cpu collector, core=total) and NPU process presence
// (npu collector, process_total) via a scheduler tap, drives a small
// three-state CPU idle state machine (Active → Observing → ConfirmedIdle
// with a 2-strike non-idle hysteresis and an NPU override that bypasses the
// hysteresis), and pins all CPU cores to cpuinfo_min_freq while both CPU and
// NPU are confirmed idle. The actuator is idempotent and self-healing; the
// controller restores saved original frequencies on graceful shutdown.
//
// Linux-only: writes /sys/devices/system/cpu/<core>/cpufreq/scaling_*_freq.
// Non-Linux builds do not import this package; the main package provides a
// no-op energysave stub for other platforms.
package cpugov
