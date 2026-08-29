# CATMonitor v0.3.6：Ascend NPU 节点

适用于 Ascend NPU Linux 节点。支持 Ascend Monitoring、可选 CPU Stress 和可选
NPU Burn。v0.3.6 当前只声明已完成 A2/Ascend910B4、CANN 8.3、`runc` 的功能验收；
其他 SoC/CANN 组合必须单独验收，不能由 A2 结果外推。

Docker Compose 是推荐入口；本页同时给出完整的 `docker run` 入口。两种入口使用
相同镜像、generator 配置、挂载、权限和 3/4/4/5 容器模型。

## 1. 前置条件

- Linux/arm64；
- Docker Engine；
- 使用 Compose 路径时安装 Docker Compose v2；只使用手工路径时不要求 Compose；
- 使用 NPU 手工路径时安装 Bash 与 Python 3，用于读取 generator 的
  `stress-profile.json`，不解析 Compose YAML；
- Ascend 驱动可用，至少存在一个 `/dev/davinciN`；
- `npu-smi info` 正常；
- NPU workload 镜像中的 CANN/torch_npu 与节点驱动兼容。

```bash
uname -m
npu-smi info
ls -1 /dev/davinci[0-9]*
docker version
docker compose version 2>/dev/null || true
python3 --version
```

generator 会动态发现实际存在的 `/dev/davinciN`。设备号不要求从 0 连续，不能用
最大设备号推导设备数量。

## 2. 镜像与源码

```bash
git clone https://github.com/Computing-Availability-Tools/CATMonitor.git
cd CATMonitor
git checkout <v0.3.6-release-ref>

export CATMONITOR_RELEASE='v0.3.6-rc.<shortsha>'
export CATMONITOR_REGISTRY='<registry>'
export CATMONITOR_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-npu:${CATMONITOR_RELEASE}"
export CATMONITOR_CPU_STRESS_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-stress-cpu:${CATMONITOR_RELEASE}"
export CATMONITOR_NPU_STRESS_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-stress-npu:${CATMONITOR_RELEASE}"
```

按实际能力拉取，未启用 NPU Burn 时无需下载较大的 NPU workload 镜像：

```bash
docker pull "$CATMONITOR_IMAGE"
# CPU Stress 可选
docker pull "$CATMONITOR_CPU_STRESS_IMAGE"
# NPU Stress 可选
docker pull "$CATMONITOR_NPU_STRESS_IMAGE"
```

从源码构建 Ascend Control：

```bash
bash docker/build.sh npu
docker tag catmonitor-npu "$CATMONITOR_IMAGE"
```

CPU/NPU workload 镜像构建见
[STRESS_USER_GUIDE.md](../features/stress/STRESS_USER_GUIDE.md)。发布镜像必须来自同一
v0.3.6 release source，不得复用 a2-r1 的镜像身份。

## 3. Monitoring-only

不启用 Stress 时只运行 `catmonitor`、`catmonitor-web`、`catmonitor-dfee`。不挂
Docker Socket，不启动 workload 容器，也不要求真实 `control.sock`。

### 3.1 Compose

```bash
export CATMONITOR_CONFIG="$PWD/docker/catmonitor.yaml"

CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_CONFIG="$CATMONITOR_CONFIG" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  -f docker/docker-compose.npu.yml \
  up -d
```

### 3.2 Manual `docker run`

这是完整兼容入口。保留 Web 的历史 `-config` flag，但不挂 Stress 配置或
control volume。

```bash
docker volume create cm-snapshot
docker volume create cm-data
docker volume create cm-csv

docker run -d --name catmonitor --restart unless-stopped \
  --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -v /usr/local/Ascend/nnae:/usr/local/Ascend/nnae:ro \
  -v /usr/local/Ascend/ascend-toolkit:/usr/local/Ascend/ascend-toolkit:ro \
  -v /usr/bin/hccn_tool:/usr/bin/hccn_tool:ro \
  -v /usr/local/sbin/npu-smi:/usr/local/sbin/npu-smi:ro \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:/usr/local/Ascend/ascend-toolkit/latest/aarch64-linux/lib64:/usr/local/Ascend/nnae/latest/lib64 \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  "$CATMONITOR_IMAGE"

docker run -d --name catmonitor-web --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  "$CATMONITOR_IMAGE" \
  -addr=:19322 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -config=/etc/catmonitor/catmonitor.yaml

docker run -d --name catmonitor-dfee --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-csv:/var/lib/catmonitor/csv \
  "$CATMONITOR_IMAGE" \
  -addr=:19323 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -exporter-port=9333 \
  -csv=disabled \
  -csv-dir=/var/lib/catmonitor/csv \
  -csv-interval=10s
```

```bash
test "$(docker ps --filter name='^/catmonitor$' --filter status=running -q | wc -l)" -eq 1
test "$(docker ps --filter name='^/catmonitor-web$' --filter status=running -q | wc -l)" -eq 1
test "$(docker ps --filter name='^/catmonitor-dfee$' --filter status=running -q | wc -l)" -eq 1
docker exec catmonitor test -s /var/lib/catmonitor/snapshot/snapshot_npu.json
curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19322/ >/dev/null
curl -fsS http://127.0.0.1:19323/ >/dev/null
curl -fsS http://127.0.0.1:9333/metrics >/dev/null
```

Web 中 Stress 显示未启用；旧 Monitoring YAML 可以完全没有顶层 `stress:` 段。

## 4. 生成 Stress 配置

generator 只生成节点配置和只读 metadata，不启动或删除容器。以下 A2 参数是当前
96 核验收节点使用的 profile；换机器时必须让 HPL 的 MPI ranks 与 `HPL.dat` 的
`P×Q` 一致，并按在线 CPU 数设置 HPCG ranks。

```bash
export CATMONITOR_GENERATED_DIR=/etc/catmonitor/generated-stress
export CATMONITOR_STRESS_STATE_DIR=/var/lib/catmonitor/stress
export CATMONITOR_NPU_OUTPUT_DIR=/var/lib/catmonitor/stress/npu-burn-output

sudo install -d -m 0750 \
  "$CATMONITOR_GENERATED_DIR" \
  "$CATMONITOR_STRESS_STATE_DIR" \
  "$CATMONITOR_NPU_OUTPUT_DIR"
```

CPU-only：

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$CATMONITOR_GENERATED_DIR" \
  --control-image "$CATMONITOR_IMAGE" \
  --cpu-image "$CATMONITOR_CPU_STRESS_IMAGE" \
  --stream-threads 0 \
  --hpl-processes 8 \
  --hpl-threads 12 \
  --hpcg-processes 96 \
  --hpcg-threads 1 \
  --enable-web \
  --force
```

NPU-only：

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$CATMONITOR_GENERATED_DIR" \
  --control-image "$CATMONITOR_IMAGE" \
  --npu-image "$CATMONITOR_NPU_STRESS_IMAGE" \
  --npu-burn-device all \
  --npu-chip-generation A2 \
  --npu-runtime runc \
  --enable-web \
  --force
```

Full：

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$CATMONITOR_GENERATED_DIR" \
  --control-image "$CATMONITOR_IMAGE" \
  --cpu-image "$CATMONITOR_CPU_STRESS_IMAGE" \
  --stream-threads 0 \
  --hpl-processes 8 \
  --hpl-threads 12 \
  --hpcg-processes 96 \
  --hpcg-threads 1 \
  --npu-image "$CATMONITOR_NPU_STRESS_IMAGE" \
  --npu-burn-device all \
  --npu-chip-generation A2 \
  --npu-runtime runc \
  --enable-web \
  --force
```

手工 NPU 命令从 `stress-profile.json` 读取 host IDs、映射数量与 runtime logical IDs：

```bash
export CATMONITOR_STRESS_PROFILE="$CATMONITOR_GENERATED_DIR/stress-profile.json"
mapfile -t CATMONITOR_NPU_PROFILE_FIELDS < <(
  python3 - "$CATMONITOR_STRESS_PROFILE" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as stream:
    profile = json.load(stream)
npu = profile["npu"]
ids = npu["host_device_ids"]
if not npu["enabled"] or not ids:
    raise SystemExit("generated NPU profile is disabled or empty")
print(len(ids))
print(npu["runtime_visible_device_ids"])
print(npu["burn_logical_ids"])
for device_id in ids:
    print(device_id)
PY
)

export CATMONITOR_NPU_DEVICE_COUNT="${CATMONITOR_NPU_PROFILE_FIELDS[0]}"
export CATMONITOR_NPU_RUNTIME_VISIBLE_DEVICES="${CATMONITOR_NPU_PROFILE_FIELDS[1]}"
export CATMONITOR_NPU_BURN_DEVICE="${CATMONITOR_NPU_PROFILE_FIELDS[2]}"
CATMONITOR_NPU_DEVICE_ARGS=()
for device_id in "${CATMONITOR_NPU_PROFILE_FIELDS[@]:3}"; do
  test -e "/dev/davinci${device_id}"
  CATMONITOR_NPU_DEVICE_ARGS+=(
    "--device=/dev/davinci${device_id}:/dev/davinci${device_id}"
  )
done
test "${#CATMONITOR_NPU_DEVICE_ARGS[@]}" -eq "$CATMONITOR_NPU_DEVICE_COUNT"
```

例如宿主机观察到 `/dev/davinci2` 与 `/dev/davinci5`，只表示两个 host device
node；它们不代表 NPU Burn logical ID 是 `2,5`。generator 会得到 mapped count 2，
runtime logical namespace `0,1`，并在 workload preflight 中与 PCI topology、
`torch_npu.npu.device_count()` 交叉检查。

## 5. CPU Stress

### 5.1 Compose

先执行第 4 节 CPU-only generator，然后：

```bash
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_STRESS_STATE_DIR="$CATMONITOR_STRESS_STATE_DIR" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f "$CATMONITOR_GENERATED_DIR/docker-compose.stress.generated.yml" \
  --profile stress-cpu up -d
```

### 5.2 Manual `docker run` — CPU Stress

先执行第 4 节 CPU-only generator。下面命令完整创建 CPU workload、daemon、Web 与
DFeE 共 4 个容器；只有 daemon 挂 Docker Socket。

```bash
docker volume create cm-snapshot
docker volume create cm-data
docker volume create cm-straggler
docker volume create cm-control
docker volume create cm-csv
docker volume create cm-stress-cpu-state

docker run -d --name catmonitor-stress-cpu --restart unless-stopped \
  --read-only --network none \
  --cap-drop ALL \
  --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
  --cap-add SETGID --cap-add SETPCAP --cap-add SETUID --cap-add SYS_NICE \
  --security-opt no-new-privileges:true \
  --pids-limit 4096 --shm-size=16g \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m \
  -e STREAM_THREADS=0 \
  -e HPL_MPI_PROCESSES=8 \
  -e HPL_THREADS_PER_PROCESS=12 \
  -e HPCG_MPI_PROCESSES=96 \
  -e HPCG_THREADS_PER_PROCESS=1 \
  -e HPCG_NX=32 -e HPCG_NY=32 -e HPCG_NZ=32 \
  -e HPCG_RUNTIME_SECONDS=60 \
  -v cm-stress-cpu-state:/var/lib/catmonitor/stress \
  --health-cmd='/usr/bin/setpriv --bounding-set=-all --inh-caps=-all --ambient-caps=-all --reuid=65532 --regid=65532 --init-groups --no-new-privs /usr/local/bin/catmonitor-stress-exec describe --benchmark stream --json' \
  --health-interval=5s --health-timeout=3s --health-retries=12 \
  --health-start-period=5s \
  "$CATMONITOR_CPU_STRESS_IMAGE"

docker run -d --name catmonitor --restart unless-stopped \
  --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -v /usr/local/Ascend/nnae:/usr/local/Ascend/nnae:ro \
  -v /usr/local/Ascend/ascend-toolkit:/usr/local/Ascend/ascend-toolkit:ro \
  -v /usr/bin/hccn_tool:/usr/bin/hccn_tool:ro \
  -v /usr/local/sbin/npu-smi:/usr/local/sbin/npu-smi:ro \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:/usr/local/Ascend/ascend-toolkit/latest/aarch64-linux/lib64:/usr/local/Ascend/nnae/latest/lib64 \
  -v "$CATMONITOR_GENERATED_DIR/catmonitor-stress.yaml:/etc/catmonitor/catmonitor.yaml:ro" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$CATMONITOR_STRESS_STATE_DIR:/var/lib/catmonitor/stress" \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  -v cm-straggler:/var/lib/catmonitor/straggler \
  -v cm-control:/run/catmonitor \
  "$CATMONITOR_IMAGE"

docker run -d --name catmonitor-web --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-control:/run/catmonitor:ro \
  "$CATMONITOR_IMAGE" \
  -addr=:19322 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -control-socket=/run/catmonitor/control.sock

docker run -d --name catmonitor-dfee --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-csv:/var/lib/catmonitor/csv \
  "$CATMONITOR_IMAGE" \
  -addr=:19323 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled -exporter-port=9333 \
  -csv=disabled -csv-dir=/var/lib/catmonitor/csv -csv-interval=10s
```

与 canonical Compose 的契约对照：Control 使用 host network/PID、privileged、
generated YAML、state、control 与 Docker Socket；CPU workload 使用 `network none`、
只读根文件系统、最小 capabilities、16 GiB shm、只读安全选项和独立可写 state；
Web/DFeE 不挂 Docker Socket。

## 6. NPU Stress

### 6.1 Compose

先执行第 4 节 NPU-only generator，然后：

```bash
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_STRESS_STATE_DIR="$CATMONITOR_STRESS_STATE_DIR" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f "$CATMONITOR_GENERATED_DIR/docker-compose.stress.generated.yml" \
  --profile stress-npu up -d
```

### 6.2 Manual `docker run` — NPU Stress

先执行第 4 节 NPU-only generator和 profile 读取块。下面命令动态使用 generator
发现的所有 host nodes，不允许手工把稀疏 host ID 当成 NPU Burn logical ID。

```bash
docker volume create cm-snapshot
docker volume create cm-data
docker volume create cm-straggler
docker volume create cm-control
docker volume create cm-csv
docker volume create cm-stress-npu-state

docker run -d --name catmonitor-stress-npu --restart unless-stopped \
  --runtime runc --privileged --read-only --network none \
  "${CATMONITOR_NPU_DEVICE_ARGS[@]}" \
  --device=/dev/davinci_manager:/dev/davinci_manager \
  --device=/dev/devmm_svm:/dev/devmm_svm \
  --device=/dev/hisi_hdc:/dev/hisi_hdc \
  --tmpfs /tmp:rw,nosuid,nodev,size=256m \
  --tmpfs /opt/catmonitor/npuburn-home:rw,nosuid,nodev,size=1g,mode=0750 \
  -e CATMONITOR_NPU_DEVICE_COUNT="$CATMONITOR_NPU_DEVICE_COUNT" \
  -e ASCEND_RT_VISIBLE_DEVICES="$CATMONITOR_NPU_RUNTIME_VISIBLE_DEVICES" \
  -e NPU_BURN_DEVICE="$CATMONITOR_NPU_BURN_DEVICE" \
  -e NPU_BURN_RUN_CASE=matmul \
  -e NPU_BURN_GROUP= \
  -e NPU_BURN_CHIP_GENERATION=A2 \
  -e NPU_BURN_INTERNAL_TIMEOUT_SECONDS=300 \
  -v cm-stress-npu-state:/var/lib/catmonitor/stress \
  -v "$CATMONITOR_NPU_OUTPUT_DIR:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output" \
  -v /sys/bus/pci:/sys/bus/pci:ro \
  -v /usr/local/Ascend/driver/lib64:/usr/local/Ascend/driver/lib64:ro \
  -v /usr/local/Ascend/driver/version.info:/usr/local/Ascend/driver/version.info:ro \
  -v /etc/ascend_install.info:/etc/ascend_install.info:ro \
  -v /usr/local/dcmi:/usr/local/dcmi:ro \
  -v /usr/local/bin/npu-smi:/usr/local/bin/npu-smi:ro \
  --health-cmd='/usr/local/bin/catmonitor-stress-exec describe --json' \
  --health-interval=10s --health-timeout=5s --health-retries=12 \
  --health-start-period=20s \
  "$CATMONITOR_NPU_STRESS_IMAGE"

docker run -d --name catmonitor --restart unless-stopped \
  --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -v /usr/local/Ascend/nnae:/usr/local/Ascend/nnae:ro \
  -v /usr/local/Ascend/ascend-toolkit:/usr/local/Ascend/ascend-toolkit:ro \
  -v /usr/bin/hccn_tool:/usr/bin/hccn_tool:ro \
  -v /usr/local/sbin/npu-smi:/usr/local/sbin/npu-smi:ro \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:/usr/local/Ascend/ascend-toolkit/latest/aarch64-linux/lib64:/usr/local/Ascend/nnae/latest/lib64 \
  -v "$CATMONITOR_GENERATED_DIR/catmonitor-stress.yaml:/etc/catmonitor/catmonitor.yaml:ro" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$CATMONITOR_STRESS_STATE_DIR:/var/lib/catmonitor/stress" \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  -v cm-straggler:/var/lib/catmonitor/straggler \
  -v cm-control:/run/catmonitor \
  "$CATMONITOR_IMAGE"

docker run -d --name catmonitor-web --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-control:/run/catmonitor:ro \
  "$CATMONITOR_IMAGE" \
  -addr=:19322 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -control-socket=/run/catmonitor/control.sock

docker run -d --name catmonitor-dfee --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-csv:/var/lib/catmonitor/csv \
  "$CATMONITOR_IMAGE" \
  -addr=:19323 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled -exporter-port=9333 \
  -csv=disabled -csv-dir=/var/lib/catmonitor/csv -csv-interval=10s
```

NPU workload 的根文件系统只读；`HOME=/opt/catmonitor/npuburn-home` 由 tmpfs 提供
运行期可写目录，结果另外持久化到 `$CATMONITOR_NPU_OUTPUT_DIR`。A2/CANN 8.3/runc
profile 只给 NPU workload `privileged=true`，其网络为 `none`；Web/DFeE 不获得该权限。

## 7. CPU + NPU Full

### 7.1 Compose

先执行第 4 节 Full generator，然后：

```bash
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_STRESS_STATE_DIR="$CATMONITOR_STRESS_STATE_DIR" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f "$CATMONITOR_GENERATED_DIR/docker-compose.stress.generated.yml" \
  --profile stress-cpu --profile stress-npu up -d
```

### 7.2 Manual `docker run` — CPU + NPU Full

先执行第 4 节 Full generator和 profile 读取块。下面是完整的 5 容器命令，不需要从
其他章节拼接隐藏参数。

```bash
docker volume create cm-snapshot
docker volume create cm-data
docker volume create cm-straggler
docker volume create cm-control
docker volume create cm-csv
docker volume create cm-stress-cpu-state
docker volume create cm-stress-npu-state

docker run -d --name catmonitor-stress-cpu --restart unless-stopped \
  --read-only --network none \
  --cap-drop ALL \
  --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
  --cap-add SETGID --cap-add SETPCAP --cap-add SETUID --cap-add SYS_NICE \
  --security-opt no-new-privileges:true \
  --pids-limit 4096 --shm-size=16g \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m \
  -e STREAM_THREADS=0 \
  -e HPL_MPI_PROCESSES=8 -e HPL_THREADS_PER_PROCESS=12 \
  -e HPCG_MPI_PROCESSES=96 -e HPCG_THREADS_PER_PROCESS=1 \
  -e HPCG_NX=32 -e HPCG_NY=32 -e HPCG_NZ=32 \
  -e HPCG_RUNTIME_SECONDS=60 \
  -v cm-stress-cpu-state:/var/lib/catmonitor/stress \
  --health-cmd='/usr/bin/setpriv --bounding-set=-all --inh-caps=-all --ambient-caps=-all --reuid=65532 --regid=65532 --init-groups --no-new-privs /usr/local/bin/catmonitor-stress-exec describe --benchmark stream --json' \
  --health-interval=5s --health-timeout=3s --health-retries=12 \
  --health-start-period=5s \
  "$CATMONITOR_CPU_STRESS_IMAGE"

docker run -d --name catmonitor-stress-npu --restart unless-stopped \
  --runtime runc --privileged --read-only --network none \
  "${CATMONITOR_NPU_DEVICE_ARGS[@]}" \
  --device=/dev/davinci_manager:/dev/davinci_manager \
  --device=/dev/devmm_svm:/dev/devmm_svm \
  --device=/dev/hisi_hdc:/dev/hisi_hdc \
  --tmpfs /tmp:rw,nosuid,nodev,size=256m \
  --tmpfs /opt/catmonitor/npuburn-home:rw,nosuid,nodev,size=1g,mode=0750 \
  -e CATMONITOR_NPU_DEVICE_COUNT="$CATMONITOR_NPU_DEVICE_COUNT" \
  -e ASCEND_RT_VISIBLE_DEVICES="$CATMONITOR_NPU_RUNTIME_VISIBLE_DEVICES" \
  -e NPU_BURN_DEVICE="$CATMONITOR_NPU_BURN_DEVICE" \
  -e NPU_BURN_RUN_CASE=matmul -e NPU_BURN_GROUP= \
  -e NPU_BURN_CHIP_GENERATION=A2 \
  -e NPU_BURN_INTERNAL_TIMEOUT_SECONDS=300 \
  -v cm-stress-npu-state:/var/lib/catmonitor/stress \
  -v "$CATMONITOR_NPU_OUTPUT_DIR:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output" \
  -v /sys/bus/pci:/sys/bus/pci:ro \
  -v /usr/local/Ascend/driver/lib64:/usr/local/Ascend/driver/lib64:ro \
  -v /usr/local/Ascend/driver/version.info:/usr/local/Ascend/driver/version.info:ro \
  -v /etc/ascend_install.info:/etc/ascend_install.info:ro \
  -v /usr/local/dcmi:/usr/local/dcmi:ro \
  -v /usr/local/bin/npu-smi:/usr/local/bin/npu-smi:ro \
  --health-cmd='/usr/local/bin/catmonitor-stress-exec describe --json' \
  --health-interval=10s --health-timeout=5s --health-retries=12 \
  --health-start-period=20s \
  "$CATMONITOR_NPU_STRESS_IMAGE"

docker run -d --name catmonitor --restart unless-stopped \
  --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -v /usr/local/Ascend/nnae:/usr/local/Ascend/nnae:ro \
  -v /usr/local/Ascend/ascend-toolkit:/usr/local/Ascend/ascend-toolkit:ro \
  -v /usr/bin/hccn_tool:/usr/bin/hccn_tool:ro \
  -v /usr/local/sbin/npu-smi:/usr/local/sbin/npu-smi:ro \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:/usr/local/Ascend/ascend-toolkit/latest/aarch64-linux/lib64:/usr/local/Ascend/nnae/latest/lib64 \
  -v "$CATMONITOR_GENERATED_DIR/catmonitor-stress.yaml:/etc/catmonitor/catmonitor.yaml:ro" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$CATMONITOR_STRESS_STATE_DIR:/var/lib/catmonitor/stress" \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  -v cm-straggler:/var/lib/catmonitor/straggler \
  -v cm-control:/run/catmonitor \
  "$CATMONITOR_IMAGE"

docker run -d --name catmonitor-web --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-control:/run/catmonitor:ro \
  "$CATMONITOR_IMAGE" \
  -addr=:19322 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -control-socket=/run/catmonitor/control.sock

docker run -d --name catmonitor-dfee --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-csv:/var/lib/catmonitor/csv \
  "$CATMONITOR_IMAGE" \
  -addr=:19323 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled -exporter-port=9333 \
  -csv=disabled -csv-dir=/var/lib/catmonitor/csv -csv-interval=10s
```

Full 必须恰好 5 个容器。daemon 因启用 Stress 获得 Docker Socket；Web 与 DFeE
仍没有 Docker Socket。

## 8. Doctor、Run、Status 与 Cancel

Compose 与手工入口都在 daemon 容器执行同一 CLI：

```bash
docker exec catmonitor catmonitor stress doctor \
  -c /etc/catmonitor/catmonitor.yaml -o table

docker exec catmonitor catmonitor stress run --bench stream -o table
docker exec catmonitor catmonitor stress run --bench npu_burn -o table
docker exec catmonitor catmonitor stress status -o table
docker exec catmonitor catmonitor stress cancel --job <job-id>
```

取消后必须检查完整进程树已退出：

```bash
docker top catmonitor-stress-cpu | grep -E 'xhpl|xhpcg|mpirun|hydra' && exit 1 || true
docker top catmonitor-stress-npu | grep -E 'ascend_npu_burn|python' && exit 1 || true
```

## 9. Web

```text
http://<node-address>:19322/
```

这是唯一 Web listener，同时提供 Monitoring、Stress Run、Cancel、Status 与 History。
Web/DFeE 不挂 Docker Socket。当前 operator API 尚无认证/RBAC，只能向可信管理网络
开放，或由带认证的反向代理保护。

## 10. 停止与升级

手工入口停止但保留容器：

```bash
docker stop catmonitor catmonitor-web catmonitor-dfee \
  catmonitor-stress-cpu catmonitor-stress-npu 2>/dev/null || true
```

重新启动只选择当前 profile 实际存在的容器。删除容器不会删除 named volumes；不要在
需要保留 snapshot/history 时删除这些 volumes。

升级到 v0.3.6：

```text
OLD_STRESS_YAML_COMPATIBLE=false
```

必须重新运行 generator，不能复制旧 `script_path`、固定 NPU 容器、CPU Runner socket
或独立 Stress Web 配置。Monitoring-only YAML 可以没有 `stress:` 段。

## 11. 故障排查

- NPU Monitoring 为空：检查 `npu-smi info`、driver/toolkit 挂载和 daemon 日志。
- NPU Burn unavailable：执行 doctor，核对 CANN、torch_npu、lspci 与 device count。
- 稀疏设备映射错误：不要手写连续 `0..N-1` host node，让 generator 动态发现。
- `No available device`：核对容器内 `lspci -D -d 19e5:`、
  `ASCEND_RT_VISIBLE_DEVICES`、`torch_npu.npu.device_count()` 与 logical topology。
- `aclInit 507899` / `Resource_Busy` / `Device id is invalid`：A2 验收 profile 必须让
  NPU workload 使用 `--privileged`；不要把该权限授予 Web 或 DFeE。
- HPL 立即失败：核对 `HPL_MPI_PROCESSES == P×Q`，不要盲用默认值 1。
- Web snapshot 未就绪：先检查 `19320/-/ready`，不要另起第二个 Web。
- Docker API 版本不匹配：v0.3.6 Controller 会通过配置的 Unix socket 自动协商，
  用户不应手工设置 `DOCKER_API_VERSION`。
- Registry 不可达：离线传输同一 v0.3.6 镜像，不得替换成旧 a2-r1 镜像。
