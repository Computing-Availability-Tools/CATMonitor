#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
GO_BIN=${GO_BIN:-go}
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-stress-workload-e2e.XXXXXXXX")
cleanup() { if [ -n "${RUN_PID-}" ]; then kill "$RUN_PID" 2>/dev/null || true; fi; rm -rf -- "$TEST_ROOT"; }
trap cleanup EXIT HUP INT TERM

mkdir -p "$TEST_ROOT/bin" "$TEST_ROOT/state"
"$GO_BIN" build -trimpath -o "$TEST_ROOT/bin/catmonitor-stress-exec" "$REPO_ROOT/features/stress/cmd/workload-exec"
cat >"$TEST_ROOT/bin/stream" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [ -e "$CATMONITOR_E2E_BLOCK" ]; then exec sleep 30; fi
printf 'Copy: 1000\nScale: 900\nAdd: 800\nTriad: 700\n'
EOF
cat >"$TEST_ROOT/bin/numactl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[ "${1-}" = --interleave=all ]
shift
exec "$@"
EOF
chmod 0755 "$TEST_ROOT/bin/stream" "$TEST_ROOT/bin/numactl"

export CATMONITOR_STRESS_BENCHMARKS=stream
export CATMONITOR_STRESS_STATE_ROOT="$TEST_ROOT/state"
export CATMONITOR_E2E_BLOCK="$TEST_ROOT/block"
export STREAM_EXECUTABLE="$TEST_ROOT/bin/stream"
export STREAM_NUMACTL="$TEST_ROOT/bin/numactl"
export STREAM_THREADS=0

"$TEST_ROOT/bin/catmonitor-stress-exec" describe --benchmark stream --json >"$TEST_ROOT/describe.json"
grep -Fq '"benchmark":"stream"' "$TEST_ROOT/describe.json"
grep -Fq '"status":"pass"' "$TEST_ROOT/describe.json"

printf '%s\n' '{"protocol_version":1,"job_id":"aabb","benchmark":"stream","timeout_seconds":10,"options":{}}' |
    "$TEST_ROOT/bin/catmonitor-stress-exec" run --request - >"$TEST_ROOT/run.json"
grep -Fq '"status":"healthy"' "$TEST_ROOT/run.json"
grep -Fq 'Copy: 1000' "$TEST_ROOT/run.json"

touch "$CATMONITOR_E2E_BLOCK"
printf '%s\n' '{"protocol_version":1,"job_id":"ccdd","benchmark":"stream","timeout_seconds":20,"options":{}}' |
    "$TEST_ROOT/bin/catmonitor-stress-exec" run --request - >"$TEST_ROOT/cancelled.json" &
RUN_PID=$!
for _ in $(seq 1 100); do [ -f "$TEST_ROOT/state/workload-jobs/ccdd/pgid" ] && break; sleep 0.02; done
[ -f "$TEST_ROOT/state/workload-jobs/ccdd/pgid" ]
"$TEST_ROOT/bin/catmonitor-stress-exec" cancel --job-id ccdd >"$TEST_ROOT/cancel.json"
wait "$RUN_PID"
unset RUN_PID
grep -Fq '"accepted":true' "$TEST_ROOT/cancel.json"
grep -Fq '"status":"cancelled"' "$TEST_ROOT/cancelled.json"
test ! -e "$TEST_ROOT/state/workload-jobs/ccdd/pgid"
printf 'PASS: common typed workload plugin describe/run/cancel E2E\n'

# Hardware-free NPU protocol integration: real workload executor, synthetic
# topology and CSV producer. This is NOT evidence of a real NPU workload.
mkdir -p "$TEST_ROOT/dev" "$TEST_ROOT/output" "$TEST_ROOT/log" "$TEST_ROOT/home"
touch "$TEST_ROOT/dev/davinci2" "$TEST_ROOT/dev/davinci5"
cat >"$TEST_ROOT/bin/lspci" <<'EOF'
#!/bin/sh
printf '%s\n' '0000:01:00.0 Processing accelerators: Device d802' '0000:81:00.0 Processing accelerators: Device d802'
EOF
cat >"$TEST_ROOT/bin/preflight" <<'EOF'
#!/bin/sh
if [ "${1-}" = --cases-json ]; then
  printf '%s\n' '{"cases":[{"name":"matmul","selectable":true},{"name":"conv2d","selectable":true}],"selected":["matmul","conv2d"],"selection_error":"","configuration_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}'
fi
exit 0
EOF
cat >"$TEST_ROOT/bin/npu-smi" <<'EOF'
#!/bin/sh
printf '%s\n' '| 2 910B4 | OK |' '| 5 910B4 | OK |' '| NPU Chip | Process id | Process name |' \
 '| No running processes found in NPU 2 |' '| No running processes found in NPU 5 |'
EOF
cat >"$TEST_ROOT/bin/npu-burn" <<'EOF'
#!/bin/bash
set -euo pipefail
[ "${1-}" = --device ]
[ "$2" = "$NPU_BURN_DEVICE" ]
[ "$CATMONITOR_NPU_JOB_TIMEOUT_SECONDS" = 10 ]
printf 'task,device_id,case_idx,run_count,stream_count,exetime,err_count,result,case_config\n' >"$NPU_BURN_OUTPUT_DIR/npu_burn_results.csv"
IFS=, read -r -a ids <<<"$2"
for id in "${ids[@]}"; do
  if [ "${CATMONITOR_E2E_MISSING_DEVICE:-0}" = 1 ] && [ "$id" = 1 ]; then continue; fi
  for op in matmul conv2d; do
    if [ "${CATMONITOR_E2E_MISSING_CASE:-0}" = 1 ] && [ "$op" = conv2d ]; then continue; fi
    printf '%s,%s,0,20,1,1,0,PASS,fixture\n' "$op" "$id" >>"$NPU_BURN_OUTPUT_DIR/npu_burn_results.csv"
  done
done
EOF
chmod 0755 "$TEST_ROOT/bin/lspci" "$TEST_ROOT/bin/preflight" "$TEST_ROOT/bin/npu-smi" "$TEST_ROOT/bin/npu-burn"
export CATMONITOR_STRESS_BENCHMARKS=npu_burn
export CATMONITOR_LSPCI="$TEST_ROOT/bin/lspci" NPU_SMI_EXECUTABLE="$TEST_ROOT/bin/npu-smi"
export NPU_BURN_PREFLIGHT_EXECUTABLE="$TEST_ROOT/bin/preflight" NPU_BURN_EXECUTABLE="$TEST_ROOT/bin/npu-burn"
export NPU_BURN_DEVICE_ROOT="$TEST_ROOT/dev" NPU_BURN_OUTPUT_DIR="$TEST_ROOT/output" NPU_BURN_LOG_DIR="$TEST_ROOT/log" HOME="$TEST_ROOT/home"
export NPU_BURN_DEVICE=0,1 CATMONITOR_NPU_DEVICE_MODE=full CATMONITOR_NPU_REQUIRE_IDLE=1
export NPU_BURN_INTERNAL_TIMEOUT_SECONDS=2 NPU_BURN_CHIP_GENERATION=A2 NPU_BURN_RUN_CASE=matmul,conv2d NPU_BURN_GROUP=
export CATMONITOR_NPU_COMPAT_PROFILE=a2-portable-runtime-v1
if ! "$TEST_ROOT/bin/catmonitor-stress-exec" describe --benchmark npu_burn --json --require-pass >"$TEST_ROOT/npu-describe.json"; then
    cat "$TEST_ROOT/npu-describe.json" >&2
    exit 1
fi
printf '%s\n' '{"protocol_version":1,"job_id":"eeff","benchmark":"npu_burn","timeout_seconds":10,"npu_devices":[0,1],"npu_cases":["matmul","conv2d"],"options":{}}' |
    "$TEST_ROOT/bin/catmonitor-stress-exec" run --request - >"$TEST_ROOT/npu-run.json"
python3 - "$TEST_ROOT/npu-run.json" <<'PY'
import json,sys
r=json.load(open(sys.argv[1]))
assert r['status']=='healthy' and r['npu_devices']==[0,1]
assert r['npu_cases']==['matmul','conv2d']
assert r['values']['errors']==0
PY
export CATMONITOR_E2E_MISSING_DEVICE=1
printf '%s\n' '{"protocol_version":1,"job_id":"eeee","benchmark":"npu_burn","timeout_seconds":10,"npu_devices":[0,1],"options":{}}' |
    "$TEST_ROOT/bin/catmonitor-stress-exec" run --request - >"$TEST_ROOT/npu-incomplete.json"
python3 - "$TEST_ROOT/npu-incomplete.json" <<'PY'
import json,sys
r=json.load(open(sys.argv[1]))
assert r['status']=='unhealthy' and 'npu_devices' not in r
PY
printf 'PASS: NPU selection/fresh CSV coverage protocol E2E (synthetic)\n'
unset CATMONITOR_E2E_MISSING_DEVICE
export CATMONITOR_E2E_MISSING_CASE=1
printf '%s\n' '{"protocol_version":1,"job_id":"eeaa","benchmark":"npu_burn","timeout_seconds":10,"npu_devices":[0,1],"npu_cases":["matmul","conv2d"],"options":{}}' |
    "$TEST_ROOT/bin/catmonitor-stress-exec" run --request - >"$TEST_ROOT/npu-incomplete-case.json"
python3 - "$TEST_ROOT/npu-incomplete-case.json" <<'PY'
import json,sys
r=json.load(open(sys.argv[1]))
assert r['status']=='unhealthy' and 'npu_cases' not in r
PY
printf 'PASS: NPU operator coverage protocol E2E (synthetic)\n'
