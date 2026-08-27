# CATMonitor v0.3.6：Ascend NPU 节点

适用于 Ascend NPU Linux 节点。支持 Ascend Monitoring、可选 CPU Stress 和可选
NPU Burn。v0.3.6 当前已完成 A2/Ascend910B4 功能验收；其他 SoC/CANN 组合必须单独验收，
不能由 A2 结果外推。

## 1. 前置条件

- Linux/arm64；
- Docker Engine 与 Docker Compose v2；
- Ascend 驱动可用；
- 至少一个 `/dev/davinciN`；
- `npu-smi info` 正常；
- NPU workload 镜像中的 CANN/torch_npu 与节点驱动兼容。

```bash
uname -m
npu-smi info
ls -1 /dev/davinci[0-9]*
docker version
docker compose version
```

generator 会动态发现实际存在的 `/dev/davinciN`，不要求设备号从 0 连续，也不能用
最大设备号推导设备数量。

## 2. 获取 v0.3.6

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

按实际能力拉取：

```bash
# 必需：Control
docker pull "$CATMONITOR_IMAGE"

# 可选：CPU Stress
docker pull "$CATMONITOR_CPU_STRESS_IMAGE"

# 可选：NPU Burn；镜像较大，不启用时无需下载
docker pull "$CATMONITOR_NPU_STRESS_IMAGE"
```

从源码构建 Ascend Control：

```bash
bash docker/build.sh npu
docker tag catmonitor-npu "$CATMONITOR_IMAGE"
```

CPU/NPU workload 镜像的构建方法见
[STRESS_USER_GUIDE.md](../features/stress/STRESS_USER_GUIDE.md)。三张 V2 镜像均需以
v0.3.6 源码重新构建，不能复用 a2-r1 Image ID 或 digest。

## 3. Ascend Monitoring-only

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

应只有 3 个服务，不创建任何 Stress workload：

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  -f docker/docker-compose.npu.yml \
  ps

curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19320/metrics | grep -i npu | head
curl -fsS http://127.0.0.1:19322/ >/dev/null
curl -fsS http://127.0.0.1:19323/ >/dev/null
```

### 3.1 手工 `docker run`（Monitoring-only 兼容）

Compose 是推荐方式；不能使用 Compose 时仍可用下面的旧部署方式。它只启动
`catmonitor`、`catmonitor-web`、`catmonitor-dfee`，不挂 Docker Socket/control
socket，也不需要 CPU/NPU workload 镜像。

```bash
docker volume create cm-snapshot
docker volume create cm-data
docker volume create cm-csv

docker run -d --name catmonitor --privileged --network host --pid host \
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

docker run -d --name catmonitor-web --network host \
  --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  "$CATMONITOR_IMAGE" \
  -addr=:19322 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -config=/etc/catmonitor/catmonitor.yaml

docker run -d --name catmonitor-dfee --network host \
  --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-csv:/var/lib/catmonitor/csv \
  "$CATMONITOR_IMAGE" \
  -addr=:19323 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -exporter-port=9333 \
  -csv=disabled \
  -csv-dir=/var/lib/catmonitor/csv
```

```bash
docker exec catmonitor test -s /var/lib/catmonitor/snapshot/snapshot_npu.json
curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19322/ >/dev/null
curl -fsS http://127.0.0.1:19323/ >/dev/null
```

Web 中 Stress 配置会显示为未启用；Monitoring 页面不受影响。旧 Monitoring YAML
可以完全没有顶层 `stress:` 段。

## 4. 生成 Stress 节点配置

先创建管理员目录：

```bash
sudo install -d -m 0750 \
  /etc/catmonitor/generated-stress \
  /var/lib/catmonitor/stress \
  /var/lib/catmonitor/stress/npu-burn-output
```

### 4.1 CPU-only

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image "$CATMONITOR_IMAGE" \
  --cpu-image "$CATMONITOR_CPU_STRESS_IMAGE" \
  --enable-web \
  --force
```

### 4.2 NPU-only

A2 v0.3.6 默认使用全部由 PCI topology 验证通过的 NPU Burn logical devices：

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image "$CATMONITOR_IMAGE" \
  --npu-image "$CATMONITOR_NPU_STRESS_IMAGE" \
  --npu-burn-device all \
  --npu-chip-generation A2 \
  --npu-runtime runc \
  --enable-web \
  --force
```

不要把宿主机稀疏 device node ID 当成 NPU Burn logical ID。generator 会：

- identity-map 实际 `/dev/davinciN`；
- 按映射数量设置连续的 CANN runtime visible IDs；
- 通过容器内 `lspci` 建立并核对 NPU Burn logical namespace；
- 由 runtime preflight 核对 `torch_npu.npu.device_count()` 与映射数量。

A2/CANN 8.3/runc 实机还要求 NPU workload 容器使用 `privileged: true`，否则即使
device node 已显式映射，CANN 仍可能在 `aclInit` 阶段返回 `Resource_Busy` 或
`Device id is invalid`。该权限只授予 `catmonitor-stress-npu`；Web、DFeE 与 CPU
workload 不获得此权限，NPU workload 继续使用 `network_mode: none`。

### 4.3 CPU + NPU Full

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image "$CATMONITOR_IMAGE" \
  --cpu-image "$CATMONITOR_CPU_STRESS_IMAGE" \
  --npu-image "$CATMONITOR_NPU_STRESS_IMAGE" \
  --npu-burn-device all \
  --npu-chip-generation A2 \
  --npu-runtime runc \
  --enable-web \
  --force
```

## 5. 启动

### 5.1 CPU-only（4 个服务）

```bash
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_STRESS_STATE_DIR=/var/lib/catmonitor/stress \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu up -d
```

### 5.2 NPU-only（4 个服务）

```bash
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_STRESS_STATE_DIR=/var/lib/catmonitor/stress \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-npu up -d
```

### 5.3 CPU + NPU Full（5 个服务）

```bash
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_STRESS_STATE_DIR=/var/lib/catmonitor/stress \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu \
  --profile stress-npu \
  up -d
```

## 6. 验证与运行

以 Full 为例：

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu \
  --profile stress-npu \
  ps
```

运行 doctor，必须在执行真实负载前确认 CPU/NPU 预检通过：

```bash
SERVICE_ID=$(docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu \
  --profile stress-npu ps -q catmonitor)

docker exec "$SERVICE_ID" catmonitor stress doctor \
  -c /etc/catmonitor/catmonitor.yaml -o table
```

确认设备空闲后再运行：

```bash
docker exec "$SERVICE_ID" catmonitor stress run --bench stream -o table
docker exec "$SERVICE_ID" catmonitor stress run --bench npu_burn -o table
docker exec "$SERVICE_ID" catmonitor stress status -o table
```

NPU Burn 会占用所有选定设备。取消：

```bash
docker exec "$SERVICE_ID" catmonitor stress cancel --job <job-id>
docker exec catmonitor-stress-npu pgrep -af 'ascend_npu_burn|python' || true
```

第二条无 workload 输出才表示取消清理完成。

Web：

```text
http://<node-address>:19322/
```

这是唯一 Web listener，同时提供监控、Run、Cancel 与 History。Web/DFeE 不挂
Docker Socket。当前 operator API 尚无认证/RBAC，只应向可信管理网络开放。

## 7. 停止和升级

使用与启动时相同的 Compose 文件和 profile，将 `up -d` 替换为 `stop`。恢复使用
`start`；删除容器使用 `down`。需要保留 snapshot 和 Stress history 时不要执行
`down -v`。

升级到 v0.3.6：

```text
OLD_STRESS_YAML_COMPATIBLE=false
```

必须重新运行 generator，不能复制旧 `script_path`、固定 NPU 容器、CPU Runner socket
或独立 Stress Web 配置。

## 8. 常见问题

- NPU Monitoring 为空：检查 `npu-smi info`、driver/toolkit 挂载和 daemon 日志。
- NPU Burn unavailable：执行 doctor，核对 CANN、torch_npu、lspci 与 device count。
- 稀疏设备映射错误：不要手写连续 `0..N-1` host node，默认让 generator 发现。
- `No available device`：检查容器内 `lspci -D -d 19e5:`、
  `ASCEND_RT_VISIBLE_DEVICES`、`torch_npu.npu.device_count()` 与 logical topology。
- `aclInit 507899` / `Resource_Busy` / `Device id is invalid`：确认使用 generator
  生成的 A2 override，且 NPU workload 服务具有 `privileged: true`；不要把该权限
  加到 Web 或 DFeE。
- CANN source 失败：NPU workload 镜像必须与实际 CANN/torch_npu profile 匹配。
- Web snapshot 未就绪：先检查 `19320/-/ready`，不要另起第二个 Web。
- Registry 不可达：离线传输同一 v0.3.6 镜像；不要替换成旧 a2-r1 镜像。
