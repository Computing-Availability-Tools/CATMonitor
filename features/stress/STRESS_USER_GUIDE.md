# CATMonitor 可靠性压测使用指南

本文面向首次部署和日常操作 CATMonitor Stress 的节点管理员。用户只需要理解：

```text
CATMonitor Monitoring
+
optional CPU Stress（STREAM / HPL / HPCG）
+
optional NPU Stress（Ascend NPU Burn）
```

项目专属 IP、代理、账号、PAT 和实机绝对路径不得写入仓库。受限节点使用临时
环境变量或离线镜像包。

> 当前程序内部版本是 `0.3.5`，当前 ARM64 pre-release 镜像标签是
> `arm64-v0.3.5-stress`，目标发布线是 `v0.3.6`。

## 1. 镜像与容器

| 镜像 | 作用 | 是否必需 |
|---|---|---:|
| Control | daemon、Web、DFeE、CLI | 必需 |
| CPU workload | STREAM、HPL、HPCG、MPI/OpenBLAS | CPU Stress 可选 |
| NPU workload | CANN、torch_npu、NPU Burn | NPU Stress 可选 |

容器数量：Monitoring-only 3 个；CPU-only 4 个；NPU-only 4 个；CPU+NPU Full
5 个。CPU-only 用户不需要下载较大的 NPU workload 镜像。

当前 Linux/ARM64 Stress 专用镜像命名：

```text
ghcr.io/spike677/catmonitor-generic:arm64-v0.3.5-stress
ghcr.io/spike677/catmonitor-gpu:arm64-v0.3.5-stress
ghcr.io/spike677/catmonitor-npu:arm64-v0.3.5-stress
ghcr.io/spike677/catmonitor-stress-cpu:arm64-v0.3.5-stress
ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.5-stress
```

程序版本由项目 owner 保持为 `0.3.5`；`stress` 后缀表示该镜像来自 Stress 集成分支，
不是通用 0.3.5 或最终 v0.3.6 正式镜像。Image ID、digest 和 Git source commit
仍须随发布记录核对，不能复用 a2-r1。

GPU pre-release package 当前为 Private；GPU 用户拉取前需要完成 GHCR 身份认证。

## 2. 前置条件

所有节点：

- Linux；
- Docker Engine；
- 足够的镜像与 workload 运行空间。

使用推荐的 Compose 路径时额外需要 Docker Compose v2；使用各硬件 README 中完整的
`docker run` 路径时不需要 Compose。

CPU Stress：

- CPU workload 镜像与节点架构匹配；
- MPI rank、线程和问题规模不超过节点在线资源。

Ascend NPU Stress：

- Linux/arm64；
- Ascend driver、`npu-smi` 和 `/dev/davinciN` 正常；
- NPU workload 镜像的 CANN/torch_npu 与宿主机驱动兼容；
- 当前 Stress 镜像只覆盖已验证的 A2/Ascend910B4，其他 SoC 需单独验收。

```bash
docker version
docker compose version 2>/dev/null || true
uname -m
```

## 3. 获取 ARM64 Stress pre-release 镜像

```bash
git clone https://github.com/Computing-Availability-Tools/CATMonitor.git
cd CATMonitor
git checkout refactor/unified-stress-platform
git status --short

export CATMONITOR_RELEASE='arm64-v0.3.5-stress'
export CATMONITOR_REGISTRY='ghcr.io/spike677'
```

按节点选择 Control 镜像：

```bash
# 三选一
export CONTROL_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-generic:${CATMONITOR_RELEASE}"
# export CONTROL_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-gpu:${CATMONITOR_RELEASE}"
# export CONTROL_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-npu:${CATMONITOR_RELEASE}"

export CPU_STRESS_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-stress-cpu:${CATMONITOR_RELEASE}"
export NPU_STRESS_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-stress-npu:${CATMONITOR_RELEASE}"
```

只拉需要的镜像：

```bash
docker pull "$CONTROL_IMAGE"
docker pull "$CPU_STRESS_IMAGE"  # 仅 CPU Stress
docker pull "$NPU_STRESS_IMAGE"  # 仅 NPU Stress
```

不同硬件的完整 Compose 命令见：

- [Generic](../../docker/README-generic.md)
- [NVIDIA GPU](../../docker/README-gpu.md)
- [Ascend NPU](../../docker/README-npu.md)

## 4. 镜像来源边界

节点管理员应优先拉取同一 release 发布的 Control 和所需 workload 镜像，然后从第 5
节开始生成节点配置。构建 STREAM/HPL/HPCG 或 NPU Burn 镜像、选择 CANN base、配置
mirror/proxy、生成 manifest 和制作 RC tag 属于开发/发布职责，统一见
[镜像构建与发布开发者指南](../../docker/DEVELOPER_GUIDE.md)。

不要把不同源码提交、RC 后缀或未经对应硬件验收的 workload 镜像混在同一部署中。

## 5. 生成节点配置

```bash
sudo install -d -m 0750 \
  /etc/catmonitor/generated-stress \
  /var/lib/catmonitor/stress \
  /var/lib/catmonitor/stress/npu-burn-output
```

### 5.1 CPU-only

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image "$CONTROL_IMAGE" \
  --cpu-image "$CPU_STRESS_IMAGE" \
  --enable-web \
  --force
```

### 5.2 NPU-only

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image "$CONTROL_IMAGE" \
  --npu-image "$NPU_STRESS_IMAGE" \
  --npu-burn-device all \
  --npu-chip-generation A2 \
  --npu-runtime runc \
  --enable-web \
  --force
```

默认动态发现实际 `/dev/davinciN`；`all` 表示使用经容器 PCI topology 验证通过的
全部 NPU Burn logical devices。不要用 host 最大 device ID 推导设备数量。generator
会按映射数量输出 CANN runtime visible IDs；doctor 会在 workload 容器中核对
CANN、torch_npu、custom ops、PCI topology 和实际 `torch_npu` device count。

A2/CANN 8.3/runc 的已验证运行契约会让 `catmonitor-stress-npu` 使用
`privileged: true` 与 `network_mode: none`。这是 NPU workload 独有权限；Web、
DFeE 与 CPU workload 不应获得该权限。A3/A5 仍需独立实机验收，不能从 A2
结果外推。

### 5.3 CPU + NPU Full

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image "$CONTROL_IMAGE" \
  --cpu-image "$CPU_STRESS_IMAGE" \
  --npu-image "$NPU_STRESS_IMAGE" \
  --npu-burn-device all \
  --npu-chip-generation A2 \
  --npu-runtime runc \
  --enable-web \
  --force
```

generator 只输出配置、profile、Compose override 和 deployment manifest；不会拉取镜像、
创建容器或执行压测。

## 6. 资源参数

网页不允许编辑脚本、命令、路径、MPI 或 NPU profile。管理员在生成配置时固定参数：

| 参数 | 含义 | 默认值 |
|---|---|---:|
| `--stream-threads` | STREAM OpenMP 线程；0 表示不强制 | 0 |
| `--hpl-processes` | HPL MPI ranks | 1 |
| `--hpl-threads` | 每 rank 线程 | 1 |
| `--hpcg-processes` | HPCG MPI ranks | 1 |
| `--hpcg-threads` | 每 rank 线程 | 1 |
| `--hpcg-nx/ny/nz` | 每 rank 局部网格 | 32/32/32 |
| `--hpcg-runtime` | HPCG 目标秒数 | 60 |
| `--npu-burn-device` | NPU Burn logical IDs 或 `all` | 必填 |
| `--npu-run-case` | 固定 NPU Burn case | `matmul` |
| `--npu-internal-timeout` | NPU 单 case 超时 | 300 |

参数必须按在线 CPU、内存容量、MPI ABI 和 NPU profile 评估。修改后重新运行 generator，
再用同一 Compose 命令执行 `up -d`。

## 7. 验证、运行和取消

Controller 会通过配置的 Docker Unix socket 自动协商 daemon API 版本；无需在
YAML、Compose 或容器环境中手工设置 `DOCKER_API_VERSION`。这也允许新版 Control
镜像连接项目支持的较旧 Docker daemon。

通过 Compose project/service label 找到 daemon：

```bash
DAEMON_ID=$(docker ps \
  --filter label=com.docker.compose.project=catmonitor \
  --filter label=com.docker.compose.service=catmonitor \
  --format '{{.ID}}' | head -n 1)

test -n "$DAEMON_ID"
```

先运行 doctor：

```bash
docker exec "$DAEMON_ID" catmonitor stress doctor \
  -c /etc/catmonitor/catmonitor.yaml -o table
```

再按需运行：

```bash
docker exec "$DAEMON_ID" catmonitor stress run --bench stream -o table
docker exec "$DAEMON_ID" catmonitor stress run --bench hpcg -o table
docker exec "$DAEMON_ID" catmonitor stress run --bench hpl -o table
docker exec "$DAEMON_ID" catmonitor stress run --bench npu_burn -o table
```

状态与取消：

```bash
docker exec "$DAEMON_ID" catmonitor stress status -o table
docker exec "$DAEMON_ID" catmonitor stress cancel --job <job-id>
```

一次作业可通过 `--timeout 120s` 缩短超时，但不能超过 YAML 中的项目上限。
同一 daemon 同时只允许一个 active job。

## 8. Web、结果和历史

唯一 Web 地址：

```text
http://<node-address>:19322/
```

同一页面提供 Monitoring、Stress 配置、Run、Cancel、latest、history 和 jobs。
当前没有 Web operator authentication/RBAC，必须通过防火墙、反向代理或可信管理网络
限制 `19322`；SSH 隧道可以使用，但不是功能前提。

结果语义：

- `healthy`：执行成功且必需结果完整；
- `time_limit_reached`：STREAM/HPL/HPCG 达到上限且此前未出错，按可靠性通过展示；
- `unhealthy`：进程错误、结果校验失败或协议错误；
- `unavailable`：镜像、资产、MPI/ABI、CANN、设备或配置预检不满足；
- `cancelled`：用户取消且 workload 进程已清理。

不设置 GFLOPS、带宽或耗时阈值。NPU Burn 必须产生完整 CSV/SDC 结果，NPU 超时
不能当作通过。daemon 保存 latest 和最近 100 次 history，CLI/Web 显示同一状态。

## 9. 从 Stress V1 迁移

```text
OLD_MONITORING_YAML_COMPATIBLE=true
OLD_STRESS_YAML_COMPATIBLE=false
```

只做监控的旧配置和旧 `docker run` 方式继续可用。Web 仍接受原有 `-config`，但该
参数在 V2 中只是 deprecated no-op；Web 从 snapshot 读取监控数据，并仅在 daemon
control socket 可用时启用 Stress API。未启用 Stress 时不需要 Docker socket、
workload 容器或 `/run/catmonitor/control.sock`。

启用 Stress 的旧 YAML 不兼容，升级时必须重新运行 generator。不要复制旧配置中的
脚本路径、CPU Runner socket、固定 NPU 容器或独立 Stress Web 字段。Web 只保留
`19322`，不再部署第二个 Stress Web listener。

迁移后按顺序验证：

```text
stress doctor → STREAM → HPCG → HPL → NPU Burn（如启用）
```

旧报告可以归档，但不能继续作为 V2 active job 状态写入。

## 10. 停止与故障排查

使用节点指南中的相同 Compose 文件和 profile，将 `up -d` 换为：

- `stop`：停止并保留容器；
- `start`：恢复；
- `down`：删除容器和网络；
- 不要使用 `down -v`，除非确认可以删除 snapshot 和 Stress history。

| 现象 | 检查 |
|---|---|
| Web snapshot 未就绪 | daemon `19320/-/ready`、daemon 日志和 snapshot volume |
| Stress 显示未配置 | generated override 是否加入 Compose、`stress doctor` |
| CPU benchmark unavailable | CPU workload 状态、MPI ABI、benchmark 资产和资源规模 |
| NPU benchmark unavailable | runtime preflight 中的驱动、CANN/torch_npu、设备数量和 PCI topology |
| `aclInit 507899` / device invalid | 使用 generator 生成的 A2 override；确认只有 NPU workload 为 privileged |
| Web 无法 Run/Cancel | `--enable-web`、daemon 状态、`19322` 网络策略 |
| Cancel 后仍有进程 | workload 容器进程；应按缺陷处理，不能忽略 |
| Registry 不可达 | 用 `docker save/load` 转移同一标签和 Image ID 的镜像 |

设计与测试人员可继续参考 [STRESS_DESIGN.md](STRESS_DESIGN.md) 和
[STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md)。
