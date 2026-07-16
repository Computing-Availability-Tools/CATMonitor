# 能效相关指标清单

> 本文档汇总 CATMonitor 中与服务器能效（energy efficiency）相关的指标，
> 按部件分类列出指标序号、名称、单位及数据来源，便于能效分析场景查阅。

---

## 1. NPU 能效指标（46 项）

### 1.1 频率（7 项）

| 指标序号 | 指标名称 | 中文名称 | 单位 | 数据来源 |
|:--------:|---------|---------|:----:|---------|
| 5.34 | aicpu_freq | AICPU频率 | MHz | DCMI dcmi_get_aicpu_info |
| 5.35 | aicore_rated_freq | AICore额定频率 | MHz | DCMI dcmi_get_device_frequency(AICORE_MAX) |
| 5.36 | aicore_freq | AICore频率 | MHz | DCMI dcmi_get_device_frequency(AICORE_CURRENT) |
| 5.37 | ctrlcpu_freq | CTRLCPU频率 | MHz | DCMI dcmi_get_device_frequency(CTRLCPU) |
| 5.38 | vector_core_freq | Vector Core频率 | MHz | DCMI dcmi_get_device_frequency(VECTORCORE) |
| 5.39 | hbm_freq | HBM频率 | MHz | DCMI dcmi_get_device_frequency(HBM) |
| 5.40 | ddr_freq | DDR频率(内存频率) | MHz | DCMI dcmi_get_device_frequency(DDR) |

### 1.2 利用率（14 项）

| 指标序号 | 指标名称 | 中文名称 | 单位 | 数据来源 |
|:--------:|---------|---------|:----:|---------|
| 5.1 | utilization | AICore利用率 | % | DCMI dcmi_get_device_utilization_rate(AICORE) |
| 5.2 | memory_usage | HBM利用率(显存使用率) | % | DCMI dcmi_get_device_hbm_info |
| 5.41 | npu_util | NPU利用率(整体) | % | DCMI dcmi_get_device_utilization_rate(NPU) |
| 5.42 | aicpu_util | AICPU利用率 | % | DCMI dcmi_get_device_utilization_rate(AICPU) |
| 5.43 | ctrlcpu_util | CTRLCPU利用率 | % | DCMI dcmi_get_device_utilization_rate(CTRLCPU) |
| 5.44 | vector_core_util | Vector Core利用率 | % | DCMI dcmi_get_device_utilization_rate(VECTORCORE) |
| 5.45 | hbm_bandwidth_util | HBM带宽利用率 | % | DCMI dcmi_get_device_utilization_rate(HBM_BANDWIDTH) |
| 5.46 | ddr_util | DDR利用率(内存利用率) | % | DCMI dcmi_get_device_utilization_rate(DDR) |
| 5.47 | ddr_bandwidth_util | DDR带宽利用率(内存带宽利用率) | % | DCMI dcmi_get_device_utilization_rate(DDR_BANDWIDTH) |
| 5.48 | vdec_util | VDEC利用率 | % | DCMI dcmi_get_device_dvpp_ratio_info |
| 5.49 | vpc_util | VPC利用率 | % | DCMI dcmi_get_device_dvpp_ratio_info |
| 5.50 | venc_util | VENC利用率 | % | DCMI dcmi_get_device_dvpp_ratio_info |
| 5.51 | jpege_util | JPEGE利用率 | % | DCMI dcmi_get_device_dvpp_ratio_info |
| 5.52 | jpegd_util | JPEGD利用率 | % | DCMI dcmi_get_device_dvpp_ratio_info |

### 1.3 温度（14 项）

| 指标序号 | 指标名称 | 中文名称 | 单位 | 数据来源 |
|:--------:|---------|---------|:----:|---------|
| 5.3 | temperature | NPU温度 | °C | DCMI dcmi_get_device_temperature |
| 5.21 | hbm_temp | HBM温度 | °C | DCMI dcmi_get_device_hbm_info.temp |
| 5.22 | cluster_temp | CLUSTER温度 | °C | DCMI dcmi_get_device_sensor_info(CLUSTER) |
| 5.23 | peri_temp | PERI温度 | °C | DCMI dcmi_get_device_sensor_info(PERI) |
| 5.24 | aicore0_temp | AICORE0温度 | °C | DCMI dcmi_get_device_sensor_info(AICORE0) |
| 5.25 | aicore1_temp | AICORE1温度 | °C | DCMI dcmi_get_device_sensor_info(AICORE1) |
| 5.26 | ntc1_temp | 热敏电阻1温度 | °C | DCMI dcmi_get_device_sensor_info(NTC).ntc_tmp[0] |
| 5.27 | ntc2_temp | 热敏电阻2温度 | °C | DCMI dcmi_get_device_sensor_info(NTC).ntc_tmp[1] |
| 5.28 | ntc3_temp | 热敏电阻3温度 | °C | DCMI dcmi_get_device_sensor_info(NTC).ntc_tmp[2] |
| 5.29 | ntc4_temp | 热敏电阻4温度 | °C | DCMI dcmi_get_device_sensor_info(NTC).ntc_tmp[3] |
| 5.30 | soc_max_temp | SOC最高温 | °C | DCMI dcmi_get_device_sensor_info(SOC) |
| 5.31 | fp_max_temp | 光模块最高温度 | °C | DCMI dcmi_get_device_sensor_info(FP) |
| 5.32 | ndie_temp | NDIE温度 | °C | DCMI dcmi_get_device_sensor_info(N_DIE) |
| 5.33 | hbm_max_temp | HBM最高温度 | °C | DCMI dcmi_get_device_sensor_info(HBM) |

### 1.4 电压与功耗（7 项）

| 指标序号 | 指标名称 | 中文名称 | 单位 | 数据来源 |
|:--------:|---------|---------|:----:|---------|
| 5.4 | power_draw | NPU功耗 | W | DCMI dcmi_get_device_power_info |
| 5.14 | voltage | NPU电压 | V | DCMI dcmi_get_device_voltage |
| 5.15 | aicore_voltage | AICORE电压电流寄存器值 | V | DCMI dcmi_get_device_info(LP/AICORE_VOLTAGE) |
| 5.16 | hybrid_voltage | HYBIRD电压电流寄存器值 | V | DCMI dcmi_get_device_info(LP/HYBIRD_VOLTAGE) |
| 5.17 | cpu_voltage | CPU电压电流寄存器值 | V | DCMI dcmi_get_device_info(LP/TAISHAN_VOLTAGE) |
| 5.18 | ddr_voltage | DDR电压电流寄存器值 | V | DCMI dcmi_get_device_info(LP/DDR_VOLTAGE) |
| 5.19 | acg_count | ACG调频计数值 | 次 | DCMI dcmi_get_device_info(LP/ACG) |

### 1.5 风扇（1 项）

| 指标序号 | 指标名称 | 中文名称 | 单位 | 数据来源 |
|:--------:|---------|---------|:----:|---------|
| 5.20 | fan_speed | 风扇实际转速 | % | DCMI dcmi_get_device_fan_count+speed |

### 1.6 LLC 性能（3 项）

| 指标序号 | 指标名称 | 中文名称 | 单位 | 数据来源 |
|:--------:|---------|---------|:----:|---------|
| 5.63 | llc_write_hit_rate | LLC写命中率 | % | DCMI dcmi_get_device_llc_perf_para.wr_hit_rate |
| 5.64 | llc_read_hit_rate | LLC读命中率 | % | DCMI dcmi_get_device_llc_perf_para.rd_hit_rate |
| 5.65 | llc_throughput | LLC吞吐量 | MB/s | DCMI dcmi_get_device_llc_perf_para.throughput |

---

## 2. CPU 能效指标（10 项）

### 2.1 CPU 时间分解（8 项）

| 指标序号 | 指标名称 | 中文名称 | 单位 | 数据来源 |
|:--------:|---------|---------|:----:|---------|
| 1.8 | user_time | 用户态运行时间 | jiffies | /proc/stat |
| 1.9 | nice_time | nice(低优先级用户态) | jiffies | /proc/stat |
| 1.10 | system_time | 内核态运行时间 | jiffies | /proc/stat |
| 1.11 | idle_time | 空闲时间 | jiffies | /proc/stat |
| 1.12 | iowait_time | iowait(等待IO时间) | jiffies | /proc/stat |
| 1.13 | irq_time | 硬中断处理时间 | jiffies | /proc/stat |
| 1.14 | softirq_time | 软中断处理时间 | jiffies | /proc/stat |
| 1.15 | steal_time | steal(被窃取时间) | jiffies | /proc/stat |

### 2.2 负载（1 项，3 个 interval）

| 指标序号 | 指标名称 | Labels | 中文名称 | 数据来源 |
|:--------:|---------|--------|---------|---------|
| 1.2 | load_average | interval=1m | 1分钟平均负载率 | /proc/loadavg |
| 1.2 | load_average | interval=5m | 5分钟平均负载率 | /proc/loadavg |
| 1.2 | load_average | interval=15m | 15分钟平均负载率 | /proc/loadavg |

### 2.3 CPU 功耗（1 项）

| 指标序号 | 指标名称 | 中文名称 | 单位 | 数据来源 |
|:--------:|---------|---------|:----:|---------|
| 1.34 | power | CPU功率 | W | ipmitool SDR "CPU* Pwr" |

---

## 3. Memory 能效指标（7 项）

| 指标序号 | 指标名称 | Labels.field | 中文名称 | 单位 | 数据来源 |
|:--------:|---------|:------------:|---------|:----:|---------|
| 2.1 | usage_detail | total | mem total(内存总容量) | MB | /proc/meminfo MemTotal |
| 2.1 | usage_detail | free | mem free(空闲内存) | MB | /proc/meminfo MemFree |
| 2.1 | usage_detail | buffers | mem buffers(缓冲区) | MB | /proc/meminfo Buffers |
| 2.1 | usage_detail | cached | mem cached(缓存) | MB | /proc/meminfo Cached |
| 2.1 | usage_detail | sreclaimable | mem sreclaimable(可回收slab) | MB | /proc/meminfo SReclaimable |
| 2.3 | swap_detail | total | swap total(交换区总量) | MB | /proc/meminfo SwapTotal |
| 2.3 | swap_detail | free | swap free(交换区空闲) | MB | /proc/meminfo SwapFree |

---

## 4. Disk 能效指标（4 项）

| 指标序号 | 指标名称 | 中文名称 | 单位 | 数据来源 | 说明 |
|:--------:|---------|---------|:----:|---------|------|
| 3.3 | throughput | 读写吞吐量(含 sectors read/written) | MB/s | /proc/diskstats fields 6,10 | sectors read/written 是中间值，通过 throughput 指标以 MB/s 输出 |
| 3.4 | read_latency | 读耗时(time spent reading) | ms/s | /proc/diskstats field 7 | 两次差值除以间隔 |
| 3.5 | write_latency | 写耗时(time spent writing) | ms/s | /proc/diskstats field 11 | 两次差值除以间隔 |
| 3.2 | iops | 读写IOPS | 次/s | /proc/diskstats fields 4,8 | 读写完成次数差值 |

> 注：`sectors read` 和 `sectors written` 是 /proc/diskstats 的原始字段（fields 6 和 10），代码内部读取后用于计算 throughput（MB/s），未作为独立指标输出。如需原始扇区数，可从 throughput 反推或后续新增独立指标。

---

## 5. Network 能效指标（2 项）

| 指标名称 | 中文名称 | 单位 | 数据来源 | 说明 |
|---------|---------|:----:|---------|------|
| rx_bytes_total | 总接收字节数 | bytes | /proc/net/dev | 代码已实现，输出绝对值；CATMonitor_indi_list.md 未单列（归在 throughput 关联指标） |
| tx_bytes_total | 总发送字节数 | bytes | /proc/net/dev | 同上 |

> 注：这两个指标在 network collector 代码中已实现并输出，但在 CATMonitor_indi_list.md 的网络小节表格中未列为独立行（文档后续可补充）。

---

## 6. Chassis 能效指标（5 项）

| 指标序号 | 指标名称 | 中文名称 | 单位 | 数据来源 |
|:--------:|---------|---------|:----:|---------|
| 7.1 | power | 整机功率 | W | ipmitool SDR "Power"（整机总功耗含风扇功耗） |
| 7.2 | inlet_temp | 进风口温度 | °C | ipmitool SDR "Inlet Temp" |
| 7.3 | outlet_temp | 出风口温度 | °C | ipmitool SDR "Outlet Temp" |
| 7.4 | fan_speed | 风扇转速 | RPM | ipmitool SDR "FAN* Speed" |
| 7.5 | fan_power | 风扇功耗 | W | ipmitool SDR "FAN* Power" |

---

## 汇总

| 部件 | 能效指标数 | 覆盖维度 |
|------|:---------:|---------|
| NPU | 46 | 频率、利用率、温度、电压、功耗、风扇、LLC |
| CPU | 10 | 时间分解、负载、功耗 |
| Memory | 7 | 内存池原始值、Swap |
| Disk | 4 | 吞吐量、读写耗时、IOPS |
| Network | 2 | 收发字节数 |
| Chassis | 5 | 整机功耗、进出风温度、风扇转速/功耗 |
| **合计** | **74** | |
