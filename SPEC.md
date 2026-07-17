# CATMonitor 技术规格说明书 (SPEC)

> **CATMonitor** — Computing Availability Tools Monitor
> 服务器运行指标采集与健康度评估守护进程

---

## 1. 概述

### 1.1 软件定位

CATMonitor 是 CAT (Computing Availability Tools) 系列软件之一，用于采集服务器各部件（CPU、内存、硬盘、GPU、NPU、网卡等）的运行指标，并基于采集结果评估服务器整体健康度。

### 1.2 技术栈

| 项目 | 选型 |
|------|------|
| 开发语言 | Go |
| 目标平台 | Linux / Windows |
| 数据输出 | 本地文件 (JSON) |
| 运行模式 | 常驻守护进程 |
| 配置文件 | YAML |
| 最低 Go 版本 | 1.21 |
| Windows 依赖 | 仅 Go 标准库（syscall + os/exec），无第三方依赖 |

### 1.3 核心需求

1. **易扩展**：新增部件采集器只需实现统一接口并注册，无需修改核心代码
2. **可配置**：每个指标的采集周期、是否启用均可通过配置文件调整
3. **健康度独立**：评分规则与采集逻辑解耦，便于后续修改评价规则
4. **可测试**：内置测试框架，每增加一个指标即验证，每阶段输出测试报告
5. **跨平台**：通过 Go 构建标签（build tags）隔离平台代码，Linux 和 Windows 共享采集器核心逻辑，仅数据采集层分离
6. **来源层抽象**（v0.2.0 引入，v0.2.2 扩展至全 6 采集器）：数据获取与解析集中在 `internal/source/`（14 个包），采集器不再直接 `os.ReadFile`/`exec`；来源返回 parsed struct，单例 + 可注入 fetcher，部分来源（ipmi/dmesg/smartctl/hccn_tool）带缓存 + 失败缓存，避免无硬件时反复 exec
7. **Web 可视化**（v0.2.1）：独立 `features/web` 模块提供 Web 仪表盘，与采集守护进程/CLI 解耦，零新依赖；新增部件/指标时前端尽量自动出现，最多在一处加一行
8. **NPU 指标扩展与 device 并行**（v0.2.2）：NPU 指标 5→74，采集器层 device 并行（每块 NPU 一个 goroutine，单卡失败不影响其他卡）；DCMI 指标通过 CGo 绑定 `libdcmi.so`（`//go:build cgo && linux && dcmi`，`-tags dcmi` 启用，默认构建排除并优雅降级）；GPU 迁移至 `nvidia_smi` 来源包，全部 6 个采集器接入来源层
9. **指标采集目录**（v0.3.0）：`internal/metrics` 提供指标目录（MetricSpec/Catalog/Filter），`configs/metrics.yaml` 为默认目录，模块可用自有 `metrics.yaml` 按 name 覆盖合并；High/Medium + 静态身份默认采、Low 诊断默认不采，scheduler 经 Filter 决定是否采集（interval 本期仅记录、不接 ticker）
10. **特性层抽取**（v0.3.0）：`features/` 承载上层模块——`features/health`（健康度评估，消费 `collector.Metric`，按部件评估器 + 局部 scheme，规则对齐 indi_list High/Medium）、`features/web`（Web 仪表盘由 `web/` 迁入）
11. **Chassis 机箱环境采集**（v0.3.1）：新增第 7 个采集器 `internal/collectors/chassis`（5 指标：整机功耗 / 进出风口温度 / 风扇转速 / 风扇功率，来自 ipmitool SDR），与 CPU/Memory 共享 SDR 缓存
12. **能效监控模块**（v0.3.1）：新增 `features/dfee` 能效监控模块（25 张实时图表 + CPU 利用率推导 + 网络差值），从 159 项指标中过滤 74 项能效指标，独立 SPA 路由 `/dfee/`，不修改现有 web 业务代码
13. **Disk 读/写耗时**（v0.3.1）：Disk 采集器新增 `read_latency`/`write_latency` 指标（/proc/diskstats field 7/11，ms/s），Disk 指标 7→9

---

## 2. 健康度评估需求

### 2.1 权重方案

| 场景 | CPU | Memory | Disk | GPU/NPU | 合计 |
|------|-----|--------|------|---------|------|
| 无 GPU/NPU | 30 | 40 | 30 | — | 100 |
| 8卡服务器 | 10 | 20 | 10 | 60 | 100 |
| 4卡服务器 | 10 | 20 | 10 | 60 | 100 |

> 判定逻辑：检测系统中是否存在 GPU/NPU 设备（nvidia-smi / npu-smi 是否可用），有则使用加速卡方案，无则使用 CPU-only 方案。4卡与8卡暂使用同一权重，后续可差异化。

### 2.2 扣分规则

各部件按 High/Medium 指标设定扣分阈值，触发即按满额分百分比扣分。规则覆盖：CPU（使用率/温度/Load Average/MCE）、内存（使用率/CE/UCE/Swap/饱和度/碎片化）、硬盘（使用率/SMART/I/O Error/I/O Wait）、GPU/NPU（使用率/温度/显存/ECC/功耗）。多卡场景默认取最差卡扣分。

> 规则与阈值详见 [`features/health/HEALTH_SPEC.md`](features/health/HEALTH_SPEC.md)；健康度模块设计与按部件评估器见 [DESIGN.md §3](DESIGN.md)。

### 2.3 健康等级

| 得分范围 | 等级 | 含义 |
|----------|------|------|
| 90-100 | Excellent | 服务器运行良好 |
| 75-89 | Good | 轻微问题，建议关注 |
| 60-74 | Warning | 存在风险，需检查 |
| 0-59 | Critical | 严重问题，需立即处理 |

> 健康度模块位于特性层 `features/health/`（消费 `collector.Metric`，不做底层采集）。扣分规则按 `indi_list` 的 High/Medium 指标设计，详见 [`features/health/HEALTH_SPEC.md`](features/health/HEALTH_SPEC.md)。

---

## 3. 指标采集需求

> 完整清单见 [docs/CATMonitor_indi_list.md](docs/CATMonitor_indi_list.md)。

### 3.1 采集优先级

- **High**：核心运行指标，直接影响健康度判断，默认采集，周期 3-5s
- **Medium**：重要辅助指标，对健康度有参考价值，默认采集，周期 10-60s
- **Low**：诊断性指标，按需采集，默认不采集

### 3.2 各部件指标概要

> v0.2.0 通过来源层（`internal/source/`）扩展，CPU 7→40、Memory 6→19，disk/network 迁移到来源层（指标集不变）。v0.2.2 NPU 指标 5→74（device 并行 + DCMI CGo），GPU 迁移至来源层。v0.3.1 新增 Chassis 部件（5 指标）+ Disk read/write_latency（2 指标）。完整清单见 [docs/CATMonitor_indi_list.md](docs/CATMonitor_indi_list.md)。

| 部件 | 指标数 | High | Medium | Low |
|------|--------|------|--------|-----|
| CPU | 40 | 4 | 12 | 24 |
| Memory | 19 | 4 | 7 | 8 |
| Disk | 9 | 1 | 5 | 3 |
| GPU | 7 | 3 | 3 | 1 |
| NPU | 74 | 9 | 43 | 22 |
| Network | 5 | 1 | 3 | 1 |
| Chassis | 5 | 2 | 3 | 0 |
| **合计** | **159** | **24** | **76** | **59** |

### 3.3 每个指标必须包含的属性

1. **采集优先级**：High / Medium / Low
2. **默认采集周期**：影响性能的设长一些，重要指标可 3 秒采集一次
3. **默认是否采集**：High 默认采集，Low 默认不采集，Medium 自行判断

---

## 4. 开发阶段划分

### Phase 1 — 核心架构 + High 优先级指标

**目标**：搭建可运行的核心框架，完成 CPU/内存/硬盘/网络 的核心指标采集。

| 序号 | 任务 | 说明 |
|------|------|------|
| 1.1 | 项目初始化 | go mod init, 目录结构, Makefile |
| 1.2 | 核心: Collector 接口 + Registry | `internal/collector/collector.go`, `registry.go` |
| 1.3 | 核心: Scheduler 调度引擎 | 定时触发各 Collector，支持各自周期 |
| 1.4 | 配置管理 | `internal/config/`, YAML 配置加载 |
| 1.5 | 数据存储 | `internal/storage/`, JSONL 文件写入 |
| 1.6 | 测试框架 | `tests/framework.go`, testdata 模拟数据 |
| 1.7 | CPU 采集器 | usage (高), load_average (高) |
| 1.8 | Memory 采集器 | usage (高), swap_usage (高), ECC错误 (高) |
| 1.9 | Disk 采集器 | space_usage (高) |
| 1.10 | Network 采集器 | throughput (高) |
| 1.11 | 健康度模块 (CPU-only) | 无 GPU/NPU 方案评分 |
| 1.12 | 守护进程入口 | `cmd/catmonitor/main.go`, 信号处理, 优雅退出 |
| 1.13 | Phase 1 完整测试 | 输出 `docs/test_report.md` |

### Phase 2 — Medium 优先级指标 + GPU/NPU

**目标**：补全中优先级指标，增加 GPU 和 NPU 采集器，完善健康度评分。

| 序号 | 任务 | 说明 |
|------|------|------|
| 2.1 | CPU 扩展 | temperature (中), frequency (中) |
| 2.2 | Memory 扩展 | oom_count (中) |
| 2.3 | Disk 扩展 | iops (中), throughput (中), io_wait (中) |
| 2.4 | GPU 采集器 | utilization, memory_usage, temperature (高); power_draw, fan_speed, ecc_errors (中) |
| 2.5 | NPU 采集器 | utilization, memory_usage, temperature (高); power_draw, health_status (中) |
| 2.6 | Network 扩展 | packet_count (中), error_count (中), interface_status (中) |
| 2.7 | 健康度模块 (加速卡方案) | 8卡/4卡权重方案, GPU/NPU 扣分规则 |
| 2.8 | Phase 2 完整测试 | 更新 `docs/test_report.md` |

### Phase 3 — Low 优先级指标 + 完善

**目标**：补全低优先级指标，完善配置和文档。

| 序号 | 任务 | 说明 |
|------|------|------|
| 3.1 | CPU 扩展 | context_switches (低), process_count (低), model_info (低) |
| 3.2 | Memory 扩展 | page_faults (低) |
| 3.3 | Disk 扩展 | smart_status (低), smart_temperature (低), io_errors (低) |
| 3.4 | GPU 扩展 | clock_frequency (低) |
| 3.5 | Network 扩展 | connection_count (低) |
| 3.6 | systemd 集成 | `scripts/install.sh`, service unit 文件 |
| 3.7 | Phase 3 完整测试 | 最终 `docs/test_report.md` |

---

## 5. 测试要求

### 5.1 测试流程要求

1. **每增加一个指标采集，必须验证采集是否正确**。测试不通过则修改代码重新测试，直到通过。
2. **每完成一个阶段的开发，做一次完整测试**，输出正式测试报告 `docs/test_report.md`。
3. 测试过程中遇到的问题自行解决，不依赖外部协助。

### 5.2 测试覆盖范围

| 层级 | 范围 |
|------|------|
| 单元测试 | 每个采集器独立 |
| 集成测试 | 多采集器协同 + 调度引擎 |
| 健康度测试 | 评分计算正确性 |
| Mock 测试 | GPU/NPU 无硬件场景 |
| 端到端测试 | 守护进程启动→采集→存储→评分 |

---

## 6. 配置规格

`configs/catmonitor.yaml`：

```yaml
# CATMonitor 配置文件
server:
  # 服务器类型: "cpu_only" | "accelerated"
  # 自动检测: 如果检测到 GPU 或 NPU 则用 accelerated
  # 也可以手动指定
  type: auto

collectors:
  # 每个采集器可独立配置
  cpu:
    enabled: true
    interval: 3s
  memory:
    enabled: true
    interval: 3s
  disk:
    enabled: true
    interval: 5s
  gpu:
    enabled: true          # 有硬件时自动启用
    interval: 3s
  npu:
    enabled: true
    interval: 3s
  network:
    enabled: true
    interval: 3s

storage:
  data_dir: /var/lib/catmonitor/data
  max_file_age: 168h       # 数据文件保留时长（默认7天）
  rotation: daily           # 按天轮转

health:
  enabled: true
  interval: 5s              # 健康度计算周期
  weight_scheme: auto       # auto | cpu_only | accelerated_8card | accelerated_4card
```

---

## 7. 依赖要求

尽量减少外部依赖，优先使用 Go 标准库。

| 依赖 | 用途 | 备注 |
|------|------|------|
| gopkg.in/yaml.v3 | 配置文件解析 | 唯一必需的第三方依赖 |
| Go 标准库 | /proc、/sys 文件读取、系统调用 | syscall, os, fmt 等 |
| Go 标准库 (Windows) | kernel32.dll, iphlpapi.dll 调用 | 通过 syscall.NewLazyDLL |

GPU 采集通过 `nvidia_smi` 来源包调用 `nvidia-smi`，NPU 通过 `dcmi`(CGo)/`npu_smi`/`hccn_tool` 来源包采集。DCMI 的 CGo 绑定由 `//go:build cgo && linux && dcmi` 隔离，默认构建不启用 CGo、跨环境可编译；NPU 服务器以 `go build -tags dcmi` 启用 `libdcmi.so` 绑定。Windows 平台通过 Go `syscall` 包调用 kernel32.dll / iphlpapi.dll / PowerShell，无需额外第三方依赖。

### 7.1 来源层外部命令（Linux）

来源层 `internal/source/` 通过 `os/exec` 调用系统命令（无硬件/未安装时返回空，优雅降级，失败结果同样缓存以避免反复 exec）。各来源包的外部命令、用途与缓存策略详见 [DESIGN.md §1.6 来源层设计](DESIGN.md)。

| 来源包 | 外部命令 | 缓存 |
|--------|---------|------|
| ipmi / lscpu / mce / dmesg / dmidecode / smartctl | ipmitool / lscpu / mcelog / dmesg / dmidecode / smartctl | 常驻或 TTL + 失败缓存 |
| nvidia_smi / npu_smi / hccn_tool | nvidia-smi / npu-smi -t / hccn_tool | 无缓存 或 Topo 常驻 / per-dev:opt TTL |
| dcmi | libdcmi.so (CGo，`-tags dcmi`) | 无缓存（进程内 CGo） |
| proc / sys / statfs | 纯文件读取/系统调用，无外部命令 | 无缓存 |

---

## 8. 非功能性需求

| 项目 | 要求 |
|------|------|
| 优雅退出 | 捕获 SIGINT/SIGTERM，等待当前采集周期完成 |
| 日志 | 使用 Go 标准库 `log/slog`，支持 JSON 格式 |
| 错误隔离 | 单个采集器失败不影响其他采集器 |
| 资源占用 | 目标内存 < 50MB，CPU < 2% |
| 数据轮转 | 按天生成文件，超期自动清理 |
| 跨平台 | Linux 和 Windows 双平台编译通过，`go build` 零错误 |

---

## 9. Web 仪表盘规格

> 详细设计与规格见 [`features/web/Web_SPEC.md`](features/web/Web_SPEC.md)，架构与数据流见 [DESIGN.md §6](DESIGN.md)。能效监控模块详见 [`features/dfee/dfee_SPEC.md`](features/dfee/dfee_SPEC.md)。本节仅列关键需求与约束。

### 9.1 概述

提供独立 Web 仪表盘二进制 `catmonitor-web`（`features/web`），可视化单台服务器的健康度与各部件采集指标。设计原则：

1. **解耦**：Web 服务与采集守护进程/CLI 完全解耦，不修改主项目任何文件；仅通过只读复用（blank import + 调用注册表/健康度接口）获取数据。
2. **多页面**：概览页（整体健康度 + 各部件关键指标 + 设备规格面板）+ 各部件详情页（详细指标 + 趋势）。
3. **可扩展**：新增部件类型/采集指标时尽可能自动出现，零代码或仅需一处一行的新增。
4. **极简依赖**：Go 标准库 + 已有 `gopkg.in/yaml.v3`，前端原生 HTML/CSS/JS，无构建步骤，零新依赖。

### 9.2 关键约束

- **解耦边界**：以 `features/web/data/snapshot.json` 为读写边界——采集 goroutine 为唯一写者（原子写），HTTP 层只读快照，**绝不直接调用采集器**。
- **端口占用自动回退**：`EADDRINUSE` 时端口 +1 重试（`:9527`→`:9528`…），跨平台有效。
- **静态设备规格**：启动期一次性采集硬件身份（`hwinfo.go`）+ CPU/内存首周期静态指标 stash，合并写入快照 `specs` 字段。
- **REST API**：`GET /api/snapshot`、`GET /api/collectors`、`GET|POST /api/config`、`POST /api/refresh`（详见 Web_SPEC）。
- **不应提交**：`features/web/data/*`、`features/web/bin/`（运行时/构建产物，已 git 忽略）。
- **测试**：`go test ./features/web/` 覆盖快照原子读写、历史环形缓冲、静态规格 stash、硬件身份采集、HTTP API 与端口回退。
