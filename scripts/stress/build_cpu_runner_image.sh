#!/usr/bin/env bash
# Build the optional self-contained CPU stress runner image. The image embeds
# benchmark binaries and matching runtime libraries, but receives its fixed
# node execution profile as a read-only adapter at deployment time.

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd -P)
DOCKERFILE="$REPO_ROOT/docker/stress/cpu/Dockerfile"
ENTRYPOINT="$REPO_ROOT/docker/stress/cpu/entrypoint.sh"

TARGET_IMAGE=
DOCKER_BIN=
STREAM_SRC=
HPL_SRC=
HPL_DAT=
HPCG_SRC=
HPCG_DAT=
BUILD_ROOT=/var/tmp/catmonitor-cpu-runner-build
BUILD_NETWORK=default
DEBIAN_MIRROR=
JOBS=16
STREAM_ARRAY_SIZE=80000000
STREAM_NTIMES=10
MANIFEST_PATH=
FORCE=false

usage() {
    cat <<'EOF'
Usage: build_cpu_runner_image.sh [OPTIONS]

Required:
  --image IMAGE             Output CPU runner image name and tag
  --stream-src PATH         STREAM stream.c
  --hpl-src PATH            Stock HPL source tar archive
  --hpl-dat PATH            Administrator-approved HPL.dat
  --hpcg-src PATH           Stock HPCG source tar archive
  --hpcg-dat PATH           Administrator-approved hpcg.dat

Build controls:
  --docker-bin PATH         Docker-compatible CLI (default: docker from PATH)
  --build-root PATH         Isolated context and manifest parent
  --build-network NAME      Docker build network: default, host, or none
  --debian-mirror URL       Optional Debian mirror root without credentials
  --jobs N                 Parallel jobs inside safe benchmark build phases
  --stream-array-size N     STREAM compile-time array size
  --stream-ntimes N         STREAM compile-time iteration count
  --manifest PATH           Image manifest output path
  --force                   Replace an existing image tag/manifest
  -h, --help                Show this help

The build uses Debian bookworm build/runtime stages so MPI, OpenBLAS and the
benchmark binaries share one ABI. It does not create a runner container or run
HPL/HPCG/NPU workloads.
EOF
}

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
require_value() { [ "$#" -ge 2 ] && [ -n "$2" ] || die "$1 requires a value"; }

while [ "$#" -gt 0 ]; do
    case "$1" in
        --image) require_value "$@"; TARGET_IMAGE=$2; shift 2 ;;
        --docker-bin) require_value "$@"; DOCKER_BIN=$2; shift 2 ;;
        --stream-src) require_value "$@"; STREAM_SRC=$2; shift 2 ;;
        --hpl-src) require_value "$@"; HPL_SRC=$2; shift 2 ;;
        --hpl-dat) require_value "$@"; HPL_DAT=$2; shift 2 ;;
        --hpcg-src) require_value "$@"; HPCG_SRC=$2; shift 2 ;;
        --hpcg-dat) require_value "$@"; HPCG_DAT=$2; shift 2 ;;
        --build-root) require_value "$@"; BUILD_ROOT=$2; shift 2 ;;
        --build-network) require_value "$@"; BUILD_NETWORK=$2; shift 2 ;;
        --debian-mirror) require_value "$@"; DEBIAN_MIRROR=$2; shift 2 ;;
        --jobs) require_value "$@"; JOBS=$2; shift 2 ;;
        --stream-array-size) require_value "$@"; STREAM_ARRAY_SIZE=$2; shift 2 ;;
        --stream-ntimes) require_value "$@"; STREAM_NTIMES=$2; shift 2 ;;
        --manifest) require_value "$@"; MANIFEST_PATH=$2; shift 2 ;;
        --force) FORCE=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown argument: $1" ;;
    esac
done

[ -n "$TARGET_IMAGE" ] || die "--image is required"
case "$TARGET_IMAGE" in -*|*@*|*[!A-Za-z0-9._/:-]*) die "--image has an invalid value" ;; esac
case "$BUILD_NETWORK" in default|host|none) ;; *) die "--build-network must be default, host, or none" ;; esac
case "$JOBS:$STREAM_ARRAY_SIZE:$STREAM_NTIMES" in *[!0-9:]*|0:*|*:0:*|*:0) die "numeric build values must be positive integers" ;; esac
if [ -n "$DEBIAN_MIRROR" ]; then
    case "$DEBIAN_MIRROR" in
        http://?*|https://?*) ;;
        *) die "--debian-mirror must use http:// or https://" ;;
    esac
    mirror_authority=${DEBIAN_MIRROR#*://}
    mirror_authority=${mirror_authority%/}
    case "$mirror_authority" in
        ''|*/*|*@*|*\?*|*\#*|*[[:space:]]*)
            die "--debian-mirror must be a mirror root without credentials, path, query, or fragment"
            ;;
    esac
    DEBIAN_MIRROR=${DEBIAN_MIRROR%/}
fi

if [ -z "$DOCKER_BIN" ]; then DOCKER_BIN=$(command -v docker 2>/dev/null || true); fi
[ -n "$DOCKER_BIN" ] && [ -x "$DOCKER_BIN" ] || die "Docker CLI is unavailable"
for tool in date install mktemp readlink sha256sum; do
    command -v "$tool" >/dev/null 2>&1 || die "required tool is unavailable: $tool"
done
[ -f "$DOCKERFILE" ] && [ -f "$ENTRYPOINT" ] || die "CPU runner image templates are unavailable"

canonical_file() {
    local option=$1 value=$2
    [ -n "$value" ] && [ -f "$value" ] && [ -r "$value" ] || die "$option is not a readable file: $value"
    readlink -f -- "$value"
}
STREAM_SRC=$(canonical_file --stream-src "$STREAM_SRC")
HPL_SRC=$(canonical_file --hpl-src "$HPL_SRC")
HPL_DAT=$(canonical_file --hpl-dat "$HPL_DAT")
HPCG_SRC=$(canonical_file --hpcg-src "$HPCG_SRC")
HPCG_DAT=$(canonical_file --hpcg-dat "$HPCG_DAT")

case "$BUILD_ROOT" in /*) ;; *) die "--build-root must be absolute" ;; esac
BUILD_ROOT=$(readlink -m -- "$BUILD_ROOT")
case "$BUILD_ROOT" in /|/var|/var/tmp|/tmp|/opt|/opt/catmonitor) die "--build-root must be a dedicated child directory" ;; esac
if [ -z "$MANIFEST_PATH" ]; then
    MANIFEST_PATH="$BUILD_ROOT/manifests/cpu-runner-image-manifest.json"
fi
case "$MANIFEST_PATH" in /*) ;; *) die "--manifest must be absolute" ;; esac
MANIFEST_PATH=$(readlink -m -- "$MANIFEST_PATH")

if "$DOCKER_BIN" image inspect "$TARGET_IMAGE" >/dev/null 2>&1 && [ "$FORCE" != true ]; then
    die "image already exists; use --force to replace it: $TARGET_IMAGE"
fi
if [ -e "$MANIFEST_PATH" ] && [ "$FORCE" != true ]; then
    die "manifest already exists; use --force to replace it: $MANIFEST_PATH"
fi
[ ! -L "$MANIFEST_PATH" ] || die "manifest cannot be a symbolic link"

install -d -m 0750 "$BUILD_ROOT" "$(dirname -- "$MANIFEST_PATH")"
CONTEXT=$(mktemp -d "$BUILD_ROOT/context.XXXXXXXX")
cleanup() {
    case "$CONTEXT" in "$BUILD_ROOT"/context.*) rm -rf -- "$CONTEXT" ;; esac
}
trap cleanup EXIT HUP INT TERM

install -d -m 0755 \
    "$CONTEXT/features/stress/runnerapi" \
    "$CONTEXT/features/stress/cmd/cpu-runner" \
    "$CONTEXT/scripts/stress/templates" \
    "$CONTEXT/docker/stress/cpu" \
    "$CONTEXT/inputs"
install -m 0644 "$REPO_ROOT/go.mod" "$REPO_ROOT/go.sum" "$CONTEXT/"
install -m 0644 "$REPO_ROOT/features/stress/runnerapi/"*.go "$CONTEXT/features/stress/runnerapi/"
install -m 0644 "$REPO_ROOT/features/stress/cmd/cpu-runner/"*.go "$CONTEXT/features/stress/cmd/cpu-runner/"
install -m 0755 "$REPO_ROOT/scripts/stress/build_cpu_benchmarks.sh" "$CONTEXT/scripts/stress/"
install -m 0644 "$REPO_ROOT/scripts/stress/templates/Make.HPL.CATMonitor" "$CONTEXT/scripts/stress/templates/"
install -m 0644 "$DOCKERFILE" "$CONTEXT/docker/stress/cpu/Dockerfile"
install -m 0755 "$ENTRYPOINT" "$CONTEXT/docker/stress/cpu/entrypoint.sh"
install -m 0644 "$STREAM_SRC" "$CONTEXT/inputs/stream.c"
install -m 0644 "$HPL_SRC" "$CONTEXT/inputs/hpl.tar.gz"
install -m 0644 "$HPL_DAT" "$CONTEXT/inputs/HPL.dat"
install -m 0644 "$HPCG_SRC" "$CONTEXT/inputs/hpcg.tar.gz"
install -m 0644 "$HPCG_DAT" "$CONTEXT/inputs/hpcg.dat"

declare -a BUILD_ARGS=()
for name in HTTP_PROXY HTTPS_PROXY NO_PROXY http_proxy https_proxy no_proxy GOPROXY GOSUMDB GOPRIVATE GONOSUMDB; do
    if [ -n "${!name-}" ]; then BUILD_ARGS+=(--build-arg "$name"); fi
done
if [ -n "$DEBIAN_MIRROR" ]; then
    BUILD_ARGS+=(--build-arg "DEBIAN_MIRROR=$DEBIAN_MIRROR")
fi
if [ "${#BUILD_ARGS[@]}" -gt 0 ]; then
    printf 'Docker build environment: administrator settings configured (values hidden)\n'
fi

"$DOCKER_BIN" build \
    --network "$BUILD_NETWORK" \
    "${BUILD_ARGS[@]}" \
    --build-arg "BUILD_JOBS=$JOBS" \
    --build-arg "STREAM_ARRAY_SIZE=$STREAM_ARRAY_SIZE" \
    --build-arg "STREAM_NTIMES=$STREAM_NTIMES" \
    -f "$CONTEXT/docker/stress/cpu/Dockerfile" \
    -t "$TARGET_IMAGE" \
    "$CONTEXT"

IMAGE_ID=$("$DOCKER_BIN" image inspect --format '{{.Id}}' "$TARGET_IMAGE")
case "$IMAGE_ID" in sha256:*) ;; *) die "built image has an invalid image ID" ;; esac
TEMP=$(mktemp "$(dirname -- "$MANIFEST_PATH")/.cpu-runner-image.XXXXXXXX")
json_escape() { local value=${1-}; value=${value//\\/\\\\}; value=${value//\"/\\\"}; printf '%s' "$value"; }
{
    printf '{"schema_version":1,"feature":"stress_cpu_runner","generated_at_utc":"%s"' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf ',"image":"%s","image_id":"%s"' "$(json_escape "$TARGET_IMAGE")" "$(json_escape "$IMAGE_ID")"
    if [ -n "$DEBIAN_MIRROR" ]; then
        printf ',"debian_mirror":"%s"' "$(json_escape "$DEBIAN_MIRROR")"
    else
        printf ',"debian_mirror":null'
    fi
    printf ',"inputs":{"stream_sha256":"%s","hpl_sha256":"%s","hpl_dat_sha256":"%s"' \
        "$(sha256sum "$STREAM_SRC" | awk '{print $1}')" \
        "$(sha256sum "$HPL_SRC" | awk '{print $1}')" \
        "$(sha256sum "$HPL_DAT" | awk '{print $1}')"
    printf ',"hpcg_sha256":"%s","hpcg_dat_sha256":"%s"}' \
        "$(sha256sum "$HPCG_SRC" | awk '{print $1}')" \
        "$(sha256sum "$HPCG_DAT" | awk '{print $1}')"
    printf ',"build":{"jobs":%s,"stream_array_size":%s,"stream_ntimes":%s}}\n' \
        "$JOBS" "$STREAM_ARRAY_SIZE" "$STREAM_NTIMES"
} >"$TEMP"
chmod 0640 "$TEMP"
mv -f -- "$TEMP" "$MANIFEST_PATH"
trap - EXIT HUP INT TERM
cleanup

printf 'CPU runner image: %s\n' "$TARGET_IMAGE"
printf 'Image ID: %s\n' "$IMAGE_ID"
printf 'Manifest: %s\n' "$MANIFEST_PATH"
printf 'No runner container or stress workload was started.\n'
