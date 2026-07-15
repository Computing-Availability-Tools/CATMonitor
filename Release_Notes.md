# CATMonitor Release Notes

> 本文档按时间倒序记录每次发布的版本信息。每次发布在顶部追加，不删除历史记录。

---

## v0.2.2

| 项目 | 说明 |
|------|------|
| 版本号 | v0.2.2 |
| 发布时间 | 2026-07-15 |
| 发布人 | sunnytao |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 合并来源 | v0.2.1 分支 (79dc527) → main |

### 变更摘要

- **NPU 指标扩展**：5 → 74 指标，device 并行采集（每块 NPU 一个 goroutine，单卡失败不影响其他卡），全部指标 Linux 专属
- **来源层扩展**：新增 `dcmi`(CGo)/`npu_smi`/`hccn_tool`/`nvidia_smi` 4 个来源包，来源层 10 → 14 包，全部 6 个采集器接入来源层
- **DCMI CGo**：NPU 主体指标通过 `libdcmi.so`（`//go:build cgo && linux && dcmi`，`-tags dcmi` 启用），默认构建排除并优雅降级
- **GPU 迁移**：gpu collector 从内联 exec 改为调用 `nvidia_smi` 来源包（最后一个接入来源层的 collector）
- **总指标**：83 → 152
- **测试**：176 用例全过，`go vet` 零警告，Linux/Windows 双平台编译通过

### 已知限制

- DCMI CGo 未真机验证（需 NPU 服务器 `go build -tags dcmi`）；DCMI 原始单位待实测
- NPU device 并行未在真多卡环境验证
- 继承 v0.2.0/v0.2.1 已知限制：per-metric 周期未实现、Windows 来源层迁移延后、`-c` 短选项 bug

---

## v0.2.1

| 项目 | 说明 |
|------|------|
| 版本号 | v0.2.1 |
| 发布时间 | 2026-07-14 |
| 发布人 | sunnytao, ggboom12138 |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 合并来源 | feature/jw (5461263) → main |

### 变更摘要

- **Web 仪表盘（新模块）**：新增独立二进制 `catmonitor-web`（`web/` 目录），可视化单台服务器健康度与各部件采集指标。SPA 概览页（健康度面板 + 设备规格面板 + 部件芯片 + 概览卡网格 + 趋势 sparkline）+ 部件详情页（趋势面板 + 全部指标表）。与采集守护进程/CLI 完全解耦，不修改主项目任何文件
- **解耦架构**：以 `web/data/snapshot.json` 为读写解耦边界，采集 goroutine 为唯一写者（原子写），HTTP 层只读快照；`health`/`metrics` 字段直接复用主项目结构体，不重新定义
- **静态设备规格采集**：`hwinfo.go` 启动期一次性采集跨部件身份（device_model/gpu_info/npu_info/disk_info/net_info，外部命令缺失优雅降级）+ `collector.go` staticStash 缓存 CPU/内存首周期静态指标，合并写入每个快照的 `specs` 字段
- **端口占用自动回退**：`listenWithFallback` 启动时 `net.Listen` 探测，`EADDRINUSE` 时端口 +1 递增（默认 9527 → 9528…）直至空闲，跨平台有效
- **REST API**：`/api/snapshot`、`/api/collectors`、`GET|POST /api/config`（间隔热生效 + runtime.json 持久化）、`POST /api/refresh`
- **可扩展性**：新增部件采集器只需在 `web/main.go` 加一行 blank import，导航/概览卡/详情页自动出现；新增趋势 sparkline 在 `trackedSeries` 加一行 spec
- **零新增依赖**：前端原生 HTML/CSS/JS `//go:embed` 内嵌进二进制，go.mod 仍仅 `gopkg.in/yaml.v3`；`Web_SPEC.md` 为 Web 模块唯一设计与规格文档
- **版本号**：`cmd/catmonitor` version 升至 `0.2.1`
- **测试**：168 用例全过（collectors 62 / sources 70 / health 20 / web 16），`go vet` 零警告，Linux/Windows 双平台编译通过，CLI（~4.3MB）与 Web（~9.1MB）二进制构建成功

### 已知限制（后续跟进）

- Web 为单机本地视图，不含认证与多机聚合（预留多 snapshot 源聚合）
- Web 历史仅存内存环形缓冲，重启清空，未落盘（预留 JSONL 持久化）
- Web 前端轮询而非推送（预留 WebSocket/SSE）
- 继承 v0.2.0 已知限制：gpu/npu 未迁移来源层、per-metric 周期未实现、Windows 来源层迁移延后、`-c` 短选项 bug

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
