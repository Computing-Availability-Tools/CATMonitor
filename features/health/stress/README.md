# health/stress 压测子特性

该子特性只在用户显式请求后运行 STREAM、HPL 或 HPCG，不参与周期采集，也不直接修改健康总分。

```bash
catmonitor health stress run --bench stream -c /etc/catmonitor/catmonitor.yaml -o table
```

配置位于 `health.stress`。Web 还要求 `enabled`、`web_enabled` 均为 `true`，且服务监听回环地址。执行器路径、MPI/NUMA 和环境变量由每台机器的 `benchmark_check.sh` 维护；三类 benchmark 都不从 YAML 读取执行路径，只有 HPCG 保留结果目录。STREAM、HPL、HPCG 达到配置窗口时均按计划停止并记录 `time_limit_reached`。

## 文档导航

| 文档 | 定位 |
|---|---|
| [STRESS_SPEC.md](STRESS_SPEC.md) | 功能、配置、状态、报告和 API 契约 |
| [STRESS_DESIGN.md](STRESS_DESIGN.md) | Manager、执行、解析、持久化和 Web 安全设计 |
| [STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md) | WSL、本地自动化、跨平台构建和通用验收 |
| [docs/README.md](docs/README.md) | 节点部署、验收记录和迁移历史的分类入口 |

生产部署必须从 [docs/README.md](docs/README.md) 选择与目标机器匹配的节点
指南，不得混用不同节点的 MPI、资产路径和超时配置。
