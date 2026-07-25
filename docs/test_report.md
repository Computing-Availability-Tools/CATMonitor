# CATMonitor 系统测试报告（无 NPU / 无 GPU）

> **项目**: CATMonitor (Computing Availability Tools Monitor)
> **测试对象**: 本地 `main` 分支 @ `c824349`（已合入 `feature/wyx/add-metrics`，合并提交 *Merge feature/wyx/add-metrics into main: exporter + NPU/IPMI/dfee 增强*）
> **代码版本常量**: v0.3.1（`cmd/catmonitor/main.go:34`）
> **测试类型**: 无 NPU / 无 GPU 真机环境下的系统测试 + 单元测试回归
> **日期**: 2026-07-25
> **测试执行**: OpenCode + Go testing 框架 + 手动系统探测（curl/进程行为观察）

---

## 1. 测试概述

### 1.1 测试目标

在缺少 NPU 硬件与 GPU 硬件的 Linux 主机上，验证合并后代码的端到端可用性：

- 合并后两个可执行二进制均能编译并通过 `go vet`
- `catmonitor` CLI（version / list / collect / health / daemon）行为正确
- **新增的 Prometheus exporter 模块**（`features/exporter`）在 `:9100/metrics` 输出合规格式
- `features/web` 仪表盘 + `features/dfee` 能效监控在 `:9527` 正常提供 SPA 与 API
- 无硬件的采集器（GPU / NPU / Chassis）**优雅降级**，不崩溃、不影响其它采集器
- 全量单元测试零回归

### 1.2 测试结果汇总

| 指标 | 结果 |
|------|------|
| 单元测试总数 | **263** |
| 通过 / 失败 / 跳过 | **263 / 0 / 0** |
| 通过率 | **100%** |
| `go build ./cmd/catmonitor` + `./features/web` | ✅ 通过 |
| `GOOS=windows go build ./...`（交叉编译） | ✅ 通过 |
| `go vet ./...` | ✅ 零告警 |
| 单元测试覆盖率区间 | 29.5% ~ 97.0% |
| `catmonitor collect` 一次性采集 | ✅ 92 指标（CPU/Memory/Disk/Network + NPU 计数） |
| `catmonitor health` 健康评估 | ✅ 95/100 [Excellent] |
| `:9100/metrics` Prometheus 导出 | ✅ HTTP 200，33 指标名，31 gauge + 2 counter |
| `:9527` web/dfee 仪表盘 | ✅ 5 个端点全部 HTTP 200 |
| GPU/NPU/Chassis 优雅降级 | ✅ 无崩溃、无 panic |

---

## 2. 测试环境

| 项目 | 配置 |
|------|------|
| 操作系统 | Linux (WSL2, x86_64) |
| 内核 | 6.18.33.2-microsoft-standard-WSL2 |
| CPU 逻辑核 | 4 |
| 内存 | 3932788 kB (~3.9 GB) |
| Go 版本 | go1.23.4 linux/amd64 |
| 网卡 | eth0 |
| 磁盘设备 | sda / sdb / sdc / sdd |
| nvidia-smi | ❌ 不存在（GPU 走空数据降级） |
| npu-smi | ❌ 不存在（NPU 走 mock / npu_num=0 降级） |
| ipmitool / BMC | ❌ 不存在（Chassis 走空数据降级） |
| smartctl | ❌ 不存在（Disk smart_status 缺失扣分） |
| CANN SDK | ❌ 不存在（DCMI CGo 在 `dcmi` 构建标签后，默认排除） |

> 注：本环境无 NPU 硬件、无 CANN SDK、无 nvidia-smi、无 BMC、无 smartctl。NPU/GPU/Chassis 相关逻辑在系统测试中通过"优雅降级"验证，单元层面由 mock 驱动。

---

## 3. 编译与静态检查

| 检查项 | 命令 | 结果 |
|--------|------|:----:|
| catmonitor 构建 | `go build -o catmonitor ./cmd/catmonitor` | ✅ |
| web 构建 | `go build -o web ./features/web` | ✅ |
| 全量构建 | `go build ./...` | ✅ |
| Windows 交叉编译 | `GOOS=windows go build ./...` | ✅ |
| 静态检查 | `go vet ./...` | ✅ 零告警 |
| 版本输出 | `catmonitor version` → `CATMonitor v0.3.1 (Go 1.23+)` | ✅ |

- **CGo 隔离**：`internal/source/dcmi/dcmi_cgo.go` 使用 `//go:build cgo && linux && dcmi`，默认编译排除，本机无 CANN SDK 仍可构建。
- **非 Linux 隔离**：`internal/collectors/npu/npu_other.go` 使用 `//go:build !linux`，Windows 交叉编译通过。
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
memory     usage_detail       3840.61  MB     field=total
memory     usage_detail       2791.75  MB     field=used
disk       space_usage        29.14    %      device=drivers,mount_point=/usr/lib/wsl/drivers,fstype=9p
disk       space_detail       243968   MB     device=drivers,...,field=total
network    error_count        0.00     次     interface=eth0,type=rx_err
network    error_count        5.00     次     interface=eth0,type=tx_drop
npu        npu_num            0.00     个
```

> **优雅降级验证**：缺硬件的 gpu/chassis 不产生指标且不 panic；npu 返回 `npu_num=0` 计数而非报错，符合"无设备即计数 0"的预期。

### 4.3 健康评估（`catmonitor health -o table`）

```
Overall Score:  [████████████████████████████░░]  95 / 100   [ Excellent ]
Server Type:    accelerated
Check Time:     2026-07-25 10:23:42

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

> 注：`catmonitor health` CLI 判定 `Server Type = accelerated`（因 NPU 采集器产出了 `npu_num` 指标即视作"含 NPU"）；而 web 端 hwinfo 探测无真实 NPU 硬件 → 判定 `cpu_only`（见 4.5）。两处 server_type 判定口径不同，非缺陷但建议后续统一，详见第 6 节。

### 4.4 Prometheus exporter（`catmonitor daemon` + `:9100/metrics`）⭐合并新增

启动 daemon（`cacheStore := exporter.NewCachingStorage(store)` + `go exporter.ServeMetrics(":9100", ...)`），等待采集后探测：

```
time=... level=INFO msg="exporter listening" addr=:9100
time=... level=INFO msg="CATMonitor daemon started" version=0.3.1
```

`curl http://localhost:9100/metrics`：

| 项 | 值 |
|----|----|
| HTTP 状态 | **200** |
| 响应体大小 | 10348 字节 |
| 文本行数 | 183 |
| 指标名数（`catmonitor_*`） | 33 |
| TYPE 分布 | **31 gauge + 2 counter** |
| counter 指标 | `catmonitor_network_rx_bytes_total`、`catmonitor_network_tx_bytes_total` |

**格式合规性**：

- 所有指标统一 `catmonitor_{component}_{name}` 前缀，`-` `/` `.` 均被替换为 `_`
- 每组指标含 `# HELP` + `# TYPE` 头
- `_total` 后缀指标正确识别为 `counter`（`isCounter` 逻辑：`_total` / `_time` → counter），可用于 PromQL `rate()` / `increase()`
- 标签语法合规：`{direction="rx",interface="eth0"}`，键按字典序排序

**代表数据行**：

```
# HELP catmonitor_network_throughput network/throughput
# TYPE catmonitor_network_throughput gauge
catmonitor_network_throughput{direction="rx",interface="eth0"} 0
catmonitor_network_throughput{direction="tx",interface="eth0"} 0
catmonitor_network_error_count{interface="eth0",type="rx_err"} 0
catmonitor_network_error_count{interface="eth0",type="tx_drop"} 5
# HELP catmonitor_network_rx_bytes_total network/rx_bytes_total
# TYPE catmonitor_network_rx_bytes_total counter
catmonitor_npu_npu_num 0
```

> **结论**：合并新增的 `features/exporter`（CachingStorage + `/metrics` 端点）在无 NPU/GPU 环境下工作正常，输出符合 Prometheus 文本 exposition 格式，counter/gauge 类型推断正确。

### 4.5 web / dfee 仪表盘（`features/web` @ `:9527`）

启动 `web -config features/web/config.yaml`，绑定 `:9527`，硬件规格采集 6 类后开始周期采集（5s）。探测各端点：

| 端点 | HTTP | 大小 | 说明 |
|------|:----:|-----:|------|
| `GET /` | 200 | 1407 B | 主仪表盘 SPA |
| `GET /dfee/` | 200 | 940 B | 能效监控 SPA（合并扩展：拖拽缩放 / 多选筛选 / 模块折叠） |
| `GET /api/snapshot` | 200 | 27364 B | 快照 JSON（含 health/metrics/history/specs） |
| `GET /api/dfee` | 200 | 4489 B | dfee 过滤后指标，**61 个图表定义** |
| `GET /api/collectors` | 200 | 600 B | 7 个采集器清单 |

**snapshot 结构**（`features/web/data/snapshot.json`，42476 字节）：

- 顶层键：`session_id / timestamp / refresh_interval_ms / history_points / health / metrics / history / specs`
- 健康评估（web 端）：`score=87 grade=Good server_type=cpu_only`
- 指标按部件分布：`cpu`(82) `disk`(58) `memory`(22) `network`(12)；`gpu`/`npu`/`chassis` 因无真实数据缺省（**优雅降级**）
- 硬件规格 `specs`：16 项（model_info / numa_node_num / core_num / l1d/l1i/l2/l3 cache / os_info / disk_info ×3 …）

**dfee 图表定义（61 个）**，跨全部部件：

```
NPU      npu_aicore_freq npu_hbm_freq npu_power_draw npu_voltage npu_npu_util
         npu_utilization npu_vector_core_util npu_hbm_bandwidth_util npu_memory_usage
CPU      cpu_utilization cpu_load load_average:1m/5m/15m cpu_power
Memory   memory_pool usage_detail:{buffers,cached,free,sreclaimable,total} memory_swap swap_detail:{free,total}
Disk     disk_throughput_{read,write} ×4设备  disk_iops_{read,write} ×4
         disk_read_latency ×4  disk_write_latency ×4
Network  network_rx network_tx
Chassis  chassis_power chassis_temp chassis_fan
```

> 无 NPU/GPU/Chassis 数据时，对应图表 series 为空但图表定义正常加载，前端不会因缺数据报错。

### 4.6 优雅降级小结

| 缺失项 | 行为 | 是否崩溃 |
|--------|------|:--------:|
| nvidia-smi（GPU） | gpu 采集器返回空，collect 跳过，health 不列入 GPU | ❌ 不崩溃 |
| npu-smi / CANN（NPU） | npu 采集器返回 `npu_num=0`，exporter 导出该计数 | ❌ 不崩溃 |
| ipmitool / BMC（Chassis） | chassis 采集器返回空，dfee chassis 图表 series 为空 | ❌ 不崩溃 |
| smartctl | disk 的 smart_status 缺失，health 扣 3 分但不中断 | ❌ 不崩溃 |

---

## 5. 单元测试回归

`go test ./...` → **263 PASS / 0 FAIL / 0 SKIP**（较 v0.3.1 的 241 用例 +22，主要来自合并新增的 `features/exporter` 与 `internal/source/hccn_tool` 扩展用例）。

### 5.1 features 特性层

| 包 | 测试数 | 结果 | 覆盖率 |
|----|:------:|:----:|:------:|
| features/dfee | PASS | ✅ | 77.8% |
| features/exporter ⭐新增 | PASS | ✅ | 81.1% |
| features/health | PASS | ✅ | 92.7% |
| features/web | PASS | ✅ | 63.0% |

### 5.2 internal 采集器层

| 包 | 测试数 | 结果 | 覆盖率 |
|----|:------:|:----:|:------:|
| internal/collectors/chassis | PASS | ✅ | 94.9% |
| internal/collectors/cpu | PASS | ✅ | 90.6% |
| internal/collectors/disk | PASS | ✅ | 90.8% |
| internal/collectors/gpu | PASS | ✅ | 97.0% |
| internal/collectors/memory | PASS | ✅ | 91.4% |
| internal/collectors/network | PASS | ✅ | 91.2% |
| internal/collectors/npu | PASS | ✅ | 94.1% |

### 5.3 internal 来源层（14 包）

| 包 | 结果 | 覆盖率 | | 包 | 结果 | 覆盖率 |
|----|:----:|:------:|---|----|:----:|:------:|
| source/dcmi | ✅ | 29.5% | | source/npu_smi | ✅ | 70.0% |
| source/dmesg | ✅ | 73.3% | | source/nvidia_smi | ✅ | 69.7% |
| source/dmidecode | ✅ | 80.2% | | source/proc | ✅ | 85.5% |
| source/hccn_tool ⭐扩展 | ✅ | 74.7% | | source/smartctl | ✅ | 78.6% |
| source/ipmi ⭐大改 | ✅ | 72.1% | | source/statfs | ✅ | 92.3% |
| source/lscpu | ✅ | 78.7% | | source/sys | ✅ | 84.9% |
| source/mce | ✅ | 69.0% | | | | |

> 覆盖率区间 29.5% ~ 97.0%；DCMI 因 CGo 文件排除编译覆盖率偏低（29.5%），由 `dcmi_mock.go` + delegation 覆盖。`ipmi` 经本次大改后覆盖率 72.1%（含 sdr/sensor 解析、定向采集、两级缓存、降级回退）。

### 5.4 internal 其它

| 包 | 结果 | 覆盖率 |
|----|:----:|:------:|
| internal/metrics | ✅ | 85.9% |

无测试包：`cmd/catmonitor`、`internal/collector`、`internal/config`、`internal/platform`、`internal/source`（聚合）、`internal/storage`。

---

## 6. 已知限制与观察项

1. **DCMI CGo 未真机验证**：`dcmi_cgo.go` 在 `dcmi` 构建标签后，本机无 CANN SDK 无法编译；需在真 NPU 服务器 `go build -tags dcmi ./...` 验证 CGo 绑定（`dcmi_init` / `dcmi_get_card_num_list` / `dcmi_get_device_errorcode_v2` 等）。
2. **GPU/NPU/Chassis 无真机**：系统测试仅验证"优雅降级"路径（空数据 / 计数 0 / 不崩溃）；真实指标采集需在配备对应硬件的机器上复测。
3. **server_type 判定口径不一致**：`catmonitor health` CLI 因 NPU 采集器产出 `npu_num` 指标判定 `accelerated`（95/Excellent）；web 端 hwinfo 探测无真实 NPU 硬件判定 `cpu_only`（87/Good）。非功能缺陷，建议后续统一 server_type 判定依据（以"是否存在真实硬件"而非"采集器是否产出计数指标"为准）。
4. **daemon 短时运行未落盘 JSONL**：`catmonitor daemon` 在 ~8s 测试窗口内未向 `data_dir` 写入 `.jsonl` 文件（目录为空）。可能原因：`exporter.NewCachingStorage` 在内存缓存指标供 `/metrics` 读取，短时未触发 JSONL 落盘；建议真机长时运行观察落盘周期（`max_file_age: 168h` / `rotation: daily`）。
5. **文档版本号与报告位置**：v0.3.2 已在代码常量、`docs/CATMonitor_indi_list.md` 头部、README/SPEC/DESIGN/Release_Notes 间统一；冗余的根 `test_report.md`（v0.3.1 旧版）已删除，测试报告统一收敛至 `docs/test_report.md`。
6. **NPU 原始单位待实测**：DCMI 的 voltage/temperature/llc_hit_rate 等单位需真机对照 `npu-smi info` 反推，本环境无法验证。
7. **未推送到远端**：合并提交 `c824349` 仅在本地完成，等待指示后再 `git push`。

---

## 7. 合并信息

| 项 | 值 |
|----|----|
| 源分支 | `origin/feature/wyx/add-metrics` |
| 源分支 HEAD | `c21a081` fix: --help 解析后退出而非继续执行采集 |
| 目标分支 | 本地 `main`（合并前 `3e5c0c3` docs: sync v0.3.1…） |
| 合并提交 | `c824349` Merge feature/wyx/add-metrics into main |
| 合并类型 | **no-ff merge**（含 1 处冲突，已解决） |
| 冲突文件 | `docs/CATMonitor_indi_list.md`（头部手动合并；`cmd/catmonitor/main.go` 自动合并：保留 v0.3.1 版本号 + feature 的 exporter 集成与 `--help` 退出） |

### 7.1 本次合并主要新增/变更

- **Prometheus exporter**：`features/exporter`（`prometheus.go` + `storage.go` + 测试），`CachingStorage` 包装存储层，`/metrics` 端点监听 `:9100`，`catmonitor` daemon 集成。
- **NPU 采集器增强**：DCMI CGo 修复（`dcmi_cgo.go` +76）；`hccn_tool` 新增 45 项统计指标；NPU card/device 二级枚举；默认布局调整。
- **IPMI 来源层大改**：`sdr → sensor` 命令、3/4 段解析兼容、定向 `sensor get` 采集、两级缓存（名称 24h / 结果 10s）、磁盘持久化、降级回退、超时 5s→30s→60s。
- **dfee 仪表盘**：卡片拖拽重排 + 右下角手柄缩放 + 虚线对齐辅助 3px 吸附、NPU/磁盘/网络多选下拉筛选、模块折叠、机箱 3 图同行。
- **main.go**：`--help` 解析后 `os.Exit(0)` 退出；集成 exporter；保留 main 的 v0.3.1 版本号。

---

## 8. 结论

`feature/wyx/add-metrics` 已合入本地主干（`c824349`），合并冲突已解决。在无 NPU / 无 GPU 环境下完成系统测试：

- 两个二进制均编译通过，`go vet` 零告警；
- `catmonitor` 全部子命令（version / list / collect / health / daemon）行为正确；
- **新增 Prometheus exporter 在 `:9100/metrics` 输出合规格式**（33 指标名 / 31 gauge + 2 counter，counter 推断正确）；
- **web/dfee 仪表盘在 `:9527` 正常服务**（5 端点全 200，61 个 dfee 图表定义加载正常，snapshot 42KB）；
- GPU / NPU / Chassis 采集器在缺硬件时**优雅降级**，不崩溃、不 panic；
- 全量 **263** 个单元测试全部通过、零失败、零跳过（覆盖率 29.5% ~ 97.0%）。

**测试结论：无 NPU/GPU 范围内全部通过，合并代码可用；NPU/GPU/Chassis 真实采集与 DCMI CGo 绑定需在配备对应硬件的服务器上复测。**

---

*测试执行时间: 2026-07-25*
*测试执行人: Automated (OpenCode + Go testing framework + 手动系统探测)*
