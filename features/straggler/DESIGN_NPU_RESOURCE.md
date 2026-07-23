# NPU 资源指标异常卡定位定界 — 设计文档

## 1. 概述

### 1.1 背景与动机

现有慢节点检测基于 Ascend PyTorch Profiler Level0 数据，做**单次快照**检测。Profiler 存在两个限制：

- **性能开销大**：Profiler API 侵入式采集，产生大量 `.db` 文件，不适合常态化
- **无时间维度**：仅单次快照，无法利用历史趋势

本方案基于 `kpi_collect.sh` 采集的**分钟级 NPU 资源 KPI CSV**（保留 **15 天**），以**无侵入、常态化**方式实现：

- **时间维度 + 空间维度 双维检测**：既与同伴比（空间），也与自己的历史基线比（时间）
- **先计算后通信的因果检测顺序**：计算慢会导致通信慢，先检计算可避免误判
- **1分钟截尾均值抗噪**：排序→去两端 25%→中间 50% 均值，单采样点噪声不污染检测结果

### 1.2 与 Profiler 检测的定位

```
                    ┌─────────────────────────────────────────┐
                    │         慢节点检测体系                     │
                    │                                         │
                    │  ┌──────────────────────┐               │
                    │  │ KPI 资源指标检测      │  ← 第一道防线  │
                    │  │ (本方案)              │    轻量、常态化 │
                    │  │ 时间维度 + 空间维度    │    15天数据    │
                    │  └─────────┬────────────┘               │
                    │            │                            │
                    │            │ 未发现异常                   │
                    │            ▼                            │
                    │  ┌──────────────────────┐               │
                    │  │ Profiler 慢节点检测   │  ← 第二道防线  │
                    │  │ (已有)                │    精查、深度   │
                    │  │ 单次快照、4维检测      │    按需触发    │
                    │  └──────────────────────┘               │
                    └─────────────────────────────────────────┘
```

**检测顺序：先 KPI → KPI 查不到异常 → 再触发 Profiling**

- KPI 检测覆盖面广（热降频、网络错误、功耗异常等硬件级问题），且无额外开销
- Profiling 检测覆盖 KPI 看不到的软件级问题（Kernel 慢、通信慢），按需触发避免常态开销
- KPI 发现异常 → 可以直接定界输出，也可选择再跑 Profiling 做交叉验证
- KPI 未发现异常 → 自动 fallback 到 Profiling 检测

---

## 2. 数据格式

### 2.1 CSV 结构

```
timestamp,NPU_CARD_POWER,NPU_CARD_TEMP,NPU_CARD_AICORE_FREQ,NPU_CARD_AICORE_UTIL,NPU_CARD_HBM_UTIL,NPU_TX_BANDWIDTH,NPU_RX_PFC_PKT,NPU_ROCE_TX_ERR_PKT,NPU_ROCE_OUT_OF_ORDER,NPU_ROCE_NEW_PKT_RTY,NPU_NIC_RX_ALL_PKG,CPU_average
1784547926,"{""0"":1628,...,""7"":1688}","{""0"":47,...,""7"":50}",...,"{""cpu1"":""4.26"",...}"
```

| 列 | 含义 | 单位 | 类型 |
|---|------|------|------|
| `timestamp` | 采集时间戳 | Unix秒 | int64 |
| `NPU_CARD_POWER` | 每卡功耗 | W | JSON dict[card→float] |
| `NPU_CARD_TEMP` | 每卡温度 | ℃ | JSON dict[card→float] |
| `NPU_CARD_AICORE_FREQ` | 每卡 AI Core 频率 | MHz | JSON dict[card→float] |
| `NPU_CARD_AICORE_UTIL` | 每卡 AI Core 利用率 | % | JSON dict[card→float] |
| `NPU_CARD_HBM_UTIL` | 每卡 HBM 利用率 | % | JSON dict[card→float] |
| `NPU_TX_BANDWIDTH` | 每卡发送带宽 | ? | JSON dict[card→float] |
| `NPU_RX_PFC_PKT` | 每卡接收 PFC 暂停帧 | 包数 | JSON dict[card→float] |
| `NPU_ROCE_TX_ERR_PKT` | 每卡 RoCE 发送错误包 | 包数 | JSON dict[card→float] |
| `NPU_ROCE_OUT_OF_ORDER` | 每卡 RoCE 乱序包 | 包数 | JSON dict[card→float] |
| `NPU_ROCE_NEW_PKT_RTY` | 每卡 RoCE 重传包 | 包数 | JSON dict[card→float] |
| `NPU_NIC_RX_ALL_PKG` | 每卡 NIC 接收总包 | 包数 | JSON dict[card→float] |
| `CPU_average` | 各 CPU 平均利用率 | % | JSON dict[cpu→string] |

### 2.2 数据分区

15 天数据按用途分为两个窗口：

```
│←──────────── 历史基线窗口 (baseline) ────────────│← 检测窗口 (detection) ──│
│                                                    │  最近 N 个时间点         │
│  用于构建每张卡的历史分布 (mean, std, percentiles)   │  当前待检测数据          │
│  默认：除检测窗口外的全部历史数据                    │  默认：最近 1 小时       │
```

- **历史基线窗口**：为该卡的每个指标建立"正常值范围"（自身的统计分布）
- **检测窗口**：最近一段时间的数据，同时对每行做空间维度检测
- 窗口大小可通过配置调整

---

## 3. 数据预处理：1分钟截尾均值聚合

### 3.1 为什么需要预处理

原始 KPI 采集频率可能很高（秒级甚至亚秒级），单个采样点受瞬时波动、采集噪声、短时尖峰影响大。直接用裸数据点做检测会导致：

- **误报**：一个瞬时尖峰被标记为异常
- **漏报**：持续偏高但因单点波动大被统计方法稀释

解决方案：**将 1 分钟内的所有原始采样点聚合为一个稳健的统计量**，作为后续检测的"一个数据点"。

### 3.2 截尾均值算法（Midmean）

```
输入：1分钟内某卡某指标的 N 个原始采样值
输出：该分钟该卡该指标的聚合值

步骤：
  1. 排序：将 N 个值升序排列 → sorted[0..N-1]
  2. 截尾：去掉前 25% 和后 25%，取中间 50%
     trim = floor(N * 0.25)
     保留区间 = sorted[trim .. N-1-trim]
  3. 平均：对保留区间内的值取算术平均
     midmean = avg(sorted[trim .. N-1-trim])
  4. 返回 midmean 作为该分钟该卡该指标的聚合值
```

**示例**：某卡在 1 分钟内采集了 20 个温度值（N=20）

```
原始值: [45, 47, 46, 48, 62, 47, 46, 49, 45, 48, 47, 46, 51, 47, 46, 48, 45, 47, 46, 49]
排序后: [45, 45, 45, 46, 46, 46, 46, 46, 47, 47, 47, 47, 47, 48, 48, 48, 49, 49, 51, 62]
         │←─ 前5个(25%)去掉 ─→│←────── 中间10个(50%)保留 ──────→│←─ 后5个(25%)去掉 ─→│
trim = 5
保留区间 = [46, 46, 46, 46, 47, 47, 47, 47, 47, 48]
midmean = (46+46+46+46+47+47+47+47+47+48) / 10 = 46.7

对比：
  - 全量均值 = 48.1  （被 62 和 51 拉高）
  - 中位数   = 47.0  （只看中间一个点）
  - 截尾均值 = 46.7  ← 最接近真实稳定温度，不被尖峰污染
```

### 3.3 特殊指标的处理

| 指标类型 | 聚合方式 | 理由 |
|---------|---------|------|
| 连续型指标（TEMP, POWER, FREQ, UTIL, BANDWIDTH） | 截尾均值 | 需要消除尖峰，取稳定代表值 |
| 计数型指标（ERR_PKT, RETRY, OUT_OF_ORDER, PFC_PKT） | **累加** | 错误包是累积计数器，应取 1 分钟内的总和而非均值 |
| NIC_RX_ALL_PKG | 截尾均值 | 接收包数波动大，截尾后更稳定 |
| CPU_average | 截尾均值 | 同上 |

对于计数型指标，聚合时注意处理计数器回绕（counter wrap）：
```
该分钟增量 = counter[t_end] - counter[t_start]
if 增量 < 0: 增量 += 2^64 （处理回绕）
if 增量 == 0: 正常，无错误
if 增量 > 0: 聚合值 = 增量
```

### 3.4 聚合前后数据量变化

```
聚合前：N 行/分钟（N = 采集频率 × 60）
  e.g., 每秒采集 1 次 → 60 行/分钟 → 15天 = 1,296,000 行

聚合后：1 行/分钟
  e.g., 15天 = 21,600 行

聚合后的每行数据结构保持不变（timestamp 取该分钟起始时间），
仅 value 从"瞬时采样值"变为"截尾均值/累加和"。
```

### 3.5 在管线中的位置

```
原始 CSV（秒级）
  │
  ▼
[CSV 解析]  逐行解析 → rawRows
  │
  ▼
[1分钟聚合]  ← 本步骤
  对每 1 分钟窗口：
    按(分钟, 卡号)分组 → 排序 → 截尾25% → 中间50%均值
    （或计数型指标：取增量累加）
  输出：分钟级聚合行 (每分钟 1 行)
  │
  ▼
[窗口划分]  → 基线窗口 + 检测窗口
  │
  ▼
[双维检测] ...
```

---

## 4. 双维检测模型

### 4.1 核心思想

每张卡在每个指标上有两个异常分数：

```
                    │  空间维度 (Space)          │  时间维度 (Time)
────────────────────┼────────────────────────────┼────────────────────────────
  比较对象           │  同一时间点，与其他卡比      │  同一张卡，与自己的历史比
  回答的问题         │  "这张卡比别人差吗？"        │  "这张卡比以前差了吗？"
  数据来源           │  检测窗口内每个时间点        │  历史基线窗口的统计分布
  检测方法           │  Z-Score / IQR             │  历史 Z-Score / 分位数
────────────────────┼────────────────────────────┼────────────────────────────
  典型场景           │  某卡温度比同伴高 10℃        │  某卡温度从 45℃ 爬升到 55℃
                    │  → 空间异常                 │  → 时间异常（但可能还在同伴范围内）
```

### 4.2 二维交叉判定

```
                    时间维度
                正常         异常
          ┌─────────────┬─────────────┐
  空 正常 │   ✓ 正常     │  ⚡ 早期劣化 │
  间      │             │  (时间漂移)  │
  维      ├─────────────┼─────────────┤
  度 异常 │  ◇ 个体差异  │  ✗ 确认异常 │
          │  (历史如此)  │  (双维确认)  │
          └─────────────┴─────────────┘
```

| 象限 | 空间 | 时间 | 判定 | 含义 |
|------|------|------|------|------|
| 正常 | 正常 | 正常 | ✓ 正常 | 该卡一切正常 |
| 早期劣化 | 正常 | 异常 | ⚡ 关注 | 卡正在偏离自身历史基线（如温度缓慢爬升），但尚未在同伴中突出。应关注趋势 |
| 个体差异 | 异常 | 正常 | ◇ 提示 | 卡与同伴不同，但这是该卡的一贯表现（如体质差异导致功耗偏高）。非故障 |
| 确认异常 | 异常 | 异常 | ✗ 告警 | 既偏离自身历史，又偏离同伴群体。高置信度故障 |

**判定优先级**：
- **确认异常**（双维异常）→ 高置信度告警，直接定界
- **早期劣化**（仅时间异常）→ 中度关注，跟踪趋势。若持续恶化进入确认异常
- **个体差异**（仅空间异常）→ 低优先级提示。可标记为该卡的"个性"纳入基线
- **正常** → 不告警

### 4.3 双维评分公式

```
对指标 m，卡 c：

  空间分 S_space[m][c] = Z-Score(c 的当前值 vs 同一时间点所有卡的分布)
                         = |v[c] - mean(all_cards_at_t)| / std(all_cards_at_t)

  时间分 S_time[m][c]  = Z-Score(c 的当前值 vs c 自身历史分布)
                         = |v[c] - mean(c_baseline)| / std(c_baseline)

  融合分 S_final[m][c] = α * S_space[m][c] + β * S_time[m][c]

  默认权重 α=0.4, β=0.6（时间维度权重略高，因为自身偏离更有信息量）
```

- 空间分对检测窗口内**每个时间点**分别计算，取均值作为该卡的最终空间分
- 时间分将检测窗口内的均值与历史基线比较，一次计算
- 网络错误类指标不适用 Z-Score（正常值恒为 0），改用绝对阈值

---

## 5. 检测算法设计

### 5.1 空间维度检测（Peer Comparison）

对检测窗口内的每个时间点，逐指标执行：

**方法 A：Z-Score（默认）**

```
对时间点 t，指标 m，所有卡的值 V = [v0, v1, ..., v7]：
  mean = avg(V), std = stdev(V)
  if std == 0 → 跳过（所有卡一致）
  对每张卡 i: z[i] = |vi - mean| / std
  if z[i] > zThreshold (默认 2.5) → 标记该时间点空间异常
```

适用：POWER, TEMP, AICORE_UTIL, HBM_UTIL, TX_BANDWIDTH

**方法 B：IQR**

```
Q1, Q3 = 25th, 75th percentile
IQR = Q3 - Q1
异常: vi < Q1 - 1.5*IQR 或 vi > Q3 + 1.5*IQR
```

适用：PFC_PKT（可能有少量卡出现尖峰）

**方法 C：均质化聚类**（复用现有 `spacedetector`，`--method=cluster`）

**特殊处理**：
- **AICORE_FREQ**：频率为固定档位值。某卡值 < 其他卡的最小值即直接判定（不依赖统计）
- **网络错误类**（ERR_PKT, RETRY, OUT_OF_ORDER, PFC_PKT）：正常值恒为 0，> 0 即异常
- **CPU_average**：机器粒度，不与卡级混合，独立检测

### 5.2 时间维度检测（Self Comparison）

对每张卡，用历史基线窗口的数据建立该卡每个指标的分布：

```
对卡 c，指标 m：
  历史基线值 B = [v_t1, v_t2, ..., v_tN]  （基线窗口内该卡所有值）
  baseline_mean = avg(B)
  baseline_std  = stdev(B)

  检测窗口内的均值 current_mean = avg(检测窗口内的值)
  时间 Z-Score = |current_mean - baseline_mean| / baseline_std

  if baseline_std == 0 → 跳过（历史无波动）
  if 时间 Z-Score > tThreshold (默认 2.0) → 时间维度异常
```

**趋势增强**：对历史基线窗口 + 检测窗口的整体数据做线性回归：
- `value = slope * timestamp + intercept`
- 若 `slope > 0` 且 R² > 0.6：该指标在持续恶化
- 趋势信息作为定界推理的辅助证据

**基线更新策略**：
- 初始基线：首次运行时用全部历史数据建立
- 定期更新：每 24 小时用最近 15 天数据重建基线
- 异常点的值不纳入基线（避免污染）

### 5.3 指标分类：计算类 vs 通信类

KPI 指标天然分属两个层面，且存在**因果依赖**：计算慢的卡必然表现出通信慢（无法按时参与集合通信），反之不成立。

| 类别 | 指标 | 含义 | 异常方向 |
|------|------|------|---------|
| **计算** | `AICORE_FREQ` | AI Core 频率 | ↓ 降频 |
| | `AICORE_UTIL` | AI Core 利用率 | ↓ 计算没跑满 |
| | `HBM_UTIL` | HBM 利用率 | ↓ 内存空闲 |
| | `TEMP` | 温度 | ↑ 过热 |
| | `POWER` | 功耗 | ↓ 空载 / ↑ 过热 |
| **通信** | `TX_BANDWIDTH` | 发送带宽 | ↓ 通信受限 |
| | `RX_PFC_PKT` | PFC 暂停帧 | ↑ 网络拥塞 |
| | `ROCE_TX_ERR_PKT` | RoCE 发送错误 | ↑ 链路故障 |
| | `ROCE_OUT_OF_ORDER` | 乱序包 | ↑ 网络质量问题 |
| | `ROCE_NEW_PKT_RTY` | 重传包 | ↑ 网络丢包 |

### 5.4 检测顺序：先计算后通信

**因果逻辑**：计算异常 → 卡无法按时完成计算 → 在集合通信中迟到 → 通信指标也表现为异常。如果先检测通信，会把计算慢导致的通信异常误判为网络问题。

```
对每张卡：
  ┌─────────────┐
  │ 1. 检测计算  │  ← FREQ, AICORE_UTIL, HBM_UTIL, TEMP, POWER
  └──────┬──────┘
         │
    ┌────┴────┐
    │ 计算异常? │
    └────┬────┘
         │
   ┌─────┴─────┐
   │ 是         │ 否
   ▼            ▼
┌──────────┐  ┌─────────────┐
│ 输出:     │  │ 2. 检测通信  │  ← TX_BANDWIDTH, ERR_PKT, PFC_PKT, RETRY, OUT_OF_ORDER
│ 计算类异常 │  └──────┬──────┘
│ 通信指标   │         │
│ 标记为     │    ┌────┴────┐
│ "可能继发" │    │ 通信异常? │
└──────────┘    └────┬────┘
                     │
               ┌─────┴─────┐
               │ 是         │ 否
               ▼            ▼
          ┌──────────┐  ┌──────┐
          │ 输出:     │  │ 正常  │
          │ 通信类异常 │  └──────┘
          │ (独立)    │
          └──────────┘
```

**关键规则**：
- 计算类任一指标异常 → 该卡归类为"计算异常"，通信指标**不独立检测**（标记为"可能继发于计算异常"）
- 计算类全部正常 → 才检测通信指标 → 通信异常可确认为**独立网络问题**

这个顺序也决定了定界规则的优先级：计算类规则优先匹配，通信类规则仅在计算正常时生效。

### 5.5 指标元信息定义

---

## 6. 根因定界

### 6.1 定界规则表

基于异常指标的组合模式推断根因。规则按**先计算后通信**的顺序排列，且通信类规则仅在计算类指标正常时生效（避免将计算慢导致的继发通信异常误判为网络问题）。

**计算类规则**（优先匹配）：

| # | 模式 | 根因推断 | 置信度 | 建议动作 |
|---|------|---------|--------|---------|
| C1 | TEMP↑ + FREQ↓ | **热降频** | 高 | 检查风扇转速/风道堵塞/机房环境温度 |
| C2 | TEMP↑ + POWER↑ + FREQ— | **散热能力不足** | 高 | 检查散热器接触/硅脂老化/风扇故障 |
| C3 | FREQ↓ + TEMP— | **强制降频（非热）** | 中 | 检查驱动/固件的频率策略配置 |
| C4 | POWER↓ + AICORE_UTIL↓ + HBM_UTIL↓ | **Straggler（卡空闲等待）** | 高 | 该卡可能在等通信/等数据，触发 Profiling 精查 |
| C5 | AICORE_UTIL↓ + HBM_UTIL— | **计算负载不均** | 中 | 检查数据分发策略/模型并行切分是否均衡 |
| C6 | HBM_UTIL↓ + AICORE_UTIL— | **内存带宽瓶颈** | 低 | 检查 HBM 访问模式/是否有大量 cache miss |
| C7 | TEMP↑ + POWER— + FREQ— | **温度传感器漂移** | 中 | 交叉验证功率数据（真发热必伴随功率↑） |
| C8 | 多指标同时异常（≥4个，含计算类） | **板卡综合性硬件故障** | 高 | 建议隔离该卡，安排硬件诊断/更换 |
| C9 | 单项 TEMP↑ 孤立 | **局部热点/传感器个体差异** | 低 | 持续观察，若升级为双维异常则按 C1 处理 |
| C10 | 单项 POWER↑ 孤立 | **功耗计量偏差** | 低 | 交叉验证：功率↑应伴随温度↑，否则可能是计量误差 |

**通信类规则**（仅在计算类指标全部正常时生效）：

| # | 模式 | 根因推断 | 置信度 | 建议动作 |
|---|------|---------|--------|---------|
| N1 | ERR_PKT↑（持续） | **网络物理链路故障** | 高 | 检查光模块/光纤/交换机端口 CRC 错误 |
| N2 | PFC_PKT↑（持续） | **网络拥塞（PFC 风暴）** | 高 | 检查交换机 PFC 配置/队列 buffer/ECN 标记 |
| N3 | OUT_OF_ORDER↑ + RETRY↑ | **RoCE 网络丢包乱序** | 高 | 检查 RoCE 路径 ECN 配置/DCQCN 参数 |
| N4 | TX_BANDWIDTH↓ + AICORE_UTIL— | **通信带宽受限** | 中 | 检查网卡协商速率/PCIe 带宽/光模块型号 |

> **注意**：若某卡同时命中计算类和通信类规则，以计算类为准，通信异常标记为"可能继发于计算异常"，不独立告警。

### 6.2 定界实现

```
func BoundRootCause(cardID, anomalyMatrix, trends) RootCauseResult:
    1. 检查计算类指标异常集
    2. 若计算类有异常 → 仅匹配计算类规则 C1-C10
       → 通信类异常标记为"可能继发于计算异常"
    3. 若计算类全部正常 → 才匹配通信类规则 N1-N4
    4. 按规则表优先级逐条匹配（精确匹配 + 允许额外指标）
    5. 匹配成功 → 返回根因类别 + 置信度 + 证据 + 建议
    6. 无匹配 → "unknown"，输出全量异常指标供人工分析
```

### 6.3 跨卡关联分析

部分根因会同时影响多张卡：

| 异常卡分布 | 推断 | 示例 |
|-----------|------|------|
| 同一物理节点的所有卡 | **服务器级故障**（散热/电源） | 节点 A 的 8 张卡温度同时升高 |
| 同一通信域的所有卡 | **网络级故障**（交换机端口） | tp 组内 4 张卡 ERR_PKT 同时增加 |
| 全部卡 | **任务级故障**（训练 hang） | 所有卡 UTIL↓ + POWER↓ |
| 个别孤立卡 | **板卡级故障** | 单卡 TEMP↑ + FREQ↓ |

关联分析通过 `CPU_average` 字段推断物理节点归属（每 CPU 通常对应特定的一组 NPU 卡）。

---

## 7. 检测流程：KPI 优先 + Profiling 降级

### 7.1 整体流程

```
                    ┌─────────────┐
                    │  CLI 入口    │
                    └──────┬──────┘
                           │
                           ▼
                    ┌──────────────┐
                    │ 有 KPI CSV？  │
                    └──────┬───────┘
                           │
               ┌───────────┴───────────┐
               │ 是                    │ 否
               ▼                       ▼
    ┌──────────────────────────┐  ┌──────────────────┐
    │ KPI 资源指标检测           │  │ Profiler 慢节点检测│
    │                          │  │ (已有流程)         │
    │ 1. CSV解析 + 1分钟聚合    │  └──────────────────┘
    │ 2. 窗口划分              │
    │ 3. 时间+空间基线建立      │
    │                          │
    │ ┌── 先检测计算 ─────────┐ │
    │ │ FREQ/UTIL/HBM/        │ │
    │ │ TEMP/POWER            │ │
    │ │ → 异常? → 计算类定界   │ │
    │ └──────────────────────┘ │
    │           │               │
    │           │ 计算正常       │
    │           ▼               │
    │ ┌── 再检测通信 ─────────┐ │
    │ │ BANDWIDTH/ERR_PKT/    │ │
    │ │ PFC_PKT/RETRY/OOO     │ │
    │ │ → 异常? → 通信类定界   │ │
    │ └──────────────────────┘ │
    │                          │
    │ 4. 跨卡关联分析          │
    └──────────┬───────────────┘
               │
               ▼
    ┌─────────────────────┐
    │ 发现确认异常?         │
    └──────────┬──────────┘
               │
     ┌─────────┴─────────┐
     │ 是                │ 否
     ▼                   ▼
┌──────────┐   ┌────────────────────┐
│ 输出 KPI  │   │ KPI 未发现明显异常   │
│ 检测结果  │   │ → Fallback 到       │
│ 定界报告  │   │   Profiling 精查    │
└──────────┘   └────────────────────┘
```

### 7.2 集成到 main.go

在现有 8 步管线的**前面**插入 KPI 检测：

```go
// main.go 改造示意
func main() {
    inputPath, kpiCSVPath, degradation := parseCLI()

    config.FilePath = inputPath
    config.CalThreshold = 1 + degradation
    config.CommThreshold = 1 + degradation*5

    // ────── 第一道防线：KPI 资源指标检测 ──────
    var kpiResult *nupresource.DetectionResult
    if kpiCSVPath != "" {
        kpiResult = runKpiDetection(kpiCSVPath, degradation)
        // runKpiDetection 内部按顺序执行：
        //   1. ParseCSV → 2. AggregateByMinute → 3. SplitWindows
        //   4. BuildBaselines
        //   5. 先检测计算类指标 (FREQ/UTIL/HBM/TEMP/POWER)
        //   6. 计算正常 → 再检测通信类指标 (BANDWIDTH/ERR/PFC/RETRY/OOO)
        //   7. 二维交叉验证 → 8. 根因定界(计算类优先) → 9. 关联分析
    }

    if kpiResult != nil && kpiResult.HasConfirmedAnomaly() {
        nupresource.WriteResourceReport(kpiResult, inputPath)
        nupresource.ExportResourceJSON(kpiResult, inputPath)
        if !config.AlwaysRunProfiling {
            os.Exit(0)
        }
    }

    // ────── 第二道防线：Profiler 慢节点检测 ──────
    profilingdataparse.DataParsing(inputPath)
    parallels, validRanks := nodelevel.GetCurDetectionInfo(inputPath)
    stepData := nodelevel.GetCurJobLastStepData(validRanks)
    result := nodelevel.DelimitDetection(stepData, parallels, validRanks)
    utils.Write_result(result, parallels)
    report.WriteReport(stepData, parallels, validRanks, inputPath, result, inputPath, degradation)

    // KPI + Profiling 联合输出
    if kpiResult != nil {
        nupresource.MergeAndWriteCombinedReport(kpiResult, result, inputPath)
    }
}
```

### 7.3 KPI 与 Profiling 的能力互补

| 故障类型 | KPI 能发现？ | Profiling 能发现？ |
|---------|------------|------------------|
| 热降频（TEMP↑ + FREQ↓） | ✓ 直接 | ✗ |
| 网络链路错误（ERR_PKT↑） | ✓ 直接 | ✗ |
| 网络拥塞（PFC_PKT↑） | ✓ 直接 | ✗ |
| 散热不足（POWER↑ + TEMP↑） | ✓ 直接 | ✗ |
| Straggler（UTIL↓ + POWER↓） | ✓ 间接发现 | ✓ 精确发现 |
| 单卡 Kernel 计算慢 | ✗（UTIL 可能仍高） | ✓ 精确发现 |
| 集体通信延迟 | ✗ 间接 | ✓ 精确发现 |
| CPU Host 处理慢 | ✗ | ✓ |
| Bubble 时间异常 | ✗ | ✓ |

**总结**：KPI 擅长硬件/物理层异常，Profiling 擅长软件/性能层异常。两者互补。

---

## 8. 模块设计

### 8.1 包结构

```
features/straggler/
  ├── nupresource/               # 新增：NPU 资源 KPI 异常检测
  │   ├── types.go               # 数据结构 + 常量定义
  │   ├── parser.go              # CSV 解析 → 原始行
  │   ├── aggregator.go          # 1分钟截尾均值聚合（排序→截尾→均值）
  │   ├── baseline.go            # 历史基线构建 + 更新
  │   ├── space_detector.go      # 空间维度检测（Z-Score/IQR/聚类）
  │   ├── time_detector.go       # 时间维度检测（自身基线对比 + 趋势）
  │   ├── fusion.go              # 二维交叉判定 + 融合评分
  │   ├── rootcause.go           # 根因定界推理 + 关联分析
  │   └── report.go              # 结果输出（JSON + 文本报告）
  └── config/
      └── config.go              # 扩展：KPI 检测配置项
```

### 8.2 核心数据结构

```go
// ==================== types.go ====================

// CSVRow 一行原始 CSV 数据。
type CSVRow struct {
    Timestamp      int64
    Power          map[int]float64
    Temp           map[int]float64
    AICoreFreq     map[int]float64
    AICoreUtil     map[int]float64
    HBMUtil        map[int]float64
    TXBandwidth    map[int]float64
    RXPfcPkt       map[int]float64
    RocETxErrPkt   map[int]float64
    RocEOutOfOrder map[int]float64
    RocENewPktRty  map[int]float64
    NICRxAllPkg    map[int]float64
    CPUAvg         map[string]string
}

// TimeSeriesData 解析后的完整时间序列。
type TimeSeriesData struct {
    Rows           []CSVRow
    CardIDs        []int
    TimeRange      [2]int64
    BaselineRows   []CSVRow // 历史基线窗口
    DetectionRows  []CSVRow // 检测窗口
}

// MetricName 指标枚举。
type MetricName string
const (
    MetricTemp           MetricName = "temp"
    MetricPower          MetricName = "power"
    MetricAICoreFreq     MetricName = "aicore_freq"
    MetricAICoreUtil     MetricName = "aicore_util"
    MetricHBMUtil        MetricName = "hbm_util"
    MetricTXBandwidth    MetricName = "tx_bandwidth"
    MetricRXPfcPkt       MetricName = "rx_pfc_pkt"
    MetricRocETxErrPkt   MetricName = "roce_tx_err_pkt"
    MetricRocEOutOfOrder MetricName = "roce_out_of_order"
    MetricRocENewPktRty  MetricName = "roce_new_pkt_rty"
)

// AnomalyDirection 异常方向。
type AnomalyDirection int
const ( DirHigh AnomalyDirection = iota; DirLow )

// DetectionMethod 检测方法。
type DetectionMethod string
const ( MethodZScore DetectionMethod = "zscore"; MethodIQR = "iqr"; MethodDirect = "direct"; MethodAbsolute = "absolute" )

// ==================== 基线 ====================

// CardBaseline 单张卡单个指标的历史基线。
type CardBaseline struct {
    CardID int
    Metric MetricName
    Mean   float64
    StdDev float64
    P50    float64  // 中位数
    P95    float64  // 95 分位
    P99    float64  // 99 分位
    N      int      // 样本数
}

// ==================== 检测结果 ====================

// MetricAnomalyDetail 单个指标的异常详情。
type MetricAnomalyDetail struct {
    Metric       MetricName
    SpaceScore   float64 // 空间异常分 (Z-Score)
    TimeScore    float64 // 时间异常分 (Z-Score)
    FusionScore  float64 // 融合异常分
    SpaceAbnormal bool   // 空间维是否异常
    TimeAbnormal  bool   // 时间维是否异常
    Quadrant     Quadrant // 所属象限
}

// Quadrant 二维象限。
type Quadrant int
const (
    QuadNormal          Quadrant = iota // 正常
    QuadEarlyDegradation                // 早期劣化（仅时间异常）
    QuadIndividualVariance              // 个体差异（仅空间异常）
    QuadConfirmedAnomaly                // 确认异常（双维异常）
)

// CardDetectionSummary 单卡检测汇总。
type CardDetectionSummary struct {
    CardID          int
    AnomalyCategory AnomalyCategory // "compute" | "communication" | "none"
    Quadrant        Quadrant
    AnomalyDetails  []MetricAnomalyDetail
    TrendFindings   []TrendFinding
    CompositeScore  float64
    Severity        Severity
}

// AnomalyCategory 异常大类。
type AnomalyCategory string
const (
    CatNone          AnomalyCategory = "none"
    CatCompute       AnomalyCategory = "compute"
    CatCommunication AnomalyCategory = "communication"
)

// TrendFinding 趋势发现。
type TrendFinding struct {
    Metric   MetricName
    Slope    float64
    RSquared float64
    Desc     string
}

// ==================== 定界 ====================

type RootCauseCategory string
const (
    RcThermalThrottle      RootCauseCategory = "thermal_throttle"
    RcCoolingInsufficient  RootCauseCategory = "cooling_insufficient"
    RcTempSensorFault      RootCauseCategory = "temp_sensor_fault"
    RcForcedDownclock      RootCauseCategory = "forced_downclock"
    RcStraggler            RootCauseCategory = "straggler"
    RcLoadImbalance        RootCauseCategory = "load_imbalance"
    RcMemBottleneck        RootCauseCategory = "memory_bottleneck"
    RcNetworkLinkIssue     RootCauseCategory = "network_link_issue"
    RcNetworkCongestion    RootCauseCategory = "network_congestion"
    RcNetworkPacketLoss    RootCauseCategory = "network_packet_loss"
    RcBandwidthLimited     RootCauseCategory = "bandwidth_limited"
    RcHardwareFault        RootCauseCategory = "hardware_fault"
    RcUnknown              RootCauseCategory = "unknown"
)

type RootCauseResult struct {
    CardID     int
    Category   RootCauseCategory
    Confidence Confidence
    Evidence   []MetricAnomalyDetail
    Suggestion string
}

// CorrelationResult 跨卡关联分析结果。
type CorrelationResult struct {
    Type        string   // "node_level" | "network_level" | "job_level" | "card_level"
    Description string
    CardIDs     []int
    Confidence  Confidence
}

type Severity   string // "critical" | "warning" | "info"
type Confidence string // "high" | "medium" | "low"

// ==================== 配置 ====================

type DetectionConfig struct {
    // 预处理
    AggregationWindowSec int     // 聚合窗口（秒），默认 60（1分钟）
    TrimRatio            float64 // 截尾比例，默认 0.25（去前后各25%）
    MinSamplesForTrim    int     // 截尾最少样本数，默认 4

    // 窗口划分
    BaselineHours  int  // 历史基线窗口长度（小时），默认 360（15天）
    DetectionHours int  // 检测窗口长度（小时），默认 1

    // 空间维度
    SpaceMethod      DetectionMethod // 默认 zscore
    SpaceZThreshold  float64         // 默认 2.5
    SpaceIQRMult     float64         // 默认 1.5

    // 时间维度
    TimeZThreshold   float64 // 默认 2.0

    // 融合
    TimeWeight       float64 // α (时间维权重), 默认 0.6
    SpaceWeight      float64 // β (空间维权重), 默认 0.4

    // 趋势检测
    EnableTrend      bool
    TrendMinRSquared float64 // 默认 0.6

    // 特殊阈值
    FreqDownclockGap float64 // 频率降频判定差值(MHz), 默认 200
    NetErrMinThresh  float64 // 网络错误最小阈值, 默认 0

    // 与 Profiling 联动
    FallbackToProfiling bool // KPI 未发现异常时是否自动跑 Profiling，默认 true
    AlwaysRunProfiling  bool // 是否始终跑 Profiling（交叉验证），默认 false
}
```

### 8.3 接口设计

```go
// ==================== parser.go ====================
// ParseCSV 解析 KPI CSV 文件。
func ParseCSV(filePath string) (*TimeSeriesData, error)

// SplitWindows 将数据划分为基线窗口和检测窗口。
func SplitWindows(data *TimeSeriesData, cfg DetectionConfig) error


// ==================== aggregator.go ====================
// AggregateByMinute 对原始行按1分钟窗口做截尾均值聚合。
// 连续型指标（TEMP/POWER/FREQ/UTIL/BANDWIDTH/NIC_RX）→ 排序→截尾25%→中间50%均值
// 计数型指标（ERR_PKT/RETRY/OUT_OF_ORDER/PFC_PKT）→ 取1分钟增量（处理counter wrap）
// 输出：每分钟1行聚合数据。
func AggregateByMinute(rawRows []CSVRow) ([]CSVRow, error)

// Midmean 计算截尾均值：排序→去前后各25%→中间50%取平均。
func Midmean(values []float64) float64


// ==================== baseline.go ====================
// BuildBaselines 为所有卡的所有指标建立历史基线。
func BuildBaselines(data *TimeSeriesData, cfg DetectionConfig) map[int]map[MetricName]*CardBaseline


// ==================== space_detector.go ====================
// DetectSpaceAnomalies 对检测窗口执行空间维度检测。
// 返回每个时间点每张卡每个指标的 Z-Score。
func DetectSpaceAnomalies(data *TimeSeriesData, cfg DetectionConfig) *SpaceDetectionResult


// ==================== time_detector.go ====================
// DetectTimeAnomalies 对检测窗口执行时间维度检测。
// 返回每张卡每个指标的时间 Z-Score。
func DetectTimeAnomalies(data *TimeSeriesData, baselines map[int]map[MetricName]*CardBaseline, cfg DetectionConfig) *TimeDetectionResult

// DetectTrends 对每张卡的指标执行线性趋势检测。
func DetectTrends(data *TimeSeriesData, cfg DetectionConfig) map[int][]TrendFinding


// ==================== fusion.go ====================
// CrossValidate 执行二维交叉验证，返回每张卡的象限归属。
func CrossValidate(space *SpaceDetectionResult, time *TimeDetectionResult, cfg DetectionConfig) []CardDetectionSummary

// HasConfirmedAnomaly 是否有确认异常（双维异常）的卡。
func HasConfirmedAnomaly(summaries []CardDetectionSummary) bool


// ==================== rootcause.go ====================
// BoundRootCause 对异常卡执行根因定界。
func BoundRootCause(summaries []CardDetectionSummary, data *TimeSeriesData) []RootCauseResult

// CrossCardCorrelation 跨卡关联分析。
func CrossCardCorrelation(results []RootCauseResult, data *TimeSeriesData) []CorrelationResult


// ==================== report.go ====================
// DetectionResult 是 KPI 检测的完整结果。
type DetectionResult struct {
    Summaries   []CardDetectionSummary
    RootCauses  []RootCauseResult
    Correlations []CorrelationResult
    Config      DetectionConfig
}

// WriteResourceReport 生成文本报告。
func WriteResourceReport(result *DetectionResult, outputDir string) string

// ExportResourceJSON 导出 JSON。
func ExportResourceJSON(result *DetectionResult, outputPath string) error
```

---

## 9. CLI 设计

```
# 仅 KPI 检测
slowNodeDetection --kpi-csv=/path/to/kpi.csv [options]

# KPI + Profiling 联合（KPI 优先，无异常则 fallback Profiling）
slowNodeDetection path=/data/dir --kpi-csv=/path/to/kpi.csv [options]

# 仅 Profiling（已有，不变）
slowNodeDetection path=/data/dir [degradation=0.3]

KPI 检测专用选项:
  --kpi-csv=<path>              KPI CSV 文件路径
  --baseline-hours=<int>        历史基线窗口（小时），默认 360 (15天)
  --detection-hours=<int>       检测窗口（小时），默认 1
  --space-method=<zscore|iqr>   空间检测方法，默认 zscore
  --space-z-threshold=<float>   空间 Z-Score 阈值，默认 2.5
  --time-z-threshold=<float>    时间 Z-Score 阈值，默认 2.0
  --time-weight=<float>         时间维权重，默认 0.6
  --no-trend                    禁用趋势检测
  --no-fallback                 KPI 未发现异常时不 fallback 到 Profiling
  --always-profiling            始终运行 Profiling（交叉验证模式）
```

---

## 10. 输出格式

### 10.1 JSON (`npu_resource_detection_result.json`)

```json
{
  "summary": {
    "total_cards": 8,
    "confirmed_anomalies": 1,
    "early_degradation": 0,
    "individual_variance": 0,
    "normal": 7,
    "kpi_csv": "/data/kpi.csv",
    "time_range": {"start": 1784547926, "end": 1785847926, "total_points": 21600},
    "baseline_window": "360h",
    "detection_window": "1h"
  },
  "results": [
    {
      "card_id": 3,
      "anomaly_category": "compute",
      "quadrant": "confirmed_anomaly",
      "composite_score": 0.85,
      "severity": "warning",
      "communication_anomalies_secondary": true,
      "root_cause": {
        "category": "thermal_throttle",
        "confidence": "high",
        "evidence": [
          {"metric": "temp", "space_score": 3.2, "time_score": 4.1, "quadrant": "confirmed_anomaly"},
          {"metric": "aicore_freq", "space_score": 5.0, "time_score": 6.0, "quadrant": "confirmed_anomaly"}
        ],
        "suggestion": "Card 3 温度(空间+3.2σ, 时间+4.1σ) + 降频(空间+5.0σ, 时间+6.0σ)。历史基线温度 46℃ → 当前 57℃，频率 800MHz → 400MHz。建议检查风扇/风道/散热器。"
      },
      "metric_details": {
        "temp": {
          "quadrant": "confirmed_anomaly",
          "space_zscore": 3.2,
          "time_zscore": 4.1,
          "baseline_mean": 46.2,
          "baseline_std": 2.1,
          "current_mean": 57.3,
          "peer_mean": 47.0
        },
        "aicore_freq": {
          "quadrant": "confirmed_anomaly",
          "space_zscore": 5.0,
          "time_zscore": 6.0,
          "baseline_mean": 800.0,
          "baseline_std": 0.0,
          "current_mean": 400.0,
          "peer_mean": 800.0
        }
      },
      "trend_findings": [
        {"metric": "temp", "slope": 0.002, "r_squared": 0.78, "desc": "温度持续上升趋势: +0.002℃/分钟 ≈ +2.9℃/天"}
      ]
    }
  ],
  "correlations": []
}
```

### 10.2 文本报告

类似现有 `detection_report.log` 风格，包含：
- 双维检测摘要（四个象限的卡数统计）
- 确认异常卡列表 + 定界结果
- 早期劣化卡列表（关注列表）
- 各指标的时间/空间分数柱状图
- 趋势分析（温度爬升等）

---

## 11. 关键设计决策

| # | 决策 | 理由 |
|---|------|------|
| 1 | **1分钟截尾均值预处理** | 单采样点噪声大。1分钟内排序→去前后各25%→中间50%取均值，比全量均值稳健（抗尖峰），比中位数有代表性（保留分布信息） |
| 2 | **先计算后通信的检测顺序** | 计算慢必然导致通信慢（无法按时参与集合通信），反之不成立。先检计算可避免将继发性通信异常误判为网络问题。通信类定界规则仅在计算正常时生效 |
| 3 | **双维检测（时间+空间）** | 单看空间会把"一直偏热但稳定"的卡误报；单看时间会把"全集群一同升温"误报。双维交叉消除这两类误报 |
| 4 | **KPI 优先 + Profiling 降级** | KPI 无侵入开销、覆盖硬件层异常，适合常态化；Profiling 开销大、覆盖软件层异常，按需触发 |
| 5 | **空间维 Z-Score 默认 / 聚类可选** | Z-Score O(n) vs 聚类 O(n²)。聚合后仍有大量数据点（分钟级 × 15天），计算量差异显著 |
| 6 | **时间权重(0.6) > 空间权重(0.4)** | 单卡偏离自身历史基线比偏离同伴更有信息量。同伴可能集体变化，但自身趋势是确定性的 |
| 7 | **网络错误用绝对阈值** | ERR_PKT/RETRY 正常值为 0，统计方法失效。>0 即异常 |
| 8 | **计数型指标累加而非截尾** | ERR_PKT/RETRY/PFC_PKT 是累积计数器，应取增量总和。截尾会抹掉真正的错误尖峰 |
| 9 | **基线定期滚动 + 异常排除** | 异常点的值不纳入基线，避免污染正常分布 |
| 10 | **象限分类而非单一分数** | 四个象限对应不同运维动作（告警/关注/提示/忽略），比单一分数更有可操作性 |

---

## 12. 边界情况

| 场景 | 处理 |
|------|------|
| CSV 空文件 | 报错退出，不 fallback Profiling |
| 1分钟内某卡某指标采样数 < 4 | 截尾25%后不足2个点，降级为全量均值 |
| 1分钟边界时间戳不齐 | 按 `timestamp / 60 * 60` 向下取整分桶 |
| 计数型指标出现 counter wrap | `增量 < 0` 时 += 2^64 修正，若仍 < 0 标记数据异常跳过该分钟 |
| 数据不足 2 小时 | 全部作为检测窗口，跳过时间维度检测（无法建立基线） |
| 所有卡某指标完全一致(std=0) | 空间维跳过该指标 |
| 某卡某指标历史无波动(std=0) | 时间维跳过该指标（如频率固定在 800MHz）— 但若当前值不同，直接判定时间异常 |
| 某卡在某些时间点数据缺失 | 该时间点跳过该卡，取其余点的均值 |
| 网络错误类全为 0 | 跳过该指标检测（无限信息量） |
| 总卡数 < 3 | 空间维降级为仅用时间维（peer comparison 不可靠） |
| 全部卡同时异常 | 空间维不会标记（同伴一致），但时间维可能标记。触发任务级关联告警 |
| CPU 字段引用未知 NPU | CPU 不参与卡级检测，仅用于关联分析的物理节点推断 |
| 检测窗口内出现瞬态尖峰后又恢复 | 窗口均值会稀释尖峰影响，趋势检测可能捕获。若尖峰持续时间 < 检测窗口的 10%，忽略 |
| 计算异常 + 通信异常同时出现 | 以计算类定界为准，通信异常标记为"可能继发"，不独立告警 |
| 仅通信异常（计算正常）→ 但 Profiling 发现计算慢 | KPI 指标粒度粗，可能漏检软件层面计算慢。此时 Profiling 作为补充生效 |

---

## 13. 后续扩展方向

1. **在线流式检测**：不等待完整 CSV，逐行消费 + 实时更新滑动窗口
2. **自适应基线**：用指数加权移动平均（EWMA）持续更新基线，无需定期重建
3. **与告警系统集成**：Prometheus AlertManager / 企业微信 / 邮件通知
4. **KPI + Profiling 联合报告**：将两次检测结果合并为统一的诊断报告
5. **多 Job 联合分析**：同集群多个训练任务的 KPI 数据联合分析，发现集群级基础设施问题
