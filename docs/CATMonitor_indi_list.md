# CATMonitor 采集指标清单

> 本文档列出 CATMonitor 支持的全部服务器运行指标。
> 每个指标包含：优先级、默认采集周期、默认是否采集、数据来源、采集方法、输出示例。
>
> **版本**: v0.5.3 ｜ **更新日期**: 2026-07-21 ｜ **指标总数**: 204（High 24 / Medium 121 / Low 59）
> **来源层**: 全部 7 个采集器（cpu/memory/disk/network/gpu/npu/chassis）已接入 `internal/source/` 来源层。
> **指标采集目录**：`internal/metrics` + `configs/metrics.yaml`（默认目录）+ 模块自有 `metrics.yaml` 覆盖。
> **新增模块**：`features/dfee`（能效监控 25 张实时图表）+ `features/exporter`（Prometheus 导出 :9100/metrics）。

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
| Disk | 9 | 1 | 5 | 3 |
| GPU | 7 | 3 | 3 | 1 |
| NPU | 119 | 9 | 88 | 22 |
| Network | 5 | 1 | 3 | 1 |
| Chassis | 5 | 2 | 3 | 0 |
| **合计** | **204** | **24** | **121** | **59** |

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
| 3.4 | read_latency | 读耗时 | Medium | 5s | 是 | ms/s | /proc/diskstats (field 7) |
| 3.5 | write_latency | 写耗时 | Medium | 5s | 是 | ms/s | /proc/diskstats (field 11) |
| 3.6 | io_wait | I/O等待占比 | Medium | 5s | 是 | % | /proc/stat |
| 3.7 | smart_status | SMART健康状态 | Medium | 60s | 否 | - | smartctl -H |
| 3.8 | smart_temperature | 硬盘温度 | Low | 60s | 否 | °C | smartctl -A |
| 3.9 | io_errors | I/O错误计数 | Low | 30s | 否 | 次 | /proc/diskstats, dmesg |

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

#### 3.4 read_latency（读耗时）

- **数据来源**：`/proc/diskstats` 第 7 字段（read time, ms）
- **采集方法**：两次采集间 read time 累计值差值除以间隔时间，得每秒读耗时（ms/s）。反映磁盘读 I/O 花费的时间。需 prev 快照，首次不产出
- **Labels**：`device`（"sda", "sdb", ...）
- **输出示例**：
```json
{"component":"disk","name":"read_latency","value":120.5,"unit":"ms/s","labels":{"device":"sda"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 3.5 write_latency（写耗时）

- **数据来源**：`/proc/diskstats` 第 11 字段（write time, ms）
- **采集方法**：两次采集间 write time 累计值差值除以间隔时间，得每秒写耗时（ms/s）。反映磁盘写 I/O 花费的时间。需 prev 快照，首次不产出
- **Labels**：`device`（"sda", "sdb", ...）
- **输出示例**：
```json
{"component":"disk","name":"write_latency","value":80.3,"unit":"ms/s","labels":{"device":"sda"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 3.6 io_wait（I/O等待占比）

- **数据来源**：`/proc/stat` 中 `cpu` 行的 `iowait` 字段
- **采集方法**：读取 `cpu` 行的第5个字段（iowait），与上一次采集的差值除以总 CPU 时间差值，得出 I/O Wait 百分比
- **Labels**：无
- **输出示例**：
```json
{"component":"disk","name":"io_wait","value":3.2,"unit":"%","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 3.7 smart_status（SMART健康状态）

- **数据来源**：`smartctl -H /dev/sdX` 命令输出
- **采集方法**：遍历块设备，对每个支持 SMART 的设备执行 `smartctl -H`，解析输出中的整体健康评估结果（PASSED/FAILED）。需要 smartmontools 已安装且有 root 权限
- **Labels**：`device`（"sda", "sdb"）
- **输出示例**：
```json
{"component":"disk","name":"smart_status","value":1,"unit":"","labels":{"device":"sda","status":"PASSED"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 3.8 smart_temperature（硬盘温度）

- **数据来源**：`smartctl -A /dev/sdX` 命令输出
- **采集方法**：执行 `smartctl -A`，解析 SMART 属性表中的 `Temperature_Celsius` 或 `Temperature` 属性获取温度值
- **Labels**：`device`（"sda", "sdb"）
- **输出示例**：
```json
{"component":"disk","name":"smart_temperature","value":35,"unit":"°C","labels":{"device":"sda"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 3.9 io_errors（I/O错误计数）

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

NPU 采集器通过三类数据源获取华为昇腾 NPU 运行状态，且采用 **device 并行采集**（每块 NPU 一个 goroutine）：
- **DCMI API**（CGo）：华为 CANN 的 C 库 `libdcmi.so`，通过 `dcmi_*` 函数调用。覆盖绝大多数指标。需 `-tags dcmi` 构建，无 CANN 环境时优雅降级。
- **npu-smi 命令**：`npu-smi info -t <type>` 子命令（无 CGo）。覆盖通信拓扑、HCCS 带宽。
- **hccn_tool 命令**：`hccn_tool -i <id> -<opt> -g`（无 CGo）。覆盖网口/PCIe 带宽、RoCE 速度/链路。

> 注：所有 NPU 指标均为 Linux 专属；无 NPU 硬件时采集器整体跳过。DCMI 原始单位（mV/V、毫摄氏度/°C 等）待真机实测。

| 序号 | 指标名称 | 中文名称 | 优先级 | 默认周期 | 默认采集 | 单位 | 数据来源 |
|------|----------|----------|--------|----------|----------|------|----------|
| 5.1 | utilization | NPU使用率(AICore) | High | 3s | 是 | % | DCMI dcmi_get_device_utilization_rate(AICORE) |
| 5.2 | memory_usage | NPU显存使用率(HBM) | High | 3s | 是 | % | DCMI dcmi_get_device_hbm_info |
| 5.3 | temperature | NPU温度 | High | 3s | 是 | °C | DCMI dcmi_get_device_temperature |
| 5.4 | power_draw | NPU功耗 | Medium | 10s | 是 | W | DCMI dcmi_get_device_power_info |
| 5.5 | health_status | NPU健康状态 | Medium | 10s | 是 | - | DCMI dcmi_get_device_health |
| 5.6 | npu_num | NPU设备数量 | Low | 启动时1次 | 是 | 个 | DCMI dcmi_get_card_list |
| 5.7 | chip_type | NPU芯片类型 | Low | 启动时1次 | 是 | - | DCMI dcmi_get_device_chip_info |
| 5.8 | driver_version | NPU驱动版本 | Low | 启动时1次 | 是 | - | DCMI dcmi_get_driver_version |
| 5.9 | driver_health | NPU驱动健康状态 | Medium | 10s | 是 | - | DCMI dcmi_get_driver_health |
| 5.10 | error_code | NPU错误码 | Medium | 10s | 是 | - | DCMI dcmi_get_device_errorcode_v2 |
| 5.11 | process_info | NPU进程PID信息 | Low | 30s | 否 | - | DCMI dcmi_get_device_resource_info |
| 5.12 | process_total | NPU进程总数量 | Low | 30s | 否 | 个 | DCMI dcmi_get_device_resource_info |
| 5.13 | comm_topo | NPU通信拓扑 | Low | 启动时1次 | 是 | - | npu-smi info -t topo |
| 5.14 | voltage | NPU电压 | Medium | 10s | 是 | V | DCMI dcmi_get_device_voltage |
| 5.15 | aicore_voltage | NPU AICore电压 | Medium | 10s | 是 | V | DCMI dcmi_get_device_info(LP/AICORE_VOLTAGE) |
| 5.16 | hybrid_voltage | NPU Hybrid电压 | Medium | 10s | 是 | V | DCMI dcmi_get_device_info(LP/HYBIRD_VOLTAGE) |
| 5.17 | cpu_voltage | NPU CPU电压 | Medium | 10s | 是 | V | DCMI dcmi_get_device_info(LP/TAISHAN_VOLTAGE) |
| 5.18 | ddr_voltage | NPU DDR电压 | Medium | 10s | 是 | V | DCMI dcmi_get_device_info(LP/DDR_VOLTAGE) |
| 5.19 | acg_count | NPU ACG调频计数 | Low | 60s | 否 | 次 | DCMI dcmi_get_device_info(LP/ACG) |
| 5.20 | fan_speed | NPU风扇转速 | Medium | 10s | 是 | % | DCMI dcmi_get_device_fan_count+speed |
| 5.21 | hbm_temp | NPU HBM温度 | Medium | 10s | 是 | °C | DCMI dcmi_get_device_hbm_info.temp |
| 5.22 | cluster_temp | NPU Cluster温度 | Medium | 10s | 是 | °C | DCMI dcmi_get_device_sensor_info(CLUSTER) |
| 5.23 | peri_temp | NPU Peri温度 | Medium | 10s | 是 | °C | DCMI dcmi_get_device_sensor_info(PERI) |
| 5.24 | aicore0_temp | NPU AICORE0温度 | Medium | 10s | 是 | °C | DCMI dcmi_get_device_sensor_info(AICORE0) |
| 5.25 | aicore1_temp | NPU AICORE1温度 | Medium | 10s | 是 | °C | DCMI dcmi_get_device_sensor_info(AICORE1) |
| 5.26 | ntc1_temp | NPU热敏电阻1温度 | Low | 30s | 否 | °C | DCMI dcmi_get_device_sensor_info(NTC).ntc_tmp[0] |
| 5.27 | ntc2_temp | NPU热敏电阻2温度 | Low | 30s | 否 | °C | DCMI dcmi_get_device_sensor_info(NTC).ntc_tmp[1] |
| 5.28 | ntc3_temp | NPU热敏电阻3温度 | Low | 30s | 否 | °C | DCMI dcmi_get_device_sensor_info(NTC).ntc_tmp[2] |
| 5.29 | ntc4_temp | NPU热敏电阻4温度 | Low | 30s | 否 | °C | DCMI dcmi_get_device_sensor_info(NTC).ntc_tmp[3] |
| 5.30 | soc_max_temp | NPU SOC最高温度 | Medium | 10s | 是 | °C | DCMI dcmi_get_device_sensor_info(SOC) |
| 5.31 | fp_max_temp | NPU光模块最高温度 | Medium | 10s | 是 | °C | DCMI dcmi_get_device_sensor_info(FP) |
| 5.32 | ndie_temp | NPU NDie温度 | Medium | 10s | 是 | °C | DCMI dcmi_get_device_sensor_info(N_DIE) |
| 5.33 | hbm_max_temp | NPU HBM最高温度 | Medium | 10s | 是 | °C | DCMI dcmi_get_device_sensor_info(HBM) |
| 5.34 | aicpu_freq | NPU AICPU频率 | Medium | 10s | 是 | MHz | DCMI dcmi_get_aicpu_info |
| 5.35 | aicore_rated_freq | NPU AICore额定频率 | Low | 启动时1次 | 是 | MHz | DCMI dcmi_get_device_frequency(AICORE_MAX) |
| 5.36 | aicore_freq | NPU AICore频率 | Medium | 10s | 是 | MHz | DCMI dcmi_get_device_frequency(AICORE_CURRENT) |
| 5.37 | ctrlcpu_freq | NPU CTRLCPU频率 | Medium | 10s | 是 | MHz | DCMI dcmi_get_device_frequency(CTRLCPU) |
| 5.38 | vector_core_freq | NPU Vector Core频率 | Medium | 10s | 是 | MHz | DCMI dcmi_get_device_frequency(VECTORCORE) |
| 5.39 | hbm_freq | NPU HBM频率 | Medium | 10s | 是 | MHz | DCMI dcmi_get_device_frequency(HBM) |
| 5.40 | ddr_freq | NPU DDR频率 | Medium | 10s | 是 | MHz | DCMI dcmi_get_device_frequency(DDR) |
| 5.41 | npu_util | NPU整体利用率 | High | 3s | 是 | % | DCMI dcmi_get_device_utilization_rate(NPU) |
| 5.42 | aicpu_util | NPU AICPU利用率 | Medium | 10s | 是 | % | DCMI dcmi_get_device_utilization_rate(AICPU) |
| 5.43 | ctrlcpu_util | NPU CTRLCPU利用率 | Medium | 10s | 是 | % | DCMI dcmi_get_device_utilization_rate(CTRLCPU) |
| 5.44 | vector_core_util | NPU Vector Core利用率 | Medium | 10s | 是 | % | DCMI dcmi_get_device_utilization_rate(VECTORCORE) |
| 5.45 | hbm_bandwidth_util | NPU HBM带宽利用率 | Medium | 10s | 是 | % | DCMI dcmi_get_device_utilization_rate(HBM_BANDWIDTH) |
| 5.46 | ddr_util | NPU DDR利用率 | Medium | 10s | 是 | % | DCMI dcmi_get_device_utilization_rate(DDR) |
| 5.47 | ddr_bandwidth_util | NPU DDR带宽利用率 | Medium | 10s | 是 | % | DCMI dcmi_get_device_utilization_rate(DDR_BANDWIDTH) |
| 5.48 | vdec_util | NPU VDEC利用率 | Low | 30s | 否 | % | DCMI dcmi_get_device_dvpp_ratio_info |
| 5.49 | vpc_util | NPU VPC利用率 | Low | 30s | 否 | % | DCMI dcmi_get_device_dvpp_ratio_info |
| 5.50 | venc_util | NPU VENC利用率 | Low | 30s | 否 | % | DCMI dcmi_get_device_dvpp_ratio_info |
| 5.51 | jpege_util | NPU JPEGE利用率 | Low | 30s | 否 | % | DCMI dcmi_get_device_dvpp_ratio_info |
| 5.52 | jpegd_util | NPU JPEGD利用率 | Low | 30s | 否 | % | DCMI dcmi_get_device_dvpp_ratio_info |
| 5.53 | hbm_total_memory | NPU HBM总容量 | Low | 启动时1次 | 是 | MB | DCMI dcmi_get_device_hbm_info.memory_size |
| 5.54 | hbm_used_memory | NPU HBM已用容量 | High | 3s | 是 | MB | DCMI dcmi_get_device_hbm_info.memory_usage |
| 5.55 | hbm_single_ecc | NPU HBM单bit错误 | High | 30s | 是 | 次 | DCMI dcmi_get_device_ecc_info(HBM).single_bit_error_cnt |
| 5.56 | hbm_double_ecc | NPU HBM多bit错误 | High | 30s | 是 | 次 | DCMI dcmi_get_device_ecc_info(HBM).double_bit_error_cnt |
| 5.57 | hbm_single_ecc_isolated | NPU HBM单bit隔离页数 | Medium | 30s | 是 | 个 | DCMI dcmi_get_device_ecc_info(HBM).single_bit_isolated_pages_cnt |
| 5.58 | hbm_double_ecc_isolated | NPU HBM多bit隔离页数 | Medium | 30s | 是 | 个 | DCMI dcmi_get_device_ecc_info(HBM).double_bit_isolated_pages_cnt |
| 5.59 | ddr_single_ecc | NPU DDR单bit错误 | High | 30s | 是 | 次 | DCMI dcmi_get_device_ecc_info(DDR).single_bit_error_cnt |
| 5.60 | ddr_double_ecc | NPU DDR多bit错误 | High | 30s | 是 | 次 | DCMI dcmi_get_device_ecc_info(DDR).double_bit_error_cnt |
| 5.61 | ddr_single_ecc_isolated | NPU DDR单bit隔离页数 | Medium | 30s | 是 | 个 | DCMI dcmi_get_device_ecc_info(DDR).single_bit_isolated_pages_cnt |
| 5.62 | ddr_double_ecc_isolated | NPU DDR多bit隔离页数 | Medium | 30s | 是 | 个 | DCMI dcmi_get_device_ecc_info(DDR).double_bit_isolated_pages_cnt |
| 5.63 | llc_write_hit_rate | NPU LLC写命中率 | Low | 30s | 否 | % | DCMI dcmi_get_device_llc_perf_para.wr_hit_rate |
| 5.64 | llc_read_hit_rate | NPU LLC读命中率 | Low | 30s | 否 | % | DCMI dcmi_get_device_llc_perf_para.rd_hit_rate |
| 5.65 | llc_throughput | NPU LLC吞吐量 | Low | 30s | 否 | MB/s | DCMI dcmi_get_device_llc_perf_para.throughput |
| 5.66 | net_tx_bandwidth | NPU网口发送带宽 | Medium | 5s | 是 | MB/s | hccn_tool -i N -bandwidth -g (TX) |
| 5.67 | net_rx_bandwidth | NPU网口接收带宽 | Medium | 5s | 是 | MB/s | hccn_tool -i N -bandwidth -g (RX) |
| 5.68 | roce_link_status | NPU RoCE连接状态 | Medium | 10s | 是 | - | DCMI dcmi_get_device_network_health |
| 5.69 | roce_speed_status | NPU RoCE连接速度 | Medium | 10s | 是 | - | hccn_tool -i N -speed -g |
| 5.70 | roce_link_health | NPU RoCE Link状态 | Medium | 10s | 是 | - | hccn_tool -i N -link -g |
| 5.71 | pcie_tx_bandwidth | NPU PCIe发送带宽 | Medium | 5s | 是 | MB/s | hccn_tool -i N -bandwidth -g (PCIe TX) |
| 5.72 | pcie_rx_bandwidth | NPU PCIe接收带宽 | Medium | 5s | 是 | MB/s | hccn_tool -i N -bandwidth -g (PCIe RX) |
| 5.73 | hccs_tx_bandwidth | NPU HCCS发送带宽 | Medium | 5s | 是 | MB/s | npu-smi info -t hccs-bw -i N -c 0 -time 50 |
| 5.74 | hccs_rx_bandwidth | NPU HCCS接收带宽 | Medium | 5s | 是 | MB/s | npu-smi info -t hccs-bw -i N -c 0 -time 50 |

### 采集方法

DCMI 类指标通过 CGo 调用 `libdcmi.so` 的 `dcmi_*` 函数，按 `(card_id, device_id)` 逐 NPU 查询，采用 device 并行采集（每 device 一个 goroutine）。关键 C 结构（来自 `dcmi_interface_api.h`）：`dcmi_hbm_info`、`dcmi_ecc_info`、`dcmi_llc_perf`、`dcmi_dvpp_ratio`、`dcmi_aicpu_info`。npu-smi 子命令：`-t topo`、`-t hccs-bw`。hccn_tool：`-bandwidth`、`-speed`、`-link`。

### 指标详情

#### 5.1 utilization（NPU使用率 AICore）

- **数据来源**：DCMI `dcmi_get_device_utilization_rate(card, dev, DCMI_UTILIZATION_RATE_AICORE, &rate)`
- **采集方法**：CGo 调 DCMI 取 AICore 利用率（0-100）。逐 NPU 采集
- **Labels**：`npu_id`（"0","1",...）
- **输出示例**：
```json
{"component":"npu","name":"utilization","value":45,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.2 memory_usage（NPU显存使用率 HBM）

- **数据来源**：DCMI `dcmi_get_device_hbm_info(card, dev, &hbm_info)`
- **采集方法**：取 `hbm_info.memory_usage / hbm_info.memory_size × 100`（HBM 显存使用率 %）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"memory_usage","value":32.5,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.3 temperature（NPU温度）

- **数据来源**：DCMI `dcmi_get_device_temperature(card, dev, &temp)`
- **采集方法**：取设备温度（°C，原始单位待实测）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"temperature","value":42,"unit":"°C","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.4 power_draw（NPU功耗）

- **数据来源**：DCMI `dcmi_get_device_power_info(card, dev, &power)`
- **采集方法**：取设备功耗（W）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"power_draw","value":65.0,"unit":"W","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.5 health_status（NPU健康状态）

- **数据来源**：DCMI `dcmi_get_device_health(card, dev, &health)`
- **采集方法**：取设备健康状态码。映射：OK=1, Warning=2, Alarm=3, Critical=4
- **Labels**：`npu_id`、`status`（"OK"/"Warning"/"Alarm"/"Critical"）
- **输出示例**：
```json
{"component":"npu","name":"health_status","value":1,"unit":"","labels":{"npu_id":"0","status":"OK"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.6 npu_num（NPU设备数量）

- **数据来源**：DCMI `dcmi_get_card_list(...)`
- **采集方法**：统计 NPU 设备数。启动时采集一次
- **Labels**：无
- **输出示例**：
```json
{"component":"npu","name":"npu_num","value":8,"unit":"个","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.7 chip_type（NPU芯片类型）

- **数据来源**：DCMI `dcmi_get_device_chip_info(card, dev, ...)`
- **采集方法**：取芯片型号字符串（如 Ascend910A）。字符串值放 `labels.chip_type`，`value` 填 0（对齐 CPU `model_info` 惯例）
- **Labels**：`npu_id`、`chip_type`
- **输出示例**：
```json
{"component":"npu","name":"chip_type","value":0,"unit":"","labels":{"npu_id":"0","chip_type":"Ascend910A"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.8 driver_version（NPU驱动版本）

- **数据来源**：DCMI `dcmi_get_driver_version(...)`
- **采集方法**：取驱动版本字符串，放 `labels.driver_version`，`value` 填 0
- **Labels**：`driver_version`
- **输出示例**：
```json
{"component":"npu","name":"driver_version","value":0,"unit":"","labels":{"driver_version":"23.0.0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.9 driver_health（NPU驱动健康状态）

- **数据来源**：DCMI `dcmi_get_driver_health(card, dev, &health)`
- **采集方法**：取驱动健康状态码，0=正常 非0=异常
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"driver_health","value":0,"unit":"","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.10 error_code（NPU错误码）

- **数据来源**：DCMI `dcmi_get_device_errorcode_v2(card, dev, &code)`
- **采集方法**：取设备级错误码（0=无错误）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"error_code","value":0,"unit":"","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.11 process_info（NPU进程PID信息）

- **数据来源**：DCMI `dcmi_get_device_resource_info(card, dev, ...)`
- **采集方法**：取占用 NPU 的进程 PID 列表，序列化放 `labels.process_pids`（如 "1234,5678"），`value` 填进程总数
- **Labels**：`npu_id`、`process_pids`
- **输出示例**：
```json
{"component":"npu","name":"process_info","value":3,"unit":"个","labels":{"npu_id":"0","process_pids":"1234,5678,9012"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.12 process_total（NPU进程总数量）

- **数据来源**：DCMI `dcmi_get_device_resource_info(card, dev, ...)`
- **采集方法**：取占用 NPU 的进程总数
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"process_total","value":3,"unit":"个","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.13 comm_topo（NPU通信拓扑）

- **数据来源**：`npu-smi info -t topo`
- **采集方法**：解析拓扑字符串，放 `labels.topo`，`value` 填 0。启动时采集一次
- **Labels**：`topo`
- **输出示例**：
```json
{"component":"npu","name":"comm_topo","value":0,"unit":"","labels":{"topo":"8*HCCS-Link"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.14 voltage（NPU电压）

- **数据来源**：DCMI `dcmi_get_device_voltage(card, dev, &voltage)`
- **采集方法**：取设备主电压（V，原始单位待实测）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"voltage","value":0.8,"unit":"V","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.15 aicore_voltage（NPU AICore电压）

- **数据来源**：DCMI `dcmi_get_device_info(card, dev, DCMI_MAIN_CMD_LP, DCMI_LP_SUB_CMD_AICORE_VOLTAGE_CURRENT, ...)`
- **采集方法**：取 AICore 当前电压（V）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"aicore_voltage","value":0.8,"unit":"V","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.16 hybrid_voltage（NPU Hybrid电压）

- **数据来源**：DCMI `dcmi_get_device_info(card, dev, DCMI_MAIN_CMD_LP, DCMI_LP_SUB_CMD_HYBIRD_VOLTAGE_CURRENT, ...)`
- **采集方法**：取 Hybrid 当前电压（V）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"hybrid_voltage","value":0.7,"unit":"V","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.17 cpu_voltage（NPU CPU电压）

- **数据来源**：DCMI `dcmi_get_device_info(card, dev, DCMI_MAIN_CMD_LP, DCMI_LP_SUB_CMD_TAISHAN_VOLTAGE_CURRENT, ...)`
- **采集方法**：取 CPU（泰山）当前电压（V）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"cpu_voltage","value":1.0,"unit":"V","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.18 ddr_voltage（NPU DDR电压）

- **数据来源**：DCMI `dcmi_get_device_info(card, dev, DCMI_MAIN_CMD_LP, DCMI_LP_SUB_CMD_DDR_VOLTAGE_CURRENT, ...)`
- **采集方法**：取 DDR 当前电压（V）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"ddr_voltage","value":1.2,"unit":"V","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.19 acg_count（NPU ACG调频计数）

- **数据来源**：DCMI `dcmi_get_device_info(card, dev, DCMI_MAIN_CMD_LP, DCMI_LP_SUB_CMD_ACG, ...)`
- **采集方法**：取 ACG 调频累计计数（次）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"acg_count","value":1234,"unit":"次","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.20 fan_speed（NPU风扇转速）

- **数据来源**：DCMI `dcmi_get_device_fan_count(card, dev, &count)` + `dcmi_get_device_fan_speed(card, dev, fan_id, &speed)`
- **采集方法**：遍历每风扇，取转速占最大转速百分比（%）
- **Labels**：`npu_id`、`fan`（"0","1",...）
- **输出示例**：
```json
{"component":"npu","name":"fan_speed","value":65,"unit":"%","labels":{"npu_id":"0","fan":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.21 hbm_temp（NPU HBM温度）

- **数据来源**：DCMI `dcmi_get_device_hbm_info(card, dev, &hbm_info)` → `hbm_info.temp`
- **采集方法**：取 HBM 温度（°C）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"hbm_temp","value":55,"unit":"°C","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.22 cluster_temp（NPU Cluster温度）

- **数据来源**：DCMI `dcmi_get_device_sensor_info(card, dev, DCMI_CLUSTER_TEMP_ID, ...)`
- **采集方法**：取 Cluster 温度（°C）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"cluster_temp","value":60,"unit":"°C","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.23 peri_temp（NPU Peri温度）

- **数据来源**：DCMI `dcmi_get_device_sensor_info(card, dev, DCMI_PERI_TEMP_ID, ...)`
- **采集方法**：取 Peri（外设区）温度（°C）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"peri_temp","value":58,"unit":"°C","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.24 aicore0_temp（NPU AICORE0温度）

- **数据来源**：DCMI `dcmi_get_device_sensor_info(card, dev, DCMI_AICORE0_TEMP_ID, ...)`
- **采集方法**：取 AICORE0 温度（°C）
- **Labels**：`npu_id`、`aicore`（"0"）
- **输出示例**：
```json
{"component":"npu","name":"aicore0_temp","value":62,"unit":"°C","labels":{"npu_id":"0","aicore":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.25 aicore1_temp（NPU AICORE1温度）

- **数据来源**：DCMI `dcmi_get_device_sensor_info(card, dev, DCMI_AICORE1_TEMP_ID, ...)`
- **采集方法**：取 AICORE1 温度（°C）
- **Labels**：`npu_id`、`aicore`（"1"）
- **输出示例**：
```json
{"component":"npu","name":"aicore1_temp","value":61,"unit":"°C","labels":{"npu_id":"0","aicore":"1"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.26 ntc1_temp（NPU热敏电阻1温度）

- **数据来源**：DCMI `dcmi_get_device_sensor_info(card, dev, DCMI_NTC_TEMP_ID, &ntc)` → `ntc.ntc_tmp[0]`
- **采集方法**：取热敏电阻 1 温度（°C）
- **Labels**：`npu_id`、`ntc`（"1"）
- **输出示例**：
```json
{"component":"npu","name":"ntc1_temp","value":45,"unit":"°C","labels":{"npu_id":"0","ntc":"1"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.27 ntc2_temp（NPU热敏电阻2温度）

- **数据来源**：DCMI `dcmi_get_device_sensor_info(card, dev, DCMI_NTC_TEMP_ID, &ntc)` → `ntc.ntc_tmp[1]`
- **采集方法**：取热敏电阻 2 温度（°C）
- **Labels**：`npu_id`、`ntc`（"2"）
- **输出示例**：
```json
{"component":"npu","name":"ntc2_temp","value":44,"unit":"°C","labels":{"npu_id":"0","ntc":"2"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.28 ntc3_temp（NPU热敏电阻3温度）

- **数据来源**：DCMI `dcmi_get_device_sensor_info(card, dev, DCMI_NTC_TEMP_ID, &ntc)` → `ntc.ntc_tmp[2]`
- **采集方法**：取热敏电阻 3 温度（°C）
- **Labels**：`npu_id`、`ntc`（"3"）
- **输出示例**：
```json
{"component":"npu","name":"ntc3_temp","value":43,"unit":"°C","labels":{"npu_id":"0","ntc":"3"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.29 ntc4_temp（NPU热敏电阻4温度）

- **数据来源**：DCMI `dcmi_get_device_sensor_info(card, dev, DCMI_NTC_TEMP_ID, &ntc)` → `ntc.ntc_tmp[3]`
- **采集方法**：取热敏电阻 4 温度（°C）
- **Labels**：`npu_id`、`ntc`（"4"）
- **输出示例**：
```json
{"component":"npu","name":"ntc4_temp","value":42,"unit":"°C","labels":{"npu_id":"0","ntc":"4"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.30 soc_max_temp（NPU SOC最高温度）

- **数据来源**：DCMI `dcmi_get_device_sensor_info(card, dev, DCMI_SOC_TEMP_ID, ...)`
- **采集方法**：取 SOC 最高温度（°C）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"soc_max_temp","value":65,"unit":"°C","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.31 fp_max_temp（NPU光模块最高温度）

- **数据来源**：DCMI `dcmi_get_device_sensor_info(card, dev, DCMI_FP_TEMP_ID, ...)`
- **采集方法**：取光模块（FP）最高温度（°C）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"fp_max_temp","value":50,"unit":"°C","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.32 ndie_temp（NPU NDie温度）

- **数据来源**：DCMI `dcmi_get_device_sensor_info(card, dev, DCMI_N_DIE_TEMP_ID, ...)`
- **采集方法**：取 NDie 温度（°C）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"ndie_temp","value":58,"unit":"°C","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.33 hbm_max_temp（NPU HBM最高温度）

- **数据来源**：DCMI `dcmi_get_device_sensor_info(card, dev, DCMI_HBM_TEMP_ID, ...)`
- **采集方法**：取 HBM 最高温度（°C）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"hbm_max_temp","value":55,"unit":"°C","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.34 aicpu_freq（NPU AICPU频率）

- **数据来源**：DCMI `dcmi_get_aicpu_info(card, dev, ...)`
- **采集方法**：取 AICPU 频率（MHz）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"aicpu_freq","value":1800,"unit":"MHz","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.35 aicore_rated_freq（NPU AICore额定频率）

- **数据来源**：DCMI `dcmi_get_device_frequency(card, dev, DCMI_FREQ_AICORE_MAX, &freq)`
- **采集方法**：取 AICore 额定（最大）频率（MHz）。启动时采集一次（静态）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"aicore_rated_freq","value":2000,"unit":"MHz","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.36 aicore_freq（NPU AICore频率）

- **数据来源**：DCMI `dcmi_get_device_frequency(card, dev, DCMI_FREQ_AICORE_CURRENT_, &freq)`
- **采集方法**：取 AICore 当前频率（MHz）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"aicore_freq","value":1800,"unit":"MHz","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.37 ctrlcpu_freq（NPU CTRLCPU频率）

- **数据来源**：DCMI `dcmi_get_device_frequency(card, dev, DCMI_FREQ_CTRLCPU, &freq)`
- **采集方法**：取 CTRLCPU 频率（MHz）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"ctrlcpu_freq","value":2400,"unit":"MHz","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.38 vector_core_freq（NPU Vector Core频率）

- **数据来源**：DCMI `dcmi_get_device_frequency(card, dev, DCMI_FREQ_VECTORCORE_CURRENT, &freq)`
- **采集方法**：取 Vector Core 当前频率（MHz）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"vector_core_freq","value":1800,"unit":"MHz","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.39 hbm_freq（NPU HBM频率）

- **数据来源**：DCMI `dcmi_get_device_frequency(card, dev, DCMI_FREQ_HBM, &freq)`
- **采集方法**：取 HBM 频率（MHz）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"hbm_freq","value":1600,"unit":"MHz","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.40 ddr_freq（NPU DDR频率）

- **数据来源**：DCMI `dcmi_get_device_frequency(card, dev, DCMI_FREQ_DDR, &freq)`
- **采集方法**：取 DDR 频率（MHz）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"ddr_freq","value":2400,"unit":"MHz","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.41 npu_util（NPU整体利用率）

- **数据来源**：DCMI `dcmi_get_device_utilization_rate(card, dev, DCMI_UTILIZATION_RATE_NPU, &rate)`
- **采集方法**：取 NPU 整体利用率（0-100）。与 5.1（AICore 利用率）不同，是整体口径
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"npu_util","value":50,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.42 aicpu_util（NPU AICPU利用率）

- **数据来源**：DCMI `dcmi_get_device_utilization_rate(card, dev, DCMI_UTILIZATION_RATE_AICPU, &rate)`
- **采集方法**：取 AICPU 利用率（%）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"aicpu_util","value":30,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.43 ctrlcpu_util（NPU CTRLCPU利用率）

- **数据来源**：DCMI `dcmi_get_device_utilization_rate(card, dev, DCMI_UTILIZATION_RATE_CTRLCPU, &rate)`
- **采集方法**：取 CTRLCPU 利用率（%）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"ctrlcpu_util","value":20,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.44 vector_core_util（NPU Vector Core利用率）

- **数据来源**：DCMI `dcmi_get_device_utilization_rate(card, dev, DCMI_UTILIZATION_RATE_VECTORCORE, &rate)`
- **采集方法**：取 Vector Core 利用率（%）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"vector_core_util","value":25,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.45 hbm_bandwidth_util（NPU HBM带宽利用率）

- **数据来源**：DCMI `dcmi_get_device_utilization_rate(card, dev, DCMI_UTILIZATION_RATE_HBM_BANDWIDTH, &rate)`
- **采集方法**：取 HBM 带宽利用率（%）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"hbm_bandwidth_util","value":40,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.46 ddr_util（NPU DDR利用率）

- **数据来源**：DCMI `dcmi_get_device_utilization_rate(card, dev, DCMI_UTILIZATION_RATE_DDR, &rate)`
- **采集方法**：取 DDR 利用率（%）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"ddr_util","value":15,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.47 ddr_bandwidth_util（NPU DDR带宽利用率）

- **数据来源**：DCMI `dcmi_get_device_utilization_rate(card, dev, DCMI_UTILIZATION_RATE_DDR_BANDWIDTH, &rate)`
- **采集方法**：取 DDR 带宽利用率（%）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"ddr_bandwidth_util","value":10,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.48 vdec_util（NPU VDEC利用率）

- **数据来源**：DCMI `dcmi_get_device_info(card, dev, DCMI_MAIN_CMD_DVPP, DCMI_SUB_CMD_DVPP_VDEC_RATE, ...)`
- **采集方法**：取视频解码单元 VDEC 利用率（%）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"vdec_util","value":10,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.49 vpc_util（NPU VPC利用率）

- **数据来源**：DCMI `dcmi_get_device_info(card, dev, DCMI_MAIN_CMD_DVPP, DCMI_SUB_CMD_DVPP_VPC_RATE, ...)`
- **采集方法**：取视频处理单元 VPC 利用率（%）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"vpc_util","value":5,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.50 venc_util（NPU VENC利用率）

- **数据来源**：DCMI `dcmi_get_device_info(card, dev, DCMI_MAIN_CMD_DVPP, DCMI_SUB_CMD_DVPP_VENC_RATE, ...)`
- **采集方法**：取视频编码单元 VENC 利用率（%）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"venc_util","value":8,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.51 jpege_util（NPU JPEGE利用率）

- **数据来源**：DCMI `dcmi_get_device_info(card, dev, DCMI_MAIN_CMD_DVPP, DCMI_SUB_CMD_DVPP_JPEGE_RATE, ...)`
- **采集方法**：取 JPEG 编码单元 JPEGE 利用率（%）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"jpege_util","value":3,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.52 jpegd_util（NPU JPEGD利用率）

- **数据来源**：DCMI `dcmi_get_device_info(card, dev, DCMI_MAIN_CMD_DVPP, DCMI_SUB_CMD_DVPP_JPEGD_RATE, ...)`
- **采集方法**：取 JPEG 解码单元 JPEGD 利用率（%）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"jpegd_util","value":2,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.53 hbm_total_memory（NPU HBM总容量）

- **数据来源**：DCMI `dcmi_get_device_hbm_info(card, dev, &hbm_info)` → `hbm_info.memory_size`
- **采集方法**：取 HBM 总容量（MB）。启动时采集一次（静态）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"hbm_total_memory","value":32768,"unit":"MB","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.54 hbm_used_memory（NPU HBM已用容量）

- **数据来源**：DCMI `dcmi_get_device_hbm_info(card, dev, &hbm_info)` → `hbm_info.memory_usage`
- **采集方法**：取 HBM 已用容量（MB）。与 5.2 memory_usage(%) 互补
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"hbm_used_memory","value":16384,"unit":"MB","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.55 hbm_single_ecc（NPU HBM单bit错误）

- **数据来源**：DCMI `dcmi_get_device_ecc_info(card, dev, DCMI_DEVICE_TYPE_HBM, &ecc)` → `ecc.single_bit_error_cnt`
- **采集方法**：取 HBM 单 bit（CE）累计错误数，差值得本周期新增
- **Labels**：`npu_id`、`device_type`（"hbm"）、`kind`（"single"）
- **输出示例**：
```json
{"component":"npu","name":"hbm_single_ecc","value":3,"unit":"次","labels":{"npu_id":"0","device_type":"hbm","kind":"single"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.56 hbm_double_ecc（NPU HBM多bit错误）

- **数据来源**：DCMI `dcmi_get_device_ecc_info(card, dev, DCMI_DEVICE_TYPE_HBM, &ecc)` → `ecc.double_bit_error_cnt`
- **采集方法**：取 HBM 多 bit（UE）累计错误数，差值得本周期新增
- **Labels**：`npu_id`、`device_type`（"hbm"）、`kind`（"double"）
- **输出示例**：
```json
{"component":"npu","name":"hbm_double_ecc","value":0,"unit":"次","labels":{"npu_id":"0","device_type":"hbm","kind":"double"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.57 hbm_single_ecc_isolated（NPU HBM单bit隔离页数）

- **数据来源**：DCMI `dcmi_get_device_ecc_info(card, dev, DCMI_DEVICE_TYPE_HBM, &ecc)` → `ecc.single_bit_isolated_pages_cnt`
- **采集方法**：取 HBM 因单 bit 错误被隔离的页数（个）
- **Labels**：`npu_id`、`device_type`（"hbm"）
- **输出示例**：
```json
{"component":"npu","name":"hbm_single_ecc_isolated","value":2,"unit":"个","labels":{"npu_id":"0","device_type":"hbm"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.58 hbm_double_ecc_isolated（NPU HBM多bit隔离页数）

- **数据来源**：DCMI `dcmi_get_device_ecc_info(card, dev, DCMI_DEVICE_TYPE_HBM, &ecc)` → `ecc.double_bit_isolated_pages_cnt`
- **采集方法**：取 HBM 因多 bit 错误被隔离的页数（个）
- **Labels**：`npu_id`、`device_type`（"hbm"）
- **输出示例**：
```json
{"component":"npu","name":"hbm_double_ecc_isolated","value":0,"unit":"个","labels":{"npu_id":"0","device_type":"hbm"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.59 ddr_single_ecc（NPU DDR单bit错误）

- **数据来源**：DCMI `dcmi_get_device_ecc_info(card, dev, DCMI_DEVICE_TYPE_DDR, &ecc)` → `ecc.single_bit_error_cnt`
- **采集方法**：取 DDR 单 bit（CE）累计错误数，差值得本周期新增
- **Labels**：`npu_id`、`device_type`（"ddr"）、`kind`（"single"）
- **输出示例**：
```json
{"component":"npu","name":"ddr_single_ecc","value":1,"unit":"次","labels":{"npu_id":"0","device_type":"ddr","kind":"single"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.60 ddr_double_ecc（NPU DDR多bit错误）

- **数据来源**：DCMI `dcmi_get_device_ecc_info(card, dev, DCMI_DEVICE_TYPE_DDR, &ecc)` → `ecc.double_bit_error_cnt`
- **采集方法**：取 DDR 多 bit（UE）累计错误数，差值得本周期新增
- **Labels**：`npu_id`、`device_type`（"ddr"）、`kind`（"double"）
- **输出示例**：
```json
{"component":"npu","name":"ddr_double_ecc","value":0,"unit":"次","labels":{"npu_id":"0","device_type":"ddr","kind":"double"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.61 ddr_single_ecc_isolated（NPU DDR单bit隔离页数）

- **数据来源**：DCMI `dcmi_get_device_ecc_info(card, dev, DCMI_DEVICE_TYPE_DDR, &ecc)` → `ecc.single_bit_isolated_pages_cnt`
- **采集方法**：取 DDR 因单 bit 错误被隔离的页数（个）
- **Labels**：`npu_id`、`device_type`（"ddr"）
- **输出示例**：
```json
{"component":"npu","name":"ddr_single_ecc_isolated","value":1,"unit":"个","labels":{"npu_id":"0","device_type":"ddr"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.62 ddr_double_ecc_isolated（NPU DDR多bit隔离页数）

- **数据来源**：DCMI `dcmi_get_device_ecc_info(card, dev, DCMI_DEVICE_TYPE_DDR, &ecc)` → `ecc.double_bit_isolated_pages_cnt`
- **采集方法**：取 DDR 因多 bit 错误被隔离的页数（个）
- **Labels**：`npu_id`、`device_type`（"ddr"）
- **输出示例**：
```json
{"component":"npu","name":"ddr_double_ecc_isolated","value":0,"unit":"个","labels":{"npu_id":"0","device_type":"ddr"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.63 llc_write_hit_rate（NPU LLC写命中率）

- **数据来源**：DCMI `dcmi_get_device_llc_perf_para(card, dev, &perf)` → `perf.wr_hit_rate`
- **采集方法**：取 LLC 写命中率（%，原始 0-1 或 0-100 待实测）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"llc_write_hit_rate","value":85,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.64 llc_read_hit_rate（NPU LLC读命中率）

- **数据来源**：DCMI `dcmi_get_device_llc_perf_para(card, dev, &perf)` → `perf.rd_hit_rate`
- **采集方法**：取 LLC 读命中率（%）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"llc_read_hit_rate","value":90,"unit":"%","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.65 llc_throughput（NPU LLC吞吐量）

- **数据来源**：DCMI `dcmi_get_device_llc_perf_para(card, dev, &perf)` → `perf.throughput`
- **采集方法**：取 LLC 吞吐量（MB/s，原始单位待实测）
- **Labels**：`npu_id`
- **输出示例**：
```json
{"component":"npu","name":"llc_throughput","value":1250,"unit":"MB/s","labels":{"npu_id":"0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.66 net_tx_bandwidth（NPU网口发送带宽）

- **数据来源**：`hccn_tool -i <npu_id> -bandwidth -g`（解析 Bandwidth TX）
- **采集方法**：取网口发送带宽（MB/s）
- **Labels**：`npu_id`、`direction`（"tx"）
- **输出示例**：
```json
{"component":"npu","name":"net_tx_bandwidth","value":1250,"unit":"MB/s","labels":{"npu_id":"0","direction":"tx"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.67 net_rx_bandwidth（NPU网口接收带宽）

- **数据来源**：`hccn_tool -i <npu_id> -bandwidth -g`（解析 Bandwidth RX）
- **采集方法**：取网口接收带宽（MB/s）
- **Labels**：`npu_id`、`direction`（"rx"）
- **输出示例**：
```json
{"component":"npu","name":"net_rx_bandwidth","value":980,"unit":"MB/s","labels":{"npu_id":"0","direction":"rx"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.68 roce_link_status（NPU RoCE连接状态）

- **数据来源**：DCMI `dcmi_get_device_network_health(card, dev, ...)`
- **采集方法**：取 RoCE 连接状态，up=1 / down=0
- **Labels**：`npu_id`、`status`（"up"/"down"）
- **输出示例**：
```json
{"component":"npu","name":"roce_link_status","value":1,"unit":"","labels":{"npu_id":"0","status":"up"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.69 roce_speed_status（NPU RoCE连接速度）

- **数据来源**：`hccn_tool -i <npu_id> -speed -g`
- **采集方法**：取 RoCE 速度字符串（如 "100Gbps"），放 `labels.roce_speed`，`value` 填 0
- **Labels**：`npu_id`、`roce_speed`
- **输出示例**：
```json
{"component":"npu","name":"roce_speed_status","value":0,"unit":"","labels":{"npu_id":"0","roce_speed":"100Gbps"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.70 roce_link_health（NPU RoCE Link状态）

- **数据来源**：`hccn_tool -i <npu_id> -link -g`
- **采集方法**：取 RoCE 链路状态字符串，放 `labels.roce_link`，`value` 填 0
- **Labels**：`npu_id`、`roce_link`
- **输出示例**：
```json
{"component":"npu","name":"roce_link_health","value":0,"unit":"","labels":{"npu_id":"0","roce_link":"ACTIVE"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.71 pcie_tx_bandwidth（NPU PCIe发送带宽）

- **数据来源**：`hccn_tool -i <npu_id> -bandwidth -g`（解析 PCIe TX）
- **采集方法**：取 PCIe 发送带宽（MB/s）
- **Labels**：`npu_id`、`direction`（"tx"）
- **输出示例**：
```json
{"component":"npu","name":"pcie_tx_bandwidth","value":2500,"unit":"MB/s","labels":{"npu_id":"0","direction":"tx"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.72 pcie_rx_bandwidth（NPU PCIe接收带宽）

- **数据来源**：`hccn_tool -i <npu_id> -bandwidth -g`（解析 PCIe RX）
- **采集方法**：取 PCIe 接收带宽（MB/s）
- **Labels**：`npu_id`、`direction`（"rx"）
- **输出示例**：
```json
{"component":"npu","name":"pcie_rx_bandwidth","value":2100,"unit":"MB/s","labels":{"npu_id":"0","direction":"rx"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.73 hccs_tx_bandwidth（NPU HCCS发送带宽）

- **数据来源**：`npu-smi info -t hccs-bw -i <npu_id> -c 0 -time 50`
- **采集方法**：取 HCCS 发送带宽（MB/s）
- **Labels**：`npu_id`、`direction`（"tx"）
- **输出示例**：
```json
{"component":"npu","name":"hccs_tx_bandwidth","value":300,"unit":"MB/s","labels":{"npu_id":"0","direction":"tx"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 5.74 hccs_rx_bandwidth（NPU HCCS接收带宽）

- **数据来源**：`npu-smi info -t hccs-bw -i <npu_id> -c 0 -time 50`
- **采集方法**：取 HCCS 接收带宽（MB/s）
- **Labels**：`npu_id`、`direction`（"rx"）
- **输出示例**：
```json
{"component":"npu","name":"hccs_rx_bandwidth","value":280,"unit":"MB/s","labels":{"npu_id":"0","direction":"rx"},"timestamp":"2026-07-10T10:30:00Z"}
```

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

## 7. Chassis 采集指标（机箱环境）

Chassis 采集器通过 `ipmitool sdr` 获取服务器整机级环境指标（整机功耗、进/出风口温度、机箱风扇转速）。与 CPU/Memory collector 共享同一份 30s SDR 缓存。

| 序号 | 指标名称 | 中文名称 | 优先级 | 默认周期 | 默认采集 | 单位 | 数据来源 |
|------|----------|----------|--------|----------|----------|------|----------|
| 7.1 | power | 整机功耗 | High | 10s | 是 | W | ipmitool SDR "Power" |
| 7.2 | inlet_temp | 进风口温度 | High | 10s | 是 | °C | ipmitool SDR "Inlet Temp" |
| 7.3 | outlet_temp | 出风口温度 | Medium | 10s | 是 | °C | ipmitool SDR "Outlet Temp" |
| 7.4 | fan_speed | 风扇转速 | Medium | 10s | 是 | RPM | ipmitool SDR "FAN* Speed" |
| 7.5 | fan_power | 风扇功率 | Medium | 10s | 是 | W | ipmitool SDR "FAN* Power" |

### 采集方法

从缓存的 `ipmitool sdr` 输出（30s 缓存，与 cpu/memory collector 共享）中按传感器名筛选：
- `power`：筛选 name 含 "Power" 且不含 "CPU"/"MEM"/"NPU" 的功率传感器
- `inlet_temp`：筛选 name 含 "Inlet" + "Temp" 的温度传感器
- `outlet_temp`：筛选 name 含 "Outlet" + "Temp" 的温度传感器
- `fan_speed`：筛选 name 含 "FAN" + "Speed" 的风扇传感器，解析风扇编号（1-8）和方向（F 前/R 后）

典型 SDR 输出：
```
Inlet Temp        | 28.000     | degrees C  | ok
Outlet Temp       | 42.000     | degrees C  | ok
Power             | 1848.000   | Watts      | ok
FAN1 F Speed      | 9375.000   | RPM        | ok
FAN1 R Speed      | 9300.000   | RPM        | ok
```

### 指标详情

#### 7.1 power（整机功耗）

- **数据来源**：ipmitool SDR 中 name 为 "Power" 的传感器
- **采集方法**：从缓存的 SDR 中筛选 name 含 "power" 且不含 "cpu"/"mem"/"npu" 的功率传感器，取其 Value（W）
- **Labels**：无
- **输出示例**：
```json
{"component":"chassis","name":"power","value":1848.0,"unit":"W","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 7.2 inlet_temp（进风口温度）

- **数据来源**：ipmitool SDR 中 name 含 "Inlet" + "Temp" 的传感器
- **采集方法**：从缓存的 SDR 中筛选进风口温度传感器，取 Value（°C）
- **Labels**：无
- **输出示例**：
```json
{"component":"chassis","name":"inlet_temp","value":28.0,"unit":"°C","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 7.3 outlet_temp（出风口温度）

- **数据来源**：ipmitool SDR 中 name 含 "Outlet" + "Temp" 的传感器
- **采集方法**：从缓存的 SDR 中筛选出风口温度传感器，取 Value（°C）
- **Labels**：无
- **输出示例**：
```json
{"component":"chassis","name":"outlet_temp","value":42.0,"unit":"°C","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 7.4 fan_speed（风扇转速）

- **数据来源**：ipmitool SDR 中 name 含 "FAN" + "Speed" 的传感器
- **采集方法**：从缓存的 SDR 中筛选所有风扇传感器，解析风扇编号（FAN 后的数字）和方向（F 前 / R 后），取 Value（RPM）
- **Labels**：`fan`（"1"~"8"）、`direction`（"F" 前 / "R" 后）
- **输出示例**：
```json
{"component":"chassis","name":"fan_speed","value":9375,"unit":"RPM","labels":{"fan":"1","direction":"F"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"chassis","name":"fan_speed","value":9300,"unit":"RPM","labels":{"fan":"1","direction":"R"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 7.5 fan_power（风扇功率）

- **数据来源**：ipmitool SDR 中 name 含 "FAN" + "Power" 的传感器
- **采集方法**：从缓存的 SDR 中筛选所有风扇功率传感器，解析风扇编号，取 Value（W）
- **Labels**：`fan`（"1"~"8"）
- **输出示例**：
```json
{"component":"chassis","name":"fan_power","value":8.5,"unit":"W","labels":{"fan":"1"},"timestamp":"2026-07-10T10:30:00Z"}
```

> 注：chassis `fan_power` 单位为 W（每风扇独立功率），与 `power`（整机总功耗）不同。

---

## 附录：各采集器数据来源汇总

| 采集器 | 数据来源 | 依赖外部命令 |
|--------|----------|-------------|
| CPU | /proc/stat, /proc/loadavg, /proc/cpuinfo, /proc/buddyinfo, /sys/devices/system/cpu/(cpufreq,cache,online,offline,isolated), /sys/devices/system/node, lscpu, ipmitool, dmesg//var/log/mcelog | lscpu, ipmitool, dmesg(mce) |
| Memory | /proc/meminfo, /proc/vmstat, /proc/pressure/memory, /proc/buddyinfo, /sys/devices/system/edac/mc, dmidecode, ipmitool(SDR), dmesg | dmidecode, ipmitool, dmesg(oom) |
| Disk | /proc/mounts, statfs syscall, /proc/diskstats, /proc/stat | smartctl (Phase 3) |
| GPU | nvidia-smi 命令输出 | nvidia-smi |
| NPU | libdcmi.so (DCMI dcmi_*), npu-smi info -t (topo/hccs-bw), hccn_tool (-bandwidth/-speed/-link) | DCMI(CGo,需 -tags dcmi), npu-smi, hccn_tool |
| Network | /proc/net/dev, /sys/class/net/, /proc/net/tcp, /proc/net/tcp6 | 无 |
| Chassis | ipmitool SDR (Power/Inlet/Outlet Temp/FAN Speed) | ipmitool |

---

## 附录B：已实现采集指标清单

> 以下 159 个指标均已实现并通过测试，按部件分类汇总。其中 CPU 扩展至 40、Memory 扩展至 19、Disk 扩展至 9、NPU 扩展至 74 个指标，Chassis 新增 5 个指标，且全部 7 个采集器（chassis/cpu/memory/disk/network/gpu/npu）已接入来源层(source layer)。NPU 采用 device 并行采集，DCMI 指标通过 CGo（`-tags dcmi`）调用 libdcmi.so。

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

### Disk（9 个）

| 序号 | 指标名称 | 中文名称 | 优先级 | 单位 |
|------|----------|----------|--------|------|
| 1 | space_usage | 磁盘空间使用率 | High | % |
| 2 | iops | 读写IOPS | Medium | 次/s |
| 3 | throughput | 读写吞吐量 | Medium | MB/s |
| 4 | read_latency | 读耗时 | Medium | ms/s |
| 5 | write_latency | 写耗时 | Medium | ms/s |
| 6 | io_wait | I/O等待占比 | Medium | % |
| 7 | smart_status | SMART健康状态 | Medium | - |
| 8 | smart_temperature | 硬盘温度 | Low | °C |
| 9 | io_errors | I/O错误计数 | Low | 次 |

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

### NPU（74 个）

| 序号 | 指标名称 | 中文名称 | 优先级 | 单位 |
|------|----------|----------|--------|------|
| 1 | utilization | NPU使用率(AICore) | High | % |
| 2 | memory_usage | NPU显存使用率(HBM) | High | % |
| 3 | temperature | NPU温度 | High | °C |
| 4 | power_draw | NPU功耗 | Medium | W |
| 5 | health_status | NPU健康状态 | Medium | - |
| 6 | npu_num | NPU设备数量 | Low | 个 |
| 7 | chip_type | NPU芯片类型 | Low | - |
| 8 | driver_version | NPU驱动版本 | Low | - |
| 9 | driver_health | NPU驱动健康状态 | Medium | - |
| 10 | error_code | NPU错误码 | Medium | - |
| 11 | process_info | NPU进程PID信息 | Low | - |
| 12 | process_total | NPU进程总数量 | Low | 个 |
| 13 | comm_topo | NPU通信拓扑 | Low | - |
| 14 | voltage | NPU电压 | Medium | V |
| 15 | aicore_voltage | NPU AICore电压 | Medium | V |
| 16 | hybrid_voltage | NPU Hybrid电压 | Medium | V |
| 17 | cpu_voltage | NPU CPU电压 | Medium | V |
| 18 | ddr_voltage | NPU DDR电压 | Medium | V |
| 19 | acg_count | NPU ACG调频计数 | Low | 次 |
| 20 | fan_speed | NPU风扇转速 | Medium | % |
| 21 | hbm_temp | NPU HBM温度 | Medium | °C |
| 22 | cluster_temp | NPU Cluster温度 | Medium | °C |
| 23 | peri_temp | NPU Peri温度 | Medium | °C |
| 24 | aicore0_temp | NPU AICORE0温度 | Medium | °C |
| 25 | aicore1_temp | NPU AICORE1温度 | Medium | °C |
| 26 | ntc1_temp | NPU热敏电阻1温度 | Low | °C |
| 27 | ntc2_temp | NPU热敏电阻2温度 | Low | °C |
| 28 | ntc3_temp | NPU热敏电阻3温度 | Low | °C |
| 29 | ntc4_temp | NPU热敏电阻4温度 | Low | °C |
| 30 | soc_max_temp | NPU SOC最高温度 | Medium | °C |
| 31 | fp_max_temp | NPU光模块最高温度 | Medium | °C |
| 32 | ndie_temp | NPU NDie温度 | Medium | °C |
| 33 | hbm_max_temp | NPU HBM最高温度 | Medium | °C |
| 34 | aicpu_freq | NPU AICPU频率 | Medium | MHz |
| 35 | aicore_rated_freq | NPU AICore额定频率 | Low | MHz |
| 36 | aicore_freq | NPU AICore频率 | Medium | MHz |
| 37 | ctrlcpu_freq | NPU CTRLCPU频率 | Medium | MHz |
| 38 | vector_core_freq | NPU Vector Core频率 | Medium | MHz |
| 39 | hbm_freq | NPU HBM频率 | Medium | MHz |
| 40 | ddr_freq | NPU DDR频率 | Medium | MHz |
| 41 | npu_util | NPU整体利用率 | High | % |
| 42 | aicpu_util | NPU AICPU利用率 | Medium | % |
| 43 | ctrlcpu_util | NPU CTRLCPU利用率 | Medium | % |
| 44 | vector_core_util | NPU Vector Core利用率 | Medium | % |
| 45 | hbm_bandwidth_util | NPU HBM带宽利用率 | Medium | % |
| 46 | ddr_util | NPU DDR利用率 | Medium | % |
| 47 | ddr_bandwidth_util | NPU DDR带宽利用率 | Medium | % |
| 48 | vdec_util | NPU VDEC利用率 | Low | % |
| 49 | vpc_util | NPU VPC利用率 | Low | % |
| 50 | venc_util | NPU VENC利用率 | Low | % |
| 51 | jpege_util | NPU JPEGE利用率 | Low | % |
| 52 | jpegd_util | NPU JPEGD利用率 | Low | % |
| 53 | hbm_total_memory | NPU HBM总容量 | Low | MB |
| 54 | hbm_used_memory | NPU HBM已用容量 | High | MB |
| 55 | hbm_single_ecc | NPU HBM单bit错误 | High | 次 |
| 56 | hbm_double_ecc | NPU HBM多bit错误 | High | 次 |
| 57 | hbm_single_ecc_isolated | NPU HBM单bit隔离页数 | Medium | 个 |
| 58 | hbm_double_ecc_isolated | NPU HBM多bit隔离页数 | Medium | 个 |
| 59 | ddr_single_ecc | NPU DDR单bit错误 | High | 次 |
| 60 | ddr_double_ecc | NPU DDR多bit错误 | High | 次 |
| 61 | ddr_single_ecc_isolated | NPU DDR单bit隔离页数 | Medium | 个 |
| 62 | ddr_double_ecc_isolated | NPU DDR多bit隔离页数 | Medium | 个 |
| 63 | llc_write_hit_rate | NPU LLC写命中率 | Low | % |
| 64 | llc_read_hit_rate | NPU LLC读命中率 | Low | % |
| 65 | llc_throughput | NPU LLC吞吐量 | Low | MB/s |
| 66 | net_tx_bandwidth | NPU网口发送带宽 | Medium | MB/s |
| 67 | net_rx_bandwidth | NPU网口接收带宽 | Medium | MB/s |
| 68 | roce_link_status | NPU RoCE连接状态 | Medium | - |
| 69 | roce_speed_status | NPU RoCE连接速度 | Medium | - |
| 70 | roce_link_health | NPU RoCE Link状态 | Medium | - |
| 71 | pcie_tx_bandwidth | NPU PCIe发送带宽 | Medium | MB/s |
| 72 | pcie_rx_bandwidth | NPU PCIe接收带宽 | Medium | MB/s |
| 73 | hccs_tx_bandwidth | NPU HCCS发送带宽 | Medium | MB/s |
| 74 | hccs_rx_bandwidth | NPU HCCS接收带宽 | Medium | MB/s |
| 75 | mac_tx_mac_pause_num | MAC发送pause帧总报文数 | Medium | 个 |
| 76 | mac_rx_mac_pause_num | MAC接收pause帧总报文数 | Medium | 个 |
| 77 | mac_tx_pfc_pkt_num | MAC发送PFC帧总报文数 | Medium | 个 |
| 78 | mac_tx_pfc_pri0_pkt_num | MAC 0号队列发送PFC帧数 | Medium | 个 |
| 79 | mac_tx_pfc_pri1_pkt_num | MAC 1号队列发送PFC帧数 | Medium | 个 |
| 80 | mac_tx_pfc_pri2_pkt_num | MAC 2号队列发送PFC帧数 | Medium | 个 |
| 81 | mac_tx_pfc_pri3_pkt_num | MAC 3号队列发送PFC帧数 | Medium | 个 |
| 82 | mac_tx_pfc_pri4_pkt_num | MAC 4号队列发送PFC帧数 | Medium | 个 |
| 83 | mac_tx_pfc_pri5_pkt_num | MAC 5号队列发送PFC帧数 | Medium | 个 |
| 84 | mac_tx_pfc_pri6_pkt_num | MAC 6号队列发送PFC帧数 | Medium | 个 |
| 85 | mac_tx_pfc_pri7_pkt_num | MAC 7号队列发送PFC帧数 | Medium | 个 |
| 86 | mac_rx_pfc_pkt_num | MAC接收PFC帧总报文数 | Medium | 个 |
| 87 | mac_rx_pfc_pri0_pkt_num | MAC 0号队列接收PFC帧数 | Medium | 个 |
| 88 | mac_rx_pfc_pri1_pkt_num | MAC 1号队列接收PFC帧数 | Medium | 个 |
| 89 | mac_rx_pfc_pri2_pkt_num | MAC 2号队列接收PFC帧数 | Medium | 个 |
| 90 | mac_rx_pfc_pri3_pkt_num | MAC 3号队列接收PFC帧数 | Medium | 个 |
| 91 | mac_rx_pfc_pri4_pkt_num | MAC 4号队列接收PFC帧数 | Medium | 个 |
| 92 | mac_rx_pfc_pri5_pkt_num | MAC 5号队列接收PFC帧数 | Medium | 个 |
| 93 | mac_rx_pfc_pri6_pkt_num | MAC 6号队列接收PFC帧数 | Medium | 个 |
| 94 | mac_rx_pfc_pri7_pkt_num | MAC 7号队列接收PFC帧数 | Medium | 个 |
| 95 | mac_tx_total_pkt_num | MAC发送总报文数 | Medium | 个 |
| 96 | mac_tx_total_oct_num | MAC发送总报文字节数 | Medium | bytes |
| 97 | mac_tx_bad_pkt_num | MAC发送坏包总报文数 | Medium | 个 |
| 98 | mac_tx_bad_oct_num | MAC发送坏包总字节数 | Medium | bytes |
| 99 | mac_rx_total_pkt_num | MAC接收总报文数 | Medium | 个 |
| 100 | mac_rx_total_oct_num | MAC接收总报文字节数 | Medium | bytes |
| 101 | mac_rx_bad_pkt_num | MAC接收坏包总报文数 | Medium | 个 |
| 102 | mac_rx_bad_oct_num | MAC接收坏包总字节数 | Medium | bytes |
| 103 | roce_rx_rc_pkt_num | ROCE接收RC类型报文数 | Medium | 个 |
| 104 | roce_rx_all_pkt_num | ROCE接收总报文数 | Medium | 个 |
| 105 | roce_rx_err_pkt_num | ROCE接收坏包总报文数 | Medium | 个 |
| 106 | roce_tx_rc_pkt_num | ROCE发送RC类型报文数 | Medium | 个 |
| 107 | roce_tx_all_pkt_num | ROCE发送总报文数 | Medium | 个 |
| 108 | roce_tx_err_pkt_num | ROCE发送坏包总报文数 | Medium | 个 |
| 109 | roce_cqe_num | ROCE任务完成总元素个数 | Medium | 个 |
| 110 | roce_rx_cnp_pkt_num | ROCE接收CNP类型报文数 | Medium | 个 |
| 111 | roce_tx_cnp_pkt_num | ROCE发送CNP类型报文数 | Medium | 个 |
| 112 | roce_unexpected_ack_num | ROCE接收非预期ACK报文数 | Medium | 个 |
| 113 | roce_out_of_order_num | ROCE接收乱序或重复PSN报文数 | Medium | 个 |
| 114 | roce_verification_err_num | ROCE接收校验错误报文数 | Medium | 个 |
| 115 | roce_qp_status_err_num | ROCE接收QP状态异常报文数 | Medium | 个 |
| 116 | nic_tx_all_pkg_num | NIC发送总报文数 | Medium | 个 |
| 117 | nic_tx_all_oct_num | NIC发送总报文字节数 | Medium | bytes |
| 118 | nic_rx_all_pkg_num | NIC接收总报文数 | Medium | 个 |
| 119 | nic_rx_all_oct_num | NIC接收总报文字节数 | Medium | bytes |

### Network（5 个）

| 序号 | 指标名称 | 中文名称 | 优先级 | 单位 |
|------|----------|----------|--------|------|
| 1 | throughput | 网络吞吐量 | High | bytes/s |
| 2 | packet_count | 包收发数 | Medium | 个/s |
| 3 | error_count | 错误包计数 | Medium | 次 |
| 4 | interface_status | 网卡接口状态 | Medium | - |
| 5 | connection_count | 网络连接数 | Low | 个 |

### Chassis（5 个）

| 序号 | 指标名称 | 中文名称 | 优先级 | 单位 |
|------|----------|----------|--------|------|
| 1 | power | 整机功耗 | High | W |
| 2 | inlet_temp | 进风口温度 | High | °C |
| 3 | outlet_temp | 出风口温度 | Medium | °C |
| 4 | fan_speed | 风扇转速 | Medium | RPM |
| 5 | fan_power | 风扇功率 | Medium | W |

### 统计汇总

| 部件 | 指标数 | High | Medium | Low |
|------|--------|------|--------|-----|
| CPU | 40 | 4 | 12 | 24 |
| Memory | 19 | 4 | 7 | 8 |
| Disk | 9 | 1 | 5 | 3 |
| GPU | 7 | 3 | 3 | 1 |
| NPU | 119 | 9 | 88 | 22 |
| Network | 5 | 1 | 3 | 1 |
| Chassis | 5 | 2 | 3 | 0 |
| **合计** | **204** | **24** | **121** | **59** |
