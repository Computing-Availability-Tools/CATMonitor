#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
BUILD_SCRIPT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)/build_npu_burn_image.sh
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd -P)
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-npu-image-test.XXXXXXXX")

cleanup() {
    case "$TEST_ROOT" in "${TMPDIR:-/tmp}"/catmonitor-npu-image-test.*) rm -rf -- "$TEST_ROOT" ;; esac
}
trap cleanup EXIT HUP INT TERM

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_contains() {
    grep -Fq -- "$2" "$1" || fail "$1 does not contain: $2"
}

assert_fails() {
    local log=$1
    shift
    if "$@" >"$log" 2>&1; then
        fail "command unexpectedly succeeded: $*"
    fi
}

SOURCE="$TEST_ROOT/source tree/MindCluster-AscendNPUBurn"
install -d -m 0755 "$SOURCE/build" "$SOURCE/ascend_npu_burn"
cat >"$SOURCE/build/build.sh" <<'EOF'
#!/usr/bin/env bash
set -e
echo build fixture
EOF
cat >"$SOURCE/build/setup.py" <<'EOF'
print("setup fixture")
EOF
cat >"$SOURCE/ascend_npu_burn/npu_burn.py" <<'EOF'
print("ORIGINAL_PROFILE")
EOF
cat >"$SOURCE/LICENSE.md" <<'EOF'
Mulan PSL v2 fixture
EOF
chmod 0755 "$SOURCE/build/build.sh"

FAKE_DOCKER_ROOT="$TEST_ROOT/fake-docker-state"
export FAKE_DOCKER_ROOT
install -d -m 0755 "$TEST_ROOT/tools" "$FAKE_DOCKER_ROOT"
cat >"$TEST_ROOT/tools/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$FAKE_DOCKER_ROOT/calls.log"

case "${1-}" in
    version)
        printf '26.1.4\n'
        ;;
    image)
        [ "${2-}" = inspect ] || exit 90
        if [ ! -f "$FAKE_DOCKER_ROOT/image" ] ||
           [ "$(cat "$FAKE_DOCKER_ROOT/image")" != "${5-${3-}}" ]; then
            exit 1
        fi
        if [ "${3-}" != --format ]; then
            printf '[]\n'
            exit 0
        fi
        case "$4" in
            '{{.Id}}') printf 'sha256:fixture-image-id\n' ;;
            '{{.Os}}') printf 'linux\n' ;;
            '{{.Architecture}}') printf 'arm64\n' ;;
            '{{.Created}}') printf '2026-08-10T00:00:00Z\n' ;;
            '{{join .RepoDigests ","}}') printf 'catmonitor/npuburn@sha256:fixture\n' ;;
            '{{index .Config.Labels "io.catmonitor.npu-burn.source-sha256"}}')
                if [ "${FAKE_BAD_LABEL-}" = source ]; then printf 'wrong\n'; else cat "$FAKE_DOCKER_ROOT/source-sha"; fi
                ;;
            '{{index .Config.Labels "io.catmonitor.npu-burn.patched-source-sha256"}}') cat "$FAKE_DOCKER_ROOT/patched-sha" ;;
            '{{index .Config.Labels "io.catmonitor.npu-burn.compat-profile"}}') cat "$FAKE_DOCKER_ROOT/profile" ;;
            *) printf 'unsupported format: %s\n' "$4" >&2; exit 91 ;;
        esac
        ;;
    build)
        shift
        context=
        image=
        while [ "$#" -gt 0 ]; do
            case "$1" in
                --file) dockerfile=$2; shift 2 ;;
                --tag) image=$2; shift 2 ;;
                --build-arg)
                    case "$2" in
                        SOURCE_SHA256=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/source-sha" ;;
                        PATCHED_SOURCE_SHA256=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/patched-sha" ;;
                        COMPAT_PROFILE=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/profile" ;;
                    esac
                    shift 2
                    ;;
                *) context=$1; shift ;;
            esac
        done
        [ -f "$dockerfile" ]
        [ -f "$context/entrypoint.sh" ]
        [ -f "$context/source/LICENSE.md" ]
        cp "$context/source/ascend_npu_burn/npu_burn.py" "$FAKE_DOCKER_ROOT/context-npu-burn.py"
        printf '%s\n' "$image" >"$FAKE_DOCKER_ROOT/image"
        printf 'Successfully built fixture-image-id\n'
        ;;
    *)
        printf 'forbidden docker operation: %s\n' "${1-}" >&2
        exit 99
        ;;
esac
EOF
chmod 0755 "$TEST_ROOT/tools/docker"

BUILD_ROOT="$TEST_ROOT/build root is deliberately rejected"
assert_fails "$TEST_ROOT/space-build-root.log" \
    bash "$BUILD_SCRIPT" \
    --source "$SOURCE" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$BUILD_ROOT"
assert_contains "$TEST_ROOT/space-build-root.log" '--build-root cannot contain whitespace'

BUILD_ROOT="$TEST_ROOT/build-root"
MANIFEST="$BUILD_ROOT/manifests/npu-burn-image-manifest.json"
SOURCE_BEFORE=$(sha256sum "$SOURCE/ascend_npu_burn/npu_burn.py" | awk '{print $1}')
bash "$BUILD_SCRIPT" \
    --source "$SOURCE" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --compat-profile none \
    --build-root "$BUILD_ROOT"
SOURCE_AFTER=$(sha256sum "$SOURCE/ascend_npu_burn/npu_burn.py" | awk '{print $1}')
[ "$SOURCE_BEFORE" = "$SOURCE_AFTER" ] || fail 'source tree was modified'
[ -f "$MANIFEST" ] || fail 'manifest was not created'
python3 -m json.tool "$MANIFEST" >/dev/null
assert_contains "$MANIFEST" '"profile":"none"'
assert_contains "$MANIFEST" '"architecture":"arm64"'
assert_contains "$MANIFEST" '"npu_workload_run":false'
assert_contains "$FAKE_DOCKER_ROOT/context-npu-burn.py" 'ORIGINAL_PROFILE'

assert_fails "$TEST_ROOT/no-force.log" \
    bash "$BUILD_SCRIPT" \
    --source "$SOURCE" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$BUILD_ROOT"
assert_contains "$TEST_ROOT/no-force.log" 'use --force'

bash "$BUILD_SCRIPT" \
    --source "$SOURCE" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$BUILD_ROOT" \
    --force

cat >"$TEST_ROOT/custom-test.patch" <<'EOF'
--- a/ascend_npu_burn/npu_burn.py
+++ b/ascend_npu_burn/npu_burn.py
@@ -1 +1 @@
-print("ORIGINAL_PROFILE")
+print("PATCHED_PROFILE")
EOF
CUSTOM_ROOT="$TEST_ROOT/custom-build"
bash "$BUILD_SCRIPT" \
    --source "$SOURCE" \
    --base-image registry.example/ascend:custom \
    --image catmonitor/npuburn:custom-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --compat-profile custom-test \
    --patch "$TEST_ROOT/custom-test.patch" \
    --build-root "$CUSTOM_ROOT"
CUSTOM_MANIFEST="$CUSTOM_ROOT/manifests/npu-burn-image-manifest.json"
assert_contains "$FAKE_DOCKER_ROOT/context-npu-burn.py" 'PATCHED_PROFILE'
assert_contains "$CUSTOM_MANIFEST" '"profile":"custom-test"'
assert_contains "$CUSTOM_MANIFEST" 'custom-test.patch'
[ "$(sha256sum "$SOURCE/ascend_npu_burn/npu_burn.py" | awk '{print $1}')" = "$SOURCE_BEFORE" ] || \
    fail 'custom patch modified the original source'

assert_fails "$TEST_ROOT/none-with-patch.log" \
    bash "$BUILD_SCRIPT" \
    --source "$SOURCE" \
    --base-image base:test \
    --image target:test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --compat-profile none \
    --patch "$TEST_ROOT/custom-test.patch"
assert_contains "$TEST_ROOT/none-with-patch.log" 'profile none does not accept --patch'

assert_fails "$TEST_ROOT/profile-without-patch.log" \
    bash "$BUILD_SCRIPT" \
    --source "$SOURCE" \
    --base-image base:test \
    --image target:test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --compat-profile a3-future
assert_contains "$TEST_ROOT/profile-without-patch.log" 'requires at least one --patch'

CRLF_SOURCE="$TEST_ROOT/crlf-source"
cp -a "$SOURCE" "$CRLF_SOURCE"
printf '#!/usr/bin/env bash\r\necho bad\r\n' >"$CRLF_SOURCE/build/build.sh"
assert_fails "$TEST_ROOT/crlf.log" \
    bash "$BUILD_SCRIPT" \
    --source "$CRLF_SOURCE" \
    --base-image base:test \
    --image target:test \
    --docker-bin "$TEST_ROOT/tools/docker"
assert_contains "$TEST_ROOT/crlf.log" 'must use LF line endings'

SYMLINK_SOURCE="$TEST_ROOT/symlink-source"
cp -a "$SOURCE" "$SYMLINK_SOURCE"
ln -s LICENSE.md "$SYMLINK_SOURCE/license-link"
assert_fails "$TEST_ROOT/symlink.log" \
    bash "$BUILD_SCRIPT" \
    --source "$SYMLINK_SOURCE" \
    --base-image base:test \
    --image target:test \
    --docker-bin "$TEST_ROOT/tools/docker"
assert_contains "$TEST_ROOT/symlink.log" 'must not contain symbolic links'

BAD_LABEL_ROOT="$TEST_ROOT/bad-label-build"
export FAKE_BAD_LABEL=source
assert_fails "$TEST_ROOT/bad-label.log" \
    bash "$BUILD_SCRIPT" \
    --source "$SOURCE" \
    --base-image base:test \
    --image catmonitor/npuburn:bad-label \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$BAD_LABEL_ROOT"
unset FAKE_BAD_LABEL
assert_contains "$TEST_ROOT/bad-label.log" 'source label does not match'
[ ! -e "$BAD_LABEL_ROOT/manifests/npu-burn-image-manifest.json" ] || \
    fail 'failed image validation published a manifest'

if grep -Eq '^(run|create|start|stop|rm|exec)([[:space:]]|$)' "$FAKE_DOCKER_ROOT/calls.log"; then
    fail 'image builder invoked a container lifecycle or execution operation'
fi
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" "python3 -c 'import torch; import torch_npu; import ascend_npu_burn'"
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" '/usr/local/bin/catmonitor-npu-burn --version'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'pip install --no-cache-dir --no-deps'
if grep -Eq '(^|[[:space:]])(npu-smi|npu-burn)([[:space:]]|$)' \
    "$REPO_ROOT/docker/stress/npu/Dockerfile"; then
    fail 'Dockerfile must not execute an NPU workload'
fi

ENTRYPOINT_LOG="$TEST_ROOT/entrypoint.log"
export ENTRYPOINT_LOG
cat >"$TEST_ROOT/tools/npu-burn" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >"$ENTRYPOINT_LOG"
EOF
chmod 0755 "$TEST_ROOT/tools/npu-burn"
PATH="$TEST_ROOT/tools:$PATH" bash "$REPO_ROOT/docker/stress/npu/entrypoint.sh" --version
assert_contains "$ENTRYPOINT_LOG" '--version'

printf 'PASS: build_npu_burn_image.sh\n'
