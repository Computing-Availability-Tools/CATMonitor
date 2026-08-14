# cpugov energysave 指标接入标准存储链 — 设计文档

> 日期：2026-08-14
> 范围：`cmd/catmonitor/energysave_{linux,other}.go` + `cmd/catmonitor/main.go`
> 不改：`features/cpugov/controller.go`、指标清单、输入路径、状态机、执行器

## 1. 背景

现有采集架构：

- **collector** 在 `internal/collectors/<comp>` 的 `init()` 自注册；`Scheduler` 周期调 `Collect()` → `metrics.Filter` → 写 **`sink`**（链：`PerCompWriter → cacheStore → store`）→ 进 `/metrics` + `snapshot_<comp>.json` + jsonl。`AnyWanted(component, names)` 门控子方法。
- **feature** 只用 `metrics.yaml` 声明/圈定要哪些指标 + 决定 cadence；feature 本身不采集。
- **cpugov** 是执行器特性：**消费** `cpu.usage{core=total}` 与 `npu.process_total`（读 `snapshot_cpu.json`/`snapshot_npu.json`），**产出** 7 个 `energysave.*` 指标（状态机派生：`cpu_state`/`cpu_idle_sample`/`npu_idle`/`downclock_active`/`actuator_ok`/`target_freq_khz`/`current_freq_khz`）。

## 2. 问题

`main.go:293` 把 **base `store`（`*storage.JSONLStorage`）** 而非 `sink` 传给 `startEnergysave`，于是控制器 `emitMetrics`（`controller.go:312-320`）写 base store：

```
controller.emitMetrics → metrics.Filter → store.Write(base) → 只进 energysave_*.jsonl
                                              ↘ cacheStore 没收到 → /metrics 无 energysave
                                              ↘ PerCompWriter 没收到 → 无 snapshot_energysave.json
```

`energysave.*` 已在指标清单声明（`configs/metrics.yaml` 的 `energysave` 组件 + `features/cpugov/metrics.yaml`），故会过 `metrics.Filter`；问题纯粹是**写目标错配**——产出绕过了标准链。

> 注：部署的容器 `/etc/catmonitor/metrics.yaml`（旧 v0.3.4 清单）本无 `energysave` 组件，靠 `features/cpugov/metrics.yaml` 特性覆盖补入。源码清单已具备，无需改清单。

## 3. 目标

让 cpugov 产出的 `energysave.*` 走标准存储链 `sink`，使其同时进 `/metrics` + `snapshot_energysave.json` + jsonl，与其它 collector 产出的指标可观测性一致。**不新增 collector**（指标已声明、控制器已在产出，无需再造采集者）。

## 4. 方案（A：改写目标，无新 collector）

核心：把 `startEnergysave` 注入控制器的存储从 base `store` 改为 `sink`（链末端）。

### 4.1 改动点

1. **`cmd/catmonitor/energysave_linux.go`**
   - `startEnergysave` 第 4 参：`store *storage.JSONLStorage` → `sink collector.Storage`。
   - 函数体：`cpugov.NewController(toCpugovConfig(cfg, logger), cpufreq.Default(), sink)`（把 `sink` 注入控制器）。
   - 移除不再使用的 `storage` import（若仅用于该类型）。
2. **`cmd/catmonitor/energysave_other.go`**（非 Linux no-op stub）
   - 签名同步：`store *storage.JSONLStorage` → `sink collector.Storage`；移除 `storage` import。
3. **`cmd/catmonitor/main.go:293`**
   - `startEnergysave(ctx, cfg, scheduler, store, logger)` → `startEnergysave(ctx, cfg, scheduler, sink, logger)`。

### 4.2 不改

- `features/cpugov/controller.go`：`emitMetrics` 已写到注入的 store，逻辑不变。
- `features/cpugov/metrics.yaml`、`configs/metrics.yaml`：`energysave.*` 已声明。
- 输入路径（`refreshFromSnapshot` 读快照）、状态机、执行器、cadence（`energysave.interval`）——均不动。

### 4.3 改后数据流

```
controller.tick → emitMetrics → metrics.Filter(energysave.*) → sink.Write
  sink = PerCompWriter（snapshot 开）→ inner(cacheStore).Write + snapshot_energysave.json
                                 ↘ cacheStore.AllMetrics() → /metrics（:19320）
                                 ↘ store.Write → energysave_*.jsonl
  sink = cacheStore（snapshot 关）→ /metrics + jsonl（无 snapshot_energysave.json）
```

### 4.4 关键依据（已核验）

- `collector.Storage` 接口 = `Write([]Metric) error`（`scheduler.go:23-25`）；cpugov `Storage` 接口同形（`controller.go:34-36`）→ `sink` 满足 cpugov `Storage`，可直接注入 `NewController`。
- `PerCompWriter.Write`（`comp.go:65-72`）：先 `inner.Write`（转发 cacheStore→store），再按 `metrics[0].Component` 写 `snapshot_<comp>.json`。cpugov 产出批次同属 `Component="energysave"` → 写 `snapshot_energysave.json`。✓
- `sink` 在 `main.go:191 = cacheStore`，`:226 sink = pcw`（snapshot 开时）；`startEnergysave` 在 `:293` 调用时 `sink` 已就绪。

## 5. 门控语义

- `energysave.enabled`（配置）→ 门控控制器 goroutine 是否运行（状态机 + 执行）。
- `features` 含 `cpugov` → `energysave.*` 进指标清单 scope，过 `metrics.Filter`（可观测）。
- 两开关独立：`enabled=true` 但 `features` 不含 `cpugov` → 控制器跑但指标被过滤（不可观测）。属一致行为（与其它组件同构），文档明示即可。

## 6. cadence

不变：控制器每 `energysave.interval`（默认 3s）tick 一次，`emitMetrics` 每 tick 产出。`/metrics` 经 `cacheStore` 缓存，每 tick 覆盖。

## 7. 测试策略

1. **单元（`features/cpugov/controller_test.go`，若无则新增）**：构造控制器，`store` 注入实现 `collector.Storage` 的记录型 fake，注入 cpu/npu 数据，跑一次 `tick`，断言 fake 收到 7 个 `energysave.*` 指标且 `Component="energysave"`。验证"控制器产出确实经 store 接口外发"。
2. **链路（`features/snapshot/comp_test.go` 已覆盖 PerCompWriter 单组件批次转发）**：可加一个 `energysave` 用例显式化（批次全 `Component=energysave` → 写 `snapshot_energysave.json` + 转发 inner）。
3. **集成/手工（部署后）**：
   - `curl :19320/metrics | grep catmonitor_energysave` 返回 7 行。
   - `snapshot_energysave.json` 存在且刷新。
   - `energysave_*.jsonl` 继续有数据。

> 主线 wiring（`main.go:293` 传 `sink`）属 main 包集成，单测难覆盖，以集成/手工验证兜底。

## 8. 交付与部署

- 代码改动：3 文件（`energysave_linux.go`、`energysave_other.go`、`main.go`）。
- 编译：Go 1.23.4 + `-tags dcmi`（`make build GO=/usr/local/go-1.23.4/bin/go`）。
- 部署：Docker 容器 `catmonitor`（host 网络，`:19320`）。`docker stop → docker cp 新二进制 → docker commit`（更新 `catmonitor-npu:cpugov` tag）`→ docker start`；或用既有重部署流程。
- 容器需有 `/features/cpugov/metrics.yaml`（已补）+ `features` 含 `cpugov`（已配）。
- 验证 `/metrics` 含 `catmonitor_energysave_*` + `snapshot_energysave.json` 刷新。

## 9. 风险与边界

- `PerCompWriter.Write` 假设单组件批次（取 `metrics[0].Component`）；cpugov 产出批次全为 `energysave`，满足。若未来 `emitMetrics` 混入其它 component 会归类错误——当前不会发生，记为约束。
- 全局 `snapshot.json` 的 collector 列表来自 `DefaultRegistry.All()`，`energysave` 非注册 collector 故不在该列表（仅影响全局元数据，不影响 `snapshot_energysave.json` 与 `/metrics`）。
- `dry_run` 不变：执行器仍由 `dry_run` 门控，本改动不涉及执行路径。
- 非 Linux：`energysave_other.go` stub 签名同步，行为不变。

## 10. 不在本范围

- 输入路径改造（读快照 vs scheduler tap）。
- 把 `energysave` 做成注册 collector（方案 B，已否决：指标已在清单、控制器已在产出，无需新采集者）。
- 执行器/状态机逻辑、指标清单调整。
