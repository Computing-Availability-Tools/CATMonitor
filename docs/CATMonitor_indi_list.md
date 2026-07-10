# CATMonitor 采集指标清单

> 本文档列出 CATMonitor 支持的全部服务器运行指标。
> 每个指标包含：优先级、默认采集周期、默认是否采集、数据来源、采集方法、输出示例。

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
| CPU | 7 | 2 | 2 | 3 |
| Memory | 6 | 4 | 1 | 1 |
| Disk | 7 | 1 | 3 | 3 |
| GPU | 7 | 3 | 3 | 1 |
| NPU | 5 | 3 | 2 | 0 |
| Network | 5 | 1 | 3 | 1 |
| **合计** | **37** | **14** | **14** | **9** |

---

## 1. CPU 采集指标

CPU 采集器读取 Linux 内核虚拟文件系统（/proc、/sys）获取 CPU 运行状态。

| 序号 | 指标名称 | 中文名称 | 优先级 | 默认周期 | 默认采集 | 单位 | 数据来源 |
|------|----------|----------|--------|----------|----------|------|----------|
| 1.1 | usage | CPU使用率 | High | 3s | 是 | % | /proc/stat |
| 1.2 | load_average | 系统负载 | High | 3s | 是 | - | /proc/loadavg |
| 1.3 | temperature | CPU温度 | Medium | 10s | 是 | °C | /sys/class/thermal/thermal_zone*/temp |
| 1.4 | frequency | CPU频率 | Medium | 10s | 否 | MHz | /sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq |
| 1.5 | context_switches | 上下文切换次数 | Low | 10s | 否 | 次/s | /proc/stat (ctxt行) |
| 1.6 | process_count | 运行进程数 | Low | 10s | 否 | 个 | /proc/loadavg |
| 1.7 | model_info | CPU型号信息 | Low | 启动时1次 | 是 | - | /proc/cpuinfo |

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

- **数据来源**：`/sys/class/thermal/thermal_zone*/temp`
- **采集方法**：遍历 `/sys/class/thermal/` 下所有 thermal_zone 目录，读取其 `temp` 文件，值为毫摄氏度，需除以 1000 转换为摄氏度
- **Labels**：`zone`（"thermal_zone0", "thermal_zone1", ...）
- **输出示例**：
```json
{"component":"cpu","name":"temperature","value":65.0,"unit":"°C","labels":{"zone":"thermal_zone0"},"timestamp":"2026-07-10T10:30:00Z"}
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

---

## 2. Memory 采集指标

内存采集器读取 `/proc/meminfo`、EDAC 框架（`/sys/devices/system/edac/mc/`）及内核日志获取内存运行状态。

| 序号 | 指标名称 | 中文名称 | 优先级 | 默认周期 | 默认采集 | 单位 | 数据来源 |
|------|----------|----------|--------|----------|----------|------|----------|
| 2.1 | usage | 内存使用率 | High | 3s | 是 | % | /proc/meminfo |
| 2.2 | swap_usage | Swap使用率 | High | 3s | 是 | % | /proc/meminfo |
| 2.3 | ecc_ce_errors | CE可纠正错误数 | High | 5s | 是 | 次 | /sys/devices/system/edac/mc/mc*/ce_count |
| 2.4 | ecc_uce_errors | UCE不可纠正错误数 | High | 5s | 是 | 次 | /sys/devices/system/edac/mc/mc*/ue_count |
| 2.5 | oom_count | OOM触发次数 | Medium | 30s | 否 | 次 | dmesg / journalctl |
| 2.6 | page_faults | 缺页错误次数 | Low | 10s | 否 | 次/s | /proc/vmstat |

### 指标详情

#### 2.1 usage（内存使用率）

- **数据来源**：`/proc/meminfo`
- **采集方法**：读取 `MemTotal`、`MemAvailable`、`MemFree`、`Cached`、`Buffers`、`Shmem` 等字段，使用率 = (MemTotal - MemAvailable) / MemTotal × 100
- **Labels**：无
- **输出示例**：
```json
{"component":"memory","name":"usage","value":62.5,"unit":"%","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"memory","name":"usage_detail","value":16384,"unit":"MB","labels":{"field":"total"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"memory","name":"usage_detail","value":10240,"unit":"MB","labels":{"field":"used"},"timestamp":"2026-07-10T10:30:00Z"}
{"component":"memory","name":"usage_detail","value":6144,"unit":"MB","labels":{"field":"available"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.2 swap_usage（Swap使用率）

- **数据来源**：`/proc/meminfo`
- **采集方法**：读取 `SwapTotal`、`SwapFree`、`SwapCached`，使用率 = (SwapTotal - SwapFree) / SwapTotal × 100
- **Labels**：无
- **输出示例**：
```json
{"component":"memory","name":"swap_usage","value":15.3,"unit":"%","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.3 ecc_ce_errors（CE可纠正错误数）

- **数据来源**：`/sys/devices/system/edac/mc/mc*/ce_count`
- **采集方法**：遍历 EDAC 框架下各内存控制器（mc0, mc1...）的 `ce_count` 文件，读取累计 CE 错误数。如服务器不支持 EDAC 则返回 0 并记录日志
- **Labels**：`mc`（"mc0", "mc1", ...）
- **输出示例**：
```json
{"component":"memory","name":"ecc_ce_errors","value":3,"unit":"次","labels":{"mc":"mc0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.4 ecc_uce_errors（UCE不可纠正错误数）

- **数据来源**：`/sys/devices/system/edac/mc/mc*/ue_count`
- **采集方法**：遍历 EDAC 框架下各内存控制器的 `ue_count` 文件，读取累计 UCE 错误数
- **Labels**：`mc`（"mc0", "mc1", ...）
- **输出示例**：
```json
{"component":"memory","name":"ecc_uce_errors","value":0,"unit":"次","labels":{"mc":"mc0"},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.5 oom_count（OOM触发次数）

- **数据来源**：`dmesg` 或 `journalctl -k --since` 输出
- **采集方法**：搜索内核日志中 "Out of memory" 或 "Killed process" 关键词，统计最近周期内 OOM Killer 触发次数
- **Labels**：无
- **输出示例**：
```json
{"component":"memory","name":"oom_count","value":0,"unit":"次","labels":{},"timestamp":"2026-07-10T10:30:00Z"}
```

#### 2.6 page_faults（缺页错误次数）

- **数据来源**：`/proc/vmstat`
- **采集方法**：读取 `pgfault`（总缺页）和 `pgmajfault`（主要缺页）字段，差值除以间隔时间得出每秒缺页次数
- **Labels**：`type`（"minor", "major"）
- **输出示例**：
```json
{"component":"memory","name":"page_faults","value":1523,"unit":"次/s","labels":{"type":"minor"},"timestamp":"2026-07-10T10:30:00Z"}
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
| CPU | /proc/stat, /proc/loadavg, /sys/class/thermal/, /sys/devices/system/cpu/, /proc/cpuinfo | 无 |
| Memory | /proc/meminfo, /sys/devices/system/edac/mc/, /proc/vmstat | dmesg 或 journalctl |
| Disk | /proc/mounts, statfs syscall, /proc/diskstats, /proc/stat | smartctl (Phase 3) |
| GPU | nvidia-smi 命令输出 | nvidia-smi |
| NPU | npu-smi 命令输出 | npu-smi |
| Network | /proc/net/dev, /sys/class/net/, /proc/net/tcp, /proc/net/tcp6 | 无 |

---

## 附录B：已实现采集指标清单

> 以下 37 个指标均已实现并通过测试（v0.1.0），按部件分类汇总。

### CPU（7 个）

| 序号 | 指标名称 | 中文名称 | 优先级 | 单位 |
|------|----------|----------|--------|------|
| 1 | usage | CPU使用率 | High | % |
| 2 | load_average | 系统负载 | High | - |
| 3 | temperature | CPU温度 | Medium | °C |
| 4 | frequency | CPU频率 | Medium | MHz |
| 5 | context_switches | 上下文切换次数 | Low | 次/s |
| 6 | process_count | 运行进程数 | Low | 个 |
| 7 | model_info | CPU型号信息 | Low | - |

### Memory（6 个）

| 序号 | 指标名称 | 中文名称 | 优先级 | 单位 |
|------|----------|----------|--------|------|
| 1 | usage | 内存使用率 | High | % |
| 2 | swap_usage | Swap使用率 | High | % |
| 3 | ecc_ce_errors | CE可纠正错误数 | High | 次 |
| 4 | ecc_uce_errors | UCE不可纠正错误数 | High | 次 |
| 5 | oom_count | OOM触发次数 | Medium | 次 |
| 6 | page_faults | 缺页错误次数 | Low | 次/s |

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
| CPU | 7 | 2 | 2 | 3 |
| Memory | 6 | 4 | 1 | 1 |
| Disk | 7 | 1 | 3 | 3 |
| GPU | 7 | 3 | 3 | 1 |
| NPU | 5 | 3 | 2 | 0 |
| Network | 5 | 1 | 3 | 1 |
| **合计** | **37** | **14** | **14** | **9** |
