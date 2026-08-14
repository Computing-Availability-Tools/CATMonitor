# Design: cpugov 改为消费 snapshot（方案 A）

**Date:** 2026-08-06
**Status:** Approved (pending spec review)
**Topic:** cpugov 指标获取方式从 scheduler tap 改为消费 daemon 产出的 snapshot 文件（参考 dfee），并修通 `process_total` 进入 snapshot 的路径。

## 1. 背景与动机

cpugov（CPU-senses-NPU 能效调优，`features/cpugov`）只消费两个指标：
- `cpu.usage{core=total}`（CPU 空闲%）
- `npu.process_total`（所有 `npu_id` 求和，NPU 是否有进程）

当前取数路径（`controller.go:84` `OnCollect`）：daemon 经 `scheduler.SetTap(ctl.OnCollect)` 把 scheduler 的 filtered batch 旁路给 cpugov；CLI 预览（`cli.go` `RunOnce`）则自采 cpu+npu 注入 batch。

**问题根因**（已系统排查确认）：`process_total` 在指标目录 `configs/metrics.yaml:447` 是 `priority: Low, static: false`。`metrics.Filter` 用的是 `Catalog.Selected`，其 **unscoped 分支**（`internal/metrics/metrics.go:330`）硬编码只留 `High||Medium||Static`，把所有 Low 非静态指标丢弃——无论 `collection.min_priority` 设成什么。scheduler 在 `collectAndStore`（`internal/collector/scheduler.go:116-128`）对 batch 先 `metrics.Filter` 再分发给 `storage.Write`（snapshot）和 `onCollect`（cpugov tap），**snapshot 与 tap 拿到同一份过滤后 batch**，因此 `process_total` 同时从两者消失 → cpugov `npuKnown=false` → 报 `unknown (DCMI unavailable or data stale)`（该提示语在此场景下具有误导性：DCMI 实际可用，是 Filter 截断了指标）。

**目标**：让 cpugov 像 dfee 那样作为 snapshot 的只读消费方取数，并让 `process_total` 真正进入 snapshot，使 cpugov 能工作。

## 2. 方案选型（已定 A）

- **A（采纳）**：snapshot 读数控制器 + `process_total` 升 Medium + CLI 保留自采。
- B（否决）：CLI 也读 snapshot → 丢失 `catmonitor energysave` 的独立诊断能力。
- C（否决）：正本清源修 `Selected` unscoped 分支让它尊重 `collectionThreshold`——blast radius 大（默认开始采全部 59 个 Low 指标），且 scoped 模式下不解决 cpugov 这类"非 feature-scope 特性"的问题。

## 3. 架构与数据流

### 3.1 cpugov Controller 取数改造

`Controller.tick`（`controller.go:146`）开头新增 `refreshFromSnapshot()`：

```
func (c *Controller) refreshFromSnapshot() {
    if c.cfg.SnapshotDir == "" { return }            // CLI 路径/未启用 snapshot → no-op
    var batch []collector.Metric
    for _, comp := range []string{"cpu", "npu"} {
        cs, err := snapshot.ReadComp(filepath.Join(c.cfg.SnapshotDir, "snapshot_"+comp+".json"))
        if err != nil { continue }                    // 文件缺失/读错 → 跳过该组件
        batch = append(batch, cs.Metrics...)
    }
    if len(batch) > 0 { c.OnCollect(batch) }          // 复用 OnCollect 的抽取逻辑
}
```

`tick` 改为先 `c.refreshFromSnapshot()`，再读 `c.latest`（其余逻辑不变）。复用 `OnCollect` 保证 daemon 与 CLI/测试**单一抽取逻辑**（扫 `cpu.usage{core=total}` + `npu.process_total` 求和，按 metric `Timestamp` 取最新，写 `c.latest`）。

`OnCollect` 方法**保留**（CLI `RunOnce` 与既有测试仍用），只是 daemon 不再把它注册为 tap。

### 3.2 Config 变更

`features/cpugov/controller.go` `Config` 增字段：

```
SnapshotDir string   // daemon 模式：snapshot 目录（要求 snapshot.enabled:true）；空 → 不从 snapshot 取数
```

### 3.3 daemon 接线（`cmd/catmonitor/energysave_linux.go`）

- **移除** `scheduler.SetTap(ctl.OnCollect)`（`energysave_linux.go:44`）。
- `toCpugovConfig`（`energysave_linux.go:24`）：`cfg.Snapshot.Enabled` 为真时 `SnapshotDir = cfg.Snapshot.Dir`；为假时置空并 `logger.Warn("energysave requires snapshot.enabled; cpugov will not actuate (no-op)")`。
- `startEnergysave` 启动日志补 `snapshot_dir` 字段。

### 3.4 CLI 预览（`features/cpugov/cli.go` `RunOnce`）

保留自采。`RunOnce` 显式 `cfg.SnapshotDir = ""`（确保 `tick` 的 `refreshFromSnapshot` no-op，`latest` 由 `OnCollect(batch)` 注入），再走原流程 `c.OnCollect(batch); c.tick(now)`。

### 3.5 数据流总览

- **daemon**：scheduler collect → `metrics.Filter` → `storage.Write`（PerCompWriter 写 `snapshot_<comp>.json`，含升 Medium 后的 `process_total`）→ cpugov `tick` → `refreshFromSnapshot` 读 `snapshot_cpu.json`+`snapshot_npu.json` → `OnCollect` 抽取 → `c.latest` → classify → 状态机 → actuate。
- **CLI**：直接 `c.Collect()`(cpu+npu) → `OnCollect(batch)` → `tick`（refresh no-op）→ classify → `Snapshot()`。

## 4. process_total 修通（Low → Medium）

语义上 `process_total`/`process_info` 是"NPU 运行态"指标（非诊断），升 Medium 使其在 unscoped 默认配置下过 `Selected` 的 `High||Medium||Static` 门。

**改动文件**（priority 字段 `Low` → `Medium`）：

| 文件 | 行（当前） | 指标 |
|---|---|---|
| `docs/CATMonitor_indi_list.md` | 959-960 | process_info / process_total（指标明细表）|
| `docs/CATMonitor_indi_list.md` | 2068-2069 | process_info / process_total（汇总表）|
| `configs/metrics.yaml` | 444 / 449 | process_info / process_total |
| `features/web/metrics.yaml` | ~205-208 | process_info / process_total |
| `features/health/metrics.yaml` | 444 / 449 | process_info / process_total |

`features/dfee/metrics.yaml` 不含这两个指标，不动。

> `scripts/gen_metrics_catalog.py` 有硬编码 WSL 路径 `ROOT`，本环境跑不了，故直接改 YAML + 源 doc。源 doc 已同步，未来 regen 以源 doc 为准。

## 5. 错误处理 / 回退

- **snapshot 文件缺失/读错**：`snapshot.ReadComp` err → 该组件跳过 → batch 仅 cpu 或空 → `OnCollect` 只更新有数据的字段 → `classifyNPU`/`classifyCPU` 用既有 staleness 逻辑判 `unknown`（保守不降频）。
- **`snapshot.enabled=false`**：`SnapshotDir` 空 → `refreshFromSnapshot` no-op → `latest` 始终空 → cpugov 永远 `unknown` → 永不 actuate（安全）。启动 warn。
- **数据陈旧**：metric `Timestamp`（采集时刻）vs now + `NpuStale`（默认 6s）/`2*Interval`（默认 6s）既有逻辑不变；snapshot 文件本身 `Timestamp` 不参与判定（用 metric 级 Timestamp，与 tap 路径一致）。
- **时序**：snapshot 在每个 collector 周期后写；cpugov tick 周期（默认 3s）与 cpu/npu 采集周期（3s）对齐，snapshot 最多滞后约一周期，仍在 staleness 窗口内。

## 6. 测试

- **新增** `features/cpugov/controller_snapshot_test.go`（`//go:build linux`）：
  - 写临时 `snapshot_cpu.json`+`snapshot_npu.json`（含已知 `usage{core=total}` 与 `process_total`），构造 Controller（`SnapshotDir=临时目录`），`tick` 后断言 `c.latest.cpuUsage`/`npuProcTotal`/`npuKnown` 与状态机推进。
  - 缺 `snapshot_npu.json` → `npuKnown=false` → `NPUUnknown`。
  - metric `Timestamp` 超过 `NpuStale` → `NPUUnknown`。
  - `SnapshotDir=""` → refresh no-op，`latest` 保持 `OnCollect` 注入值（兼容 CLI 路径）。
- **既有** `controller_test.go`（经 `OnCollect` 注入 batch）保持通过：`SnapshotDir` 默认空 → refresh no-op。
- **catalog**：更新/新增 `internal/metrics/metrics_test.go` 断言 `process_total` 在 unscoped + `min_priority: low` 下 `IsWanted`/`Selected` 为 true。
- **手动验证**：重建 → `catmonitor energysave` 预览 `npu_state` 不再是 `unknown`，而变为 `idle (process_total=0)`。
  > 注意：此值 `0` 来自 DCMI `ResourceInfoFull` 空桩（见 §8），**不反映真实进程数**。本改造只让 `process_total` "到达" cpugov（不再被 Filter 截断→unknown），不修"值正确性"。要 cpugov 在有 NPU 进程时正确判 non-idle，还需实现 `ResourceInfoFull` 真实 PID 取数（本次范围外，见 §8）。

## 7. 文件变更清单

新增：
- `features/cpugov/controller_snapshot_test.go`

修改：
- `features/cpugov/controller.go`：`Config` 加 `SnapshotDir`；`tick` 开头调 `refreshFromSnapshot`；新增 `refreshFromSnapshot` 方法；更新 `Controller`/`OnCollect` 文档注释反映新数据源。新增 import `features/snapshot` + `path/filepath`（snapshot→health，均不导入 cpugov，无环）。
- `features/cpugov/cli.go`：`RunOnce` 置 `cfg.SnapshotDir = ""`。
- `cmd/catmonitor/energysave_linux.go`：移除 `scheduler.SetTap`；`toCpugovConfig` 设 `SnapshotDir` + warn；启动日志补字段。
- `internal/metrics/metrics_test.go`：加 `process_total` unscoped+low 断言。
- `configs/metrics.yaml`、`features/web/metrics.yaml`、`features/health/metrics.yaml`：`process_total`/`process_info` priority `Low`→`Medium`。
- `docs/CATMonitor_indi_list.md`：959-960 + 2068-2069 priority `Low`→`Medium`。

## 8. 不在本次范围

- 修 `metrics.Selected` unscoped 分支忽略 `collectionThreshold` 的文档/代码不一致（方案 C 的"正本清源"）——单独议题，本次不动。
- cpugov 抽成独立只读二进制（方案 B）——本次不做。
- CLI 预览改读 snapshot —— 保留自采。
- `dcmi_cgo.go` `ResourceInfoFull` 空桩实现真实 PID——**本次不做，但需明确其影响**：当前桩返回 `nil,nil` → npu 采集器产出 `process_total=0`。本改造让该 `0` 值能到达 cpugov（不再 unknown），但 cpugov 据此会判 `NPUIdle`，**即使 NPU 实有进程**（如本机 vLLM）。后果：daemon 模式下若 `dry_run=false`，会在 NPU 有活进程时误降频 CPU。缓解：`dry_run` 默认 `true`（只记日志不写 sysfs）；真正安全 actuate 需先实现 `ResourceInfoFull`（或改从 `npu-smi` 取进程数）——单独议题。
