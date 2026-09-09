# CATMonitor 采集核心 User Story

> **文档定位**：采集核心（`internal/collector` + `internal/collectors` + `internal/metrics`，DESIGN.md §1-2）的用户故事清单，作为测试设计的输入需求。
>
> **配套产物**：`tests/e2e/core_testcases.xmind`（测试点）、`tests/e2e/core_testcases.xlsx`（可执行用例）。
>
> **素材来源**：由 `DESIGN.md` §1-2 反向改写为用户视角（采集核心无独立 SPEC；v0.3.6 校订：来源层 14 包计数、Chassis 跨平台编译+运行时降级），经评审定稿。与原始需求冲突时以原始需求为准。

---

## CO-US-001 采集器注册与周期调度

作为 CATMonitor 维护者，我希望采集器实现统一接口注册后由调度器自动发现并周期采集，以便新增部件零核心代码改动。

**业务规则**：
1. Collector 接口：Name/Component/Collect/Priority/DefaultInterval/DefaultEnabled；
2. `init()` 注册进 Registry，DefaultRegistry 全局唯一；
3. Scheduler 每采集器独立 goroutine，启动立即采集一次；
4. 配置可覆盖 enabled/interval；
5. Stop 优雅退出（等待在途采集完成）。

## CO-US-002 指标目录与优先级过滤

作为运维，我希望按优先级控制采集范围，以便降低无谓开销。

**业务规则**：
1. `configs/metrics.yaml` 为指标目录（name/priority/默认开关）；
2. `min_priority`（low/medium/high）预过滤，整组无目标指标时子方法跳过（AnyWanted）；
3. 未编目指标默认放行（目录漂移不丢数据）；
4. Filter 丢弃未选中指标。

## CO-US-003 feature-scope 白名单采集

作为特性开发者，我希望按 features 列表只采所需指标，以便多特性共存时互不干扰且开销最小。

**业务规则**：
1. features 非空时白名单 = 各 feature `metrics.yaml` 并集；
2. 仅白名单内且 priority ≥ min_priority 的指标被采集；
3. 多 feature 同名指标取高优先级合并（LoadFeatureOverrides）；
4. per-component cadence `C_comp = min(feature interval)` 覆盖配置 interval。

## CO-US-004 缺失依赖优雅降级

作为运维，我希望外部工具/文件缺失时对应指标不产出但不报错，以便部分监控的机器上其余指标正常。

**业务规则**：
1. 来源不可用→采集器产出空（不 error）；
2. NPU 单卡失败不影响其他卡（device 并行采集）。

## CO-US-005 跨平台编译

作为 Windows 用户，我希望同一代码库编译出 Windows 版本，以便双平台部署。

**业务规则**：
1. build tag 平台分离（`*_linux.go`/`*_windows.go`/`*_other.go`）；
2. NPU 采集与 stress 非 Linux 平台为 no-op；
3. GPU 采集双平台通用（nvidia-smi exec）。

## CO-US-006 采集器错误隔离

作为运维，我希望单个采集器错误不影响其他部件与后续周期，以便局部故障不放大。

**业务规则**：
1. 采集错误仅记日志、该周期跳过存储；
2. 下个周期自动重试；
3. 7 个采集器互不影响。
