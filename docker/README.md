# CATMonitor 容器化使用文档

## 1. 概述

CATMonitor 控制面支持三种镜像：

| 镜像 | 适用环境 | 说明 |
|------|---------|------|
| `catmonitor-npu` | 有 Ascend NPU | CGo 编译，链接 libdcmi.so，采集 123 项 NPU 指标 |
| `catmonitor-gpu` | 有 NVIDIA GPU | 纯 Go 编译，Debian/glibc 基础，兼容宿主机 nvidia-smi |
| `catmonitor-generic` | 无 NPU/GPU（纯 CPU） | 纯 Go 编译，Alpine 基础，镜像最小 |

三个服务可以组合使用：

| 服务 | 容器端口 | 功能 |
|------|---------|------|
| `catmonitor` (daemon) | 19320, 19321 | 采集指标 + Prometheus 导出 + snapshot 写入 + faultsub |
| `web` | 19322 | Web 仪表盘（读 snapshot） |
| `dfee` | 19323, 9333 | 能效监控 SPA + Prometheus exporter + CSV 输出 |

daemon 是 snapshot 唯一生产者；web/dfee 是只读消费者，不自行采集。三容器共享一个 snapshot 卷。

可靠性压测启用后仍保持四个明确的运行面，不合成巨型镜像：

| 运行面 | 基础系统 | 设计目的 |
|---|---|---|
| 通用 CATMonitor/Web/DFeE 控制面 | Alpine | 保持镜像小巧和 develop 的通用部署行为 |
| CPU Stress Runner | Debian | 携带匹配的 MPI、OpenBLAS、numactl、HPL/HPCG 运行环境 |
| Ascend CATMonitor/Web/DFeE 控制面 | Debian（glibc） | 兼容 DCMI 等厂商动态库 |
| NPU Burn 数据面 | 管理员选择的 Ascend 基础镜像 | 继承匹配的 CANN/torch_npu 环境，不替换基础发行版 |

## 2. 构建

### 安装前确认镜像存储位置

镜像、layer 和容器可写层保存在所选 Docker daemon 的 `Docker Root Dir`，不是源码
目录。构建或加载镜像前，管理员应确认该目录所在文件系统有足够空间：

```bash
docker info --format 'Docker Root Dir: {{.DockerRootDir}}'
DOCKER_ROOT=$(docker info --format '{{.DockerRootDir}}')
findmnt -T "$DOCKER_ROOT"
df -h "$DOCKER_ROOT"
```

空间不足时应由管理员选择合适的现有 daemon 或调整主机 Docker 部署；CATMonitor
不会修改 daemon 的全局存储配置。

### 构建镜像

```bash
cd CATMonitor
bash docker/build.sh            # 自动检测 NPU driver
```

或手动指定：

```bash
bash docker/build.sh npu        # 强制 NPU 镜像
bash docker/build.sh generic    # 强制通用镜像
```

`docker/build.sh` 在 Linux 宿主机或 WSL 的 Docker 环境中执行。可以通过
`CATMONITOR_DOCKER_BIN` 选择 CLI，通过 `DOCKER_HOST` 选择 data-root 位于数据盘的
daemon；隔离 daemon 没有 bridge 时可设置 `CATMONITOR_DOCKER_BUILD_NETWORK=host`。
NPU 模式把临时
二进制写入 `docker/.build`，仓库根目录不会出现或删除构建产物；打包完成后临时目录
自动清理。Windows 中转仓库时，Git 必须保留 `*.sh` 的 LF 行尾。

首次构建需要取得 Go builder、运行时基础镜像、Go modules 和系统工具包。正常节点
由 Docker 自动拉取；受限节点可在当前管理终端临时设置标准代理和 Go module proxy：

```bash
export HTTP_PROXY=http://proxy.example.com:3128
export HTTPS_PROXY=http://proxy.example.com:3128
export NO_PROXY=127.0.0.1,localhost,registry.internal.example.com
export GOPROXY=https://proxy.golang.example,direct

bash docker/build.sh npu

unset HTTP_PROXY HTTPS_PROXY NO_PROXY GOPROXY
```

构建脚本只把已设置的变量名转发给临时 build/builder，不打印值、不写进仓库配置，
也不会在最终运行镜像中设置代理。仓库不再硬编码站点专用 Go proxy。如果代理仅监听
宿主机 `127.0.0.1`，应先让 Docker 能访问该代理或配置 Docker daemon 代理。

完全离线的目标节点优先加载已经在同架构、兼容驱动环境完成验收的镜像，而不是在
作业启动时下载：

```bash
# 联网或审批构建节点
docker save -o catmonitor-npu.tar catmonitor-npu
docker save -o catmonitor-npuburn.tar catmonitor/npuburn:a3-candidate

# 离线 A3 节点
docker load -i catmonitor-npu.tar
docker load -i catmonitor-npuburn.tar
```

若必须离线构建，还要预先加载相应 builder/runtime 镜像：NPU 控制面使用
`golang:1.23` 和 `debian:bookworm-slim`，通用控制面使用
`golang:1.23-alpine` 和 `alpine:latest`，并准备 Go module cache 与对应发行版的
系统包来源。CATMonitor 容器镜像和 NPU Burn
运行镜像是两个独立产物，后者的联网/代理/离线 `pciutils` 流程见 stress 指南。

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
| `Dockerfile.generic` | 通用控制镜像（多阶段，golang 编译 + Alpine 运行时） |
| `catmonitor.yaml` | 容器版配置（打包在镜像中） |

## 3. 启动

### 方式一：统一安装入口（推荐）

先准备完整主配置和本地镜像，再安装统一命令及审核过的 Compose 定义：

```bash
sudo install -d -m 0750 /etc/catmonitor
sudo install -m 0640 docker/catmonitor.yaml /etc/catmonitor/catmonitor.yaml
sudo make install-installer

sudo catmonitor-install --profile monitoring --action plan
sudo catmonitor-install --profile monitoring
```

`catmonitor-install` 支持 `monitoring`、`cpu-stress`、`ascend-a2`、`ascend-a3`。
默认动作 `up` 只编排已有镜像与资产，不构建镜像、编译 benchmark、编辑 YAML 或
运行压测。Web 保持 develop 的默认监听 `:19322`，用于从外部监控节点；端口或绑定
地址冲突时可显式传 `--web-addr HOST:PORT`。非回环监听只提供监控和报告读取，现有
stress Web 写接口仍要求服务与请求都来自回环地址。完整 profile、前置条件和安全边界见
[`STRESS_USER_GUIDE.md`](../features/stress/STRESS_USER_GUIDE.md#9-统一容器安装入口)。

### 方式二：手工 Docker Compose（排查底层模型）

基础 Compose 不包含 Ascend 挂载，也不暴露 Docker socket，默认使用通用镜像：

```bash
CATMONITOR_IMAGE=catmonitor-generic \
docker compose -f docker/docker-compose.yml up -d
```

启动全部三个容器：daemon + web + dfee。

Ascend 节点先构建 NPU 镜像，再叠加 NPU overlay：

```bash
bash docker/build.sh npu

CATMONITOR_IMAGE=catmonitor-npu \
docker compose \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  up -d
```

#### 只启动部分服务

```bash
# daemon + dfee（跳过 web）
CATMONITOR_IMAGE=catmonitor-generic \
docker compose -f docker/docker-compose.yml up -d catmonitor dfee

# 只启动 daemon
CATMONITOR_IMAGE=catmonitor-generic \
docker compose -f docker/docker-compose.yml up -d catmonitor
```

### 方式三：手动 docker run（无 compose 或 Docker 18.09）

#### 步骤 1：创建卷

```bash
docker volume create cm-snapshot
docker volume create cm-data
```

#### 步骤 2：启动 daemon

```bash
docker run -d --name catmonitor --privileged --network host --pid host \
  -v /:/host:ro \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -v /usr/local/Ascend/nnae:/usr/local/Ascend/nnae:ro \
  -v /usr/local/Ascend/ascend-toolkit:/usr/local/Ascend/ascend-toolkit:ro \
  -v /usr/bin/hccn_tool:/usr/bin/hccn_tool:ro \
  -v /usr/local/sbin/npu-smi:/usr/local/sbin/npu-smi:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:/usr/local/Ascend/ascend-toolkit/latest/aarch64-linux/lib64:/usr/local/Ascend/nnae/latest/lib64 \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  catmonitor-npu
```

> 配置文件（`catmonitor.yaml`、`metrics.yaml`、`features/*/metrics.yaml`）已打包在镜像中，默认无需挂载。如需自定义，参见[第 9 节：配置修改](#9-配置修改)。

> NPU 环境专用参数：
> - `--pid host` + `-v /:/host:ro`：共享宿主机 PID 命名空间读取 `/proc/1/mounts`，挂载根文件系统给 statfs 访问，使磁盘空间指标反映宿主机真实文件系统而非容器 bind mount
> - `-v /usr/local/Ascend/driver` + `-v /usr/local/Ascend/nnae` + `-v /usr/local/Ascend/ascend-toolkit`：挂载驱动
> - `-v /usr/bin/hccn_tool` + `-v /usr/local/sbin/npu-smi`：挂载 NPU 命令行工具（driver 安装到宿主机系统路径，不在 Ascend 目录下）
> - `-v /etc/os-release:/etc/os-release:ro`：获取宿主机 OS 信息（容器内默认显示容器 OS）
> - `-e LD_LIBRARY_PATH`：让 glibc 找到 libdcmi.so、libc_sec.so、libmmpa.so 等依赖
> - `--privileged` 已包含 NPU 设备访问权限，无需额外 `--device`
>
> 非 NPU 环境去掉 driver/nnae/toolkit/hccn_tool/npu-smi/LD_LIBRARY_PATH，镜像名改为 `catmonitor-generic`。`/etc/os-release` 挂载在非 NPU 环境同样需要。

#### 步骤 3：等待首次采集（6-9 秒）

```bash
docker exec catmonitor ls /var/lib/catmonitor/snapshot
# 预期：snapshot.json + snapshot_cpu.json + snapshot_npu.json + ...
```

#### 步骤 4：启动 web

```bash
docker run -d --name catmonitor-web --network host --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  catmonitor-npu \
  -addr=:19322 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -config=/etc/catmonitor/catmonitor.yaml
```

> `--network host` 后不需要 `-p` 端口映射，容器直接用宿主机网络栈。

#### 步骤 5：启动 dfee

```bash
# 基础模式（仅 SPA + API）
docker run -d --name catmonitor-dfee --network host --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  catmonitor-npu -snapshot-dir /var/lib/catmonitor/snapshot

# 含 Prometheus exporter + CSV 输出
docker run -d --name catmonitor-dfee --network host --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-csv:/var/lib/catmonitor/csv \
  catmonitor-npu \
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
  catmonitor-npu \
  -snapshot-dir /var/lib/catmonitor/snapshot

# 含 exporter + CSV
docker run -d --name dfee --network host --entrypoint /usr/local/bin/dfee \
  -v /var/lib/catmonitor/snapshot:/var/lib/catmonitor/snapshot:ro \
  -v /var/lib/catmonitor/csv:/var/lib/catmonitor/csv \
  catmonitor-npu \
  -snapshot-dir /var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -exporter-port=9333 \
  -csv=enabled \
  -csv-dir=/var/lib/catmonitor/csv
```

### GPU 环境运行

`catmonitor-gpu` 镜像基于 Debian/glibc，可直接运行宿主机的 `nvidia-smi`（glibc 编译）。
Alpine 基础的 `catmonitor-generic` 因 musl libc 不兼容，无法运行 `nvidia-smi`，
GPU 环境请使用 `catmonitor-gpu`。

#### 构建

```bash
bash docker/build.sh gpu
```

#### Docker Compose

GPU overlay 挂载宿主机的 `nvidia-smi` 和 NVIDIA 运行时库：

```bash
CATMONITOR_IMAGE=catmonitor-gpu \
docker compose \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.gpu.yml \
  up -d
```

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

> 挂载整个 `/usr/lib/x86_64-linux-gnu` 目录而非单个 `.so` 文件，因为
> `nvidia-smi` 可能依赖多个 NVIDIA 库（libnvidia-ml.so、libcuda.so 等）。
> web/dfee 容器只读 snapshot，不需要 GPU 访问，镜像用 `catmonitor-gpu` 或
> `catmonitor-generic` 均可。

#### 手动 docker run

```bash
# 1. 创建卷
docker volume create cm-snapshot
docker volume create cm-data

# 2. 启动 daemon（挂载 nvidia-smi + NVIDIA 库）
docker run -d --name catmonitor --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v /usr/bin/nvidia-smi:/usr/bin/nvidia-smi:ro \
  -v /usr/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu:ro \
  -e LD_LIBRARY_PATH=/usr/lib/x86_64-linux-gnu \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  catmonitor-gpu

# 3. 等待首次采集（6-9 秒）
docker exec catmonitor ls /var/lib/catmonitor/snapshot
# 预期包含 snapshot_gpu.json

# 4. 启动 web
docker run -d --name catmonitor-web --network host --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  catmonitor-gpu \
  -addr=:19322 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -config=/etc/catmonitor/catmonitor.yaml

# 5. 启动 dfee
docker run -d --name catmonitor-dfee --network host --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  catmonitor-gpu \
  -addr=:19323 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -exporter-port=9333
```

#### 验证 GPU 采集

```bash
# 检查 snapshot 文件
docker exec catmonitor ls /var/lib/catmonitor/snapshot/snapshot_gpu.json

# 查看 GPU 指标
curl -s http://localhost:19322/api/snapshot | python3 -m json.tool | grep gpu | head -5

# Prometheus exporter
curl -s http://localhost:19320/metrics | grep gpu
```

## 4. 端口说明

| 容器端口 | 服务 | 端点 |
|---------|------|------|
| 19320 | daemon Prometheus exporter | `/metrics`、`/-/healthy`、`/-/ready` |
| 19321 | faultsub REST API（可选） | `/faultsub/events` 等 |
| 19322 | web 仪表盘 | `/`、`/api/snapshot`、`/api/collectors` |
| 19323 | dfee SPA | `/`、`/dfee/` |
| 9333 | dfee Prometheus exporter | `/metrics` |

如需自定义端口映射（如映射到不同主机端口）：

```bash
docker run -d --name catmonitor --privileged --network host \
  ...其他参数...
  catmonitor-npu
```

## 5. 验证

```bash
# daemon
curl http://localhost:19320/-/healthy           # 200
curl http://localhost:19320/metrics | grep npu    # NPU 指标

# web
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:19322/   # 200
curl -s http://localhost:19322/api/snapshot | head -c 120           # JSON

# dfee
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:19323/   # 200
curl -s http://localhost:19323/api/dfee | head -c 120           # dfee API
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

### 运行时库依赖

`libdcmi.so` 是 glibc 链接的，运行时需要：
- 挂载 `/usr/local/Ascend/driver`（libdcmi.so 本体）
- 挂载 `/usr/local/Ascend/nnae`（libc_sec.so、libmmpa.so 依赖）
- 挂载 `/usr/local/Ascend/ascend-toolkit`（toolkit 库）
- 设置 `LD_LIBRARY_PATH` 指向四个库目录

## 7. 非 NPU 环境

```bash
# 构建
bash docker/build.sh generic

# 启动（不需要 driver/nnae 挂载、device、LD_LIBRARY_PATH）
docker run -d --name catmonitor --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  catmonitor-generic

docker run -d --name catmonitor-web --network host --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  catmonitor-generic \
  -addr=:19322 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -config=/etc/catmonitor/catmonitor.yaml

docker run -d --name catmonitor-dfee --network host --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  catmonitor-generic \
  -snapshot-dir /var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -csv=enabled \
  -csv-dir=/var/lib/catmonitor/csv
```

Compose 无需修改文件，设置 `CATMONITOR_IMAGE=catmonitor-generic` 后直接使用基础
`docker-compose.yml`。

## 8. 容器化可靠性压测

可靠性压测是显式启用能力。基础 Compose 不挂载 stress 插件或 Docker socket，
也不允许 Web 触发作业。CPU 压测节点叠加 `docker-compose.stress.yml`；只有启用
NPU Burn `docker_exec` 时才继续叠加 `docker-compose.stress-npuburn.yml`。

### 8.1 三个容器层次

容器化部署包含两个互相独立的生命周期：

1. CATMonitor daemon/Web 控制容器：读取同一份 YAML、只读插件目录和可写报告目录；
2. 可选 CPU runner 容器：自带 STREAM/HPL/HPCG 及匹配的 MPI/OpenBLAS，只通过
   Unix Socket 接受三个固定项目名；
3. 固定 NPU Burn 容器：由管理员提前构建并创建，持有 CANN、torch_npu、NPU
   device 和结果目录。

宿主机原生 CPU adapter 仍受支持并且是默认后端；CPU runner 是容器化控制面推荐的
可选交付方式。它不挂 Docker Socket、不监听 TCP，也不接受任意命令或参数。HPL/
HPCG 的可写工作目录位于共享状态卷，benchmark 和动态库只读保存在 runner 镜像。

CATMonitor 不在作业时 `pull`、`run`、重建或删除 NPU Burn 容器，只通过
`docker exec <固定容器> ...` 启动一次负载。为此仅在
`docker-compose.stress-npuburn.yml` 中把宿主机 Docker socket 挂入 daemon 和
Web。Docker socket 等价于宿主机 root 权限，只能在受控节点启用，不能作为基础
或 CPU stress 部署的默认项。

### 8.2 准备部署文件

先按 [`features/stress/STRESS_TEST_GUIDE.md`](../features/stress/STRESS_TEST_GUIDE.md)
构建 CPU runner 和 NPU Burn 镜像、创建固定 NPU 容器，再生成节点部署文件：

```bash
sudo bash scripts/stress/build_cpu_runner_image.sh \
  --image catmonitor/stress-cpu:node-v1 \
  --stream-src /path/to/stream.c \
  --hpl-src /path/to/hpl-2.3.tar.gz \
  --hpl-dat /path/to/HPL.dat \
  --hpcg-src /path/to/hpcg-3.1.tar.gz \
  --hpcg-dat /path/to/hpcg.dat \
  --build-root /var/tmp/catmonitor-cpu-runner-build
```

默认使用基础镜像自带的 Debian 软件源。受限网络可显式增加
`--debian-mirror https://mirror.example.com`。参数只接受不含 userinfo、路径、查询
或片段的 HTTP(S) mirror root；实际 override 会记录在 CPU Runner image manifest，
未指定时记录为 `null` 且构建行为保持不变。

容器内的路径
必须和 adapter 中的绝对路径一致；建议将共享状态固定为
`/var/lib/catmonitor/stress`：

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/stress-deployment \
  --plugin-root /opt/catmonitor/stress \
  --cpu-backend unix \
  --cpu-runner-image catmonitor/stress-cpu:node-v1 \
  --cpu-runner-manifest /var/tmp/catmonitor-cpu-runner-build/manifests/cpu-runner-image-manifest.json \
  --hpl-processes 8 --hpl-threads 12 \
  --hpcg-processes 96 --hpcg-threads 1 \
  --npu-manifest /absolute/path/npu-burn-image-manifest.json \
  --npu-runtime /usr/bin/docker \
  --npu-container catmonitor-npuburn-a3 \
  --npu-image catmonitor/npuburn:a3-candidate \
  --npu-device 14 \
  --npu-chip-generation A3 \
  --npu-cann '<deployed-version>' \
  --npu-torch-npu '<deployed-version>' \
  --npu-soc '<deployed-model>' \
  --npu-run-case quant_matmul \
  --report-path /var/lib/catmonitor/stress/stress-latest.json \
  --enable-web
```

生成后，把节点 adapter 和清单安装到稳定插件目录。此命令只安装文件和创建状态
目录，不会编辑 YAML、启动容器或运行压测：

```bash
sudo bash scripts/stress/install_stress_runtime.sh \
  --adapter /etc/catmonitor/stress-deployment/benchmark_check.sh \
  --cpu-runner-adapter /etc/catmonitor/stress-deployment/cpu-runner-benchmark_check.sh \
  --deployment-manifest /etc/catmonitor/stress-deployment/stress-deployment-manifest.json \
  --force
```

上面的 MPI 数量、设备 ID、版本和镜像名只是参数位置示例，必须按目标节点重新确认，
不能照抄为通用默认值。生成器输出：

- `catmonitor-stress.yaml`：可供 CLI/Web 验证的顶层 `stress:` 配置片段；
- `benchmark_check.sh`：节点 adapter；
- `cpu-runner-benchmark_check.sh`：runner 容器内部固定 CPU profile；
- `stress-deployment-manifest.json`：部署输入与哈希。

daemon 不能直接把该片段当作完整容器配置，否则 `snapshot.enabled` 会回落为 false。
部署时复制 `docker/catmonitor.yaml` 为节点主配置，并用生成文件中的整个顶层
`stress:` 块替换其默认禁用块。最终文件应同时包含 `collectors`、`storage`、
`snapshot.enabled: true` 和生成的 `stress` 配置：

```bash
sudo install -d -m 0750 /etc/catmonitor
sudo install -m 0640 docker/catmonitor.yaml /etc/catmonitor/catmonitor.yaml

# 将 /etc/catmonitor/stress-deployment/catmonitor-stress.yaml 中的
# 顶层 stress: 块合并到 /etc/catmonitor/catmonitor.yaml，替换默认 stress: 块。
```

合并后必须检查只有一个顶层 `stress:`，且 `script_path`、`report_path` 仍是容器内
路径。不要把 adapter 中的 MPI、资产或 NPU 宿主机参数搬进 YAML。

### 8.3 启动 CATMonitor 与 Web

安装统一入口后，纯 CPU 节点直接选择 `cpu-stress`。安装器从部署 manifest 读取并
严格匹配 runner image，等待 runner 健康后自动运行无负载 doctor：

```bash
sudo make install-installer
sudo catmonitor-install --profile cpu-stress --action plan
sudo catmonitor-install --profile cpu-stress
```

纯 CPU 节点的完整主配置必须设置 `npu_burn.enabled: false`。安装器不会因为 YAML
误启用 NPU Burn 而给 CPU profile 增加 Docker Socket；这种不一致会由 doctor 明确
失败并要求管理员修正配置。

统一插件目录
`/opt/catmonitor/stress` 已由 stress overlay 按相同绝对路径只读挂入 daemon 和
Web。CPU 二进制和 MPI/OpenBLAS 只存在于 CPU runner image，并在同一发行版构建；
控制容器通过 `/run/catmonitor-stress/cpu-runner.sock` 调用固定协议客户端。启动前
仍以安装器的 plan 与 `catmonitor stress doctor` 为准。

Ascend 节点选择与部署 manifest 一致的代际。当前 NPU Burn `docker_exec` 仍需要
明确确认 Docker Socket 的 root 等价权限：

```bash
sudo catmonitor-install --profile ascend-a3 --action plan
sudo catmonitor-install \
  --profile ascend-a3 \
  --acknowledge-root-docker-socket
```

A2 改用 `--profile ascend-a2`。自定义或隔离 Docker daemon 可设置
`--docker-socket /absolute/path/docker.sock` 和 `--docker-bin /absolute/path/docker`。
CPU-only stress 的内部 Compose 集合不包含 NPU socket overlay。

只有排查 Compose 合并问题时才应手工组合底层文件；公共只读配置层不能省略：

```bash
export CATMONITOR_CONFIG=/etc/catmonitor/catmonitor.yaml
export CATMONITOR_STRESS_ROOT=/opt/catmonitor/stress
export CATMONITOR_STRESS_STATE_DIR=/var/lib/catmonitor/stress
export CATMONITOR_CPU_STRESS_IMAGE=catmonitor/stress-cpu:node-v1

CATMONITOR_IMAGE=catmonitor-generic \
docker compose \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  -f docker/docker-compose.stress.yml \
  up -d cpu-stress-runner catmonitor web dfee
```

默认 `:19322` 监听所有接口，以保持 develop 的外部监控能力。它用于普通监控和读取
已有压测报告，不挂载 root 等价的 Docker socket，也不能提交或取消压测。

需要网页 Run/Cancel 时，额外叠加 `docker-compose.stress-web.yml`。该服务复用同一
control image，但固定监听 `127.0.0.1:29592`，并挂入经过配置的主 YAML、stress
plugin/state、snapshot、CPU Runner socket、Docker socket 和 NPU 结果目录：

```bash
export CATMONITOR_CONFIG=/etc/catmonitor/catmonitor.yaml
export CATMONITOR_STRESS_ROOT=/opt/catmonitor/stress
export CATMONITOR_STRESS_STATE_DIR=/var/lib/catmonitor/stress
export CATMONITOR_DOCKER_SOCKET=/var/run/docker.sock
export CATMONITOR_NPU_OUTPUT_DIR=/var/lib/catmonitor/npu-output

docker compose \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f docker/docker-compose.stress-npuburn.yml \
  -f docker/docker-compose.stress-web.yml \
  up -d stress-web

ssh -N -L 29592:127.0.0.1:29592 root@node
```

访问入口保持分离：

- 普通只读 Web：`http://<node-address>:19322/`
- 运维 Stress Web：通过 SSH 隧道访问 `http://127.0.0.1:29592/stress/`

运维 Web 复用基础 Compose 的 `snapshot` 命名卷，不创建第二份快照存储。

`CATMONITOR_NPU_OUTPUT_DIR` 必须与 NPU adapter profile 中记录的
`result_directory` 一致。29592 不得直接暴露到管理网或公网。

### 8.4 验证

```bash
sudo catmonitor-install --profile cpu-stress --action status
sudo catmonitor-install --profile cpu-stress --action doctor

curl -fsS http://127.0.0.1:19322/api/stress/config
curl -fsS http://127.0.0.1:29592/stress/ >/dev/null
curl -fsS http://127.0.0.1:29592/api/stress/config
```

Ascend 节点把上面的 profile 换为实际 `ascend-a2` 或 `ascend-a3`；`status/doctor/down`
不需要重复 Socket 风险确认。`down` 不删除持久卷，并在运行资产丢失时仍可用于恢复。

本机无 Ascend 硬件时只能执行容器边界 E2E：

```bash
CATMONITOR_CONTAINER_IMAGE=catmonitor-npu:latest \
make test-stress-container-e2e
```

该测试用模拟固定容器验证 Docker socket、`docker exec`、NPU logical device 预检、
PASS CSV 解析和报告持久化，不声称验证 CANN ABI 或真实 NPU 计算。A3 上还必须运行
真实 fixed-container smoke、CLI、Web 和设备观测。

## 9. 配置修改

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
  catmonitor-npu
```

Docker Compose 用户取消 `docker-compose.yml` 中 volumes 段的注释，将宿主机文件挂载覆盖即可。

### 开启 faultsub（故障订阅推送）

faultsub 是 NPU 故障检测与推送机制，运行在 daemon 内部。开启后，daemon 周期性采集 NPU 指标时自动检测故障，并推送给已订阅的 webhook。

**配置**：

```yaml
faultsub:
  enabled: true
  rest_addr: ":19321"           # REST API 监听地址
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

**REST API 端点**（端口 19321）：

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
curl -X POST http://localhost:19321/faultsub/subscriptions \
  -H "Content-Type: application/json" \
  -d '{"webhook_url": "http://my-fault-manager:8080/fault", "types": ["card_drop", "npu_health"]}'

# 查看当前故障
curl http://localhost:19321/faultsub/snapshot

# 查看最近事件
curl http://localhost:19321/faultsub/events

# 列出所有订阅
curl http://localhost:19321/faultsub/subscriptions
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

## 10. 数据卷说明

| Volume | 写入方 | 读取方 | 内容 |
|--------|--------|--------|------|
| `cm-snapshot` | daemon | web, dfee | snapshot.json + snapshot_*.json |
| `cm-data` | daemon | — | JSONL 历史数据 |
| `cm-straggler` | daemon | — | straggler KPI 文件（可选） |
| `cm-csv` | dfee | — | dfee CSV 输出（可选，`-csv=enabled` 时） |

## 11. 停止与清理

```bash
# 停止全部容器
docker rm -f catmonitor catmonitor-web catmonitor-dfee

# 清理数据卷（保留数据则跳过）
docker volume rm cm-snapshot cm-data cm-csv

# 删除镜像
docker rmi catmonitor-npu catmonitor-generic
```

## 12. 启动顺序

1. **先启动 daemon**（snapshot 生产者），等待 6-9 秒完成首次采集
2. **后启动 web/dfee**（snapshot 消费者），snapshot 就绪后即有数据

web/dfee 可在任意时刻拉起，只要 snapshot 已存在就有数据。若 snapshot 尚未就绪，web/dfee 返回 503，自动重试即可。

## 13. 常见问题

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
3. 确认 `hccn_tool` 和 `npu-smi` 已挂载（driver 安装到宿主机 `/usr/bin` 和 `/usr/local/sbin`，不在 Ascend 目录下）
4. 检查 daemon 日志：`docker logs catmonitor`

### Q: Web 仪表盘只显示 eth0，MAC 地址相同

daemon 容器未使用 `--network host`，容器有自己的网络命名空间，`/sys/class/net/` 只显示虚拟网卡。加 `--network host` 重启 daemon 即可。

### Q: docker build 时 apt-get 很慢

Dockerfile 默认使用 Debian 官方源，不要直接修改并提交站点专用镜像地址。控制镜像构建
可由管理员临时设置代理，构建脚本只转发已存在的代理变量，不保存其值：

```bash
export HTTP_PROXY=http://proxy.example.com:3128
export HTTPS_PROXY=http://proxy.example.com:3128
./docker/build.sh npu
unset HTTP_PROXY HTTPS_PROXY
```

CPU Runner clean build 还可显式使用
`--debian-mirror https://mirror.example.com`；默认行为不变，override 会记录在 image
manifest 中，并拒绝包含用户名或密码的 URL。

## 14. dfee Prometheus Exporter + Grafana

dfee 支持独立的 Prometheus exporter（`:9333`），输出 CPU/内存/磁盘/网络/NPU/机箱指标的 `node_*`/`dsmi_*`/`ipmi_*` 格式，可直接接入 Prometheus + Grafana。

### dfee 参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-addr` | `:19323` | dfee SPA + API 监听地址 |
| `-snapshot-dir` | `/var/lib/catmonitor/snapshot` | daemon snapshot 目录 |
| `-exporter` | `disabled` | `enabled` 开启 Prometheus exporter |
| `-exporter-port` | `9333` | exporter 监听端口 |
| `-device` | `""` | NPU 设备过滤（如 `0,1`），空=全部 |
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
  catmonitor-npu \
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

Dashboard 包含 24 个面板（CPU/内存/网络/磁盘/NPU/机箱），支持 `instance`、`job`、`npu_id`、`chip_id` 变量过滤。

> 完整使用文档见 `features/dfee/USAGE.md`。
