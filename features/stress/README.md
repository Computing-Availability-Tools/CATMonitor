# stress 可靠性压测特性

`features/stress` 是与 `health`、`dfee` 同级的独立特性。它只在用户显式请求后
运行 STREAM、HPL 或 HPCG，不进入 daemon 周期，也不直接修改健康总分。
CLI 参数解析与结果展示位于 `features/stress/cli` 子包，主程序只挂载 `stress` 命令。

```bash
catmonitor stress -o table
```

Web 入口为 `http://127.0.0.1:9527/stress/`。它拥有自己的嵌入式 SPA 和
`/api/stress/*` API，由新版只读 snapshot `catmonitor-web` 挂载；Web
进程不通过 shell 调用 CLI，而是与 CLI 复用 `stress.Manager`。

配置只有一份，位于 CATMonitor 主配置的顶层 `stress:`。Web 默认使用平台主配置
路径，也可用 `CATMONITOR_CONFIG` 或 `-config` 覆盖；它不复制领域
配置，也不恢复已删除的 Web YAML。CLI 与 Web 共享
`report_path` 和 Linux 文件锁，因此 Web 能读取 CLI 作业结果，且两个入口
不能同时启动压测。

`report_path` 保存运行态和最近作业；每次作业结束后还会在同目录更新
`stress-history.json`，按新到旧保留最近 100 次最终报告。Web 可切换历史作业，
并按 STREAM 带宽、HPL/HPCG GFLOP/s、时间和运行参数分别展示，避免不同单位
共用一个比例尺。

节点上的 benchmark 可执行文件、环境变量、MPI/NUMA 参数和工作目录统一维护在
`benchmark_check.sh`。生产环境应将适配后的脚本部署到源码目录之外，防止升级
覆盖；特定机器路径和实测数据不得提交到开源仓库。

适配脚本同时实现只读协议
`benchmark_check.sh describe <stream|hpl|hpcg>`。它不会启动 benchmark，
只返回实际路径、线程/MPI 规模、HPL/HPCG 问题规模、资产状态及 MPI ABI
预检 JSON。Web 在启动前展示这份 profile；作业报告和历史保存 profile、
脚本/输入资产 SHA-256 及聚合配置哈希，便于复现实机结果。旧脚本可继续运行，
但页面会提示 describe 不可用，直到部署副本合入新协议。

仓库模板的 HPL/HPCG 启动命令只使用 MPICH/Hydra 与 OpenMPI 共同支持的
`-np`，并依赖已 `export` 的线程变量。部署时应先确认 launcher 与 benchmark
使用同一种 MPI 实现，再在部署副本中增加该实现专用的绑核或通信参数。

## 文档

| 文档 | 内容 |
|---|---|
| [STRESS_SPEC.md](STRESS_SPEC.md) | 功能、配置、状态、CLI 与 API 契约 |
| [STRESS_DESIGN.md](STRESS_DESIGN.md) | 包边界、执行、互斥、持久化和 Web 设计 |
| [STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md) | 构建、新装/升级、candidate 迁移、实机验收与回滚 |
