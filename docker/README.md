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

运行时需要设置 `LD_LIBRARY_PATH` 指向 driver 和 nnae 库目录：

```
LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/nnae/latest/lib64
```

同时需要挂载 nnae 目录（`libc_sec.so` 和 `libmmpa.so` 在 nnae 而非 driver 中）。

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
docker volume create cm-snapshot cm-data
```

#### 步骤 2：启动 daemon

```bash
docker run -d --name catmonitor --privileged \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -v /usr/local/Ascend/nnae:/usr/local/Ascend/nnae:ro \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/nnae/latest/lib64 \
  --device /dev/davinci0 \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  -p 9100:9100 \
  catmonitor-npu
```

> NPU 环境专用参数：
> - `--device /dev/davinci0`：按实际卡号调整，`ls /dev/davinci*` 查看
> - `-v /usr/local/Ascend/driver` + `-v /usr/local/Ascend/nnae`：挂载驱动
> - `-e LD_LIBRARY_PATH`：让 glibc 找到 libdcmi.so 及其依赖
>
> 非 NPU 环境去掉以上三行，镜像名改为 `catmonitor-generic`。

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
| 9101 | faultsub REST API（可选） | `/faultsub/events` 等 |
| 9527 | web 仪表盘 | `/`、`/api/snapshot`、`/api/collectors` |
| 9528 | dfee SPA | `/`、`/dfee/` |
| 9333 | dfee Prometheus exporter | `/metrics`（node_*/ipmi_*/static_*） |

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

根据实际 NPU 卡数调整 `--device` 或 compose 中的 `devices`：

```bash
ls /dev/davinci*    # 查看可用设备
```

### 权限

容器需要 `--privileged` 模式以访问：
- `/dev/ipmi0`（ipmitool）
- `/dev/sd*`（smartctl）
- `/dev/davinci*`（NPU DCMI）
- `/proc`、`/sys`（系统指标）
- SMBIOS（dmidecode）

### 运行时库依赖

`libdcmi.so` 是 glibc 链接的，运行时需要：
- 挂载 `/usr/local/Ascend/driver`（libdcmi.so 本体）
- 挂载 `/usr/local/Ascend/nnae`（libc_sec.so、libmmpa.so 依赖）
- 设置 `LD_LIBRARY_PATH` 指向两个库目录

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
3. 删除 `devices`、NPU driver/nnae `volumes`、`LD_LIBRARY_PATH`

## 8. 配置修改

### 挂载自定义配置

```bash
docker run -d --name catmonitor --privileged \
  -v /path/to/my-catmonitor.yaml:/etc/catmonitor/catmonitor.yaml:ro \
  ...其他参数...
  catmonitor-npu
```

### 开启 faultsub

```yaml
faultsub:
  enabled: true
  rest_addr: ":9101"
```

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

需要挂载 nnae 目录并设置 LD_LIBRARY_PATH：

```bash
-v /usr/local/Ascend/nnae:/usr/local/Ascend/nnae:ro \
-e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/nnae/latest/lib64
```

### Q: dfee/web 容器输出 "snapshot not ready"

daemon 尚未完成首次采集。等待 6-9 秒后重试。检查 snapshot 是否已生成：

```bash
docker exec catmonitor ls /var/lib/catmonitor/snapshot/
```

### Q: NPU 指标为空

1. 确认使用了 `catmonitor-npu` 镜像（不是 generic）
2. 确认 `--device /dev/davinci*` 设备已挂载
3. 确认 driver + nnae 已挂载 + `LD_LIBRARY_PATH` 已设置
4. 检查 daemon 日志：`docker logs catmonitor`

### Q: docker build 时 apt-get 很慢

Dockerfile.npu 默认用 Debian 官方源。如遇网络慢，在 Dockerfile 的 RUN 行前加清华镜像源：

```dockerfile
RUN sed -i 's|deb.debian.org|mirrors.tuna.tsinghua.edu.cn|g; s|security.debian.org|mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list.d/debian.sources && \
    apt-get update && apt-get install -y --no-install-recommends \
    ipmitool smartmontools util-linux dmidecode && rm -rf /var/lib/apt/lists/*
```
