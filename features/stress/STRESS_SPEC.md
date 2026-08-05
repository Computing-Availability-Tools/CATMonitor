# stress 可靠性压测特性规格（STRESS_SPEC）

## 1. 范围与边界

`features/stress` 提供显式触发的 STREAM、HPL 和 HPCG 压测。它是顶层 feature：

- 不属于 `features/health`，不复用健康评分状态；
- 不进入 `catmonitor daemon` 周期；
- 不设置性能阈值，只判断执行/结果协议是否成功；
- 压测结果不直接计入 0–100 健康分；
- 压测产生的资源占用可能让同期采集到的健康分暂时下降。

第一版仅在 Linux 执行。Windows 必须可构建，执行时返回 `unsupported`。OSU、
任意 benchmark 名称及多节点 MPI 实机能力均不在第一版支持范围。

## 2. CLI 与配置

规范命令为：

```bash
catmonitor stress -o table
```

无参数时从主配置读取 `default_benchmarks` 并启动作业；`--help` 只显示帮助。
成功或帮助返回 0，参数错误返回 2，
配置、资产、执行或结果错误返回 1。`-o json` 回显完整报告，`-o table`
将状态映射为 `OK` 等表格标签并把各数值拆成独立行。
命令行适配实现位于 `features/stress/cli`；`cmd/catmonitor` 只负责顶层命令分发。

唯一领域配置位于 CATMonitor 主配置顶层：

```yaml
stress:
  enabled: true
  web_enabled: false
  script_path: /etc/catmonitor/benchmark_check.sh
  report_path: /var/lib/catmonitor/stress-latest.json
  default_benchmarks: [stream]
  benchmarks:
    stream: { enabled: true, timeout: 1m }
    hpl: { enabled: true, timeout: 10m }
    hpcg:
      enabled: true
      result_dir: /absolute/path/to/hpcg/results
      timeout: 3m
```

`enabled` 控制整个特性；`web_enabled` 仅授权 Web 发起高负载作业，CLI 不依赖
它。CLI 与新版只读 Web 都从平台路径加载同一份 CATMonitor 主配置；显式覆盖项
分别为 CLI 的 `-c/--config` 和 Web 的 `-config`，未指定时依次读取
`CATMONITOR_CONFIG` 环境变量与平台默认路径。Web
不复制 `stress:`，也不恢复已经移除的 Web 专用 YAML 配置。

YAML 不接收 benchmark 可执行路径。具体执行器、环境变量、MPI/NUMA 参数和
工作目录由节点 `benchmark_check.sh` 维护。HPCG 的 `result_dir` 仅供 Go
核验本次结果文件，不用于定位可执行文件。

仓库内脚本模板必须能够直接适配单节点 MPICH/Hydra 或 OpenMPI：环境变量由
Shell `export`，MPI 启动仅使用两者共同支持的 `-np`。模板不得硬编码 `-x`、
`--map-by`、`--bind-to`、`-mca`、`--allow-run-as-root` 等厂商专用参数。
HPL/HPCG 的 launcher 必须分别与对应二进制的 MPI ABI 匹配；厂商绑核和传输
参数只能保留在完成实机验证的部署副本中。

节点脚本必须声明 `CATMONITOR_STRESS_DESCRIBE_PROTOCOL=1`，并实现：

```bash
benchmark_check.sh describe stream
benchmark_check.sh describe hpl
benchmark_check.sh describe hpcg
```

命令必须无 benchmark 副作用，只向 stdout 输出一个协议版本为 1 的 JSON
对象。对象包含实际参数、资源规模、必需资产、MPI launcher 实现、二进制 ABI
识别结果和总预检状态。资产缺失或明确 ABI 不匹配为 `fail`；静态链接、厂商
MPI 等无法可靠识别的情况为 `warn`，不得误判为失败。CATMonitor 对 JSON
字段、版本、benchmark 名和状态做严格校验。未声明协议的旧脚本使用基础预检
兼容运行，但必须暴露 `unsupported` 警告。

## 3. 作业、状态和报告

Manager 同时只运行一个作业，所选项目按顺序执行。Linux 使用
`${report_path}.lock` 的非阻塞内核文件锁，保证不同 CLI/Web 进程互斥；锁随
进程退出释放。Web 每次读取最近报告时重新读取共享文件，因此可观察 CLI 作业，
但只能取消本 Web 进程启动的作业。

状态语义：

- `healthy`：命令自行成功结束，且必需结果解析成功；
- `time_limit_reached`：配置窗口到达后按计划停止，属于通过，允许无性能值；
- `unhealthy`：命令提前失败，或正常退出后结果协议不完整；
- `cancelled`：用户/服务关闭主动取消；
- `unavailable`：配置或资产不可用；
- `unsupported`：平台不支持；
- `timeout`：仅兼容旧报告，新作业不产生。

全部 benchmark 为 `healthy` 或 `time_limit_reached` 时，作业整体为
`healthy`。运行态和最近报告必须原子写入 `report_path`，包含 `job_id`、
`initiator`、时间、状态及逐项 `BenchmarkResult`。初始报告无法落盘时拒绝
启动；后续落盘失败通过 `report_error` 暴露。

每个 `BenchmarkResult` 必须保存启动前取得的 `profile`。profile 包含本次
实际超时、脚本 SHA-256、输入资产 SHA-256、资源规模和
`configuration_sha256`；Report 另保存所选 profile 的稳定聚合哈希。单次
缩短超时必须产生不同配置哈希。profile 是结果追溯快照，不参与性能阈值判断。

作业进入最终状态后，还必须写入与 `report_path` 同目录的历史文件：默认
`stress-latest.json` 对应 `stress-history.json`。历史按开始时间倒序，最多
保留 100 个完整作业报告，并删除每项的命令输出尾部以限制文件体积；状态、
时间、来源和性能值必须保留。历史是展示能力，不参与作业互斥或健康判定。

HPL 正常完成时解析标准结果行中的 N、NB、P、Q、进程数、时间和 GFLOP/s，
发现 residual failure 或独立 `FAILED` 必须失败。HPCG 正常完成必须找到本次
新增或发生变化的 `HPCG-Benchmark*.txt`，文件声明结果 VALID，并能解析
GFLOP/s 和执行时间；不得使用 stdout 或历史未变化文件替代。

## 4. Web 契约

独立页面：

```text
/stress/
```

规范 API：

- `GET /api/stress/config`
- `GET /api/stress/latest`
- `GET /api/stress/history?limit=20`
- `POST /api/stress/runs`
- `GET /api/stress/runs/{id}`
- `POST /api/stress/runs/{id}/cancel`

Web 提交要求 Linux、`stress.enabled=true`、`stress.web_enabled=true`、非空 `report_path`、
服务监听回环地址且请求来自回环连接。启动/取消还必须使用
`application/json`、同源 `Origin` 和 `X-CATMonitor-Action: stress`。

Web 只能选择 YAML 已启用且通过预检的项目，可为单次作业缩短超时，不能延长，
也不能提交脚本、路径、环境或 MPI 参数。启动前必须只读展示 describe 返回的
实际参数、资源规模、资产状态、MPI ABI 结果和配置哈希。`warn` 允许用户在
确认信息后运行，`fail` 禁止提交。第一版不提供脚本编辑、路径编辑、任意参数
编辑或配置写回接口；管理员命名 profile 留作后续评估。

结果页面必须按指标语义展示：STREAM 的四项 MB/s 可在同一比例尺比较；
HPL/HPCG 的 GFLOP/s 分别作为主指标，不得与问题规模、进程数或秒数混合归一化，
也不得直接比较 HPL 与 HPCG。时间和运行参数使用独立详情区域。同一 benchmark
存在至少两次历史性能值时可显示零基线趋势，但趋势不改变通过/失败状态。

## 5. 验证要求

自动化测试必须覆盖解析、按时限通过、取消、进程组清理、报告原子写入与错误、
历史上限/排序/输出裁剪、防御性复制、跨进程锁、共享报告刷新、describe
无副作用/严格 JSON/超时/旧版降级、资产和 MPI ABI 预检、profile 哈希持久化、
Web 安全策略、CLI 退出码及独立 SPA 资源。Linux 执行单元测试、竞态检查和
构建；Windows 交叉构建。真实性能只在资产与拓扑匹配的 Linux 节点验收。
