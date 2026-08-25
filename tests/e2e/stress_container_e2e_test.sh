#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/../.." && pwd)
DOCKER_BIN=${DOCKER_BIN:-docker}
CATMONITOR_IMAGE=${CATMONITOR_CONTAINER_IMAGE:-catmonitor-generic:latest}
FIXTURE_IMAGE=${CATMONITOR_CONTAINER_FIXTURE_IMAGE:-alpine:latest}
TEST_NPU_EXEC=${CATMONITOR_CONTAINER_TEST_NPU_EXEC:-false}
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-stress-container-test.XXXXXXXX")
SUFFIX=$$
DAEMON_CONTAINER="catmonitor-container-e2e-$SUFFIX"
WEB_CONTAINER="catmonitor-web-container-e2e-$SUFFIX"
NPU_CONTAINER="catmonitor-npuburn-container-e2e-$SUFFIX"
SNAPSHOT_VOLUME="catmonitor-snapshot-e2e-$SUFFIX"
DATA_VOLUME="catmonitor-data-e2e-$SUFFIX"
WEB_PORT=${CATMONITOR_CONTAINER_WEB_PORT:-19529}

case "$TEST_NPU_EXEC" in
    true|false) ;;
    *)
        printf 'FAIL: CATMONITOR_CONTAINER_TEST_NPU_EXEC must be true or false\n' >&2
        exit 1
        ;;
esac

cleanup() {
    "$DOCKER_BIN" rm -f \
        "$WEB_CONTAINER" "$DAEMON_CONTAINER" "$NPU_CONTAINER" \
        >/dev/null 2>&1 || true
    "$DOCKER_BIN" volume rm "$SNAPSHOT_VOLUME" "$DATA_VOLUME" \
        >/dev/null 2>&1 || true
    case "$TEST_ROOT" in
        "${TMPDIR:-/tmp}"/catmonitor-stress-container-test.*)
            rm -rf -- "$TEST_ROOT"
            ;;
    esac
}
trap cleanup EXIT HUP INT TERM

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

wait_http() {
    url=$1
    attempt=0
    while [ "$attempt" -lt 30 ]; do
        if curl --fail --silent --show-error "$url" >/dev/null 2>&1; then
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    return 1
}

command -v "$DOCKER_BIN" >/dev/null 2>&1 || fail "docker CLI is unavailable"
command -v curl >/dev/null 2>&1 || fail "curl is unavailable"
"$DOCKER_BIN" info >/dev/null 2>&1 || fail "docker daemon is unavailable"
"$DOCKER_BIN" image inspect "$CATMONITOR_IMAGE" >/dev/null 2>&1 ||
    fail "CATMonitor image is unavailable: $CATMONITOR_IMAGE"
if [ "$TEST_NPU_EXEC" = true ]; then
    "$DOCKER_BIN" image inspect "$FIXTURE_IMAGE" >/dev/null 2>&1 ||
        fail "fixture image is unavailable: $FIXTURE_IMAGE"
fi

mkdir -p "$TEST_ROOT/plugin" "$TEST_ROOT/state/npuburn-output" "$TEST_ROOT/mock-bin"
cp "$REPO_ROOT/features/stress/benchmark_check.sh" "$TEST_ROOT/plugin/benchmark_check.sh"
chmod 0755 "$TEST_ROOT/plugin/benchmark_check.sh"

"$DOCKER_BIN" run --rm --network none --entrypoint /bin/sh \
    -v "$TEST_ROOT/plugin:/opt/catmonitor/stress:ro" \
    "$CATMONITOR_IMAGE" -c '
    command -v bash >/dev/null &&
    command -v lspci >/dev/null &&
    command -v catmonitor-stress-cpu-client >/dev/null &&
    test -x /opt/catmonitor/stress/benchmark_check.sh
' || fail "CATMonitor image lacks the stress runtime contract"

if [ "$TEST_NPU_EXEC" = true ]; then
    "$DOCKER_BIN" run --rm --network none --entrypoint /bin/sh \
        -v /var/run/docker.sock:/var/run/docker.sock \
        "$CATMONITOR_IMAGE" -c 'docker version >/dev/null' ||
        fail "CATMonitor image cannot use the explicitly mounted Docker socket"
else
    if "$DOCKER_BIN" run --rm --network none --entrypoint /bin/sh \
        "$CATMONITOR_IMAGE" -c 'command -v docker >/dev/null'; then
        fail "generic CATMonitor image must not include the Docker client"
    fi
fi

"$DOCKER_BIN" volume create "$SNAPSHOT_VOLUME" >/dev/null
"$DOCKER_BIN" volume create "$DATA_VOLUME" >/dev/null
"$DOCKER_BIN" run -d --name "$DAEMON_CONTAINER" \
    --privileged --network host --pid host \
    -v /:/host:ro \
    -v /etc/os-release:/etc/os-release:ro \
    -v "$SNAPSHOT_VOLUME:/var/lib/catmonitor/snapshot" \
    -v "$DATA_VOLUME:/var/lib/catmonitor/data" \
    "$CATMONITOR_IMAGE" >/dev/null

attempt=0
while [ "$attempt" -lt 30 ]; do
    if "$DOCKER_BIN" exec "$DAEMON_CONTAINER" \
        test -s /var/lib/catmonitor/snapshot/snapshot.json 2>/dev/null; then
        break
    fi
    attempt=$((attempt + 1))
    sleep 1
done
[ "$attempt" -lt 30 ] || {
    "$DOCKER_BIN" logs "$DAEMON_CONTAINER" >&2 || true
    fail "daemon did not produce snapshot.json"
}

"$DOCKER_BIN" run -d --name "$WEB_CONTAINER" \
    --network host --entrypoint /usr/local/bin/web \
    -v "$SNAPSHOT_VOLUME:/var/lib/catmonitor/snapshot:ro" \
    "$CATMONITOR_IMAGE" \
    -addr="127.0.0.1:$WEB_PORT" \
    -snapshot-dir=/var/lib/catmonitor/snapshot \
    -config=/etc/catmonitor/catmonitor.yaml >/dev/null

wait_http "http://127.0.0.1:$WEB_PORT/" || {
    "$DOCKER_BIN" logs "$WEB_CONTAINER" >&2 || true
    fail "Web home did not become ready"
}
curl --fail --silent --show-error \
    "http://127.0.0.1:$WEB_PORT/api/snapshot" >/dev/null ||
    fail "snapshot API is unavailable"
curl --fail --silent --show-error \
    "http://127.0.0.1:$WEB_PORT/stress/" >/dev/null ||
    fail "stress page is unavailable"
curl --fail --silent --show-error \
    "http://127.0.0.1:$WEB_PORT/api/stress/config" \
    >"$TEST_ROOT/stress-config.json"
grep -Fq '"name":"stream"' "$TEST_ROOT/stress-config.json" ||
    fail "container config does not expose STREAM"
grep -Fq '"name":"hpl"' "$TEST_ROOT/stress-config.json" ||
    fail "container config does not expose HPL"
grep -Fq '"name":"hpcg"' "$TEST_ROOT/stress-config.json" ||
    fail "container config does not expose HPCG"
grep -Fq '"name":"npu_burn"' "$TEST_ROOT/stress-config.json" ||
    fail "container config does not expose NPU Burn"
grep -Fq '"feature_enabled":false' "$TEST_ROOT/stress-config.json" ||
    fail "container stress feature must default to disabled"

if [ "$TEST_NPU_EXEC" != true ]; then
    printf 'PASS: generic containerized daemon/Web snapshot and stress read boundary\n'
    exit 0
fi

touch "$TEST_ROOT/davinci0"
cat >"$TEST_ROOT/mock-bin/catmonitor-npu-burn" <<'EOF'
#!/bin/sh
set -eu
for argument in "$@"; do
    [ "$argument" != --output ] || exit 20
    [ "$argument" != --exec_count ] || exit 21
done
printf '%s\n' "$@" | grep -Fxq -- --sdc_detect
printf '%s\n' "$@" | grep -Fxq -- quant_matmul
cat > /opt/catmonitor/npuburn-home/.ascend_npu_burn/output/npu_burn_results.csv <<'CSV'
task,device_id,case_idx,run_count,stream_count,exetime,err_count,result,case_config
quant_matmul,0,0,100,1,1.25,0,PASS,shape=fixture
CSV
EOF
cat >"$TEST_ROOT/mock-bin/lspci" <<'EOF'
#!/bin/sh
printf '%s\n' '0000:00:00.0 Processing accelerators: Huawei Technologies Co., Ltd. Device d803'
EOF
chmod 0755 \
    "$TEST_ROOT/mock-bin/catmonitor-npu-burn" \
    "$TEST_ROOT/mock-bin/lspci"

"$DOCKER_BIN" run -d --name "$NPU_CONTAINER" --network none \
    --entrypoint /bin/sh \
    -v "$TEST_ROOT/mock-bin/catmonitor-npu-burn:/usr/local/bin/catmonitor-npu-burn:ro" \
    -v "$TEST_ROOT/mock-bin/lspci:/usr/local/bin/lspci:ro" \
    -v "$TEST_ROOT/davinci0:/dev/davinci0" \
    -v "$TEST_ROOT/state/npuburn-output:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output" \
    "$FIXTURE_IMAGE" -c 'while :; do sleep 3600; done' >/dev/null

sed -i \
    -e 's|^NPU_BURN_BACKEND=.*|NPU_BURN_BACKEND="docker_exec"|' \
    -e 's|^NPU_BURN_EXECUTABLE=.*|NPU_BURN_EXECUTABLE="/usr/local/bin/catmonitor-npu-burn"|' \
    -e 's|^NPU_BURN_CONTAINER_RUNTIME=.*|NPU_BURN_CONTAINER_RUNTIME="/usr/bin/docker"|' \
    -e "s|^NPU_BURN_CONTAINER_NAME=.*|NPU_BURN_CONTAINER_NAME=\"$NPU_CONTAINER\"|" \
    -e "s|^NPU_BURN_CONTAINER_IMAGE=.*|NPU_BURN_CONTAINER_IMAGE=\"$FIXTURE_IMAGE\"|" \
    -e 's|^NPU_BURN_RUNTIME_CANN=.*|NPU_BURN_RUNTIME_CANN="fixture"|' \
    -e 's|^NPU_BURN_RUNTIME_TORCH_NPU=.*|NPU_BURN_RUNTIME_TORCH_NPU="fixture"|' \
    -e 's|^NPU_BURN_SOC_MODEL=.*|NPU_BURN_SOC_MODEL="fixture"|' \
    -e 's|^NPU_BURN_OUTPUT_DIR=.*|NPU_BURN_OUTPUT_DIR="/var/lib/catmonitor/stress/npuburn-output"|' \
    -e 's|^NPU_BURN_RUN_CASE=.*|NPU_BURN_RUN_CASE="quant_matmul"|' \
    -e 's|^NPU_BURN_DEVICE=.*|NPU_BURN_DEVICE="0"|' \
    -e 's|^NPU_BURN_CHIP_GENERATION=.*|NPU_BURN_CHIP_GENERATION="A3"|' \
    "$TEST_ROOT/plugin/benchmark_check.sh"

cat >"$TEST_ROOT/catmonitor.yaml" <<'EOF'
stress:
  enabled: true
  web_enabled: false
  script_path: /opt/catmonitor/stress/benchmark_check.sh
  report_path: /var/lib/catmonitor/stress/stress-latest.json
  default_benchmarks: [npu_burn]
  benchmarks:
    npu_burn: { enabled: true, timeout: 1m }
EOF

"$DOCKER_BIN" run --rm --network none --entrypoint /usr/local/bin/catmonitor \
    -v "$TEST_ROOT/catmonitor.yaml:/etc/catmonitor/catmonitor.yaml:ro" \
    -v "$TEST_ROOT/plugin:/opt/catmonitor/stress:ro" \
    -v "$TEST_ROOT/state:/var/lib/catmonitor/stress" \
    -v /var/run/docker.sock:/var/run/docker.sock \
    "$CATMONITOR_IMAGE" stress doctor \
    -c /etc/catmonitor/catmonitor.yaml -o json \
    >"$TEST_ROOT/doctor.json" || {
        cat "$TEST_ROOT/doctor.json" >&2 || true
        fail "container-to-container NPU Burn doctor failed"
    }
grep -Fq '"status": "pass"' "$TEST_ROOT/doctor.json" ||
    fail "NPU Burn container preflight did not pass"
grep -Fq '"name": "npu_burn"' "$TEST_ROOT/doctor.json" ||
    fail "NPU Burn doctor result is missing"
grep -Fq '"key": "available_devices"' "$TEST_ROOT/doctor.json" ||
    fail "NPU Burn logical device profile is missing"
grep -Fq '"value": "0"' "$TEST_ROOT/doctor.json" ||
    fail "NPU Burn logical device 0 was not discovered"

"$DOCKER_BIN" run --rm --network none --entrypoint /usr/local/bin/catmonitor \
    -v "$TEST_ROOT/catmonitor.yaml:/etc/catmonitor/catmonitor.yaml:ro" \
    -v "$TEST_ROOT/plugin:/opt/catmonitor/stress:ro" \
    -v "$TEST_ROOT/state:/var/lib/catmonitor/stress" \
    -v /var/run/docker.sock:/var/run/docker.sock \
    "$CATMONITOR_IMAGE" stress --bench npu_burn \
    -c /etc/catmonitor/catmonitor.yaml -o json \
    >"$TEST_ROOT/run.json" || {
        cat "$TEST_ROOT/run.json" >&2 || true
        fail "container-to-container NPU Burn run failed"
    }
grep -Fq '"name": "npu_burn"' "$TEST_ROOT/run.json" ||
    fail "NPU Burn run result is missing"
grep -Fq '"status": "healthy"' "$TEST_ROOT/run.json" ||
    fail "NPU Burn container result was not healthy"
grep -Fq '"passed": 1' "$TEST_ROOT/run.json" ||
    fail "NPU Burn PASS CSV was not parsed"
test -s "$TEST_ROOT/state/stress-latest.json" ||
    fail "NPU Burn latest report was not persisted in shared state"

printf 'PASS: containerized daemon/Web and Docker-exec NPU Burn preflight/run\n'
