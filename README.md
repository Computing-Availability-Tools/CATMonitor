# CATMonitor

> **Computing Availability Tools Monitor** — 服务器运行指标采集与健康度评估守护进程

CATMonitor 是 CAT (Computing Availability Tools) 系列软件之一，用于采集服务器各部件（CPU、内存、硬盘、GPU、NPU、网卡等）的运行指标，并基于采集结果评估服务器整体健康度。

## 版本信息

| 项目 | 说明 |
|------|------|
| 版本号 | v0.3.0 |
| 发布时间 | 2026-07-17 |
| 发布人 | sunnytao |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 许可证 | 内部项目 |

## 功能特性

- **多部件采集**：支持 CPU、内存、硬盘、GPU、NPU、网卡等部件指标采集，共 152 个指标
- **来源层架构**：`internal/source/` 抽象数据获取与解析（14 个包：proc/sys/ipmi/lscpu/mce/dmesg/dmidecode/statfs/smartctl + dcmi/npu_smi/hccn_tool/nvidia_smi），全部 6 个采集器已接入来源层，采集器不再直接读文件或执行命令，来源返回 parsed struct + 单例 + 可注入 fetcher + 缓存
- **指标采集目录**（v0.3.0 新增）：`internal/metrics` 提供指标目录（MetricSpec/Catalog/Filter），`configs/metrics.yaml` 为默认目录，模块可用自有 `metrics.yaml` 覆盖（按 name 合并）；High/Medium + 静态身份默认采、Low 诊断默认不采，scheduler 经 Filter 决定是否采集
- **特性层架构**（v0.3.0 新增）：`features/` 承载基于采集基础能力构建的上层模块——`features/health`（健康度评估，消费 `collector.Metric`，按部件评估器 + 局部 scheme）、`features/web`（Web 仪表盘）
- **跨平台**：Linux 和 Windows 双平台支持，通过构建标签隔离平台代码
- **易扩展架构**：新增部件采集器只需实现统一接口并注册，核心代码零修改
- **健康度评估**：基于采集指标自动计算服务器健康度评分（0-100 分），自动检测 GPU/NPU 切换权重方案；规则对齐指标清单 High/Medium，按部件扣分
- **Web 仪表盘**（`features/web`）：独立 `catmonitor-web` 二进制，可视化单机健康度与各部件指标，SPA 概览页 + 部件详情页 + 趋势 sparkline + 静态设备规格面板，默认端口 9527（被占用时自动递增），零新依赖（Go 标准库 + `//go:embed` 内嵌前端）
- **可配置**：每个指标的采集周期、是否启用均可通过配置文件调整
- **守护进程**：Linux 下以 systemd 服务常驻运行，持续采集和评估

## 技术栈

| 项目 | 选型 |
|------|------|
| 语言 | Go 1.21+ |
| 平台 | Linux / Windows |
| 输出 | 本地文件 (JSONL) |
| 配置 | YAML |
| 外部依赖 | 仅 `gopkg.in/yaml.v3`，GPU 通过 `nvidia_smi` 来源包采集，NPU 通过 `dcmi`(CGo，`-tags dcmi`)/`npu_smi`/`hccn_tool` 来源包采集（默认构建无 CGo） |
| Windows API | kernel32.dll / iphlpapi.dll 通过 Go syscall 调用，零第三方依赖 |
| Web 仪表盘 | Go 标准库 `net/http` + `//go:embed` 内嵌原生 HTML/CSS/JS 前端，无构建步骤，零新依赖 |

## 快速开始

### 编译

```bash
make build
# Windows: go build -o bin/catmonitor.exe ./cmd/catmonitor
```

### 配置

**Linux:**

```bash
# 复制默认配置
cp configs/catmonitor.yaml /etc/catmonitor/catmonitor.yaml
# 按需修改配置
vim /etc/catmonitor/catmonitor.yaml
```

**Windows:**

```powershell
# 创建配置目录
New-Item -ItemType Directory -Path "C:\ProgramData\catmonitor" -Force
Copy-Item configs\catmonitor.yaml C:\ProgramData\catmonitor\catmonitor.yaml
```

### 启动守护进程

```bash
# 前台运行（Linux / Windows 通用）
catmonitor daemon

# Linux: 安装为 systemd 服务
sudo scripts/install.sh
sudo systemctl start catmonitor
```

### 单次采集

```bash
# 采集所有指标
catmonitor collect

# 只采集 CPU 和内存
catmonitor collect --component cpu,memory

# 表格输出
catmonitor collect -o table
```

### 健康检查

```bash
# 执行一次健康检查
catmonitor health

# 表格输出
catmonitor health -o table
```

### 查看状态

```bash
# 查看采集器列表
catmonitor list

# 查看守护进程状态
catmonitor status
```

## Web 仪表盘（v0.2.1 新增）

CATMonitor 提供独立的 Web 仪表盘二进制 `catmonitor-web`，可视化单台服务器的健康度与各部件采集指标。Web 服务与采集守护进程/CLI 完全解耦，通过 `features/web/data/snapshot.json` 文件作为读写解耦边界，不修改主项目任何文件。

### 构建

```bash
# 构建 Web 仪表盘二进制
go build -o features/web/bin/catmonitor-web ./features/web
# Windows: GOOS=windows go build -o features/web/bin/catmonitor-web.exe ./features/web
```

### 运行

```bash
# 启动 Web 仪表盘（默认监听 :9527，被占用时自动 +1 递增）
./features/web/bin/catmonitor-web -config features/web/config.yaml
# 浏览器打开 http://localhost:9527（实际端口见启动日志 "web server starting" addr=...）
```

> 工作目录需为仓库根（`config.yaml` 中 `snapshot_path`/`runtime_path` 为相对路径 `features/web/data/...`）。

### 功能页面

- **概览页**：整体健康度（总分 + 进度条 + 等级）+ 设备规格面板（设备/CPU/内存/硬盘/网卡/GPU/NPU 身份，点击展开完整规格）+ 各部件状态芯片 + 部件概览卡片网格（含头条趋势 sparkline + 关键指标）
- **部件详情页**（`#/<component>`，如 `#/cpu`）：部件得分/扣分项 + 趋势面板（自动渲染所有该部件历史序列的 sparkline）+ 全部指标表（覆盖每核心/每挂载点/每卡）
- **可配置刷新间隔**：界面调整 → `POST /api/config` 热生效 + 持久化到 `runtime.json`（重启保留）

### REST API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/snapshot` | 读取最新快照（健康度 + 指标 + 历史 + 静态规格） |
| GET | `/api/collectors` | 采集器注册表元数据（驱动导航） |
| GET / POST | `/api/config` | 读取 / 更新刷新间隔 |
| POST | `/api/refresh` | 请求立即采集 |

### 扩展性

新增部件采集器只需在 `features/web/main.go` 加一行 blank import `_ ".../internal/collectors/xxx"`，导航/概览卡/详情页自动出现；新增趋势 sparkline 在 `features/web/collector.go` 的 `trackedSeries` 加一行 spec。详见 [Web_SPEC.md](features/web/Web_SPEC.md)。

## 命令一览

```
catmonitor [command] [flags]

Commands:
  daemon       启动守护进程（持续采集）
  collect      单次采集所有指标快照
  health       执行一次健康检查
  status       查看守护进程状态
  list         列出所有已注册采集器
  version      显示版本信息

Flags:
  -c, --config      配置文件路径 (Linux: /etc/catmonitor/catmonitor.yaml, Windows: C:\ProgramData\catmonitor\catmonitor.yaml)
  -d, --data-dir    数据输出目录 (Linux: /var/lib/catmonitor/data, Windows: C:\ProgramData\catmonitor\data)
      --component   只采集指定部件 (如: cpu,memory)
  -o, --output      输出格式: json|table|yaml (默认: json)
  -i, --interval    覆盖采集周期 (如: 5s)
  -v, --verbose     详细日志输出
  -h, --help        帮助信息
```

## 健康度评分

| 场景 | CPU | Memory | Disk | GPU/NPU | 合计 |
|------|-----|--------|------|---------|------|
| 无 GPU/NPU | 30 | 40 | 30 | — | 100 |
| 有 GPU/NPU | 10 | 20 | 10 | 60 | 100 |

> 自动检测：`health` 命令根据是否存在 GPU/NPU 指标自动选择权重方案。

| 得分 | 等级 |
|------|------|
| 90-100 | Excellent |
| 75-89 | Good |
| 60-74 | Warning |
| 0-59 | Critical |

## 支持的采集指标

共 152 个指标，覆盖 6 个部件。详见 [指标清单](docs/CATMonitor_indi_list.md)。

| 部件 | 指标数 | High | Medium | Low | Linux | Windows |
|------|--------|------|--------|-----|:-----:|:-------:|
| CPU | 40 | 4 | 12 | 24 | ✅ | ✅ (基础指标，扩展指标 Linux 专有) |
| Memory | 19 | 4 | 7 | 8 | ✅ | ✅ (基础指标，扩展指标 Linux 专有) |
| Disk | 7 | 1 | 3 | 3 | ✅ | ✅ (2/7) |
| GPU | 7 | 3 | 3 | 1 | ✅ | ✅ (7/7) |
| NPU | 74 | 9 | 43 | 22 | ✅ | ✗ (Linux 专有；DCMI 走 CGo `-tags dcmi`，Windows no-op 降级) |
| Network | 5 | 1 | 3 | 1 | ✅ | ✅ (5/5) |

> v0.2.0 CPU 扩展至 40、Memory 扩展至 19，新增拓扑/频率/缓存/MCE/PSI 饱和度/碎片化等指标，通过来源层采集。Windows 来源层迁移延后，扩展指标当前为 Linux 专有，Windows 保留基础实现（优雅降级）。
> v0.2.2 NPU 指标 5→74（device 并行采集），新增 dcmi(CGo)/npu_smi/hccn_tool 来源包；GPU 迁移至 nvidia_smi 来源包，全部 6 个采集器接入来源层。NPU 全部指标为 Linux 专有。
> v0.3.0 引入指标采集目录（`configs/metrics.yaml` + 模块覆盖）与 `features/` 特性层（health/web），健康度抽取为 `features/health` 按部件评估器，Web 迁至 `features/web`。指标总数不变。

## 跨平台架构

```
internal/collectors/{component}/
├── {component}.go           # 共享：结构体、接口、指标定义、delta 逻辑
├── {component}_linux.go     # Linux: 调用来源层(proc/sys/...)获取数据
├── {component}_metrics.go    # 跨平台：新增指标采集方法(来源层报错→空)
├── {component}_windows.go   # Windows: kernel32.dll、iphlpapi.dll、PowerShell
└── {component}_test.go      # 测试（Linux 测试使用 //go:build linux）

internal/source/{source}/     # 来源层（14 包，v0.2.0 引入，v0.2.2 扩展至全 6 采集器）
├── source.go                # 通用接口：Source{Name(); Available()}
└── {source}.go              # 数据获取与解析，返回 parsed struct
```

| 采集器 | Linux 数据源 | Windows 数据源 |
|--------|-------------|---------------|
| CPU | `proc.Stat()`、`lscpu`、`sys`(freq/cache/corestate)、`mce`、`ipmi.SDR` | `GetSystemTimes` (kernel32.dll) + WMI |
| Memory | `proc.Meminfo/Vmstat/Pressure/Buddyinfo`、`dmidecode`、`ipmi.SDR`、`dmesg` | `GlobalMemoryStatusEx` (kernel32.dll) |
| Disk | `proc.Diskstats/Stat`、`statfs`、`smartctl`、`dmesg` | `GetDiskFreeSpaceExW` + `GetLogicalDrives` (kernel32.dll) |
| GPU | `nvidia_smi` 来源包(`nvidia-smi`) | `nvidia_smi` 来源包(Windows 原生支持) |
| NPU | `dcmi`(CGo `libdcmi.so`)/`npu_smi`/`hccn_tool` 来源包 | —（Linux 专有，Windows no-op 降级） |
| Network | `proc.NetDev/NetTCPStates`、`sys`(NetInterfaces/Operstate) | `Get-NetAdapterStatistics` / `Get-NetTCPConnection` (PowerShell) |

> 来源层包（14 个）：`proc`、`sys`、`ipmi`(30s缓存+失败缓存)、`lscpu`(常驻)、`mce`、`dmesg`(30s缓存)、`dmidecode`(常驻)、`statfs`(linux专有)、`smartctl`(per-dev 60s缓存)、`dcmi`(CGo，`-tags dcmi`，无缓存)、`npu_smi`(Topo 常驻缓存)、`hccn_tool`(per-dev:opt 30s缓存+失败缓存)、`nvidia_smi`(无缓存)。采集器通过单例 + `SetRoot`/可注入 fetcher 访问来源，便于测试注入 mock 数据。

## 文档

| 文档 | 说明 |
|------|------|
| [SPEC.md](SPEC.md) | 技术规格与需求 |
| [DESIGN.md](DESIGN.md) | 架构与模块设计 |
| [features/web/Web_SPEC.md](features/web/Web_SPEC.md) | Web 仪表盘设计与规格 |
| [features/health/HEALTH_SPEC.md](features/health/HEALTH_SPEC.md) | 健康度评估规格 |
| [docs/CATMonitor_indi_list.md](docs/CATMonitor_indi_list.md) | 采集指标清单 |
| [test_report.md](test_report.md) | 测试报告 |

## 项目结构

```
CATMonitor/
├── cmd/catmonitor/          # 守护进程入口
├── internal/
│   ├── collector/           # 采集核心（接口、注册表、调度引擎）
│   ├── collectors/          # 各部件采集器实现
│   │   ├── cpu/             #   cpu.go + cpu_linux.go + cpu_metrics.go + cpu_windows.go
│   │   ├── memory/          #   memory.go + memory_linux.go + memory_metrics.go + memory_windows.go
│   │   ├── disk/            #   disk.go + disk_linux.go + disk_windows.go
│   │   ├── gpu/             #   gpu.go (经 nvidia_smi 来源包采集, 双平台通用)
│   │   ├── npu/             #   npu.go + npu_linux.go(74 指标 device 并行) + npu_other.go(!linux no-op)
│   │   └── network/         #   network.go + network_linux.go + network_windows.go
│   ├── metrics/             # 指标采集目录：默认目录 + 模块覆盖(merge) + Filter（默认目录见 configs/metrics.yaml）
│   ├── source/              # 来源层：数据获取与解析抽象（14 包，v0.2.0 引入，v0.2.2 扩展至全 6 采集器）
│   │   ├── source.go        #   通用 Source 接口
│   │   ├── proc/            #   /proc 全量解析
│   │   ├── sys/             #   /sys 解析（freq/cache/thermal/net）
│   │   ├── ipmi/            #   ipmitool SDR/DCMI（带缓存）
│   │   ├── lscpu/           #   lscpu 拓扑（常驻缓存）
│   │   ├── mce/             #   mcelog/dmesg MCE 事件
│   │   ├── dmesg/           #   dmesg（带缓存）
│   │   ├── dmidecode/       #   dmidecode DIMM（常驻缓存）
│   │   ├── statfs/          #   statfs(2)（Linux 专有）
│   │   ├── smartctl/        #   smartctl -H（per-dev 缓存）
│   │   ├── dcmi/            #   libdcmi.so CGo 绑定（v0.2.2 新增，-tags dcmi，服务 npu）
│   │   ├── npu_smi/         #   npu-smi -t 拓扑/带宽（v0.2.2 新增，服务 npu）
│   │   ├── hccn_tool/       #   hccn_tool 带宽/速度/链路（v0.2.2 新增，服务 npu）
│   │   └── nvidia_smi/      #   nvidia-smi 解析（v0.2.2 新增，服务 gpu）
│   ├── platform/            # 平台抽象层（路径默认值）
│   ├── config/              # 配置管理
│   └── storage/             # 数据存储（JSONL）
├── features/                # 特性层：基于采集基础能力构建的上层模块（health/web 等）
│   ├── health/              #   健康度评估模块（消费 collector.Metric，不做底层采集）
│   │   └── metrics.yaml     #     health 自己的指标目录（启动 health 时优先读取）
│   └── web/                 #   Web 仪表盘（v0.2.1 新增，独立二进制）
│       ├── main.go          #     入口：blank-import 采集器 + 采集 goroutine + HTTP server + 端口回退 + 信号处理
│       ├── collector.go     #     DataCollector：定时采集 → 健康度 → 原子写 snapshot + 环形历史 + 静态规格 stash
│       ├── snapshot.go      #     Snapshot 结构 + 原子读写
│       ├── hwinfo.go        #     一次性硬件身份采集（device_model/gpu_info/npu_info/disk_info/net_info）
│       ├── server.go        #     HTTP 路由与处理函数
│       ├── config.go        #     配置结构 + YAML 加载 + runtime.json 运行时覆盖
│       ├── config.yaml      #     默认配置
│       ├── metrics.yaml     #     web 自己的指标目录（启动 web 时优先读取）
│       ├── static/          #     前端资源（//go:embed 内嵌）
│       └── data/            #     运行时数据（snapshot.json / runtime.json，git 忽略）
├── configs/                 # 默认配置（catmonitor.yaml + metrics.yaml 合并指标目录）
├── docs/                    # 文档
├── tests/                   # 测试框架与数据
└── scripts/                 # 安装脚本
```
