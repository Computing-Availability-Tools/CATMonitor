#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
INSTALLER="$REPO_ROOT/scripts/stress/install_stress_runtime.sh"
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-stress-install-test.XXXXXXXX")

cleanup() {
    case "$TEST_ROOT" in "${TMPDIR:-/tmp}"/catmonitor-stress-install-test.*) rm -rf -- "$TEST_ROOT" ;; esac
}
trap cleanup EXIT HUP INT TERM

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

mkdir -p "$TEST_ROOT/source/stream" "$TEST_ROOT/source/hpl" "$TEST_ROOT/source/hpcg"
printf '#!/bin/sh\nexit 0\n' >"$TEST_ROOT/source/stream/stream_omp"
printf '#!/bin/sh\nexit 0\n' >"$TEST_ROOT/source/hpl/xhpl"
printf 'HPL fixture\n' >"$TEST_ROOT/source/hpl/HPL.dat"
printf '#!/bin/sh\nexit 0\n' >"$TEST_ROOT/source/hpcg/xhpcg"
printf '32 32 32\n60\n' >"$TEST_ROOT/source/hpcg/hpcg.dat"
chmod 0755 "$TEST_ROOT/source/stream/stream_omp" "$TEST_ROOT/source/hpl/xhpl" "$TEST_ROOT/source/hpcg/xhpcg"
printf '{"schema_version":1}\n' >"$TEST_ROOT/cpu.json"
printf '{"schema_version":1}\n' >"$TEST_ROOT/deployment.json"

bash -n "$INSTALLER"
bash "$INSTALLER" \
    --plugin-root "$TEST_ROOT/install/stress" \
    --state-root "$TEST_ROOT/state/stress" \
    --cpu-runner-adapter "$REPO_ROOT/features/stress/benchmark_check.sh" \
    --runtime-source "$TEST_ROOT/source" \
    --cpu-manifest "$TEST_ROOT/cpu.json" \
    --deployment-manifest "$TEST_ROOT/deployment.json"

test -x "$TEST_ROOT/install/stress/benchmark_check.sh" || fail 'adapter was not installed'
test -x "$TEST_ROOT/install/stress/cpu-runner-benchmark_check.sh" || fail 'CPU runner adapter was not installed'
test -x "$TEST_ROOT/install/stress/runtime/stream/stream_omp" || fail 'STREAM was not installed'
test -x "$TEST_ROOT/install/stress/runtime/hpl/xhpl" || fail 'HPL was not installed'
test -f "$TEST_ROOT/install/stress/runtime/hpl/HPL.dat" || fail 'HPL.dat was not installed'
test -x "$TEST_ROOT/install/stress/runtime/hpcg/xhpcg" || fail 'HPCG was not installed'
test -f "$TEST_ROOT/install/stress/runtime/hpcg/hpcg.dat" || fail 'hpcg.dat was not installed'
test -f "$TEST_ROOT/install/stress/manifests/cpu-build-manifest.json" || fail 'CPU manifest was not installed'
test -f "$TEST_ROOT/install/stress/manifests/stress-deployment-manifest.json" || fail 'deployment manifest was not installed'
test -d "$TEST_ROOT/state/stress/npu-burn-output" || fail 'state layout was not created'
test -d "$TEST_ROOT/state/stress/work/hpl" || fail 'HPL runner work directory was not created'
test -d "$TEST_ROOT/state/stress/work/hpcg" || fail 'HPCG runner work directory was not created'

if bash "$INSTALLER" \
    --plugin-root "$TEST_ROOT/install/stress" \
    --state-root "$TEST_ROOT/state/stress" \
    >"$TEST_ROOT/no-force.log" 2>&1; then
    fail 'installer replaced an existing adapter without --force'
fi
grep -Fq 'use --force to replace it' "$TEST_ROOT/no-force.log" || fail 'no-force diagnostic is unclear'

bash "$INSTALLER" \
    --plugin-root "$TEST_ROOT/install/stress" \
    --state-root "$TEST_ROOT/state/stress" \
    --force >/dev/null

printf 'PASS: stress plugin installation layout and replacement boundary\n'
