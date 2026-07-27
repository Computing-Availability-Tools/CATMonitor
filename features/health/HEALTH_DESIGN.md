# health 特性设计

`features/health` 包含两类职责：

- 根包负责消费采集指标并计算 0–100 健康分。
- `stress` 子包负责用户显式触发的 STREAM、HPL、HPCG 高负载作业。

两者共享 `health` 这一业务边界，但不共享状态模型。评分使用
`HealthScore`、`Grade` 和各部件扣分；压测使用独立的作业
`Status`、`Report`、`BenchmarkResult`，避免把作业生命周期误当成设备
健康等级。压测期间的资源占用可能令同期采集到的健康指标发生变化，
但压测结果不直接折算进健康总分。

配置在主程序和 Web 中都嵌套为 `health.stress`，结构类型由
`features/health/stress.Config` 定义并复用。CLI 使用
`catmonitor health stress run`，Web API 使用 `/api/health/stress/*`；
两条入口共用 `stress.Manager`、结果解析和报告格式。

详细评分契约见 [HEALTH_SPEC.md](HEALTH_SPEC.md)，压测契约与实现见
[stress/STRESS_SPEC.md](stress/STRESS_SPEC.md) 和
[stress/STRESS_DESIGN.md](stress/STRESS_DESIGN.md)。
