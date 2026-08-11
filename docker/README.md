# CATMonitor 容器化使用文档

## 1. 概述

CATMonitor 容器化方案支持两种镜像：

| 镜像 | 适用环境 | 说明 |
|------|---------|------|
| `catmonitor-npu` | 有 Ascend NPU | CGo 编译，链接 libdcmi.so，采集 119 项 NPU 指标 |
| `catmonitor-generic` | 无 NPU（纯 CPU/GPU） | 纯 Go 编译，不依赖 NPU 驱动 |

三个服务可以组合使用：

| 服务 | 容器端口 | 功能 |
|------|---------|------|
| `catmonitor` (daemon) | 9100, 9101 | 采集指标 + Prometheus 导出 + snapshot 写入 + faultsub |
| `web` | 9527 | Web 仪表盘（读 snapshot） |
| `dfee` | 9528, 9333 | 能效监控 SPA + Prometheus 导出 |

daemon 是 snapshot 唯一生产者；web/dfee 是只读消费者，不自行采集。三容器共享一个 snapshot 卷。

## 2. 构建

### 构建镜像

```bash
cd CATMonitor
docker/docker/build.sh          # 自动检测 NPU driver
```

或手动指定：

```bash
docker/docker/build.sh npu      # 强制 NPU 镜像
docker/docker/build.sh generic  # 强制通用镜像
```

### NPU 镜像构建说明

NPU 镜像采用**两步构建**，且**必须使用 Debian（glibc）基础镜像**，因为 `libdcmi.so` 是 glibc 链接的，无法在 Alpine（musl libc）上运行：

1. **编译**：启动 `golang:1.23`（Debian/glibc）容器，挂载宿主机 Ascend driver，在容器内用 CGo 编译 `catmonitor`（`-tags dcmi`）+ `dfee` + `web`。
2. **打包**：将编译好的二进制 COPY 进 `debian:bookworm-slim` 运行时镜像。

编译时 `CGO_LDFLAGS` 加 `-Wl,--allow-shlib-undefined`（`build.sh` 已配置），因为 Debian 的 `ld` 默认不递归解析共享库的传递依赖。

运行时需要设置 `LD_LIBRARY_PATH` 指向 driver、common、toolkit 和 nnae 库目录：

```
LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:/usr/local/Ascend/ascend-toolkit/latest/aarch64-linux/lib64:/usr/local/Ascend/nnae/latest/lib64
```

同时需要挂载 driver、nnae 和 toolkit 目录（`libdcmi.so`、`libc_sec.so`、`libmmpa.so` 等分布在不同目录中）。

### Dockerfile 说明

| 文件 | 用途 |
|------|------|
| `Dockerfile.npu` | NPU 运行时镜像（debian + 预编译二进制） |
| `Dockerfile.generic` | 通用镜像（多阶段，golang 编译 + alpine 运行时） |
| `catmonitor.yaml` | 容器版配置（打包在镜像中） |

## 3. 启动

### 方式一：Docker Compose 一键启动（推荐）

```bash
docker compose -f docker/docker-compose.yml up -d
```

启动全部三个容器：daemon + web + dfee。

#### 只启动部分服务

```bash
# daemon + dfee（跳过 web）
docker compose -f docker/docker-compose.yml up -d catmonitor dfee

# 只启动 daemon
docker compose -f docker/docker-compose.yml up -d catmonitor
```

### 方式二：手动 docker run（无 compose 或 Docker 18.09）

#### 步骤 1：创建卷

```bash
docker volume create cm-snapshot
docker volume create cm-data
```

#### 步骤 2：启动 daemon

```bash
docker run -d --name catmonitor --privileged \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -v /usr/local/Ascend/nnae:/usr/local/Ascend/nnae:ro \
  -v /usr/local/Ascend/ascend-toolkit:/usr/local/Ascend/ascend-toolkit:ro \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:/usr/local/Ascend/ascend-toolkit/latest/aarch64-linux/lib64:/usr/local/Ascend/nnae/latest/lib64 \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  -p 9100:9100 \
  catmonitor-npu
```

> NPU 环境专用参数：
> - `--network host`：共享宿主机网络命名空间（必须，否则网卡信息错误）
> - `-v /usr/local/Ascend/driver` + `-v /usr/local/Ascend/nnae` + `-v /usr/local/Ascend/ascend-toolkit`：挂载驱动
> - `-e LD_LIBRARY_PATH`：让 glibc 找到 libdcmi.so、libc_sec.so、libmmpa.so 等依赖
> - `--privileged` 已包含 NPU 设备访问权限，无需额外 `--device`
>
> 非 NPU 环境去掉 driver/nnae/toolkit/LD_LIBRARY_PATH 三行，镜像名改为 `catmonitor-generic`。
> `--network host` 仍然建议保留（读真实网卡）。

#### 步骤 3：等待首次采集（6-9 秒）

```bash
docker exec catmonitor ls /var/lib/catmonitor/snapshot
# 预期：snapshot.json + snapshot_cpu.json + snapshot_npu.json + ...
```

#### 步骤 4：启动 web

```bash
docker run -d --name catmonitor-web --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -p 9527:9527 \
  catmonitor-npu -snapshot-dir /var/lib/catmonitor/snapshot
```

> `--entrypoint /usr/local/bin/web` 覆盖镜像默认的 daemon 入口。

#### 步骤 5：启动 dfee

```bash
docker run -d --name catmonitor-dfee --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -p 9528:9528 -p 9333:9333 \
  catmonitor-npu -exporter=enabled -snapshot-dir /var/lib/catmonitor/snapshot
```

### 方式三：只运行 dfee（daemon 在宿主机或其他容器）

```bash
docker run -d --name dfee --entrypoint /usr/local/bin/dfee \
  -v /var/lib/catmonitor/snapshot:/var/lib/catmonitor/snapshot:ro \
  -p 9528:9528 -p 9333:9333 \
  catmonitor-npu \
  -exporter=enabled -snapshot-dir /var/lib/catmonitor/snapshot
```

## 4. 端口说明

| 容器端口 | 服务 | 端点 |
|---------|------|------|
| 9100 | daemon Prometheus exporter | `/metrics`、`/-/healthy`、`/-/ready` |
| `9101` | faultsub REST API（可选） | `/faultsub/events` 等 |
| 9527 | web 仪表盘 | `/`、`/api/snapshot`、`/api/collectors` |
| 9528 | dfee SPA | `/`、`/dfee/` |

如需自定义端口映射（如映射到不同主机端口）：

```bash
docker run -d --name catmonitor --privileged \
  -p 4900:9100 \
  ...其他参数...
  catmonitor-npu
```

## 5. 验证

```bash
# daemon
curl http://localhost:9100/-/healthy           # 200
curl http://localhost:9100/metrics | grep npu    # NPU 指标

# web
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:9527/   # 200
curl -s http://localhost:9527/api/snapshot | head -c 120           # JSON

# dfee
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:9528/   # 200
curl -s http://localhost:9333/metrics | grep node_load1           # 能效指标
```

## 6. NPU 环境配置

### 设备挂载

`--privileged` 模式下容器自动获得所有设备访问权限（包括 `/dev/davinci*`、`/dev/ipmi0`、`/dev/sd*`），无需额外 `--device`。

如需收紧权限（不用 `--privileged`），可以改为：

```bash
docker run -d --name catmonitor \
  --device /dev/davinci0 \
  --device /dev/davinci_manager \
  --device /dev/ipmi0 \
  --cap-add SYS_ADMIN \
  ...其他参数...
```

按实际设备调整，`ls /dev/davinci*` 查看可用设备。

### 权限

容器需要 `--privileged` 模式以访问：
- `/dev/ipmi0`（ipmitool）
- `/dev/sd*`（smartctl）
- `/dev/davinci*`（NPU DCMI）
- `/proc`、`/sys`（系统指标）
- SMBIOS（dmidecode）

### 网络模式

**必须使用 `--network host`**（daemon 容器），否则容器内 `/sys/class/net/` 只显示虚拟网卡 eth0，web 仪表盘的网卡信息会显示错误。

`--network host` 后容器直接使用宿主机网络栈：
- 不需要 `-p` 端口映射，容器内端口直接是宿主机端口
- 能读到宿主机真实网卡（enp125s0f1 等）和真实 MAC 地址

web 和 dfee 容器也建议加 `--network host`，避免端口映射冲突。

### 运行时库依赖

`libdcmi.so` 是 glibc 链接的，运行时需要：
- 挂载 `/usr/local/Ascend/driver`（libdcmi.so 本体）
- 挂载 `/usr/local/Ascend/nnae`（libc_sec.so、libmmpa.so 依赖）
- 挂载 `/usr/local/Ascend/ascend-toolkit`（toolkit 库）
- 设置 `LD_LIBRARY_PATH` 指向四个库目录

## 7. 非 NPU 环境

```bash
# 构建
docker/docker/build.sh generic

# 启动（不需要 driver/nnae 挂载、device、LD_LIBRARY_PATH）
docker run -d --name catmonitor --privileged \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  -p 9100:9100 \
  catmonitor-generic

docker run -d --name catmonitor-web --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -p 9527:9527 \
  catmonitor-generic -snapshot-dir /var/lib/catmonitor/snapshot

docker run -d --name catmonitor-dfee --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -p 9528:9528 -p 9333:9333 \
  catmonitor-generic -exporter=enabled -snapshot-dir /var/lib/catmonitor/snapshot
```

如果使用 docker-compose，修改 `docker-compose.yml`：
1. `dockerfile` 改为 `Dockerfile.generic`
2. `image` 改为 `catmonitor-generic`
3. 删除 `devices`、NPU driver/nnae/toolkit `volumes`、`LD_LIBRARY_PATH`

## 8. 配置修改

### 挂载自定义配置

```bash
docker run -d --name catmonitor --privileged \
  -v /path/to/my-catmonitor.yaml:/etc/catmonitor/catmonitor.yaml:ro \
  ...其他参数...
  catmonitor-npu
```

### 开启 faultsub（故障订阅推送）

faultsub 是 NPU 故障检测与推送机制，运行在 daemon 内部。开启后，daemon 周期性采集 NPU 指标时自动检测故障，并推送给已订阅的 webhook。

**配置**：

```yaml
faultsub:
  enabled: true
  rest_addr: ":9101"            # REST API 监听地址
  webhook_timeout: 5s           # webhook 推送超时
  webhook_retry: 1             # 失败重试次数
  event_buffer: 1024           # 事件环形缓冲区大小
  defaults:
    debounce_ms: 0             # 订阅去抖窗口（毫秒）
    min_severity: "warning"    # 最低推送级别
  rules:                       # 故障检测规则开关
    card_drop: true            # NPU 掉卡
    npu_health: true           # NPU 健康状态异常
    npu_error_code: true       # NPU 错误码
    hbm_uce: true              # HBM 不可纠正错误
    ddr_uce: true              # DDR 不可纠正错误
    roce_link_down: true      # RoCE 链路断开
    driver_unhealthy: false   # 驱动不健康
```

**REST API 端点**（端口 9101）：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/-/healthy` | 健康检查 |
| GET | `/-/ready` | 就绪检查 |
| GET | `/faultsub/types` | 支持的故障类型列表 |
| GET | `/faultsub/snapshot` | 当前故障快照 |
| GET | `/faultsub/events` | 最近事件列表 |
| POST | `/faultsub/events` | 手动注入事件 |
| POST | `/faultsub/subscriptions` | 创建 webhook 订阅 |
| GET | `/faultsub/subscriptions` | 列出所有订阅 |
| GET | `/faultsub/subscriptions/{id}` | 查询指定订阅 |
| DELETE | `/faultsub/subscriptions/{id}` | 删除订阅 |

**使用示例**：

```bash
# 创建 webhook 订阅（故障事件推送到指定 URL）
curl -X POST http://localhost:9101/faultsub/subscriptions \
  -H "Content-Type: application/json" \
  -d '{"webhook_url": "http://my-fault-manager:8080/fault", "types": ["card_drop", "npu_health"]}'

# 查看当前故障
curl http://localhost:9101/faultsub/snapshot

# 查看最近事件
curl http://localhost:9101/faultsub/events

# 列出所有订阅
curl http://localhost:9101/faultsub/subscriptions
```

**前提**：daemon 容器需要 `--privileged` 模式（已包含 NPU 设备访问），否则故障检测无数据来源。

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

## 9. 数据卷说明

| Volume | 写入方 | 读取方 | 内容 |
|--------|--------|--------|------|
| `cm-snapshot` | daemon | web, dfee | snapshot.json + snapshot_*.json |
| `cm-data` | daemon | — | JSONL 历史数据 |
| `cm-straggler` | daemon | — | straggler KPI 文件（可选） |

## 10. 停止与清理

```bash
# 停止全部容器
docker rm -f catmonitor catmonitor-web catmonitor-dfee

# 清理数据卷（保留数据则跳过）
docker volume rm cm-snapshot cm-data

# 删除镜像
docker rmi catmonitor-npu catmonitor-generic
```

## 11. 启动顺序

1. **先启动 daemon**（snapshot 生产者），等待 6-9 秒完成首次采集
2. **后启动 web/dfee**（snapshot 消费者），snapshot 就绪后即有数据

web/dfee 可在任意时刻拉起，只要 snapshot 已存在就有数据。若 snapshot 尚未就绪，web/dfee 返回 503，自动重试即可。

## 12. 常见问题

### Q: 构建失败，提示找不到 dcmi.h 或 GLIBC 符号

NPU 镜像必须使用 Debian（glibc）基础镜像，不能用 Alpine（musl libc）。

1. 确保构建主机上已安装 Ascend driver：`ls /usr/local/Ascend/driver/include/dcmi_interface_api.h`
2. 确保使用 `build.sh` 而非手动 `docker build`（脚本会自动选择 `golang:1.23` + `debian:bookworm-slim`）
3. 如果 driver 安装在其他路径，修改 `docker/build.sh` 中的 `DRIVER_PATH`

### Q: 容器内 ipmitool 报错 "Unable to open /dev/ipmi0"

确保宿主机已加载 ipmi 内核模块：

```bash
sudo modprobe ipmi_devintf
sudo modprobe ipmi_si
ls /dev/ipmi0
```

### Q: daemon 容器报 "libc_sec.so: cannot open shared object file"

需要挂载 nnae 和 toolkit 目录并设置完整 LD_LIBRARY_PATH：

```bash
-v /usr/local/Ascend/nnae:/usr/local/Ascend/nnae:ro \
-v /usr/local/Ascend/ascend-toolkit:/usr/local/Ascend/ascend-toolkit:ro \
-e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:/usr/local/Ascend/ascend-toolkit/latest/aarch64-linux/lib64:/usr/local/Ascend/nnae/latest/lib64
```

### Q: dfee/web 容器输出 "snapshot not ready"

daemon 尚未完成首次采集。等待 6-9 秒后重试。检查 snapshot 是否已生成：

```bash
docker exec catmonitor ls /var/lib/catmonitor/snapshot/
```

### Q: NPU 指标为空

1. 确认使用了 `catmonitor-npu` 镜像（不是 generic）
2. 确认 driver + nnae + toolkit 已挂载 + `LD_LIBRARY_PATH` 已设置
3. 检查 daemon 日志：`docker logs catmonitor`

### Q: Web 仪表盘只显示 eth0，MAC 地址相同

daemon 容器未使用 `--network host`，容器有自己的网络命名空间，`/sys/class/net/` 只显示虚拟网卡。加 `--network host` 重启 daemon 即可。

### Q: docker build 时 apt-get 很慢

Dockerfile.npu 默认用 Debian 官方源。如遇网络慢，在 Dockerfile 的 RUN 行前加清华镜像源：

```dockerfile
RUN sed -i 's|deb.debian.org|mirrors.tuna.tsinghua.edu.cn|g; s|security.debian.org|mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list.d/debian.sources && \
    apt-get update && apt-get install -y --no-install-recommends \
    ipmitool smartmontools util-linux dmidecode && rm -rf /var/lib/apt/lists/*
```
