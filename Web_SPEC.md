# CATMonitor Web 技术规格说明书 (Web_SPEC)

> **文档定位**：本文档是 CATMonitor Web 仪表盘的**唯一设计与规格文档**，描述当前实现的真实状态，并明确为未来"新增部件 / 新增采集指标"预留的扩展点。后续开发以本文档为准。
>
> **对应代码**：`web/` 目录（与主项目同一 Go module，不新增 go.mod）。
> **不改动主项目任何现有文件**：与 `cmd/catmonitor`、`internal/collectors`、`internal/health`、`internal/storage`、`internal/config`、`internal/platform` 解耦，仅通过只读复用（blank import + 调用注册表/健康度接口）获取数据。

---

## 1. 概述

### 1.1 目标

提供一个 Web 仪表盘，可视化单台服务器的健康度与各部件采集指标，支持可配置刷新间隔。设计原则：

1. **解耦**：Web 服务与现有采集守护进程/CLI 完全解耦，不修改任何主项目文件。
2. **多页面**：概览页（整体健康度 + 各部件关键指标）+ 各部件详情页（详细指标 + 趋势）。
3. **可扩展**：新增部件类型 / 采集指标时，尽可能自动出现，零代码或仅需一处一行的新增。
4. **极简依赖**：Go 标准库 + 已有 `gopkg.in/yaml.v3`，前端原生 HTML/CSS/JS，无构建步骤，零新依赖。

### 1.2 架构总览

单一 Go 二进制 `catmonitor-web`，内含两个角色，以 `web/data/snapshot.json` 为解耦边界：

```
┌──────────────────── catmonitor-web (单二进制) ────────────────────┐
│                                                                    │
│  采集 goroutine (DataCollector)          HTTP server (net/http)     │
│    定时: 遍历注册表 → Collect()            静态页 + REST API          │
│         → health.Evaluate()                  读取 snapshot.json      │
│         → 原子写 snapshot.json                  ↑                   │
│                  │写                              │读（不调采集器）     │
│                  └──────── snapshot.json ────────┘                  │
│                                  ↑热更新间隔                          │
└────────────────────────────────────────────────────────────────────┘
                  ↑ fetch /api/snapshot (setInterval)
            浏览器（SPA：概览 + 各部件详情页）
```

**解耦边界**：HTTP 层**只读** `snapshot.json`，**绝不直接调用采集器**；采集 goroutine 是 `snapshot.json` 的**唯一写者**（写临时文件 + `os.Rename` 原子写，读者永不会读到半截文件）。

### 1.3 技术栈

| 项目 | 选型 |
|------|------|
| 后端语言 | Go 1.23.4（沿用主项目 go.mod） |
| HTTP | Go 标准库 `net/http` |
| 配置 | `gopkg.in/yaml.v3`（已有依赖，无新增） |
| 前端 | 原生 HTML5 + CSS + 原生 JS（ES2015+），无框架、无构建步骤 |
| 前端打包 | `//go:embed static` 内嵌进二进制，单文件部署 |
| 图表 | 手写内联 SVG sparkline（~30 行），无图表库 |
| 进程托管 | systemd 临时 unit（可选），支持信号优雅退出 |

---

## 2. 目录结构

```
web/
├── main.go            # 入口：blank-import 采集器 + 起采集 goroutine + HTTP server + 信号处理
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
└── data/              # 运行时数据（运行时生成，不应提交到 git）
    ├── snapshot.json  # 采集 goroutine 写，HTTP 层读
    └── runtime.json   # 界面调整的刷新间隔持久化
```

> `web/data/` 由程序运行时创建（`os.MkdirAll`），无需预先存在；git 不跟踪空目录，运行时文件应被 gitignore（见 §10）。

---

## 3. 配置设计

### 3.1 配置文件 `web/config.yaml`

```yaml
server:
  addr: ":9527"                # 监听地址（端口被占用时自动 +1 递增直到空闲，见 §8.5）
collector:
  refresh_interval: 5s         # 采集周期（也作为前端默认轮询间隔）
  history_points: 60           # 环形历史保留的采样点数
  # enabled_components: []     # 空 = 采集全部已注册部件；指定则只采集列出的部件
storage:
  snapshot_path: web/data/snapshot.json   # 快照文件
  runtime_path:  web/data/runtime.json   # 运行时覆盖持久化
```

### 3.2 配置加载优先级（`config.go`）

1. `DefaultConfig()` 提供默认值（addr `:9527`，5s，60 点，全部件启用，相对路径 `web/data/...`）。
2. 若配置文件存在，YAML 覆盖默认值；**文件不存在则静默用默认值**（与主项目 `internal/config` 行为一致，不报错）。
3. 若 `runtime.json` 存在，其 `refresh_interval_ms` 再覆盖采集周期（界面调整持久化，重启后保留）。
4. 配置文件路径由 `-config` 命令行 flag 指定，默认 `web/config.yaml`。

> 端口占用回退：启动时 `net.Listen` 探测 `server.addr`，若返回 `EADDRINUSE`（端口被占用）则端口 +1 重试（`:9527`→`:9528`→`:9529`…），直至获取可用端口，实际绑定地址回写 `cfg.Server.Addr` 并打印 warn 日志。非 `EADDRINUSE` 错误（如权限不足）直接失败退出。详见 §8.5。

> 字段类型：`refresh_interval` 为 `time.Duration`（已验证 `yaml.v3` 可直接解析 `5s`）。`enabled_components` 为字符串数组，空/缺省 = 全部。

### 3.3 运行时覆盖 `runtime.json`

界面改刷新间隔 → `POST /api/config` → 更新内存间隔 + 调 `DataCollector.SetInterval` 热生效 + 原子写 `runtime.json`。下次启动时由步骤 3 自动加载。

---

## 4. 数据模型

### 4.1 `Snapshot` 结构（`snapshot.go`，HTTP 层唯一数据源）

```json
{
  "timestamp": "2026-07-13T14:47:55+08:00",
  "refresh_interval_ms": 5000,
  "history_points": 60,
  "health": {
    "score": 100,
    "grade": "Excellent",
    "server_type": "cpu_only",
    "components": {
      "cpu": {"score": 30, "max": 30, "deductions": null},
      "memory": {"score": 40, "max": 40, "deductions": null},
      "disk": {"score": 30, "max": 30, "deductions": null}
    }
  },
  "metrics": [
    {"component":"cpu","name":"usage","value":12.3,"unit":"%","labels":{"core":"total"},"timestamp":"..."}
  ],
  "history": {
    "cpu_usage": [12.3, 13.1, ...],
    "memory_usage": [29.9, 30.1, ...],
    "disk_space_usage": [0.23, ...],
    "cpu_load_average": [1.41, ...],
    "memory_swap_usage": [0.0, ...]
  },
  "specs": [
    {"component":"system","name":"device_model","value":1,"labels":{"manufacturer":"...","product_name":"..."},"timestamp":"..."},
    {"component":"cpu","name":"model_info","value":48,"unit":"cores","labels":{"model_name":"Intel(R) Xeon(R) ..."},"timestamp":"..."},
    {"component":"disk","name":"disk_info","value":476.9,"unit":"GB","labels":{"device":"sda","model":"..."},"timestamp":"..."}
  ]
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `timestamp` | time | 本次快照生成时间 |
| `refresh_interval_ms` | int | 当前生效的采集周期（毫秒），供前端轮询对齐 |
| `history_points` | int | 历史环形缓冲容量 |
| `health` | `health.HealthScore` | 健康度结果（直接复用 `internal/health` 的序列化） |
| `metrics` | `[]collector.Metric` | 本次采集的全部指标（复用 `internal/collector.Metric`） |
| `history` | `map[string][]float64` | 趋势序列，key 形如 `<component>_<suffix>`，供详情页按部件前缀过滤 |
| `specs` | `[]collector.Metric` | 静态设备规格（一次性身份信息），见 §5.4。`omitempty`：无任何静态指标时不出现在 JSON 中 |

> `health` 与 `metrics` 直接使用主项目的结构体 JSON tag，**不重新定义**，保证与采集器/健康度模块的契约一致。

### 4.2 原子读写（`snapshot.go`）

- `WriteAtomic(path, *Snapshot)`：`json.MarshalIndent` → 写同目录临时文件 `.snapshot-*.tmp` → `os.Rname` 覆盖目标。读者只见完整文件。
- `Read(path)`：`os.ReadFile` + `json.Unmarshal`。

---

## 5. 采集与历史（`collector.go`）

### 5.1 DataCollector 职责

- 持有配置、`slog.Logger`、环形历史 `map[string][]float64`、当前间隔、reload/collectNow 通道。
- `Run(ctx)`：立即采集一次，进入 `select` 循环：定时器到期 → 采集；`reload` 通道 → 重置定时器（间隔热更新）；`collectNow` 通道 → 立即采集（供 `/api/refresh`）；`ctx.Done` → 退出。
- `collectOnce()`：遍历 `collector.DefaultRegistry.All()`，对启用的采集器调 `Collect()` → `health.NewEvaluator(health.GetScheme("auto")).Evaluate(allMetrics)` → 组装 `Snapshot`（含 `updateHistory` 结果）→ `WriteAtomic`。
- `SetInterval(d)`：更新内存间隔 + 非阻塞通知 reload 通道。
- `CollectNow()`：非阻塞通知 collectNow 通道（**串行**触发，避免与主循环并发写历史）。

### 5.2 健康度方案自动检测

传入 `"auto"` scheme：`health.Evaluate()` 内部检测到 `gpu` 或 `npu` 指标时自动切换到加速卡方案（CPU 10/Mem 20/Disk 10/GPU 60），否则 CPU-only（CPU 30/Mem 40/Disk 30）。**无需 Web 侧任何配置**。

### 5.3 历史趋势：`trackedSeries`（扩展核心）

历史序列由一个**可扩展的 spec 列表**驱动——这是新增趋势 sparkline 的唯一入口：

```go
type seriesSpec struct {
    component string
    name      string
    labelKey  string   // 可选标签过滤（"" = 任意）
    labelVal  string
    key       string   // 必须为 "<component>_<suffix>"，供详情页按部件前缀过滤
    mode      int      // 0 = 取首个匹配，1 = 取所有匹配的最大值
}
```

当前已跟踪序列：

| key | component | name | 过滤 | mode | 说明 |
|-----|----------|------|------|------|------|
| `cpu_usage` | cpu | usage | core=total | first | CPU 总使用率 |
| `cpu_load_average` | cpu | load_average | interval=1m | first | 1 分钟负载 |
| `memory_usage` | memory | usage | — | first | 内存使用率 |
| `memory_swap_usage` | memory | swap_usage | — | first | Swap 使用率 |
| `disk_space_usage` | disk | space_usage | — | max | 各挂载点最大使用率 |
| `gpu_utilization` | gpu | utilization | — | first | GPU 使用率 |
| `gpu_memory_usage` | gpu | memory_usage | — | first | GPU 显存使用率 |
| `gpu_temperature` | gpu | temperature | — | first | GPU 温度 |
| `npu_utilization` | npu | utilization | — | first | NPU 使用率 |
| `npu_memory_usage` | npu | memory_usage | — | first | NPU 显存使用率 |
| `npu_temperature` | npu | temperature | — | first | NPU 温度 |

环形缓冲：每个 key 保留最近 `history_points` 个点，超出则丢弃最旧。`updateHistory` 返回历史的拷贝写入快照。

> **新增趋势的规则**：在 `trackedSeries` 末尾加一行 spec，key 遵循 `<component>_<suffix>` 命名，前端详情页会自动渲染该 sparkline。无需改前端。

### 5.4 静态设备规格（`hwinfo.go` + `collector.go` 的 `staticStash`）

静态规格是设备的**身份信息**（型号/拓扑/序列号/容量），非时序数据，采集一次即可。Web 侧用两条互补路径收集，合并写入每个快照的 `specs` 字段（`Snapshot.Specs`）：

#### 5.4.1 启动期一次性硬件身份（`hwinfo.go` `collectHWSpecs`）

`main.go` 在启动时起一个 goroutine 调 `collectHWSpecs()`，它**不是注册采集器**（不在 `collector` 注册表、不被定时循环调用），因为这些跨部件身份指标没有别的采集器产出。结果经 `DataCollector.SetHWSpecs` 存入 `hwSpecs` 字段（`hwMu` 保护），`collectOnce` 每周期读取并合入 `specs`。

| metric name | component | 来源 | 说明 |
|-------------|-----------|------|------|
| `device_model` | system | `dmidecode` SMBIOS type 1 | 厂商/产品名/版本/序列号 |
| `gpu_info` | gpu | `nvidia-smi --query-gpu=index,name,uuid,driver_version` | 每卡一条 |
| `npu_info` | npu | `npu-smi info` | 每卡一条（id/name/bus_id） |
| `disk_info` | disk | `/sys/block` + `smartctl`（可选富化 serial/firmware/interface） | 每真实块设备一条，value=容量 GB |
| `net_info` | network | `/sys/class/net`（跳过 lo） | 每接口一条（mac/mtu/speed/driver） |

> 可用性：`nvidia-smi`/`npu-smi` 不在 PATH 则对应项跳过（不报错）；`dmidecode`/`smartctl` 缺失则降级（device_model 不产出 / disk_info 缺少 serial 等富化字段）。`/sys` 始终可用。

#### 5.4.2 CPU/内存静态指标 stash（`collector.go` `filterStatic`）

CPU/内存采集器在启动首周期产出一次静态指标（型号/拓扑/频率范围/缓存大小/DIMM 清单），随后通过内部 flag 抑制重复产出。Web 侧在 `collectOnce` 中用 `filterStatic` 提取这些指标，首次出现即缓存到 `staticStash`，之后每周期重新注入快照——否则首周期之后这些设备规格会从 snapshot 消失。

`staticMetricNames` 集合（决定哪些指标被 stash）：

```
model_info, numa_node_num, core_num, die_core_num, numa_core_num, cpu_num
min_freq, max_freq, l1d_cache_size, l1i_cache_size, l2_cache_size, l3_cache_size
module_info, module_size, module_num
```

#### 5.4.3 合并写入

每个快照的 `Specs = staticStash + hwSpecs`（`collector.go:collectOnce`）。两者互不重叠：`staticStash` 是 CPU/内存静态指标（由周期采集器产出），`hwSpecs` 是跨部件身份（由 `hwinfo.go` 启动期产出）。任一为空时 `specs` 仍正常拼装；两者皆空时 JSON 因 `omitempty` 不含 `specs` 键。

---

## 6. HTTP API 规范（`server.go`）

所有路由由 `Server.Routes()` 注册。响应体均为 JSON（除静态资源/HTML）。

### 6.1 路由表

| 方法 | 路径 | 说明 | 成功码 | 失败码 |
|------|------|------|:------:|:------:|
| GET | `/` | 返回 `index.html`（SPA 外壳） | 200 | 500 |
| GET | `/static/{file}` | 静态资源（css/js） | 200 | 404 |
| GET | `/api/snapshot` | 读取 `snapshot.json` 返回 | 200 | 503 |
| GET | `/api/collectors` | 注册表元数据列表（驱动导航） | 200 | — |
| GET | `/api/config` | 当前配置 | 200 | — |
| POST | `/api/config` | 更新刷新间隔（热生效 + 持久化） | 200 | 400 / 405 |
| POST | `/api/refresh` | 请求立即采集 | 200 | 405 |

### 6.2 详细契约

**GET /api/collectors** → 驱动前端导航，取自 `collector.DefaultRegistry`：
```json
[
  {"name":"cpu","component":"cpu","priority":"High","interval":"3s","enabled":true},
  {"name":"disk","component":"disk","priority":"High","interval":"5s","enabled":true}
]
```
> 顺序为注册表内排序（按 name）。**新增采集器**（在 `main.go` 加 blank import）自动出现在此列表与前端导航。

**GET /api/snapshot** → 见 §4.1。快照未就绪（首次采集前）返回 503 `{"error":"snapshot not ready"}`，带 `Cache-Control: no-cache`。

**GET /api/config** → `{"refresh_interval_ms": 5000, "history_points": 60}`。

**POST /api/config** 请求体 `{"refresh_interval_ms": 8000}`，校验：
- `refresh_interval_ms >= 1000`，否则 400；
- JSON 非法 → 400；
- 非 GET/POST 方法 → 405（`Allow: GET, POST`）。
成功 → 更新内存间隔、`SetInterval` 热生效、原子写 `runtime.json`，返回 `{"refresh_interval_ms": 8000, "history_points": 60}`。

**POST /api/refresh** → 调 `DataCollector.CollectNow()`（经主循环串行触发，不并发写历史），返回 `{"ok":true}`。前端随后轮询即可见新数据。

---

## 7. 前端设计（`static/`）

### 7.1 SPA 与路由

单页应用，hash 路由（无后端路由、无历史 API 复杂度）：
- `#/` → 概览页
- `#/<component>`（如 `#/cpu`）→ 该部件详情页
- `hashchange` 事件触发重渲染；导航高亮当前路由。

数据获取：`fetchCollectors()`（导航）→ `fetchConfigData()`（间隔）→ `startPolling()`（`setInterval` 调 `/api/snapshot`，间隔 = `refresh_interval_ms`）。改间隔 → `POST /api/config` → 重置轮询。

### 7.2 概览页（`renderOverview`）

- **健康度面板**：大号总分 + 进度条 + 等级（Excellent/Good/Warning/Critical，颜色映射）+ 服务器类型 + 更新时间 + 采集间隔。
- **设备规格面板**（`renderSpecs`，hero 右上）：从 `snap.specs` 抽取核心静态身份的紧凑键值表（设备/CPU/内存总量/硬盘数与总容量/网卡/GPU/NPU）。内存总量取自每周期的 `usage_detail` 指标（非 specs）。点击面板弹出完整规格 modal（`openSpecsModal`）：按 component 分组（system→cpu→memory→disk→gpu→npu→network，未知部件排末尾），每组一张"类型/标识/明细"表（`specsGroup`）。无任何 specs 时显示"无静态规格信息"。
- **部件芯片**：每个已注册部件一个彩色圆点芯片（颜色由该部件得分比决定），点击进详情。
- **部件概览卡片网格**：每卡 = 部件名 + 得分/满分 + 状态徽章 + 头条趋势 sparkline（若 manifest 指定）+ 关键指标键值表（manifest.key）。无数据时显示"无数据"徽章。点击进详情。

### 7.3 部件详情页（`renderDetail`）

- 头部：返回链接 + 部件标题 + 得分/满分 + 状态徽章 + 扣分项列表。
- **趋势面板**：自动列出所有 `<component>_*` 历史序列，每个渲染 sparkline + 当前值。
- **全部指标面板**：表格列出该部件全部指标（指标名/值/标签），覆盖该部件所有 metric 实例（如每核心、每挂载点、每卡）。

### 7.4 显示 manifest（`app.js`，可选提示）

```js
const MANIFEST = {
  cpu: { title:'CPU', headline:'cpu_usage', headlineLabel:'CPU 使用率 (%)',
         key:[ {name:'usage',prefer:{core:'total'}}, 'load_average', 'temperature', 'model_info' ] },
  // memory / disk / gpu / npu / network ...
};
```

- `title`：导航与卡片显示名；未登记部件用 `key.toUpperCase()`。
- `headline` / `headlineLabel`：概览卡头条 sparkline 序列；未登记则无头条 sparkline。
- `key`：概览卡关键指标（支持字符串=指标名取首个，或 `{name, prefer:{label:value}}` 按标签精确选）；未登记部件取前 4 条 metric。

### 7.5 其他前端常量

- `METRIC_NAMES`：指标名 → 中文显示名映射（未命中则用原始名）。含静态身份（`device_model`/`gpu_info`/`npu_info`/`disk_info`/`net_info`/`module_info` 等）。
- `SERIES_LABELS`：历史序列 key → 显示名（未命中则用 `key` 去前缀 + 下划线转空格）。
- `NAV_ORDER`：导航排序（`['cpu','memory','disk','gpu','npu','network']`，未知部件排末尾按字母序）。
- `SPEC_DEFS`：静态 spec 指标名 → `{type, primary}`（类型显示名 + 持有主标识的 label key），驱动 specs 面板/modal 的"类型/标识"列。覆盖 `device_model`/`model_info`/`gpu_info`/`npu_info`/`disk_info`/`net_info`/`module_info`。
- `LABEL_NAMES`：label key → 中文显示名（如 `manufacturer`→厂商、`product_name`→型号、`serial`→序列号、`mac`→MAC 等），用于 specs modal 的"明细"列。

### 7.6 状态色映射

`statusOf(score, max)`：比率 ≥0.9 OK(绿) / ≥0.75 Good / ≥0.6 Warning(橙) / 否则 Critical(红)。`gradeColor(grade)` 同色系。无 max 时 N/A(灰)。

---

## 8. 部署与运行

### 8.1 构建

```bash
go build -o web/bin/catmonitor-web ./web     # web/bin/ 已被根 .gitignore 的 bin/ 覆盖
```
Windows：`GOOS=windows go build -o web/bin/catmonitor-web.exe ./web`（无 CGo，纯 syscall）。

### 8.2 运行

```bash
./web/bin/catmonitor-web -config web/config.yaml    # 默认监听 :9527，被占用则自动递增
# 浏览器打开 http://localhost:9527（实际端口见启动日志 "web server starting" addr=...）
```
工作目录需为仓库根（`config.yaml` 中 `snapshot_path`/`runtime_path` 为相对路径 `web/data/...`）；或改用绝对路径配置。

### 8.3 systemd 常驻（推荐）

```bash
systemd-run --unit=catmonitor-web \
  --working-directory=<repo-root> \
  <repo-root>/web/bin/catmonitor-web -config <repo-root>/web/config.yaml

systemctl status catmonitor-web
journalctl -u catmonitor-web -f
systemctl restart catmonitor-web   # 重启（重新加载配置）
systemctl stop catmonitor-web
```

### 8.4 优雅退出

捕获 `SIGINT`/`SIGTERM` → `cancel` ctx（采集循环退出）→ `http.Server.Shutdown`（5s 超时）。

### 8.5 端口占用回退（`main.go` `listenWithFallback`）

启动 HTTP 前先以 `net.Listen("tcp", addr)` 探测端口，避免 `ListenAndServe` 在 goroutine 中异步失败导致难以定位：

1. 解析 `server.addr` 的 host/port（`net.SplitHostPort`）；不可解析则直接 listen 原值（不做回退）。
2. 循环 `net.Listen`：成功 → 返回 listener；失败且 `errors.Is(err, syscall.EADDRINUSE)` → 端口 +1（`net.JoinHostPort` 重组地址）打印 warn 日志后重试。
3. 其他错误（权限不足、地址非法等）直接返回，启动失败退出（`os.Exit(1)`）。
4. 成功获取的 listener 交给 `http.Server.Serve(ln)`（不再用 `ListenAndServe`），实际绑定地址回写 `cfg.Server.Addr`，启动日志打印最终 `addr`，便于确认浏览器应访问的端口。

> 该回退仅针对端口占用（`EADDRINUSE`），跨平台有效（`syscall.EADDRINUSE` 在 Linux/Windows 均定义）。

---

## 9. 扩展性设计（新增部件 / 新增指标）

> 这是本规格的重点。设计目标是：**新增采集器/指标时，尽量自动出现，最多在一处加一行**。

### 9.1 场景 A：新增一个部件类型（如 FPGA 采集器）

前置：按主项目 `AGENTS.md` "Adding a collector" 在 `internal/collectors/fpga/` 实现并 `init()` 注册（**主项目既定流程，不在本规格范围**）。

| 步骤 | 是否必须 | 效果 |
|------|:--------:|------|
| 在 `web/main.go` 加 blank import `_ ".../internal/collectors/fpga"` | 必须 | 采集器被注册，`/api/collectors` 自动含 fpga |
| 前端导航 | **自动** | 出现 FPGA 导航项与概览芯片 |
| 概览卡片 | **自动** | 出现 FPGA 概览卡（通用：取前 4 条指标，无头条 sparkline） |
| 详情页 `#/fpga` | **自动** | 列出 fpga 全部指标；若有 `<component>_*` 历史序列则渲染趋势 |
| 概览卡显示名/关键指标 | 可选 | 在 `app.js` 的 `MANIFEST` 加 `fpga:{title, key:[...]}` |
| FPGA 趋势 sparkline | 可选 | 在 `collector.go` 的 `trackedSeries` 加 spec（key 形如 `fpga_utilization`） |

**结论：一行 blank import 即可让新部件完整可用**；后续按需在 MANIFEST/trackedSeries 美化。

### 9.2 场景 B：现有部件新增采集指标

采集器 `Collect()` 多返回若干 `Metric` 后：

| 出现位置 | 是否自动 | 备注 |
|----------|:--------:|------|
| 部件详情页"全部指标"表 | **自动** | 通用表格渲染该部件全部指标 |
| 概览卡关键指标 | 需在 MANIFEST.key 加条目 | 否则概览卡只展示原关键指标 |
| 趋势 sparkline | 需在 trackedSeries 加 spec | 否则只显示当前值，无趋势 |

### 9.3 场景 C：新增一条趋势序列

在 `web/collector.go` 的 `trackedSeries` 末尾加一行：
```go
{component: "fpga", name: "temperature", key: "fpga_temperature", mode: 0},
```
- `key` 必须形如 `<component>_<suffix>`，详情页 `componentSeries()` 按 `<component>_` 前缀过滤自动渲染。
- 在 `app.js` 的 `SERIES_LABELS` 加可选显示名（不加则用通用标签）。
- **无需改任何渲染逻辑**。

### 9.4 场景 D：调整历史深度

改 `config.yaml` 的 `history_points`（重启生效）；或调 `collector` interval。

### 9.5 扩展点汇总表

| 扩展需求 | 改动位置 | 自动部分 |
|----------|----------|----------|
| 新部件采集器 | `web/main.go`（blank import） | 导航/概览卡/详情页 |
| 部件显示名/关键指标 | `web/static/app.js` MANIFEST | — |
| 新指标展示 | （采集器侧，无需改 web） | 详情页全部指标表 |
| 概览卡纳入新指标 | `web/static/app.js` MANIFEST.key | — |
| 新趋势 sparkline | `web/collector.go` trackedSeries | 详情页趋势面板 |
| 趋势显示名 | `web/static/app.js` SERIES_LABELS | — |
| 新静态身份指标（采集器侧） | 加入 `staticMetricNames`（`collector.go`）即被 stash 进 `specs` | specs modal 通用表自动渲染 |
| 新静态身份指标（web 侧 hwinfo） | `hwinfo.go` 加采集方法 + `SPEC_DEFS`/`LABEL_NAMES` 加显示名 | specs modal 按 component 分组自动出现 |
| 导航排序 | `web/static/app.js` NAV_ORDER | 未知部件自动排末尾 |
| 历史深度 | `web/config.yaml` history_points | — |

### 9.6 兼容性保证

- `health` 与 `metrics` 字段直接复用主项目结构体，**采集器新增任何字段/标签**都会原样透传到前端。
- 未知部件/未知指标/未知序列均有通用回退（部件用名大写、指标用原始名、序列用去前缀名），**不会因未登记而崩溃或消失**。

---

## 10. Git 与运行时文件

- **应提交**：`web/` 下所有源码与静态资源（见上传清单）。
- **不应提交**：`web/data/*`（运行时生成：`snapshot.json`、`runtime.json`）。建议在根 `.gitignore` 加一行 `web/data/`（当前未加，因遵守"不改现有文件"约束；提交前请自行添加）。
- **构建产物**：`web/bin/` 已被根 `.gitignore` 的 `bin/` 覆盖，自动忽略。

---

## 11. 测试

自测脚本 `webtest.sh`（位于仓库外 `/tmp/opencode`，非交付物）覆盖 26 项断言：

- 构建：`go build` / `go vet` / `GOOS=windows` 交叉编译。
- 路由：`GET /`、`/static/*`、404、Content-Type、旧端口未占用。
- API：`/api/collectors`（6 采集器元数据）、`/api/config`、`/api/snapshot` 结构深度校验（timestamp/health/metrics/history 齐全、score 范围、grade 枚举、components 含 score/max/deductions）。
- 扩展历史：验证 `cpu_load_average`、`memory_swap_usage` 等新序列出现。
- 间隔热更新：`POST /api/config` 8s → `runtime.json` 持久化 → `GET` 回读一致 → snapshot 反映 8000ms。
- 边界：`<1000ms→400`、坏 JSON→400、`PUT→405`。
- 立即刷新：`POST /api/refresh` ok + snapshot 热刷新。
- 趋势增长：连续刷新后历史点数递增。

单元测试（`web/*_test.go`，`go test ./web/`）覆盖：

- 快照：`TestSnapshotRoundTrip`（原子读写）、`TestCollectOnceSmoke`（端到端采集→写盘）。
- 历史：`TestTrackedSeriesInvariants`、`TestUpdateHistoryRingBuffer`、`TestUpdateHistoryV02Metrics`、`TestUpdateHistoryMissingMetric`。
- 静态规格 stash：`TestFilterStatic`（`staticMetricNames` 过滤）、`TestStashStaticsPersistsAcrossCycles`（首周期后静态指标持续存活于 `specs`）。
- 硬件身份采集（`hwinfo.go`）：`TestHWGpuInfo`、`TestHWNpuInfo`、`TestHWDeviceModel`、`TestHWNetInfo`、`TestHWDiskInfo`（各 mock 注入）、`TestCollectHWSpecsSmoke`（整体冒烟）、`TestParseNPUStatic`（npu-smi 解析）。
- HTTP：`TestHTTPAPISmoke`（路由 + 端口回退 + snapshot 结构）。

运行：`make` 无 web 目标（不新增，避免改根 Makefile），直接 `go test ./web/`。

---

## 12. 已知限制与后续预留

1. **单机本地视图**：不含认证、不含多机聚合；如需多机，预留为"多个 snapshot 源 + 概览聚合"未来扩展。
2. **轮询而非推送**：前端 `setInterval` 轮询 `/api/snapshot`；如需实时推送，预留 WebSocket/SSE（当前 `snapshot.json` 解耦边界可直接复用）。
3. **无持久化历史存储**：历史仅存内存环形缓冲（重启清空），未落盘；如需长期趋势，预留为 `internal/storage` 风格的 JSONL 落盘（web 侧另起存储，不复用主项目 storage 以保持解耦）。
4. **`max_file_age` 类清理未实现**：`runtime.json` 不做清理（单文件，无需）。
5. **扩展前置依赖主项目采集器**：新部件的真正采集逻辑仍需在 `internal/collectors/<name>/` 实现（见主项目 `AGENTS.md`），web 仅负责可视化与一行注册。
6. **指标展示优先级**：当前 metric 不携带优先级字段（主项目 `collector.Metric` 无 Priority），概览关键指标靠 MANIFEST 人工指定；未来若主项目 Metric 增加优先级，可改为按优先级自动选取关键指标。

---

## 13. 关键设计决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| 数据获取 | 进程内导入采集器库（非 shell 调 CLI / 非读 JSONL） | 无子进程开销，复用代码，仍通过 snapshot.json 与 HTTP 层解耦 |
| 解耦边界 | snapshot.json 文件 | HTTP 层只读文件，不调采集器；采集器唯一写者，原子写 |
| 多页面 | SPA + hash 路由 | 单文件部署、无后端路由、无构建步骤 |
| 扩展驱动 | `/api/collectors`（注册表）+ `trackedSeries`（趋势）+ `MANIFEST`（显示提示） | 新部件/指标自动出现，显示美化集中可选 |
| 静态规格 | 双路径：`hwinfo.go` 启动期一次性采跨部件身份 + `staticStash` 缓存 CPU/内存一次性指标 | 身份信息非时序，跑一次即可；stash 保证首周期后不丢失；不污染定时循环与注册表 |
| 端口 | 9527（占用时自动 +1 递增） | 用户指定默认 9527；端口被占用自动探测下一可用端口，保证可拉起（见 §8.5） |
| 前端打包 | `//go:embed` | 单二进制可移植，离线可用 |
| 配置持久化 | runtime.json 叠加 YAML | 界面调整重启保留，又不污染 YAML |

---

*文档版本：v1.2 · 对应代码状态：web/ 多页可扩展版（端口 9527 占用自动递增；静态规格双路径采集）*
