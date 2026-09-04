# CATMonitor：Generic CPU 节点

适用于没有 NVIDIA GPU 或 Ascend NPU 的 Linux 节点。推荐部署 Monitoring，按需增加
STREAM、HPL、HPCG CPU Stress。

> 当前程序内部版本是 `0.3.5`，当前 ARM64 pre-release 镜像标签是
> `arm64-v0.3.5-stress`，目标发布线是 `v0.3.6`。

## 1. 前置条件

- Linux/amd64 或 Linux/arm64；
- Docker Engine；
- 访问镜像 registry，或已离线导入相同的 Stress 镜像。

使用推荐的 Compose 部署方式时才额外需要 Docker Compose v2。只使用本页的
`docker run` 兼容入口时不需要 Compose。直接拉取预构建镜像时需要访问镜像
registry，受限节点也可以提前离线导入相同标签和 Image ID 的镜像。

```bash
docker version
uname -m
# 仅 Compose 部署需要
docker compose version 2>/dev/null || true
```

## 2. 获取 ARM64 Stress pre-release 镜像

```bash
git clone https://github.com/Computing-Availability-Tools/CATMonitor.git
cd CATMonitor
git checkout refactor/unified-stress-platform
```

当前 Linux/ARM64 Stress 专用镜像如下：

```bash
export CATMONITOR_RELEASE='arm64-v0.3.5-stress'
export CATMONITOR_REGISTRY='ghcr.io/spike677'
export CATMONITOR_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-generic:${CATMONITOR_RELEASE}"
export CATMONITOR_CPU_STRESS_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-stress-cpu:${CATMONITOR_RELEASE}"
```

Monitoring-only 只需 Control 镜像：

```bash
docker pull "$CATMONITOR_IMAGE"
```

启用 CPU Stress 时再拉 CPU workload 镜像，不需要下载 NPU 镜像：

```bash
docker pull "$CATMONITOR_CPU_STRESS_IMAGE"
```

需要从源码构建 Generic Control 或 CPU workload、配置 Alpine mirror，或制作 RC
镜像时，请使用 [镜像构建与发布开发者指南](DEVELOPER_GUIDE.md)。普通节点只需拉取
与当前 release 匹配的镜像。

## 3. Monitoring-only

使用项目提供的默认容器配置：

```bash
export CATMONITOR_CONFIG="$PWD/docker/catmonitor.yaml"

CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_CONFIG="$CATMONITOR_CONFIG" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  up -d
```

应创建 3 个服务：`catmonitor`、`web`、`dfee`。

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  ps

curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19322/ >/dev/null
curl -fsS http://127.0.0.1:19323/ >/dev/null
```

### 3.1 手工 `docker run`（Monitoring-only 兼容）

Compose 是推荐方式；不能使用 Compose 时，下面的旧部署方式仍完整受支持。它只启动
`catmonitor`、`catmonitor-web`、`catmonitor-dfee` 三个容器，不挂 Docker Socket，
不创建 control socket 共享卷，也不需要任何 Stress workload 镜像。

```bash
docker volume create cm-snapshot
docker volume create cm-data
docker volume create cm-csv

docker run -d --name catmonitor --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
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

等待首次采集后验证：

```bash
docker exec catmonitor ls /var/lib/catmonitor/snapshot
curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19322/ >/dev/null
curl -fsS http://127.0.0.1:19323/ >/dev/null
```

Web 中 Stress 配置会显示为未启用；Monitoring 页面不受影响。旧 Monitoring YAML
可以完全没有顶层 `stress:` 段。

## 4. CPU Stress

### 4.1 Generate

先生成节点配置。该命令不构建镜像、不启动容器，也不执行压测：

```bash
sudo install -d -m 0750 \
  /etc/catmonitor/generated-stress \
  /var/lib/catmonitor/stress

sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
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

以上是已验证的 96 核 profile；其他节点必须按第 6 节调整，并确保 HPL ranks 等于
`HPL.dat` 的 `P×Q`。

### 4.2 Compose

启动 Monitoring + CPU Stress：

```bash
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_STRESS_STATE_DIR=/var/lib/catmonitor/stress \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu \
  up -d
```

应创建 4 个服务，且不存在 NPU workload 容器：

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu \
  ps
```

### 4.3 Manual `docker run`

不能使用 Compose 时，下面命令完整创建 CPU workload、daemon、Web 与 DFeE 共 4 个
容器。只有 daemon 挂 Docker Socket；Web/DFeE 不挂 socket。

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
  -v /etc/catmonitor/generated-stress/catmonitor-stress.yaml:/etc/catmonitor/catmonitor.yaml:ro \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /var/lib/catmonitor/stress:/var/lib/catmonitor/stress \
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

手工与 Compose 入口使用相同的 generated YAML 和资源参数；不要只修改容器环境而
不重新生成配置。

## 5. 验证和使用

从 daemon 容器验证三项 CPU workload：

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu \
  exec catmonitor \
  catmonitor stress doctor -c /etc/catmonitor/catmonitor.yaml -o table
```

运行单项压测：

```bash
# 将 SERVICE_ID 替换为：docker compose ... ps -q catmonitor 的输出
SERVICE_ID=$(docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu ps -q catmonitor)

docker exec "$SERVICE_ID" catmonitor stress run --bench stream -o table
docker exec "$SERVICE_ID" catmonitor stress run --bench hpcg -o table
docker exec "$SERVICE_ID" catmonitor stress run --bench hpl -o table
docker exec "$SERVICE_ID" catmonitor stress status -o table
```

浏览器访问：

```text
http://<node-address>:19322/
```

同一页面提供监控、Stress Run、Cancel 和 History。当前没有 operator
认证/RBAC，请仅向可信管理网络开放 `19322`。

## 6. 资源参数

CPU 线程、MPI rank 和 HPCG 规模在生成的 Compose profile 中固定，不能从 Web
编辑。需要调整时重新运行 generator，例如：

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image "$CATMONITOR_IMAGE" \
  --cpu-image "$CATMONITOR_CPU_STRESS_IMAGE" \
  --stream-threads 96 \
  --hpl-processes 8 \
  --hpl-threads 12 \
  --hpcg-processes 96 \
  --hpcg-threads 1 \
  --hpcg-runtime 60 \
  --enable-web \
  --force
```

参数必须按节点在线 CPU、MPI ABI 和内存容量评估。完整说明见
[STRESS_USER_GUIDE.md](../features/stress/STRESS_USER_GUIDE.md)。

## 7. 停止、启动和升级

Monitoring-only：

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml stop
```

CPU Stress：

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu stop
```

将 `stop` 换成 `start` 可恢复；换成 `down` 可删除容器和网络。命名卷默认保留，
不要在需要保留 snapshot/history 时使用 `down -v`。

切换到新的 Stress 镜像集合时必须重新生成 Stress 配置：

```text
OLD_STRESS_YAML_COMPATIBLE=false
```

## 8. 常见问题

- Web 显示 snapshot 未就绪：先检查 `19320/-/ready` 和 daemon 日志。
- Stress 显示未配置：确认使用了 generated Compose override，并执行 `stress doctor`。
- HPL/HPCG 不可用：检查 workload 镜像内 MPI 与 benchmark ABI，不要在 Web 编辑命令。
- Docker Socket 权限失败：仅 daemon 需要访问；不要把 socket 挂给 Web 或 DFeE。
- Registry 不可达：在联网节点 `docker save`，离线节点 `docker load` 同一标签和 Image ID 的镜像。
