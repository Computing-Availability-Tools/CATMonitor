# CATMonitor 采集指标清单

> 本文档列出 CATMonitor 支持的全部服务器运行指标。
> 每个指标包含：优先级、默认采集周期、默认是否采集、数据来源、采集方法、输出示例。
>
> **版本**: v0.2.0 ｜ **更新日期**: 2026-07-14 ｜ **指标总数**: 83（High 16 / Medium 30 / Low 37）
> **来源层**: cpu/memory/disk/network 采集器已接入 `internal/source/` 来源层（proc/sys/ipmi/lscpu/mce/dmesg/dmidecode/statfs/smartctl）；gpu/npu 待后续接入 nvsmi/npsmi。
> 完整测试报告见 [test_report.md](test_report.md)（141 用例通过，覆盖率 69.0%~92.3%）。

---

## 采集优先级定义

| 优先级 | 说明 | 默认采集 | 典型周期 |
|--------|------|----------|----------|
| **High** | 核心运行指标，直接影响健康度判断 | 是 | 3-5s |
| **Medium** | 重要辅助指标，对健康度有参考价值 | 是 | 10-60s |
| **Low** | 诊断性指标，用于深度排查 | 否 | 10-60s |

---

## 汇总统计

| 部件 | 指标数 | High | Medium | Low |
|------|--------|------|--------|-----|
| CPU | 40 | 4 | 12 | 24 |
| Memory | 19 | 4 | 7 | 8 |
| Disk | 7 | 1 | 3 | 3 |
| GPU | 7 | 3 | 3 | 1 |
| NPU | 5 | 3 | 2 | 0 |
| Network | 5 | 1 | 3 | 1 |
| **合计** | **83** | **16** | **30** | **37** |

---

## 1. CPU 采集指标

CPU 采集器通过 `/proc`、`/sys`、`lscpu`、`ipmitool`、`/var/log`(mcelog/dmesg)等多种数据源获取 CPU 运行状态与拓扑信息。数据获取与解析统一封装在来源层(`internal/source/`),CPU 采集器只负责编排与产出指标。

> 注:`temperature`/`mem_temperature`/`power` 来自 `ipmitool`(需 BMC,无 BMC 时优雅降级为空);`frequency`/`avg_freq`/`min_freq`/`max_freq`/`*_cache_size`/`*_core_num`(online/offline/isolated)来自 `/sys`(虚拟化/容器无对应 sysfs 时降级为空);`cpu_ce_errors`/`cpu_uce_errors` 来自 mcelog/dmesg(无 MCE 事件时降级为空)。

| 序号 | 指标名称 | 中文名称 | 优先级 | 默认周期 | 默认采集 | 单位 | 数据来源 |
|------|----------|----------|--------|----------|----------|------|----------|
| 1.1 | usage | CPU使用率 | High | 3s | 是 | % | /proc/stat |
| 1.2 | load_average | 系统负载 | High | 3s | 是 | - | /proc/loadavg |
| 1.3 | temperature | CPU温度 | Medium | 10s | 是 | °C | ipmitool (SDR) |
| 1.4 | frequency | CPU频率 | Medium | 10s | 否 | MHz | /sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq |
| 1.5 | context_switches | 上下文切换次数 | Low | 10s | 否 | 次/s | /proc/stat (ctxt 行) |
| 1.6 | process_count | 运行进程数 | Low | 10s | 否 | 个 | /proc/loadavg |
| 1.7 | model_info | CPU型号信息 | Low | 启动时1次 | 是 | - | /proc/cpuinfo |
| 1.8 | user_time | 用户态运行时间 | Low | 10s | 否 | jiffies | /proc/stat |
| 1.9 | nice_time | 低优先级用户进程时间 | Low | 10s | 否 | jiffies | /proc/stat |
| 1.10 | system_time | 内核态运行时间 | Low | 10s | 否 | jiffies | /proc/stat |
| 1.11 | idle_time | 空闲时间 | Low | 10s | 否 | jiffies | /proc/stat |
| 1.12 | iowait_time | 等待IO时间 | Low | 10s | 否 | jiffies | /proc/stat |
| 1.13 | irq_time | 硬中断处理时间 | Low | 10s | 否 | jiffies | /proc/stat |
| 1.14 | softirq_time | 软中断处理时间 | Low | 10s | 否 | jiffies | /proc/stat |
| 1.15 | steal_time | 被窃取时间 | Low | 10s | 否 | jiffies | /proc/stat |
| 1.16 | user_util | 用户态平均利用率 | Medium | 10s | 否 | % | /proc/stat |
| 1.17 | system_util | 内核态平均利用率 | Medium | 10s | 否 | % | /proc/stat |
| 1.18 | idle_util | 空闲状态占用率 | Medium | 10s | 否 | % | /proc/stat |
| 1.19 | iowait_util | IO等待状态占用率 | Medium | 10s | 否 | % | /proc/stat |
| 1.20 | numa_node_num | NUMA节点数量 | Low | 启动时1次 | 是 | 个 | lscpu / /sys/devices/system/node |
| 1.21 | online_core_num | 核在线数量 | Medium | 60s | 是 | 个 | /sys/devices/system/cpu/online |
| 1.22 | offline_core_num | 核离线数量 | Medium | 60s | 是 | 个 | /sys/devices/system/cpu/offline |
| 1.23 | isolated_core_num | 核隔离数量 | Medium | 60s | 是 | 个 | /sys/devices/system/cpu/isolated |
| 1.24 | mem_temperature | CPU内存区域温度 | Medium | 10s | 是 | °C | ipmitool (SDR) |
| 1.25 | core_num | CPU核数量 | Low | 启动时1次 | 是 | 个 | lscpu |
| 1.26 | die_core_num | 单个die核数量 | Low | 启动时1次 | 是 | 个 | lscpu |
| 1.27 | numa_core_num | NUMA核数量 | Low | 启动时1次 | 是 | 个 | lscpu |
| 1.28 | cpu_num | CPU个数 | Low | 启动时1次 | 是 | 个 | lscpu |
| 1.29 | avg_freq | CPU平均频率 | Medium | 10s | 否 | MHz | /sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq |
| 1.30 | min_freq | CPU最小频率 | Low | 启动时1次 | 否 | MHz | /sys/devices/system/cpu/cpu*/cpufreq/cpuinfo_min_freq |
| 1.31 | max_freq | CPU最大频率 | Low | 启动时1次 | 否 | MHz | /sys/devices/system/cpu/cpu*/cpufreq/cpuinfo_max_freq |
| 1.32 | cpu_ce_errors | CPU CE错误数量 | High | 30s | 是 | 次 | dmesg / /var/log/mcelog |
| 1.33 | cpu_uce_errors | CPU UCE错误数量 | High | 30s | 是 | 次 | dmesg / /var/log/mcelog |
| 1.34 | power | CPU功率 | Medium | 60s | 否 | W | ipmitool (SDR/DCMI) |
| 1.35 | l1d_cache_size | L1d缓存大小 | Low | 启动时1次 | 否 | KB | /sys/devices/system/cpu/cpu*/cache/index*/size |
| 1.36 | l1i_cache_size | L1i缓存大小 | Low | 启动时1次 | 否 | KB | /sys/devices/system/cpu/cpu*/cache/index*/size |
| 1.37 | l2_cache_size | L2缓存大小 | Low | 启动时1次 | 否 | KB | /sys/devices/system/cpu/cpu*/cache/index*/size |
| 1.38 | l3_cache_size | L3缓存大小 | Low | 启动时1次 | 否 | KB | /sys/devices/system/cpu/cpu*/cache/index*/size |
| 1.39 | numa_order_num | NUMA节点buddy order数量 | Low | 60s | 否 | 个 | /proc/buddyinfo |
| 1.40 | numa_info | NUMA节点内存碎片信息 | Low | 60s | 否 | order | /proc/buddyinfo |

### 指标详情

#### 1.1 usage（CPU使用率）

- **数据来源**：`/proc/stat`
- **采集方法**：读取 `/proc/stat` 中 `cpu` 和 `cpu0`~`cpuN` 行的 10 个时间字段（user, nice, system, idle, iowait, irq, softirq, steal, guest, guest_nice），计算相邻两次采集的时间差值，使用率 = (total_delta - idle_delta) / total_delta × 100
- **Labels**：`core`（"0", "1", "2", ... 或 "total"）
- **输出示例**：
```json
{"component":"cpu","name":"usage","value":45.2,"unit":"%","labels":{"core":"total"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"cpu","name":"usage","value":42.1,"unit":"%","labels":{"core":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.2 load_average（系统负载）

- **数据来源**：`/proc/loadavg`
- **采集方法**：读取 `/proc/loadavg` 的前三个字段，分别为 1分钟、5分钟、15分钟平均负载
- **Labels**：`interval`（"1m", "5m", "15m"）
- **输出示例**：
```json
{"component":"cpu","name":"load_average","value":2.34,"unit":"","labels":{"interval":"1m"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.3 temperature（CPU温度）

- **数据来源**：`ipmitool`（`ipmitool sdr`，筛选 CPU 相关温度传感器）
- **采集方法**：调用 `ipmitool sdr` 读取主板传感器列表，筛选 CPU 相关温度项（如 "CPU1 Temp"），解析输出取温度值。来源层对 SDR 结果做 30s 缓存（一次拉取供 temperature/mem_temperature/power 共用）。需 ipmitool 已安装且有 BMC 访问权限；无 BMC 时该指标为空（优雅降级）
- **Labels**：`cpu`（socket 编号）、`sensor`（传感器名）
- **输出示例**：
```json
{"component":"cpu","name":"temperature","value":65.0,"unit":"°C","labels":{"cpu":"0","sensor":"CPU1 Temp"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.4 frequency（CPU频率）

- **数据来源**：`/sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq`
- **采集方法**：遍历各 CPU 核心的 `scaling_cur_freq` 文件，值为 kHz，除以 1000 转为 MHz
- **Labels**：`core`（"0", "1", ...）
- **输出示例**：
```json
{"component":"cpu","name":"frequency","value":2400,"unit":"MHz","labels":{"core":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.5 context_switches（上下文切换次数）

- **数据来源**：`/proc/stat` 中 `ctxt` 行
- **采集方法**：读取 `ctxt` 行的总切换次数，两次采集的差值除以间隔时间得出每秒切换次数
- **Labels**：无
- **输出示例**：
```json
{"component":"cpu","name":"context_switches","value":15234,"unit":"次/s","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.6 process_count（运行进程数）

- **数据来源**：`/proc/loadavg` 第四个字段（格式：`running/total`）
- **采集方法**：解析 `/proc/loadavg` 第四个字段 `running/total`，取 running 值和 total 值
- **Labels**：`type`（"running", "total"）
- **输出示例**：
```json
{"component":"cpu","name":"process_count","value":287,"unit":"个","labels":{"type":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.7 model_info（CPU型号信息）

- **数据来源**：`/proc/cpuinfo`
- **采集方法**：解析 `/proc/cpuinfo`，提取 model name、核心数、线程数、缓存大小等静态信息，启动时采集一次
- **Labels**：`model_name`（如 "Intel Xeon Gold 6248R"）
- **输出示例**：
```json
{"component":"cpu","name":"model_info","value":48,"unit":"cores","labels":{"model_name":"Intel(R) Xeon(R) Gold 6248R"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.8 user_time（用户态运行时间）

- **数据来源**：`/proc/stat` 中 `cpu` / `cpuN` 行第 1 字段（user）
- **采集方法**：读取各 cpu 行的 user 字段累计 jiffies（USER_HZ，x86 默认 1/100s）。与 usage/time/util 共享一次 `/proc/stat` 读取
- **Labels**：`core`（"0", "1", ... 或 "total"）
- **输出示例**：
```json
{"component":"cpu","name":"user_time","value":335700,"unit":"jiffies","labels":{"core":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.9 nice_time（低优先级用户进程时间）

- **数据来源**：`/proc/stat` 第 2 字段（nice）
- **采集方法**：读取 nice 字段累计 jiffies
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"nice_time","value":0,"unit":"jiffies","labels":{"core":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.10 system_time（内核态运行时间）

- **数据来源**：`/proc/stat` 第 3 字段（system）
- **采集方法**：读取 system 字段累计 jiffies
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"system_time","value":43130,"unit":"jiffies","labels":{"core":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.11 idle_time（空闲时间）

- **数据来源**：`/proc/stat` 第 4 字段（idle）
- **采集方法**：读取 idle 字段累计 jiffies
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"idle_time","value":1362393,"unit":"jiffies","labels":{"core":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.12 iowait_time（等待IO时间）

- **数据来源**：`/proc/stat` 第 5 字段（iowait）
- **采集方法**：读取 iowait 字段累计 jiffies
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"iowait_time","value":15234,"unit":"jiffies","labels":{"core":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.13 irq_time（硬中断处理时间）

- **数据来源**：`/proc/stat` 第 6 字段（irq）
- **采集方法**：读取 irq 字段累计 jiffies
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"irq_time","value":1024,"unit":"jiffies","labels":{"core":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.14 softirq_time（软中断处理时间）

- **数据来源**：`/proc/stat` 第 7 字段（softirq）
- **采集方法**：读取 softirq 字段累计 jiffies
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"softirq_time","value":876,"unit":"jiffies","labels":{"core":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.15 steal_time（被窃取时间）

- **数据来源**：`/proc/stat` 第 8 字段（steal）
- **采集方法**：读取 steal 字段累计 jiffies；虚拟化环境下表示被 hypervisor 偷走的时间
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"steal_time","value":0,"unit":"jiffies","labels":{"core":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.16 user_util（用户态平均利用率）

- **数据来源**：`/proc/stat`（user+nice 字段差值）
- **采集方法**：两次采集间 (user+nice) 增量占总时间增量的百分比。需 prev 快照，首次采集不产出
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"user_util","value":18.5,"unit":"%","labels":{"core":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.17 system_util（内核态平均利用率）

- **数据来源**：`/proc/stat`（system 字段差值）
- **采集方法**：system 增量占总时间增量百分比
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"system_util","value":3.2,"unit":"%","labels":{"core":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.18 idle_util（空闲状态占用率）

- **数据来源**：`/proc/stat`（idle 字段差值）
- **采集方法**：idle 增量占总时间增量百分比
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"idle_util","value":75.4,"unit":"%","labels":{"core":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.19 iowait_util（IO等待状态占用率）

- **数据来源**：`/proc/stat`（iowait 字段差值）
- **采集方法**：iowait 增量占总时间增量百分比
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"iowait_util","value":3.2,"unit":"%","labels":{"core":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.20 numa_node_num（NUMA节点数量）

- **数据来源**：`lscpu` 或 `/sys/devices/system/node/`
- **采集方法**：统计 NUMA 节点数；UMA 返回 1。启动时采集一次
- **Labels**：无
- **输出示例**：
```json
{"component":"cpu","name":"numa_node_num","value":2,"unit":"个","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.21 online_core_num（核在线数量）

- **数据来源**：`/sys/devices/system/cpu/online`
- **采集方法**：解析 "0-3,5,7-9" 格式为在线核数
- **Labels**：无
- **输出示例**：
```json
{"component":"cpu","name":"online_core_num","value":28,"unit":"个","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.22 offline_core_num（核离线数量）

- **数据来源**：`/sys/devices/system/cpu/offline`
- **采集方法**：解析离线核数
- **Labels**：无
- **输出示例**：
```json
{"component":"cpu","name":"offline_core_num","value":0,"unit":"个","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.23 isolated_core_num（核隔离数量）

- **数据来源**：`/sys/devices/system/cpu/isolated`
- **采集方法**：解析隔离核数（isolcpus）
- **Labels**：无
- **输出示例**：
```json
{"component":"cpu","name":"isolated_core_num","value":2,"unit":"个","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.24 mem_temperature（CPU内存区域温度）

- **数据来源**：`ipmitool`（SDR，筛选内存区域温度传感器）
- **采集方法**：从缓存的 SDR 中筛选 "MEM* Temp" 传感器取温度。无 BMC 时空
- **Labels**：`cpu`、`sensor`
- **输出示例**：
```json
{"component":"cpu","name":"mem_temperature","value":42.0,"unit":"°C","labels":{"cpu":"0","sensor":"MEM1 Temp"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.25 core_num（CPU核数量）

- **数据来源**：`lscpu`（CPU(s) 字段）
- **采集方法**：解析逻辑核总数。启动时采集一次
- **Labels**：无
- **输出示例**：
```json
{"component":"cpu","name":"core_num","value":28,"unit":"个","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.26 die_core_num（单个die核数量）

- **数据来源**：`lscpu`（Cores per socket / Die(s) per socket）
- **采集方法**：CoresPerSocket / DiesPerSocket
- **Labels**：`die`（"0", "1", ...）
- **输出示例**：
```json
{"component":"cpu","name":"die_core_num","value":14,"unit":"个","labels":{"die":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.27 numa_core_num（NUMA核数量）

- **数据来源**：`lscpu`
- **采集方法**：Cores / NUMA 节点数（均匀分配假设）
- **Labels**：`node`（"0", "1", ...）
- **输出示例**：
```json
{"component":"cpu","name":"numa_core_num","value":14,"unit":"个","labels":{"node":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.28 cpu_num（CPU个数）

- **数据来源**：`lscpu`（Socket(s) 字段）
- **采集方法**：解析物理 CPU 封装数
- **Labels**：无
- **输出示例**：
```json
{"component":"cpu","name":"cpu_num","value":2,"unit":"个","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.29 avg_freq（CPU平均频率）

- **数据来源**：`/sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq`
- **采集方法**：所有在线核心当前频率的算术平均，kHz/1000 转 MHz
- **Labels**：无
- **输出示例**：
```json
{"component":"cpu","name":"avg_freq","value":2400,"unit":"MHz","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.30 min_freq（CPU最小频率）

- **数据来源**：`/sys/devices/system/cpu/cpu*/cpufreq/cpuinfo_min_freq`
- **采集方法**：硬件最低频率。启动时采集一次
- **Labels**：无
- **输出示例**：
```json
{"component":"cpu","name":"min_freq","value":800,"unit":"MHz","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.31 max_freq（CPU最大频率）

- **数据来源**：`/sys/devices/system/cpu/cpu*/cpufreq/cpuinfo_max_freq`
- **采集方法**：硬件最高频率。启动时采集一次
- **Labels**：无
- **输出示例**：
```json
{"component":"cpu","name":"max_freq","value":3500,"unit":"MHz","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.32 cpu_ce_errors（CPU CE错误数量）

- **数据来源**：`dmesg` 或 `/var/log/mcelog`
- **采集方法**：解析 MCE 记录统计 CE（已纠正硬件错误）数，差值得本周期新增。属 CPU 级 MCE，与 Memory 模块的 EDAC 内存 ECC 不同
- **Labels**：`cpu`（socket 编号）
- **输出示例**：
```json
{"component":"cpu","name":"cpu_ce_errors","value":3,"unit":"次","labels":{"cpu":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.33 cpu_uce_errors（CPU UCE错误数量）

- **数据来源**：`dmesg` 或 `/var/log/mcelog`
- **采集方法**：解析 MCE 记录统计 UCE（不可纠正硬件错误）数
- **Labels**：`cpu`
- **输出示例**：
```json
{"component":"cpu","name":"cpu_uce_errors","value":0,"unit":"次","labels":{"cpu":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.34 power（CPU功率）

- **数据来源**：`ipmitool`（SDR 中 CPU 功率传感器 或 `dcmi power reading`）
- **采集方法**：从缓存的 SDR 中筛选 "CPU* Pwr" 传感器取功率。无 BMC 时空
- **Labels**：`cpu`、`sensor`
- **输出示例**：
```json
{"component":"cpu","name":"power","value":125.5,"unit":"W","labels":{"cpu":"0","sensor":"CPU1 Pwr"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.35 l1d_cache_size（L1d缓存大小）

- **数据来源**：`/sys/devices/system/cpu/cpu*/cache/index*/size`（level=1、type=Data）
- **采集方法**：解析 "32K" 为 KB。启动时采集一次
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"l1d_cache_size","value":32,"unit":"KB","labels":{"core":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.36 l1i_cache_size（L1i缓存大小）

- **数据来源**：`/sys/.../cache/index*/size`（level=1、type=Instruction）
- **采集方法**：同上
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"l1i_cache_size","value":32,"unit":"KB","labels":{"core":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.37 l2_cache_size（L2缓存大小）

- **数据来源**：`/sys/.../cache/index*/size`（level=2）
- **采集方法**：同上
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"l2_cache_size","value":1024,"unit":"KB","labels":{"core":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.38 l3_cache_size（L3缓存大小）

- **数据来源**：`/sys/.../cache/index*/size`（level=3）
- **采集方法**：同上
- **Labels**：`core`
- **输出示例**：
```json
{"component":"cpu","name":"l3_cache_size","value":35840,"unit":"KB","labels":{"core":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.39 numa_order_num（NUMA节点buddy order数量）

- **数据来源**：`/proc/buddyinfo`
- **采集方法**：解析每行（node,zone）的 order 列数
- **Labels**：`node`、`zone`
- **输出示例**：
```json
{"component":"cpu","name":"numa_order_num","value":11,"unit":"个","labels":{"node":"0","zone":"Normal"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 1.40 numa_info（NUMA节点内存碎片信息）

- **数据来源**：`/proc/buddyinfo`
- **采集方法**：取该 node/zone 可用最大连续 order（从高阶扫描首个空闲块数 >0 的 order），值越大碎片越少
- **Labels**：`node`、`zone`
- **输出示例**：
```json
{"component":"cpu","name":"numa_info","value":8,"unit":"order","labels":{"node":"0","zone":"Normal"},"timestamp":"2026-07-10T10:30:00Z"}
```

---

## 2. Memory 采集指标

内存采集器通过 `/proc/meminfo`、`/proc/vmstat`、`/proc/pressure/memory`、`/proc/buddyinfo`、EDAC 框架(`/sys/devices/system/edac/mc/`)、内核日志(`dmesg`)、`dmidecode`、`ipmitool` 等数据源获取内存运行状态与硬件信息。

> 注:`saturation` 依赖内核 PSI(`/proc/pressure/memory`,需 `CONFIG_PSI`);`module_*` 依赖 `dmidecode`(需 root);`power` 依赖 `ipmitool`(需 BMC);`ecc_*` 依赖 EDAC。对应数据源不可用时指标优雅降级为空。

| 序号 | 指标名称 | 中文名称 | 优先级 | 默认周期 | 默认采集 | 单位 | 数据来源 |
|------|----------|----------|--------|----------|----------|------|----------|
| 2.1 | usage | 内存使用率 | High | 3s | 是 | % | /proc/meminfo |
| 2.2 | swap_usage | Swap使用率 | High | 3s | 是 | % | /proc/meminfo |
| 2.3 | swap_detail | Swap原始值 | Medium | 3s | 是 | MB | /proc/meminfo |
| 2.4 | swap_in | 换入次数 | Medium | 3s | 是 | 次/s | /proc/vmstat (pswpin) |
| 2.5 | swap_out | 换出次数 | Medium | 3s | 是 | 次/s | /proc/vmstat (pswpout) |
| 2.6 | saturation | 内存饱和度 | Medium | 3s | 是 | % | /proc/pressure/memory |
| 2.7 | fragmentation | 内存碎片化程度 | Medium | 60s | 是 | % | /proc/buddyinfo |
| 2.8 | ecc_ce_errors | CE可纠正错误数 | High | 5s | 是 | 次 | /sys/devices/system/edac/mc/mc*/ce_count |
| 2.9 | ecc_uce_errors | UCE不可纠正错误数 | High | 5s | 是 | 次 | /sys/devices/system/edac/mc/mc*/ue_count |
| 2.10 | oom_count | OOM触发次数 | Medium | 30s | 否 | 次 | dmesg / journalctl |
| 2.11 | page_faults | 缺页错误次数 | Low | 10s | 否 | 次/s | /proc/vmstat |
| 2.12 | isolated_pages | 隔离页总数 | Low | 10s | 否 | 个 | /proc/vmstat (nr_isolated_anon+nr_isolated_file) |
| 2.13 | isolated_anon_pages | 隔离匿名页数 | Low | 10s | 否 | 个 | /proc/vmstat (nr_isolated_anon) |
| 2.14 | isolated_file_pages | 隔离文件页数 | Low | 10s | 否 | 个 | /proc/vmstat (nr_isolated_file) |
| 2.15 | free_pages | 空闲页数 | Low | 10s | 否 | 个 | /proc/vmstat (nr_free_pages) |
| 2.16 | module_num | 内存条数量 | Low | 启动时1次 | 是 | 个 | dmidecode --type 17 |
| 2.17 | module_size | 内存条大小 | Low | 启动时1次 | 是 | MB | dmidecode --type 17 |
| 2.18 | module_info | 内存条静态信息 | Low | 启动时1次 | 是 | - | dmidecode --type 17 |
| 2.19 | power | 内存功率 | Medium | 60s | 否 | W | ipmitool (SDR) |

### 指标详情

#### 2.1 usage（内存使用率）

- **数据来源**：`/proc/meminfo`
- **采集方法**：读取 `MemTotal`、`MemAvailable`，使用率 = (MemTotal - MemAvailable) / MemTotal × 100。同时产出 `usage_detail`（含多个 field 的原始值，单位 MB）：`total`/`used`/`available`（原有）+ `free`(MemFree)/`buffers`(Buffers)/`cached`(Cached)/`sreclaimable`(SReclaimable)/`unevictable`(Unevictable)（本版新增）
- **Labels**：usage 无；usage_detail 含 `field`
- **输出示例**：
```json
{"component":"memory","name":"usage","value":62.5,"unit":"%","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"memory","name":"usage_detail","value":16384,"unit":"MB","labels":{"field":"total"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"memory","name":"usage_detail","value":10240,"unit":"MB","labels":{"field":"used"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"memory","name":"usage_detail","value":6144,"unit":"MB","labels":{"field":"available"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"memory","name":"usage_detail","value":4096,"unit":"MB","labels":{"field":"free"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"memory","name":"usage_detail","value":256,"unit":"MB","labels":{"field":"buffers"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"memory","name":"usage_detail","value":2048,"unit":"MB","labels":{"field":"cached"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"memory","name":"usage_detail","value":512,"unit":"MB","labels":{"field":"sreclaimable"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"memory","name":"usage_detail","value":64,"unit":"MB","labels":{"field":"unevictable"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.2 swap_usage（Swap使用率）

- **数据来源**：`/proc/meminfo`
- **采集方法**：读取 `SwapTotal`、`SwapFree`，使用率 = (SwapTotal - SwapFree) / SwapTotal × 100。原始值见 2.3 swap_detail
- **Labels**：无
- **输出示例**：
```json
{"component":"memory","name":"swap_usage","value":15.3,"unit":"%","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.3 swap_detail（Swap原始值）

- **数据来源**：`/proc/meminfo`（SwapTotal / SwapFree）
- **采集方法**：产出 swap 原始大小（MB），`field` 区分 total/free/used（used = SwapTotal - SwapFree）。与 swap_usage（%）互补
- **Labels**：`field`（"total"、"free"、"used"）
- **输出示例**：
```json
{"component":"memory","name":"swap_detail","value":8192,"unit":"MB","labels":{"field":"total"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"memory","name":"swap_detail","value":192,"unit":"MB","labels":{"field":"used"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"memory","name":"swap_detail","value":8000,"unit":"MB","labels":{"field":"free"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.4 swap_in（换入次数）

- **数据来源**：`/proc/vmstat`（pswpin 字段）
- **采集方法**：两次采集间 pswpin 累计值差值除以间隔时间，得每秒从 swap 区换入内存的页数。首次采集无 prev 快照不产出
- **Labels**：无
- **输出示例**：
```json
{"component":"memory","name":"swap_in","value":12,"unit":"次/s","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.5 swap_out（换出次数）

- **数据来源**：`/proc/vmstat`（pswpout 字段）
- **采集方法**：两次采集间 pswpout 累计值差值除以间隔时间，得每秒从内存换出到 swap 区的页数
- **Labels**：无
- **输出示例**：
```json
{"component":"memory","name":"swap_out","value":5,"unit":"次/s","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.6 saturation（内存饱和度）

- **数据来源**：`/proc/pressure/memory`（PSI memory，需内核启用 `CONFIG_PSI`）
- **采集方法**：解析 "some" 行的 avg10/avg60/avg300（部分任务因内存压力阻塞的时间占比，%）。无 PSI 文件时为空（优雅降级）
- **Labels**：`interval`（"avg10"、"avg60"、"avg300"）
- **输出示例**：
```json
{"component":"memory","name":"saturation","value":0.06,"unit":"%","labels":{"interval":"avg10"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.7 fragmentation（内存碎片化程度）

- **数据来源**：`/proc/buddyinfo`
- **采集方法**：对每个 (node,zone)，计算 **order 0 空闲页数占总空闲页数的百分比**（按页数加权：order0_free / Σ(count_i × 2^i) × 100）。值越大越碎片化；0 表示无空闲页
- **Labels**：`node`、`zone`
- **输出示例**：
```json
{"component":"memory","name":"fragmentation","value":20.7,"unit":"%","labels":{"node":"0","zone":"Normal"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.8 ecc_ce_errors（CE可纠正错误数）

- **数据来源**：`/sys/devices/system/edac/mc/mc*/ce_count`
- **采集方法**：遍历 EDAC 框架下各内存控制器（mc0, mc1...）的 `ce_count` 文件，读取累计 CE 错误数。如服务器不支持 EDAC 则返回空
- **Labels**：`mc`（"mc0"、"mc1"、...）
- **输出示例**：
```json
{"component":"memory","name":"ecc_ce_errors","value":3,"unit":"次","labels":{"mc":"mc0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.9 ecc_uce_errors（UCE不可纠正错误数）

- **数据来源**：`/sys/devices/system/edac/mc/mc*/ue_count`
- **采集方法**：遍历 EDAC 框架下各内存控制器的 `ue_count` 文件，读取累计 UCE 错误数
- **Labels**：`mc`（"mc0"、"mc1"、...）
- **输出示例**：
```json
{"component":"memory","name":"ecc_uce_errors","value":0,"unit":"次","labels":{"mc":"mc0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.10 oom_count（OOM触发次数）

- **数据来源**：`dmesg` 或 `journalctl -k --since` 输出
- **采集方法**：搜索内核日志中 "Out of memory" 或 "Killed process" 关键词，统计最近周期内 OOM Killer 触发次数
- **Labels**：无
- **输出示例**：
```json
{"component":"memory","name":"oom_count","value":0,"unit":"次","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.11 page_faults（缺页错误次数）

- **数据来源**：`/proc/vmstat`
- **采集方法**：读取 `pgfault`（总缺页）和 `pgmajfault`（主要缺页）字段，差值除以间隔时间得出每秒缺页次数
- **Labels**：`type`（"minor"、"major"）
- **输出示例**：
```json
{"component":"memory","name":"page_faults","value":1523,"unit":"次/s","labels":{"type":"minor"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.12 isolated_pages（隔离页总数）

- **数据来源**：`/proc/vmstat`（nr_isolated_anon + nr_isolated_file）
- **采集方法**：匿名隔离页与文件隔离页之和（正在进行迁移/offline 而被临时隔离的页总数）
- **Labels**：无
- **输出示例**：
```json
{"component":"memory","name":"isolated_pages","value":128,"unit":"个","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.13 isolated_anon_pages（隔离匿名页数）

- **数据来源**：`/proc/vmstat`（nr_isolated_anon）
- **采集方法**：读取匿名隔离页数
- **Labels**：无
- **输出示例**：
```json
{"component":"memory","name":"isolated_anon_pages","value":80,"unit":"个","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.14 isolated_file_pages（隔离文件页数）

- **数据来源**：`/proc/vmstat`（nr_isolated_file）
- **采集方法**：读取文件隔离页数
- **Labels**：无
- **输出示例**：
```json
{"component":"memory","name":"isolated_file_pages","value":48,"unit":"个","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.15 free_pages（空闲页数）

- **数据来源**：`/proc/vmstat`（nr_free_pages）
- **采集方法**：读取空闲页总数（页数，与 usage_detail 的 `free` 字段单位不同：前者是页，后者是 MB）
- **Labels**：无
- **输出示例**：
```json
{"component":"memory","name":"free_pages","value":1500000,"unit":"个","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.16 module_num（内存条数量）

- **数据来源**：`dmidecode --type 17`（SMBIOS Memory Device）
- **采集方法**：解析 dmidecode 输出，统计已安装内存条（DIMM）数量。启动时采集一次。需 root 权限
- **Labels**：无
- **输出示例**：
```json
{"component":"memory","name":"module_num","value":8,"unit":"个","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.17 module_size（内存条大小）

- **数据来源**：`dmidecode --type 17`（Size 字段）
- **采集方法**：解析每条 DIMM 的 Size（MB）。启动时采集一次
- **Labels**：`locator`（如 "DIMM0"）
- **输出示例**：
```json
{"component":"memory","name":"module_size","value":16384,"unit":"MB","labels":{"locator":"DIMM0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.18 module_info（内存条静态信息）

- **数据来源**：`dmidecode --type 17`
- **采集方法**：解析每条 DIMM 的静态信息（型号/速率/厂商/类型）。启动时采集一次
- **Labels**：`locator`、`type`（如 "DDR4"）、`speed`、`manufacturer`
- **输出示例**：
```json
{"component":"memory","name":"module_info","value":16384,"unit":"MB","labels":{"locator":"DIMM0","type":"DDR4","speed":"3200 MT/s","manufacturer":"Samsung"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.19 power（内存功率）

- **数据来源**：`ipmitool`（SDR 中内存功率传感器，如 "MEM* Pwr"）
- **采集方法**：从缓存的 SDR 中筛选内存功率传感器取功率值（W）。与 CPU 的 power 共用同一份 SDR 缓存（30s）。无 BMC 时为空
- **Labels**：`sensor`
- **输出示例**：
```json
{"component":"memory","name":"power","value":12.5,"unit":"W","labels":{"sensor":"MEM1 Pwr"},"timestamp":"2026-07-10T10:30:00Z"}
```

---

## 3. Disk 采集指标

磁盘采集器通过 statfs 系统调用、`/proc/diskstats`、`/proc/stat` 和 `smartctl` 命令获取硬盘运行状态。

| 序号 | 指标名称 | 中文名称 | 优先级 | 默认周期 | 默认采集 | 单位 | 数据来源 |
|------|----------|----------|--------|----------|----------|------|----------|
| 3.1 | space_usage | 磁盘空间使用率 | High | 5s | 是 | % | statfs syscall |
| 3.2 | iops | 读写IOPS | Medium | 5s | 是 | 次/s | /proc/diskstats |
| 3.3 | throughput | 读写吞吐量 | Medium | 5s | 是 | MB/s | /proc/diskstats |
| 3.4 | io_wait | I/O等待占比 | Medium | 5s | 是 | % | /proc/stat |
| 3.5 | smart_status | SMART健康状态 | Medium | 60s | 否 | - | smartctl -H |
| 3.6 | smart_temperature | 硬盘温度 | Low | 60s | 否 | °C | smartctl -A |
| 3.7 | io_errors | I/O错误计数 | Low | 30s | 否 | 次 | /proc/diskstats, dmesg |

### 指标详情

#### 3.1 space_usage（磁盘空间使用率）

- **数据来源**：`statfs` 系统调用
- **采集方法**：读取 `/proc/mounts` 获取所有挂载点，对每个挂载点调用 `statfs()` 获取总块数、空闲块数、块大小，计算 total、used、available、使用率。过滤虚拟文件系统（proc, sysfs, tmpfs, devtmpfs 等）
- **Labels**：`device`（"/dev/sda1"）、`mount_point`（"/"）、`fstype`（"ext4"）
- **输出示例**：
```json
{"component":"disk","name":"space_usage","value":72.5,"unit":"%","labels":{"device":"/dev/sda1","mount_point":"/","fstype":"ext4"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"disk","name":"space_detail","value":512000,"unit":"MB","labels":{"device":"/dev/sda1","mount_point":"/","field":"total"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"disk","name":"space_detail","value":371200,"unit":"MB","labels":{"device":"/dev/sda1","mount_point":"/","field":"used"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 3.2 iops（读写IOPS）

- **数据来源**：`/proc/diskstats`
- **采集方法**：读取各块设备的第4字段（reads completed）和第8字段（writes completed），两次差值除以间隔时间得出每秒 IOPS。过滤虚拟设备和分区（只采集主设备 sda, sdb, nvme0n1 等，排除 sda1, sda2 等分区）
- **Labels**：`device`（"sda", "sdb", "nvme0n1"）、`direction`（"read", "write"）
- **输出示例**：
```json
{"component":"disk","name":"iops","value":152,"unit":"次/s","labels":{"device":"sda","direction":"read"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 3.3 throughput（读写吞吐量）

- **数据来源**：`/proc/diskstats`
- **采集方法**：读取各块设备的第6字段（sectors read）和第10字段（sectors written），扇区数 × 512B 差值除以间隔时间得出吞吐量（MB/s）
- **Labels**：`device`（"sda", "nvme0n1"）、`direction`（"read", "write"）
- **输出示例**：
```json
{"component":"disk","name":"throughput","value":25.6,"unit":"MB/s","labels":{"device":"sda","direction":"read"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 3.4 io_wait（I/O等待占比）

- **数据来源**：`/proc/stat` 中 `cpu` 行的 `iowait` 字段
- **采集方法**：读取 `cpu` 行的第5个字段（iowait），与上一次采集的差值除以总 CPU 时间差值，得出 I/O Wait 百分比
- **Labels**：无
- **输出示例**：
```json
{"component":"disk","name":"io_wait","value":3.2,"unit":"%","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 3.5 smart_status（SMART健康状态）

- **数据来源**：`smartctl -H /dev/sdX` 命令输出
- **采集方法**：遍历块设备，对每个支持 SMART 的设备执行 `smartctl -H`，解析输出中的整体健康评估结果（PASSED/FAILED）。需要 smartmontools 已安装且有 root 权限
- **Labels**：`device`（"sda", "sdb"）
- **输出示例**：
```json
{"component":"disk","name":"smart_status","value":1,"unit":"","labels":{"device":"sda","status":"PASSED"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 3.6 smart_temperature（硬盘温度）

- **数据来源**：`smartctl -A /dev/sdX` 命令输出
- **采集方法**：执行 `smartctl -A`，解析 SMART 属性表中的 `Temperature_Celsius` 或 `Temperature` 属性获取温度值
- **Labels**：`device`（"sda", "sdb"）
- **输出示例**：
```json
{"component":"disk","name":"smart_temperature","value":35,"unit":"°C","labels":{"device":"sda"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 3.7 io_errors（I/O错误计数）

- **数据来源**：`/proc/diskstats` 错误字段 + `dmesg` 日志
- **采集方法**：读取 `/proc/diskstats` 中错误相关字段（读错误、写错误），同时搜索 `dmesg` 中的 I/O error 关键词
- **Labels**：`device`（"sda"）、`type`（"read_err", "write_err"）
- **输出示例**：
```json
{"component":"disk","name":"io_errors","value":0,"unit":"次","labels":{"device":"sda","type":"read_err"},"timestamp":"2026-07-10T10:30:00Z"}
```

---

## 4. GPU 采集指标（NVIDIA）

GPU 采集器通过调用 `nvidia-smi` 命令获取 NVIDIA GPU 运行状态。采集器启动时检测 `nvidia-smi` 是否可用，不可用则自动跳过该采集器。

| 序号 | 指标名称 | 中文名称 | 优先级 | 默认周期 | 默认采集 | 单位 | 数据来源 |
|------|----------|----------|--------|----------|----------|------|----------|
| 4.1 | utilization | GPU使用率 | High | 3s | 是 | % | nvidia-smi |
| 4.2 | memory_usage | 显存使用率 | High | 3s | 是 | % | nvidia-smi |
| 4.3 | temperature | GPU温度 | High | 3s | 是 | °C | nvidia-smi |
| 4.4 | power_draw | GPU功耗 | Medium | 10s | 是 | W | nvidia-smi |
| 4.5 | fan_speed | 风扇转速 | Medium | 10s | 是 | % | nvidia-smi |
| 4.6 | ecc_errors | ECC错误数 | Medium | 30s | 是 | 次 | nvidia-smi |
| 4.7 | clock_frequency | 时钟频率 | Low | 10s | 否 | MHz | nvidia-smi |

### 采集方法

使用 `nvidia-smi` 的 query 模式批量查询，单次命令获取所有 GPU 的指定字段：

```bash
nvidia-smi \
  --query-gpu=index,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,fan.speed,ecc.errors.uncorrected.volatile.total,clocks.gr \
  --format=csv,noheader,nounits
```

按行解析输出，每行对应一块 GPU，字段间以逗号分隔。

### 指标详情

#### 4.1 utilization（GPU使用率）

- **数据来源**：`nvidia-smi --query-gpu=utilization.gpu`
- **采集方法**：query 模式获取各 GPU 计算单元使用率（0-100）
- **Labels**：`gpu_id`（"0", "1", "2", ...）
- **输出示例**：
```json
{"component":"gpu","name":"utilization","value":82,"unit":"%","labels":{"gpu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 4.2 memory_usage（显存使用率）

- **数据来源**：`nvidia-smi --query-gpu=memory.used,memory.total`
- **采集方法**：获取显存已用量和总量，使用率 = memory.used / memory.total × 100
- **Labels**：`gpu_id`（"0", "1", ...）
- **输出示例**：
```json
{"component":"gpu","name":"memory_usage","value":75.5,"unit":"%","labels":{"gpu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"gpu","name":"memory_detail","value":16384,"unit":"MB","labels":{"gpu_id":"0","field":"used"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"gpu","name":"memory_detail","value":24576,"unit":"MB","labels":{"gpu_id":"0","field":"total"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 4.3 temperature（GPU温度）

- **数据来源**：`nvidia-smi --query-gpu=temperature.gpu`
- **采集方法**：获取各 GPU 核心温度（摄氏度）
- **Labels**：`gpu_id`（"0", "1", ...）
- **输出示例**：
```json
{"component":"gpu","name":"temperature","value":72,"unit":"°C","labels":{"gpu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 4.4 power_draw（GPU功耗）

- **数据来源**：`nvidia-smi --query-gpu=power.draw`
- **采集方法**：获取各 GPU 实时功耗（瓦特）
- **Labels**：`gpu_id`（"0", "1", ...）
- **输出示例**：
```json
{"component":"gpu","name":"power_draw","value":250.5,"unit":"W","labels":{"gpu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 4.5 fan_speed（风扇转速）

- **数据来源**：`nvidia-smi --query-gpu=fan.speed`
- **采集方法**：获取各 GPU 风扇转速占最大转速的百分比（0-100）。某些服务器 GPU 无独立风扇，返回 N/A
- **Labels**：`gpu_id`（"0", "1", ...）
- **输出示例**：
```json
{"component":"gpu","name":"fan_speed","value":65,"unit":"%","labels":{"gpu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 4.6 ecc_errors（ECC错误数）

- **数据来源**：`nvidia-smi --query-gpu=ecc.errors.uncorrected.volatile.total`
- **采集方法**：获取各 GPU 不可纠正 ECC 错误累计数
- **Labels**：`gpu_id`（"0", "1", ...）
- **输出示例**：
```json
{"component":"gpu","name":"ecc_errors","value":0,"unit":"次","labels":{"gpu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 4.7 clock_frequency（时钟频率）

- **数据来源**：`nvidia-smi --query-gpu=clocks.gr`
- **采集方法**：获取各 GPU 图形时钟频率（MHz）
- **Labels**：`gpu_id`（"0", "1", ...）
- **输出示例**：
```json
{"component":"gpu","name":"clock_frequency","value":1545,"unit":"MHz","labels":{"gpu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

---

## 5. NPU 采集指标（华为昇腾）

NPU 采集器通过调用 `npu-smi` 命令获取华为昇腾 NPU 运行状态。采集器启动时检测 `npu-smi` 是否可用，不可用则自动跳过。

| 序号 | 指标名称 | 中文名称 | 优先级 | 默认周期 | 默认采集 | 单位 | 数据来源 |
|------|----------|----------|--------|----------|----------|------|----------|
| 5.1 | utilization | NPU使用率 | High | 3s | 是 | % | npu-smi info |
| 5.2 | memory_usage | NPU显存使用率 | High | 3s | 是 | % | npu-smi info |
| 5.3 | temperature | NPU温度 | High | 3s | 是 | °C | npu-smi info |
| 5.4 | power_draw | NPU功耗 | Medium | 10s | 是 | W | npu-smi info |
| 5.5 | health_status | NPU健康状态 | Medium | 10s | 是 | - | npu-smi info |

### 采集方法

执行 `npu-smi info` 命令，解析表格输出。典型输出格式：

```
+-------------------------------------------------------------------------------------------+
| npu-smi 23.0.0                  Version: 23.0.0                                          |
+======================+===============+=======================================================+
| NPU     Name         | Health        | Power(W)     Temp(C)               Hugepages-Usage(page) |
| Chip                   | Bus-Id        | AICore(%)  Memory-Usage(MB)                            |
+======================+===============+=======================================================+
| 0       910A         | OK            | 65.0        42                  0    / 0                 |
| 0                      | 0000:01:00.0  | 0           0  / 0                                       |
+======================+===============+=======================================================+
```

解析每块 NPU 的 Health、Power、Temp、AICore(%)、Memory-Usage 字段。

### 指标详情

#### 5.1 utilization（NPU使用率）

- **数据来源**：`npu-smi info` 输出中的 AICore(%) 列
- **采集方法**：解析 `npu-smi info` 表格输出，提取每块 NPU 的 AI Core 使用率
- **Labels**：`npu_id`（"0", "1", ...）
- **输出示例**：
```json
{"component":"npu","name":"utilization","value":45,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.2 memory_usage（NPU显存使用率）

- **数据来源**：`npu-smi info` 输出中的 Memory-Usage(MB) 列
- **采集方法**：解析 `used / total` 格式的显存信息，计算使用率
- **Labels**：`npu_id`（"0", "1", ...）
- **输出示例**：
```json
{"component":"npu","name":"memory_usage","value":32.5,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.3 temperature（NPU温度）

- **数据来源**：`npu-smi info` 输出中的 Temp(C) 列
- **采集方法**：解析表格输出，提取每块 NPU 的温度
- **Labels**：`npu_id`（"0", "1", ...）
- **输出示例**：
```json
{"component":"npu","name":"temperature","value":42,"unit":"°C","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.4 power_draw（NPU功耗）

- **数据来源**：`npu-smi info` 输出中的 Power(W) 列
- **采集方法**：解析表格输出，提取每块 NPU 的功耗
- **Labels**：`npu_id`（"0", "1", ...）
- **输出示例**：
```json
{"component":"npu","name":"power_draw","value":65.0,"unit":"W","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.5 health_status（NPU健康状态）

- **数据来源**：`npu-smi info` 输出中的 Health 列
- **采集方法**：解析表格输出，提取每块 NPU 的健康状态（OK / Warning / Alarm / Critical）
- **Labels**：`npu_id`（"0", "1", ...）
- **输出示例**：
```json
{"component":"npu","name":"health_status","value":1,"unit":"","labels":{"npu_id":"0","status":"OK"},"timestamp":"2026-07-10T10:30:00Z"}
```

> 状态值映射：OK=1, Warning=2, Alarm=3, Critical=4

---

## 6. Network 采集指标

网络采集器读取 `/proc/net/dev`、`/sys/class/net/` 和 `/proc/net/tcp` 获取网卡运行状态。

| 序号 | 指标名称 | 中文名称 | 优先级 | 默认周期 | 默认采集 | 单位 | 数据来源 |
|------|----------|----------|--------|----------|----------|------|----------|
| 6.1 | throughput | 网络吞吐量 | High | 3s | 是 | bytes/s | /proc/net/dev |
| 6.2 | packet_count | 包收发数 | Medium | 5s | 是 | 个/s | /proc/net/dev |
| 6.3 | error_count | 错误包计数 | Medium | 5s | 是 | 次 | /proc/net/dev |
| 6.4 | interface_status | 网卡接口状态 | Medium | 10s | 是 | - | /sys/class/net/*/operstate |
| 6.5 | connection_count | 网络连接数 | Low | 10s | 否 | 个 | /proc/net/tcp, /proc/net/tcp6 |

### 指标详情

#### 6.1 throughput（网络吞吐量）

- **数据来源**：`/proc/net/dev`
- **采集方法**：读取各网卡的 `bytes` 字段（接收字节第1列，发送字节第9列），两次差值除以间隔时间得出每秒吞吐量。过滤 `lo` 回环接口
- **Labels**：`interface`（"eth0", "ens33", ...）、`direction`（"rx", "tx"）
- **输出示例**：
```json
{"component":"network","name":"throughput","value":1250000,"unit":"bytes/s","labels":{"interface":"eth0","direction":"rx"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"network","name":"throughput","value":890000,"unit":"bytes/s","labels":{"interface":"eth0","direction":"tx"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 6.2 packet_count（包收发数）

- **数据来源**：`/proc/net/dev`
- **采集方法**：读取各网卡的 `packets` 字段（接收包第2列，发送包第10列），两次差值除以间隔时间得出每秒包数
- **Labels**：`interface`（"eth0", ...）、`direction`（"rx", "tx"）
- **输出示例**：
```json
{"component":"network","name":"packet_count","value":1523,"unit":"个/s","labels":{"interface":"eth0","direction":"rx"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 6.3 error_count（错误包计数）

- **数据来源**：`/proc/net/dev`
- **采集方法**：读取各网卡的 `errs` 和 `drop` 字段（接收错误第3列、接收丢弃第5列、发送错误第11列、发送丢弃第13列），累计错误计数
- **Labels**：`interface`（"eth0", ...）、`type`（"rx_err", "rx_drop", "tx_err", "tx_drop"）
- **输出示例**：
```json
{"component":"network","name":"error_count","value":0,"unit":"次","labels":{"interface":"eth0","type":"rx_err"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 6.4 interface_status（网卡接口状态）

- **数据来源**：`/sys/class/net/*/operstate`
- **采集方法**：遍历 `/sys/class/net/` 下所有网卡目录，读取 `operstate` 文件获取接口状态（up/down）
- **Labels**：`interface`（"eth0", "ens33", ...）
- **输出示例**：
```json
{"component":"network","name":"interface_status","value":1,"unit":"","labels":{"interface":"eth0","status":"up"},"timestamp":"2026-07-10T10:30:00Z"}
```

> 状态值映射：up=1, down=0

#### 6.5 connection_count（网络连接数）

- **数据来源**：`/proc/net/tcp` 和 `/proc/net/tcp6`
- **采集方法**：解析 TCP 连接表，按状态码统计各状态连接数。状态码映射：01=ESTABLISHED, 02=SYN_SENT, 03=SYN_RECV, 04=FIN_WAIT1, 05=FIN_WAIT2, 06=TIME_WAIT, 07=CLOSE, 08=CLOSE_WAIT, 09=LAST_ACK, 0A=LISTEN, 0B=CLOSING
- **Labels**：`state`（"ESTABLISHED", "TIME_WAIT", "LISTEN", ...）
- **输出示例**：
```json
{"component":"network","name":"connection_count","value":152,"unit":"个","labels":{"state":"ESTABLISHED"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"network","name":"connection_count","value":34,"unit":"个","labels":{"state":"TIME_WAIT"},"timestamp":"2026-07-10T10:30:00Z"}
```

---

## 附录：各采集器数据来源汇总

| 采集器 | 数据来源 | 依赖外部命令 |
|--------|----------|-------------|
| CPU | /proc/stat, /proc/loadavg, /proc/cpuinfo, /proc/buddyinfo, /sys/devices/system/cpu/(cpufreq,cache,online,offline,isolated), /sys/devices/system/node, lscpu, ipmitool, dmesg//var/log/mcelog | lscpu, ipmitool, dmesg(mce) |
| Memory | /proc/meminfo, /proc/vmstat, /proc/pressure/memory, /proc/buddyinfo, /sys/devices/system/edac/mc, dmidecode, ipmitool(SDR), dmesg | dmidecode, ipmitool, dmesg(oom) |
| Disk | /proc/mounts, statfs syscall, /proc/diskstats, /proc/stat | smartctl (Phase 3) |
| GPU | nvidia-smi 命令输出 | nvidia-smi |
| NPU | npu-smi 命令输出 | npu-smi |
| Network | /proc/net/dev, /sys/class/net/, /proc/net/tcp, /proc/net/tcp6 | 无 |

---

## 附录B：已实现采集指标清单

> 以下 83 个指标均已实现并通过测试，按部件分类汇总。其中 CPU 扩展至 40、Memory 扩展至 19 个指标，且 cpu/memory/disk/network 采集器已接入来源层(source layer)（v0.2.0）。

### CPU（40 个）

| 序号 | 指标名称 | 中文名称 | 优先级 | 单位 |
|------|----------|----------|--------|------|
| 1 | usage | CPU使用率 | High | % |
| 2 | load_average | 系统负载 | High | - |
| 3 | temperature | CPU温度 | Medium | °C |
| 4 | frequency | CPU频率 | Medium | MHz |
| 5 | context_switches | 上下文切换次数 | Low | 次/s |
| 6 | process_count | 运行进程数 | Low | 个 |
| 7 | model_info | CPU型号信息 | Low | - |
| 8 | user_time | 用户态运行时间 | Low | jiffies |
| 9 | nice_time | 低优先级用户进程时间 | Low | jiffies |
| 10 | system_time | 内核态运行时间 | Low | jiffies |
| 11 | idle_time | 空闲时间 | Low | jiffies |
| 12 | iowait_time | 等待IO时间 | Low | jiffies |
| 13 | irq_time | 硬中断处理时间 | Low | jiffies |
| 14 | softirq_time | 软中断处理时间 | Low | jiffies |
| 15 | steal_time | 被窃取时间 | Low | jiffies |
| 16 | user_util | 用户态平均利用率 | Medium | % |
| 17 | system_util | 内核态平均利用率 | Medium | % |
| 18 | idle_util | 空闲状态占用率 | Medium | % |
| 19 | iowait_util | IO等待状态占用率 | Medium | % |
| 20 | numa_node_num | NUMA节点数量 | Low | 个 |
| 21 | online_core_num | 核在线数量 | Medium | 个 |
| 22 | offline_core_num | 核离线数量 | Medium | 个 |
| 23 | isolated_core_num | 核隔离数量 | Medium | 个 |
| 24 | mem_temperature | CPU内存区域温度 | Medium | °C |
| 25 | core_num | CPU核数量 | Low | 个 |
| 26 | die_core_num | 单个die核数量 | Low | 个 |
| 27 | numa_core_num | NUMA核数量 | Low | 个 |
| 28 | cpu_num | CPU个数 | Low | 个 |
| 29 | avg_freq | CPU平均频率 | Medium | MHz |
| 30 | min_freq | CPU最小频率 | Low | MHz |
| 31 | max_freq | CPU最大频率 | Low | MHz |
| 32 | cpu_ce_errors | CPU CE错误数量 | High | 次 |
| 33 | cpu_uce_errors | CPU UCE错误数量 | High | 次 |
| 34 | power | CPU功率 | Medium | W |
| 35 | l1d_cache_size | L1d缓存大小 | Low | KB |
| 36 | l1i_cache_size | L1i缓存大小 | Low | KB |
| 37 | l2_cache_size | L2缓存大小 | Low | KB |
| 38 | l3_cache_size | L3缓存大小 | Low | KB |
| 39 | numa_order_num | NUMA节点buddy order数量 | Low | 个 |
| 40 | numa_info | NUMA节点内存碎片信息 | Low | order |

### Memory（19 个）

| 序号 | 指标名称 | 中文名称 | 优先级 | 单位 |
|------|----------|----------|--------|------|
| 1 | usage | 内存使用率 | High | % |
| 2 | swap_usage | Swap使用率 | High | % |
| 3 | swap_detail | Swap原始值 | Medium | MB |
| 4 | swap_in | 换入次数 | Medium | 次/s |
| 5 | swap_out | 换出次数 | Medium | 次/s |
| 6 | saturation | 内存饱和度 | Medium | % |
| 7 | fragmentation | 内存碎片化程度 | Medium | % |
| 8 | ecc_ce_errors | CE可纠正错误数 | High | 次 |
| 9 | ecc_uce_errors | UCE不可纠正错误数 | High | 次 |
| 10 | oom_count | OOM触发次数 | Medium | 次 |
| 11 | page_faults | 缺页错误次数 | Low | 次/s |
| 12 | isolated_pages | 隔离页总数 | Low | 个 |
| 13 | isolated_anon_pages | 隔离匿名页数 | Low | 个 |
| 14 | isolated_file_pages | 隔离文件页数 | Low | 个 |
| 15 | free_pages | 空闲页数 | Low | 个 |
| 16 | module_num | 内存条数量 | Low | 个 |
| 17 | module_size | 内存条大小 | Low | MB |
| 18 | module_info | 内存条静态信息 | Low | - |
| 19 | power | 内存功率 | Medium | W |

### Disk（7 个）

| 序号 | 指标名称 | 中文名称 | 优先级 | 单位 |
|------|----------|----------|--------|------|
| 1 | space_usage | 磁盘空间使用率 | High | % |
| 2 | iops | 读写IOPS | Medium | 次/s |
| 3 | throughput | 读写吞吐量 | Medium | MB/s |
| 4 | io_wait | I/O等待占比 | Medium | % |
| 5 | smart_status | SMART健康状态 | Medium | - |
| 6 | smart_temperature | 硬盘温度 | Low | °C |
| 7 | io_errors | I/O错误计数 | Low | 次 |

### GPU（7 个）

| 序号 | 指标名称 | 中文名称 | 优先级 | 单位 |
|------|----------|----------|--------|------|
| 1 | utilization | GPU使用率 | High | % |
| 2 | memory_usage | 显存使用率 | High | % |
| 3 | temperature | GPU温度 | High | °C |
| 4 | power_draw | GPU功耗 | Medium | W |
| 5 | fan_speed | 风扇转速 | Medium | % |
| 6 | ecc_errors | ECC错误数 | Medium | 次 |
| 7 | clock_frequency | 时钟频率 | Low | MHz |

### NPU（5 个）

| 序号 | 指标名称 | 中文名称 | 优先级 | 单位 |
|------|----------|----------|--------|------|
| 1 | utilization | NPU使用率 | High | % |
| 2 | memory_usage | NPU显存使用率 | High | % |
| 3 | temperature | NPU温度 | High | °C |
| 4 | power_draw | NPU功耗 | Medium | W |
| 5 | health_status | NPU健康状态 | Medium | - |

### Network（5 个）

| 序号 | 指标名称 | 中文名称 | 优先级 | 单位 |
|------|----------|----------|--------|------|
| 1 | throughput | 网络吞吐量 | High | bytes/s |
| 2 | packet_count | 包收发数 | Medium | 个/s |
| 3 | error_count | 错误包计数 | Medium | 次 |
| 4 | interface_status | 网卡接口状态 | Medium | - |
| 5 | connection_count | 网络连接数 | Low | 个 |

### 统计汇总

| 部件 | 指标数 | High | Medium | Low |
|------|--------|------|--------|-----|
| CPU | 40 | 4 | 12 | 24 |
| Memory | 19 | 4 | 7 | 8 |
| Disk | 7 | 1 | 3 | 3 |
| GPU | 7 | 3 | 3 | 1 |
| NPU | 5 | 3 | 2 | 0 |
| Network | 5 | 1 | 3 | 1 |
| **合计** | **83** | **16** | **30** | **37** |
