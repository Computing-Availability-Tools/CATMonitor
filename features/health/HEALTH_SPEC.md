# CATMonitor 健康度模块规格说明书 (HEALTH_SPEC)

> 本文从零设计 CATMonitor 的**健康度评估模块**：在已具备采集架构、已定义指标清单、已确定等级划分的前提下，定义"如何从采集到的指标计算服务器健康度"。
> 指标清单以 [`docs/CATMonitor_indi_list.md`](../../docs/CATMonitor_indi_list.md) 为唯一事实来源；总规格见 [SPEC.md](../../SPEC.md)。

| 版本 | 日期 | 说明 |
|------|------|------|
| v0.1 | 2026-07-15 | 初版：基于采集架构 + High/Medium 指标 + 四级等级的从零设计 |

---

## 1. 概述

### 1.1 目标

为 CATMonitor 设计一个**纯评估库**：输入是采集器产出的指标流，输出是一个 0–100 的健康度评分与等级，供 CLI 与 Web 仪表盘展示。模块本身**不采集任何数据**——所有输入来自既有采集架构。

### 1.2 设计前提（三条锚点）

1. **当前采集架构**：6 个采集器（cpu/memory/disk/gpu/npu/network）经来源层（`internal/source/` 14 包）获取数据、产出 `collector.Metric`；外部工具/文件不可用时采集器优雅降级（对应指标不产出，不报错）。
2. **能采集的指标**：共 152 个（见 [`indi_list`](../../docs/CATMonitor_indi_list.md)），按部件 × 优先级分布如下。本模块**只纳入 CPU / 内存 / 硬盘 / GPU·NPU 四类部件**（网卡属链路吞吐、非部件故障健康信号，不参与健康度），**只纳入 High 与 Medium 优先级指标**，Low 级诊断指标一律不参与扣分。

   | 部件 | 指标总数 | High | Medium | Low | 是否参与健康度 |
   |------|:---:|:---:|:---:|:---:|---|
   | CPU | 40 | 4 | 12 | 24 | 是（High+Medium） |
   | Memory | 19 | 4 | 7 | 8 | 是（High+Medium） |
   | Disk | 7 | 1 | 4 | 2 | 是（High+Medium） |
   | GPU | 7 | 3 | 3 | 1 | 是（High+Medium） |
   | NPU | 74 | 9 | 43 | 22 | 是（High+Medium） |
   | Network | 5 | 1 | 3 | 1 | **否**（链路指标，非健康信号） |

3. **等级划分**（既定，本模块直接采用）：

   | 得分 | 等级 | 含义 |
   |------|------|------|
   | 90–100 | Excellent | 服务器运行良好 |
   | 75–89 | Good | 轻微问题，建议关注 |
   | 60–74 | Warning | 存在风险，需检查 |
   | 0–59 | Critical | 严重问题，需立即处理 |

### 1.3 非目标

- 不新增采集逻辑、不触碰采集器与来源层（底座零改动）。
- 不做配置驱动的阈值/权重（规则结构固定于代码，YAML 仅选权重方案）。
- 不纳入 Low 级指标扣分；不为网卡设计健康度。
- 不依赖 GPU/NPU 功率 TDP 参考值（采集器暂未产出，相关规则暂缓）。

---

## 2. 模块边界与依赖

### 2.1 位置

- 仓库根 `health/`，与 `web/`、`cmd/`、`internal/` 同层级，同一 `go.mod`。作为根级公开包，CAT 系列其他工具可直接 import。
- 包名 `package health`。

### 2.2 依赖关系

```
cmd/catmonitor ──┐
web/ ─────────────┼──>  health/  ──>  internal/collector (仅 collector.Metric 类型)
                  │
internal/config ──┘   (HealthConfig 留在 internal/config，不动)
```

- **唯一下游依赖**：`internal/collector` 的 `collector.Metric`。同 `go.mod` 下根级包可 import `internal/`，故无需解耦数据模型、无需引入中性 `Sample` 类型。
- **禁止**：import `internal/source/*`、`internal/config`、`cmd/*`、`web/*`；禁止 `os.ReadFile`/`exec.Command`/系统调用。一切数据经采集器产出后喂入。

### 2.3 上游消费方

- `cmd/catmonitor`：`health` 子命令（一次性评估并表格输出）、`daemon`（周期性评估记日志）。
- `web`：每个采集周期调用一次 `Evaluate`，结果写入 `snapshot.json` 的 health 字段。
- 调用契约：传入**一轮全量采集**的 `[]collector.Metric`，返回 `HealthScore`。

---

## 3. 数据模型（输出契约）

设计为可直接序列化为 `snapshot.json` 的 health 字段，字段名稳定、前端可直接消费。

```go
// HealthScore 一次评估的总结果。
type HealthScore struct {
    Score      int                       `json:"score"`        // 0-100 总分
    Grade      string                    `json:"grade"`        // Excellent|Good|Warning|Critical
    ServerType string                    `json:"server_type"` // cpu_only | accelerated
    Components map[string]ComponentScore `json:"components"`   // 按部件拆分
    Timestamp  time.Time                 `json:"timestamp"`
}

// ComponentScore 单个部件的得分与扣分明细。
type ComponentScore struct {
    Score      int         `json:"score"`      // 该部件实得
    Max        int         `json:"max"`        // 该部件满额（随权重方案）
    Deductions []Deduction `json:"deductions"` // 触发的扣分项
}

// Deduction 单条扣分。
type Deduction struct {
    Rule    string  `json:"rule"`    // 规则名（如 "usage>90%"）
    Penalty float64 `json:"penalty"` // 扣减分值
}
```

设计要点：
- 总分 = 各部件 `Score` 之和，各部件满额之和恰为 100（见 §5 权重方案），保证 0–100 区间。
- `Deductions` 透出每条触发规则与扣减量，供前端展示扣分项、便于排障。
- 缺指标的部件不出现于 `Components`（或出现但 `Score==Max`、无扣分），自然降级。

---

## 4. 评估架构

### 4.1 流程

1. **分组**：按 `Metric.Component` 将一轮指标分为 cpu/memory/disk/gpu/npu 五组。
2. **server-type 自动检测**：若存在 `gpu` 或 `npu` 指标 → `accelerated` + 加速权重方案；否则 `cpu_only` + CPU-only 方案。配置可显式指定覆盖自动检测。
3. **逐部件评估**：对每个存在的部件分组，调用 `evaluate<部件>(metrics, 满额)`，返回 `ComponentScore`（满额起步，逐条规则扣分，下限 0）。
4. **多卡聚合**：GPU/NPU 多卡场景对每条规则取**最差卡**的值触发（worst across cards），保证一张异常卡即体现扣分。
5. **汇总**：`Score` = Σ 部件 Score；`Grade` 由 §1.2 等级表映射。

### 4.2 文件结构

```
health/
├── health.go      # 公共类型 + Evaluator + Evaluate(编排) + 分组 + 等级映射
├── scheme.go       # WeightScheme + 预置方案 + GetScheme
├── cpu.go          # evaluateCPU
├── memory.go       # evaluateMemory
├── disk.go         # evaluateDisk
├── gpu.go          # evaluateGPU
├── npu.go          # evaluateNPU
├── util.go         # findMetric / worstAcross 等内部工具
└── *_test.go       # 每部件每规则单测
```

按"一函数一部件 + 分文件"组织（与采集器分文件一致），不引入 `Rule` 接口/注册表等额外抽象；如未来需可插拔规则，可在不破坏 §3 契约前提下叠加，本次不做。

### 4.3 优雅降级

来源不可用 → 采集器不产出对应指标 → 分组为空 → 跳过该部件或该规则，不报错、不扣分。规则函数对"指标缺失"一律视为不触发。典型：无 BMC 则 CPU `temperature` 缺失；无 smartctl 则硬盘 `smart_status` 缺失；无 CANN 则 NPU 全部缺失（采集器 no-op）。

---

## 5. 权重方案

各部件满额之和 = 100。按服务器是否带加速卡分两档：

| 方案（配置名） | CPU | Memory | Disk | GPU/NPU | 适用 |
|------|:---:|:---:|:---:|:---:|------|
| `cpu_only` | 30 | 40 | 30 | — | 无 GPU/NPU |
| `accelerated_8card` | 10 | 20 | 10 | 60 | 带加速卡（8 卡） |
| `accelerated_4card` | 10 | 20 | 10 | 60 | 带加速卡（4 卡，暂同 8 卡） |

设计依据：
- **CPU-only 场景**：内存是最大故障面（ECC/可用率/OOM）→ 满额最高 40；CPU 与硬盘各 30。
- **加速场景**：GPU/NPU 单卡故障代价最高 → 满额 60；CPU/内存/硬盘各降为 10/20/10。
- 4 卡与 8 卡暂同权重，后续可差异化。
- `auto`（默认）= 运行时按 §4.1 自动检测二选一。
- GPU 与 NPU **共用** `GPU` 满额档（加速卡只算一类，不重复计权）。

---

## 6. 扣分规则设计

### 6.1 设计原则

1. **只对"能采集到"的 High/Medium 指标设规则**，每条规则标注 `indi_list` 序号、单位、数据来源，确保有据可依。
2. **严重度分级扣分**：UCE/SMART 失败/health Alarm 等严重事件扣大分；CE/使用率/温度等渐近问题按比例扣分。
3. **满额百分比 vs 绝对分**：渐近型用 `-X% 满额`（随方案缩放）；错误计数型用 `-N 分/个`（绝对值，跨方案一致）。
4. **多卡取最差**；**缺指标不触发**。

> 下表"状态"列：●=本次设计纳入；○=暂缓（依赖未采集的参考值）。

### 6.2 CPU 规则（满额 = scheme.CPU）

| 指标（序号·优先级·单位） | 触发条件 | 扣分 | 数据来源与降级 | 状态 |
|---|---|---|---|---|
| usage（1.1·High·%） | >90% | -20% 满额 | /proc/stat（恒有） | ● |
| usage（1.1·High·%） | >80% | -10% 满额 | /proc/stat | ● |
| load_average（1.2·High·-，label interval=1m） | > core_num×2 | -10% 满额 | /proc/loadavg；阈值按 core_num 动态（无 core_num 时 fallback 8=4×2） | ● |
| temperature（1.3·Medium·°C） | >85°C（取最差） | -30% 满额 | ipmitool SDR；无 BMC 时缺失→不触发 | ● |
| temperature（1.3·Medium·°C） | >75°C（取最差） | -15% 满额 | 同上 | ● |
| cpu_ce_errors（1.32·High·次） | 每个错误 | -2 分 | mcelog/dmesg；无则缺失 | ● |
| cpu_uce_errors（1.33·High·次） | 每个错误 | -10 分 | mcelog/dmesg；UCE 为严重硬件错误 | ● |

### 6.3 内存规则（满额 = scheme.Memory）

| 指标（序号·优先级·单位） | 触发条件 | 扣分 | 数据来源与降级 | 状态 |
|---|---|---|---|---|
| usage（2.1·High·%） | >90% | -30% 满额 | /proc/meminfo | ● |
| usage（2.1·High·%） | >80% | -15% 满额 | /proc/meminfo | ● |
| swap_usage（2.2·High·%） | >50% | -10% 满额 | /proc/meminfo | ● |
| ecc_ce_errors（2.8·High·次） | 每个错误 | -2 分 | EDAC /sys/.../ce_count；无 EDAC→缺失 | ● |
| ecc_uce_errors（2.9·High·次） | 每个错误 | -10 分 | EDAC ue_count；UCE 严重 | ● |
| saturation（2.6·Medium·%，PSI avg10） | >80% | -15% 满额 | /proc/pressure/memory；需 CONFIG_PSI，否则缺失 | ● |
| fragmentation（2.7·Medium·%） | >80% | -10% 满额 | /proc/buddyinfo | ● |

> `oom_count`（2.10·Medium）默认采集=否，作为可选规则（>0 → -10% 满额），缺数据不触发；`swap_in/out` 为速率参考，不扣分。

### 6.4 硬盘规则（满额 = scheme.Disk）

| 指标（序号·优先级·单位） | 触发条件 | 扣分 | 数据来源与降级 | 状态 |
|---|---|---|---|---|
| space_usage（3.1·High·%） | 最差挂载点 >90% | -40% 满额 | statfs syscall（恒有） | ● |
| space_usage（3.1·High·%） | 最差挂载点 >80% | -20% 满额 | statfs syscall | ● |
| io_wait（3.4·Medium·%） | >20% | -10% 满额 | /proc/stat | ● |
| smart_status（3.5·Medium·-） | SMART 异常 | -30% 满额 | smartctl -H；默认采集否、无 smartctl→缺失 | ● |

> `io_errors`（3.7·Low）、`smart_temperature`（3.6·Low）为 Low，不纳入。`iops`/`throughput`（Medium）为吞吐参考，不扣分。

### 6.5 GPU 规则（满额 = scheme.GPU）

| 指标（序号·优先级·单位） | 触发条件 | 扣分 | 数据来源与降级 | 状态 |
|---|---|---|---|---|
| temperature（4.3·High·°C） | 最差卡 >90°C | -30% 满额 | nvidia-smi | ● |
| temperature（4.3·High·°C） | 最差卡 >80°C | -15% 满额 | nvidia-smi | ● |
| memory_usage（4.2·High·%） | 最差卡 >95% | -10% 满额 | nvidia-smi | ● |
| utilization（4.1·High·%） | 最差卡 >95% | -10% 满额 | nvidia-smi | ● |
| ecc_errors（4.6·Medium·次） | >0 | -20% 满额 | nvidia-smi（不可纠正 ECC） | ● |
| power_draw（4.4·Medium·W） | >110% TDP | -15% 满额 | nvidia-smi；**采集器无 TDP 参考值→暂缓** | ○ |

> `fan_speed`（Medium）为散热响应，非直接健康信号，不扣分。

### 6.6 NPU 规则（满额 = scheme.GPU，与 GPU 共用档）

| 指标（序号·优先级·单位） | 触发条件 | 扣分 | 数据来源与降级 | 状态 |
|---|---|---|---|---|
| temperature（5.3·High·°C） | 最差卡 >90°C | -30% 满额 | DCMI；无 CANN→采集器 no-op，缺失 | ● |
| temperature（5.3·High·°C） | 最差卡 >80°C | -15% 满额 | DCMI | ● |
| memory_usage（5.2·High·%，HBM） | 最差卡 >95% | -10% 满额 | DCMI hbm_info | ● |
| utilization（5.1·High·%，AICore） | 最差卡 >95% | -10% 满额 | DCMI；与 5.41 npu_util 语义重叠，合并取最差，避免重复扣分 | ● |
| health_status（5.5·Medium·-） | 值≥3（Alarm） | -30% 满额 | DCMI dcmi_get_device_health | ● |
| health_status（5.5·Medium·-） | 值==2（Warning） | -15% 满额 | DCMI | ● |
| hbm_double_ecc（5.56·High·次） | >0 | -20% 满额 | DCMI ECC（HBM UCE，严重） | ● |
| ddr_double_ecc（5.60·High·次） | >0 | -20% 满额 | DCMI ECC（DDR UCE） | ● |
| hbm_single_ecc（5.55·High·次） | >0 | -10% 满额 | DCMI ECC（HBM CE，阈值暂定） | ● |
| ddr_single_ecc（5.59·High·次） | >0 | -10% 满额 | DCMI ECC（DDR CE，暂定） | ● |
| error_code（5.10·Medium·-） | >0 | -10% 满额 | DCMI errorcode_v2（暂定，可选） | ● |
| power_draw（5.4·Medium·W） | >110% TDP | -15% 满额 | DCMI；**无 TDP 参考值→暂缓** | ○ |

> 温度规则以 High `temperature` 为主取最差，并纳入 Medium 子温度（`soc_max_temp`/`hbm_max_temp`/`cluster_temp` 等）作为"取最差"的补充输入（任一超阈值即触发）。电压/频率/带宽类 Medium 指标为诊断性，不扣分。

### 6.7 规则小结

- 纳入规则总数：CPU×7、内存×7、硬盘×4、GPU×5(+1暂缓)、NPU×10(+1暂缓)，全部源自 `indi_list` 的 High/Medium 可采集指标。
- 暂缓 2 条（GPU/NPU `power_draw`）：因采集器未产出 TDP 额定值，无法判定 110% TDP，待采集层补充 TDP 参考指标后启用。
- 网卡 5 指标全部不参与健康度。
- 阈值暂定项（saturation/fragmentation/NPU CE-ECC/error_code）的数值见上表，可在评审时微调，不破坏 §3 契约。

---

## 7. 等级映射与校准

- 总分 → 等级采用 §1.2 既定四级（Excellent≥90 / Good≥75 / Warning≥60 / Critical<60）。
- **校准原则**：单个 Medium 偏高问题（如温度>75°C、usage>80%）扣 ~10–15% 满额，使一台"仅一项轻微告警"的服务器落在 75–89（Good）；一项严重事件（UCE、SMART 异常、health Alarm）扣 20–30% 满额，叠加后可跌入 60–74（Warning）甚至 <60（Critical）。错误计数型（UCE 每个 -10 分）在加速场景满额 10/20 偏低时由绝对分主导，符合"硬件不可纠正错误即严重"的语义。
- 该校准使前端进度条/等级芯片的色阶与运维直觉一致。

---

## 8. 公共 API

```go
package health

type Evaluator struct{ /* 持有权重方案 */ }

// NewEvaluator 用指定方案构造评估器。
func NewEvaluator(scheme WeightScheme) *Evaluator

// Evaluate 对一轮全量采集指标评分。scheme 为 auto 时按 gpu/npu 存在性自动选方案。
func (e *Evaluator) Evaluate(metrics []collector.Metric) HealthScore

type WeightScheme struct { CPU, Memory, Disk, GPU int }

var (
    CPUOnlyScheme            = WeightScheme{CPU: 30, Memory: 40, Disk: 30, GPU: 0}
    Accelerated8CardScheme   = WeightScheme{CPU: 10, Memory: 20, Disk: 10, GPU: 60}
    Accelerated4CardScheme   = WeightScheme{CPU: 10, Memory: 20, Disk: 10, GPU: 60}
)

// GetScheme 按配置名返回方案：auto/cpu_only/accelerated_8card/accelerated_4card。
func GetScheme(name string) WeightScheme
```

消费方典型用法：
```go
score := health.NewEvaluator(health.GetScheme(cfg.Health.WeightScheme)).Evaluate(allMetrics)
// score 即可直接 JSON 序列化写入 snapshot.json 或 CLI 表格输出。
```

---

## 9. 测试要求

- **单规则覆盖**：每条规则至少"触发/不触发"两用例，断言 `Deductions[].Rule` 与 `Penalty`。
- **多卡 worst**：GPU/NPU 构造多卡数据，验证最差卡驱动扣分。
- **优雅降级**：指标缺失（空切片/缺某部件）时不报错、不扣分、`Score==Max`。
- **等级映射**：覆盖四级边界（89/90、74/75、59/60）。
- **schema 稳定**：`HealthScore` 序列化字段集与类型固定，作为前端契约。
- **跨平台**：`health/` 无 `//go:build` 标签、纯逻辑无 OS 依赖，Linux/Windows 均编译且可跑测试。
- 运行：`go vet ./...` → `go build ./...`（+ `GOOS=windows GOARCH=amd64` 交叉编译）→ `go test ./health/`，从仓库根运行。

---

## 10. 设计决策记录（已确认）

基于三条锚点所做的设计选择，均经评审确认，记录如下：

1. **规则引擎形态**：采用"一函数一部件 + 分文件"的轻量结构，不引入 `Rule` 接口/注册表/配置驱动（最简且够用）。可插拔规则作为后续可选项。
2. **网卡不参与健康度**：网卡 5 指标均为链路吞吐/连接类，非部件故障信号，故不入权重方案、不设扣分。
3. **`power_draw` 暂缓**：GPU/NPU 功率规则需 TDP 额定值，采集器暂未产出，本次不做，标 ○ 暂缓。
4. **CE 类 ECC 扣分阈值暂定**：NPU `hbm_single_ecc`/`ddr_single_ecc` 按"CE 轻扣(-10% 满额)、UCE 重扣(-20% 满额)"类比设定，数值可在评审时调整。
5. **`load_average` 动态阈值**：阈值取 `core_num×2`（`core_num` 为采集器产出的静态指标）；无 `core_num` 时 fallback 硬编码 8（=4×2）。
6. **GPU 与 NPU 共用满额档**：加速场景 GPU/NPU 同属"加速卡"，仅占一个权重 60，不重复计权；二者规则各自独立但满额相同。
7. **SPEC 文件位置**：本文件置于模块目录 `features/health/HEALTH_SPEC.md`，与实现代码同目录，便于模块自洽维护。
