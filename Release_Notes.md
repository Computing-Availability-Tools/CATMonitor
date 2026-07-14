# CATMonitor Release Notes

> 本文档按时间倒序记录每次发布的版本信息。每次发布在顶部追加，不删除历史记录。

---

## v0.2.0

| 项目 | 说明 |
|------|------|
| 版本号 | v0.2.0 |
| 发布时间 | 2026-07-14 |
| 发布人 | sunnytao, ggboom12138 |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 合并来源 | feature/wyx/add-metrics (b114848) → main (merge 21c7083) |

### 变更摘要

- **来源层（source layer）**：新增 `internal/source/` 9 个来源包（proc/sys/ipmi/lscpu/mce/dmesg/dmidecode/statfs/smartctl），抽象数据获取与解析；采集器不再直接 `os.ReadFile`/`exec`，来源返回 parsed struct + 单例 + `SetRoot`/可注入 fetcher + 缓存
- **CPU 指标扩展 7 → 40**：拓扑/核状态/频率/缓存/BuddyInfo/MCE 错误/IPMI 温度功率
- **Memory 指标扩展 6 → 19**：usage_detail/swap/PSI 饱和度/碎片化/页计数/DIMM 模块/功率
- **disk/network 迁移**：迁移到来源层（指标集不变，行为不变）
- **平台抽象层**：`internal/platform` 抽象配置路径与数据目录跨平台化
- **健康度自动检测**：`Evaluate()` 根据是否存在 GPU/NPU 指标自动选择权重方案
- **缺陷修复 4 项**：/sys 符号链接过滤、swap 无 swap 机器产出、ipmitool negative cache、statfs build tag
- **测试**：141 用例全过（collectors 62 / sources 59 / health 20），覆盖率 69.0%~92.3%，`go vet` 零警告，Linux/Windows 双平台编译通过
- **零新增依赖**：go.mod 仍仅 `gopkg.in/yaml.v3`

### 已知限制（后续跟进）

- gpu/npu 未迁移到来源层（待建 nvsmi/npsmi 来源）
- health 未给 CPU MCE / Memory saturation 加扣分规则
- per-metric 采集周期未实现（仍为 per-collector interval）
- Windows 来源层迁移延后（`*_windows.go` 保留原实现，扩展指标当前 Linux 专有）
- `-c` 短选项 bug 未修（建议使用 `--config`）

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
