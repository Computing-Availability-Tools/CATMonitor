# CATMonitor 健康度模块规格说明书 (HEALTH_SPEC)

> 本文从零设计 CATMonitor 的**健康度评估模块**：在已具备采集架构、已定义指标清单、已确定等级划分的前提下，定义"如何从采集到的指标计算服务器健康度"。
> 指标清单以 [`docs/CATMonitor_indi_list.md`](../../docs/CATMonitor_indi_list.md) 为唯一事实来源；总规格见 [SPEC.md](../../SPEC.md)。

| 版本 | 日期 | 说明 |
|------|------|------|
| v0.1 | 2026-07-15 | 初版：基于采集架构 + High/Medium 指标 + 四级等级的从零设计 |
| v0.2 | 2026-08-13 | 重构：4 套权重方案（按 NPU 卡数自动选择）+ Network/Chassis 纳入评估 + 关注点预算 + card_drop + 分级互斥 |

---

## 1. 概述

### 1.1 目标

为 CATMonitor 设计一个**纯评估库**：输入是采集器产出的指标流，输出是一个 0–100 的健康度评分与等级，供 CLI 与 Web 仪表盘展示。模块本身**不采集任何数据**——所有输入来自既有采集架构。

### 1.2 设计前提

1. **当前采集架构**：7 个采集器（cpu/memory/disk/gpu/npu/network/chassis）经来源层（`internal/source/` 14 包）获取数据、产出 `collector.Metric`；外部工具/文件不可用时采集器优雅降级（对应指标不产出，不报错）。
2. **纳入评估的部件**：全部 7 个部件均参与健康度评估。每个部件的扣分规则基于其 High/Medium 优先级指标设计。

   | 部件 | 指标数 | High | Medium | Low | 是否参与健康度 |
   |------|:---:|:---:|:---:|:---:|---|
   | CPU | 40 | 4 | 12 | 24 | 是 |
   | Memory | 20 | 4 | 8 | 8 | 是 |
   | Disk | 14 | 1 | 9 | 4 | 是 |
   | GPU | 7 | 3 | 3 | 1 | 是 |
   | NPU | 123 | 11 | 91 | 21 | 是 |
   | Network | 7 | 1 | 5 | 1 | 是 |
   | Chassis | 5 | 2 | 2 | 1 | 是 |

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
- 不依赖 GPU/NPU 功率 TDP 参考值（采集器暂未产出，相关规则暂缓）。

---

## 2. 模块边界与依赖

### 2.1 位置

- `features/health/`，与 `features/web/`、`features/dfee/` 同层级，同一 `go.mod`。作为公开包，CAT 系列其他工具可直接 import。
- 包名 `package health`。

### 2.2 依赖关系

```
cmd/catmonitor ──┐
features/web/ ───┼──>  features/health/  ──>  internal/collector (仅 collector.Metric 类型)
                 │
internal/config ─┘   (HealthConfig 留在 internal/config，不动)
```

- **唯一下游依赖**：`internal/collector` 的 `collector.Metric`。
- **禁止**：import `internal/source/*`、`internal/config`、`cmd/*`、`web/*`；禁止 `os.ReadFile`/`exec.Command`/系统调用。一切数据经采集器产出后喂入。

### 2.3 上游消费方

- `cmd/catmonitor`：`health` 子命令（一次性评估并表格输出）、`daemon`（周期性评估写入 snapshot）。
- `features/web`：读取 snapshot 中的 health 字段展示。
- 调用契约：传入**一轮全量采集**的 `[]collector.Metric`，返回 `HealthScore`。

---

## 3. 数据模型（输出契约）

设计为可直接序列化为 `snapshot.json` 的 health 字段，字段名稳定、前端可直接消费。

```go
type HealthScore struct {
    Score      int                       `json:"score"`        // 0-100 总分
    Grade      string                    `json:"grade"`        // Excellent|Good|Warning|Critical
    ServerType string                    `json:"server_type"` // cpu_only | accelerated
    Components map[string]ComponentScore `json:"components"`   // 按部件拆分
    Timestamp  time.Time                 `json:"timestamp"`
}

type ComponentScore struct {
    Score      int         `json:"score"`      // 该部件实得
    Max        int         `json:"max"`        // 该部件满额（随权重方案）
    Deductions []Deduction `json:"deductions"` // 触发的扣分项
}

type Deduction struct {
    Rule    string  `json:"rule"`    // 规则名（如 "usage>90%"）
    Penalty float64 `json:"penalty"` // 扣减分值
}
```

---

## 4. 评估架构

### 4.1 流程

1. **分组**：按 `Metric.Component` 将一轮指标分为 7 组（cpu/memory/disk/gpu/npu/network/chassis）。
2. **权重方案自动检测**：
   - 若存在 GPU 指标 → `Accelerated8CardScheme`
   - 若存在 NPU 指标 → 按 NPU 卡数（唯一 `npu_id` 标签数）选择：
     - ≥5 卡 → `Accelerated8CardScheme`
     - 3-4 卡 → `Accelerated4CardScheme`
     - 1-2 卡 → `Accelerated2CardScheme`
   - 无 GPU/NPU → `CPUOnlyScheme`
   - 配置可显式指定覆盖自动检测。
3. **逐部件评估**：对每个存在的部件分组，调用 `evaluate<部件>(metrics, 满额)`，返回 `ComponentScore`（满额起步，逐条规则扣分，下限 0）。
4. **多卡聚合**：GPU/NPU 多卡场景对每条规则取**最差卡**的值触发（worst across cards）。
5. **汇总**：`Score` = Σ 部件 Score；`Grade` 由 §1.2 等级表映射。

### 4.2 文件结构

```
features/health/
├── health.go        # 公共类型 + Evaluator + Evaluate(编排) + 分组 + npuCardCount + 等级映射
├── scheme.go        # WeightScheme + 4 套预置方案 + GetScheme
├── cpu.go           # evaluateCPU
├── memory.go        # evaluateMemory
├── disk.go          # evaluateDisk
├── gpu.go           # evaluateGPU
├── npu.go           # evaluateNPU
├── network.go       # evaluateNetwork
├── chassis.go       # evaluateChassis
├── util.go          # findMetric / worstValue / hasAnyPositive 内部工具
├── HEALTH_SPEC.md   # 本文件
└── *_test.go        # 每部件每规则单测
```

### 4.3 优雅降级

来源不可用 → 采集器不产出对应指标 → 分组为空 → 跳过该部件或该规则，不报错、不扣分。规则函数对"指标缺失"一律视为不触发。

---

## 5. 权重方案

各部件满额之和 = 100。按服务器 NPU 卡数分四档：

| 方案（配置名） | CPU | Memory | Disk | GPU/NPU | Network | Chassis | 适用 |
|------|:---:|:---:|:---:|:---:|:---:|:---:|------|
| `cpu_only` | 25 | 25 | 30 | 0 | 10 | 10 | 无 GPU/NPU |
| `accelerated_2card` | 20 | 20 | 20 | 20 | 10 | 10 | 1-2 卡 |
| `accelerated_4card` | 15 | 15 | 20 | 30 | 10 | 10 | 3-4 卡 |
| `accelerated_8card` | 15 | 15 | 15 | 35 | 10 | 10 | 5-8 卡 |

设计依据：
- **CPU-only 场景**：CPU 与内存各 25，硬盘 30（最大故障面），网络/机箱各 10。
- **加速场景**：NPU 卡数越多，NPU 权重越高（20→30→35），CPU/内存/硬盘相应降低。
- **Network 与 Chassis**：所有方案均为 10，环境因素稳定不变。
- GPU 与 NPU **共用** `GPU` 满额档（加速卡只算一类，不重复计权）。
- `auto`（默认）= 运行时按 NPU 卡数自动选择。
- 卡数统计：唯一 `npu_id` 标签值数量（chip0+chip1 共享同一 card_id = 1 张卡）；fallback 用 `npu_num/2`。

---

## 6. 扣分规则设计

### 6.1 设计原则

1. **关注点预算制**：每个部件将满额分为若干**关注点**，每个关注点有独立预算（百分比），同关注点内分级互斥（只扣最高触发档）。
2. **预算总和 = 100%**：所有关注点预算之和恰为满额的 100%，保证最差情况下分数归零。
3. **严重度分级**：严重事件（UCE/SMART 失败/掉卡/Alarm）预算占比高；渐近问题（使用率/温度）分级递增。
4. **多卡取最差**；**缺指标不触发**。

### 6.2 CPU 规则

| 关注点 | 预算 | 分级 | 数据来源 |
|--------|:---:|------|---------|
| Temperature | 30% | >85°C: 30%, >75°C: 15% | ipmitool SDR |
| Usage | 20% | >90%: 20%, >80%: 10% | /proc/stat |
| Load | 20% | >cores×2: 20% | /proc/loadavg |
| CE Error | 10% | ≥3: 10%, >0: 5% | mcelog/dmesg |
| UCE Error | 20% | >0: 20% | mcelog/dmesg |

> load 阈值取 `core_num×2`；无 `core_num` 时 fallback 8（=4×2）。

### 6.3 Memory 规则

| 关注点 | 预算 | 分级 | 数据来源 |
|--------|:---:|------|---------|
| Usage | 25% | >90%: 25%, >80%: 12% | /proc/meminfo |
| Swap | 10% | >50%: 10% | /proc/meminfo |
| Saturation | 15% | >80%: 15% | /proc/pressure/memory (PSI) |
| Fragmentation | 10% | >80%: 10% | /proc/buddyinfo |
| CE Error | 10% | ≥3: 10%, >0: 5% | EDAC /sys/.../ce_count |
| UCE Error | 30% | >0: 30% | EDAC ue_count |

### 6.4 Disk 规则

| 关注点 | 预算 | 分级 | 数据来源 |
|--------|:---:|------|---------|
| Space | 35% | >90%: 35%, >80%: 15% | statfs syscall |
| I/O Wait | 15% | >20%: 15% | /proc/stat |
| SMART | 50% | failed: 50% | smartctl -H |

### 6.5 GPU 规则

| 关注点 | 预算 | 分级 | 数据来源 |
|--------|:---:|------|---------|
| Temperature | 35% | >90°C: 35%, >80°C: 15% | nvidia-smi |
| Memory | 20% | >95%: 20% | nvidia-smi |
| Utilization | 15% | >95%: 15% | nvidia-smi |
| ECC Error | 30% | >0: 30% | nvidia-smi |

### 6.6 NPU 规则

| 关注点 | 预算 | 分级 | 数据来源 |
|--------|:---:|------|---------|
| Card Drop | 20% | >0: 20% | DCMI dcmi_get_device_health (CardDrop 包装) |
| Temperature | 15% | >90°C: 15%, >80°C: 8% | DCMI + 子温度传感器取最差 |
| Health | 15% | Alarm(≥3): 15%, Warning(=2): 8% | DCMI dcmi_get_device_health |
| HBM ECC | 15% | double: 15%, single: 5% | DCMI dcmi_get_device_ecc_info (HBM) |
| DDR ECC | 15% | double: 15%, single: 5% | DCMI dcmi_get_device_ecc_info (DDR) |
| Memory | 8% | >95%: 8% | DCMI dcmi_get_device_hbm_info |
| Utilization | 5% | >95%: 5% | DCMI dcmi_get_device_utilization_rate |
| Error Code | 7% | >0: 7% | DCMI dcmi_get_device_errorcode_v2 |

> 温度规则纳入 High `temperature` 及 Medium 子温度（`hbm_temp`/`cluster_temp`/`soc_max_temp` 等）取最差。HBM/DDR ECC：double 优先于 single，互斥不叠加。

### 6.7 Network 规则

| 关注点 | 预算 | 分级 | 数据来源 |
|--------|:---:|------|---------|
| Error Count | 45% | >100: 45%, >10: 15% | /proc/net/dev (delta) |
| TIME_WAIT | 30% | >2000: 30% | /proc/net/tcp |
| ESTABLISHED | 25% | >5000: 25%, >3000: 10% | /proc/net/tcp |

### 6.8 Chassis 规则

| 关注点 | 预算 | 分级 | 数据来源 |
|--------|:---:|------|---------|
| Inlet Temp | 50% | >40°C: 50%, >35°C: 25% | ipmitool SDR |
| Outlet Temp | 50% | >60°C: 50%, >50°C: 25% | ipmitool SDR |

---

## 7. 等级映射与校准

- 总分 → 等级采用 §1.2 既定四级（Excellent≥90 / Good≥75 / Warning≥60 / Critical<60）。
- **校准原则**：单个轻微告警（如温度>75°C、usage>80%）扣 ~10–15% 满额，使"仅一项轻微告警"的服务器落在 Good（75–89）；一项严重事件（UCE 30%、SMART 50%、掉卡 20%）扣大分，叠加后可跌入 Warning 或 Critical。各部件关注点预算之和 = 100%，最差情况归零。

---

## 8. 公共 API

```go
package health

type Evaluator struct{ /* 持有权重方案 */ }

func NewEvaluator(scheme WeightScheme) *Evaluator
func (e *Evaluator) Evaluate(metrics []collector.Metric) HealthScore

type WeightScheme struct {
    CPU     int
    Memory  int
    Disk    int
    GPU     int // GPU 和 NPU 共用
    Network int
    Chassis int
}

var (
    CPUOnlyScheme           = WeightScheme{CPU: 25, Memory: 25, Disk: 30, GPU: 0, Network: 10, Chassis: 10}
    Accelerated2CardScheme  = WeightScheme{CPU: 20, Memory: 20, Disk: 20, GPU: 20, Network: 10, Chassis: 10}
    Accelerated4CardScheme  = WeightScheme{CPU: 15, Memory: 15, Disk: 20, GPU: 30, Network: 10, Chassis: 10}
    Accelerated8CardScheme  = WeightScheme{CPU: 15, Memory: 15, Disk: 15, GPU: 35, Network: 10, Chassis: 10}
)

func GetScheme(name string) WeightScheme
```

消费方典型用法：
```go
score := health.NewEvaluator(health.GetScheme(cfg.Health.WeightScheme)).Evaluate(allMetrics)
```

---

## 9. 测试要求

- **单规则覆盖**：每条规则至少"触发/不触发"两用例，断言 `Deductions[].Rule` 与 `Penalty`。
- **多卡 worst**：GPU/NPU 构造多卡数据，验证最差卡驱动扣分。
- **优雅降级**：指标缺失（空切片/缺某部件）时不报错、不扣分、`Score==Max`。
- **等级映射**：覆盖四级边界（89/90、74/75、59/60）。
- **卡数自动检测**：构造不同 NPU 卡数验证方案自动选择。
- 运行：`go test ./features/health/...`

---

## 10. 设计决策记录

1. **关注点预算制**：v0.2 改为每个关注点独立预算 + 分级互斥，替代 v0.1 的混合扣分（满额百分比 + 绝对分），使规则更清晰、预算可控。
2. **CE/UCE 改为档位制**：v0.1 按错误次数扣绝对分（×2/×10），v0.2 改为按档位（>0: 低档, ≥3: 高档），避免高频错误场景下单部件扣分失控。
3. **Network 纳入评估**：v0.1 认为"网卡属链路吞吐、非部件故障"，v0.2 将 error_count/TIME_WAIT/ESTABLISHED 纳入评估（10 分权重）。
4. **Chassis 纳入评估**：v0.2 新增，进风口/出风口温度环境监控（10 分权重）。
5. **4 套权重方案**：v0.1 只有 2 套（cpu_only + accelerated），v0.2 按 NPU 卡数细分 4 套。
6. **card_drop 新增**：NPU 掉卡检测独立规则（20% 预算），使 faultsub 故障检测与健康评估一致。
7. **`power_draw` 暂缓**：GPU/NPU 功率规则需 TDP 额定值，采集器暂未产出，不做。
8. **GPU 与 NPU 共用满额档**：加速场景 GPU/NPU 同属"加速卡"，仅占一个权重，不重复计权。
