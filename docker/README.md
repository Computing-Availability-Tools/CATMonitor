# CATMonitor 容器化使用文档

## 1. 概述

CATMonitor 容器化方案支持两种镜像：

| 镜像 | 适用环境 | 说明 |
|------|---------|------|
| `catmonitor-npu` | 有 Ascend NPU | CGo 编译，链接 libdcmi.so，采集 119 项 NPU 指标 |
| `catmonitor-generic` | 无 NPU（纯 CPU/GPU） | 纯 Go 编译，不依赖 NPU 驱动 |

三个服务可以组合使用：

| 服务 | 端口 | 功能 |
|------|------|------|
| `catmonitor` (daemon) | 9100, 9101 | 采集指标 + Prometheus 导出 + snapshot 写入 + faultsub |
| `web` | 9527 | Web 仪表盘（读 snapshot） |
| `dfee` | 9528, 9333 | 能效监控 SPA + Prometheus 导出 + CSV 落盘 |

## 2. 构建镜像

### 自动检测构建（推荐）

```bash
cd CATMonitor
docker/docker/build.sh
```

脚本自动检测 `/usr/local/Ascend/driver` 是否存在：
- 存在 → 构建 `catmonitor-npu` 镜像
- 不存在 → 构建 `catmonitor-generic` 镜像

### 手动指定

```bash
# 强制构建 NPU 镜像
docker/docker/build.sh npu

# 强制构建通用镜像
docker/docker/build.sh generic
```

### NPU 镜像构建说明

NPU 镜像采用**两步构建**：

1. **编译**：启动一个临时的 `golang:1.23-alpine` 容器，将宿主机的 Ascend driver 挂载进去（`-v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro`），在容器内用 CGo 编译 `catmonitor`（`-tags dcmi`）+ `dfee` + `web` 三个二进制。
2. **打包**：将编译好的二进制 COPY 进 `alpine:latest` 运行时镜像。

这种方式不需要在宿主机安装 Go，也不依赖 BuildKit，兼容旧版 Docker。

## 3. 启动服务

### 方式一：docker compose 一键启动（推荐）

```bash
docker compose -f docker/docker-compose.yml up -d
```

启动全部三个容器：daemon + web + dfee。

### 方式二：只启动 daemon + dfee（跳过 web）

```bash
docker compose -f docker/docker-compose.yml up -d catmonitor dfee
```

### 方式三：单独运行 dfee 容器（daemon 在宿主机或其他容器运行）

```bash
docker run -d --name dfee \
  -v /var/lib/catmonitor/snapshot:/var/lib/catmonitor/snapshot:ro \
  -v /var/lib/catmonitor/csv:/var/lib/catmonitor/csv \
  -p 9528:9528 -p 9333:9333 \
  catmonitor-npu \
  /usr/local/bin/dfee \
  -exporter=enabled -exporter-port=9333 \
  -csv=enabled -csv-dir=/var/lib/catmonitor/csv \
  -snapshot-dir=/var/lib/catmonitor/snapshot
```

## 4. 验证

```bash
# daemon Prometheus exporter
curl http://localhost:9100/metrics

# web 仪表盘
curl -s http://localhost:9527/ | head -5

# dfee SPA
curl -s http://localhost:9528/ | head -5

# dfee Prometheus exporter
curl http://localhost:9333/metrics

# 查看 CSV 文件
ls /var/lib/docker/volumes/*csv/_data/

# 查看容器日志
docker compose -f docker/docker-compose.yml logs -f catmonitor
docker compose -f docker/docker-compose.yml logs -f dfee
```

## 5. NPU 环境配置

### 设备挂载

`docker-compose.yml` 中默认挂载了 NPU 设备和驱动：

```yaml
devices:
  - /dev/davinci0
  - /dev/davinci1
volumes:
  - /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro
```

根据实际 NPU 卡数调整 `devices` 列表。可通过以下命令查看可用设备：

```bash
ls /dev/davinci*
```

### 权限

容器需要 `--privileged` 模式以访问：
- `/dev/ipmi0`（ipmitool）
- `/dev/sd*`（smartctl）
- `/dev/davinci*`（NPU DCMI）
- `/proc`、`/sys`（系统指标）
- SMBIOS（dmidecode）

后续可逐步收紧为 `--device` + `--cap-add`。

## 6. 非 NPU 环境（通用环境）

如果不需要 NPU 指标采集，使用通用镜像：

```bash
# 构建
docker/docker/build.sh generic

# 启动（docker-compose.yml 中删除 devices 和 driver volume）
docker compose -f docker/docker-compose.yml up -d
```

如果使用 docker-compose，需要将 `dockerfile` 改为 `Dockerfile.generic`，并删除 NPU 相关的 `devices` 和 `volumes` 配置。

## 7. 配置修改

### 修改采集配置

编辑 `docker/catmonitor.yaml`（打包在镜像中），或挂载自定义配置：

```bash
docker run -d --name catmonitor \
  -v /path/to/my-catmonitor.yaml:/etc/catmonitor/catmonitor.yaml:ro \
  ...其他参数...
```

### 开启 faultsub

编辑配置：

```yaml
faultsub:
  enabled: true
  rest_addr: ":9101"
```

### 开启 straggler_output

编辑配置：

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

## 8. 数据卷说明

| Volume | 写入方 | 读取方 | 内容 |
|--------|--------|--------|------|
| `snapshot` | daemon | web, dfee | snapshot.json + snapshot_*.json |
| `data` | daemon | — | JSONL 历史数据 |
| `csv` | dfee | — | CSV 落盘文件 |
| `straggler` | daemon | — | straggler KPI 文件 |

查看数据：

```bash
# snapshot 文件
docker compose -f docker/docker-compose.yml exec catmonitor ls /var/lib/catmonitor/snapshot/

# CSV 文件
docker compose -f docker/docker-compose.yml exec dfee ls /var/lib/catmonitor/csv/

# JSONL 历史数据
docker compose -f docker/docker-compose.yml exec catmonitor ls /var/lib/catmonitor/data/
```

## 9. 停止与清理

```bash
# 停止所有容器
docker compose -f docker/docker-compose.yml down

# 停止并删除数据卷
docker compose -f docker/docker-compose.yml down -v

# 删除镜像
docker rmi catmonitor-npu catmonitor-generic
```

## 10. 常见问题

### Q: 构建失败，提示找不到 dcmi.h

NPU 镜像需要 Ascend driver 的头文件。确保构建主机上已安装 driver：

```bash
ls /usr/local/Ascend/driver/include/dcmi.h
```

如果 driver 安装在其他路径，修改 `docker/build.sh` 中的 `ASCEND_DRIVER_PATH`。

### Q: 容器内 ipmitool 报错 "Unable to open /dev/ipmi0"

确保宿主机已加载 ipmi 内核模块：

```bash
sudo modprobe ipmi_devintf
sudo modprobe ipmi_si
ls /dev/ipmi0
```

### Q: dfee 容器输出 "snapshot not ready"

daemon 尚未完成首次采集。等待 5-10 秒后重试。如果持续报错，检查 daemon 容器日志：

```bash
docker compose -f docker/docker-compose.yml logs catmonitor
```

### Q: NPU 指标为空

1. 确认使用了 `catmonitor-npu` 镜像（不是 generic）
2. 确认 `/dev/davinci*` 设备已挂载
3. 确认 `/usr/local/Ascend/driver` 已挂载
4. 检查 daemon 日志是否有 DCMI 错误

### Q: 如何在非 NPU 环境使用 docker-compose

修改 `docker/docker-compose.yml`：
1. `dockerfile` 改为 `docker/Dockerfile.generic`
2. 删除 `devices` 列表
3. 删除 NPU driver 的 `volumes` 挂载
4. `image` 标签改为 `catmonitor-generic`
