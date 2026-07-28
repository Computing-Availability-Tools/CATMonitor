# CATMonitor 系统测试报告（无 NPU / 无 GPU）

> **项目**: CATMonitor (Computing Availability Tools Monitor)
> **测试对象**: 本地 `main` 分支 @ `e1c14c4`（No-FF 合并 `feature/wyx/add-metrics`）+ `b2181e6`（npu 非 linux 桩签名热修）
> **合并提交**: *Merge feature/wyx/add-metrics into main: 基于优先级的采集粒度控制 (min_priority + AnyWanted DI) + 采集器修正*
> **代码版本常量**: v0.3.3（`cmd/catmonitor/main.go:33`）
> **测试类型**: 无 NPU / 无 GPU 真机环境下的系统测试 + 单元测试回归 + Windows 交叉编译验证
> **日期**: 2026-07-28
> **发布人**: sunnytao
> **测试执行**: OpenCode + Go testing 框架 + 手动系统探测（curl/进程行为观察）

---

## 1. 测试概述

### 1.1 测试目标

在缺少 NPU 硬件与 GPU 硬件的 Linux 主机上，验证 v0.3.3 合并后代码的端到端可用性：

- 合并后两个可执行二进制均能编译并通过 `go vet`
- **Windows 交叉编译**恢复通过（修复 v0.3.2 起遗留的 `npu_other.go` 签名不匹配）
- `catmonitor` CLI（version / list / collect / health / daemon）行为正确
- **新增采集粒度控制**（`collection.min_priority` + `AnyWanted` DI）装配生效，默认 `low` 全采
- daemon 移除周期健康检查后仍正常采集 + Prometheus 导出
- `features/web` 仪表盘 + `features/dfee` 能效监控在 `:9527` 正常提供 SPA 与 API，**退出时清 snapshot**
- 无硬件的采集器（GPU / NPU / Chassis）**优雅降级**，不崩溃、不影响其它采集器
- 全量单元测试零回归

### 1.2 测试结果汇总

| 指标 | 结果 |
|------|------|
| 单元测试总数 | **263** |
| 通过 / 失败 / 跳过 | **263 / 0 / 0** |
| 通过率 | **100%** |
| `go build ./cmd/catmonitor` + `./features/web` | ✅ 通过 |
| `GOOS=windows go build ./...`（交叉编译） | ✅ 通过（已修复） |
| `go vet ./...` | ✅ 零告警 |
| 单元测试覆盖率区间 | 29.5% ~ 94.3% |
| `catmonitor collect` 一次性采集 | ✅ 92 指标（CPU/Memory/Disk/Network + NPU 计数） |
| `catmonitor health` 健康评估 | ✅ 95/100 [Excellent] |
| `:9100/metrics` Prometheus 导出 | ✅ HTTP 200，52 个 `# TYPE`，173 条指标行 |
| `:9527` web/dfee 仪表盘 | ✅ root/dfee/snapshot/collectors 全 HTTP 200 |
| `:9100/-/healthy` / `/-/ready` | ✅ 200 / 200 |
| GPU/NPU/Chassis 优雅降级 | ✅ 无崩溃、无 panic |

---

## 2. 测试环境

| 项目 | 配置 |
|------|------|
| 操作系统 | Linux (WSL2, x86_64) |
| 内核 | 6.18.33.2-microsoft-standard-WSL2 |
| CPU 逻辑核 | 4 |
| 内存 | ~3.9 GB |
| Go 版本 | go1.23.4 linux/amd64 |
| 网卡 | eth0 |
| 磁盘设备 | sda / sdb / sdc / sdd |
| nvidia-smi | ❌ 不存在（GPU 走空数据降级） |
| npu-smi | ❌ 不存在（NPU 走 `npu_num=0` 降级） |
| ipmitool / BMC | ❌ 不存在（Chassis 走空数据降级） |
| smartctl | ❌ 不存在（Disk `smart_status` 缺失扣分） |
| CANN SDK | ❌ 不存在（DCMI CGo 在 `dcmi` 构建标签后，默认排除） |

> 注：本环境无 NPU 硬件、无 CANN SDK、无 nvidia-smi、无 BMC、无 smartctl。NPU/GPU/Chassis 相关逻辑在系统测试中通过"优雅降级"验证，单元层面由 mock 驱动。

---

## 3. 编译与静态检查

| 检查项 | 命令 | 结果 |
|--------|------|:----:|
| catmonitor 构建 | `go build -o catmonitor ./cmd/catmonitor` | ✅ |
| web 构建 | `go build -o catmonitor-web ./features/web` | ✅ |
| 全量构建 | `go build ./...` | ✅ |
| Windows 交叉编译 | `GOOS=windows go build ./...` | ✅（已修复） |
| 静态检查 | `go vet ./...` | ✅ 零告警 |
| 版本输出 | `catmonitor version` → `CATMonitor v0.3.3 (Go 1.23+)` | ✅ |

- **CGo 隔离**：`internal/source/dcmi/dcmi_cgo.go` 使用 `//go:build cgo && linux && dcmi`，默认编译排除，本机无 CANN SDK 仍可构建。
- **非 Linux 隔离**：`internal/collectors/npu/npu_other.go` 使用 `//go:build !linux`；v0.3.3 热修将其 `collectDevice(devID int, ...)` 同步为 `collectDevice(dev npuDevice, ...)`，与 `npu_linux.go` 对齐，Windows 交叉编译恢复通过。
- **无硬件编译**：gpu/npu/chassis 采集器即使对应命令（nvidia-smi/npu-smi/ipmitool）缺失仍正常编译（运行期返回空/错误并降级）。

---

## 4. 系统测试结果

### 4.1 采集器注册（`catmonitor list`）

7 个采集器全部注册成功：

| Name | Component | Priority | Interval | Enabled |
|------|-----------|----------|----------|---------|
| chassis | chassis | High | 3s | true |
| cpu | cpu | High | 3s | true |
| disk | disk | High | 5s | true |
| gpu | gpu | High | 3s | true |
| memory | memory | High | 3s | true |
| network | network | High | 3s | true |
| npu | npu | High | 3s | true |

### 4.2 一次性采集（`catmonitor collect -o table`）

无 NPU/GPU 环境下采集到 **92 条指标**，无 stderr 报错：

| 采集器 | 产出指标数 | 说明 |
|--------|:----------:|------|
| disk | 44 | 真实数据（/proc/diskstats + statfs，4 设备 sda~sdd） |
| cpu | 21 | 真实数据（/proc/stat，4 核 + total + load_average） |
| memory | 19 | 真实数据（/proc/meminfo） |
| network | 7 | 真实数据（/proc/net/dev，eth0） |
| npu | 1 | `npu_num=0`（无 NPU 硬件，优雅返回计数 0） |
| gpu | 0 | nvidia-smi 不存在 → 返回空，**无崩溃** |
| chassis | 0 | ipmitool/BMC 不存在 → 返回空，**无崩溃** |

**代表值抽样**：

```
cpu        usage              0.00     %      core=total
cpu        load_average       1.62            interval=1m
memory     usage              72.69    %
disk       space_usage        29.14    %      device=drivers,mount_point=/usr/lib/wsl/drivers
network    error_count        0.00     次     interface=eth0,type=rx_err
network    error_count        5.00     次     interface=eth0,type=tx_drop
npu        npu_num            0.00     个
```

> **采集粒度控制验证**：默认配置 `collection.min_priority: low` 全量采集，92 条指标与 v0.3.2 一致。`AnyWanted` DI 在 daemon 与 `runCollect` 启动时注入（`metrics.SetCollectionThreshold` + `collector.SetWantedChecker(metrics.AnyWanted)`），`low` 阈值下所有优先级通过，无指标被预过滤跳过。
>
> **优雅降级验证**：缺硬件的 gpu/chassis 不产生指标且不 panic；npu 返回 `npu_num=0` 计数而非报错。

### 4.3 健康评估（`catmonitor health -o table`）

```
Overall Score:  [████████████████████████████░░]  95 / 100   [ Excellent ]
Server Type:    accelerated
Check Time:     2026-07-28 09:16:xx

  Component        Score / Max    Status       Deductions
  CPU                10 / 10       OK           -
  MEMORY             18 / 20       OK           swap>50% (-2)
  DISK                7 / 10       Warning      smart_failed (-3)
  NPU                60 / 60       OK           -
  TOTAL              95 / 100      Excellent
```

| 部件 | 得分 | 状态 | 说明 |
|------|:----:|:----:|------|
| CPU | 10/10 | OK | 正常 |
| MEMORY | 18/20 | OK | swap>50% 扣 2 分 |
| DISK | 7/10 | Warning | smartctl 缺失导致 smart_failed 扣 3 分 |
| NPU | 60/60 | OK | `npu_num=0` 存在 → 不扣分 |
| GPU | — | 未列入 | 无任何 GPU 指标，未进入评估 |

> 注：内存使用率在 ~80% 阈值附近波动，单次采集间 `usage>80%` 扣分可能触发（导致总分在 92~95 间浮动），属环境真实状态而非缺陷。`catmonitor health` CLI 因 NPU 采集器产出 `npu_num` 指标判定 `accelerated`；web 端 hwinfo 探测无真实 NPU 硬件判定 `cpu_only`，两处口径不同，建议后续统一（见第 6 节）。

### 4.4 Prometheus exporter（`catmonitor daemon` + `:9100/metrics`）

启动 daemon（`metrics.SetCollectionThreshold` + `collector.SetWantedChecker(metrics.AnyWanted)` 装配 + `cacheStore := exporter.NewCachingStorage(store)` + `go exporter.ServeMetrics(":9100", ...)`），等待采集后探测：

```
time=... level=INFO msg="exporter listening" addr=:9100
time=... level=INFO msg="CATMonitor daemon started" version=0.3.3
```

`curl http://localhost:9100/metrics`：

| 项 | 值 |
|----|----|
| HTTP 状态 | **200** |
| 响应体大小 | 14617 字节 |
| 指标行数（`catmonitor_*`） | 173 |
| `# TYPE` 行数 | 52 |
| 健康端点 `/-/healthy` | **200** |
| 就绪端点 `/-/ready` | **200** |

**格式合规性**：

- 所有指标统一 `catmonitor_{component}_{name}` 前缀，`-` `/` `.` 均被替换为 `_`
- 每组指标含 `# HELP` + `# TYPE` 头
- `_total` / `_time` 后缀指标正确识别为 `counter`，可用于 PromQL `rate()` / `increase()`
- 标签语法合规：`{direction="rx",interface="eth0"}`，键按字典序排序

**代表数据行**：

```
# HELP catmonitor_network_throughput network/throughput
# TYPE catmonitor_network_throughput gauge
catmonitor_network_throughput{direction="rx",interface="eth0"} 0
catmonitor_network_throughput{direction="tx",interface="eth0"} 0
catmonitor_network_error_count{interface="eth0",type="tx_drop"} 5
# HELP catmonitor_network_rx_bytes_total network/rx_bytes_total
# TYPE catmonitor_network_rx_bytes_total counter
catmonitor_npu_npu_num 0
```

> **结论**：`features/exporter`（CachingStorage + `/metrics` 端点）在无 NPU/GPU 环境下工作正常，输出符合 Prometheus 文本 exposition 格式。daemon v0.3.3 移除周期健康检查 goroutine 后，采集 + 导出链路不受影响。

### 4.5 web / dfee 仪表盘（`features/web` @ `:9527`）

启动 `catmonitor-web`，绑定 `:9527`，硬件规格采集 6 类后开始周期采集（5s）。探测各端点：

| 端点 | HTTP | 大小 | 说明 |
|------|:----:|-----:|------|
| `GET /` | 200 | 1407 B | 主仪表盘 SPA |
| `GET /dfee/` | 200 | 940 B | 能效监控 SPA |
| `GET /api/snapshot` | 200 | 25980 B | 快照 JSON（含 health/metrics/history/specs） |
| `GET /api/collectors` | 200 | — | 7 个采集器清单 |

**snapshot 结构**（`features/web/data/snapshot.json`）：

- 顶层键：`session_id / timestamp / refresh_interval_ms / history_points / health / metrics / history / specs`
- 健康评估（web 端）：`score=95 grade=Excellent server_type=accelerated`
- 指标按部件分布：`cpu` / `disk` / `memory` / `network`；`gpu`/`npu`/`chassis` 因无真实数据缺省（**优雅降级**）
- 硬件规格 `specs`：16 项（model_info / numa_node_num / core_num / l1d/l1i/l2/l3 cache / os_info / disk_info ×3 …）

> 无 NPU/GPU/Chassis 数据时，对应图表 series 为空但图表定义正常加载，前端不会因缺数据报错。
>
> **退出清 snapshot 验证**：web 进程收到 `SIGTERM` 后日志输出 `shutting down signal=terminated` 并清理 snapshot，退出干净（v0.3.3 新增行为）。

### 4.6 优雅降级小结

| 缺失项 | 行为 | 是否崩溃 |
|--------|------|:--------:|
| nvidia-smi（GPU） | gpu 采集器返回空，collect 跳过，health 不列入 GPU | ❌ 不崩溃 |
| npu-smi / CANN（NPU） | npu 采集器返回 `npu_num=0`，exporter 导出该计数 | ❌ 不崩溃 |
| ipmitool / BMC（Chassis） | chassis 采集器返回空，dfee chassis 图表 series 为空 | ❌ 不崩溃 |
| smartctl | disk 的 `smart_status` 缺失，health 扣 3 分但不中断 | ❌ 不崩溃 |

---

## 5. 单元测试回归

`go test ./...` → **263 PASS / 0 FAIL / 0 SKIP**（用例数与 v0.3.2 持平；本次合并的 `AnyWanted` DI 与采集器 `Wanted` 守卫未引入新测试用例，既有用例全绿）。

### 5.1 features 特性层

| 包 | 测试数 | 结果 | 覆盖率 |
|----|:------:|:----:|:------:|
| features/dfee | PASS | ✅ | 77.8% |
| features/exporter | PASS | ✅ | 81.1% |
| features/health | PASS | ✅ | 92.7% |
| features/web | PASS | ✅ | 62.7% |

### 5.2 internal 采集器层

| 包 | 测试数 | 结果 | 覆盖率 |
|----|:------:|:----:|:------:|
| internal/collectors/chassis | PASS | ✅ | 92.7% |
| internal/collectors/cpu | PASS | ✅ | 91.1% |
| internal/collectors/disk | PASS | ✅ | 91.2% |
| internal/collectors/gpu | PASS | ✅ | 94.3% |
| internal/collectors/memory | PASS | ✅ | 91.9% |
| internal/collectors/network | PASS | ✅ | 89.8% |
| internal/collectors/npu | PASS | ✅ | 94.1% |

### 5.3 internal 来源层（14 包）

| 包 | 结果 | 覆盖率 | | 包 | 结果 | 覆盖率 |
|----|:----:|:------:|---|----|:----:|:------:|
| source/dcmi | ✅ | 29.5% | | source/npu_smi | ✅ | 70.0% |
| source/dmesg | ✅ | 73.3% | | source/nvidia_smi | ✅ | 69.7% |
| source/dmidecode | ✅ | 80.2% | | source/proc | ✅ | 85.5% |
| source/hccn_tool | ✅ | 74.7% | | source/smartctl | ✅ | 78.6% |
| source/ipmi | ✅ | 72.1% | | source/statfs | ✅ | 92.3% |
| source/lscpu | ✅ | 78.7% | | source/sys | ✅ | 84.9% |
| source/mce | ✅ | 69.0% | | | | |

> 覆盖率区间 29.5% ~ 94.3%；DCMI 因 CGo 文件排除编译覆盖率偏低（29.5%），由 `dcmi_mock.go` + delegation 覆盖。

### 5.4 internal 其它

| 包 | 结果 | 覆盖率 |
|----|:----:|:------:|
| internal/metrics | ✅ | 66.3% |

> `internal/metrics` 覆盖率由 v0.3.2 的 85.9% 降至 66.3%：本次新增 `SetCollectionThreshold` / `AnyWanted` / `IsWanted` 等预过滤函数未补充对应测试用例，属后续补测项。

无测试包：`cmd/catmonitor`、`internal/collector`、`internal/config`、`internal/platform`、`internal/source`（聚合）、`internal/storage`。

---

## 6. 已知限制与观察项

1. **DCMI CGo 未真机验证**：`dcmi_cgo.go` 在 `dcmi` 构建标签后，本机无 CANN SDK 无法编译；需在真 NPU 服务器 `go build -tags dcmi ./...` 验证 CGo 绑定。
2. **GPU/NPU/Chassis 无真机**：系统测试仅验证"优雅降级"路径（空数据 / 计数 0 / 不崩溃）；真实指标采集需在配备对应硬件的机器上复测。
3. **server_type 判定口径不一致**：`catmonitor health` CLI 因 NPU 采集器产出 `npu_num` 指标判定 `accelerated`（95/Excellent）；web 端 hwinfo 探测无真实 NPU 硬件判定 `cpu_only`。非功能缺陷，建议后续统一判定依据。
4. **采集粒度控制仅验证默认 low**：本环境仅验证 `collection.min_priority: low`（全采）路径；`medium`（跳过 Low）/ `high`（仅 High）的预过滤行为未在系统测试中实跑，建议后续补测 + 补单元测试（同时提升 `internal/metrics` 覆盖率）。
5. **Windows 交叉编译回归已修复**：v0.3.2 起遗留的 `npu_other.go` `collectDevice` 签名不匹配（致 `GOOS=windows go build` 失败）已由本次热修 `b2181e6` 修复并验证通过；v0.3.2 test_report 中"Windows 交叉编译 ✅"系误报（未实跑或早于 `67ef5f1` 引入 `npuDevice`）。
6. **daemon 短时运行未落盘 JSONL**：daemon 在 ~5s 测试窗口内未向 `data_dir` 写入 `.jsonl` 文件（`CachingStorage` 内存缓存供 `/metrics` 读取，短时未触发 JSONL 落盘），建议真机长时运行观察落盘周期。
7. **未推送到远端**：合并提交 `e1c14c4` + 热修 `b2181e6` 暂在本地完成，待用户显式指示后推送 `origin/main`。

---

## 7. 合并信息

| 项 | 值 |
|----|----|
| 源分支 | `origin/feature/wyx/add-metrics`（HEAD `70865d7`） |
| 目标分支 | 本地 `main`（合并前 `36d9032` license 提交） |
| 合并提交 | `e1c14c4` Merge feature/wyx/add-metrics into main: 基于优先级的采集粒度控制 (min_priority + AnyWanted DI) + 采集器修正 |
| 合并类型 | **no-ff merge**（无冲突，自动合并） |
| 热修提交 | `b2181e6` fix: npu 非 linux 桩 collectDevice 形参同步为 npuDevice，修复 windows 交叉编译 |

### 7.1 本次合并主要新增/变更

- **采集粒度控制（核心特性）**：`internal/config` 新增 `Collection.MinPriority`（low/medium/high）；`internal/metrics` 暴露 `SetCollectionThreshold` / `AnyWanted` / `IsWanted`（优先级值大小写不敏感）；`internal/collector` 经 `SetWantedChecker` DI 注入；CPU/Memory/Disk/NPU 等采集器在执行前调用 `collector.AnyWanted` 判断是否跳过指标组；daemon 与 `runCollect` 启动时装配。
- **daemon 移除周期健康检查**：`runDaemon` 不再启动 health 评估 goroutine，健康度评估改由 `catmonitor health` 子命令按需执行。
- **web 退出清 snapshot**：`features/web/main.go` 收到信号后清理 snapshot 再退出。
- **配置**：`configs/catmonitor.yaml` 新增 `collection.min_priority: low`；`.gitignore` 增加 `loc_configs/`（本地测试用 `metrics_low.yaml` 不入库）。
- **dfee**：CPU 图表标题 `CPU 利用率分解` → `CPU 利用率`。
- **热修**：`internal/collectors/npu/npu_other.go` `collectDevice(devID int, ...)` → `collectDevice(dev npuDevice, ...)`，修复非 linux 平台签名不匹配致 Windows 交叉编译失败（v0.3.2 起遗留，非本次合并引入）。

---

## 8. 结论

`feature/wyx/add-metrics` 已以 No-FF 方式合入本地主干（`e1c14c4`），并附加 npu 非 linux 桩签名热修（`b2181e6`）。在无 NPU / 无 GPU 环境下完成系统测试：

- 两个二进制均编译通过，`go vet` 零告警；**Windows 交叉编译恢复通过**；
- `catmonitor` 全部子命令（version / list / collect / health / daemon）行为正确，版本号 `v0.3.3`；
- **采集粒度控制装配生效**（`AnyWanted` DI + `min_priority` 默认 low 全采），daemon 移除周期健康检查后采集 + 导出链路正常；
- `:9100/metrics` 输出合规格式（52 `# TYPE` / 173 指标行，`/-/healthy` / `/-/ready` 200）；
- `:9527` web/dfee 仪表盘正常服务（root/dfee/snapshot/collectors 全 200，snapshot 25980 B，退出清 snapshot）；
- GPU / NPU / Chassis 采集器在缺硬件时**优雅降级**，不崩溃、不 panic；
- 全量 **263** 个单元测试全部通过、零失败、零跳过（覆盖率 29.5% ~ 94.3%）。

**测试结论：无 NPU/GPU 范围内全部通过，合并代码可用；采集粒度控制的 medium/high 预过滤路径、NPU/GPU/Chassis 真实采集与 DCMI CGo 绑定需在配备对应硬件的服务器上复测。**

---

*测试执行时间: 2026-07-28*
*测试执行人: Automated (OpenCode + Go testing framework + 手动系统探测)*
*发布人: sunnytao*
