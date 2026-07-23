# CATMonitor — 慢节点（Straggler）检测

AI 智算集群中识别性能劣化 NPU 卡的两道防线检测体系。

---

## 目录结构

```
straggler/
├── main.go                 # 统一入口
├── README.md               # 本文件
├── resource/               # 第一道防线：资源指标检查（KPI）
│   ├── types.go            #   数据结构 & 配置
│   ├── parser.go           #   CSV 解析
│   ├── aggregator.go       #   1分钟截尾均值聚合
│   ├── baseline.go         #   历史基线构建
│   ├── space_detector.go   #   空间维度检测（Peer对比）
│   ├── time_detector.go    #   时间维度检测（自对比）+ 趋势
│   ├── fusion.go           #   二维交叉验证 + 先计算后通信
│   ├── rootcause.go        #   根因定界推理
│   └── report.go           #   管线编排 + JSON/文本报告
├── profiling/              # 第二道防线：Profiling 检查
│   ├── dataparse/          #   数据清洗（SQLite → CSV）
│   │   ├── utils.go
│   │   ├── data_process.go
│   │   └── scenario_segregate.go
│   └── detector/           #   检查算法
│       ├── constants.go
│       ├── data_parser.go
│       ├── data_handler.go
│       ├── detection.go
│       └── clustering.go   #   均质化聚类算法
├── config/                 # 共享配置
│   └── config.go
├── utils/                  # 共享工具
│   └── tools.go
├── report/                 # Profiling 报告生成
│   └── report.go
├── DESIGN.md               # Profiling 检测设计文档
├── DESIGN_NPU_RESOURCE.md  # KPI 资源检测设计文档
└── SPEC.md                 # Profiling 检测技术规范
```

---

## 使用流程

### 第一步：采集 KPI 数据（常态化）

使用 `kpi_collect.sh` 脚本在训练节点上采集 NPU 资源指标，输出为 CSV 文件。

```bash
# 在训练节点上运行（持续采集，分钟级）
./kpi_collect.sh > /data/kpi_$(date +%Y%m%d).csv
```

CSV 格式：
```
timestamp,NPU_CARD_POWER,NPU_CARD_TEMP,...,CPU_average
1784547926,"{""0"":1628,...}","{""0"":47,...}",...,"{""cpu1"":""4.26"",...}"
```

采集建议：
- 保留最近 15 天数据用于历史基线
- 单文件或按天分文件均可（目前支持单文件输入）

---

### 第二步：采集 Profiling 数据（按需触发）

当 KPI 指标检测未发现异常但仍怀疑有性能问题时，启用 Profiler 采集。

```bash
# 在训练脚本中插入 Profiler API（Ascend PyTorch）
# 采集后会在指定目录生成 ascend_pytorch_profiler_*.db 文件
```

Profiler 输出结构：
```
/data/profiler_output/
├── ascend_pytorch_profiler_0.db
├── ascend_pytorch_profiler_1.db
├── ...
└── ascend_pytorch_profiler_N.db
```

---

### 第三步：运行检测

#### 3a. 仅 KPI 资源指标检测

```bash
cd features/straggler
go run . --kpi-csv=/data/kpi.csv
```

输出：
- `./npu_resource_detection_result.json` — JSON 格式详细结果
- `./analysis_result/npu_resource_detection_report.log` — 文本报告

#### 3b. KPI + Profiling 联合检测

```bash
go run . path=/data/profiler_output --kpi-csv=/data/kpi.csv degradation=0.3
```

检测顺序：
1. 先跑 KPI 检测（轻量、无侵入）
2. KPI 发现异常 → 直接输出定界结果
3. KPI 未发现异常 → 自动 fallback 到 Profiling 精查
4. Profiling 输出慢计算/慢通信/慢CPU/Bubble 检测结果

#### 3c. 仅 Profiling 检测（无 KPI 数据时）

```bash
go run . path=/data/profiler_output degradation=0.3
```

---

### 第四步：阅读检测报告

#### KPI 检测报告解读

报告结构：
```
================================================================================
  NPU 资源 KPI 异常检测报告
================================================================================

[SUMMARY]
  CSV:        /data/kpi.csv
  数据点:     21600 (分钟级)
  基线窗口:   360h
  检测窗口:   1h
  总卡数:     8
  ✓ 正常:     7
  ✗ 确认异常: 1
  ⚡ 早期劣化: 0
  ◇ 个体差异: 0

================================================================================
  确认异常详情
================================================================================

  Card 3 | thermal_throttle | 置信度: high
  建议: 热降频。检查风扇转速/风道堵塞/机房环境温度
  异常指标:
    temp                space=3.2 time=4.1 quadrant=confirmed_anomaly
    aicore_freq         space=5.0 time=6.0 quadrant=confirmed_anomaly
```

关键字段说明：
| 字段 | 含义 |
|------|------|
| `confirmed_anomaly` | 时间+空间双维确认异常（高置信度） |
| `early_degradation` | 仅时间维异常，卡在偏离自身基线（关注） |
| `individual_variance` | 仅空间维异常，卡一贯如此（非故障） |
| `anomaly_category` | `compute`（计算类）/ `communication`（通信类） |
| `root_cause.category` | 定界结果：热降频/散热不足/网络链路/Straggler 等 |
| `secondary_comm_anomalies` | 计算异常导致的继发性通信异常 |

#### Profiling 检测报告解读

报告路径：`analysis_result/detection_report.log`

包含：
- 并行域拓扑摘要
- 四类检测结果（慢计算/慢通信/慢CPU/NPU Bubble）
- ZP_Kernel / ZP_Host 柱状图排序
- 各通信域分组对比

---

### 第五步：根据定界结果采取行动

| 定界结果 | 排查方向 |
|---------|---------|
| `thermal_throttle` | 检查风扇转速、风道堵塞、机房温度 |
| `cooling_insufficient` | 检查散热器接触、硅脂老化 |
| `forced_downclock` | 检查驱动/固件频率策略 |
| `straggler` | 触发 Profiling 精查，确认计算慢/通信慢根因 |
| `network_link_issue` | 检查光模块、光纤、交换机端口 CRC |
| `network_congestion` | 检查 PFC 配置、队列 buffer、ECN |
| `network_packet_loss` | 检查 RoCE ECN/DCQCN 参数 |
| `hardware_fault` | 隔离该卡，安排硬件诊断 |

---

## 配置参数

### KPI 检测参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--kpi-csv` | (必需) | KPI CSV 文件路径 |
| `degradation` | 0.3 | 灵敏度（也影响空间Z阈值） |
| 聚合窗口 | 60s | 1分钟聚合窗口（代码内配置） |
| 截尾比例 | 0.25 | 去前后各25%（代码内配置） |
| 空间Z阈值 | 2.5 | Peer对比 Z-Score 阈值 |
| 时间Z阈值 | 2.0 | 自对比 Z-Score 阈值 |
| 基线窗口 | 360h | 15天历史基线 |
| 检测窗口 | 1h | 最近1小时检测窗口 |

### Profiling 检测参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `path` | (必需) | 包含 `ascend_pytorch_profiler_*.db` 的目录 |
| `degradation` | 0.3 | 劣化灵敏度，CalThreshold=1+degradation，CommThreshold=1+degradation×5 |

---

## 构建

```bash
# Linux ARM64（目标平台）
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 /AIdata/LBW/sdk/go/bin/go build -o slowNodeDetection .
```

全静态二进制，无外部依赖（除 SQLite 驱动）。

---

## 设计文档

- [DESIGN_NPU_RESOURCE.md](./DESIGN_NPU_RESOURCE.md) — KPI 资源指标检测设计
- [DESIGN.md](./DESIGN.md) — Profiling 检测设计
- [SPEC.md](./SPEC.md) — Profiling 检测技术规范
