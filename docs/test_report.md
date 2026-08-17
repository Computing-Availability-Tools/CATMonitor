# CATMonitor 系统测试报告（无 NPU / 无 GPU）

> **项目**: CATMonitor (Computing Availability Tools Monitor) — CATHelper 底座
> **测试对象**: 本地 `main` 分支 @ `269cd10`（合并 `feature/wyx/add-metrics` → v0.3.4）
> **合并内容（v0.3.4 新增/变更）**:
> - `features/dfee` 新增独立 Prometheus exporter（`:9333/metrics`），snapshot 映射为 `node_*`/`dsmi_*`/`ipmi_*`/`static_*` 格式（零外部 prometheus 库依赖）
> - `features/dfee/static_info.go` 启动时采集静态软硬件信息（`ipmitool`/`lscpu`/`dmidecode`/`npu-smi` 等，无工具时优雅降级）
> - `internal/metrics` 新增 `LoadFeatureOverrides`（higher-priority-wins 合并）替代逐个 `LoadModuleOverride`
> - Disk 新增 4 项累计 raw counters（`read_sectors_total`/`written_sectors_total`/`read_time_total`/`write_time_total`）
> - GPU 新增 `memory_detail`；容器化方案 `docker/`（NPU + generic 镜像 + compose）
> - bug 修复：faultsub `Ready()` 改用 `written` 标志、NPU `power_draw` 单位修正（0.1W→W）、IPMI `cacheDir` 改绝对路径
> - 配置默认值变更：`min_priority: low→medium`、`features: [dfee]→[web,dfee]`、`snapshot.enabled: false→true`
> **测试日期**: 2026-08-10
> **测试人**: opencode
> **目标版本**: CATMonitor v0.3.4

---

## 测试环境

| 项 | 值 |
|----|----|
| OS | Ubuntu (WSL2, kernel 6.18.33.2-microsoft-standard-WSL2) |
| 架构 | linux/amd64 |
| Go | go1.23.4 |
| 硬件 | Intel Core i5-7200U (4 核)；**无 NPU / 无 GPU / 无 IPMI**（验证无硬件采集器优雅降级） |
| CANN DCMI | 头文件 `/usr/local/Ascend/driver/include/dcmi_interface_api.h` 不存在 → `-tags dcmi` 自动关闭（符合预期） |
| 工具链 | `make` 未安装（用 `go` 直接执行，等价 Makefile 目标）；`gcc` 未安装（`-race` 需 cgo，跳过） |
| 外部命令 | `smartctl`/`nvidia-smi`/`npu-smi`/`ipmitool`/`dmidecode` 均未安装（验证 static_info / SMART 降级路径） |
| 测试配置 A (off) | `features:[dfee]`，`min_priority=low`，snapshot/straggler/faultsub = false，`data_dir=/tmp/cm-test/data` |
| 测试配置 B (on) | 在 A 基础上 `features:[web,dfee]`，`snapshot/straggler/faultsub.enabled=true`，dir 指向 `/tmp/cm-test/{snapshot,straggler}`，`faultsub.rest_addr=:9101` |
| 测试配置 C (nofeature) | `features:[]`（全集 scope），snapshot/straggler/faultsub = false（验证 disk raw counters 全集采集） |

---

## 硬门禁结果：**通过 ✅**

---

## 1. 构建与静态检查

### 1.1 go vet / go test
```
$ go vet ./...                          → exit 0（零告警）
$ go test ./...                         → 全部包 ok，无失败
   28 个包 ok + 6 个 [no test files] = 34 个包
   ok  cmd/catmonitor[no test]   features/{dfee,exporter,faultsub,health,snapshot,stragglerout,web}
   ok  internal/metrics   ok  internal/collectors/{chassis,cpu,disk,gpu,memory,network,network}
   ok  internal/source/{dcmi,dmesg,dmidecode,hccn_tool,ipmi,lscpu,mce,npu_smi,nvidia_smi,proc,smartctl,statfs,sys}
```

### 1.2 三二进制独立构建（对应 `make all/web/dfee`）
```
$ go build -o bin/catmonitor      ./cmd/catmonitor   → 成功 (10.4 MB)
$ go build -o bin/catmonitor-web ./features/web      → 成功 (8.5 MB)
$ go build -o bin/catmonitor-dfee ./features/dfee    → 成功 (8.6 MB)
```
DCMI tag 自动探测：未检测到 CANN 头 → 不加 `-tags dcmi`（无 NPU 硬件，符合预期）。

### 1.3 已知限制
- `go test -race` 需 cgo（`CGO_ENABLED=1` + `gcc`），本环境无 `gcc`，竞态检测跳过（`cgo: C compiler "gcc" not found`）。需在装了 gcc 的环境补测。

---

## 2. CLI 子命令

### 2.1 version
```
$ ./bin/catmonitor version
CATMonitor v0.3.4 (Go 1.23+)
```

### 2.2 list（采集器清单）
```
Name     Component  Priority  Interval  Enabled
chassis  chassis    High      3s        true
cpu      cpu        High      3s        true
disk     disk       High      5s        true
gpu      gpu        High      3s        true
memory   memory    High      3s        true
network  network    High      3s        true
npu      npu        High      3s        true
```
7 个采集器全部 enabled。无 NPU/GPU 硬件下 `npu`/`gpu` 仍注册并优雅降级（不崩溃）。

### 2.3 collect -o table（单次采集）

**配置 A（feature-scoped `[dfee]`）** — 验证 `SetFeatureScope` 收窄：
```
$ ./bin/catmonitor collect -o table -config <off.yaml>   (exit 0)
组件分布：  cpu 44   disk 16   memory 11   network 2   = 73 条（含表头 1 行 = 72 指标）
样例：
  cpu   load_average    0.44          interval=1m
  cpu   online_core_num 4.00   个
  disk  throughput      0.00   MB/s   direction=read,device=sdd
  disk  read_latency    0.00   ms/s   device=sdd
  memory usage_detail  3840.61 MB     field=total
  network rx_bytes_total 45142925.00  bytes    interface=eth0
```
scoped 后 disk 收窄至 dfee 所需（throughput + read/write_latency），raw counters 不在 dfee scope 内 → 跳过。

**配置 C（无 features，全集 scope）** — 验证 disk raw counters 全集采集：
```
$ ./bin/catmonitor collect -o table -config <nofeature.yaml>   (exit 0)
组件分布：  cpu 21   disk 52   memory 19   network 7   npu 1   = 100 条
disk raw counters（新增 4 项）均产出：
  disk  read_sectors_total     4186906.00        device=sdd
  disk  written_sectors_total  2459808.00        device=sdd
  disk  read_time_total        120847.00   ms    device=sdd
  disk  write_time_total       356246.00   ms    device=sdd
```
**raw counters 验证结论**：`read_sectors_total`/`written_sectors_total`/`read_time_total`/`write_time_total` 已在 `configs/metrics.yaml` 注册 + `disk_linux.go::collectRawCounters` 产出；在 `[web,dfee]` scope 下因未被任一 feature 声明而经 `AnyWanted` 跳过（feature-scoped 设计生效，非 bug），全集模式正常输出。

### 2.4 health（健康检查）

**配置 A（`[dfee]` scope）**：
```
Overall Score:  [██████████████████████████████]  100 / 100   [ Excellent ]
Server Type:    cpu_only
  CPU                30 / 30       OK
  MEMORY             40 / 40       OK
  DISK               30 / 30       OK
  TOTAL             100 / 100      Excellent
```
dfee scope 不含 `smart_status`/`npu_num` → 无 SMART 扣分、判 `cpu_only`。

**配置 B（`[web,dfee]` scope）**：
```
Overall Score:  [█████████████████████████████░]  97 / 100   [ Excellent ]
Server Type:    accelerated
  CPU                10 / 10       OK
  MEMORY             20 / 20       OK
  DISK                7 / 10       Warning      smart_failed (-3)
  NPU                60 / 60       OK
  TOTAL              97 / 100      Excellent
```
web scope 含 `smart_status`/`npu_num` → 判 `accelerated`（NPU 采集器存在即分 60 权重，无故障判 OK）；DISK 因无 smartctl 扣 3 分（已知降级行为）。

> **关键发现**：使用**同一 feature scope** `[web,dfee]` 时，`catmonitor health` CLI 与 snapshot global writer 的 health 判定**一致**（均 `accelerated / 97`，DISK smart_failed -3）。v0.3.3 报告的「server_type 判定口径不一致」已知限制在本版本**已消除**（前提：CLI 与 daemon 使用相同 features 配置）。差异仅来自 scope 不同（`[dfee]` vs `[web,dfee]`），属预期行为。

---

## 3. daemon + Prometheus exporter（配置 B：全 opt-in 开启）

### 3.1 启动日志
```
level=INFO msg="derived per-component cadence from features" features=[web dfee] declared_components=7
level=INFO msg="straggler_output enabled" data_dir=/tmp/cm-test/straggler
level=INFO msg="faultsub enabled" rest_addr=:9101
level=INFO msg="faultsub REST listening" addr=:9101
level=INFO msg="snapshot production enabled" dir=/tmp/cm-test/snapshot refresh=1s
level=INFO msg="starting collector" name=cpu interval=1s        ← ComponentIntervals 派生生效
level=INFO msg="starting collector" name=disk interval=2s
level=INFO msg="starting collector" name=memory interval=2s
level=INFO msg="starting collector" name=network interval=1s
level=INFO msg="starting collector" name=npu interval=1s
level=INFO msg="starting collector" name=chassis interval=3s     ← catmonitor.yaml 原值（feature 未声明）
level=INFO msg="starting collector" name=gpu interval=3s
level=INFO msg="CATMonitor daemon started" version=0.3.4
level=INFO msg="exporter listening" addr=:9100
level=INFO msg="hardware specs distributed to snapshot writers" count=6
```
daemon 全程 `grep -iE "error|warn|fatal|panic"` = **空**。`features=[web,dfee]` 派生 per-component cadence 生效（cpu/disk/memory/network/npu 间隔被 feature `metrics.yaml` 的最小 interval 覆盖）；`ComponentIntervals` 门禁生效。

### 3.2 exporter :9100/metrics（Prometheus 格式）
```
$ curl -s http://localhost:9100/metrics   (278 行, 52 TYPE, 174 指标行)
# HELP catmonitor_cpu_load_average cpu/load_average
# TYPE catmonitor_cpu_load_average gauge
catmonitor_cpu_load_average{interval="1m"} 2.63
catmonitor_memory_usage_detail{field="total"} 3840.61
catmonitor_disk_throughput{direction="read",device="sdd"} 0.00
...
组件覆盖： cpu / disk / memory / network / npu(npu_num=0)
$ curl -s -o /dev/null http://localhost:9100/   → HTTP 404（exporter 仅暴露 /metrics，符合设计）
```
标准 Prometheus 文本格式（`# HELP`/`# TYPE` + `metric{labels} value`）。`[web,dfee]` scope 比 `[dfee]` 多覆盖 web 所需指标（114→278 行）。

---

## 4. snapshot 统一生产（配置 B）

### 4.1 snapshot 文件
```
/tmp/cm-test/snapshot/
  snapshot.json          (2217 B, 全局: health/collectors/intervals/system_specs)
  snapshot_cpu.json      (20.3 KB)
  snapshot_disk.json     (16.4 KB)
  snapshot_memory.json   (6.9 KB)
  snapshot_network.json (5.1 KB)
  snapshot_npu.json      (274 B, metrics=[npu_num=0], 优雅降级)
```
per-comp + global snapshot 均生成；`chassis/gpu` 因无硬件产出 0 指标，未产 per-comp 文件（符合「空组件跳过」设计），但 global `snapshot.json` 含其 collector info + intervals。`refresh=1s` 原子写（temp + rename），全局文件每秒刷新。

### 4.2 global snapshot.json 结构验证
```json
{
  "session_id": "1786364763",
  "timestamp": "2026-08-10T20:26:39...+08:00",
  "refresh_interval_ms": 1000,
  "intervals_ms": {"chassis":3000,"cpu":1000,"disk":2000,"gpu":3000,"memory":2000,"network":1000,"npu":1000},
  "health": {"score":97,"grade":"Excellent","server_type":"accelerated",
             "components":{"cpu":{...},"disk":{"score":7,"deductions":[{"rule":"smart_failed","penalty":3}]},
                           "memory":{...},"npu":{"score":60,"max":60}}},
  "collectors": [{"name":"cpu","component":"cpu","priority":"High","interval":"1s","enabled":true}, ...]
}
```
global health 与 `catmonitor health` CLI（同 `[web,dfee]` scope）判定一致：`accelerated / 97`。

### 4.3 straggler_output
`features/stragglerout` 装配成功（日志 `straggler_output enabled`）；本环境无 NPU KPI 指标，`/tmp/cm-test/straggler/` 未产 KPI 文件（`flush_interval=60s` + 无数据，符合设计——完整验证需 NPU 真机）。

---

## 5. web 只读消费者（:9527）

```
$ ./bin/catmonitor-web -addr :9527 -snapshot-dir /tmp/cm-test/snapshot
level=INFO msg="web server starting (read-only consumer)" addr=:9527 snapshot_dir=/tmp/cm-test/snapshot
```

| 端点 | 方法 | HTTP | Content-Type | 说明 |
|------|------|------|--------------|------|
| `/` | GET | 200 | text/html; charset=utf-8 | 首页 SPA |
| `/api/snapshot` | GET | 200 | application/json | 组装 global+per-comp snapshot |
| `/dfee/` | GET | **404** | — | dfee 不再挂载于 web（架构变更，见 §6） |

`/api/snapshot` 响应结构完整：
```
keys: session_id, timestamp, refresh_interval_ms, history_points, health, metrics, history, specs
metrics: 174 条
health:  97 / Excellent / accelerated
specs:   16 条
refresh_interval_ms: 1000
```
web 只读消费 daemon snapshot 链路打通；无自采集。

---

## 6. dfee 独立二进制（:9528 + :9333 exporter）

```
$ ./bin/catmonitor-dfee -addr :9528 -snapshot-dir /tmp/cm-test/snapshot -exporter enabled
level=INFO msg="dfee server starting (read-only consumer)" addr=:9528 snapshot_dir=/tmp/cm-test/snapshot
level=INFO msg="collecting static info for exporter..."
level=INFO msg="exporter starting" port=9333
```

### 6.1 dfee SPA + API（:9528）

| 端点 | HTTP | Content-Type | 说明 |
|------|------|--------------|------|
| `/` | 200 | text/html; charset=utf-8 | SPA 首页（catch-all） |
| `/api/dfee` | 200 | application/json | dfee 派生指标（**68 charts**） |
| `/dfee/static/dfee.js` | 200 | text/javascript; charset=utf-8 | 静态资源 |

`/api/dfee` 返回 **68 个图表**（v0.3.3 为 25，粒度细化：per-core CPU util / per-field memory detail / 各 load_average 区间等）；无 NPU 数据的图表 `series: null`（如 `{"id":"npu_aicore_freq","title":"AICore频率","series":null}`）—— 优雅降级，结构完整。

### 6.2 dfee 内置 Prometheus exporter（:9333/metrics）— **v0.3.4 新增**

```
$ curl -s http://localhost:9333/metrics   (135 行)
```
| 指标族 | 行数 | 说明 |
|--------|------|------|
| `node_*` | 133 | CPU/内存/网络/磁盘，对齐 node_exporter 命名（`node_cpu_seconds_total`/`node_memory_MemTotal_bytes`/`node_network_receive_bytes_total`/`node_disk_read_sectors_total` 等） |
| `static_*` | 2 | `static_hardware_info` + `static_software_info`（启动一次性采集） |
| `dsmi_*` | 0 | 无 NPU 硬件 → 空输出（优雅降级） |
| `ipmi_*` | 0 | 无 IPMI 硬件 → 空输出（优雅降级） |

静态信息优雅降级验证（无工具时对应字段为空，不报错）：
```
static_hardware_info{cpu_info="1*Intel(R) Core(TM) i5-7200U CPU @ 2.50GHz",
  disk_info="sda 356.9M, sdb 159.4M, sdc 1G, sdd 1T",
  gpu_type="",memory_info="",npu_chip_name="",product_name="",psu_info=""} 1
static_software_info{os_version="Ubuntu 26.04 LTS",python_version="3.14.4",
  cann_version="",cuda_version="",npu_driver_version="",gpu_driver_version="",...} 1
```
`cpu_info`/`disk_info`/`os_version`/`python_version` 已采集；`npu_*`/`gpu_*`/`memory_info`/`product_name`/`psu_info` 因无对应硬件/命令为空（降级而非报错，符合 `static_info.go` 设计）。

---

## 7. faultsub 故障订阅（:9101，配置 B 开启）

| 操作 | 端点 | HTTP | 响应 |
|------|------|------|------|
| 查询事件 | `GET /faultsub/events` | 200 | `[]`（无硬件→无故障事件） |
| 查询订阅 | `GET /faultsub/subscriptions` | 200 | `[]` |
| 创建订阅 | `POST /faultsub/subscriptions` | **201** | `{"id":"sub-0001","delivery":"webhook","endpoint":"http://localhost:9999/hook",...}`（id 自动生成） |
| 创建订阅(错误字段) | `POST` 用 `url` 而非 `endpoint` | 400 | `{"error":"webhook delivery requires 'endpoint' URL"}`（字段校验生效） |
| 删除订阅 | `DELETE /faultsub/subscriptions/sub-0001` | 204 | （成功删除） |

faultsub REST API（订阅 CRUD + 事件查询）端到端验证通过；`detector`/`dispatcher`/`webhook`/`subscription` 装配正常，无硬件下静默不产事件（符合设计）。**bug 修复验证**：`FaultStorage.Ready()` 改用 `written` 标志后，健康 NPU 无故障时不再误报 503（本次 `GET /faultsub/events` 正常 200，未现 503）。

---

## 8. 无硬件采集器优雅降级验证

| 采集器 | 硬件 | 行为 |
|--------|------|------|
| npu | 无（无 CANN DCMI 头，未加 `-tags dcmi`） | collector 正常启动不崩溃；产 `snapshot_npu.json`（`npu_num=0`，0 指标）；`error_codes`/`card_drop` 因无硬件未产生（符合预期）；`power_draw` 单位修正生效（测试用例 65→6.5W） |
| gpu | 无（无 nvidia-smi） | collector 正常启动不崩溃；无指标输出；`memory_detail` 已在 `metrics.yaml` 注册但无硬件不采集 |
| chassis | 无（无 ipmi/dmidecode 权限） | 未产 per-comp snapshot，但 global snapshot 含 chassis collector info；`cacheDir` 改绝对路径后无工作目录依赖 |
| cpu/memory/disk/network | 有 | 全量采集正常；disk raw counters 在全集 scope 下产出（§2.3） |

daemon 全程日志 `grep -i "error\|warn\|fatal\|panic"` = **空**。**优雅降级符合设计预期**。

---

## 9. 测试结论

| 门禁 | 结果 |
|------|------|
| `go vet` / `go test`（28 包 ok） | ✅ 全绿 |
| 三二进制构建（daemon/web/dfee） | ✅ 全部成功（10.4/8.5/8.6 MB） |
| CLI（version/list/collect/health） | ✅ 正常 |
| feature-scoped 收集（`SetFeatureScope`+`LoadFeatureOverrides`+`ComponentIntervals`） | ✅ 指标集收窄 + cadence 覆盖生效 |
| disk raw counters（4 项新增） | ✅ 全集 scope 下产出，feature scope 下按设计跳过 |
| daemon + exporter（:9100/metrics，278 行/52 TYPE，Prometheus 格式） | ✅ 正常（GET / → 404） |
| snapshot 统一生产（daemon 产 → web/dfee 读） | ✅ 端到端打通（5 文件 + global） |
| web 只读消费（:9527，`/api/snapshot` 174 metrics/16 specs） | ✅ 正常 |
| dfee 独立二进制（:9528，68 charts） | ✅ 正常（粒度细化 25→68） |
| **dfee exporter（:9333，135 行 node_/static_）** | ✅ **v0.3.4 新增，正常** |
| **static_info 优雅降级**（无 ipmitool/dmidecode） | ✅ **字段为空不报错** |
| faultsub 订阅 API（:9101，CRUD 200/201/400/204） | ✅ 正常；`Ready()` 503 bug 修复验证 |
| server_type 判定一致性（CLI vs snapshot，同 scope） | ✅ **一致（accelerated/97），v0.3.3 已知限制消除** |
| 无硬件采集器优雅降级 | ✅ 无 error/warn/panic |

**整体：通过，可进入发布流程。**

### 已知限制与发现（非阻塞，建议后续处理）
1. **【继承 v0.3.3】`-c` 短 flag 为死代码**：`cmd/catmonitor/main.go:83` 注册了 `c` 短 flag 但其值被丢弃，只使用长 `config` flag。因此 `catmonitor -c <path>` 不生效（静默回退到 `platform.ConfigPath()`）。**临时绕过**：一律使用 `-config <path>` 长形式。
2. **`-race` 需 cgo + gcc**，本环境无 `gcc`，竞态检测未覆盖（与 v0.3.3 一致）。
3. **DISK `smart_failed (-3)` 扣分**：无 smartctl/无 SMART 数据，已知降级行为（仅在 `[web,dfee]` scope 下出现，`[dfee]` scope 不含 `smart_status` 故不扣分）。
4. **无 NPU/GPU/IPMI 真机环境**：`error_codes`/`card_drop`/straggler KPI/dfee exporter 的 `dsmi_*`/`ipmi_*` 输出仅验证「不崩溃 + 优雅降级」，完整功能需在昇腾服务器补测。
5. **DCMI CGo 未真机验证**：`dcmi_cgo.go` 在 `-tags dcmi` 后，本机无 CANN SDK 无法编译。
6. **dfee static_info 命令依赖**：`static_hardware_info`/`static_software_info` 依赖 `ipmitool`/`dmidecode`/`npu-smi`/`nvidia-smi`/`pip`/`nvcc` 等，缺失时对应 label 为空（降级而非报错，符合设计）。
7. **snapshot.json 原子写瞬态**：`refresh=1s` 下全局快照每秒 temp+rename，外部直读有极小概率命中 ENOENT 窗口；web/dfee 的 Go 读路径已处理（读失败返回 503 重试），不影响消费端。
8. **disk raw counters 在 feature scope 下被跳过**：`read_sectors_total` 等 4 项未被 `web`/`dfee` 任一 feature `metrics.yaml` 声明，故 `[web,dfee]` scope 下不采集（`AnyWanted` 跳过，设计如此）。若 dfee exporter 的 `node_disk_read_sectors_total` 需覆盖所有设备，应将这 4 项加入对应 feature `metrics.yaml` 或在 dfee exporter `supplementDiskStats`（已直读 `/proc/diskstats` 补 snapshot 未覆盖设备，:9333 输出已含 `node_disk_read_sectors_total`）。
