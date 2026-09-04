#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/../../.." && pwd -P)
GO_BIN=${GO_BIN:-go}
TMP=$(mktemp -d)
DAEMON_PID=""
WEB_PID=""
cleanup() {
    if [ -n "$DAEMON_PID" ] && kill -0 "$DAEMON_PID" 2>/dev/null; then
        kill "$DAEMON_PID" 2>/dev/null || true
        wait "$DAEMON_PID" 2>/dev/null || true
    fi
    if [ -n "$WEB_PID" ] && kill -0 "$WEB_PID" 2>/dev/null; then
        kill "$WEB_PID" 2>/dev/null || true
        wait "$WEB_PID" 2>/dev/null || true
    fi
    rm -rf "$TMP"
}
trap cleanup EXIT

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
require_help_flag() {
    local output=$1 flag=$2 label=$3
    grep -Fq -- "$flag" <<<"$output" || fail "$label does not accept legacy flag $flag"
}

cd "$REPO_ROOT"
"$GO_BIN" build -o "$TMP/catmonitor" ./cmd/catmonitor
"$GO_BIN" build -o "$TMP/web" ./features/web
"$GO_BIN" build -o "$TMP/dfee" ./features/dfee

legacy_web_flags_test() {
    local help
    help=$($TMP/web -h 2>&1)
    require_help_flag "$help" '-addr' web
    require_help_flag "$help" '-snapshot-dir' web
    require_help_flag "$help" '-config' web
    "$TMP/web" \
        -addr=127.0.0.1:0 \
        -snapshot-dir="$TMP/snapshot" \
        -config="$TMP/legacy-web.yaml" \
        -control-socket="$TMP/missing-control.sock" \
        >"$TMP/web.log" 2>&1 &
    WEB_PID=$!
    sleep 1
    kill -0 "$WEB_PID" 2>/dev/null || {
        cat "$TMP/web.log" >&2
        fail 'Web did not start with legacy docker-run flags and no control socket'
    }
    kill "$WEB_PID"
    wait "$WEB_PID" || true
    WEB_PID=""
}

legacy_dfee_flags_test() {
    local help
    help=$($TMP/dfee -h 2>&1)
    for flag in -addr -snapshot-dir -exporter -exporter-port -device \
        -docker-container -csv -csv-dir -csv-interval -max-runtime; do
        require_help_flag "$help" "$flag" dfee
    done
    "$TMP/dfee" \
        -addr=127.0.0.1:0 \
        -snapshot-dir="$TMP/snapshot" \
        -max-runtime=100ms \
        >"$TMP/dfee.log" 2>&1 || {
        cat "$TMP/dfee.log" >&2
        fail 'DFeE did not start with legacy docker-run flags'
    }
}

daemon_without_docker_socket_test() {
    local config="$TMP/monitoring.yaml"
    local missing_socket="$TMP/missing-docker.sock"
    local control_socket="$TMP/control.sock"
    mkdir -p "$TMP/data"
    cat >"$config" <<EOF
collectors:
  cpu: { enabled: false, interval: 3s }
storage:
  data_dir: $TMP/data
  max_file_age: 1h
  rotation: daily
features: []
snapshot:
  enabled: false
stress:
  enabled: false
  web_enabled: false
  control_socket: $control_socket
  report_path: $TMP/stress-latest.json
  executor:
    type: docker_exec
    docker_binary: /definitely/missing/docker
    docker_socket: $missing_socket
EOF
    "$TMP/catmonitor" daemon --config "$config" >"$TMP/daemon.log" 2>&1 &
    DAEMON_PID=$!
    sleep 1
    kill -0 "$DAEMON_PID" 2>/dev/null || {
        cat "$TMP/daemon.log" >&2
        fail 'monitoring-only daemon exited without Docker'
    }
    [ ! -e "$control_socket" ] || fail 'disabled Stress created a control socket'
    kill "$DAEMON_PID"
    wait "$DAEMON_PID" || true
    DAEMON_PID=""
}

daemon_without_stress_section_test() {
    local config="$TMP/legacy-monitoring-no-stress.yaml"
    mkdir -p "$TMP/legacy-data"
    cat >"$config" <<EOF
server:
  type: cpu_only
collectors:
  chassis: { enabled: false, interval: 3s }
  cpu: { enabled: false, interval: 3s }
  memory: { enabled: false, interval: 3s }
  disk: { enabled: false, interval: 3s }
  gpu: { enabled: false, interval: 3s }
  npu: { enabled: false, interval: 3s }
  network: { enabled: false, interval: 3s }
storage:
  data_dir: $TMP/legacy-data
  max_file_age: 1h
  rotation: daily
features: []
snapshot:
  enabled: false
EOF
    "$TMP/catmonitor" daemon --config "$config" >"$TMP/legacy-daemon.log" 2>&1 &
    DAEMON_PID=$!
    sleep 1
    kill -0 "$DAEMON_PID" 2>/dev/null || {
        cat "$TMP/legacy-daemon.log" >&2
        fail 'legacy monitoring daemon exited without a stress section'
    }
    ! grep -Fq 'stress controller listening' "$TMP/legacy-daemon.log" || \
        fail 'legacy monitoring daemon started the Stress Controller'

    local fd link inode
    for fd in "/proc/$DAEMON_PID/fd/"*; do
        link=$(readlink "$fd" 2>/dev/null || true)
        case "$link" in
            'socket:['*']')
                inode=${link#'socket:['}
                inode=${inode%']'}
                if awk -v inode="$inode" '$7 == inode && $8 ~ /\/control\.sock$/ { found=1 } END { exit !found }' /proc/net/unix; then
                    fail 'legacy monitoring daemon owns a Stress control socket'
                fi
                ;;
        esac
    done

    kill "$DAEMON_PID"
    wait "$DAEMON_PID" || true
    DAEMON_PID=""
}

monitoring_only_compose_test() {
    local services
    services=$(awk '
        { sub(/\r$/, "", $0) }
        /^services:/ { in_services=1; next }
        /^volumes:/ { in_services=0 }
        in_services && /^  [A-Za-z0-9_-]+:$/ {
            name=$1; sub(/:$/, "", name); print name
        }
    ' docker/docker-compose.yml)
    [ "$services" = $'catmonitor\nweb\ndfee' ] || fail "monitoring Compose services differ: $services"
    ! grep -Fq '/var/run/docker.sock' docker/docker-compose.yml || fail 'monitoring Compose mounts Docker socket'
    ! grep -Fq '/var/lib/catmonitor/stress' docker/docker-compose.yml || fail 'monitoring Compose mounts Stress state'
    ! grep -Eq '^  catmonitor-stress-(cpu|npu):' docker/docker-compose.yml || fail 'monitoring Compose defines workload containers'
}

web_without_control_socket_test() {
    "$GO_BIN" test ./features/web -run '^TestWebWithoutControlSocket$' -count=1
}

legacy_monitoring_config_test() {
    "$GO_BIN" test ./internal/config -run '^(TestLegacyMonitoringConfig|TestLegacyMonitoringConfigWithoutStressSection|TestEnabledLegacyStressConfigRequiresMigration|TestLegacyHealthStressConfigRequiresMigration)$' -count=1
}

legacy_web_flags_test
legacy_dfee_flags_test
daemon_without_docker_socket_test
daemon_without_stress_section_test
web_without_control_socket_test
legacy_monitoring_config_test
monitoring_only_compose_test

printf 'PASS: monitoring backward compatibility fixtures\n'
