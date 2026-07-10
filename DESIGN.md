# CATMonitor 设计文档 (DESIGN)

> 本文档描述 CATMonitor 的架构设计、模块设计、采集器设计和命令行设计。
> 规格与需求见 [SPEC.md](SPEC.md)，指标清单见 [CATMonitor_indi_list.md](docs/CATMonitor_indi_list.md)。

---

## 1. 架构设计

### 1.1 分层架构

```
┌─────────────────────────────────────────────────────┐
│                    cmd/catmonitor                    │
│                   (守护进程入口)                       │
├─────────────────────────────────────────────────────┤
│  internal/config        internal/storage              │
│  (配置管理)              (JSON文件写入)                │
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
│  ┌─────┐ ┌────────┐ ┌──────┐ ┌─────┐ ┌─────┐ ┌──────┐│
│  │ CPU │ │ Memory │ │ Disk │ │ GPU │ │ NPU │ │ Network││
│  └─────┘ └────────┘ └──────┘ └─────┘ └─────┘ └──────┘│
└─────────────────────────────────────────────────────┘
```

### 1.2 扩展机制：Collector 接口 + Registry 注册表

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

### 1.3 目录结构

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
│   ├── collectors/                   # 具体采集器实现（每个部件一个包）
│   │   ├── cpu/
│   │   │   ├── cpu.go               # CPU 采集器
│   │   │   └── cpu_test.go
│   │   ├── memory/
│   │   │   ├── memory.go
│   │   │   └── memory_test.go
│   │   ├── disk/
│   │   │   ├── disk.go
│   │   │   └── disk_test.go
│   │   ├── gpu/
│   │   │   ├── gpu.go               # 通过 nvidia-smi 采集
│   │   │   └── gpu_test.go
│   │   ├── npu/
│   │   │   ├── npu.go               # 通过 npu-smi 采集（华为昇腾）
│   │   │   └── npu_test.go
│   │   └── network/
│   │       ├── network.go
│   │       └── network_test.go
│   ├── health/                      # 健康度评估模块（独立）
│   │   ├── health.go                # HealthEvaluator 接口
│   │   ├── rules.go                 # 评分规则定义（权重、扣分项）
│   │   ├── scorer.go                # 计分器实现
│   │   └── health_test.go
│   ├── config/                      # 配置管理
│   │   ├── config.go                # 配置结构体 + 加载逻辑
│   │   └── config_test.go
│   └── storage/                     # 数据存储
│       ├── storage.go               # JSON 文件写入器
│       └── storage_test.go
├── configs/
│   └── catmonitor.yaml              # 默认配置文件
├── docs/
│   └── CATMonitor_indi_list.md      # 指标清单文档
├── tests/
│   ├── framework.go                 # 测试框架（通用断言、Mock工具）
│   ├── integration_test.go          # 集成测试
│   └── testdata/                    # 测试数据（/proc、/sys 模拟文件）
├── scripts/
│   └── install.sh                   # 安装为 systemd 服务
├── go.mod
├── go.sum
└── Makefile
```

### 1.4 数据流与数据格式

```
  Scheduler 按各自周期触发
         │
         ▼
  Collector.Collect()  ──→  []Metric
         │
         ▼
  Storage.Write(metrics)  ──→  JSON 文件
  (路径: /var/lib/catmonitor/data/{component}_{date}.jsonl)
         │
         ▼
  HealthEvaluator.Evaluate(latestMetrics)  ──→  HealthScore
         │
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

---

## 2. 采集器详细设计

每个采集器是一个独立的 Go 包，位于 `internal/collectors/{component}/`。采集器启动时自动检测目标硬件/工具是否可用，不可用则自动跳过，不影响其他采集器运行。

### 2.1 CPU 采集器

| 项目 | 说明 |
|------|------|
| 包路径 | `internal/collectors/cpu` |
| 数据来源 | `/proc/stat`、`/proc/loadavg`、`/sys/class/thermal/`、`/sys/devices/system/cpu/cpu*/cpufreq/`、`/proc/cpuinfo` |
| 外部依赖 | 无（全部使用内核虚拟文件系统） |
| 采集方式 | 直接读取文件 + 解析，CPU 使用率需维护上一次采集的快照计算差值 |

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
| 数据来源 | `/proc/meminfo`、`/sys/devices/system/edac/mc/`、`/proc/vmstat` |
| 外部依赖 | `dmesg` 或 `journalctl`（仅 OOM 指标） |
| 采集方式 | 文件读取 + 解析，ECC 错误需读取 EDAC 框架文件 |

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
| 数据来源 | `/proc/mounts`、`statfs` 系统调用、`/proc/diskstats`、`/proc/stat` |
| 外部依赖 | `smartctl`（仅 SMART 指标，Phase 3） |
| 采集方式 | 系统调用 + 文件解析，IOPS/吞吐量需差值计算 |

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
| 数据来源 | `nvidia-smi` 命令输出 |
| 外部依赖 | `nvidia-smi`（NVIDIA 驱动自带） |
| 采集方式 | 单次 `nvidia-smi --query-gpu=...` 批量查询，解析 CSV 输出 |
| 可用性检测 | 启动时执行 `which nvidia-smi`，不可用则跳过该采集器 |

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
| 数据来源 | `npu-smi info` 命令输出 |
| 外部依赖 | `npu-smi`（昇腾驱动自带） |
| 采集方式 | 执行 `npu-smi info`，解析表格格式输出 |
| 可用性检测 | 启动时执行 `which npu-smi`，不可用则跳过该采集器 |

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
| 数据来源 | `/proc/net/dev`、`/sys/class/net/`、`/proc/net/tcp`、`/proc/net/tcp6` |
| 外部依赖 | 无 |
| 采集方式 | 文件读取 + 解析，吞吐量/包数需差值计算 |

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

检测系统中是否存在 GPU/NPU 设备（`nvidia-smi` / `npu-smi` 是否可用），有则使用加速卡方案，无则使用 CPU-only 方案。4卡与8卡暂使用同一权重，后续可差异化。

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
| `--config` | `-c` | `/etc/catmonitor/catmonitor.yaml` | 配置文件路径 |
| `--data-dir` | `-d` | `/var/lib/catmonitor/data` | 数据输出目录 |
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
│ cpu      │ cpu      │ High     │ 3s       │ true    │ 7       │
│ memory   │ memory   │ High     │ 3s       │ true    │ 6       │
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
