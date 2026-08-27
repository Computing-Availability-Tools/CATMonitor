#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd -P)
GENERATOR="$REPO_ROOT/scripts/stress/generate_stress_deployment.sh"
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-stress-generator-v2.XXXXXXXX")
cleanup() { rm -rf -- "$TEST_ROOT"; }
trap cleanup EXIT HUP INT TERM

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
contains() { grep -F -- "$2" "$1" >/dev/null || fail "$1 is missing: $2"; }
not_contains() { ! grep -F -- "$2" "$1" >/dev/null || fail "$1 unexpectedly contains: $2"; }
expect_fail() { if "$@" >/dev/null 2>&1; then fail "command unexpectedly succeeded: $*"; fi; }

CPU_OUT="$TEST_ROOT/cpu-only"
bash "$GENERATOR" \
    --output-dir "$CPU_OUT" \
    --control-image ghcr.io/example/catmonitor:fixture \
    --cpu-image ghcr.io/example/cpu:fixture \
    --hpl-processes 8 --hpl-threads 12 \
    --hpcg-processes 96 --hpcg-threads 1 \
    --enable-web

for file in catmonitor-stress.yaml stress-profile.json docker-compose.stress.generated.yml stress-deployment-manifest.json; do
    test -f "$CPU_OUT/$file" || fail "missing CPU-only output: $file"
done
contains "$CPU_OUT/catmonitor-stress.yaml" 'type: docker_exec'
contains "$CPU_OUT/catmonitor-stress.yaml" 'container: catmonitor-stress-cpu'
contains "$CPU_OUT/catmonitor-stress.yaml" 'npu_burn: { enabled: false'
contains "$CPU_OUT/docker-compose.stress.generated.yml" "image: 'ghcr.io/example/cpu:fixture'"
not_contains "$CPU_OUT/docker-compose.stress.generated.yml" 'devices:'
contains "$CPU_OUT/stress-profile.json" '"cpu":{"enabled":true'
contains "$CPU_OUT/stress-profile.json" '"daemon_docker_socket":true'
contains "$CPU_OUT/stress-profile.json" '"web_docker_socket":false'
expect_fail bash "$GENERATOR" --output-dir "$TEST_ROOT/no-profile"
expect_fail bash "$GENERATOR" --output-dir "$CPU_OUT" --cpu-image ghcr.io/example/cpu:fixture
bash "$GENERATOR" --output-dir "$CPU_OUT" --cpu-image ghcr.io/example/cpu:fixture --force >/dev/null

HOST="$TEST_ROOT/host"
mkdir -p \
    "$HOST/dev" \
    "$HOST/usr/local/Ascend/driver/lib64" \
    "$HOST/usr/local/Ascend/driver" \
    "$HOST/usr/local/dcmi" \
    "$HOST/usr/local/bin" \
    "$HOST/etc"
touch \
    "$HOST/dev/davinci2" "$HOST/dev/davinci5" \
    "$HOST/dev/davinci_manager" "$HOST/dev/devmm_svm" "$HOST/dev/hisi_hdc" \
    "$HOST/usr/local/Ascend/driver/version.info" \
    "$HOST/etc/ascend_install.info" "$HOST/usr/local/bin/npu-smi"
printf '{"schema_version":1}\n' >"$TEST_ROOT/cpu-manifest.json"
printf '{"schema_version":1}\n' >"$TEST_ROOT/npu-manifest.json"

NPU_OUT="$TEST_ROOT/npu-only"
bash "$GENERATOR" \
    --output-dir "$NPU_OUT" \
    --npu-image ghcr.io/example/npu:fixture \
    --npu-manifest "$TEST_ROOT/npu-manifest.json" \
    --npu-host-root "$HOST" \
    --npu-device-nodes 5,2 \
    --npu-burn-device 0,1 \
    --npu-chip-generation A2 \
    --npu-run-case matmul

contains "$NPU_OUT/catmonitor-stress.yaml" 'default_benchmarks: [npu_burn]'
contains "$NPU_OUT/catmonitor-stress.yaml" 'stream: { enabled: false'
contains "$NPU_OUT/catmonitor-stress.yaml" 'hpl: { enabled: false'
contains "$NPU_OUT/catmonitor-stress.yaml" 'hpcg: { enabled: false'
contains "$NPU_OUT/catmonitor-stress.yaml" 'npu_burn: { enabled: true'
not_contains "$NPU_OUT/docker-compose.stress.generated.yml" 'catmonitor-stress-cpu:'
contains "$NPU_OUT/docker-compose.stress.generated.yml" 'catmonitor-stress-npu:'
not_contains "$NPU_OUT/docker-compose.stress.generated.yml" 'privileged: true'
not_contains "$NPU_OUT/docker-compose.stress.generated.yml" 'network_mode: host'
not_contains "$NPU_OUT/docker-compose.stress.generated.yml" 'read_only: false'
contains "$NPU_OUT/stress-profile.json" '"cpu":{"enabled":false,"image":null'

FULL_OUT="$TEST_ROOT/full"
bash "$GENERATOR" \
    --output-dir "$FULL_OUT" \
    --cpu-image ghcr.io/example/cpu:fixture \
    --npu-image ghcr.io/example/npu:fixture \
    --cpu-manifest "$TEST_ROOT/cpu-manifest.json" \
    --npu-manifest "$TEST_ROOT/npu-manifest.json" \
    --npu-host-root "$HOST" \
    --npu-device-nodes 5,2 \
    --npu-burn-device 0,1 \
    --npu-chip-generation A2 \
    --npu-run-case matmul

contains "$FULL_OUT/catmonitor-stress.yaml" 'npu_burn: { enabled: true'
contains "$FULL_OUT/docker-compose.stress.generated.yml" 'CATMONITOR_NPU_DEVICE_COUNT:'
contains "$FULL_OUT/docker-compose.stress.generated.yml" "CATMONITOR_NPU_DEVICE_COUNT: '2'"
contains "$FULL_OUT/docker-compose.stress.generated.yml" '/dev/davinci2:/dev/davinci2'
contains "$FULL_OUT/docker-compose.stress.generated.yml" '/dev/davinci5:/dev/davinci5'
contains "$FULL_OUT/docker-compose.stress.generated.yml" "NPU_BURN_DEVICE: '0,1'"
contains "$FULL_OUT/stress-profile.json" '"host_device_ids":[2,5]'
contains "$FULL_OUT/stress-profile.json" '"burn_logical_ids":"0,1"'
contains "$FULL_OUT/stress-deployment-manifest.json" '"cpu_manifest_sha256":"'
contains "$FULL_OUT/stress-deployment-manifest.json" '"npu_manifest_sha256":"'
not_contains "$FULL_OUT/docker-compose.stress.generated.yml" 'privileged: true'
not_contains "$FULL_OUT/docker-compose.stress.generated.yml" 'network_mode: host'
not_contains "$FULL_OUT/docker-compose.stress.generated.yml" 'read_only: false'

expect_fail bash "$GENERATOR" \
    --output-dir "$TEST_ROOT/bad" --cpu-image cpu:test --npu-image npu:test \
    --npu-host-root "$HOST" --npu-device-nodes 2,9 --npu-burn-device 0,1

printf 'PASS: daemon/controller Stress deployment generator\n'
