# CATMonitor 容器化使用文档

CATMonitor 控制面支持三种镜像，按服务器硬件选择：

| 镜像 | 适用环境 | 基础系统 | 说明 |
|------|---------|---------|------|
| `catmonitor-npu` | 有 Ascend NPU | Debian (glibc) | CGo 编译，链接 libdcmi.so，采集 123 项 NPU 指标 |
| `catmonitor-gpu` | 有 NVIDIA GPU | Debian (glibc) | 纯 Go 编译，glibc 兼容宿主机 nvidia-smi |
| `catmonitor-generic` | 纯 CPU（无 NPU/GPU） | Alpine (musl) | 纯 Go 编译，镜像最小 |

三个服务可以组合使用：

| 服务 | 容器端口 | 功能 |
|------|---------|------|
| `catmonitor` (daemon) | 19320, 19321 | 采集指标 + Prometheus 导出 + snapshot 写入 + faultsub |
| `web` | 19322 | Web 仪表盘（读 snapshot） |
| `dfee` | 19323, 9333 | 能效监控 SPA + Prometheus exporter + CSV 输出 |

daemon 是 snapshot 唯一生产者；web/dfee 是只读消费者，不自行采集。三容器共享一个 snapshot 卷。

## 按环境选择文档

| 文档 | 适用场景 |
|------|---------|
| [README-npu.md](README-npu.md) | Ascend NPU 服务器（镜像构建、NPU 挂载、可靠性压测、faultsub 故障订阅） |
| [README-gpu.md](README-gpu.md) | NVIDIA GPU 服务器（镜像构建、nvidia-smi 挂载、GPU 指标验证） |
| [README-generic.md](README-generic.md) | 通用 CPU 服务器（Alpine 镜像、无 NPU/GPU 依赖） |
