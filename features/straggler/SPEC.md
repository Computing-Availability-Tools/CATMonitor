# 慢节点检测算法 — 技术规范

基于 Ascend PyTorch Profiler Level0 数据（每设备一个 `.db` SQLite 文件），从四个维度检测 AI 训练集群中性能劣化的 NPU 卡。

## 数据流

```
ascend_pytorch_profiler_{N}.db （每个设备一个）
  │
  ▼
[profilingdataparse] SQLite 解析
  ├── 读取 META_DATA → parallel_group_info（JSON）→ op_metric/group_info_{N}.json
  ├── 合并所有 step 时间范围为单个聚合 step
  ├── 查询通信算子、Host 时间、Kernel 时间等指标
  └── 输出 op_metric/global_rank_{N}.csv （单行数据）
  │
  ▼
[nodelevel] 检测引擎
  ├── GetCurDetectionInfo()    → 并行域拓扑 + 有效 rank 列表
  ├── GetCurJobLastStepData()  → 单次快照数据映射
  └── DelimitDetection()       → 执行 4 类检测
  │
  ▼
[utils]  Write_result()       → stdout + straggler_detection_result.json
[report] WriteReport()        → analysis_result/detection_report.log
```

## CLI

```
slowNodeDetection path=/data/dir [degradation=0.3]
```

| 参数 | 类型 | 必需 | 默认 | 说明 |
|------|------|------|------|------|
| `path` | string | 是 | — | 包含 `ascend_pytorch_profiler_*.db` 的数据目录 |
| `degradation` | float64 | 否 | 0.3 | 灵敏度，< 0 重置为 0.3，> 1 允许但警告 |

阈值计算：`CalThreshold = 1 + degradation`，`CommThreshold = 1 + degradation × 5`

## 输入输出

### 输入目录结构
```
<path>/
  ├── ascend_pytorch_profiler_0.db
  ├── ascend_pytorch_profiler_1.db
  └── ascend_pytorch_profiler_N.db
```

### 中间产物（op_metric/）
| 文件 | 格式 | 内容 |
|------|------|------|
| `global_rank_{N}.csv` | CSV，单行 | 设备 N 的性能指标 |
| `group_info_{N}.json` | JSON | 并行域拓扑（sync.Once 去重） |

### CSV 列说明
| 列 | 含义 |
|------|------|
| `StepIndex` | 合并后 step ID（始终为 0） |
| `StepDuration` | 聚合 step 总时长（ns） |
| `ZP_Device` | step 内非通信时间 = stepDuration - 合并后通信总跨度 |
| `ZP_Duration` | 总通信时间（合并重叠区间） |
| `ZP_Host` | 平均 Host 耗时（通信算子 + KERNEL_AICORE 的 Host 端耗时均值） |
| `ZP_Bubble` | 平均 Bubble 时间（OpStartNs - HostEndNs 的正值均值） |
| `ZP_Kernel` | 平均 KERNEL_AICORE 任务耗时 |
| `DataLoader` | MSTX_EVENTS 中 DataLoader 耗时 |
| `{domain}_Duration` | 该并行域内通信算子平均耗时 |
| `{domain}_Count` | 该并行域内通信算子平均计数 |

### 最终输出
| 文件 | 位置 | 格式 |
|------|------|------|
| `straggler_detection_result.json` | `<path>/` | JSON |
| `detection_report.log` | `<path>/analysis_result/` | 文本报告（含柱状图） |

## 检测类型

| 类别 | 标签 | 指标 | 方向 | 阈值 | 结果粒度 |
|------|------|------|------|------|---------|
| 慢计算 | `cal` | ZP_Kernel（优先）/ ZP_Duration（降级） | max / min | CalThreshold | 单卡 |
| 慢通信 | `comm` | `{domain}_Duration`（各域独立） | max | CommThreshold | 卡组 |
| 慢CPU | `cpu` | ZP_Host（4卡截尾均值预处理） | max | CalThreshold | 单卡 |
| NPU Bubble | `npu_bubble` | ZP_Bubble | < 5000ns | 固定 | 单卡 |

### 检测方法

**慢计算**：对主检测组内每组卡，优先使用 ZP_Kernel（方向 max，值大 = 计算慢）；若组内有卡缺少 ZP_Kernel 则降级为 ZP_Duration（方向 min，值小 = 计算慢导致通信时间短）。

**慢通信**：对每个非 PP/非 embd 并行域，每组取通信时间最小的卡为代表，按 PP stage 分桶后均质化聚类，异常代表映射回完整组。

**慢CPU**：按 4 卡一台机器的假设，每组计算截尾均值（去 min/max 后平均其余值），覆盖原始值后均质化聚类，消除机器内差异暴露机器间差异。

**NPU Bubble**：固定阈值 `< 5000 ns`（5µs），直接判定。

## 输出格式

### straggler_detection_result.json
```json
{
  "cal": [
    {"display_key": "0", "metric_value": 1.5, "is_abnormal": true}
  ],
  "comm": [
    {"display_key": "tp[0, 1, 2, 3]", "metric_value": 3.2, "is_abnormal": true}
  ],
  "cpu": [
    {"display_key": "2", "metric_value": 1.4, "is_abnormal": true}
  ],
  "npu_bubble": [
    {"display_key": "3", "metric_value": 3200.0, "is_abnormal": true}
  ]
}
```

排序：`npu_bubble` 升序（越小越异常），其余降序（越大越异常）。
display_key：`comm` 为 `域名[排序后的 rank 列表]`，其余为 rank 字符串。

### detection_report.log

带柱状图（`█`，最大 40 字符宽度）的可读文本报告，包含：
- 数据目录、时间、有效 rank 数
- 并行域拓扑摘要
- 四类检测结果表格
- ZP_Kernel / ZP_Host 排序柱状图（Top 30 + Bottom 5）
- 各通信域分组对比（min/mean/max）
- 时间自动单位转换（s / ms / µs / ns）

## 包结构

| 包 | 文件数 | 职责 |
|------|--------|------|
| `main` | 1 | CLI 参数解析、7 步管线编排 |
| `config` | 1 | 全局配置（FilePath、阈值）、DegradationData 结果聚合 |
| `profilingdataparse` | 3 | SQLite `.db` 解析 → CSV + JSON 中间文件 |
| `nodelevel` | 4 | 并行域拓扑解析、单步快照、四类检测逻辑 |
| `spacedetector` | 1 | 均质化聚类算法（所有检测的统一异常检测器） |
| `utils` | 1 | 结果写入（stdout + JSON 文件） |
| `report` | 1 | 文本报告生成 |

## 并行域名称

`tp`, `dp_cp`, `dp`, `cp`, `exp`（Expert Parallel，非 "ep"）, `tp_exp`, `pp`, `cp_ring`, `cp_ulysses`, `default_group`

主检测组优先级：`tp → exp → ep → tp_exp → cp → cp2 → cp_ulysses → cp_ring → dp → dp_cp → dp_modulo_exp_cp`

## SQLite 源表

| 表 | 关键列 | 用途 |
|------|---------|------|
| `META_DATA` | `name, value` | 存储 `parallel_group_info` JSON |
| `STRING_IDS` | `id, value` | 名称 ↔ ID 映射 |
| `STEP_TIME` | `id, startNs, endNs` | Step 时间戳（降级链第一级） |
| `COMMUNICATION_OP` | `opName, startNs, endNs, connectionId, count, groupName` | 设备级通信算子 |
| `CANN_API` | `startNs, endNs, connectionId` | Host API 调用时序 |
| `MSTX_EVENTS` | `startNs, endNs, connectionId, message` | Host 事件（DataLoader、Step 标记） |
| `TASK` | `startNs, endNs, taskType, connectionId` | 任务执行（KERNEL_AICORE） |

运行时创建索引：`idx_string_ids_value`, `idx_device_op_time`, `idx_task_time_type`

## 均质化聚类算法

唯一的异常检测算法，通过方向和阈值参数化适配所有检测场景。

**核心流程**：
1. 按值升序排序（保留原始索引）
2. 计算相邻差值，找最大间隙位置
3. 条件 1：`maxDiff ≥ sum(allDiff) / 2`（最大间隙至少占总跨度一半）
4. 条件 2：`bigMean / littleMean ≥ threshold`（两组均值比达阈值）
5. 按方向取异常组（"max"→大值组, "min"→小值组）
6. 对异常组递归执行，直到无法再分割

**示例**：数据 `[10, 10, 20, 10]`，阈值 1.3，方向 "max"
- 排序：`[(0,10), (1,10), (3,10), (2,20)]`，最大差值 10（位置 2）
- `10 ≥ 10/2 ✓`，`20/10 = 2.0 ≥ 1.3 ✓`
- 返回索引 2（异常），劣化 = 20/10 = 2.0

## 关键设计决策

- **合并 Step**：所有 step 合并为单聚合 step（minStart → maxEnd），CSV 仅一行。Profiler 时间分辨率低，逐 step 不可靠。
- **倒数第二行**：多行数据取 n-2 行，避免末行不完整。
- **-99999 哨兵**：统一无效数据标记，在 GetCurJobLastStepData、detectionZpBubbleData、report.filterValid 中跳过。
- **单一算法**：均质化聚类是唯一的异常检测器，所有场景通用。
- **不做时序分析**：仅处理单次快照，不进行趋势/移动平均/变点检测。

## 边界情况

| 场景 | 处理 |
|------|------|
| 无 .db 文件 | `log.Fatalf` 退出 |
| ZP_Kernel 数据不全 | 慢计算降级为 ZP_Duration + 方向 "min" |
| 通信算子缺失 | 除 ZP_Host 外所有指标填充 -99999；ZP_Host 回退用 KERNEL_AICORE Host 耗时 |
| 通信耗时 > step 总耗时 | ZP_Device 钳位到 0 |
| 组内有效卡 < 2 | 跳过该组检测 |
| PP = 1（无流水线并行） | ppStageNum=1，所有代表卡放同一桶聚类 |
| 跨节点拓扑 | getDetectionGroups 通过 nodeGlobalRank 集合过滤 |
| group_info 写入竞态 | sync.Once 保证每个文件名只写一次 |
| DataLoader 不存在 | DataLoader = 0 |
| Kernel 查询无数据 | ZP_Kernel = 0 |

## 构建

```bash
# Linux ARM64（目标平台）
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o slowNodeDetection .

# Linux AMD64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o slownode_linux_amd64 .

# Windows AMD64
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o slownode_win_amd64.exe .
```

全静态二进制，无外部依赖。
