#!/usr/bin/env bash
# Generate one complete, node-owned CATMonitor stress deployment from already
# built CPU assets and an already created fixed NPU Burn container.
#
# This tool is intentionally read-only with respect to benchmark runtimes and
# containers. It writes only the selected output directory and never starts a
# benchmark, MPI process, container, or CATMonitor service.

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd -P)
ADAPTER_TEMPLATE="$REPO_ROOT/features/stress/benchmark_check.sh"

RUNTIME_ROOT=/opt/catmonitor/benchmarks/runtime
OUTPUT_DIR=
CPU_MANIFEST=
NPU_MANIFEST=
REPORT_PATH=/var/lib/catmonitor/stress-latest.json
NUMACTL=
MPI_LAUNCHER=
HPL_LIBRARY_DIR=
STREAM_THREADS=0
HPL_PROCESSES=
HPL_THREADS=
HPCG_PROCESSES=
HPCG_THREADS=
HPCG_NX=32
HPCG_NY=32
HPCG_NZ=32
HPCG_RUNTIME=60
NPU_RUNTIME=
NPU_CONTAINER=
NPU_IMAGE=
NPU_EXECUTABLE=/usr/local/bin/catmonitor-npu-burn
NPU_OUTPUT_DIR=/var/lib/catmonitor/npu-burn-output
NPU_DEVICE=
NPU_RUN_CASE=
NPU_GROUP=
NPU_CHIP_GENERATION=
NPU_INTERNAL_TIMEOUT=300
NPU_CANN=
NPU_TORCH_NPU=
NPU_SOC=
WEB_ENABLED=false
FORCE=false

usage() {
    cat <<'EOF'
Usage: generate_stress_deployment.sh [OPTIONS]

Required deployment inputs:
  --output-dir PATH          Directory for generated adapter, YAML and manifest
  --mpi-launcher PATH        MPI launcher matching the HPL/HPCG build ABI
  --hpl-processes N          HPL MPI process count
  --hpl-threads N            HPL threads per MPI process
  --hpcg-processes N         HPCG MPI process count
  --hpcg-threads N           HPCG threads per MPI process
  --npu-manifest PATH        Manifest produced by build_npu_burn_image.sh
  --npu-runtime PATH         Container runtime executable (for example docker)
  --npu-container NAME       Existing fixed NPU Burn container name
  --npu-image IMAGE          Exact image reference expected by the adapter
  --npu-device IDS           Reserved logical IDs, comma separated, or all
  --npu-chip-generation ID   Explicit chip generation (A2, A3 or A5)
  --npu-cann VERSION         Deployed CANN identity shown in the profile
  --npu-torch-npu VERSION    Deployed torch_npu identity shown in the profile
  --npu-soc MODEL            Deployed SoC model shown in the profile
  --npu-run-case NAME        One upstream run case (mutually exclusive with group)
  --npu-group NAME           One upstream group (mutually exclusive with run case)

CPU/runtime inputs:
  --runtime-root PATH        CPU runtime root (default: /opt/catmonitor/benchmarks/runtime)
  --cpu-manifest PATH        CPU build manifest (default: sibling manifests/build-manifest.json)
  --numactl PATH             numactl executable (default: resolve from PATH)
  --hpl-library-dir PATH     Optional OpenBLAS runtime library directory
  --stream-threads N         0 leaves OMP_NUM_THREADS unset (default: 0)
  --hpcg-nx N               HPCG local X dimension (default: 32)
  --hpcg-ny N               HPCG local Y dimension (default: 32)
  --hpcg-nz N               HPCG local Z dimension (default: 32)
  --hpcg-runtime N          HPCG target runtime seconds (default: 60)

NPU/runtime inputs:
  --npu-executable PATH      Executable inside the fixed container
  --npu-output-dir PATH      Host-visible result directory
  --npu-internal-timeout N  Per-case NPU Burn timeout seconds (default: 300)

CATMonitor output:
  --report-path PATH         Shared latest report path
  --enable-web               Enable loopback-only Web mutation in generated YAML
  --force                    Replace generated files already in output-dir
  -h, --help                 Show this help

The command does not build assets, create/start containers, write /etc by
itself, or run stress. After generation, use `catmonitor stress doctor`.
EOF
}

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
require_value() { [ "$#" -ge 2 ] && [ -n "$2" ] || die "$1 requires a value"; }

while [ "$#" -gt 0 ]; do
    case "$1" in
        --output-dir) require_value "$@"; OUTPUT_DIR=$2; shift 2 ;;
        --runtime-root) require_value "$@"; RUNTIME_ROOT=$2; shift 2 ;;
        --cpu-manifest) require_value "$@"; CPU_MANIFEST=$2; shift 2 ;;
        --npu-manifest) require_value "$@"; NPU_MANIFEST=$2; shift 2 ;;
        --report-path) require_value "$@"; REPORT_PATH=$2; shift 2 ;;
        --numactl) require_value "$@"; NUMACTL=$2; shift 2 ;;
        --mpi-launcher) require_value "$@"; MPI_LAUNCHER=$2; shift 2 ;;
        --hpl-library-dir) require_value "$@"; HPL_LIBRARY_DIR=$2; shift 2 ;;
        --stream-threads) require_value "$@"; STREAM_THREADS=$2; shift 2 ;;
        --hpl-processes) require_value "$@"; HPL_PROCESSES=$2; shift 2 ;;
        --hpl-threads) require_value "$@"; HPL_THREADS=$2; shift 2 ;;
        --hpcg-processes) require_value "$@"; HPCG_PROCESSES=$2; shift 2 ;;
        --hpcg-threads) require_value "$@"; HPCG_THREADS=$2; shift 2 ;;
        --hpcg-nx) require_value "$@"; HPCG_NX=$2; shift 2 ;;
        --hpcg-ny) require_value "$@"; HPCG_NY=$2; shift 2 ;;
        --hpcg-nz) require_value "$@"; HPCG_NZ=$2; shift 2 ;;
        --hpcg-runtime) require_value "$@"; HPCG_RUNTIME=$2; shift 2 ;;
        --npu-runtime) require_value "$@"; NPU_RUNTIME=$2; shift 2 ;;
        --npu-container) require_value "$@"; NPU_CONTAINER=$2; shift 2 ;;
        --npu-image) require_value "$@"; NPU_IMAGE=$2; shift 2 ;;
        --npu-executable) require_value "$@"; NPU_EXECUTABLE=$2; shift 2 ;;
        --npu-output-dir) require_value "$@"; NPU_OUTPUT_DIR=$2; shift 2 ;;
        --npu-device) require_value "$@"; NPU_DEVICE=$2; shift 2 ;;
        --npu-run-case) require_value "$@"; NPU_RUN_CASE=$2; shift 2 ;;
        --npu-group) require_value "$@"; NPU_GROUP=$2; shift 2 ;;
        --npu-chip-generation) require_value "$@"; NPU_CHIP_GENERATION=$2; shift 2 ;;
        --npu-internal-timeout) require_value "$@"; NPU_INTERNAL_TIMEOUT=$2; shift 2 ;;
        --npu-cann) require_value "$@"; NPU_CANN=$2; shift 2 ;;
        --npu-torch-npu) require_value "$@"; NPU_TORCH_NPU=$2; shift 2 ;;
        --npu-soc) require_value "$@"; NPU_SOC=$2; shift 2 ;;
        --enable-web) WEB_ENABLED=true; shift ;;
        --force) FORCE=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown argument: $1" ;;
    esac
done

require_command() { command -v "$1" >/dev/null 2>&1 || die "required command is unavailable: $1"; }
for command_name in awk date install mktemp mv readlink sha256sum; do require_command "$command_name"; done

absolute_path() {
    local option=$1 value=$2
    [ -n "$value" ] || die "$option is required"
    case "$value" in /*) ;; *) die "$option must be absolute: $value" ;; esac
    case "$value" in *$'\n'*|*$'\r'*) die "$option cannot contain a newline" ;; esac
    readlink -m -- "$value"
}

positive_integer() { case "$1" in ''|*[!0-9]*|0) return 1 ;; *) return 0 ;; esac; }
nonnegative_integer() { case "$1" in ''|*[!0-9]*) return 1 ;; *) return 0 ;; esac; }

OUTPUT_DIR=$(absolute_path --output-dir "$OUTPUT_DIR")
case "$OUTPUT_DIR" in
    /|/etc|/var|/var/lib) die "--output-dir must be a dedicated child directory" ;;
esac
RUNTIME_ROOT=$(absolute_path --runtime-root "$RUNTIME_ROOT")
REPORT_PATH=$(absolute_path --report-path "$REPORT_PATH")
NPU_MANIFEST=$(absolute_path --npu-manifest "$NPU_MANIFEST")
NPU_RUNTIME=$(absolute_path --npu-runtime "$NPU_RUNTIME")
NPU_EXECUTABLE=$(absolute_path --npu-executable "$NPU_EXECUTABLE")
NPU_OUTPUT_DIR=$(absolute_path --npu-output-dir "$NPU_OUTPUT_DIR")
MPI_LAUNCHER=$(absolute_path --mpi-launcher "$MPI_LAUNCHER")
if [ -n "$HPL_LIBRARY_DIR" ]; then HPL_LIBRARY_DIR=$(absolute_path --hpl-library-dir "$HPL_LIBRARY_DIR"); fi

if [ -z "$NUMACTL" ]; then NUMACTL=$(command -v numactl 2>/dev/null || true); fi
NUMACTL=$(absolute_path --numactl "$NUMACTL")
if [ -z "$CPU_MANIFEST" ]; then
    CPU_MANIFEST="$(dirname -- "$RUNTIME_ROOT")/manifests/build-manifest.json"
fi
CPU_MANIFEST=$(absolute_path --cpu-manifest "$CPU_MANIFEST")

[ -x "$ADAPTER_TEMPLATE" ] || [ -f "$ADAPTER_TEMPLATE" ] || die "adapter template is unavailable: $ADAPTER_TEMPLATE"
[ -x "$NUMACTL" ] || die "numactl is not executable: $NUMACTL"
[ -x "$MPI_LAUNCHER" ] || die "MPI launcher is not executable: $MPI_LAUNCHER"
[ -x "$NPU_RUNTIME" ] || die "NPU container runtime is not executable: $NPU_RUNTIME"
[ -f "$CPU_MANIFEST" ] || die "CPU build manifest is unavailable: $CPU_MANIFEST"
[ -f "$NPU_MANIFEST" ] || die "NPU image manifest is unavailable: $NPU_MANIFEST"
[ -d "$NPU_OUTPUT_DIR" ] || die "NPU result directory is unavailable: $NPU_OUTPUT_DIR"
[ -z "$HPL_LIBRARY_DIR" ] || [ -d "$HPL_LIBRARY_DIR" ] || die "HPL library directory is unavailable: $HPL_LIBRARY_DIR"

STREAM_EXECUTABLE="$RUNTIME_ROOT/stream/stream_omp"
HPL_WORKDIR="$RUNTIME_ROOT/hpl"
HPL_EXECUTABLE="$HPL_WORKDIR/xhpl"
HPCG_WORKDIR="$RUNTIME_ROOT/hpcg"
HPCG_EXECUTABLE="$HPCG_WORKDIR/xhpcg"
for executable in "$STREAM_EXECUTABLE" "$HPL_EXECUTABLE" "$HPCG_EXECUTABLE"; do
    [ -x "$executable" ] || die "CPU benchmark executable is unavailable: $executable"
done
[ -f "$HPL_WORKDIR/HPL.dat" ] || die "HPL.dat is unavailable: $HPL_WORKDIR/HPL.dat"
[ -f "$HPCG_WORKDIR/hpcg.dat" ] || die "hpcg.dat is unavailable: $HPCG_WORKDIR/hpcg.dat"

nonnegative_integer "$STREAM_THREADS" || die "--stream-threads must be a non-negative integer"
for pair in \
    "--hpl-processes:$HPL_PROCESSES" "--hpl-threads:$HPL_THREADS" \
    "--hpcg-processes:$HPCG_PROCESSES" "--hpcg-threads:$HPCG_THREADS" \
    "--hpcg-nx:$HPCG_NX" "--hpcg-ny:$HPCG_NY" "--hpcg-nz:$HPCG_NZ" \
    "--hpcg-runtime:$HPCG_RUNTIME" "--npu-internal-timeout:$NPU_INTERNAL_TIMEOUT"; do
    positive_integer "${pair#*:}" || die "${pair%%:*} must be a positive integer"
done

case "$NPU_CONTAINER" in ''|*[!A-Za-z0-9_.-]*) die "--npu-container has an invalid value" ;; esac
case "$NPU_IMAGE" in ''|*[[:space:]]*) die "--npu-image has an invalid value" ;; esac
if [ "$NPU_DEVICE" != all ] && ! [[ "$NPU_DEVICE" =~ ^[0-9]+(,[0-9]+)*$ ]]; then
    die "--npu-device must be all or comma-separated logical IDs"
fi
case "$NPU_CHIP_GENERATION" in A2|A3|A5) ;; *) die "--npu-chip-generation must be A2, A3 or A5" ;; esac
[ -n "$NPU_CANN" ] || die "--npu-cann is required"
[ -n "$NPU_TORCH_NPU" ] || die "--npu-torch-npu is required"
[ -n "$NPU_SOC" ] || die "--npu-soc is required"
if { [ -n "$NPU_RUN_CASE" ] && [ -n "$NPU_GROUP" ]; } ||
   { [ -z "$NPU_RUN_CASE" ] && [ -z "$NPU_GROUP" ]; }; then
    die "exactly one of --npu-run-case or --npu-group is required"
fi

ADAPTER_PATH="$OUTPUT_DIR/benchmark_check.sh"
CONFIG_PATH="$OUTPUT_DIR/catmonitor-stress.yaml"
MANIFEST_PATH="$OUTPUT_DIR/stress-deployment-manifest.json"
for target in "$ADAPTER_PATH" "$CONFIG_PATH" "$MANIFEST_PATH"; do
    if [ -e "$target" ] && [ "$FORCE" != true ]; then
        die "generated file already exists; use --force to replace it: $target"
    fi
    [ ! -L "$target" ] || die "generated file cannot be a symbolic link: $target"
done

install -d -m 0750 "$OUTPUT_DIR"
ADAPTER_TEMP=$(mktemp "$OUTPUT_DIR/.benchmark_check.XXXXXXXX")
CONFIG_TEMP=$(mktemp "$OUTPUT_DIR/.catmonitor-stress.XXXXXXXX")
MANIFEST_TEMP=$(mktemp "$OUTPUT_DIR/.stress-deployment-manifest.XXXXXXXX")
cleanup() { rm -f -- "$ADAPTER_TEMP" "$CONFIG_TEMP" "$MANIFEST_TEMP"; }
trap cleanup EXIT HUP INT TERM

declare -A OVERRIDE=(
    [STREAM_EXECUTABLE]="$STREAM_EXECUTABLE"
    [STREAM_NUMACTL]="$NUMACTL"
    [STREAM_THREADS]="$STREAM_THREADS"
    [HPL_WORKDIR]="$HPL_WORKDIR"
    [HPL_EXECUTABLE]="$HPL_EXECUTABLE"
    [HPL_LIBRARY_DIR]="$HPL_LIBRARY_DIR"
    [HPL_MPI_LAUNCHER]="$MPI_LAUNCHER"
    [HPL_MPI_PROCESSES]="$HPL_PROCESSES"
    [HPL_THREADS_PER_PROCESS]="$HPL_THREADS"
    [HPCG_WORKDIR]="$HPCG_WORKDIR"
    [HPCG_EXECUTABLE]="$HPCG_EXECUTABLE"
    [HPCG_MPI_LAUNCHER]="$MPI_LAUNCHER"
    [HPCG_MPI_PROCESSES]="$HPCG_PROCESSES"
    [HPCG_THREADS_PER_PROCESS]="$HPCG_THREADS"
    [HPCG_NX]="$HPCG_NX"
    [HPCG_NY]="$HPCG_NY"
    [HPCG_NZ]="$HPCG_NZ"
    [HPCG_RUNTIME_SECONDS]="$HPCG_RUNTIME"
    [NPU_BURN_BACKEND]=docker_exec
    [NPU_BURN_EXECUTABLE]="$NPU_EXECUTABLE"
    [NPU_BURN_CONTAINER_RUNTIME]="$NPU_RUNTIME"
    [NPU_BURN_CONTAINER_NAME]="$NPU_CONTAINER"
    [NPU_BURN_CONTAINER_IMAGE]="$NPU_IMAGE"
    [NPU_BURN_RUNTIME_CANN]="$NPU_CANN"
    [NPU_BURN_RUNTIME_TORCH_NPU]="$NPU_TORCH_NPU"
    [NPU_BURN_SOC_MODEL]="$NPU_SOC"
    [NPU_BURN_OUTPUT_DIR]="$NPU_OUTPUT_DIR"
    [NPU_BURN_RUN_CASE]="$NPU_RUN_CASE"
    [NPU_BURN_GROUP]="$NPU_GROUP"
    [NPU_BURN_DEVICE]="$NPU_DEVICE"
    [NPU_BURN_INTERNAL_TIMEOUT_SECONDS]="$NPU_INTERNAL_TIMEOUT"
    [NPU_BURN_CHIP_GENERATION]="$NPU_CHIP_GENERATION"
)

while IFS= read -r line || [ -n "$line" ]; do
    if [[ "$line" =~ ^([A-Z][A-Z0-9_]*)= ]] && [ -n "${OVERRIDE[${BASH_REMATCH[1]}]+x}" ]; then
        key=${BASH_REMATCH[1]}
        printf '%s=%q\n' "$key" "${OVERRIDE[$key]}"
    else
        printf '%s\n' "$line"
    fi
done <"$ADAPTER_TEMPLATE" >"$ADAPTER_TEMP"
chmod 0750 "$ADAPTER_TEMP"
bash -n "$ADAPTER_TEMP"

yaml_quote() {
    local value=$1
    value=${value//\\/\\\\}
    value=${value//\"/\\\"}
    printf '"%s"' "$value"
}

{
    printf '# Generated by scripts/stress/generate_stress_deployment.sh.\n'
    printf '# Merge this top-level stress block into the shared CATMonitor config,\n'
    printf '# or pass this file directly with -c for stress CLI/Web validation.\n'
    printf 'stress:\n'
    printf '  enabled: true\n'
    printf '  web_enabled: %s\n' "$WEB_ENABLED"
    printf '  script_path: '; yaml_quote "$ADAPTER_PATH"; printf '\n'
    printf '  report_path: '; yaml_quote "$REPORT_PATH"; printf '\n'
    printf '  default_benchmarks: [stream]\n'
    printf '  benchmarks:\n'
    printf '    stream: { enabled: true, timeout: 1m }\n'
    printf '    hpl: { enabled: true, timeout: 10m }\n'
    printf '    hpcg:\n'
    printf '      enabled: true\n'
    printf '      result_dir: '; yaml_quote "$HPCG_WORKDIR"; printf '\n'
    printf '      timeout: 3m\n'
    printf '    npu_burn: { enabled: true, timeout: 30m }\n'
} >"$CONFIG_TEMP"
chmod 0640 "$CONFIG_TEMP"

json_escape() {
    local value=${1-}
    value=${value//\\/\\\\}; value=${value//\"/\\\"}
    value=${value//$'\n'/\\n}; value=${value//$'\r'/\\r}; value=${value//$'\t'/\\t}
    printf '%s' "$value"
}
json_string() { printf '"%s"' "$(json_escape "${1-}")"; }

ADAPTER_SHA256=$(sha256sum -- "$ADAPTER_TEMP" | awk '{print $1}')
CONFIG_SHA256=$(sha256sum -- "$CONFIG_TEMP" | awk '{print $1}')
CPU_MANIFEST_SHA256=$(sha256sum -- "$CPU_MANIFEST" | awk '{print $1}')
NPU_MANIFEST_SHA256=$(sha256sum -- "$NPU_MANIFEST" | awk '{print $1}')
{
    printf '{"schema_version":1,"feature":"stress","generated_at_utc":'; json_string "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf ',"adapter":{"path":'; json_string "$ADAPTER_PATH"; printf ',"sha256":'; json_string "$ADAPTER_SHA256"; printf '}'
    printf ',"config":{"path":'; json_string "$CONFIG_PATH"; printf ',"sha256":'; json_string "$CONFIG_SHA256"; printf '}'
    printf ',"inputs":{"cpu_build_manifest":{"path":'; json_string "$CPU_MANIFEST"; printf ',"sha256":'; json_string "$CPU_MANIFEST_SHA256"; printf '}'
    printf ',"npu_image_manifest":{"path":'; json_string "$NPU_MANIFEST"; printf ',"sha256":'; json_string "$NPU_MANIFEST_SHA256"; printf '}}'
    printf ',"profile":{"stream_threads":%s,"hpl_mpi_processes":%s,"hpl_threads_per_process":%s' "$STREAM_THREADS" "$HPL_PROCESSES" "$HPL_THREADS"
    printf ',"hpcg_mpi_processes":%s,"hpcg_threads_per_process":%s,"hpcg_grid":' "$HPCG_PROCESSES" "$HPCG_THREADS"; json_string "${HPCG_NX}x${HPCG_NY}x${HPCG_NZ}"
    printf ',"hpcg_runtime_seconds":%s,"npu_container":' "$HPCG_RUNTIME"; json_string "$NPU_CONTAINER"
    printf ',"npu_image":'; json_string "$NPU_IMAGE"; printf ',"npu_device":'; json_string "$NPU_DEVICE"
    printf ',"npu_chip_generation":'; json_string "$NPU_CHIP_GENERATION"; printf ',"npu_workload":'; json_string "${NPU_RUN_CASE:-$NPU_GROUP}"
    printf ',"npu_internal_timeout_seconds":%s}}\n' "$NPU_INTERNAL_TIMEOUT"
} >"$MANIFEST_TEMP"
chmod 0640 "$MANIFEST_TEMP"

mv -f -- "$ADAPTER_TEMP" "$ADAPTER_PATH"
mv -f -- "$CONFIG_TEMP" "$CONFIG_PATH"
mv -f -- "$MANIFEST_TEMP" "$MANIFEST_PATH"
trap - EXIT HUP INT TERM

printf 'Stress adapter: %s\n' "$ADAPTER_PATH"
printf 'Stress config: %s\n' "$CONFIG_PATH"
printf 'Deployment manifest: %s\n' "$MANIFEST_PATH"
printf 'Next: catmonitor stress doctor -c %s -o table\n' "$CONFIG_PATH"
