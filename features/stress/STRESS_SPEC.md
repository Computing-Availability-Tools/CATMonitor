# CATMonitor 压测模块规格说明书（STRESS_SPEC）

## 1. 目标与边界

`features/stress` 提供显式触发的 STREAM、HPL 与 HPCG 压测作业。它不属于周期采集器，benchmark 结果不会直接写入或折算进 `features/health` 的 0--100 分，也不会由 `daemon` 或 `catmonitor health` 自动执行。压测本身会影响 CPU/内存利用率、负载、温度和 I/O 等实时采集指标，因此运行期间健康分可能暂时下降。

第一版只在 Linux 执行。Windows 构建必须通过；若在 Windows 执行，结果为 `unsupported`，不得调用 shell 或 benchmark 二进制。

## 2. 执行模型

- CLI：`catmonitor stress run [--bench hpl,hpcg,stream] [-c config.yaml] [-o json|table]`。
- Web：仅在 `stress.enabled: true`、`stress.web_enabled: true`、Web 绑定回环地址且资产齐全时允许提交作业。
- Manager 同时只运行一个作业；每个 benchmark 依次执行，单项失败不阻断后续项。
- Go 使用 `exec.CommandContext("bash", script, ...)` 调用受控的 `benchmark_check.sh`。Web/API 不接受命令、路径、环境变量或 MPI 参数。
- 每项有超时和取消上下文；stdout/stderr 摘要限制为 16 KiB。

通用脚本仅保留 benchmark 的调度命令；不包含 HPCKit、毕昇或任何厂商/部署环境的初始化逻辑。目标环境如需初始化，应由部署方维护独立包装脚本，并将 `script_path` 指向该脚本；该脚本不进入通用仓库。

## 3. 配置

```yaml
stress:
  enabled: false
  web_enabled: false
  script_path: features/stress/benchmark_check.sh
  report_path: features/web/data/stress-latest.json
  default_benchmarks: [hpl, hpcg, stream]
  benchmarks:
    # The per-host benchmark_check.sh owns the binary path and environment.
    stream: { enabled: false, timeout: 30m }
    hpl:    { enabled: false, path: /opt/benchmarks/hpl, timeout: 2h }
    hpcg:   { enabled: false, path: /opt/benchmarks/hpcg, result_dir: features/stress, timeout: 2h }
```

部署者必须按目标机器的 MPI、UCX、NUMA 与 CPU 拓扑校准脚本中的 HPL/HPCG 参数；不得把旧硬件的 `ppr` / `pe` 数字视为通用默认值。

STREAM 的可执行文件绝对路径、NUMA 命令和环境变量都在每台机器的 `benchmark_check.sh` 中维护；配置文件不传递这些部署参数。当前适配使用 `numactl --interleave=all /root/haoran/stream_omp`，部署到其他主机时应在该脚本中按目标资产修改。

## 4. 报告契约

Manager 原子写入 `stress-latest.json`，其中包含作业 ID、时间、平台、总体 `health_condition` 与逐项结果。逐项状态为：`pending`、`running`、`healthy`、`unhealthy`、`timeout`、`unavailable`、`unsupported`、`cancelled`。

“健康”只表示脚本退出成功且所需数值成功解析；不比较性能阈值。

- STREAM：必须从 stdout 解析 Copy、Scale、Add、Triad 四项 MB/s。
- HPL：必须从 stdout 的结果行解析 Time 与 Gflops。
- HPCG：必须从 stdout 或最新 `HPCG-Benchmark*.txt` 解析有效的 GFLOP/s 与执行时间。不能把旧版“stdout 非空但没有数值仍显示成功”的行为迁入。

整体 `health_condition` 仅代表本次压测作业：全部项目 `healthy` 时为 `Healthy`，否则为 `Unhealthy`；它不修改 `HealthScore`。

## 5. Web API

- `GET /api/stress/config`：公开可展示的 benchmark 名称、启用状态和 Web 运行能力；不暴露路径或命令。
- `GET /api/stress/latest`：最近报告。
- `POST /api/stress/runs`：接受 `{"benchmarks":["hpl","stream"],"timeout_seconds":1800}`，返回 202 和作业报告。`timeout_seconds` 可省略；提供时只对本次作业生效，且不得超过任一所选 benchmark 的 YAML 超时。
- `GET /api/stress/runs/{id}`：获取当前/最近作业。
- `POST /api/stress/runs/{id}/cancel`：请求取消。

## 6. 安全要求

压测是高负载运维操作。Web 默认关闭，且只有回环绑定才允许触发；非回环部署需要在启用按钮前补充认证/授权。页面必须显示资源占用警告并要求用户确认。报告文件和快照/JSONL 分离，HTTP 层只能读取报告或提交固定配置的作业。

## 7. 验证

- 单元测试覆盖三类输出解析、HPCG 结果文件回退、脚本夹具执行和原子报告写入。
- 在 WSL Linux 执行 `go test ./...`、`go build ./...`。
- Windows 交叉编译必须通过；真实 STREAM/HPL/HPCG 只在部署资产与拓扑匹配的 Linux 主机验证。
