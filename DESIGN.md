# CATMonitor 设计文档 (DESIGN)

> 本文档描述 CATMonitor 的架构设计、模块设计、采集器设计和命令行设计。
> 规格与需求见 [SPEC.md](SPEC.md)，指标清单见 [CATMonitor_indi_list.md](docs/CATMonitor_indi_list.md)。

---

## 1. 架构设计

### 1.1 分层架构

```
┌─────────────────────────────────────────────────────┐
│   cmd/catmonitor            features/web            │
│   (守护进程入口)            (catmonitor-web 仪表盘)  │
├─────────────────────────────────────────────────────┤
│  features/ (特性层：基于采集基础能力构建的上层模块)    │
│  ┌──────────────────────┐  ┌──────────────────────┐  ┌──────────────┐ │
│  │ features/health      │  │ features/web         │  │ features/dfee │ │
│  │ 健康度评估(消费       │  │ Web 仪表盘(独立二进制)│  │ 能效监控(25图) │ │
│  │  collector.Metric,    │  │ snapshot.json 解耦    │  │ /dfee/ 路由   │ │
│  │  按部件评估器+scheme) │  │ blank-import 采集器   │  │ 74项过滤+推导 │ │
│  └──────────────────────┘  └──────────────────────┘  └──────────────┘ │
├─────────────────────────────────────────────────────┤
│  internal/config   internal/metrics   internal/storage│
│  internal/platform  (指标采集目录: 默认+模块覆盖+Filter)│
│                    (配置管理 + 平台适配 + 数据写入)     │
├─────────────────────────────────────────────────────┤
│            internal/collector (采集核心)               │
│     ┌──────────┐  ┌──────────┐  ┌──────────────┐    │
│     │ Collector │  │ Registry │  │  Scheduler   │    │
│     │ Interface │  │ (注册表)  │  │  (调度+Filter)│    │
│     └──────────┘  └──────────┘  └──────────────┘    │
├─────────────────────────────────────────────────────┤
│            internal/collectors (采集器实现)           │
│   ┌─────┬──────────┬──────┬─────┬─────┬──────────┬───────┐  │
│   │ CPU │  Memory  │ Disk │ GPU │ NPU │  Network │Chassis│  │
│   │ Linux/Win 分离  │Linux/Win│Linux/Win│双平台│Linux专有│Linux专│  │
│   └───────────────────┴─────┴─────┴────────────┴───────┘  │
├─────────────────────────────────────────────────────┤
│         internal/source (来源层, 14 包)              │
│  proc/sys/ipmi/lscpu/mce/dmesg/dmidecode/statfs/     │
│  smartctl + dcmi/npu_smi/hccn_tool/nvidia_smi       │
│  parsed struct + 单例 + SetRoot/可注入 fetcher       │
│  collector 调用来源拿数据，不再直接 os.ReadFile/exec   │
├─────────────────────────────────────────────────────┤
│         Linux 系统接口 (procfs/sysfs/syscall/exec)    │
│         Windows 系统 API (kernel32/iphlpapi/PS)        │
└─────────────────────────────────────────────────────┘
```

> v0.2.0 引入来源层（`internal/source/`）后，Linux 采集器通过来源包间接访问 `/proc`、`/sys`、`statfs`、`ipmitool` 等系统接口；Windows 保留直接 syscall 实现（来源层迁移延后）。来源返回 parsed struct，带缓存（ipmi/dmesg/smartctl/hccn_tool）与可注入 fetcher，便于单元测试 mock。
>
> v0.2.1 新增 `web/` 模块（独立二进制 `catmonitor-web`），与主项目同一 Go module，不新增 go.mod、不改主项目任何文件。Web 复用采集器注册表与健康度模块（blank import），以 `snapshot.json` 为读写解耦边界：采集 goroutine 是唯一写者，HTTP 层只读快照文件。
>
> v0.2.2 来源层扩展至 14 包（新增 `dcmi`/`npu_smi`/`hccn_tool`/`nvidia_smi`），全部 6 个采集器接入来源层；NPU 指标 5→74 并在采集器层 device 并行采集（每块 NPU 一个 goroutine，单卡失败不影响其他卡）；DCMI 指标通过 CGo（`//go:build cgo && linux && dcmi`，`-tags dcmi`）绑定 `libdcmi.so`，默认构建排除并优雅降级；GPU 从内联 exec 迁移至 `nvidia_smi` 来源包（最后一个接入来源层的 collector）。
>
> v0.3.0 引入 **`features/` 特性层** + **`internal/metrics` 指标采集目录**：`web/`、`internal/health` 统一迁入 `features/`（`features/web`、`features/health`），health 重构为按部件评估器（消费 `collector.Metric`，`Evaluate` 用局部 scheme 不改写 receiver，规则对齐 indi_list High/Medium）；`internal/metrics` 提供 MetricSpec/Catalog/Filter，`configs/metrics.yaml` 为默认目录、模块自有 `metrics.yaml` 按 name 覆盖合并，scheduler 经 Filter 决定是否采集。
>
> v0.3.1 新增第 7 个采集器 `internal/collectors/chassis`（5 指标：整机功耗 / 进出风口温度 / 风扇转速 / 风扇功率，来自 ipmitool SDR，与 CPU/Memory 共享 30s SDR 缓存）；Disk 采集器新增 `read_latency`/`write_latency`（/proc/diskstats field 7/11，ms/s）；新增 `features/dfee` 能效监控模块（25 张实时图表 + CPU 8 jiffies→7 利用率推导 + 网络差值，从 159 项指标中过滤 74 项能效指标，独立 SPA 路由 `/dfee/`）。指标总数 152→159，部件 6→7。

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
- Linux 代码通过 `internal/source/` 来源层访问 `/proc`、`/sys`、`statfs`、`ipmitool` 等（v0.2.0；v0.2.2 全 6 采集器接入）
- Windows 代码使用 Go `syscall` 包直接调用 kernel32.dll / iphlpapi.dll，零第三方依赖
- GPU 采集器无需平台分离（`nvidia_smi` 来源包在双平台均可通过 `os/exec` 调用 nvidia-smi）
- NPU 采集器平台分离：`npu_linux.go`（74 指标 device 并行 + DCMI CGo + npu_smi/hccn_tool）与 `npu_other.go`（`//go:build !linux` no-op stub），Windows 上整体降级跳过
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
│   │   │   ├── gpu.go               # 跨平台: 经 nvidia_smi 来源包采集
│   │   │   └── gpu_test.go
│   │   ├── npu/
│   │   │   ├── npu.go               # 共享: struct/deviceIDs/prevEcc + device 并行 Collect()
│   │   │   ├── npu_linux.go         # Linux: ensureDevices + collectStatic + collectDevice(74 指标) (DCMI/npu_smi/hccn_tool)
│   │   │   ├── npu_other.go         # !linux no-op stub
│   │   │   └── npu_test.go
│   │   └── network/
│   │       ├── network.go           # 共享
│   │       ├── network_linux.go     # Linux: 通过来源层(proc/sys)
│   │       ├── network_windows.go   # Windows: Get-NetAdapterStatistics (PowerShell)
│   │       └── network_test.go      # 测试 (//go:build linux)
│   │   ├── chassis/                 # Chassis 机箱环境采集器（v0.3.1 新增）
│   │   │   ├── chassis.go           # 5 指标：power/inlet_temp/outlet_temp/fan_speed/fan_power (ipmitool SDR)
│   │   │   └── chassis_test.go      # 测试 (//go:build linux)
│   │   └── ...（其余 collector 子目录同上）
│   ├── source/                      # 来源层：数据获取与解析抽象（14 包，v0.2.0 引入，v0.2.2 扩展）
│   │   ├── source.go                # 通用 Source 接口 {Name(); Available()}
│   │   ├── proc/                    # /proc 全量解析（11 个 typed 方法）
│   │   ├── sys/                     # /sys 解析（freq/cache/corestate/thermal/net）
│   │   ├── ipmi/                    # ipmitool SDR/DCMI（30s缓存+失败缓存+5s超时）
│   │   ├── lscpu/                   # lscpu 拓扑（常驻 sync.Once）
│   │   ├── mce/                     # mcelog/dmesg MCE 事件
│   │   ├── dmesg/                   # dmesg（30s缓存+失败缓存）
│   │   ├── dmidecode/               # dmidecode DIMM（常驻 sync.Once）
│   │   ├── statfs/                  # statfs(2)（Linux 专有，//go:build linux）
│   │   ├── smartctl/                # smartctl -H（per-dev 60s缓存+失败缓存）
│   │   ├── dcmi/                    # libdcmi.so CGo 绑定（v0.2.2，//go:build cgo&&linux&&dcmi，服务 npu）
│   │   ├── npu_smi/                 # npu-smi -t topo/hccs-bw（v0.2.2，服务 npu）
│   │   ├── hccn_tool/               # hccn_tool 带宽/速度/链路（v0.2.2，服务 npu）
│   │   └── nvidia_smi/              # nvidia-smi 9 字段解析（v0.2.2，服务 gpu）
│   ├── metrics/                     # 指标采集目录（v0.3.0 新增）：MetricSpec/Catalog/Init/LoadModuleOverride/Filter
│   │   └── metrics.go
│   ├── config/                      # 配置管理
│   │   └── config.go                # 配置结构体 + 加载逻辑
│   └── storage/                     # 数据存储
│       └── storage.go               # JSON 文件写入器
├── features/                        # 特性层（v0.3.0 新增）：基于采集基础能力构建的上层模块
│   ├── health/                      #   健康度评估（消费 collector.Metric，按部件评估器）
│   │   ├── health.go                #     Evaluate() 入口 + 局部 scheme（不改写 receiver）
│   │   ├── scheme.go                #     权重方案（CPU-only / 加速卡）
│   │   ├── cpu.go / memory.go / disk.go / gpu.go / npu.go  # 按部件评估器 + 扣分规则
│   │   ├── util.go                   #     公共工具（取最差子温度等）
│   │   ├── metrics.yaml             #     health 自有指标目录（启动时优先读取）
│   │   └── HEALTH_SPEC.md           #     健康度规则规格
│   └── web/                         #   Web 仪表盘（v0.2.1 新增，v0.3.0 由 web/ 迁入 features/）
│       ├── main.go                  #     入口：blank-import 采集器 + 采集 goroutine + HTTP server + 端口回退 + 信号处理
│       ├── static.go                #     //go:embed static，内嵌前端资源
│       ├── config.go                #     配置结构 + YAML 加载 + runtime.json 运行时覆盖
│       ├── collector.go             #     DataCollector：定时采集 → 健康度 → 原子写 snapshot + 环形历史 + 热重载 + 静态 specs stash
│       ├── snapshot.go              #     Snapshot 结构（含 Specs 字段）+ 原子读写
│       ├── hwinfo.go                #     一次性硬件身份采集（device_model/gpu_info/npu_info/disk_info/net_info/os_info）
│       ├── server.go                #     HTTP 路由与处理函数（含 dfee.Register，v0.3.1）
│       ├── config.yaml              #     默认配置
│       ├── metrics.yaml             #     web 自有指标目录（启动时优先读取）
│       ├── static/                  #     前端资源（index.html + style.css + app.js，含能效分析导航入口）
│       └── data/                    #     运行时数据（snapshot.json / runtime.json，git 忽略）
│   └── dfee/                         #   能效监控模块（v0.3.1 新增，25 张实时图表 + CPU 利用率推导）
│       ├── dfee_SPEC.md             #     能效模块设计规格
│       ├── energy_efficiency_metrics.md #  74 项能效指标清单
│       ├── filter.go                #     能效指标过滤 + 分组
│       ├── cpu_derive.go            #     CPU 8 jiffies → 7 利用率推导
│       ├── net_derive.go            #     网络差值计算
│       ├── handler.go               #     HTTP handler + 静态文件服务
│       ├── embed.go                 #     //go:embed static
│       ├── metrics.yaml             #     dfee 指标目录覆盖（CPU Low → Medium）
│       └── static/                  #     前端（dfee.js + dfee.css + index.html）
├── configs/
│   ├── catmonitor.yaml              # 默认配置文件
│   └── metrics.yaml                 # 默认指标采集目录（6 部件，v0.3.0 新增）
├── docs/
│   └── CATMonitor_indi_list.md      # 指标清单文档
├── tests/
│   ├── framework.go                 # 测试框架
│   └── testdata/                    # 测试数据（/proc、/sys、npu-smi/hccn-tool 输出等模拟文件）
├── scripts/
│   ├── install.sh                   # 安装为 systemd 服务（部署 metrics.yaml）
│   └── gen_metrics_catalog.py       # 指标目录生成脚本（v0.3.0 新增）
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
         │   (proc/sys/ipmi/lscpu/mce/dmesg/dmidecode/statfs/smartctl
         │    + dcmi/npu_smi/hccn_tool/nvidia_smi)
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

### 1.6 来源层设计（v0.2.0 引入，v0.2.2 扩展至全 6 采集器 / 14 包）

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
| dcmi | libdcmi.so (CGo) | Temperature/Power/HbmInfo/UtilizationRate/Frequency/EccInfo/ChipInfo/DriverVersion/LlcPerf/CardList 等 22 方法 | 无 | `//go:build cgo && linux && dcmi`；`-tags dcmi` 启用，默认排除降级；进程内 CGo 无 fork/exec |
| npu_smi | npu-smi -t | Topo()/HccsBandwidth(devID) | Topo 常驻 (sync.Once) + 5s 超时 | 服务 npu；fetcher 可注入 |
| hccn_tool | hccn_tool -i -opt -g | Bandwidth(devID)/Speed(devID)/Link(devID) | per-dev:opt 30s + 失败 | 复合缓存 key 修复；服务 npu |
| nvidia_smi | nvidia-smi | Query() → []GPU(9 字段) | 无 | 指标需新鲜；fetcher 可注入；服务 gpu |

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
| gpu | nvidia_smi | utilization, memory_usage, temperature, power_draw, fan_speed, ecc_errors, clock_frequency |
| npu | dcmi, npu_smi, hccn_tool | 74 指标：utilization/memory/temperature/power/health + 电压/风扇/13路温度/频率/利用率/HBM/ECC(delta)/LLC/带宽网络 |
| chassis | ipmi | power, inlet_temp, outlet_temp, fan_speed, fan_power（与 CPU/Memory 共享 SDR 缓存） |

### 1.7 指标采集目录系统（v0.3.0 新增）

为统一管控"采哪些指标、按什么优先级、默认是否采集"，引入 `internal/metrics` 指标采集目录。

#### 设计要点

1. **MetricSpec**：每个可采指标携带 `name/cn_name/priority(High|Medium|Low)/unit/static` 元数据；`static=true` 为一次性身份规格，默认采集。
2. **Catalog**：解析后的选择状态，按 `component → name → MetricSpec` 索引；`Init(paths...)` 从候选路径加载默认目录（env `CATMONITOR_METRICS` → 配置目录 → `configs/metrics.yaml` 开发回退），无文件则空目录（默认放行全部）。
3. **模块覆盖**：模块自有 `metrics.yaml`（如 `features/health/metrics.yaml`、`features/web/metrics.yaml`）经 `LoadModuleOverride` 按 `name` 合并覆盖默认目录（模块值优先，缺省字段保留默认）。
4. **Filter（选择策略）**：`priority ∈ {High,Medium} OR static==true` 默认采集；Low 诊断指标默认不采。**目录中缺失的指标默认放行**（default-allow），避免目录漂移静默丢数据。模块覆盖可通过改写 priority 单独 opt-in/out。
5. **DI 注入**：`scheduler.SetFilter(catalog.Filter)` 由 `cmd/catmonitor` 启动时装配；`interval` 本期仅记录、不接 ticker（采集仍 per-collector 既有节拍）。

#### 目录文件（YAML）

```yaml
components:
  - component: cpu
    interval: 3s            # 组件级 interval（记录，本期不接 ticker）
    metrics:
      - { name: usage, cn_name: CPU使用率, priority: High, unit: "%", static: false }
      - { name: model_info, cn_name: CPU型号信息, priority: Low, unit: "-", static: true }
  # ... 6 部件
```

#### 生成与部署

- `scripts/gen_metrics_catalog.py`：从采集器/指标清单生成默认 `configs/metrics.yaml`。
- `scripts/install.sh`：部署 `metrics.yaml` 到 `/etc/catmonitor/`。

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
4. **read_latency**（v0.3.1 新增）：读取 `/proc/diskstats` 第7字段（time spent reading, ms），两次采集差值除以间隔得出每秒读耗时（ms/s）。
5. **write_latency**（v0.3.1 新增）：读取 `/proc/diskstats` 第11字段（time spent writing, ms），差值除以间隔得出每秒写耗时（ms/s）。
6. **io_wait**：读取 `/proc/stat` 中 `cpu` 行第5字段（iowait），与总 CPU 时间差值计算占比。
7. **smart_status**：对每个块设备执行 `smartctl -H /dev/sdX`，解析输出中的 `PASSED`/`FAILED`。
8. **smart_temperature**：执行 `smartctl -A /dev/sdX`，解析 SMART 属性表中的 `Temperature_Celsius`。
9. **io_errors**：读取 `/proc/diskstats` 错误字段 + 搜索 `dmesg` 中 I/O error 关键词。

**设备过滤规则**：排除虚拟设备（loop/ram/dm-/md 等），只采集物理块设备。设备名匹配正则 `^(sd|nvme|vd|xvd|hba)[a-z]+[0-9]*n[0-9]+$`。

### 2.4 GPU 采集器（NVIDIA）

| 项目 | 说明 |
|------|------|
| 包路径 | `internal/collectors/gpu` |
| 数据来源 | `nvidia_smi` 来源包（`internal/source/nvidia_smi`），底层执行 `nvidia-smi`（Linux/Windows 双平台通过 os/exec 调用） |
| 外部依赖 | `nvidia-smi`（NVIDIA 驱动自带） |
| 采集方式 | 调用 `nvidia_smi.Default().Query()` 一次取回全部 GPU 的 9 字段，collector 遍历构建 7 类指标；解析逻辑下沉到来源包 |
| 平台分离 | 无需分离，`os/exec` 在双平台行为一致 |
| 可用性检测 | collector 不再门控 `Available()`，直接调用来源并处理 error（无驱动时返回空，优雅降级） |
| Mock | 测试通过 `nvidia_smi.SetMock(testdata)` 注入 |

**采集逻辑**：

来源包单次执行以下命令，一次获取所有 GPU 的全部字段：

```bash
nvidia-smi \
  --query-gpu=index,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,fan.speed,ecc.errors.uncorrected.volatile.total,clocks.gr \
  --format=csv,noheader,nounits
```

来源包按行解析输出（每行一块 GPU，字段间逗号分隔），返回 `[]GPU`；collector 遍历产出 7 类指标：
1. **utilization**：第2列，GPU 计算单元使用率（%）。
2. **memory_usage**：第3/4列计算，`memory.used / memory.total × 100`，同时输出 used/total 明细。
3. **temperature**：第5列，核心温度（°C）。
4. **power_draw**：第6列，实时功耗（W）。
5. **fan_speed**：第7列，风扇转速占百分比（%），无风扇的返回 N/A。
6. **ecc_errors**：第8列，不可纠正 ECC 错误累计数。
7. **clock_frequency**：第9列，图形时钟频率（MHz）。

> v0.2.2 迁移：采集器从内联 `exec.Command("nvidia-smi", ...)` + `parseOutput/parseCSVLine/parseFloat` 改为调用来源包 `nvidia_smi.Default().Query()`，解析逻辑迁移到来源包，行为不变（7 指标，2 GPU × 9 = 18 条）。GPU 是最后一个接入来源层的 collector。

**错误处理**：`nvidia-smi` 执行超时（5s）或返回错误时，来源返回 error，collector 记录日志并跳过本次采集，不影响下次采集。某块 GPU 的字段为 `N/A` 时，该指标值设为 -1 并在 Labels 中标注 `unavailable: true`。

### 2.5 NPU 采集器（华为昇腾）

| 项目 | 说明 |
|------|------|
| 包路径 | `internal/collectors/npu` |
| 数据来源 | `dcmi`(CGo)/`npu_smi`/`hccn_tool` 三个来源包；NPU 全部指标为 Linux 专属 |
| 外部依赖 | `libdcmi.so`（CANN，CGo，需 `-tags dcmi`）；`npu-smi`、`hccn_tool`（昇腾驱动自带，无 CGo） |
| 采集方式 | **device 并行采集**：collector 层每块 NPU 一个 goroutine，`WaitGroup` 等齐，单卡失败不影响其他卡 |
| 平台分离 | `npu_linux.go`（`//go:build linux`，实现 74 指标）+ `npu_other.go`（`//go:build !linux`，no-op stub）；Windows 上 `Collect()` 整体降级跳过 |
| 可用性检测 | `dcmi.Default().Available()` = CGo provider 是否注册（`-tags dcmi` 时为 true）；命令类来源去掉 `Available()` 门控，直接调 + 处理 error |
| Mock | 测试通过 `dcmi.SetMockProvider()`、`npu_smi.SetMock()`、`hccn_tool.SetMock()` 注入 |

**采集逻辑（device 并行）**：

```
Collect() {
  Phase 1: collectStatic(now)        // 全局/静态指标采 1 次：npu_num/comm_topo/driver_version/chip_type
  Phase 2: for each deviceID {
    go collectDevice(devID, now)     // 每 device 一个 goroutine，采全部 74 指标
  }
  wg.Wait()                          // 等齐，合并结果
}
```

- 并行在 collector 层（来源层保持单 device 接口，简单可测）；ECC delta 用 mutex 保护 `prevEcc` map。
- 既有 5 个指标改走 DCMI：utilization(`dcmi_get_device_utilization_rate`)、memory_usage(`dcmi_get_device_hbm_info`)、temperature(`dcmi_get_device_temperature`)、power_draw(`dcmi_get_device_power_info`)、health_status(`dcmi_get_device_health`)。

**74 指标分布**：

| 组 | 指标数 | 来源 |
|----|:------:|------|
| 既有 5（改 DCMI） | 5 | dcmi |
| 基础信息 | 8 | dcmi + npu_smi(-t topo) |
| 电压/风扇 | 7 | dcmi(DeviceInfo LP) |
| 温度(13 路) | 13 | dcmi(SensorInfo) |
| 频率(7) | 7 | dcmi(Frequency/AicpuInfo) |
| 利用率(12) | 12 | dcmi(UtilizationRate/DvppRatio) |
| HBM 内存 | 2 | dcmi(HbmInfo) |
| ECC(8) | 8 | dcmi(EccInfo, delta) |
| LLC(3) | 3 | dcmi(LlcPerf) |
| 带宽/网络(9) | 9 | hccn_tool + npu_smi(-t hccs-bw) + dcmi(NetworkHealth) |
| **合计** | **74** | |

**错误处理**：`-tags dcmi` 未启用时（无 CANN SDK），DCMI `Available()=false`，所有 DCMI 方法返回 `errNotAvailable`，`Collect()` 不报错、仅输出非 DCMI 指标（优雅降级）。`npu_smi`/`hccn_tool` 命令执行超时（5s）或缺失时返回 error，静默跳过。DCMI 原始单位（mV/V、毫摄氏度/°C、hit_rate 等）待真机实测。


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

### 2.7 Chassis 采集器（v0.3.1 新增）

| 项目 | 说明 |
|------|------|
| 包路径 | `internal/collectors/chassis` |
| 数据来源 | `ipmitool sdr`（经 `internal/source/ipmi` 来源包，与 CPU/Memory 共享 30s SDR 缓存 + 失败缓存） |
| 外部依赖 | `ipmitool` + BMC 访问权限 |
| 采集方式 | 遍历 SDR 传感器列表，按名称关键词匹配分类（inlet/outlet/fan/power），无 BMC 时优雅降级返回空 |
| 平台分离 | Linux 专有（依赖 ipmitool + BMC），Windows 无 BMC 不采集 |
| 指标数 | 5（High 2 / Medium 3 / Low 0） |

**采集逻辑**：
1. **power**（High）：整机功耗（W），匹配 SDR 名称 `"power"` 或不含 CPU/MEM/NPU/FAN 的 power 传感器。
2. **inlet_temp**（High）：进风口温度（°C），匹配名称含 `"inlet"` + `"temp"`。
3. **outlet_temp**（Medium）：出风口温度（°C），匹配名称含 `"outlet"` + `"temp"`。
4. **fan_speed**（Medium）：风扇转速（RPM），匹配名称含 `"fan"` + `"speed"`，Labels 含 `fan` 编号 + `direction`（F/R）。
5. **fan_power**（Medium）：风扇功率（W），匹配名称含 `"fan"` + `"power"`，Labels 含 `fan` 编号。

> **设计要点**：Chassis 是第一个不绑定具体硬件部件（CPU/Memory/Disk/GPU/NPU/Network）的采集器，覆盖 BMC 管理的机箱级环境传感器。与 CPU/Memory 共用同一份 SDR 缓存（ipmi 来源包 30s TTL），无额外 exec 开销。`power` 匹配排除 CPU/MEM/NPU/FAN 的功率传感器，避免误匹配。

---

## 3. 健康度评估模块设计

> v0.3.0 健康度模块从 `internal/health` 抽取至特性层 `features/health`：消费 `collector.Metric`，不做底层采集；规则对齐 `indi_list` 的 High/Medium 指标。规则与扣分阈值详见 [`features/health/HEALTH_SPEC.md`](features/health/HEALTH_SPEC.md)。

### 3.1 设计原则

- **特性层模块**：`features/health` 包，仅消费 `collector.Metric`，不依赖任何采集器实现
- **按部件评估器**：每个部件一个评估器文件（cpu/memory/disk/gpu/npu），规则就近定义，修改规则不影响采集逻辑
- **局部 scheme**：`Evaluate` 使用局部权重方案、不改写 receiver；权重自适应——根据服务器是否含 GPU/NPU 自动选择
- **规则对齐指标清单**：扣分触发项对应 High/Medium 指标（CPU MCE、内存 saturation/fragmentation、硬盘 smart_status、GPU utilization、NPU utilization/ECC/error_code 等；温度取子温度最差值）

### 3.2 模块结构

```
features/health/
├── health.go          # Evaluate() 入口：分组 metrics → 选 scheme → 按部件评估 → 汇总
├── scheme.go          # 权重方案（CPUOnlyScheme / AcceleratedScheme）
├── cpu.go             # CPU 评估器 + 扣分规则
├── memory.go          # Memory 评估器（含 saturation/fragmentation）
├── disk.go            # Disk 评估器（含 smart_status）
├── gpu.go             # GPU 评估器（含 utilization）
├── npu.go             # NPU 评估器（含 utilization/ECC/error_code）
├── util.go            # 公共工具（取最差子温度等）
├── metrics.yaml       # health 自有指标目录（启动时优先读取覆盖默认）
├── HEALTH_SPEC.md     # 规则与扣分阈值规格
└── *_test.go          # 表驱动测试
```

**工作流程**：
1. 接收最近一轮所有采集器输出的 `[]Metric`，按 component 分组
2. 根据是否存在 GPU/NPU 指标自动选择权重方案（局部 scheme）
3. 逐部件调用评估器，匹配扣分规则，计算各部件扣分（多卡取最差卡）
4. 各部件满额分减去扣分得部件得分，汇总为总分
5. 按总分映射健康等级（Excellent/Good/Warning/Critical）
6. 输出 `HealthScore` 结构体（含总分、等级、各部件明细、扣分详情）

### 3.3 权重自适应判定逻辑

`Evaluate()` 在分组 metrics 后自动检测：
- 如果存在 GPU 指标（`byComponent["gpu"]` 非空），切换到加速卡方案（CPU:10, Mem:20, Disk:10, GPU/NPU:60）
- 如果存在 NPU 指标，同上
- 否则使用默认 CPU-only 方案（CPU:30, Mem:40, Disk:30）

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

## 6. Web 仪表盘设计

> 详细规格见 [`features/web/Web_SPEC.md`](features/web/Web_SPEC.md)。本节描述架构、数据流与扩展机制。v0.3.0 由 `web/` 迁入 `features/web/`。

### 6.1 模块定位与解耦

`features/web/` 是与主项目同一 Go module 的独立二进制 `catmonitor-web`，**不新增 go.mod、不改主项目任何文件**。与 `cmd/catmonitor`、`internal/collectors`、`features/health`、`internal/storage`、`internal/config`、`internal/platform` 解耦，仅通过只读复用（blank import + 调用注册表/健康度接口）获取数据。

### 6.2 目录结构

```
features/web/
├── main.go            # 入口：blank-import 采集器 + 起采集 goroutine + HTTP server + 信号处理 + 端口回退
├── static.go          # //go:embed static，内嵌前端资源
├── config.go          # 配置结构 + YAML 加载 + runtime.json 运行时覆盖
├── collector.go       # DataCollector：定时采集 → 健康度 → 原子写 snapshot + 环形历史 + 热重载 + 静态 specs stash
├── snapshot.go        # Snapshot 结构（含 Specs 字段）+ 原子读写
├── hwinfo.go          # 一次性硬件身份采集（device_model/gpu_info/npu_info/disk_info/net_info/os_info），非注册采集器
├── server.go          # HTTP 路由与处理函数
├── config.yaml        # 默认配置
├── metrics.yaml       # web 自有指标目录（启动时优先读取覆盖默认）
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
| `health` | `health.HealthScore` | 健康度结果（直接复用 `features/health` 序列化） |
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

- **路由**（`server.go`）：`GET /`（SPA 外壳）、`GET /static/{file}`、`GET /api/snapshot`、`GET /api/collectors`、`GET|POST /api/config`、`POST /api/refresh`、`/dfee/`（能效监控，见 §7）。详见 SPEC §9.7。
- **前端**（`static/`）：SPA + hash 路由（`#/` 概览，`#/<component>` 详情）。概览页含健康度面板 + 设备规格面板（点击弹出完整规格 modal）+ 部件芯片 + 概览卡网格；详情页含趋势面板（自动列出 `<component>_*` 历史 sparkline）+ 全部指标表。导航栏含「能效分析」入口（v0.3.1，跳转 `/dfee/`）。
- **显示 manifest**（`app.js`）：`MANIFEST`（部件显示名/关键指标）、`SERIES_LABELS`（序列显示名）、`NAV_ORDER`（导航排序）、`SPEC_DEFS`/`LABEL_NAMES`（规格面板）。未登记部件/指标/序列均有通用回退，不会崩溃。

### 6.9 扩展机制

| 扩展需求 | 改动位置 | 自动部分 |
|----------|----------|----------|
| 新部件采集器 | `features/web/main.go`（blank import） | 导航/概览卡/详情页 |
| 部件显示名/关键指标 | `features/web/static/app.js` MANIFEST | — |
| 新趋势 sparkline | `features/web/collector.go` trackedSeries 加一行 | 详情页趋势面板 |
| 趋势显示名 | `features/web/static/app.js` SERIES_LABELS | — |
| 新静态身份指标（采集器侧） | 加入 `staticMetricNames` 即被 stash 进 `specs` | specs modal 自动渲染 |

> **结论：一行 blank import 即可让新部件完整可用**；后续按需在 MANIFEST/trackedSeries 美化。`health` 与 `metrics` 字段直接复用主项目结构体，采集器新增任何字段/标签都原样透传到前端。

### 6.10 已知限制与后续预留

1. **单机本地视图**：不含认证、不含多机聚合；如需多机，预留"多个 snapshot 源 + 概览聚合"。
2. **轮询而非推送**：前端 `setInterval` 轮询 `/api/snapshot`；如需实时推送，预留 WebSocket/SSE（`snapshot.json` 解耦边界可直接复用）。
3. **无持久化历史存储**：历史仅存内存环形缓冲（重启清空），未落盘；如需长期趋势，预留 JSONL 落盘。
4. **指标展示优先级**：当前 metric 不携带优先级字段，概览关键指标靠 MANIFEST 人工指定；未来若主项目 Metric 增加优先级可改为自动选取。

---

## 7. 能效监控模块设计（features/dfee，v0.3.1 新增）

> 详细规格见 [`features/dfee/dfee_SPEC.md`](features/dfee/dfee_SPEC.md)。本节描述架构、数据流与扩展机制。

### 7.1 模块定位与解耦

`features/dfee/` 是与 `features/web` 同级的独立 Go package，**不修改现有 web 业务代码**（唯一改动：`features/web/server.go` 加 1 行 `dfee.Register(mux, ...)` 路由注册 + `features/web/main.go` 加 dfee metrics override 加载 + `features/web/static/app.js` 加 1 个导航入口）。dfee 从 `snapshot.json` 过滤能效指标，独立渲染 25 张实时图表。

### 7.2 目录结构

```
features/dfee/
├── dfee_SPEC.md               # 设计规格文档
├── energy_efficiency_metrics.md # 74 项能效指标清单
├── filter.go                  # 能效指标过滤集 + filterEfficiency() + 分组定义
├── cpu_derive.go              # CPU 8 jiffies → 7 利用率推导（有状态）
├── net_derive.go              # 网络差值计算
├── handler.go                 # HTTP handler：组装 /api/dfee 响应 + 静态文件
├── embed.go                   # //go:embed static
├── metrics.yaml               # dfee 指标目录覆盖（CPU 8 个 Low → Medium）
├── static/
│   ├── index.html             # 能效监控 SPA 页面
│   ├── dfee.js                # 25 张实时图表渲染 + 轮询
│   └── dfee.css               # 样式
└── *_test.go                  # 过滤/推导/HTTP 测试
```

### 7.3 数据流与解耦边界

```
采集层 (7 collectors, 不变)
  → DataCollector.collectOnce() (不变)
  → snapshot.json (不变, 159 指标)
        │
        ├──────────────────────────────────────────┐
        ↓                                          ↓
  GET /api/snapshot (现有, 不变)          GET /api/dfee (dfee 新增)
  → 全量 159 指标                        → 过滤 74 能效指标
  → 前端 SPA 概览/详情页                   → CPU 8 jiffies → 7 利用率推导
                                         → 按小节分组 → 25 张图表数据
                                         → 前端 Canvas 实时折线图
```

**解耦边界**：dfee 只读 `snapshot.json`（与 web HTTP 层同一数据源），**绝不直接调用采集器**。CPU 利用率推导（8 jiffies → 7 utilization%）在 dfee 后端有状态完成（`cpu_derive.go` 维护 prev 快照做 delta），前端只收成品百分比。

### 7.4 74 项能效指标来源

| 来源部件 | 指标数 | 典型指标 |
|----------|--------|----------|
| NPU | 46 | 频率(7)/利用率(12)/温度(13)/电压(5)/ECC(8)/带宽网络(9)/HBM(2) |
| CPU | 10 | 利用率推导(7) + 时间原始(8→推导消耗) + 温度/power/MCE |
| Memory | 7 | usage/swap/saturation/fragmentation/power/ecc |
| Disk | 4 | space_usage/iops/throughput/io_wait + read/write_latency |
| Network | 2 | throughput/packet_count |
| Chassis | 5 | power/inlet_temp/outlet_temp/fan_speed/fan_power |

> 完整清单见 `features/dfee/energy_efficiency_metrics.md`。

### 7.5 指标目录覆盖

dfee 需要 8 个 CPU 时间原始指标（`user_time`/`nice_time`/`system_time`/`idle_time`/`iowait_time`/`irq_time`/`softirq_time`/`steal_time`）做利用率推导，但这 8 个在默认目录中为 Low（默认不采集）。`features/dfee/metrics.yaml` 将它们覆盖为 Medium，由 `features/web/main.go` 启动时经 `metrics.LoadModuleOverride` 加载，使它们通过 Filter 进入 snapshot.json。同时覆盖若干 NPU Low 指标（acg_count/ntc*_temp/aicore_rated_freq/v*_util/llc_*）为 Medium 以供能效图表展示。

### 7.6 扩展机制

| 扩展需求 | 改动位置 |
|----------|----------|
| 新增能效指标图表 | `features/dfee/filter.go` 分组定义加条目 + `dfee.js` 加图表 |
| 新增 CPU 推导指标 | `features/dfee/cpu_derive.go` 加推导逻辑 |
| 新增能效指标来源 | （采集器侧新增指标 + `features/dfee/metrics.yaml` 覆盖优先级） |
| 导航入口 | `features/web/static/app.js` renderNav（已有） |
