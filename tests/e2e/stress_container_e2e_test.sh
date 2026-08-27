#!/usr/bin/env bash
set -euo pipefail

DOCKER_BIN=${DOCKER_BIN:-docker}
CONTROL_IMAGE=${CATMONITOR_CONTAINER_IMAGE:-catmonitor-generic:latest}
CPU_IMAGE=${CATMONITOR_CPU_STRESS_IMAGE:-catmonitor/stress-cpu:latest}
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-stress-v2-container.XXXXXXXX")
SUFFIX=$$
DAEMON="catmonitor-v2-e2e-$SUFFIX"
WEB="catmonitor-v2-web-e2e-$SUFFIX"
CPU="catmonitor-v2-cpu-e2e-$SUFFIX"
SNAPSHOT="catmonitor-v2-snapshot-e2e-$SUFFIX"
DATA="catmonitor-v2-data-e2e-$SUFFIX"
CONTROL="catmonitor-v2-control-e2e-$SUFFIX"
STRESS="catmonitor-v2-stress-e2e-$SUFFIX"
WEB_PORT=${CATMONITOR_CONTAINER_WEB_PORT:-19529}

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
cleanup() {
    "$DOCKER_BIN" rm -f "$WEB" "$DAEMON" "$CPU" >/dev/null 2>&1 || true
    "$DOCKER_BIN" volume rm "$SNAPSHOT" "$DATA" "$CONTROL" "$STRESS" >/dev/null 2>&1 || true
    case "$TEST_ROOT" in
        "${TMPDIR:-/tmp}"/catmonitor-stress-v2-container.*) rm -rf -- "$TEST_ROOT" ;;
    esac
}
trap cleanup EXIT HUP INT TERM

wait_exec_file() {
    container=$1 path=$2
    for _ in $(seq 1 45); do
        "$DOCKER_BIN" exec "$container" test -s "$path" 2>/dev/null && return 0
        sleep 1
    done
    return 1
}
wait_http() {
    url=$1
    for _ in $(seq 1 45); do
        curl -fsS "$url" >/dev/null 2>&1 && return 0
        sleep 1
    done
    return 1
}

command -v "$DOCKER_BIN" >/dev/null 2>&1 || fail 'docker CLI unavailable'
command -v curl >/dev/null 2>&1 || fail 'curl unavailable'
"$DOCKER_BIN" info >/dev/null 2>&1 || fail 'docker daemon unavailable'
"$DOCKER_BIN" image inspect "$CONTROL_IMAGE" >/dev/null 2>&1 || fail "control image unavailable: $CONTROL_IMAGE"
"$DOCKER_BIN" image inspect "$CPU_IMAGE" >/dev/null 2>&1 || fail "CPU workload image unavailable: $CPU_IMAGE"

cat >"$TEST_ROOT/catmonitor.yaml" <<EOF
features: [web, health]
snapshot:
  enabled: true
  dir: /var/lib/catmonitor/snapshot
stress:
  enabled: true
  web_enabled: true
  control_socket: /run/catmonitor/control.sock
  report_path: /var/lib/catmonitor/stress/stress-latest.json
  default_benchmarks: [stream]
  executor:
    type: docker_exec
    docker_binary: /usr/bin/docker
    docker_socket: /var/run/docker.sock
  benchmarks:
    stream:
      enabled: true
      plugin: stream
      container: $CPU
      user: "65532:65532"
      timeout: 1m
EOF

for volume in "$SNAPSHOT" "$DATA" "$CONTROL" "$STRESS"; do
    "$DOCKER_BIN" volume create "$volume" >/dev/null
done

"$DOCKER_BIN" run -d --name "$CPU" --network none --read-only \
    --cap-drop ALL \
    --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
    --cap-add SETGID --cap-add SETPCAP --cap-add SETUID --cap-add SYS_NICE \
    --security-opt no-new-privileges:true \
    --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m \
    -v "$STRESS:/var/lib/catmonitor/stress" \
    "$CPU_IMAGE" >/dev/null

"$DOCKER_BIN" run -d --name "$DAEMON" --privileged --network none --pid host \
    -v /:/host:ro \
    -v /etc/os-release:/etc/os-release:ro \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v "$TEST_ROOT/catmonitor.yaml:/etc/catmonitor/catmonitor.yaml:ro" \
    -v "$SNAPSHOT:/var/lib/catmonitor/snapshot" \
    -v "$DATA:/var/lib/catmonitor/data" \
    -v "$CONTROL:/run/catmonitor" \
    -v "$STRESS:/var/lib/catmonitor/stress" \
    "$CONTROL_IMAGE" >/dev/null

wait_exec_file "$DAEMON" /var/lib/catmonitor/snapshot/snapshot.json || {
    "$DOCKER_BIN" logs "$DAEMON" >&2 || true
    fail 'daemon did not produce snapshot'
}
"$DOCKER_BIN" exec "$DAEMON" test -S /run/catmonitor/control.sock ||
    fail 'daemon control socket missing'

"$DOCKER_BIN" run -d --name "$WEB" --network host \
    --entrypoint /usr/local/bin/web \
    -v "$SNAPSHOT:/var/lib/catmonitor/snapshot:ro" \
    -v "$CONTROL:/run/catmonitor:ro" \
    "$CONTROL_IMAGE" \
    -addr="127.0.0.1:$WEB_PORT" \
    -snapshot-dir=/var/lib/catmonitor/snapshot \
    -control-socket=/run/catmonitor/control.sock >/dev/null

wait_http "http://127.0.0.1:$WEB_PORT/stress/" || {
    "$DOCKER_BIN" logs "$WEB" >&2 || true
    fail 'unified Web did not start'
}
curl -fsS "http://127.0.0.1:$WEB_PORT/api/stress/config" >"$TEST_ROOT/config.json"
grep -Fq '"operator":true' "$TEST_ROOT/config.json" || fail 'unified listener lacks operator routes'
grep -Fq '"security_debt_web_operator_auth":true' "$TEST_ROOT/config.json" || fail 'operator auth debt is not explicit'
grep -Fq '"available":true' "$TEST_ROOT/config.json" || fail 'STREAM preflight unavailable'

curl -fsS -X POST \
    -H 'Content-Type: application/json' \
    -H 'X-CATMonitor-Action: stress' \
    -H "Origin: http://127.0.0.1:$WEB_PORT" \
    -d '{"benchmarks":["stream"]}' \
    "http://127.0.0.1:$WEB_PORT/api/stress/runs" >"$TEST_ROOT/run.json"

job_id=$(sed -n 's/.*"job_id":"\([0-9a-f]*\)".*/\1/p' "$TEST_ROOT/run.json")
[ -n "$job_id" ] || fail 'Web response missing job id'
for _ in $(seq 1 120); do
    curl -fsS "http://127.0.0.1:$WEB_PORT/api/stress/runs/$job_id" >"$TEST_ROOT/job.json"
    grep -Eq '"status":"(healthy|time_limit_reached|unhealthy|cancelled)"' "$TEST_ROOT/job.json" && break
    sleep 1
done
grep -Fq '"status":"healthy"' "$TEST_ROOT/job.json" || { cat "$TEST_ROOT/job.json" >&2; fail 'STREAM Web run did not complete healthy'; }
grep -Fq '"initiator":"web"' "$TEST_ROOT/job.json" || fail 'Web initiator not persisted'
grep -Fq '"triad_mb_s"' "$TEST_ROOT/job.json" || fail 'normalized STREAM values missing'
"$DOCKER_BIN" exec "$CPU" test ! -d /var/lib/catmonitor/stress/active ||
    fail 'CPU workload active marker remained after completion'

printf 'PASS: daemon controller -> Docker workload plugin -> single-listener Web -> STREAM\n'
