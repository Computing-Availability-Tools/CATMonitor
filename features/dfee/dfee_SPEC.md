# dfee 能效监控模块技术规格说明书 (dfee_SPEC)

> **文档定位**：本文档是 dfee 能效监控模块的唯一设计与规格文档。
>
> **对应代码**：`dfee/` 目录（Go package `dfee`，与主项目同一 Go module）。
>
> **不改动现有 web 代码**：除 `web/server.go` 加 1 行 import + 1 行路由注册外，dfee 的全部逻辑、API、前端均独立于 `dfee/` 目录内。

---

## 1. 概述

### 1.1 目标

在现有 `catmonitor-web` 上新增能效监控页面，专门展示 74 项能效指标，以**实时图表**形式分小节呈现。核心需求：

1. **只监控能效指标**：从现有 snapshot.json 过滤出 74 项能效相关指标（NPU 46 + CPU 10 + Memory 7 + Disk 4 + Network 2 + Chassis 5）。
2. **CPU 时间分解转换**：8 项原始 jiffies 累计值不直接显示，在后端计算为 7 项利用率百分比。
3. **分小节实时图表**：每个小节（如 NPU 频率、NPU 温度、CPU 利用率等）的所有指标放在一张实时折线图里，随采集周期自动刷新。
4. **解耦**：dfee 作为独立 Go package，前端独立 SPA，不修改现有 web 的任何业务代码。

### 1.2 指标来源

所有 74 项能效指标均为现有 7 个 collector 已在采集的 159 个指标的**子集**。无需新增采集器，无需修改 snapshot.json 格式，无需新增 blank import。指标清单见 `dfee/energy_efficiency_metrics.md`。

---

## 2. 目录结构

```
dfee/
├── dfee_SPEC.md                   # 本设计文档
├── energy_efficiency_metrics.md    # 能效目标指标清单（74 项）
├── filter.go                      # 能效指标过滤集 + filterEfficiency() + 分组定义
├── cpu_derive.go                  # CPU 8 项 jiffies → 7 项利用率推导（有状态）
├── handler.go                     # HTTP handler：组装 /api/dfee 响应 + 静态文件
├── embed.go                       # //go:embed static
├── filter_test.go                 # 过滤逻辑测试
├── cpu_derive_test.go             # CPU 推导测试
├── handler_test.go                # HTTP 端到端测试
└── static/
    ├── index.html                  # 能效监控 SPA 页面
    ├── dfee.js                     # 实时图表渲染 + 轮询
    └── dfee.css                    # 样式
```

### 与现有 web 的关系

```
CATMonitor (Go module)
├── web/                  # 现有 web 仪表盘（不修改）
│   ├── server.go         # ← 唯一改动：加 1 行 import + 1 行 dfee.Register(mux, ...)
│   └── ...
├── dfee/                 # 能效监控模块（全部新增）
│   ├── ...（见上）
│   └── static/
└── internal/             # 采集器/来源层/健康度（不修改）
```

---

## 3. 架构与数据流

### 3.1 数据流

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
                                         → 按小节分组 → 14 张图表数据
                                         → 前端 Canvas 实时折线图
```

### 3.2 解耦边界

| 边界 | 说明 |
|------|------|
| dfee ← snapshot.json | dfee 只读 snapshot.json，不调采集器，不改快照格式 |
| dfee → 前端 | dfee 有独立 SPA（/dfee/），不复用 web/static/app.js |
| dfee ↔ web | 唯一接触点：web/server.go 调用 `dfee.Register(mux, snapshotPath)` |

### 3.3 对现有 web 的改动（仅 server.go）

```go
// web/server.go — 在 Routes() 中追加：
import "github.com/Computing-Availability-Tools/CATMonitor/dfee"

func (s *Server) Routes() http.Handler {
    mux := http.NewServeMux()
    // ... 现有路由不变 ...
    dfee.Register(mux, s.cfg.Storage.SnapshotPath)  // ← 新增 1 行
    return mux
}
```

**不修改**：main.go, collector.go, snapshot.go, config.go, hwinfo.go, static/app.js, index.html, style.css。

---

## 4. 后端设计

### 4.1 能效指标过滤集（filter.go）

#### 4.1.1 过滤 spec 结构

```go
type efficiencySpec struct {
    component  string
    name       string
    labelKey   string   // "" = 不做 label 过滤
    labelVals  []string // 空 = 匹配任意值
}
```

#### 4.1.2 过滤规则

| 部件 | 特殊 label 过滤 | 说明 |
|------|----------------|------|
| NPU (46) | 无 | 所有 NPU 能效指标均包含所有设备实例（npu_id 标签保留） |
| CPU 时间 (8) | `core=total` | 仅取聚合值，排除 per-core |
| CPU 负载 (1) | 无 | 所有 interval（1m/5m/15m）均包含 |
| CPU 功耗 (1) | 无 | 所有 socket 均包含 |
| Memory usage_detail (5) | `field∈{total,free,buffers,cached,sreclaimable}` | 排除 used/available 等 |
| Memory swap_detail (2) | `field∈{total,free}` | 排除 used |
| Disk/Network/Chassis | 无 | 所有实例均包含 |

#### 4.1.3 过滤函数

```go
func filterEfficiency(metrics []collector.Metric) []collector.Metric
```

遍历输入 metrics，若存在匹配的 spec 则保留。每个输入 metric 最多出现一次，保持原始顺序。

### 4.2 图表分组定义（filter.go）

74 项指标按 `energy_efficiency_metrics.md` 的小节组织为 **14 张图表**：

| # | 图表 ID | 标题 | 部件 | 指标数 | 说明 |
|---|---------|------|------|:---:|------|
| 1 | npu_frequency | NPU 频率 | npu | 7 | aicpu_freq ~ ddr_freq |
| 2 | npu_utilization | NPU 利用率 | npu | 14 | utilization ~ jpegd_util |
| 3 | npu_temperature | NPU 温度 | npu | 14 | temperature ~ hbm_max_temp |
| 4 | npu_voltage_power | NPU 电压与功耗 | npu | 7 | power_draw ~ acg_count |
| 5 | npu_fan | NPU 风扇 | npu | 1 | fan_speed |
| 6 | npu_llc | NPU LLC 性能 | npu | 3 | llc_write/read_hit_rate, llc_throughput |
| 7 | cpu_utilization | CPU 利用率分解 | cpu | 7 | **推导值**（见 §4.3），替换原始 8 项 jiffies |
| 8 | cpu_load | CPU 负载 | cpu | 1(3 interval) | load_average |
| 9 | cpu_power | CPU 功耗 | cpu | 1 | power |
| 10 | memory_pool | 内存池 | memory | 5 | usage_detail (5 fields) |
| 11 | memory_swap | Swap | memory | 2 | swap_detail (2 fields) |
| 12 | disk_io | 磁盘 IO | disk | 4 | throughput, read/write_latency, iops |
| 13 | network_traffic | 网络流量 | network | 2 | rx/tx_bytes_total |
| 14 | chassis_power_temp | 机箱功耗与温度 | chassis | 5 | power, inlet/outlet_temp, fan_speed/power |

> **注意**：图表 7 (CPU 利用率分解) 的 7 项指标是后端推导的，不是 snapshot 中的原始指标。原始 8 项 jiffies（user_time ~ steal_time）**不出现在 API 响应中**。

### 4.3 CPU 时间 → 利用率推导（cpu_derive.go）

#### 4.3.1 背景

CPU 采集器产出的 8 项时间指标（user_time, nice_time, system_time, idle_time, iowait_time, irq_time, softirq_time, steal_time）是 /proc/stat 的**累计 jiffies 值**。直接显示无意义，需计算两次采集之间的增量占比。

#### 4.3.2 推导逻辑

```
设本周期值 = curr[8], 上周期值 = prev[8]（缓存于 Handler）
delta[i] = curr[i] - prev[i]
total_delta = Σ delta[i]  (i=0..7)

→ 7 项利用率（%）：
  idle_util      = delta[idle] / total_delta × 100
  non_idle_util  = (total_delta - delta[idle]) / total_delta × 100
  user_util      = (delta[user] + delta[nice]) / total_delta × 100
  system_util    = delta[system] / total_delta × 100
  iowait_util    = delta[iowait] / total_delta × 100
  irq_util       = (delta[irq] + delta[softirq]) / total_delta × 100
  steal_util     = delta[steal] / total_delta × 100
```

首次调用（无 prev 缓存）→ 所有利用率为 0%，或省略该图表。

#### 4.3.3 Handler 状态

```go
type Handler struct {
    snapshotPath string
    mu           sync.Mutex
    prevCPU      cpuTimeSnapshot  // 上周期的 8 项累计值
    hasPrev      bool
}

type cpuTimeSnapshot struct {
    user, nice, system, idle    float64
    iowait, irq, softirq, steal float64
}
```

每次 `/api/dfee` 调用：
1. 读 snapshot.json
2. 提取 8 项 CPU time（core=total）
3. 若 `hasPrev`：计算 7 项利用率
4. 更新 `prevCPU` + `hasPrev = true`
5. 返回 7 项利用率替换原始 8 项 jiffies

> **并发安全**：`mu` 保护 `prevCPU`，同一时刻只有一个请求能读写状态。

#### 4.3.4 推导结果与原始指标的关系

| 原始指标 (jiffies) | 推导指标 (%) | 用户需求名称 |
|---------------------|-------------|-------------|
| idle_time | idle_util | idle 利用率 |
| user_time + nice_time | user_util | 用户空间利用率 |
| system_time | system_util | 系统进程利用率 |
| iowait_time | iowait_util | IO 等待 |
| irq_time + softirq_time | irq_util | 中断 |
| steal_time | steal_util | 虚拟机利用率 |
| (total - idle) | non_idle_util | 非 idle 利用率 |

> non_idle_util = user_util + system_util + iowait_util + irq_util + steal_util = 100 - idle_util。虽冗余但用户要求展示。

### 4.4 API 端点（handler.go）

#### 4.4.1 路由注册

```go
// dfee 包导出函数，由 web/server.go 调用
func Register(mux *http.ServeMux, snapshotPath string) {
    h := NewHandler(snapshotPath)
    mux.HandleFunc("/api/dfee", h.handleAPI)
    mux.Handle("/dfee/static/", http.StripPrefix("/dfee/static/", http.FileServer(http.FS(staticFiles))))
    mux.HandleFunc("/dfee/", h.handleIndex)
}
```

#### 4.4.2 API 契约

**GET /api/dfee**

读取 snapshot.json → filterEfficiency → CPU 推导 → 按图表分组 → 返回 JSON。

响应结构：

```json
{
  "timestamp": "2026-07-16T14:30:00+08:00",
  "refresh_interval_ms": 5000,
  "charts": [
    {
      "id": "npu_frequency",
      "title": "NPU 频率",
      "y_unit": "MHz",
      "series": [
        {
          "id": "0:aicpu_freq",
          "label": "AICPU频率 [NPU 0]",
          "value": 1200,
          "unit": "MHz"
        },
        {
          "id": "0:aicore_rated_freq",
          "label": "AICore额定频率 [NPU 0]",
          "value": 1800,
          "unit": "MHz"
        }
      ]
    },
    {
      "id": "cpu_utilization",
      "title": "CPU 利用率分解",
      "y_unit": "%",
      "series": [
        { "id": "idle_util", "label": "空闲利用率", "value": 65.5, "unit": "%" },
        { "id": "non_idle_util", "label": "非空闲利用率", "value": 34.5, "unit": "%" },
        { "id": "user_util", "label": "用户空间利用率", "value": 20.0, "unit": "%" },
        { "id": "system_util", "label": "系统进程利用率", "value": 8.5, "unit": "%" },
        { "id": "iowait_util", "label": "IO 等待", "value": 1.0, "unit": "%" },
        { "id": "irq_util", "label": "中断", "value": 2.5, "unit": "%" },
        { "id": "steal_util", "label": "虚拟机利用率", "value": 0.0, "unit": "%" }
      ]
    }
  ]
}
```

**series.id 命名规则**：
- 普通指标：`{device_id}:{metric_name}`（如 `0:aicpu_freq`）
- 无 device 标签的指标：`{metric_name}`（如 `idle_util`）
- Memory field 类：`{metric_name}:{field_value}`（如 `usage_detail:total`）
- CPU 推导值：`{derived_name}`（如 `idle_util`）

前端用 `series.id` 作为滚动缓冲区的 key。

**快照未就绪**：返回 503 `{"error":"snapshot not ready"}`。

**y_unit**：取该小节所有指标的公共单位（如 NPU 频率都是 MHz）；混合单位时为空字符串。

### 4.5 静态文件服务

```go
//go:embed static
var staticFiles embed.FS
```

- `GET /dfee/` → 返回 `static/index.html`
- `GET /dfee/static/{file}` → 静态资源（CSS/JS）
- 前端 SPA 不依赖现有 web 的 app.js / style.css

---

## 5. 前端设计

### 5.1 页面结构

```
┌──────────────────────────────────────────────────────┐
│ CATMonitor 能效监控                  间隔: 5s  [立即刷新] │  ← 顶栏
├───────────────────────────┬──────────────────────────┤
│ NPU 频率 (MHz)            │ NPU 利用率 (%)             │
│ [Canvas 实时折线图]        │ [Canvas 实时折线图]         │
├───────────────────────────┼──────────────────────────┤
│ NPU 温度 (°C)             │ NPU 电压与功耗 (V/W)       │
│ [Canvas]                  │ [Canvas]                  │
├───────────────────────────┼──────────────────────────┤
│ NPU 风扇 (%)              │ NPU LLC 性能 (%, MB/s)     │
│ [Canvas]                  │ [Canvas]                  │
├───────────────────────────┼──────────────────────────┤
│ CPU 利用率分解 (%)         │ CPU 负载                   │
│ [Canvas]                  │ [Canvas]                  │
├───────────────────────────┼──────────────────────────┤
│ CPU 功耗 (W)              │ 内存池 (MB)                │
│ [Canvas]                  │ [Canvas]                  │
├───────────────────────────┼──────────────────────────┤
│ Swap (MB)                 │ 磁盘 IO                    │
│ [Canvas]                  │ [Canvas]                  │
├───────────────────────────┼──────────────────────────┤
│ 网络流量 (bytes)          │ 机箱功耗与温度              │
│ [Canvas]                  │ [Canvas]                  │
└───────────────────────────┴──────────────────────────┘
```

### 5.2 数据获取与轮询

```js
// 启动时：获取配置 → 开始轮询
async function init() {
    const data = await fetch('/api/dfee').then(r => r.json());
    refreshIntervalMs = data.refresh_interval_ms || 5000;
    startPolling();
}

function startPolling() {
    pollTimer = setInterval(pollTick, refreshIntervalMs);
    pollTick();
}

async function pollTick() {
    const resp = await fetch('/api/dfee', { cache: 'no-store' });
    if (!resp.ok) return;
    const data = await resp.json();
    updateBuffers(data);      // 追加到滚动缓冲
    renderAllCharts();        // 重绘所有图表
}
```

### 5.3 滚动缓冲区

前端维护每个 series 的滚动缓冲区（纯内存，不落盘）：

```js
const HISTORY_POINTS = 60;  // 保留最近 60 个采样点
const buffers = {};         // { seriesId: number[] }

function updateBuffers(data) {
    for (const chart of data.charts) {
        for (const s of chart.series) {
            if (!buffers[s.id]) buffers[s.id] = [];
            buffers[s.id].push(s.value);
            if (buffers[s.id].length > HISTORY_POINTS)
                buffers[s.id].shift();
        }
    }
}
```

### 5.4 Canvas 实时图表渲染

每张图表用一个 `<canvas>` 元素，纯 vanilla JS Canvas 2D API 绘制：

```
renderChart(canvas, seriesList, buffers)
├── 清空 canvas
├── 计算所有 series 的全局 min/max（用于 Y 轴缩放）
├── 绘制 Y 轴：max/min 标签 + 水平网格线
├── 绘制 X 轴：时间跨度标签（−N分钟 ~ 现在）
├── 为每个 series 绘制折线：
│     遍历 buffers[id] → 逐点映射到 canvas 坐标 → stroke
│     颜色：从 12 色调色板循环分配
├── 绘制图例：
│     series label + 当前值 + 颜色色块
└── 数据不足（<2 点）时显示"数据采集中…"
```

**颜色分配**：12 色调色板循环使用，保证同一 series 在不同刷新间颜色稳定（基于 series 在列表中的索引）。

**自适应**：canvas 宽度跟随容器（`canvas.width = canvas.clientWidth * devicePixelRatio`），高 DPI 屏幕清晰。

### 5.5 关键交互

| 交互 | 说明 |
|------|------|
| 自动刷新 | 按 `refresh_interval_ms` 轮询 `/api/dfee` |
| 立即刷新 | 调 `POST /api/refresh`（现有 web 端点）→ 触发采集 → 下次轮询看到新数据 |
| 间隔调整 | 调 `POST /api/config`（现有 web 端点）→ 热生效 |
| 图表悬停 | 鼠标移到折线上 → tooltip 显示该 series 的 label + 当前值 |

### 5.6 空数据处理

- 硬件不可用（如无 NPU）：对应图表的 `series` 为空 → 图表区域显示"无数据（硬件或采集器不可用）"
- 首次加载（无历史）：所有缓冲区为空 → 图表显示"数据采集中…"
- 快照未就绪：`/api/dfee` 返回 503 → 顶栏显示"快照尚未就绪，等待首次采集…"

---

## 6. 图表分组完整清单

与 `energy_efficiency_metrics.md` 小节一一对应，共 14 张图表：

### NPU（6 张图，46 指标）

| 图表 | 小节 | 指标 | Y 单位 |
|------|------|:---:|:---:|
| npu_frequency | 1.1 频率 | aicpu_freq, aicore_rated_freq, aicore_freq, ctrlcpu_freq, vector_core_freq, hbm_freq, ddr_freq | MHz |
| npu_utilization | 1.2 利用率 | utilization, memory_usage, npu_util, aicpu_util, ctrlcpu_util, vector_core_util, hbm_bandwidth_util, ddr_util, ddr_bandwidth_util, vdec_util, vpc_util, venc_util, jpege_util, jpegd_util | % |
| npu_temperature | 1.3 温度 | temperature, hbm_temp, cluster_temp, peri_temp, aicore0_temp, aicore1_temp, ntc1~4_temp, soc_max_temp, fp_max_temp, ndie_temp, hbm_max_temp | °C |
| npu_voltage_power | 1.4 电压与功耗 | power_draw, voltage, aicore_voltage, hybrid_voltage, cpu_voltage, ddr_voltage, acg_count | 混合 |
| npu_fan | 1.5 风扇 | fan_speed | % |
| npu_llc | 1.6 LLC | llc_write_hit_rate, llc_read_hit_rate, llc_throughput | 混合 |

### CPU（3 张图，10 指标 → 7 推导 + 3 原始）

| 图表 | 小节 | 指标 | Y 单位 |
|------|------|:---:|:---:|
| cpu_utilization | 2.1 → 推导 | idle_util, non_idle_util, user_util, system_util, iowait_util, irq_util, steal_util | % |
| cpu_load | 2.2 负载 | load_average (1m/5m/15m) | — |
| cpu_power | 2.3 功耗 | power | W |

### Memory / Disk / Network / Chassis（5 张图，18 指标）

| 图表 | 小节 | 指标 | Y 单位 |
|------|------|:---:|:---:|
| memory_pool | 3 内存池 | usage_detail (total/free/buffers/cached/sreclaimable) | MB |
| memory_swap | 3 Swap | swap_detail (total/free) | MB |
| disk_io | 4 IO | throughput, read_latency, write_latency, iops | 混合 |
| network_traffic | 5 流量 | rx_bytes_total, tx_bytes_total | bytes |
| chassis_power_temp | 6 功耗与温度 | power, inlet_temp, outlet_temp, fan_speed, fan_power | 混合 |

---

## 7. 与现有 web 的集成

### 7.1 web/server.go 改动

仅 `Routes()` 函数追加 2 行：

```go
import "github.com/Computing-Availability-Tools/CATMonitor/dfee"

func (s *Server) Routes() http.Handler {
    mux := http.NewServeMux()
    // ... 现有路由不变 ...
    dfee.Register(mux, s.cfg.Storage.SnapshotPath)
    return mux
}
```

### 7.2 访问入口

- 能效监控页：`http://localhost:9527/dfee/`
- 能效 API：`http://localhost:9527/api/dfee`
- 现有仪表盘：`http://localhost:9527/`（不变）
- 现有 API：`/api/snapshot`, `/api/collectors`, `/api/config`, `/api/refresh`（不变）

### 7.3 共享但不耦合

| 共享资源 | dfee 的使用方式 |
|---------|----------------|
| snapshot.json | 只读（`Read()` 函数，复用 web/snapshot.go） |
| `/api/refresh` | dfee 前端调现有端点触发立即采集 |
| `/api/config` | dfee 前端调现有端点读取/设置刷新间隔 |
| 采集器注册表 | 不使用（dfee 不调采集器） |
| health 评分 | 不使用（能效监控不涉及健康度） |

---

## 8. 扩展性设计

### 8.1 新增能效指标

1. 在 `filter.go` 的 `efficiencySpecs` 加一行 spec
2. 在 `filter.go` 的 `chartGroups` 对应图表的 metric 名列表加一项
3. 前端自动渲染（无需改 JS）

### 8.2 新增图表

1. 在 `filter.go` 的 `chartGroups` 加一个 chart 定义
2. 前端自动多渲染一张图（无需改 JS）

### 8.3 新增 CPU 推导指标

1. 在 `cpu_derive.go` 的 `deriveCPUUtil` 函数加计算逻辑
2. 在 `chartGroups` 的 cpu_utilization 图表 series 列表加一项

---

## 9. 测试策略

### 9.1 后端测试

| 测试文件 | 覆盖内容 |
|---------|---------|
| `filter_test.go` | spec 不变量（非空/无重复）、全量过滤（含/排除）、label 过滤精确性、空输入、多设备、Memory field 过滤 |
| `cpu_derive_test.go` | 首次调用（无 prev→零值）、正常增量推导、total_delta=0 边界、负 delta 防护、并发安全 |
| `handler_test.go` | HTTP 端到端（写 snapshot→GET /api/dfee→断言图表结构+series+CPU 推导）、503 快照未就绪、静态文件 200 |

### 9.2 前端验证

手动冒烟测试：
1. `go build` + `go vet` + `go test ./dfee/`
2. 启动 catmonitor-web → 访问 `http://localhost:9527/dfee/`
3. 14 张图表全部渲染（有数据的显示折线，无数据的显示"无数据"）
4. 等待几秒 → 图表自动刷新，折线增长
5. 点击"立即刷新" → 数据立即更新
6. 切换到 `/`（现有仪表盘）→ 不受影响

---

## 10. 已知限制与后续预留

1. **前端历史不持久**：滚动缓冲区纯内存，刷新页面后清空。与现有 web 的 history 行为一致。
2. **多设备图表线多**：8 卡 NPU × 14 利用率指标 = 112 条线在一张图上，可能拥挤。后续可加设备选择器或聚合（max/avg）开关。
3. **CPU 推导有状态**：Handler 缓存 prev 值，进程重启后首次数据为零。与 CPU 采集器的 prevStats 行为一致。
4. **无独立配置**：dfee 复用 web 的 config.yaml + runtime.json，不新增配置文件。刷新间隔由现有 `/api/config` 统一管理。
5. **单位混合图表**：NPU 电压与功耗（V + W + 次）、LLC（% + MB/s）等混合单位在同一图表，Y 轴只能显示为空或主导单位。后续可拆分子图。

---

## 11. 关键设计决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| dfee 形态 | 独立 Go package + 独立 SPA | 全部代码在 dfee/ 目录，web 仅 1 行注册改动 |
| 数据来源 | 复用 snapshot.json（只读过滤） | 零采集改动，零快照改动 |
| CPU 推导位置 | 后端（Handler 有状态） | 推导是业务逻辑，前端只展示 |
| 前端图表技术 | Canvas 2D API（无外部库） | 与现有 web 一致（零依赖），多 series 性能优于 SVG |
| 前端历史缓冲 | 内存滚动数组（60 点） | 无需后端维护能效专用 history，解耦 |
| 图表组织 | 按 energy_efficiency_metrics.md 小节一一对应 | 用户明确要求"每个小节所有指标放一起" |
| 页面入口 | /dfee/ 独立 SPA | 不修改 web/static/app.js，完全解耦 |
| 静态资源打包 | //go:embed | 单二进制部署，与现有 web 一致 |

---

*文档版本：v1.0 · 对应指标清单：dfee/energy_efficiency_metrics.md（74 项，修正后）*
