#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
GENERATOR=$(cd -- "$SCRIPT_DIR/.." && pwd -P)/generate_stress_deployment.sh
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/catmonitor-stress-deploy-test.XXXXXXXX")

cleanup() {
    case "$TEST_ROOT" in */catmonitor-stress-deploy-test.*) rm -rf -- "$TEST_ROOT" ;; esac
}
trap cleanup EXIT HUP INT TERM

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
assert_contains() { grep -Fq -- "$2" "$1" || fail "$1 does not contain: $2"; }
assert_fails() { if "$@" >/dev/null 2>&1; then fail "command unexpectedly succeeded: $*"; fi; }

RUNTIME_ROOT="$TEST_ROOT/runtime"
PLUGIN_ROOT="$TEST_ROOT/plugin"
OUTPUT_DIR="$TEST_ROOT/deployment"
TOOLS="$TEST_ROOT/tools"
install -d -m 0755 \
    "$RUNTIME_ROOT/stream" "$RUNTIME_ROOT/hpl" "$RUNTIME_ROOT/hpcg" \
    "$TEST_ROOT/manifests" "$TEST_ROOT/npu-output" "$TOOLS"

for executable in \
    "$RUNTIME_ROOT/stream/stream_omp" \
    "$RUNTIME_ROOT/hpl/xhpl" \
    "$RUNTIME_ROOT/hpcg/xhpcg"; do
    printf '#!/usr/bin/env bash\nexit 0\n' >"$executable"
    chmod 0755 "$executable"
done
cat >"$RUNTIME_ROOT/hpl/HPL.dat" <<'EOF'
HPLinpack benchmark input file
CATMonitor fixture
HPL.out
6
1
50000
1
256
0
1
4
2
EOF
printf '32 32 32\n60\n' >"$RUNTIME_ROOT/hpcg/hpcg.dat"
printf '{"schema_version":1,"fixture":"cpu"}\n' >"$TEST_ROOT/manifests/cpu.json"
printf '{"schema_version":1,"fixture":"npu"}\n' >"$TEST_ROOT/manifests/npu.json"
printf '{"schema_version":1,"feature":"stress_cpu_runner"}\n' >"$TEST_ROOT/manifests/cpu-runner.json"

cat >"$TOOLS/numactl" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat >"$TOOLS/mpirun" <<'EOF'
#!/usr/bin/env bash
if [ "${1-}" = --version ]; then echo 'HYDRA build details: Version 4.1.3'; fi
exit 0
EOF
cat >"$TOOLS/docker" <<'EOF'
#!/usr/bin/env bash
case "${1-}" in
    inspect)
        printf 'true|catmonitor/npuburn:test\n'
        ;;
    exec)
        command_line=$*
        case "$command_line" in
            *'/usr/bin/test -x'*) exit 0 ;;
            *'/dev/davinci[0-9]'*) printf '0\n1\n' ;;
            *'lspci_path='*) printf '0\n1\n' ;;
            *) exit 0 ;;
        esac
        ;;
    *) exit 2 ;;
esac
EOF
chmod 0755 "$TOOLS/"*

generate() {
    bash "$GENERATOR" \
        --output-dir "$OUTPUT_DIR" \
        --runtime-root "$RUNTIME_ROOT" \
        --plugin-root "$PLUGIN_ROOT" \
        --cpu-manifest "$TEST_ROOT/manifests/cpu.json" \
        --npu-manifest "$TEST_ROOT/manifests/npu.json" \
        --numactl "$TOOLS/numactl" \
        --mpi-launcher "$TOOLS/mpirun" \
        --hpl-processes 8 \
        --hpl-threads 12 \
        --hpcg-processes 96 \
        --hpcg-threads 1 \
        --npu-runtime "$TOOLS/docker" \
        --npu-container catmonitor-npu-burn \
        --npu-image catmonitor/npuburn:test \
        --npu-output-dir "$TEST_ROOT/npu-output" \
        --npu-device 1 \
        --npu-chip-generation A3 \
        --npu-cann 9.0.1 \
        --npu-torch-npu 2.10.0.post2 \
        --npu-soc Ascend-A3 \
        --npu-run-case quant_matmul \
        --enable-web \
        "$@"
}

assert_fails bash "$GENERATOR"
assert_fails generate --output-dir "$TEST_ROOT/bad-device" --npu-device 0,,1
assert_fails generate --output-dir "$TEST_ROOT/bad-processes" --hpl-processes 0
assert_fails generate --output-dir "$TEST_ROOT/missing-manifest" --npu-manifest "$TEST_ROOT/manifests/missing.json"
generate

ADAPTER="$OUTPUT_DIR/benchmark_check.sh"
CONFIG="$OUTPUT_DIR/catmonitor-stress.yaml"
MANIFEST="$OUTPUT_DIR/stress-deployment-manifest.json"
for file in "$ADAPTER" "$CONFIG" "$MANIFEST"; do [ -f "$file" ] || fail "missing generated file: $file"; done
if command -v python3 >/dev/null 2>&1; then python3 -m json.tool "$MANIFEST" >/dev/null; fi
bash -n "$ADAPTER"
assert_contains "$ADAPTER" "HPL_MPI_PROCESSES=8"
assert_contains "$ADAPTER" "HPCG_MPI_PROCESSES=96"
assert_contains "$ADAPTER" "NPU_BURN_BACKEND=docker_exec"
assert_contains "$ADAPTER" "NPU_BURN_DEVICE=1"
assert_contains "$CONFIG" "web_enabled: true"
assert_contains "$CONFIG" "script_path: \"$PLUGIN_ROOT/benchmark_check.sh\""
assert_contains "$CONFIG" "stream: { enabled: true, timeout: 1m }"
assert_contains "$CONFIG" "hpl: { enabled: true, timeout: 10m }"
assert_contains "$CONFIG" "npu_burn: { enabled: true, timeout: 30m }"
assert_contains "$MANIFEST" '"schema_version":1'
assert_contains "$MANIFEST" "\"installed_path\":\"$PLUGIN_ROOT/benchmark_check.sh\""
assert_contains "$MANIFEST" '"hpcg_mpi_processes":96'

for benchmark in stream hpl hpcg npu_burn; do
    profile="$TEST_ROOT/$benchmark.json"
    bash "$ADAPTER" describe "$benchmark" >"$profile"
    assert_contains "$profile" "\"benchmark\":\"$benchmark\""
    grep -Eq '"preflight":\{"status":"(pass|warn)"' "$profile" || \
        fail "$benchmark describe did not pass: $(cat "$profile")"
done

assert_fails generate
generate --force

UNIX_OUTPUT="$TEST_ROOT/deployment-unix"
generate \
    --output-dir "$UNIX_OUTPUT" \
    --cpu-backend unix \
    --cpu-runner-image catmonitor/stress-cpu:test \
    --cpu-runner-manifest "$TEST_ROOT/manifests/cpu-runner.json"

UNIX_ADAPTER="$UNIX_OUTPUT/benchmark_check.sh"
RUNNER_ADAPTER="$UNIX_OUTPUT/cpu-runner-benchmark_check.sh"
UNIX_CONFIG="$UNIX_OUTPUT/catmonitor-stress.yaml"
UNIX_MANIFEST="$UNIX_OUTPUT/stress-deployment-manifest.json"
for file in "$UNIX_ADAPTER" "$RUNNER_ADAPTER" "$UNIX_CONFIG" "$UNIX_MANIFEST"; do
    [ -f "$file" ] || fail "missing generated CPU runner deployment file: $file"
done
if command -v python3 >/dev/null 2>&1; then python3 -m json.tool "$UNIX_MANIFEST" >/dev/null; fi
bash -n "$UNIX_ADAPTER"
bash -n "$RUNNER_ADAPTER"
assert_contains "$UNIX_ADAPTER" 'CPU_EXECUTION_BACKEND=unix'
assert_contains "$UNIX_ADAPTER" 'CPU_RUNNER_CLIENT=/usr/local/bin/catmonitor-stress-cpu-client'
assert_contains "$UNIX_ADAPTER" 'CPU_RUNNER_SOCKET=/run/catmonitor-stress/cpu-runner.sock'
assert_contains "$RUNNER_ADAPTER" 'CPU_EXECUTION_BACKEND=local'
assert_contains "$RUNNER_ADAPTER" 'CPU_EXECUTION_PROFILE=container_runner'
assert_contains "$RUNNER_ADAPTER" 'CPU_EXECUTION_IMAGE=catmonitor/stress-cpu:test'
assert_contains "$RUNNER_ADAPTER" 'HPL_WORKDIR=/var/lib/catmonitor/stress/work/hpl'
assert_contains "$RUNNER_ADAPTER" 'HPCG_WORKDIR=/var/lib/catmonitor/stress/work/hpcg'
assert_contains "$UNIX_CONFIG" 'result_dir: "/var/lib/catmonitor/stress/work/hpcg"'
assert_contains "$UNIX_MANIFEST" '"cpu_backend":"unix"'
assert_contains "$UNIX_MANIFEST" '"cpu_runner_image":"catmonitor/stress-cpu:test"'
assert_contains "$UNIX_MANIFEST" '"cpu_runner_image_manifest":{'

printf 'PASS: complete stress deployment generator fixture\n'
