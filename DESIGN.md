# CATMonitor 设计文档 (DESIGN)

> 本文档描述 CATMonitor 的架构设计、模块设计、采集器设计和命令行设计。
> 规格与需求见 [SPEC.md](SPEC.md)，指标清单见 [CATMonitor_indi_list.md](docs/CATMonitor_indi_list.md)。

---

## 1. 架构设计

### 1.1 分层架构

```
┌─────────────────────────────────────────────────────┐
│              cmd/catmonitor        web/ (v0.2.1)       │
│              (守护进程入口)      (catmonitor-web 仪表盘)│
├─────────────────────────────────────────────────────┤
│  internal/config        internal/storage              │
│  internal/platform       (JSON文件写入)               │
│  (配置管理 + 平台适配)                                 │
├─────────────────────────────────────────────────────┤
│            internal/collector (采集核心)               │
│     ┌──────────┐  ┌──────────┐  ┌──────────────┐    │
│     │ Collector │  │ Registry │  │  Scheduler   │    │
│     │ Interface │  │ (注册表)  │  │  (调度引擎)  │    │
│     └──────────┘  └──────────┘  └──────────────┘    │
├─────────────────────────────────────────────────────┤
│              internal/health (健康度模块)              │
│     ┌──────────┐  ┌──────────┐  ┌──────────────┐    │
│     │ Evaluator │  │  Rules   │  │   Scorer    │    │
│     │ (评估器)   │  │ (规则表)  │  │  (计分器)    │    │
│     └──────────┘  └──────────┘  └──────────────┘    │
├─────────────────────────────────────────────────────┤
│            internal/collectors (采集器实现)           │
│   ┌─────┬──────────┬──────┬─────┬─────┬──────────┐  │
│   │ CPU │  Memory  │ Disk │ GPU │ NPU │  Network │  │
│   │ ┌───┴───┐ ┌───┴──┐ ┌┴───┐ │     │     │ ┌───┬──┐│  │
│   │ │Linux │ │Linux │ │Linux│ │     │     │ │Linux│Win││  │
│   │ │/proc │ │/proc │ │Statfs│ │     │     │ │/proc│PS ││  │
│   │ ├──────┤ ├──────┤ ├─────┤ │     │     │ │/net │API││  │
│   │ │ Win  │ │ Win  │ │ Win │ │     │     │ └───┴──┘│  │
│   │ │k32.dll│ │k32.dll│ │k32.dll│    │     │         │  │
│   │ └──────┘ └──────┘ └─────┘ │     │     │         │  │
│   └───────────────────────────┴─────┴─────┴─────────┘  │
├─────────────────────────────────────────────────────┤
│         internal/source (来源层 v0.2.0 新增)           │
│  ┌─────┬──────┬──────┬──────┬──────┬──────┬──────┐   │
│  │proc │ sys  │ ipmi │lscpu │ mce  │dmesg │...   │   │
│  │     │      │(缓存)│(常驻)│      │(缓存)│      │   │
│  └──┬──┴──┬───┴──┬───┴──┬───┴──┬───┴──┬───┴──────┘   │
│     │     │      │      │      │      │              │
│  来源层：parsed struct + 单例 + SetRoot/可注入 fetcher │
│  collector 调用来源拿数据，不再直接 os.ReadFile/exec     │
├─────────────────────────────────────────────────────┤
│         Linux 系统接口 (procfs/sysfs/syscall/exec)    │
│         Windows 系统 API (kernel32/iphlpapi/PS)        │
└─────────────────────────────────────────────────────┘
```

> v0.2.0 引入来源层（`internal/source/`）后，Linux 采集器通过来源包间接访问 `/proc`、`/sys`、`statfs`、`ipmitool` 等系统接口；Windows 保留直接 syscall 实现（来源层迁移延后）。来源返回 parsed struct，带缓存（ipmi/dmesg/smartctl）与可注入 fetcher，便于单元测试 mock。
>
> v0.2.1 新增 `web/` 模块（独立二进制 `catmonitor-web`），与主项目同一 Go module，不新增 go.mod、不改主项目任何文件。Web 复用采集器注册表与健康度模块（blank import），以 `web/data/snapshot.json` 为读写解耦边界：采集 goroutine 是唯一写者，HTTP 层只读快照文件。

### 1.2 跨平台架构设计

核心策略：**共享逻辑 + 平台数据源分离**，通过 Go 构建标签在编译时选择。

```
collectors/{component}/
  ├── {component}.go         ← 共享：struct, Collect(), 指标定义, delta 逻辑
  ├── {component}_linux.go   ← Linux: 调用来源层(proc/sys/ipmi/...)采集
  ├── {component}_metrics.go ← 跨平台(无build tag)：新增指标采集(来源报错→空)
  ├── {component}_windows.go ← Windows: kernel32.dll, PowerShell
  └── {component}_test.go    ← 测试 (//go:build linux)
```

**关键原则**：
- `Collector` 接口、`Metric` 结构体、健康度模块不感知平台差异
- 每个采集器的 `Collect()` 方法调用平台特定的数据采集函数
- Linux 代码通过 `internal/source/` 来源层访问 `/proc`、`/sys`、`statfs`、`ipmitool` 等（v0.2.0）
- Windows 代码使用 Go `syscall` 包直接调用 kernel32.dll / iphlpapi.dll，零第三方依赖
- GPU/NPU 采集器无需分离（nvidia-smi 和 npu-smi 在双平台均可通过 os/exec 调用）
- `*_metrics.go` 为跨平台文件（无 build tag），新增指标方法定义于此；Windows 上来源层不可用时返回空值

### 1.3 扩展机制：Collector 接口 + Registry 注册表

核心设计原则：**新增部件只需实现 `Collector` 接口并在 `init()` 中注册**，调度引擎自动发现并调度。

```go
// Metric — 单条采集指标数据
type Metric struct {
    Component  string            // 部件类型: "cpu", "memory", "disk"...
    Name       string            // 指标名称: "usage", "temperature"...
    Value      float64           // 指标值
    Unit       string            // 单位: "%", "MB", "rpm", "count"
    Labels     map[string]string // 附加标签: 设备号、核心号等
    Timestamp  time.Time         // 采集时间
}

// Collector — 所有采集器必须实现的接口
type Collector interface {
    // 基本信息
    Name() string                    // 采集器名称
    Component() string               // 部件类型
    // 采集行为
    Collect() ([]Metric, error)      // 执行一次采集，返回指标列表
    // 默认配置
    Priority() Priority             // 优先级: High / Medium / Low
    DefaultInterval() time.Duration  // 默认采集周期
    DefaultEnabled() bool            // 默认是否启用
}

// Registry — 采集器注册表（全局单例）
type Registry struct { ... }
func (r *Registry) Register(c Collector)     // 注册采集器
func (r *Registry) All() []Collector         // 获取所有已注册采集器
```

**扩展方式示例**：新增一个 FPGA 采集器

```go
// internal/collectors/fpga/fpga.go
package fpga

func init() {
    collector.DefaultRegistry.Register(&FPGACollector{})
}

type FPGACollector struct{}

func (c *FPGACollector) Name() string           { return "fpga" }
func (c *FPGACollector) Component() string      { return "fpga" }
func (c *FPGACollector) Collect() ([]collector.Metric, error) {
    // ... 采集逻辑
}
func (c *FPGACollector) Priority() collector.Priority        { return collector.PriorityHigh }
func (c *FPGACollector) DefaultInterval() time.Duration      { return 3 * time.Second }
func (c *FPGACollector) DefaultEnabled() bool                 { return true }
```

在 `main.go` 中通过 `import _ "catmonitor/internal/collectors/fpga"` 即可激活，核心代码无需任何修改。

### 1.4 目录结构

```
CATMonitor/
├── cmd/
│   └── catmonitor/
│       └── main.go                  # 守护进程入口
├── internal/
│   ├── collector/                   # 采集核心
│   │   ├── collector.go             # Collector 接口 + Metric 类型定义
│   │   ├── registry.go              # 注册表（扩展机制核心）
│   │   └── scheduler.go             # 调度引擎（按周期定时调用各 Collector）
│   ├── platform/                    # 平台抽象层（新增）
│   │   ├── platform.go              # 共享接口（DataDir, ConfigPath）
│   │   ├── platform_linux.go        # Linux 默认路径
│   │   └── platform_windows.go      # Windows 默认路径
│   ├── collectors/                  # 具体采集器实现
│   │   ├── cpu/
│   │   │   ├── cpu.go               # 共享：struct, Collect(), 工具函数
│   │   │   ├── cpu_linux.go         # Linux: 通过来源层采集 usage/loadavg 等
│   │   │   ├── cpu_metrics.go       # 跨平台(无tag)：拓扑/频率/缓存/MCE/IPMI 等新指标
│   │   │   ├── cpu_windows.go       # Windows: GetSystemTimes + PowerShell
│   │   │   └── cpu_test.go          # 测试 (//go:build linux)
│   │   ├── memory/
│   │   │   ├── memory.go            # 共享
│   │   │   ├── memory_linux.go      # Linux: 通过来源层采集
│   │   │   ├── memory_metrics.go    # 跨平台(无tag)：swap/PSI/碎片化/DIMM 等新指标
│   │   │   ├── memory_windows.go     # Windows: GlobalMemoryStatusEx
│   │   │   └── memory_test.go       # 测试 (//go:build linux)
│   │   ├── disk/
│   │   │   ├── disk.go              # 共享
│   │   │   ├── disk_linux.go        # Linux: 通过来源层(statfs/proc/smartctl/dmesg)
│   │   │   ├── disk_windows.go      # Windows: GetDiskFreeSpaceExW, GetLogicalDrives
│   │   │   └── disk_test.go         # 测试 (//go:build linux)
│   │   ├── gpu/
│   │   │   ├── gpu.go               # 跨平台: nvidia-smi (os/exec)
│   │   │   └── gpu_test.go
│   │   ├── npu/
│   │   │   ├── npu.go               # 跨平台: npu-smi (os/exec)
│   │   │   └── npu_test.go
│   │   └── network/
│   │       ├── network.go           # 共享
│   │       ├── network_linux.go     # Linux: 通过来源层(proc/sys)
│   │       ├── network_windows.go   # Windows: Get-NetAdapterStatistics (PowerShell)
│   │       └── network_test.go      # 测试 (//go:build linux)
│   ├── source/                      # 来源层（v0.2.0 新增）：数据获取与解析抽象
│   │   ├── source.go                # 通用 Source 接口 {Name(); Available()}
│   │   ├── proc/                    # /proc 全量解析（11 个 typed 方法）
│   │   ├── sys/                     # /sys 解析（freq/cache/corestate/thermal/net）
│   │   ├── ipmi/                    # ipmitool SDR/DCMI（30s缓存+失败缓存+5s超时）
│   │   ├── lscpu/                   # lscpu 拓扑（常驻 sync.Once）
│   │   ├── mce/                     # mcelog/dmesg MCE 事件
│   │   ├── dmesg/                   # dmesg（30s缓存+失败缓存）
│   │   ├── dmidecode/               # dmidecode DIMM（常驻 sync.Once）
│   │   ├── statfs/                  # statfs(2)（Linux 专有，//go:build linux）
│   │   └── smartctl/                # smartctl -H（per-dev 60s缓存+失败缓存）
│   ├── health/                      # 健康度评估模块（独立，纯逻辑跨平台）
│   │   ├── health.go                # HealthEvaluator + Evaluate() 入口
│   │   ├── rules.go                 # 评分规则定义（权重、扣分项）
│   │   ├── scorer.go                # 计分器实现
│   │   └── health_test.go
│   ├── config/                      # 配置管理
│   │   └── config.go                # 配置结构体 + 加载逻辑
│   └── storage/                     # 数据存储
│       └── storage.go               # JSON 文件写入器
├── web/                             # Web 仪表盘（v0.2.1 新增，独立二进制）
│   ├── main.go                      # 入口：blank-import 采集器 + 采集 goroutine + HTTP server + 端口回退 + 信号处理
│   ├── static.go                    # //go:embed static，内嵌前端资源
│   ├── config.go                    # 配置结构 + YAML 加载 + runtime.json 运行时覆盖
│   ├── collector.go                 # DataCollector：定时采集 → 健康度 → 原子写 snapshot + 环形历史 + 热重载 + 静态 specs stash
│   ├── snapshot.go                  # Snapshot 结构（含 Specs 字段）+ 原子读写
│   ├── hwinfo.go                    # 一次性硬件身份采集（device_model/gpu_info/npu_info/disk_info/net_info）
│   ├── server.go                     # HTTP 路由与处理函数
│   ├── config.yaml                   # 默认配置
│   ├── static/                       # 前端资源（index.html + style.css + app.js）
│   └── data/                         # 运行时数据（snapshot.json / runtime.json，git 忽略）
├── configs/
│   └── catmonitor.yaml              # 默认配置文件
├── docs/
│   ├── CATMonitor_indi_list.md      # 指标清单文档
│   └── test_report.md               # 测试报告
├── tests/
│   ├── framework.go                 # 测试框架
│   ├── integration_test.go          # 集成测试
│   └── testdata/                    # 测试数据（/proc、/sys 模拟文件）
├── scripts/
│   └── install.sh                   # 安装为 systemd 服务
├── go.mod
├── go.sum
└── Makefile
```

### 1.5 数据流与数据格式

```
  Scheduler 按各自周期触发
         │
         ▼
  Collector.Collect()  ──→  []Metric
         │                    │
         │ Linux: 经来源层 source.Xxx() 拿 parsed struct
         │   (proc/sys/ipmi/lscpu/mce/dmesg/dmidecode/statfs/smartctl)
         │ Windows: kernel32.dll / PowerShell 直接 syscall
         ▼
  Storage.Write(metrics)  ──→  JSON 文件
  (路径: {data_dir}/{component}_{date}.jsonl)
         │
         ▼
  HealthEvaluator.Evaluate(latestMetrics)  ──→  HealthScore
         │                                     (自动检测 GPU/NPU)
         ▼
  Storage.WriteHealth(score)  ──→  health_{date}.jsonl
```

数据文件格式（JSONL — 每行一个 JSON 对象）：

```jsonl
{"component":"cpu","name":"usage","value":45.2,"unit":"%","labels":{"core":"0"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"cpu","name":"usage","value":43.8,"unit":"%","labels":{"core":"1"},"timestamp":"2026-07-10T10:30:00Z"}
```

健康度文件：

```jsonl
{"score":85,"grade":"Good","components":{"cpu":{"score":25,"max":30,"details":[...]},"memory":{"score":35,"max":40,"details":[...]}},"timestamp":"2026-07-10T10:30:00Z"}
```

### 1.6 来源层设计（v0.2.0 新增）

为解耦采集器与系统数据获取细节，引入 `internal/source/` 来源层。采集器不再直接 `os.ReadFile`/`exec`，而是调用来源包拿 parsed struct。

#### 设计原则

1. **parsed struct 返回**：来源返回 typed struct（如 `proc.CPUStat`、`proc.Meminfo`），采集器只做指标映射，不做字符串解析
2. **单例 + 可注入**：来源包暴露单例访问点 + `SetRoot(path)`（重定向 /proc、/sys 测试根）+ 可注入 fetcher（测试时 mock exec）
3. **缓存策略分档**：
   - **不缓存**：`proc`/`sys`/`statfs`（实时性要求高）
   - **带 TTL 缓存**：`ipmi`(30s)、`dmesg`(30s)、`smartctl`(per-dev 60s)
   - **常驻缓存 (sync.Once)**：`lscpu`、`dmidecode`（拓扑静态，启动采集一次）
4. **失败缓存（negative cache）**：`ipmi`/`dmesg`/`smartctl` 无硬件或未安装时，失败结果也缓存，避免每周期重试 exec
5. **跨平台降级**：`*_metrics.go` 为跨平台文件（无 build tag），Windows 上来源层不可用时返回空（优雅降级）
6. **不建 Registry**：决策上暂不引入 `source.Registry` + list，采集器按需 import 来源包

#### 来源包清单

| 包 | 数据源 | typed 方法 | 缓存 | 备注 |
|----|--------|-----------|------|------|
| proc | /proc 全量 | Stat/Loadavg/Meminfo/Diskstats/NetDev/Vmstat/Cpuinfo/Buddyinfo/Mounts/NetTCPStates/Pressure | 无 | 11 个方法 |
| sys | /sys | CpuFreqs/CacheInfos/CpuOnline·Offline·Isolated/Nodes/Edac/NetOperstate/NetInterfaces/Thermal | 无 | 符号链接修复 (IsDir \|\| ModeSymlink) |
| ipmi | ipmitool SDR/DCMI | SDR()/DCMIPower() | 30s + 失败 + 5s 超时 | fetcher 可注入；温度/功率共用一份 SDR |
| lscpu | lscpu | Topology() | 常驻 (sync.Once) | 拓扑静态 |
| mce | mcelog/dmesg | Errors() | 无 | MCE CE/UCE 事件 |
| dmesg | dmesg | Text() | 30s + 失败 | 供 oom_count / io_errors |
| dmidecode | dmidecode --type 17 | MemoryDevices() | 常驻 (sync.Once) | DIMM 信息 |
| statfs | statfs(2) | Statfs(path) | 无 | Linux 专有 (`//go:build linux`)；fetcher 可注入 |
| smartctl | smartctl -H | Health(dev) | per-dev 60s + 失败 | |

#### 通用接口

```go
// internal/source/source.go
type Source interface {
    Name() string        // 来源名称
    Available() bool     // 当前环境是否可用（外部命令存在/权限正常）
}
```

#### 采集器与来源的依赖关系

| 采集器 | 依赖的来源包 | 产出指标示例 |
|--------|-------------|-------------|
| cpu | proc, sys, lscpu, mce, ipmi | usage/time/util, topology, freq, cache, MCE, temperature/power |
| memory | proc, dmidecode, ipmi, dmesg | usage_detail, swap, PSI 饱和度, 碎片化, DIMM, oom_count, power |
| disk | proc, statfs, smartctl, dmesg | space_usage, iops, throughput, io_wait, io_errors, SMART |
| network | proc, sys | throughput, packet_count, error_count, interface_status, connection_count |
| gpu | （未接入，待建 nvsmi 来源） | utilization, memory_usage, temperature, power_draw, ecc_errors, clock_frequency |
| npu | （未接入，待建 npsmi 来源） | utilization, memory_usage, temperature, power_draw, health_status |

---

## 2. 采集器详细设计

每个采集器是一个独立的 Go 包，位于 `internal/collectors/{component}/`。采集器启动时自动检测目标硬件/工具是否可用，不可用则自动跳过，不影响其他采集器运行。

### 2.1 CPU 采集器

| 项目 | 说明 |
|------|------|
| 包路径 | `internal/collectors/cpu` |
| Linux 数据来源 | `/proc/stat`、`/proc/loadavg`、`/sys/class/thermal/`、`/sys/devices/system/cpu/cpu*/cpufreq/`、`/proc/cpuinfo` |
| Windows 数据来源 | `GetSystemTimes` (kernel32.dll) for usage; PowerShell `Get-CimInstance Win32_Processor` for frequency/model_info; `(Get-Process).Count` for process_count |
| 外部依赖 | 无（纯 Go syscall + os/exec） |
| 采集方式 | Linux: 读取 /proc + /sys 文件解析；Windows: syscall 调用 + PowerShell

**采集逻辑**：
1. **usage**：读取 `/proc/stat` 中 `cpu` 行和 `cpu0`~`cpuN` 行的 10 个时间字段（user/nice/system/idle/iowait/irq/softirq/steal/guest/guest_nice），与上次快照差值计算使用率。公式：`usage% = (total_delta - idle_delta) / total_delta × 100`。每个核心和总体各输出一条。
2. **load_average**：读取 `/proc/loadavg` 前三个字段，分别对应 1m/5m/15m 负载。
3. **temperature**：遍历 `/sys/class/thermal/thermal_zone*/temp`，值为毫摄氏度，除以 1000 转换。
4. **frequency**：遍历 `/sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq`，值为 kHz，除以 1000 转换。
5. **context_switches**：读取 `/proc/stat` 中 `ctxt` 行，差值除以间隔得出每秒切换次数。
6. **process_count**：解析 `/proc/loadavg` 第四字段 `running/total`。
7. **model_info**：解析 `/proc/cpuinfo`，启动时采集一次，提取型号名、核心数、缓存大小。

**错误处理**：温度/频率文件不存在时跳过该核心，不影响其他核心采集。首次采集时（无历史快照），usage 类指标返回 0 并等待下一次采集。

### 2.2 Memory 采集器

| 项目 | 说明 |
|------|------|
| 包路径 | `internal/collectors/memory` |
| Linux 数据来源 | `/proc/meminfo`、`/sys/devices/system/edac/mc/`、`/proc/vmstat` |
| Windows 数据来源 | `GlobalMemoryStatusEx` (kernel32.dll) for usage/swap_usage |
| 外部依赖 | Linux: `dmesg`（仅 OOM 指标）；Windows: 无 |
| 采集方式 | Linux: 文件读取 + 解析；Windows: syscall 调用（纯 Go） |

**采集逻辑**：
1. **usage**：读取 `/proc/meminfo` 的 `MemTotal`、`MemAvailable` 字段，使用率 = `(MemTotal - MemAvailable) / MemTotal × 100`。同时输出 total/used/available 明细值（MB）。
2. **swap_usage**：读取 `SwapTotal`、`SwapFree`，使用率 = `(SwapTotal - SwapFree) / SwapTotal × 100`。
3. **ecc_ce_errors**：遍历 `/sys/devices/system/edac/mc/mc*/ce_count`，读取每个内存控制器的 CE 错误累计数。EDAC 不支持时返回 0。
4. **ecc_uce_errors**：遍历 `/sys/devices/system/edac/mc/mc*/ue_count`，读取 UCE 错误累计数。EDAC 不支持时返回 0。
5. **oom_count**：执行 `dmesg` 或 `journalctl -k --since "5min ago"` 搜索 "Out of memory"/"Killed process" 关键词，统计 OOM 触发次数。
6. **page_faults**：读取 `/proc/vmstat` 的 `pgfault`/`pgmajfault`，差值除以间隔得出每秒缺页次数。

**错误处理**：EDAC 路径不存在时记录一条 INFO 日志说明服务器不支持 EDAC，该指标返回 0。`dmesg`/`journalctl` 不可用时跳过 oom_count 指标。

### 2.3 Disk 采集器

| 项目 | 说明 |
|------|------|
| 包路径 | `internal/collectors/disk` |
| Linux 数据来源 | `/proc/mounts`、`statfs` 系统调用、`/proc/diskstats`、`/proc/stat` |
| Windows 数据来源 | `GetDiskFreeSpaceExW` + `GetLogicalDrives` + `GetVolumeInformationW` (kernel32.dll) |
| 外部依赖 | Linux: `smartctl`（仅 SMART 指标）；Windows: 无 |
| 采集方式 | Linux: 系统调用 + 文件解析；Windows: syscall 调用（纯 Go） |

**采集逻辑**：
1. **space_usage**：读取 `/proc/mounts` 获取挂载点列表，过滤虚拟文件系统（proc/sysfs/devtmpfs/tmpfs/overlay 等），对每个挂载点调用 `statfs()` 获取总块数、空闲块数、块大小，计算使用率。同时输出 total/used/available 明细值（MB）。
2. **iops**：读取 `/proc/diskstats`，取第4字段（reads completed）和第8字段（writes completed），差值除以间隔得出每秒 IOPS。只采集主块设备（sda/nvme0n1 等），排除分区。
3. **throughput**：读取 `/proc/diskstats`，取第6字段（sectors read）和第10字段（sectors written），`扇区数 × 512B` 差值除以间隔得出 MB/s。
4. **io_wait**：读取 `/proc/stat` 中 `cpu` 行第5字段（iowait），与总 CPU 时间差值计算占比。
5. **smart_status**：对每个块设备执行 `smartctl -H /dev/sdX`，解析输出中的 `PASSED`/`FAILED`。
6. **smart_temperature**：执行 `smartctl -A /dev/sdX`，解析 SMART 属性表中的 `Temperature_Celsius`。
7. **io_errors**：读取 `/proc/diskstats` 错误字段 + 搜索 `dmesg` 中 I/O error 关键词。

**设备过滤规则**：排除虚拟设备（loop/ram/dm-/md 等），只采集物理块设备。设备名匹配正则 `^(sd|nvme|vd|xvd|hba)[a-z]+[0-9]*n[0-9]+$`。

### 2.4 GPU 采集器（NVIDIA）

| 项目 | 说明 |
|------|------|
| 包路径 | `internal/collectors/gpu` |
| 数据来源 | `nvidia-smi` 命令输出（Linux/Windows 双平台通过 os/exec 调用） |
| 外部依赖 | `nvidia-smi`（NVIDIA 驱动自带） |
| 采集方式 | 单次 `nvidia-smi --query-gpu=...` 批量查询，解析 CSV 输出 |
| 平台分离 | 无需分离，`os/exec` 在双平台行为一致 |
| 可用性检测 | 启动时执行 `exec.LookPath("nvidia-smi")`，不可用则跳过 |

**采集逻辑**：

单次执行以下命令，一次获取所有 GPU 的全部字段：

```bash
nvidia-smi \
  --query-gpu=index,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,fan.speed,ecc.errors.uncorrected.volatile.total,clocks.gr \
  --format=csv,noheader,nounits
```

按行解析输出，每行对应一块 GPU，字段间逗号分隔：
1. **utilization**：第2列，GPU 计算单元使用率（%）。
2. **memory_usage**：第3/4列计算，`memory.used / memory.total × 100`，同时输出 used/total 明细。
3. **temperature**：第5列，核心温度（°C）。
4. **power_draw**：第6列，实时功耗（W）。
5. **fan_speed**：第7列，风扇转速占百分比（%），无风扇的返回 N/A。
6. **ecc_errors**：第8列，不可纠正 ECC 错误累计数。
7. **clock_frequency**：第9列，图形时钟频率（MHz）。

**错误处理**：`nvidia-smi` 执行超时（默认 10s）或返回错误时，记录日志并跳过本次采集，不影响下次采集。某块 GPU 的字段为 `N/A` 时，该指标值设为 -1 并在 Labels 中标注 `unavailable: true`。

### 2.5 NPU 采集器（华为昇腾）

| 项目 | 说明 |
|------|------|
| 包路径 | `internal/collectors/npu` |
| 数据来源 | `npu-smi info` 命令输出（Linux/Windows 双平台通过 os/exec 调用） |
| 外部依赖 | `npu-smi`（昇腾驱动自带） |
| 采集方式 | 执行 `npu-smi info`，解析表格格式输出 |
| 平台分离 | 无需分离，有驱动时双平台可用 |
| 可用性检测 | 启动时执行 `exec.LookPath("npu-smi")`，不可用则跳过 |

**采集逻辑**：

执行 `npu-smi info`，解析表格输出。每块 NPU 占两行，需按 NPU ID 分组解析：

1. **utilization**：解析 `AICore(%)` 列，NPU 使用率（%）。
2. **memory_usage**：解析 `Memory-Usage(MB)` 列，格式为 `used / total`，计算使用率。
3. **temperature**：解析 `Temp(C)` 列，NPU 温度（°C）。
4. **power_draw**：解析 `Power(W)` 列，实时功耗（W）。
5. **health_status**：解析 `Health` 列，状态值映射：OK=1, Warning=2, Alarm=3, Critical=4。

**错误处理**：`npu-smi` 执行超时（默认 10s）或返回错误时，记录日志并跳过本次采集。不同版本 `npu-smi` 输出格式可能略有差异，解析器需做兼容处理。

### 2.6 Network 采集器

| 项目 | 说明 |
|------|------|
| 包路径 | `internal/collectors/network` |
| Linux 数据来源 | `/proc/net/dev`、`/sys/class/net/`、`/proc/net/tcp`、`/proc/net/tcp6` |
| Windows 数据来源 | PowerShell `Get-NetAdapterStatistics` + `Get-NetAdapter` + `Get-NetTCPConnection` |
| 外部依赖 | Windows: PowerShell 4.0+ |
| 采集方式 | Linux: 文件读取 + 解析；Windows: os/exec 调用 PowerShell |

**采集逻辑**：
1. **throughput**：读取 `/proc/net/dev`，取 `bytes` 字段（接收第1列、发送第9列），差值除以间隔得出 bytes/s。过滤 `lo` 回环接口。
2. **packet_count**：取 `packets` 字段（接收第2列、发送第10列），差值除以间隔得出个/s。
3. **error_count**：取 `errs`（接收第3列、发送第11列）和 `drop`（接收第5列、发送第13列），累计错误计数。
4. **interface_status**：遍历 `/sys/class/net/*/operstate`，读取各网卡接口状态（up/down）。
5. **connection_count**：解析 `/proc/net/tcp` 和 `/proc/net/tcp6`，按状态码统计连接数。状态码：`01`=ESTABLISHED, `06`=TIME_WAIT, `0A`=LISTEN 等。

**接口过滤**：过滤 `lo` 回环接口。`docker0`/`br-*` 等虚拟网桥默认不采集，可通过配置开启。

---

## 3. 健康度评估模块设计

### 3.1 设计原则

- **独立模块**：`internal/health` 包，不依赖任何采集器实现
- **规则可配置**：评分规则集中在 `rules.go`，修改规则不影响采集逻辑
- **权重自适应**：根据服务器是否含 GPU/NPU 自动选择权重方案

### 3.2 模块结构

```
internal/health/
├── health.go          # HealthEvaluator 接口定义 + Evaluate() 入口
├── rules.go           # 评分规则定义（权重方案、扣分阈值）
├── scorer.go          # 计分器实现（遍历指标 → 匹配规则 → 计算扣分 → 输出总分）
└── health_test.go     # 表驱动测试
```

**工作流程**：
1. 接收最近一轮所有采集器输出的 `[]Metric`
2. 根据是否存在 GPU/NPU 指标自动选择权重方案
3. 遍历各部件指标，匹配扣分规则，计算各部件扣分
4. 多卡场景取最差卡扣分（可配置为平均扣分）
5. 各部件满额分减去扣分得部件得分，汇总为总分
6. 按总分映射健康等级
7. 输出 `HealthScore` 结构体（含总分、等级、各部件明细、扣分详情）

### 3.3 权重自适应判定逻辑

`Evaluate()` 方法在分组 metrics 后自动检测：
- 如果存在 GPU 指标（`byComponent["gpu"]` 非空），切换到 `Accelerated8CardScheme`（CPU:10, Mem:20, Disk:10, GPU:60）
- 如果存在 NPU 指标，同上
- 否则使用默认 `CPUOnlyScheme`（CPU:30, Mem:40, Disk:30）

> 判定逻辑基于实际采集到的指标，而非 `nvidia-smi` / `npu-smi` 是否可用。这样在无硬件或有硬件但采集失败时都能正确选择方案。

---

## 4. 测试框架设计

### 4.1 设计原则

- **每加一个指标，立即测试**：利用 Go 原生 `testing` + 表驱动测试
- **无硬件也能测**：GPU/NPU 采集器在无硬件环境用 Mock 测试
- **/proc /sys 模拟**：用 testdata 目录模拟 Linux procfs，保证测试可复现

### 4.2 测试框架组成

```
tests/
├── framework.go                 # 通用测试工具
│   ├── AssertMetric()           # 断言单条指标值
│   ├── AssertMetricExists()     # 断言指标存在
│   ├── MockProcFS()             # 挂载模拟 /proc、/sys 文件系统
│   └── RunCollectorTest()       # 通用采集器测试流程
├── integration_test.go          # 端到端集成测试
└── testdata/                    # 模拟数据
    ├── proc/
    │   ├── stat                 # 模拟 /proc/stat
    │   ├── meminfo              # 模拟 /proc/meminfo
    │   ├── loadavg              # 模拟 /proc/loadavg
    │   ├── diskstats            # 模拟 /proc/diskstats
    │   └── net/dev              # 模拟 /proc/net/dev
    ├── sys/
    │   ├── class/thermal/       # 模拟温度
    │   └── devices/system/edac/ # 模拟 ECC
    └── nvidia-smi-output.txt    # 模拟 nvidia-smi 输出
```

### 4.3 测试层级

| 层级 | 范围 | 工具 |
|------|------|------|
| 单元测试 | 每个采集器独立 | Go testing + testdata |
| 集成测试 | 多采集器协同 + 调度引擎 | Go testing |
| 健康度测试 | 评分计算正确性 | 表驱动测试 |
| Mock 测试 | GPU/NPU 无硬件场景 | 模拟 nvidia-smi/npu-smi 输出 |
| 端到端测试 | 守护进程启动→采集→存储→评分 | Go testing + 临时目录 |

### 4.4 测试命令

```bash
make test          # 运行全部测试
make test-verbose  # 详细输出
make test-coverage # 覆盖率报告
make lint          # 代码检查
```

---

## 5. 命令行设计

### 5.1 命令结构

```
catmonitor [command] [flags]
```

支持子命令模式，默认行为（不带子命令）等同于 `daemon`。

### 5.2 子命令

| 子命令 | 说明 | 示例 |
|--------|------|------|
| `daemon` | 启动守护进程，持续周期采集指标并计算健康度 | `catmonitor daemon` |
| `collect` | 单次采集所有指标，输出快照到标准输出或文件 | `catmonitor collect` |
| `health` | 基于当前指标执行一次健康检查，输出评估报告 | `catmonitor health` |
| `status` | 查看守护进程运行状态（PID、运行时长、已注册采集器） | `catmonitor status` |
| `list` | 列出所有已注册采集器及其指标清单 | `catmonitor list` |
| `version` | 显示版本号、Go 版本、编译时间 | `catmonitor version` |

### 5.3 全局参数

| 参数 | 短选项 | 默认值 | 说明 |
|------|--------|--------|------|
| `--config` | `-c` | 平台自适应 (Linux: `/etc/catmonitor/catmonitor.yaml`, Windows: `C:\ProgramData\catmonitor\catmonitor.yaml`) | 配置文件路径 |
| `--data-dir` | `-d` | 平台自适应 (Linux: `/var/lib/catmonitor/data`, Windows: `C:\ProgramData\catmonitor\data`) | 数据输出目录 |
| `--component` | 无 | 空（全部） | 只采集指定部件，逗号分隔：`cpu,memory,disk` |
| `--output` | `-o` | `json` | 输出格式：`json` / `table` / `yaml` |
| `--interval` | `-i` | 空（使用配置） | 覆盖采集周期，如 `5s` |
| `--verbose` | `-v` | false | 输出详细日志（调试用） |
| `--help` | `-h` | — | 显示帮助信息 |

### 5.4 使用场景

#### 场景一：启动守护进程（日常运行）

```bash
# 使用默认配置启动
catmonitor daemon

# 指定配置文件启动
catmonitor daemon -c /etc/catmonitor/my-config.yaml

# 前台运行（调试模式）
catmonitor daemon -v

# 指定数据输出目录
catmonitor daemon --data-dir /tmp/catmonitor-data
```

守护进程启动后，按各采集器配置周期持续采集指标，写入 `{data_dir}/{component}_{date}.jsonl`，同时按健康度周期写入 `health_{date}.jsonl`。

#### 场景二：单次采集快照（巡检）

```bash
# 采集所有指标，输出 JSON
catmonitor collect

# 只采集 CPU 和内存
catmonitor collect --component cpu,memory

# 采集并以表格形式输出
catmonitor collect -o table

# 采集并保存到指定文件
catmonitor collect -o json > /tmp/snapshot.json

# 覆盖采集周期为 1s（仅影响单次采集的行为）
catmonitor collect --interval 1s
```

表格输出示例：

```
┌───────────┬──────────────┬────────┬─────────┬──────────────────┐
│ Component │ Metric       │ Value  │ Unit    │ Labels           │
├───────────┼──────────────┼────────┼─────────┼──────────────────┤
│ cpu       │ usage        │ 45.2   │ %       │ core=total       │
│ cpu       │ load_average │ 2.34   │         │ interval=1m      │
│ memory    │ usage        │ 62.5   │ %       │                  │
│ memory    │ swap_usage   │ 15.3   │ %       │                  │
│ disk      │ space_usage  │ 72.5   │ %       │ mount=/          │
│ network   │ throughput   │ 125000 │ bytes/s │ if=eth0,dir=rx   │
└───────────┴──────────────┴────────┴─────────┴──────────────────┘
```

#### 场景三：健康检查（运维诊断）

```bash
# 执行一次健康检查，输出 JSON 格式报告
catmonitor health

# 以表格形式输出
catmonitor health -o table

# 保存健康报告
catmonitor health -o json > /tmp/health-report.json
```

健康报告输出示例（JSON）：

```json
{
  "score": 85,
  "grade": "Good",
  "server_type": "accelerated_8card",
  "components": {
    "cpu":     {"score": 9,  "max": 10, "deductions": [{"rule": "usage>80%", "penalty": -1}]},
    "memory":  {"score": 18, "max": 20, "deductions": [{"rule": "ce_error", "penalty": -2}]},
    "disk":    {"score": 10, "max": 10, "deductions": []},
    "gpu":     {"score": 48, "max": 60, "deductions": [{"rule": "temp>80C", "penalty": -9}, {"rule": "mem>95%", "penalty": -3}]}
  },
  "timestamp": "2026-07-10T10:30:00Z"
}
```

健康报告输出示例（表格）：

```
┌───────────┬───────┬──────┬──────────────────────────┐
│ Component │ Score │ Max  │ Deductions               │
├───────────┼───────┼──────┼──────────────────────────┤
│ cpu       │ 9     │ 10   │ usage>80%: -1            │
│ memory    │ 18    │ 20   │ ce_error(x1): -2         │
│ disk      │ 10    │ 10   │ (none)                   │
│ gpu       │ 48    │ 60   │ temp>80°C: -9, mem>95%: -3│
├───────────┼───────┼──────┼──────────────────────────┤
│ TOTAL     │ 85    │ 100  │ Grade: Good              │
└───────────┴───────┴──────┴──────────────────────────┘
```

#### 场景四：查看采集器列表（配置确认）

```bash
catmonitor list
```

输出示例：

```
Registered Collectors:
┌──────────┬──────────┬──────────┬──────────┬─────────┬─────────┐
│ Name     │ Component│ Priority │ Interval │ Enabled │ Metrics │
├──────────┼──────────┼──────────┼──────────┼─────────┼─────────┤
│ cpu      │ cpu      │ High     │ 3s       │ true    │ 40      │
│ memory   │ memory   │ High     │ 3s       │ true    │ 19      │
│ disk     │ disk     │ High     │ 5s       │ true    │ 7       │
│ gpu      │ gpu      │ High     │ 3s       │ true    │ 7       │
│ npu      │ npu      │ High     │ 3s       │ false   │ 5       │
│ network  │ network  │ High     │ 3s       │ true    │ 5       │
└──────────┴──────────┴──────────┴──────────┴─────────┴─────────┘
```

#### 场景五：查看守护进程状态（运维监控）

```bash
catmonitor status
```

输出示例：

```
CATMonitor Daemon Status
┌─────────────────┬──────────────────────────────┐
│ PID             │ 12345                        │
│ Uptime          │ 3h 24m 15s                   │
│ Active Collectors │ 5 (cpu, memory, disk, gpu, network) │
│ Data Directory  │ /var/lib/catmonitor/data     │
│ Server Type     │ accelerated_8card            │
│ Last Health     │ 85 (Good) @ 2026-07-10 10:29 │
│ Config File     │ /etc/catmonitor/catmonitor.yaml │
└─────────────────┴──────────────────────────────┘
```

### 5.5 systemd 集成

安装为 systemd 服务（Phase 3 实现）：

```bash
# 安装服务
sudo scripts/install.sh

# 启动/停止/重启
sudo systemctl start catmonitor
sudo systemctl stop catmonitor
sudo systemctl restart catmonitor

# 查看状态
sudo systemctl status catmonitor

# 查看日志
sudo journalctl -u catmonitor -f
```

systemd service 文件：

```ini
[Unit]
Description=CATMonitor - Server Metrics Collector
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/catmonitor daemon -c /etc/catmonitor/catmonitor.yaml
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

---

## 6. Web 仪表盘设计（v0.2.1 新增）

> 详细规格见 [Web_SPEC.md](Web_SPEC.md)。本节描述架构、数据流与扩展机制。

### 6.1 模块定位与解耦

`web/` 是与主项目同一 Go module 的独立二进制 `catmonitor-web`，**不新增 go.mod、不改主项目任何文件**。与 `cmd/catmonitor`、`internal/collectors`、`internal/health`、`internal/storage`、`internal/config`、`internal/platform` 解耦，仅通过只读复用（blank import + 调用注册表/健康度接口）获取数据。

### 6.2 目录结构

```
web/
├── main.go            # 入口：blank-import 采集器 + 起采集 goroutine + HTTP server + 信号处理 + 端口回退
├── static.go          # //go:embed static，内嵌前端资源
├── config.go          # 配置结构 + YAML 加载 + runtime.json 运行时覆盖
├── collector.go       # DataCollector：定时采集 → 健康度 → 原子写 snapshot + 环形历史 + 热重载 + 静态 specs stash
├── snapshot.go        # Snapshot 结构（含 Specs 字段）+ 原子读写
├── hwinfo.go          # 一次性硬件身份采集（device_model/gpu_info/npu_info/disk_info/net_info），非注册采集器
├── server.go          # HTTP 路由与处理函数
├── config.yaml        # 默认配置
├── static/
│   ├── index.html     # SPA 外壳（顶栏 + nav + #page 容器）
│   ├── style.css       # 浅色卡片式主题
│   └── app.js          # SPA 路由 + 概览页 + 部件详情页 + 扩展 manifest
└── data/              # 运行时数据（运行时生成，git 忽略）
    ├── snapshot.json  # 采集 goroutine 写，HTTP 层读
    └── runtime.json   # 界面调整的刷新间隔持久化
```

### 6.3 数据流与解耦边界

```
  采集 goroutine (DataCollector)          HTTP server (net/http)
    定时: 遍历注册表 → Collect()            静态页 + REST API
         → health.Evaluate()                  读取 snapshot.json
         → 原子写 snapshot.json                  ↑读（不调采集器）
                  │写
                  └──────── snapshot.json ────────┘
                                  ↑热更新间隔
                   浏览器（SPA：概览 + 各部件详情页）fetch /api/snapshot
```

**解耦边界**：HTTP 层**只读** `snapshot.json`，**绝不直接调用采集器**；采集 goroutine 是 `snapshot.json` 的**唯一写者**（写临时文件 + `os.Rename` 原子写，读者永不会读到半截文件）。

### 6.4 Snapshot 数据模型

`Snapshot`（`snapshot.go`）是 HTTP 层唯一数据源，字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| `timestamp` | time | 本次快照生成时间 |
| `refresh_interval_ms` | int | 当前生效的采集周期（毫秒），供前端轮询对齐 |
| `history_points` | int | 历史环形缓冲容量 |
| `health` | `health.HealthScore` | 健康度结果（直接复用 `internal/health` 序列化） |
| `metrics` | `[]collector.Metric` | 本次采集的全部指标（复用 `internal/collector.Metric`） |
| `history` | `map[string][]float64` | 趋势序列，key 形如 `<component>_<suffix>`，供详情页按部件前缀过滤 |
| `specs` | `[]collector.Metric` | 静态设备规格（一次性身份信息），`omitempty`：无任何静态指标时不出现 |

> `health` 与 `metrics` 直接使用主项目结构体 JSON tag，**不重新定义**，保证与采集器/健康度模块的契约一致。

### 6.5 DataCollector 采集与历史

- `Run(ctx)`：立即采集一次，进入 `select` 循环：定时器到期 → 采集；`reload` 通道 → 重置定时器（间隔热更新）；`collectNow` 通道 → 立即采集；`ctx.Done` → 退出。
- `collectOnce()`：遍历 `collector.DefaultRegistry.All()` → `Collect()` → `health.NewEvaluator(health.GetScheme("auto")).Evaluate()` → 组装 `Snapshot` → `WriteAtomic`。
- **历史趋势**由可扩展的 `trackedSeries` spec 列表驱动（key 形如 `<component>_<suffix>`），环形缓冲每 key 保留最近 `history_points` 个点。当前跟踪 cpu_usage / cpu_load_average / memory_usage / memory_swap_usage / disk_space_usage 及 GPU/NPU 利用率/显存/温度。
- **静态规格 stash**：CPU/内存采集器首周期产出一次静态指标后被抑制，Web 侧 `filterStatic` 提取并缓存到 `staticStash`，之后每周期重新注入快照。

### 6.6 硬件身份采集（hwinfo.go）

`main.go` 启动时起 goroutine 调 `collectHWSpecs()`（**非注册采集器**），收集跨部件身份指标：

| metric name | component | 来源 |
|-------------|-----------|------|
| `device_model` | system | dmidecode SMBIOS type 1 |
| `gpu_info` | gpu | nvidia-smi |
| `npu_info` | npu | npu-smi info |
| `disk_info` | disk | /sys/block + smartctl 富化 |
| `net_info` | network | /sys/class/net（跳过 lo） |

外部命令缺失则降级（不报错），`/sys` 始终可用。结果经 `SetHWSpecs` 存入 `hwSpecs`（`hwMu` 保护），每周期合入 `specs = staticStash + hwSpecs`。

### 6.7 端口占用回退（main.go listenWithFallback）

启动 HTTP 前先以 `net.Listen("tcp", addr)` 探测端口，避免 `ListenAndServe` 异步失败难定位：

1. `net.SplitHostPort` 解析 host/port；不可解析则直接 listen 原值（不回退）。
2. 循环 `net.Listen`：成功返回；失败且 `errors.Is(err, syscall.EADDRINUSE)` → 端口 +1 重试。
3. 其他错误（权限不足等）直接失败退出。listener 交给 `http.Server.Serve`，实际绑定地址回写配置并打印日志。跨平台有效。

### 6.8 HTTP API 与前端设计

- **路由**（`server.go`）：`GET /`（SPA 外壳）、`GET /static/{file}`、`GET /api/snapshot`、`GET /api/collectors`、`GET|POST /api/config`、`POST /api/refresh`。详见 SPEC §9.7。
- **前端**（`static/`）：SPA + hash 路由（`#/` 概览，`#/<component>` 详情）。概览页含健康度面板 + 设备规格面板（点击弹出完整规格 modal）+ 部件芯片 + 概览卡网格；详情页含趋势面板（自动列出 `<component>_*` 历史 sparkline）+ 全部指标表。
- **显示 manifest**（`app.js`）：`MANIFEST`（部件显示名/关键指标）、`SERIES_LABELS`（序列显示名）、`NAV_ORDER`（导航排序）、`SPEC_DEFS`/`LABEL_NAMES`（规格面板）。未登记部件/指标/序列均有通用回退，不会崩溃。

### 6.9 扩展机制

| 扩展需求 | 改动位置 | 自动部分 |
|----------|----------|----------|
| 新部件采集器 | `web/main.go`（blank import） | 导航/概览卡/详情页 |
| 部件显示名/关键指标 | `web/static/app.js` MANIFEST | — |
| 新趋势 sparkline | `web/collector.go` trackedSeries 加一行 | 详情页趋势面板 |
| 趋势显示名 | `web/static/app.js` SERIES_LABELS | — |
| 新静态身份指标（采集器侧） | 加入 `staticMetricNames` 即被 stash 进 `specs` | specs modal 自动渲染 |

> **结论：一行 blank import 即可让新部件完整可用**；后续按需在 MANIFEST/trackedSeries 美化。`health` 与 `metrics` 字段直接复用主项目结构体，采集器新增任何字段/标签都原样透传到前端。

### 6.10 已知限制与后续预留

1. **单机本地视图**：不含认证、不含多机聚合；如需多机，预留"多个 snapshot 源 + 概览聚合"。
2. **轮询而非推送**：前端 `setInterval` 轮询 `/api/snapshot`；如需实时推送，预留 WebSocket/SSE（`snapshot.json` 解耦边界可直接复用）。
3. **无持久化历史存储**：历史仅存内存环形缓冲（重启清空），未落盘；如需长期趋势，预留 JSONL 落盘。
4. **指标展示优先级**：当前 metric 不携带优先级字段，概览关键指标靠 MANIFEST 人工指定；未来若主项目 Metric 增加优先级可改为自动选取。
