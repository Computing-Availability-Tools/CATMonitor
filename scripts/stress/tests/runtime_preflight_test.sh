#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd -P)
PREFLIGHT="$REPO_ROOT/docker/stress/npu/runtime_preflight.sh"
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-npu-preflight-test.XXXXXXXX")

cleanup() {
    case "$TEST_ROOT" in "${TMPDIR:-/tmp}"/catmonitor-npu-preflight-test.*) rm -rf -- "$TEST_ROOT" ;; esac
}
trap cleanup EXIT HUP INT TERM

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_contains() {
    grep -Fq -- "$2" "$1" || fail "$1 does not contain: $2"
}

assert_fails() {
    local log=$1
    shift
    if "$@" >"$log" 2>&1; then
        fail "command unexpectedly succeeded: $*"
    fi
}

TOOLS="$TEST_ROOT/tools"
install -d -m 0755 "$TOOLS"

cat >"$TEST_ROOT/ascend_env.sh" <<'EOF'
catmonitor_source_ascend_env() {
    [ "${FAKE_SOURCE_FAIL-}" != true ] || return 9
    export CATMONITOR_CANN_VERSION=9.0.1
}
EOF

cat >"$TOOLS/lspci" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[ "$*" = '-D -d 19e5:' ] || exit 8
[ "${FAKE_TOPOLOGY_EMPTY-}" != true ] || exit 0
printf '%s\n' \
    '0000:01:00.0 Processing accelerators: Huawei fixture' \
    '0000:02:00.0 Processing accelerators: Huawei fixture'
EOF

cat >"$TOOLS/python3" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null
[ "${FAKE_IMPORT_FAIL-}" != true ] || exit 7
printf '%s\n' \
    'CATMONITOR_RUNTIME_IMPORT_TORCH=fixture' \
    'CATMONITOR_RUNTIME_IMPORT_TORCH_NPU=fixture' \
    'CATMONITOR_RUNTIME_IMPORT_ASCEND_NPU_BURN=fixture' \
    'CATMONITOR_RUNTIME_IMPORT_ASCEND_NPU_BURN_CUSTOM_OPS_CUSTOM_OPS_LIB=fixture'
EOF
chmod 0755 "$TOOLS/lspci" "$TOOLS/python3"

run_preflight() {
    env \
        PATH="$TOOLS:/usr/bin:/bin" \
        CATMONITOR_ASCEND_ENV_HELPER="$TEST_ROOT/ascend_env.sh" \
        bash "$PREFLIGHT"
}

run_preflight >"$TEST_ROOT/success.log"
assert_contains "$TEST_ROOT/success.log" 'CATMONITOR_RUNTIME_CANN_VERSION=9.0.1'
assert_contains "$TEST_ROOT/success.log" 'CATMONITOR_RUNTIME_TOPOLOGY_DEVICES=2'
assert_contains "$TEST_ROOT/success.log" 'CATMONITOR_RUNTIME_PREFLIGHT=PASS'

FAKE_TOPOLOGY_EMPTY=true assert_fails "$TEST_ROOT/topology.log" run_preflight
assert_contains "$TEST_ROOT/topology.log" 'no Ascend Processing accelerators were found by lspci'

FAKE_IMPORT_FAIL=true assert_fails "$TEST_ROOT/import.log" run_preflight
FAKE_SOURCE_FAIL=true assert_fails "$TEST_ROOT/source.log" run_preflight

printf 'PASS: NPU Burn fixed-container runtime preflight\n'
