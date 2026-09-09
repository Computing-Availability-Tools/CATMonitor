# CATMonitor exporter 特性 User Story

> **文档定位**：exporter 特性（`features/exporter/`）的用户故事清单，作为测试设计的输入需求。
>
> **配套产物**：`tests/e2e/exporter_testcases.xmind`（测试点）、`tests/e2e/exporter_testcases.xlsx`（可执行用例）。
>
> **素材来源**：由 `features/exporter/exporter_SPEC.md`（v1.0）反向改写为用户视角，经评审定稿。SPEC 中端口写作 `:9100`，实际代码为 `:19320`（v0.3.5 端口统一），本文以 **19320** 为准。与原始需求冲突时以原始需求为准。

---

## EX-US-001 Prometheus 拉取全部采集指标

作为监控系统的维护者，我希望通过 Prometheus 协议拉取 CATMonitor 全部指标，以便长期存储、告警与 Grafana 可视化。

**业务规则**：
1. GET `/metrics` 输出 `text/plain; version=0.0.4`，daemon 端口 `:19320`；
2. 指标名 `catmonitor_{component}_{name}`，特殊字符（`/`、`-`、`.`）替换为 `_`；
3. 类型判定：名以 `_time`/`_total` 结尾为 counter，其余 gauge；
4. 每组一次 HELP+TYPE（HELP 文本为 `component/name` 形式，如 `cpu/usage`），同组多数据行按标签排序；
5. 标签 key/value 做引号转义；
6. 空缓存返回 200+空体。

## EX-US-002 指标缓存语义

作为 daemon 维护者，我希望各组件最新一批指标独立缓存，以便多个采集器不同间隔写入互不覆盖。

**业务规则**：
1. 按 component 分组缓存，新 Write 覆盖该组件旧值；
2. 不同组件互不覆盖；
3. AllMetrics 返回全部组件最新值；
4. Ready=缓存非空。

## EX-US-003 健康探针

作为部署编排系统（如 K8s）的使用者，我希望探查 exporter 的存活与就绪状态，以便编排系统正确管理容器。

**业务规则**：
1. `/-/healthy` 恒 200；
2. `/-/ready` 缓存非空 200、否则 503。

## EX-US-004 零侵入单进程集成

作为 CATMonitor 维护者，我希望 exporter 以 Storage 插件形态复用采集管道，以便不重复采集、不加进程。

**业务规则**：
1. CachingStorage 包装 JSONLStorage，一次 Write 同时落盘+缓存；
2. Prometheus 拉取与 Scheduler 采集并发安全（读写锁）；
3. 指标范围=metrics.Filter 后的 High/Medium 集（与 JSONL 落盘一致）。

## EX-US-005 采集与拉取节奏解耦

作为监控数据的使用者，我需要理解缓存值的时间语义，以便正确书写 PromQL。

**业务规则**：
1. 缓存为各组件最近一次采集值（非全局同一时刻快照）；
2. counter 重启重置由 Prometheus rate() 自动处理；
3. 当前无 TLS/认证（需经反向代理加固）。
