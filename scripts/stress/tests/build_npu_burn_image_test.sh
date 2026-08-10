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

SOURCE_BUNDLE="$TEST_ROOT/source tree/override-bundle"
SOURCE="$SOURCE_BUNDLE/source"
SOURCE_METADATA="$SOURCE_BUNDLE/UPSTREAM"
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
cat >"$SOURCE_METADATA" <<'EOF'
schema_version=1
repository=https://example.invalid/override.git
revision=1111111111111111111111111111111111111111
tree=2222222222222222222222222222222222222222
tag_context=development
sync_date=2026-08-10
archive_sha256=3333333333333333333333333333333333333333333333333333333333333333
source_manifest_sha256=4444444444444444444444444444444444444444444444444444444444444444
license=MulanPSL-2.0
direct_modifications=true
EOF

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
        target=${5-${3-}}
        is_base=false
        case "$target" in registry.example/*|base:test) is_base=true ;; esac
        if [ "$is_base" != true ]; then
            if [ ! -f "$FAKE_DOCKER_ROOT/image" ] ||
               [ "$(cat "$FAKE_DOCKER_ROOT/image")" != "$target" ]; then
                exit 1
            fi
        fi
        if [ "${3-}" != --format ]; then
            printf '[]\n'
            exit 0
        fi
        case "$4" in
            '{{.Id}}')
                if [ "$is_base" = true ]; then printf 'sha256:fixture-base-image-id\n'; else printf 'sha256:fixture-image-id\n'; fi
                ;;
            '{{.Os}}') printf 'linux\n' ;;
            '{{.Architecture}}') printf 'arm64\n' ;;
            '{{.Created}}') printf '2026-08-10T00:00:00Z\n' ;;
            '{{join .RepoDigests ","}}')
                if [ "$is_base" = true ]; then printf '%s@sha256:base-fixture\n' "$target"; else printf 'catmonitor/npuburn@sha256:fixture\n'; fi
                ;;
            '{{index .Config.Labels "io.catmonitor.npu-burn.source-sha256"}}')
                if [ "${FAKE_BAD_LABEL-}" = source ]; then printf 'wrong\n'; else cat "$FAKE_DOCKER_ROOT/source-sha"; fi
                ;;
            '{{index .Config.Labels "io.catmonitor.npu-burn.patched-source-sha256"}}') cat "$FAKE_DOCKER_ROOT/patched-sha" ;;
            '{{index .Config.Labels "io.catmonitor.npu-burn.compat-profile"}}') cat "$FAKE_DOCKER_ROOT/profile" ;;
            '{{index .Config.Labels "io.catmonitor.npu-burn.source-origin"}}') cat "$FAKE_DOCKER_ROOT/origin" ;;
            '{{index .Config.Labels "io.catmonitor.npu-burn.upstream-repository"}}') cat "$FAKE_DOCKER_ROOT/repository" ;;
            '{{index .Config.Labels "io.catmonitor.npu-burn.upstream-revision"}}') cat "$FAKE_DOCKER_ROOT/revision" ;;
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
                        SOURCE_ORIGIN=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/origin" ;;
                        UPSTREAM_REPOSITORY=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/repository" ;;
                        UPSTREAM_REVISION=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/revision" ;;
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
        cp "$context/source/README.md" "$FAKE_DOCKER_ROOT/context-readme.md" 2>/dev/null || true
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
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$BUILD_ROOT"
assert_contains "$TEST_ROOT/space-build-root.log" '--build-root cannot contain whitespace'

assert_fails "$TEST_ROOT/missing-base-image.log" \
    bash "$BUILD_SCRIPT" \
    --base-image missing-base:test \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$TEST_ROOT/missing-base-build"
assert_contains "$TEST_ROOT/missing-base-image.log" \
    'base image is unavailable locally; pull or load the approved image first'

BUILD_ROOT="$TEST_ROOT/build-root"
MANIFEST="$BUILD_ROOT/manifests/npu-burn-image-manifest.json"
BUNDLED_SOURCE="$REPO_ROOT/third_party/ascend_npu_burn/source"
BUNDLED_BEFORE=$(sha256sum "$BUNDLED_SOURCE/README.md" | awk '{print $1}')
bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --compat-profile none \
    --build-root "$BUILD_ROOT"
[ "$BUNDLED_BEFORE" = "$(sha256sum "$BUNDLED_SOURCE/README.md" | awk '{print $1}')" ] || \
    fail 'bundled source tree was modified'
[ -f "$MANIFEST" ] || fail 'manifest was not created'
python3 -m json.tool "$MANIFEST" >/dev/null
assert_contains "$MANIFEST" '"schema_version":"2"'
assert_contains "$MANIFEST" '"origin":"bundled"'
assert_contains "$MANIFEST" '"upstream_revision":"381028b688a70e881d97477d7fa1ae8f2a26288e"'
assert_contains "$MANIFEST" '"profile":"none"'
assert_contains "$MANIFEST" '"base_id":"sha256:fixture-base-image-id"'
assert_contains "$MANIFEST" '"architecture":"arm64"'
assert_contains "$MANIFEST" '"npu_workload_run":false'
assert_contains "$FAKE_DOCKER_ROOT/context-npu-burn.py" 'argparse'

assert_fails "$TEST_ROOT/no-force.log" \
    bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$BUILD_ROOT"
assert_contains "$TEST_ROOT/no-force.log" 'use --force'

bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$BUILD_ROOT" \
    --force

cat >"$TEST_ROOT/bundled-test.patch" <<'EOF'
--- a/README.md
+++ b/README.md
@@ -1 +1 @@
-# MindCluster-AscendNPUBurn
+# MindCluster-AscendNPUBurn isolated patch fixture
EOF
BUNDLED_PATCH_ROOT="$TEST_ROOT/bundled-patch-build"
bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:bundled \
    --image catmonitor/npuburn:bundled-patch-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --compat-profile isolated-test \
    --patch "$TEST_ROOT/bundled-test.patch" \
    --build-root "$BUNDLED_PATCH_ROOT"
assert_contains "$FAKE_DOCKER_ROOT/context-readme.md" 'isolated patch fixture'
[ "$BUNDLED_BEFORE" = "$(sha256sum "$BUNDLED_SOURCE/README.md" | awk '{print $1}')" ] || \
    fail 'compatibility patch modified the vendored source tree'
assert_contains "$BUNDLED_PATCH_ROOT/manifests/npu-burn-image-manifest.json" '"origin":"bundled"'

OVERRIDE_ROOT="$TEST_ROOT/override-build"
OVERRIDE_MANIFEST="$OVERRIDE_ROOT/manifests/npu-burn-image-manifest.json"
SOURCE_BEFORE=$(sha256sum "$SOURCE/ascend_npu_burn/npu_burn.py" | awk '{print $1}')
bash "$BUILD_SCRIPT" \
    --source "$SOURCE" \
    --base-image registry.example/ascend:override \
    --image catmonitor/npuburn:override-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$OVERRIDE_ROOT"
assert_contains "$OVERRIDE_MANIFEST" '"origin":"override"'
assert_contains "$OVERRIDE_MANIFEST" '"upstream_repository":"https://example.invalid/override.git"'
assert_contains "$OVERRIDE_MANIFEST" '"upstream_revision":"1111111111111111111111111111111111111111"'
assert_contains "$FAKE_DOCKER_ROOT/context-npu-burn.py" 'ORIGINAL_PROFILE'

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

MISSING_SOURCE_REPO="$TEST_ROOT/missing-source-repo"
install -d -m 0755 "$MISSING_SOURCE_REPO/scripts/stress"
cp "$BUILD_SCRIPT" "$MISSING_SOURCE_REPO/scripts/stress/build_npu_burn_image.sh"
assert_fails "$TEST_ROOT/missing-bundled-source.log" \
    bash "$MISSING_SOURCE_REPO/scripts/stress/build_npu_burn_image.sh" \
    --base-image base:test \
    --image target:test \
    --docker-bin "$TEST_ROOT/tools/docker"
assert_contains "$TEST_ROOT/missing-bundled-source.log" 'source directory is unavailable'

MISSING_METADATA_REPO="$TEST_ROOT/missing-metadata-repo"
install -d -m 0755 \
    "$MISSING_METADATA_REPO/scripts/stress" \
    "$MISSING_METADATA_REPO/third_party/ascend_npu_burn"
cp "$BUILD_SCRIPT" "$MISSING_METADATA_REPO/scripts/stress/build_npu_burn_image.sh"
cp -a "$SOURCE" "$MISSING_METADATA_REPO/third_party/ascend_npu_burn/source"
assert_fails "$TEST_ROOT/missing-bundled-metadata.log" \
    bash "$MISSING_METADATA_REPO/scripts/stress/build_npu_burn_image.sh" \
    --base-image base:test \
    --image target:test \
    --docker-bin "$TEST_ROOT/tools/docker"
assert_contains "$TEST_ROOT/missing-bundled-metadata.log" 'upstream metadata is unavailable'

TAMPERED_BUNDLE_REPO="$TEST_ROOT/tampered-bundle-repo"
install -d -m 0755 \
    "$TAMPERED_BUNDLE_REPO/scripts/stress" \
    "$TAMPERED_BUNDLE_REPO/third_party"
cp "$BUILD_SCRIPT" "$TAMPERED_BUNDLE_REPO/scripts/stress/build_npu_burn_image.sh"
cp -a "$REPO_ROOT/third_party/ascend_npu_burn" "$TAMPERED_BUNDLE_REPO/third_party/ascend_npu_burn"
printf '\nTAMPERED\n' >>"$TAMPERED_BUNDLE_REPO/third_party/ascend_npu_burn/source/README.md"
assert_fails "$TEST_ROOT/tampered-bundled-source.log" \
    bash "$TAMPERED_BUNDLE_REPO/scripts/stress/build_npu_burn_image.sh" \
    --base-image base:test \
    --image target:test \
    --docker-bin "$TEST_ROOT/tools/docker"
assert_contains "$TEST_ROOT/tampered-bundled-source.log" 'bundled source does not match SOURCE_SHA256SUMS'

EXTRA_FILE_BUNDLE_REPO="$TEST_ROOT/extra-file-bundle-repo"
install -d -m 0755 \
    "$EXTRA_FILE_BUNDLE_REPO/scripts/stress" \
    "$EXTRA_FILE_BUNDLE_REPO/third_party"
cp "$BUILD_SCRIPT" "$EXTRA_FILE_BUNDLE_REPO/scripts/stress/build_npu_burn_image.sh"
cp -a "$REPO_ROOT/third_party/ascend_npu_burn" "$EXTRA_FILE_BUNDLE_REPO/third_party/ascend_npu_burn"
printf 'untracked source input\n' >"$EXTRA_FILE_BUNDLE_REPO/third_party/ascend_npu_burn/source/EXTRA_FILE"
assert_fails "$TEST_ROOT/extra-bundled-source-file.log" \
    bash "$EXTRA_FILE_BUNDLE_REPO/scripts/stress/build_npu_burn_image.sh" \
    --base-image base:test \
    --image target:test \
    --docker-bin "$TEST_ROOT/tools/docker"
assert_contains "$TEST_ROOT/extra-bundled-source-file.log" 'bundled source file set does not match SOURCE_SHA256SUMS'

NO_METADATA_BUNDLE="$TEST_ROOT/no-metadata-override"
install -d -m 0755 "$NO_METADATA_BUNDLE"
cp -a "$SOURCE" "$NO_METADATA_BUNDLE/source"
assert_fails "$TEST_ROOT/missing-override-metadata.log" \
    bash "$BUILD_SCRIPT" \
    --source "$NO_METADATA_BUNDLE/source" \
    --base-image base:test \
    --image target:test \
    --docker-bin "$TEST_ROOT/tools/docker"
assert_contains "$TEST_ROOT/missing-override-metadata.log" 'upstream metadata is unavailable'

INVALID_SCHEMA_BUNDLE="$TEST_ROOT/invalid-schema-override"
install -d -m 0755 "$INVALID_SCHEMA_BUNDLE"
cp -a "$SOURCE" "$INVALID_SCHEMA_BUNDLE/source"
sed 's/^schema_version=1$/schema_version=99/' "$SOURCE_METADATA" >"$INVALID_SCHEMA_BUNDLE/UPSTREAM"
assert_fails "$TEST_ROOT/invalid-schema.log" \
    bash "$BUILD_SCRIPT" \
    --source "$INVALID_SCHEMA_BUNDLE/source" \
    --base-image base:test \
    --image target:test \
    --docker-bin "$TEST_ROOT/tools/docker"
assert_contains "$TEST_ROOT/invalid-schema.log" 'unsupported upstream metadata schema_version: 99'

assert_fails "$TEST_ROOT/metadata-without-source.log" \
    bash "$BUILD_SCRIPT" \
    --source-metadata "$SOURCE_METADATA" \
    --base-image base:test \
    --image target:test \
    --docker-bin "$TEST_ROOT/tools/docker"
assert_contains "$TEST_ROOT/metadata-without-source.log" '--source-metadata is only valid with --source'

CRLF_SOURCE="$TEST_ROOT/crlf-source"
cp -a "$SOURCE" "$CRLF_SOURCE"
printf '#!/usr/bin/env bash\r\necho bad\r\n' >"$CRLF_SOURCE/build/build.sh"
assert_fails "$TEST_ROOT/crlf.log" \
    bash "$BUILD_SCRIPT" \
    --source "$CRLF_SOURCE" \
    --source-metadata "$SOURCE_METADATA" \
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
    --source-metadata "$SOURCE_METADATA" \
    --base-image base:test \
    --image target:test \
    --docker-bin "$TEST_ROOT/tools/docker"
assert_contains "$TEST_ROOT/symlink.log" 'must not contain symbolic links'

BAD_LABEL_ROOT="$TEST_ROOT/bad-label-build"
export FAKE_BAD_LABEL=source
assert_fails "$TEST_ROOT/bad-label.log" \
    bash "$BUILD_SCRIPT" \
    --source "$SOURCE" \
    --source-metadata "$SOURCE_METADATA" \
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
