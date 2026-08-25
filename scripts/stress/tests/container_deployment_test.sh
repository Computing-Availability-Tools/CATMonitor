#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/../../.." && pwd)

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

require_fixed() {
    file=$1
    expected=$2
    grep -Fq -- "$expected" "$REPO_ROOT/$file" ||
        fail "$file does not contain required deployment contract: $expected"
}

bash -n "$REPO_ROOT/docker/build.sh"
bash -n "$REPO_ROOT/scripts/stress/build_cpu_runner_image.sh"
bash -n "$REPO_ROOT/docker/stress/cpu/entrypoint.sh"
bash -n "$REPO_ROOT/scripts/stress/install_stress_runtime.sh"
bash -n "$REPO_ROOT/scripts/catmonitor-install"
bash -n "$REPO_ROOT/tests/e2e/stress_container_e2e_test.sh"

require_fixed .gitattributes '*.sh text eol=lf'
require_fixed .gitattributes 'scripts/catmonitor-install text eol=lf'
require_fixed .gitignore 'docker/.build/'

require_fixed docker/Dockerfile.generic 'ARG GOPROXY'
require_fixed docker/Dockerfile.generic 'FROM golang:1.23-alpine AS builder'
require_fixed docker/Dockerfile.generic 'FROM alpine:latest'
require_fixed docker/Dockerfile.generic 'apk add --no-cache'
require_fixed docker/Dockerfile.generic '/opt/catmonitor/stress'
require_fixed docker/Dockerfile.generic 'catmonitor-stress-cpu-client'
require_fixed docker/Dockerfile.npu 'FROM debian:bookworm-slim'
require_fixed docker/Dockerfile.npu 'docker.io'
require_fixed docker/Dockerfile.npu 'COPY docker/.build/catmonitor /usr/local/bin/catmonitor'
require_fixed docker/Dockerfile.npu '/opt/catmonitor/stress'
require_fixed docker/Dockerfile.npu 'catmonitor-stress-cpu-client'
require_fixed docker/stress/cpu/Dockerfile 'catmonitor-stress-cpu-runner'
require_fixed docker/stress/cpu/Dockerfile 'build_cpu_benchmarks.sh'
require_fixed docker/stress/cpu/Dockerfile 'libmpich-dev'
require_fixed docker/stress/cpu/Dockerfile 'ARG RUNTIME_IMAGE=debian:bookworm-slim'
require_fixed docker/stress/cpu/entrypoint.sh 'setpriv'
require_fixed docker/stress/npu/Dockerfile 'FROM ${BUILDER_BASE_IMAGE} AS npuburn_builder'
require_fixed docker/stress/npu/Dockerfile 'FROM ${RUNTIME_BASE_IMAGE} AS npuburn_runtime'
require_fixed docker/stress/npu/runtime_preflight.sh 'CATMONITOR_RUNTIME_PREFLIGHT=PASS'
require_fixed scripts/stress/create_npu_burn_container.sh 'CANN runtime and torch_npu stay inside the image'

require_fixed docker/catmonitor.yaml 'stress:'
require_fixed docker/catmonitor.yaml 'enabled: false'
require_fixed docker/catmonitor.yaml 'script_path: /opt/catmonitor/stress/benchmark_check.sh'
require_fixed docker/catmonitor.yaml 'report_path: /var/lib/catmonitor/stress/stress-latest.json'

require_fixed docker/docker-compose.yml 'network_mode: host'
require_fixed docker/docker-compose.yml 'entrypoint: /usr/local/bin/web'
require_fixed docker/docker-compose.yml 'entrypoint: /usr/local/bin/dfee'
require_fixed docker/docker-compose.yml 'CATMONITOR_WEB_ADDR:-:19322'
require_fixed docker/docker-compose.config.yml 'CATMONITOR_CONFIG'
require_fixed docker/docker-compose.config.yml 'create_host_path: false'
require_fixed docker/docker-compose.config.yml 'read_only: true'
require_fixed docker/docker-compose.npu.yml '/usr/local/Ascend/driver:/usr/local/Ascend/driver:ro'
require_fixed docker/docker-compose.stress.yml 'CATMONITOR_STRESS_ROOT'
require_fixed docker/docker-compose.stress.yml 'CATMONITOR_STRESS_STATE_DIR'
require_fixed docker/docker-compose.stress.yml 'create_host_path: false'
require_fixed docker/docker-compose.stress.yml 'cpu-stress-runner:'
require_fixed docker/docker-compose.stress.yml 'network_mode: none'
require_fixed docker/docker-compose.stress.yml 'CATMONITOR_CPU_STRESS_SHM_SIZE:-16gb'
require_fixed docker/docker-compose.stress.yml 'catmonitor_stress_socket'
require_fixed docker/docker-compose.stress.yml 'DAC_OVERRIDE'
require_fixed docker/docker-compose.stress.yml 'FOWNER'
require_fixed docker/docker-compose.stress.yml 'SETPCAP'
require_fixed docker/docker-compose.stress.yml 'SYS_NICE'
require_fixed docker/stress/cpu/entrypoint.sh '--bounding-set=-all'
require_fixed docker/docker-compose.stress-npuburn.yml 'CATMONITOR_DOCKER_SOCKET'
require_fixed docker/docker-compose.stress-npuburn.yml '/var/run/docker.sock'
require_fixed docker/docker-compose.stress-web.yml 'stress-web:'
require_fixed docker/docker-compose.stress-web.yml 'image: ${CATMONITOR_IMAGE:-catmonitor-generic}'
require_fixed docker/docker-compose.stress-web.yml '-addr=127.0.0.1:29592'
require_fixed docker/docker-compose.stress-web.yml 'snapshot:/var/lib/catmonitor/snapshot:ro'
require_fixed docker/docker-compose.stress-web.yml 'CATMONITOR_CONFIG'
require_fixed docker/docker-compose.stress-web.yml 'CATMONITOR_STRESS_ROOT'
require_fixed docker/docker-compose.stress-web.yml 'CATMONITOR_STRESS_STATE_DIR'
require_fixed docker/docker-compose.stress-web.yml 'catmonitor_stress_socket'
require_fixed docker/docker-compose.stress-web.yml 'CATMONITOR_DOCKER_SOCKET'
require_fixed docker/docker-compose.stress-web.yml 'CATMONITOR_NPU_OUTPUT_DIR'
require_fixed docker/docker-compose.stress-web.yml 'create_host_path: false'
require_fixed docker/docker-compose.stress-web.yml 'restart: unless-stopped'
require_fixed docker/build.sh 'Docker build proxy: configured'
require_fixed docker/build.sh 'Go module environment: configured'
require_fixed docker/build.sh 'CATMONITOR_DOCKER_BUILD_NETWORK'
require_fixed docker/build.sh 'run --rm --network "$DOCKER_BUILD_NETWORK"'
require_fixed tests/e2e/stress_container_e2e_test.sh 'catmonitor-generic:latest'
require_fixed tests/e2e/stress_container_e2e_test.sh 'CATMONITOR_CONTAINER_TEST_NPU_EXEC'

if grep -Fq 'goproxy.cn' "$REPO_ROOT/docker/build.sh"; then
    fail "container build must not hardcode a site-specific Go proxy"
fi

if grep -Eq '^[[:space:]]*version:' "$REPO_ROOT/docker/docker-compose.yml"; then
    fail "base Compose file must not use the obsolete version key"
fi
if grep -Eq '^[[:space:]]*network:[[:space:]]*host' "$REPO_ROOT/docker/docker-compose.yml"; then
    fail "base Compose file must use network_mode, not the invalid network key"
fi
if grep -Fq '/var/run/docker.sock' "$REPO_ROOT/docker/docker-compose.yml"; then
    fail "Docker socket must remain opt-in and absent from base Compose"
fi
if grep -Fq '/var/run/docker.sock' "$REPO_ROOT/docker/docker-compose.stress.yml"; then
    fail "CPU stress overlay must not grant Docker socket access"
fi
if grep -Fq 'CATMONITOR_CONFIG' "$REPO_ROOT/docker/docker-compose.stress.yml"; then
    fail "stress overlay must not duplicate the common read-only config mount"
fi
require_fixed scripts/catmonitor-install '--acknowledge-root-docker-socket'
require_fixed scripts/catmonitor-install 'workload execution: none'
if grep -Eq '^[[:space:]]{2}web:' "$REPO_ROOT/docker/docker-compose.stress-npuburn.yml"; then
    fail "externally reachable Web must not receive the root-equivalent Docker socket"
fi
if grep -Eq '(privileged:|pid:[[:space:]]*host)' \
    "$REPO_ROOT/docker/docker-compose.stress-web.yml"; then
    fail "operational Stress Web must not receive privileged or host PID access"
fi
if grep -Eq -- '-addr=(0\.0\.0\.0|:)' "$REPO_ROOT/docker/docker-compose.stress-web.yml"; then
    fail "operational Stress Web must remain loopback-only"
fi
if grep -Eq '(privileged:|pid:[[:space:]]*host|network_mode:[[:space:]]*host)' \
    "$REPO_ROOT/docker/docker-compose.stress.yml"; then
    fail "CPU stress runner overlay must not grant host-level namespaces or privileges"
fi
if grep -Fq 'benchmark_check.sh' "$REPO_ROOT/docker/Dockerfile.generic" ||
    grep -Fq 'benchmark_check.sh' "$REPO_ROOT/docker/Dockerfile.npu"; then
    fail "stress adapter must be installed as a host plugin, not baked into images"
fi
if grep -Fq 'docker-cli' "$REPO_ROOT/docker/Dockerfile.generic"; then
    fail "generic image must not carry the NPU Burn Docker client dependency"
fi
if grep -Eq '/usr/local/Ascend/(ascend-toolkit|cann-|nnae)' \
    "$REPO_ROOT/scripts/stress/create_npu_burn_container.sh"; then
    fail "NPU Burn fixed container must not mount host CANN/toolkit paths"
fi

printf 'PASS: container deployment definitions and stress opt-in boundary\n'
