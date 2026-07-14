# CATMonitor 修改说明 (v0.2.0)

> **版本**: v0.2.0  
> **日期**: 2026-07-13  
> **范围**: 引入来源层(source layer)、CPU/Memory 指标扩展、disk/network 迁移到来源层  
> **回归**: 全量 `go test ./...` 通过、Linux/Windows 双平台编译通过、`go vet` 零警告

---

## 一、变更总览

| 维度 | 变更 |
|------|------|
| 新增分层 | `internal/source/` 来源层（9 个包） |
| CPU 指标 | 7 → 40（+33） |
| Memory 指标 | 6 → 19（+13，含 usage_detail 扩展 5 field） |
| disk / network | 迁移到来源层（指标集不变，行为不变） |
| gpu / npu | 未动（待后续建 nvsmi/npsmi 来源） |
| 文档 | `CATMonitor_indi_list.md` 同步 CPU/Memory；新增 `CPU_metrics.md`、`MEM_metrics.md`、`test_report_v0.2.0.md` |
| 外部依赖 | 无新增（仍仅 yaml.v3） |

---

## 二、新增：来源层（`internal/source/`）

抽象数据获取与解析，collector 不再直接 `os.ReadFile`/`exec`。来源返回 **parsed struct**，单例 + `SetRoot`/可注入 fetcher，部分带缓存。

| 包 | 文件 | 数据源 | 缓存 | 备注 |
|----|------|--------|------|------|
| proc | source.go + proc.go | /proc 全量 | 无 | 11 个 typed 方法（Stat/Loadavg/Meminfo/Diskstats/NetDev/Vmstat/Cpuinfo/Buddyinfo/Mounts/NetTCPStates/Pressure） |
| sys | sys.go | /sys | 无 | CpuFreqs/CacheInfos/CpuOnline·Offline·Isolated/Nodes/Edac/NetOperstate/NetInterfaces/Thermal |
| ipmi | ipmi.go | ipmitool SDR/DCMI | 30s + 失败缓存 + 5s 超时 | fetcher 可注入；温度/功率共用一份 SDR |
| lscpu | lscpu.go | lscpu | 常驻(sync.Once) | 拓扑静态 |
| mce | mce.go | mcelog/dmesg | 无 | MCE CE/UCE 事件 |
| dmesg | dmesg.go | dmesg | 30s + 失败缓存 | 供 oom_count |
| dmidecode | dmidecode.go | dmidecode --type 17 | 常驻(sync.Once) | DIMM 信息 |
| statfs | statfs.go (`//go:build linux`) | statfs(2) | 无 | fetcher 可注入；Linux 专有 |
| smartctl | smartctl.go | smartctl -H | per-dev 60s + 失败缓存 | |

通用接口 `internal/source/source.go`：`Source{Name(); Available()}`（决策 F：不建 Registry）。

---

## 三、CPU 采集器扩展（7 → 40）

### 新增/扩展的 collectXxx

| 方法 | 文件 | 产出 | 来源 |
|------|------|------|------|
| collectCpuTimeStats（重构自 collectUsage） | cpu_linux.go | usage + 8 *_time + 4 *_util | proc.Stat()（共享一次读） |
| collectTopology | cpu_metrics.go | numa_node_num/core_num/die_core_num/numa_core_num/cpu_num | lscpu |
| collectCoreState | cpu_metrics.go | online/offline/isolated_core_num | sys |
| collectFreqStats | cpu_metrics.go | min_freq/max_freq | sys（启动缓存） |
| collectCacheInfo | cpu_metrics.go | l1d/l1i/l2/l3_cache_size | sys（启动缓存） |
| collectBuddyInfo | cpu_metrics.go | numa_order_num/numa_info | proc.Buddyinfo |
| collectMCEErrors | cpu_metrics.go | cpu_ce_errors/cpu_uce_errors | mce（delta） |
| collectIpmiMetrics（取代旧 thermal collectTemperature） | cpu_metrics.go | temperature/mem_temperature/power | ipmi.SDR |

### 结构/迁移

- `cpu.go`：结构体 `prevStats` 改用 `proc.CPUStat`，加 MCE prev 字段 + 4 个缓存标志；`Collect()` 编排 13 个方法；新增 `stateUtil`/`cpuStatTotal`。
- `cpu_linux.go`：现有 collectXxx 迁移到 proc/sys 来源；删 `parseCPUStat`/`SetProcPath`/`SetSysPath`。
- `cpu_windows.go`：`collectUsage`→`collectCpuTimeStats`（仅 usage，时间分解降级）；删 `collectTemperature`。
- `cpu_metrics.go`（跨平台，无 build tag）：7 个新方法，Windows 上来源报错→空。

---

## 四、Memory 采集器扩展（6 → 19）+ 迁移

### 新增/扩展的 collectXxx

| 方法 | 产出 | 来源 |
|------|------|------|
| collectUsage（扩展） | usage + usage_detail 8 field（+free/buffers/cached/sreclaimable/unevictable） | proc.Meminfo |
| collectSwapUsage（扩展） | swap_usage + swap_detail(total/free/used) | proc.Meminfo |
| collectSwapIO | swap_in/swap_out（delta） | proc.Vmstat |
| collectSaturation | saturation(avg10/avg60/avg300) | proc.Pressure("memory") |
| collectFragmentation | fragmentation（order0/总空闲页数×100，按页数加权） | proc.Buddyinfo |
| collectPageCounters | isolated_pages/anon/file_pages/free_pages | proc.Vmstat |
| collectModuleInfo | module_num/module_size/module_info | dmidecode（启动缓存） |
| collectPower | power | ipmi.SDR |

### 结构/迁移

- `memory.go`：删 `mockDmesg` + 5 个未用 Windows delta 字段；加 `prevSwapIn/prevSwapOut/hasPrevSwapIO/moduleInfoCollected`。
- `memory_linux.go`：6 个现有 collectXxx 迁移到 proc/sys/dmesg 来源；删 `parseMeminfo`/`SetProcPath`/`SetSysPath`/`SetMockDmesg`。
- `memory_windows.go`：删 `SetMockDmesg`；`netDevStats`→`proc.NetDevStat`。
- `memory_metrics.go`（跨平台）：6 个新方法。

---

## 五、disk / network 迁移到来源层（行为不变）

### disk

- `disk.go`：结构体用 `proc.DiskStat`/`proc.CPUStat`；删 `diskStats`/`MountInfo`/`mockDmesg`/`mockSmartctl`/`SetMock*`/`parseU64`/`sumU64`；保留 `parseSmartOutput`/`withField`/`cpuStatTotal`。
- `disk_linux.go`：Collect + collectSpaceUsage(`statfs`)/collectIOPS·Throughput(`proc.Diskstats`)/collectIoWait(`proc.Stat`)/collectIoErrors(`dmesg`)/collectSMART(`smartctl`)；删 `parseMounts`/`parseDiskStats`/`parseCPUStatForIoWait`/`linuxDiskPaths`/`SetProcPath`。`deviceFilter`/`virtualFS` 业务过滤保留。
- `disk_windows.go`：未动（kernel32 GetDiskFreeSpaceExW）。

### network

- `network.go`：`prevStats` 改用 `proc.NetDevStat`；删 `netDevStats`/`parseUint`。
- `network_linux.go`：Collect + collectConnectionCount(`proc.NetTCPStates`)/collectInterfaceStatus(`sys.NetInterfaces`+`NetOperstate`)；删 `parseNetDev`/`SetProcPath`/`SetSysPath`。
- `network_windows.go`：`netDevStats`→`proc.NetDevStat`（字段大写）；其余未动。

---

## 六、来源层补齐（为 disk/memory/network）

- `sys`：加 `NetInterfaces()`（含符号链接修复）、`Thermal()`、`Pressure` 进 proc。
- `statfs`（新包，linux）：`Statfs(path)` + 可注入 fetcher。
- `smartctl`（新包）：`Health(dev)` + per-dev 60s 缓存 + 失败缓存。
- `dmesg`（新包）：`Text()` + 30s 缓存 + 失败缓存（供 disk.io_errors / memory.oom_count）。
- `dmidecode`（新包）：`MemoryDevices()` + 常驻缓存。
- `proc`：加 `Pressure(resource)`（PSI）。

---

## 七、测试数据与 testdata

### 测试（141 个，全过）

- collectors: cpu 16 / memory 13 / disk 14 / network 4 / gpu 6 / npu 9
- sources: proc 14 / sys 12 / ipmi 9 / lscpu 4 / mce 5 / dmesg 4 / dmidecode 3 / statfs 3 / smartctl 5
- health 20
- 覆盖率：collectors 87.9~91.4%、sources 69~92.3%

### testdata 新增/调整

- `tests/testdata/proc/buddyinfo`、`proc/pressure/memory`
- `tests/testdata/lscpu-output.txt`、`ipmitool-sdr-output.txt`（加 MEM1 Pwr）、`ipmitool-dcmi-power.txt`、`dmesg-mce-sample.txt`、`dmidecode-type17.txt`、`smartctl-health-output.txt`、`dmesg-oom-sample.txt`
- `tests/testdata/sys/devices/system/cpu/{online,offline,isolated}`、`cpu*/cpufreq/cpuinfo_min_freq|cpuinfo_max_freq`、`cpu*/cache/index*/{level,type,size}`、`sys/devices/system/node/node*/`
- `tests/testdata/proc/meminfo`（加 SReclaimable/Unevictable）、`proc/vmstat`（加 pswpin/pswpout/nr_isolated_*/nr_free_pages）

---

## 八、缺陷修复

1. **`/sys/class/*` 符号链接过滤**：`NetInterfaces`/`Thermal` 真机返回空（符号链接 IsDir=false）。修：`IsDir() || ModeSymlink`。
2. **swap_in/out 无 swap 机器不产出**：`prevSwapIn > 0` 判断改为 `hasPrevSwapIO` 标志位。
3. **ipmitool 装但无 BMC 反复 exec**：ipmi/dmesg/smartctl 失败结果也缓存（negative cache），避免每周期重试。
4. **statfs 包漏 linux build tag**：补 `//go:build linux`。

---

## 九、文档

- `docs/CATMonitor_indi_list.md`：CPU 小节(7→40)、Memory 小节(6→19)同步；汇总/附录 A/附录 B 更新（合计 70→83）。副本同步至 `/mnt/d/wyx/doc_metrics/`。
- `docs/test_report_v0.2.0.md`：本版系统测试报告。
- `/mnt/d/wyx/doc_metrics/CPU_metrics.md`、`MEM_metrics.md`：新增指标需求清单。
- `CATMonitor_indi_list.md` 的 disk/network 小节未改（指标集未变，仅内部迁移）。

---

## 十、未做 / 后续

- **gpu/npu 迁移**：需先建 `nvsmi`/`npsmi` 来源包（exec + 缓存 + Available），再迁 collector。
- **health 扣分扩展**：未给 CPU MCE / Memory saturation 加扣分规则（可选）。
- **per-metric 采集周期**：框架仍 per-collector interval，文档"默认周期"为设计目标。
- **Windows 来源迁移**：`*_windows.go` 保留原实现（决策 H）。
- **`-c` 短选项 bug**：未修（用 `--config`）。
- **版本号**：`main.go` 仍为 `version = "0.1.1"`，未升号（可按发布流程升 v0.2.0）。

---

## 十一、8 项架构决策（已落地）

| # | 决策 | 结论 |
|---|------|------|
| A | 来源返回形态 | parsed struct |
| B | 部件依赖来源方式 | 单例 + SetRoot/可注入 fetcher |
| C | proc/sys 缓存 | 不缓存 |
| D | ipmi SDR 缓存 | 30s + 失败缓存 |
| E | 温度/功率归属 | 留 collector，共享 ipmi 缓存 |
| F | source.Registry + list | 延后 |
| G | 本阶段迁移范围 | cpu/memory/disk/network |
| H | Windows 来源 | 延后 |

---

*所有改动在本地工作树，未提交 git。*
