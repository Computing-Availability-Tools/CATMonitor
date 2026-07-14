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

---

## 2. 健康度评估需求

### 2.1 权重方案

| 场景 | CPU | Memory | Disk | GPU/NPU | 合计 |
|------|-----|--------|------|---------|------|
| 无 GPU/NPU | 30 | 40 | 30 | — | 100 |
| 8卡服务器 | 10 | 20 | 10 | 60 | 100 |
| 4卡服务器 | 10 | 20 | 10 | 60 | 100 |

> 判定逻辑：检测系统中是否存在 GPU/NPU 设备（nvidia-smi / npu-smi 是否可用），有则使用加速卡方案，无则使用 CPU-only 方案。4卡与8卡暂使用同一权重，后续可差异化。

### 2.2 扣分规则（初版，后续可调整）

**CPU（满额分随场景而定）**

| 触发条件 | 扣分 |
|----------|------|
| 使用率 > 90% 持续 | -20% 满额分 |
| 使用率 > 80% 持续 | -10% 满额分 |
| 温度 > 85°C | -30% 满额分 |
| 温度 > 75°C | -15% 满额分 |
| Load Average (1min) > CPU核心数 × 2 | -10% 满额分 |

**内存（满额分随场景而定）**

| 触发条件 | 扣分 |
|----------|------|
| 使用率 > 90% | -30% 满额分 |
| 使用率 > 80% | -15% 满额分 |
| 每个 CE 错误（可纠正错误） | -2 分 |
| 每个 UCE 错误（不可纠正错误） | -10 分 |
| Swap 使用率 > 50% | -10% 满额分 |

**硬盘（满额分随场景而定）**

| 触发条件 | 扣分 |
|----------|------|
| 分区使用率 > 90% | -40% 满额分 |
| 分区使用率 > 80% | -20% 满额分 |
| SMART 状态异常 | -30% 满额分 |
| I/O Error 计数 > 0 | -20% 满额分 |
| I/O Wait > 20% | -10% 满额分 |

**GPU/NPU（满额 60 分）**

| 触发条件 | 扣分 |
|----------|------|
| 使用率 > 95% 持续 | -10% 满额分 |
| 温度 > 90°C | -30% 满额分 |
| 温度 > 80°C | -15% 满额分 |
| 显存使用率 > 95% | -10% 满额分 |
| ECC 错误（不可纠正） > 0 | -20% 满额分 |
| 功耗 > 额定 TDP 的 110% | -15% 满额分 |

> 多卡场景：取所有卡的平均扣分或最差卡扣分（配置项，默认取最差卡）。

### 2.3 健康等级

| 得分范围 | 等级 | 含义 |
|----------|------|------|
| 90-100 | Excellent | 服务器运行良好 |
| 75-89 | Good | 轻微问题，建议关注 |
| 60-74 | Warning | 存在风险，需检查 |
| 0-59 | Critical | 严重问题，需立即处理 |

---

## 3. 指标采集需求

> 完整清单见 [docs/CATMonitor_indi_list.md](docs/CATMonitor_indi_list.md)。

### 3.1 采集优先级

- **High**：核心运行指标，直接影响健康度判断，默认采集，周期 3-5s
- **Medium**：重要辅助指标，对健康度有参考价值，默认采集，周期 10-60s
- **Low**：诊断性指标，按需采集，默认不采集

### 3.2 各部件指标概要

| 部件 | 指标数 | High | Medium | Low |
|------|--------|------|--------|-----|
| CPU | 7 | 2 | 2 | 3 |
| Memory | 6 | 4 | 1 | 1 |
| Disk | 7 | 1 | 3 | 3 |
| GPU | 7 | 3 | 3 | 1 |
| NPU | 5 | 3 | 2 | 0 |
| Network | 5 | 1 | 3 | 1 |
| **合计** | **37** | **14** | **14** | **9** |

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

GPU 采集通过 `os/exec` 调用 `nvidia-smi`，NPU 通过调用 `npu-smi`，不引入 CGo 绑定，保证跨环境编译。Windows 平台通过 Go `syscall` 包调用 kernel32.dll / iphlpapi.dll / PowerShell，无需额外第三方依赖。

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
