# CATMonitor 测试报告（Linux 平台）

> **项目**: CATMonitor (Computing Availability Tools Monitor)  
> **版本**: v0.1.1  
> **平台**: Linux (WSL2)  
> **日期**: 2026-07-13 12:21  
> **测试执行**: 自动化 Go testing 框架 + CLI 功能验证  

---

## 1. 测试概述

### 1.1 测试目标

验证 CATMonitor v0.1.1 在 Linux 平台下的完整功能，作为 Windows 平台报告（`docs/test_report.md`）中提及的"Linux 环境全部 75 个测试已验证通过"的补充验证，包括：
- Linux 平台编译与运行
- 全部 6 个采集器（CPU、Memory、Disk、GPU、NPU、Network）的指标采集功能（Linux 真实 `/proc` / `/sys` 数据 + Mock 数据）
- 健康度评估模块（CPU-only 和 accelerated 双方案）的 auto-detection 与评分计算
- CLI 命令（version、list、collect、health）在 Linux 下的兼容性
- 跨平台构建标签隔离：`GOOS=windows` 交叉编译通过

### 1.2 测试结果汇总

| 指标 | 结果 |
|------|------|
| 测试总数（单元） | **75**（Linux 环境可用） |
| 通过 | **75** |
| 失败 | **0** |
| 通过率 | **100%** |
| `go vet` | 通过 |
| `go build` | 通过 |
| `GOOS=windows go build` | 通过（交叉编译） |

> 注：CPU、Memory、Disk、Network 共 40 个测试标记为 `//go:build linux`，在 Windows 上编译时自动排除；GPU、NPU 的 15 个测试与 health 的 20 个测试无构建标签，跨平台均可运行。本报告在 Linux 下执行全部 75 个测试。

### 1.3 测试覆盖一览

| 包 | 测试数 | 覆盖率 | 说明 |
|----|:------:|:------:|------|
| internal/collectors/cpu | 11 | 87.2% | Linux 专属（/proc，/sys），`//go:build linux` |
| internal/collectors/memory | 8 | 90.2% | Linux 专属，`//go:build linux` |
| internal/collectors/disk | 15 | 90.4% | Linux 专属，`//go:build linux` |
| internal/collectors/network | 6 | 93.3% | Linux 专属，`//go:build linux` |
| internal/collectors/gpu | 6 | 87.9% | 跨平台（nvidia-smi），无构建标签 |
| internal/collectors/npu | 9 | 90.6% | 跨平台（npu-smi），无构建标签 |
| internal/health | 20 | 70.1% | 跨平台（纯逻辑），无构建标签 |
| internal/platform | — | 0.0% | 无测试文件 |
| internal/storage | — | 0.0% | 无测试文件 |
| internal/collector | — | — | 无测试文件 |
| internal/config | — | — | 无测试文件 |
| **合计** | **75** | **67.1%（总体）** | |

> 采集器包平均覆盖率 ~89.9%，核心逻辑覆盖良好。`platform`/`storage`/`config`/`collector` 目前无测试，是后续补测的重点。

---

## 2. 测试环境

| 项目 | 配置 |
|------|------|
| 操作系统 | Linux (WSL2, kernel 6.18.33.2-microsoft-standard-WSL2, x86_64) |
| 主机名 | DESKTOP-S7P61EP |
| CPU | Intel(R) Core(TM) i5-7200U @ 2.50GHz (4 核) |
| 内存 | 3840 MB (MemTotal 3932788 kB) |
| 磁盘 | /dev/sdd ext4，1007 GB 总量，已用 2.4 GB |
| GPU | 无（`nvidia-smi` 不可用） |
| NPU | 无（`npu-smi` 不可用） |
| 网络 | eth0 |
| Go 版本 | go1.23.4 linux/amd64 |
| 外部依赖 | gopkg.in/yaml.v3 v3.0.1 |
| 测试框架 | Go 原生 testing |
| 测试数据 | `tests/testdata/`（proc / sys / nvidia-smi / npu-smi 输出样本） |

---

## 3. 跨平台架构验证

### 3.1 构建标签隔离

| 平台 | 编译命令 | 结果 |
|------|----------|:----:|
| Linux (本机) | `go build ./...` | ✅ |
| Windows (交叉编译) | `GOOS=windows go build ./...` | ✅ |
| 静态检查 | `go vet ./...` | ✅ 零警告 |

### 3.2 模块文件布局

| 模块 | 共享文件 | Linux 文件 | Windows 文件 | 编译 |
|------|----------|------------|--------------|:----:|
| platform | platform.go | platform_linux.go | platform_windows.go | ✅ |
| cpu | cpu.go | cpu_linux.go | cpu_windows.go | ✅ |
| memory | memory.go | memory_linux.go | memory_windows.go | ✅ |
| disk | disk.go | disk_linux.go | disk_windows.go | ✅ |
| network | network.go | network_linux.go | network_windows.go | ✅ |
| gpu | gpu.go | — | — | ✅ |
| npu | npu.go | — | — | ✅ |
| health | health.go, rules.go, scorer.go | — | — | ✅ |
| config | config.go | — | — | ✅ |
| storage | storage.go | — | — | ✅ |

> GPU/NPU 单文件跨平台：通过 `os/exec` 调用 `nvidia-smi` / `npu-smi`，双平台逻辑一致，无需构建标签隔离。零新增依赖，go.mod 仅保留 gopkg.in/yaml.v3。

---

## 4. 单元测试结果

> 全部 75 个测试通过，0 失败。命令：`go test -v -count=1 ./...`

### 4.1 CPU 采集器（11 个测试）✅

| 测试名称 | 验证内容 | 结果 |
|----------|----------|:----:|
| TestParseCPUStat | /proc/stat 解析（cpu 总 + cpu0~cpu3） | PASS |
| TestCalculateUsage | 使用率计算（两次快照 delta） | PASS |
| TestCollectLoadAverage | load_average 采集（1m/5m/15m） | PASS |
| TestCollectUsage | usage 采集（含 delta 逻辑） | PASS |
| TestCollectIntegration | 集成采集（多指标） | PASS |
| TestCollectTemperature | 温度采集（/sys/class/thermal） | PASS |
| TestCollectFrequency | 频率采集（/sys/.../cpufreq） | PASS |
| TestCollectContextSwitches | 上下文切换（/proc/stat） | PASS |
| TestCollectProcessCount | 进程数（running/total） | PASS |
| TestCollectModelInfo | CPU 型号信息（/proc/cpuinfo） | PASS |
| TestCollectorInterface | Collector 接口实现完整性 | PASS |

### 4.2 Memory 采集器（8 个测试）✅

| 测试名称 | 验证内容 | 结果 |
|----------|----------|:----:|
| TestParseMeminfo | /proc/meminfo 解析 | PASS |
| TestCollectUsage | usage 采集 + usage_detail | PASS |
| TestCollectSwapUsage | swap 使用率 | PASS |
| TestCollectECCErrors | ECC 错误（/sys/edac） | PASS |
| TestCollectOOMCount | OOM 计数（dmesg） | PASS |
| TestCollectPageFaults | page faults（/proc/vmstat） | PASS |
| TestCollectIntegration | 集成采集 | PASS |
| TestCollectorInterface | Collector 接口实现完整性 | PASS |

### 4.3 Disk 采集器（15 个测试）✅

| 测试名称 | 验证内容 | 结果 |
|----------|----------|:----:|
| TestParseMounts | /proc/mounts 解析 | PASS |
| TestParseMountsEdgeCases | 边界情况 | PASS |
| TestVirtualFSFiltering | 虚拟文件系统过滤 | PASS |
| TestVirtualFSMap | 虚拟文件系统映射 | PASS |
| TestWithField | 字段辅助函数 | PASS |
| TestParseDiskStats | /proc/diskstats 解析 | PASS |
| TestCollectSpaceUsage | space_usage + space_detail | PASS |
| TestCollectIOPS | IOPS 采集 | PASS |
| TestCollectThroughput | 吞吐量采集 | PASS |
| TestCollectIoWait | I/O wait 采集 | PASS |
| TestCollectIoErrors | I/O 错误（dmesg） | PASS |
| TestCollectSMART | SMART 状态（smartctl） | PASS |
| TestCollectIntegration | 集成采集 | PASS |
| TestCollectorInterface | Collector 接口实现完整性 | PASS |
| TestRoundFloat | 浮点数精度处理 | PASS |

### 4.4 Network 采集器（6 个测试）✅

| 测试名称 | 验证内容 | 结果 |
|----------|----------|:----:|
| TestParseNetDev | /proc/net/dev 解析 | PASS |
| TestParseUint | 无符号整数解析 | PASS |
| TestCollectIntegration | 集成采集（throughput） | PASS |
| TestCollectInterfaceStatus | 接口状态（/sys/class/net） | PASS |
| TestCollectConnectionCount | 连接数（/proc/net/tcp） | PASS |
| TestCollectorInterface | Collector 接口实现完整性 | PASS |

### 4.5 GPU 采集器（6 个测试）✅

| 测试名称 | 验证内容 | 结果 |
|----------|----------|:----:|
| TestParseCSVLine | nvidia-smi CSV 行解析 | PASS |
| TestParseOutput | 完整输出解析，2 块 GPU × 9 指标 | PASS |
| TestCollectWithMock | Mock 输出采集集成测试 | PASS |
| TestUnavailableReturnsEmpty | nvidia-smi 不可用时返回空 | PASS |
| TestCollectorInterface | Collector 接口实现完整性 | PASS |
| TestRoundFloat | 浮点数精度处理 | PASS |

### 4.6 NPU 采集器（9 个测试）✅

| 测试名称 | 验证内容 | 结果 |
|----------|----------|:----:|
| TestIsNPUDataLine | NPU 数据行识别 | PASS |
| TestSplitPipeFields | 管道分隔字段解析 | PASS |
| TestParseMemoryUsage | "used / total" 格式显存解析 | PASS |
| TestParseOutput | 完整输出解析，2 块 NPU × 7 指标 | PASS |
| TestCollectWithMock | Mock 输出采集集成测试 | PASS |
| TestUnavailableReturnsEmpty | npu-smi 不可用时返回空 | PASS |
| TestCollectorInterface | Collector 接口实现完整性 | PASS |
| TestHealthMap | 健康状态值映射 (OK=1, Warning=2) | PASS |
| TestRoundFloat | 浮点数精度处理 | PASS |

### 4.7 健康度评估模块（20 个测试）✅

#### CPU 评分

| 测试名称 | 场景 | 预期得分 | 结果 |
|----------|------|----------|:----:|
| TestEvaluateCPUHealthy | CPU 正常 | 30/30 | PASS |
| TestEvaluateCPUUsageHigh | 使用率 95% (>90%) | 24/30 (-6) | PASS |
| TestEvaluateCPUUsageMedium | 使用率 85% (>80%) | 27/30 (-3) | PASS |
| TestEvaluateCPUTemperatureHigh | 温度 90°C (>85°C) | 21/30 (-9) | PASS |

#### Memory 评分

| 测试名称 | 场景 | 预期得分 | 结果 |
|----------|------|----------|:----:|
| TestEvaluateMemoryHealthy | 内存正常 | 40/40 | PASS |
| TestEvaluateMemoryUsageHigh | 使用率 95% (>90%) | 28/40 (-12) | PASS |
| TestEvaluateMemoryCEErrors | 3 个 CE 错误 | 34/40 (-6) | PASS |
| TestEvaluateMemoryUCErrors | 1 个 UCE 错误 | 30/40 (-10) | PASS |
| TestEvaluateMemorySwapHigh | Swap 60% (>50%) | 36/40 (-4) | PASS |

#### Disk 评分

| 测试名称 | 场景 | 预期得分 | 结果 |
|----------|------|----------|:----:|
| TestEvaluateDiskHealthy | 磁盘正常 | 30/30 | PASS |
| TestEvaluateDiskSpaceHigh | 使用率 85% (>80%) | 24/30 (-6) | PASS |
| TestEvaluateDiskSpaceCritical | 使用率 95% (>90%) | 18/30 (-12) | PASS |

#### GPU 评分

| 测试名称 | 场景 | 预期得分 | 结果 |
|----------|------|----------|:----:|
| TestEvaluateGPUHealthy | GPU 正常 | 60/60 | PASS |
| TestEvaluateGPUTempHigh | 温度 92°C (>90°C) | 42/60 (-18) | PASS |
| TestEvaluateGPUEccError | 有 ECC 错误 | 48/60 (-12) | PASS |

#### 综合评分

| 测试名称 | 场景 | 预期 | 结果 |
|----------|------|------|:----:|
| TestGradeForScore | 分数区间级别映射 | 正确 | PASS |
| TestEvaluateFullCPUOnly | 全健康 (CPU-only) | 100 (Excellent) | PASS |
| TestEvaluateFullCPUOnlyWithIssues | 多部件有问题 | 72 (Warning) | PASS |
| TestEvaluateAcceleratedScheme | 全健康 (加速卡) | 100 (Excellent) | PASS |
| TestGetScheme | 权重方案选择 | 正确返回 | PASS |

---

## 5. CLI 功能测试（Linux 真实环境）

### 5.1 命令测试结果

| 命令 | 结果 | 说明 |
|------|:----:|------|
| `catmonitor version` | ✅ | CATMonitor v0.1.1 (Go 1.23+) |
| `catmonitor list` | ✅ | 6 个采集器全部注册（cpu, disk, gpu, memory, network, npu） |
| `catmonitor collect -o json` | ✅ | 输出所有部件真实指标 JSONL |
| `catmonitor collect -o table` | ✅ | 输出表格格式 |
| `catmonitor health -o table` | ✅ | 健康报告，无 GPU/NPU 时正确使用 cpu_only 方案 |
| `catmonitor health -o json` | ✅ | JSON 格式健康报告 |

### 5.2 health 命令输出（真实环境）

```
CATMonitor Health Report
======================================================================

  Overall Score:  [██████████████████████████████]  100 / 100   [ Excellent ]
  Server Type:    cpu_only
  Check Time:     2026-07-13 12:21:22

  ----------------------------------------------------------------------
  Component        Score / Max    Status       Deductions
  ----------------------------------------------------------------------
  CPU                30 / 30       OK           -
  MEMORY             40 / 40       OK           -
  DISK               30 / 30       OK           -
  ----------------------------------------------------------------------
  TOTAL             100 / 100      Excellent
  ----------------------------------------------------------------------

  [OK]    All systems are healthy.
```

> 关键验证点：
> - Server Type 正确识别为 `cpu_only`（无 GPU/NPU 指标），权重方案为 CPU 30 + Memory 40 + Disk 30 = 100
> - 三部件指标全部在健康阈值内，零扣分
> - 与 Windows 报告（accelerated, 98/100）形成对照：本环境无 GPU，未触发加速卡方案

### 5.3 collect 命令输出（真实数据摘录）

| 部件 | 指标 | 真实值 | 数据源 |
|------|------|--------|--------|
| CPU | usage | 0.00% (首次) | /proc/stat（首次无历史快照） |
| CPU | load_average | 1.41 (1m) / 1.03 (5m) / 0.56 (15m) | /proc/loadavg |
| CPU | context_switches | 0 次/s (首次) | /proc/stat |
| CPU | process_count | 6 running / 222 total | /proc/stat |
| CPU | model_info | i5-7200U @ 2.50GHz, 3072 KB cache | /proc/cpuinfo |
| Memory | usage | 30.81% | /proc/meminfo |
| Memory | usage_detail | 3840 MB total / 1183 MB used / 2657 MB avail | /proc/meminfo |
| Memory | swap_usage | 0.00% | /proc/meminfo |
| Memory | oom_count | 0 | dmesg |
| Disk | space_usage | 0.23% (/ ext4), 21.69% (/mnt/c 9p), 5.28% (/mnt/d 9p) | syscall.Statfs |
| Disk | space_detail | / ext4: 1031018 MB total / 2356 MB used | syscall.Statfs |
| Disk | throughput | 0.00 MB/s (sda~sdd, 首次) | /proc/diskstats |
| Disk | io_errors | 0 | dmesg |
| Network | rx_bytes_total | 2968051 bytes (eth0) | /proc/net/dev |
| Network | tx_bytes_total | 2823232 bytes (eth0) | /proc/net/dev |
| Network | error_count | 0 rx_err/rx_drop/tx_err, 5 tx_drop (eth0) | /proc/net/dev |
| Network | connection_count | 3 LISTEN / 1 ESTABLISHED | /proc/net/tcp |

> GPU/NPU 指标未输出：本环境 `nvidia-smi` / `npu-smi` 均不可用，采集器优雅降级返回空（`TestUnavailableReturnsEmpty` 验证此行为），不影响其他采集器与整体健康度评估。

---

## 6. 指标实现完整性（Linux 平台）

### 全部 37 个指标均已实现（Linux 完整支持）

| 部件 | 指标数 | Linux 实现 | 本环境实际采集 | 说明 |
|------|:------:|:----------:|:--------------:|------|
| CPU | 7 | ✅ 全部 | 5 个 | temperature/frequency 在 WSL2 下 sysfs 无数据，优雅跳过 |
| Memory | 6 | ✅ 全部 | 5 个 | page_faults 在 collect 输出中未显示（/proc/vmstat 字段缺失时跳过） |
| Disk | 7 | ✅ 全部 | 4 类 | smart_status 无 smartctl；iops 合并入 throughput 展示 |
| GPU | 7 | ✅ | 0 | nvidia-smi 不可用，返回空 |
| NPU | 5 | ✅ | 0 | npu-smi 不可用，返回空 |
| Network | 5 | ✅ 全部 | 5 个 | 全部采集 |

> 说明：Linux 代码层面对 37 个指标均有实现（单元测试通过 fixture 验证）。真实环境采集的指标数取决于该机器上 /proc、/sys、smartctl、nvidia-smi、npu-smi 的实际可用性，缺失的数据源被优雅跳过而非报错。

### 健康度评估

| 方案 | CPU | Memory | Disk | GPU | 合计 | 触发条件 |
|------|:---:|:------:|:----:|:---:|:----:|------|
| CPU-only | 30 | 40 | 30 | — | 100 | 无 GPU/NPU 指标时（本环境） |
| Accelerated | 10 | 20 | 10 | 60 | 100 | GPU 或 NPU 指标存在时（auto） |

> `Evaluate()` 自动检测：存在 `gpu` 或 `npu` 指标时切换到 Accelerated8CardScheme，否则 CPUOnlyScheme。本环境无 GPU/NPU，使用 cpu_only 方案。

### 跨平台默认路径

| 项目 | Linux | Windows |
|------|-------|---------|
| 配置文件 | `/etc/catmonitor/catmonitor.yaml` | `C:\ProgramData\catmonitor\catmonitor.yaml` |
| 数据目录 | `/var/lib/catmonitor/data` | `C:\ProgramData\catmonitor\data` |
| 环境变量覆盖 | `CATMONITOR_CONFIG` / `CATMONITOR_DATA_DIR` | 同左 |

---

## 7. 代码质量

| 检查项 | 结果 |
|--------|:----:|
| `go build ./...` | ✅ 通过 |
| `GOOS=windows go build ./...` | ✅ 通过（交叉编译，双平台零错误） |
| `go vet ./...` | ✅ 通过，零警告 |
| `go test ./...` | ✅ 全部通过（75/75） |
| 外部依赖 | 仅 gopkg.in/yaml.v3（无新增） |
| 构建标签 | 4 个采集器使用 `_linux.go` / `_windows.go` 隔离 |
| 总体覆盖率 | 67.1%（采集器包平均 ~89.9%） |

---

## 8. 已知限制

1. **CPU 使用率首次采集**：返回 0.00%（无历史快照），第二次调用起有真实值（delta 计算）
2. **Disk throughput / context_switches 首次采集**：同为 delta 类指标，首次返回 0
3. **WSL2 环境指标缺失**：temperature、frequency 等 sysfs 依赖指标在 WSL2 内核下对应路径无数据，被优雅跳过；非代码缺陷
4. **GPU/NPU 无硬件**：`nvidia-smi` / `npu-smi` 不可用时采集器返回空，健康度评估自动退回 cpu_only 方案
5. **无测试的包**：`internal/platform`、`internal/storage`、`internal/config`、`internal/collector` 暂无单元测试，覆盖率 0%，建议后续补测
6. **storage 的 max_file_age 未实现**：配置项存在但 `internal/storage` 尚未实现过期清理逻辑
7. **裸命令 panic**：直接运行 `catmonitor`（无子命令）会在 `loadConfig` 中 panic（`os.Args[2:]` 越界），需显式使用 `catmonitor daemon`

---

## 9. 结论

CATMonitor v0.1.1 在 Linux (WSL2) 平台下**全部 75 个单元测试通过**（0 失败），CLI 5 个命令功能正常。6 个采集器在真实 Linux 环境中成功采集了 CPU、内存、磁盘、网络指标的真实数据（GPU/NPU 因无硬件优雅降级），健康度评估在无加速卡时正确使用 cpu_only 权重方案，总分 100/100（Excellent）。

跨平台验证：`GOOS=windows go build ./...` 交叉编译通过，Windows 平台代码（`_windows.go`）完整保留且可编译，无任何 Linux 功能被破坏。go.mod 保持单一外部依赖（yaml.v3），`go vet` 零警告。

**测试结论：Linux 平台全部通过，与 Windows 平台报告共同确认软件可在双平台运行。**

---

*测试执行时间: 2026-07-13 12:21 CST*  
*测试执行人: Automated (OpenCode + Go testing framework)*
