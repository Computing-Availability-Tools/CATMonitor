# dfee 能效监控模块技术规格说明书 (dfee_SPEC)

> **文档定位**：本文档是 dfee 能效监控模块的唯一设计与规格文档。
>
> **对应代码**：`features/dfee/` 目录（Go package `dfee`，与主项目同一 Go module）。
>
> **不改动现有 web 代码**：除 `features/web/server.go` 加 import + 1 行路由注册、`features/web/main.go` 加 4 行 metrics override 外，dfee 的全部逻辑、API、前端均独立于 `features/dfee/` 目录内。

---

## 1. 概述

### 1.1 目标

在现有 `catmonitor-web` 上新增能效监控页面，专门展示 74 项能效指标，以 25 张**实时 Canvas 折线图**按部件分组呈现。核心需求：

1. **只监控能效指标**：从现有 snapshot.json 过滤出 74 项能效相关指标（NPU 46 + CPU 10 + Memory 7 + Disk 4 + Network 2 + Chassis 5）。
2. **CPU 时间分解转换**：8 项原始 jiffies 累计值不直接显示，在后端计算为 7 项利用率百分比。
3. **网络字节差值转换**：rx/tx_bytes_total 累计值转换为两次采集间的增量。
4. **分小节实时图表**：每个小节的所有指标放在一张实时折线图里，随采集周期自动刷新。混合单位的小节拆分为多张图（每张单位统一）。
5. **NPU 优先级筛选**：9 张 NPU 图分高/中/低三级，前端可按优先级筛选显示。
6. **解耦**：dfee 作为独立 Go package，前端独立 SPA，不修改现有 web 的任何业务代码。

### 1.2 指标来源

所有 74 项能效指标均为现有 7 个 collector 已在采集的指标的**子集**。无需新增采集器，无需修改 snapshot.json 格式。指标清单见 `features/dfee/energy_efficiency_metrics.md`。

### 1.3 metrics 优先级覆盖

main 分支新增的 `metrics.Filter()` 按优先级过滤指标（Low 被丢弃）。dfee 使用的 22 个指标在默认目录中标为 Low，需通过 `features/dfee/metrics.yaml` 覆盖为 Medium：

| 部件 | 被覆盖指标 | 数量 |
|------|-----------|:---:|
| cpu | user_time, nice_time, system_time, idle_time, iowait_time, irq_time, softirq_time, steal_time | 8 |
| npu | acg_count, ntc1-4_temp, aicore_rated_freq, vdec/vpc/venc/jpege/jpegd_util, llc_write_hit_rate, llc_read_hit_rate, llc_throughput | 14 |

在 `features/web/main.go` 中加载此 override（+4 行）。

---

## 2. 目录结构

```
features/dfee/
├── dfee_SPEC.md                   # 本设计文档
├── energy_efficiency_metrics.md    # 能效目标指标清单（74 项）
├── metrics.yaml                   # 指标优先级覆盖（22 个 Low → Medium）
├── filter.go                      # 能效指标过滤集 + 25 张图表分组 + seriesID/label + naturalLess
├── cpu_derive.go                  # CPU 8 项 jiffies → 7 项利用率推导
├── net_derive.go                  # 网络累计字节 → 两次采集差值
├── handler.go                     # HTTP handler + CPU 推导缓存 + 网络差值
├── embed.go                       # //go:embed static
├── filter_test.go                 # 过滤逻辑测试
├── cpu_derive_test.go             # CPU 推导测试
├── handler_test.go                # HTTP 端到端测试
└── static/
    ├── index.html                  # 能效监控 SPA 页面
    ├── dfee.js                     # 实时图表渲染 + 轮询 + NPU 优先级筛选
    └── dfee.css                    # 样式
```

### 与现有 web 的关系

```
CATMonitor (Go module)
├── features/web/             # 现有 web 仪表盘
│   ├── server.go             # ← 改动：import + dfee.Register(mux, ...)
│   ├── main.go               # ← 改动：加载 dfee/metrics.yaml override
│   └── static/app.js         # ← 改动：导航栏加"能效分析"链接
├── features/dfee/            # 能效监控模块（全部新增）
│   ├── ...（见上）
│   └── static/
├── features/health/          # 健康度评估模块（main 分支）
└── internal/                  # 采集器/来源层/metrics 目录（不修改）
```

---

## 3. 架构与数据流

### 3.1 数据流

```
采集层 (7 collectors, 不变)
  → DataCollector.collectOnce() → metrics.Filter() → snapshot.json
        │
        ├──────────────────────────────────────────┐
        ↓                                          ↓
  GET /api/snapshot (现有, 不变)          GET /api/dfee (dfee 新增)
  → 全量指标                              → 过滤 74 能效指标
  → 前端 SPA 概览/详情页                   → CPU 8 jiffies → 7 利用率推导（缓存）
                                          → 网络累计 → 差值
                                          → 按小节分组 → 25 张图表数据
                                          → 前端 Canvas 实时折线图
```

### 3.2 解耦边界

| 边界 | 说明 |
|------|------|
| dfee ← snapshot.json | dfee 只读 snapshot.json，不调采集器，不改快照格式 |
| dfee → 前端 | dfee 有独立 SPA（/dfee/），不复用 features/web/static/app.js |
| dfee ↔ web | 接触点：server.go 注册路由 + main.go 加载 metrics override |

### 3.3 对现有 web 的改动

**features/web/server.go**（+2 行）：

```go
import "github.com/Computing-Availability-Tools/CATMonitor/features/dfee"

func (s *Server) Routes() http.Handler {
    mux := http.NewServeMux()
    // ... 现有路由不变 ...
    dfee.Register(mux, s.cfg.Storage.SnapshotPath)
    return mux
}
```

**features/web/main.go**（+4 行）：

```go
if err := metrics.LoadModuleOverride("features/dfee/metrics.yaml"); err != nil {
    logger.Error("dfee metrics override failed", "error", err)
}
```

**features/web/static/app.js**（+2 行）：导航栏追加"能效分析"链接指向 `/dfee/`。

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

### 4.2 图表分组定义（filter.go）

#### 4.2.1 chartGroup 结构

```go
type chartGroup struct {
    id          string
    title       string
    component   string
    metricNames []string
    labelKey    string // 可选：按此 label key 过滤
    labelVal    string // 可选：匹配此 label 值（空值=不过滤，只触发简化标签）
    priority    string // "high" / "medium" / "low" / "" (非 NPU)
}
```

#### 4.2.2 25 张图表完整清单

| # | 图表 ID | 标题 | 部件 | 指标数 | Y 单位 | 优先级 |
|---|---------|------|------|:---:|:---:|:---:|
| 1 | npu_frequency | NPU 频率 | npu | 7 | MHz | medium |
| 2 | npu_utilization | NPU 利用率 | npu | 14 | % | high |
| 3 | npu_temperature | NPU 温度 | npu | 14 | °C | high |
| 4 | npu_power | NPU 功耗 | npu | 1 | W | high |
| 5 | npu_voltage | NPU 电压 | npu | 5 | V | medium |
| 6 | npu_acg | NPU 调频计数 | npu | 1 | 次 | low |
| 7 | npu_fan | NPU 风扇 | npu | 1 | % | low |
| 8 | npu_llc_hit_rate | NPU LLC 命中率 | npu | 2 | % | medium |
| 9 | npu_llc_throughput | NPU LLC 吞吐量 | npu | 1 | MB/s | medium |
| 10 | cpu_utilization | CPU 利用率分解 | cpu | 7 | % | — |
| 11 | cpu_load | CPU 负载 | cpu | 1(3) | — | — |
| 12 | cpu_power | CPU 功耗 | cpu | 1 | W | — |
| 13 | memory_pool | 内存池 | memory | 5 | MB | — |
| 14 | memory_swap | Swap | memory | 2 | MB | — |
| 15 | disk_throughput_read | 磁盘吞吐量(读) | disk | 1 | MB/s | — |
| 16 | disk_throughput_write | 磁盘吞吐量(写) | disk | 1 | MB/s | — |
| 17 | disk_iops_read | IOPS(读) | disk | 1 | 次/s | — |
| 18 | disk_iops_write | IOPS(写) | disk | 1 | 次/s | — |
| 19 | disk_read_latency | 磁盘读耗时 | disk | 1 | ms/s | — |
| 20 | disk_write_latency | 磁盘写耗时 | disk | 1 | ms/s | — |
| 21 | network_rx | 网络接收 | network | 1 | bytes | — |
| 22 | network_tx | 网络发送 | network | 1 | bytes | — |
| 23 | chassis_power | 机箱功耗 | chassis | 2 | W | — |
| 24 | chassis_temp | 机箱温度 | chassis | 2 | °C | — |
| 25 | chassis_fan | 机箱风扇转速 | chassis | 1 | RPM | — |

> 图表 10 (CPU 利用率分解) 的 7 项指标是后端推导的，原始 8 项 jiffies 不出现在 API 响应中。
> 图表 21-22 (网络接收/发送) 的值是两次采集的差值，不是累计绝对值。
> 磁盘 6 张图按指标×方向拆分；网络 2 张按收发拆分；机箱 3 张按单位拆分。

### 4.3 CPU 时间 → 利用率推导（cpu_derive.go + handler.go）

#### 4.3.1 推导逻辑

```
delta[i] = curr[i] - prev[i]（负值钳零）
total_delta = Σ delta[i]

→ 7 项利用率（%）：
  idle_util      = delta[idle] / total_delta × 100
  non_idle_util  = (total_delta - delta[idle]) / total_delta × 100
  user_util      = (delta[user] + delta[nice]) / total_delta × 100
  system_util    = delta[system] / total_delta × 100
  iowait_util    = delta[iowait] / total_delta × 100
  irq_util       = (delta[irq] + delta[softirq]) / total_delta × 100
  steal_util     = delta[steal] / total_delta × 100
```

#### 4.3.2 推导结果命名

| 指标 ID | 图例名 | 原始来源 |
|---------|--------|---------|
| idle_util | 空闲 | idle_time |
| non_idle_util | 非空闲 | total - idle |
| user_util | 用户态 | user_time + nice_time |
| system_util | 内核态 | system_time |
| iowait_util | IO等待 | iowait_time |
| irq_util | 中断 | irq_time + softirq_time |
| steal_util | Steal | steal_time |

#### 4.3.3 Handler 状态与缓存

```go
type Handler struct {
    snapshotPath string
    mu           sync.Mutex
    prevCPU      cpuTimeSnapshot  // 上周期 8 项累计值
    hasPrev      bool
    lastDerived  []derivedMetric // 缓存上次非零推导结果
    prevNet      map[string]float64 // 上周期网络累计字节
    hasPrevNet   bool
}
```

**total=0 缓存策略**：当 `total_delta = 0`（如同一 snapshot 被读两次），`deriveCPUUtil` 返回 nil，Handler 复用 `lastDerived` 缓存值。series 持续存在，前端缓冲区不被清空，不出现 0 值凹点。

首次调用（无 prev + 无缓存）→ 不产出指标，图显示"无数据"。

### 4.4 网络字节差值（net_derive.go）

rx_bytes_total / tx_bytes_total 是累计计数器。Handler 缓存上一次值，每次 API 调用计算 `delta = curr - prev`（负值钳零，计数器重置时）。首次调用返回 0。

### 4.5 图例命名（filter.go）

#### 4.5.1 显示名映射

`metricDisplayNames` 按 `部件:指标名` 做 key，区分同名不同部件指标：

```go
"cpu:power": "CPU功耗",
"chassis:power": "整机功耗",
"npu:fan_speed": "风扇转速",
"chassis:fan_speed": "风扇转速",
```

#### 4.5.2 seriesLabel 简化逻辑

当 chartGroup 的 `labelKey != ""` 时（如磁盘 `direction`、网络 `interface`），图例只返回设备标识（如 `sda`、`eth0`），不拼指标名和部件前缀。因为图表标题已含指标名和方向。

#### 4.5.3 自然排序

`groupForChart` 使用 `naturalLess` 排序（数字段按数值比较），确保 `load_average:1m < 5m < 15m`，而非 ASCII 的 `15m < 1m < 5m`。

### 4.6 API 端点（handler.go）

**GET /api/dfee**：读 snapshot → 过滤 → CPU 推导（带缓存）→ 网络差值 → 按图表分组 → 返回 JSON。

响应结构：

```json
{
  "timestamp": "2026-07-16T14:30:00+08:00",
  "refresh_interval_ms": 5000,
  "charts": [
    {
      "id": "npu_frequency",
      "title": "NPU 频率 (MHz)",
      "y_unit": "MHz",
      "priority": "medium",
      "series": [
        { "id": "0:aicpu_freq", "label": "AICPU频率 [NPU 0]", "value": 1200, "unit": "MHz" }
      ]
    },
    {
      "id": "cpu_utilization",
      "title": "CPU 利用率分解 (%)",
      "y_unit": "%",
      "series": [
        { "id": "idle_util", "label": "空闲", "value": 65.5, "unit": "%" },
        { "id": "non_idle_util", "label": "非空闲", "value": 34.5, "unit": "%" },
        { "id": "user_util", "label": "用户态", "value": 20.0, "unit": "%" },
        { "id": "system_util", "label": "内核态", "value": 8.5, "unit": "%" },
        { "id": "iowait_util", "label": "IO等待", "value": 1.0, "unit": "%" },
        { "id": "irq_util", "label": "中断", "value": 2.5, "unit": "%" },
        { "id": "steal_util", "label": "Steal", "value": 0.0, "unit": "%" }
      ]
    }
  ]
}
```

**series.id 命名规则**：
- NPU：`{npu_id}:{metric_name}`（如 `0:aicpu_freq`）
- 磁盘：`{device}:{metric_name}:{direction}`（如 `sda:throughput:read`）
- 网络：`{interface}:{metric_name}`（如 `eth0:rx_bytes_total`）
- Memory：`{metric_name}:{field}`（如 `usage_detail:total`）
- CPU 推导：`{derived_name}`（如 `idle_util`）
- 无标签：`{metric_name}`

快照未就绪返回 503。

---

## 5. 前端设计

### 5.1 页面结构

按部件分组，每区块有彩色竖线标题 + 可用数 + 优先级筛选（仅 NPU）：

```
┌─ NPU 能效  0/9 可用    [全部] [高] [中] [低] ─┐
│ [频率] [利用率] [温度] [功耗] [电压] [调频] [风扇] [LLC命中率] [LLC吞吐] │
├─ CPU 能效  2/3 可用 ──────────────────────────┐
│ [利用率分解] [负载] [功耗]                        │
├─ 内存  2/2 可用 ─── 磁盘  6/6 可用 ────────────┐
│ [内存池] [Swap]   [吞吐读] [吞吐写] [IOPS读] [IOPS写] [读耗时] [写耗时] │
├─ 网络  2/2 可用 ─── 机箱  0/3 可用 ────────────┐
│ [接收] [发送]     [功耗] [温度] [风扇转速]       │
└──────────────────────────────────────────────────┘
```

### 5.2 数据获取与轮询

- 启动时 fetch `/api/dfee` → 获取 `refresh_interval_ms` → 开始轮询
- 每周期：`updateBuffers`（在 `buildSections` 之前）→ 重建检测 → `renderAllCharts`
- 立即刷新：`POST /api/refresh` → 400ms 后 pollTick
- 间隔调整：`POST /api/config`（现有 web 端点）

### 5.3 滚动缓冲区

前端维护每个 series 的内存滚动数组（60 点）。`updateBuffers` 在 `buildSections` 之前执行，确保 `buildCard` 检查 buffer 时已有当前数据。消失的 series 缓冲区被自动清理。

### 5.4 Canvas 实时图表渲染

每张图用一个 `<canvas>`，纯 Canvas 2D API：

- **Y 轴**：全局 min/max，4 条网格线，每条带缩写标签（K/M/G/T）+ 单位
- **Y 轴近平坦**：当数据变化幅度 <1%（如累计计数器），Y 轴从 0 展开
- **X 轴**：标签反映实际数据时长（`-5s` → `-5min`），非固定满容量
- **折线右对齐**：最新数据在右，旧数据往左长。避免少量数据铺满整张图
- **图例（HTML）**：在 canvas 上方，彩色圆点 + label + 当前值，不占画图空间
- **高度**：统一 200px（非空图），空数据图卡为 compact（无 canvas）
- **状态徽章**：绿色"N 条" / 灰色"无数据" / 蓝色"采集中"，每轮动态刷新
- **高 DPI**：`canvas.width = clientWidth * devicePixelRatio`

### 5.5 NPU 优先级筛选

NPU 区块标题栏右侧有 `[全部] [高] [中] [低]` 按钮：
- 默认"全部"显示 9 张
- "高/中/低"可多选叠加
- 纯前端 show/hide，不额外请求 API
- 其他区块不受影响

### 5.6 空数据处理

- 硬件不可用 → compact 卡片 + 灰色"无数据"徽章
- 首次加载 → canvas 显示"数据采集中…"
- 快照未就绪 → 503 + 顶栏 banner 提示
- 图卡一旦获得 canvas 不缩回 compact（series 临时消失时显示"采集中"文字）

---

## 6. 与现有 web 的集成

### 6.1 改动文件

| 文件 | 改动 |
|------|------|
| `features/web/server.go` | import + `dfee.Register(mux, ...)` |
| `features/web/main.go` | `metrics.LoadModuleOverride("features/dfee/metrics.yaml")` |
| `features/web/static/app.js` | 导航栏加"能效分析"链接 |

### 6.2 访问入口

- 能效监控页：`http://localhost:9527/dfee/`
- 能效 API：`http://localhost:9527/api/dfee`
- 现有仪表盘：`http://localhost:9527/`（不变）
- 现有导航栏"能效分析"链接 → 跳转 `/dfee/`

### 6.3 共享但不耦合

| 共享资源 | dfee 的使用方式 |
|---------|----------------|
| snapshot.json | 只读（dfee 自带 `readSnapshot`） |
| `/api/refresh` | dfee 前端调现有端点触发采集 |
| `/api/config` | dfee 前端调现有端点读取/设置刷新间隔 |
| metrics 目录 | dfee 通过 `metrics.yaml` override 提升指标优先级 |
| 采集器注册表 | 不使用 |
| health 评分 | 不使用 |

---

## 7. 扩展性设计

| 扩展需求 | 改动位置 | 自动部分 |
|----------|----------|---------|
| 新增能效指标 | efficiencySpecs + chartGroups | 前端自动渲染 |
| 新增图表 | chartGroups | 前端自动多渲染 |
| 新增 CPU 推导指标 | deriveCPUUtil + chartGroups | — |
| 新增 NPU 优先级 | chartGroup.priority | 前端自动出现筛选按钮 |
| 新增 metrics 覆盖 | metrics.yaml | — |

---

## 8. 测试策略

| 测试文件 | 覆盖内容 | 测试数 |
|---------|---------|:---:|
| `filter_test.go` | spec 不变量、全量过滤、label 过滤、空输入、多设备、图表分组、seriesID 稳定性（含 direction 后缀）、单位检测、图表数量(25) | 9 |
| `cpu_derive_test.go` | CPU 时间提取、缺失检测、正常推导、无 prev、零 delta→nil、负 delta 钳零、转换、判断函数 | 8 |
| `handler_test.go` | API 端到端（两次调用验证 CPU 推导+缓存）、503、静态文件 | 3 |

运行：`go test ./features/dfee/`

---

## 9. 已知限制与后续预留

1. **前端历史不持久**：滚动缓冲区纯内存，刷新页面后清空。
2. **多设备图表线多**：8 卡 NPU × 14 利用率指标 = 112 条线在一张图上。后续可加设备选择器或聚合开关。
3. **CPU 推导有状态**：Handler 缓存 prev 值，进程重启后首次数据为零。total=0 时复用缓存值避免图表中断。
4. **无独立配置**：dfee 复用 web 的 config.yaml + runtime.json，刷新间隔由现有 `/api/config` 统一管理。
5. **CPU 负载无单位**：load_average 是无量纲比值，Y 轴不显示单位。

---

## 10. 关键设计决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| dfee 形态 | 独立 Go package + 独立 SPA | 全部代码在 features/dfee/，web 仅几行注册改动 |
| 数据来源 | 复用 snapshot.json（只读过滤） | 零采集改动，零快照改动 |
| CPU 推导位置 | 后端 Handler 有状态 | 推导是业务逻辑，前端只展示 |
| CPU total=0 处理 | 复用 lastDerived 缓存 | 避免图表清空，不出现 0 值凹点 |
| 网络差值位置 | 后端 Handler 有状态 | 累计计数器→增量，与 CPU 推导同模式 |
| 前端图表技术 | Canvas 2D API（无外部库） | 零依赖，多 series 性能优于 SVG |
| 前端历史缓冲 | 内存滚动数组（60 点）右对齐 | 无需后端维护 history，避免少量数据铺满假象 |
| 图例位置 | HTML 元素在 canvas 上方 | 文字清晰，不占画图空间 |
| 图表组织 | 按小节拆分至单位统一 | 每张图 Y 轴有明确单位 |
| 排序 | naturalLess 自然排序 | load_average 1m→5m→15m 正确 |
| 显示名映射 | 按部件:指标名做 key | 区分同名不同部件（cpu:power vs chassis:power） |
| NPU 优先级 | 后端 priority 字段 + 前端筛选按钮 | 高 3 张/中 4 张/低 2 张，按需展示 |
| metrics 覆盖 | features/dfee/metrics.yaml | 22 个 Low 指标覆盖为 Medium，不改默认目录 |
| 页面入口 | /dfee/ 独立 SPA | 不修改 web/static/app.js（仅加导航链接） |

---

*文档版本：v2.0 · 对应指标清单：features/dfee/energy_efficiency_metrics.md（74 项）· 25 张图表*
