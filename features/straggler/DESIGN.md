# 慢节点检测算法 — 设计文档

## 模块接口

### config
```go
var FilePath string                                  // CLI path= 设置
var CalThreshold float64                             // = 1 + degradation（默认 1.3）
var CommThreshold float64                            // = 1 + degradation × 5（默认 2.5）

type DegradationData map[string]map[string]float64   // 类别 → (key → 劣化分数)
func NewDegradationData() DegradationData
func (d DegradationData) AddSingle(category string, rank int, degradation float64)
func (d DegradationData) AddGroup(category string, ranks []int, degradation float64)
```
- **Key 编码**：单卡 `strconv.Itoa(rank)`（如 `"0"`），组 `sort + strings.Join(ranks, ",")`（如 `"0,2,4"`）
- **AddGroup 去重**：已存在 key 时保留**最大**劣化值

### profilingdataparse
```go
func DataParsing(folderPath string)                          // 入口：遍历 .db → StartProcess
func StartProcess(dbFiles []string, outDir string) error     // 信号量（cap=4）+ WaitGroup
func ProcessDatabase(dbPath string, outDir string) error     // 单文件完整管线
func GetAllStepTimes(db *sql.DB) ([]StepTime, error)         // 3 级降级
func GetStepTimesFromSTEP_TIME(db *sql.DB) ([]StepTime, error)
func GetStepTimesFromTASK(db *sql.DB) ([]StepTime, error)
func TimeDiffForStep(db, xpToGroupName, stepTime) (PerformanceMetrics, error)
func GetAvgKernelTaskDuration(db *sql.DB, stepTime StepTime) (int, error)
func WriteResultsToCSV(outputFile string, pMS []PerformanceMetrics) error
func CalculateMean(values []int) (int, error)
func CalculateMidMeanPair(stats []OpStat) (meanDuration, meanCount int, err error)
```

**ProcessDatabase 执行顺序**：
1. `sql.Open("sqlite", path+"?mode=ro")` + WAL 模式
2. 创建 3 个索引（IF NOT EXISTS，幂等）
3. `extractGlobalRankFromFilename` → rank 字符串
4. `readGroupInfo` → META_DATA → group_info JSON（sync.Once 写入）+ xpToGroupName 映射
5. `GetAllStepTimes` → 合并为单 step（minStart → maxEnd）
6. `TimeDiffForStep` → 计算所有指标
7. `WriteResultsToCSV` → 单行 CSV

**Step 时间降级链**：
1. `STEP_TIME` 表 → `SELECT id, startNs, endNs ORDER BY id DESC` → 反转升序
2. `TASK` + `STRING_IDS` + `MSTX_EVENTS` → 正则匹配 `step \d+` → 查 connectionId → 查 TASK 时间
3. 哨兵：`{ID: -1, StartNs: math.MinInt, EndNs: math.MaxInt}`

**指标计算（TimeDiffForStep）**：
| 指标 | 计算方式 |
|------|---------|
| ZP_Host | 所有通信算子和 KERNEL_AICORE 的 `HEndNs - HStartNs` 均值（HStartNs > 0 && HEndNs ≥ HStartNs） |
| ZP_Bubble | 所有 `OpStartNs - HostEndNs > 0` 的正值均值 |
| ZP_Duration | 收集所有通信区间 → `mergeIntervalsSimple` 合并重叠 → 总跨度 |
| ZP_Device | `stepDuration - ZP_Duration`（钳位到 0） |
| ZP_Kernel | `SELECT AVG(endNs - startNs) FROM TASK ... WHERE KERNEL_AICORE` |
| 各域 Duration/Count | 域内算子 → `CalculateMidMeanPair`（去 min/max 后均值） |

**三种数据缺失场景**：
- `xpToGroupName` 为空 → 全部填充 -99999，ZP_Kernel/DataLoader 独立查询
- `groupNameIds` 为空 → 通信指标填充 -99999
- `deviceOps` 为空 → 除 ZP_Host 外的指标填充 -99999，ZP_Host 回退用 KERNEL_AICORE Host 耗时

**区间合并（mergeIntervalsSimple）**：按 Start 排序 → 遍历合并重叠区间 → 累加非重叠部分总长。

**并发控制**：
```go
var csvMutex sync.Mutex                    // CSV 写入全局锁
var fileWriteOnce map[string]*sync.Once    // group_info JSON 去重
var fileWriteOnceMu sync.Mutex             // 保护 fileWriteOnce map
```
- DB 并发：`make(chan struct{}, 4)` 信号量
- CSV：全局 Mutex（每 goroutine 写不同文件，但保留锁保安全）
- group_info JSON：`sync.Once` 每文件名（所有卡拓扑相同，只需写一次）

### nodelevel
```go
func GetCurDetectionInfo(jobPath string) (parallels map[string][][]int, validRanks []int)
func GetCurJobLastStepData(ranks []int) map[string]map[int]float64
func DelimitDetection(StepData map[string]map[int]float64, parallels map[string][][]int, validRanks []int) config.DegradationData
func GetCalDetectionGroup(parallels map[string][][]int, curNpus []int) (string, [][]int)
```

**GetCurDetectionInfo**：遍历 `op_metric/group_info_*.json`，收集所有 rank ID 和域名称，对每个域调用 `getDetectionJobParallelInfo` 提取组，过滤 < 2 卡的组，返回 parallels 映射和排序 validRanks。

**GetCurJobLastStepData**：对每个 rank 读 CSV → `map[列名][]float64` → 取倒数第二行（n > 1 时 n-2）→ 跳过 -99999 → 返回 `map[指标名]map[rank]值`。

**主检测组优先级**：`tp → exp → ep → tp_exp → cp → cp2 → cp_ulysses → cp_ring → dp → dp_cp → dp_modulo_exp_cp`

**并行域去重**：`checkRankParallelExist` 通过 `parallelInfo map[int]map[int]bool` 追踪每个 rank 已归属的组，避免同域组重复。

### 检测逻辑

#### 慢计算（getSlowCalculateRanks → detCalForOneGroup）
```
对主检测组每个子组：
  1. 检查 ZP_Kernel 可用性（组内所有卡 > 0）
     ✓ → 指标 = ZP_Kernel，方向 = "max"
     ✗ → 指标 = ZP_Duration，方向 = "min"
  2. 收集非零值，要求 >= minRanksInGroup(2)
  3. 均质化聚类 → AddSingle("cal", rank, degradation)
```

#### 慢通信（detectionAllCommunicationParallel → HomogenizationForSlowCommunication）
```
对每个非 PP/非 embd 域：
  ppStageNum = len(parallels["pp"][0])（若无 PP 则为 1）
  1. 每个子组内部排序，组间字典序排序
  2. 每组取 {domain}_Duration 最小的卡为代表
  3. detectionCards 按 ppStageNum 均分桶
  4. 每桶内对代表卡做均质化聚类（方向 "max"）
  5. 异常代表卡通过 rank2Group 映射回完整组
  → AddGroup("comm", fullGroup, degradation)
```
PP=1 时所有代表卡在同一桶，算法天然降级为普通聚类。

#### 慢CPU（getSlowHostRanksByHomogenize）
```
1. 收集所有 validRanks 的 ZP_Host 值
2. processCPUData(values) — 原地修改：
   每 4 个一组：
     > 2 个 → 去掉 min/max，计算剩余均值
     ≤ 2 个 → 普通均值
   用均值覆盖组内所有值
3. 均质化聚类（方向 "max"）→ AddSingle("cpu", rank, degradation)
```

#### NPU Bubble（detectionZpBubbleData）
```go
for npuID, value := range ZP_Bubble:
    if value > 0 && value < 5000:
        AddSingle("npu_bubble", npuID, value)
```
注：使用硬编码 `< 5000`，非 config 中的 `zpBubbleAbnormalBoundary = 50000`。

### spacedetector
```go
type IndexAndValue struct { Index int; Value float64 }

func HomogenizationComparisonFunc(fileRanks []int, alignedData []float64,
    degradationPercent float64, abnormalType string) ([]int, []float64)
```

**recurseDimensionalClusteringWithDegradation**：
```
baseVal = abnormalType=="max" ? min(data) : max(data)     // baseVal==0 → SmallestNonzeroFloat64
input = dataList, result = nil
loop:
  tmpResult, nextList = oneDimensionalClustering(input, threshold, type)
  if tmpResult empty → break
  input = nextList
  映射局部索引 → 原始 dataList 索引（通过 result 中间层）
degradation = abnormalType=="max" ? data[i]/baseVal : baseVal/data[i]
```

**oneDimensionalClustering**：
```
1. sortDataByIndexAndValue(data) → 升序 IndexAndValue 列表
2. calculateDifferences → diff[i] = sorted[i+1].Value - sorted[i].Value + totalSum
3. maxDiffIdx = argmax(diff)
4. 条件1: diff[maxDiffIdx] >= totalSum / 2.0
5. 分割：littleGroup = sorted[:maxDiffIdx+1], bigGroup = sorted[maxDiffIdx+1:]
6. 条件2: bigMean/littleMean >= threshold（littleMean != 0）
7. abnormalType=="max" → 返回 bigGroup 的 Index/Value
   abnormalType=="min" → 返回 littleGroup 的 Index/Value
```

时间复杂度 O(n²) 最坏，空间复杂度 O(n)。

### utils
```go
func Write_result(finalResult map[string]map[string]float64, parallels map[string][][]int)
func CheckFileOrDirectoryReadMode(path string) bool
func CheckFileOrDirectoryIsSoftLink(path string) bool
func TransferFloatArrayToInt(ids []interface{}) []int
func ReadFile(filePath string) ([]byte, error)
```

**Write_result 逻辑**：
1. 对每个类别构建 `[]DetectionEntry`
2. 排序：bubble 升序，其余降序
3. comm 的 display_key：将 groupKey（如 `"0,1,2,3"`）通过 parallels 匹配找到域名称 → `"tp[0, 1, 2, 3]"`
4. 写入 `config.FilePath/straggler_detection_result.json` + stdout 打印

### report
```go
func WriteReport(stepData, parallels, validRanks, outputDir, detectionResult, inputPath, degradation) string
func GenerateReport(stepData, parallels, validRanks, detectionResult, inputPath, degradation) string
```

**报告章节**：头部 → 并行域拓扑 → 检测摘要表 → ZP_Kernel 柱状图（Top 30 + Bottom 5）→ ZP_Host 柱状图 → 总通信时间 → 各域分组对比（min/mean/max + 柱状图）。

**常量**：柱状图 `█`，最大宽度 40，Top N = 30，Bottom N = 5。

**时间格式化**：`≥1e9` → s，`≥1e6` → ms，`≥1e3` → µs，其余 → ns。

## 数据结构

```go
type StepTime struct {
    ID      int   // Step 编号（合并后 = 0）
    StartNs int   // 开始时间（ns）
    EndNs   int   // 结束时间（ns）
}

type CommunicationOp struct {
    OpStreamIndex int   // COMMUNICATION_OP._rowid_
    OpName        int   // 算子名称（STRING_IDS ID）
    StartNs       int   // 设备侧开始时间
    EndNs         int   // 设备侧结束时间
    HStartNs      int   // Host 侧开始时间（由 CANN_API/MSTX_EVENTS 关联填充）
    HEndNs        int   // Host 侧结束时间
    Count         int
    ConnectionID  int   // 设备-主机关联键
    DomainID      int   // 并行域 ID（STRING_IDS ID）
}

type PerformanceMetrics struct {
    StepIndex    int            // 合并后 = 0
    StepDuration int            // maxEndNs - minStartNs
    ZPDevice     int            // 非通信时间 = stepDuration - ZP_Duration（钳位到 0）
    ZPDuration   int            // 总通信时间（合并区间后）
    ZPHost       int            // 平均 Host 耗时
    ZPBubble     int            // 平均 Bubble 时间
    ZPCount      int            // 未使用
    ZPKernel     int            // 平均 KERNEL_AICORE 耗时
    DataLoader   int            // DataLoader 耗时
    Durations    map[string]int // 各域通信耗时（按 xp 名称）
    Counts       map[string]int // 各域通信计数
}

type OpStat struct { Duration, Count int }
type Interval struct { Start, End int }
type HostOp struct  { StartNs, EndNs int }
```

## 关键 SQL

```sql
-- 并行域配置
SELECT value FROM META_DATA WHERE name = 'parallel_group_info'

-- 通信算子（step 时间窗口内 + 指定 groupName ID）
SELECT opName, startNs, endNs, connectionId, count, _rowid_, groupName
FROM COMMUNICATION_OP
WHERE groupName IN (?, ...) AND startNs >= ? AND endNs <= ?
ORDER BY startNs ASC

-- Host 时序（批量查 connectionId）
SELECT startNs, endNs, connectionId
FROM {CANN_API|MSTX_EVENTS}
WHERE connectionId IN (?, ...)

-- KERNEL_AICORE connectionId
SELECT t.connectionId FROM TASK t
INNER JOIN STRING_IDS s ON t.taskType = s.id
WHERE s.value IN ('KERNEL_AICORE') AND t.startNs >= ? AND t.endNs <= ? AND t.connectionId > 0

-- 平均 Kernel 耗时
SELECT AVG(t.endNs - t.startNs) FROM TASK t
INNER JOIN STRING_IDS s ON t.taskType = s.id
WHERE s.value IN ('KERNEL_AICORE') AND t.startNs >= ? AND t.endNs <= ?

-- DataLoader
SELECT startNs, endNs FROM MSTX_EVENTS
WHERE message = ? AND startNs >= ? AND endNs <= ? LIMIT 1
```

## 错误处理

**致命（程序终止）**：
- 无 CLI 参数 / 缺 path / path 非目录
- 目录下无 `.db` 文件
- 获取并行域/有效 rank 失败
- 无有效 step data
- 检测返回空

**非致命（跳过/降级，继续执行）**：
| 场景 | 处理 |
|------|------|
| 单个 .db 处理失败 | 日志 + 继续下一个 |
| CSV 为空 | 跳过该 rank |
| group_info JSON 无效 | 跳过该 rank 的拓扑 |
| CANN_API/MSTX_EVENTS 表不存在 | 跳过 Host 时间查询 |
| DataLoader 查询失败 | DataLoader = 0 |
| Kernel 查询无数据 | ZP_Kernel = 0 |
| 通信耗时 > step 总耗时 | ZP_Device 钳位到 0 + 警告 |

## 日志前缀

`[SLOWNODE ALGO]` 算法通用 | `[DATA PROCESS]` 数据解析 | `[WARN]` 警告 | `[REPORT]` 报告生成
