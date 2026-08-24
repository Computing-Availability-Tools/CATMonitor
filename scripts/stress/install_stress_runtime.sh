#!/usr/bin/env bash
# Install the optional CATMonitor stress plugin layout on one Linux host.
# This command never builds benchmarks, edits CATMonitor YAML, starts a service,
# or runs a workload.

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd -P)

PLUGIN_ROOT=/opt/catmonitor/stress
STATE_ROOT=/var/lib/catmonitor/stress
ADAPTER_SOURCE="$REPO_ROOT/features/stress/benchmark_check.sh"
CPU_RUNNER_ADAPTER=
RUNTIME_SOURCE=
CPU_MANIFEST=
DEPLOYMENT_MANIFEST=
FORCE=false

usage() {
    cat <<'EOF'
Usage: install_stress_runtime.sh [OPTIONS]

Locations:
  --plugin-root PATH          Installed plugin root (default: /opt/catmonitor/stress)
  --state-root PATH           Writable state root (default: /var/lib/catmonitor/stress)

Optional inputs:
  --adapter PATH              Adapter to install (default: repository template)
  --cpu-runner-adapter PATH   Optional runner-local CPU adapter to install
  --runtime-source PATH       Runtime root containing stream/, hpl/ and/or hpcg/
  --cpu-manifest PATH         CPU build manifest to install
  --deployment-manifest PATH  Generated deployment manifest to install
  --force                     Allow replacement of installed files
  -h, --help                  Show this help

The installer creates the stable host layout and copies only known stress
assets. Build assets first with build_cpu_benchmarks.sh, or omit optional
inputs to create an empty plugin skeleton. CATMonitor configuration and service
startup remain explicit administrator actions.
EOF
}

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
require_value() { [ "$#" -ge 2 ] && [ -n "$2" ] || die "$1 requires a value"; }

while [ "$#" -gt 0 ]; do
    case "$1" in
        --plugin-root) require_value "$@"; PLUGIN_ROOT=$2; shift 2 ;;
        --state-root) require_value "$@"; STATE_ROOT=$2; shift 2 ;;
        --adapter) require_value "$@"; ADAPTER_SOURCE=$2; shift 2 ;;
        --cpu-runner-adapter) require_value "$@"; CPU_RUNNER_ADAPTER=$2; shift 2 ;;
        --runtime-source) require_value "$@"; RUNTIME_SOURCE=$2; shift 2 ;;
        --cpu-manifest) require_value "$@"; CPU_MANIFEST=$2; shift 2 ;;
        --deployment-manifest) require_value "$@"; DEPLOYMENT_MANIFEST=$2; shift 2 ;;
        --force) FORCE=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown argument: $1" ;;
    esac
done

absolute_path() {
    local option=$1 value=$2
    case "$value" in /*) ;; *) die "$option must be absolute: $value" ;; esac
    case "$value" in *$'\n'*|*$'\r'*) die "$option cannot contain a newline" ;; esac
    readlink -m -- "$value"
}

PLUGIN_ROOT=$(absolute_path --plugin-root "$PLUGIN_ROOT")
STATE_ROOT=$(absolute_path --state-root "$STATE_ROOT")
ADAPTER_SOURCE=$(absolute_path --adapter "$ADAPTER_SOURCE")
case "$PLUGIN_ROOT" in /|/opt|/opt/catmonitor) die "--plugin-root must be a dedicated child directory" ;; esac
case "$STATE_ROOT" in /|/var|/var/lib|/var/lib/catmonitor) die "--state-root must be a dedicated child directory" ;; esac

[ -f "$ADAPTER_SOURCE" ] && [ ! -L "$ADAPTER_SOURCE" ] || die "adapter is not a regular file: $ADAPTER_SOURCE"
bash -n "$ADAPTER_SOURCE" || die "adapter syntax validation failed: $ADAPTER_SOURCE"

if [ -n "$CPU_RUNNER_ADAPTER" ]; then
    CPU_RUNNER_ADAPTER=$(absolute_path --cpu-runner-adapter "$CPU_RUNNER_ADAPTER")
    [ -f "$CPU_RUNNER_ADAPTER" ] && [ ! -L "$CPU_RUNNER_ADAPTER" ] || \
        die "CPU runner adapter is not a regular file: $CPU_RUNNER_ADAPTER"
    bash -n "$CPU_RUNNER_ADAPTER" || die "CPU runner adapter syntax validation failed: $CPU_RUNNER_ADAPTER"
fi

if [ -n "$RUNTIME_SOURCE" ]; then
    RUNTIME_SOURCE=$(absolute_path --runtime-source "$RUNTIME_SOURCE")
    [ -d "$RUNTIME_SOURCE" ] && [ ! -L "$RUNTIME_SOURCE" ] || die "runtime source is not a directory: $RUNTIME_SOURCE"
fi
if [ -n "$CPU_MANIFEST" ]; then
    CPU_MANIFEST=$(absolute_path --cpu-manifest "$CPU_MANIFEST")
    [ -f "$CPU_MANIFEST" ] && [ ! -L "$CPU_MANIFEST" ] || die "CPU manifest is not a regular file: $CPU_MANIFEST"
fi
if [ -n "$DEPLOYMENT_MANIFEST" ]; then
    DEPLOYMENT_MANIFEST=$(absolute_path --deployment-manifest "$DEPLOYMENT_MANIFEST")
    [ -f "$DEPLOYMENT_MANIFEST" ] && [ ! -L "$DEPLOYMENT_MANIFEST" ] || die "deployment manifest is not a regular file: $DEPLOYMENT_MANIFEST"
fi

install -d -m 0755 "$PLUGIN_ROOT" "$PLUGIN_ROOT/runtime" "$PLUGIN_ROOT/manifests"
install -d -m 0755 "$PLUGIN_ROOT/runtime/stream" "$PLUGIN_ROOT/runtime/hpl" "$PLUGIN_ROOT/runtime/hpcg"
install -d -m 0750 \
    "$STATE_ROOT" "$STATE_ROOT/npu-burn-output" \
    "$STATE_ROOT/work" "$STATE_ROOT/work/hpl" "$STATE_ROOT/work/hpcg"

install_file() {
    local source=$1 target=$2 mode=$3
    if [ -e "$target" ] && [ "$FORCE" != true ]; then
        die "target already exists; use --force to replace it: $target"
    fi
    [ ! -L "$target" ] || die "target cannot be a symbolic link: $target"
    install -m "$mode" "$source" "$target"
}

install_file "$ADAPTER_SOURCE" "$PLUGIN_ROOT/benchmark_check.sh" 0755
if [ -n "$CPU_RUNNER_ADAPTER" ]; then
    install_file "$CPU_RUNNER_ADAPTER" "$PLUGIN_ROOT/cpu-runner-benchmark_check.sh" 0755
fi

installed_runtime=0
if [ -n "$RUNTIME_SOURCE" ]; then
    while IFS='|' read -r relative mode; do
        [ -f "$RUNTIME_SOURCE/$relative" ] || continue
        install_file "$RUNTIME_SOURCE/$relative" "$PLUGIN_ROOT/runtime/$relative" "$mode"
        installed_runtime=$((installed_runtime + 1))
    done <<'EOF'
stream/stream_omp|0755
hpl/xhpl|0755
hpl/HPL.dat|0644
hpcg/xhpcg|0755
hpcg/hpcg.dat|0644
EOF
    [ "$installed_runtime" -gt 0 ] || die "runtime source contains no recognized CATMonitor stress assets"
fi

[ -z "$CPU_MANIFEST" ] || install_file "$CPU_MANIFEST" "$PLUGIN_ROOT/manifests/cpu-build-manifest.json" 0644
[ -z "$DEPLOYMENT_MANIFEST" ] || install_file "$DEPLOYMENT_MANIFEST" "$PLUGIN_ROOT/manifests/stress-deployment-manifest.json" 0644

printf 'Installed CATMonitor stress plugin root: %s\n' "$PLUGIN_ROOT"
printf 'Prepared CATMonitor stress state root: %s\n' "$STATE_ROOT"
