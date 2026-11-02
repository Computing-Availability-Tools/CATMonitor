# 指标目录（216 项）

> 权威来源：[docs/CATMonitor_indi_list.md](../../../docs/CATMonitor_indi_list.md)（本文件是技能侧导航版，每个指标的采集方法与输出示例以权威清单为准）。

## 汇总统计

| 部件 | 指标数 | High | Medium | Low | 说明 |
|------|--------|------|--------|-----|------|
| cpu | 39 | 4 | 21 | 14 | /proc、/sys、lscpu、ipmitool、mcelog/dmesg |
| memory | 20 | 4 | 11 | 5 | /proc/meminfo、PSI、buddyinfo、EDAC |
| disk | 14 | 1 | 9 | 4 | statfs、/proc/diskstats、smartctl |
| gpu | 8 | 3 | 4 | 1 | nvidia-smi |
| npu | 123 | 11 | 91 | 21 | DCMI/npu-smi/hccn_tool（昇腾） |
| network | 7 | 1 | 5 | 1 | /proc/net/dev、/proc/net/tcp |
| chassis | 5 | 2 | 2 | 1 | ipmitool（需 BMC） |

优先级含义：High 核心运行指标（默认采，3-5s）；Medium 重要辅助（默认采，10-60s）；Low 诊断性（默认不采）。
实际采集受 `collection.min_priority` 配置预过滤。

## 标签约定（同一指标名多行的区分维度）

| 标签 | 适用部件 | 含义 |
|------|----------|------|
| `core` | cpu | 核编号；`core=total` 为总核聚合值 |
| `interval` | cpu | load_average 的窗口：`1m`/`5m`/`15m` |
| `npu_id` | npu | 昇腾设备号（chip 粒度）；健康度卡数统计按唯一 npu_id 计 |
| `interface` | network | 网卡名（eth0 等） |
| `type` | network | 方向/错误类型（如 `rx_err`） |
| `device` / `mount_point` | disk | 块设备与挂载点（space_usage 逐挂载点一行） |

## 常用 High 优先级指标

| 指标 | 单位 | 说明 |
|------|------|------|
| `cpu.usage` | % | CPU 总使用率（`core=total`） |
| `cpu.load_average` | - | 系统负载（interval=1m/5m/15m 三行） |
| `cpu.cpu_ce_errors` / `cpu.cpu_uce_errors` | 次 | CPU 可纠正/不可纠正错误（mcelog/dmesg） |
| `memory.usage` | % | 内存使用率 |
| `memory.saturation` | % | 内存压力（PSI） |
| `memory.ecc_ce_errors` / `ecc_uce_errors` | 次 | EDAC CE/UCE |
| `memory.oom_count` | 次 | OOM 触发次数 |
| `disk.space_usage` | % | 每挂载点一行 |
| `gpu.utilization` / `gpu.temperature` / `gpu.ecc_errors` | %/°C/次 | NVIDIA 使用率/温度/ECC |
| `npu.utilization` | % | AICore 利用率 |
| `npu.memory_usage` | % | HBM 使用率 |
| `npu.temperature` | °C | NPU 温度 |
| `npu.health_status` | - | 健康状态 |
| `npu.card_drop` | - | 掉卡状态（0 正常） |
| `npu.npu_num` | 个 | NPU 设备数量 |
| `npu.error_code` | - | 设备错误码 |
| `network.throughput` | MB/s | 网络吞吐 |
| `chassis.power` | W | 整机功耗（需 BMC） |

## 各部件指标名速查

**cpu（39）**：usage, load_average, temperature, frequency, context_switches, process_count, model_info, user_time, nice_time, system_time, idle_time, iowait_time, irq_time, softirq_time, steal_time, user_util, system_util, idle_util, iowait_util, numa_node_num, online_core_num, offline_core_num, isolated_core_num, mem_temperature, core_num, numa_core_num, cpu_num, avg_freq, min_freq, max_freq, cpu_ce_errors, cpu_uce_errors, power, l1d_cache_size, l1i_cache_size, l2_cache_size, l3_cache_size, numa_order_num, numa_info

**memory（20）**：usage, swap_usage, swap_detail, swap_in, swap_out, saturation, fragmentation, ecc_ce_errors, ecc_uce_errors, oom_count, page_faults, isolated_pages, isolated_anon_pages, isolated_file_pages, free_pages, module_num, module_size, module_info, power

**disk（14）**：space_usage, iops, throughput, read_latency, write_latency, io_wait, smart_status, smart_temperature, io_errors, read_sectors_total, written_sectors_total, read_time_total, write_time_total

**gpu（8）**：utilization, memory_usage, temperature, power_draw, fan_speed, ecc_errors, clock_frequency, memory_detail

**npu（123）**：utilization, memory_usage, temperature, power_draw, health_status, npu_num, chip_type, driver_version, driver_health, error_code, card_drop, process_info, process_total, comm_topo, voltage, aicore_voltage, hybrid_voltage, cpu_voltage, ddr_voltage, acg_count, fan_speed, hbm_temp, cluster_temp, peri_temp, aicore0_temp, aicore1_temp, ntc1_temp~ntc4_temp, soc_max_temp, fp_max_temp, ndie_temp, hbm_max_temp, aicpu_freq, aicore_rated_freq, aicore_freq, ctrlcpu_freq, vector_core_freq, hbm_freq, ddr_freq, npu_util, aicpu_util, ctrlcpu_util, vector_core_util, hbm_bandwidth_util, ddr_util, ddr_bandwidth_util, vdec_util, vpc_util, venc_util, jpege_util, jpegd_util, hbm_total_memory, hbm_used_memory, hbm_single_ecc, hbm_double_ecc, hbm_single_ecc_isolated, hbm_double_ecc_isolated, ddr_single_ecc, ddr_double_ecc, ddr_single_ecc_isolated, ddr_double_ecc_isolated, llc_write_hit_rate, llc_read_hit_rate, llc_throughput, net_tx_bandwidth, net_rx_bandwidth, roce_link_status, roce_speed_status, roce_link_health, pcie_tx_bandwidth, pcie_rx_bandwidth, hccs_tx_bandwidth, hccs_rx_bandwidth

**network（7）**：throughput, packet_count, error_count, interface_status, connection_count, rx_bytes_total, tx_bytes_total

**chassis（5，需 BMC）**：power, inlet_temp, outlet_temp, fan_speed, fan_power

## NPU 常用子温度与利用率说明

- 温度族：`temperature`（主）、`hbm_temp`、`cluster_temp`、`soc_max_temp`、`hbm_max_temp` 等，健康度取最差值评估。
- 利用率族：`utilization`（AICore）、`npu_util`（整体）、`aicpu_util`、`vector_core_util`、`hbm_bandwidth_util` 等。
- ECC：`hbm_single_ecc`/`hbm_double_ecc`（单/多 bit）、`*_ecc_isolated`（隔离页数）；`*_double_ecc` 即 UCE，属严重事件。
- RoCE 链路：`roce_link_status`/`roce_link_health`，与 catmonitor-faults 技能的 `roce_link_down` 事件对应。
