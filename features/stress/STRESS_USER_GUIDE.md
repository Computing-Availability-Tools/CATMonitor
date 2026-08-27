# CATMonitor 可靠性压测使用指南

本文面向安装和使用 CATMonitor Stress V2 的节点管理员。项目专属 IP、代理、账号、
PAT 和实机绝对路径不应写入仓库配置；受限节点通过临时环境变量或离线镜像包处理。

## 1. 运行模型

正式部署由三类镜像组成：

| 镜像 | 作用 | 是否包含 benchmark |
|---|---|---|
| CATMonitor control | daemon、Web、DFeE、CLI、Docker executor | 否 |
| CPU workload | workload plugin、STREAM、HPL、HPCG、MPI/OpenBLAS | 是 |
| NPU workload | workload plugin、Ascend NPU Burn、CANN/torch_npu runtime | 是 |

CPU-only 节点只需要前两类镜像。NPU 镜像不是基础监控和 CPU Stress 的依赖。

daemon 通过固定命令调用 workload 容器：

```text
docker exec <fixed-container> \
  /usr/local/bin/catmonitor-stress-exec <describe|run|status|cancel>
```

请求中不存在 shell、可执行路径或任意参数。网页不能编辑脚本或 workload profile。

## 2. 前置条件

- Linux/arm64 或项目支持的 Linux 架构；
- Docker Engine 与 Compose v2；
- 控制镜像能够访问当前 Docker daemon；
- 构建 CPU 镜像时准备 stock STREAM/HPL/HPCG 源码和管理员审核的 `HPL.dat`、`hpcg.dat`；
- 构建 NPU 镜像时准备匹配的 builder/runtime base，运行节点有 Ascend driver 和设备节点；
- NPU logical ID 必须通过实机 profile 验证，不能用 `/dev/davinciN` 最大编号推导设备数量。

先确认：

```bash
docker version
docker compose version
uname -m
```

## 3. 获取源码

```bash
git clone https://github.com/Computing-Availability-Tools/CATMonitor.git
cd CATMonitor
git checkout <reviewed-commit-or-tag>
git status --short
```

生产交付应记录源码 commit、镜像 digest 和生成的 deployment manifest。

### 3.1 从 Stress V1 迁移

`OLD_STRESS_YAML_COMPATIBLE=false`。V1 配置不能原样用于 V2，升级时必须从
[`configs/stress-full.example.yaml`](../../configs/stress-full.example.yaml) 重新生成配置，
不要复制旧的 `script_path`、CPU Runner socket、固定 NPU 容器或独立 Stress Web 字段。

V2 的关键替换关系如下：

| V1 | V2 |
|---|---|
| `script_path` / `benchmark_check.sh` | workload 镜像内固定 typed plugin |
| CPU Runner Unix socket | daemon `DockerExecExecutor` 直接调用 workload 容器 |
| 独立 Stress Web / `:29592` | 统一 Web `:19322` |
| frontend 直接操作执行后端 | CLI/Web 只连接 `/run/catmonitor/control.sock` |
| 固定 NPU Burn 容器创建脚本 | `docker-compose.stress.yml` 的 `stress-npu` profile |

迁移后先执行 `catmonitor stress doctor`，再按 STREAM、HPCG、HPL、NPU Burn
顺序做实机验收。旧报告 JSON 可以归档保存，但不应作为 V2 active job 状态继续写入。

## 4. 构建镜像

### 4.1 Control

按节点类型保留项目原有构建入口：

```bash
# 通用 CPU 节点
bash docker/build.sh generic

# Ascend 采集节点
bash docker/build.sh npu
```

受限网络可按项目 `docker/build.sh` 支持的环境变量设置 build network、Go proxy 或
Debian mirror；代理和凭据只能临时注入，不写入 Dockerfile、YAML 或 manifest。

镜像内至少应包含：

```bash
docker run --rm --entrypoint /bin/sh catmonitor-generic:latest -c '
  test -x /usr/local/bin/catmonitor
  test -x /usr/local/bin/web
  test -x /usr/local/bin/dfee
  test -x /usr/bin/docker || test -x /usr/local/bin/docker
'
```

### 4.2 CPU workload

历史脚本文件名仍为 `build_cpu_runner_image.sh`，但 V2 产物是 workload image，
不再启动 Unix Runner server，也不包含 CPU client。

```bash
bash scripts/stress/build_cpu_runner_image.sh \
  --image catmonitor/stress-cpu:v0.4.0 \
  --stream-src /srv/sources/stream.c \
  --hpl-src /srv/sources/hpl-2.3.tar.gz \
  --hpl-dat /srv/profiles/HPL.dat \
  --hpcg-src /srv/sources/hpcg-3.1.tar.gz \
  --hpcg-dat /srv/profiles/hpcg.dat \
  --build-root /home/catmonitor/build/cpu-v0.4.0 \
  --manifest /home/catmonitor/build/cpu-v0.4.0/cpu-image-manifest.json \
  --jobs 16
```

网络受限节点可加：

```bash
--debian-mirror https://mirrors.aliyun.com/debian
```

镜像构建会编译 CPU benchmark 并验证构建资产，但不会运行 HPL/HPCG 实机负载。

### 4.3 NPU workload

推荐 builder/runtime 分离。builder 提供完整编译环境；runtime 包含 CANN runtime、
Python、torch/torch_npu 与 pciutils，宿主机只挂 driver 和设备。

```bash
bash scripts/stress/build_npu_burn_image.sh \
  --builder-base-image <reviewed-cann-builder> \
  --runtime-base-image <reviewed-cann-runtime> \
  --image catmonitor/npuburn:v0.4.0-a2 \
  --compat-profile a2-cann83 \
  --patch scripts/stress/patches/ascend_npu_burn/a2-cann83.patch \
  --build-root /home/catmonitor/build/npuburn-v0.4.0-a2 \
  --manifest /home/catmonitor/build/npuburn-v0.4.0-a2/npu-image-manifest.json
```

若 runtime 已有 `lspci`，不会安装；否则构建按镜像包管理器安装 `pciutils`，或使用
`--pciutils-package` 注入离线依赖闭包。构建阶段只做 ABI/import/preflight，不运行 NPU workload。

## 5. 生成节点部署

### 5.1 CPU-only

```bash
sudo install -d -m 0750 /etc/catmonitor/generated-stress

sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image registry.example/catmonitor/control:v0.4.0 \
  --cpu-image registry.example/catmonitor/stress-cpu:v0.4.0 \
  --cpu-manifest /home/catmonitor/build/cpu-v0.4.0/cpu-image-manifest.json \
  --stream-threads 0 \
  --hpl-processes 8 \
  --hpl-threads 12 \
  --hpcg-processes 96 \
  --hpcg-threads 1 \
  --hpcg-nx 32 --hpcg-ny 32 --hpcg-nz 32 \
  --hpcg-runtime 60 \
  --enable-web \
  --force
```

### 5.2 Ascend NPU-only

以下示例明确区分 host device node 与 NPU Burn logical ID；不传 `--cpu-image`
时不会生成 CPU workload service，CPU 基准会在配置中禁用：

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image registry.example/catmonitor/control:v0.4.0 \
  --npu-image registry.example/catmonitor/npuburn:v0.4.0-a2 \
  --npu-manifest /home/catmonitor/build/npuburn-v0.4.0-a2/npu-image-manifest.json \
  --npu-device-nodes 2,5 \
  --npu-burn-device 0,1 \
  --npu-runtime runc \
  --npu-run-case matmul \
  --npu-chip-generation A2 \
  --npu-output-dir /var/lib/catmonitor/stress/npu-burn-output \
  --enable-web \
  --force
```

Canonical Compose 会把 NPU runtime HOME 挂为限额 1 GiB 的 tmpfs，供日志、
TBE 临时数据和缓存使用，再把 NPU Burn CSV 输出子目录叠加为持久卷。不要删除
runtime HOME 挂载，否则只读 workload 容器会在启动实际算子前失败。

### 5.3 CPU + NPU

在 5.2 命令中再加入以下参数即可同时生成 CPU workload profile：

```bash
  --cpu-image registry.example/catmonitor/stress-cpu:v0.4.0 \
  --cpu-manifest /home/catmonitor/build/cpu-v0.4.0/cpu-image-manifest.json \
```

输出文件：

```text
catmonitor-stress.yaml
stress-profile.json
docker-compose.stress.generated.yml
stress-deployment-manifest.json
```

generator 只生成配置，不调用 Docker、不创建容器、不运行压测。

## 6. 启动

### 6.1 CPU-only

```bash
export CATMONITOR_IMAGE=registry.example/catmonitor/control:v0.4.0

docker compose \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu up -d
```

### 6.2 NPU-only

```bash
export CATMONITOR_IMAGE=registry.example/catmonitor/control:v0.4.0

docker compose \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-npu up -d
```

### 6.3 CPU + NPU

```bash
export CATMONITOR_IMAGE=registry.example/catmonitor/control:v0.4.0

docker compose \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu --profile stress-npu up -d
```

不加任何 Stress profile 时只启动基础监控三容器；CPU-only 增加 1 个 workload
容器，NPU-only 增加 1 个 workload 容器，Full 增加 2 个 workload 容器。

正式关系是一个 daemon、一个统一 Web、一个 DFeE，以及可选 CPU/NPU workload 容器；
不再创建第二个 `catmonitor-stress-web`。

## 7. 验证与使用

```bash
docker compose ps
docker exec catmonitor catmonitor stress doctor -o table
docker exec catmonitor catmonitor stress run --bench stream -o table
docker exec catmonitor catmonitor stress status -o json
```

若 CLI 在宿主机执行，默认通过 `/etc/catmonitor/catmonitor.yaml` 找到
`/run/catmonitor/control.sock`；宿主机需要共享该 socket，不能直接创建第二个 Manager。

一次作业可缩短超时：

```bash
catmonitor stress run --bench hpcg --timeout 120s -o table
```

不能超过 YAML 中项目上限，也不能修改 MPI、线程或脚本。

## 8. Web

唯一 `/usr/local/bin/web` 进程监听 `:19322`，同时提供：

- 健康监控与 snapshot；
- Stress 配置、latest、history 与 jobs；
- Run 与 Cancel。

```text
http://<node>:19322/
http://<node>:19322/stress/
```

Web 只挂 snapshot 与 `/run/catmonitor/control.sock`，不得挂 Docker Socket 或 workload 私有目录。写请求必须同源、使用 `Content-Type: application/json` 并携带 `X-CATMonitor-Action: stress`。当前版本尚无 operator authentication/RBAC，必须通过节点防火墙、反向代理或可信管理网络限制 `:19322` 的访问；SSH 隧道可以使用，但不是 Stress 功能前提。
## 9. 超时、取消和结果

- workload plugin 使用独立进程组启动 benchmark；取消会终止整个 MPI/benchmark 进程组；
- STREAM/HPL/HPCG 达到时间上限且此前没有执行错误时返回 `time_limit_reached`，可视为可靠性通过；
- NPU Burn 必须形成完整 SDC/CSV 结果；超时不能当作通过；
- HPCG 只接受本次运行新建或修改的 `HPCG-Benchmark*.txt`，拒绝历史文件；
- NPU Burn CSV 在 workload 容器内解析为标准摘要；
- Controller 保存 latest 与最近 100 次 history，CLI/Web 读取同一份状态。

## 10. 常见故障

| 现象 | 检查 |
|---|---|
| `connect to CATMonitor control socket` | daemon 状态、`/run/catmonitor/control.sock` mount/权限 |
| benchmark unavailable | workload 容器是否运行、plugin `describe --json`、镜像是否正确 |
| Docker executor failed | daemon 是否唯一挂载正确 Docker socket、Docker CLI 是否存在 |
| HPL/HPCG preflight fail | MPI launcher 与二进制 ABI、工作目录、输入文件、资源规模 |
| HPCG no fresh result | workload volume 写权限、运行是否生成新的结果文件 |
| NPU logical ID unavailable | `/dev/davinciN` 映射、容器 `lspci` topology、logical ID profile |
| Web 无法 Run/Cancel | 确认 `stress.web_enabled: true`、control socket 可达且 `:19322` 网络策略允许访问 |
| Cancel 后仍有进程 | workload plugin 状态与容器内进程组；按缺陷处理，不手工忽略 |

## 11. 停止

使用与启动时完全相同的 Compose 文件和 profile：

```bash
docker compose \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu down
```

不要直接删除 state/report volume；它们包含可审计的历史结果。
