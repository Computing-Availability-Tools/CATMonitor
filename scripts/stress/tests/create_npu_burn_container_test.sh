#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
SCRIPT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)/create_npu_burn_container.sh
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-npu-container-test.XXXXXXXX")

cleanup() {
    case "$TEST_ROOT" in "${TMPDIR:-/tmp}"/catmonitor-npu-container-test.*) rm -rf -- "$TEST_ROOT" ;; esac
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

assert_not_line() {
    if grep -Fxq -- "$2" "$1"; then
        fail "$1 unexpectedly contains line: $2"
    fi
}

assert_fails() {
    local log=$1
    shift
    if "$@" >"$log" 2>&1; then
        fail "command unexpectedly succeeded: $*"
    fi
}

HOST_ROOT="$TEST_ROOT/host"
STATE_ROOT="$TEST_ROOT/docker-state"
TOOLS="$TEST_ROOT/tools"
OUTPUT_DIR="$TEST_ROOT/output dir"
install -d -m 0755 \
    "$HOST_ROOT/dev" \
    "$HOST_ROOT/usr/local/Ascend/driver/lib64" \
    "$HOST_ROOT/usr/local/dcmi" \
    "$HOST_ROOT/usr/local/bin" \
    "$HOST_ROOT/etc" \
    "$STATE_ROOT" \
    "$TOOLS"
for path in \
    dev/davinci0 dev/davinci1 dev/davinci7 \
    dev/davinci_manager dev/devmm_svm dev/hisi_hdc \
    usr/local/Ascend/driver/version.info \
    etc/ascend_install.info \
    usr/local/bin/npu-smi; do
    printf 'fixture\n' >"$HOST_ROOT/$path"
done

cat >"$TOOLS/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${FAKE_DOCKER_STATE:?}"
printf 'CALL' >>"$FAKE_DOCKER_STATE/calls.log"
for arg in "$@"; do printf ' <%s>' "$arg" >>"$FAKE_DOCKER_STATE/calls.log"; done
printf '\n' >>"$FAKE_DOCKER_STATE/calls.log"

case "${1-} ${2-}" in
    'version '*) printf '26.1.0\n' ;;
    'image inspect') printf 'sha256:fixture-npuburn-image\n' ;;
    'container inspect')
        [ -f "$FAKE_DOCKER_STATE/container" ] || exit 1
        profile=$(cat "$FAKE_DOCKER_STATE/profile")
        running=$(cat "$FAKE_DOCKER_STATE/running")
        image_id=sha256:fixture-npuburn-image
        if [ "${FAKE_DOCKER_MISMATCH-}" = true ]; then profile=unexpected; fi
        restart=unless-stopped
        if [ "${FAKE_DOCKER_IMAGE_MISMATCH-}" = true ]; then image_id=sha256:unexpected-image; fi
        if [ "${FAKE_DOCKER_RESTART_MISMATCH-}" = true ]; then restart=no; fi
        printf '%s|%s|%s|ascend|%s|true|host|67108864|/workspace|/bin/bash\n' \
            "$running" "$image_id" "$profile" "$restart"
        ;;
    'start '*) printf 'true\n' >"$FAKE_DOCKER_STATE/running"; printf '%s\n' "${2-}" ;;
    'run --detach')
        : >"$FAKE_DOCKER_STATE/run.args"
        previous=
        for arg in "$@"; do
            printf '%s\n' "$arg" >>"$FAKE_DOCKER_STATE/run.args"
            case "$previous" in
                --label)
                    case "$arg" in
                        io.catmonitor.npu-burn.profile-sha256=*)
                            printf '%s\n' "${arg#*=}" >"$FAKE_DOCKER_STATE/profile"
                            ;;
                    esac
                    ;;
            esac
            previous=$arg
        done
        : >"$FAKE_DOCKER_STATE/container"
        printf 'true\n' >"$FAKE_DOCKER_STATE/running"
        printf 'fixture-container-id\n'
        ;;
    *) printf 'unexpected docker call: %s\n' "$*" >&2; exit 98 ;;
esac
EOF
chmod 0755 "$TOOLS/docker"

run_bootstrap() {
    CATMONITOR_NPU_BOOTSTRAP_TESTING=true \
    CATMONITOR_NPU_BOOTSTRAP_HOST_ROOT="$HOST_ROOT" \
    FAKE_DOCKER_STATE="$STATE_ROOT" \
        bash "$SCRIPT" \
        --image catmonitor/npuburn:a3-v3 \
        --name catmonitor-npuburn-a3 \
        --output-dir "$OUTPUT_DIR" \
        --docker-bin "$TOOLS/docker"
}

run_bootstrap >"$TEST_ROOT/create.log"
[ -d "$OUTPUT_DIR" ] || fail 'output directory was not created'
assert_contains "$TEST_ROOT/create.log" 'Logical devices: 0,1,7'
assert_contains "$TEST_ROOT/create.log" 'Restart policy: unless-stopped'
for mapping in \
    "$HOST_ROOT/dev/davinci0:/dev/davinci0" \
    "$HOST_ROOT/dev/davinci1:/dev/davinci1" \
    "$HOST_ROOT/dev/davinci7:/dev/davinci7" \
    "$HOST_ROOT/dev/davinci_manager:/dev/davinci_manager" \
    "$HOST_ROOT/dev/devmm_svm:/dev/devmm_svm" \
    "$HOST_ROOT/dev/hisi_hdc:/dev/hisi_hdc"; do
    assert_contains "$STATE_ROOT/run.args" "$mapping"
done
assert_not_contains "$STATE_ROOT/run.args" '/dev/davinci2:/dev/davinci2'
for required in \
    '--runtime' ascend '--restart' unless-stopped '--privileged' '--network' host '--shm-size' 64m \
    '--workdir' /workspace '--security-opt' label=disable '--entrypoint' /bin/bash; do
    assert_contains "$STATE_ROOT/run.args" "$required"
done
assert_contains "$STATE_ROOT/run.args" "$OUTPUT_DIR:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output:rw"
assert_not_line "$STATE_ROOT/run.args" '--env'
assert_not_line "$STATE_ROOT/run.args" '-e'

run_calls_before=$(grep -c 'CALL <run>' "$STATE_ROOT/calls.log")
run_bootstrap >"$TEST_ROOT/idempotent.log"
run_calls_after=$(grep -c 'CALL <run>' "$STATE_ROOT/calls.log")
[ "$run_calls_before" -eq "$run_calls_after" ] || fail 'matching running container was recreated'
assert_contains "$TEST_ROOT/idempotent.log" 'already running with the expected profile'

printf 'false\n' >"$STATE_ROOT/running"
run_bootstrap >"$TEST_ROOT/start.log"
assert_contains "$TEST_ROOT/start.log" 'Started matching NPU Burn container'
[ "$(cat "$STATE_ROOT/running")" = true ] || fail 'matching stopped container was not started'

FAKE_DOCKER_MISMATCH=true assert_fails "$TEST_ROOT/mismatch.log" run_bootstrap
assert_contains "$TEST_ROOT/mismatch.log" 'unexpected image or runtime profile'
FAKE_DOCKER_IMAGE_MISMATCH=true assert_fails "$TEST_ROOT/image-mismatch.log" run_bootstrap
assert_contains "$TEST_ROOT/image-mismatch.log" 'unexpected image or runtime profile'
FAKE_DOCKER_RESTART_MISMATCH=true assert_fails "$TEST_ROOT/restart-mismatch.log" run_bootstrap
assert_contains "$TEST_ROOT/restart-mismatch.log" 'unexpected image or runtime profile'

assert_fails "$TEST_ROOT/invalid-restart.log" env \
    CATMONITOR_NPU_BOOTSTRAP_TESTING=true \
    CATMONITOR_NPU_BOOTSTRAP_HOST_ROOT="$HOST_ROOT" \
    FAKE_DOCKER_STATE="$STATE_ROOT" \
    bash "$SCRIPT" --image catmonitor/npuburn:a3-v3 \
    --docker-bin "$TOOLS/docker" --restart-policy invalid
assert_contains "$TEST_ROOT/invalid-restart.log" \
    '--restart-policy must be no, on-failure, always, or unless-stopped'

for control in davinci_manager devmm_svm hisi_hdc; do
    mv -- "$HOST_ROOT/dev/$control" "$HOST_ROOT/dev/$control.saved"
    assert_fails "$TEST_ROOT/missing-$control.log" run_bootstrap
    assert_contains "$TEST_ROOT/missing-$control.log" \
        "required Ascend control device is unavailable: /dev/$control"
    mv -- "$HOST_ROOT/dev/$control.saved" "$HOST_ROOT/dev/$control"
done

mv -- "$HOST_ROOT/usr/local/Ascend/driver/version.info" \
    "$HOST_ROOT/usr/local/Ascend/driver/version.info.saved"
assert_fails "$TEST_ROOT/missing-driver-path.log" run_bootstrap
assert_contains "$TEST_ROOT/missing-driver-path.log" \
    'required Ascend host path is unavailable: /usr/local/Ascend/driver/version.info'
mv -- "$HOST_ROOT/usr/local/Ascend/driver/version.info.saved" \
    "$HOST_ROOT/usr/local/Ascend/driver/version.info"

for id in 0 1 7; do
    mv -- "$HOST_ROOT/dev/davinci$id" "$HOST_ROOT/dev/davinci$id.saved"
done
assert_fails "$TEST_ROOT/no-devices.log" run_bootstrap
assert_contains "$TEST_ROOT/no-devices.log" 'no host /dev/davinciN device nodes were found'

printf 'PASS: create_npu_burn_container.sh\n'
