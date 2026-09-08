# CATMonitor Web 技术规格说明书 (Web_SPEC)

> **文档定位**：本文档是 CATMonitor Web 仪表盘的**唯一设计与规格文档**，描述当前实现的真实状态，并明确为未来"新增部件 / 新增采集指标"预留的扩展点。后续开发以本文档为准。
>
> **对应代码**：`features/web/` 目录（与主项目同一 Go module，不新增 go.mod）。
> **只读消费者**：Web 是独立二进制 `catmonitor-web`，自身**不采集**、不加载指标目录、不写任何文件。Monitoring 数据只读 daemon 写出的快照文件（`features/snapshot` 结构）；Stress 操作经 unix control socket 代理给 daemon Controller（`features/stress` ControlClient）。daemon（`cmd/catmonitor`）是唯一快照生产者。

---

## 1. 概述

### 1.1 目标

提供一个 Web 仪表盘，可视化单台服务器的健康度与各部件采集指标，并代理 Stress 压测操作。设计原则：

1. **解耦**：Web 与采集守护进程完全解耦——只读消费快照文件，不修改任何主项目行为，不重复实现采集/健康度。
2. **多页面**：概览页（整体健康度 + 各部件关键指标）+ 各部件详情页（详细指标 + 趋势）+ Stress 页。
3. **可扩展**：新增部件类型 / 采集指标时（daemon 侧），Web 页面尽可能自动出现，零代码或仅需一处一行的新增。
4. **极简依赖**：Go 标准库，前端原生 HTML/CSS/JS，无构建步骤，零新依赖。

### 1.2 架构总览

单一 Go 二进制 `catmonitor-web`，是**只读快照消费者 + Stress 代理**，不含任何采集 goroutine：

```
┌──────────── catmonitor daemon (cmd/catmonitor) ────────────┐
│  采集调度 → metrics.Filter → 存储链                            │
│    ├─ PerCompWriter：每批次原子写 snapshot_<comp>.json        │
│    │   （该部件 metrics + 历史环形缓冲 + specs）               │
│    └─ GlobalWriter：全局节奏原子写 snapshot.json               │
│        （健康度(全量并集) + collectors 元数据 + system_specs） │
│  Stress Controller：unix socket /run/catmonitor/control.sock │
└──────────────────────────────────────────────────────────────┘
              │ snapshot 文件（只读）        │ control socket（代理）
┌──────────── catmonitor-web (单二进制, :19322) ───────────────┐
│  HTTP server (net/http)                                       │
│    Monitoring：读 -snapshot-dir 下的 snapshot.json            │
│      + snapshot_<comp>.json，按请求聚合为 /api/snapshot        │
│    Stress：WebHandler 策略代理（不拥有 Manager）               │
│  前端 SPA：//go:embed static 内嵌                             │
└──────────────────────────────────────────────────────────────┘
              ↑ fetch /api/snapshot (setInterval) / Stress API
        浏览器（SPA：概览 + 各部件详情页 + Stress 页）
```

**解耦边界**：HTTP 层**只读** daemon 写出的快照文件（`snapshot.json` + `snapshot_<comp>.json`），**绝不直接调用采集器**；daemon 是快照文件的**唯一写者**（写临时文件 + `os.Rename` 原子写，读者永不会读到半截文件）。

**指标筛选**：发生在 daemon 侧（`metrics.Init` 目录 + `features: [web]` 等特性作用域 + `metrics.Filter`），Web 拿到的快照已是筛选结果，自身不做任何目录加载或过滤。

### 1.3 技术栈

| 项目 | 选型 |
|------|------|
| 后端语言 | Go 1.23.4（沿用主项目 go.mod） |
| HTTP | Go 标准库 `net/http` |
| 依赖 | 仅 Go 标准库（Web 自身不解析 YAML；`gopkg.in/yaml.v3` 为主 module 既有依赖） |
| 前端 | 原生 HTML5 + CSS + 原生 JS（ES2015+），无框架、无构建步骤 |
| 前端打包 | `//go:embed static` 内嵌进二进制，单文件部署 |
| 图表 | 手写内联 SVG sparkline，无图表库 |
| 进程托管 | systemd 临时 unit（可选），支持信号优雅退出 |

---

## 2. 目录结构

```
features/web/
├── main.go            # 入口：flag 解析 + ControlClient 构造 + listenWithFallback + HTTP server + 信号处理
├── server.go          # Monitoring 路由（/、/static、/api/snapshot、/api/collectors、/api/config）+ 快照聚合 + Stress 路由挂载
├── static.go          # //go:embed static，内嵌前端资源
├── metrics.yaml       # 特性作用域文件：catmonitor.yaml features 列表含 web 时由 daemon 加载（Web 二进制自身不读取）
├── static/
│   ├── index.html     # SPA 外壳（顶栏 + nav + #page 容器）
│   ├── style.css      # 浅色卡片式主题
│   └── app.js         # SPA 路由 + 概览页 + 部件详情页 + 扩展 manifest
├── data/              # 旧版自采集时期的运行时残留（如 ipmi_sensor_map.json 缓存；已被根 .gitignore 忽略，当前代码不读写）
├── http_linux_test.go                     # linux 单测：无 Stress client 时 Monitoring 路由仍可用
├── monitoring_compatibility_linux_test.go # linux 单测：旧 flag 兼容 + 无 control socket 的降级行为
└── stress_mount_linux_test.go             # linux 单测：统一 Stress 视图挂载（control socket fixture）
```

> 历史版本中的 `config.go`、`collector.go`、`snapshot.go`、`hwinfo.go`、`config.yaml` 已随只读化改造移除：采集/快照/硬件身份逻辑移至 `features/snapshot`（由 daemon 执行），Web 仅保留 HTTP 服务与前端。快照结构定义与读取函数见 `features/snapshot/`（`snapshot.go`、`global.go`、`comp.go`、`read.go`）。

---

## 3. 启动参数与数据源

### 3.1 命令行参数

| 参数 | 默认值 | 作用 |
|---|---|---|
| `-addr` | `:19322` | Monitoring 与 Stress API 的统一 listener；端口占用时自动递增 |
| `-snapshot-dir` | `/var/lib/catmonitor/snapshot` | daemon 生成的只读 snapshot 目录 |
| `-control-socket` | `/run/catmonitor/control.sock` | 可选的 daemon Stress 控制 socket |
| `-config` | 空 | deprecated no-op，仅保留旧 `docker run` 命令兼容 |

### 3.2 数据边界

Web 不加载 CATMonitor YAML、不采集指标、不写 runtime 配置。Monitoring 数据只来自
`-snapshot-dir`；Stress config/report/history/job/run/cancel 只通过
`-control-socket` 代理给 daemon Controller。

### 3.3 Monitoring 兼容性

control socket 未配置或不存在时，Web 仍必须正常启动，首页与 snapshot API 正常工作。
`GET /api/stress/config` 返回 HTTP 200，并明确给出 `enabled=false`、
`available=false`；Stress Run/Cancel 等写操作仍返回 `503 Service Unavailable`。
旧命令中的 `-config=<path>` 可以继续传入，
但不会读取或校验该文件。

> 端口占用回退：启动时 `net.Listen` 探测 `-addr`，若返回 `EADDRINUSE` 则端口 +1
> 重试，直至获取可用端口；非 `EADDRINUSE` 错误直接失败退出。
---

## 4. 数据模型

### 4.1 快照文件与 `/api/snapshot` 聚合视图

daemon（`snapshot.enabled: true` 时）在 `-snapshot-dir` 写两类文件；Web 按请求聚合：

**全局快照 `snapshot.json`**（`features/snapshot.GlobalSnapshot`，GlobalWriter 以全局节奏 C_global 原子写）：

```json
{
  "session_id": "1757431277",
  "timestamp": "2026-09-08T14:47:55+08:00",
  "refresh_interval_ms": 5000,
  "intervals_ms": {"cpu": 3000, "memory": 5000, "...": 0},
  "health": {
    "score": 100, "grade": "Excellent", "server_type": "cpu_only",
    "components": {
      "cpu": {"score": 25, "max": 25, "deductions": null},
      "memory": {"score": 25, "max": 25, "deductions": null},
      "disk": {"score": 30, "max": 30, "deductions": null}
    }
  },
  "collectors": [
    {"name":"cpu","component":"cpu","priority":"High","interval":"3s","enabled":true}
  ],
  "system_specs": [
    {"component":"system","name":"device_model","value":1,"labels":{"manufacturer":"...","product_name":"..."},"timestamp":"..."}
  ]
}
```

> `health` 由 daemon 在**全量指标并集**上评估（auto 方案检测因此正确）；`intervals_ms` 为各部件采集节奏；`system_specs` 为启动期一次性系统身份（device_model / os_info）。

**部件快照 `snapshot_<comp>.json`**（`features/snapshot.CompSnapshot`，PerCompWriter 在该部件每个采集批次后原子写）：`component` / `timestamp` / `metrics`（该部件本次全部指标）/ `history`（该部件趋势环形缓冲）/ `specs`（`omitempty`，stash 的 CPU/内存静态指标 + 启动期硬件身份）。

**`/api/snapshot` 聚合视图**（`features/snapshot.Snapshot`，Web 按请求组装，`server.go handleSnapshot`）：

```json
{
  "session_id": "1757431277",
  "timestamp": "2026-09-08T14:47:56+08:00",
  "refresh_interval_ms": 5000,
  "history_points": 60,
  "health": { "...同全局快照..." },
  "metrics": [
    {"component":"cpu","name":"usage","value":12.3,"unit":"%","labels":{"core":"total"},"timestamp":"..."}
  ],
  "history": {
    "cpu_usage": [12.3, 13.1],
    "memory_usage": [29.9, 30.1],
    "disk_space_usage": [23.4],
    "cpu_load_average": [1.41],
    "memory_swap_usage": [0.0]
  },
  "specs": [
    {"component":"system","name":"device_model","value":1,"labels":{"manufacturer":"...","product_name":"..."},"timestamp":"..."},
    {"component":"cpu","name":"model_info","value":48,"unit":"cores","labels":{"model_name":"Intel(R) Xeon(R) ..."},"timestamp":"..."},
    {"component":"disk","name":"disk_info","value":476.9,"unit":"GB","labels":{"device":"sda","model":"..."},"timestamp":"..."}
  ]
}
```

| 字段 | 来源 | 说明 |
|------|------|------|
| `session_id` | 全局快照 | daemon 会话标识（Web 进程重启后不变，daemon 重启后变化；前端据此清空本地折叠状态） |
| `timestamp` | Web 组装时刻 | 本次响应生成时间 |
| `refresh_interval_ms` | 全局快照 | 全局节奏 C_global（各部件采集间隔的最小值），供前端轮询对齐 |
| `history_points` | Web 常量（60） | 历史环形缓冲容量 |
| `health` | 全局快照 | 健康度结果（直接复用 `features/health.HealthScore` 的序列化） |
| `metrics` | 各部件快照并集 | 本次采集的全部指标（复用 `internal/collector.Metric`） |
| `history` | 各部件快照并集 | 趋势序列，key 形如 `<component>_<suffix>`，供详情页按部件前缀过滤 |
| `specs` | 各部件 specs + 全局 `system_specs` | 静态设备规格（一次性身份信息），见 §5.4。`omitempty`：无任何静态指标时不出现在 JSON 中 |

> `health` 与 `metrics` 直接使用主项目的结构体 JSON tag，**不重新定义**，保证与采集器/健康度模块的契约一致。

### 4.2 原子性与读写

- **写（daemon 侧）**：`features/snapshot` 的 `WriteJSONAtomic`：`json.MarshalIndent` → 写同目录临时文件 → `os.Rename` 覆盖目标。读者只见完整文件。
- **读（Web 侧）**：`snapshot.ReadGlobal(path)` / `snapshot.ReadComp(path)`：`os.ReadFile` + `json.Unmarshal`。Web 无任何写路径。

---

## 5. 快照生产与历史趋势（daemon 侧，`features/snapshot`）

> 本章描述的生产行为全部在 daemon 内执行，Web 只是结果消费者；理解本章是为了正确扩展（新增序列/静态规格需改 daemon 侧代码）。

### 5.1 快照生产者

- **`PerCompWriter`**（`comp.go`）：`collector.Storage` 装饰器——每个采集批次先委托内层存储（JSONL/缓存/faultsub/stragglerout），再为该批次部件原子写 `snapshot_<comp>.json`。为每个部件维护独立的 `History` 环形缓冲（容量 60，超出丢最旧）与 `staticStash`（§5.4.2）。采集节奏 == 部件快照刷新节奏（文件在每个采集周期后即写）。
- **`GlobalWriter`**（`global.go`）：以全局节奏 C_global（= 各部件采集间隔的最小值）周期性从 `exporter.CachingStorage` 读取全量指标并集，用 `health.NewEvaluator(health.GetScheme("auto"))` 评估健康度，原子写 `snapshot.json`（含 collectors 元数据、intervals_ms、system_specs）。
- **启动期硬件身份分发**（`cmd/catmonitor/main.go`）：daemon 启动时调 `snapshot.CollectHWSpecs()` 一次性采集（§5.4.1），system 规格（device_model/os_info）→ `GlobalWriter.SetSystemSpecs`，部件规格（gpu_info/npu_info/disk_info/net_info）→ `PerCompWriter.SetCompSpecs`。

前置条件：`catmonitor.yaml` 的 `snapshot.enabled: true`（代码默认关闭，随附 `configs/catmonitor.yaml` 已开启）；关闭时 daemon 不写任何快照文件，Web 的 `/api/snapshot` 等接口返回 503。

### 5.2 健康度方案自动检测

daemon 的 `GlobalWriter` 传入 `"auto"` scheme，`health.Evaluate()` 按本次指标自动选择权重方案（`features/health/scheme.go`）：

- 检测到 `gpu` 指标 → `accelerated_8card`：CPU 15 / Mem 15 / Disk 15 / GPU 35 / Net 10 / Chassis 10；
- 检测到 `npu` 指标：1-2 卡 → `accelerated_2card`（CPU 20 / Mem 20 / Disk 20 / GPU 20 / Net 10 / Chassis 10）；3-4 卡 → `accelerated_4card`（CPU 15 / Mem 15 / Disk 20 / GPU 30 / Net 10 / Chassis 10）；5-8 卡 → `accelerated_8card`（同上 GPU 行）；
- 否则 `cpu_only`：CPU 25 / Mem 25 / Disk 30 / Net 10 / Chassis 10；
- 无 `chassis` 指标（无 BMC/ipmitool）时，Chassis 权重并入 CPU。

**无需 Web 侧任何配置**；Web 直接读全局快照中的健康度结果。

### 5.3 历史趋势：`TrackedSeries`（`features/snapshot/series.go`，扩展核心）

历史序列由一个**可扩展的 spec 列表**驱动——这是新增趋势 sparkline 的唯一入口：

```go
type seriesSpec struct {
    component   string
    name        string
    labelKey    string // 可选标签过滤（"" = 任意）
    labelVal    string
    labelPrefix string // 设 labelKey 时，标签值需以前缀开头
    labelSuffix string // 设 labelKey 时，标签值需以后缀结尾
    key         string // 必须为 "<component>_<suffix>"，供详情页按部件前缀过滤
    mode        int    // 0 = 取首个匹配，1 = 取所有匹配的最大值
}
```

当前已跟踪 **23 条**序列：

| key | component | name | 过滤 | mode | 说明 |
|-----|----------|------|------|------|------|
| `cpu_usage` | cpu | usage | core=total | first | CPU 总使用率 |
| `cpu_load_average` | cpu | load_average | interval=1m | first | 1 分钟负载 |
| `memory_usage` | memory | usage | — | first | 内存使用率 |
| `memory_swap_usage` | memory | swap_usage | — | first | Swap 使用率 |
| `disk_space_usage` | disk | space_usage | device 前缀 `/dev/` | max | 各物理挂载点最大使用率 |
| `gpu_utilization` | gpu | utilization | — | max | GPU 使用率（跨卡最大） |
| `gpu_memory_usage` | gpu | memory_usage | — | max | GPU 显存使用率（跨卡最大） |
| `gpu_temperature` | gpu | temperature | — | max | GPU 温度（跨卡最大） |
| `npu_utilization` | npu | utilization | — | max | NPU 使用率（跨卡最大） |
| `npu_temperature` | npu | temperature | — | max | NPU 温度（跨卡最大） |
| `npu_memory_usage` | npu | memory_usage | — | max | NPU 显存使用率（跨卡最大） |
| `npu_power_draw` | npu | power_draw | — | max | NPU 功耗（跨卡最大） |
| `cpu_temperature` | cpu | temperature | — | max | CPU 温度（ipmi SDR，跨 socket 取最大） |
| `cpu_avg_freq` | cpu | avg_freq | — | first | CPU 平均频率（/sys cpufreq） |
| `memory_swap_in` | memory | swap_in | — | first | Swap 入页（/proc/vmstat pswpin delta） |
| `memory_fragmentation` | memory | fragmentation | — | max | 内存碎片化（/proc/buddyinfo 跨 zone 取最大） |
| `disk_io_wait` | disk | io_wait | — | first | I/O Wait 占比（/proc/stat） |
| `disk_iops` | disk | iops | — | max | 磁盘 IOPS（/proc/diskstats 跨设备方向取最大） |
| `disk_throughput` | disk | throughput | — | max | 磁盘吞吐（/proc/diskstats 跨设备方向取最大） |
| `network_throughput` | network | throughput | — | max | 网络吞吐（/proc/net/dev 跨接口方向取最大） |
| `network_packet_count` | network | packet_count | — | max | 网络包速率（/proc/net/dev 跨接口方向取最大） |
| `network_error_packets` | network | error_count | type 后缀 `_err` | max | 网络错包（跨接口方向取最大） |
| `network_dropped_packets` | network | error_count | type 后缀 `_drop` | max | 网络丢包（跨接口方向取最大） |

环形缓冲：每个 key 保留最近 60 个点（`PerCompWriter` 的 historyCap），超出则丢弃最旧。`History.Update` 返回历史的拷贝写入部件快照。

> **硬件依赖**：v0.2.0 源指标（temperature/fragmentation/swap_in/iops/throughput/io_wait 等）依赖 ipmi/mce/PSI//proc 等；工具或文件缺失时该系列不产出（不报错、不零填充），仅当源产生值时历史才出现。
>
> **新增趋势的规则**：在 `features/snapshot/series.go` 的 `TrackedSeries` 末尾加一行 spec，key 遵循 `<component>_<suffix>` 命名，前端详情页会自动渲染该 sparkline。无需改前端。

### 5.4 静态设备规格（daemon 启动期 `CollectHWSpecs` + `PerCompWriter` 的 `staticStash`）

静态规格是设备的**身份信息**（型号/拓扑/序列号/容量），非时序数据，采集一次即可。daemon 侧用两条互补路径收集，Web 在 `handleSnapshot` 中合并（各部件 specs + 全局 `system_specs`）：

#### 5.4.1 启动期一次性硬件身份（`features/snapshot/hwinfo.go` `CollectHWSpecs`）

daemon 启动时调用一次，它**不是注册采集器**（不在 `collector` 注册表、不被定时循环调用），因为这些跨部件身份指标没有别的采集器产出。system 规格进全局快照 `system_specs`，其余按部件进对应 `snapshot_<comp>.json` 的 `specs`：

| metric name | component | 来源 | 说明 |
|-------------|-----------|------|------|
| `device_model` | system | `dmidecode` SMBIOS type 1 | 厂商/产品名/版本/序列号 |
| `os_info` | system | `/etc/os-release` + `uname -r`（Linux）；`cmd /c ver`（Windows） | OS PrettyName/版本号/内核 |
| `gpu_info` | gpu | `nvidia-smi --query-gpu=index,name,uuid,driver_version` | 每卡一条 |
| `npu_info` | npu | `npu-smi info` | 每卡一条（id/name/bus_id） |
| `disk_info` | disk | `/sys/block` + `smartctl`（可选富化 serial/firmware/interface） | 每真实块设备一条，value=容量 GB |
| `net_info` | network | `/sys/class/net`（跳过 lo） | 每接口一条（mac/mtu/speed/driver/pci_addr） |

> 可用性：`nvidia-smi`/`npu-smi` 不在 PATH 则对应项跳过（不报错）；`dmidecode`/`smartctl` 缺失则降级（device_model 不产出 / disk_info 缺少 serial 等富化字段）；`/etc/os-release` 缺失则 `os_info` 不产出（如最小容器）。`/sys` 始终可用。该采集随 daemon 运行（Windows 上 daemon 交叉编译后同样执行 `cmd /c ver` 分支），与 Web 进程无关。

#### 5.4.2 CPU/内存静态指标 stash（`features/snapshot` `FilterStatic` / `StaticMetricNames`）

CPU/内存采集器在启动首周期产出一次静态指标（型号/拓扑/频率范围/缓存大小/DIMM 清单），随后通过内部 flag 抑制重复产出。daemon 的 `PerCompWriter` 在每批次用 `FilterStatic` 提取这些指标，首次出现即缓存到该部件的 `staticStash`，之后每周期重新注入部件快照——否则首周期之后这些设备规格会从快照消失。

`StaticMetricNames` 集合（决定哪些指标被 stash）：

```
model_info, numa_node_num, core_num, numa_core_num, cpu_num
min_freq, max_freq, l1d_cache_size, l1i_cache_size, l2_cache_size, l3_cache_size
module_info, module_size, module_num
```

#### 5.4.3 合并写入

每个部件快照的 `Specs = staticStash + hwSpecs`（daemon `PerCompWriter.Write`）。Web 的 `/api/snapshot` 再合并：`specs = Σ 各部件 Specs + 全局 system_specs`（`server.go handleSnapshot`）。两者互不重叠：`staticStash` 是 CPU/内存静态指标（由周期采集器产出），`hwSpecs` 是启动期硬件身份（由 `hwinfo.go` 产出）。任一为空时 `specs` 仍正常拼装；全空时 JSON 因 `omitempty` 不含 `specs` 键。

---

## 6. HTTP API 规范（`server.go` + `features/stress/handler.go`）

Monitoring 路由由 `Server.Routes()` 注册；Stress 路由由 `stress.Register` 挂载（仅当 ControlClient 非 nil；`main.go` 总是构造 client——socket 路径非法才退出——故生产环境恒注册，测试注入 nil 时不注册，`/api/stress/*` 返回 404）。响应体均为 JSON（除静态资源/HTML）。

### 6.1 路由表

| 方法 | 路径 | 说明 | 成功码 | 失败码 |
|------|------|------|:------:|:------:|
| GET | `/` | 返回 `index.html`（SPA 外壳） | 200 | 500 |
| GET | `/static/{file}` | 静态资源（css/js） | 200 | 404 |
| GET | `/api/snapshot` | 聚合快照视图（§4.1） | 200 | 503 |
| GET | `/api/collectors` | 采集器元数据列表（驱动导航，来自全局快照） | 200 | 503 |
| GET | `/api/config` | Web 只读运行信息 | 200 | 503 |
| GET | `/stress/` | Stress SPA 首页（no-store） | 200 | 500 |
| GET | `/stress/static/{file}` | Stress SPA 静态资源（no-cache） | 200 | 404 |
| GET | `/api/stress/config` | Stress 能力视图（代理 daemon） | 200 | 503 |
| GET | `/api/stress/latest` | 最近一次压测报告（代理） | 200 | 503 |
| GET | `/api/stress/history` | 历史报告列表（`?limit=1..100`，默认 20；代理） | 200 | 400 / 503 |
| GET | `/api/stress/runs/{id}` | 单次运行报告（代理） | 200 | 503 |
| POST | `/api/stress/runs` | 发起压测（代理） | 202 | 400 / 403 / 415 / 503 |
| POST | `/api/stress/runs/{id}/cancel` | 取消运行（代理） | 202 | 403 / 415 / 503 |

> **不存在写操作式 Monitoring 端点**：`POST /api/config`（热更新间隔）与 `POST /api/refresh`（立即采集）已随只读化移除——采集节奏完全由 daemon 决定，Web 仅提供 GET。

### 6.2 详细契约

**GET /api/collectors** → 驱动前端导航，取自全局快照的 `collectors` 字段（daemon 启动时从 `collector.DefaultRegistry` 导出，当前 7 个采集器：chassis/cpu/disk/gpu/memory/network/npu）：
```json
[
  {"name":"cpu","component":"cpu","priority":"High","interval":"3s","enabled":true},
  {"name":"disk","component":"disk","priority":"High","interval":"5s","enabled":true}
]
```
> 顺序为注册表内排序（按 name）。**新增采集器**（daemon 侧注册）自动出现在 daemon 导出的元数据中，进而自动出现在此列表与前端导航。`system` **不在**此列表——它只是 `hwinfo.go` 产出的静态身份指标的归属部件，非注册采集器，因此不出现在导航/概览卡（前端 `orderedComponents` 显式过滤 `component === 'system'`）。

**GET /api/snapshot** → 见 §4.1。快照未就绪（daemon 首个全局快照未写出/文件不可读）返回 503 `{"error":"snapshot not ready"}`，带 `Cache-Control: no-cache`。

**GET /api/config** → Web 只读运行信息：
```json
{"version": "0.3.5", "started_at": 1757431277, "refresh_interval_ms": 5000, "history_points": 60, "stress_operator": true}
```
- `version`：与 daemon 共用的 `internal/version.Version`（前端顶栏副标题显示）；
- `started_at`：Web 进程启动时刻（unix 秒）；变化时前端清空本地指标组折叠状态（localStorage `mg:*`）；
- `refresh_interval_ms` / `history_points`：来自全局快照 / Web 常量；
- `stress_operator`：恒 true，标识 Stress 操作员能力（无 Web 操作员鉴权，见 handler 的 `security_debt_web_operator_auth`）。

**Stress 代理**（`features/stress/handler.go` WebHandler）→ 全部经 `ControlClient`（unix socket 上的 HTTP）转发给 daemon 的 Stress Controller，Web 不拥有 Manager、不触碰压测执行：

- `POST /api/stress/runs` 请求体 `{"benchmarks": [...], "timeout_seconds": N}`（`timeout_seconds >= 0`，未知字段拒绝）。守卫（任一不满足即拒绝）：仅 Linux（否则 403）；`Content-Type: application/json`（否则 415）；请求头 `X-CATMonitor-Action: stress`（否则 403）；同源校验（Origin 与 Host 不一致 → 403）。成功 → 202 Accepted + 运行报告（`cancellable=true`）。
- `POST /api/stress/runs/{id}/cancel` 同样守卫；成功 → 202 `{"ok": true}`。
- `GET /api/stress/history?limit=N`：N 需在 1..100，非法 → 400；缺省 20。
- daemon 侧错误按 `ControlAPIError` 透传状态码，其余代理失败 → 503。

**降级行为**（control socket 缺失/daemon 未启 Stress，见 §3.3）：`GET /api/stress/config` 仍返回 200，payload 明确 `enabled=false`、`available=false`、`feature_enabled=false`、`web_enabled=false`、`message: "Stress controller is not enabled"` 等；`latest` / `history` / `runs/{id}` / `run` / `cancel` 返回 503。

---

## 7. 前端设计（`static/`）

### 7.1 SPA 与路由

单页应用，hash 路由（无后端路由、无历史 API 复杂度）：
- `#/` → 概览页
- `#/<component>`（如 `#/cpu`）→ 该部件详情页
- `hashchange` 事件触发重渲染；导航高亮当前路由。

数据获取：`fetchCollectors()`（导航）→ `fetchConfigData()`（版本/启动时刻，见 §6.2）→ `startPolling()`（`setInterval` 调 `/api/snapshot`；初始 3 秒）。顶栏"刷新间隔"输入框 + "应用"按钮**仅调整前端轮询节奏**（服务端无对应 POST 端点，采集间隔由 daemon 决定）；"立即刷新"按钮重新拉取 `/api/snapshot`（服务端无立即采集端点，数据新鲜度取决于 daemon 采集节奏）；"自动"开关启停轮询。

### 7.2 概览页（`renderOverview`）

- **健康度面板**：大号总分 + 进度条 + 等级（Excellent/Good/Warning/Critical，颜色映射）+ 服务器类型 + 更新时间 + 采集间隔。
- **设备规格面板**（`renderSpecs`，hero 右上）：从 `snap.specs` 抽取核心静态身份的紧凑键值表（设备/OS/CPU/内存总量/硬盘数与总容量/网卡/GPU/NPU）。内存总量取自每周期的 `usage_detail` 指标（非 specs）。点击面板弹出完整规格 modal（`openSpecsModal`）：按 component 分组（system→cpu→memory→disk→gpu→npu→network，未知部件排末尾），每组一张"类型/标识/明细"表（`specsGroup`）。无任何 specs 时显示"无静态规格信息"。
- **部件芯片**：每个已注册部件一个彩色圆点芯片（颜色由该部件得分比决定），点击进详情。
- **部件概览卡片网格**：每卡 = 部件名 + 得分/满分 + 状态徽章 + 头条趋势 sparkline（若 manifest 指定）+ 关键指标键值表（manifest.key）。无数据时显示"无数据"徽章。点击进详情。

### 7.3 部件详情页（`renderDetail`）

- 头部：返回链接 + 部件标题 + 得分/满分 + 状态徽章 + 扣分项列表。
- **趋势面板**：自动列出所有 `<component>_*` 历史序列（`componentSeries` 按 `SERIES_ORDER` 排序），每个渲染 sparkline + 当前值。
- **全部指标面板**：按指标名分组列出该部件全部指标（组名/值/标签），覆盖该部件所有 metric 实例（如每核心、每挂载点、每卡）；磁盘/网络等方向类指标有专门的收发分组渲染（如 `renderDeviceDirectionGroup`、`renderErrorCountGroup` 错包/丢包分组）。

### 7.4 显示 manifest（`app.js`，可选提示）

```js
const MANIFEST = {
  cpu: { title: 'CPU', headline: 'cpu_usage', headlineLabel: 'CPU 使用率 (%)',
         key: [ {name:'usage',prefer:{core:'total'}}, 'load_average', 'avg_freq',
                {name:'temperature', max:true}, {name:'power', max:true} ] },
  memory: { title: '内存', headline: 'memory_usage', headlineLabel: '内存使用率 (%)',
            key: [ 'usage', 'swap_usage', {name:'fragmentation', max:true}, 'oom_count' ] },
  disk: { title: '磁盘', headline: 'disk_space_usage', headlineLabel: '挂载点空间使用率最高 (%)',
          key: [ 'space_usage', {name:'throughput', sum:true}, {name:'iops', sum:true} ] },
  gpu: { title: 'GPU', headline: 'gpu_utilization', headlineLabel: 'GPU 使用率最高 (%)',
         key: [ {name:'utilization', avg:true}, {name:'memory_usage', max:true},
                {name:'temperature', max:true}, {name:'power_draw', max:true} ] },
  npu: { title: 'NPU', headline: 'npu_utilization', headlineLabel: 'NPU 使用率最高 (%)',
         key: [ {name:'utilization', avg:true}, {name:'memory_usage', max:true},
                {name:'temperature', max:true}, {name:'power_draw', max:true} ] },
  network: { title: '网络', headline: null,
             key: [ {name:'throughput', sum:true}, {name:'packet_count', sum:true},
                    {name:'error_count', sum:true} ] },
  chassis: { title: '机箱', headline: null,
             key: [ 'power', 'inlet_temp', 'outlet_temp', {name:'fan_power', max:true} ] },
};
```

- `title`：导航与卡片显示名；未登记部件用 `key.toUpperCase()`。
- `headline` / `headlineLabel`：概览卡头条 sparkline 序列；未登记则无头条 sparkline。
- `key`：概览卡关键指标。条目支持：字符串 = 指标名取首个；`{name, prefer:{label:value}}` = 按标签精确选；`{name, max:true}` / `{name, avg:true}` / `{name, sum:true}` = 跨实例聚合取最大/平均/求和；未登记部件取前 4 条 metric。

### 7.5 其他前端常量

- `METRIC_NAMES`：指标名 → 中文显示名映射（未命中则用原始名），并支持 `<component>:<name>` 组件级覆盖（如 `chassis:power` → 整机功耗；`fan_power` → 风扇功耗）。涵盖核心指标、v0.2.0 源层指标（user_time/system_time/avg_freq/numa_*/cache_*、swap_in/fragmentation/ecc_*/oom_count/page_faults/isolated_* 等）、以及静态身份（`device_model`/`os_info`/`gpu_info`/`npu_info`/`disk_info`/`net_info`/`module_info` 等）。
- `SERIES_LABELS`：历史序列 key → 显示名（未命中则用 `key` 去前缀 + 下划线转空格）。覆盖全部 **23 条** TrackedSeries key（与 §5.3 一一对应）。
- `SERIES_ORDER`：详情页趋势面板的组内排序（未命中排 99）。
- `NAV_ORDER`：导航排序（`['cpu','memory','disk','gpu','npu','network']`，未登记部件如 chassis 排末尾按字母序）。
- `SPEC_DEFS`：静态 spec 指标名 → `{type, primary}`（类型显示名 + 持有主标识的 label key），驱动 specs 面板/modal 的"类型/标识"列。覆盖 `device_model`/`os_info`/`model_info`/`gpu_info`/`npu_info`/`disk_info`/`net_info`/`module_info`。
- `LABEL_NAMES`：label key → 中文显示名（如 `manufacturer`→厂商、`product_name`→型号、`serial`→序列号、`mac`→MAC、`pretty_name`→OS、`kernel`→内核、`chip_id`→芯片 ID 等），用于 specs modal 的"明细"列。
- `LABEL_PRIORITY`：标签显示与排序优先级：`npu_id, chip_id, gpu_id, core, cpu, node, die, zone, interface, device, mount_point, mc, locator, sensor, fan, aicore, ntc, direction, type, field, device_type, kind, interval, state, status`。同时驱动同组指标实例排序（`metricSortCmp`：`total` 优先、数值感知比较）。
- **标签去重**（详情页 `cleanLabels`）：指标值已含某标签信息时隐藏该标签，避免重复——`error_code`→`error_codes`；`roce_speed_status`→`roce_speed`；`roce_link_health`→`roce_link`；`health_status`→`status`；`*_ecc` / `*_ecc_isolated`→`device_type`/`kind`；`aicore*_temp`→`aicore`；`ntc*_temp`→`ntc`；`*_tx_bandwidth` / `*_rx_bandwidth`→`direction`。

### 7.6 状态色映射

`statusOf(score, max)`：比率 ≥0.9 OK(绿) / ≥0.75 Good / ≥0.6 Warning(橙) / 否则 Critical(红)。`gradeColor(grade)` 同色系。无 max 时 N/A(灰)。

---

## 8. 部署与运行

### 8.1 构建

```bash
make web    # 仓库根 Makefile 目标（Makefile:29-31）→ bin/catmonitor-web
# 或
go build -o bin/catmonitor-web ./features/web
     # bin/ 已被根 .gitignore 覆盖
```
Windows：`GOOS=windows go build -o bin/catmonitor-web.exe ./features/web`（纯 Go、无 CGo，可交叉编译；Web 不依赖任何 Linux-only 采集路径）。

**运行前提**：daemon 以 `snapshot.enabled: true` 运行，且其 `snapshot.dir` 与 Web 的 `-snapshot-dir` 指向同一目录（代码默认 `/var/lib/catmonitor/snapshot`）；Stress 代理需要 daemon 的 control socket 存在（缺失仅降级，不影响 Monitoring）。

### 8.2 运行

```bash
./bin/catmonitor-web -addr=:19322 -snapshot-dir=/var/lib/catmonitor/snapshot
# 浏览器打开 http://localhost:19322（实际端口见启动日志 "web server starting" addr=...）
```
`-control-socket` 默认 `/run/catmonitor/control.sock`。socket 缺失不影响 Monitoring；
Stress 配置查询返回禁用能力视图，Run/Cancel 返回 503。旧 `-config=<path>` 仍可保留，
但不会读取该文件。

### 8.3 systemd 常驻（推荐）

```bash
systemd-run --unit=catmonitor-web \
  --working-directory=<repo-root> \
  <repo-root>/bin/catmonitor-web -addr=:19322 -snapshot-dir=/var/lib/catmonitor/snapshot

systemctl status catmonitor-web
journalctl -u catmonitor-web -f
systemctl restart catmonitor-web   # 重启并重新读取命令行参数
systemctl stop catmonitor-web
```

### 8.4 优雅退出

捕获 `SIGINT`/`SIGTERM` → `http.Server.Shutdown`（5s 超时）。Web 不拥有采集循环。

### 8.5 端口占用回退（`main.go` `listenWithFallback`）

启动 HTTP 前先以 `net.Listen("tcp", addr)` 探测端口，避免 `ListenAndServe` 在 goroutine 中异步失败导致难以定位：

1. 解析 `-addr` 的 host/port（`net.SplitHostPort`）；不可解析则直接 listen 原值（不做回退）。
2. 循环 `net.Listen`：成功 → 返回 listener；失败且 `errors.Is(err, syscall.EADDRINUSE)` → 端口 +1（`net.JoinHostPort` 重组地址）打印 warn 日志后重试。
3. 其他错误（权限不足、地址非法等）直接返回，启动失败退出（`os.Exit(1)`）。
4. 成功获取的 listener 交给 `http.Server.Serve(ln)`（不再用 `ListenAndServe`），实际绑定地址传入 `NewServer`（供 `/api/stress/config` 的 loopback 判定），启动日志打印最终 `addr`，便于确认浏览器应访问的端口。

> 该回退仅针对端口占用（`EADDRINUSE`），跨平台有效（`syscall.EADDRINUSE` 在 Linux/Windows 均定义）。

---

## 9. 扩展性设计（新增部件 / 新增指标）

> 这是本规格的重点。设计目标是：**新增采集器/指标时，Web 侧尽量零改动**；改动集中在 daemon 侧（主项目既定流程）或 `app.js` 的一处可选美化。

### 9.1 场景 A：新增一个部件类型（如 FPGA 采集器）

前置：按主项目 `AGENTS.md` 在 `internal/collectors/fpga/` 实现并 `init()` 注册，daemon blank-import（**主项目既定流程，不在本规格范围**）。

| 步骤 | 是否必须 | 效果 |
|------|:--------:|------|
| 在 `cmd/catmonitor/main.go` 加 blank import `_ ".../internal/collectors/fpga"` | 必须（daemon 侧） | 采集器被注册；`/api/collectors`（经全局快照）自动含 fpga |
| Web 侧改动 | **无** | Web 不 import 采集器 |
| 前端导航 | **自动** | 出现 FPGA 导航项与概览芯片 |
| 概览卡片 | **自动** | 出现 FPGA 概览卡（通用：取前 4 条指标，无头条 sparkline） |
| 详情页 `#/fpga` | **自动** | 列出 fpga 全部指标；若有 `<component>_*` 历史序列则渲染趋势 |
| 概览卡显示名/关键指标 | 可选 | 在 `app.js` 的 `MANIFEST` 加 `fpga:{title, key:[...]}` |
| FPGA 趋势 sparkline | 可选 | 在 `features/snapshot/series.go` 的 `TrackedSeries` 加 spec（key 形如 `fpga_utilization`） |

**结论：daemon 一行 blank import 即可让新部件完整出现在 Web**；后续按需在 MANIFEST/TrackedSeries 美化。

### 9.2 场景 B：现有部件新增采集指标

采集器 `Collect()` 多返回若干 `Metric` 后（daemon 侧）：

| 出现位置 | 是否自动 | 备注 |
|----------|:--------:|------|
| 部件详情页"全部指标"表 | **自动** | 通用表格渲染该部件全部指标 |
| 概览卡关键指标 | 需在 MANIFEST.key 加条目 | 否则概览卡只展示原关键指标 |
| 趋势 sparkline | 需在 TrackedSeries 加 spec | 否则只显示当前值，无趋势 |

### 9.3 场景 C：新增一条趋势序列

在 `features/snapshot/series.go` 的 `TrackedSeries` 末尾加一行：
```go
{component: "fpga", name: "temperature", key: "fpga_temperature", mode: 0},
```
- `key` 必须形如 `<component>_<suffix>`，详情页 `componentSeries()` 按 `<component>_` 前缀过滤自动渲染。
- 在 `app.js` 的 `SERIES_LABELS` 加可选显示名（不加则用通用标签）。
- **无需改任何渲染逻辑**。

### 9.4 场景 D：调整历史深度

改 daemon 侧 `snapshot.NewPerCompWriter(..., historyCap, ...)` 的容量参数（`cmd/catmonitor/main.go` 当前传 60；`<=0` 时默认 60）；重启 daemon 生效。

### 9.5 扩展点汇总表

| 扩展需求 | 改动位置 | 自动部分 |
|----------|----------|----------|
| 新部件采集器 | `cmd/catmonitor/main.go`（daemon blank import） | 导航/概览卡/详情页 |
| 部件显示名/关键指标 | `features/web/static/app.js` MANIFEST | — |
| 新指标展示 | （daemon 采集器侧，无需改 web） | 详情页全部指标表 |
| 概览卡纳入新指标 | `features/web/static/app.js` MANIFEST.key | — |
| 新趋势 sparkline | `features/snapshot/series.go` TrackedSeries（daemon） | 详情页趋势面板 |
| 趋势显示名 | `features/web/static/app.js` SERIES_LABELS | — |
| 指标采集与否/优先级 | `features/web/metrics.yaml`（daemon 的 `features: [web]` 特性作用域）+ `configs/metrics.yaml` | daemon 经 metrics.Filter 自动应用 |
| 新静态身份指标（采集器侧） | 加入 `features/snapshot` 的 `StaticMetricNames` 即被 stash 进部件快照 `specs` | specs modal 通用表自动渲染 |
| 新静态身份指标（hwinfo 侧） | `features/snapshot/hwinfo.go` 加采集方法 + `app.js` 的 `SPEC_DEFS`/`LABEL_NAMES` 加显示名 | specs modal 按 component 分组自动出现 |
| 导航排序 | `features/web/static/app.js` NAV_ORDER | 未知部件自动排末尾 |
| 历史深度 | `cmd/catmonitor/main.go` PerCompWriter historyCap | — |

### 9.6 兼容性保证

- `health` 与 `metrics` 字段直接复用主项目结构体，**采集器新增任何字段/标签**都会原样透传到前端。
- 未知部件/未知指标/未知序列均有通用回退（部件用名大写、指标用原始名、序列用去前缀名），**不会因未登记而崩溃或消失**。

---

## 10. Git 与运行时文件

- **应提交**：`features/web/` 下所有源码与静态资源（`main.go`、`server.go`、`static.go`、`metrics.yaml`、`static/`、三个 `*_linux_test.go`）。
- **不应提交**：`features/web/data/`（旧版运行时残留，如 `ipmi_sensor_map.json` 缓存）与 `features/web/.runtime-*.tmp`（历史原子写残留）。根 `.gitignore` 已含 `features/web/data/`、`features/web/.runtime-*.tmp` 与 `**/ipmi_sensor_map.json` 三条。
- **构建产物**：`bin/catmonitor-web` 被根 `.gitignore` 的 `bin/` 覆盖，自动忽略。

---

## 11. 测试

`features/web` 的单元测试均为 `//go:build linux`（依赖真实 HTTP 栈与 unix socket 行为），`go test ./features/web/` 运行：

- `http_linux_test.go`：`TestWebRoutesRemainAvailableWithoutStressController` —— 注入 nil ControlClient 时 `/` 返回 200、`/api/stress/config` 返回 404（Stress 路由未注册）。
- `monitoring_compatibility_linux_test.go`：`TestLegacyWebFlags`（`-config=<path>` 作为废弃 flag 仍可传入且不校验）；`TestWebWithoutControlSocket`（control socket 缺失时：`/api/snapshot` 200、`/api/stress/config` 200 且 `enabled=false`/`available=false`、Run/Cancel 均 503）。
- `stress_mount_linux_test.go`：`TestWebMountsUnifiedStressView`（以 unix socket fixture 模拟 daemon Controller：`/api/stress/config` 200，`enabled`/`operator`/`security_debt_web_operator_auth` 均为 true）。

快照生产行为（序列/环形历史/静态 stash/硬件身份/全局与部件写盘）的单测位于 `features/snapshot`（`snapshot_test.go`、`series_test.go`、`global_test.go`、`comp_test.go`、`hwinfo_test.go`）。

`/api/collectors` 返回 **7 个采集器**的元数据（chassis/cpu/disk/gpu/memory/network/npu；`system` 不在列表，见 §6.2）。

运行与集成：

- `make web`（Makefile 目标）构建 `bin/catmonitor-web`；`make all` = build + web + dfee。
- `go test ./features/web/`；`make test-stress-ut` 与 `make test-stress-race`（`-race`）亦覆盖 `./features/web`。
- 非 Linux 平台该包无测试可跑（全部测试文件带 linux 构建标签），交叉编译仅验证构建。

---

## 12. 已知限制与后续预留

1. **单机本地视图**：不含认证、不含多机聚合；如需多机，预留为"多个 snapshot 源 + 概览聚合"未来扩展。
2. **轮询而非推送**：前端 `setInterval` 轮询 `/api/snapshot`；如需实时推送，预留 WebSocket/SSE（快照文件解耦边界可直接复用）。
3. **无持久化历史存储**：历史环形缓冲在 daemon 内存中（daemon 重启清空），快照文件仅保留最近 60 点窗口；如需长期趋势，预留为 `internal/storage` 风格的 JSONL 落盘（daemon 侧另起存储）。
4. **数据新鲜度依赖 daemon**：daemon 未运行或 `snapshot.enabled` 关闭时 `/api/snapshot` 等 Monitoring API 返回 503；control socket 不可用时 Stress 降级（§3.3）。Web 自身无法触发立即采集。
5. **扩展前置依赖主项目采集器**：新部件的真正采集逻辑仍需在 `internal/collectors/<name>/` 实现（见主项目 `AGENTS.md`），web 仅负责可视化（零注册、零 import）。
6. **指标展示优先级**：当前 metric 不携带优先级字段（主项目 `collector.Metric` 无 Priority），概览关键指标靠 MANIFEST 人工指定；未来若主项目 Metric 增加优先级，可改为按优先级自动选取关键指标。

---

## 13. 关键设计决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| 数据获取 | 只读消费 daemon 快照文件（非进程内采集 / 非 shell 调 CLI / 非读 JSONL） | Web 与采集彻底解耦；采集节奏、指标筛选、健康度评估全部收敛到 daemon 单点 |
| 解耦边界 | `snapshot.json`（全局）+ `snapshot_<comp>.json`（部件）文件 | HTTP 层只读文件，不调采集器；daemon 是唯一写者，原子写 |
| Stress 集成 | 经 unix control socket 代理（`stress.WebHandler` 策略代理，不拥有 Manager） | Web 保持只读；压测策略与执行归 daemon Controller；socket 缺失时优雅降级 |
| 多页面 | SPA + hash 路由 | 单文件部署、无后端路由、无构建步骤 |
| 扩展驱动 | `/api/collectors`（全局快照元数据）+ `TrackedSeries`（daemon）+ `MANIFEST`（显示提示） | 新部件/指标自动出现，显示美化集中可选 |
| 指标目录 | daemon 侧：`metrics.Init` 默认目录 + `features: [web]` 时加载 `features/web/metrics.yaml` 特性作用域，采集前经 `metrics.Filter` | Web 二进制不做目录筛选；快照里有什么由 daemon 决定 |
| 静态规格 | 双路径（daemon）：启动期 `CollectHWSpecs` 一次性采跨部件身份 + `PerCompWriter` stash CPU/内存一次性指标 | 身份信息非时序，跑一次即可；stash 保证首周期后不丢失 |
| 端口 | `:19322`（占用时自动 +1 递增） | Monitoring 与 Stress 统一入口；端口被占用自动探测下一可用端口，保证可拉起（见 §8.5） |
| 前端打包 | `//go:embed` | 单二进制可移植，离线可用 |
| 配置方式 | 仅命令行 flag（`-addr`/`-snapshot-dir`/`-control-socket`；`-config` 已废弃为 no-op） | 只读进程无可变配置，无需持久化 |

---

*文档版本：v1.4 · 对应代码状态：features/web/ 只读快照消费者 + 统一 Stress 代理（默认端口 :19322；23 条趋势序列；7 采集器；daemon 生产 snapshot.json + snapshot_<comp>.json）*
