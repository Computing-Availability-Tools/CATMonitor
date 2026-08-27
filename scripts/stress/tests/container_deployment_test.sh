#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/../../.." && pwd -P)
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
require_fixed() {
    local file=$1 expected=$2
    grep -Fq -- "$expected" "$REPO_ROOT/$file" || fail "$file is missing: $expected"
}
for script in \
    docker/build.sh \
    docker/stress/cpu/entrypoint.sh \
    docker/stress/npu/entrypoint.sh \
    scripts/stress/build_cpu_runner_image.sh \
    scripts/stress/build_npu_burn_image.sh \
    scripts/stress/generate_stress_deployment.sh; do
    bash -n "$REPO_ROOT/$script"
done

require_fixed .gitattributes '*.sh text eol=lf'
require_fixed .gitignore 'docker/.build/'

# Control images contain the daemon/Web/DFeE and Docker CLI transport, but no
# workload runtime and no retired CPU Unix-socket client.
require_fixed docker/Dockerfile.generic 'docker-cli'
require_fixed docker/Dockerfile.gpu 'docker.io'
require_fixed docker/Dockerfile.npu 'docker.io'
for file in docker/Dockerfile.generic docker/Dockerfile.gpu docker/Dockerfile.npu; do
    require_fixed "$file" '/usr/local/bin/catmonitor'
    require_fixed "$file" '/usr/local/bin/web'
    require_fixed "$file" '/usr/local/bin/dfee'
    if grep -Fq 'catmonitor-stress-cpu-client' "$REPO_ROOT/$file"; then
        fail "$file still packages the retired CPU Unix client"
    fi
done

# Both workload images implement the exact same fixed plugin entrypoint.
require_fixed docker/stress/cpu/Dockerfile 'catmonitor-stress-exec'
require_fixed docker/stress/cpu/Dockerfile 'features/stress/workloadplugin'
require_fixed docker/stress/cpu/Dockerfile 'CATMONITOR_STRESS_BENCHMARKS=stream,hpl,hpcg'
require_fixed docker/stress/cpu/entrypoint.sh 'setpriv'
require_fixed docker/stress/cpu/entrypoint.sh '--bounding-set=-all'
require_fixed docker/stress/npu/Dockerfile 'catmonitor-stress-exec'
require_fixed docker/stress/npu/Dockerfile 'features/stress/workloadplugin'
require_fixed docker/stress/npu/Dockerfile 'CATMONITOR_STRESS_BENCHMARKS=npu_burn'
require_fixed docker/stress/npu/Dockerfile 'FROM ${BUILDER_BASE_IMAGE} AS npuburn_builder'
require_fixed docker/stress/npu/Dockerfile 'FROM ${RUNTIME_BASE_IMAGE} AS npuburn_runtime'
if grep -Fq 'features/stress/runnerapi' "$REPO_ROOT/docker/stress/cpu/Dockerfile"; then
    fail 'CPU workload image still builds runnerapi'
fi

require_fixed docker/catmonitor.yaml 'control_socket: /run/catmonitor/control.sock'
require_fixed docker/catmonitor.yaml 'type: docker_exec'
require_fixed docker/catmonitor.yaml 'container: catmonitor-stress-cpu'
require_fixed docker/catmonitor.yaml 'container: catmonitor-stress-npu'

# Canonical Web exposes monitoring and Stress read/write APIs on :19322 and receives only the
# local control socket. The daemon alone receives Docker socket access when
# the stress overlay is enabled.
require_fixed docker/docker-compose.yml 'control:/run/catmonitor'
require_fixed docker/docker-compose.yml '-addr=${CATMONITOR_WEB_ADDR:-:19322}'
if grep -Fq -- '-operator-addr' "$REPO_ROOT/docker/docker-compose.yml"; then
    fail 'canonical Web still exposes the retired second listener'
fi
require_fixed docker/docker-compose.yml '-control-socket=/run/catmonitor/control.sock'
if grep -Fq '/var/run/docker.sock' "$REPO_ROOT/docker/docker-compose.yml"; then
    fail 'base monitoring Compose must not receive Docker socket access'
fi
require_fixed docker/docker-compose.stress.yml 'source: ${CATMONITOR_DOCKER_SOCKET:-/var/run/docker.sock}'
require_fixed docker/docker-compose.stress.yml 'catmonitor-stress-cpu:'
require_fixed docker/docker-compose.stress.yml 'profiles: ["stress-cpu"]'
require_fixed docker/docker-compose.stress.yml '--reuid=65532'
require_fixed docker/docker-compose.stress.yml 'catmonitor-stress-npu:'
require_fixed docker/docker-compose.stress.yml 'profiles: ["stress-npu"]'
require_fixed docker/docker-compose.stress.yml 'privileged: true'
require_fixed docker/docker-compose.stress.yml 'ASCEND_RT_VISIBLE_DEVICES:'
require_fixed docker/docker-compose.stress.yml '/opt/catmonitor/npuburn-home:rw,nosuid,nodev,size=1g,mode=0750'
require_fixed docker/docker-compose.stress.yml 'stress_npu_output:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output'
require_fixed docker/stress/npu/Dockerfile 'NPU_BURN_LOG_DIR=/opt/catmonitor/npuburn-home/.ascend_npu_burn/log'
if grep -Fq 'cpu-runner.sock' "$REPO_ROOT/docker/docker-compose.stress.yml"; then
    fail 'canonical Stress Compose still exposes the retired CPU socket'
fi
if grep -Eq '^[[:space:]]{2}(web|dfee):' "$REPO_ROOT/docker/docker-compose.stress.yml"; then
    fail 'Stress overlay must not grant Web/DFeE extra execution mounts'
fi

require_fixed scripts/stress/generate_stress_deployment.sh 'docker-compose.stress.generated.yml'
require_fixed scripts/stress/generate_stress_deployment.sh 'CATMONITOR_NPU_DEVICE_COUNT'
require_fixed scripts/stress/generate_stress_deployment.sh 'host_device_ids'
if grep -Fq 'docker run' "$REPO_ROOT/scripts/stress/generate_stress_deployment.sh"; then
    fail 'deployment generator must not create or start a container'
fi

if grep -Fq 'goproxy.cn' "$REPO_ROOT/docker/build.sh"; then
    fail 'container build must not hard-code a site-specific Go proxy'
fi

printf 'PASS: unified daemon/controller and CPU/NPU workload container contracts\n'
