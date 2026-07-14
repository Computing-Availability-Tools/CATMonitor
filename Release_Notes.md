# CATMonitor Release Notes

> 本文档按时间倒序记录每次发布的版本信息。每次发布在顶部追加，不删除历史记录。

---

## v0.1.1

| 项目 | 说明 |
|------|------|
| 版本号 | v0.1.1 |
| 发布时间 | 2026-07-12 |
| 发布人 | sunnytao |
| 平台支持 | Linux (x86_64), Windows (x86_64) |

### 变更摘要

- **跨平台支持**：新增 Windows 平台适配，通过 Go 构建标签（build tags）隔离平台代码
- **6 个采集器**：CPU、Memory、Disk、GPU、NPU、Network 全部支持双平台
- **37 个采集指标**：Linux 全部 37 个，Windows 可用 32 个（5 个无可靠数据源优雅降级）
- **健康度评估**：新增 GPU/NPU 自动检测逻辑，根据实际采集指标自动切换权重方案
- **零新增依赖**：Windows 通过 Go 标准库 syscall 调用 kernel32.dll，go.mod 仍仅 yaml.v3
- **平台抽象层**：新增 `internal/platform` 包，统一管理跨平台默认路径

---

## v0.1.0

| 项目 | 说明 |
|------|------|
| 版本号 | v0.1.0 |
| 发布时间 | 2026-07-10 |
| 发布人 | sunnytao |
| 平台支持 | Linux (x86_64) |

### 变更摘要

- **核心架构**：Collector 接口 + Registry 注册表 + Scheduler 调度引擎
- **6 个采集器**：CPU、Memory、Disk、GPU、NPU、Network
- **37 个采集指标**：覆盖全部 6 个部件（High 14, Medium 14, Low 9）
- **健康度评估**：CPU-only 和 Accelerated 双权重方案，阈值扣分规则
- **CLI 命令**：daemon、collect、health、status、list、version
- **数据存储**：JSONL 格式，按天轮转
- **外部依赖**：仅 gopkg.in/yaml.v3
