# cpugov energysave 指标接入标准存储链 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 cpugov 产出的 `energysave.*` 指标从写 base `store` 改为写 `sink`（标准存储链末端），使其进 `/metrics` + `snapshot_energysave.json` + jsonl。

**Architecture:** 改 `startEnergysave` 注入控制器的存储：base `*storage.JSONLStorage` → `sink collector.Storage`。控制器 `emitMetrics` 逻辑不变（已单测覆盖）。无新 collector——指标已在清单声明、控制器已在产出，仅写目标错配。

**Tech Stack:** Go 1.23.4 + `-tags dcmi`（CGo/CANN），Docker 容器 `catmonitor`（host 网络 `:19320`）。

## Global Constraints

- Go 工具链 1.23.4（`/usr/local/go-1.23.4/bin/go`，不在 PATH，需 `export PATH=/usr/local/go-1.23.4/bin:$PATH`）。`go.mod` 声明 `go 1.23.4`。
- DCMI（Ascend NPU）默认开：`make build` 自动检测 `/usr/local/Ascend/driver/include/dcmi_interface_api.h` 加 `-tags dcmi`；需 `libdcmi.so`（在 `/usr/local/Ascend/driver/lib64/driver/`，cgo 指令已带 `-L`）。
- `features/cpugov/` 与 `cmd/catmonitor/energysave_linux.go` 为 `//go:build linux`；`energysave_other.go` 为 `//go:build !linux` no-op stub。两者签名必须同步。
- 代码注释用英文（团队约定）；prose 文档用中文。
- **本工作区非 git 仓库**（无 `.git`）。任务 gate 用 `go vet` + `go test` + `make build` 替代 commit；无 `git add/commit` 步骤。
- 验证命令：`make lint`=`go vet ./...`；`make test`=`go test ./...`；单包 `go test ./features/cpugov/...`；交叉编译验证非 Linux stub：`GOOS=windows go build ./cmd/catmonitor`。

## 依据（已核验，无需再查）

- `collector.Storage` 接口 = `Write([]Metric) error`（`internal/collector/scheduler.go:23-25`）；cpugov `Storage` 接口同形（`features/cpugov/controller.go:34-36`）→ `sink` 满足 cpugov `Storage`，可直接传给 `cpugov.NewController(cfg, src, sink)`。
- `PerCompWriter.Write`（`features/snapshot/comp.go:65-72`）：先 `inner.Write`（转发 cacheStore→store），再按 `metrics[0].Component` 写 `snapshot_<comp>.json`。cpugov 产出批次同属 `Component="energysave"` → 写 `snapshot_energysave.json`。
- `sink` 生命周期（`cmd/catmonitor/main.go`）：`:191 var sink collector.Storage = cacheStore`；`:226 sink = pcw`（snapshot 开时）；`startEnergysave` 在 `:293` 调用时 `sink` 已就绪。
- `exporter.CachingStorage`：`NewCachingStorage(inner collector.Storage)`、`Write(metrics)`、`AllMetrics() []collector.Metric`（`features/exporter/storage.go:22,31,46`）。`/metrics` handler 调 `cacheStore.AllMetrics()`（`features/exporter/prometheus.go:108`）。
- 控制器 emit 行为已由 `TestControllerEmitMetricsContainsExpected`（`features/cpugov/controller_test.go:171-196`）覆盖——故本计划无新单测（见 Task 2 说明）。

---

### Task 1: 基线确认（red）—— energysave.* 当前不在 /metrics

**Files:**
- Read-only 验证：`http://127.0.0.1:19320/metrics`、`/home/docker/volumes/cm-snapshot/_data/`

**Interfaces:** 无（仅观测，确立 red 基线）。

- [ ] **Step 1: 确认 /metrics 无 energysave**

```bash
curl -s -m 3 http://127.0.0.1:19320/metrics | grep -c catmonitor_energysave
```
Expected: `0`（red：当前 energysave.* 写 base store，绕过 cacheStore，故 /metrics 无）。若非 0，先排查是否已改过。

- [ ] **Step 2: 确认无 snapshot_energysave.json**

```bash
ls /home/docker/volumes/cm-snapshot/_data/snapshot_energysave.json 2>/dev/null && echo EXISTS || echo MISSING
```
Expected: `MISSING`（red：PerCompWriter 未收到 energysave 批次）。

- [ ] **Step 3: 确认 energysave_*.jsonl 仍有数据（产出本身正常，仅写错地方）**

```bash
tail -1 /home/docker/volumes/cm-data/_data/energysave_2026-08-14.jsonl
```
Expected: 一行 `{"component":"energysave",...}`（证明控制器在产出，问题在写目标）。

---

### Task 2: 改写目标——startEnergysave 注入 sink（而非 base store）

**Files:**
- Modify: `cmd/catmonitor/energysave_linux.go`（`startEnergysave` 签名 + 注入 + import）
- Modify: `cmd/catmonitor/energysave_other.go`（no-op stub 签名同步 + import）
- Modify: `cmd/catmonitor/main.go:293`（调用传 `sink`）

**Interfaces:**
- Consumes: `collector.Storage`（`Write([]collector.Metric) error`，来自 `internal/collector/scheduler.go:23`）；`sink` 变量（`main.go:191/226`）。
- Produces: `startEnergysave(ctx, cfg, scheduler, sink collector.Storage, logger)`——`sink` 经 `cpugov.NewController` 注入控制器，控制器 `emitMetrics` 写 `sink`。

- [ ] **Step 1: 改 `cmd/catmonitor/energysave_linux.go` 签名 + 注入**

把 `startEnergysave` 第 4 参 `store *storage.JSONLStorage` 改为 `sink collector.Storage`，函数体内 `NewController` 的 `store` 改 `sink`：

```go
// startEnergysave starts the cpugov controller, which reads
// snapshot_<cpu|npu>.json directly (no scheduler tap), and starts its
// control goroutine. No-op when cfg.Energysave.Enabled is false.
// energysave.* state metrics are written to sink (the storage chain end,
// e.g. PerCompWriter -> CachingStorage -> JSONLStorage) so they surface in
// /metrics + snapshot_energysave.json + jsonl like collector-produced metrics.
func startEnergysave(ctx context.Context, cfg *config.Config, scheduler *collector.Scheduler, sink collector.Storage, logger *slog.Logger) {
	if !cfg.Energysave.Enabled {
		return
	}
	if !cfg.Snapshot.Enabled {
		logger.Warn("energysave requires snapshot.enabled; cpugov will not actuate (no-op)")
	}
	ctl := cpugov.NewController(toCpugovConfig(cfg, logger), cpufreq.Default(), sink)
	energysaveCtl = ctl
	go ctl.Run(ctx)
	logger.Info("energysave controller started",
		"dry_run", cfg.Energysave.DryRun, "interval", cfg.Energysave.Interval,
		"snapshot_dir", cfg.Snapshot.Dir)
}
```

- [ ] **Step 2: 从 `energysave_linux.go` 移除不再使用的 `storage` import**

确认 `storage` 仅用于原签名后，删除该 import 行：

删除：
```go
	"github.com/Computing-Availability-Tools/CATMonitor/internal/storage"
```
（`collector` import 已存在，无需新增。）

- [ ] **Step 3: 改 `cmd/catmonitor/energysave_other.go` no-op stub 签名同步**

```go
// startEnergysave is a no-op on non-Linux: cpufreq sysfs actuation is
// Linux-only. Matches the linux signature so main.go can call it
// unconditionally.
func startEnergysave(_ context.Context, _ *config.Config, _ *collector.Scheduler, _ collector.Storage, _ *slog.Logger) {
}
```
并删除该文件 `storage` import（若仅用于原签名）：
```go
	"github.com/Computing-Availability-Tools/CATMonitor/internal/storage"
```

- [ ] **Step 4: 改 `cmd/catmonitor/main.go:293` 调用传 `sink`**

将：
```go
	startEnergysave(ctx, cfg, scheduler, store, logger)
```
改为：
```go
	startEnergysave(ctx, cfg, scheduler, sink, logger)
```
（`store` 仍被 `cacheStore := exporter.NewCachingStorage(store)` 等使用，不删 `store` 变量；仅改这一处调用实参。）

- [ ] **Step 5: go vet**

```bash
export PATH=/usr/local/go-1.23.4/bin:$PATH
go vet ./...
```
Expected: 无输出（clean）。若报 `storage` import 未用 → 回 Step 2/3 确认已删；若报 `sink` undefined → 确认改在 `runDaemon` 内 `sink` 作用域（`main.go:191` 之后）。

- [ ] **Step 6: go test（现有单测全绿）**

```bash
go test ./...
```
Expected: 全 PASS。重点关注 `features/cpugov`：`TestControllerEmitMetricsContainsExpected`、`TestControllerDownclocksAtConfirmedIdleNPUIdle`、`TestControllerDryRunNoWrites` 等仍绿（控制器逻辑未动）。

- [ ] **Step 7: make build（Linux + dcmi）**

```bash
make build
```
Expected: `build daemon (dcmi: on)` + 产出 `bin/catmonitor`。验证新二进制含 energysave + 链接 dcmi：
```bash
bin/catmonitor -h 2>&1 | grep -i energysave
ldd bin/catmonitor | grep -i dcmi
```
Expected: 一行 `energysave ...`；一行 `libdcmi.so => /usr/local/Ascend/driver/lib64/driver/libdcmi.so`。

- [ ] **Step 8: 交叉编译验证非 Linux stub（energysave_other.go 签名同步）**

```bash
GOOS=windows go build ./cmd/catmonitor
```
Expected: 成功（无错误）。证明 `energysave_other.go` 的 `_ collector.Storage` 签名与 linux 侧一致、非 Linux 路径编译通过。若失败看是否 stub 签名未同步或漏删 import。

> **说明（为何无新单测，TDD 仍成立）：** 控制器 emit 行为已由 `TestControllerEmitMetricsContainsExpected` 单测覆盖（断言 7 个 energysave.* 写到注入 store）。本变更是 main 包 wiring（把注入对象从 base store 换成 sink），控制器逻辑不变，无自然 red→green 单测。真正的 red→green 在 Task 3 集成层：`/metrics` 由无 energysave（red）→ 有（green）。

---

### Task 3: 部署 + 集成验证（green）—— energysave.* 进 /metrics

**Files:**
- 部署目标：Docker 容器 `catmonitor`（image `catmonitor-npu`，host 网络 `:19320`）
- 复用：`bin/catmonitor`（Task 2 产出）、`/features/cpugov/metrics.yaml`（容器已有）、`/etc/catmonitor/catmonitor.yaml`（容器已有 `features: [web,dfee,health,cpugov]` + `energysave.enabled:true dry_run:true`）

**Interfaces:** 无（部署 + 观测）。

- [ ] **Step 1: 停 daemon 容器**

```bash
docker stop catmonitor
```
Expected: `catmonitor`（短暂中断采集；web/dfee 只读消费快照，不受影响）。

- [ ] **Step 2: 注入新二进制到容器（停机态，避 ETXTBSY）**

```bash
docker cp bin/catmonitor catmonitor:/usr/local/bin/catmonitor
```
Expected: 无输出（成功）。

- [ ] **Step 3: 提交 patched 镜像（持久化 safety tag）**

```bash
docker commit catmonitor catmonitor-npu:cpugov
```
Expected: `sha256:...`（把新二进制烤进 `catmonitor-npu:cpugov` tag，防容器重建丢失）。

- [ ] **Step 4: 启动**

```bash
docker start catmonitor
```
Expected: `catmonitor`。

- [ ] **Step 5: 等初始化 + 看日志确认 cpugov 起来**

```bash
sleep 8
docker logs catmonitor 2>&1 | tail -15
```
Expected: 含 `energysave controller started dry_run=true`、`cpugov controller started ... cpufreq_available=true`、`exporter listening addr=:19320`、`CATMonitor daemon started`，且**无** `feature metrics override failed feature=cpugov`（容器已有 `/features/cpugov/metrics.yaml`）。

- [ ] **Step 6: green——/metrics 出现 energysave（red→green）**

```bash
curl -s -m 3 http://127.0.0.1:19320/metrics | grep catmonitor_energysave
```
Expected: 7 行（`catmonitor_energysave_cpu_state`、`cpu_idle_sample`、`npu_idle`、`downclock_active`、`actuator_ok`、`target_freq_khz`、`current_freq_khz`）。对照 Task 1 Step 1 的 `0` → 现 7 行 = green。

- [ ] **Step 7: green——snapshot_energysave.json 生成且刷新**

```bash
ls -l --time-style=full-iso /home/docker/volumes/cm-snapshot/_data/snapshot_energysave.json
```
Expected: 文件存在，mtime ≈ 当前时间（对照 Task 1 Step 2 的 MISSING → 现存在 = green）。内容抽检：
```bash
head -c 300 /home/docker/volumes/cm-snapshot/_data/snapshot_energysave.json
```
Expected: 含 `"component":"energysave"`。

- [ ] **Step 8: 健康检查 + jsonl 仍在**

```bash
curl -s -o /dev/null -w "healthy=%{http_code} ready=%{http_code}\n" -m 3 http://127.0.0.1:19320/-/healthy
tail -1 /home/docker/volumes/cm-data/_data/energysave_2026-08-14.jsonl
```
Expected: `healthy=200 ready=200`；最后一行 `{"component":"energysave",...}`。

- [ ] **Step 9: 收尾确认容器状态**

```bash
docker ps --filter name=catmonitor --format '{{.Names}}\t{{.Status}}\t{{.Image}}'
```
Expected: 三容器（catmonitor / catmonitor-web / catmonitor-dfee）均 Up，catmonitor 用 `catmonitor-npu`（可执行 `docker images catmonitor-npu` 确认 `:cpugov` tag 存在）。

---

## Self-Review（写完后自查）

1. **Spec 覆盖**：设计文档 §4.1 三处改动 → Task 2 Step 1-4 全覆盖；§4.4 依据 → 已核验段；§5 门控语义 → 部署 Step 复用既有 `features`+`energysave` 配置；§7 测试 → Task 1(red)+Task 3(green) 集成 + Task 2 现有单测；§8 部署 → Task 3；§9 风险（PerCompWriter 单组件批次）→ 已在"依据"确认 cpugov 批次同 `energysave`。无遗漏。
2. **Placeholder 扫描**：无 TBD/TODO；每步含具体命令/代码/期望。
3. **类型一致性**：`startEnergysave` 签名 `sink collector.Storage` 在 Task 2 Step 1（linux）、Step 3（other）、Step 4（main 调用）三处一致；`collector.Storage` = `Write([]collector.Metric) error` 与 cpugov `Storage` 同形（依据段）。
4. **无新单测的合理性**：已在 Task 2 末"说明"段交代（控制器 emit 已单测、本变更是 wiring、red→green 在集成层）。

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-14-cpugov-energysave-metrics-routing.md`. Two execution options:

1. **Subagent-Driven (recommended)** — 每个 Task 派一个 fresh subagent，Task 间复核，迭代快。
2. **Inline Execution** — 本会话内按 executing-plans 批量执行，带 checkpoint 复核。

选哪种？
