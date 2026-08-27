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
