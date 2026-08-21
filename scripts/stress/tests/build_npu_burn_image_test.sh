#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
BUILD_SCRIPT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)/build_npu_burn_image.sh
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd -P)
AUDIT_SCRIPT="$REPO_ROOT/scripts/stress/audit_stress_release.sh"
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

assert_not_contains() {
    if grep -Fq -- "$2" "$1"; then
        fail "$1 unexpectedly contains: $2"
    fi
}

assert_fails() {
    local log=$1
    shift
    if "$@" >"$log" 2>&1; then
        fail "command unexpectedly succeeded: $*"
    fi
}

DOCKERFILE="$REPO_ROOT/docker/stress/npu/Dockerfile"
wheel_install_line=$(grep -nF '# C. Replace any same-version package' "$DOCKERFILE" | cut -d: -f1)
validation_nonce_line=$(grep -nF 'ARG BUILD_VALIDATION_NONCE' "$DOCKERFILE" | head -n 1 | cut -d: -f1)
[ -n "$wheel_install_line" ] && [ -n "$validation_nonce_line" ] || \
    fail 'Dockerfile validation/cache markers are unavailable'
[ "$validation_nonce_line" -gt "$wheel_install_line" ] || \
    fail 'BUILD_VALIDATION_NONCE invalidates the native wheel build cache'

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
        case "$target" in registry.example/*|base:test|missing-base:test) is_base=true ;; esac
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
                if [ "$is_base" = true ]; then
                    case "$target" in
                        *:builder) printf 'sha256:fixture-builder-base-image-id\n' ;;
                        *:runtime|*:runtime-amd64|*:runtime-large) printf 'sha256:fixture-runtime-base-image-id\n' ;;
                        missing-base:test) exit 1 ;;
                        *) printf 'sha256:fixture-base-image-id\n' ;;
                    esac
                else
                    printf 'sha256:fixture-image-id\n'
                fi
                ;;
            '{{.Os}}') printf 'linux\n' ;;
            '{{.Architecture}}') case "$target" in *:runtime-amd64) printf 'amd64\n' ;; *) printf 'arm64\n' ;; esac ;;
            '{{.Size}}')
                case "$target" in
                    *:builder) printf '18000000000\n' ;;
                    *:runtime) printf '4000000000\n' ;;
                    *:runtime-amd64) printf '4000000000\n' ;;
                    *:runtime-large) printf '19000000000\n' ;;
                    *) if [ "$is_base" = true ]; then printf '18000000000\n'; else printf '4500000000\n'; fi ;;
                esac
                ;;
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
                --network)
                    case "$2" in default|host|none) ;; *) exit 92 ;; esac
                    printf '%s\n' "$2" >"$FAKE_DOCKER_ROOT/network"
                    shift 2
                    ;;
                --build-arg)
                    case "$2" in
                        SOURCE_SHA256=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/source-sha" ;;
                        PATCHED_SOURCE_SHA256=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/patched-sha" ;;
                        COMPAT_PROFILE=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/profile" ;;
                        SOURCE_ORIGIN=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/origin" ;;
                        UPSTREAM_REPOSITORY=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/repository" ;;
                        UPSTREAM_REVISION=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/revision" ;;
                        BUILDER_ASCEND_ENV_SCRIPT=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/builder-ascend-env-override" ;;
                        RUNTIME_ASCEND_ENV_SCRIPT=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/runtime-ascend-env-override" ;;
                        BUILDER_BASE_IMAGE=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/builder-base-image" ;;
                        RUNTIME_BASE_IMAGE=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/runtime-base-image" ;;
                        PCIUTILS_ONLINE_INSTALL=*) printf '%s\n' "${2#*=}" >"$FAKE_DOCKER_ROOT/pciutils-online-install" ;;
                    esac
                    shift 2
                    ;;
                *) context=$1; shift ;;
            esac
        done
        [ -f "$dockerfile" ]
        [ -f "$context/entrypoint.sh" ]
        [ -f "$context/ascend_env.sh" ]
        [ -f "$context/validate_entrypoint.sh" ]
        [ -f "$context/validate_runtime_abi.py" ]
        [ -f "$context/runtime_preflight.sh" ]
        [ -f "$context/source/LICENSE.md" ]
        [ -d "$context/pciutils-packages" ]
        grep -Fxq 'pciutils' "$context/runtime-packages.txt"
        if [ "${FAKE_ECHO_PROXY-}" = true ]; then
            printf 'proxy-debug=%s\n' "${HTTP_PROXY-}"
        fi
        cp "$context/source/ascend_npu_burn/npu_burn.py" "$FAKE_DOCKER_ROOT/context-npu-burn.py"
        cp "$context/source/README.md" "$FAKE_DOCKER_ROOT/context-readme.md" 2>/dev/null || true
        selected_builder_env=$(cat "$FAKE_DOCKER_ROOT/builder-ascend-env-override")
        selected_runtime_env=$(cat "$FAKE_DOCKER_ROOT/runtime-ascend-env-override")
        if [ -z "$selected_builder_env" ]; then selected_builder_env=/usr/local/Ascend/ascend-toolkit/set_env.sh; fi
        if [ -z "$selected_runtime_env" ]; then selected_runtime_env=/usr/local/Ascend/cann-9.0.1/set_env.sh; fi
        printf 'CATMONITOR_BUILDER_ASCEND_ENV_SCRIPT=%s\n' "$selected_builder_env"
        printf 'CATMONITOR_RUNTIME_ASCEND_ENV_SCRIPT=%s\n' "$selected_runtime_env"
        printf 'CATMONITOR_ASCEND_ENV_SCRIPT=%s\n' "$selected_runtime_env"
        printf 'CATMONITOR_CANN_VERSION=9.0.1\n'
        if find -L "$context/build-driver-lib64" -maxdepth 3 -name 'libascend_hal.so*' -print -quit | grep -q .; then
            printf 'CATMONITOR_DRIVER_MOUNT_PRESENT_AT_BUILD=true\n'
        else
            printf 'CATMONITOR_DRIVER_MOUNT_PRESENT_AT_BUILD=false\n'
        fi
        if [ "${FAKE_DOCKER_BUILD_FAIL-}" = hal ]; then
            printf 'ERROR: Ascend build environment preflight failed\n' >&2
            printf 'libascend_hal:\n  unresolved: fixture failure\n' >&2
            exit 70
        fi
        printf 'CATMONITOR_PREFLIGHT_LIBASCEND_HAL=PASS\n'
        printf 'CATMONITOR_PREFLIGHT_TORCH=PASS\n'
        printf 'CATMONITOR_PREFLIGHT_TORCH_NPU=PASS\n'
        printf 'CATMONITOR_PREFLIGHT_TBE=PASS\n'
        printf 'CATMONITOR_WHEEL_FILENAME=ascend_npu_burn-26.1.0+torch.2.10.0-cp312-cp312-linux_aarch64.whl\n'
        printf 'CATMONITOR_WHEEL_SHA256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
        printf 'CATMONITOR_PACKAGE_VERSION=26.1.0+torch.2.10.0\n'
        printf 'CATMONITOR_PACKAGE_FILE=/usr/local/lib/python3.12/site-packages/ascend_npu_burn/__init__.py\n'
        printf 'CATMONITOR_CUSTOM_OPS_IMPORT=PASS\n'
        printf 'CATMONITOR_ENTRYPOINT_EXECUTABLE=PASS\n'
        printf 'CATMONITOR_BUILDER_PYTHON_SOABI=cpython-312-aarch64-linux-gnu\n'
        printf 'CATMONITOR_BUILDER_TORCH_VERSION=2.10.0\n'
        printf 'CATMONITOR_BUILDER_TORCH_NPU_VERSION=2.10.0.post2\n'
        printf 'CATMONITOR_RUNTIME_PYTHON_SOABI=cpython-312-aarch64-linux-gnu\n'
        printf 'CATMONITOR_RUNTIME_TORCH_VERSION=2.10.0\n'
        printf 'CATMONITOR_RUNTIME_TORCH_NPU_VERSION=2.10.0.post2\n'
        printf 'CATMONITOR_RUNTIME_NPUBURN_PACKAGE_VERSION=26.1.0+torch.2.10.0\n'
        printf 'CATMONITOR_RUNTIME_ABI=PASS\n'
        printf 'CATMONITOR_RUNTIME_PCIUTILS=PASS\n'
        set -- "$context"/pciutils-packages/*.rpm
        if [ -f "$1" ]; then
            printf 'CATMONITOR_PCIUTILS_SOURCE=offline-rpm\n'
            find "$context/pciutils-packages" -maxdepth 1 -type f -printf '%f\n' | sort >"$FAKE_DOCKER_ROOT/pciutils-package-files"
        else
            set -- "$context"/pciutils-packages/*.deb
            if [ -f "$1" ]; then
                printf 'CATMONITOR_PCIUTILS_SOURCE=offline-deb\n'
                find "$context/pciutils-packages" -maxdepth 1 -type f -printf '%f\n' | sort >"$FAKE_DOCKER_ROOT/pciutils-package-files"
            elif [ "$(cat "$FAKE_DOCKER_ROOT/pciutils-online-install")" = true ]; then
                printf 'CATMONITOR_PCIUTILS_SOURCE=online-dnf\n'
            else
                printf 'CATMONITOR_PCIUTILS_SOURCE=base-image\n'
            fi
        fi
        printf 'CATMONITOR_LSPCI_PATH=/usr/bin/lspci\n'
        printf 'CATMONITOR_LSPCI_VERSION=lspci version 3.8.0\n'
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

assert_fails "$TEST_ROOT/relative-ascend-env.log" \
    bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --ascend-env-script relative/set_env.sh
assert_contains "$TEST_ROOT/relative-ascend-env.log" '--builder-ascend-env-script must be an absolute path'

assert_fails "$TEST_ROOT/relative-build-driver.log" \
    bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$TEST_ROOT/relative-driver-build" \
    --build-driver-lib-dir relative/driver/lib64
assert_contains "$TEST_ROOT/relative-build-driver.log" \
    '--build-driver-lib-dir must be an absolute path on the build host'

assert_fails "$TEST_ROOT/invalid-build-network.log" \
    bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-network bridge
assert_contains "$TEST_ROOT/invalid-build-network.log" \
    '--build-network must be default, host, or none'

touch "$TEST_ROOT/not-a-package.tar"
assert_fails "$TEST_ROOT/invalid-pciutils-package.log" \
    bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --pciutils-package "$TEST_ROOT/not-a-package.tar"
assert_contains "$TEST_ROOT/invalid-pciutils-package.log" \
    '--pciutils-package accepts only .rpm or .deb files'

printf 'rpm fixture\n' >"$TEST_ROOT/real-pciutils.rpm"
ln -s "$TEST_ROOT/real-pciutils.rpm" "$TEST_ROOT/symlink-pciutils.rpm"
assert_fails "$TEST_ROOT/symlink-pciutils-package.log" \
    bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --pciutils-package "$TEST_ROOT/symlink-pciutils.rpm"
assert_contains "$TEST_ROOT/symlink-pciutils-package.log" \
    'pciutils package must not be a symbolic link'

assert_fails "$TEST_ROOT/missing-base-image.log" \
    bash "$BUILD_SCRIPT" \
    --base-image missing-base:test \
    --image catmonitor/npuburn:a3-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$TEST_ROOT/missing-base-build"
assert_contains "$TEST_ROOT/missing-base-image.log" \
    'builder base image is unavailable locally; pull or load the approved image first'

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
assert_contains "$MANIFEST" '"schema_version":7'
printf '{"schema_version":1,"fixture":"cpu"}\n' >"$TEST_ROOT/cpu-manifest.json"
bash "$AUDIT_SCRIPT" \
    --cpu-manifest "$TEST_ROOT/cpu-manifest.json" \
    --npu-manifest "$MANIFEST" \
    --require-runtime-manifests >"$TEST_ROOT/fresh-manifest-audit.log"
assert_contains "$TEST_ROOT/fresh-manifest-audit.log" 'PASS: NPU manifest sha256='
assert_contains "$MANIFEST" '"origin":"bundled"'
assert_contains "$MANIFEST" '"upstream_revision":"381028b688a70e881d97477d7fa1ae8f2a26288e"'
assert_contains "$MANIFEST" '"profile":"none"'
assert_contains "$MANIFEST" '"base_id":"sha256:fixture-base-image-id"'
assert_contains "$MANIFEST" '"base_images":{"mode":"shared"'
assert_contains "$MANIFEST" '"archive_in_final_image":false'
assert_contains "$MANIFEST" '"builder_runtime_match":true'
assert_contains "$MANIFEST" '"runtime_device_preflight_required":true'
assert_contains "$MANIFEST" '"architecture":"arm64"'
assert_contains "$MANIFEST" '"ascend_env_script":"/usr/local/Ascend/cann-9.0.1/set_env.sh"'
assert_contains "$MANIFEST" '"builder_ascend_env_script":"/usr/local/Ascend/ascend-toolkit/set_env.sh"'
assert_contains "$MANIFEST" '"cann_version":"9.0.1"'
assert_contains "$MANIFEST" '"build_network":"default"'
assert_contains "$MANIFEST" '"pciutils":true'
assert_contains "$MANIFEST" '"pciutils_source":"online-dnf"'
assert_contains "$MANIFEST" '"pciutils_package_format":""'
assert_contains "$MANIFEST" '"pciutils_package_count":0'
assert_contains "$MANIFEST" '"pciutils_package_bundle_sha256":""'
assert_contains "$MANIFEST" '"required_packages":["pciutils"]'
assert_contains "$MANIFEST" '"lspci_path":"/usr/bin/lspci"'
assert_contains "$MANIFEST" '"lspci_version":"lspci version 3.8.0"'
assert_contains "$MANIFEST" '"libascend_hal_resolved":true'
assert_contains "$MANIFEST" '"torch_npu_import":true'
assert_contains "$MANIFEST" '"tbe_import":true'
assert_contains "$MANIFEST" '"filename":"ascend_npu_burn-26.1.0+torch.2.10.0-cp312-cp312-linux_aarch64.whl"'
assert_contains "$MANIFEST" '"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"'
assert_contains "$MANIFEST" '"installed_version":"26.1.0+torch.2.10.0"'
assert_contains "$MANIFEST" '"force_installed":true'
assert_contains "$MANIFEST" '"network_access":false'
assert_contains "$MANIFEST" '"custom_ops_import":true'
assert_contains "$MANIFEST" '"runtime_pci_topology_dependency":true'
assert_contains "$MANIFEST" '"entrypoint_validator_sha256":"'
assert_contains "$MANIFEST" '"runtime_abi_validator_sha256":"'
assert_contains "$MANIFEST" '"runtime_preflight_sha256":"'
assert_contains "$MANIFEST" '"driver_mount_present_at_build":false'
assert_contains "$MANIFEST" '"npu_workload_run":false'
assert_contains "$FAKE_DOCKER_ROOT/network" 'default'
assert_contains "$FAKE_DOCKER_ROOT/context-npu-burn.py" 'argparse'

SPLIT_BUILD_ROOT="$TEST_ROOT/split-build"
SPLIT_MANIFEST="$SPLIT_BUILD_ROOT/manifests/npu-burn-image-manifest.json"
bash "$BUILD_SCRIPT" \
    --builder-base-image registry.example/ascend:builder \
    --runtime-base-image registry.example/ascend:runtime \
    --builder-ascend-env-script /opt/ascend/builder/set_env.sh \
    --runtime-ascend-env-script /opt/ascend/runtime/set_env.sh \
    --image catmonitor/npuburn:a3-slim-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$SPLIT_BUILD_ROOT"
assert_contains "$FAKE_DOCKER_ROOT/builder-base-image" 'registry.example/ascend:builder'
assert_contains "$FAKE_DOCKER_ROOT/runtime-base-image" 'registry.example/ascend:runtime'
assert_contains "$SPLIT_MANIFEST" '"base_images":{"mode":"split"'
assert_contains "$SPLIT_MANIFEST" '"name":"registry.example/ascend:builder"'
assert_contains "$SPLIT_MANIFEST" '"name":"registry.example/ascend:runtime"'
assert_contains "$SPLIT_MANIFEST" '"builder_ascend_env_script":"/opt/ascend/builder/set_env.sh"'
assert_contains "$SPLIT_MANIFEST" '"ascend_env_script":"/opt/ascend/runtime/set_env.sh"'
assert_contains "$SPLIT_MANIFEST" '"size_bytes":18000000000'
assert_contains "$SPLIT_MANIFEST" '"size_bytes":4000000000'
assert_contains "$SPLIT_MANIFEST" '"runtime_base_delta_bytes":500000000'

assert_fails "$TEST_ROOT/split-missing-runtime.log" \
    bash "$BUILD_SCRIPT" \
    --builder-base-image registry.example/ascend:builder \
    --image catmonitor/npuburn:invalid \
    --docker-bin "$TEST_ROOT/tools/docker"
assert_contains "$TEST_ROOT/split-missing-runtime.log" '--runtime-base-image is required'

assert_fails "$TEST_ROOT/split-with-legacy.log" \
    bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:legacy \
    --builder-base-image registry.example/ascend:builder \
    --runtime-base-image registry.example/ascend:runtime \
    --image catmonitor/npuburn:invalid \
    --docker-bin "$TEST_ROOT/tools/docker"
assert_contains "$TEST_ROOT/split-with-legacy.log" \
    '--base-image cannot be combined with split base-image options'

assert_fails "$TEST_ROOT/shared-env-with-per-image.log" \
    bash "$BUILD_SCRIPT" \
    --builder-base-image registry.example/ascend:builder \
    --runtime-base-image registry.example/ascend:runtime \
    --image catmonitor/npuburn:invalid \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --ascend-env-script /opt/ascend/shared/set_env.sh \
    --runtime-ascend-env-script /opt/ascend/runtime/set_env.sh
assert_contains "$TEST_ROOT/shared-env-with-per-image.log" \
    '--ascend-env-script cannot be combined with per-image environment overrides'

assert_fails "$TEST_ROOT/split-same-image.log" \
    bash "$BUILD_SCRIPT" \
    --builder-base-image registry.example/ascend:same-a \
    --runtime-base-image registry.example/ascend:same-b \
    --image catmonitor/npuburn:invalid \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$TEST_ROOT/same-image-build"
assert_contains "$TEST_ROOT/split-same-image.log" \
    'split builder/runtime base images resolve to the same image ID'

assert_fails "$TEST_ROOT/split-arch-mismatch.log" \
    bash "$BUILD_SCRIPT" \
    --builder-base-image registry.example/ascend:builder \
    --runtime-base-image registry.example/ascend:runtime-amd64 \
    --image catmonitor/npuburn:invalid \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$TEST_ROOT/arch-mismatch-build"
assert_contains "$TEST_ROOT/split-arch-mismatch.log" \
    'builder/runtime base image architectures do not match'

assert_fails "$TEST_ROOT/split-runtime-not-smaller.log" \
    bash "$BUILD_SCRIPT" \
    --builder-base-image registry.example/ascend:builder \
    --runtime-base-image registry.example/ascend:runtime-large \
    --image catmonitor/npuburn:invalid \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$TEST_ROOT/runtime-large-build"
assert_contains "$TEST_ROOT/split-runtime-not-smaller.log" \
    'runtime base image must be smaller than the builder base image'

ABI_FIXTURE="$TEST_ROOT/abi-fixture"
install -d -m 0755 \
    "$ABI_FIXTURE/site/torch-1.0.dist-info" \
    "$ABI_FIXTURE/site/torch_npu-1.0.post1.dist-info"
printf 'Name: torch\nVersion: 1.0\n' >"$ABI_FIXTURE/site/torch-1.0.dist-info/METADATA"
printf 'Name: torch-npu\nVersion: 1.0.post1\n' >"$ABI_FIXTURE/site/torch_npu-1.0.post1.dist-info/METADATA"
PYTHONPATH="$ABI_FIXTURE/site" python3 "$REPO_ROOT/docker/stress/npu/validate_runtime_abi.py" \
    capture --cann-version 9.0.1 --output "$ABI_FIXTURE/expected.json"
PYTHONPATH="$ABI_FIXTURE/site" python3 "$REPO_ROOT/docker/stress/npu/validate_runtime_abi.py" \
    validate --cann-version 9.0.1 --expected "$ABI_FIXTURE/expected.json" \
    >"$ABI_FIXTURE/validate.log"
assert_contains "$ABI_FIXTURE/validate.log" 'CATMONITOR_RUNTIME_ABI=PASS'
assert_fails "$ABI_FIXTURE/mismatch.log" env PYTHONPATH="$ABI_FIXTURE/site" \
    python3 "$REPO_ROOT/docker/stress/npu/validate_runtime_abi.py" validate \
    --cann-version 8.3 --expected "$ABI_FIXTURE/expected.json"
assert_contains "$ABI_FIXTURE/mismatch.log" 'builder/runtime ABI mismatch'

OFFLINE_PACKAGE_ROOT="$TEST_ROOT/offline packages"
install -d -m 0755 "$OFFLINE_PACKAGE_ROOT"
printf 'pciutils fixture\n' >"$OFFLINE_PACKAGE_ROOT/pciutils-3.8.0.aarch64.rpm"
printf 'libpci fixture\n' >"$OFFLINE_PACKAGE_ROOT/pciutils-libs-3.8.0.aarch64.rpm"
OFFLINE_BUILD_ROOT="$TEST_ROOT/offline-package-build"
OFFLINE_MANIFEST="$OFFLINE_BUILD_ROOT/manifests/npu-burn-image-manifest.json"
bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-offline-pciutils \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --pciutils-package "$OFFLINE_PACKAGE_ROOT/pciutils-3.8.0.aarch64.rpm" \
    --pciutils-package "$OFFLINE_PACKAGE_ROOT/pciutils-libs-3.8.0.aarch64.rpm" \
    --build-root "$OFFLINE_BUILD_ROOT"
assert_contains "$OFFLINE_MANIFEST" '"build_network":"none"'
assert_contains "$OFFLINE_MANIFEST" '"pciutils_source":"offline-rpm"'
assert_contains "$OFFLINE_MANIFEST" '"pciutils_package_format":"rpm"'
assert_contains "$OFFLINE_MANIFEST" '"pciutils_package_count":2'
assert_contains "$OFFLINE_MANIFEST" '"pciutils_package_bundle_sha256":"'
assert_contains "$FAKE_DOCKER_ROOT/pciutils-package-files" 'pciutils-3.8.0.aarch64.rpm'
assert_contains "$FAKE_DOCKER_ROOT/pciutils-package-files" 'pciutils-libs-3.8.0.aarch64.rpm'
[ "$(cat "$FAKE_DOCKER_ROOT/network")" = none ] || \
    fail 'offline package build must keep Docker networking disabled'

ONLINE_BUILD_ROOT="$TEST_ROOT/online-package-build"
bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-online-pciutils \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-network default \
    --build-root "$ONLINE_BUILD_ROOT"
assert_contains "$ONLINE_BUILD_ROOT/manifests/npu-burn-image-manifest.json" '"build_network":"default"'
assert_contains "$ONLINE_BUILD_ROOT/manifests/npu-burn-image-manifest.json" '"pciutils_source":"online-dnf"'

PROXY_BUILD_ROOT="$TEST_ROOT/proxy-build"
PROXY_SECRET='http://proxy-user:p[a]*?ss@proxy.example.invalid:3128'
HTTP_PROXY="$PROXY_SECRET" \
HTTPS_PROXY="$PROXY_SECRET" \
NO_PROXY='127.0.0.1,localhost,registry.example' \
FAKE_ECHO_PROXY=true \
bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:cann9 \
    --image catmonitor/npuburn:a3-proxy \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$PROXY_BUILD_ROOT" >"$TEST_ROOT/proxy-build.log"
assert_contains "$FAKE_DOCKER_ROOT/calls.log" '--build-arg HTTP_PROXY'
assert_contains "$FAKE_DOCKER_ROOT/calls.log" '--build-arg HTTPS_PROXY'
assert_contains "$FAKE_DOCKER_ROOT/calls.log" '--build-arg NO_PROXY'
assert_not_contains "$FAKE_DOCKER_ROOT/calls.log" "$PROXY_SECRET"
assert_contains "$TEST_ROOT/proxy-build.log" 'HTTP proxy: configured'
assert_contains "$TEST_ROOT/proxy-build.log" 'proxy-debug=[proxy-redacted]'
assert_not_contains "$TEST_ROOT/proxy-build.log" "$PROXY_SECRET"
assert_not_contains "$PROXY_BUILD_ROOT/manifests/npu-burn-image-manifest.json" "$PROXY_SECRET"
assert_not_contains "$PROXY_BUILD_ROOT/manifests/npu-burn-image-manifest.json" 'HTTP_PROXY'

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

A2_PATCH="$REPO_ROOT/scripts/stress/patches/ascend_npu_burn/a2-cann83.patch"
A2_PATCH_ROOT="$TEST_ROOT/a2-patch-build"
A2_DRIVER_LIB="$TEST_ROOT/a2-driver/lib64"
install -d -m 0755 "$A2_DRIVER_LIB"
printf 'fixture driver library\n' >"$A2_DRIVER_LIB/libascend_hal.so"
[ -f "$A2_PATCH" ] || fail 'A2 compatibility patch is unavailable'
bash "$BUILD_SCRIPT" \
    --base-image registry.example/ascend:a2-cann83 \
    --image catmonitor/npuburn:a2-cann83-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --compat-profile a2-cann83 \
    --patch "$A2_PATCH" \
    --build-driver-lib-dir "$A2_DRIVER_LIB" \
    --build-root "$A2_PATCH_ROOT"
assert_contains "$FAKE_DOCKER_ROOT/context-npu-burn.py" 'choices=["A2", "A3", "A5"]'
assert_contains "$A2_PATCH_ROOT/manifests/npu-burn-image-manifest.json" '"profile":"a2-cann83"'
assert_contains "$A2_PATCH_ROOT/manifests/npu-burn-image-manifest.json" 'a2-cann83.patch'
assert_contains "$A2_PATCH_ROOT/manifests/npu-burn-image-manifest.json" '"build_driver":{"injected":true'
assert_contains "$A2_PATCH_ROOT/manifests/npu-burn-image-manifest.json" '"included_in_final_image":false'
[ "$BUNDLED_BEFORE" = "$(sha256sum "$BUNDLED_SOURCE/README.md" | awk '{print $1}')" ] || \
    fail 'A2 compatibility patch modified the vendored source tree'

OVERRIDE_ROOT="$TEST_ROOT/override-build"
OVERRIDE_MANIFEST="$OVERRIDE_ROOT/manifests/npu-burn-image-manifest.json"
SOURCE_BEFORE=$(sha256sum "$SOURCE/ascend_npu_burn/npu_burn.py" | awk '{print $1}')
bash "$BUILD_SCRIPT" \
    --source "$SOURCE" \
    --base-image registry.example/ascend:override \
    --image catmonitor/npuburn:override-test \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --ascend-env-script /opt/ascend/custom/set_env.sh \
    --build-root "$OVERRIDE_ROOT"
assert_contains "$OVERRIDE_MANIFEST" '"origin":"override"'
assert_contains "$OVERRIDE_MANIFEST" '"upstream_repository":"https://example.invalid/override.git"'
assert_contains "$OVERRIDE_MANIFEST" '"upstream_revision":"1111111111111111111111111111111111111111"'
assert_contains "$OVERRIDE_MANIFEST" '"ascend_env_script":"/opt/ascend/custom/set_env.sh"'
assert_contains "$OVERRIDE_MANIFEST" '"builder_ascend_env_script":"/opt/ascend/custom/set_env.sh"'
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

PREFLIGHT_FAIL_ROOT="$TEST_ROOT/preflight-fail-build"
export FAKE_DOCKER_BUILD_FAIL=hal
assert_fails "$TEST_ROOT/preflight-build-fail.log" \
    bash "$BUILD_SCRIPT" \
    --base-image base:test \
    --image catmonitor/npuburn:preflight-fail \
    --docker-bin "$TEST_ROOT/tools/docker" \
    --build-root "$PREFLIGHT_FAIL_ROOT"
unset FAKE_DOCKER_BUILD_FAIL
assert_contains "$TEST_ROOT/preflight-build-fail.log" 'Ascend build environment preflight failed'
assert_contains "$TEST_ROOT/preflight-build-fail.log" 'libascend_hal:'
assert_contains "$TEST_ROOT/preflight-build-fail.log" \
    'Docker image build failed during Ascend initialization, wheel build/install, or package validation'
[ ! -e "$PREFLIGHT_FAIL_ROOT/manifests/npu-burn-image-manifest.json" ] || \
    fail 'failed build-time preflight published a manifest'

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
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'SHELL ["/bin/bash", "-c"]'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'ASCEND_ENV_SCRIPT=${BUILDER_ASCEND_ENV_SCRIPT}'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'ASCEND_ENV_SCRIPT=${RUNTIME_ASCEND_ENV_SCRIPT}'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'catmonitor_source_ascend_env'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'catmonitor_ascend_build_preflight'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'metadata.version("ascend-npu-burn")'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'import ascend_npu_burn; print(ascend_npu_burn.__file__)'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'import ascend_npu_burn.custom_ops.custom_ops_lib'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'validate_entrypoint.sh /usr/local/bin/catmonitor-npu-burn'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'CATMONITOR_ENTRYPOINT_EXECUTABLE=PASS'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'CATMONITOR_RUNTIME_PCIUTILS=PASS'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'CATMONITOR_PCIUTILS_SOURCE='
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'command -v lspci'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'COPY pciutils-packages/'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'COPY runtime-packages.txt'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'rpm -Uvh --replacepkgs'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'dpkg -i'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'dnf install -y "${runtime_packages[@]}"'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'apt-get install -y --no-install-recommends "${runtime_packages[@]}"'
assert_not_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" '[ -x /usr/local/bin/catmonitor-npu-burn ]'
assert_not_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" '/usr/local/bin/catmonitor-npu-burn --version'
assert_not_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'CATMONITOR_NPU_DEVICE_COUNT'
assert_not_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'ARG HTTP_PROXY'
assert_not_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'ARG HTTPS_PROXY'
assert_not_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'ENV HTTP_PROXY'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'cd /tmp'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'COPY build-driver-lib64/'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'FROM ${BUILDER_BASE_IMAGE} AS npuburn_builder'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'FROM ${RUNTIME_BASE_IMAGE} AS npuburn_runtime'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" '--target /opt/catmonitor/npuburn-python'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'COPY --from=npuburn_runtime_package /opt/catmonitor/npuburn-python/'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'FROM npuburn_builder AS npuburn_runtime_package'
assert_not_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'FROM ${RUNTIME_BASE_IMAGE} AS npuburn_runtime_package'
assert_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'COPY runtime_preflight.sh /usr/local/bin/catmonitor-npu-burn-preflight'
assert_not_contains "$REPO_ROOT/docker/stress/npu/Dockerfile" 'FROM ${BASE_IMAGE}'
NORMALIZED_DOCKERFILE=$(tr '\n' ' ' <"$REPO_ROOT/docker/stress/npu/Dockerfile")
printf '%s\n' "$NORMALIZED_DOCKERFILE" | grep -Eq \
    'python3 -m pip install .*--no-index .*--no-cache-dir .*--no-deps .*--force-reinstall .*"\$1"' || \
    fail 'local wheel install must be offline and force reinstall the built wheel'
assert_contains "$BUILD_SCRIPT" '--network "$BUILD_NETWORK"'
assert_contains "$BUILD_SCRIPT" 'BUILD_NETWORK=default'
assert_contains "$BUILD_SCRIPT" 'PROXY_ARGS+=(--build-arg "$proxy_name")'
assert_not_contains "$BUILD_SCRIPT" '--build-arg "$proxy_name=$proxy_value"'
if grep -Fq 'TORCH_DEVICE_BACKEND_AUTOLOAD' "$REPO_ROOT/docker/stress/npu/Dockerfile"; then
    fail 'Dockerfile must not disable torch backend autoload'
fi
if grep -Fq 'test -d /usr/local/Ascend/driver' "$REPO_ROOT/docker/stress/npu/Dockerfile"; then
    fail 'Dockerfile must not require a build-time driver mount'
fi
PREFLIGHT_LINE=$(grep -n 'catmonitor_ascend_build_preflight' "$REPO_ROOT/docker/stress/npu/Dockerfile" | head -n 1 | cut -d: -f1)
WHEEL_BUILD_LINE=$(grep -n 'bash build/build.sh' "$REPO_ROOT/docker/stress/npu/Dockerfile" | cut -d: -f1)
WHEEL_INSTALL_LINE=$(grep -n -- '--force-reinstall' "$REPO_ROOT/docker/stress/npu/Dockerfile" | head -n 1 | cut -d: -f1)
RUNTIME_INSTALL_LINE=$(grep -n -- '--target /opt/catmonitor/npuburn-python' "$REPO_ROOT/docker/stress/npu/Dockerfile" | cut -d: -f1)
RUNTIME_ABI_LINE=$(grep -n -- 'validate_runtime_abi.py validate' "$REPO_ROOT/docker/stress/npu/Dockerfile" | cut -d: -f1)
PACKAGE_VALIDATE_LINE=$(grep -n 'CATMONITOR_PACKAGE_VERSION' "$REPO_ROOT/docker/stress/npu/Dockerfile" | cut -d: -f1)
[ "$PREFLIGHT_LINE" -lt "$WHEEL_BUILD_LINE" ] || fail 'Ascend preflight must run before wheel build'
[ "$WHEEL_BUILD_LINE" -lt "$WHEEL_INSTALL_LINE" ] || fail 'wheel build and install must use separate ordered layers'
[ "$WHEEL_INSTALL_LINE" -lt "$PACKAGE_VALIDATE_LINE" ] || fail 'package validation must follow wheel installation'
[ "$PACKAGE_VALIDATE_LINE" -lt "$RUNTIME_INSTALL_LINE" ] || fail 'clean runtime installation must follow builder validation'
[ "$RUNTIME_INSTALL_LINE" -lt "$RUNTIME_ABI_LINE" ] || fail 'runtime ABI validation must follow overlay installation'
[ "$(grep -c '^RUN set -euo pipefail' "$REPO_ROOT/docker/stress/npu/Dockerfile")" -ge 5 ] || \
    fail 'Dockerfile must keep preparation, preflight, wheel build, install, and validation layers separate'
if grep -Eq '(^|[[:space:]])(npu-smi|npu-burn)([[:space:]]|$)' \
    "$REPO_ROOT/docker/stress/npu/Dockerfile"; then
    fail 'Dockerfile must not execute an NPU workload'
fi
if grep -Eq '^COPY --from=npuburn_builder .*driver' "$REPO_ROOT/docker/stress/npu/Dockerfile"; then
    fail 'final image must not copy staged host driver libraries from the builder'
fi
assert_contains "$REPO_ROOT/docker/stress/npu/runtime_preflight.sh" 'ctypes.CDLL("libascend_hal.so")'
assert_contains "$REPO_ROOT/docker/stress/npu/runtime_preflight.sh" 'ascend_npu_burn.custom_ops.custom_ops_lib'
assert_contains "$REPO_ROOT/docker/stress/npu/runtime_preflight.sh" 'CATMONITOR_RUNTIME_PREFLIGHT=PASS'
assert_not_contains "$REPO_ROOT/docker/stress/npu/runtime_preflight.sh" 'npu-burn '

ENTRYPOINT_VALIDATOR="$REPO_ROOT/docker/stress/npu/validate_entrypoint.sh"
assert_contains "$ENTRYPOINT_VALIDATOR" "stat -c '%a'"
assert_contains "$ENTRYPOINT_VALIDATOR" 'mode_value & 0111'
if grep -Eq '(^|[[:space:]])-x([[:space:]]|$)' "$ENTRYPOINT_VALIDATOR"; then
    fail 'build-time entrypoint validator must not use test -x'
fi
VALIDATOR_FIXTURE="$TEST_ROOT/validator-entrypoint"
printf '#!/usr/bin/env bash\nexit 0\n' >"$VALIDATOR_FIXTURE"
chmod 0755 "$VALIDATOR_FIXTURE"
bash "$ENTRYPOINT_VALIDATOR" "$VALIDATOR_FIXTURE"
chmod 0644 "$VALIDATOR_FIXTURE"
assert_fails "$TEST_ROOT/validator-0644.log" \
    bash "$ENTRYPOINT_VALIDATOR" "$VALIDATOR_FIXTURE"
assert_contains "$TEST_ROOT/validator-0644.log" 'has no execute mode bit'
printf '' >"$VALIDATOR_FIXTURE"
chmod 0755 "$VALIDATOR_FIXTURE"
assert_fails "$TEST_ROOT/validator-empty.log" \
    bash "$ENTRYPOINT_VALIDATOR" "$VALIDATOR_FIXTURE"
assert_contains "$TEST_ROOT/validator-empty.log" 'entrypoint is empty'
assert_fails "$TEST_ROOT/validator-directory.log" \
    bash "$ENTRYPOINT_VALIDATOR" "$TEST_ROOT"
assert_contains "$TEST_ROOT/validator-directory.log" 'entrypoint is not a regular file'

ENTRYPOINT_LOG="$TEST_ROOT/entrypoint.log"
export ENTRYPOINT_LOG
cat >"$TEST_ROOT/tools/npu-burn" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >"$ENTRYPOINT_LOG"
EOF
chmod 0755 "$TEST_ROOT/tools/npu-burn"

if CATMONITOR_ASCEND_ENV_HELPER="$TEST_ROOT/missing-helper.sh" \
    PATH="$TEST_ROOT/tools:$PATH" \
    bash "$REPO_ROOT/docker/stress/npu/entrypoint.sh" --version \
    >"$TEST_ROOT/entrypoint-missing-helper.log" 2>&1; then
    fail 'entrypoint unexpectedly accepted a missing Ascend helper'
fi
assert_contains "$TEST_ROOT/entrypoint-missing-helper.log" 'Ascend environment helper is missing'

cat >"$TEST_ROOT/bad-ascend-helper.sh" <<'EOF'
return 19
EOF
if CATMONITOR_ASCEND_ENV_HELPER="$TEST_ROOT/bad-ascend-helper.sh" \
    PATH="$TEST_ROOT/tools:$PATH" \
    bash "$REPO_ROOT/docker/stress/npu/entrypoint.sh" --version \
    >"$TEST_ROOT/entrypoint-bad-helper.log" 2>&1; then
    fail 'entrypoint unexpectedly accepted a failing Ascend helper'
fi
assert_contains "$TEST_ROOT/entrypoint-bad-helper.log" 'failed to source Ascend environment helper:'
if grep -Eq '(^|[[:space:]])-r([[:space:]]|$)' "$REPO_ROOT/docker/stress/npu/entrypoint.sh"; then
    fail 'entrypoint must not require test -r for the Ascend helper'
fi

ENTRY_ASCEND_ROOT="$TEST_ROOT/entrypoint-ascend"
install -d -m 0755 "$ENTRY_ASCEND_ROOT/cann-9.0.1"
cat >"$ENTRY_ASCEND_ROOT/cann-9.0.1/set_env.sh" <<'EOF'
export CATMONITOR_ENTRYPOINT_ENV_SOURCED=true
EOF
CATMONITOR_ASCEND_ENV_ROOT="$ENTRY_ASCEND_ROOT" \
CATMONITOR_ASCEND_ENV_HELPER="$REPO_ROOT/docker/stress/npu/ascend_env.sh" \
PATH="$TEST_ROOT/tools:$PATH" \
    bash "$REPO_ROOT/docker/stress/npu/entrypoint.sh" --version
assert_contains "$ENTRYPOINT_LOG" '--version'

printf 'PASS: build_npu_burn_image.sh\n'
