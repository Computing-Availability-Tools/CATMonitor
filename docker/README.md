# CATMonitor 容器部署

本目录是 CATMonitor 新用户的容器部署入口。Docker Compose 是推荐入口；
`docker run` 是各节点指南均已写明的手工兼容入口，两种方式使用相同的镜像、配置和
运行契约。文档契约完整不等于每种硬件镜像都已经实机执行，验证范围见第 7 节。

> 当前程序内部版本是 `0.3.5`，当前 ARM64 pre-release 镜像标签是
> `arm64-v0.3.5-stress`，目标发布线是 `v0.3.6`。
>
> 该标签明确表示 Linux/ARM64 Stress 专用构建，不是通用 `v0.3.5` 或最终
> `v0.3.6` 镜像；源码提交、Image ID 与 registry digest 仍须单独记录。

## 1. 选择部署模式

| 节点 | Monitoring | CPU Stress | NPU Stress |
|---|---:|---:|---:|
| Generic CPU | ✅ | ✅ STREAM / HPL / HPCG | — |
| NVIDIA GPU | ✅ NVIDIA 指标 | ✅ STREAM / HPL / HPCG | — |
| Ascend NPU | ✅ Ascend 指标 | ✅ STREAM / HPL / HPCG | ✅ NPU Burn |

服务数量：

| 模式 | 容器 |
|---|---:|
| Monitoring-only | 3：`catmonitor`、`web`、`dfee` |
| CPU-only Stress | 4：Monitoring + `catmonitor-stress-cpu` |
| NPU-only Stress | 4：Monitoring + `catmonitor-stress-npu` |
| Ascend Full | 5：Monitoring + CPU + NPU workload |

## 2. Stress 镜像命名

当前已验收的构建平台是 Linux/ARM64，Stress 专用 tag 统一为
`arm64-v0.3.5-stress`：

| 职责 | 镜像名称 |
|---|---|
| Generic Control | `ghcr.io/spike677/catmonitor-generic:arm64-v0.3.5-stress` |
| NVIDIA Control | `ghcr.io/spike677/catmonitor-gpu:arm64-v0.3.5-stress`（当前为 Private，需先登录 GHCR） |
| Ascend Control | `ghcr.io/spike677/catmonitor-npu:arm64-v0.3.5-stress` |
| CPU workload | `ghcr.io/spike677/catmonitor-stress-cpu:arm64-v0.3.5-stress` |
| NPU workload | `ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.5-stress` |

Control 镜像包含 `catmonitor`、Web 与 DFeE。CPU workload 镜像包含
STREAM/HPL/HPCG；NPU workload 镜像包含 CANN/torch_npu/NPU Burn 运行环境。
三张 Control 镜像是“监控能力 + Stress Controller”的集成构建；CPU/NPU workload
镜像才是专用压测执行环境。五张镜像属于同一 Stress 候选集合，不能与普通 0.3.5
镜像混用。

## 3. 获取源码和镜像

使用与镜像 manifest 记录一致的 Stress release ref：

```bash
git clone https://github.com/Computing-Availability-Tools/CATMonitor.git
cd CATMonitor
git checkout refactor/unified-stress-platform
```

设置当前 pre-release registry namespace：

```bash
export CATMONITOR_REGISTRY='ghcr.io/spike677'
export CATMONITOR_RELEASE='arm64-v0.3.5-stress'
```

随后按节点文档拉取对应 Control 和可选 workload 镜像。

需要从源码构建 Control/Stress workload 镜像、配置构建镜像源或代理，或制作
RC/Release 镜像的开发者，请参阅 [镜像构建与发布开发者指南](DEVELOPER_GUIDE.md)。
普通部署不需要理解 build inputs、manifest 或 build daemon。

## 4. 唯一运行链路

```text
CLI / Web :19322
        ↓
catmonitor daemon
        ↓
CPU/NPU workload container（可选）
```

- daemon 是 snapshot 和 Stress 作业的唯一所有者；
- Web `:19322` 同时提供监控、Stress Run/Cancel/History；
- Web 与 DFeE 不挂 Docker Socket；
- 启用 Stress 时，仅 daemon 挂 Docker Socket，这是 V2 已记录的安全债务；
- 当前 Web operator 尚无认证/RBAC，应通过防火墙或反向代理限制访问。

## 5. 选择详细指南

| 文档 | 场景 |
|---|---|
| [README-generic.md](README-generic.md) | Generic Monitoring / CPU Stress |
| [README-gpu.md](README-gpu.md) | NVIDIA Monitoring / CPU Stress |
| [README-npu.md](README-npu.md) | Ascend Monitoring / CPU Stress / NPU Burn |
| [STRESS_USER_GUIDE.md](../features/stress/STRESS_USER_GUIDE.md) | Stress 配置、资源参数、操作、迁移和故障排查 |
| [DEVELOPER_GUIDE.md](DEVELOPER_GUIDE.md) | 五张镜像构建、mirror/proxy、manifest 与 RC 发布 |

## 6. 通用验证入口

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19322/ >/dev/null
curl -fsS http://127.0.0.1:19323/ >/dev/null
```

启用 Stress 后：

在对应节点指南的 Compose 命令中执行 `exec catmonitor`，或通过 Compose 的
project/service label 定位 daemon 后运行：

```bash
DAEMON_ID=$(docker ps \
  --filter label=com.docker.compose.project=catmonitor \
  --filter label=com.docker.compose.service=catmonitor \
  --format '{{.ID}}' | head -n 1)

docker exec "$DAEMON_ID" catmonitor stress doctor \
  -c /etc/catmonitor/catmonitor.yaml -o table
```

浏览器访问：

```text
http://<node-address>:19322/
```

## 7. Monitoring 兼容边界与验证范围

Unified Stress V2 是原 `develop` Monitoring 的增量能力，不替换原有部署契约：

- Monitoring-only 仍使用 `catmonitor`、`catmonitor-web`、`catmonitor-dfee`；
- daemon/Web/DFeE 程序、`catmonitor.yaml`/`metrics.yaml`、snapshot/data/straggler/CSV
  路径与 `19320`、`19321`、`19322`、`19323`、`9333` 端口保持兼容；
- 各硬件指南继续给出 Compose、Monitoring-only `docker run`、部分服务启动、独立
  DFeE、自定义配置及 Prometheus/Grafana 接入；
- 旧 Monitoring YAML 可以没有 `stress:`；旧 Stress V1 YAML 不能直接迁移。

```text
OLD_MONITORING_YAML_COMPATIBLE=true
OLD_STRESS_YAML_COMPATIBLE=false
```

当前验证口径：

| 路径 | 静态文档契约 | 独立实机验收 |
|---|---:|---:|
| Generic Monitoring / CPU Stress `docker run` | PASS | 待 Generic 节点闭环 |
| NVIDIA Monitoring / CPU Stress `docker run` | PASS | 待 NVIDIA 节点闭环 |
| Ascend A2 Monitoring / CPU-only / NPU-only / Full | PASS | PASS |

已有 A2 实机证据使用 Ascend Control 镜像，不能替代 Generic 或 NVIDIA Control 镜像
验收；后来增加的 Generic/GPU CPU 手工章节目前仅由静态契约测试覆盖。详细 workload
与版本边界见 [Stress 测试指南](../features/stress/STRESS_TEST_GUIDE.md)。
