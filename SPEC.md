# CATMonitor 功能规格说明书 (SPEC)

> **CATMonitor** — Computing Availability Tools Monitor
> 服务器运行指标采集、健康度评估与 Prometheus 导出守护进程
>
> 本文档描述 CATMonitor 的功能需求，不含技术实现细节。架构与模块设计见 [DESIGN.md](DESIGN.md)，使用方法见 [使用手册](docs/User_Manual.md)，指标清单见 [docs/CATMonitor_indi_list.md](docs/CATMonitor_indi_list.md)。各特性的详细规格见对应 `features/<feature>/*_SPEC.md`。

---

## 1. 概述

### 1.1 软件定位

CATMonitor 是 CAT (Computing Availability Tools) 系列软件之一，用于采集服务器各部件（CPU、内存、硬盘、GPU、NPU、网卡、机箱等）的运行指标，基于采集结果评估服务器整体健康度，并以 Prometheus 兼容格式导出供长期存储与告警。

### 1.2 核心需求

1. **多部件指标采集**：覆盖 CPU、内存、硬盘、GPU、NPU、网卡、机箱共 7 个部件，**204 个指标**。
2. **健康度评估**：基于采集指标自动计算 0-100 健康分，自动检测 GPU/NPU 切换权重方案，输出等级与扣分明细。
3. **Prometheus 导出**：daemon 内置 `/metrics` 端点，以 Prometheus 文本格式导出全部采集指标，零额外进程。
4. **Web 可视化**：独立 Web 仪表盘二进制，可视化单机健康度与各部件指标。
5. **能效监控**：能效指标实时图表 SPA，支持交互式筛选与卡片拖拽缩放。
6. **易扩展**：新增部件采集器只需实现统一接口并注册，核心代码零修改。
7. **可配置**：每个指标的采集周期、是否启用、采集优先级均可通过配置调整。
8. **跨平台**：Linux 与 Windows 双平台支持。
9. **优雅降级**：无 GPU/NPU/BMC 等硬件或工具缺失时，对应采集器返回空、不崩溃、不影响其它采集器。

### 1.3 技术约束（概要）

| 项目 | 约束 |
|------|------|
| 开发语言 | Go 1.21+ |
| 目标平台 | Linux / Windows |
| 运行模式 | 常驻守护进程 |
| 配置文件 | YAML |
| 外部依赖 | 仅 `gopkg.in/yaml.v3`；GPU 经 `nvidia-smi`、NPU 经 `dcmi`(CGo)/`npu-smi`/`hccn_tool`、机箱经 `ipmitool`，默认构建无 CGo |
| 输出 | 本地 JSONL 文件 + Prometheus 文本（`/metrics`） |

> 技术实现、目录结构、数据流见 [DESIGN.md](DESIGN.md)。

---

## 2. 功能模块

CATMonitor 由采集核心与特性层组成。特性层各模块独立成包，均有自己的规格文档：

| 模块 | 功能 | 规格 |
|------|------|------|
| 采集核心 | Collector 接口 + Registry 注册表 + Scheduler 调度，7 个部件采集器 + 来源层（14 包） + 指标采集目录 | [DESIGN.md](DESIGN.md) §1-2 |
| `features/health` | 健康度评估：消费采集指标，按部件评估器 + 权重自适应，输出总分/等级/扣分明细 | [HEALTH_SPEC.md](features/health/HEALTH_SPEC.md) |
| `features/web` | Web 仪表盘二进制：概览页 + 部件详情页 + 趋势 + 设备规格，REST API | [Web_SPEC.md](features/web/Web_SPEC.md) |
| `features/dfee` | 能效监控 SPA：能效指标过滤 + CPU 利用率推导 + 网络差值，交互式实时图表 | [dfee_SPEC.md](features/dfee/dfee_SPEC.md) |
| `features/exporter` | Prometheus 导出：CachingStorage 包装存储 + `/metrics` 端点 + 健康端点 | [exporter_SPEC.md](features/exporter/exporter_SPEC.md) |

---

## 3. 健康度评估

### 3.1 权重方案

| 场景 | CPU | Memory | Disk | GPU/NPU | 合计 |
|------|-----|--------|------|---------|------|
| 无 GPU/NPU（cpu_only） | 30 | 40 | 30 | — | 100 |
| 有 GPU/NPU（accelerated） | 10 | 20 | 10 | 60 | 100 |

> 自动检测：根据实际采集到的指标是否含 GPU/NPU 指标自动选择方案（非命令是否存在），无硬件或采集失败时均能正确选择。4 卡与 8 卡暂用同一权重，后续可差异化。

### 3.2 等级

| 得分范围 | 等级 | 含义 |
|----------|------|------|
| 90-100 | Excellent | 服务器运行良好 |
| 75-89 | Good | 轻微问题，建议关注 |
| 60-74 | Warning | 存在风险，需检查 |
| 0-59 | Critical | 严重问题，需立即处理 |

### 3.3 扣分规则

各部件按 High/Medium 指标设定扣分阈值，触发即按满额分百分比扣分，多卡场景取最差卡。覆盖：
- **CPU**：使用率、温度、Load Average、MCE
- **内存**：使用率、CE/UCE 错误、Swap、饱和度、碎片化
- **硬盘**：使用率、SMART、I/O Error、I/O Wait
- **GPU/NPU**：使用率、温度、显存、ECC、功耗、错误码

> 规则与阈值详见 [features/health/HEALTH_SPEC.md](features/health/HEALTH_SPEC.md)。

---

## 4. 指标采集

> 完整清单见 [docs/CATMonitor_indi_list.md](docs/CATMonitor_indi_list.md)。

### 4.1 采集优先级

- **High**：核心运行指标，直接影响健康度判断，默认采集，周期 3-5s
- **Medium**：重要辅助指标，对健康度有参考价值，默认采集，周期 10-60s
- **Low**：诊断性指标，按需采集，默认不采集

### 4.2 各部件指标概要

| 部件 | 指标数 | High | Medium | Low | Linux | Windows |
|------|--------|------|--------|-----|:-----:|:-------:|
| CPU | 40 | 4 | 12 | 24 | ✅ | ✅（基础指标，扩展指标 Linux 专有） |
| Memory | 19 | 4 | 7 | 8 | ✅ | ✅（同上） |
| Disk | 9 | 1 | 5 | 3 | ✅ | ✅（2/9） |
| GPU | 7 | 3 | 3 | 1 | ✅ | ✅（7/7） |
| NPU | 119 | 9 | 88 | 22 | ✅ | ✗（Linux 专有；DCMI 走 CGo `-tags dcmi`，Windows no-op 降级） |
| Network | 5 | 1 | 3 | 1 | ✅ | ✅（5/5） |
| Chassis | 5 | 2 | 3 | 0 | ✅ | ✗（Linux 专有，依赖 ipmitool） |
| **合计** | **204** | **24** | **121** | **59** | | |

> v0.3.2 NPU 指标 74→119（新增 45 项 `hccn_tool` 网络统计指标，Medium），指标总数 159→204。

### 4.3 指标采集目录

为统一管控"采哪些指标、按什么优先级、默认是否采集"，提供指标采集目录（`configs/metrics.yaml` 为默认目录，模块可用自有 `metrics.yaml` 按 name 覆盖合并）。默认策略：High/Medium + 静态身份指标默认采集，Low 诊断指标默认不采集。目录中缺失的指标默认放行，避免静默丢数据。

---

## 5. 命令行功能

| 子命令 | 功能 |
|--------|------|
| `daemon` | 启动守护进程：持续采集 + 健康度评估 + Prometheus 导出（默认） |
| `collect` | 单次采集所有指标，输出 JSON 或表格 |
| `health` | 基于当前指标执行一次健康检查，输出评估报告 |
| `list` | 列出所有已注册采集器 |
| `version` | 显示版本信息 |

| 参数 | 说明 |
|------|------|
| `-c, --config` | 配置文件路径（平台自适应默认值） |
| `-o, --output` | 输出格式：`json` / `table` |
| `-h, --help` | 帮助信息（解析后即退出） |

> 完整用法与示例见 [使用手册](docs/User_Manual.md)。

---

## 6. Web 仪表盘与能效监控

### 6.1 Web 仪表盘

独立二进制 `catmonitor-web`，可视化单台服务器的健康度与各部件采集指标。与采集守护进程/CLI 解耦，以 `snapshot.json` 为读写边界。

- **概览页**：整体健康度 + 设备规格面板 + 各部件状态 + 部件概览卡片（趋势 sparkline）
- **部件详情页**：部件得分/扣分项 + 趋势面板 + 全部指标表
- **REST API**：`GET /api/snapshot`、`GET /api/collectors`、`GET|POST /api/config`、`POST /api/refresh`
- **端口回退**：`:9527` 被占用时自动 +1 递增

> 详见 [features/web/Web_SPEC.md](features/web/Web_SPEC.md)。

### 6.2 能效监控（dfee）

`/dfee/` SPA，专门展示能效相关指标的实时图表。

- **能效指标过滤**：从全量指标过滤能效项，按部件分组
- **CPU 利用率推导**：8 个原始 jiffies → 7 项利用率百分比（后端有状态 delta）
- **网络字节差值**：累计值 → 采集间增量
- **交互**：卡片拖拽重排 + 手柄缩放 + 虚线对齐辅助、多选下拉筛选、模块折叠
- **解耦**：独立 Go package，只读 `snapshot.json`，不修改现有 web 业务代码

> 详见 [features/dfee/dfee_SPEC.md](features/dfee/dfee_SPEC.md)。

---

## 7. Prometheus 导出

daemon 内置 Prometheus 兼容导出，无需额外进程。

- **端点**：`GET /metrics`（`:9100`）输出 Prometheus 文本格式全部采集指标
- **命名**：`catmonitor_{component}_{name}`，特殊字符替换为 `_`
- **类型**：累计型（`_total`/`_time` 后缀）为 counter，其余为 gauge，含 `# HELP`/`# TYPE` 头
- **健康端点**：`GET /-/healthy`（存活）、`GET /-/ready`（缓存就绪）
- **零侵入**：CachingStorage 包装在 JSONLStorage 外，一次采集同时落盘 + 缓存导出

> 详见 [features/exporter/exporter_SPEC.md](features/exporter/exporter_SPEC.md)。

---

## 8. 配置规格

`configs/catmonitor.yaml`：

```yaml
server:
  type: auto              # auto | cpu_only | accelerated

collectors:               # 每个采集器可独立配置 enabled + interval
  chassis:  { enabled: true, interval: 3s }
  cpu:      { enabled: true, interval: 3s }
  memory:   { enabled: true, interval: 3s }
  disk:     { enabled: true, interval: 5s }
  gpu:      { enabled: true, interval: 3s }
  npu:      { enabled: true, interval: 3s }
  network:  { enabled: true, interval: 3s }

storage:
  data_dir: /var/lib/catmonitor/data
  max_file_age: 168h
  rotation: daily

health:
  enabled: true
  interval: 5s
  weight_scheme: auto     # auto | cpu_only | accelerated_8card | accelerated_4card
```

---

## 9. 非功能性需求

| 项目 | 要求 |
|------|------|
| 优雅退出 | 捕获 SIGINT/SIGTERM，等待当前采集周期完成 |
| 日志 | Go 标准库 `log/slog` |
| 错误隔离 | 单个采集器失败不影响其他采集器 |
| 资源占用 | 目标内存 < 50MB，CPU < 2% |
| 数据轮转 | 按天生成文件，超期自动清理 |
| 跨平台 | Linux 和 Windows 双平台编译通过 |
| 优雅降级 | 无 GPU/NPU/BMC/工具时对应采集器返回空、不崩溃 |

---

## 10. 测试要求

1. **每增加一个指标采集，必须验证采集是否正确**，不通过则修改重新测试。
2. **每完成一个阶段，做一次完整测试**，输出测试报告 `docs/test_report.md`。
3. **无硬件由 mock 驱动**：GPU/NPU/Chassis 在无硬件环境用 mock + 优雅降级路径验证。

| 层级 | 范围 |
|------|------|
| 单元测试 | 每个采集器/来源包/特性模块独立 |
| 集成测试 | 多采集器协同 + 调度引擎 |
| 健康度测试 | 评分计算正确性 |
| Mock 测试 | GPU/NPU 无硬件场景 |
| 端到端测试 | 守护进程启动→采集→存储→评分→导出 |

> 当前测试结果见 [docs/test_report.md](docs/test_report.md)（v0.3.2：263 单元测试 PASS + 无 NPU/GPU 系统测试）。

---

## 11. 版本演进

| 版本 | 主要内容 |
|------|----------|
| v0.3.2 | 新增 Prometheus exporter（`:9100/metrics`）；NPU 新增 45 项 `hccn_tool` 网络统计（74→119）；IPMI 来源层重构（`sdr→sensor`、定向采集、两级缓存、降级回退）；dfee 能效监控增强（卡片拖拽缩放、多选下拉筛选、模块折叠）；`--help` 解析后退出 |
| v0.3.1 | 新增 Chassis 机箱环境采集器（5 指标）、Disk 读/写耗时、`features/dfee` 能效监控模块 |
| v0.3.0 | 引入指标采集目录 + `features/` 特性层（health/web），健康度抽取为按部件评估器 |
| v0.2.x | 来源层引入与扩展（14 包）、NPU 指标 5→74 device 并行、Web 仪表盘 |
