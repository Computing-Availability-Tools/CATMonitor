#!/usr/bin/env bash
# Generate a daemon-owned Stress Controller configuration and workload Compose
# override. This command never builds images, creates containers, or runs a
# workload.

set -euo pipefail

OUTPUT_DIR=
CONTROL_IMAGE=
CPU_IMAGE=
CPU_ENABLED=false
CPU_CONTAINER=catmonitor-stress-cpu
NPU_CONTAINER=catmonitor-stress-npu
NPU_IMAGE=
CPU_MANIFEST=
NPU_MANIFEST=
DOCKER_SOCKET=/var/run/docker.sock
REPORT_PATH=/var/lib/catmonitor/stress/stress-latest.json
STREAM_THREADS=0
HPL_PROCESSES=1
HPL_THREADS=1
HPL_PROFILE=small
HPL_N=
HPL_NB=
HPL_P=
HPL_Q=
HPCG_PROCESSES=1
HPCG_THREADS=1
HPCG_NX=32
HPCG_NY=32
HPCG_NZ=32
HPCG_RUNTIME=60
# Invoking this generator explicitly opts into Stress. Monitoring-only config
# defaults remain disabled; generated Stress deployments expose the complete UI.
WEB_ENABLED=true
NPU_HOST_ROOT=/
NPU_DEVICE_ROOT=/dev
NPU_DEVICE_NODES=
NPU_BURN_DEVICE=0
NPU_RUNTIME=runc
NPU_OUTPUT_DIR=/var/lib/catmonitor/stress/npu-burn-output
NPU_RUN_CASE=matmul
NPU_GROUP=
NPU_CHIP_GENERATION=
NPU_INTERNAL_TIMEOUT=300
NPU_DEVICE_MODE=full
NPU_THREADS=1
NPU_ASCEND_ENV_SCRIPT=
NPU_ALLOWED_DEVICES=all
NPU_ALLOWED_MODELS=auto
NPU_REFERENCE_GENERATION=auto
NPU_MODEL_MEMORY_MODE=standard
NPU_CONFIG_DIR=
NPU_CONFIG_IDENTITY=null
NPU_JOB_TIMEOUT=2h
NPU_SECURITY_PROFILE=
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
  --cpu-container NAME      Existing/custom workload container binding
  --hpl-profile NAME        small (default: 4096/256/1x1) or image (original input)
  --hpl-n/--hpl-nb/--hpl-p/--hpl-q N
                            Explicit dimensions; all four required together
  --stream-threads N         0 leaves OMP_NUM_THREADS unset
  --hpl-processes N          HPL MPI ranks (default: 1)
  --hpl-threads N            HPL threads per rank (default: 1)
  --hpcg-processes N         HPCG MPI ranks (default: 1)
  --hpcg-threads N           HPCG threads per rank (default: 1)
  --hpcg-nx/--hpcg-ny/--hpcg-nz N
  --hpcg-runtime N           HPCG target seconds (default: 60)

NPU workload profile (enabled only when --npu-image is supplied):
  --npu-container NAME      Existing/custom workload container binding
  --npu-allowed-devices IDS  Administrator logical allowlist (default: all mapped)
  --npu-allowed-models NAMES gpt3,llama2 (default), either model, or none to disable
  --allow-npu-reference-generation
                            Allow single-device immutable references (default with models)
  --deny-npu-reference-generation
                            Disable reference creation; retain model comparison
  --npu-model-memory-mode M  standard (default) or checkpointed; full model shapes
  --npu-config-dir PATH      Complete external JSON config directory (read-only)
  --npu-job-timeout DURATION Positive s/m/h job limit (default: 2h; not run duration)
  --npu-security-profile P   restricted or legacy-privileged; single is restricted
  --npu-host-root PATH       Fixture root for host discovery (default: /)
  --npu-device-root PATH     Host device directory (default: /dev)
  --npu-device-nodes IDS     /dev/davinci IDs, comma separated; default: discover
  --npu-burn-device IDS      NPU Burn logical IDs, comma separated, or all (default: 0)
  --npu-runtime NAME         Docker runtime (default: runc)
  --npu-output-dir PATH      Host result directory
  --npu-run-case NAME        Deployment default run case (default: matmul)
  --npu-group NAME           Deployment default group; excludes --npu-run-case
  --npu-chip-generation ID   A2, A3, or A5 profile identity
  --npu-internal-timeout N   Stream synchronization timeout (default: 300 seconds)
  --npu-device-mode MODE     full (legacy) or single (portable-runtime-v1 required)
  --npu-threads N            BLAS/OpenMP thread budget (default: 1; not a performance preset)
  --npu-ascend-env-script P   Explicit absolute CANN script inside workload image

Controller:
  --docker-socket PATH       Daemon Docker socket
  --report-path PATH         Daemon report path
  --enable-web               Enable Run/Cancel on unified :19322 (default)
  --disable-web              Disable Web Run/Cancel; retain CLI and monitoring
  --force                    Replace existing generated files
  -h, --help                 Show this help

Outputs:
  catmonitor-stress.yaml
  stress-profile.json
  docker-compose.stress.generated.yml
  stress-deployment-manifest.json
  cpu-workload.env (only when CPU is enabled; for manual --env-file)
  npu-workload.env (only when NPU is enabled; for manual --env-file)

Defaults enable capabilities, never start jobs, select all cards, or generate
references automatically. Use trusted management networks: Run/Cancel has no
authentication/RBAC. Device idle, reference identity and result checks remain.
EOF
}

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
require_value() { [ "$#" -ge 2 ] && [ -n "$2" ] || die "$1 requires a value"; }
while [ "$#" -gt 0 ]; do
    case "$1" in
        --output-dir) require_value "$@"; OUTPUT_DIR=$2; shift 2 ;;
        --control-image) require_value "$@"; CONTROL_IMAGE=$2; shift 2 ;;
        --cpu-image) require_value "$@"; CPU_IMAGE=$2; shift 2 ;;
        --cpu-container) require_value "$@"; CPU_CONTAINER=$2; shift 2 ;;
        --npu-container) require_value "$@"; NPU_CONTAINER=$2; shift 2 ;;
        --hpl-profile) require_value "$@"; HPL_PROFILE=$2; shift 2 ;;
        --hpl-n) require_value "$@"; HPL_N=$2; shift 2 ;;
        --hpl-nb) require_value "$@"; HPL_NB=$2; shift 2 ;;
        --hpl-p) require_value "$@"; HPL_P=$2; shift 2 ;;
        --hpl-q) require_value "$@"; HPL_Q=$2; shift 2 ;;
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
        --npu-device-mode) require_value "$@"; NPU_DEVICE_MODE=$2; shift 2 ;;
        --npu-threads) require_value "$@"; NPU_THREADS=$2; shift 2 ;;
        --npu-allowed-devices) require_value "$@"; NPU_ALLOWED_DEVICES=$2; shift 2 ;;
        --npu-allowed-models) require_value "$@"; NPU_ALLOWED_MODELS=$2; shift 2 ;;
        --allow-npu-reference-generation) NPU_REFERENCE_GENERATION=true; shift ;;
        --deny-npu-reference-generation) NPU_REFERENCE_GENERATION=false; shift ;;
        --npu-model-memory-mode) require_value "$@"; NPU_MODEL_MEMORY_MODE=$2; shift 2 ;;
        --npu-config-dir) require_value "$@"; NPU_CONFIG_DIR=$2; shift 2 ;;
        --npu-job-timeout) require_value "$@"; NPU_JOB_TIMEOUT=$2; shift 2 ;;
        --npu-security-profile) require_value "$@"; NPU_SECURITY_PROFILE=$2; shift 2 ;;
        --npu-ascend-env-script) require_value "$@"; NPU_ASCEND_ENV_SCRIPT=$2; shift 2 ;;
        --enable-web) WEB_ENABLED=true; shift ;;
        --disable-web) WEB_ENABLED=false; shift ;;
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
positive "$NPU_THREADS" || die "--npu-threads must be positive"
case "$NPU_DEVICE_MODE" in full|single) ;; *) die "--npu-device-mode must be full or single" ;; esac
if [ -n "$NPU_ASCEND_ENV_SCRIPT" ]; then
    NPU_ASCEND_ENV_SCRIPT=$(absolute --npu-ascend-env-script "$NPU_ASCEND_ENV_SCRIPT")
fi
NPU_PRIVILEGED=false
[[ "$CPU_CONTAINER" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]] || die "invalid CPU container name"
[[ "$NPU_CONTAINER" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]] || die "invalid NPU container name"
[ "$CPU_CONTAINER" != "$NPU_CONTAINER" ] || die "CPU and NPU containers must be distinct"
case "$HPL_PROFILE" in
    image) ;;
    small)
        # Set one complete default tuple. Partial administrator overrides still
        # require all four dimensions and must not silently inherit a grid.
        if [ -z "$HPL_N$HPL_NB$HPL_P$HPL_Q" ]; then
            HPL_N=4096; HPL_NB=256; HPL_P=1; HPL_Q=1
        fi ;;
    *) die "--hpl-profile must be image or small" ;;
esac
if [ -n "$HPL_N$HPL_NB$HPL_P$HPL_Q" ]; then
    for value in "$HPL_N" "$HPL_NB" "$HPL_P" "$HPL_Q"; do
        positive "$value" && [ "$value" -le 2147483647 ] || die "HPL N/NB/P/Q must all be positive bounded integers"
    done
    [ "$((HPL_P * HPL_Q))" -eq "$HPL_PROCESSES" ] || die "HPL P x Q must equal --hpl-processes"
fi
for value in "$HPL_PROCESSES" "$HPL_THREADS" "$HPCG_PROCESSES" "$HPCG_THREADS" "$HPCG_NX" "$HPCG_NY" "$HPCG_NZ" "$HPCG_RUNTIME" "$NPU_INTERNAL_TIMEOUT"; do
    positive "$value" || die "CPU/NPU resource values must be positive integers"
done

NPU_ENABLED=false
device_ids=()
if [ -n "$NPU_IMAGE" ]; then
    NPU_ENABLED=true
    if [ "$NPU_DEVICE_MODE" = full ]; then NPU_PRIVILEGED=true; fi
    case "$NPU_SECURITY_PROFILE" in
        '') ;;
        restricted) NPU_PRIVILEGED=false ;;
        legacy-privileged) [ "$NPU_DEVICE_MODE" != single ] || die "single mode cannot be privileged"; NPU_PRIVILEGED=true ;;
        *) die "invalid NPU security profile" ;;
    esac
    safe_token "$NPU_RUNTIME" || die "--npu-runtime has an invalid value"
    valid_ids "$NPU_BURN_DEVICE" || die "--npu-burn-device must contain logical IDs or all"
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
    if [ "$NPU_DEVICE_MODE" = single ]; then
        [ "${#device_ids[@]}" -eq 1 ] && [ "$NPU_BURN_DEVICE" = 0 ] || die "single mode requires one explicit host node and --npu-burn-device 0"
        [ -n "$NPU_DEVICE_NODES" ] || die "single mode requires explicit --npu-device-nodes authorization"
    fi
    for id in "${device_ids[@]}"; do
        [ -e "$NPU_HOST_ROOT${NPU_DEVICE_ROOT}/davinci$id" ] || die "NPU device node is unavailable: ${NPU_DEVICE_ROOT}/davinci$id"
    done
    for path in /dev/davinci_manager /dev/devmm_svm /dev/hisi_hdc /usr/local/Ascend/driver/lib64 /usr/local/Ascend/driver/version.info /etc/ascend_install.info /usr/local/dcmi /usr/local/bin/npu-smi; do
        [ -e "$NPU_HOST_ROOT$path" ] || die "required Ascend host path is unavailable: $path"
    done
    logical_device_ids=()
    for ((i = 0; i < ${#device_ids[@]}; i++)); do logical_device_ids+=("$i"); done
    logical_device_csv=$(IFS=,; printf '%s' "${logical_device_ids[*]}")
    if [ "$NPU_ALLOWED_DEVICES" = all ]; then NPU_ALLOWED_DEVICES=$logical_device_csv; fi
    valid_ids "$NPU_ALLOWED_DEVICES" || die "invalid NPU allowlist"
    IFS=, read -r -a allowed_ids <<<"$NPU_ALLOWED_DEVICES"
    declare -A seen_allowed=()
    for id in "${allowed_ids[@]}"; do
        [ "$id" -lt "${#device_ids[@]}" ] && [ -z "${seen_allowed[$id]-}" ] || die "allowlist contains duplicate/unavailable logical ID"
        seen_allowed[$id]=1
    done
    selection=$NPU_BURN_DEVICE
    if [ "$selection" = all ]; then selection=$logical_device_csv; fi
    IFS=, read -r -a selection_ids <<<"$selection"
    declare -A seen_selection=()
    for id in "${selection_ids[@]}"; do
        [ -n "${seen_allowed[$id]-}" ] && [ -z "${seen_selection[$id]-}" ] || die "default NPU selection is duplicated or unauthorized"
        seen_selection[$id]=1
    done
fi
[ "$CPU_ENABLED" = true ] || [ "$NPU_ENABLED" = true ] || die "configure at least one of --cpu-image or --npu-image"
if [ -n "$NPU_CONFIG_DIR" ]; then
    [ "$NPU_ENABLED" = true ] || die "--npu-config-dir requires --npu-image"
    NPU_CONFIG_DIR=$(absolute --npu-config-dir "$NPU_CONFIG_DIR")
    command -v python3 >/dev/null || die "external NPU JSON validation requires python3"
    NPU_CONFIG_IDENTITY=$(python3 "$(dirname -- "${BASH_SOURCE[0]}")/npu_config_profile.py" --validate-dir "$NPU_CONFIG_DIR") || die "invalid external NPU config directory"
fi

files=(catmonitor-stress.yaml stress-profile.json docker-compose.stress.generated.yml stress-deployment-manifest.json)
if [ "$NPU_ENABLED" = true ]; then files+=(npu-workload.env); fi
if [ "$CPU_ENABLED" = true ]; then files+=(cpu-workload.env); fi
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
NPU_ALLOW_FIELD=
case "$NPU_MODEL_MEMORY_MODE" in standard|checkpointed) ;; *) die "--npu-model-memory-mode must be standard or checkpointed" ;; esac
[[ "$NPU_JOB_TIMEOUT" =~ ^[1-9][0-9]*(s|m|h)$ ]] || die "--npu-job-timeout must be a positive s/m/h duration"
# Resolve capability defaults only for an explicitly selected NPU image. CPU-only
# deployments must not acquire model authorization or require an NPU runtime.
case "$NPU_ALLOWED_MODELS" in
    auto) if [ "$NPU_ENABLED" = true ]; then NPU_ALLOWED_MODELS=gpt3,llama2; else NPU_ALLOWED_MODELS=; fi ;;
    none) NPU_ALLOWED_MODELS= ;;
esac
if [ "$NPU_REFERENCE_GENERATION" = auto ]; then
    if [ -n "$NPU_ALLOWED_MODELS" ]; then NPU_REFERENCE_GENERATION=true; else NPU_REFERENCE_GENERATION=false; fi
fi
if [ -n "$NPU_ALLOWED_MODELS" ] || [ "$NPU_REFERENCE_GENERATION" = true ]; then
    [ "$NPU_ENABLED" = true ] || die "model authorization requires --npu-image"
    case "$NPU_ALLOWED_MODELS" in
        gpt3|llama2|gpt3,llama2|llama2,gpt3) ;;
        *) die "--npu-allowed-models must contain unique gpt3/llama2 names" ;;
    esac
fi
if [ "$NPU_ENABLED" = true ]; then
    NPU_ALLOW_FIELD=", allowed_npu_devices: [$NPU_ALLOWED_DEVICES]"
    if [ -n "$NPU_ALLOWED_MODELS" ]; then NPU_ALLOW_FIELD+=", allowed_npu_models: [$NPU_ALLOWED_MODELS], allow_npu_reference_generation: $NPU_REFERENCE_GENERATION"; fi
fi

# This file is mounted as the daemon's complete /etc/catmonitor/catmonitor.yaml
# by both the generated Compose override and the documented manual deployment
# flow. Keep the canonical monitoring configuration here; emitting only the
# stress section silently disables snapshot production because the safe config
# default for snapshot.enabled is false.
cat >"$CONFIG" <<EOF
collectors:
  chassis: { enabled: true, interval: 3s }
  cpu: { enabled: true, interval: 3s }
  memory: { enabled: true, interval: 3s }
  disk: { enabled: true, interval: 5s }
  gpu: { enabled: true, interval: 3s }
  npu: { enabled: true, interval: 3s }
  network: { enabled: true, interval: 3s }

storage:
  data_dir: /var/lib/catmonitor/data
  max_file_age: 168h
  rotation: daily

collection:
  min_priority: medium

features: [web, dfee, health]

snapshot:
  enabled: true
  dir: /var/lib/catmonitor/snapshot

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
    stream: { enabled: $CPU_ENABLED, plugin: stream, container: $CPU_CONTAINER, user: '65532:65532', timeout: 1m }
    hpl: { enabled: $CPU_ENABLED, plugin: hpl, container: $CPU_CONTAINER, user: '65532:65532', timeout: 2h }
    hpcg: { enabled: $CPU_ENABLED, plugin: hpcg, container: $CPU_CONTAINER, user: '65532:65532', timeout: 3m }
    npu_burn: { enabled: $NPU_ENABLED, plugin: npu_burn, container: $NPU_CONTAINER, timeout: $NPU_JOB_TIMEOUT$NPU_ALLOW_FIELD }
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
    cat >"$OUTPUT_DIR/cpu-workload.env" <<EOF
STREAM_THREADS=$STREAM_THREADS
HPL_MPI_PROCESSES=$HPL_PROCESSES
HPL_THREADS_PER_PROCESS=$HPL_THREADS
HPL_N=$HPL_N
HPL_NB=$HPL_NB
HPL_P=$HPL_P
HPL_Q=$HPL_Q
HPCG_MPI_PROCESSES=$HPCG_PROCESSES
HPCG_THREADS_PER_PROCESS=$HPCG_THREADS
HPCG_NX=$HPCG_NX
HPCG_NY=$HPCG_NY
HPCG_NZ=$HPCG_NZ
HPCG_RUNTIME_SECONDS=$HPCG_RUNTIME
EOF
    chmod 0640 "$OUTPUT_DIR/cpu-workload.env"
    cat >>"$COMPOSE" <<EOF
  catmonitor-stress-cpu:
    image: $(yaml_quote "$CPU_IMAGE")
    container_name: $CPU_CONTAINER
    environment:
      HPL_N: '$HPL_N'
      HPL_NB: '$HPL_NB'
      HPL_P: '$HPL_P'
      HPL_Q: '$HPL_Q'
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
    cat >"$OUTPUT_DIR/npu-workload.env" <<EOF
CATMONITOR_NPU_DEVICE_COUNT=${#device_ids[@]}
CATMONITOR_NPU_DEVICE_MODE=$NPU_DEVICE_MODE
CATMONITOR_NPU_REQUIRE_IDLE=0
CATMONITOR_MODEL_REFERENCE_ROOT=/var/lib/catmonitor/stress/model-references
CATMONITOR_MODEL_MEMORY_MODE=$NPU_MODEL_MEMORY_MODE
ASCEND_RT_VISIBLE_DEVICES=$logical_device_csv
NPU_BURN_DEVICE=$NPU_BURN_DEVICE
NPU_BURN_RUN_CASE=$NPU_RUN_CASE
NPU_BURN_GROUP=$NPU_GROUP
NPU_BURN_CHIP_GENERATION=$NPU_CHIP_GENERATION
NPU_BURN_INTERNAL_TIMEOUT_SECONDS=$NPU_INTERNAL_TIMEOUT
OPENBLAS_NUM_THREADS=$NPU_THREADS
OMP_NUM_THREADS=$NPU_THREADS
MKL_NUM_THREADS=$NPU_THREADS
NUMEXPR_NUM_THREADS=$NPU_THREADS
BLIS_NUM_THREADS=$NPU_THREADS
EOF
    if [ -n "$NPU_ASCEND_ENV_SCRIPT" ]; then
        printf 'ASCEND_ENV_SCRIPT=%s\n' "$NPU_ASCEND_ENV_SCRIPT" >>"$OUTPUT_DIR/npu-workload.env"
    fi
    chmod 0640 "$OUTPUT_DIR/npu-workload.env"
    cat >>"$COMPOSE" <<EOF
  catmonitor-stress-npu:
    image: $(yaml_quote "$NPU_IMAGE")
    container_name: $NPU_CONTAINER
    runtime: $(yaml_quote "$NPU_RUNTIME")
    privileged: $NPU_PRIVILEGED
    init: true

    environment:
      CATMONITOR_NPU_DEVICE_COUNT: '${#device_ids[@]}'
      CATMONITOR_NPU_DEVICE_MODE: '$NPU_DEVICE_MODE'
      CATMONITOR_NPU_REQUIRE_IDLE: '0'
      CATMONITOR_MODEL_REFERENCE_ROOT: /var/lib/catmonitor/stress/model-references
      CATMONITOR_MODEL_MEMORY_MODE: '$NPU_MODEL_MEMORY_MODE'
      OPENBLAS_NUM_THREADS: '$NPU_THREADS'
      OMP_NUM_THREADS: '$NPU_THREADS'
      MKL_NUM_THREADS: '$NPU_THREADS'
      NUMEXPR_NUM_THREADS: '$NPU_THREADS'
      BLIS_NUM_THREADS: '$NPU_THREADS'
      ASCEND_RT_VISIBLE_DEVICES: $(yaml_quote "$logical_device_csv")
      NPU_BURN_DEVICE: $(yaml_quote "$NPU_BURN_DEVICE")
      NPU_BURN_RUN_CASE: $(yaml_quote "$NPU_RUN_CASE")
      NPU_BURN_GROUP: $(yaml_quote "$NPU_GROUP")
      NPU_BURN_CHIP_GENERATION: $(yaml_quote "$NPU_CHIP_GENERATION")
      NPU_BURN_INTERNAL_TIMEOUT_SECONDS: '$NPU_INTERNAL_TIMEOUT'
EOF
    if [ -n "$NPU_ASCEND_ENV_SCRIPT" ]; then
        printf '      ASCEND_ENV_SCRIPT: %s\n' "$(yaml_quote "$NPU_ASCEND_ENV_SCRIPT")" >>"$COMPOSE"
    fi
    if [ "$NPU_PRIVILEGED" = false ]; then
        printf '    cap_drop: [ALL]\n    security_opt: ["no-new-privileges:true"]\n' >>"$COMPOSE"
    fi
    printf '    devices:\n' >>"$COMPOSE"
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
    if [ -n "$NPU_CONFIG_DIR" ]; then
        cat >>"$COMPOSE" <<EOF
      - type: bind
        source: $(yaml_quote "$NPU_CONFIG_DIR")
        target: /opt/catmonitor/npuburn-python/ascend_npu_burn/config
        read_only: true
        bind: { create_host_path: false }
EOF
    fi
fi

generated_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
device_json='[]'
if [ "$NPU_ENABLED" = true ]; then
    device_json=$(printf '%s\n' "${device_ids[@]}" | awk 'BEGIN{printf "["} {if(NR>1)printf ",";printf "%d",$1} END{printf "]"}')
fi
cat >"$PROFILE" <<EOF
{"schema_version":2,"generated_at_utc":$(json_string "$generated_at"),"architecture":"daemon_controller_docker_exec","control_image":$(if [ -n "$CONTROL_IMAGE" ]; then json_string "$CONTROL_IMAGE"; else printf null; fi),"cpu":{"enabled":$CPU_ENABLED,"image":$(if [ -n "$CPU_IMAGE" ]; then json_string "$CPU_IMAGE"; else printf null; fi),"container":"$CPU_CONTAINER","plugins":["stream","hpl","hpcg"],"user":"65532:65532","resources":{"stream_threads":$STREAM_THREADS,"hpl_processes":$HPL_PROCESSES,"hpl_threads":$HPL_THREADS,"hpcg_processes":$HPCG_PROCESSES,"hpcg_threads":$HPCG_THREADS}},"npu":{"enabled":$NPU_ENABLED,"image":$(if [ -n "$NPU_IMAGE" ]; then json_string "$NPU_IMAGE"; else printf null; fi),"container":"$NPU_CONTAINER","host_device_ids":$device_json,"runtime_visible_device_ids":$(if [ "$NPU_ENABLED" = true ]; then json_string "$logical_device_csv"; else printf null; fi),"burn_logical_ids":$(json_string "$NPU_BURN_DEVICE"),"device_mode":$(json_string "$NPU_DEVICE_MODE"),"runtime":$(json_string "$NPU_RUNTIME"),"threads":$NPU_THREADS,"ascend_env_script":$(json_string "$NPU_ASCEND_ENV_SCRIPT"),"privileged":$NPU_PRIVILEGED},"security":{"daemon_docker_socket":true,"web_docker_socket":false,"dfee_docker_socket":false,"npu_workload_privileged":$NPU_PRIVILEGED,"debt":"daemon Docker socket and the enabled NPU workload privilege are temporary V2 boundaries"}}
EOF

config_sha=$(sha256sum "$CONFIG" | awk '{print $1}')
profile_sha=$(sha256sum "$PROFILE" | awk '{print $1}')
compose_sha=$(sha256sum "$COMPOSE" | awk '{print $1}')
npu_env_sha=null
if [ "$NPU_ENABLED" = true ]; then npu_env_sha=$(sha_or_null "$OUTPUT_DIR/npu-workload.env"); fi
cpu_env_sha=null
if [ "$CPU_ENABLED" = true ]; then cpu_env_sha=$(sha_or_null "$OUTPUT_DIR/cpu-workload.env"); fi
cat >"$MANIFEST" <<EOF
{"schema_version":2,"generated_at_utc":$(json_string "$generated_at"),"config_sha256":"$config_sha","profile_sha256":"$profile_sha","compose_sha256":"$compose_sha","cpu_env_sha256":$cpu_env_sha,"npu_env_sha256":$npu_env_sha,"npu_config_dir":$(if [ -n "$NPU_CONFIG_DIR" ]; then json_string "$NPU_CONFIG_DIR"; else printf null; fi),"npu_config_files_sha256":$NPU_CONFIG_IDENTITY,"cpu_manifest_sha256":$(sha_or_null "$CPU_MANIFEST"),"npu_manifest_sha256":$(sha_or_null "$NPU_MANIFEST")}
EOF

chmod 0640 "$CONFIG" "$PROFILE" "$COMPOSE" "$MANIFEST"
printf 'Generated daemon/controller Stress deployment: %s\n' "$OUTPUT_DIR"
printf 'CPU profile: %s' "$CPU_ENABLED"
if [ "$CPU_ENABLED" = true ]; then printf ' (%s)' "$CPU_IMAGE"; fi
printf '\n'
printf 'NPU profile: %s\n' "$NPU_ENABLED"
printf 'Web Run/Cancel: %s (trusted management networks only; no authentication/RBAC)\n' "$WEB_ENABLED"
if [ "$NPU_ENABLED" = true ]; then
    printf 'NPU logical allowlist: %s; default selection: %s\n' "$NPU_ALLOWED_DEVICES" "$NPU_BURN_DEVICE"
    printf 'NPU models: %s; reference creation allowed: %s\n' "${NPU_ALLOWED_MODELS:-none}" "$NPU_REFERENCE_GENERATION"
    printf 'No workload or reference generation was started; idle and reference checks remain required.\n'
fi
