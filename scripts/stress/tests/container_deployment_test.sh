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
require_fixed docker/build.sh 'CATMONITOR_ALPINE_MIRROR'
require_fixed docker/build.sh 'CATMONITOR_DEBIAN_MIRROR'
require_fixed docker/Dockerfile.generic 'ARG ALPINE_MIRROR=""'
require_fixed docker/Dockerfile.gpu 'ARG DEBIAN_MIRROR=""'
require_fixed docker/Dockerfile.npu 'ARG DEBIAN_MIRROR=""'

# README-npu is the public source of truth for the supported manual docker run
# path. Keep the three Stress profiles complete and aligned with the canonical
# Compose security boundary.
require_fixed docker/README.md '`docker run` 是完整支持的手工兼容入口'
require_fixed docker/README-npu.md '### 5.3 Manual `docker run`'
require_fixed docker/README-npu.md '### 6.4 Manual `docker run`'
require_fixed docker/README-npu.md '### 7.3 Manual `docker run`'
require_fixed docker/README-npu.md 'stress-profile.json'
require_fixed docker/README-npu.md 'CATMONITOR_NPU_DEVICE_COUNT'
require_fixed docker/README-npu.md 'CATMONITOR_NPU_DEVICE_ARGS'
require_fixed docker/README-npu.md '--runtime runc --privileged --read-only --network none'
require_fixed docker/README-npu.md '--cap-drop ALL'
require_fixed docker/README-npu.md '--pids-limit 4096 --shm-size=16g'
require_fixed docker/README-npu.md '-control-socket=/run/catmonitor/control.sock'
require_fixed docker/README-npu.md '-v /var/run/docker.sock:/var/run/docker.sock'

cpu_manual=$(sed -n '/^### 5\.3 /,/^### 5\.4 /p' "$REPO_ROOT/docker/README-npu.md")
npu_manual=$(sed -n '/^### 6\.4 /,/^### 6\.5 /p' "$REPO_ROOT/docker/README-npu.md")
full_manual=$(sed -n '/^### 7\.3 /,/^## 8\./p' "$REPO_ROOT/docker/README-npu.md")
for name in cpu npu full; do
    case "$name" in
        cpu) section=$cpu_manual ;;
        npu) section=$npu_manual ;;
        full) section=$full_manual ;;
    esac
    [ -n "$section" ] || fail "README-npu $name manual section is empty"
    [ "$(grep -Fc -- '-v /var/run/docker.sock:/var/run/docker.sock' <<<"$section")" -eq 1 ] ||
        fail "README-npu $name manual path must mount Docker socket exactly once on daemon"
    grep -Fq -- '--name catmonitor' <<<"$section" || fail "$name manual path lacks daemon"
    grep -Fq -- '--name catmonitor-web' <<<"$section" || fail "$name manual path lacks Web"
    grep -Fq -- '--name catmonitor-dfee' <<<"$section" || fail "$name manual path lacks DFeE"
    if grep -Fq 'docker compose' <<<"$section"; then
        fail "README-npu $name manual section must not require Compose"
    fi
done
grep -Fq -- '--name catmonitor-stress-cpu' <<<"$cpu_manual" || fail 'CPU manual path lacks workload'
grep -Fq -- '--name catmonitor-stress-npu' <<<"$npu_manual" || fail 'NPU manual path lacks workload'
grep -Fq -- '--name catmonitor-stress-cpu' <<<"$full_manual" || fail 'Full manual path lacks CPU workload'
grep -Fq -- '--name catmonitor-stress-npu' <<<"$full_manual" || fail 'Full manual path lacks NPU workload'

if grep -Eq -- '--device=/dev/davinci(2|5)(:|[[:space:]])' "$REPO_ROOT/docker/README-npu.md"; then
    fail 'README-npu must not hard-code the observed A2 sparse device IDs in docker run'
fi
for retired in catmonitor-install benchmark_check.sh catmonitor-stress-web ':29592'; do
    if grep -Fq -- "$retired" "$REPO_ROOT/docker/README-npu.md"; then
        fail "README-npu still references retired V1 entry: $retired"
    fi
done

printf 'PASS: unified daemon/controller and CPU/NPU workload container contracts\n'
