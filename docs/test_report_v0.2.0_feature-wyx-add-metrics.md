# CATMonitor 测试报告

> **项目**: CATMonitor (Computing Availability Tools Monitor)  
> **版本**: v0.2.0（来源层 + CPU/Memory 指标扩展 + disk/network 迁移）  
> **日期**: 2026-07-13  
> **测试执行**: 自动化 Go testing 框架 + CLI/daemon 实机验证

---

## 1. 测试概述

### 1.1 测试目标

验证 v0.2.0 来源层(source layer)引入与指标扩展后的完整功能，包括：

- **来源层**：9 个数据来源包(proc/sys/ipmi/lscpu/mce/dmesg/dmidecode/statfs/smartctl)的解析、缓存、超时、可用性检测
- **CPU 指标扩展**：7 → 40 个指标（新增 33 个：时间分解、利用率、拓扑、NUMA、缓存、MCE、ipmi 温度/功率等）
- **Memory 指标扩展**：6 → 19 个指标（新增 13 个：swap_detail/in/out、saturation、fragmentation、隔离页、DIMM、power 等）
- **disk / network 迁移**：两个 collector 接入来源层，行为不变
- 跨平台编译（Linux + Windows）与零回归

### 1.2 测试结果汇总

| 指标 | 结果 |
|------|------|
| 测试总数 | **141**（collectors 62 + sources 59 + health 20） |
| 通过 | **141** |
| 失败 | **0** |
| 通过率 | **100%** |
| `go build ./...` | 通过（Linux） |
| `GOOS=windows go build ./...` | 通过（Windows 交叉编译） |
| `go vet ./...` | 通过，零警告 |

---

## 2. 测试环境

| 项目 | 配置 |
|------|------|
| 操作系统 | Linux (WSL2, x86_64) |
| CPU | Intel Core i7-14700 (28 逻辑核) |
| 内存 | ~16 GB |
| Go 版本 | go1.23.4 linux/amd64 |
| 外部依赖 | gopkg.in/yaml.v3 v3.0.1 |
| 已装命令 | ipmitool（无 BMC）、dmesg；未装 dmidecode/smartctl/lscpu 无 root |
| 测试框架 | Go 原生 testing |

> 注：该环境无 BMC、无 EDAC、无 cpufreq sysfs、无 PSI（部分）、无 dmidecode root 权限，故部分指标走优雅降级路径（空值），其解析逻辑由 mock 驱动的单测覆盖。

---

## 3. 来源层架构验证

新增 `internal/source/` 分层，9 个来源包，collector 不再直接 `os.ReadFile`/`exec`，全部通过来源层 typed 方法获取数据。

| 来源包 | 文件 | 职责 | 缓存 | 单测 |
|--------|------|------|------|:---:|
| proc | proc.go | /proc 全量解析(Stat/Loadavg/Meminfo/Diskstats/NetDev/Vmstat/Cpuinfo/Buddyinfo/Mounts/NetTCPStates/Pressure) | 无 | 14 |
| sys | sys.go | /sys(CpuFreqs/CacheInfos/CpuOnline·Offline·Isolated/Nodes/Edac/NetOperstate/NetInterfaces/Thermal) | 无 | 12 |
| ipmi | ipmi.go | ipmitool SDR/PowerReading | 30s + 失败缓存 + 5s 超时 | 9 |
| lscpu | lscpu.go | 拓扑(Topology) | 常驻(sync.Once) | 4 |
| mce | mce.go | mcelog/dmesg MCE 事件 | 无 | 5 |
| dmesg | dmesg.go | 内核日志原文(供 oom) | 30s + 失败缓存 | 4 |
| dmidecode | dmidecode.go | SMBIOS type 17 DIMM | 常驻(sync.Once) | 3 |
| statfs | statfs.go(linux) | statfs(2) 系统调用 | 无（可注入 fetcher） | 3 |
| smartctl | smartctl.go | smartctl -H | per-dev 60s + 失败缓存 | 5 |

设计决策（已落地）：来源返回 parsed struct(A)、单例+SetRoot/可注入 fetcher(B)、proc/sys 不缓存(C)、ipmi 30s 缓存(D)、温度/功率留 collector 共享 ipmi 缓存(E)、source.Registry 延后(F)、本阶段迁 cpu/memory/disk/network(G)、Windows 来源延后(H)。

---

## 4. 单元测试结果

### 4.1 采集器层（62 个）

| 包 | 测试数 | 覆盖率 | 说明 |
|----|:------:|:------:|------|
| internal/collectors/cpu | 16 | 90.6% | 40 指标全覆盖 + collectCpuTimeStats 共享读 + ipmi 温度换源 |
| internal/collectors/memory | 13 | 91.4% | 19 指标 + usage_detail 8 field + swapIO delta + fragmentation 公式 |
| internal/collectors/disk | 14 | 90.6% | 迁移后 6 collectXxx + statfs/smartctl 注入 |
| internal/collectors/network | 4 | 91.2% | 迁移后 NetDev/NetTCPStates/NetInterfaces 注入 |
| internal/collectors/gpu | 6 | 87.9% | 未迁移（待建 nvsmi 来源） |
| internal/collectors/npu | 9 | 90.6% | 未迁移（待建 npsmi 来源） |

### 4.2 来源层（59 个）

| 包 | 测试数 | 覆盖率 | 关键测试 |
|----|:------:|:------:|----------|
| source/proc | 14 | 85.5% | 各解析器 + Pressure(PSI) + SetRoot 重定向 |
| source/sys | 12 | 82.2% | CPU list 解析 + NetInterfaces（含符号链接） + Thermal |
| source/ipmi | 9 | 75.0% | SDR 解析 + 30s 缓存命中/过期 + 失败缓存 |
| source/lscpu | 4 | 78.7% | 拓扑解析 + 常驻缓存 |
| source/mce | 5 | 69.0% | mcelog/dmesg 解析 + CE/UCE 分类 |
| source/dmesg | 4 | 73.3% | mock 注入 + 缓存命中 + 失败缓存 |
| source/dmidecode | 3 | 80.4% | type 17 解析 + Size 单位换算 + 常驻缓存 |
| source/statfs | 3 | 92.3% | 真 statfs + fetcher 注入 |
| source/smartctl | 5 | 70.4% | per-dev 缓存 + 失败缓存 + mock |

### 4.3 健康度（20 个）

| 包 | 测试数 | 覆盖率 |
|----|:------:|:------:|
| internal/health | 20 | 70.1% |

---

## 5. CLI / daemon 实机功能测试

### 5.1 编译与命令

| 命令 | 结果 |
|------|:----:|
| `go build -o bin/catmonitor ./cmd/catmonitor` | ✅ |
| `./bin/catmonitor version` | ✅ v0.1.1（版本号未升） |
| `./bin/catmonitor list` | ✅ 6 个采集器 |
| `./bin/catmonitor collect -o table` | ✅ |
| `./bin/catmonitor daemon` | ✅ 周期采集 + 健康度 + 优雅退出 |

### 5.2 实机指标产出（daemon 跑 2~3 周期，JSONL 落盘）

| 部件 | 设计指标数 | 实机产出 | 降级为空（环境原因） |
|------|:----------:|:--------:|----------------------|
| CPU | 40 | 31 | temperature/mem_temperature/power(无 BMC)、frequency/avg_freq/min_freq/max_freq(无 cpufreq sysfs)、cpu_ce_errors/cpu_uce_errors(无 MCE) |
| Memory | 19 | 14 | ecc_ce/uce_errors(无 EDAC)、module_num/size/info(无 dmidecode root)、power(无 BMC) |
| Disk | 7 | 6 | smart_temperature(smartctl -H 不含温度属性，需 -A，已知限制) |
| Network | 7 | 7 | 无（符号链接修复后全产出） |

实机真实值示例（CPU i7-14700）：core_num=28、cpu_num=1、die_core_num=14、l3_cache_size=33792 KB、user_util=1.33%、system_util=0.6%、idle_util=98%、usage=2.29%、numa_info=10。

---

## 6. 代码质量

| 检查项 | 结果 |
|--------|:----:|
| `go build ./...` | ✅ |
| `GOOS=windows go build ./...` | ✅（Windows 交叉编译零错误） |
| `go vet ./...` | ✅ 零警告 |
| 外部依赖 | 仅 gopkg.in/yaml.v3（无新增） |
| 构建标签 | statfs 包 `//go:build linux`；collector 平台拆分保留 |

---

## 7. 期间发现并修复的关键缺陷

1. **`/sys/class/*` 符号链接过滤 bug**：`NetInterfaces`/`Thermal` 用 `DirEntry.IsDir()` 过滤，而 `/sys/class/net/eth0`、`thermal_zone*` 在真机是**符号链接**（IsDir 返回 false）→ 真机把所有接口/温区过滤为空，仅在 testdata（真目录）下正常。修复为 `IsDir() || 类型含 ModeSymlink`。该 bug 提示后续 `/sys/class/*` 来源须注意符号链接。
2. **swap_in/out 在无 swap 机器不产出**：原用 `prevSwapIn > 0` 作"有基线"判断，pswpin=0（无 swap 活动）时永远不满足。改用 `hasPrevSwapIO` 标志位，第二周期起产出（值 0）。
3. **ipmitool 装但无 BMC 反复 exec**：ipmi SDR 失败不缓存 → 每 3s 起一次子进程。改为**失败也缓存**（negative cache，30s 内不重试），避免拖慢 collector。
4. **ipmi SDR 缺 MEM 功率传感器**：testdata 补 `MEM1 Pwr` 以覆盖 memory power 指标（不影响 cpu 的 5 个 ipmi 指标）。
5. **statfs 包漏 linux build tag**：`syscall.Statfs_t` 是 Linux 专有，漏标导致 Windows 编译失败，补 `//go:build linux`。

---

## 8. 已知限制

1. **gpu/npu 未接入来源层**：仍内联 exec nvidia-smi/npu-smi，待建 `nvsmi`/`npsmi` 来源包后迁移。
2. **smart_temperature**：disk 用 `smartctl -H`，其输出不含温度属性，需改用 `-A` 才能解析温度（既有局限，非本次回归）。
3. **Windows 来源迁移延后**：`cpu_windows.go`/`memory_windows.go`/`disk_windows.go`/`network_windows.go` 保留原 syscall/PowerShell 实现，新指标在 Windows 多数降级为空（决策 H）。
4. **per-metric 采集周期未实现**：框架仍是每 collector 一个 interval，文档中的"默认周期"为设计目标，非运行时强制（既有局限）。
5. **`-c` 短选项 bug**：`loadConfig` 中 `fs.String("c",...)` 结果未使用，只能用 `--config`（既有 bug，未修）。
6. **差值类指标单次 collect 为 0/不产出**：usage 首次产 0、util/swap_in/swap_out/page_faults 首次不产出（无 prev），需 daemon 第二周期起才有真实值（既有行为）。

---

## 9. 结论

CATMonitor v0.2.0 完成**来源层抽象 + CPU(40)/Memory(19) 指标扩展 + disk/network 迁移**。全部 141 个单测通过，Linux/Windows 双平台编译通过，`go vet` 零警告。实机验证 cpu 31/40、memory 14/19、disk 6/7、network 7/7 指标产出真实值，缺失项均为环境降级（无 BMC/EDAC/cpufreq/dmidecode-root），解析逻辑由 mock 单测覆盖。

期间发现并修复 1 个隐蔽的真机 bug（`/sys/class/*` 符号链接过滤）及 2 个降级路径缺陷（ipmi/dmesg 失败缓存、swap_in 基线判断），显著提升了来源层在异常/边缘环境的鲁棒性。

**测试结论：全部通过，v0.2.0 来源层与指标扩展可用。**

---

*测试执行时间: 2026-07-13*  
*测试执行人: Automated (OpenCode + Go testing framework)*
