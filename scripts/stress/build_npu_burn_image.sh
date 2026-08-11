#!/usr/bin/env bash
# Build a traceable Ascend NPU Burn image without creating containers or
# executing an NPU workload. Runtime devices, mounts and lifecycle remain an
# administrator-owned deployment concern.

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd -P)
DOCKERFILE_TEMPLATE="$REPO_ROOT/docker/stress/npu/Dockerfile"
ENTRYPOINT_TEMPLATE="$REPO_ROOT/docker/stress/npu/entrypoint.sh"
ASCEND_ENV_TEMPLATE="$REPO_ROOT/docker/stress/npu/ascend_env.sh"
BUNDLED_SOURCE="$REPO_ROOT/third_party/ascend_npu_burn/source"
BUNDLED_METADATA="$REPO_ROOT/third_party/ascend_npu_burn/UPSTREAM"

SOURCE_ROOT="$BUNDLED_SOURCE"
SOURCE_ORIGIN=bundled
SOURCE_METADATA_PATH="$BUNDLED_METADATA"
SOURCE_METADATA_EXPLICIT=false
BASE_IMAGE=
TARGET_IMAGE=
DOCKER_BIN=
ASCEND_ENV_SCRIPT_OVERRIDE=${ASCEND_ENV_SCRIPT:-}
BUILD_DRIVER_LIB_DIR=
COMPAT_PROFILE=none
BUILD_ROOT=/var/tmp/catmonitor-npu-burn-build
MANIFEST_PATH=
FORCE=false
declare -a PATCH_FILES=()

usage() {
    cat <<'EOF'
Usage: build_npu_burn_image.sh [OPTIONS]

Required:
  --base-image IMAGE        Administrator-approved CANN/torch_npu base image
  --image IMAGE             Output image name and tag

Source controls:
  --source PATH             Override the bundled NPU Burn source for upstream
                            update, development or compatibility testing only
  --source-metadata PATH    UPSTREAM metadata for --source (default: UPSTREAM
                            next to the override source directory)

Build controls:
  --docker-bin PATH         Docker-compatible CLI (default: docker from PATH)
  --ascend-env-script PATH  Explicit CANN env script path inside the base image
                            (default: deterministic auto-discovery)
  --build-driver-lib-dir PATH
                            Optional host Ascend driver lib64 directory used
                            only by the disposable builder stage; it is never
                            copied into the final runtime image
  --compat-profile NAME     Compatibility identity (default: none)
  --patch PATH              Apply an audited -p1 patch; repeatable
  --build-root PATH         Isolated build parent
                            (default: /var/tmp/catmonitor-npu-burn-build)
  --manifest PATH           Output manifest (default: BUILD_ROOT/manifests/
                            npu-burn-image-manifest.json)
  --force                   Replace an existing image tag or manifest
  -h, --help                Show this help

Profile rules:
  * Normal release builds use the source bundled under third_party/.
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
        --source) require_value "$@"; SOURCE_ROOT=$2; SOURCE_ORIGIN=override; shift 2 ;;
        --source-metadata) require_value "$@"; SOURCE_METADATA_PATH=$2; SOURCE_METADATA_EXPLICIT=true; shift 2 ;;
        --base-image) require_value "$@"; BASE_IMAGE=$2; shift 2 ;;
        --image) require_value "$@"; TARGET_IMAGE=$2; shift 2 ;;
        --docker-bin) require_value "$@"; DOCKER_BIN=$2; shift 2 ;;
        --ascend-env-script) require_value "$@"; ASCEND_ENV_SCRIPT_OVERRIDE=$2; shift 2 ;;
        --build-driver-lib-dir) require_value "$@"; BUILD_DRIVER_LIB_DIR=$2; shift 2 ;;
        --compat-profile) require_value "$@"; COMPAT_PROFILE=$2; shift 2 ;;
        --patch) require_value "$@"; PATCH_FILES+=("$2"); shift 2 ;;
        --build-root) require_value "$@"; BUILD_ROOT=$2; shift 2 ;;
        --manifest) require_value "$@"; MANIFEST_PATH=$2; shift 2 ;;
        --force) FORCE=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown argument: $1" ;;
    esac
done

[ -n "$BASE_IMAGE" ] || die "--base-image is required"
[ -n "$TARGET_IMAGE" ] || die "--image is required"
if [ "$SOURCE_ORIGIN" = bundled ] && [ "$SOURCE_METADATA_EXPLICIT" = true ]; then
    die "--source-metadata is only valid with --source"
fi
case "$BASE_IMAGE" in
    -*|*[!A-Za-z0-9._/@:-]*) die "--base-image contains unsupported characters" ;;
esac
case "$TARGET_IMAGE" in
    -*|*@*|*[!A-Za-z0-9._/:-]*) die "--image must be a name/tag, not a digest or option" ;;
esac
if [ -n "$ASCEND_ENV_SCRIPT_OVERRIDE" ]; then
    case "$ASCEND_ENV_SCRIPT_OVERRIDE" in
        /*) ;;
        *) die "--ascend-env-script must be an absolute path inside the base image" ;;
    esac
    case "$ASCEND_ENV_SCRIPT_OVERRIDE" in
        *$'\n'*|*$'\r'*|*$'\t'*) die "--ascend-env-script contains unsupported whitespace" ;;
    esac
fi
if [ -n "$BUILD_DRIVER_LIB_DIR" ]; then
    case "$BUILD_DRIVER_LIB_DIR" in
        /*) ;;
        *) die "--build-driver-lib-dir must be an absolute path on the build host" ;;
    esac
    BUILD_DRIVER_LIB_DIR=$(readlink -f -- "$BUILD_DRIVER_LIB_DIR") || \
        die "build driver lib directory is unavailable"
    [ -d "$BUILD_DRIVER_LIB_DIR" ] || die "build driver lib directory is unavailable"
    case "$BUILD_DRIVER_LIB_DIR" in
        /|/usr|/usr/local|/usr/local/Ascend|/usr/local/Ascend/driver)
            die "--build-driver-lib-dir must name the dedicated driver lib64 directory"
            ;;
    esac
    find -L "$BUILD_DRIVER_LIB_DIR" -maxdepth 3 -name 'libascend_hal.so*' -print -quit | grep -q . || \
        die "build driver lib directory does not contain libascend_hal.so"
fi
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

for tool in readlink install mktemp tar sha256sum find grep date awk wc tee; do
    command -v "$tool" >/dev/null 2>&1 || die "required tool is unavailable: $tool"
done
[ "${#PATCH_FILES[@]}" -eq 0 ] || command -v patch >/dev/null 2>&1 || \
    die "required tool is unavailable: patch"

SOURCE_ROOT=$(readlink -m -- "$SOURCE_ROOT")
[ -d "$SOURCE_ROOT" ] || die "source directory is unavailable"
if [ "$SOURCE_ORIGIN" = override ] && [ "$SOURCE_METADATA_EXPLICIT" != true ]; then
    SOURCE_METADATA_PATH="$(dirname -- "$SOURCE_ROOT")/UPSTREAM"
fi
SOURCE_METADATA_PATH=$(readlink -m -- "$SOURCE_METADATA_PATH")
[ -f "$SOURCE_METADATA_PATH" ] || \
    die "upstream metadata is unavailable; use --source-metadata with an override source"
[ ! -L "$SOURCE_METADATA_PATH" ] || die "upstream metadata must not be a symbolic link"
for required in build/build.sh build/setup.py ascend_npu_burn/npu_burn.py LICENSE.md; do
    [ -f "$SOURCE_ROOT/$required" ] || die "source is missing required file: $required"
done
if find "$SOURCE_ROOT" -type l -print -quit | grep -q .; then
    die "source directory must not contain symbolic links"
fi
if grep -q $'\r' "$SOURCE_ROOT/build/build.sh"; then
    die "source build/build.sh must use LF line endings"
fi

metadata_value() {
    local key=$1
    awk -v key="$key" '
        index($0, key "=") == 1 {
            count++
            value=substr($0, length(key) + 2)
        }
        END {
            if (count != 1 || value == "") exit 1
            print value
        }
    ' "$SOURCE_METADATA_PATH"
}

UPSTREAM_SCHEMA_VERSION=$(metadata_value schema_version) || die "upstream metadata has invalid schema_version"
UPSTREAM_REPOSITORY=$(metadata_value repository) || die "upstream metadata has invalid repository"
UPSTREAM_REVISION=$(metadata_value revision) || die "upstream metadata has invalid revision"
UPSTREAM_TREE=$(metadata_value tree) || die "upstream metadata has invalid tree"
UPSTREAM_TAG_CONTEXT=$(metadata_value tag_context) || die "upstream metadata has invalid tag_context"
UPSTREAM_SYNC_DATE=$(metadata_value sync_date) || die "upstream metadata has invalid sync_date"
UPSTREAM_ARCHIVE_SHA256=$(metadata_value archive_sha256) || die "upstream metadata has invalid archive_sha256"
UPSTREAM_SOURCE_MANIFEST_SHA256=$(metadata_value source_manifest_sha256) || \
    die "upstream metadata has invalid source_manifest_sha256"
UPSTREAM_LICENSE=$(metadata_value license) || die "upstream metadata has invalid license"
UPSTREAM_DIRECT_MODIFICATIONS=$(metadata_value direct_modifications) || \
    die "upstream metadata has invalid direct_modifications"

[ "$UPSTREAM_SCHEMA_VERSION" = 1 ] || die "unsupported upstream metadata schema_version: $UPSTREAM_SCHEMA_VERSION"
case "$UPSTREAM_REVISION:$UPSTREAM_TREE" in
    *[!0-9a-f:]*)
        die "upstream revision and tree must be lowercase 40-character Git object IDs"
        ;;
esac
[ "${#UPSTREAM_REVISION}" -eq 40 ] && [ "${#UPSTREAM_TREE}" -eq 40 ] || \
    die "upstream revision and tree must be lowercase 40-character Git object IDs"
case "$UPSTREAM_ARCHIVE_SHA256:$UPSTREAM_SOURCE_MANIFEST_SHA256" in
    *[!0-9a-f:]*)
        die "upstream SHA-256 metadata is invalid"
        ;;
esac
[ "${#UPSTREAM_ARCHIVE_SHA256}" -eq 64 ] && \
    [ "${#UPSTREAM_SOURCE_MANIFEST_SHA256}" -eq 64 ] || \
    die "upstream SHA-256 metadata is invalid"
case "$UPSTREAM_DIRECT_MODIFICATIONS" in true|false) ;; *) die "upstream direct_modifications must be true or false" ;; esac

if [ "$SOURCE_ORIGIN" = bundled ]; then
    SOURCE_MANIFEST_PATH="$(dirname -- "$SOURCE_METADATA_PATH")/SOURCE_SHA256SUMS"
    [ -f "$SOURCE_MANIFEST_PATH" ] || die "bundled source checksum manifest is unavailable"
    [ "$(sha256sum -- "$SOURCE_MANIFEST_PATH" | awk '{print $1}')" = "$UPSTREAM_SOURCE_MANIFEST_SHA256" ] || \
        die "bundled source checksum manifest does not match upstream metadata"
    (
        cd "$SOURCE_ROOT"
        sha256sum --check --strict "$SOURCE_MANIFEST_PATH" >/dev/null
    ) || die "bundled source does not match SOURCE_SHA256SUMS"
    EXPECTED_SOURCE_FILE_COUNT=$(grep -Ec '^[0-9a-f]{64}  \./' "$SOURCE_MANIFEST_PATH")
    ACTUAL_SOURCE_FILE_COUNT=$(find "$SOURCE_ROOT" -type f | wc -l | awk '{print $1}')
    [ "$EXPECTED_SOURCE_FILE_COUNT" -gt 0 ] && \
        [ "$ACTUAL_SOURCE_FILE_COUNT" -eq "$EXPECTED_SOURCE_FILE_COUNT" ] || \
        die "bundled source file set does not match SOURCE_SHA256SUMS"
    [ "$UPSTREAM_DIRECT_MODIFICATIONS" = false ] || \
        die "bundled upstream metadata must declare direct_modifications=false"
fi

for index in "${!PATCH_FILES[@]}"; do
    PATCH_FILES[$index]=$(readlink -m -- "${PATCH_FILES[$index]}")
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
[ -f "$ASCEND_ENV_TEMPLATE" ] || die "Ascend environment helper template is unavailable"
for shell_template in "$ENTRYPOINT_TEMPLATE" "$ASCEND_ENV_TEMPLATE"; do
    if grep -q $'\r' "$shell_template"; then
        die "shell template must use LF line endings: $shell_template"
    fi
done

DOCKER_VERSION=$("$DOCKER_BIN" version --format '{{.Server.Version}}' 2>&1) || \
    die "docker daemon is unavailable: $DOCKER_VERSION"
BASE_IMAGE_ID=$("$DOCKER_BIN" image inspect --format '{{.Id}}' "$BASE_IMAGE" 2>/dev/null) || \
    die "base image is unavailable locally; pull or load the approved image first: $BASE_IMAGE"
BASE_IMAGE_DIGESTS=$("$DOCKER_BIN" image inspect --format '{{join .RepoDigests ","}}' "$BASE_IMAGE")
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
STAGED_BUILD_DRIVER="$CONTEXT/build-driver-lib64"
install -d -m 0755 "$STAGED_SOURCE" "$STAGED_BUILD_DRIVER"

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

BUILD_DRIVER_INJECTED=false
BUILD_DRIVER_SHA256=
if [ -n "$BUILD_DRIVER_LIB_DIR" ]; then
    (
        cd "$BUILD_DRIVER_LIB_DIR"
        tar -cf - .
    ) | tar --no-same-owner --no-same-permissions -xf - -C "$STAGED_BUILD_DRIVER"
    BUILD_DRIVER_INJECTED=true
    BUILD_DRIVER_SHA256=$(hash_tree "$STAGED_BUILD_DRIVER")
fi

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
install -m 0644 "$ASCEND_ENV_TEMPLATE" "$CONTEXT/ascend_env.sh"
DOCKERFILE_SHA256=$(sha256_file "$DOCKERFILE_TEMPLATE")
ENTRYPOINT_SHA256=$(sha256_file "$ENTRYPOINT_TEMPLATE")
ASCEND_ENV_SHA256=$(sha256_file "$ASCEND_ENV_TEMPLATE")
BUILD_VALIDATION_NONCE="$(date -u +%s)-$$"
DOCKER_BUILD_LOG="$RUN_ROOT/docker-build.log"

printf '==> building Ascend NPU Burn image %s\n' "$TARGET_IMAGE"
printf '    source: %s\n' "$SOURCE_ROOT"
printf '    source origin: %s\n' "$SOURCE_ORIGIN"
printf '    upstream revision: %s\n' "$UPSTREAM_REVISION"
printf '    base image: %s\n' "$BASE_IMAGE"
if [ "$BUILD_DRIVER_INJECTED" = true ]; then
    printf '    build-only driver lib64: %s\n' "$BUILD_DRIVER_LIB_DIR"
else
    printf '    build-only driver lib64: not staged\n'
fi
if [ -n "$ASCEND_ENV_SCRIPT_OVERRIDE" ]; then
    printf '    Ascend env override: %s\n' "$ASCEND_ENV_SCRIPT_OVERRIDE"
else
    printf '    Ascend env: deterministic auto-discovery\n'
fi
printf '    compatibility profile: %s\n' "$COMPAT_PROFILE"
set +e
"$DOCKER_BIN" build \
    --file "$CONTEXT/Dockerfile" \
    --tag "$TARGET_IMAGE" \
    --network none \
    --build-arg "BASE_IMAGE=$BASE_IMAGE" \
    --build-arg "SOURCE_SHA256=$SOURCE_SHA256" \
    --build-arg "PATCHED_SOURCE_SHA256=$PATCHED_SOURCE_SHA256" \
    --build-arg "COMPAT_PROFILE=$COMPAT_PROFILE" \
    --build-arg "SOURCE_ORIGIN=$SOURCE_ORIGIN" \
    --build-arg "UPSTREAM_REPOSITORY=$UPSTREAM_REPOSITORY" \
    --build-arg "UPSTREAM_REVISION=$UPSTREAM_REVISION" \
    --build-arg "ASCEND_ENV_SCRIPT=$ASCEND_ENV_SCRIPT_OVERRIDE" \
    --build-arg "BUILD_VALIDATION_NONCE=$BUILD_VALIDATION_NONCE" \
    "$CONTEXT" 2>&1 | tee "$DOCKER_BUILD_LOG"
DOCKER_BUILD_STATUS=${PIPESTATUS[0]}
set -e
[ "$DOCKER_BUILD_STATUS" -eq 0 ] || \
    die "Docker image build failed during Ascend initialization, wheel build/install, or package validation"

build_marker() {
    local key=$1
    awk -v marker="$key=" '
        index($0, marker) {
            value=substr($0, index($0, marker) + length(marker))
        }
        END {
            sub(/[[:space:]]+$/, "", value)
            if (value == "") exit 1
            print value
        }
    ' "$DOCKER_BUILD_LOG"
}

ASCEND_ENV_SCRIPT_SELECTED=$(build_marker CATMONITOR_ASCEND_ENV_SCRIPT) || \
    die "Docker build did not report the selected Ascend environment script"
CANN_VERSION=$(build_marker CATMONITOR_CANN_VERSION) || \
    die "Docker build did not report the CANN version"
DRIVER_MOUNT_PRESENT_AT_BUILD=$(build_marker CATMONITOR_DRIVER_MOUNT_PRESENT_AT_BUILD) || \
    die "Docker build did not report build-time driver presence"
for marker in LIBASCEND_HAL TORCH TORCH_NPU TBE; do
    [ "$(build_marker "CATMONITOR_PREFLIGHT_$marker")" = PASS ] || \
        die "Docker build did not pass the $marker preflight"
done
WHEEL_FILENAME=$(build_marker CATMONITOR_WHEEL_FILENAME) || \
    die "Docker build did not report the wheel filename"
WHEEL_SHA256=$(build_marker CATMONITOR_WHEEL_SHA256) || \
    die "Docker build did not report the wheel SHA-256"
PACKAGE_VERSION=$(build_marker CATMONITOR_PACKAGE_VERSION) || \
    die "Docker build did not report the installed package version"
PACKAGE_FILE=$(build_marker CATMONITOR_PACKAGE_FILE) || \
    die "Docker build did not report the installed package path"
[ "$(build_marker CATMONITOR_CUSTOM_OPS_IMPORT)" = PASS ] || \
    die "Docker build did not pass the custom ops import validation"
case "$WHEEL_FILENAME" in
    ""|*/*|*\\*) die "Docker build reported an invalid wheel filename" ;;
esac
case "$WHEEL_SHA256" in
    *[!0-9A-Fa-f]*|"") die "Docker build reported an invalid wheel SHA-256" ;;
esac
[ "${#WHEEL_SHA256}" -eq 64 ] || die "Docker build reported an invalid wheel SHA-256 length"
case "$PACKAGE_FILE" in
    /*) ;;
    *) die "Docker build reported a non-absolute installed package path" ;;
esac
case "$DRIVER_MOUNT_PRESENT_AT_BUILD" in
    true|false) ;;
    *) die "Docker build reported invalid driver presence" ;;
esac
if [ "$BUILD_DRIVER_INJECTED" = true ] && [ "$DRIVER_MOUNT_PRESENT_AT_BUILD" != true ]; then
    die "Docker build did not detect the staged build-only driver libraries"
fi

IMAGE_ID=$("$DOCKER_BIN" image inspect --format '{{.Id}}' "$TARGET_IMAGE")
IMAGE_OS=$("$DOCKER_BIN" image inspect --format '{{.Os}}' "$TARGET_IMAGE")
IMAGE_ARCH=$("$DOCKER_BIN" image inspect --format '{{.Architecture}}' "$TARGET_IMAGE")
IMAGE_CREATED=$("$DOCKER_BIN" image inspect --format '{{.Created}}' "$TARGET_IMAGE")
IMAGE_DIGESTS=$("$DOCKER_BIN" image inspect --format '{{join .RepoDigests ","}}' "$TARGET_IMAGE")
LABEL_SOURCE=$("$DOCKER_BIN" image inspect --format '{{index .Config.Labels "io.catmonitor.npu-burn.source-sha256"}}' "$TARGET_IMAGE")
LABEL_PATCHED=$("$DOCKER_BIN" image inspect --format '{{index .Config.Labels "io.catmonitor.npu-burn.patched-source-sha256"}}' "$TARGET_IMAGE")
LABEL_PROFILE=$("$DOCKER_BIN" image inspect --format '{{index .Config.Labels "io.catmonitor.npu-burn.compat-profile"}}' "$TARGET_IMAGE")
LABEL_ORIGIN=$("$DOCKER_BIN" image inspect --format '{{index .Config.Labels "io.catmonitor.npu-burn.source-origin"}}' "$TARGET_IMAGE")
LABEL_REPOSITORY=$("$DOCKER_BIN" image inspect --format '{{index .Config.Labels "io.catmonitor.npu-burn.upstream-repository"}}' "$TARGET_IMAGE")
LABEL_REVISION=$("$DOCKER_BIN" image inspect --format '{{index .Config.Labels "io.catmonitor.npu-burn.upstream-revision"}}' "$TARGET_IMAGE")
[ "$LABEL_SOURCE" = "$SOURCE_SHA256" ] || die "built image source label does not match the staged source"
[ "$LABEL_PATCHED" = "$PATCHED_SOURCE_SHA256" ] || die "built image patched-source label does not match"
[ "$LABEL_PROFILE" = "$COMPAT_PROFILE" ] || die "built image compatibility label does not match"
[ "$LABEL_ORIGIN" = "$SOURCE_ORIGIN" ] || die "built image source-origin label does not match"
[ "$LABEL_REPOSITORY" = "$UPSTREAM_REPOSITORY" ] || die "built image upstream repository label does not match"
[ "$LABEL_REVISION" = "$UPSTREAM_REVISION" ] || die "built image upstream revision label does not match"

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
    printf '{"schema_version":"5","generated_at":'; json_string "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf ',"builder":"build_npu_burn_image.sh","source":{"origin":'; json_string "$SOURCE_ORIGIN"
    printf ',"path":'; json_string "$SOURCE_ROOT"
    printf ',"metadata_path":'; json_string "$SOURCE_METADATA_PATH"
    printf ',"metadata_schema_version":'; json_string "$UPSTREAM_SCHEMA_VERSION"
    printf ',"upstream_repository":'; json_string "$UPSTREAM_REPOSITORY"
    printf ',"upstream_revision":'; json_string "$UPSTREAM_REVISION"
    printf ',"upstream_tree":'; json_string "$UPSTREAM_TREE"
    printf ',"upstream_tag_context":'; json_string "$UPSTREAM_TAG_CONTEXT"
    printf ',"upstream_sync_date":'; json_string "$UPSTREAM_SYNC_DATE"
    printf ',"upstream_archive_sha256":'; json_string "$UPSTREAM_ARCHIVE_SHA256"
    printf ',"source_manifest_sha256":'; json_string "$UPSTREAM_SOURCE_MANIFEST_SHA256"
    printf ',"license":'; json_string "$UPSTREAM_LICENSE"
    printf ',"direct_modifications":%s' "$UPSTREAM_DIRECT_MODIFICATIONS"
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
    printf ',"entrypoint_sha256":'; json_string "$ENTRYPOINT_SHA256"
    printf ',"ascend_env_sha256":'; json_string "$ASCEND_ENV_SHA256"; printf '}'
    printf ',"docker":{"path":'; json_string "$DOCKER_BIN"
    printf ',"server_version":'; json_string "$DOCKER_VERSION"; printf '}'
    printf ',"image":{"name":'; json_string "$TARGET_IMAGE"
    printf ',"base":'; json_string "$BASE_IMAGE"
    printf ',"base_id":'; json_string "$BASE_IMAGE_ID"
    printf ',"base_repo_digests":'; json_string "$BASE_IMAGE_DIGESTS"
    printf ',"id":'; json_string "$IMAGE_ID"
    printf ',"repo_digests":'; json_string "$IMAGE_DIGESTS"
    printf ',"os":'; json_string "$IMAGE_OS"
    printf ',"architecture":'; json_string "$IMAGE_ARCH"
    printf ',"created":'; json_string "$IMAGE_CREATED"; printf '}'
    printf ',"runtime":{"ascend_env_script":'; json_string "$ASCEND_ENV_SCRIPT_SELECTED"
    printf ',"cann_version":'; json_string "$CANN_VERSION"; printf '}'
    printf ',"build_driver":{"injected":%s' "$BUILD_DRIVER_INJECTED"
    printf ',"source_path":'; json_string "$BUILD_DRIVER_LIB_DIR"
    printf ',"sha256":'; json_string "$BUILD_DRIVER_SHA256"
    printf ',"included_in_final_image":false}'
    printf ',"wheel":{"filename":'; json_string "$WHEEL_FILENAME"
    printf ',"sha256":'; json_string "${WHEEL_SHA256,,}"
    printf ',"installed_version":'; json_string "$PACKAGE_VERSION"
    printf ',"installed_package_file":'; json_string "$PACKAGE_FILE"
    printf ',"force_installed":true,"network_access":false}'
    printf ',"validation":{"libascend_hal_resolved":true'
    printf ',"torch_import":true,"torch_npu_import":true,"tbe_import":true'
    printf ',"wheel_build":true,"wheel_install":true,"ascend_npu_burn_import":true'
    printf ',"custom_ops_import":true'
    printf ',"version_command":true,"driver_mount_present_at_build":%s' "$DRIVER_MOUNT_PRESENT_AT_BUILD"
    printf ',"npu_workload_run":false}'
    printf '}\n'
} >"$MANIFEST_TEMP"
chmod 0640 "$MANIFEST_TEMP"
mv -f -- "$MANIFEST_TEMP" "$MANIFEST_PATH"
MANIFEST_TEMP=

printf '==> image build complete\n'
printf 'Image: %s\n' "$TARGET_IMAGE"
printf 'Image ID: %s\n' "$IMAGE_ID"
printf 'Manifest: %s\n' "$MANIFEST_PATH"
