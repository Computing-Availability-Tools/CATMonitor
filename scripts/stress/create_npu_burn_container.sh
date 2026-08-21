#!/usr/bin/env bash
set -euo pipefail

IMAGE=
CONTAINER_NAME=catmonitor-npuburn
OUTPUT_DIR=/var/lib/catmonitor/stress/npu-burn-output
DOCKER_BIN=
RUNTIME=ascend
RESTART_POLICY=unless-stopped

usage() {
    cat <<'EOF'
Usage: create_npu_burn_container.sh --image IMAGE [OPTIONS]

Create or safely start the administrator-maintained CATMonitor NPU Burn
container. Stress jobs only use docker exec and never manage its lifecycle.

Required:
  --image IMAGE          Locally available CATMonitor NPU Burn image

Options:
  --name NAME            Container name (default: catmonitor-npuburn)
  --output-dir PATH      Host result directory
                         (default: /var/lib/catmonitor/stress/npu-burn-output)
  --docker-bin PATH      Docker-compatible CLI (default: docker from PATH)
  --runtime NAME         OCI runtime (default: ascend)
  --restart-policy NAME  Container restart policy: no, on-failure, always, or
                         unless-stopped (default: unless-stopped)
  -h, --help             Show this help

The script maps every host /dev/davinciN node into the container with the same
host device-node ID, plus the required manager/devmm/HDC devices and validated
Ascend driver/tool paths. NPU Burn logical IDs are derived separately from PCI
topology. The container inherits environment variables from the image.

An existing matching stopped container is started. An existing mismatched
container is never removed or replaced automatically.
EOF
}

die() {
    printf 'ERROR: %s\n' "$*" >&2
    exit 1
}

require_value() {
    [ "$#" -ge 2 ] && [ -n "$2" ] || die "$1 requires a value"
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --image) require_value "$@"; IMAGE=$2; shift 2 ;;
        --name) require_value "$@"; CONTAINER_NAME=$2; shift 2 ;;
        --output-dir) require_value "$@"; OUTPUT_DIR=$2; shift 2 ;;
        --docker-bin) require_value "$@"; DOCKER_BIN=$2; shift 2 ;;
        --runtime) require_value "$@"; RUNTIME=$2; shift 2 ;;
        --restart-policy) require_value "$@"; RESTART_POLICY=$2; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown argument: $1" ;;
    esac
done

[ -n "$IMAGE" ] || die "--image is required"
case "$IMAGE" in -*|*$'\n'*|*$'\r'*) die "--image is invalid" ;; esac
case "$CONTAINER_NAME" in ''|-*|*[!A-Za-z0-9_.-]*) die "--name is invalid" ;; esac
case "$RUNTIME" in ''|-*|*[!A-Za-z0-9_.-]*) die "--runtime is invalid" ;; esac
case "$RESTART_POLICY" in
    no|on-failure|always|unless-stopped) ;;
    *) die "--restart-policy must be no, on-failure, always, or unless-stopped" ;;
esac
case "$OUTPUT_DIR" in /*) ;; *) die "--output-dir must be absolute" ;; esac
case "$OUTPUT_DIR" in *:*|*$'\n'*|*$'\r'*) die "--output-dir is invalid for a bind mount" ;; esac
OUTPUT_DIR=$(readlink -m -- "$OUTPUT_DIR")
[ "$OUTPUT_DIR" != / ] || die "--output-dir must not be the filesystem root"
[ ! -L "$OUTPUT_DIR" ] || die "--output-dir must not be a symbolic link"

if [ -z "$DOCKER_BIN" ]; then
    DOCKER_BIN=$(command -v docker 2>/dev/null) || die "docker is unavailable; use --docker-bin"
else
    DOCKER_BIN=$(command -v -- "$DOCKER_BIN" 2>/dev/null) || die "container runtime is unavailable: $DOCKER_BIN"
fi

# Test fixtures can model the host filesystem without writing under /dev or
# /usr. Production callers must not set these internal variables.
HOST_ROOT=${CATMONITOR_NPU_BOOTSTRAP_HOST_ROOT-}
TESTING=false
if [ -n "$HOST_ROOT" ]; then
    [ "${CATMONITOR_NPU_BOOTSTRAP_TESTING-}" = true ] || \
        die "CATMONITOR_NPU_BOOTSTRAP_HOST_ROOT is reserved for repository tests"
    case "$HOST_ROOT" in /*) ;; *) die "test host root must be absolute" ;; esac
    HOST_ROOT=${HOST_ROOT%/}
    TESTING=true
fi

host_path() {
    printf '%s%s' "$HOST_ROOT" "$1"
}

"$DOCKER_BIN" version >/dev/null 2>&1 || die "docker daemon is unavailable"
IMAGE_ID=$("$DOCKER_BIN" image inspect --format '{{.Id}}' "$IMAGE" 2>/dev/null) || \
    die "image is unavailable locally: $IMAGE"
case "$IMAGE_ID" in sha256:*) ;; *) die "docker reported an invalid image ID for $IMAGE" ;; esac

if [ ! -e "$OUTPUT_DIR" ]; then
    install -d -m 0750 -- "$OUTPUT_DIR" || die "cannot create output directory: $OUTPUT_DIR"
fi
[ -d "$OUTPUT_DIR" ] || die "output path is not a directory: $OUTPUT_DIR"

DEVICE_DIR=$(host_path /dev)
device_candidates=("$DEVICE_DIR"/davinci[0-9]*)
device_records=()
for device_path in "${device_candidates[@]}"; do
    device_name=${device_path##*/}
    device_id=${device_name#davinci}
    case "$device_id" in ''|*[!0-9]*) continue ;; esac
    stat -- "$device_path" >/dev/null 2>&1 || continue
    if [ "$TESTING" != true ] && [ ! -c "$device_path" ]; then continue; fi
    device_records+=("$device_id"$'\t'"$device_path")
done
[ "${#device_records[@]}" -gt 0 ] || die "no host /dev/davinciN device nodes were found"
mapfile -t device_records < <(printf '%s\n' "${device_records[@]}" | sort -n -t $'\t' -k1,1)

control_devices=(
    /dev/davinci_manager
    /dev/devmm_svm
    /dev/hisi_hdc
)
for container_path in "${control_devices[@]}"; do
    source_path=$(host_path "$container_path")
    stat -- "$source_path" >/dev/null 2>&1 || \
        die "required Ascend control device is unavailable: $container_path"
    if [ "$TESTING" != true ] && [ ! -c "$source_path" ]; then
        die "required Ascend control path is not a character device: $container_path"
    fi
done

readonly_mounts=(
    /usr/local/Ascend/driver/lib64
    /usr/local/Ascend/driver/version.info
    /etc/ascend_install.info
    /usr/local/dcmi
    /usr/local/bin/npu-smi
)
for container_path in "${readonly_mounts[@]}"; do
    source_path=$(host_path "$container_path")
    stat -- "$source_path" >/dev/null 2>&1 || \
        die "required Ascend host path is unavailable: $container_path"
    case "$container_path" in
        /usr/local/Ascend/driver/lib64|/usr/local/dcmi)
            [ -d "$source_path" ] || die "required Ascend host path is not a directory: $container_path"
            ;;
        *)
            [ -f "$source_path" ] || die "required Ascend host path is not a regular file: $container_path"
            ;;
    esac
done

profile_material=$(
    printf 'image_id=%s\nruntime=%s\nrestart=%s\noutput=%s\n' \
        "$IMAGE_ID" "$RUNTIME" "$RESTART_POLICY" "$OUTPUT_DIR"
    printf 'privileged=true\nnetwork=host\nshm=64m\nworkdir=/workspace\n'
    printf 'security_opt=label=disable\nentrypoint=/bin/bash\n'
    for record in "${device_records[@]}"; do
        device_id=${record%%$'\t'*}
        printf 'device=/dev/davinci%s:/dev/davinci%s\n' "$device_id" "$device_id"
    done
    for container_path in "${control_devices[@]}"; do
        printf 'device=%s:%s\n' "$container_path" "$container_path"
    done
    for container_path in "${readonly_mounts[@]}"; do
        printf 'mount=%s:%s:ro\n' "$container_path" "$container_path"
    done
    printf 'mount=%s:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output:rw\n' "$OUTPUT_DIR"
)
PROFILE_SHA256=$(printf '%s' "$profile_material" | sha256sum | awk '{print $1}')

inspect_format='{{.State.Running}}|{{.Image}}|{{index .Config.Labels "io.catmonitor.npu-burn.profile-sha256"}}|{{.HostConfig.Runtime}}|{{.HostConfig.RestartPolicy.Name}}|{{.HostConfig.Privileged}}|{{.HostConfig.NetworkMode}}|{{.HostConfig.ShmSize}}|{{.Config.WorkingDir}}|{{.Path}}'
if "$DOCKER_BIN" container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
    container_probe=$("$DOCKER_BIN" container inspect --format "$inspect_format" "$CONTAINER_NAME" 2>/dev/null) || \
        die "cannot inspect existing container profile: $CONTAINER_NAME"
    IFS='|' read -r running existing_image_id existing_profile existing_runtime existing_restart \
        existing_privileged existing_network existing_shm existing_workdir existing_path <<<"$container_probe"
    if [ "$existing_image_id" != "$IMAGE_ID" ] || \
       [ "$existing_profile" != "$PROFILE_SHA256" ] || \
       [ "$existing_runtime" != "$RUNTIME" ] || \
       [ "$existing_restart" != "$RESTART_POLICY" ] || \
       [ "$existing_privileged" != true ] || \
       [ "$existing_network" != host ] || \
       [ "$existing_shm" != 67108864 ] || \
       [ "$existing_workdir" != /workspace ] || \
       [ "$existing_path" != /bin/bash ]; then
        die "container $CONTAINER_NAME exists with an unexpected image or runtime profile; inspect it and remove/recreate it explicitly"
    fi
    case "$running" in
        true)
            printf 'NPU Burn container is already running with the expected profile: %s\n' "$CONTAINER_NAME"
            exit 0
            ;;
        false)
            "$DOCKER_BIN" start "$CONTAINER_NAME" >/dev/null || \
                die "failed to start matching container: $CONTAINER_NAME"
            printf 'Started matching NPU Burn container: %s\n' "$CONTAINER_NAME"
            exit 0
            ;;
        *) die "container $CONTAINER_NAME reported an invalid running state" ;;
    esac
fi

run_args=(
    run --detach
    --name "$CONTAINER_NAME"
    --label io.catmonitor.npu-burn.fixed-container=true
    --label "io.catmonitor.npu-burn.profile-sha256=$PROFILE_SHA256"
    --runtime "$RUNTIME"
    --restart "$RESTART_POLICY"
    --privileged
    --network host
    --shm-size 64m
    --workdir /workspace
    --security-opt label=disable
)
for record in "${device_records[@]}"; do
    device_id=${record%%$'\t'*}
    source_path=${record#*$'\t'}
    run_args+=(--device "$source_path:/dev/davinci$device_id")
done
for container_path in "${control_devices[@]}"; do
    source_path=$(host_path "$container_path")
    run_args+=(--device "$source_path:$container_path")
done
for container_path in "${readonly_mounts[@]}"; do
    source_path=$(host_path "$container_path")
    run_args+=(--volume "$source_path:$container_path:ro")
done
run_args+=(
    --volume "$OUTPUT_DIR:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output:rw"
    --entrypoint /bin/bash
    "$IMAGE"
    -lc 'trap : TERM INT; sleep infinity & wait'
)

CONTAINER_ID=$("$DOCKER_BIN" "${run_args[@]}") || \
    die "failed to create NPU Burn container: $CONTAINER_NAME"
printf 'Created NPU Burn container: %s\n' "$CONTAINER_NAME"
printf 'Container ID: %s\n' "$CONTAINER_ID"
printf 'Image: %s (%s)\n' "$IMAGE" "$IMAGE_ID"
printf 'Restart policy: %s\n' "$RESTART_POLICY"
printf 'Logical devices: '
separator=
for record in "${device_records[@]}"; do
    device_id=${record%%$'\t'*}
    printf '%s%s' "$separator" "$device_id"
    separator=,
done
printf '\nOutput: %s\n' "$OUTPUT_DIR"
