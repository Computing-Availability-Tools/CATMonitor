# GPU 服务器（NVIDIA）容器化指南

适用于有 NVIDIA GPU 的服务器。使用 `catmonitor-gpu` 镜像（Debian/glibc，纯 Go 编译，兼容宿主机 nvidia-smi）。

> **不要使用 `catmonitor-generic`（Alpine/musl）运行 GPU 环境。** Alpine 使用 musl libc，无法运行 glibc 编译的 `nvidia-smi`，导致 GPU 指标采集静默失败。

---

## 1. 构建

### 构建镜像

```bash
cd CATMonitor
bash docker/build.sh gpu
```

输出镜像 `catmonitor-gpu`。多阶段构建：`golang:1.23` 编译 + `debian:bookworm-slim` 运行时。

### 代理与离线构建

```bash
export HTTP_PROXY=http://proxy.example.com:3128
export HTTPS_PROXY=http://proxy.example.com:3128
export NO_PROXY=127.0.0.1,localhost
export GOPROXY=https://proxy.golang.example,direct

bash docker/build.sh gpu

unset HTTP_PROXY HTTPS_PROXY NO_PROXY GOPROXY
```

完全离线节点优先加载已构建好的镜像：

```bash
# 联网节点
docker save -o catmonitor-gpu.tar catmonitor-gpu

# 离线节点
docker load -i catmonitor-gpu.tar
```

离线构建还需预先加载 `golang:1.23` 和 `debian:bookworm-slim`，并准备 Go module cache。

### Dockerfile 说明

| 文件 | 用途 |
|------|------|
| `Dockerfile.gpu` | GPU 控制镜像（多阶段，golang 编译 + Debian 运行时） |
| `catmonitor.yaml` | 容器版配置（打包在镜像中） |

---

## 2. 启动

### 方式一：Docker Compose（推荐）

GPU overlay 挂载宿主机的 `nvidia-smi` 和 NVIDIA 运行时库：

```bash
CATMONITOR_IMAGE=catmonitor-gpu \
docker compose \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.gpu.yml \
  up -d
```

启动全部三个容器：daemon + web + dfee。

`docker-compose.gpu.yml` 内容：

```yaml
services:
  catmonitor:
    volumes:
      - /usr/bin/nvidia-smi:/usr/bin/nvidia-smi:ro
      - /usr/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu:ro
    environment:
      - LD_LIBRARY_PATH=/usr/lib/x86_64-linux-gnu
```

> 挂载整个 `/usr/lib/x86_64-linux-gnu` 目录而非单个 `.so` 文件，因为 `nvidia-smi` 可能依赖多个 NVIDIA 库（libnvidia-ml.so、libcuda.so 等）。

#### 只启动部分服务

```bash
# daemon + dfee（跳过 web）
CATMONITOR_IMAGE=catmonitor-gpu \
docker compose \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.gpu.yml \
  up -d catmonitor dfee

# 只启动 daemon
CATMONITOR_IMAGE=catmonitor-gpu \
docker compose \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.gpu.yml \
  up -d catmonitor
```

> web/dfee 容器只读 snapshot，不需要 GPU 访问，镜像用 `catmonitor-gpu` 或 `catmonitor-generic` 均可。

### 方式二：手动 docker run

#### 步骤 1：创建卷

```bash
docker volume create cm-snapshot
docker volume create cm-data
```

#### 步骤 2：启动 daemon（挂载 nvidia-smi + NVIDIA 库）

```bash
docker run -d --name catmonitor --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v /usr/bin/nvidia-smi:/usr/bin/nvidia-smi:ro \
  -v /usr/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu:ro \
  -e LD_LIBRARY_PATH=/usr/lib/x86_64-linux-gnu \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  catmonitor-gpu
```

> - `-v /usr/bin/nvidia-smi`：挂载宿主机的 nvidia-smi（glibc 编译，需 Debian/glibc 运行时）
> - `-v /usr/lib/x86_64-linux-gnu`：挂载 NVIDIA 运行时库（libnvidia-ml.so、libcuda.so 等）
> - `-e LD_LIBRARY_PATH`：让动态链接器找到 NVIDIA 库
> - `--pid host` + `-v /:/host:ro`：共享宿主机 PID 命名空间读取 `/proc/1/mounts`，挂载根文件系统给 statfs 访问
> - `-v /etc/os-release:/etc/os-release:ro`：获取宿主机 OS 信息
> - `--privileged`：访问 `/dev/ipmi0`（ipmitool）、`/dev/sd*`（smartctl）、`/dev/nvidia*`（GPU）、`/proc`、`/sys`、SMBIOS（dmidecode）

#### 步骤 3：等待首次采集（6-9 秒）

```bash
docker exec catmonitor ls /var/lib/catmonitor/snapshot
# 预期：snapshot.json + snapshot_cpu.json + snapshot_gpu.json + ...
```

#### 步骤 4：启动 web

```bash
docker run -d --name catmonitor-web --network host --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  catmonitor-gpu \
  -addr=:19322 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -config=/etc/catmonitor/catmonitor.yaml
```

> `--network host` 后不需要 `-p` 端口映射。

#### 步骤 5：启动 dfee

```bash
# 基础模式（仅 SPA + API）
docker run -d --name catmonitor-dfee --network host --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  catmonitor-gpu \
  -snapshot-dir /var/lib/catmonitor/snapshot

# 含 Prometheus exporter + CSV 输出
docker run -d --name catmonitor-dfee --network host --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-csv:/var/lib/catmonitor/csv \
  catmonitor-gpu \
  -snapshot-dir /var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -exporter-port=9333 \
  -csv=enabled \
  -csv-dir=/var/lib/catmonitor/csv \
  -csv-interval=10s
```

### 方式三：只运行 dfee（daemon 在宿主机或其他容器）

```bash
# 基础模式
docker run -d --name dfee --network host --entrypoint /usr/local/bin/dfee \
  -v /var/lib/catmonitor/snapshot:/var/lib/catmonitor/snapshot:ro \
  catmonitor-gpu \
  -snapshot-dir /var/lib/catmonitor/snapshot

# 含 exporter + CSV
docker run -d --name dfee --network host --entrypoint /usr/local/bin/dfee \
  -v /var/lib/catmonitor/snapshot:/var/lib/catmonitor/snapshot:ro \
  -v /var/lib/catmonitor/csv:/var/lib/catmonitor/csv \
  catmonitor-gpu \
  -snapshot-dir /var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -exporter-port=9333 \
  -csv=enabled \
  -csv-dir=/var/lib/catmonitor/csv
```

---

## 3. 验证 GPU 采集

```bash
# 检查 snapshot 文件
docker exec catmonitor ls /var/lib/catmonitor/snapshot/snapshot_gpu.json

# 查看 GPU 指标
curl -s http://localhost:19322/api/snapshot | python3 -m json.tool | grep gpu | head -5

# Prometheus exporter
curl -s http://localhost:19320/metrics | grep gpu
```

如果 `snapshot_gpu.json` 不存在，参见[常见问题](#7-常见问题)排查。

---

## 4. 端口说明

| 容器端口 | 服务 | 端点 |
|---------|------|------|
| 19320 | daemon Prometheus exporter | `/metrics`、`/-/healthy`、`/-/ready` |
| 19322 | web 仪表盘 | `/`、`/api/snapshot`、`/api/collectors` |
| 19323 | dfee SPA | `/`、`/dfee/` |
| 9333 | dfee Prometheus exporter | `/metrics` |

---

## 5. 验证

```bash
# daemon
curl http://localhost:19320/-/healthy           # 200
curl http://localhost:19320/metrics | grep gpu    # GPU 指标

# web
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:19322/   # 200
curl -s http://localhost:19322/api/snapshot | head -c 120           # JSON

# dfee
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:19323/   # 200
curl -s http://localhost:19323/api/dfee | head -c 120           # dfee API
```

---

## 6. 配置修改

### 挂载自定义配置

容器内配置文件位置：

| 文件 | 容器路径 | 用途 |
|------|---------|------|
| `catmonitor.yaml` | `/etc/catmonitor/catmonitor.yaml` | 主配置（采集器/端口/功能开关等） |
| `metrics.yaml` | `/etc/catmonitor/metrics.yaml` | 指标目录（优先级/单位/采集间隔） |
| `features/web/metrics.yaml` | `/features/web/metrics.yaml` | web 特性指标范围 |
| `features/dfee/metrics.yaml` | `/features/dfee/metrics.yaml` | dfee 特性指标范围 |
| `features/health/metrics.yaml` | `/features/health/metrics.yaml` | 健康评估指标范围 |

以上文件均已打包在镜像中，挂载自定义文件覆盖即可：

```bash
docker run -d --name catmonitor --privileged --network host \
  -v /path/to/my-catmonitor.yaml:/etc/catmonitor/catmonitor.yaml:ro \
  -v /path/to/my-metrics.yaml:/etc/catmonitor/metrics.yaml:ro \
  ...其他参数...
  catmonitor-gpu
```

Docker Compose 用户取消 `docker-compose.yml` 中 volumes 段的注释，将宿主机文件挂载覆盖即可。

### 开启 straggler_output

```yaml
straggler_output:
  enabled: true
  data_dir: /var/lib/catmonitor/straggler
```

### 调整采集优先级

```yaml
collection:
  min_priority: low      # low(全采) | medium(跳过Low) | high(只采High)
```

---

## 7. 数据卷说明

| Volume | 写入方 | 读取方 | 内容 |
|--------|--------|--------|------|
| `cm-snapshot` | daemon | web, dfee | snapshot.json + snapshot_*.json |
| `cm-data` | daemon | — | JSONL 历史数据 |
| `cm-straggler` | daemon | — | straggler KPI 文件（可选） |
| `cm-csv` | dfee | — | dfee CSV 输出（可选，`-csv=enabled` 时） |

---

## 8. 停止与清理

```bash
# 停止全部容器
docker rm -f catmonitor catmonitor-web catmonitor-dfee

# 清理数据卷（保留数据则跳过）
docker volume rm cm-snapshot cm-data cm-csv

# 删除镜像
docker rmi catmonitor-gpu
```

---

## 9. 启动顺序

1. **先启动 daemon**（snapshot 生产者），等待 6-9 秒完成首次采集
2. **后启动 web/dfee**（snapshot 消费者），snapshot 就绪后即有数据

web/dfee 可在任意时刻拉起，只要 snapshot 已存在就有数据。若 snapshot 尚未就绪，web/dfee 返回 503，自动重试即可。

---

## 10. 常见问题

### Q: 没有 snapshot_gpu.json，GPU 指标为空

最常见原因：使用了 `catmonitor-generic`（Alpine）而非 `catmonitor-gpu`（Debian）。Alpine 使用 musl libc，无法运行 glibc 编译的 `nvidia-smi`。

解决：改用 `catmonitor-gpu` 镜像，并挂载 nvidia-smi + NVIDIA 库：

```bash
-v /usr/bin/nvidia-smi:/usr/bin/nvidia-smi:ro \
-v /usr/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu:ro \
-e LD_LIBRARY_PATH=/usr/lib/x86_64-linux-gnu \
```

验证 nvidia-smi 是否能在容器内运行：

```bash
docker exec catmonitor nvidia-smi --query-gpu=index,name --format=csv
```

### Q: 容器内 ipmitool 报错 "Unable to open /dev/ipmi0"

确保宿主机已加载 ipmi 内核模块：

```bash
sudo modprobe ipmi_devintf
sudo modprobe ipmi_si
ls /dev/ipmi0
```

### Q: dfee/web 容器输出 "snapshot not ready"

daemon 尚未完成首次采集。等待 6-9 秒后重试。检查 snapshot 是否已生成：

```bash
docker exec catmonitor ls /var/lib/catmonitor/snapshot/
```

### Q: Web 仪表盘只显示 eth0，MAC 地址相同

daemon 容器未使用 `--network host`，容器有自己的网络命名空间，`/sys/class/net/` 只显示虚拟网卡。加 `--network host` 重启 daemon 即可。

### Q: docker build 时 apt-get 很慢

Dockerfile 默认使用 Debian 官方源。管理员可临时设置代理：

```bash
export HTTP_PROXY=http://proxy.example.com:3128
export HTTPS_PROXY=http://proxy.example.com:3128
./docker/build.sh gpu
unset HTTP_PROXY HTTPS_PROXY
```

---

## 11. dfee Prometheus Exporter + Grafana

dfee 支持独立的 Prometheus exporter（`:9333`），输出 CPU/内存/磁盘/网络/GPU/机箱指标的 `node_*`/`ipmi_*` 格式，可直接接入 Prometheus + Grafana。

### dfee 参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-addr` | `:19323` | dfee SPA + API 监听地址 |
| `-snapshot-dir` | `/var/lib/catmonitor/snapshot` | daemon snapshot 目录 |
| `-exporter` | `disabled` | `enabled` 开启 Prometheus exporter |
| `-exporter-port` | `9333` | exporter 监听端口 |
| `-csv` | `disabled` | `enabled` 开启 CSV 输出 |
| `-csv-dir` | `/var/lib/catmonitor/csv` | CSV 输出目录 |
| `-csv-interval` | `10s` | CSV 写入间隔 |
| `-max-runtime` | `0` | 最大运行时长（如 `10m`、`1h`），0=永久 |

### Docker Compose

`docker-compose.yml` 中 dfee 服务已默认开启 exporter。如需关闭，将 `-exporter=enabled` 改为 `-exporter=disabled`。

### 手动 Docker 启动

```bash
docker run -d --name dfee --network host --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-csv:/var/lib/catmonitor/csv \
  catmonitor-gpu \
  -snapshot-dir /var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -exporter-port=9333 \
  -csv=enabled \
  -csv-dir=/var/lib/catmonitor/csv
```

### 安装 Prometheus + Grafana

```bash
# 1. Prometheus
docker pull prom/prometheus

mkdir -p $PWD/prometheus/data
touch $PWD/prometheus/prometheus.yml
chown -R 65534:65534 $PWD/prometheus/data
chown 65534:65534 $PWD/prometheus/prometheus.yml

docker run -d \
  --name prometheus \
  -v $PWD/prometheus/data:/prometheus \
  -v $PWD/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml \
  -p 9090:9090 \
  prom/prometheus

# 编辑配置（targets 改为 dfee exporter 地址）
# vim $PWD/prometheus/prometheus.yml
# scrape_configs:
#   - job_name: "CATMonitor"
#     scrape_interval: 2s
#     static_configs:
#       - targets: ["<dfee_exporter_ip>:9333"]
#         labels:
#           instance: <dfee_exporter_ip>

# 2. Grafana
docker pull grafana/grafana

mkdir $PWD/grafana-storage
chown -R 472:472 $PWD/grafana-storage

docker run -d \
  --name=grafana \
  --restart=always \
  -p 3000:3000 \
  -v $PWD/grafana-storage:/var/lib/grafana \
  grafana/grafana
```

### 导入 Grafana Dashboard

1. 浏览器访问 `http://localhost:3000`（默认账号 `admin / admin`）
2. **Configuration → Data Sources → Add data source → Prometheus**
3. URL 填入 `http://<prometheus_ip>:9090`，点击 **Save & Test**
4. **Dashboards → Import → Upload JSON file**
5. 选择 `features/dfee/grafana-dashboard.json`
6. 在导入页面选择 Prometheus 数据源，点击 **Import**

> 完整使用文档见 `features/dfee/USAGE.md`。
