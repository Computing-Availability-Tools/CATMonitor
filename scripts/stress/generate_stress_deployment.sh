#!/usr/bin/env bash
# Generate a daemon-owned Stress Controller configuration and workload Compose
# override. This command never builds images, creates containers, or runs a
# workload.

set -euo pipefail

OUTPUT_DIR=
CONTROL_IMAGE=
CPU_IMAGE=
CPU_ENABLED=false
NPU_IMAGE=
CPU_MANIFEST=
NPU_MANIFEST=
DOCKER_SOCKET=/var/run/docker.sock
REPORT_PATH=/var/lib/catmonitor/stress/stress-latest.json
STREAM_THREADS=0
HPL_PROCESSES=1
HPL_THREADS=1
HPCG_PROCESSES=1
HPCG_THREADS=1
HPCG_NX=32
HPCG_NY=32
HPCG_NZ=32
HPCG_RUNTIME=60
WEB_ENABLED=false
NPU_HOST_ROOT=/
NPU_DEVICE_ROOT=/dev
NPU_DEVICE_NODES=
NPU_BURN_DEVICE=
NPU_RUNTIME=runc
NPU_OUTPUT_DIR=/var/lib/catmonitor/stress/npu-burn-output
NPU_RUN_CASE=matmul
NPU_GROUP=
NPU_CHIP_GENERATION=
NPU_INTERNAL_TIMEOUT=300
FORCE=false

usage() {
    cat <<'EOF'
Usage: generate_stress_deployment.sh [OPTIONS]

Required:
  --output-dir PATH          Dedicated output directory

At least one workload profile:
  --cpu-image IMAGE          Reviewed CPU workload image
  --npu-image IMAGE          Reviewed NPU Burn workload image

Optional release identity:
  --control-image IMAGE      Control image recorded in the node profile
  --cpu-manifest PATH        CPU image/build manifest recorded by SHA-256
  --npu-manifest PATH        NPU image manifest recorded by SHA-256

CPU workload profile:
  --stream-threads N         0 leaves OMP_NUM_THREADS unset
  --hpl-processes N          HPL MPI ranks (default: 1)
  --hpl-threads N            HPL threads per rank (default: 1)
  --hpcg-processes N         HPCG MPI ranks (default: 1)
  --hpcg-threads N           HPCG threads per rank (default: 1)
  --hpcg-nx/--hpcg-ny/--hpcg-nz N
  --hpcg-runtime N           HPCG target seconds (default: 60)

NPU workload profile (enabled only when --npu-image is supplied):
  --npu-host-root PATH       Fixture root for host discovery (default: /)
  --npu-device-root PATH     Host device directory (default: /dev)
  --npu-device-nodes IDS     /dev/davinci IDs, comma separated; default: discover
  --npu-burn-device IDS      NPU Burn logical IDs, comma separated, or all
  --npu-runtime NAME         Docker runtime (default: runc)
  --npu-output-dir PATH      Host result directory
  --npu-run-case NAME        Fixed upstream run case (default: matmul)
  --npu-group NAME           Fixed upstream group; excludes --npu-run-case
  --npu-chip-generation ID   A2, A3, or A5 profile identity
  --npu-internal-timeout N   Per-case timeout seconds (default: 300)

Controller:
  --docker-socket PATH       Daemon Docker socket
  --report-path PATH         Daemon report path
  --enable-web               Enable Run/Cancel on the unified :19322 Web listener
  --force                    Replace existing generated files
  -h, --help                 Show this help

Outputs:
  catmonitor-stress.yaml
  stress-profile.json
  docker-compose.stress.generated.yml
  stress-deployment-manifest.json
EOF
}

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
require_value() { [ "$#" -ge 2 ] && [ -n "$2" ] || die "$1 requires a value"; }
while [ "$#" -gt 0 ]; do
    case "$1" in
        --output-dir) require_value "$@"; OUTPUT_DIR=$2; shift 2 ;;
        --control-image) require_value "$@"; CONTROL_IMAGE=$2; shift 2 ;;
        --cpu-image) require_value "$@"; CPU_IMAGE=$2; shift 2 ;;
        --npu-image) require_value "$@"; NPU_IMAGE=$2; shift 2 ;;
        --cpu-manifest) require_value "$@"; CPU_MANIFEST=$2; shift 2 ;;
        --npu-manifest) require_value "$@"; NPU_MANIFEST=$2; shift 2 ;;
        --docker-socket) require_value "$@"; DOCKER_SOCKET=$2; shift 2 ;;
        --report-path) require_value "$@"; REPORT_PATH=$2; shift 2 ;;
        --stream-threads) require_value "$@"; STREAM_THREADS=$2; shift 2 ;;
        --hpl-processes) require_value "$@"; HPL_PROCESSES=$2; shift 2 ;;
        --hpl-threads) require_value "$@"; HPL_THREADS=$2; shift 2 ;;
        --hpcg-processes) require_value "$@"; HPCG_PROCESSES=$2; shift 2 ;;
        --hpcg-threads) require_value "$@"; HPCG_THREADS=$2; shift 2 ;;
        --hpcg-nx) require_value "$@"; HPCG_NX=$2; shift 2 ;;
        --hpcg-ny) require_value "$@"; HPCG_NY=$2; shift 2 ;;
        --hpcg-nz) require_value "$@"; HPCG_NZ=$2; shift 2 ;;
        --hpcg-runtime) require_value "$@"; HPCG_RUNTIME=$2; shift 2 ;;
        --npu-host-root) require_value "$@"; NPU_HOST_ROOT=$2; shift 2 ;;
        --npu-device-root) require_value "$@"; NPU_DEVICE_ROOT=$2; shift 2 ;;
        --npu-device-nodes) require_value "$@"; NPU_DEVICE_NODES=$2; shift 2 ;;
        --npu-burn-device) require_value "$@"; NPU_BURN_DEVICE=$2; shift 2 ;;
        --npu-runtime) require_value "$@"; NPU_RUNTIME=$2; shift 2 ;;
        --npu-output-dir) require_value "$@"; NPU_OUTPUT_DIR=$2; shift 2 ;;
        --npu-run-case) require_value "$@"; NPU_RUN_CASE=$2; shift 2 ;;
        --npu-group) require_value "$@"; NPU_GROUP=$2; NPU_RUN_CASE=; shift 2 ;;
        --npu-chip-generation) require_value "$@"; NPU_CHIP_GENERATION=$2; shift 2 ;;
        --npu-internal-timeout) require_value "$@"; NPU_INTERNAL_TIMEOUT=$2; shift 2 ;;
        --enable-web) WEB_ENABLED=true; shift ;;
        --force) FORCE=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown argument: $1" ;;
    esac
done

absolute() {
    local option=$1 value=$2
    [ -n "$value" ] || die "$option is required"
    case "$value" in /*) ;; *) die "$option must be absolute: $value" ;; esac
    case "$value" in *$'\n'*|*$'\r'*) die "$option cannot contain a newline" ;; esac
    readlink -m -- "$value"
}
positive() { case "$1" in ''|0|*[!0-9]*) return 1 ;; *) return 0 ;; esac; }
nonnegative() { case "$1" in ''|*[!0-9]*) return 1 ;; *) return 0 ;; esac; }
safe_token() { case "$1" in ''|-*|*[!A-Za-z0-9._/:@+-]*) return 1 ;; *) return 0 ;; esac; }
valid_ids() {
    local value=$1 item
    [ "$value" = all ] && return 0
    case "$value" in ''|,*|*,|*,,*) return 1 ;; esac
    IFS=, read -r -a parts <<<"$value"
    for item in "${parts[@]}"; do case "$item" in ''|*[!0-9]*) return 1 ;; esac; done
}
json_string() {
    local value=$1
    value=${value//\\/\\\\}; value=${value//\"/\\\"}
    value=${value//$'\n'/\\n}; value=${value//$'\r'/\\r}; value=${value//$'\t'/\\t}
    printf '"%s"' "$value"
}
yaml_quote() { local value=$1; value=${value//\'/\'\'}; printf "'%s'" "$value"; }
sha_or_null() {
    local path=$1
    if [ -z "$path" ]; then printf null; return; fi
    path=$(absolute manifest "$path")
    [ -f "$path" ] || die "manifest is unavailable: $path"
    json_string "$(sha256sum -- "$path" | awk '{print $1}')"
}

OUTPUT_DIR=$(absolute --output-dir "$OUTPUT_DIR")
case "$OUTPUT_DIR" in /|/etc|/var|/var/lib) die "--output-dir must be a dedicated child directory" ;; esac
DOCKER_SOCKET=$(absolute --docker-socket "$DOCKER_SOCKET")
REPORT_PATH=$(absolute --report-path "$REPORT_PATH")
NPU_HOST_ROOT=$(absolute --npu-host-root "$NPU_HOST_ROOT")
NPU_DEVICE_ROOT=$(absolute --npu-device-root "$NPU_DEVICE_ROOT")
NPU_OUTPUT_DIR=$(absolute --npu-output-dir "$NPU_OUTPUT_DIR")
if [ -n "$CPU_IMAGE" ]; then
    safe_token "$CPU_IMAGE" || die "--cpu-image has an invalid value"
    CPU_ENABLED=true
fi
[ -z "$CONTROL_IMAGE" ] || safe_token "$CONTROL_IMAGE" || die "--control-image has an invalid value"
[ -z "$NPU_IMAGE" ] || safe_token "$NPU_IMAGE" || die "--npu-image has an invalid value"
nonnegative "$STREAM_THREADS" || die "--stream-threads must be non-negative"
for value in "$HPL_PROCESSES" "$HPL_THREADS" "$HPCG_PROCESSES" "$HPCG_THREADS" "$HPCG_NX" "$HPCG_NY" "$HPCG_NZ" "$HPCG_RUNTIME" "$NPU_INTERNAL_TIMEOUT"; do
    positive "$value" || die "CPU/NPU resource values must be positive integers"
done

NPU_ENABLED=false
device_ids=()
if [ -n "$NPU_IMAGE" ]; then
    NPU_ENABLED=true
    safe_token "$NPU_RUNTIME" || die "--npu-runtime has an invalid value"
    [ -n "$NPU_BURN_DEVICE" ] && valid_ids "$NPU_BURN_DEVICE" || die "--npu-burn-device is required and must contain logical IDs or all"
    [ -n "$NPU_RUN_CASE" ] || [ -n "$NPU_GROUP" ] || die "NPU profile requires a run case or group"
    [ -z "$NPU_RUN_CASE" ] || safe_token "$NPU_RUN_CASE" || die "invalid NPU run case"
    [ -z "$NPU_GROUP" ] || safe_token "$NPU_GROUP" || die "invalid NPU group"
    if [ -n "$NPU_DEVICE_NODES" ]; then
        valid_ids "$NPU_DEVICE_NODES" || die "--npu-device-nodes must contain numeric IDs"
        IFS=, read -r -a device_ids <<<"$NPU_DEVICE_NODES"
    else
        shopt -s nullglob
        for path in "$NPU_HOST_ROOT${NPU_DEVICE_ROOT}"/davinci[0-9]*; do
            id=${path##*davinci}
            case "$id" in ''|*[!0-9]*) continue ;; esac
            device_ids+=("$id")
        done
        shopt -u nullglob
    fi
    [ "${#device_ids[@]}" -gt 0 ] || die "no /dev/davinciN device nodes were selected"
    mapfile -t device_ids < <(printf '%s\n' "${device_ids[@]}" | sort -nu)
    for id in "${device_ids[@]}"; do
        [ -e "$NPU_HOST_ROOT${NPU_DEVICE_ROOT}/davinci$id" ] || die "NPU device node is unavailable: ${NPU_DEVICE_ROOT}/davinci$id"
    done
    for path in /dev/davinci_manager /dev/devmm_svm /dev/hisi_hdc /usr/local/Ascend/driver/lib64 /usr/local/Ascend/driver/version.info /etc/ascend_install.info /usr/local/dcmi /usr/local/bin/npu-smi; do
        [ -e "$NPU_HOST_ROOT$path" ] || die "required Ascend host path is unavailable: $path"
    done
    logical_device_ids=()
    for ((i = 0; i < ${#device_ids[@]}; i++)); do logical_device_ids+=("$i"); done
    logical_device_csv=$(IFS=,; printf '%s' "${logical_device_ids[*]}")
fi
[ "$CPU_ENABLED" = true ] || [ "$NPU_ENABLED" = true ] || die "configure at least one of --cpu-image or --npu-image"

files=(catmonitor-stress.yaml stress-profile.json docker-compose.stress.generated.yml stress-deployment-manifest.json)
install -d -m 0750 "$OUTPUT_DIR"
for file in "${files[@]}"; do
    if [ -e "$OUTPUT_DIR/$file" ] && [ "$FORCE" != true ]; then die "output exists; use --force: $OUTPUT_DIR/$file"; fi
done

DEFAULT_BENCHMARK=stream
if [ "$CPU_ENABLED" != true ]; then DEFAULT_BENCHMARK=npu_burn; fi

CONFIG="$OUTPUT_DIR/catmonitor-stress.yaml"
PROFILE="$OUTPUT_DIR/stress-profile.json"
COMPOSE="$OUTPUT_DIR/docker-compose.stress.generated.yml"
MANIFEST="$OUTPUT_DIR/stress-deployment-manifest.json"

cat >"$CONFIG" <<EOF
stress:
  enabled: true
  web_enabled: $WEB_ENABLED
  control_socket: /run/catmonitor/control.sock
  report_path: $(yaml_quote "$REPORT_PATH")
  default_benchmarks: [$DEFAULT_BENCHMARK]
  executor:
    type: docker_exec
    docker_binary: /usr/bin/docker
    docker_socket: $(yaml_quote "$DOCKER_SOCKET")
  benchmarks:
    stream: { enabled: $CPU_ENABLED, plugin: stream, container: catmonitor-stress-cpu, user: '65532:65532', timeout: 1m }
    hpl: { enabled: $CPU_ENABLED, plugin: hpl, container: catmonitor-stress-cpu, user: '65532:65532', timeout: 2h }
    hpcg: { enabled: $CPU_ENABLED, plugin: hpcg, container: catmonitor-stress-cpu, user: '65532:65532', timeout: 3m }
    npu_burn: { enabled: $NPU_ENABLED, plugin: npu_burn, container: catmonitor-stress-npu, timeout: 30m }
EOF

cat >"$COMPOSE" <<EOF
services:
  catmonitor:
    volumes:
      - type: bind
        source: $CONFIG
        target: /etc/catmonitor/catmonitor.yaml
        read_only: true
        bind: { create_host_path: false }
EOF

if [ "$CPU_ENABLED" = true ]; then
    cat >>"$COMPOSE" <<EOF
  catmonitor-stress-cpu:
    image: $(yaml_quote "$CPU_IMAGE")
    environment:
      STREAM_THREADS: '$STREAM_THREADS'
      HPL_MPI_PROCESSES: '$HPL_PROCESSES'
      HPL_THREADS_PER_PROCESS: '$HPL_THREADS'
      HPCG_MPI_PROCESSES: '$HPCG_PROCESSES'
      HPCG_THREADS_PER_PROCESS: '$HPCG_THREADS'
      HPCG_NX: '$HPCG_NX'
      HPCG_NY: '$HPCG_NY'
      HPCG_NZ: '$HPCG_NZ'
      HPCG_RUNTIME_SECONDS: '$HPCG_RUNTIME'
EOF
fi

if [ "$NPU_ENABLED" = true ]; then
    cat >>"$COMPOSE" <<EOF
  catmonitor-stress-npu:
    image: $(yaml_quote "$NPU_IMAGE")
    runtime: $(yaml_quote "$NPU_RUNTIME")
    privileged: true

    environment:
      CATMONITOR_NPU_DEVICE_COUNT: '${#device_ids[@]}'
      ASCEND_RT_VISIBLE_DEVICES: $(yaml_quote "$logical_device_csv")
      NPU_BURN_DEVICE: $(yaml_quote "$NPU_BURN_DEVICE")
      NPU_BURN_RUN_CASE: $(yaml_quote "$NPU_RUN_CASE")
      NPU_BURN_GROUP: $(yaml_quote "$NPU_GROUP")
      NPU_BURN_CHIP_GENERATION: $(yaml_quote "$NPU_CHIP_GENERATION")
      NPU_BURN_INTERNAL_TIMEOUT_SECONDS: '$NPU_INTERNAL_TIMEOUT'
    devices:
EOF
    for id in "${device_ids[@]}"; do printf '      - %s/davinci%s:/dev/davinci%s\n' "$NPU_DEVICE_ROOT" "$id" "$id" >>"$COMPOSE"; done
    for path in /dev/davinci_manager /dev/devmm_svm /dev/hisi_hdc; do printf '      - %s:%s\n' "$path" "$path" >>"$COMPOSE"; done
    cat >>"$COMPOSE" <<EOF
    volumes:
      - /usr/local/Ascend/driver/lib64:/usr/local/Ascend/driver/lib64:ro
      - /usr/local/Ascend/driver/version.info:/usr/local/Ascend/driver/version.info:ro
      - /etc/ascend_install.info:/etc/ascend_install.info:ro
      - /usr/local/dcmi:/usr/local/dcmi:ro
      - /usr/local/bin/npu-smi:/usr/local/bin/npu-smi:ro
      - $(yaml_quote "$NPU_OUTPUT_DIR:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output")
EOF
fi

generated_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
device_json='[]'
if [ "$NPU_ENABLED" = true ]; then
    device_json=$(printf '%s\n' "${device_ids[@]}" | awk 'BEGIN{printf "["} {if(NR>1)printf ",";printf "%d",$1} END{printf "]"}')
fi
cat >"$PROFILE" <<EOF
{"schema_version":2,"generated_at_utc":$(json_string "$generated_at"),"architecture":"daemon_controller_docker_exec","control_image":$(if [ -n "$CONTROL_IMAGE" ]; then json_string "$CONTROL_IMAGE"; else printf null; fi),"cpu":{"enabled":$CPU_ENABLED,"image":$(if [ -n "$CPU_IMAGE" ]; then json_string "$CPU_IMAGE"; else printf null; fi),"container":"catmonitor-stress-cpu","plugins":["stream","hpl","hpcg"],"user":"65532:65532","resources":{"stream_threads":$STREAM_THREADS,"hpl_processes":$HPL_PROCESSES,"hpl_threads":$HPL_THREADS,"hpcg_processes":$HPCG_PROCESSES,"hpcg_threads":$HPCG_THREADS}},"npu":{"enabled":$NPU_ENABLED,"image":$(if [ -n "$NPU_IMAGE" ]; then json_string "$NPU_IMAGE"; else printf null; fi),"container":"catmonitor-stress-npu","host_device_ids":$device_json,"runtime_visible_device_ids":$(if [ "$NPU_ENABLED" = true ]; then json_string "$logical_device_csv"; else printf null; fi),"burn_logical_ids":$(json_string "$NPU_BURN_DEVICE"),"privileged":$NPU_ENABLED},"security":{"daemon_docker_socket":true,"web_docker_socket":false,"dfee_docker_socket":false,"npu_workload_privileged":$NPU_ENABLED,"debt":"daemon Docker socket and the enabled NPU workload privilege are temporary V2 boundaries"}}
EOF

config_sha=$(sha256sum "$CONFIG" | awk '{print $1}')
profile_sha=$(sha256sum "$PROFILE" | awk '{print $1}')
compose_sha=$(sha256sum "$COMPOSE" | awk '{print $1}')
cat >"$MANIFEST" <<EOF
{"schema_version":2,"generated_at_utc":$(json_string "$generated_at"),"config_sha256":"$config_sha","profile_sha256":"$profile_sha","compose_sha256":"$compose_sha","cpu_manifest_sha256":$(sha_or_null "$CPU_MANIFEST"),"npu_manifest_sha256":$(sha_or_null "$NPU_MANIFEST")}
EOF

chmod 0640 "$CONFIG" "$PROFILE" "$COMPOSE" "$MANIFEST"
printf 'Generated daemon/controller Stress deployment: %s\n' "$OUTPUT_DIR"
printf 'CPU profile: %s' "$CPU_ENABLED"
if [ "$CPU_ENABLED" = true ]; then printf ' (%s)' "$CPU_IMAGE"; fi
printf '\n'
printf 'NPU profile: %s\n' "$NPU_ENABLED"
