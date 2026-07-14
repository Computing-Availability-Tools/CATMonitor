# CATMonitor

> **Computing Availability Tools Monitor** — 服务器运行指标采集与健康度评估守护进程

CATMonitor 是 CAT (Computing Availability Tools) 系列软件之一，用于采集服务器各部件（CPU、内存、硬盘、GPU、NPU、网卡等）的运行指标，并基于采集结果评估服务器整体健康度。

## 版本信息

| 项目 | 说明 |
|------|------|
| 版本号 | v0.1.1 |
| 发布时间 | 2026-07-12 |
| 发布人 | sunnytao |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 许可证 | 内部项目 |

## 功能特性

- **多部件采集**：支持 CPU、内存、硬盘、GPU、NPU、网卡等部件指标采集
- **跨平台**：Linux 和 Windows 双平台支持，通过构建标签隔离平台代码
- **易扩展架构**：新增部件采集器只需实现统一接口并注册，核心代码零修改
- **健康度评估**：基于采集指标自动计算服务器健康度评分（0-100 分），自动识别 GPU 切换权重方案
- **可配置**：每个指标的采集周期、是否启用均可通过配置文件调整
- **守护进程**：Linux 下以 systemd 服务常驻运行，持续采集和评估

## 技术栈

| 项目 | 选型 |
|------|------|
| 语言 | Go 1.21+ |
| 平台 | Linux / Windows |
| 输出 | 本地文件 (JSONL) |
| 配置 | YAML |
| 外部依赖 | 仅 `gopkg.in/yaml.v3`，GPU/NPU 通过命令行工具采集（无 CGo） |
| Windows API | kernel32.dll / iphlpapi.dll 通过 Go syscall 调用，零第三方依赖 |

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

共 37 个指标，覆盖 6 个部件。详见 [指标清单](docs/CATMonitor_indi_list.md)。

| 部件 | 指标数 | High | Medium | Low | Linux | Windows |
|------|--------|------|--------|-----|:-----:|:-------:|
| CPU | 7 | 2 | 2 | 3 | ✅ | ✅ (5/7) |
| Memory | 6 | 4 | 1 | 1 | ✅ | ✅ (3/6) |
| Disk | 7 | 1 | 3 | 3 | ✅ | ✅ (2/7) |
| GPU | 7 | 3 | 3 | 1 | ✅ | ✅ (7/7) |
| NPU | 5 | 3 | 2 | 0 | ✅ | ✅ (5/5) |
| Network | 5 | 1 | 3 | 1 | ✅ | ✅ (5/5) |

> Windows 下部分指标（ECC 错误、CPU 温度、SMART 状态等）无可靠系统数据源，返回空值（优雅降级）。

## 跨平台架构

```
internal/collectors/{component}/
├── {component}.go           # 共享：结构体、接口、指标定义、delta 逻辑
├── {component}_linux.go     # Linux: /proc、/sys、syscall.Statfs、dmesg
├── {component}_windows.go   # Windows: kernel32.dll、iphlpapi.dll、PowerShell
└── {component}_test.go      # 测试（Linux 测试使用 //go:build linux）
```

| 采集器 | Linux 数据源 | Windows 数据源 |
|--------|-------------|---------------|
| CPU | `/proc/stat`, `/sys/class/thermal`, `/proc/cpuinfo` | `GetSystemTimes` (kernel32.dll) + WMI |
| Memory | `/proc/meminfo`, `/sys/edac`, `/proc/vmstat`, `dmesg` | `GlobalMemoryStatusEx` (kernel32.dll) |
| Disk | `/proc/mounts`, `/proc/diskstats`, `syscall.Statfs`, `smartctl` | `GetDiskFreeSpaceExW` + `GetLogicalDrives` (kernel32.dll) |
| GPU | `nvidia-smi` | `nvidia-smi` (Windows 原生支持) |
| NPU | `npu-smi` | `npu-smi` (有驱动时可用) |
| Network | `/proc/net/dev`, `/sys/class/net`, `/proc/net/tcp` | `Get-NetAdapterStatistics` / `Get-NetTCPConnection` (PowerShell) |

## 文档

| 文档 | 说明 |
|------|------|
| [SPEC.md](SPEC.md) | 技术规格与需求 |
| [DESIGN.md](DESIGN.md) | 架构与模块设计 |
| [docs/CATMonitor_indi_list.md](docs/CATMonitor_indi_list.md) | 采集指标清单 |
| [docs/test_report.md](docs/test_report.md) | 测试报告 |

## 项目结构

```
CATMonitor/
├── cmd/catmonitor/          # 守护进程入口
├── internal/
│   ├── collector/           # 采集核心（接口、注册表、调度引擎）
│   ├── collectors/          # 各部件采集器实现
│   │   ├── cpu/             #   cpu.go + cpu_linux.go + cpu_windows.go
│   │   ├── memory/          #   memory.go + memory_linux.go + memory_windows.go
│   │   ├── disk/            #   disk.go + disk_linux.go + disk_windows.go
│   │   ├── gpu/             #   gpu.go (nvidia-smi, 双平台通用)
│   │   ├── npu/             #   npu.go (npu-smi, 双平台通用)
│   │   └── network/         #   network.go + network_linux.go + network_windows.go
│   ├── health/              # 健康度评估模块（独立）
│   ├── platform/            # 平台抽象层（路径默认值）
│   ├── config/              # 配置管理
│   └── storage/             # 数据存储（JSONL）
├── configs/                 # 默认配置
├── docs/                    # 文档
├── tests/                   # 测试框架与数据
└── scripts/                 # 安装脚本
```
