# CATMonitor 使用手册 (User Manual)

> 本文档说明 CATMonitor 的构建、安装、配置、命令行、Web 仪表盘、能效监控、Prometheus 导出与运维用法。
> 功能规格见 [SPEC.md](../SPEC.md)，架构设计见 [DESIGN.md](../DESIGN.md)，指标清单见 [CATMonitor_indi_list.md](CATMonitor_indi_list.md)。

---

## 0. 快速上手（60 秒）

```bash
make all                                       # 编译 catmonitor + web + dfee 三二进制
./bin/catmonitor daemon &                      # 启动守护进程（采集 + Prometheus :9100 + snapshot 生产）
curl -s http://localhost:9100/metrics | head   # 抓取 Prometheus 指标（catmonitor_* 命名）
./bin/catmonitor-dfee -exporter=enabled -snapshot-dir /var/lib/catmonitor/snapshot &  # dfee + :9333 exporter（node_*/dsmi_*）
curl -s http://localhost:9333/metrics | head   # 抓取 node_exporter 风格指标
./bin/catmonitor health                        # 一次性健康检查
./bin/catmonitor collect -o table | head       # 一次性指标采集
```

> Web 仪表盘（可选）：`./bin/catmonitor-web -snapshot-dir /var/lib/catmonitor/snapshot`，浏览器打开 http://localhost:9527。
> 容器化部署（可选）：`docker/docker/build.sh && docker compose -f docker/docker-compose.yml up -d`，详见 [§12](#12-容器化部署) 与 [docker/README.md](../docker/README.md)。

## 目录

- [1. 构建与安装](#1-构建与安装)
- [2. 配置](#2-配置)
- [3. 命令行](#3-命令行)（含 [常用场景速查](#常用场景速查)）
- [4. Web 仪表盘](#4-web-仪表盘catmonitor-web)
- [5. Prometheus 导出](#5-prometheus-导出exporter)（含 [§5.5 dfee 独立 exporter :9333](#55-dfee-独立-prometheus-exporter9333)）
- [6. 能效监控（dfee）](#6-能效监控dfee)
- [7. 健康度评分](#7-健康度评分)
- [8. 数据格式](#8-数据格式)
- [9. 无硬件环境的优雅降级](#9-无硬件环境的优雅降级)
- [10. 扩展](#10-扩展)
- [11. 排错与常见问题](#11-排错与常见问题)
- [12. 容器化部署](#12-容器化部署)

---

## 1. 构建与安装

### 1.1 依赖

- Go 1.23.4+（以 `go.mod` 为准；系统同时安装多个版本时必须明确选择满足要求的工具链）
- （可选）NPU 服务器：CANN SDK（`libdcmi.so`），用 `-tags dcmi` 启用 DCMI CGo 采集
- （可选）NPU 可靠性压测：仓库已包含 Mulan PSL v2 的固定 Ascend NPU Burn 源码；节点管理员另行准备匹配的 CANN、PyTorch/torch_npu 和驱动环境或基础镜像
- （可选）`nvidia-smi` / `npu-smi` / `hccn_tool` / `ipmitool` / `smartctl`：对应采集器无该命令时优雅降级（返回空，不崩溃）

### 1.2 编译

```bash
# 先确认实际使用的工具链；不能只确认系统中“安装过”新 Go
GO_BIN=${GO_BIN:-/opt/catmonitor/toolchains/go1.25.1/bin/go}
test -x "$GO_BIN"
"$GO_BIN" version
mkdir -p bin

# 一键构建 daemon + web + dfee 三个二进制
make all GO="$GO_BIN"

# 分别构建
make build GO="$GO_BIN"  # daemon: bin/catmonitor（CANN DCMI 头存在时自动加 -tags dcmi）
make web GO="$GO_BIN"    # catmonitor-web: bin/catmonitor-web
make dfee GO="$GO_BIN"   # catmonitor-dfee: bin/catmonitor-dfee

# 或直接 go build
GOTOOLCHAIN=local "$GO_BIN" build -o bin/catmonitor ./cmd/catmonitor
GOTOOLCHAIN=local "$GO_BIN" build -o bin/catmonitor-web ./features/web
GOTOOLCHAIN=local "$GO_BIN" build -o bin/catmonitor-dfee ./features/dfee

# NPU 服务器强制启用 DCMI CGo 采集（make build 已自动探测，手动时需加 tag）
GOTOOLCHAIN=local "$GO_BIN" build -tags dcmi -o bin/catmonitor ./cmd/catmonitor

# Windows 交叉编译
GOOS=windows GOTOOLCHAIN=local "$GO_BIN" build -o bin/catmonitor.exe ./cmd/catmonitor
GOOS=windows GOTOOLCHAIN=local "$GO_BIN" build -o bin/catmonitor-web.exe ./features/web
GOOS=windows GOTOOLCHAIN=local "$GO_BIN" build -o bin/catmonitor-dfee.exe ./features/dfee
```

`GOTOOLCHAIN=local` 不会下载或升级 Go，它只要求使用当前被调用的 `go` 二进制。
因此系统默认 `go` 为 1.21.9 时，`GOTOOLCHAIN=local go build` 仍会失败；必须显式
调用满足 `go.mod` 要求的 `GO_BIN`，或先把对应 `bin` 目录放到 `PATH` 最前面。

> 默认构建排除 CGo（`dcmi_cgo.go` 在 `//go:build cgo && linux && dcmi` 后），无 CANN SDK 也能编译；NPU 的 DCMI 指标在无 `-tags dcmi` 时优雅降级。`make build` 自动探测 `/usr/local/Ascend/driver/include/dcmi_interface_api.h`，存在则加 `-tags dcmi`（可用 `DCMITAG=` 强制关闭、`DCMITAG=-tags dcmi` 强制开启）。
> 三个二进制产物均在 `bin/`（Makefile：`make all` = build + web + dfee）。

### 1.3 安装（Linux systemd）

```bash
sudo scripts/install.sh            # 部署二进制 + 配置 + metrics.yaml + service unit
sudo systemctl start catmonitor
sudo systemctl enable catmonitor
sudo systemctl status catmonitor
sudo journalctl -u catmonitor -f   # 查看日志
```

---

## 2. 配置

默认配置见 `configs/catmonitor.yaml`：

```yaml
server:
  type: auto              # auto | cpu_only | accelerated

collectors:
  chassis:  { enabled: true, interval: 3s }
  cpu:      { enabled: true, interval: 3s }
  memory:   { enabled: true, interval: 3s }
  disk:     { enabled: true, interval: 5s }
  gpu:      { enabled: true, interval: 3s }
  npu:      { enabled: true, interval: 3s }
  network:  { enabled: true, interval: 3s }

storage:
  data_dir: /var/lib/catmonitor/data
  max_file_age: 168h       # 数据保留时长（默认 7 天）
  rotation: daily           # 按天轮转

health:
  enabled: true             # v0.3.3 起 daemon 不再周期评估（保留字段）
  weight_scheme: auto      # 仅 `catmonitor health` CLI 使用：auto | cpu_only | accelerated_8card | accelerated_4card

stress:
  enabled: false
  web_enabled: false
  script_path: /opt/catmonitor/stress/benchmark_check.sh
  report_path: /var/lib/catmonitor/stress/stress-latest.json
  default_benchmarks: [stream]
  benchmarks:
    stream: { enabled: false, timeout: 1m }
    hpl: { enabled: false, timeout: 2h }
    hpcg: { enabled: false, result_dir: "", timeout: 3m }
    npu_burn: { enabled: false, timeout: 30m }

collection:
  min_priority: medium     # low (全采) | medium (跳过 Low) | high (仅 High)——按优先级阈值预过滤采集

features: [web, dfee]      # feature-scope 白名单（各 feature metrics.yaml 并集；空 = 默认全集）；派生 per-comp cadence

snapshot:                 # daemon 统一生产 snapshot 供 web/dfee 只读消费（默认 on）
  enabled: true           # off 时 daemon 不写 snapshot；on 时 web/dfee 须以只读消费者运行
  dir: /var/lib/catmonitor/snapshot

faultsub:                 # 故障订阅推送（默认 off）；详见 features/faultsub/faultsub_SPEC.md
  enabled: false
  rest_addr: ":9101"      # 订阅 REST API 监听地址
  webhook_timeout: 5s     # webhook 推送超时
  webhook_retry: 1        # 失败重试次数
  event_buffer: 1024     # 近期事件环形缓冲（GET /faultsub/events 回补）
  defaults: { debounce_ms: 0, min_severity: "warning" }
  rules:                  # 故障判定规则开关（unset = enabled）；含 NPU 掉卡检测 card_drop（基于 card_drop 指标 / DCMI DeviceNotReady -8012）
    card_drop: true
    npu_health: true
    npu_error_code: true
    hbm_uce: true
    ddr_uce: true
    roce_link_down: true
    driver_unhealthy: false

straggler_output:         # 落后节点 KPI 文件输出（默认 off）；详见 features/stragglerout/stragglerout_SPEC.md
  enabled: false
  data_dir: /var/lib/catmonitor/straggler
  retention: 360h         # 15 天
  flush_interval: 60s    # 内存缓冲 flush 周期
```

**字段用途**：

| 字段 | 用于 | 说明 |
|------|------|------|
| `server.type` | daemon / health | 服务器类型提示，`auto` 由实际采集指标自动判定 |
| `collectors.*.enabled` / `.interval` | daemon | 各采集器开关与采集周期 |
| `storage.*` | daemon | JSONL 数据目录、保留时长、轮转策略 |
| `health.weight_scheme` | `health` CLI | 健康度权重方案；`enabled` v0.3.3 起 daemon 不再周期评估 |
| `stress.*` | `stress` CLI / Web | 可靠性压测授权、共享报告、项目和最大运行窗口；节点执行参数由部署脚本维护 |
| `collection.min_priority` | daemon / collect | 按优先级阈值预过滤采集粒度 |
| `features` | daemon | feature-scope 白名单：各 feature `metrics.yaml` 并集；派生 per-component cadence `C_comp = min(feature interval)` |
| `snapshot.enabled` / `.dir` | daemon | snapshot 生产开关与目录（on 时 web/dfee 只读消费） |
| `faultsub.*` | daemon | 故障订阅推送 opt-in：`enabled`/`rest_addr`(:9101)/`webhook_*`/`event_buffer`/`defaults`/`rules`（含 `card_drop` 掉卡检测开关）；off 时 daemon 行为不变 |
| `straggler_output.*` | daemon | 落后节点 KPI 输出 opt-in：`enabled`/`data_dir`/`retention`/`flush_interval`；off 时无 KPI 文件 |

### 配置路径

| 平台 | 默认配置路径 | 默认数据目录 |
|------|-------------|-------------|
| Linux | `/etc/catmonitor/catmonitor.yaml` | `/var/lib/catmonitor/data` |
| Windows | `C:\ProgramData\catmonitor\catmonitor.yaml` | `C:\ProgramData\catmonitor\data` |

普通 CLI 可用 `-config` 覆盖配置路径（现有短 flag `-c` 尚未接入普通命令）；stress CLI 同时支持 `-c/--config`。Web 默认读取同一平台路径，非标准部署可使用 `CATMONITOR_CONFIG` 或 `-config` 覆盖。指标采集目录 `metrics.yaml` 加载顺序：环境变量 `CATMONITOR_METRICS` → 配置目录下 `metrics.yaml` → 开发回退 `configs/metrics.yaml`。

---

## 3. 命令行

```
catmonitor [command] [flags]

Commands:
  daemon       启动守护进程（持续采集 + Prometheus 导出）— 默认；健康度评估由 health 子命令按需执行
  collect      单次采集所有指标快照
  health       执行一次健康检查
  stress       显式运行 Linux 可靠性压测
  list         列出所有已注册采集器
  version      显示版本信息

Flags:
  -c, --config      配置文件路径
  -o, --output      输出格式: json|table
  -h, --help        帮助信息（解析后即退出）
```

> 仅上述 flag 实际生效。`-d/--data-dir`、`-v/--verbose`、`--component`、`--interval` 未实现（历史文档/二进制 help 曾误列），数据目录与采集周期请通过配置文件调整。

### 常用场景速查

| 场景 | 命令 |
|------|------|
| 常驻采集 + Prometheus | `catmonitor daemon` |
| 一次性巡检（指标） | `catmonitor collect -o table` |
| 一次性健康体检 | `catmonitor health` |
| Linux 可靠性压测 | `catmonitor stress -o table` |
| 查看采集器清单 | `catmonitor list` |
| 查看版本 | `catmonitor version` |
| Web 仪表盘 | `catmonitor-web`（独立二进制，见 §4） |

### 3.1 version

```bash
$ catmonitor version
CATMonitor v0.3.3 (Go 1.23+)
```

### 3.2 list — 采集器注册表

```bash
$ catmonitor list
Name     Component  Priority  Interval  Enabled
chassis  chassis    High      3s        true
cpu      cpu        High      3s        true
disk     disk       High      5s        true
gpu      gpu        High      3s        true
memory   memory     High      3s        true
network  network    High      3s        true
npu      npu        High      3s        true
```

### 3.3 collect — 单次采集

```bash
catmonitor collect                 # JSON 输出
catmonitor collect -o table         # 表格输出
catmonitor collect -o json > /tmp/snapshot.json
```

表格输出示例（节选）：

```
Component  Metric        Value    Unit  Labels
cpu        usage         0.00     %     core=total
cpu        load_average  1.62           interval=1m
memory     usage         72.69    %
disk       space_usage   29.14    %     device=drivers,mount_point=/usr/lib/wsl/drivers
network    error_count   0.00     次    interface=eth0,type=rx_err
npu        npu_num       0.00     个
```

> 无 NPU/GPU/BMC 环境下，gpu/chassis 返回空、npu 返回 `npu_num=0`，均不报错（优雅降级）。
> `collect` 输出受配置 `collection.min_priority` 影响：`low`（默认）全采、`medium` 跳过 Low 指标、`high` 仅采 High；改配置后 `collect` 行数会相应变化。

### 3.4 health — 健康检查

```bash
catmonitor health            # 默认表格
catmonitor health -o json    # JSON
```

输出示例：

```
Overall Score:  [████████████████████████████░░]  95 / 100   [ Excellent ]
Server Type:    accelerated
  CPU        10 / 10   OK
  MEMORY     18 / 20   OK      swap>50% (-2)
  DISK        7 / 10   Warning smart_failed (-3)
  NPU        60 / 60   OK
  TOTAL      95 / 100  Excellent
```

### 3.5 stress — 可靠性压测

```bash
catmonitor stress --help
catmonitor stress -o table                  # 运行 YAML 的 default_benchmarks
catmonitor stress --bench stream -o table   # 覆盖本次运行项目
catmonitor stress --bench npu_burn -o table # Ascend NPU Burn（需节点安装和脚本适配）
catmonitor stress -o json                   # 回显完整 JSON 报告
```

stress 只在用户显式请求时运行。主配置只定义功能开关、共享报告、项目和最大运行窗口；benchmark 绝对路径、环境变量和 MPI/NUMA 参数由节点部署的 `benchmark_check.sh` 维护。Ascend NPU Burn 固定源码随仓库提供，管理员只需准备匹配的原生依赖或 CANN/torch_npu 基础镜像并构建运行环境；脚本校验其 `npu_burn_results.csv` 全部 PASS、无 SDC 错误且全局设备汇总无 FAIL。CLI 与 Web 共享报告和 Linux 文件锁，不能同时启动两组作业。日常启用与使用见 [STRESS_USER_GUIDE.md](../features/stress/STRESS_USER_GUIDE.md)，完整构建、适配、升级及验收见 [STRESS_TEST_GUIDE.md](../features/stress/STRESS_TEST_GUIDE.md)。

### 3.6 daemon — 守护进程

```bash
catmonitor daemon                       # 默认配置前台运行
catmonitor daemon -c /etc/catmonitor/my.yaml
# 无 -v/--verbose 选项；如需更详细日志，改 slog Handler 级别（见源码 setupLogger）
```

daemon 启动后：
- Scheduler 按各采集器 `interval` 周期采集 → 写 JSONL（`{data_dir}/{component}_{date}.jsonl`）
- 采集粒度由 `collection.min_priority` 控制：`low` 全采（默认），`medium` 跳过 Low 指标组，`high` 仅采 High；采集器经 `AnyWanted` DI 在执行前跳过无需采集的指标组
- `exporter.CachingStorage` 包装存储层，一次采集同时落盘 + 缓存到内存
- HTTP `/metrics` 端点监听 `:9100`（见 §5）
- v0.3.3 起 daemon 不再周期评估健康度；健康度评估改由 `catmonitor health` 子命令按需执行

捕获 `SIGINT`/`SIGTERM` 优雅退出。

---

## 4. Web 仪表盘（catmonitor-web）

独立二进制，**只读消费** daemon 产出的 snapshot（global `snapshot.json` + per-comp `snapshot_<comp>.json`），可视化单机健康度与各部件指标，不再自行采集。独立 stress feature 可按主配置额外挂载受保护的作业接口，但不会写 snapshot。

### 4.1 启动

```bash
# 先启动 daemon 并在 catmonitor.yaml 中开启 snapshot 生产：
#   snapshot:
#     enabled: true
#     dir: /var/lib/catmonitor/snapshot
catmonitor daemon

# 再启动 web 只读消费者（-snapshot-dir 须与 daemon snapshot.dir 一致）
catmonitor-web -addr 127.0.0.1:9527 -snapshot-dir /var/lib/catmonitor/snapshot
# 非标准主配置路径追加：-config /path/to/catmonitor.yaml
# 浏览器打开 http://localhost:9527（实际端口见启动日志 "web server starting"）
```

> 端口 `:9527` 被占用时自动 +1 回退（`:9528`…）。能效监控改为独立二进制 `catmonitor-dfee`（见 §6），不再作为 web 子路由。

### 4.2 页面

- **概览页**（`/`）：整体健康度（总分 + 进度条 + 等级）+ 设备规格面板（点击展开完整规格）+ 各部件状态 + 部件概览卡片（含趋势 sparkline）
- **部件详情页**（`#/<component>`，如 `#/cpu`）：部件得分/扣分项 + 趋势面板（该部件全部历史序列 sparkline，数据来自 daemon 产出的 per-comp history）+ 全部指标表
- **可靠性压测**（`/stress/`）：独立页面；默认只读展示配置和共享报告，只有 Linux、回环监听及 `stress.enabled/web_enabled` 均满足时才能提交作业

### 4.3 REST API（只读）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/snapshot` | 组装 global+per-comp 快照（健康度 + 指标 + 历史 + 静态规格） |
| GET | `/api/collectors` | 采集器注册表元数据（取自 daemon snapshot） |
| GET | `/api/config` | 当前配置（cadence + history_points，只读） |

> 重构删除 `POST /api/config`（间隔热改）与 `POST /api/refresh`（立即采集）——刷新间隔由 daemon cadence 决定。详细设计与扩展机制见 [features/web/Web_SPEC.md](../features/web/Web_SPEC.md)。
>
> `/api/stress/*` 由独立 stress feature 提供，不属于 snapshot API；契约见 [STRESS_SPEC.md](../features/stress/STRESS_SPEC.md)。

---

## 5. Prometheus 导出（exporter）

daemon 内置 Prometheus 兼容导出，无需额外进程。

### 5.1 接入

启动 `catmonitor daemon` 后，`/metrics` 端点监听 `:9100`：

```bash
$ curl http://localhost:9100/metrics
# HELP catmonitor_cpu_usage cpu/usage
# TYPE catmonitor_cpu_usage gauge
catmonitor_cpu_usage{core="total"} 0
# HELP catmonitor_network_rx_bytes_total network/rx_bytes_total
# TYPE catmonitor_network_rx_bytes_total counter
catmonitor_network_rx_bytes_total{interface="eth0"} 123456
...
```

### 5.2 指标命名与类型

- 统一前缀 `catmonitor_{component}_{name}`，`-` `/` `.` 替换为 `_`
- 每组指标含 `# HELP` + `# TYPE` 头
- `counter`：名称以 `_total` 或 `_time` 结尾的累计型指标（如 `rx_bytes_total`、CPU 时间类），可用于 PromQL `rate()`/`increase()`
- 其余为 `gauge`
- 标签按字典序排序，格式 `{key="value",...}`

### 5.3 健康端点

| 路径 | 说明 |
|------|------|
| `GET /metrics` | Prometheus 文本格式全部指标 |
| `GET /-/healthy` | `200 OK`（存活） |
| `GET /-/ready` | 缓存非空返回 200，否则 503 |

### 5.4 Prometheus 抓取配置

```yaml
scrape_configs:
  - job_name: catmonitor
    static_configs:
      - targets: ['localhost:9100']
    metrics_path: /metrics
    scrape_interval: 15s
```

> 原理：`exporter.CachingStorage` 实现 `collector.Storage` 接口，包装在 JSONLStorage 外——一次采集同时落盘 JSONL + 更新内存缓存（原子替换），HTTP 层从缓存读取转 Prometheus 文本。详见 [features/exporter/exporter_SPEC.md](../features/exporter/exporter_SPEC.md)。

### 5.5 dfee 独立 Prometheus exporter（:9333）

除 daemon 的 `:9100/metrics`（`catmonitor_*` 命名）外，dfee 二进制另内置独立 exporter，输出对齐 node_exporter/dsmi 风格的命名，与 daemon 导出互不冲突。由 `-exporter=enabled` 开启：

```bash
$ catmonitor-dfee -addr :9528 -exporter=enabled -exporter-port=9333 -snapshot-dir /var/lib/catmonitor/snapshot
$ curl http://localhost:9333/metrics
node_cpu_seconds_total{mode="user"} 948021
node_memory_MemTotal_bytes 2163847168
node_network_receive_bytes_total{interface="eth0"} 32629770
node_disk_read_sectors_total{device="sda"} 9472
dsmi_power_w{npu_id="0"} 89.5
ipmi_power_w 1848
static_hardware_info{cpu_info="1*Intel(R) Core(TM) i5-7200U",disk_info="sda 356.9M",...} 1
static_software_info{os_version="Ubuntu 26.04 LTS",python_version="3.14.4",...} 1
```

| 前缀 | 来源 | 说明 |
|------|------|------|
| `node_*` | snapshot CPU/内存/网络/磁盘 | 对齐 node_exporter 命名（`node_cpu_seconds_total`/`node_memory_*_bytes`/`node_network_*_bytes_total`/`node_disk_*`/`node_load*`/`node_cpu_cores_online`） |
| `dsmi_*` | snapshot NPU | 对齐 dsmi 命名（`dsmi_power_w`/`dsmi_voltage_mv`/`dsmi_aicore_utilization_percent` 等） |
| `ipmi_*` | snapshot chassis | 机箱环境（`ipmi_power_w`/`ipmi_inlet_temp_celsius`/`ipmi_fan_speed_rpm`） |
| `static_*` | 启动时采集 | 一次性硬件/软件身份（`static_hardware_info`/`static_software_info`，value=1，信息在 labels） |

> 磁盘指标由 `supplementDiskStats` 直接读 `/proc/diskstats` 补 snapshot 未覆盖设备。静态信息经 `ipmitool`/`lscpu`/`dmidecode`/`npu-smi`/`nvidia-smi`/`pip`/`nvcc` 等命令采集，无对应工具时对应 label 为空（优雅降级）。可选 `-device 0,1` 过滤 NPU 设备，`-docker-container <name>` 在容器内采集软件版本。

---

## 6. 能效监控（dfee）

能效监控独立二进制 `catmonitor-dfee`（`features/dfee` package main），**只读消费** daemon 产出的 snapshot，渲染能效相关指标的实时图表 SPA。

### 6.1 启动

```bash
# 先启动 daemon 并开启 snapshot 生产（同 §4.1）
catmonitor daemon

# 启动 dfee 独立二进制（-snapshot-dir 须与 daemon snapshot.dir 一致）
catmonitor-dfee -addr :9528 -snapshot-dir /var/lib/catmonitor/snapshot
# 浏览器打开 http://localhost:9528/dfee/（实际端口见启动日志 "dfee server starting"）

# 启动 dfee 并开启 Prometheus exporter（:9333/metrics，详见 §5.5）
catmonitor-dfee -addr :9528 -exporter=enabled -exporter-port=9333 -snapshot-dir /var/lib/catmonitor/snapshot
```

> 端口 `:9528` 被占用时自动 +1 回退。dfee 不再注册到 web 路由，独立进程运行。

**启动参数**：

| 参数 | 默认 | 说明 |
|------|------|------|
| `-addr` | `:9528` | SPA 监听地址 |
| `-snapshot-dir` | `/var/lib/catmonitor/snapshot` | daemon snapshot 目录（须一致） |
| `-exporter` | `disabled` | Prometheus exporter 开关：`enabled`/`disabled` |
| `-exporter-port` | `9333` | exporter 监听端口（输出 `node_*`/`dsmi_*`/`ipmi_*`/`static_*`，见 §5.5） |
| `-device` | 空（全部） | NPU 设备过滤（逗号分隔，如 `0,1`） |
| `-docker-container` | 空 | docker 容器名，用于在容器内采集软件版本 |

### 6.2 功能

- **能效指标过滤**：从 snapshot 指标中过滤能效相关指标（NPU 频率/利用率/温度/电压/ECC/带宽、CPU 利用率推导、内存/磁盘/网络/机箱能效项），按部件分组展示
- **CPU 利用率推导**：8 个原始 jiffies 累计值在后端推导为 7 项利用率百分比（有状态 delta）
- **网络字节差值**：`rx/tx_bytes_total` 累计值转换为采集间增量
- **交互**：图表卡片拖拽重排 + 右下角手柄缩放、虚线对齐辅助（3px 吸附）、多选下拉筛选（NPU/磁盘/网络）、模块折叠
- **Prometheus exporter**：`-exporter=enabled` 时启动 `:9333/metrics`，将 snapshot 映射为 `node_*`/`dsmi_*`/`ipmi_*`/`static_*`（详见 §5.5），零外部库依赖
- **静态信息采集**：启动时一次性采集硬件/软件身份（OS/NPU 驱动/CANN/Python/PyTorch/vLLM 等），无工具时优雅降级
- **独立二进制**：`features/dfee` package main，只读 snapshot，不依赖 web 进程

> API：`GET /api/dfee` 返回过滤+推导后的图表数据。详见 [features/dfee/dfee_SPEC.md](../features/dfee/dfee_SPEC.md)。

---

## 7. 健康度评分

### 7.1 权重方案

| 场景 | CPU | Memory | Disk | GPU/NPU | 合计 |
|------|-----|--------|------|---------|------|
| 无 GPU/NPU（cpu_only） | 30 | 40 | 30 | — | 100 |
| 有 GPU/NPU（accelerated） | 10 | 20 | 10 | 60 | 100 |

自动检测：根据实际采集到的指标是否含 GPU/NPU 指标自动选择方案（非命令是否存在）。

### 7.2 等级

| 得分 | 等级 |
|------|------|
| 90-100 | Excellent |
| 75-89 | Good |
| 60-74 | Warning |
| 0-59 | Critical |

### 7.3 扣分

各部件按 High/Medium 指标设定扣分阈值（CPU 使用率/温度/Load/MCE、内存 CE/UCE/Swap/饱和度/碎片化、硬盘 SMART/I/O、GPU/NPU 利用率/温度/显存/ECC/功耗），触发即按满额分百分比扣分；多卡场景取最差卡。规则与阈值见 [features/health/HEALTH_SPEC.md](../features/health/HEALTH_SPEC.md)。

---

## 8. 数据格式

### 8.1 采集指标（JSONL，每行一条）

```jsonl
{"component":"cpu","name":"usage","value":45.2,"unit":"%","labels":{"core":"0"},"timestamp":"2026-07-25T10:30:00Z"}
```

文件路径：`{data_dir}/{component}_{date}.jsonl`，按天轮转，超 `max_file_age` 清理。

### 8.2 健康度（CLI 输出，不落盘）

`catmonitor health` 输出到 stdout（表格或 `-o json`），v0.3.3 起 daemon 不再周期评估/落盘健康度，故无 `health_*.jsonl` 文件产生：

```json
{"score":95,"grade":"Excellent","server_type":"accelerated","components":{"cpu":{"score":10,"max":10,"deductions":null},...},"timestamp":"..."}
```

> 如需持久化健康度，重定向 CLI 输出：`catmonitor health -o json >> health.jsonl`。

---

## 9. 无硬件环境的优雅降级

| 缺失项 | 行为 |
|--------|------|
| `nvidia-smi` | gpu 采集器返回空，collect 跳过，health 不列入 GPU |
| `npu-smi` / CANN | npu 返回 `npu_num=0`（无 `-tags dcmi` 时 DCMI 降级）；`error_code`/`card_drop` 不产生，faultsub 无事件 |
| `ipmitool` / BMC | chassis 采集器返回空，dfee 机箱图表 series 为空 |
| `smartctl` | disk 的 `smart_status` 缺失，health 扣分但不中断 |

> 无 NPU/GPU 环境的系统测试结果见 [docs/test_report.md](test_report.md)。

---

## 10. 扩展

- **新增部件采集器**：实现 `Collector` 接口并在 `init()` 注册 + `main.go` 加 `import _`，核心代码零修改。详见 [DESIGN.md §1.3](../DESIGN.md)。
- **Web 展示新部件**：在 daemon 侧注册新采集器（见上），snapshot 即包含其指标，web 仪表盘自动展示（web 是只读消费者，不再 blank import 采集器、不改 web 代码）。
- **新增能效图表**：`features/dfee/filter.go` 加分组条目 + `dfee.js` 加图表。

---

## 11. 排错与常见问题

| 现象 | 原因 / 解决 |
|------|-------------|
| `catmonitor daemon -v` 直接退出 | `-v/--verbose` 未实现；移除该参数，日志级别见源码 `setupLogger` |
| `catmonitor daemon --data-dir X` 退出 | `--data-dir` 未实现；数据目录改配置 `storage.data_dir` |
| `:9100` 被占用 | exporter 端口固定 `:9100`，释放占用进程或修改 `features/exporter` 中 `ServeMetrics(":9100", ...)` |
| `:9527` 被占用 | web 自动 +1 回退到 `:9528` 等，看启动日志 `web server starting addr=...` |
| `catmonitor-web` 报 `metrics catalog init failed` | web 须从仓库根目录运行（`metrics.Init` 加载相对路径 `configs/metrics.yaml`） |
| collect / :9100 指标数量变少 | 检查 `collection.min_priority`：`medium` 跳过 Low、`high` 仅 High |
| GPU/NPU/Chassis 无数据 | 对应命令（`nvidia-smi`/`npu-smi`/`ipmitool`）缺失即优雅降级返回空，非错误；`npu` 仍输出 `npu_num=0` |
| `health` 评分与 web 不一致 | CLI 因 `npu_num` 指标判 `accelerated`、web 据真实硬件判 `cpu_only`，口径不同（已知项） |
| DCMI/NPU CGo 编译失败 | `dcmi_cgo.go` 在 `-tags dcmi` 后，需真机 CANN SDK；默认构建排除即可 |
| 健康度无 `health_*.jsonl` 文件 | v0.3.3 起 daemon 不再落盘健康度；用 `catmonitor health -o json >> health.jsonl` 自行持久化 |
| dfee exporter `:9333` 无 dsmi/ipmi 数据 | 无 NPU/IPMI 硬件时优雅降级为空，符合预期；`static_*` 仍采集（命令可用字段有值，否则空） |
| dfee `static_software_info` 字段全空 | 软件采集依赖 `python3`/`pip`/`nvcc`/`npu-smi` 等命令；容器内可用 `-docker-container <name>` 经 `docker exec` 采集 |

> 无 NPU/GPU 环境的系统测试结果见 [docs/test_report.md](test_report.md)。

---

## 12. 容器化部署

CATMonitor 提供容器化方案，支持 NPU 与通用两种镜像。完整文档见 [docker/README.md](../docker/README.md)。

### 12.1 镜像类型

| 镜像 | 适用环境 | 说明 |
|------|---------|------|
| `catmonitor-npu` | 有 Ascend NPU | Debian/glibc 两步构建（golang 容器挂载 driver 编译 + debian 运行时打包），链接 `libdcmi.so`，采集 120 项 NPU 指标 |
| `catmonitor-generic` | 无 NPU（纯 CPU/GPU） | alpine 多阶段构建，纯 Go 编译，不依赖 NPU 驱动 |

### 12.2 构建与启动

```bash
cd CATMonitor
docker/docker/build.sh            # 自动检测 NPU driver（有则 npu，无则 generic）
docker/docker/build.sh npu         # 强制 NPU 镜像
docker/docker/build.sh generic    # 强制通用镜像

# Docker Compose 一键启动 daemon + web + dfee 三服务
docker compose -f docker/docker-compose.yml up -d
```

### 12.3 端口

| 容器端口 | 服务 | 端点 |
|---------|------|------|
| 9100 | daemon Prometheus exporter | `/metrics`、`/-/healthy`、`/-/ready` |
| 9101 | faultsub REST API（配置开启） | `/faultsub/*` |
| 9527 | web 仪表盘 | `/`、`/api/snapshot`、`/api/collectors` |
| 9528 | dfee SPA | `/`、`/api/dfee` |
| 9333 | dfee Prometheus exporter | `/metrics`（`node_*`/`dsmi_*`/`ipmi_*`/`static_*`） |

### 12.4 NPU 环境注意事项

- NPU 镜像**必须用 Debian（glibc）**，alpine（musl libc）与 `libdcmi.so` 不兼容
- 需挂载 `/usr/local/Ascend/driver` + `/usr/local/Ascend/nnae` 并设置 `LD_LIBRARY_PATH`
- `--device /dev/davinci*` 挂载 NPU 设备，`--privileged` 访问 ipmitool/dmidecode/smartctl

> 构建细节、docker run 手动启动、故障排查等完整内容见 [docker/README.md](../docker/README.md)。
