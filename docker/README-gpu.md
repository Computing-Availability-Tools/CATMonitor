# CATMonitor：NVIDIA GPU 节点

适用于带 NVIDIA GPU 的 Linux 节点。当前提供 NVIDIA Monitoring 和可选 CPU Stress，
不提供 GPU workload 压测插件。

> 当前程序内部版本是 `0.3.5`，当前 ARM64 pre-release 镜像标签是
> `arm64-v0.3.5-stress`，目标发布线是 `v0.3.6`。
>
> GPU pre-release 镜像当前为 Private，拉取前需要完成 GHCR 身份认证。

## 1. 前置条件

- Linux/amd64 或 Linux/arm64；
- Docker Engine 与 Docker Compose v2；
- 宿主机 `nvidia-smi` 可执行；
- Docker 可读取宿主机 NVIDIA 动态库路径。

```bash
nvidia-smi
docker version
docker compose version
```

## 2. 获取 ARM64 Stress pre-release 镜像

```bash
git clone https://github.com/Computing-Availability-Tools/CATMonitor.git
cd CATMonitor
git checkout refactor/unified-stress-platform

export CATMONITOR_RELEASE='arm64-v0.3.5-stress'
export CATMONITOR_REGISTRY='ghcr.io/spike677'
export CATMONITOR_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-gpu:${CATMONITOR_RELEASE}"
export CATMONITOR_CPU_STRESS_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-stress-cpu:${CATMONITOR_RELEASE}"
```

Monitoring-only：

```bash
docker pull "$CATMONITOR_IMAGE"
```

启用 CPU Stress 时再执行：

```bash
docker pull "$CATMONITOR_CPU_STRESS_IMAGE"
```

需要从源码构建 GPU Control 或 CPU workload、配置 Debian mirror，或制作 RC 镜像时，
请使用 [镜像构建与发布开发者指南](DEVELOPER_GUIDE.md)。

## 3. NVIDIA Monitoring

```bash
export CATMONITOR_CONFIG="$PWD/docker/catmonitor.yaml"

CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_CONFIG="$CATMONITOR_CONFIG" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  -f docker/docker-compose.gpu.yml \
  up -d
```

应创建 `catmonitor`、`web`、`dfee` 三个服务：

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  -f docker/docker-compose.gpu.yml \
  ps

curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19322/ >/dev/null
curl -fsS http://127.0.0.1:19323/ >/dev/null
```

确认 GPU snapshot 与 exporter：

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  -f docker/docker-compose.gpu.yml \
  exec catmonitor \
  sh -c 'test -s /var/lib/catmonitor/snapshot/snapshot_gpu.json'

curl -fsS http://127.0.0.1:19320/metrics | grep -i gpu | head
```

### 3.1 手工 `docker run`（Monitoring-only 兼容）

Compose 是推荐方式；不能使用 Compose 时仍可用下面的旧部署方式。它只启动三个
Monitoring 容器，不挂 Docker Socket/control socket，也不需要 CPU workload 镜像。
以下 NVIDIA 库目录按节点实际架构调整。

```bash
export NVIDIA_LIB_DIR=/usr/lib/x86_64-linux-gnu

docker volume create cm-snapshot
docker volume create cm-data
docker volume create cm-straggler
docker volume create cm-csv

docker run -d --name catmonitor --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v /usr/bin/nvidia-smi:/usr/bin/nvidia-smi:ro \
  -v "$NVIDIA_LIB_DIR:$NVIDIA_LIB_DIR:ro" \
  -e "LD_LIBRARY_PATH=$NVIDIA_LIB_DIR" \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  -v cm-straggler:/var/lib/catmonitor/straggler \
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
docker exec catmonitor test -s /var/lib/catmonitor/snapshot/snapshot_gpu.json
curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19322/ >/dev/null
curl -fsS http://127.0.0.1:19323/ >/dev/null
```

Web 中 Stress 配置会显示为未启用；Monitoring 页面不受影响。旧 Monitoring YAML
可以完全没有顶层 `stress:` 段。

### 3.2 只启动部分 Monitoring 服务

```bash
# daemon + DFeE（跳过 Web）
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_CONFIG="$CATMONITOR_CONFIG" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  -f docker/docker-compose.gpu.yml \
  up -d catmonitor dfee

# 只启动 daemon
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_CONFIG="$CATMONITOR_CONFIG" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  -f docker/docker-compose.gpu.yml \
  up -d catmonitor
```

### 3.3 自定义 Monitoring 配置

Monitoring 公共配置路径保持不变：

| 文件 | 容器内路径 | 所有者 |
|---|---|---|
| 主配置 | `/etc/catmonitor/catmonitor.yaml` | daemon |
| 指标目录 | `/etc/catmonitor/metrics.yaml` | daemon |

Compose 通过 `CATMONITOR_CONFIG` 挂载主配置。手工启动时如需替换两份配置，把下面
两个只读 bind mount 加到 `catmonitor` 容器；Web/DFeE 只消费 snapshot，不重复读取
daemon 配置。

```bash
-v /path/to/my-catmonitor.yaml:/etc/catmonitor/catmonitor.yaml:ro \
-v /path/to/my-metrics.yaml:/etc/catmonitor/metrics.yaml:ro
```

旧 Monitoring YAML 不含 `stress:` 时仍可启动，且不会创建 Stress Manager、
`control.sock` 或 Docker executor。旧 Stress V1 YAML 不兼容 V2。

### 3.4 独立运行 DFeE

daemon 已在宿主机写入 `/var/lib/catmonitor/snapshot` 时：

```bash
sudo install -d -m 0750 /var/lib/catmonitor/csv

docker run -d --name catmonitor-dfee --network host \
  --entrypoint /usr/local/bin/dfee \
  -v /var/lib/catmonitor/snapshot:/var/lib/catmonitor/snapshot:ro \
  -v /var/lib/catmonitor/csv:/var/lib/catmonitor/csv \
  "$CATMONITOR_IMAGE" \
  -addr=:19323 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled -exporter-port=9333 \
  -csv=disabled -csv-dir=/var/lib/catmonitor/csv
```

daemon 位于另一个容器时改为共享 `cm-snapshot` named volume。DFeE 不挂 NVIDIA
设备、NVIDIA 库或 Docker Socket，它消费 daemon 已生成的 snapshot。

### 3.5 Monitoring 端口与数据

| 端口 | 组件 | 用途 |
|---:|---|---|
| `19320` | daemon | `/metrics`、`/-/healthy`、`/-/ready` |
| `19321` | faultsub | 启用后提供故障订阅 REST API |
| `19322` | Web | Monitoring；启用 V2 Stress 后同时提供 Run/Cancel/History |
| `19323` | DFeE | 能效页面与 API |
| `9333` | DFeE exporter | Prometheus metrics |

| 卷/宿主目录 | 容器内路径 | 用途 |
|---|---|---|
| `cm-snapshot` | `/var/lib/catmonitor/snapshot` | daemon 写，Web/DFeE 只读 |
| `cm-data` | `/var/lib/catmonitor/data` | daemon 原始 JSONL；按 retention 管理 |
| `cm-straggler`（可选） | `/var/lib/catmonitor/straggler` | straggler 专用输出 |
| `cm-csv` | `/var/lib/catmonitor/csv` | DFeE CSV |

要启用 faultsub，在 daemon 的 `catmonitor.yaml` 中设置：

```yaml
faultsub:
  enabled: true
  rest_addr: ":19321"
  webhook_timeout: 5s
  webhook_retry: 1
  event_buffer: 1024
```

可选的 straggler 专用输出仍使用原路径：

```yaml
straggler_output:
  enabled: true
  data_dir: /var/lib/catmonitor/straggler
  retention: 360h
  flush_interval: 60s
```

验证：

```bash
curl -fsS http://127.0.0.1:19321/-/ready
curl -fsS http://127.0.0.1:19321/faultsub/snapshot
```

### 3.6 DFeE Exporter、Prometheus 与 Grafana

```bash
curl -fsS http://127.0.0.1:9333/metrics | head
```

Prometheus 增加：

```yaml
scrape_configs:
  - job_name: CATMonitor
    scrape_interval: 2s
    static_configs:
      - targets: ["<dfee_exporter_ip>:9333"]
```

在 `http://<prometheus-address>:9090/targets` 确认 target 为 `UP`，再在 Grafana 导入
`features/dfee/grafana-dashboard.json`。完整 DFeE 参数与独立二进制用法见
[DFeE 使用文档](../features/dfee/USAGE.md)。

## 4. NVIDIA Monitoring + CPU Stress

### 4.1 Generate

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

以上是已验证的 96 核 profile；其他节点必须按在线 CPU 数和 HPL `P×Q` 调整。

### 4.2 Compose

启动四个服务：

```bash
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_STRESS_STATE_DIR=/var/lib/catmonitor/stress \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.gpu.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu \
  up -d
```

该组合不会创建 NPU workload，也不会将 Docker Socket 挂给 Web/DFeE。

### 4.3 Manual `docker run`

不能使用 Compose 时，下面命令完整创建 CPU workload、NVIDIA daemon、Web 与 DFeE
共 4 个容器。按节点实际架构设置 NVIDIA 动态库目录。

```bash
export NVIDIA_LIB_DIR=/usr/lib/aarch64-linux-gnu

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
  -v /usr/bin/nvidia-smi:/usr/bin/nvidia-smi:ro \
  -v "$NVIDIA_LIB_DIR:$NVIDIA_LIB_DIR:ro" \
  -e "LD_LIBRARY_PATH=$NVIDIA_LIB_DIR" \
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

只有 daemon 挂 Docker Socket；CPU workload 使用 `network none`，Web/DFeE 不挂
socket，也不会创建 NPU workload。

## 5. 验证与运行

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.gpu.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu \
  exec catmonitor \
  catmonitor stress doctor -c /etc/catmonitor/catmonitor.yaml -o table
```

获取 daemon 容器 ID 后运行：

```bash
SERVICE_ID=$(docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.gpu.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu ps -q catmonitor)

docker exec "$SERVICE_ID" catmonitor stress run --bench stream -o table
docker exec "$SERVICE_ID" catmonitor stress run --bench hpcg -o table
docker exec "$SERVICE_ID" catmonitor stress run --bench hpl -o table
```

Web：

```text
http://<node-address>:19322/
```

`19322` 同时提供 Monitoring、Stress Run/Cancel/History。当前没有 Web operator
认证/RBAC，应限制为可信管理网络。

## 6. 配置边界

- GPU workload plugin：当前不支持；
- CPU benchmark 参数由生成的 Compose profile 固定；
- Web 只能选项目并缩短单次超时，不能编辑脚本、命令或 MPI 参数；
- daemon 是唯一 Controller，Web 与 DFeE 不挂 Docker Socket。

高级 CPU 参数见
[STRESS_USER_GUIDE.md](../features/stress/STRESS_USER_GUIDE.md)。

## 7. 停止与升级

Monitoring-only：

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  -f docker/docker-compose.gpu.yml stop
```

CPU Stress：

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.gpu.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu stop
```

换成 `start` 可恢复，换成 `down` 可删除容器。保留数据时不要使用 `down -v`。
切换到新的 Stress 镜像集合时必须重新生成 Stress 配置：

```text
OLD_STRESS_YAML_COMPATIBLE=false
```

## 8. 常见问题

- 无 `snapshot_gpu.json`：先在宿主机运行 `nvidia-smi`，再检查 GPU overlay 的挂载路径。
- Web snapshot 未就绪：检查 daemon `19320/-/ready` 与日志。
- Stress 未配置：确认 generated override 已加入 Compose 命令。
- CPU benchmark unavailable：执行 `stress doctor` 检查 workload、MPI ABI 和资产。
- Registry 不可达：使用 `docker save/load` 传输同一标签和 Image ID 的镜像。
