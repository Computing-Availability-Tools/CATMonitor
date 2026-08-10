#!/usr/bin/env bash
# Build a traceable Ascend NPU Burn image without creating containers or
# executing an NPU workload. Runtime devices, mounts and lifecycle remain an
# administrator-owned deployment concern.

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd -P)
DOCKERFILE_TEMPLATE="$REPO_ROOT/docker/stress/npu/Dockerfile"
ENTRYPOINT_TEMPLATE="$REPO_ROOT/docker/stress/npu/entrypoint.sh"

SOURCE_ROOT=
BASE_IMAGE=
TARGET_IMAGE=
DOCKER_BIN=
COMPAT_PROFILE=none
BUILD_ROOT=/var/tmp/catmonitor-npu-burn-build
MANIFEST_PATH=
FORCE=false
declare -a PATCH_FILES=()

usage() {
    cat <<'EOF'
Usage: build_npu_burn_image.sh [OPTIONS]

Required:
  --source PATH             MindCluster-AscendNPUBurn source directory
  --base-image IMAGE        Administrator-approved CANN/torch_npu base image
  --image IMAGE             Output image name and tag

Build controls:
  --docker-bin PATH         Docker-compatible CLI (default: docker from PATH)
  --compat-profile NAME     Compatibility identity (default: none)
  --patch PATH              Apply an audited -p1 patch; repeatable
  --build-root PATH         Isolated build parent
                            (default: /var/tmp/catmonitor-npu-burn-build)
  --manifest PATH           Output manifest (default: BUILD_ROOT/manifests/
                            npu-burn-image-manifest.json)
  --force                   Replace an existing image tag or manifest
  -h, --help                Show this help

Profile rules:
  * "none" accepts no patches and is the initial A3 profile.
  * Any other safe profile name requires at least one explicit --patch.
  * Patches are applied only to the isolated source snapshot, never in place.

The Docker build verifies imports and `npu-burn --version`. It does not run an
NPU workload and never calls docker run/create/start/stop/rm.
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
        --source) require_value "$@"; SOURCE_ROOT=$2; shift 2 ;;
        --base-image) require_value "$@"; BASE_IMAGE=$2; shift 2 ;;
        --image) require_value "$@"; TARGET_IMAGE=$2; shift 2 ;;
        --docker-bin) require_value "$@"; DOCKER_BIN=$2; shift 2 ;;
        --compat-profile) require_value "$@"; COMPAT_PROFILE=$2; shift 2 ;;
        --patch) require_value "$@"; PATCH_FILES+=("$2"); shift 2 ;;
        --build-root) require_value "$@"; BUILD_ROOT=$2; shift 2 ;;
        --manifest) require_value "$@"; MANIFEST_PATH=$2; shift 2 ;;
        --force) FORCE=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown argument: $1" ;;
    esac
done

[ -n "$SOURCE_ROOT" ] || die "--source is required"
[ -n "$BASE_IMAGE" ] || die "--base-image is required"
[ -n "$TARGET_IMAGE" ] || die "--image is required"
case "$BASE_IMAGE" in
    -*|*[!A-Za-z0-9._/@:-]*) die "--base-image contains unsupported characters" ;;
esac
case "$TARGET_IMAGE" in
    -*|*@*|*[!A-Za-z0-9._/:-]*) die "--image must be a name/tag, not a digest or option" ;;
esac
case "$COMPAT_PROFILE" in
    none)
        [ "${#PATCH_FILES[@]}" -eq 0 ] || die "compat profile none does not accept --patch"
        ;;
    ''|-*|*[!a-z0-9._-]*)
        die "--compat-profile must be none or a lowercase safe name"
        ;;
    *)
        [ "${#PATCH_FILES[@]}" -gt 0 ] || \
            die "compat profile $COMPAT_PROFILE requires at least one --patch"
        ;;
esac

for tool in readlink install mktemp tar sha256sum find grep date; do
    command -v "$tool" >/dev/null 2>&1 || die "required tool is unavailable: $tool"
done
[ "${#PATCH_FILES[@]}" -eq 0 ] || command -v patch >/dev/null 2>&1 || \
    die "required tool is unavailable: patch"

SOURCE_ROOT=$(readlink -f -- "$SOURCE_ROOT")
[ -d "$SOURCE_ROOT" ] || die "source directory is unavailable"
for required in build/build.sh build/setup.py ascend_npu_burn/npu_burn.py LICENSE.md; do
    [ -f "$SOURCE_ROOT/$required" ] || die "source is missing required file: $required"
done
if find "$SOURCE_ROOT" -type l -print -quit | grep -q .; then
    die "source directory must not contain symbolic links"
fi
if grep -q $'\r' "$SOURCE_ROOT/build/build.sh"; then
    die "source build/build.sh must use LF line endings"
fi

for index in "${!PATCH_FILES[@]}"; do
    PATCH_FILES[$index]=$(readlink -f -- "${PATCH_FILES[$index]}")
    [ -f "${PATCH_FILES[$index]}" ] || die "patch file is unavailable: ${PATCH_FILES[$index]}"
done

if [ -z "$DOCKER_BIN" ]; then
    DOCKER_BIN=$(command -v docker 2>/dev/null || true)
fi
[ -n "$DOCKER_BIN" ] || die "docker is unavailable; use --docker-bin"
DOCKER_BIN=$(readlink -f -- "$DOCKER_BIN")
[ -x "$DOCKER_BIN" ] || die "docker CLI is not executable: $DOCKER_BIN"

case "$BUILD_ROOT" in /*) ;; *) die "--build-root must be absolute" ;; esac
case "$BUILD_ROOT" in /|/var|/var/tmp|/tmp) die "--build-root must be a dedicated child directory" ;; esac
case "$BUILD_ROOT" in *$'\n'*|*$'\r'*|*$'\t'*|*' '*) die "--build-root cannot contain whitespace" ;; esac
BUILD_ROOT=$(readlink -m -- "$BUILD_ROOT")
case "$BUILD_ROOT/" in "$SOURCE_ROOT"/*) die "--build-root cannot be inside the source directory" ;; esac
[ ! -L "$BUILD_ROOT" ] || die "--build-root cannot be a symbolic link"
install -d -m 0755 "$BUILD_ROOT"

if [ -z "$MANIFEST_PATH" ]; then
    MANIFEST_PATH="$BUILD_ROOT/manifests/npu-burn-image-manifest.json"
fi
case "$MANIFEST_PATH" in /*) ;; *) die "--manifest must be absolute" ;; esac
MANIFEST_PATH=$(readlink -m -- "$MANIFEST_PATH")
case "$MANIFEST_PATH" in "$SOURCE_ROOT"|"$SOURCE_ROOT"/*) die "--manifest cannot modify the source directory" ;; esac
[ ! -L "$MANIFEST_PATH" ] || die "manifest path cannot be a symbolic link"

[ -f "$DOCKERFILE_TEMPLATE" ] || die "Dockerfile template is unavailable"
[ -f "$ENTRYPOINT_TEMPLATE" ] || die "entrypoint template is unavailable"
if grep -q $'\r' "$ENTRYPOINT_TEMPLATE"; then
    die "entrypoint template must use LF line endings"
fi

DOCKER_VERSION=$("$DOCKER_BIN" version --format '{{.Server.Version}}' 2>&1) || \
    die "docker daemon is unavailable: $DOCKER_VERSION"
if "$DOCKER_BIN" image inspect "$TARGET_IMAGE" >/dev/null 2>&1; then
    [ "$FORCE" = true ] || die "image already exists; use --force to replace its tag: $TARGET_IMAGE"
fi
if [ -e "$MANIFEST_PATH" ] && [ "$FORCE" != true ]; then
    die "manifest already exists; use --force to replace it: $MANIFEST_PATH"
fi

RUN_ROOT=$(mktemp -d "$BUILD_ROOT/catmonitor-npu-burn-build.XXXXXXXX")
MANIFEST_TEMP=
cleanup() {
    if [ -n "$MANIFEST_TEMP" ]; then rm -f -- "$MANIFEST_TEMP"; fi
    case "$RUN_ROOT" in "$BUILD_ROOT"/catmonitor-npu-burn-build.*) rm -rf -- "$RUN_ROOT" ;; esac
}
trap cleanup EXIT HUP INT TERM
CONTEXT="$RUN_ROOT/context"
STAGED_SOURCE="$CONTEXT/source"
install -d -m 0755 "$STAGED_SOURCE"

# Copy a clean source snapshot. Generated wheels, VCS metadata and Python/C++
# cache products are not accepted as source inputs.
(
    cd "$SOURCE_ROOT"
    tar \
        --exclude='./.git' \
        --exclude='./build/dist' \
        --exclude='./.pytest_cache' \
        --exclude='__pycache__' \
        --exclude='*.pyc' \
        --exclude='*.so' \
        -cf - .
) | tar --no-same-owner --no-same-permissions -xf - -C "$STAGED_SOURCE"

hash_tree() {
    local root=$1
    (
        cd "$root"
        tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 \
            --numeric-owner -cf - .
    ) | sha256sum | awk '{print $1}'
}

sha256_file() {
    sha256sum -- "$1" | awk '{print $1}'
}

SOURCE_SHA256=$(hash_tree "$STAGED_SOURCE")
for patch_file in "${PATCH_FILES[@]}"; do
    patch --dry-run --batch --forward -p1 -d "$STAGED_SOURCE" <"$patch_file" >/dev/null || \
        die "patch does not apply cleanly: $patch_file"
    patch --batch --forward -p1 -d "$STAGED_SOURCE" <"$patch_file"
done
if grep -q $'\r' "$STAGED_SOURCE/build/build.sh"; then
    die "patched source build/build.sh must use LF line endings"
fi
PATCHED_SOURCE_SHA256=$(hash_tree "$STAGED_SOURCE")

install -m 0644 "$DOCKERFILE_TEMPLATE" "$CONTEXT/Dockerfile"
install -m 0755 "$ENTRYPOINT_TEMPLATE" "$CONTEXT/entrypoint.sh"
DOCKERFILE_SHA256=$(sha256_file "$DOCKERFILE_TEMPLATE")
ENTRYPOINT_SHA256=$(sha256_file "$ENTRYPOINT_TEMPLATE")

printf '==> building Ascend NPU Burn image %s\n' "$TARGET_IMAGE"
printf '    source: %s\n' "$SOURCE_ROOT"
printf '    base image: %s\n' "$BASE_IMAGE"
printf '    compatibility profile: %s\n' "$COMPAT_PROFILE"
"$DOCKER_BIN" build \
    --file "$CONTEXT/Dockerfile" \
    --tag "$TARGET_IMAGE" \
    --build-arg "BASE_IMAGE=$BASE_IMAGE" \
    --build-arg "SOURCE_SHA256=$SOURCE_SHA256" \
    --build-arg "PATCHED_SOURCE_SHA256=$PATCHED_SOURCE_SHA256" \
    --build-arg "COMPAT_PROFILE=$COMPAT_PROFILE" \
    "$CONTEXT"

IMAGE_ID=$("$DOCKER_BIN" image inspect --format '{{.Id}}' "$TARGET_IMAGE")
IMAGE_OS=$("$DOCKER_BIN" image inspect --format '{{.Os}}' "$TARGET_IMAGE")
IMAGE_ARCH=$("$DOCKER_BIN" image inspect --format '{{.Architecture}}' "$TARGET_IMAGE")
IMAGE_CREATED=$("$DOCKER_BIN" image inspect --format '{{.Created}}' "$TARGET_IMAGE")
IMAGE_DIGESTS=$("$DOCKER_BIN" image inspect --format '{{join .RepoDigests ","}}' "$TARGET_IMAGE")
LABEL_SOURCE=$("$DOCKER_BIN" image inspect --format '{{index .Config.Labels "io.catmonitor.npu-burn.source-sha256"}}' "$TARGET_IMAGE")
LABEL_PATCHED=$("$DOCKER_BIN" image inspect --format '{{index .Config.Labels "io.catmonitor.npu-burn.patched-source-sha256"}}' "$TARGET_IMAGE")
LABEL_PROFILE=$("$DOCKER_BIN" image inspect --format '{{index .Config.Labels "io.catmonitor.npu-burn.compat-profile"}}' "$TARGET_IMAGE")
[ "$LABEL_SOURCE" = "$SOURCE_SHA256" ] || die "built image source label does not match the staged source"
[ "$LABEL_PATCHED" = "$PATCHED_SOURCE_SHA256" ] || die "built image patched-source label does not match"
[ "$LABEL_PROFILE" = "$COMPAT_PROFILE" ] || die "built image compatibility label does not match"

json_escape() {
    local value=${1-}
    value=${value//\\/\\\\}
    value=${value//\"/\\\"}
    value=${value//$'\n'/\\n}
    value=${value//$'\r'/\\r}
    value=${value//$'\t'/\\t}
    printf '%s' "$value"
}

json_string() {
    printf '"%s"' "$(json_escape "${1-}")"
}

install -d -m 0755 "$(dirname -- "$MANIFEST_PATH")"
MANIFEST_TEMP=$(mktemp "$MANIFEST_PATH.tmp.XXXXXXXX")
{
    printf '{"schema_version":"1","generated_at":'; json_string "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf ',"builder":"build_npu_burn_image.sh","source":{"path":'; json_string "$SOURCE_ROOT"
    printf ',"sha256":'; json_string "$SOURCE_SHA256"
    printf ',"patched_sha256":'; json_string "$PATCHED_SOURCE_SHA256"; printf '}'
    printf ',"compatibility":{"profile":'; json_string "$COMPAT_PROFILE"
    printf ',"patches":['
    for index in "${!PATCH_FILES[@]}"; do
        [ "$index" -eq 0 ] || printf ','
        printf '{"path":'; json_string "${PATCH_FILES[$index]}"
        printf ',"sha256":'; json_string "$(sha256_file "${PATCH_FILES[$index]}")"; printf '}'
    done
    printf ']}'
    printf ',"templates":{"dockerfile_sha256":'; json_string "$DOCKERFILE_SHA256"
    printf ',"entrypoint_sha256":'; json_string "$ENTRYPOINT_SHA256"; printf '}'
    printf ',"docker":{"path":'; json_string "$DOCKER_BIN"
    printf ',"server_version":'; json_string "$DOCKER_VERSION"; printf '}'
    printf ',"image":{"name":'; json_string "$TARGET_IMAGE"
    printf ',"base":'; json_string "$BASE_IMAGE"
    printf ',"id":'; json_string "$IMAGE_ID"
    printf ',"repo_digests":'; json_string "$IMAGE_DIGESTS"
    printf ',"os":'; json_string "$IMAGE_OS"
    printf ',"architecture":'; json_string "$IMAGE_ARCH"
    printf ',"created":'; json_string "$IMAGE_CREATED"; printf '}'
    printf ',"validation":{"python_import":true,"version_command":true,"npu_workload_run":false}'
    printf '}\n'
} >"$MANIFEST_TEMP"
chmod 0640 "$MANIFEST_TEMP"
mv -f -- "$MANIFEST_TEMP" "$MANIFEST_PATH"
MANIFEST_TEMP=

printf '==> image build complete\n'
printf 'Image: %s\n' "$TARGET_IMAGE"
printf 'Image ID: %s\n' "$IMAGE_ID"
printf 'Manifest: %s\n' "$MANIFEST_PATH"
