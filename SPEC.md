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
6. **来源层抽象**（v0.2.0）：数据获取与解析集中在 `internal/source/`，采集器不再直接 `os.ReadFile`/`exec`；来源返回 parsed struct，单例 + 可注入 fetcher，部分来源（ipmi/dmesg/smartctl）带缓存 + 失败缓存，避免无硬件时反复 exec
7. **Web 可视化**（v0.2.1）：独立 `web/` 模块提供 Web 仪表盘，与采集守护进程/CLI 解耦，零新依赖；新增部件/指标时前端尽量自动出现，最多在一处加一行

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

> v0.2.0 通过来源层（`internal/source/`）扩展，CPU 7→40、Memory 6→19，disk/network 迁移到来源层（指标集不变）。完整清单见 [docs/CATMonitor_indi_list.md](docs/CATMonitor_indi_list.md)。

| 部件 | 指标数 | High | Medium | Low |
|------|--------|------|--------|-----|
| CPU | 40 | 4 | 12 | 24 |
| Memory | 19 | 4 | 7 | 8 |
| Disk | 7 | 1 | 3 | 3 |
| GPU | 7 | 3 | 3 | 1 |
| NPU | 5 | 3 | 2 | 0 |
| Network | 5 | 1 | 3 | 1 |
| **合计** | **83** | **16** | **30** | **37** |

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

### 7.1 来源层外部命令（v0.2.0，Linux）

来源层 `internal/source/` 通过 `os/exec` 调用以下系统命令（无硬件/未安装时返回空，优雅降级，失败结果同样缓存以避免反复 exec）：

| 来源包 | 外部命令 | 用途 | 缓存策略 |
|--------|---------|------|---------|
| ipmi | `ipmitool` (sdr/dcmi) | 温度/功率 | 30s + 失败缓存 + 5s 超时 |
| lscpu | `lscpu` | CPU 拓扑 | 常驻 (sync.Once) |
| mce | `mcelog` / `dmesg` | MCE 错误 | 无 |
| dmesg | `dmesg` | OOM / I/O 错误 | 30s + 失败缓存 |
| dmidecode | `dmidecode --type 17` | DIMM 模块信息 | 常驻 (sync.Once) |
| smartctl | `smartctl -H` | SMART 健康 | per-dev 60s + 失败缓存 |
| proc / sys / statfs | （纯文件读取/系统调用，无外部命令） | /proc、/sys、statfs(2) | 无缓存 |

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

## 9. Web 仪表盘规格（v0.2.1 新增）

> 详细设计与规格见 [Web_SPEC.md](Web_SPEC.md)。本节列出关键需求与约束。

### 9.1 概述

提供独立 Web 仪表盘二进制 `catmonitor-web`，可视化单台服务器的健康度与各部件采集指标。设计原则：

1. **解耦**：Web 服务与现有采集守护进程/CLI 完全解耦，不修改主项目任何文件；仅通过只读复用（blank import + 调用注册表/健康度接口）获取数据。
2. **多页面**：概览页（整体健康度 + 各部件关键指标）+ 各部件详情页（详细指标 + 趋势）。
3. **可扩展**：新增部件类型/采集指标时，尽可能自动出现，零代码或仅需一处一行的新增。
4. **极简依赖**：Go 标准库 + 已有 `gopkg.in/yaml.v3`，前端原生 HTML/CSS/JS，无构建步骤，零新依赖。

### 9.2 技术栈

| 项目 | 选型 |
|------|------|
| 后端语言 | Go（沿用主项目 go.mod，不新增 go.mod） |
| HTTP | Go 标准库 `net/http` |
| 配置 | `gopkg.in/yaml.v3`（已有依赖，无新增） |
| 前端 | 原生 HTML5 + CSS + 原生 JS（ES2015+），无框架、无构建步骤 |
| 前端打包 | `//go:embed static` 内嵌进二进制，单文件部署 |
| 图表 | 手写内联 SVG sparkline，无图表库 |
| 进程托管 | systemd 临时 unit（可选），支持信号优雅退出 |

### 9.3 解耦边界

单一二进制内含两个角色，以 `web/data/snapshot.json` 为解耦边界：

- **采集 goroutine**（唯一写者）：定时遍历注册表 → `Collect()` → `health.Evaluate()` → 原子写 `snapshot.json`（写临时文件 + `os.Rename`，读者永不会读到半截文件）。
- **HTTP server**（只读）：静态页 + REST API，读取 `snapshot.json` 返回，**绝不直接调用采集器**。

### 9.4 配置

`web/config.yaml`：

```yaml
server:
  addr: ":9527"                # 监听地址（端口被占用时自动 +1 递增直到空闲）
collector:
  refresh_interval: 5s         # 采集周期（也作为前端默认轮询间隔）
  history_points: 60           # 环形历史保留的采样点数
  # enabled_components: []     # 空 = 采集全部已注册部件
storage:
  snapshot_path: web/data/snapshot.json
  runtime_path:  web/data/runtime.json
```

配置加载优先级：默认值 → YAML 覆盖（文件不存在静默用默认）→ `runtime.json` 运行时覆盖（界面调整持久化，重启保留）→ `-config` flag。

### 9.5 端口占用自动回退

启动时以 `net.Listen` 探测 `server.addr`，若返回 `EADDRINUSE` 则端口 +1 重试（`:9527`→`:9528`→`:9529`…）直至可用，实际绑定地址回写配置并打印日志。非 `EADDRINUSE` 错误（如权限不足）直接失败退出。跨平台有效（`syscall.EADDRINUSE` 在 Linux/Windows 均定义）。

### 9.6 静态设备规格

静态规格是设备的**身份信息**（型号/拓扑/序列号/容量），非时序数据，采集一次即可。Web 侧用两条互补路径收集，合并写入每个快照的 `specs` 字段：

1. **启动期一次性硬件身份**（`hwinfo.go`，非注册采集器）：`device_model`（dmidecode SMBIOS type 1）、`gpu_info`（nvidia-smi）、`npu_info`（npu-smi）、`disk_info`（/sys/block + smartctl 富化）、`net_info`（/sys/class/net）。外部命令缺失则降级（不报错）。
2. **CPU/内存静态指标 stash**（`collector.go` `filterStatic`）：CPU/内存采集器首周期产出一次静态指标（型号/拓扑/频率/缓存/DIMM），Web 侧首次出现即缓存到 `staticStash`，之后每周期重新注入快照，避免首周期后规格消失。

### 9.7 REST API 规范

| 方法 | 路径 | 说明 | 成功码 | 失败码 |
|------|------|------|:------:|:------:|
| GET | `/` | 返回 `index.html`（SPA 外壳） | 200 | 500 |
| GET | `/static/{file}` | 静态资源（css/js） | 200 | 404 |
| GET | `/api/snapshot` | 读取 `snapshot.json` 返回 | 200 | 503 |
| GET | `/api/collectors` | 注册表元数据列表（驱动导航） | 200 | — |
| GET | `/api/config` | 当前配置 | 200 | — |
| POST | `/api/config` | 更新刷新间隔（热生效 + 持久化） | 200 | 400 / 405 |
| POST | `/api/refresh` | 请求立即采集 | 200 | 405 |

- `POST /api/config` 请求体 `{"refresh_interval_ms": 8000}`，校验 `< 1000` → 400；JSON 非法 → 400；非 GET/POST → 405。成功后热生效 + 原子写 `runtime.json`。
- 快照未就绪（首次采集前）返回 503。

### 9.8 扩展性需求

| 扩展需求 | 改动位置 | 自动部分 |
|----------|----------|----------|
| 新部件采集器 | `web/main.go`（blank import） | 导航/概览卡/详情页 |
| 部件显示名/关键指标 | `web/static/app.js` MANIFEST | — |
| 新趋势 sparkline | `web/collector.go` trackedSeries | 详情页趋势面板 |
| 新静态身份指标（采集器侧） | 加入 `staticMetricNames` 即被 stash 进 `specs` | specs modal 自动渲染 |

> 兼容性保证：`health` 与 `metrics` 字段直接复用主项目结构体，采集器新增任何字段/标签都原样透传到前端；未知部件/指标/序列均有通用回退，不会因未登记而崩溃或消失。

### 9.9 Web 模块约束

- **不改动主项目任何现有文件**：与 `cmd/catmonitor`、`internal/collectors`、`internal/health`、`internal/storage`、`internal/config`、`internal/platform` 解耦。
- **不应提交**：`web/data/*`（运行时生成：`snapshot.json`、`runtime.json`），已加入根 `.gitignore`。
- **构建产物**：`web/bin/` 已被根 `.gitignore` 的 `bin/` 覆盖，自动忽略。
- **测试**：`go test ./web/` 覆盖快照原子读写、历史环形缓冲、静态规格 stash、硬件身份采集、HTTP API 路由与端口回退。
