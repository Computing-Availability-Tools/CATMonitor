# cpugov CPU 感应 NPU 节能模块技术规格说明书 (cpugov_SPEC)

> **文档定位**：本文档是 cpugov（CPU governor actuator）模块的唯一设计与规格文档。
>
> **对应代码**：`features/cpugov/`（Go package `cpugov`，与主项目同一 Go module）+ 新增 source 包 `internal/source/cpufreq/` + `internal/config` / `cmd/catmonitor` 的少量增量改动。
>
> **构建约束**：`//go:build linux`。该模块是"执行器"（actuator）而非纯监控——会写 sysfs 调频。Windows/无 cpufreq 环境降级为 no-op。

---

## 1. 概述

### 1.1 目标

在 CPU+NPU 异构节点上，当 **CPU 与 NPU 同时处于空闲** 时，将 CPU 所有核频率钉到硬件最低可调频率以节能；任一方脱离空闲则恢复原频率。核心需求：

1. **CPU idle 判定带迟滞**：CPU idle 百分比 ≥ 阈值（默认 97%，等价于利用率 ≤ 3%）视为一次"idle 采样"。需在观测窗 x 秒（默认 120s）内持续 idle 才确认进入 idle 态；观测窗内单次 non-idle 抖动不重置计时，仅连续 2 次 non-idle 才中止观测——防止瞬时波动导致永远无法进入 idle。
2. **NPU idle 无迟滞**：所有 NPU 的 `process_total == 0` 即 NPU idle；任一卡有进程即非 idle。DCMI 不可用时不降频（no-op）。
3. **双 idle 联动降频（NPU 强制优先、快恢复慢进入）**：CPU 确认 idle 且 NPU idle → pin 所有核 `scaling_min_freq = scaling_max_freq = cpuinfo_min_freq`。**NPU 一旦有进程（非 idle）→ 立即将 CPU 从任意态切回 Active 并恢复频率，绕过 2 次迟滞**；而 CPU 自身采样失配仍需连续 2 次 non-idle 才退出 idle（迟滞）。即"快恢复（NPU 触发，即时）、慢进入（CPU 观测窗 + 2 次迟滞）"，既防瞬时抖动无法进入 idle，又保证 NPU 起算即解除降频、不拖累 NPU 喂数据。
4. **安全默认**：`dry_run` 默认 true（只判定与日志，不写 sysfs）；`enabled` 默认 false；写 sysfs 需 root。

### 1.2 设计原则

- **不新增采集器**：复用 `cpu` 采集器已产出的 `usage{core=total}` 与 `npu` 采集器已产出的 `process_total`，经 scheduler tap 投递给 cpugov，避免对 `cpu` 采集器的有状态 `Collect()` 并发调用（其 `prevStats` map 非线程安全）。
- **遵循 source 层约束**：sysfs 读写集中在新增的 `internal/source/cpufreq/`，feature 层不直接 `os.ReadFile`/`os.WriteFile`。
- **幂等与可恢复**：每次写 sysfs 前比对当前值，避免重复写；降频前保存原值，进程退出/异常时尽量恢复（best-effort）。
- **模块自包含**：除 `config.go`（加 1 段配置）、`cmd/catmonitor/main.go`（加守护 goroutine + CLI 子命令）、`internal/collector/scheduler.go`（加 1 个可选 tap）、`configs/metrics.yaml`（加 `energysave` 组件条目）外，全部逻辑在 `features/cpugov/` 与 `internal/source/cpufreq/` 内。

---

## 2. 目录结构

```
features/cpugov/                       # 全部 //go:build linux
├── cpugov_SPEC.md                    # 本设计文档
├── state.go                          # CPU idle 状态机（A/B/C 三态）+ NPU/CPUSample 分类
├── state_test.go                     # 状态机表驱动单测（含抖动/连击/计时边界 + NPU override）
├── controller.go                     # 控制循环：tap 输入 → 状态机 → 执行器 → 指标产出 + Restore + Snapshot
├── controller_test.go                # 控制循环单测（mock cpufreq source + 假 tap + fake storage）
├── actuator.go                       # 降频/恢复执行器（调 cpufreq source，保存/恢复原值，幂等自愈）
├── actuator_test.go                  # 执行器单测（mock writer，验证写顺序与幂等）
├── cli.go                            # RunOnce + FormatSnapshot（`catmonitor energysave` 只读预览）
├── doc.go                            # package 说明 + build 约束
└── (无 metrics.yaml：energysave 指标已入默认 catalog，无需模块覆盖；无 static/)

internal/source/cpufreq/
├── cpufreq.go                  # Source 接口 + defaultSource（sysfs 读写）
├── cpufreq_test.go             # 用 SetRoot + testdata tree 测读路径
└── mock.go                     # MockSource（带 SetMock）供 actuator/controller 测试
```

### 与现有代码的关系

```
CATMonitor (Go module)
├── cmd/catmonitor/main.go            # ← 增量：runDaemon 加 energysave goroutine；加 "energysave" case
├── cmd/catmonitor/energysave_linux.go# 新增（//go:build linux）：startEnergysave/stopEnergysave/runEnergysaveCLI
├── cmd/catmonitor/energysave_other.go# 新增（//go:build !linux）：同名 no-op stub（不导入 cpugov）
├── internal/config/config.go         # ← 增量：Config 加 Energysave 字段 + Default 加默认段
├── internal/collector/scheduler.go   # ← 增量：加 onCollect tap 字段 + SetTap() + collectAndStore 末尾调用
├── configs/metrics.yaml              # ← 增量：加 energysave 组件 7 个指标条目
├── features/cpugov/                  # 全部新增（//go:build linux）
├── internal/source/cpufreq/          # 全部新增
└── (其余不变)
```

> **跨平台策略**：cpugov 包整体 Linux-only（写 sysfs），不在包内放空实现；非 Linux 由 main 包 `energysave_other.go` 提供同名 no-op（不导入 cpugov），`main.go` 无条件调用。故 `GOOS=windows` 交叉编译不引入 cpugov。

---

## 3. 数据流

```
 ┌──────────┐  Collect()   ┌──────────┐
 │ cpu coll │─────────────▶│ scheduler│
 └──────────┘              │  runColl │
 ┌──────────┐  Collect()   │   ↓filter│  onCollect tap ┌──────────┐
 │ npu coll │─────────────▶│   store  │───────────────▶│  cpugov  │
 └──────────┘              └──────────┘                 │ control  │
                                                        │   ↓      │
                                          ┌────────────▶│ state    │
                                          │             │ machine  │
                                          │             │   ↓      │
                          latest cpu/npu │             │ actuator │── write ──▶ /sys/.../cpufreq/*
                          metric snapshot│             │   ↓      │
                                          │             │ metrics  │── Write ──▶ storage (经 metrics.Filter)
                                          │             └──────────┘
                                          │
                            ┌────────────┴───────────┐
                            │ controller.atomicLatest │  (sync.Mutex 守护)
                            │  cpuUsage float64        │
                            │  cpuUsageTs time.Time    │
                            │  npuProcTotal int        │
                            │  npuTs time.Time         │
                            │  npuKnown bool           │
                            └──────────────────────────┘
```

- scheduler 每完成一次 `collectAndStore`，若已安装 tap，则以**该批次过滤后的 metrics** 调用 `onCollect(metrics)`。cpu 批次与 npu 批次分别到达。
- cpugov 的 tap handler 从批次中提取：
  - `cpu.usage{core=total}` → `cpuUsage`、`cpuUsageTs`
  - `npu.process_total`（所有 npu_id 求和）→ `npuProcTotal`、`npuTs`、`npuKnown=true`
- 控制循环（独立 goroutine，`interval` 周期）读取 `latest` 快照 → 喂入状态机 → 决策 → 执行器 → 写状态指标。

---

## 4. CPU idle 状态机

### 4.1 三态定义

| 状态 | 含义 | 降频动作 |
|------|------|----------|
| **A: Active** | 未确认 idle（含初始态、被 NPU 强制中断后） | 不降频（若此前在降频则恢复） |
| **B: Observing** | 观测期，计时器向 `observe_window` 推进 | 不降频 |
| **C: ConfirmedIdle** | 已确认 idle（隐含进入时 NPU idle） | NPU 仍 idle 且 `!dry_run` 时降频 |

### 4.2 输入与判定

每 tick 取两类输入：

**输入 1 — NPU 状态**（取自 `latest.npuProcTotal` / `npuTs` / `npuKnown`）：

- **NPU idle**：`npuKnown && npuProcTotal == 0`。
- **NPU 非 idle**：`npuKnown && npuProcTotal > 0`（有进程）。
- **NPU unknown**：`!npuKnown`（DCMI 不可用）或 `now - npuTs > npu_stale_sec`（数据陈旧）。

**输入 2 — CPU 采样**（取自 `latest.cpuUsage`）：

- `idlePct = 100 - cpuUsage`。
- **idle 采样**：`idlePct >= cpu_idle_threshold_pct`（默认 97 ⇒ `cpuUsage <= 3`）。
- **non-idle 采样**：否则。
- **CPU 数据陈旧**：`now - cpuUsageTs > 2*interval`（默认 6s）→ CPU 采样记 unknown。

### 4.2.1 NPU override（最高优先级，先于 CPU 转移评估）

每 tick **首先**评估 NPU override：

- **NPU 非 idle**（有进程）⇒ **无论 CPU 当前处于 A/B/C 何态，立即转 A**：清零 `streak`、`elapsed`；若此前 `downclock_active` 为真则恢复频率（§6.3）。**此路径不经 2 次迟滞**，下一 tick 即生效（≤ `interval`）。
- **NPU idle 或 unknown** ⇒ 不触发 override，按 §4.3 CPU 采样转移评估。其中 NPU unknown 时 CPU 状态机仍可推进（仅影响 `downclock_active`，见 §6.4），即 CPU 可进 C 但不降频。

### 4.3 状态转移

记 `streak` = 连续 non-idle 计数；`elapsed` = 自进入 B 起的累计时长。

> 下表"NPU idle/unknown"行才评估 CPU 采样；"NPU 非 idle"行（override）优先，见 §4.2.1。

| 当前态 | NPU | CPU 采样 | 动作 & 转移 |
|--------|-----|---------|-------------|
| **任意** | **非 idle（override）** | 任意 | **立即 → A**：`streak=0`、`elapsed=0`；若 `downclock_active` 则恢复频率。不经迟滞 |
| A | idle/unknown | idle | → B：`elapsed=0`、`streak=0`、`enteredAt=now` |
| A | idle/unknown | non-idle | 留 A（`streak` 不维护） |
| B | idle/unknown | idle | `streak=0`；`elapsed += tick`；若 `elapsed >= observe_window` → C |
| B | idle/unknown | non-idle | `streak++`；**idleElapsed 不推进（暂停但不重置）**；若 `streak >= non_idle_break`(默认2) → A（清零计时）；否则留 B（容忍单次抖动，不计入持续 idle 时长） |
| B | idle/unknown | unknown（CPU 数据陈旧） | 不推进计时（暂停），不改变 streak |
| C | idle | idle | `streak=0`；留 C（`downclock_active` 维持，见 §6.4） |
| C | idle | non-idle | `streak++`；若 `streak >= non_idle_break` → A（触发恢复频率）；否则留 C |
| C | idle | unknown（CPU 数据陈旧） | 留 C（保守：已 idle，不因短暂缺数据即恢复） |
| C | unknown（NPU 数据陈旧/DCMI 不可用） | 任意 | 留 C 但 `downclock_active=false`（不降频/触发恢复，§6.4） |
| 任意 | 任意 | 任意 | `enabled=false` / `dry_run` 切回 → 立即回 A 并恢复频率（见 §7.4） |

> **对原文"反之在观测期间，需连续两次都是非 idle 态才会退出观测期，直到下次进入 idle 态再进入观测期"的落实**：B 中 `streak>=2` → A；A 下一次 idle 采样 → B。"防止瞬时波动一直无法进入 idle"对应 B 中单次 non-idle **不重置也不推进** idleElapsed（暂停），仅 `streak++`；窗口只累计 idle 时长，故瞬时抖动只延缓确认而不阻断。
>
> **NPU override 的语义**：NPU 有进程即"系统不再整体空闲"，CPU 应立刻解除降频以备喂数据；故 override 绕过迟滞即时生效。CPU 自身 2 次迟滞仅处理 NPU 已 idle 时的 CPU 抖动。

### 4.4 时序示例

```
[场景 1：CPU 自身 2 次迟滞退出（idleElapsed 仅累计 idle 时长）]
t=0   usage=2%  (idle)     NPU idle   A → B  idleElapsed=0  streak=0   (entry tick, delta=0)
t=3  usage=95% (non-idle) NPU idle   B      streak=1   (idleElapsed 暂停，容忍单次抖动)
t=6  usage=1%  (idle)     NPU idle   B      streak=0   idleElapsed += 3
...                                          (持续 idle，idleElapsed 每tick +3)
t≈123 usage=1% (idle)     NPU idle   B → C  idleElapsed >= 120  → 降频
                (因 t=3 那次抖动少计 3s，确认比 120s 晚约一个 tick)
t=126 usage=99%(non-idle) NPU idle   C      streak=1   (单次 non-idle，留 C)
t=129 usage=99%(non-idle) NPU idle   C → A  streak=2  → 恢复频率
t=132 usage=2% (idle)     NPU idle   A → B  重新观测

[场景 2：NPU override 即时退出（不经迟滞）]
t=200 CPU∈C 降频中       NPU idle   C  维持降频
t=203 CPU idle 采样      NPU proc=1 NPU 非 idle → override  C → A 立即 → 恢复频率
t=206 CPU idle 采样      NPU proc=0 NPU idle   A → B  重新进入观测（需满窗才再降频）
```

---

## 5. NPU idle 判定

- **判定式**：`npuKnown && npuProcTotal == 0` ⇒ NPU idle；`npuKnown && npuProcTotal > 0` ⇒ 非 idle。
- **多卡**：`npuProcTotal` = 所有 `npu.process_total{}` 指标 Value 之和（任一卡有进程即整体非 idle）。
- **DCMI 不可用**：npu 采集器 `src.Available()==false`，不产出 `process_total` ⇒ `npuKnown=false`。此时**不降频**（保守：无法确认 NPU idle 则不动作），状态指标 `energysave.npu_idle` 记为 `-1`（unknown）；CPU 状态机仍可推进，但 `downclock_active` 始终 false。
- **数据陈旧**：`now - npuTs > npu_stale_sec` ⇒ 视为 unknown，不降频。
- **无迟滞**：NPU 自身无观测期，进程有无直接决定 NPU 状态。
- **对 CPU 的反作用（override）**：NPU 非 idle 是 CPU 状态机的**强制输入**——一旦判定 NPU 非 idle，立即将 CPU 从 A/B/C 任意态切回 A 并恢复频率（§4.2.1），绕过 2 次迟滞。即 NPU 状态不仅门控 `downclock_active`，更直接驱动 CPU 状态。

---

## 6. 降频/恢复执行器

### 6.1 cpufreq source 接口（`internal/source/cpufreq/`）

```go
type Source interface {
    Available() bool                       // 任一核存在 cpufreq 目录且可读 cpuinfo_min_freq
    Cores() ([]string, error)              // 在线且有 cpufreq 的核名（"cpu0"…）
    InfoMinFreq() (uint64, error)          // cpuinfo_min_freq（kHz，硬件最低，静态）
    InfoMaxFreq() (uint64, error)          // cpuinfo_max_freq（kHz，硬件最高，静态）
    CurMinFreq(core string) (uint64, error)
    CurMaxFreq(core string) (uint64, error)
    Governor(core string) (string, error)
    SetMinFreq(core string, kHz uint64) error
    SetMaxFreq(core string, kHz uint64) error
    SetGovernor(core string, gov string) error
}
// SetRoot(path) 测试缝；SetMock(m) 测试缝
```

- `Available()==false`（WSL/虚拟机无 cpufreq）→ 整个模块 no-op。
- 写失败（权限/驱动只读，如 intel_pstate active 模式）→ 记 error 日志，标记 `actuatorOk=false`，本周期不再重试写，下周期重试。

### 6.2 降频动作（pin to min）

**触发条件**：每 tick 重算 `downclock_active = (CPU∈C) AND (NPU idle) AND (!dry_run) AND cpufreqAvailable AND actuatorOk`。因 §4.2.1 override 保证"NPU 非 idle 时 CPU 不可能在 C"，故 `CPU∈C` 已隐含"进入 C 的瞬间 NPU idle"，`(NPU idle)` 项额外兜底"NPU 后续变 unknown（数据陈旧/DCMI 不可用）时不降频"。

**边沿驱动**：仅当 `downclock_active` 由 false→true 时执行降频（避免每 tick 重复写）；由 true→false 时执行恢复（§6.3）。NPU override 触发的 false→false 维持、true→false 由 §4.2.1 经 A 转移触发恢复。

对每个 core，目标 `min=max=infoMin`：

1. 读 `infoMin = InfoMinFreq()`（首核，全局硬件下限）。
2. 对每个 core（顺序无关，可并发但 sysfs 写建议串行避免竞争）：
   1. 若 `CurMinFreq != infoMin` → `SetMinFreq(core, infoMin)`（**先写 min**，确保 `min <= max` 不变式；当前 min 通常 ≥ infoMin，降低 min 合法）。
   2. 若 `CurMaxFreq != infoMin` → `SetMaxFreq(core, infoMin)`（后写 max；此时 `min==infoMin`，`max=infoMin >= min` ✓）。
   3. 不改 governor（pin 范围即生效，governor 无法越过 [min,max]）。
3. `min_freq_override` 配置非 0 时，用其值替代 `infoMin`（需落在 `[cpuinfo_min, cpuinfo_max]` 区间，越界则回退用 `infoMin` 并记 warn）。

> **写顺序**：先 min 后 max，避免"先写 max=infoMin 导致 max<当前 min 触发 EINVAL"。

### 6.3 恢复动作

降频前对每个 core 保存 `(origMin, origMax, origGov)`。恢复时：

1. 对每个 core：
   1. 若 `CurMaxFreq != origMax` → `SetMaxFreq(core, origMax)`（**先恢复 max**，抬高上界）。
   2. 若 `CurMinFreq != origMin` → `SetMinFreq(core, origMin)`。
   3. governor 不变（从未改过）。
2. 清空保存（恢复后丢弃，下次降频重新采集）。

### 6.4 幂等与一致性

- 每次写前读当前值比对，值已一致则跳过写（减少 sysfs 抖动 + 日志噪声）。
- `downclock_active` = (CPU∈C) AND (NPU idle) AND (!dry_run) AND cpufreqAvailable AND actuatorOk；仅在边沿（false→true / true→false）触发降频/恢复，稳态不重复写。状态指标 `energysave.downclock_active` 反映此目标态；实际写成功/失败用 `energysave.actuator_ok` 反映。
- 进程收到 SIGINT/SIGTERM：在 `main.go` 的 shutdown 路径调用 `controller.Restore()` best-effort 恢复（避免遗留低频）。

---

## 7. 配置

### 7.1 config 增量（`internal/config/config.go`）

```go
type Config struct {
    Server     ServerConfig            `yaml:"server"`
    Collectors map[string]CollectorCfg `yaml:"collectors"`
    Storage    StorageConfig           `yaml:"storage"`
    Health     HealthConfig           `yaml:"health"`
    Energysave EnergysaveConfig        `yaml:"energysave"`   // 新增
}

type EnergysaveConfig struct {
    Enabled             bool          `yaml:"enabled"`               // 默认 false
    Interval            time.Duration `yaml:"interval"`              // 默认 3s，控制循环周期
    CpuIdleThresholdPct float64       `yaml:"cpu_idle_threshold_pct"`// 默认 97；idle% ≥ 此值视为 idle 采样
    ObserveWindow       time.Duration `yaml:"observe_window"`        // 默认 120s（x 秒观测期）
    NonIdleBreak        int           `yaml:"non_idle_break"`         // 默认 2，连续 non-idle 退出阈值
    DryRun              bool          `yaml:"dry_run"`                // 默认 true，只判定与日志不写 sysfs
    MinFreqOverride     uint64        `yaml:"min_freq_override"`      // 默认 0=用 cpuinfo_min_freq
    NpuStaleSec         int           `yaml:"npu_stale_sec"`          // 默认 6，NPU 数据陈旧阈值
}
```

`Default()` 增加：

```go
Energysave: EnergysaveConfig{
    Enabled:             false,
    Interval:            3 * time.Second,
    CpuIdleThresholdPct: 97,
    ObserveWindow:       120 * time.Second,
    NonIdleBreak:        2,
    DryRun:              true,
    MinFreqOverride:     0,
    NpuStaleSec:         6,
},
```

### 7.2 配置示例（`configs/catmonitor.yaml`）

```yaml
energysave:
  enabled: true            # 需 root 守护进程
  interval: 3s
  cpu_idle_threshold_pct: 97   # idle 率 ≥ 97%（≈ 利用率 ≤ 3%）
  observe_window: 120s          # x 秒观测期
  non_idle_break: 2             # 连续 2 次 non-idle 退出
  dry_run: false                # 生产环境关闭 dry_run 才真正写 sysfs
  min_freq_override: 0          # 0 = 用 cpuinfo_min_freq
  npu_stale_sec: 6
```

### 7.3 字段语义对照（需求 → 配置）

| 需求 | 配置项 | 默认 |
|------|--------|------|
| CPU 利用率 < 阈值进 idle | `cpu_idle_threshold_pct=97` ⇒ `usage ≤ 3` | 97 |
| 持续 x 秒确认 idle | `observe_window` | 120s |
| 连续 2 次 non-idle 退出 | `non_idle_break` | 2 |
| NPU 无进程即 idle | （硬编码 `process_total==0`） | — |

### 7.4 运行期开关

- `enabled=false` → 不启动 goroutine。
- 运行中 `dry_run` 由配置一次性读取；CLI `energysave --dry-run/--apply` 可覆盖单次运行行为。`--apply` 显式覆盖 `dry_run=false`（用于排障后一键启用）。

---

## 8. 集成方式

### 8.1 守护 goroutine（`cmd/catmonitor/main.go` runDaemon）

仿 `health` goroutine 模式：

```go
if cfg.Energysave.Enabled {
    cpugovCtl := cpugov.NewController(cpugov.Config{
        Interval:            cfg.Energysave.Interval,
        CpuIdleThresholdPct: cfg.Energysave.CpuIdleThresholdPct,
        ObserveWindow:       cfg.Energysave.ObserveWindow,
        NonIdleBreak:        cfg.Energysave.NonIdleBreak,
        DryRun:              cfg.Energysave.DryRun,
        MinFreqOverride:     cfg.Energysave.MinFreqOverride,
        NpuStaleSec:        cfg.Energysave.NpuStaleSec,
        Logger:             logger,
    })
    scheduler.SetTap(cpugovCtl.OnCollect)   // tap 投递最新指标
    go cpugovCtl.Run(ctx, store)           // store 用于写状态指标（经 metrics.Filter）
}
```

shutdown 路径：`cpugovCtl.Restore()`（best-effort 恢复频率）需在 `cancel()` 后、`scheduler.Stop()` 前调用。

### 8.2 scheduler tap（`internal/collector/scheduler.go`）

```go
type Scheduler struct {
    ...
    onCollect func([]Metric) // 可选 tap，DI 避免 import cycle
}

func (s *Scheduler) SetTap(f func([]Metric)) { s.onCollect = f }

// collectAndStore 末尾追加：
if s.onCollect != nil {
    s.onCollect(metrics)   // metrics 为过滤后批次
}
```

> tap 只读 metrics，不得阻塞（cpugov 的 OnCollect 仅做原子拷贝，O(1)）。

### 8.3 CLI 子命令（`catmonitor energysave`）

`features/cpugov/cli.go` 暴露 `RunOnce(cfg, src, batch, now) Snapshot` + `FormatSnapshot`；`cmd/catmonitor/energysave_linux.go` 负责采集一次 + 调用。`main.go` 增加 case：

```
case "energysave":
    runEnergysave()   // 一次性：采集一次 → 判定 → 打印只读状态预览
```

实现要点：
- **CPU usage 需 delta**：cpu 采集器 `usage` 由 prev/curr 算出，单次调用 `hasPrev=false` → usage=0（伪 idle）。CLI 先 warm-up 调一次 `cpu.Collect()`（建立 prevStats），`sleep 1s`，再正式采集，得真实利用率。
- **只读**：`RunOnce` 内部强制 `DryRun=true`，单次 tick 无法从 fresh 态到达 ConfirmedIdle（需观测窗 x 秒），故 CLI 永不写 sysfs。写操作只由守护进程承担。
- **非 Linux**：`energysave_other.go` 打印 "not supported on this platform"。

flags：`--config`（复用，配置 energysave 段决定阈值/窗口用于预览判定）。原设计 `--apply` 经评审改为只读：单次进程无法保存"原值"用于恢复，强制写会遗留低频且无法自恢复，风险高于收益，故 CLI 不提供写开关。

### 8.4 指标产出

cpugov 每周期产出下列指标，经 `metrics.Filter` 后 `store.Write`：

| 指标 | Component | Unit | 说明 |
|------|-----------|------|------|
| `cpu_state` | energysave | | 0=Active,1=Observing,2=ConfirmedIdle |
| `cpu_idle_sample` | energysave | bool(0/1) | 本轮 idle 采样 |
| `npu_idle` | energysave | | -1=unknown,0=non-idle,1=idle |
| `downclock_active` | energysave | bool(0/1) | 当前是否在降频态（C + NPU idle + !dry_run） |
| `actuator_ok` | energysave | bool(0/1) | 上次写 sysfs 是否成功 |
| `target_freq_khz` | energysave | kHz | 目标最低频率（infoMin 或 override） |
| `current_freq_khz` | energysave | kHz | 首核当前 `scaling_cur_freq`（观测用） |

`configs/metrics.yaml` 新增 component：

```yaml
  - component: energysave
    interval: 3s
    metrics:
      - { name: cpu_state,          cn_name: "节能CPU态",        priority: Medium, unit: "",       static: false }
      - { name: cpu_idle_sample,    cn_name: "CPU idle采样",      priority: Medium, unit: "",       static: false }
      - { name: npu_idle,           cn_name: "NPU idle",          priority: Medium, unit: "",       static: false }
      - { name: downclock_active,   cn_name: "降频生效",          priority: Medium, unit: "",       static: false }
      - { name: actuator_ok,        cn_name: "执行器正常",        priority: Medium, unit: "",       static: false }
      - { name: target_freq_khz,    cn_name: "目标频率",          priority: Low,    unit: "kHz",    static: false }
      - { name: current_freq_khz,   cn_name: "当前频率",          priority: Low,    unit: "kHz",    static: false }
```

---

## 9. 降级与安全

| 场景 | 行为 |
|------|------|
| 非 Linux（Windows） | `cpugov_other.go` (`//go:build !linux`) 空实现，`Controller.Run` 直接 return |
| 无 cpufreq（虚拟机/WSL） | `cpufreq.Available()==false` → no-op，记 info 日志 |
| 非 root | `SetMinFreq/SetMaxFreq` 写失败 → `actuator_ok=false`，不 panic，下周期重试 |
| DCMI 不可用（无 `-tags dcmi` 或无 CANN） | `npu_known=false` → 永不降频，`npu_idle=-1` |
| CPU 数据陈旧（>2×interval） | 本轮不推进状态机 |
| `dry_run=true`（默认） | 状态机正常运转 + 日志，但执行器只读不写，`downclock_active` 始终 0 |
| 进程退出 | `Restore()` best-effort 恢复（仅 graceful shutdown 路径；kill -9 无法恢复） |

**安全建议**：首次部署保持 `dry_run=true` 观察日志 1–2 个观测周期，确认状态机行为符合预期后再 `dry_run=false`。

---

## 10. 测试策略

### 10.1 状态机（`state_test.go`）

表驱动：喂入 idle/non-idle 采样序列，断言状态序列与 streak/elapsed。覆盖：

- 连续 idle 满窗 → A→B→C。
- B 中单次 non-idle 抖动不重置 elapsed（`streak=1` 后 idle 复位 streak，elapsed 持续）。
- B 中连续 2 次 non-idle → A。
- C 中连续 2 次 non-idle → A（触发恢复）。
- C 中单次 non-idle 不退出。
- 数据 unknown tick 不推进。
- **NPU override**：CPU∈C 降频中 → 喂 NPU `process_total>0` → 立即转 A 且触发恢复（无需 2 次 non-idle CPU 采样）；override 在 B 中同样中止观测回 A。
- **NPU unknown**：`npu_known=false` 时 CPU 可进 C 但 `downclock_active=false`。
- `non_idle_break` / `observe_window` 可配置边界。

### 10.2 NPU 判定（`state_test.go`）

- 多卡 process_total 求和；全 0 → idle；任一 >0 → 非 idle。
- `npuKnown=false` → unknown。

### 10.3 cpufreq source（`internal/source/cpufreq/cpufreq_test.go`）

- `SetRoot(testdata)`：构造 `tests/testdata/sys/cpufreq/...` 树，测 `Cores/InfoMinFreq/CurMin/CurMax/Governor` 读路径。
- `SetMock`：测 `SetMinFreq/SetMaxFreq` 调用序列与值。

### 10.4 执行器（`actuator_test.go`）

- 降频写顺序：先 min 后 max，幂等跳过。
- 恢复写顺序：先 max 后 min。
- `min_freq_override` 越界回退 `infoMin`。
- 写失败时 `actuator_ok=false`。

### 10.5 控制循环（`controller_test.go`）

- 注入假 tap（直接调 `OnCollect` 喂 cpu/npu 批次）+ mock cpufreq source。
- 端到端：A→B→C→降频（验证 mock 收到 `SetMinFreq/SetMaxFreq`=infoMin）→ 喂 non-idle×2 → C→A → 验证恢复调用。
- **override 端到端**：C 降频中 → 喂 NPU `process_total>0` → 立即收到恢复调用（无中间 2 次 non-idle）。
- `dry_run=true` 时执行器不写。

### 10.6 testdata

新增 `tests/testdata/sys/devices/system/cpu/cpu0/cpufreq/{cpuinfo_min_freq,cpuinfo_max_freq,scaling_min_freq,scaling_max_freq,scaling_governor,scaling_cur_freq,scaling_available_frequencies}` 等样例文件。

---

## 11. 边界与限制

1. **intel_pstate active 模式**：`scaling_min/max_freq` 对部分驱动只读；此时降频无效，`actuator_ok=false`。建议 BIOS/内核切 `intel_pstate=passive` 或用 `acpi-cpufreq`。
2. **governor 不改**：仅 pin min/max 范围。如需切 powersave，留作 `min_freq_override` 之外的后续扩展（`SetGovernor` 接口已预留）。
3. **频率粒度**：`cpuinfo_min_freq` 为硬件最低；某些 SKU 仍有 `scaling_available_frequencies` 离散档位，pin `cpuinfo_min_freq` 落在档位内即合法。
4. **kill -9 不恢复**：仅 graceful shutdown 恢复；强杀会遗留低频，重启后 cpugov 重新采集并在条件满足时再降频（幂等），不满足时恢复原值（首次启动无"原值"则保持现状）。
5. **NPU 进程判定口径**：`process_total` 来自 DCMI `ResourceInfoFull`，反映占用 NPU 的 PID 数；非 NPU 上的纯 CPU 进程不计入。符合"NPU 是否有进程"语义。
6. **scheduler tap 非线程安全调用**：tap 在 `collectAndStore` 末尾于 collector goroutine 内调用；cpugov `OnCollect` 内仅 `sync.Mutex`+字段赋值，无阻塞、无外部调用，安全。
7. **与 health goroutine 并存**：health 仍独立 `c.Collect()`（pre-existing 竞态不在本特性范围内）；cpugov 不调用任何采集器 `Collect()`，零新增竞态。
8. **NPU override 时延**：override 的"立即"受限于 npu 采集周期（`npu` 默认 3s）——NPU 进程出现后，最迟在下一次 npu tap 到达后一个控制周期内解除降频（≤ ~2×interval）。若需更短时延，调小 `npu` 采集器 interval 与 `energysave.interval`。
9. **NPU 进程频繁进出**：NPU 无迟滞 + override 即时退出，但**重新进入降频仍需 CPU 满观测窗 x 秒**（§4.3 A→B→C），故"NPU 抖动"不会高频切档——退出快、重入慢，正是预期。仅当 NPU 以 > x 秒间隔周期性空转且 CPU 持续 idle 时才会反复降/恢复，属正常节能行为。

---

## 12. 实现步骤（建议顺序）

1. `internal/source/cpufreq/`：接口 + defaultSource + mock + testdata + 单测。
2. `features/cpugov/state.go` + `state_test.go`：状态机纯逻辑。
3. `features/cpugov/actuator.go` + `actuator_test.go`：降频/恢复，调 cpufreq source。
4. `features/cpugov/controller.go` + `controller_test.go`：tap handler + 控制循环 + 指标产出。
5. `features/cpugov/cli.go`：`energysave` 子命令一次性状态。
6. 增量改动：`internal/config/config.go`（+字段/默认）、`internal/collector/scheduler.go`（+tap）、`configs/metrics.yaml`（+energysave 组件）、`cmd/catmonitor/main.go`（+goroutine/+case）。
7. `features/cpugov/doc.go`（package 说明 + build 约束）+ `cmd/catmonitor/energysave_{linux,other}.go`（守护 goroutine + CLI 子命令 + 非 Linux stub）。
8. 验证：`go vet ./...` → `go build ./...`（含 `GOOS=windows` 交叉编译确认 stub 不引入 cpugov）→ `go test ./...` → 手动 `catmonitor energysave` 只读预览 + `catmonitor daemon`（enabled=true dry_run=true）启停观察日志。

---

## 13. 需求点回溯

| 需求条 | 落实章节 |
|--------|----------|
| CPU 利用率低于阈值进 idle（idle 率 ≥ 97%） | §4.2, §7.3 |
| 连续观测 x 秒（默认 2 分钟）确认 idle | §4.3 (B→C), §7.1 `observe_window` |
| 观测期内连续 2 次 non-idle 才退出观测 | §4.3 (B→A), §7.1 `non_idle_break` |
| 防瞬时波动无法进 idle | §4.3 单次 non-idle 不重置 elapsed |
| NPU 有无进程直接定 idle（无观测器） | §5 |
| NPU 一有进程立即强制 CPU 退出 idle（不经迟滞） | §4.2.1 override, §4.3, §5 |
| CPU+NPU 均 idle 才降频 | §4.3 (C), §6.2 触发条件 |
| 所有核降至可调节最低频率 | §6.2 pin min=max=cpuinfo_min_freq |
| x 可配置、默认 2 分钟 | §7.1 `observe_window=120s` |
