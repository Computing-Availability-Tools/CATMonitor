# CATMonitor v0.3.6：Generic CPU 节点

适用于没有 NVIDIA GPU 或 Ascend NPU 的 Linux 节点。推荐部署 Monitoring，按需增加
STREAM、HPL、HPCG CPU Stress。

## 1. 前置条件

- Linux/amd64 或 Linux/arm64；
- Docker Engine；
- Docker Compose v2（`docker compose version`）；
- 访问镜像 registry，或已离线导入相同的 v0.3.6 镜像。

```bash
docker version
docker compose version
uname -m
```

## 2. 获取 v0.3.6

```bash
git clone https://github.com/Computing-Availability-Tools/CATMonitor.git
cd CATMonitor
git checkout <v0.3.6-release-ref>
```

v0.3.6 最终镜像名如下；发布前将 `<registry>` 替换为正式 namespace：

```bash
export CATMONITOR_RELEASE='v0.3.6-rc.<shortsha>'
export CATMONITOR_REGISTRY='<registry>'
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

需要从源码构建 Control 时：

```bash
# 可选；不设置时继续使用 Alpine 官方仓库
export CATMONITOR_ALPINE_MIRROR='https://mirror.example.com/alpine'
bash docker/build.sh generic
docker tag catmonitor-generic "$CATMONITOR_IMAGE"
```

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

先生成节点配置。该命令不构建镜像、不启动容器，也不执行压测：

```bash
sudo install -d -m 0750 \
  /etc/catmonitor/generated-stress \
  /var/lib/catmonitor/stress

sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image "$CATMONITOR_IMAGE" \
  --cpu-image "$CATMONITOR_CPU_STRESS_IMAGE" \
  --enable-web \
  --force
```

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

升级到 v0.3.6 时必须重新生成 Stress 配置：

```text
OLD_STRESS_YAML_COMPATIBLE=false
```

## 8. 常见问题

- Web 显示 snapshot 未就绪：先检查 `19320/-/ready` 和 daemon 日志。
- Stress 显示未配置：确认使用了 generated Compose override，并执行 `stress doctor`。
- HPL/HPCG 不可用：检查 workload 镜像内 MPI 与 benchmark ABI，不要在 Web 编辑命令。
- Docker Socket 权限失败：仅 daemon 需要访问；不要把 socket 挂给 Web 或 DFeE。
- Registry 不可达：在联网节点 `docker save`，离线节点 `docker load` 同一 v0.3.6 镜像。
