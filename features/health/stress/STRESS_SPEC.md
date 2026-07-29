# health/stress 压测子特性规格（STRESS_SPEC）

## 1. 边界

`features/health/stress` 提供显式触发的 STREAM、HPL 与 HPCG 压测。它不属于周期采集器，不会由 `daemon` 或普通 `catmonitor health` 自动执行，结果也不直接折算进 0--100 健康分。运行期间的 CPU、内存、温度或 I/O 变化可能使实时健康分暂时下降。

第一版仅在 Linux 执行；Windows 必须可构建，运行时返回 `unsupported`。

第一版支持集合严格限定为 STREAM、HPL、HPCG。OSU、任意 MPI 通信测试及用户提供的 benchmark 名称均不支持，调度脚本不得保留对应执行分支。

## 2. 命令和配置

必须显式使用：

```bash
catmonitor health stress run --bench hpl,hpcg,stream -c config.yaml -o table
```

空参数只显示帮助，不允许启动默认作业。配置嵌套在 `health.stress`：

```yaml
health:
  stress:
    enabled: false
    web_enabled: false
    script_path: features/health/stress/benchmark_check.sh
    report_path: features/web/data/stress-latest.json
    default_benchmarks: [stream]
    benchmarks:
      stream: { enabled: false, timeout: 1m }
      hpl: { enabled: false, timeout: 2h }
      hpcg: { enabled: false, result_dir: "", timeout: 3m }
```

每台机器的二进制路径、环境变量和 MPI/NUMA 参数由 `benchmark_check.sh` 维护；Web 不接收这些部署参数。STREAM、HPL、HPCG 均不使用 YAML 执行路径。HPCG 的 `result_dir` 只用于 Go 在运行前后核验并读取本次结果文件，不用于执行器定位。

帮助和成功作业返回退出码 0；命令/参数错误返回 2；配置、资产、执行或作业结果错误返回 1。

## 3. 状态与报告

Manager 同时只运行一个作业，逐项串行执行并原子写入 `stress-latest.json`。初始报告无法落盘时拒绝提交；运行中发生后续持久化错误时在内存报告的 `report_error` 中显式返回。

- `healthy`：命令自行成功结束，且所需结果解析成功。
- `time_limit_reached`：STREAM、HPL 或 HPCG 达到配置窗口后被主动停止；统一属于通过，允许没有最终性能值。
- `unhealthy`：命令提前非零退出，或自行结束后应有结果解析失败。
- `cancelled`、`unavailable`、`unsupported`：分别表示取消、资产不可用和平台不支持。
- `timeout`：仅兼容旧报告，新作业不产生。

全部项目为 `healthy` 或 `time_limit_reached` 时，作业整体为 `Healthy`。不设置性能阈值。

HPL 正常完成时从标准结果行解析 `n`、`nb`、`p`、`q`、`process`、`time_seconds` 与 `gflops`。发现非零 failed residual check 或独立 `FAILED` 状态时必须判为 `unhealthy`，即使结果行中存在性能数值。

HPCG 正常完成必须同时满足：脚本退出码为 0、`result_dir` 中存在本次新增或内容/元数据发生变化的 `HPCG-Benchmark*.txt`、文件声明 `HPCG result is VALID`，并能解析 GFLOP/s 与执行时间。stdout 中即使出现数值也不能替代本次结果文件；不得用未变化的历史文件充当本次结果。

## 4. Web API

- `GET /api/health/stress/config`
- `GET /api/health/stress/latest`
- `POST /api/health/stress/runs`
- `GET /api/health/stress/runs/{id}`
- `POST /api/health/stress/runs/{id}/cancel`

Web 仅在 `health.stress.enabled`、`web_enabled` 均为 true，服务监听回环地址且请求连接来源也是回环地址时允许提交。请求只能选择已配置且通过基础资产预检的 benchmark，并可为单次作业缩短 YAML 超时，不能延长。

所有启动/取消请求必须使用 `application/json`、同源 `Origin`（浏览器请求）及 `X-CATMonitor-Action: health-stress`。服务拒绝未知 JSON 字段、跨站请求和超大请求体。脚本内的具体可执行文件与 MPI/NUMA 环境仍由脚本运行时做最终检查。

## 5. 验证

单元测试覆盖 STREAM/HPL/HPCG 解析、HPCG 新旧结果隔离、所有项目限时通过、进程组停止、报告持久化错误、CLI 退出码和 Web API 同源保护。Linux 运行测试和构建；Windows 做交叉构建。真实 benchmark 只在资产与拓扑匹配的 Linux 节点验收。

当前进程组停止保证本机 Bash、MPI 启动器及同进程组子进程被清理；多节点 MPI 的远端进程清理取决于 MPI 实现和部署脚本，未完成多节点实机验收前不应宣称支持。

## 6. 相关文档

- [README.md](README.md)：子特性入口；
- [STRESS_DESIGN.md](STRESS_DESIGN.md)：实现设计；
- [STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md)：开发、构建与通用验收；
- [STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md)：通用构建、测试和部署验收。
