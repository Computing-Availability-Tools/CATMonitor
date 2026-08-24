#!/usr/bin/env bash
set -euo pipefail

# Hermetic CATMonitor stress product-chain E2E.
#
# This test builds the real CLI and Web binaries, but uses a generated host
# adapter instead of consuming CPU/MPI/NPU resources. Hardware performance and
# Ascend workload validation are separate, explicit node acceptance gates.

if [ "$(uname -s)" != Linux ]; then
    echo "SKIP: stress E2E requires Linux"
    exit 0
fi

GO_BIN=${GO_BIN:-go}
command -v "$GO_BIN" >/dev/null 2>&1 || {
    echo "FAIL: Go toolchain is unavailable: $GO_BIN" >&2
    exit 1
}
command -v curl >/dev/null 2>&1 || {
    echo "FAIL: curl is required by the stress HTTP E2E" >&2
    exit 1
}
command -v python3 >/dev/null 2>&1 || {
    echo "FAIL: python3 is required for JSON assertions and an ephemeral port" >&2
    exit 1
}

REPO_ROOT=$(cd "$(dirname "$0")/../.." && pwd)
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-stress-e2e.XXXXXX")
WEB_PID=""

cleanup() {
    if [ -n "$WEB_PID" ] && kill -0 "$WEB_PID" 2>/dev/null; then
        kill "$WEB_PID" 2>/dev/null || true
        wait "$WEB_PID" 2>/dev/null || true
    fi
    if [ "${CATMONITOR_E2E_KEEP_TMP:-0}" = 1 ]; then
        echo "INFO: retained stress E2E workspace: $TEST_ROOT"
    else
        rm -rf -- "$TEST_ROOT"
    fi
}
trap cleanup EXIT INT TERM

fail() {
    echo "FAIL: $*" >&2
    if [ -f "$TEST_ROOT/web.log" ]; then
        echo "--- catmonitor-web log ---" >&2
        tail -n 80 "$TEST_ROOT/web.log" >&2 || true
    fi
    exit 1
}

assert_json() {
    local path=$1
    local expression=$2
    local message=$3
    python3 - "$path" "$expression" "$message" <<'PY'
import json
import sys

path, expression, message = sys.argv[1:]
with open(path, encoding="utf-8") as stream:
    value = json.load(stream)
safe_globals = {
    "__builtins__": {},
    "all": all,
    "any": any,
    "len": len,
    "next": next,
    "value": value,
}
if not eval(expression, safe_globals, {}):
    raise SystemExit("FAIL: " + message)
PY
}

mkdir -p "$TEST_ROOT/bin" "$TEST_ROOT/hpcg" "$TEST_ROOT/snapshot"

(
    cd "$REPO_ROOT"
    "$GO_BIN" build -buildvcs=false -trimpath -o "$TEST_ROOT/bin/catmonitor" ./cmd/catmonitor
    "$GO_BIN" build -buildvcs=false -trimpath -o "$TEST_ROOT/bin/catmonitor-web" ./features/web
)

ADAPTER="$TEST_ROOT/benchmark_check.sh"
cat >"$ADAPTER" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
# CATMONITOR_STRESS_DESCRIBE_PROTOCOL=1

benchmark=${1-}
if [ "$benchmark" = describe ]; then
    [ "$#" -eq 2 ]
    benchmark=$2
    case "$benchmark" in
        stream|hpl|hpcg|npu_burn) ;;
        *) exit 1 ;;
    esac
    printf '{"protocol_version":1,"benchmark":"%s","parameters":[{"key":"fixture","label":"Fixture","value":"hermetic"}],"resources":{"mpi_processes":0,"threads_per_process":0,"total_workers":0,"runtime_seconds":1},"assets":[{"name":"fixture_adapter","path":"/bin/true","kind":"executable","required":true,"status":"pass","message":"hermetic E2E adapter"}],"mpi":{"required":false,"implementation":"none","executable_abi":"none","status":"pass","message":"not required by hermetic E2E"},"preflight":{"status":"pass","message":"hermetic E2E preflight passed"}}\n' "$benchmark"
    exit 0
fi

[ "$#" -eq 1 ]
case "$benchmark" in
    stream)
        sleep "${CATMONITOR_E2E_STREAM_DELAY_SECONDS:-0}"
        printf 'Copy: 101.10\nScale: 102.20\nAdd: 103.30\nTriad: 104.40\n'
        ;;
    hpl)
        printf 'T/V N NB P Q Time Gflops\n'
        printf 'WR00R2R4 20000 128 2 2 1.25 100.50\n'
        printf '1 tests completed and passed residual checks,\n'
        printf '0 tests completed and failed residual checks,\n'
        ;;
    hpcg)
        : "${CATMONITOR_E2E_HPCG_DIR:?CATMONITOR_E2E_HPCG_DIR is required}"
        result="$CATMONITOR_E2E_HPCG_DIR/HPCG-Benchmark_3.1_$(date +%s%N).txt"
        printf 'Final Summary::HPCG result is VALID with a GFLOP/s rating of=12.50\n' >"$result"
        printf 'Final Summary::Results are valid but execution time (sec) is=1.50\n' >>"$result"
        printf 'HPCG fixture completed\n'
        ;;
    npu_burn)
        printf 'CATMONITOR_NPU_BURN_SUMMARY devices=1 cases=2 passed=2 failed=0 errors=0 case_time_seconds=0.25\n'
        ;;
    *) exit 1 ;;
esac
EOF
chmod 0755 "$ADAPTER"

CONFIG="$TEST_ROOT/catmonitor.yaml"
cat >"$CONFIG" <<EOF
stress:
  enabled: true
  web_enabled: true
  script_path: $ADAPTER
  report_path: $TEST_ROOT/stress-latest.json
  default_benchmarks: [stream]
  benchmarks:
    stream: { enabled: true, timeout: 10s }
    hpl: { enabled: true, timeout: 10s }
    hpcg: { enabled: true, result_dir: $TEST_ROOT/hpcg, timeout: 10s }
    npu_burn: { enabled: true, timeout: 10s }
EOF

export CATMONITOR_E2E_HPCG_DIR="$TEST_ROOT/hpcg"

# First run the real CLI through all parsers and shared persistence.
"$TEST_ROOT/bin/catmonitor" stress \
    --bench stream,hpl,hpcg,npu_burn \
    --config "$CONFIG" \
    --output json >"$TEST_ROOT/cli-report.json"

assert_json "$TEST_ROOT/cli-report.json" \
    'value["status"] == "healthy" and value["initiator"] == "cli"' \
    'CLI report is not a healthy CLI-initiated job'
assert_json "$TEST_ROOT/cli-report.json" \
    'len(value["benchmarks"]) == 4 and all(item["status"] == "healthy" for item in value["benchmarks"])' \
    'CLI did not complete all four benchmark adapters'
assert_json "$TEST_ROOT/cli-report.json" \
    'next(item for item in value["benchmarks"] if item["name"] == "stream")["values"]["triad_mb_s"] == 104.4' \
    'STREAM metric was not parsed through the real CLI'
assert_json "$TEST_ROOT/cli-report.json" \
    'next(item for item in value["benchmarks"] if item["name"] == "hpl")["values"]["gflops"] == 100.5' \
    'HPL metric was not parsed through the real CLI'
assert_json "$TEST_ROOT/cli-report.json" \
    'next(item for item in value["benchmarks"] if item["name"] == "hpcg")["values"]["gflops"] == 12.5' \
    'HPCG result file was not parsed through the real CLI'
assert_json "$TEST_ROOT/cli-report.json" \
    'next(item for item in value["benchmarks"] if item["name"] == "npu_burn")["values"]["passed"] == 2' \
    'NPU Burn summary was not parsed through the real CLI'

# Start the real Web binary and verify it observes the CLI-owned shared report.
PORT=$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
)
BASE_URL="http://127.0.0.1:$PORT"
export CATMONITOR_E2E_STREAM_DELAY_SECONDS=2
"$TEST_ROOT/bin/catmonitor-web" \
    -addr "127.0.0.1:$PORT" \
    -snapshot-dir "$TEST_ROOT/snapshot" \
    -config "$CONFIG" >"$TEST_ROOT/web.log" 2>&1 &
WEB_PID=$!

for _ in $(seq 1 100); do
    if curl -fsS "$BASE_URL/api/stress/config" >"$TEST_ROOT/web-config.json" 2>/dev/null; then
        break
    fi
    sleep 0.05
done
kill -0 "$WEB_PID" 2>/dev/null || fail "catmonitor-web exited before readiness"
[ -s "$TEST_ROOT/web-config.json" ] || fail "stress config API did not become ready"

curl -fsS "$BASE_URL/stress/" >"$TEST_ROOT/stress-page.html"
grep -Fq 'RELIABILITY STRESS' "$TEST_ROOT/stress-page.html" || fail "standalone stress page was not served"
assert_json "$TEST_ROOT/web-config.json" \
    'value["enabled"] is True and len(value["benchmarks"]) == 4 and all(item["available"] for item in value["benchmarks"])' \
    'Web did not expose four available, enabled stress adapters'

curl -fsS "$BASE_URL/api/stress/latest" >"$TEST_ROOT/web-latest-cli.json"
assert_json "$TEST_ROOT/web-latest-cli.json" \
    'value["status"] == "healthy" and value["initiator"] == "cli" and len(value["benchmarks"]) == 4' \
    'Web did not read the report produced by the separate CLI process'

# Submit a real Web job, then prove that a second CLI process is rejected by
# the shared Linux lock while the Web-owned adapter is running.
curl -fsS -X POST "$BASE_URL/api/stress/runs" \
    -H 'Content-Type: application/json' \
    -H 'X-CATMonitor-Action: stress' \
    -H "Origin: $BASE_URL" \
    --data '{"benchmarks":["stream"],"timeout_seconds":5}' \
    >"$TEST_ROOT/web-start.json"
assert_json "$TEST_ROOT/web-start.json" \
    'value["status"] == "running" and value["initiator"] == "web" and len(value["benchmarks"]) == 1' \
    'Web did not accept a single STREAM job'

if "$TEST_ROOT/bin/catmonitor" stress --bench hpl --config "$CONFIG" \
    --output json >"$TEST_ROOT/busy-cli.out" 2>"$TEST_ROOT/busy-cli.err"; then
    fail "CLI started while the Web process owned the shared stress lock"
fi
grep -Fq 'already running' "$TEST_ROOT/busy-cli.err" || fail "CLI busy rejection was not explicit"

JOB_ID=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["job_id"])' "$TEST_ROOT/web-start.json")
for _ in $(seq 1 100); do
    curl -fsS "$BASE_URL/api/stress/runs/$JOB_ID" >"$TEST_ROOT/web-final.json"
    STATUS=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["status"])' "$TEST_ROOT/web-final.json")
    [ "$STATUS" = running ] || break
    sleep 0.05
done
[ "${STATUS:-}" = healthy ] || fail "Web STREAM job did not finish healthy"
assert_json "$TEST_ROOT/web-final.json" \
    'value["initiator"] == "web" and value["benchmarks"][0]["values"]["copy_mb_s"] == 101.1' \
    'Web job did not retain its initiator and parsed STREAM values'

curl -fsS "$BASE_URL/api/stress/history?limit=10" >"$TEST_ROOT/history.json"
assert_json "$TEST_ROOT/history.json" \
    'len(value) >= 2 and value[0]["initiator"] == "web" and any(item["initiator"] == "cli" for item in value)' \
    'shared history does not contain both CLI and Web jobs'

kill "$WEB_PID"
wait "$WEB_PID"
WEB_PID=""

echo "PASS: stress CLI/Web binary E2E (four parsers, shared report/history, cross-process lock)"
