#!/usr/bin/env bash
set -euo pipefail
REPO_ROOT=$(cd "$(dirname "$0")/../../.." && pwd -P)
GO_BIN=${GO_BIN:-go}
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-npu-native-topology.XXXXXXXX")
trap 'rm -rf -- "$TEST_ROOT"' EXIT HUP INT TERM
fail(){ printf 'FAIL: %s\n' "$*" >&2; exit 1; }
install -d -m 0755 "$TEST_ROOT/bin" "$TEST_ROOT/dev" "$TEST_ROOT/output" "$TEST_ROOT/state"
touch "$TEST_ROOT/dev/davinci2" "$TEST_ROOT/dev/davinci5"
cat >"$TEST_ROOT/bin/npu-burn" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat >"$TEST_ROOT/bin/npu-preflight" <<'EOF'
#!/usr/bin/env bash
printf 'CATMONITOR_RUNTIME_DEVICE_COUNT=2\nCATMONITOR_RUNTIME_PREFLIGHT=PASS\n'
EOF
cat >"$TEST_ROOT/bin/lspci" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' '0000:01:00.0 Processing accelerators: Huawei Technologies Co., Ltd. Device d803'
if [ "${LSPCI_MODE:-two}" = two ]; then printf '%s\n' '0000:02:00.0 Processing accelerators: Huawei Technologies Co., Ltd. Device d803'; fi
EOF
chmod 0755 "$TEST_ROOT/bin/npu-burn" "$TEST_ROOT/bin/npu-preflight" "$TEST_ROOT/bin/lspci"
"$GO_BIN" build -trimpath -o "$TEST_ROOT/bin/catmonitor-stress-exec" "$REPO_ROOT/features/stress/cmd/workload-exec"

describe(){
  CATMONITOR_STRESS_BENCHMARKS=npu_burn CATMONITOR_STRESS_STATE_ROOT="$TEST_ROOT/state" CATMONITOR_LSPCI="$TEST_ROOT/bin/lspci" \
  NPU_BURN_EXECUTABLE="$TEST_ROOT/bin/npu-burn" NPU_BURN_PREFLIGHT_EXECUTABLE="$TEST_ROOT/bin/npu-preflight" \
  NPU_BURN_OUTPUT_DIR="$TEST_ROOT/output" NPU_BURN_RUN_CASE=matmul NPU_BURN_GROUP= \
  NPU_BURN_DEVICE=0,1 NPU_BURN_DEVICE_ROOT="$TEST_ROOT/dev" NPU_BURN_INTERNAL_TIMEOUT_SECONDS=300 NPU_BURN_CHIP_GENERATION=A2 \
  "$TEST_ROOT/bin/catmonitor-stress-exec" describe --benchmark npu_burn --json
}
describe >"$TEST_ROOT/profile.json"
grep -Fq '"key":"device_node_ids","label":"Visible /dev/davinci node IDs","value":"2,5"' "$TEST_ROOT/profile.json" || fail 'sparse IDs missing'
grep -Fq '"key":"available_devices","label":"Available logical devices","value":"0,1"' "$TEST_ROOT/profile.json" || fail 'logical IDs missing'
grep -Fq '"status":"pass"' "$TEST_ROOT/profile.json" || fail 'preflight did not pass'
LSPCI_MODE=one describe >"$TEST_ROOT/mismatch.json"
grep -Fq 'does not match NPU Burn lspci topology count' "$TEST_ROOT/mismatch.json" || fail 'mismatch not reported'
grep -Fq '"status":"fail"' "$TEST_ROOT/mismatch.json" || fail 'mismatch did not fail'
printf 'PASS: NPU plugin maps sparse /dev IDs to contiguous PCI logical IDs\n'
