# health/stress 压测子特性

该子特性只在用户显式请求后运行 STREAM、HPL 或 HPCG，不参与周期采集，也不直接修改健康总分。

```bash
catmonitor health stress run --bench stream -c /etc/catmonitor/catmonitor.yaml -o table
```

配置位于 `health.stress`。Web 还要求 `enabled`、`web_enabled` 均为 `true`，且服务监听回环地址。执行器路径、MPI/NUMA 和环境变量由每台机器的 `benchmark_check.sh` 维护；三类 benchmark 都不从 YAML 读取执行路径，只有 HPCG 保留结果目录。STREAM、HPL、HPCG 达到配置窗口时均按计划停止并记录 `time_limit_reached`。

详细契约见 [STRESS_SPEC.md](STRESS_SPEC.md)，实现设计见
[STRESS_DESIGN.md](STRESS_DESIGN.md)，完整的 WSL、跨平台构建、Web 预览
和 Linux 实机验收步骤见 [STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md)。
