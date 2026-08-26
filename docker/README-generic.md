# 通用服务器（纯 CPU）容器化指南

适用于无 NPU、无 NVIDIA GPU 的通用服务器。使用 `catmonitor-generic` 镜像（Alpine/musl，纯 Go，镜像最小）。

---

## 1. 构建

### 构建镜像

```bash
cd CATMonitor
bash docker/build.sh generic
```

输出镜像 `catmonitor-generic`。多阶段构建：`golang:1.23-alpine` 编译 + `alpine:latest` 运行时。

### 代理与离线构建

```bash
export HTTP_PROXY=http://proxy.example.com:3128
export HTTPS_PROXY=http://proxy.example.com:3128
export NO_PROXY=127.0.0.1,localhost
export GOPROXY=https://proxy.golang.example,direct

bash docker/build.sh generic

unset HTTP_PROXY HTTPS_PROXY NO_PROXY GOPROXY
```

完全离线节点优先加载已构建好的镜像：

```bash
# 联网节点
docker save -o catmonitor-generic.tar catmonitor-generic

# 离线节点
docker load -i catmonitor-generic.tar
```

离线构建还需预先加载 `golang:1.23-alpine` 和 `alpine:latest`，并准备 Go module cache。

### Dockerfile 说明

| 文件 | 用途 |
|------|------|
| `Dockerfile.generic` | 通用控制镜像（多阶段，golang 编译 + Alpine 运行时） |
| `catmonitor.yaml` | 容器版配置（打包在镜像中） |

---

## 2. 启动

### 方式一：统一安装入口（推荐）

```bash
sudo install -d -m 0750 /etc/catmonitor
sudo install -m 0640 docker/catmonitor.yaml /etc/catmonitor/catmonitor.yaml
sudo make install-installer

sudo catmonitor-install --profile monitoring --action plan
sudo catmonitor-install --profile monitoring
```

`catmonitor-install` 支持 `monitoring`、`cpu-stress`、`ascend-a2`、`ascend-a3`。默认动作 `up` 只编排已有镜像与资产，不构建镜像、编译 benchmark、编辑 YAML 或运行压测。

### 方式二：Docker Compose

```bash
CATMONITOR_IMAGE=catmonitor-generic \
docker compose -f docker/docker-compose.yml up -d
```

启动全部三个容器：daemon + web + dfee。

#### 只启动部分服务

```bash
# daemon + dfee（跳过 web）
CATMONITOR_IMAGE=catmonitor-generic \
docker compose -f docker/docker-compose.yml up -d catmonitor dfee

# 只启动 daemon
CATMONITOR_IMAGE=catmonitor-generic \
docker compose -f docker/docker-compose.yml up -d catmonitor
```

### 方式三：手动 docker run

#### 步骤 1：创建卷

```bash
docker volume create cm-snapshot
docker volume create cm-data
```

#### 步骤 2：启动 daemon

```bash
docker run -d --name catmonitor --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  catmonitor-generic
```

> - `--pid host` + `-v /:/host:ro`：共享宿主机 PID 命名空间读取 `/proc/1/mounts`，挂载根文件系统给 statfs 访问，使磁盘空间指标反映宿主机真实文件系统而非容器 bind mount
> - `-v /etc/os-release:/etc/os-release:ro`：获取宿主机 OS 信息
> - `--privileged`：访问 `/dev/ipmi0`（ipmitool）、`/dev/sd*`（smartctl）、`/proc`、`/sys`、SMBIOS（dmidecode）

#### 步骤 3：等待首次采集（6-9 秒）

```bash
docker exec catmonitor ls /var/lib/catmonitor/snapshot
# 预期：snapshot.json + snapshot_cpu.json + snapshot_memory.json + ...
```

#### 步骤 4：启动 web

```bash
docker run -d --name catmonitor-web --network host --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  catmonitor-generic \
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
  catmonitor-generic \
  -snapshot-dir /var/lib/catmonitor/snapshot

# 含 Prometheus exporter + CSV 输出
docker run -d --name catmonitor-dfee --network host --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-csv:/var/lib/catmonitor/csv \
  catmonitor-generic \
  -snapshot-dir /var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -exporter-port=9333 \
  -csv=enabled \
  -csv-dir=/var/lib/catmonitor/csv \
  -csv-interval=10s
```

### 方式四：只运行 dfee（daemon 在宿主机或其他容器）

```bash
# 基础模式
docker run -d --name dfee --network host --entrypoint /usr/local/bin/dfee \
  -v /var/lib/catmonitor/snapshot:/var/lib/catmonitor/snapshot:ro \
  catmonitor-generic \
  -snapshot-dir /var/lib/catmonitor/snapshot

# 含 exporter + CSV
docker run -d --name dfee --network host --entrypoint /usr/local/bin/dfee \
  -v /var/lib/catmonitor/snapshot:/var/lib/catmonitor/snapshot:ro \
  -v /var/lib/catmonitor/csv:/var/lib/catmonitor/csv \
  catmonitor-generic \
  -snapshot-dir /var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -exporter-port=9333 \
  -csv=enabled \
  -csv-dir=/var/lib/catmonitor/csv
```

---

## 3. 端口说明

| 容器端口 | 服务 | 端点 |
|---------|------|------|
| 19320 | daemon Prometheus exporter | `/metrics`、`/-/healthy`、`/-/ready` |
| 19322 | web 仪表盘 | `/`、`/api/snapshot`、`/api/collectors` |
| 19323 | dfee SPA | `/`、`/dfee/` |
| 9333 | dfee Prometheus exporter | `/metrics` |

---

## 4. 验证

```bash
# daemon
curl http://localhost:19320/-/healthy           # 200
curl http://localhost:19320/metrics | head -5

# web
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:19322/   # 200
curl -s http://localhost:19322/api/snapshot | head -c 120           # JSON

# dfee
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:19323/   # 200
curl -s http://localhost:19323/api/dfee | head -c 120           # dfee API
```

---

## 5. 配置修改

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
  catmonitor-generic
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

## 6. 数据卷说明

| Volume | 写入方 | 读取方 | 内容 |
|--------|--------|--------|------|
| `cm-snapshot` | daemon | web, dfee | snapshot.json + snapshot_*.json |
| `cm-data` | daemon | — | JSONL 历史数据 |
| `cm-straggler` | daemon | — | straggler KPI 文件（可选） |
| `cm-csv` | dfee | — | dfee CSV 输出（可选，`-csv=enabled` 时） |

---

## 7. 停止与清理

```bash
# 停止全部容器
docker rm -f catmonitor catmonitor-web catmonitor-dfee

# 清理数据卷（保留数据则跳过）
docker volume rm cm-snapshot cm-data cm-csv

# 删除镜像
docker rmi catmonitor-generic
```

---

## 8. 启动顺序

1. **先启动 daemon**（snapshot 生产者），等待 6-9 秒完成首次采集
2. **后启动 web/dfee**（snapshot 消费者），snapshot 就绪后即有数据

web/dfee 可在任意时刻拉起，只要 snapshot 已存在就有数据。若 snapshot 尚未就绪，web/dfee 返回 503，自动重试即可。

---

## 9. 常见问题

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

Dockerfile 默认使用官方源。管理员可临时设置代理，构建脚本只转发已存在的代理变量，不保存其值：

```bash
export HTTP_PROXY=http://proxy.example.com:3128
export HTTPS_PROXY=http://proxy.example.com:3128
./docker/build.sh generic
unset HTTP_PROXY HTTPS_PROXY
```

---

## 10. dfee Prometheus Exporter + Grafana

dfee 支持独立的 Prometheus exporter（`:9333`），输出 CPU/内存/磁盘/网络/机箱指标的 `node_*`/`ipmi_*` 格式，可直接接入 Prometheus + Grafana。

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
  catmonitor-generic \
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
