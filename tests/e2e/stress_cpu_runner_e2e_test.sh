#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/../.." && pwd -P)
GO_BIN=${GO_BIN:-go}
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-cpu-runner-e2e.XXXXXXXX")
RUNNER_PID=

cleanup() {
    if [ -n "$RUNNER_PID" ]; then
        kill "$RUNNER_PID" >/dev/null 2>&1 || true
        wait "$RUNNER_PID" >/dev/null 2>&1 || true
    fi
    case "$TEST_ROOT" in "${TMPDIR:-/tmp}"/catmonitor-cpu-runner-e2e.*) rm -rf -- "$TEST_ROOT" ;; esac
}
trap cleanup EXIT HUP INT TERM

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

mkdir -p "$TEST_ROOT/bin" "$TEST_ROOT/socket"
"$GO_BIN" build -trimpath -o "$TEST_ROOT/bin/cpu-runner" \
    "$REPO_ROOT/features/stress/cmd/cpu-runner"
"$GO_BIN" build -trimpath -o "$TEST_ROOT/bin/cpu-client" \
    "$REPO_ROOT/features/stress/cmd/cpu-runner-client"

cat >"$TEST_ROOT/bin/stream_omp" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' \
  'Copy: 101000.0 0 0 0' \
  'Scale: 102000.0 0 0 0' \
  'Add: 103000.0 0 0 0' \
  'Triad: 104000.0 0 0 0' \
  'Solution Validates: avg error less than epsilon on all three arrays'
EOF
cat >"$TEST_ROOT/bin/numactl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[ "${1-}" = --interleave=all ]
shift
exec "$@"
EOF
chmod 0755 "$TEST_ROOT/bin/stream_omp" "$TEST_ROOT/bin/numactl"

cp "$REPO_ROOT/features/stress/benchmark_check.sh" "$TEST_ROOT/runner-adapter.sh"
sed -i \
    -e 's|^CPU_EXECUTION_PROFILE=.*|CPU_EXECUTION_PROFILE="container_runner"|' \
    -e 's|^CPU_EXECUTION_IMAGE=.*|CPU_EXECUTION_IMAGE="catmonitor/stress-cpu:e2e"|' \
    -e "s|^STREAM_EXECUTABLE=.*|STREAM_EXECUTABLE=\"$TEST_ROOT/bin/stream_omp\"|" \
    -e "s|^STREAM_NUMACTL=.*|STREAM_NUMACTL=\"$TEST_ROOT/bin/numactl\"|" \
    "$TEST_ROOT/runner-adapter.sh"
chmod 0755 "$TEST_ROOT/runner-adapter.sh"

SOCKET="$TEST_ROOT/socket/cpu-runner.sock"
"$TEST_ROOT/bin/cpu-runner" \
    -socket "$SOCKET" \
    -adapter "$TEST_ROOT/runner-adapter.sh" \
    >"$TEST_ROOT/runner.log" 2>&1 &
RUNNER_PID=$!
for _ in $(seq 1 100); do
    [ -S "$SOCKET" ] && break
    sleep 0.05
done
[ -S "$SOCKET" ] || { cat "$TEST_ROOT/runner.log" >&2; fail 'runner socket did not become ready'; }

cp "$REPO_ROOT/features/stress/benchmark_check.sh" "$TEST_ROOT/control-adapter.sh"
sed -i \
    -e 's|^CPU_EXECUTION_BACKEND=.*|CPU_EXECUTION_BACKEND="unix"|' \
    -e "s|^CPU_RUNNER_CLIENT=.*|CPU_RUNNER_CLIENT=\"$TEST_ROOT/bin/cpu-client\"|" \
    -e "s|^CPU_RUNNER_SOCKET=.*|CPU_RUNNER_SOCKET=\"$SOCKET\"|" \
    "$TEST_ROOT/control-adapter.sh"
chmod 0755 "$TEST_ROOT/control-adapter.sh"

"$TEST_ROOT/control-adapter.sh" describe stream >"$TEST_ROOT/profile.json"
grep -Fq '"benchmark":"stream"' "$TEST_ROOT/profile.json" || fail 'proxied profile is missing'
grep -Fq '"key":"execution_backend"' "$TEST_ROOT/profile.json" || fail 'profile backend parameter is missing'
grep -Fq '"value":"container_runner"' "$TEST_ROOT/profile.json" || fail 'runner backend identity is wrong'
grep -Fq '"value":"catmonitor/stress-cpu:e2e"' "$TEST_ROOT/profile.json" || fail 'runner image identity is missing'

"$TEST_ROOT/control-adapter.sh" stream >"$TEST_ROOT/stream.log"
grep -Fq 'Triad: 104000.0' "$TEST_ROOT/stream.log" || fail 'proxied STREAM output is missing'

if "$TEST_ROOT/bin/cpu-client" -socket "$SOCKET" run npu_burn >"$TEST_ROOT/npu.log" 2>&1; then
    fail 'client accepted NPU Burn through the CPU runner'
fi
grep -Fq 'unsupported CPU benchmark' "$TEST_ROOT/npu.log" || fail 'client rejection is unclear'

printf 'PASS: control adapter -> Unix client -> CPU runner -> runner adapter -> STREAM\n'
