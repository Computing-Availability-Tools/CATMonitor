# CATMonitor v0.3.6 容器部署

本目录是 CATMonitor 新用户的容器部署入口。推荐使用 Docker Compose；
`docker run` 仅作为故障排查参考。

> v0.3.6 镜像尚待 Fresh Image Acceptance。正式发行名以 `v0.3.6` 为目标；RC
> 构建只能使用 `v0.3.6-rc.<shortsha>`，不得提前创建正式 image/tag。registry
> namespace、Image ID 与 digest 在正式发布后补齐，不能复用 a2-r1。

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

## 2. v0.3.6 镜像命名

最终 tag 统一为 `v0.3.6`：

| 职责 | 镜像名称 |
|---|---|
| Generic Control | `<registry>/catmonitor-generic:v0.3.6` |
| NVIDIA Control | `<registry>/catmonitor-gpu:v0.3.6` |
| Ascend Control | `<registry>/catmonitor-npu:v0.3.6` |
| CPU workload | `<registry>/catmonitor-stress-cpu:v0.3.6` |
| NPU workload | `<registry>/catmonitor-stress-npu:v0.3.6` |

Control 镜像包含 `catmonitor`、Web 与 DFeE。CPU workload 镜像包含
STREAM/HPL/HPCG；NPU workload 镜像包含 CANN/torch_npu/NPU Burn 运行环境。
三类 V2 镜像都必须重新构建和验收。

## 3. 获取源码和镜像

正式发布后使用 v0.3.6 release ref：

```bash
git clone https://github.com/Computing-Availability-Tools/CATMonitor.git
cd CATMonitor
git checkout <v0.3.6-release-ref>
```

设置发布后的 registry namespace；当前草案不要把占位符直接复制执行：

```bash
export CATMONITOR_REGISTRY='<registry>'
export CATMONITOR_RELEASE='v0.3.6-rc.<shortsha>'
```

随后按节点文档拉取对应 Control 和可选 workload 镜像。

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
| [STRESS_USER_GUIDE.md](../features/stress/STRESS_USER_GUIDE.md) | 镜像构建、资源参数、迁移和故障排查 |

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
