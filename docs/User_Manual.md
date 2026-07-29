# CATMonitor 使用手册 (User Manual)

> 本文档说明 CATMonitor 的构建、安装、配置、命令行、Web 仪表盘、能效监控、Prometheus 导出与运维用法。
> 功能规格见 [SPEC.md](../SPEC.md)，架构设计见 [DESIGN.md](../DESIGN.md)，指标清单见 [CATMonitor_indi_list.md](CATMonitor_indi_list.md)。

---

## 1. 构建与安装

### 1.1 依赖

- Go 1.21+
- （可选）NPU 服务器：CANN SDK（`libdcmi.so`），用 `-tags dcmi` 启用 DCMI CGo 采集
- （可选）`nvidia-smi` / `npu-smi` / `hccn_tool` / `ipmitool` / `smartctl`：对应采集器无该命令时优雅降级（返回空，不崩溃）

### 1.2 编译

```bash
# 主守护进程二进制
make build
# 等价于
go build -o bin/catmonitor ./cmd/catmonitor

# Web 仪表盘二进制
go build -o features/web/bin/catmonitor-web ./features/web

# NPU 服务器启用 DCMI CGo 采集
go build -tags dcmi -o bin/catmonitor ./cmd/catmonitor

# Windows 交叉编译
GOOS=windows go build -o bin/catmonitor.exe ./cmd/catmonitor
GOOS=windows go build -o features/web/bin/catmonitor-web.exe ./features/web
```

> 默认构建排除 CGo（`dcmi_cgo.go` 在 `//go:build cgo && linux && dcmi` 后），无 CANN SDK 也能编译；NPU 的 DCMI 指标在无 `-tags dcmi` 时优雅降级。

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
  enabled: true
  interval: 5s
  weight_scheme: auto      # auto | cpu_only | accelerated_8card | accelerated_4card
  stress:
    enabled: false         # CLI 总开关
    web_enabled: false     # Web 触发附加开关
    script_path: features/health/stress/benchmark_check.sh
    report_path: features/web/data/stress-latest.json
    default_benchmarks: [stream]
    benchmarks:
      stream: { enabled: false, timeout: 1m }
      hpl: { enabled: false, timeout: 2h }
      hpcg: { enabled: false, result_dir: "", timeout: 3m } # 启用前填写结果目录
```

### 配置路径

| 平台 | 默认配置路径 | 默认数据目录 |
|------|-------------|-------------|
| Linux | `/etc/catmonitor/catmonitor.yaml` | `/var/lib/catmonitor/data` |
| Windows | `C:\ProgramData\catmonitor\catmonitor.yaml` | `C:\ProgramData\catmonitor\data` |

可用 `-c` 覆盖配置路径。指标采集目录 `metrics.yaml` 加载顺序：环境变量 `CATMONITOR_METRICS` → 配置目录下 `metrics.yaml` → 开发回退 `configs/metrics.yaml`。

---

## 3. 命令行

```
catmonitor [command] [flags]

Commands:
  daemon       启动守护进程（持续采集 + 健康度 + Prometheus 导出）— 默认
  collect      单次采集所有指标快照
  health       执行一次健康检查
  list         列出所有已注册采集器
  version      显示版本信息

Flags:
  -c, --config      配置文件路径
  -o, --output      输出格式: json|table
  -h, --help        帮助信息（解析后即退出）
```

### 3.1 version

```bash
$ catmonitor version
CATMonitor v0.3.2 (Go 1.23+)
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

### 3.5 daemon — 守护进程

```bash
catmonitor daemon                       # 默认配置前台运行
catmonitor daemon -c /etc/catmonitor/my.yaml
catmonitor daemon -v                    # 详细日志
```

daemon 启动后：
- Scheduler 按各采集器 `interval` 周期采集 → 写 JSONL（`{data_dir}/{component}_{date}.jsonl`）
- `exporter.CachingStorage` 包装存储层，一次采集同时落盘 + 缓存到内存
- HTTP `/metrics` 端点监听 `:9100`（见 §5）
- 按 `health.interval` 周期评估健康度 → 写 `health_{date}.jsonl`

捕获 `SIGINT`/`SIGTERM` 优雅退出。

### 3.6 health stress run — 显式健康压测

第一版仅在 Linux 运行，Windows 构建可用但执行返回 `unsupported`。先在每台
机器的 `features/health/stress/benchmark_check.sh` 中写入实际二进制全路径、
环境变量、MPI/NUMA 命令，再在 `health.stress.benchmarks` 启用对应项目。
三类 benchmark 的执行路径都不放入 YAML；HPCG 只配置本次结果文件的读取
目录。HPL 正常完成后会上报 N、NB、P、Q、MPI 进程数、耗时和 GFLOPS。

```bash
bash -n features/health/stress/benchmark_check.sh
catmonitor health stress run --help
catmonitor health stress run --bench stream -c configs/catmonitor.yaml -o table
catmonitor health stress run --bench hpl,hpcg -c configs/catmonitor.yaml -o json
```

`enabled` 是 CLI 和 Manager 的总开关，`web_enabled` 只是额外开放 Web
提交，两者职责不同。STREAM、HPL、HPCG 达到 YAML 时限后均会停止本机
进程组并记录 `time_limit_reached`，该状态按通过处理，允许没有最终性能值。
命令提前报错或正常结束后解析不到必需结果才是 `unhealthy`。HPCG 只读取
本次作业新增或变化的结果文件。压测结果不直接计入健康总分，但高负载可能
使同期实时健康分暂时变化。

完整的 WSL 构建、跨平台测试、Web 预览、Linux 节点配置及 STREAM/HPL/HPCG
实机验收步骤见
[health/stress 执行与测试指南](../features/health/stress/STRESS_TEST_GUIDE.md)。
具体节点的资产路径、MPI/NUMA 参数和性能结果应由部署方单独维护，不提交到
开源仓库。

---

## 4. Web 仪表盘（catmonitor-web）

独立的 Web 仪表盘二进制，可视化单机健康度与各部件指标。与采集守护进程/CLI 解耦，以 `features/web/data/snapshot.json` 为读写边界。

### 4.1 启动

```bash
# 仓库根目录运行（config.yaml 中 snapshot_path 为相对路径）
./features/web/bin/catmonitor-web -config features/web/config.yaml
# 浏览器打开 http://localhost:9527（实际端口见启动日志 "web server starting"）
```

> 端口 `:9527` 被占用时自动 +1 回退（`:9528`…）。

### 4.2 页面

- **概览页**（`/`）：整体健康度、设备规格、部件卡片，以及最近健康压测摘要
- **部件详情页**（`#/<component>`，如 `#/cpu`）：部件得分/扣分项 + 趋势面板（该部件全部历史序列 sparkline）+ 全部指标表
- **健康压测**（`#/stress`）：选择已启用项目、可选缩短本次超时、启动/停止并查看分行结果
- **能效监控**（`/dfee/`）：见 §6

### 4.3 REST API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/snapshot` | 最新快照（健康度 + 指标 + 历史 + 静态规格） |
| GET | `/api/collectors` | 采集器注册表元数据 |
| GET / POST | `/api/config` | 读取 / 更新刷新间隔（热生效 + 持久化到 `runtime.json`） |
| POST | `/api/refresh` | 请求立即采集 |
| GET | `/api/health/stress/config` | 可由 Web 选择的压测项目与超时上限 |
| GET | `/api/health/stress/latest` | 最近一份压测报告 |
| POST | `/api/health/stress/runs` | 提交压测作业 |
| GET | `/api/health/stress/runs/{id}` | 查询作业 |
| POST | `/api/health/stress/runs/{id}/cancel` | 停止作业 |

Web 提交仅在 `health.stress.enabled: true`、`web_enabled: true` 且 Web
绑定 `127.0.0.1`/`localhost`/`::1` 时开放。远端 Windows 浏览器建议经
SSH 隧道访问，例如
`ssh -L 9527:127.0.0.1:9527 root@<Linux节点>`，再打开
`http://127.0.0.1:9527`。

直接调用启动/取消 API 时还必须使用
`Content-Type: application/json` 和
`X-CATMonitor-Action: health-stress`；浏览器请求必须同源。例如：

```bash
curl -H 'Content-Type: application/json' \
     -H 'X-CATMonitor-Action: health-stress' \
     -d '{"benchmarks":["stream"],"timeout_seconds":60}' \
     http://127.0.0.1:9527/api/health/stress/runs
```

> 详细设计与扩展机制见 [features/web/Web_SPEC.md](../features/web/Web_SPEC.md)。

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

---

## 6. 能效监控（dfee）

能效监控模块 `features/dfee` 提供 `/dfee/` SPA，专门展示能效相关指标的实时图表。

### 6.1 启动

dfee 随 `catmonitor-web` 启动自动挂载（`features/web/server.go` 注册 `dfee.Register`）。访问 `http://localhost:9527/dfee/`。

### 6.2 功能

- **能效指标过滤**：从全量指标中过滤能效相关指标（NPU 频率/利用率/温度/电压/ECC/带宽、CPU 利用率推导、内存/磁盘/网络/机箱能效项），按部件分组展示
- **CPU 利用率推导**：8 个原始 jiffies 累计值在后端推导为 7 项利用率百分比（有状态 delta）
- **网络字节差值**：`rx/tx_bytes_total` 累计值转换为采集间增量
- **交互**：图表卡片拖拽重排 + 右下角手柄缩放、虚线对齐辅助（3px 吸附）、多选下拉筛选（NPU/磁盘/网络）、模块折叠
- **独立 SPA**：不修改现有 web 业务代码，只读 `snapshot.json`

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

### 8.2 健康度（JSONL）

```jsonl
{"score":95,"grade":"Excellent","components":{"cpu":{"score":10,"max":10,...},...},"timestamp":"..."}
```

---

## 9. 无硬件环境的优雅降级

| 缺失项 | 行为 |
|--------|------|
| `nvidia-smi` | gpu 采集器返回空，collect 跳过，health 不列入 GPU |
| `npu-smi` / CANN | npu 返回 `npu_num=0`（无 `-tags dcmi` 时 DCMI 降级） |
| `ipmitool` / BMC | chassis 采集器返回空，dfee 机箱图表 series 为空 |
| `smartctl` | disk 的 `smart_status` 缺失，health 扣分但不中断 |

> 使用 `go test ./...` 执行完整单元测试；硬件相关系统测试应在目标环境执行并由 CI 或发布流程保存结果。

---

## 10. 扩展

- **新增部件采集器**：实现 `Collector` 接口并在 `init()` 注册 + `main.go` 加 `import _`，核心代码零修改。详见 [DESIGN.md §1.3](../DESIGN.md)。
- **Web 接入新部件**：`features/web/main.go` 加一行 blank import，导航/卡片/详情页自动出现。
- **新增能效图表**：`features/dfee/filter.go` 加分组条目 + `dfee.js` 加图表。
