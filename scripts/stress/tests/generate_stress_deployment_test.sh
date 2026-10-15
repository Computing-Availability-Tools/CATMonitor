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
contains "$CPU_OUT/catmonitor-stress.yaml" 'collectors:'
contains "$CPU_OUT/catmonitor-stress.yaml" 'features: [web, dfee, health]'
contains "$CPU_OUT/catmonitor-stress.yaml" 'snapshot:'
contains "$CPU_OUT/catmonitor-stress.yaml" '  enabled: true'
contains "$CPU_OUT/catmonitor-stress.yaml" '  dir: /var/lib/catmonitor/snapshot'
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
contains "$CPU_OUT/catmonitor-stress.yaml" 'web_enabled: true'
not_contains "$CPU_OUT/catmonitor-stress.yaml" 'allowed_npu_models:'
not_contains "$CPU_OUT/catmonitor-stress.yaml" 'allow_npu_reference_generation:'

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
contains "$NPU_OUT/catmonitor-stress.yaml" 'container: catmonitor-stress-npu, timeout: 2h'
not_contains "$NPU_OUT/docker-compose.stress.generated.yml" 'catmonitor-stress-cpu:'
contains "$NPU_OUT/docker-compose.stress.generated.yml" 'catmonitor-stress-npu:'
contains "$NPU_OUT/docker-compose.stress.generated.yml" 'privileged: true'
contains "$NPU_OUT/docker-compose.stress.generated.yml" "ASCEND_RT_VISIBLE_DEVICES: '0,1'"
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
contains "$FULL_OUT/docker-compose.stress.generated.yml" "ASCEND_RT_VISIBLE_DEVICES: '0,1'"
contains "$FULL_OUT/docker-compose.stress.generated.yml" '/dev/davinci2:/dev/davinci2'
contains "$FULL_OUT/docker-compose.stress.generated.yml" '/dev/davinci5:/dev/davinci5'
contains "$FULL_OUT/docker-compose.stress.generated.yml" "NPU_BURN_DEVICE: '0,1'"
contains "$FULL_OUT/stress-profile.json" '"host_device_ids":[2,5]'
contains "$FULL_OUT/stress-profile.json" '"burn_logical_ids":"0,1"'
contains "$FULL_OUT/stress-deployment-manifest.json" '"cpu_manifest_sha256":"'
contains "$FULL_OUT/stress-deployment-manifest.json" '"npu_manifest_sha256":"'
contains "$FULL_OUT/docker-compose.stress.generated.yml" 'privileged: true'
not_contains "$FULL_OUT/docker-compose.stress.generated.yml" 'network_mode: host'
not_contains "$FULL_OUT/docker-compose.stress.generated.yml" 'read_only: false'

expect_fail bash "$GENERATOR" \
    --output-dir "$TEST_ROOT/bad" --cpu-image cpu:test --npu-image npu:test \
    --npu-host-root "$HOST" --npu-device-nodes 2,9 --npu-burn-device 0,1

SINGLE_OUT="$TEST_ROOT/single"
bash "$GENERATOR" --output-dir "$SINGLE_OUT" --npu-image npu:portable \
    --npu-host-root "$HOST" --npu-device-nodes 5 --npu-burn-device 0 \
    --npu-device-mode single --npu-chip-generation A3 --npu-threads 2 \
    --npu-ascend-env-script /usr/local/Ascend/cann-example/set_env.sh
contains "$SINGLE_OUT/docker-compose.stress.generated.yml" 'privileged: false'
contains "$SINGLE_OUT/docker-compose.stress.generated.yml" 'cap_drop: [ALL]'
contains "$SINGLE_OUT/npu-workload.env" 'CATMONITOR_NPU_DEVICE_MODE=single'
contains "$SINGLE_OUT/npu-workload.env" 'OMP_NUM_THREADS=2'
contains "$SINGLE_OUT/npu-workload.env" 'ASCEND_ENV_SCRIPT=/usr/local/Ascend/cann-example/set_env.sh'
contains "$SINGLE_OUT/stress-deployment-manifest.json" '"npu_env_sha256":"'
contains "$SINGLE_OUT/docker-compose.stress.generated.yml" "CATMONITOR_NPU_DEVICE_MODE: 'single'"
contains "$SINGLE_OUT/docker-compose.stress.generated.yml" "OPENBLAS_NUM_THREADS: '2'"
contains "$SINGLE_OUT/docker-compose.stress.generated.yml" "ASCEND_ENV_SCRIPT: '/usr/local/Ascend/cann-example/set_env.sh'"
not_contains "$SINGLE_OUT/docker-compose.stress.generated.yml" '/dev/davinci2'
expect_fail bash "$GENERATOR" --output-dir "$TEST_ROOT/single-bad" --npu-image npu:portable \
    --npu-host-root "$HOST" --npu-device-nodes 2,5 --npu-burn-device 0 --npu-device-mode single
printf 'PASS: daemon/controller Stress deployment generator\n'

CUSTOM="$TEST_ROOT/custom"
bash "$GENERATOR" --output-dir "$CUSTOM" --cpu-image cpu:test --npu-image npu:test \
    --cpu-container site-cpu --npu-container site-npu --hpl-profile small \
    --npu-host-root "$HOST" --npu-device-nodes 2,5 --npu-burn-device 0 \
    --npu-allowed-devices 0,1 --npu-security-profile restricted --npu-chip-generation A2
contains "$CUSTOM/catmonitor-stress.yaml" 'container: site-cpu'
contains "$CUSTOM/catmonitor-stress.yaml" 'container: site-npu'
contains "$CUSTOM/catmonitor-stress.yaml" 'allowed_npu_devices: [0,1]'
contains "$CUSTOM/docker-compose.stress.generated.yml" 'container_name: site-npu'
contains "$CUSTOM/docker-compose.stress.generated.yml" 'privileged: false'
contains "$CUSTOM/cpu-workload.env" 'HPL_N=4096'
contains "$CUSTOM/cpu-workload.env" 'HPL_Q=1'
contains "$CUSTOM/npu-workload.env" 'CATMONITOR_NPU_REQUIRE_IDLE=0'
contains "$CUSTOM/docker-compose.stress.generated.yml" "CATMONITOR_NPU_REQUIRE_IDLE: '0'"
contains "$CUSTOM/stress-profile.json" '"container":"site-npu"'
contains "$CUSTOM/stress-deployment-manifest.json" '"cpu_env_sha256":"'
expect_fail bash "$GENERATOR" --output-dir "$TEST_ROOT/invalid-grid" --cpu-image cpu:test --hpl-profile small --hpl-processes 2
expect_fail bash "$GENERATOR" --output-dir "$TEST_ROOT/partial-size" --cpu-image cpu:test --hpl-n 4096
expect_fail bash "$GENERATOR" --output-dir "$TEST_ROOT/invalid-name" --cpu-image cpu:test --cpu-container 'cpu;bad'
expect_fail bash "$GENERATOR" --output-dir "$TEST_ROOT/unauthorized" --npu-image npu:test \
    --npu-host-root "$HOST" --npu-burn-device 1 --npu-allowed-devices 0
expect_fail bash "$GENERATOR" --output-dir "$TEST_ROOT/duplicate-allow" --npu-image npu:test \
    --npu-host-root "$HOST" --npu-burn-device 0 --npu-allowed-devices 0,0
printf 'PASS: per-job allowlist, restricted profile, custom containers and HPL dimensions\n'

MODEL="$TEST_ROOT/models"
bash "$GENERATOR" --output-dir "$MODEL" --npu-image npu:models \
    --npu-host-root "$HOST" --npu-burn-device 0 --npu-chip-generation A3 \
    --npu-allowed-models gpt3,llama2 --allow-npu-reference-generation \
    --npu-model-memory-mode checkpointed --npu-job-timeout 12h
contains "$MODEL/npu-workload.env" 'CATMONITOR_MODEL_MEMORY_MODE=checkpointed'
contains "$MODEL/docker-compose.stress.generated.yml" "CATMONITOR_MODEL_MEMORY_MODE: 'checkpointed'"
contains "$MODEL/catmonitor-stress.yaml" 'timeout: 12h'
contains "$MODEL/catmonitor-stress.yaml" 'allowed_npu_models: [gpt3,llama2]'
expect_fail bash "$GENERATOR" --output-dir "$TEST_ROOT/bad-mode" --cpu-image cpu:test --npu-model-memory-mode auto
expect_fail bash "$GENERATOR" --output-dir "$TEST_ROOT/bad-time" --cpu-image cpu:test --npu-job-timeout 0h
printf 'PASS: explicit model memory policy, authorization and job time limit\n'

EXTERNAL_CONFIG="$TEST_ROOT/external config"
python3 "$REPO_ROOT/scripts/stress/npu_config_profile.py" \
    --source-dir "$REPO_ROOT/third_party/ascend_npu_burn/source/ascend_npu_burn/config" \
    --output-dir "$EXTERNAL_CONFIG" \
    --preset "$REPO_ROOT/configs/stress/npu-model/a2-model-b32-checkpointed-36steps.json" >/dev/null
EXTERNAL="$TEST_ROOT/external-deployment"
bash "$GENERATOR" --output-dir "$EXTERNAL" --npu-image npu:models \
    --npu-host-root "$HOST" --npu-burn-device 0 --npu-chip-generation A2 \
    --npu-config-dir "$EXTERNAL_CONFIG" --npu-model-memory-mode checkpointed --npu-security-profile restricted
contains "$EXTERNAL/docker-compose.stress.generated.yml" "source: '$EXTERNAL_CONFIG'"
contains "$EXTERNAL/docker-compose.stress.generated.yml" 'target: /opt/catmonitor/npuburn-python/ascend_npu_burn/config'
contains "$EXTERNAL/docker-compose.stress.generated.yml" 'read_only: true'
contains "$EXTERNAL/docker-compose.stress.generated.yml" "CATMONITOR_MODEL_MEMORY_MODE: 'checkpointed'"
contains "$EXTERNAL/docker-compose.stress.generated.yml" 'privileged: false'
contains "$EXTERNAL/stress-deployment-manifest.json" '"npu_config_files_sha256":{'
contains "$EXTERNAL/stress-deployment-manifest.json" "$(sha256sum "$EXTERNAL_CONFIG/base_config.json" | awk '{print $1}')"
not_contains "$MODEL/docker-compose.stress.generated.yml" 'target: /opt/catmonitor/npuburn-python/ascend_npu_burn/config'
expect_fail bash "$GENERATOR" --output-dir "$TEST_ROOT/cpu-config" --cpu-image cpu:test --npu-config-dir "$EXTERNAL_CONFIG"
expect_fail bash "$GENERATOR" --output-dir "$TEST_ROOT/missing-config" --npu-image npu:test \
    --npu-host-root "$HOST" --npu-burn-device 0 --npu-chip-generation A2 --npu-config-dir "$TEST_ROOT/missing"
printf 'PASS: optional external NPU config mount and content identity\n'

# Complete capabilities are the generator defaults, not automatic execution or
# permission to widen a restricted/single-device container's mapping.
DEFAULTS="$TEST_ROOT/default-capabilities"
bash "$GENERATOR" --output-dir "$DEFAULTS" --npu-image npu:models \
    --npu-host-root "$HOST" --npu-chip-generation A2
contains "$DEFAULTS/catmonitor-stress.yaml" 'web_enabled: true'
contains "$DEFAULTS/catmonitor-stress.yaml" 'allowed_npu_devices: [0,1]'
contains "$DEFAULTS/catmonitor-stress.yaml" 'allowed_npu_models: [gpt3,llama2]'
contains "$DEFAULTS/catmonitor-stress.yaml" 'allow_npu_reference_generation: true'
contains "$DEFAULTS/npu-workload.env" 'NPU_BURN_DEVICE=0'
contains "$DEFAULTS/npu-workload.env" 'NPU_BURN_RUN_CASE=matmul'
contains "$DEFAULTS/npu-workload.env" 'ASCEND_RT_VISIBLE_DEVICES=0,1'
contains "$DEFAULTS/npu-workload.env" 'NPU_BURN_INTERNAL_TIMEOUT_SECONDS=300'
contains "$DEFAULTS/docker-compose.stress.generated.yml" 'privileged: true'
not_contains "$DEFAULTS/docker-compose.stress.generated.yml" 'read_only: false'

RESTRICTED="$TEST_ROOT/admin-restrictions"
bash "$GENERATOR" --output-dir "$RESTRICTED" --npu-image npu:models \
    --npu-host-root "$HOST" --npu-chip-generation A2 \
    --npu-allowed-devices 0 --npu-allowed-models gpt3 \
    --deny-npu-reference-generation --disable-web --npu-security-profile restricted
contains "$RESTRICTED/catmonitor-stress.yaml" 'web_enabled: false'
contains "$RESTRICTED/catmonitor-stress.yaml" 'allowed_npu_devices: [0]'
contains "$RESTRICTED/catmonitor-stress.yaml" 'allowed_npu_models: [gpt3]'
contains "$RESTRICTED/catmonitor-stress.yaml" 'allow_npu_reference_generation: false'
contains "$RESTRICTED/docker-compose.stress.generated.yml" 'privileged: false'

NO_MODELS="$TEST_ROOT/no-models"
bash "$GENERATOR" --output-dir "$NO_MODELS" --npu-image npu:operators \
    --npu-host-root "$HOST" --npu-chip-generation A2 --npu-allowed-models none
not_contains "$NO_MODELS/catmonitor-stress.yaml" 'allowed_npu_models:'
not_contains "$NO_MODELS/catmonitor-stress.yaml" 'allow_npu_reference_generation: true'
expect_fail bash "$GENERATOR" --output-dir "$TEST_ROOT/conflicting-reference" \
    --npu-image npu:models --npu-host-root "$HOST" --npu-chip-generation A2 \
    --npu-allowed-models none --allow-npu-reference-generation
expect_fail bash "$GENERATOR" --output-dir "$TEST_ROOT/cpu-model-authorization" \
    --cpu-image cpu:test --npu-allowed-models gpt3
contains "$SINGLE_OUT/catmonitor-stress.yaml" 'allowed_npu_devices: [0]'
contains "$SINGLE_OUT/npu-workload.env" 'ASCEND_RT_VISIBLE_DEVICES=0'

WEB_OFF="$TEST_ROOT/cpu-web-off"
bash "$GENERATOR" --output-dir "$WEB_OFF" --cpu-image cpu:test --disable-web
contains "$WEB_OFF/catmonitor-stress.yaml" 'web_enabled: false'
bash "$GENERATOR" --output-dir "$WEB_OFF" --cpu-image cpu:test --disable-web --enable-web --force
contains "$WEB_OFF/catmonitor-stress.yaml" 'web_enabled: true'
printf 'PASS: complete defaults, explicit admin restrictions and unchanged device mappings\n'
