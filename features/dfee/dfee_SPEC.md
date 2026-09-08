# dfee 能效监控模块技术规格说明书 (dfee_SPEC)

> **文档定位**：本文档是 dfee 能效监控模块的唯一设计与规格文档。
>
> **对应代码**：`features/dfee/` 目录（Go `package main`，与主项目同一 Go module）。
>
> **独立二进制**：dfee 是独立可执行程序 `catmonitor-dfee`（`make dfee` 构建），只读消费守护进程产生的快照文件。它与 `features/web` 无任何代码耦合（web 不挂载 dfee，二者互不引用），自身不做任何采集。

---

## 1. 概述

### 1.1 目标

提供独立运行的能效监控服务，专门展示 78 项能效指标，以 34 张**实时 Canvas 折线图**按部件分组呈现。核心需求：

1. **只监控能效指标**：从守护进程快照中过滤出 78 项能效相关指标（NPU 50（含 4 项带宽）+ GPU 5 + CPU 10 + Memory 2 + Disk 4 + Network 2 + Chassis 5）。
2. **CPU 时间分解转换**：8 项原始 jiffies 累计值不直接显示，在后端计算为 7 项利用率百分比。
3. **网络字节差值转换**：rx/tx_bytes_total 累计值转换为两次采集间的增量。
4. **分小节实时图表**：每个小节的指标按图表分组呈现，随采集周期自动刷新。混合单位的小节拆分为多张图（每张单位统一）。
5. **设备筛选**：NPU 区块支持 NPU ID + CHIP ID 双维度多选下拉筛选，GPU/磁盘/网络区块各支持单维度筛选。
6. **只读解耦**：dfee 不采集、不写快照，仅读取守护进程的 `snapshot_<comp>.json` + `snapshot.json`。
7. **可选扩展能力**：内置 Prometheus 导出器（`-exporter`）与 CSV 周期落盘（`-csv`），默认关闭。

### 1.2 指标来源

所有 78 项能效指标均为现有 7 个 collector（cpu/memory/disk/network/npu/gpu/chassis）已在采集的指标的**子集**。无需新增采集器，无需修改快照格式。原始指标清单见 `features/dfee/energy_efficiency_metrics.md`；权威过滤集为 `filter.go` 的 `efficiencySpecs`（在原始清单基础上新增 4 项 NPU 带宽指标与 5 项 GPU 指标）。

### 1.3 metrics 作用域（daemon 侧）

守护进程启动时按 `catmonitor.yaml` 的 `features` 列表加载每个 feature 的 `features/<name>/metrics.yaml`：取并集覆盖优先级，并激活 feature 作用域——只有被某个启用 feature 列出**且**优先级 ≥ `collection.min_priority` 的指标才被采集（`cmd/catmonitor/main.go` 的 `loadConfig`）。

dfee 的清单 `features/dfee/metrics.yaml` 共 79 条：

| 部件 | 指标数 | interval |
|------|:---:|:---:|
| npu | 50 | 1s |
| gpu | 5 | 1s |
| cpu | 11 | 1s |
| memory | 2 | 2s |
| disk | 4 | 2s |
| network | 2 | 1s |
| chassis | 5 | 3s |

其中 cpu 的 `online_core_num` 不在图表过滤集内，仅供内置导出器映射为 `node_cpu_cores_online`。

dfee 二进制自身**不读取** metrics.yaml——作用域与采集频率完全由守护进程决定，dfee 只消费快照。

---

## 2. 目录结构

```
features/dfee/
├── dfee_SPEC.md                   # 本设计文档
├── USAGE.md                       # 使用说明（部署 / 配置 / Prometheus+Grafana）
├── energy_efficiency_metrics.md    # 能效指标原始清单（文档）
├── metrics.yaml                   # 指标作用域清单（daemon 加载，79 条）
├── main.go                        # 独立二进制入口：flags + HTTP 服务 + 导出器/CSV 可选启动
├── handler.go                     # /api/dfee API + SPA 静态服务 + CPU 推导/网络差值状态
├── filter.go                      # 78 项过滤集 + 34 张图表分组 + seriesID/label + naturalLess
├── cpu_derive.go                  # CPU 8 项 jiffies → 7 项利用率推导
├── net_derive.go                  # 网络累计字节 → 两次采集差值
├── exporter.go                    # 内置 Prometheus 导出器（node_*/dsmi_*/ipmi_*）
├── static_info.go                 # 硬件/软件静态信息采集（启动时一次，供导出器/CSV）
├── csv_writer.go                  # 周期 CSV 持久化
├── embed.go                       # //go:embed static
├── grafana-dashboard.json         # Grafana 仪表盘模板（6 行 24 个数据面板）
├── filter_test.go                 # 过滤逻辑测试（9 个）
├── cpu_derive_test.go             # CPU 推导测试（8 个）
├── handler_test.go                # HTTP 端到端测试（3 个）
├── csv_writer_test.go             # CSV 格式测试（4 个）
└── static/
    ├── index.html                 # 能效监控 SPA 页面壳
    ├── dfee.js                    # 实时图表渲染 + 轮询 + 筛选 + 布局自定义
    └── dfee.css                   # 样式
```

### 与守护进程的关系

```
CATMonitor (Go module)
├── cmd/catmonitor/               # 守护进程（唯一快照生产者）
├── features/dfee/                # 能效监控独立二进制（本模块，只读消费者）
├── features/web/                 # web 仪表盘（独立二进制，与 dfee 无代码耦合）
├── features/snapshot/            # 快照读写库（dfee 与 daemon 共享）
└── internal/                     # 采集器/来源层/metrics 目录（不修改）
```

---

## 3. 架构与数据流

### 3.1 数据流

```
守护进程 catmonitor（snapshot.enabled: true，features 含 dfee）
  采集 → feature 作用域过滤 → snapshot.dir 下的
         snapshot_<comp>.json + snapshot.json
        │
        ↓ 只读（-snapshot-dir 指向同一目录）
  catmonitor-dfee（独立二进制，默认 :19323）
    ├─ GET /api/dfee → 过滤 78 能效指标
    │                   → CPU 8 jiffies → 7 利用率推导（缓存）
    │                   → 网络累计 → 差值
    │                   → 按 34 张图表分组 → SPA 轮询 + Canvas 实时折线图
    ├─ GET :9333/metrics（可选 -exporter enabled）
    │                   → node_* / dsmi_* / ipmi_* + static_*_info
    └─ CSV 周期落盘（可选 -csv enabled）
```

### 3.2 解耦边界

| 边界 | 说明 |
|------|------|
| dfee ← 快照目录 | 只读 `snapshot_<comp>.json` + `snapshot.json`，不调采集器，不改快照格式 |
| dfee → 前端 | 独立 SPA（`/` 与 `/dfee/` 均可进入），不复用 features/web 的任何代码 |
| dfee ↔ daemon | 接触点仅两个：`snapshot.dir` 目录约定 + features 列表触发的 metrics.yaml 作用域 |
| dfee ↔ web | 无接触点（web 不挂载 dfee 路由，dfee 不引用 web 代码） |

### 3.3 部署与运行

**构建**：`make dfee` → `bin/catmonitor-dfee`（纯 Go，无 CGo，web/dfee 均不需要 dcmi tag）。

**前提**：守护进程需配置 `snapshot.enabled: true` 且 `snapshot.dir` 与 dfee 的 `-snapshot-dir` 一致；`features` 列表需含 `dfee`（否则能效指标不在采集作用域内）。

**命令行 flags**（`main.go`）：

| flag | 默认值 | 说明 |
|------|--------|------|
| `-addr` | `:19323` | 监听地址；端口被占用时自动 +1 重试 |
| `-snapshot-dir` | `/var/lib/catmonitor/snapshot` | 守护进程快照目录（须与 catmonitor.yaml 的 snapshot.dir 一致） |
| `-exporter` | `disabled` | 内置 Prometheus 导出器开关（enabled/disabled） |
| `-exporter-port` | `9333` | 导出器监听端口 |
| `-device` | 空 | NPU 设备过滤（逗号分隔，如 `0,1`；空 = 全部） |
| `-docker-container` | 空 | 软件版本信息经 `docker exec <容器>` 采集 |
| `-csv` | `disabled` | CSV 落盘开关（enabled/disabled） |
| `-csv-dir` | `/var/lib/catmonitor/csv` | CSV 输出目录 |
| `-csv-interval` | `10s` | CSV 写入周期 |
| `-max-runtime` | `0` | 最长运行时长（如 `10m`、`1h`；0 = 一直运行） |

**HTTP 路由**（`handler.go` 的 `Register`）：

| 路由 | 说明 |
|------|------|
| `/api/dfee` | 数据 API（GET，JSON） |
| `/dfee/` | SPA 页面 |
| `/` | SPA 根入口（catch-all，直接返回 index.html；静态资源以 `/dfee/static/...` 绝对路径引用，两个入口均可工作） |
| `/dfee/static/` | 静态资源（embed 内嵌） |

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
| NPU (50) | 无 | 频率 7 + 利用率 14 + 温度 14 + 电压/功耗 7 + 风扇 1 + LLC 3 + 带宽 4；所有设备实例均包含（npu_id/chip_id 标签保留） |
| GPU (5) | 无 | power_draw, utilization, temperature, memory_usage, clock_frequency；gpu_id 标签保留 |
| CPU 时间 (8) | `core=total` | 仅取聚合值，排除 per-core |
| CPU 负载 (1) | 无 | 所有 interval（1m/5m/15m）均包含 |
| CPU 功耗 (1) | 无 | 所有 socket 均包含 |
| Memory usage_detail (5 个 field) | `field∈{total,free,buffers,cached,sreclaimable}` | 排除 used/available 等 |
| Memory swap_detail (2 个 field) | `field∈{total,free}` | 排除 used |
| Disk (4) / Network (2) / Chassis (5) | 无 | 所有实例均包含 |

Memory 两类按 field 过滤，实际放行 7 个 field 实例；过滤集共 78 个 spec 条目。

### 4.2 图表分组定义（filter.go）

#### 4.2.1 chartGroup 结构

```go
type chartGroup struct {
    id          string
    title       string
    component   string
    metricNames []string
    labelKey    string // 可选：按此 label key 过滤（触发简化标签）
    labelVal    string // 可选：匹配此 label 值（空值=不过滤，只触发简化标签）
    priority    string // "high" / "medium" / "low" / ""（当前全部为空）
    aggregate   string // "avg" = 聚合为单序列均值；"" = 按实例逐条
}
```

#### 4.2.2 34 张图表完整清单

| # | 图表 ID | 标题 | 部件 | 指标 | 筛选 / 聚合 |
|---|---------|------|------|------|------------|
| 1 | npu_aicore_freq | AICore频率 | npu | aicore_freq | npu_id |
| 2 | npu_hbm_freq | HBM频率 | npu | hbm_freq | npu_id |
| 3 | npu_power_draw | NPU功耗 | npu | power_draw | npu_id |
| 4 | npu_voltage | NPU电压 | npu | voltage | npu_id |
| 5 | npu_npu_util | NPU利用率 | npu | npu_util | npu_id |
| 6 | npu_utilization | AICore利用率 | npu | utilization | npu_id |
| 7 | npu_vector_core_util | Vector Core利用率 | npu | vector_core_util | npu_id |
| 8 | npu_hbm_bandwidth_util | HBM带宽利用率 | npu | hbm_bandwidth_util | npu_id |
| 9 | npu_memory_usage | HBM利用率 | npu | memory_usage | npu_id |
| 10 | npu_hccs_tx_bw | HCCS带宽(发送) | npu | hccs_tx_bandwidth | npu_id |
| 11 | npu_hccs_rx_bw | HCCS带宽(接收) | npu | hccs_rx_bandwidth | npu_id |
| 12 | npu_pcie_tx_bw | PCIe带宽(发送) | npu | pcie_tx_bandwidth | npu_id |
| 13 | npu_pcie_rx_bw | PCIe带宽(接收) | npu | pcie_rx_bandwidth | npu_id |
| 14 | gpu_power_draw | GPU功耗 | gpu | power_draw | gpu_id |
| 15 | gpu_utilization | GPU利用率 | gpu | utilization | gpu_id |
| 16 | gpu_temperature | GPU温度 | gpu | temperature | gpu_id |
| 17 | gpu_memory_usage | GPU显存利用率 | gpu | memory_usage | gpu_id |
| 18 | gpu_clock_frequency | GPU频率 | gpu | clock_frequency | gpu_id |
| 19 | cpu_utilization | CPU 利用率 | cpu | 7 项推导值 | — |
| 20 | cpu_load | CPU 负载 | cpu | load_average（3 个 interval） | — |
| 21 | cpu_power | CPU 功耗 | cpu | power | — |
| 22 | memory_pool | 内存池 | memory | usage_detail（5 个 field） | — |
| 23 | memory_swap | Swap | memory | swap_detail（2 个 field） | — |
| 24 | disk_throughput_read | 磁盘吞吐量(读) | disk | throughput | direction=read |
| 25 | disk_throughput_write | 磁盘吞吐量(写) | disk | throughput | direction=write |
| 26 | disk_iops_read | IOPS(读) | disk | iops | direction=read |
| 27 | disk_iops_write | IOPS(写) | disk | iops | direction=write |
| 28 | disk_read_latency | 磁盘读耗时 | disk | read_latency | device（简化标签） |
| 29 | disk_write_latency | 磁盘写耗时 | disk | write_latency | device（简化标签） |
| 30 | network_rx | 网络接收 | network | rx_bytes_total | interface（简化标签） |
| 31 | network_tx | 网络发送 | network | tx_bytes_total | interface（简化标签） |
| 32 | chassis_power | 整机功耗 | chassis | power | — |
| 33 | chassis_temp | 机箱温度 | chassis | inlet_temp + outlet_temp | — |
| 34 | chassis_fan | 机箱风扇转速 | chassis | fan_speed | 聚合：全部风扇平均为单序列"平均转速" |

> 图表 19 (CPU 利用率) 的 7 项指标是后端推导的，原始 8 项 jiffies 不出现在 API 响应中。
> 图表 30-31 (网络接收/发送) 的值是两次采集的差值，不是累计绝对值。
> Y 轴单位由 `dominantUnit` 按 series 实际单位自动判定（混合单位时为空），前端拼进卡片标题。
> 过滤集 78 项中其余 38 项（NPU 温度/电压细分/AICPU 等利用率/LLC/ACG/风扇 + chassis fan_power）由 metrics.yaml 照常采集并可通过导出器输出，但当前未挂接图表。
> 机箱功耗图仅含 power 一项；fan_power 作为独立能效指标保留在过滤集中。

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
    dir         string             // 快照目录
    mu          sync.Mutex
    prevCPU     cpuTimeSnapshot    // 上周期 8 项累计值
    hasPrev     bool
    lastDerived []derivedMetric    // 缓存上次非零推导结果
    prevNet     map[string]float64 // 上周期网络累计字节
    hasPrevNet  bool
}
```

**total=0 缓存策略**：当 `total_delta = 0`（如同一 snapshot 被读两次），`deriveCPUUtil` 返回 nil，Handler 复用 `lastDerived` 缓存值。series 持续存在，前端缓冲区不被清空，不出现 0 值凹点。

首次调用（无 prev + 无缓存）→ 不产出指标，图显示"无数据"。

### 4.4 网络字节差值（net_derive.go）

rx_bytes_total / tx_bytes_total 是累计计数器。Handler 按 seriesID 缓存上一次值，每次 API 调用计算 `delta = curr - prev`（负值钳零，计数器重置时）。首次调用返回 0。

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

当 chartGroup 的 `labelKey != ""` 时（如磁盘 `device`、网络 `interface`、NPU `npu_id`、GPU `gpu_id`），图例只返回设备标识（如 `sda`、`eth0`、`NPU 0 Chip 1`、`GPU 0`），不拼指标名和部件前缀。因为图表标题已含指标名和方向。

#### 4.5.3 自然排序

`groupForChart` 使用 `naturalLess` 排序（数字段按数值比较），确保 `load_average:1m < 5m < 15m`，而非 ASCII 的 `15m < 1m < 5m`。

### 4.6 API 端点（handler.go）

**GET /api/dfee**：读快照目录（全局 snapshot.json 取 session/时间戳/刷新间隔，拼接所有 snapshot_<comp>.json 的指标）→ 过滤 → CPU 推导（带缓存）→ 网络差值 → 按图表分组 → 返回 JSON。

响应结构：

```json
{
  "session_id": "…",
  "version": "0.3.5",
  "timestamp": "2026-09-08T14:30:00+08:00",
  "refresh_interval_ms": 5000,
  "charts": [
    {
      "id": "npu_aicore_freq",
      "title": "AICore频率",
      "y_unit": "MHz",
      "series": [
        { "id": "0:0:aicore_freq", "label": "NPU 0 Chip 0", "value": 1200, "unit": "MHz" }
      ]
    },
    {
      "id": "cpu_utilization",
      "title": "CPU 利用率",
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
- NPU：`{npu_id}:{chip_id}:{metric_name}`（如 `0:0:aicore_freq`；无 chip_id 标签时为 `{npu_id}::{metric_name}`）
- GPU：`{gpu_id}:{metric_name}`（如 `0:power_draw`）
- 网络：`{interface}:{metric_name}`（如 `eth0:rx_bytes_total`）
- 磁盘：`{device}:{metric_name}`（如 `sda:throughput`；带 direction 时追加 `:{direction}`，如 `sda:throughput:read`）
- 机箱风扇：`{fan}:{metric_name}`
- Memory：`{metric_name}:{field}`（如 `usage_detail:total`）
- CPU 负载：`{metric_name}:{interval}`（如 `load_average:1m`）
- CPU 推导：`{derived_name}`（如 `idle_util`）
- 无标签：`{metric_name}`

快照未就绪返回 503（`{"error":"snapshot not ready"}`）。

---

## 5. 前端设计

### 5.1 页面结构

顶栏：左上角品牌区（CATMonitor 链接 + 副标题，副标题由 API 返回的 version 动态刷新为 `v<版本> · 能效监控`）；右上角更新时间、刷新间隔、**重置布局**按钮、**立即刷新**按钮。

主体按部件分区块，每区块有彩色标题 + 可用数（`N/M 可用`）+ 筛选下拉：

| 区块 | 图表数 | 网格 | 筛选下拉 |
|------|:---:|:---:|----------|
| NPU | 13 | 6 列 | NPU ID + CHIP ID（双多选） |
| GPU | 5 | 3 列 | GPU ID（多选） |
| CPU | 3 | 默认 | — |
| 内存 | 2 | 默认 | — |
| 磁盘 | 6 | 2 列 | DISK（多选） |
| 网络 | 2 | 2 列 | NIC（多选） |
| 机箱 | 3 | 3 列 | — |

**区块自动隐藏**：某区块所有图表均无数据（如机器无 NPU/GPU/机箱硬件）时，该区块整体不渲染（`if (available === 0) continue;`），不显示"0/N 可用"的空架子。

### 5.2 数据获取与轮询

- 启动时 fetch `/api/dfee` → 获取 `refresh_interval_ms` → 按该间隔轮询（首次立即执行）
- 每周期：`updateBuffers`（在 `buildSections` 之前）→ 重建检测（图表集合变化、或图表从空到有数据）→ `renderAllCharts`
- 立即刷新按钮：约 400ms 后重新拉取一次 `/api/dfee`（纯前端行为；dfee 服务端只提供 GET 数据接口，无服务端触发端点）
- 刷新间隔来自全局快照的 `refresh_interval_ms`（即守护进程采集周期），前端只读显示
- 服务重启检测：`session_id` 变化（守护进程重启）→ 自动清空本地布局/筛选缓存

### 5.3 滚动缓冲区

前端维护每个 series 的内存滚动数组（60 点）。`updateBuffers` 在 `buildSections` 之前执行，确保 `buildCard` 检查 buffer 时已有当前数据。消失的 series 缓冲区被自动清理。

### 5.4 Canvas 实时图表渲染

每张图用一个 `<canvas>`，纯 Canvas 2D API：

- **Y 轴**：全局 min/max，4 条网格线，每条带缩写标签（K/M/G/T）+ 单位
- **Y 轴近平坦**：当数据变化幅度 <1%（如累计计数器），Y 轴从 0 展开
- **X 轴**：标签反映实际数据时长（`-5s` → `-5min`），非固定满容量
- **折线右对齐**：最新数据在右，旧数据往左长。避免少量数据铺满整张图
- **图例（HTML）**：在 canvas 上方，彩色圆点 + label + 当前值，点击可隐藏/恢复单条 series，不占画图空间
- **高度**：默认 200px（可拖拽调整，120–500px），空数据图卡为 compact（无 canvas）
- **状态徽章**：绿色"N 条" / 灰色"无数据" / 蓝色"采集中"，每轮动态刷新
- **高 DPI**：`canvas.width = clientWidth * devicePixelRatio`

### 5.5 设备筛选

多选下拉（checkbox dropdown），选项从各区块 series.id 的对应段自动提取：

- NPU 区块：**NPU ID** 与 **CHIP ID** 两个维度可同时过滤（对应 series.id 前两段）
- GPU / 磁盘 / 网络区块：各一个维度（GPU ID / 设备名 / 网卡名）
- 纯前端 show/hide，不额外请求 API；全选等价于不过滤
- 选择持久化到 localStorage；图例点击隐藏单条 series；重置布局按钮一并清空

`chartGroup.priority` 字段与前端 全部/高/中/低 筛选按钮机制保留，但当前无图表设置 priority，筛选栏不显示。

### 5.6 布局自定义

- **卡片拖拽排序**：拖动卡片标题栏可在同区块内重排，顺序持久化
- **卡片缩放**：右下角缩放手柄，横向按网格列 span、纵向调整高度（120–500px），带对齐辅助线，持久化
- **区块折叠**：点击区块标题折叠/展开，状态持久化
- **重置布局**：清空以上全部本地布局与筛选，恢复默认
- 守护进程重启（session_id 变化）时自动重置

### 5.7 空数据处理

- 硬件不可用 → 该区块全部图表无数据 → 区块自动隐藏（见 5.1）
- 单张图无数据 → compact 卡片 + 灰色"无数据"徽章
- 首次加载 → 蓝色"采集中"徽章（有 series 但缓冲区尚无数据，如 IOPS/差值类需前一周期）
- 快照未就绪 → 503 + 顶栏 banner 提示
- 图卡一旦获得 canvas 不缩回 compact（series 临时消失时显示"采集中"文字）

---

## 6. 内置 Prometheus 导出器（exporter.go + static_info.go）

`-exporter enabled` 时在 `-exporter-port`（默认 9333）提供 `GET /metrics`，将快照指标映射为 Prometheus 文本格式。零外部依赖（自实现文本暴露格式，标签按 key 排序，不输出 HELP/TYPE 头）。

### 6.1 动态指标映射

| 前缀 | 来源部件 | 映射示例 |
|------|---------|---------|
| `node_*` | cpu / memory / network / disk | `node_cpu_seconds_total{mode}`、`node_cpu_cores_online`、`node_load1/5/15`、`node_memory_*_bytes`（MB→bytes）、`node_network_receive/transmit_bytes_total{interface}`、`node_disk_read/written_sectors_total{device}`、`node_disk_read/write_time_seconds_total{device}` |
| `dsmi_*` | npu | `dsmi_aicore_current_frequency_hz`、`dsmi_hbm_frequency_hz`、`dsmi_power_w`、`dsmi_voltage_mv`、`dsmi_aicore_utilization_percent`、`dsmi_hbm_utilization_percent`、`dsmi_hbm_bandwidth_utilization_percent`、`dsmi_vector_utilization_percent`、`dsmi_npu_utilization_percent`（均带 `npu_id` + `chip_id` 标签） |
| `ipmi_*` | chassis | `ipmi_power_w`、`ipmi_inlet_temp_celsius`、`ipmi_outlet_temp_celsius`、`ipmi_fan_power_w`、`ipmi_fan_speed_rpm{fan_id}` |

- `-device 0,1`：仅导出指定 npu_id 的 dsmi_* 指标
- `supplementDiskStats`：直接读 `/proc/diskstats`，为快照未覆盖的设备（dm-*、分区等）补全 `node_disk_*` 四项计数器

### 6.2 静态信息（启动时一次采集，缓存复用）

| 指标 | 标签 | 采集方式 |
|------|------|---------|
| `static_hardware_info` | product_name / cpu_info / memory_info / disk_info / gpu_type / npu_chip_name / psu_info | `ipmitool fru print`、`lscpu`（Socket*Model）、`dmidecode -t 17`（Count*Type Size）、`lsblk -d -o NAME,SIZE -n`、`nvidia-smi --query-gpu=name`、`npu-smi info`、`ipmitool fru print`（PSU 计数） |
| `static_software_info` | os_version / npu_driver_version / npu_firmware_version / cann_version / python_version / torch / torch_npu / transformers / mindspeed / vllm / vllm_ascend / sglang / mindie / verl / verl_npu / gpu_driver_version / cuda_version | `/etc/os-release`、`/usr/local/Ascend/{driver,firmware}/version.info`、CANN `ascend_toolkit_install.info`（aarch64-linux 与 x86_64-linux 路径）、`python -V`、`pip list`、`nvcc --version`、`nvidia-smi --query-gpu=driver_version` |

- 命令或文件缺失 → 对应标签为空字符串（优雅降级，不报错）
- `-docker-container <name>`：CANN 版本、python、pip 包版本等改经 `docker exec <name>` 在容器内采集

---

## 7. CSV 持久化（csv_writer.go）

`-csv enabled` 时按 `-csv-interval`（默认 10s）周期落盘，数据源复用导出器的 `buildMetrics()`（与 `/metrics` 完全一致）：

- 文件名：`dfee_metrics_<启动时间yyyyMMdd_HHmmss>.csv`，写入 `-csv-dir`（默认 `/var/lib/catmonitor/csv`），启动即写一次
- 表头：`timestamp,metric_name,labels,value`；labels 格式 `key="value",key2="value2"`（按 key 排序，无标签为空）
- 标准 CSV 转义（含逗号/引号/换行的字段加双引号，内部引号翻倍）
- 数值格式：整数直出；绝对值 ≥1e11 用科学计数（`1.52204E+11`）；非整数用紧凑小数

---

## 8. Grafana 仪表盘（grafana-dashboard.json）

内置模板 **CATMonitor dfee Exporter Dashboard**：6 个行分组（CPU、内存、网络、磁盘、NPU、机箱）共 24 个数据面板（23 个 timeseries + 1 个 stat），默认刷新 10s。导入 Grafana 后抓取 dfee 导出器（`:9333/metrics`）即可获得持久化可视化，与 SPA 实时视图互补。配置步骤见 `USAGE.md`。

---

## 9. 扩展性设计

| 扩展需求 | 改动位置 | 自动部分 |
|----------|----------|---------|
| 新增能效指标（仅采集/导出） | efficiencySpecs + metrics.yaml | — |
| 新增图表 | chartGroups + dfee.js SECTIONS | 序列渲染、筛选下拉选项 |
| 新增 CPU 推导指标 | deriveCPUUtil + chartGroups | — |
| 新增导出映射 | exporter.go 的 map* 函数 | — |
| 新增设备筛选维度 | seriesID 分段 + dfee.js filterKey | 下拉选项自动提取 |
| 新增 metrics 覆盖 | metrics.yaml（daemon 侧生效） | — |

---

## 10. 测试策略

| 测试文件 | 覆盖内容 | 测试数 |
|---------|---------|:---:|
| `filter_test.go` | spec 不变量、全量过滤、label 过滤、空输入、多设备、图表分组、seriesID 稳定性（NPU chip_id / direction 后缀）、单位检测、图表数量(34) | 9 |
| `cpu_derive_test.go` | CPU 时间提取、缺失检测、正常推导、无 prev、零 delta→nil、负 delta 钳零、转换、判断函数 | 8 |
| `handler_test.go` | API 端到端（两次调用验证 CPU 推导+缓存、34 张图表、原始 jiffies 不泄漏）、503、SPA 静态文件 | 3 |
| `csv_writer_test.go` | 数值格式、标签格式、CSV 转义、文件输出 | 4 |

运行：`go test ./features/dfee/`（共 24 个测试）。

---

## 11. 已知限制与后续预留

1. **前端历史不持久**：滚动缓冲区纯内存，刷新页面后清空。持久化需求由导出器 + Prometheus + Grafana 承担。
2. **多设备图表线多**：已通过 NPU ID / CHIP ID / GPU / DISK / NIC 多选筛选缓解；仍可考虑聚合开关。
3. **CPU 推导有状态**：Handler 缓存 prev 值，进程重启后首次数据为零。total=0 时复用缓存值避免图表中断。
4. **无独立配置文件**：全部行为由命令行 flags 控制；刷新间隔跟随守护进程采集周期，dfee 不可调整。
5. **CPU 负载无单位**：load_average 是无量纲比值，Y 轴不显示单位。
6. **导出器无 HELP/TYPE 头**：文本暴露格式仅输出数据行（按当前需求）。

---

## 12. 关键设计决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| dfee 形态 | 独立二进制 `catmonitor-dfee`（`make dfee`） | 只读消费者与采集守护进程生命周期解耦，可与 web 各自独立部署 |
| 数据来源 | 守护进程快照目录（只读过滤） | 零采集改动，零快照改动 |
| 默认端口 | `:19323`（占用自动 +1）；导出器 `:9333` | 与 web(:19322) 相邻便于记忆，且避开 faultsub(:9101) 等其它组件 |
| CPU 推导位置 | 后端 Handler 有状态 | 推导是业务逻辑，前端只展示 |
| CPU total=0 处理 | 复用 lastDerived 缓存 | 避免图表清空，不出现 0 值凹点 |
| 网络差值位置 | 后端 Handler 有状态 | 累计计数器→增量，与 CPU 推导同模式 |
| 前端图表技术 | Canvas 2D API（无外部库） | 零依赖，多 series 性能优于 SVG |
| 前端历史缓冲 | 内存滚动数组（60 点）右对齐 | 无需后端维护 history，避免少量数据铺满假象 |
| 图例位置 | HTML 元素在 canvas 上方 | 文字清晰，不占画图空间，可点击隐藏 |
| 图表组织 | 按小节拆分至单位统一 | 每张图 Y 轴有明确单位 |
| 排序 | naturalLess 自然排序 | load_average 1m→5m→15m 正确 |
| 显示名映射 | 按部件:指标名做 key | 区分同名不同部件（cpu:power vs chassis:power） |
| 设备筛选 | series.id 分段解析 + 多选下拉 | NPU 双维度（ID+Chip），纯前端零请求 |
| 布局持久化 | localStorage（拖拽/缩放/折叠/筛选） | 刷新页面不丢失；session 变化自动重置 |
| 区块自动隐藏 | 0 可用即不渲染 | 无 NPU/GPU/机箱硬件时不留空架子 |
| 内置导出器 | 自实现文本格式，零外部依赖 | 快照→node_*/dsmi_*/ipmi_* 直接可用，附带静态信息 |
| CSV 输出 | 复用导出器 buildMetrics | 与 /metrics 口径一致，Grafana/离线分析可互换 |
| metrics 作用域 | daemon 按 features 列表加载 metrics.yaml | 79 条清单联合 min_priority 前置过滤，不碰硬件 |
| 页面入口 | `/` 与 `/dfee/` 均可 | 静态资源绝对路径引用，双入口等价 |

---

*文档版本：v0.3.6 · 能效指标 78 项 · 实时图表 34 张*
