// ---- display manifest (optional hints; unknown components render generically) ----
const MANIFEST = {
  cpu: { title: 'CPU', headline: 'cpu_usage', headlineLabel: 'CPU 使用率 (%)',
         key: [ {name:'usage', prefer:{core:'total'}}, 'load_average', 'avg_freq',
                'temperature', 'power', 'cpu_ce_errors', 'model_info' ] },
  memory: { title: '内存', headline: 'memory_usage', headlineLabel: '内存使用率 (%)',
            key: [ 'usage', 'swap_usage', 'saturation', 'fragmentation',
                   'module_num', 'ecc_ce_errors', 'oom_count', 'page_faults' ] },
  disk: { title: '磁盘', headline: 'disk_space_usage', headlineLabel: '分区空间使用率最高 (%)',
          key: [ 'space_usage', 'throughput', 'iops', 'io_wait',
                 'io_errors', 'smart_status' ] },
  gpu: { title: 'GPU', headline: 'gpu_utilization', headlineLabel: 'GPU 使用率最高 (%)',
         key: [ 'utilization', 'memory_usage', 'temperature', 'power_draw' ] },
  npu: { title: 'NPU', headline: 'npu_utilization', headlineLabel: 'NPU 使用率最高 (%)',
         key: [ 'utilization', 'memory_usage', 'temperature', 'power_draw' ] },
  network: { title: '网络', headline: null,
             key: [ 'throughput', 'packet_count', 'error_count', 'connection_count' ] },
  chassis: { title: '机箱', headline: null,
             key: [ 'power', 'inlet_temp', 'outlet_temp', 'fan_speed', 'fan_power' ] },
};

const METRIC_NAMES = {
  usage: '使用率', load_average: '负载', context_switches: '上下文切换',
  process_count: '进程数', model_info: '型号', temperature: '温度', frequency: '频率',
  space_usage: '分区空间使用率', space_detail: '分区空间明细', throughput: '吞吐量',
  io_wait: 'IO Wait', io_errors: 'IO 错误', iops: 'IOPS',
  smart_status: 'SMART 状态', smart_temperature: 'SMART 温度',
  memory_usage: '显存使用率', memory_detail: '明细',
  power_draw: '功耗', fan_speed: '风扇转速', ecc_errors: 'ECC 错误',
  inlet_temp: '进风口温度', outlet_temp: '出风口温度', fan_power: '风扇功率',
  clock_frequency: '频率', utilization: '使用率', health_status: '健康状态',
  swap_usage: 'Swap 使用率', swap_detail: 'Swap 明细', oom_count: 'OOM 次数', page_faults: '页错误',
  rx_bytes_total: '接收字节', tx_bytes_total: '发送字节',
  error_count: '错误计数', connection_count: '连接数',
  interface_status: '接口状态', usage_detail: '明细', packet_count: '包计数',
  // v0.2.0 CPU source-layer metrics.
  user_time: '用户时间', nice_time: 'Nice 时间', system_time: '系统时间',
  idle_time: '空闲时间', iowait_time: 'IO 等待时间', irq_time: '中断时间',
  softirq_time: '软中断时间', steal_time: '抢占时间',
  user_util: '用户使用率', system_util: '系统使用率', idle_util: '空闲率', iowait_util: 'IO 等待率',
  avg_freq: '平均频率', min_freq: '最小频率', max_freq: '最大频率',
  numa_node_num: 'NUMA 节点数', core_num: '物理核数',
  numa_core_num: 'NUMA 核数', cpu_num: 'CPU 数',
  online_core_num: '在线核数', offline_core_num: '离线核数', isolated_core_num: '隔离核数',
  l1d_cache_size: 'L1d 缓存', l1i_cache_size: 'L1i 缓存', l2_cache_size: 'L2 缓存', l3_cache_size: 'L3 缓存',
  numa_order_num: 'NUMA 阶数', numa_info: 'NUMA 最高阶',
  cpu_ce_errors: 'CPU CE 错误', cpu_uce_errors: 'CPU UCE 错误',
  mem_temperature: '内存温度', power: '功耗',
  // v0.2.0 Memory source-layer metrics.
  swap_in: 'Swap 入', swap_out: 'Swap 出',
  saturation: '内存压力', fragmentation: '碎片化',
  isolated_pages: '隔离页', isolated_anon_pages: '隔离匿名页',
  isolated_file_pages: '隔离文件页', free_pages: '空闲页',
  module_size: '内存条容量', module_info: '内存条信息', module_num: '内存条数',
  ecc_ce_errors: 'ECC CE 错误', ecc_uce_errors: 'ECC UCE 错误',
  // Hardware identity (system collector, one-shot).
  device_model: '设备型号', os_info: 'OS 信息', gpu_info: 'GPU 信息', npu_info: 'NPU 信息',
  disk_info: '磁盘信息', net_info: '网卡信息',
};

// Label key -> Chinese display name, for the detail-page hardware-specs panel.
const LABEL_NAMES = {
  manufacturer: '厂商', product_name: '型号', version: '版本', serial_number: '序列号',
  name: '名称', uuid: 'UUID', driver_version: '驱动版本', bus_id: '总线',
  model: '型号', serial: '序列号', size: '容量', interface: '接口', firmware: '固件',
  mac: 'MAC', mtu: 'MTU', speed: '速率', driver: '驱动',
  locator: '插槽', type: '类型',
  model_name: '型号', cache_size: '缓存', core: '核心', node: '节点', die: 'Die',
  pretty_name: 'OS', version_id: '版本号', kernel: '内核',
  npu_id: 'NPU ID', chip_id: '芯片 ID',
  pci_addr: 'PCI 地址', pci_device: '设备型号',
};

const SERVER_TYPE_TEXT = {
  cpu_only: '通用服务器',
  accelerated: 'AI 服务器（含 NPU/GPU）',
};

const RULE_TEXT = {
  // CPU
  'usage>90%': 'CPU 使用率超过 90%',
  'usage>80%': 'CPU 使用率超过 80%',
  'temp>85C':  'CPU 温度超过 85°C',
  'temp>75C':  'CPU 温度超过 75°C',
  'load>cores*2': '系统负载超过核心数 × 2',
  'cpu_ce_error':  'CPU 发生可纠正 ECC 错误',
  'cpu_uce_error': 'CPU 发生不可纠正 ECC 错误',
  // Memory
  'usage>90%':       '内存使用率超过 90%',
  'usage>80%':       '内存使用率超过 80%',
  'swap>50%':       '交换分区使用率超过 50%',
  'ce_error':       '内存可纠正 ECC 错误',
  'uce_error':      '内存不可纠正 ECC 错误',
  'saturation>80%': '内存饱和度超过 80%',
  'fragmentation>80%': '内存碎片率超过 80%',
  // Disk
  'space>90%':   '磁盘空间使用率超过 90%',
  'space>80%':   '磁盘空间使用率超过 80%',
  'io_wait>20%': '磁盘 IO 等待超过 20%',
  'smart_failed': 'SMART 健康检查未通过',
  // GPU
  'mem>95%':    '显存使用率超过 95%',
  'util>95%':   '利用率超过 95%',
  'ecc_error':  '发生 ECC 错误',
  // NPU
  'card_drop':       'NPU 卡掉线',
  'health_alarm':    'NPU 健康状态告警',
  'health_warning':  'NPU 健康状态预警',
  'hbm_double_ecc':  'HBM 发生双比特 ECC 错误',
  'ddr_double_ecc':  'DDR 发生双比特 ECC 错误',
  'hbm_single_ecc':  'HBM 发生单比特 ECC 错误',
  'ddr_single_ecc':  'DDR 发生单比特 ECC 错误',
  'error_code':      'NPU 存在错误代码',
  // Network
  'error_count>100': '网络错误包数超过 100',
  'error_count>10':  '网络错误包数超过 10',
  'time_wait>2000':  'TIME_WAIT 连接数超过 2000',
  'estab>5000':     'ESTABLISHED 连接数超过 5000',
  'estab>3000':     'ESTABLISHED 连接数超过 3000',
  // Chassis
  'inlet_temp>40':  '进风口温度超过 40°C',
  'inlet_temp>35':  '进风口温度超过 35°C',
  'outlet_temp>60': '出风口温度超过 60°C',
  'outlet_temp>50': '出风口温度超过 50°C',
};

// Maps a static spec metric name to (display type, the label key that holds
// the primary identity shown in the "标识" column of the specs panel + the
// overview card spec summary.
const SPEC_DEFS = {
  device_model: { type: '设备', primary: 'product_name' },
  os_info:      { type: 'OS', primary: 'pretty_name' },
  model_info:   { type: 'CPU', primary: 'model_name' },
  gpu_info:     { type: 'GPU', primary: 'name' },
  npu_info:     { type: 'NPU', primary: 'name' },
  disk_info:    { type: '磁盘', primary: 'model' },
  net_info:     { type: '网卡', primary: 'interface' },
  module_info:  { type: '内存条', primary: 'locator' },
};

const CPU_SPEC_ORDER = {
  model_info: 0, cpu_num: 1, core_num: 2,
  min_freq: 3, max_freq: 4,
  l1d_cache_size: 5, l1i_cache_size: 6, l2_cache_size: 7, l3_cache_size: 8,
  numa_node_num: 9, numa_core_num: 10,
};

const SERIES_LABELS = {
  cpu_usage: 'CPU 使用率 (%)', cpu_load_average: '系统负载 1m',
  memory_usage: '内存使用率 (%)', memory_swap_usage: 'Swap 使用率 (%)',
  disk_space_usage: '分区空间使用率最高 (%)',
  gpu_utilization: 'GPU 使用率最高 (%)', gpu_memory_usage: 'GPU 显存使用率最高 (%)', gpu_temperature: 'GPU 最高温度 (°C)',
  npu_utilization: 'NPU 使用率最高 (%)', npu_memory_usage: 'NPU 显存使用率最高 (%)', npu_temperature: 'NPU 最高温度 (°C)', npu_power_draw: 'NPU 功耗 (W)',
  // v0.2.0 trends.
  cpu_temperature: 'CPU 最高温度 (°C)',
  cpu_avg_freq: 'CPU 平均频率 (MHz)',
  memory_swap_in: 'Swap 入页 (次/s)', memory_fragmentation: '内存碎片化最大 (%)',
  disk_io_wait: 'IO Wait (%)', disk_iops: '磁盘 IOPS 最大 (次/s)', disk_throughput: '磁盘吞吐最大 (MB/s)',
  network_throughput: '网络吞吐最大 (bytes/s)', network_packet_count: '网络包速率最大 (个/s)',
  network_error_packets: '网络错包最大 (次)', network_dropped_packets: '网络丢包最大 (次)',
};

const METRIC_DESCRIPTIONS = {
  // CPU
  'cpu:usage': 'CPU 使用率',
  load_average: '系统平均负载，过去 N 分钟运行队列平均进程数',
  'cpu:temperature': 'CPU 温度',
  frequency: '每核当前频率',
  context_switches: '上下文切换次数，每秒切换次数',
  process_count: '运行进程数',
  model_info: 'CPU 型号信息',
  user_time: '用户态运行时间，用户程序占用的 CPU 时间',
  nice_time: '低优先级用户进程时间',
  system_time: '内核态运行时间，内核程序占用的 CPU 时间',
  idle_time: '空闲时间，CPU 未执行任何任务的时间',
  iowait_time: '等待 IO 时间，CPU 等待磁盘 IO 完成的时间',
  irq_time: '硬中断处理时间',
  softirq_time: '软中断处理时间',
  steal_time: '虚拟化被窃取时间，被 Hypervisor 占用的时间',
  user_util: '用户态平均利用率',
  system_util: '内核态平均利用率',
  idle_util: '空闲占比',
  iowait_util: 'IO 等待占比',
  numa_node_num: 'NUMA 节点数量',
  online_core_num: '在线核心数',
  offline_core_num: '离线核心数',
  isolated_core_num: '隔离核心数，被 isolcpus 隔离的核数',
  mem_temperature: 'CPU 内存区域温度',
  core_num: '物理核总数',
  numa_core_num: '每个 NUMA 节点的核数',
  cpu_num: 'CPU 个数（路数），物理 CPU 封装数',
  avg_freq: '所有在线核心当前频率的算术平均',
  min_freq: 'CPU 最小频率（硬件最低频率）',
  max_freq: 'CPU 最大频率（硬件最高频率）',
  cpu_ce_errors: 'CPU 可纠正 ECC 错误数',
  cpu_uce_errors: 'CPU 不可纠正 ECC 错误数',
  power: 'CPU 功率',
  l1d_cache_size: 'L1d 缓存大小（数据缓存）',
  l1i_cache_size: 'L1i 缓存大小（指令缓存）',
  l2_cache_size: 'L2 缓存大小',
  l3_cache_size: 'L3 缓存大小',
  numa_order_num: 'NUMA 节点 buddy order 数量，可用内存块规格数',
  numa_info: 'NUMA 节点最大可用连续 order，值越大碎片越少',
  // Memory
  'memory:usage': '内存使用率',
  swap_usage: 'Swap 使用率',
  swap_detail: 'Swap 原始值（总量/已用/空闲）',
  swap_in: '换入页数，每秒从 Swap 换入内存的页数',
  swap_out: '换出页数，每秒从内存换出到 Swap 的页数',
  saturation: '内存饱和度，因内存压力导致任务阻塞的时间占比',
  fragmentation: '内存碎片化程度，order 0 空闲页占比',
  ecc_ce_errors: '内存可纠正 ECC 错误数',
  ecc_uce_errors: '内存不可纠正 ECC 错误数',
  oom_count: 'OOM 触发次数，内核 OOM Killer 杀进程的次数',
  page_faults: '缺页错误次数，每秒缺页次数',
  isolated_pages: '隔离页总数，正在迁移或 offline 被临时隔离的页',
  isolated_anon_pages: '隔离匿名页数',
  isolated_file_pages: '隔离文件页数',
  free_pages: '空闲页数',
  module_num: '内存条数量',
  module_size: '内存条容量',
  module_info: '内存条静态信息（型号/速率/厂商/类型）',
  usage_detail: '内存明细（总量/已用/空闲/Buffers/Cached 等）',
  // Disk
  space_usage: '分区空间使用率',
  space_detail: '分区空间明细（总量/已用/可用）',
  iops: '每秒读写 IOPS（输入输出操作数）',
  'disk:throughput': '读写吞吐量',
  read_latency: '读耗时，每秒读 IO 花费的时间',
  write_latency: '写耗时，每秒写 IO 花费的时间',
  io_wait: 'IO 等待占比，CPU 等待磁盘 IO 的时间占比',
  smart_status: 'SMART 健康状态（PASSED/FAILED）',
  smart_temperature: '硬盘温度',
  io_errors: 'IO 错误计数',
  read_sectors_total: '读扇区总数',
  written_sectors_total: '写扇区总数',
  read_time_total: '读耗时总计（累计）',
  write_time_total: '写耗时总计（累计）',
  // GPU
  utilization: 'GPU 使用率',
  memory_usage: 'GPU 显存使用率',
  'gpu:temperature': 'GPU 温度',
  power_draw: 'GPU 功耗',
  fan_speed: '风扇转速',
  ecc_errors: 'GPU ECC 错误数',
  clock_frequency: 'GPU 时钟频率',
  memory_detail: 'GPU 显存明细（已用/总量）',
  // NPU
  health_status: 'NPU 健康状态码（OK/Warning/Alarm/Critical）',
  npu_num: 'NPU 设备数量',
  chip_type: 'NPU 芯片类型（如 Ascend910A）',
  driver_version: 'NPU 驱动版本',
  driver_health: 'NPU 驱动健康状态',
  error_code: 'NPU 错误码，设备级完整错误码列表',
  process_info: 'NPU 进程 PID 列表',
  process_total: 'NPU 进程总数',
  comm_topo: 'NPU 通信拓扑（如 8*HCCS-Link）',
  voltage: 'NPU 主电压',
  aicore_voltage: 'AICore 电压，AI 运算核心电压',
  hybrid_voltage: 'Hybrid 电压',
  cpu_voltage: 'CPU 电压，NPU 内部 CPU（泰山）电压',
  ddr_voltage: 'DDR 电压',
  acg_count: 'ACG 调频累计计数',
  hbm_temp: 'HBM 温度（高带宽内存）',
  cluster_temp: 'Cluster 温度',
  peri_temp: '外设区温度',
  aicore0_temp: 'AICORE0 温度',
  aicore1_temp: 'AICORE1 温度',
  ntc1_temp: '热敏电阻 1 温度',
  ntc2_temp: '热敏电阻 2 温度',
  ntc3_temp: '热敏电阻 3 温度',
  ntc4_temp: '热敏电阻 4 温度',
  soc_max_temp: 'SOC 最高温度',
  fp_max_temp: '光模块最高温度',
  ndie_temp: 'NDie 温度',
  hbm_max_temp: 'HBM 最高温度',
  aicpu_freq: 'AICPU 频率，AI 处理器频率',
  aicore_rated_freq: 'AICore 额定频率（最大频率）',
  aicore_freq: 'AICore 当前频率，AI 核心运算单元的运行频率',
  ctrlcpu_freq: 'CTRLCPU 频率，NPU 控制单元的运行频率',
  vector_core_freq: 'Vector Core 频率，向量运算核心频率',
  hbm_freq: 'HBM 频率，高带宽内存频率',
  ddr_freq: 'DDR 频率',
  npu_util: 'NPU 整体利用率',
  aicpu_util: 'AICPU 利用率',
  ctrlcpu_util: 'CTRLCPU 利用率，控制单元利用率',
  vector_core_util: 'Vector Core 利用率，向量运算核心利用率',
  hbm_bandwidth_util: 'HBM 带宽利用率',
  ddr_util: 'DDR 利用率',
  ddr_bandwidth_util: 'DDR 带宽利用率',
  vdec_util: '视频解码单元 VDEC 利用率',
  vpc_util: '视频处理单元 VPC 利用率',
  venc_util: '视频编码单元 VENC 利用率',
  jpege_util: 'JPEG 编码单元利用率',
  jpegd_util: 'JPEG 解码单元利用率',
  hbm_total_memory: 'HBM 总容量（高带宽内存总量）',
  hbm_used_memory: 'HBM 已用容量',
  hbm_single_ecc: 'HBM 单比特 ECC 错误（可纠正）',
  hbm_double_ecc: 'HBM 双比特 ECC 错误（不可纠正）',
  hbm_single_ecc_isolated: 'HBM 单比特错误隔离页数',
  hbm_double_ecc_isolated: 'HBM 双比特错误隔离页数',
  ddr_single_ecc: 'DDR 单比特 ECC 错误（可纠正）',
  ddr_double_ecc: 'DDR 双比特 ECC 错误（不可纠正）',
  ddr_single_ecc_isolated: 'DDR 单比特错误隔离页数',
  ddr_double_ecc_isolated: 'DDR 双比特错误隔离页数',
  llc_write_hit_rate: 'LLC 写命中率（末级缓存）',
  llc_read_hit_rate: 'LLC 读命中率（末级缓存）',
  llc_throughput: 'LLC 吞吐量（末级缓存）',
  net_tx_bandwidth: 'NPU 网口发送带宽',
  net_rx_bandwidth: 'NPU 网口接收带宽',
  roce_link_status: 'RoCE 连接状态（up/down）',
  roce_speed_status: 'RoCE 连接速度（如 100Gbps）',
  roce_link_health: 'RoCE 链路状态',
  pcie_tx_bandwidth: 'PCIe 发送带宽',
  pcie_rx_bandwidth: 'PCIe 接收带宽',
  hccs_tx_bandwidth: 'HCCS 发送带宽（卡间高速互联）',
  hccs_rx_bandwidth: 'HCCS 接收带宽（卡间高速互联）',
  card_drop: 'NPU 卡掉线状态，1=掉卡 0=正常',
  mac_tx_mac_pause_num: 'MAC 发送 pause 帧数',
  mac_rx_mac_pause_num: 'MAC 接收 pause 帧数',
  mac_tx_pfc_pkt_num: 'MAC 发送 PFC 帧总数（优先级流量控制）',
  mac_tx_pfc_pri0_pkt_num: 'MAC 0 号队列发送 PFC 帧数',
  mac_tx_pfc_pri1_pkt_num: 'MAC 1 号队列发送 PFC 帧数',
  mac_tx_pfc_pri2_pkt_num: 'MAC 2 号队列发送 PFC 帧数',
  mac_tx_pfc_pri3_pkt_num: 'MAC 3 号队列发送 PFC 帧数',
  mac_tx_pfc_pri4_pkt_num: 'MAC 4 号队列发送 PFC 帧数',
  mac_tx_pfc_pri5_pkt_num: 'MAC 5 号队列发送 PFC 帧数',
  mac_tx_pfc_pri6_pkt_num: 'MAC 6 号队列发送 PFC 帧数',
  mac_tx_pfc_pri7_pkt_num: 'MAC 7 号队列发送 PFC 帧数',
  mac_rx_pfc_pkt_num: 'MAC 接收 PFC 帧总数（优先级流量控制）',
  mac_rx_pfc_pri0_pkt_num: 'MAC 0 号队列接收 PFC 帧数',
  mac_rx_pfc_pri1_pkt_num: 'MAC 1 号队列接收 PFC 帧数',
  mac_rx_pfc_pri2_pkt_num: 'MAC 2 号队列接收 PFC 帧数',
  mac_rx_pfc_pri3_pkt_num: 'MAC 3 号队列接收 PFC 帧数',
  mac_rx_pfc_pri4_pkt_num: 'MAC 4 号队列接收 PFC 帧数',
  mac_rx_pfc_pri5_pkt_num: 'MAC 5 号队列接收 PFC 帧数',
  mac_rx_pfc_pri6_pkt_num: 'MAC 6 号队列接收 PFC 帧数',
  mac_rx_pfc_pri7_pkt_num: 'MAC 7 号队列接收 PFC 帧数',
  mac_tx_total_pkt_num: 'MAC 发送总报文数',
  mac_tx_total_oct_num: 'MAC 发送总字节数',
  mac_tx_bad_pkt_num: 'MAC 发送坏包数',
  mac_tx_bad_oct_num: 'MAC 发送坏包字节数',
  mac_rx_total_pkt_num: 'MAC 接收总报文数',
  mac_rx_total_oct_num: 'MAC 接收总字节数',
  mac_rx_bad_pkt_num: 'MAC 接收坏包数',
  mac_rx_bad_oct_num: 'MAC 接收坏包字节数',
  mac_rx_fcs_err_pkt_num: 'MAC 接收 FCS 校验错误报文数',
  roce_rx_rc_pkt_num: 'ROCE 接收 RC 类型报文数',
  roce_rx_all_pkt_num: 'ROCE 接收总报文数',
  roce_rx_err_pkt_num: 'ROCE 接收坏包数',
  roce_tx_rc_pkt_num: 'ROCE 发送 RC 类型报文数',
  roce_tx_all_pkt_num: 'ROCE 发送总报文数',
  roce_tx_err_pkt_num: 'ROCE 发送坏包数',
  roce_cqe_num: 'ROCE 任务完成总元素个数',
  roce_rx_cnp_pkt_num: 'ROCE 接收 CNP 类型报文数（拥塞通知）',
  roce_tx_cnp_pkt_num: 'ROCE 发送 CNP 类型报文数（拥塞通知）',
  roce_unexpected_ack_num: 'ROCE 接收非预期 ACK 报文数',
  roce_out_of_order_num: 'ROCE 接收乱序或重复报文数',
  roce_verification_err_num: 'ROCE 接收校验错误报文数',
  roce_qp_status_err_num: 'ROCE 接收 QP 状态异常报文数',
  roce_new_pkt_rty_num: 'ROCE 接收重传报文数',
  roce_ecn_db_num: 'ROCE ECN 标记丢弃计数',
  nic_tx_all_pkg_num: 'NIC 发送总报文数',
  nic_tx_all_oct_num: 'NIC 发送总字节数',
  nic_rx_all_pkg_num: 'NIC 接收总报文数',
  nic_rx_all_oct_num: 'NIC 接收总字节数',
  // Network
  'network:throughput': '网络吞吐量',
  packet_count: '包收发速率',
  error_count: '错误包计数（丢包/错包）',
  interface_status: '网卡接口状态（up/down）',
  connection_count: 'TCP 连接数（按状态统计）',
  rx_bytes_total: '接收字节累计值',
  tx_bytes_total: '发送字节累计值',
  // Chassis
  inlet_temp: '进风口温度',
  outlet_temp: '出风口温度',
  fan_power: '风扇功率',
};

const NAV_ORDER = ['cpu', 'memory', 'disk', 'gpu', 'npu', 'network'];

function metricSortCmp(a, b) {
  const la = a.labels || {}, lb = b.labels || {};
  for (const key of [
    'npu_id', 'chip_id', 'gpu_id', 'core', 'cpu', 'node', 'die', 'zone',
    'interface', 'mount_point', 'device', 'mc', 'locator', 'sensor',
    'fan', 'aicore', 'ntc', 'direction', 'type', 'field', 'device_type',
    'kind', 'interval', 'state', 'status',
  ]) {
    const va = la[key], vb = lb[key];
    if (va === undefined && vb === undefined) continue;
    if (va === undefined) return 1;
    if (vb === undefined) return -1;
    if (va === vb) continue;
    if (va === 'total') return -1;
    if (vb === 'total') return 1;
    const na = parseFloat(va), nb = parseFloat(vb);
    if (!isNaN(na) && !isNaN(nb)) return na - nb;
    const da = parseFloat(va.replace(/\D/g, '')), db = parseFloat(vb.replace(/\D/g, ''));
    if (!isNaN(da) && !isNaN(db) && da !== db) return da - db;
    return va < vb ? -1 : 1;
  }
  return 0;
}

function renderErrorCountGroup(items) {
  const drops = items.filter(m => (m.labels || {}).type && m.labels.type.includes('drop'));
  const errs = items.filter(m => (m.labels || {}).type && m.labels.type.includes('err'));
  const container = el('div');
  if (errs.length) container.appendChild(renderRxTxSubGroup('错包计数', errs));
  if (drops.length) container.appendChild(renderRxTxSubGroup('丢包计数', drops));
  return container;
}

function renderRxTxSubGroup(title, items) {
  const byIface = {};
  const order = [];
  for (const mt of items) {
    const lb = mt.labels || {};
    const iface = lb.interface || '';
    if (!byIface[iface]) { byIface[iface] = {}; order.push(iface); }
    const dir = lb.type || '';
    if (dir.startsWith('rx')) byIface[iface].rx = mt.value;
    if (dir.startsWith('tx')) byIface[iface].tx = mt.value;
  }
  const sub = el('div', 'error-sub-group');
  sub.appendChild(elText('div', 'error-sub-title', title));
  for (const iface of order) {
    const d = byIface[iface];
    const rx = d.rx !== undefined ? fmt(d.rx) : '--';
    const tx = d.tx !== undefined ? fmt(d.tx) : '--';
    const row = el('div', 'metric-row rw-row');
    row.innerHTML =
      '<span class="rw-read">接收 ' + rx + '</span>' +
      '<span class="rw-write">发送 ' + tx + '</span>' +
      '<span class="metric-labels">' + iface + '</span>';
    sub.appendChild(row);
  }
  return sub;
}

function renderInterfaceDirectionGroup(items) {
  const byIface = {};
  const order = [];
  for (const mt of items) {
    const lb = mt.labels || {};
    const iface = lb.interface || '';
    if (!byIface[iface]) { byIface[iface] = {}; order.push(iface); }
    byIface[iface][lb.direction || ''] = mt.value;
  }
  order.sort();
  const container = el('div');
  for (const iface of order) {
    const d = byIface[iface];
    const rx = d.rx !== undefined ? fmt(d.rx) : '--';
    const tx = d.tx !== undefined ? fmt(d.tx) : '--';
    const row = el('div', 'metric-row rw-row');
    row.innerHTML =
      '<span class="rw-read">接收 ' + rx + '</span>' +
      '<span class="rw-write">发送 ' + tx + '</span>' +
      '<span class="metric-labels">' + iface + '</span>';
    container.appendChild(row);
  }
  return container;
}

function renderInterfaceStatusGroup(items) {
  const container = el('div');
  for (const mt of items) {
    const lb = mt.labels || {};
    const iface = lb.interface || '';
    const isUp = mt.value > 0;
    const row = el('div', 'metric-row rw-row');
    row.innerHTML =
      '<span class="rw-read">' + (isUp ? 'up' : 'down') + '</span>' +
      '<span class="metric-labels">' + iface + '</span>';
    container.appendChild(row);
  }
  return container;
}

function renderFanSpeedGroup(items) {
  const byFan = {};
  const order = [];
  for (const mt of items) {
    const lb = mt.labels || {};
    const fan = lb.fan || '';
    if (!byFan[fan]) { byFan[fan] = {}; order.push(fan); }
    byFan[fan][lb.direction || ''] = mt.value;
  }
  order.sort((a, b) => parseInt(a) - parseInt(b));
  const container = el('div');
  for (const fan of order) {
    const d = byFan[fan];
    const parts = [];
    if (d.F !== undefined) parts.push('<span class="rw-read">前 ' + fmt(d.F) + '</span>');
    if (d.R !== undefined) parts.push('<span class="rw-write">后 ' + fmt(d.R) + '</span>');
    const row = el('div', 'metric-row rw-row');
    row.innerHTML = parts.join('') + '<span class="metric-labels">风扇 ' + fan + '</span>';
    container.appendChild(row);
  }
  return container;
}

function renderSmartStatusGroup(items) {
  const container = el('div');
  for (const mt of items) {
    const lb = mt.labels || {};
    const passed = mt.value >= 1;
    const row = el('div', 'metric-row rw-row');
    row.innerHTML =
      '<span class="rw-read" style="color:' + (passed ? 'var(--ok)' : 'var(--crit)') + '">' +
      (passed ? 'PASSED' : 'FAILED') + '</span>' +
      '<span class="metric-labels">' + (lb.device || '--') + '</span>';
    container.appendChild(row);
  }
  return container;
}

function renderDeviceDirectionGroup(items) {
  const byDev = {};
  const order = [];
  for (const mt of items) {
    const lb = mt.labels || {};
    const dev = lb.device || '';
    if (!byDev[dev]) { byDev[dev] = {}; order.push(dev); }
    byDev[dev][lb.direction || ''] = mt.value;
  }
  order.sort((a, b) => {
    const na = parseInt(a.replace(/\D/g, ''), 10);
    const nb = parseInt(b.replace(/\D/g, ''), 10);
    if (!isNaN(na) && !isNaN(nb)) return na - nb;
    return a < b ? -1 : 1;
  });
  const container = el('div');
  for (const dev of order) {
    const d = byDev[dev];
    const rd = d.read !== undefined ? fmt(d.read) : '--';
    const wr = d.write !== undefined ? fmt(d.write) : '--';
    const row = el('div', 'metric-row rw-row');
    row.innerHTML =
      '<span class="rw-read">读 ' + rd + '</span>' +
      '<span class="rw-write">写 ' + wr + '</span>' +
      '<span class="metric-labels">' + dev + '</span>';
    container.appendChild(row);
  }
  return container;
}

function renderSwapDetailGroup(items) {
  const fields = {};
  for (const mt of items) {
    const f = (mt.labels || {}).field || '';
    fields[f] = mt.value;
  }
  const total = fields.total ? fmtMB(fields.total) : '--';
  const used = fields.used !== undefined ? fmtMB(fields.used) : '--';
  const free = fields.free ? fmtMB(fields.free) : '--';
  const row = el('div', 'metric-row space-detail-row');
  row.innerHTML =
    '<span class="metric-val">' + total + '</span>' +
    '<span class="space-detail-used">' + used + ' used</span>' +
    '<span class="space-detail-avail">' + free + ' free</span>';
  const container = el('div');
  container.appendChild(row);
  return container;
}

function renderSpaceDetailGroup(items) {
  const byMount = {};
  const order = [];
  for (const mt of items) {
    const lb = mt.labels || {};
    const key = (lb.device || '') + '|' + (lb.mount_point || '');
    if (!byMount[key]) { byMount[key] = {}; order.push(key); }
    byMount[key][lb.field] = mt.value;
    byMount[key].device = lb.device;
    byMount[key].mount_point = lb.mount_point;
    byMount[key].fstype = lb.fstype;
  }
  order.sort((a, b) => {
    const pa = byMount[a].mount_point || '';
    const pb = byMount[b].mount_point || '';
    if (pa === '/') return -1;
    if (pb === '/') return 1;
    return pa < pb ? -1 : 1;
  });
  const container = el('div');
  for (const key of order) {
    const d = byMount[key];
    const total = d.total ? fmtMB(d.total) : '--';
    const used = d.used ? fmtMB(d.used) : '--';
    const avail = d.available ? fmtMB(d.available) : '--';
    const availPct = d.total > 0 ? (d.available / d.total * 100) : 100;
    const availColor = availPct < 10 ? ' style="color:var(--crit)"' : '';
    const row = el('div', 'metric-row space-detail-row');
    row.innerHTML =
      '<span class="metric-val">' + total + '</span>' +
      '<span class="space-detail-used">' + used + ' used</span>' +
      '<span class="space-detail-avail"' + availColor + '>' + avail + ' avail</span>' +
      '<span class="metric-labels">' + (d.device || '--') + ' → ' + (d.mount_point || '--') + (d.fstype ? ' (' + d.fstype + ')' : '') + '</span>';
    container.appendChild(row);
  }
  return container;
}

function parentPciAddr(addr) {
  if (!addr) return '';
  var parts = addr.split(':');
  if (parts.length < 3) return addr;
  var devFunc = parts[2];
  var devOnly = devFunc.split('.')[0];
  return parts[0] + ':' + parts[1] + ':' + devOnly;
}

function renderNetworkCardGroup(specs) {
  var netSpecs = specs.filter(function(m) { return m.name === 'net_info'; });
  if (netSpecs.length === 0) return null;

  var groups = {};
  var order = [];
  for (var i = 0; i < netSpecs.length; i++) {
    var m = netSpecs[i];
    var lb = m.labels || {};
    var parent = parentPciAddr(lb.pci_addr || '');
    var key = parent || ('no_pci_' + (lb.interface || ''));
    if (!groups[key]) { groups[key] = { ports: [], device: '', pciBase: parent }; order.push(key); }
    groups[key].ports.push(lb);
    if (lb.pci_device) groups[key].device = lb.pci_device;
  }
  order.sort(function(a, b) { return (groups[a].pciBase || '') < (groups[b].pciBase || '') ? -1 : 1; });

  var container = el('div');

  var title = el('div', 'metric-group-head');
  title.style.cursor = 'default';
  title.innerHTML = '<span class="metric-group-name">网络 (' + order.length + ')</span>';
  container.appendChild(title);

  var body = el('div', 'metric-group-body');
  for (var i = 0; i < order.length; i++) {
    var g = groups[order[i]];
    var deviceName = g.device || g.pciBase || g.ports[0].driver || g.ports[0].interface || '未知设备';
    var cardHead = el('div', 'metric-row');
    cardHead.style.fontWeight = '600';
    cardHead.style.marginTop = '4px';
    cardHead.innerHTML = '<span class="metric-val">' + deviceName + '</span>' +
      '<span class="metric-labels">' + (g.pciBase && g.device ? 'PCI: ' + g.pciBase : '') + '</span>';
    body.appendChild(cardHead);

    for (var j = 0; j < g.ports.length; j++) {
      var p = g.ports[j];
      var speedStr = p.speed && p.speed !== '-1' ? p.speed + 'Mb/s' : '--';
      var row = el('div', 'metric-row');
      row.style.paddingLeft = '16px';
      row.innerHTML = '<span class="metric-labels">' +
        '接口: ' + (p.interface || '--') +
        (p.pci_addr ? '  PCI: ' + p.pci_addr : '') +
        '  MAC: ' + (p.mac || '--') +
        '  MTU: ' + (p.mtu || '--') +
        '  速率: ' + speedStr +
        '  驱动: ' + (p.driver || '--') +
        '</span>';
      body.appendChild(row);
    }
  }
  container.appendChild(body);
  return container;
}

var networkFSTypes = { 'nfs': true, 'nfs4': true, 'cifs': true, 'smb': true, 'fuse.sshfs': true, 'fuse.glusterfs': true };

function diskTypeLabel(device, model) {
  var dev = (device || '').replace('/dev/', '');
  if (dev.indexOf('nvme') === 0) return 'NVMe SSD';
  if ((model || '').toUpperCase().indexOf('RAID') >= 0) return 'RAID 逻辑盘';
  if (dev.indexOf('sd') === 0) return 'SAS/SATA 硬盘';
  return '硬盘';
}

function renderDiskGroup(specs) {
  var diskSpecs = specs.filter(function(m) { return m.name === 'disk_info'; });
  if (diskSpecs.length === 0) return null;

  var container = el('div');

  var title = el('div', 'metric-group-head');
  title.style.cursor = 'default';
  title.innerHTML = '<span class="metric-group-name">硬盘 (' + diskSpecs.length + ')</span>';
  container.appendChild(title);

  var body = el('div', 'metric-group-body');

  var grid = el('div', 'disk-grid');
  var headers = ['设备', '类型', '容量', '型号', '序列号', '固件', '接口'];
  for (var h = 0; h < headers.length; h++) {
    var hh = el('div', 'disk-grid-h');
    hh.textContent = headers[h];
    grid.appendChild(hh);
  }
  for (var i = 0; i < diskSpecs.length; i++) {
    var m = diskSpecs[i];
    var lb = m.labels || {};
    var dev = lb.device || '--';
    var model = lb.model || '--';
    var serial = lb.serial || '--';
    var typeLabel = diskTypeLabel(dev, model);
    var sizeStr = m.value > 0 ? fmtGB(m.value) : '--';
    if (model !== '--' && serial !== '--' && model === serial) serial = '--';

    var cells = [
      '/dev/' + dev, typeLabel, sizeStr, model, serial,
      lb.firmware || '--', lb.interface || '--'
    ];
    for (var c = 0; c < cells.length; c++) {
      var cell = el('div', c === 0 ? 'disk-grid-val-bold' : 'disk-grid-val');
      cell.textContent = cells[c];
      grid.appendChild(cell);
    }
  }
  body.appendChild(grid);
  container.appendChild(body);
  return container;
}

function parentDisk(device) {
  var dev = (device || '').replace('/dev/', '');
  if (/^nvme\d+n\d+p\d+$/.test(dev)) return dev.replace(/p\d+$/, '');
  if (/^(sd|vd|xvd)[a-z]+\d+$/.test(dev)) return dev.replace(/\d+$/, '');
  if (dev.indexOf('mapper/') === 0) return '/dev/' + dev;
  return dev;
}

function classifySpaceMetrics(metrics) {
  var local = [], network = [];
  for (var i = 0; i < metrics.length; i++) {
    var m = metrics[i];
    var lb = m.labels || {};
    if (networkFSTypes[lb.fstype] || !(lb.device || '').startsWith('/dev/')) {
      network.push(m);
    } else {
      local.push(m);
    }
  }
  return { local: local, network: network };
}

function aggregateByPhysicalDisk(spaceDetailMetrics, spaceUsageMetrics) {
  var groups = {};
  var order = [];
  for (var i = 0; i < spaceDetailMetrics.length; i++) {
    var m = spaceDetailMetrics[i];
    var lb = m.labels || {};
    var pd = parentDisk(lb.device || '');
    if (!groups[pd]) { groups[pd] = { parts: {}, total: 0, used: 0, avail: 0 }; order.push(pd); }
    groups[pd].parts[lb.device || ''] = true;
    if (lb.field === 'total') groups[pd].total += m.value;
    if (lb.field === 'used') groups[pd].used += m.value;
    if (lb.field === 'available') groups[pd].avail += m.value;
  }
  for (var i = 0; i < spaceUsageMetrics.length; i++) {
    var m = spaceUsageMetrics[i];
    var lb = m.labels || {};
    var pd = parentDisk(lb.device || '');
    if (groups[pd]) groups[pd].usage = m.value;
  }
  order.sort(function(a, b) {
    var na = parseInt(a.replace(/\D/g, ''), 10);
    var nb = parseInt(b.replace(/\D/g, ''), 10);
    if (!isNaN(na) && !isNaN(nb)) return na - nb;
    return a < b ? -1 : 1;
  });
  return order.map(function(pd) { return Object.assign({ disk: pd, parts: Object.keys(groups[pd].parts) }, groups[pd]); });
}

function isPhysicalDiskDevice(device) {
  var dev = (device || '').replace('/dev/', '');
  if (dev.indexOf('mapper/') === 0 || dev.indexOf('dm-') === 0) return false;
  if (/^(sd|vd|xvd|nvme)[a-z0-9]+$/.test(dev)) return true;
  return false;
}

function renderPhysicalDiskGroups(localMetrics) {
  var spaceDetail = localMetrics.filter(function(m) { return m.name === 'space_detail' && isPhysicalDiskDevice((m.labels || {}).device); });
  var spaceUsage = localMetrics.filter(function(m) { return m.name === 'space_usage' && isPhysicalDiskDevice((m.labels || {}).device); });
  var disks = aggregateByPhysicalDisk(spaceDetail, spaceUsage);
  if (disks.length === 0) return null;

  var container = el('div');

  var usageTitle = el('div', 'metric-group-head');
  usageTitle.style.cursor = 'default';
  usageTitle.innerHTML = '<span class="metric-group-name">物理盘空间使用率</span><span class="metric-group-count">' + disks.length + ' 条</span>';
  container.appendChild(usageTitle);
  var usageBody = el('div', 'metric-group-body');
  for (var i = 0; i < disks.length; i++) {
    var d = disks[i];
    var pct = d.total > 0 ? Math.round(d.used / d.total * 100 * 100) / 100 : 0;
    var row = el('div', 'metric-row space-detail-row');
    row.innerHTML = '<span class="metric-val">' + pct + ' %</span>' +
      '<span class="metric-labels">' + d.disk + (d.parts.length > 1 ? ' (' + d.parts.join(', ') + ')' : '') + '</span>';
    usageBody.appendChild(row);
  }
  container.appendChild(usageBody);

  var detailTitle = el('div', 'metric-group-head');
  detailTitle.style.cursor = 'default';
  detailTitle.innerHTML = '<span class="metric-group-name">物理盘空间明细</span><span class="metric-group-count">' + disks.length + ' 条</span>';
  container.appendChild(detailTitle);
  var detailBody = el('div', 'metric-group-body');
  for (var i = 0; i < disks.length; i++) {
    var d = disks[i];
    var total = d.total > 0 ? fmtMB(d.total) : '--';
    var used = d.total > 0 ? fmtMB(d.used) : '--';
    var avail = d.avail > 0 ? fmtMB(d.avail) : '--';
    var row = el('div', 'metric-row space-detail-row');
    row.innerHTML = '<span class="metric-val">' + total + '</span>' +
      '<span class="space-detail-used">' + used + ' used</span>' +
      '<span class="space-detail-avail">' + avail + ' avail</span>' +
      '<span class="metric-labels">' + d.disk + (d.parts.length > 1 ? ' (' + d.parts.join(', ') + ')' : '') + '</span>';
    detailBody.appendChild(row);
  }
  container.appendChild(detailBody);

  return container;
}

function renderNetworkStorageGroup(networkMetrics) {
  var spaceUsage = networkMetrics.filter(function(m) { return m.name === 'space_usage'; });
  var spaceDetail = networkMetrics.filter(function(m) { return m.name === 'space_detail'; });
  if (spaceUsage.length === 0 && spaceDetail.length === 0) return null;

  var byMount = {};
  var order = [];
  for (var i = 0; i < spaceDetail.length; i++) {
    var m = spaceDetail[i];
    var lb = m.labels || {};
    var key = (lb.device || '') + '|' + (lb.mount_point || '');
    if (!byMount[key]) { byMount[key] = { device: lb.device, mount: lb.mount_point, fstype: lb.fstype, total: 0, used: 0, avail: 0 }; order.push(key); }
    if (lb.field === 'total') byMount[key].total = m.value;
    if (lb.field === 'used') byMount[key].used = m.value;
    if (lb.field === 'available') byMount[key].avail = m.value;
  }
  for (var i = 0; i < spaceUsage.length; i++) {
    var m = spaceUsage[i];
    var lb = m.labels || {};
    var key = (lb.device || '') + '|' + (lb.mount_point || '');
    if (byMount[key]) byMount[key].usage = m.value;
  }
  if (order.length === 0) return null;
  order.sort(function(a, b) { return (byMount[a].mount || '') < (byMount[b].mount || '') ? -1 : 1; });

  var container = el('div');

  var usageTitle = el('div', 'metric-group-head');
  usageTitle.style.cursor = 'default';
  usageTitle.innerHTML = '<span class="metric-group-name">网络存储空间使用率</span><span class="metric-group-count">' + order.length + ' 条</span>';
  container.appendChild(usageTitle);
  var usageBody = el('div', 'metric-group-body');
  for (var i = 0; i < order.length; i++) {
    var d = byMount[order[i]];
    var row = el('div', 'metric-row space-detail-row');
    row.innerHTML = '<span class="metric-val">' + (d.usage !== undefined ? d.usage + ' %' : '--') + '</span>' +
      '<span class="metric-labels">' + (d.device || '--') + ' → ' + (d.mount || '--') + '</span>';
    usageBody.appendChild(row);
  }
  container.appendChild(usageBody);

  var detailTitle = el('div', 'metric-group-head');
  detailTitle.style.cursor = 'default';
  detailTitle.innerHTML = '<span class="metric-group-name">网络存储空间明细</span><span class="metric-group-count">' + order.length + ' 条</span>';
  container.appendChild(detailTitle);
  var detailBody = el('div', 'metric-group-body');
  for (var i = 0; i < order.length; i++) {
    var d = byMount[order[i]];
    var total = d.total > 0 ? fmtMB(d.total) : '--';
    var used = d.total > 0 ? fmtMB(d.used) : '--';
    var avail = d.avail > 0 ? fmtMB(d.avail) : '--';
    var availPct = d.total > 0 ? (d.avail / d.total * 100) : 100;
    var availColor = availPct < 10 ? ' style="color:var(--crit)"' : '';
    var row = el('div', 'metric-row space-detail-row');
    row.innerHTML = '<span class="metric-val">' + total + '</span>' +
      '<span class="space-detail-used">' + used + ' used</span>' +
      '<span class="space-detail-avail"' + availColor + '>' + avail + ' avail</span>' +
      '<span class="metric-labels">' + (d.device || '--') + ' → ' + (d.mount || '--') + (d.fstype ? ' (' + d.fstype + ')' : '') + '</span>';
    detailBody.appendChild(row);
  }
  container.appendChild(detailBody);

  return container;
}

// ---- state ----
let collectors = [];
let lastSnapshot = null;
let refreshIntervalMs = 3000;
let pollTimer = null;
let autoOn = true;
let appVersion = '';

// ---- helpers ----
function el(tag, cls) { const e = document.createElement(tag); if (cls) e.className = cls; return e; }
function elText(tag, cls, text) { const e = el(tag, cls); e.textContent = text; return e; }
function anchor(href, text, cls) { const a = el('a', cls); a.href = href; a.textContent = text; return a; }

function compTitle(key) { return (MANIFEST[key] || {}).title || key.toUpperCase(); }
function navOrder(key) { const i = NAV_ORDER.indexOf(key); return i < 0 ? 999 : i; }

function statusOf(score, max) {
  if (!max) return { label: 'N/A', color: '#9ca3af' };
  const r = score / max;
  if (r >= 0.9) return { label: 'OK', color: '#2e7d32' };
  if (r >= 0.75) return { label: 'Good', color: '#689f38' };
  if (r >= 0.6) return { label: 'Warning', color: '#f57c00' };
  return { label: 'Critical', color: '#c62828' };
}
function gradeColor(grade) {
  if (grade === 'Excellent') return '#2e7d32';
  if (grade === 'Good') return '#689f38';
  if (grade === 'Warning') return '#f57c00';
  return '#c62828';
}
function fmt(v) {
  if (v === null || v === undefined) return '-';
  if (Number.isInteger(v)) return String(v);
  return Number(v).toFixed(2);
}
function seriesLabel(k) {
  if (SERIES_LABELS[k]) return SERIES_LABELS[k];
  return k.replace(/^[a-z]+_/, '').replace(/_/g, ' ');
}

// statBounds computes min/max/mean of a series. max is nudged above min so a
// flat series still renders (avoids divide-by-zero).
function statBounds(series) {
  let min = Infinity, max = -Infinity, sum = 0;
  for (const v of series) { if (v < min) min = v; if (v > max) max = v; sum += v; }
  if (max === min) max = min + 1;
  return { min, max, mean: sum / series.length };
}

// meanLine returns a horizontal dashed SVG line at viewBox y, used to mark the
// series average on both the small sparkline and the detail chart.
function meanLine(y) {
  const l = document.createElementNS('http://www.w3.org/2000/svg', 'line');
  l.setAttribute('x1', 0); l.setAttribute('x2', 100);
  l.setAttribute('y1', y); l.setAttribute('y2', y);
  l.setAttribute('stroke', '#9ca3af');
  l.setAttribute('stroke-width', '1');
  l.setAttribute('stroke-dasharray', '3,2');
  l.setAttribute('vector-effect', 'non-scaling-stroke');
  return l;
}

// sparkline renders a compact preview polyline (overview card headline).
// Includes the mean dashed line; no axis labels (too small for ticks there).
function sparkline(series, color) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 100 30');
  svg.setAttribute('preserveAspectRatio', 'none');
  svg.classList.add('spark');
  const n = series.length;
  if (n < 2) return svg;
  const { min, max, mean } = statBounds(series);
  const yOf = v => 28 - ((v - min) / (max - min)) * 24 - 2;
  svg.appendChild(meanLine(yOf(mean)));
  const pts = series.map((v, i) => ((i / (n - 1)) * 100).toFixed(2) + ',' + yOf(v).toFixed(2)).join(' ');
  const poly = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
  poly.setAttribute('points', pts);
  poly.setAttribute('fill', 'none');
  poly.setAttribute('stroke', color);
  poly.setAttribute('stroke-width', '1.5');
  poly.setAttribute('vector-effect', 'non-scaling-stroke');
  svg.appendChild(poly);
  return svg;
}

// chartTimeSpan formats the X-axis window: history_points × refresh interval.
function chartTimeSpan(snap) {
  const pts = snap.history_points || 60;
  const s = ((snap.refresh_interval_ms || 5000) * pts) / 1000;
  if (s >= 3600) return (s / 3600).toFixed(0) + ' 小时';
  if (s >= 60) return Math.round(s / 60) + ' 分钟';
  return Math.round(s) + ' 秒';
}

// renderChart renders the detail-page trend chart: Y axis (min/max labels), the
// data polyline + a mean dashed line, and an X axis (time span -> now).
function renderChart(series, color, snap) {
  const n = series.length;
  const chart = el('div', 'chart');
  if (n < 2) { chart.appendChild(elText('div', 'empty', '数据不足')); return chart; }
  const { min, max, mean } = statBounds(series);
  const yOf = v => 28 - ((v - min) / (max - min)) * 24 - 2;

  const row = el('div', 'chart-row');
  const ycol = el('div', 'chart-y');
  ycol.appendChild(elText('span', 'axis-val', fmt(max)));
  ycol.appendChild(elText('span', 'axis-val', fmt(min)));

  const plot = el('div', 'chart-plot');
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 100 30');
  svg.setAttribute('preserveAspectRatio', 'none');
  svg.classList.add('spark');
  svg.appendChild(meanLine(yOf(mean)));
  const pts = series.map((v, i) => ((i / (n - 1)) * 100).toFixed(2) + ',' + yOf(v).toFixed(2)).join(' ');
  const poly = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
  poly.setAttribute('points', pts);
  poly.setAttribute('fill', 'none');
  poly.setAttribute('stroke', color);
  poly.setAttribute('stroke-width', '1.5');
  poly.setAttribute('vector-effect', 'non-scaling-stroke');
  svg.appendChild(poly);
  plot.appendChild(svg);
  // Mean value label sitting on the dashed line: the line's viewBox y maps to
  // (y/30) of the stretched plot height, so position the badge at that %.
  const badge = elText('span', 'mean-badge', '均值 ' + fmt(mean));
  badge.style.top = (yOf(mean) / 30 * 100) + '%';
  plot.appendChild(badge);
  row.appendChild(ycol);
  row.appendChild(plot);
  chart.appendChild(row);

  const xrow = el('div', 'chart-x');
  xrow.appendChild(elText('span', 'axis-val', '−' + chartTimeSpan(snap)));
  xrow.appendChild(elText('span', 'axis-val', '现在'));
  chart.appendChild(xrow);
  return chart;
}

function metricsFor(snap, compKey) { return (snap.metrics || []).filter(m => m.component === compKey); }

function pickMetric(metrics, spec) {
  const name = typeof spec === 'string' ? spec : spec.name;
  const prefer = typeof spec === 'string' ? null : spec.prefer;
  let first = null;
  for (const m of metrics) {
    if (m.name !== name) continue;
    if (!first) first = m;
    if (prefer) {
      let match = true;
      for (const k in prefer) { if ((m.labels || {})[k] !== prefer[k]) { match = false; break; } }
      if (match) return m;
    }
  }
  return first;
}

function orderedComponents(snap) {
  // The "system" collector emits static identity metrics (device_model etc.
  // attributed to gpu/npu/disk/network) but has no dynamic page of its own, so
  // hide it from the nav and overview grid.
  let comps = collectors.filter(c => c.component !== 'system');
  if (snap) {
    comps = comps.filter(c => {
      const hasMetrics = (snap.metrics || []).some(m => m.component === c.component);
      const hasHealth = snap.health && snap.health.components && snap.health.components[c.component];
      return hasMetrics || hasHealth;
    });
  }
  return comps.slice().sort((a, b) => {
    const oa = navOrder(a.component), ob = navOrder(b.component);
    if (oa !== ob) return oa - ob;
    return a.component < b.component ? -1 : 1;
  });
}

function componentSeries(compKey, history) {
  const prefix = compKey + '_';
  const out = [];
  for (const k in history) {
    if (k.indexOf(prefix) === 0 && history[k].length > 0) {
      out.push({ key: k, label: seriesLabel(k), data: history[k] });
    }
  }
  return out;
}

function currentRoute() {
  const h = location.hash.replace(/^#/, '');
  if (!h || h === '/') return 'overview';
  return h.replace(/^\//, '');
}
function navigate(key) { location.hash = '/' + key; }

// ---- topbar pill ----
function renderPill(snap) {
  const h = snap.health || {};
  const color = gradeColor(h.grade);
  document.getElementById('pillScore').textContent = h.score !== undefined ? h.score : '--';
  document.getElementById('pillGrade').textContent = h.grade || '--';
  document.getElementById('pillGrade').style.color = color;
  document.getElementById('pillDot').style.background = color;
}

// ---- nav ----
function renderNav() {
  const nav = document.getElementById('nav');
  nav.innerHTML = '';
  const route = currentRoute();
  const aOverview = el('a'); aOverview.href = '#/'; aOverview.textContent = '概览';
  if (route === 'overview') aOverview.className = 'active';
  nav.appendChild(aOverview);
  for (const c of orderedComponents(lastSnapshot)) {
    const a = el('a'); a.href = '#/' + c.component; a.textContent = compTitle(c.component);
    if (route === c.component) a.className = 'active';
    nav.appendChild(a);
  }
  const aStress = el('a'); aStress.href = '/stress/'; aStress.textContent = '可靠性压测';
  nav.appendChild(aStress);
}

// specSummary returns a one-line identity string for a component's overview
// card (e.g. "Tesla T4 等 2 卡", "Samsung 970 等 2 块", "eth0 1000Mb/s").
// Reads stashed static specs (snap.specs); empty string when none available.
// ---- specs helpers ----
// specVal finds the first static spec with the given name (and optional
// component) in snap.specs.
function specVal(specs, name, comp) {
  for (const m of specs) {
    if (m.name === name && (comp === undefined || m.component === comp)) return m;
  }
  return null;
}
// memoryTotalMB returns the total physical memory in MB, read from the
// every-cycle usage_detail metric (works on Linux + Windows without root).
function memoryTotalMB(snap) {
  const m = pickMetric(snap.metrics || [], { name: 'usage_detail', prefer: { field: 'total' } });
  return m ? m.value : 0;
}
function fmtGB(gb) {
  if (gb >= 1024) return (gb / 1024).toFixed(1) + ' TB';
  return gb.toFixed(gb >= 10 ? 0 : 1) + ' GB';
}
function fmtMB(mb) { return fmtGB(mb / 1024); }
function netStr(m) {
  const lb = m.labels || {};
  const sfx = lb.speed && lb.speed !== '-1' ? ' ' + lb.speed + 'Mb/s' : '';
  return (lb.interface || '') + sfx;
}

// ---- specs panel (top-right of overview hero) ----
// Compact: 1-2 core static specs per component. Click opens a modal with the
// full static hardware info grouped by component.
function renderSpecs(snap) {
  const specs = snap.specs || [];
  const panel = el('div', 'specs clickable');
  panel.onclick = () => openSpecsModal(snap);
  panel.appendChild(elText('div', 'specs-title', '设备规格'));
  const kv = el('div', 'specs-kv');
  const add = (k, v) => {
    if (!v) return;
    kv.appendChild(elText('div', 'k', k));
    kv.appendChild(elText('div', 'v', v));
  };

  const dev = specVal(specs, 'device_model');
  if (dev) add('设备', [dev.labels.manufacturer, dev.labels.product_name].filter(x => x).join(' '));

  const osi = specVal(specs, 'os_info');
  if (osi) add('OS', (osi.labels || {}).pretty_name || '');

  const mi = specVal(specs, 'model_info');
  if (mi) add('CPU', (mi.labels || {}).model_name || '');

  const mt = memoryTotalMB(snap);
  if (mt) add('内存', fmtMB(mt));

  const disks = specs.filter(m => m.name === 'disk_info');
  if (disks.length) add('硬盘', disks.length + ' 块, 共 ' + fmtGB(disks.reduce((s, m) => s + m.value, 0)));

  const nets = specs.filter(m => m.name === 'net_info');
  if (nets.length) add('网卡', nets.slice(0, 2).map(netStr).join(', ') + (nets.length > 2 ? ' …' : ''));

  const gpus = specs.filter(m => m.name === 'gpu_info');
  if (gpus.length) add('GPU', (gpus[0].labels || {}).name + (gpus.length > 1 ? ' 等 ' + gpus.length + ' 卡' : ''));
  const npus = specs.filter(m => m.name === 'npu_info');
  if (npus.length) add('NPU', (npus[0].labels || {}).name + (npus.length > 1 ? ' 等 ' + npus.length + ' 卡' : ''));

  if (!kv.children.length) {
    panel.appendChild(elText('div', 'empty', '无静态规格信息'));
  } else {
    panel.appendChild(kv);
    panel.appendChild(elText('div', 'specs-hint', '点击查看完整规格 ▸'));
  }
  return panel;
}

// ---- specs modal (full static hardware info) ----
function openSpecsModal(snap) {
  const old = document.getElementById('specsModal');
  if (old) old.remove();
  const specs = snap.specs || [];
  const overlay = el('div', 'modal-overlay');
  overlay.id = 'specsModal';
  overlay.onclick = (e) => { if (e.target === overlay) overlay.remove(); };

  const card = el('div', 'modal-card');
  const head = el('div', 'modal-head');
  head.appendChild(elText('span', '', '设备规格 · 完整信息'));
  const close = el('button', 'modal-close');
  close.textContent = '×';
  close.onclick = () => overlay.remove();
  head.appendChild(close);
  card.appendChild(head);

  const body = el('div', 'modal-body');
  const groups = {};
  for (const m of specs) { (groups[m.component] = groups[m.component] || []).push(m); }

  // Memory total comes from the every-cycle usage_detail metric, not specs;
  // inject it so the memory group is never empty on hosts without dmidecode.
  const mt = memoryTotalMB(snap);
  if (mt) {
    groups['memory'] = (groups['memory'] || []).concat([{
      component: 'memory', name: 'mem_total', value: mt,
      labels: { capacity: fmtMB(mt) }, synthetic: true,
    }]);
  }

  const order = ['system', 'cpu', 'memory', 'disk', 'gpu', 'npu', 'network'];
  const seen = {};
  for (const comp of order) {
    seen[comp] = true;
    if (groups[comp] && groups[comp].length) {
      if (comp === 'memory') {
        body.appendChild(specsGroupMemory(groups[comp]));
      } else if (comp === 'network') {
        var netGroup = renderNetworkCardGroup(groups[comp]);
        if (netGroup) body.appendChild(netGroup);
      } else if (comp === 'disk') {
        var diskGroup = renderDiskGroup(groups[comp]);
        if (diskGroup) body.appendChild(diskGroup);
      } else {
        body.appendChild(specsGroup(comp, groups[comp]));
      }
    }
  }
  for (const comp in groups) {
    if (!seen[comp] && groups[comp].length) body.appendChild(specsGroup(comp, groups[comp]));
  }
  if (!body.children.length) body.appendChild(elText('div', 'empty', '无静态规格信息'));
  card.appendChild(body);
  overlay.appendChild(card);
  document.body.appendChild(overlay);
}

// specsGroup renders one component's static specs as a titled table
// (类型 / 标识 / 明细). Synthetic entries (mem_total) get a friendly type.
function specsGroup(comp, arr) {
  const sorted = comp === 'cpu' ? arr.slice().sort(function(a, b) {
    var ia = CPU_SPEC_ORDER[a.name], ib = CPU_SPEC_ORDER[b.name];
    if (ia === undefined) ia = 99;
    if (ib === undefined) ib = 99;
    if (ia !== ib) return ia - ib;
    return (a.name < b.name) ? -1 : 1;
  }) : arr;
  const sec = el('div', 'specs-group');
  const title = comp === 'system' ? '系统' : compTitle(comp);
  sec.appendChild(elText('div', 'specs-group-title', title + ' (' + sorted.length + ')'));
  const tbl = document.createElement('table');
  tbl.className = 'table';
  tbl.innerHTML = '<thead><tr><th>类型</th><th>标识</th><th>明细</th></tr></thead>';
  const tb = document.createElement('tbody');
  for (const m of sorted) {
    const def = SPEC_DEFS[m.name] || { type: (METRIC_NAMES[m.name] || m.name), primary: '' };
    const lb = m.labels || {};
    // Identity specs carry their main value in a label (def.primary); synthetic
    // memory total uses fmtMB. Numeric static specs (cpu_num/core_num/numa/cache
    // sizes/frequencies) carry their value in m.value — fall back to value+unit.
    let primary;
    if (def.primary) {
      primary = lb[def.primary] || '';
    } else if (m.synthetic) {
      primary = fmtMB(m.value);
    } else {
      primary = (m.value !== undefined && m.value !== null) ? (m.value + (m.unit ? ' ' + m.unit : '')) : '';
    }
    const rest = [];
    for (const k in lb) {
      if (k === def.primary) continue;
      rest.push((LABEL_NAMES[k] || k) + ': ' + lb[k]);
    }
    const tr = document.createElement('tr');
    tr.innerHTML =
      '<td class="m-name">' + (m.synthetic ? '内存容量' : def.type) + '</td>' +
      '<td class="m-val">' + primary + '</td>' +
      '<td class="m-labels">' + rest.join(', ') + '</td>';
    tb.appendChild(tr);
  }
  tbl.appendChild(tb);
  sec.appendChild(tbl);
  return sec;
}

function specsGroupMemory(specs) {
  const sec = el('div', 'specs-group');
  const totalDimms = specs.filter(m => m.name === 'module_size' || m.name === 'module_info').length;
  const dimmCount = new Set(specs.filter(m => m.name === 'module_size' || m.name === 'module_info').map(m => (m.labels||{}).locator||'')).size;
  const titleDiv = el('div', 'metric-group-head');
  titleDiv.style.cursor = 'default';
  titleDiv.innerHTML = '<span class="metric-group-name">内存 (' + dimmCount + ')</span>';
  sec.appendChild(titleDiv);
  const tbl = document.createElement('table');
  tbl.className = 'table';
  tbl.innerHTML = '<thead><tr><th>插槽</th><th>容量</th><th>类型</th><th>速率</th><th>厂商</th></tr></thead>';
  const tb = document.createElement('tbody');

  const sizes = {};
  const infos = {};
  const order = [];
  let memTotal = null;

  for (const m of specs) {
    if (m.name === 'mem_total') { memTotal = m; continue; }
    const loc = (m.labels || {}).locator || '';
    if (m.name === 'module_size') {
      sizes[loc] = m;
      if (!order.includes(loc)) order.push(loc);
    } else if (m.name === 'module_info') {
      infos[loc] = m;
      if (!order.includes(loc)) order.push(loc);
    }
  }

  if (memTotal) {
    const tr = document.createElement('tr');
    tr.innerHTML = '<td>合计</td><td>' + (memTotal.labels || {}).capacity + '</td><td colspan="3"></td>';
    tb.appendChild(tr);
  }

  order.sort(metricLocatorCmp);
  for (const loc of order) {
    const sz = sizes[loc], info = infos[loc];
    const lb = (info || sz || {}).labels || {};
    const sizeStr = sz ? fmtMB(sz.value) : (lb.capacity || '--');
    const tr = document.createElement('tr');
    tr.innerHTML =
      '<td>' + (loc || '--') + '</td>' +
      '<td>' + sizeStr + '</td>' +
      '<td>' + (lb.type || '--') + '</td>' +
      '<td>' + (lb.speed || '--') + '</td>' +
      '<td>' + (lb.manufacturer || '--') + '</td>';
    tb.appendChild(tr);
  }

  tbl.appendChild(tb);
  sec.appendChild(tbl);
  return sec;
}

function metricLocatorCmp(a, b) {
  const na = parseInt(a.replace(/\D/g, ''), 10);
  const nb = parseInt(b.replace(/\D/g, ''), 10);
  if (!isNaN(na) && !isNaN(nb)) return na - nb;
  return a < b ? -1 : 1;
}

// ---- overview ----
function renderOverview(snap) {
  const page = document.getElementById('page');
  page.innerHTML = '';
  const h = snap.health || {};
  const stColor = gradeColor(h.grade);
  const scorePct = (h.score || 0);

  const hero = el('div', 'hero');
  const sc = el('div', 'hero-score');
  sc.innerHTML =
    '<div class="hero-num" style="color:' + stColor + '">' +
      '<span>' + (h.score !== undefined ? h.score : '--') + '</span><span class="max">/100</span>' +
    '</div>' +
    '<div class="hero-grade" style="color:' + stColor + '">' + (h.grade || '--') + '</div>' +
    '<div class="hero-bar"><div class="fill" style="width:' + scorePct + '%;background:' + stColor + '"></div></div>';
  hero.appendChild(sc);

  const info = el('div', 'hero-info');
  info.innerHTML =
    '<div>服务器类型: <b>' + (SERVER_TYPE_TEXT[h.server_type] || h.server_type || '--') + '</b></div>' +
    '<div>更新时间: <b>' + (snap.timestamp ? new Date(snap.timestamp).toLocaleString('zh-CN') : '--') + '</b></div>';
  const comps = orderedComponents(snap);
  if (comps.length) {
    const chips = el('div', 'hero-components');
    for (const c of comps) {
      const ch = (h.components || {})[c.component];
      const color = ch ? statusOf(ch.score, ch.max).color : '#9ca3af';
      const chip = el('div', 'comp-chip');
      chip.style.cursor = 'pointer';
      chip.innerHTML = '<span class="dot" style="background:' + color + '"></span>' + compTitle(c.component);
      chip.onclick = () => navigate(c.component);
      chips.appendChild(chip);
    }
    info.appendChild(chips);
  }
  hero.appendChild(info);
  hero.appendChild(renderSpecs(snap));
  page.appendChild(hero);

  const title = elText('div', 'section-title', '部件概览');
  title.innerHTML = '部件概览 <span class="hint">点击卡片或芯片进入详情</span>';
  page.appendChild(title);

  const grid = el('div', 'grid');
  for (const c of comps) {
    const card = summaryCard(c.component, snap);
    if (card) grid.appendChild(card);
  }
  if (!grid.children.length) grid.appendChild(elText('div', 'empty', '无可用部件数据'));
  page.appendChild(grid);
}

function summaryCard(compKey, snap) {
  const m = MANIFEST[compKey] || {};
  const compHealth = (snap.health && snap.health.components) ? snap.health.components[compKey] : null;
  const metrics = metricsFor(snap, compKey);
  // Hide cards that have neither metrics nor a health score (e.g. no GPU/NPU
  // hardware present); they carry no information for the overview grid.
  if (metrics.length === 0 && !compHealth) return null;
  const st = statusOf(compHealth ? compHealth.score : 0, compHealth ? compHealth.max : 0);

  const card = el('div', 'card');
  card.onclick = () => navigate(compKey);
  const head = el('div', 'card-head');
  const t = el('div', 'card-title');
  t.innerHTML = '<span class="dot" style="background:' + st.color + '"></span>' + compTitle(compKey);
  const sc = el('div', 'card-score');
  if (compHealth) {
    sc.innerHTML = '<b style="color:' + st.color + '">' + compHealth.score + '</b> / ' + compHealth.max +
      ' <span class="badge" style="background:' + st.color + '">' + st.label + '</span>';
  } else {
    sc.innerHTML = '<span class="badge na">无数据</span>';
  }
  head.appendChild(t); head.appendChild(sc);
  card.appendChild(head);

  const body = el('div', 'card-body');
  var sparklineShown = false;
  if (m.headline && snap.history && snap.history[m.headline] && snap.history[m.headline].length > 1) {
    sparklineShown = true;
    const histData = snap.history[m.headline];
    const curVal = histData[histData.length - 1];
    const meanVal = statBounds(histData).mean;
    const labelDiv = el('div', 'spark-label');
    labelDiv.innerHTML = '<span>' + (m.headlineLabel || '') + '</span>' +
      '<span class="spark-vals"><span class="spark-cur" style="color:' + st.color + '">当前 ' + fmt(curVal) + '</span>' +
      '<span class="spark-mean">均值 ' + fmt(meanVal) + '</span></span>';
    body.appendChild(labelDiv);
    body.appendChild(sparkline(histData, st.color));
  }
  if (metrics.length === 0) {
    body.appendChild(elText('div', 'empty', '无数据'));
  } else {
    const kv = el('div', 'kv');
    const keys = m.key || metrics.slice(0, 4).map(x => x.name);
    const headlineMetric = sparklineShown && m.headline ? m.headline.replace(/^[a-z]+_/, '') : '';
    for (const spec of keys) {
      const mm = pickMetric(metrics, spec);
      if (!mm) continue;
      if (mm.name === headlineMetric) continue;
      kv.appendChild(elText('div', 'k', METRIC_NAMES[mm.name] || mm.name));
      const v = el('div', 'v');
      if (mm.name === 'smart_status') {
        v.textContent = mm.value >= 1 ? 'PASSED' : 'FAILED';
      } else if (mm.name === 'interface_status') {
        v.textContent = mm.value > 0 ? 'up' : 'down';
      } else if (mm.name === 'health_status') {
        const statusMap = {1: 'OK', 2: 'Warning', 3: 'Alarm', 4: 'Critical'};
        v.textContent = statusMap[mm.value] || '--';
      } else if (mm.name === 'driver_health') {
        v.textContent = mm.value === 0 ? '正常' : '异常';
      } else if (mm.name === 'error_code') {
        v.textContent = mm.value === 0 ? '无' : ((mm.labels || {}).error_codes || String(mm.value));
      } else {
        v.textContent = fmt(mm.value) + ' ' + (mm.unit || '');
      }
      kv.appendChild(v);
    }
    body.appendChild(kv);
  }
  card.appendChild(body);
  return card;
}

// ---- detail ----
function renderDetail(compKey, snap) {
  const page = document.getElementById('page');
  page.innerHTML = '';

  if (!collectors.some(c => c.component === compKey)) {
    const head = el('div', 'detail-head');
  head.appendChild(anchor('#/', '← 概览', 'btn'));
    head.appendChild(elText('span', 'detail-title', '未找到该部件'));
    page.appendChild(head);
    page.appendChild(elText('div', 'empty', '部件 "' + compKey + '" 未注册'));
    return;
  }

  const m = MANIFEST[compKey] || {};
  const compHealth = (snap.health && snap.health.components) ? snap.health.components[compKey] : null;
  const metrics = metricsFor(snap, compKey);
  const st = statusOf(compHealth ? compHealth.score : 0, compHealth ? compHealth.max : 0);

  const head = el('div', 'detail-head');
  head.appendChild(anchor('#/', '← 概览', 'back'));
  head.appendChild(elText('span', 'detail-title', compTitle(compKey)));
  const sc = el('div', 'detail-score');
  if (compHealth) {
    sc.innerHTML = '<b style="color:' + st.color + '">' + compHealth.score + '</b> / ' + compHealth.max +
      ' <span class="badge" style="background:' + st.color + '">' + st.label + '</span>';
  } else {
    sc.innerHTML = '<span class="badge na">无数据</span>';
  }
  head.appendChild(sc);
  page.appendChild(head);

  if (compHealth && compHealth.deductions && compHealth.deductions.length) {
    const dpanel = el('div', 'panel deductions-panel');
    const dph = el('div', 'panel-head');
    dph.appendChild(elText('span', '', '扣分项'));
    dph.appendChild(elText('span', 'sub', compHealth.deductions.length + ' 条'));
    dpanel.appendChild(dph);
    const dbody = el('div', 'panel-body');
    const d = el('div', 'deductions-list');
    for (const dd of compHealth.deductions) {
      const text = RULE_TEXT[dd.rule] || dd.rule;
      const item = el('div', 'deduction-item');
      item.innerHTML =
        '<span class="deduction-icon">⚠</span>' +
        '<span class="deduction-text">' + text + '</span>' +
        '<span class="deduction-value">-' + (Math.round(dd.penalty * 10) / 10) + ' 分</span>';
      d.appendChild(item);
    }
    dbody.appendChild(d);
    dpanel.appendChild(dbody);
    page.appendChild(dpanel);
  }

  // hardware specs for this component
  const allSpecs = snap.specs || [];
  const compSpecs = allSpecs.filter(m => m.component === compKey);
  if (compKey === 'memory') {
    const mt = memoryTotalMB(snap);
    if (mt) {
      compSpecs.push({ component: 'memory', name: 'mem_total', value: mt,
        labels: { capacity: fmtMB(mt) }, synthetic: true });
    }
  }
  if (compSpecs.length) {
    const spanel = el('div', 'panel');
    const sph = el('div', 'panel-head');
    sph.appendChild(elText('span', '', '硬件信息'));
    sph.appendChild(elText('span', 'sub', compSpecs.length + ' 条'));
    spanel.appendChild(sph);
    const sbody = el('div', 'panel-body');
    if (compKey === 'memory') {
      sbody.appendChild(specsGroupMemory(compSpecs));
    } else if (compKey === 'network') {
      var netCardGroup = renderNetworkCardGroup(compSpecs);
      if (netCardGroup) sbody.appendChild(netCardGroup);
    } else if (compKey === 'disk') {
      var diskGroup = renderDiskGroup(compSpecs);
      if (diskGroup) sbody.appendChild(diskGroup);
    } else {
      sbody.appendChild(specsGroup(compKey, compSpecs));
    }
    spanel.appendChild(sbody);
    page.appendChild(spanel);
  }

  // trends
  const series = componentSeries(compKey, snap.history || {});
  if (series.length) {
    const panel = el('div', 'panel');
    const ph = el('div', 'panel-head');
    ph.appendChild(elText('span', '', '趋势'));
    ph.appendChild(elText('span', 'sub', '近 ' + (snap.history_points || 60) + ' 个采样点'));
    panel.appendChild(ph);
    const body = el('div', 'panel-body trend');
    for (const s of series) {
      const cur = s.data[s.data.length - 1];
      const item = el('div', 'trend-item');
      item.appendChild(elText('div', 'tlabel', s.label));
      item.appendChild(elText('div', 'tval', '当前 ' + fmt(cur)));
      item.appendChild(renderChart(s.data, st.color, snap));
      body.appendChild(item);
    }
    panel.appendChild(body);
    page.appendChild(panel);
  }

  // all metrics (grouped by name)
  const mpanel = el('div', 'panel');
  const mph = el('div', 'panel-head');
  mph.appendChild(elText('span', '', '全部指标'));
  mph.appendChild(elText('span', 'sub', metrics.length + ' 条'));
  mpanel.appendChild(mph);
  const mbody = el('div', 'panel-body');
  if (metrics.length === 0) {
    mbody.appendChild(elText('div', 'empty', '无数据（采集器不可用或无硬件）'));
  } else {
    var diskNetMetrics = null;
    if (compKey === 'disk') {
      var classified = classifySpaceMetrics(metrics);
      diskNetMetrics = classified.network;
    }
    const groups = {};
    const order = [];
    for (const mt of metrics) {
      if (compKey === 'disk' && (mt.name === 'space_usage' || mt.name === 'space_detail')) {
        var lb = mt.labels || {};
        if (networkFSTypes[lb.fstype] || !(lb.device || '').startsWith('/dev/')) continue;
      }
      if (!groups[mt.name]) { groups[mt.name] = []; order.push(mt.name); }
      groups[mt.name].push(mt);
    }
    for (const name of order) {
      const items = groups[name];
      items.sort(metricSortCmp);
      const dispName = METRIC_NAMES[name] || name;
      const unit = items[0].unit || '';
      const grp = el('div', 'metric-group');
      const gh = el('div', 'metric-group-head');
      const groupKey = compKey + ':' + name;
      const collapsed = localStorage.getItem('mg:' + groupKey) === '1';
      const desc = METRIC_DESCRIPTIONS[compKey + ':' + name] || METRIC_DESCRIPTIONS[name] || '';
      gh.title = desc;
      gh.innerHTML = '<span class="metric-group-name">' + dispName + '</span>' +
        (unit ? '<span class="metric-group-unit">(' + unit + ')</span>' : '') +
        '<span class="metric-group-count">' + items.length + ' 条</span>' +
        '<span class="metric-group-toggle">' + (collapsed ? '▸' : '▾') + '</span>';
      const gb = el('div', 'metric-group-body');
      if (collapsed) gb.style.display = 'none';
      if (compKey === 'disk' && name === 'space_detail') {
        gb.appendChild(renderSpaceDetailGroup(items));
      } else if (compKey === 'memory' && name === 'swap_detail') {
        gb.appendChild(renderSwapDetailGroup(items));
      } else if (compKey === 'disk' && (name === 'iops' || name === 'throughput')) {
        gb.appendChild(renderDeviceDirectionGroup(items));
      } else if (compKey === 'network' && (name === 'throughput' || name === 'packet_count')) {
        gb.appendChild(renderInterfaceDirectionGroup(items));
      } else if (compKey === 'network' && name === 'interface_status') {
        gb.appendChild(renderInterfaceStatusGroup(items));
      } else if (compKey === 'chassis' && name === 'fan_speed') {
        gb.appendChild(renderFanSpeedGroup(items));
      } else if (compKey === 'disk' && name === 'smart_status') {
        gb.appendChild(renderSmartStatusGroup(items));
      } else if (compKey === 'network' && name === 'error_count') {
        gb.appendChild(renderErrorCountGroup(items));
      } else {
        for (const mt of items) {
          const labels = mt.labels ? Object.entries(mt.labels).map(([k, v]) => k + '=' + v).join(', ') : '';
          const row = el('div', 'metric-row');
          let valStr;
          if (mt.name === 'health_status') {
            const statusMap = {1: 'OK', 2: 'Warning', 3: 'Alarm', 4: 'Critical'};
            const statusColor = {1: 'var(--ok)', 2: 'var(--warn)', 3: 'var(--crit)', 4: 'var(--crit)'};
            const statusText = statusMap[mt.value] || '--';
            valStr = '<span style="color:' + (statusColor[mt.value] || 'var(--muted)') + ';font-weight:600">' + statusText + '</span>';
          } else if (mt.name === 'driver_health') {
            valStr = mt.value === 0
              ? '<span style="color:var(--ok);font-weight:600">正常</span>'
              : '<span style="color:var(--crit);font-weight:600">异常</span>';
          } else if (mt.name === 'error_code') {
            if (mt.value === 0) {
              valStr = '<span style="color:var(--ok)">无</span>';
            } else {
              const codes = (mt.labels || {}).error_codes || '';
              valStr = '<span style="color:var(--crit);font-weight:600">' + codes + '</span>';
            }
          } else {
            valStr = fmt(mt.value) + (mt.unit ? ' ' + mt.unit : '');
          }
          const cleanLabels = mt.labels ? Object.entries(mt.labels)
            .filter(([k]) => !(mt.name === 'error_code' && k === 'error_codes'))
            .map(([k, v]) => k + '=' + v).join(', ') : '';
          row.innerHTML =
            '<span class="metric-val">' + valStr + '</span>' +
            '<span class="metric-labels">' + cleanLabels + '</span>';
          gb.appendChild(row);
        }
      }
      gh.onclick = () => {
        const open = gb.style.display !== 'none';
        gb.style.display = open ? 'none' : '';
        gh.querySelector('.metric-group-toggle').textContent = open ? '▸' : '▾';
        localStorage.setItem('mg:' + groupKey, open ? '1' : '0');
      };
      grp.appendChild(gh);
      grp.appendChild(gb);
      mbody.appendChild(grp);
      if (compKey === 'disk' && name === 'space_detail' && diskNetMetrics && diskNetMetrics.length > 0) {
        var netGroup = renderNetworkStorageGroup(diskNetMetrics);
        if (netGroup) mbody.appendChild(netGroup);
      }
    }
  }
  mpanel.appendChild(mbody);
  page.appendChild(mpanel);
}

// ---- render dispatch ----
function render() {
  renderNav();
  if (!lastSnapshot) {
    document.getElementById('page').innerHTML = '<div class="empty">正在加载数据…</div>';
    return;
  }
  renderPill(lastSnapshot);
  const route = currentRoute();
  if (route === 'overview') renderOverview(lastSnapshot);
  else renderDetail(route, lastSnapshot);
}

// ---- data fetching ----
async function fetchCollectors() {
  try {
    const r = await fetch('/api/collectors', { cache: 'no-store' });
    if (r.ok) collectors = await r.json();
  } catch (e) { /* server starting */ }
}

async function fetchSnapshot() {
  try {
    const r = await fetch('/api/snapshot', { cache: 'no-store' });
    if (!r.ok) { showBanner('快照尚未就绪，等待首次采集…', true); return; }
    lastSnapshot = await r.json();
    hideBanner();
    render();
  } catch (e) {
    showBanner('获取数据失败：' + e.message, true);
  }
}

async function fetchConfigData() {
  try {
    const r = await fetch('/api/config', { cache: 'no-store' });
    if (!r.ok) return;
    const c = await r.json();
    document.getElementById('intervalInput').value = Math.round(refreshIntervalMs / 1000);
    if (c.version) {
      appVersion = c.version;
      const el = document.querySelector('.brand .subtitle');
      if (el) el.textContent = 'v' + appVersion + ' · 设备健康度';
    }
    if (c.started_at) {
      const prev = sessionStorage.getItem('webStartedAt');
      if (prev && prev !== String(c.started_at)) {
        for (let i = localStorage.length - 1; i >= 0; i--) {
          const k = localStorage.key(i);
          if (k && k.startsWith('mg:')) localStorage.removeItem(k);
        }
      }
      sessionStorage.setItem('webStartedAt', String(c.started_at));
    }
  } catch (e) { /* ignore */ }
}

function startPolling() {
  stopPolling();
  if (!autoOn) return;
  pollTimer = setInterval(fetchSnapshot, refreshIntervalMs);
  fetchSnapshot();
}
function stopPolling() { if (pollTimer) { clearInterval(pollTimer); pollTimer = null; } }

async function applyInterval() {
  const sec = parseInt(document.getElementById('intervalInput').value, 10);
  if (!sec || sec < 1) { showBanner('请输入有效的刷新间隔（秒）', true); return; }
  refreshIntervalMs = sec * 1000;
  startPolling();
  showBanner('刷新间隔已更新为 ' + sec + ' 秒', false);
}

async function manualRefresh() {
  try { await fetch('/api/refresh', { method: 'POST' }); } catch (e) { /* ignore */ }
  setTimeout(fetchSnapshot, 400);
}

function showBanner(msg, isError) {
  const b = document.getElementById('banner');
  b.textContent = msg;
  b.classList.remove('hidden');
  b.style.background = isError ? '#fee2e2' : '#dcfce7';
  b.style.color = isError ? '#991b1b' : '#166534';
}
function hideBanner() { document.getElementById('banner').classList.add('hidden'); }

// ---- wiring ----
document.getElementById('applyBtn').addEventListener('click', applyInterval);
document.getElementById('refreshBtn').addEventListener('click', manualRefresh);
document.getElementById('autoToggle').addEventListener('change', (e) => {
  autoOn = e.target.checked;
  if (autoOn) startPolling(); else stopPolling();
});
document.getElementById('intervalInput').addEventListener('keydown', (e) => {
  if (e.key === 'Enter') applyInterval();
});
window.addEventListener('hashchange', render);

(async function init() {
  await fetchCollectors();
  await fetchConfigData();
  startPolling();
})();
