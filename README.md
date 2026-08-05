# CATMonitor

> **Computing Availability Tools Monitor** — 服务器运行指标采集、健康度评估与 Prometheus 导出守护进程

CATMonitor 是 CAT (Computing Availability Tools) 系列软件之一，用于采集服务器各部件（CPU、内存、硬盘、GPU、NPU、网卡、机箱等）的运行指标，基于采集结果评估服务器整体健康度，并以 Prometheus 格式导出供长期存储与告警。

## 版本信息

| 项目 | 说明 |
|------|------|
| 版本号 | v0.3.3 |
| 发布时间 | 2026-07-28 |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 许可证 | Apache-2.0（见 [LICENSE](LICENSE)） |

## 功能特性

- **多部件采集**：CPU / 内存 / 硬盘 / GPU / NPU / 网卡 / 机箱 共 7 个部件，**204 个指标**（详见 [指标清单](docs/CATMonitor_indi_list.md)）
- **健康度评估**：基于采集指标自动计算 0-100 健康分，自动检测 GPU/NPU 切换权重方案
- **Snapshot 统一生产**：daemon 作为唯一 snapshot 生产者，产出 per-component `snapshot_<comp>.json` + 全局 `snapshot.json`（health/collectors/intervals/system_specs）；只读特性（web/dfee）消费快照而不再各自采集，避免重复跑硬件
- **Web 仪表盘**：独立 `catmonitor-web` 二进制，**只读消费** daemon 产出的 snapshot，可视化单机健康度与各部件指标，默认端口 9527
- **能效监控（dfee）**：独立 `catmonitor-dfee` 二进制，**只读消费** snapshot 渲染能效指标实时图表 SPA（卡片拖拽缩放、多选下拉筛选、模块折叠），默认端口 9528
- **Prometheus 导出（exporter）**：daemon 内置 `/metrics` 端点（`:9100`），一次采集同时落盘 JSONL + 缓存导出，零额外进程
- **指标采集目录**：`configs/metrics.yaml` 统一管控采哪些指标、优先级、默认是否采集，模块可覆盖
- **Feature-scoped 采集**：`features` 配置列表声明各特性所需指标，`internal/metrics` 以 `SetFeatureScope` 建立白名单（各 feature `metrics.yaml` 的并集）；非空时只采白名单内且 `priority ≥ min_priority` 的指标，`AnyWanted` 跳过产出全 out-of-scope 的子方法，避免空跑硬件；空则用默认目录全集
- **采集粒度控制**：`collection.min_priority` 配置（low/medium/high）按优先级阈值预过滤采集，采集器经 `AnyWanted` DI 在执行前跳过无需采集的指标组，降低开销
- **故障订阅推送（faultsub）**：opt-in 特性，对采集到的 NPU 指标做故障判定（卡掉线/健康状态/错误码/HBM UCE/RoCE 链路等），经 **HTTP Webhook** 向已订阅的外部故障管理者推送 `FaultEvent`，并提供订阅注册/快照/事件回补 REST API（`:9101`）。零新依赖（`net/http`），默认关闭
- **来源层架构**：`internal/source/`（14 包）抽象数据获取与解析，采集器不直接读文件/执行命令，无硬件时优雅降级
- **跨平台**：Linux / Windows 双平台，构建标签隔离平台代码
- **易扩展**：新增部件采集器只需实现统一接口并注册，核心代码零修改

> 各特性功能规格见 [SPEC.md](SPEC.md)，各特性的设计与规格见对应 `features/<feature>/*_SPEC.md`。

## 技术栈

| 项目 | 选型 |
|------|------|
| 语言 | Go 1.21+ |
| 平台 | Linux / Windows |
| 输出 | 本地文件 (JSONL) + Prometheus 文本 (`/metrics`) |
| 配置 | YAML |
| 外部依赖 | 仅 `gopkg.in/yaml.v3`；GPU 经 `nvidia-smi`，NPU 经 `dcmi`(CGo, `-tags dcmi`)/`npu-smi`/`hccn_tool`，默认构建无 CGo |
| Web/导出 | Go 标准库 `net/http` + `//go:embed` 内嵌前端，无构建步骤 |

## 快速开始

```bash
# 编译（daemon + web + dfee 三个二进制）
make all               # 或分别 make build / make web / make dfee

# 配置（Linux）：开启 snapshot 生产以供 web/dfee 只读消费
cp configs/catmonitor.yaml /etc/catmonitor/catmonitor.yaml
#   在 /etc/catmonitor/catmonitor.yaml 中设：
#     snapshot.enabled: true
#     snapshot.dir: /var/lib/catmonitor/snapshot
#     features: [dfee]   # 按特性所需指标做 scope 白名单采集（可选）

# 启动守护进程（采集 + 健康度 + Prometheus :9100 + snapshot 生产）
catmonitor daemon

# 启动只读消费者（消费 daemon 产出的 snapshot，不自行采集）
catmonitor-web -addr :9527 -snapshot-dir /var/lib/catmonitor/snapshot
catmonitor-dfee -addr :9528 -snapshot-dir /var/lib/catmonitor/snapshot

# 单次采集 / 健康检查 / 采集器列表
catmonitor collect -o table
catmonitor health -o table
catmonitor list
```

> 完整安装、配置、命令、Web 仪表盘、dfee 能效监控、Prometheus 接入与示例见 [使用手册](docs/User_Manual.md)。

## 健康度评分

| 场景 | CPU | Memory | Disk | GPU/NPU | 合计 |
|------|-----|--------|------|---------|------|
| 无 GPU/NPU | 30 | 40 | 30 | — | 100 |
| 有 GPU/NPU | 10 | 20 | 10 | 60 | 100 |

| 得分 | 等级 |
|------|------|
| 90-100 | Excellent |
| 75-89 | Good |
| 60-74 | Warning |
| 0-59 | Critical |

> 扣分规则与阈值见 [features/health/HEALTH_SPEC.md](features/health/HEALTH_SPEC.md)。

## 文档

| 文档 | 说明 |
|------|------|
| [使用手册](docs/User_Manual.md) | 构建、安装、配置、命令、Web/dfee/exporter 用法与示例 |
| [SPEC.md](SPEC.md) | 功能规格说明书（不含技术细节） |
| [DESIGN.md](DESIGN.md) | 架构与模块设计 |
| [docs/CATMonitor_indi_list.md](docs/CATMonitor_indi_list.md) | 采集指标清单（204 项） |
| [docs/test_report.md](docs/test_report.md) | 测试报告（无 NPU/GPU 系统测试） |
| [features/health/HEALTH_SPEC.md](features/health/HEALTH_SPEC.md) | 健康度评估规格 |
| [features/web/Web_SPEC.md](features/web/Web_SPEC.md) | Web 仪表盘规格 |
| [features/dfee/dfee_SPEC.md](features/dfee/dfee_SPEC.md) | 能效监控模块规格 |
| [features/exporter/exporter_SPEC.md](features/exporter/exporter_SPEC.md) | Prometheus 导出模块规格 |
| [features/faultsub/faultsub_SPEC.md](features/faultsub/faultsub_SPEC.md) | 故障订阅推送模块规格 |

## 项目结构

```
CATMonitor/
├── cmd/catmonitor/          # 守护进程入口（daemon/collect/health/list/version）
├── internal/
│   ├── collector/           # 采集核心：Collector 接口 + Registry + Scheduler
│   ├── collectors/          # 7 个部件采集器（cpu/memory/disk/gpu/npu/network/chassis）
│   ├── source/              # 来源层（14 包：proc/sys/ipmi/lscpu/mce/dmesg/dmidecode/statfs/smartctl + dcmi/npu_smi/hccn_tool/nvidia_smi）
│   ├── metrics/             # 指标采集目录（MetricSpec/Catalog/Filter + SetFeatureScope 白名单）
│   ├── config/ platform/ storage/   # 配置 / 平台适配 / 数据存储(JSONL)
├── features/                # 特性层
│   ├── health/              #   健康度评估
│   ├── snapshot/            #   Snapshot 统一生产（PerCompWriter + GlobalWriter + 原子写/只读读）
│   ├── web/                 #   Web 仪表盘（catmonitor-web，只读消费 snapshot）
│   ├── dfee/                #   能效监控（catmonitor-dfee 独立二进制，package main，只读消费 snapshot）
│   ├── exporter/            #   Prometheus 导出（CachingStorage + /metrics）
│   └── faultsub/            #   故障订阅推送（FaultStorage + HTTP Webhook + REST）
├── configs/                 # 默认配置（catmonitor.yaml + metrics.yaml）
├── docs/                    # 文档（指标清单 / 使用手册 / 测试报告）
├── tests/ scripts/          # 测试框架与数据 / 安装脚本
└── Makefile                # make all/build/web/dfee + DCMI 头自动探测
```

> 完整目录与模块设计见 [DESIGN.md](DESIGN.md)。
