#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd -P)
HELPER="$REPO_ROOT/docker/stress/npu/ascend_env.sh"
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-ascend-env-test.XXXXXXXX")

cleanup() {
    case "$TEST_ROOT" in "${TMPDIR:-/tmp}"/catmonitor-ascend-env-test.*) rm -rf -- "$TEST_ROOT" ;; esac
}
trap cleanup EXIT HUP INT TERM

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_contains() {
    grep -Fq -- "$2" "$1" || fail "$1 does not contain: $2"
}

if grep -Eq '(^|[[:space:]])-r([[:space:]]|$)' "$HELPER"; then
    fail 'Ascend environment discovery must not require test -r'
fi

write_env() {
    local path=$1
    local identity=$2
    install -d -m 0755 "$(dirname -- "$path")"
    {
        printf 'export CATMONITOR_ENV_FIXTURE=%q\n' "$identity"
        # Real CANN scripts may append an unset path variable. The helper must
        # source them safely even when the caller uses `set -u`.
        printf '%s\n' 'export CATMONITOR_UNSET_PATH_FIXTURE="${CATMONITOR_OPTIONAL_ASCEND_PATH}"'
    } >"$path"
}

# A versioned CANN layout is discovered and sourced without a driver mount.
CANN_ROOT="$TEST_ROOT/cann-layout"
write_env "$CANN_ROOT/cann-9.0.1/set_env.sh" cann-9.0.1
(
    export CATMONITOR_ASCEND_ENV_ROOT=$CANN_ROOT
    source "$HELPER"
    catmonitor_source_ascend_env
    [ "$CATMONITOR_ENV_FIXTURE" = cann-9.0.1 ]
    [ "$CATMONITOR_UNSET_PATH_FIXTURE" = "" ]
    [ "$CATMONITOR_CANN_VERSION" = 9.0.1 ]
) >"$TEST_ROOT/cann.log"
assert_contains "$TEST_ROOT/cann.log" "$CANN_ROOT/cann-9.0.1/set_env.sh"

# The canonical toolkit path wins even when multiple versioned layouts exist.
CANONICAL_ROOT="$TEST_ROOT/canonical-layout"
write_env "$CANONICAL_ROOT/ascend-toolkit/set_env.sh" canonical
write_env "$CANONICAL_ROOT/cann-8.3/set_env.sh" cann-8.3
write_env "$CANONICAL_ROOT/cann-9.0.1/set_env.sh" cann-9.0.1
(
    export CATMONITOR_ASCEND_ENV_ROOT=$CANONICAL_ROOT
    source "$HELPER"
    catmonitor_source_ascend_env
    [ "$CATMONITOR_ENV_FIXTURE" = canonical ]
) >"$TEST_ROOT/canonical.log"
assert_contains "$TEST_ROOT/canonical.log" "$CANONICAL_ROOT/ascend-toolkit/set_env.sh"

# The legacy latest/bin layout remains supported.
LATEST_ROOT="$TEST_ROOT/latest-layout"
write_env "$LATEST_ROOT/ascend-toolkit/latest/bin/setenv.bash" latest
(
    export CATMONITOR_ASCEND_ENV_ROOT=$LATEST_ROOT
    source "$HELPER"
    catmonitor_source_ascend_env
    [ "$CATMONITOR_ENV_FIXTURE" = latest ]
) >"$TEST_ROOT/latest.log"
assert_contains "$TEST_ROOT/latest.log" "$LATEST_ROOT/ascend-toolkit/latest/bin/setenv.bash"

# An explicit container path overrides every discovered layout.
OVERRIDE_ROOT="$TEST_ROOT/override-layout"
write_env "$OVERRIDE_ROOT/custom/set_env.sh" explicit
write_env "$OVERRIDE_ROOT/ascend-toolkit/set_env.sh" canonical
(
    export CATMONITOR_ASCEND_ENV_ROOT=$OVERRIDE_ROOT
    export ASCEND_ENV_SCRIPT="$OVERRIDE_ROOT/custom/set_env.sh"
    source "$HELPER"
    catmonitor_source_ascend_env
    [ "$CATMONITOR_ENV_FIXTURE" = explicit ]
) >"$TEST_ROOT/override.log"
assert_contains "$TEST_ROOT/override.log" "$OVERRIDE_ROOT/custom/set_env.sh"

# Missing and ambiguous layouts fail with actionable diagnostics.
EMPTY_ROOT="$TEST_ROOT/empty-layout"
install -d -m 0755 "$EMPTY_ROOT"
if CATMONITOR_ASCEND_ENV_ROOT="$EMPTY_ROOT" bash -c 'source "$1"; catmonitor_source_ascend_env' _ "$HELPER" \
    >"$TEST_ROOT/empty.log" 2>&1; then
    fail 'missing Ascend environment unexpectedly succeeded'
fi
assert_contains "$TEST_ROOT/empty.log" 'no supported Ascend environment initialization script found'

if ASCEND_ENV_SCRIPT="$EMPTY_ROOT/missing-set_env.sh" \
    CATMONITOR_ASCEND_ENV_ROOT="$EMPTY_ROOT" \
    bash -c 'source "$1"; catmonitor_source_ascend_env' _ "$HELPER" \
    >"$TEST_ROOT/missing-override.log" 2>&1; then
    fail 'missing explicit Ascend environment unexpectedly succeeded'
fi
assert_contains "$TEST_ROOT/missing-override.log" \
    'explicit Ascend environment script is not a regular file'

MULTI_ROOT="$TEST_ROOT/multiple-layout"
write_env "$MULTI_ROOT/cann-8.3/set_env.sh" cann-8.3
write_env "$MULTI_ROOT/cann-9.0.1/set_env.sh" cann-9.0.1
if CATMONITOR_ASCEND_ENV_ROOT="$MULTI_ROOT" bash -c 'source "$1"; catmonitor_source_ascend_env' _ "$HELPER" \
    >"$TEST_ROOT/multiple.log" 2>&1; then
    fail 'ambiguous Ascend environments unexpectedly succeeded'
fi
assert_contains "$TEST_ROOT/multiple.log" 'multiple Ascend CANN environment scripts were found'
assert_contains "$TEST_ROOT/multiple.log" 'set ASCEND_ENV_SCRIPT explicitly'

# Candidate discovery uses file identity; source is the final usability check.
BAD_SOURCE_ROOT="$TEST_ROOT/bad-source-layout"
install -d -m 0755 "$BAD_SOURCE_ROOT/cann-9.0.1"
cat >"$BAD_SOURCE_ROOT/cann-9.0.1/set_env.sh" <<'EOF'
return 23
EOF
if CATMONITOR_ASCEND_ENV_ROOT="$BAD_SOURCE_ROOT" \
    bash -c 'source "$1"; catmonitor_source_ascend_env' _ "$HELPER" \
    >"$TEST_ROOT/bad-source.log" 2>&1; then
    fail 'failing Ascend environment source unexpectedly succeeded'
fi
assert_contains "$TEST_ROOT/bad-source.log" 'failed to source Ascend environment script:'
assert_contains "$TEST_ROOT/bad-source.log" "$BAD_SOURCE_ROOT/cann-9.0.1/set_env.sh"

# Build-time preflight is independent of /usr/local/Ascend/driver. A failing
# HAL/import check propagates before wheel build.
install -d -m 0755 "$TEST_ROOT/tools"
cat >"$TEST_ROOT/tools/python3" <<'EOF'
#!/usr/bin/env bash
if [ "${FAKE_PREFLIGHT_FAIL:-}" = hal ]; then
    printf 'ERROR: Ascend build environment preflight failed\n\n' >&2
    printf 'libascend_hal:\n  unresolved: fixture failure\n' >&2
    exit 1
fi
printf 'CATMONITOR_PREFLIGHT_LIBASCEND_HAL=PASS\n'
printf 'CATMONITOR_PREFLIGHT_TORCH=PASS\n'
printf 'CATMONITOR_PREFLIGHT_TORCH_NPU=PASS\n'
printf 'CATMONITOR_PREFLIGHT_TBE=PASS\n'
EOF
chmod 0755 "$TEST_ROOT/tools/python3"
(
    export PATH="$TEST_ROOT/tools:$PATH"
    export CATMONITOR_ASCEND_ENV_ROOT="$CANN_ROOT"
    export CATMONITOR_ASCEND_ENV_SCRIPT_SELECTED="$CANN_ROOT/cann-9.0.1/set_env.sh"
    source "$HELPER"
    catmonitor_ascend_build_preflight
) >"$TEST_ROOT/preflight-pass.log"
assert_contains "$TEST_ROOT/preflight-pass.log" 'CATMONITOR_PREFLIGHT_TBE=PASS'
assert_contains "$TEST_ROOT/preflight-pass.log" 'CATMONITOR_DRIVER_MOUNT_PRESENT_AT_BUILD=false'

# A fixture-local driver directory must be reported as present without reading
# the real host /usr/local/Ascend tree.
DRIVER_ROOT="$TEST_ROOT/preflight-driver-layout"
write_env "$DRIVER_ROOT/cann-9.0.1/set_env.sh" cann-9.0.1
install -d -m 0755 "$DRIVER_ROOT/driver"
(
    export PATH="$TEST_ROOT/tools:$PATH"
    export CATMONITOR_ASCEND_ENV_ROOT="$DRIVER_ROOT"
    export CATMONITOR_ASCEND_ENV_SCRIPT_SELECTED="$DRIVER_ROOT/cann-9.0.1/set_env.sh"
    source "$HELPER"
    catmonitor_ascend_build_preflight
) >"$TEST_ROOT/preflight-driver.log"
assert_contains "$TEST_ROOT/preflight-driver.log" \
    'CATMONITOR_DRIVER_MOUNT_PRESENT_AT_BUILD=true'

if PATH="$TEST_ROOT/tools:$PATH" FAKE_PREFLIGHT_FAIL=hal \
    CATMONITOR_ASCEND_ENV_ROOT="$CANN_ROOT" \
    CATMONITOR_ASCEND_ENV_SCRIPT_SELECTED="$CANN_ROOT/cann-9.0.1/set_env.sh" \
    bash -c 'source "$1"; catmonitor_ascend_build_preflight' _ "$HELPER" \
    >"$TEST_ROOT/preflight-fail.log" 2>&1; then
    fail 'failed Ascend preflight unexpectedly succeeded'
fi
assert_contains "$TEST_ROOT/preflight-fail.log" 'Ascend build environment preflight failed'
assert_contains "$TEST_ROOT/preflight-fail.log" 'libascend_hal:'
assert_contains "$TEST_ROOT/preflight-fail.log" 'unresolved: fixture failure'
assert_contains "$HELPER" 'ctypes.CDLL("libascend_hal.so")'
assert_contains "$HELPER" '("torch_npu", "TORCH_NPU")'
assert_contains "$HELPER" '("tbe", "TBE")'

printf 'PASS: ascend_env.sh\n'
