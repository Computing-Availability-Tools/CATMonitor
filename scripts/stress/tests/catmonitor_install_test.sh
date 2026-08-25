#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd -- "$(dirname -- "$0")/../../.." && pwd -P)
INSTALLER="$REPO_ROOT/scripts/catmonitor-install"
TEST_ROOT=$(mktemp -d)
trap 'rm -rf -- "$TEST_ROOT"' EXIT HUP INT TERM

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_contains() {
    local file=$1 expected=$2
    grep -Fq -- "$expected" "$file" || fail "$file does not contain: $expected"
}

assert_fails() {
    if "$@" >"$TEST_ROOT/unexpected.out" 2>"$TEST_ROOT/unexpected.err"; then
        fail "command unexpectedly succeeded: $*"
    fi
}

bash -n "$INSTALLER"

CONFIG="$TEST_ROOT/catmonitor.yaml"
STRESS_ROOT="$TEST_ROOT/stress"
STATE_DIR="$TEST_ROOT/state"
HOST_ROOT="$TEST_ROOT/host"
DOCKER_LOG="$TEST_ROOT/docker.log"
FAKE_DOCKER="$TEST_ROOT/docker"

printf 'snapshot:\n  enabled: true\nstress:\n  enabled: true\n  web_enabled: true\n' >"$CONFIG"
install -d "$STRESS_ROOT/manifests" "$STATE_DIR" \
    "$HOST_ROOT/usr/local/Ascend/driver" \
    "$HOST_ROOT/usr/local/Ascend/nnae" \
    "$HOST_ROOT/usr/local/Ascend/ascend-toolkit" \
    "$HOST_ROOT/usr/bin" "$HOST_ROOT/usr/local/sbin" "$HOST_ROOT/run"
printf '#!/usr/bin/env bash\nexit 0\n' >"$STRESS_ROOT/benchmark_check.sh"
printf '#!/usr/bin/env bash\nexit 0\n' >"$STRESS_ROOT/cpu-runner-benchmark_check.sh"
chmod 0755 "$STRESS_ROOT/benchmark_check.sh" "$STRESS_ROOT/cpu-runner-benchmark_check.sh"
touch "$HOST_ROOT/usr/bin/hccn_tool" "$HOST_ROOT/usr/local/sbin/npu-smi" \
    "$HOST_ROOT/run/docker.sock"

cat >"$FAKE_DOCKER" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'DOCKER_HOST=%s CATMONITOR_WEB_ADDR=%s command=%s\n' \
    "${DOCKER_HOST-}" "${CATMONITOR_WEB_ADDR-}" "$*" >>"${CATMONITOR_TEST_DOCKER_LOG:?}"

if [ "${1-}" = compose ] && [ "${2-}" = version ]; then
    printf 'Docker Compose version fixture\n'
    exit 0
fi
if [ "${1-}" = info ]; then exit 0; fi
if [ "${1-}" = image ] && [ "${2-}" = inspect ]; then exit 0; fi
if [ "${1-}" = container ] && [ "${2-}" = inspect ]; then
    printf 'true|true|catmonitor/npuburn:test\n'
    exit 0
fi
if [ "${1-}" = inspect ]; then
    printf 'healthy\n'
    exit 0
fi
if [ "${1-}" = compose ]; then
    case " $* " in
        *" ps -q cpu-stress-runner "*) printf 'cpu-runner-fixture\n' ;;
        *" exec -T catmonitor catmonitor stress doctor "*) printf 'Stress deployment doctor: PASS\n' ;;
        *) ;;
    esac
    exit 0
fi
exit 0
EOF
chmod 0755 "$FAKE_DOCKER"

write_manifest() {
    local generation=$1
    printf '%s\n' \
        '{"schema_version":1,"feature":"stress","profile":{"cpu_backend":"unix","cpu_runner_image":"catmonitor/stress-cpu:test","npu_container":"catmonitor-npuburn-test","npu_image":"catmonitor/npuburn:test","npu_chip_generation":"'"$generation"'"}}' \
        >"$STRESS_ROOT/manifests/stress-deployment-manifest.json"
}
write_manifest A3

common_args=(
    --config "$CONFIG"
    --stress-root "$STRESS_ROOT"
    --state-dir "$STATE_DIR"
    --compose-dir "$REPO_ROOT/docker"
    --docker-bin "$FAKE_DOCKER"
)

CATMONITOR_TEST_DOCKER_LOG="$DOCKER_LOG" bash "$INSTALLER" --help >"$TEST_ROOT/help.out"
assert_contains "$TEST_ROOT/help.out" 'catmonitor-install --profile PROFILE'
assert_contains "$TEST_ROOT/help.out" 'does not build images or benchmarks'

assert_fails env CATMONITOR_TEST_DOCKER_LOG="$DOCKER_LOG" \
    bash "$INSTALLER" "${common_args[@]}" --action plan
assert_contains "$TEST_ROOT/unexpected.err" '--profile is required'

CATMONITOR_TEST_DOCKER_LOG="$DOCKER_LOG" bash "$INSTALLER" \
    --profile monitoring --action plan \
    "${common_args[@]}" >"$TEST_ROOT/monitoring.out"
assert_contains "$TEST_ROOT/monitoring.out" 'profile: monitoring'
assert_contains "$TEST_ROOT/monitoring.out" 'control image: catmonitor-generic'
assert_contains "$TEST_ROOT/monitoring.out" 'docker-compose.config.yml'
assert_contains "$TEST_ROOT/monitoring.out" 'Docker socket: not mounted'
assert_contains "$TEST_ROOT/monitoring.out" 'Web listener: :19322 (all interfaces)'
assert_contains "$DOCKER_LOG" 'command=compose'
assert_contains "$DOCKER_LOG" 'CATMONITOR_WEB_ADDR'
if grep -Fq 'docker-compose.stress.yml' "$TEST_ROOT/monitoring.out"; then
    fail 'monitoring profile unexpectedly includes stress overlay'
fi

CATMONITOR_TEST_DOCKER_LOG="$DOCKER_LOG" bash "$INSTALLER" \
    --profile monitoring --action plan --web-addr 127.0.0.1:19530 \
    "${common_args[@]}" >"$TEST_ROOT/monitoring-loopback.out"
assert_contains "$TEST_ROOT/monitoring-loopback.out" \
    'Web listener: 127.0.0.1:19530 (loopback)'

CATMONITOR_TEST_DOCKER_LOG="$DOCKER_LOG" bash "$INSTALLER" \
    --profile monitoring --action plan --web-addr 0.0.0.0:19531 \
    "${common_args[@]}" >"$TEST_ROOT/monitoring-all-ipv4.out"
assert_contains "$TEST_ROOT/monitoring-all-ipv4.out" \
    'Web listener: 0.0.0.0:19531 (all interfaces)'

CATMONITOR_TEST_DOCKER_LOG="$DOCKER_LOG" bash "$INSTALLER" \
    --profile cpu-stress --action plan "${common_args[@]}" >"$TEST_ROOT/cpu-plan.out"
assert_contains "$TEST_ROOT/cpu-plan.out" 'CPU runner image: catmonitor/stress-cpu:test'
assert_contains "$TEST_ROOT/cpu-plan.out" 'docker-compose.stress.yml'
assert_contains "$TEST_ROOT/cpu-plan.out" 'workload execution: none'

CATMONITOR_TEST_DOCKER_LOG="$DOCKER_LOG" bash "$INSTALLER" \
    --profile cpu-stress --action up "${common_args[@]}" >"$TEST_ROOT/cpu-up.out"
assert_contains "$TEST_ROOT/cpu-up.out" 'Stress deployment doctor: PASS'
assert_contains "$TEST_ROOT/cpu-up.out" 'CATMonitor profile is up: cpu-stress'
assert_contains "$TEST_ROOT/cpu-up.out" 'Web: http://<node-address>:19322/'
assert_contains "$TEST_ROOT/cpu-up.out" 'Stress: http://<node-address>:19322/stress/'
assert_contains "$DOCKER_LOG" 'up -d cpu-stress-runner catmonitor web dfee'
assert_contains "$DOCKER_LOG" 'exec -T catmonitor catmonitor stress doctor -c /etc/catmonitor/catmonitor.yaml -o table'

ascend_env=(
    CATMONITOR_INSTALL_TESTING=true
    CATMONITOR_INSTALL_HOST_ROOT="$HOST_ROOT"
    CATMONITOR_TEST_DOCKER_LOG="$DOCKER_LOG"
)
env "${ascend_env[@]}" bash "$INSTALLER" \
    --profile ascend-a3 --action plan "${common_args[@]}" >"$TEST_ROOT/a3-plan.out"
assert_contains "$TEST_ROOT/a3-plan.out" 'NPU generation: A3'
assert_contains "$TEST_ROOT/a3-plan.out" 'root-equivalent compatibility boundary'
assert_contains "$TEST_ROOT/a3-plan.out" 'note: Ascend up requires --acknowledge-root-docker-socket'

assert_fails env "${ascend_env[@]}" bash "$INSTALLER" \
    --profile ascend-a3 --action up "${common_args[@]}"
assert_contains "$TEST_ROOT/unexpected.err" 'requires --acknowledge-root-docker-socket'

env "${ascend_env[@]}" bash "$INSTALLER" \
    --profile ascend-a3 --action up --acknowledge-root-docker-socket \
    "${common_args[@]}" >"$TEST_ROOT/a3-up.out"
assert_contains "$TEST_ROOT/a3-up.out" 'CATMonitor profile is up: ascend-a3'
assert_contains "$DOCKER_LOG" 'docker-compose.npu.yml'
assert_contains "$DOCKER_LOG" 'docker-compose.stress-npuburn.yml'
assert_contains "$DOCKER_LOG" 'DOCKER_HOST=unix:///run/docker.sock'

assert_fails env "${ascend_env[@]}" bash "$INSTALLER" \
    --profile ascend-a2 --action plan "${common_args[@]}"
assert_contains "$TEST_ROOT/unexpected.err" 'requires manifest npu_chip_generation=A2'
assert_fails env CATMONITOR_TEST_DOCKER_LOG="$DOCKER_LOG" \
    bash "$INSTALLER" --profile monitoring --action plan --web-addr not-an-address \
    "${common_args[@]}"
assert_contains "$TEST_ROOT/unexpected.err" 'must be a valid host:port listen address'

# Recovery actions must remain available even when config/assets are missing.
CATMONITOR_TEST_DOCKER_LOG="$DOCKER_LOG" bash "$INSTALLER" \
    --profile ascend-a3 --action down \
    --config "$TEST_ROOT/missing.yaml" \
    --stress-root "$TEST_ROOT/missing-stress" \
    --state-dir "$TEST_ROOT/missing-state" \
    --compose-dir "$REPO_ROOT/docker" \
    --docker-bin "$FAKE_DOCKER" >"$TEST_ROOT/recovery-down.out"
assert_contains "$DOCKER_LOG" ' down'

PACKAGE_ROOT="$TEST_ROOT/package"
make -s -C "$REPO_ROOT" install-installer DESTDIR="$PACKAGE_ROOT" PREFIX=/usr/local
INSTALLED_INSTALLER="$PACKAGE_ROOT/usr/local/sbin/catmonitor-install"
[ -x "$INSTALLED_INSTALLER" ] || fail 'packaged catmonitor-install is not executable'
[ -f "$PACKAGE_ROOT/usr/local/lib/catmonitor/docker/docker-compose.config.yml" ] || \
    fail 'packaged Compose definitions are incomplete'
CATMONITOR_TEST_DOCKER_LOG="$DOCKER_LOG" bash "$INSTALLED_INSTALLER" \
    --profile monitoring --action plan --config "$CONFIG" \
    --docker-bin "$FAKE_DOCKER" >"$TEST_ROOT/installed-plan.out"
assert_contains "$TEST_ROOT/installed-plan.out" "$PACKAGE_ROOT/usr/local/lib/catmonitor/docker/docker-compose.yml"

if grep -Eq 'stress[[:space:]]+--bench|benchmark_check\.sh[[:space:]]+(stream|hpl|hpcg|npu_burn)' "$DOCKER_LOG"; then
    fail 'installer must not start a stress workload'
fi

printf 'PASS: unified catmonitor-install profile, safety and no-workload contract\n'
