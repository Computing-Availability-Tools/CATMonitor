#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
AUDIT_SCRIPT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)/audit_stress_release.sh
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-stress-audit-test.XXXXXXXX")

cleanup() {
    case "$TEST_ROOT" in */catmonitor-stress-audit-test.*) rm -rf -- "$TEST_ROOT" ;; esac
}
trap cleanup EXIT HUP INT TERM

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_contains() {
    grep -Fq -- "$2" "$1" || fail "$1 does not contain: $2"
}

assert_rejected() {
    local name=$1 manifest=$2 log="$TEST_ROOT/$1.log"
    if bash "$AUDIT_SCRIPT" \
        --cpu-manifest "$manifest" \
        --npu-manifest "$TEST_ROOT/npu-numeric.json" \
        --require-runtime-manifests >"$log" 2>&1; then
        fail "$name schema_version unexpectedly passed"
    fi
    assert_contains "$log" 'manifest does not declare a positive schema_version'
}

printf '{"schema_version":1,"fixture":"numeric"}\n' >"$TEST_ROOT/cpu-numeric.json"
printf '{"schema_version":"1","fixture":"legacy-string"}\n' >"$TEST_ROOT/cpu-legacy.json"
printf '{"schema_version":6,"fixture":"numeric"}\n' >"$TEST_ROOT/npu-numeric.json"
printf '{"schema_version":"6","fixture":"legacy-string"}\n' >"$TEST_ROOT/npu-legacy.json"

bash "$AUDIT_SCRIPT" \
    --cpu-manifest "$TEST_ROOT/cpu-numeric.json" \
    --npu-manifest "$TEST_ROOT/npu-legacy.json" \
    --require-runtime-manifests >"$TEST_ROOT/numeric.log"
assert_contains "$TEST_ROOT/numeric.log" 'PASS: CPU manifest sha256='
assert_contains "$TEST_ROOT/numeric.log" 'PASS: NPU manifest sha256='

bash "$AUDIT_SCRIPT" \
    --cpu-manifest "$TEST_ROOT/cpu-legacy.json" \
    --npu-manifest "$TEST_ROOT/npu-numeric.json" \
    --require-runtime-manifests >"$TEST_ROOT/legacy.log"
assert_contains "$TEST_ROOT/legacy.log" 'PASS: CPU manifest sha256='

printf '{"schema_version":0}\n' >"$TEST_ROOT/zero.json"
printf '{"schema_version":"0"}\n' >"$TEST_ROOT/quoted-zero.json"
printf '{"schema_version":"current"}\n' >"$TEST_ROOT/non-numeric.json"
printf '{"fixture":"missing"}\n' >"$TEST_ROOT/missing.json"
printf '{"schema_version":1.5}\n' >"$TEST_ROOT/decimal.json"

assert_rejected zero "$TEST_ROOT/zero.json"
assert_rejected quoted-zero "$TEST_ROOT/quoted-zero.json"
assert_rejected non-numeric "$TEST_ROOT/non-numeric.json"
assert_rejected missing "$TEST_ROOT/missing.json"
assert_rejected decimal "$TEST_ROOT/decimal.json"

printf 'PASS: audit_stress_release.sh manifest schema compatibility\n'
