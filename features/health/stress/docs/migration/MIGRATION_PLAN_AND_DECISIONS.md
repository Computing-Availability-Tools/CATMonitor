# CATMonitor 健康检查压测迁移计划

> 文档类型：迁移计划与决策历史。已完成阶段用于追溯设计取舍，不作为当前
> 部署步骤。当前使用方法见 [STRESS_TEST_GUIDE.md](../../STRESS_TEST_GUIDE.md)
> 和 [`../deployment/`](../deployment/)；现行契约以
> [STRESS_SPEC.md](../../STRESS_SPEC.md) 为准。

> **v0.3.2 当前实现（2026-07-25）**：早期章节中的独立
> `features/stress`、顶层 `stress` 配置、`catmonitor stress` 和
> `/api/stress/*` 是历史方案，已由最终设计取代。当前代码位于
> `features/health/stress`，配置为 `health.stress`，CLI 为
> `catmonitor health stress run`，Web API 为
> `/api/health/stress/*`。Web 概览与 `#/stress` 页面均已接入；Linux
> STREAM/HPL/HPCG 达到配置窗口均按 `time_limit_reached` 通过；本机
> 限时/取消会停止完整 benchmark/MPI 进程组。HPCG 只接受本次新增或变化
> 的结果文件。Web 启动/取消要求 JSON、自定义操作头和浏览器同源校验。部署操作以
> [`../deployment/NODE_51_62_10_90_GUIDE.md`](../deployment/NODE_51_62_10_90_GUIDE.md)
> 为准。

更新日期：2026-07-24

## 1. 结论

旧系统的 STREAM、HPL、HPCG 压测能力应作为新的特性模块迁入，不能直接塞进现有 `features/health` 的评分逻辑。`features/health` 目前的职责是消费采集器产出的 `collector.Metric`，完成纯逻辑健康评分；它明确不应执行外部命令、读取系统文件或依赖 `cmd` / `internal/source`。

已确认采用独立模块 `features/stress`，由 CLI 显式触发。第一期仅提供一次性压测与结构化结果，不能改变现有 `catmonitor health` 的默认行为或 0--100 健康评分契约。压测仅支持 Linux；在 Windows 上命令和项目整体必须可编译，执行时对每个请求的 benchmark 返回结构化 `unsupported` 状态。

## 2. 已核查的新项目现状

### 2.1 命令行

入口为 `cmd/catmonitor/main.go`（Go 标准库 `flag`，不是 Cobra）。当前命令：

- `daemon`：默认守护进程；按配置周期采集，并周期性运行健康评分。
- `collect`：单次采集，默认 JSON，支持 `-o/--output table`。
- `health`：单次采集后健康评分，默认表格；支持 `-o/--output json`。
- `list`、`version`。

当前全局/实际解析的关键参数是 `-c/--config` 和 `-o/--output`。使用 `loadConfig()` 初始化指标目录；`health` 额外加载 `features/health/metrics.yaml` 覆盖目录。未实现 `status`，README / DESIGN 中的个别命令描述与代码不完全一致。

### 2.2 健康模块边界

`features/health` 的公共入口是 `health.NewEvaluator(health.GetScheme(...)).Evaluate([]collector.Metric)`。输出 `HealthScore`，包含总分、等级、服务类型、各组件分数与扣分项；CLI 和 Web 都直接消费该 JSON 契约。

必须保持以下约束：

- 不在 `features/health` 内加入 `exec.Command`、shell、系统探测或 benchmark 二进制执行。
- 不能让压测失败被误判成 CPU/内存等采集指标缺失；现有健康模块对缺指标采用“不报错、不扣分”的优雅降级。
- GPU/NPU 的健康分共享加速卡权重档位，网络不参与健康评分；不能因新增压测而破坏既有权重语义。

### 2.3 旧功能（迁移来源）

迁移分析 [LEGACY_MIGRATION_ANALYSIS.md](LEGACY_MIGRATION_ANALYSIS.md) 记录的首批能力为：

- STREAM：以 `OMP_NUM_THREADS` 和 `numactl --localalloc` 执行 `stream_c.exe`，解析带宽/校验结果。
- HPL：在 benchmark 目录执行 `xhpl`，依赖 MPI、OpenMP、UCX 与 `HPL.dat`。
- HPCG：执行 `xhpcg`，解析 stdout，并处理其结果文件。

本迁移按用户确认保留原始脚本调度模式：Go 只负责调用配置指定的 `benchmark_check.sh`，不重写 STREAM/HPL/HPCG 的命令行。脚本与命令参数应以 benchmark 官方提供版本为准。HPCKit/毕昇等特定部署环境的初始化不进入通用开源仓库；如目标环境确有需要，由部署方维护独立包装脚本，并通过 `script_path` 配置使用，Go 不尝试复制环境设置。

已确认原始脚本位于 `D:\project\agent_memory\CATMonitor\benchmark_check.sh`（1,526 字节）。编码时应将其纳入项目的可版本化模块资产目录，或在配置中明确其绝对部署路径与版本；不得只依赖 `agent_memory` 目录作为部署资产。

## 3. 文档 / SPEC 要求判断

项目没有发现自动检查“每项改动必须新增 SPEC”的脚本或贡献规范；Makefile 只定义 build/test/vet。但是项目的既有模式和 DESIGN/SPEC 的引用关系表明：新增独立特性必须有同目录规格文档，并同步更新顶层文档索引。

因此，本迁移在编码前应新增：

1. `features/stress/STRESS_SPEC.md`：范围、非目标、Linux / Windows 行为、CLI 契约、配置、数据模型、脚本外部依赖、超时/取消、权限与安全边界、各 benchmark 解析契约、结果文件策略、失败分类、测试与验收标准。
2. 如压测结果进入健康报告或 Web 快照，更新 `features/health/HEALTH_SPEC.md`、`DESIGN.md` 与 `SPEC.md` 的数据模型和职责说明；否则明确写为独立诊断结果。
3. 更新 `README.md` 的命令一览和示例；必要时更新配置示例。

还应修订现有 `HEALTH_SPEC.md` 中已过期的目录 / 验证命令表述：实际模块是 `features/health`，项目测试命令应与 `go test ./...` 或 `go test ./features/health` 对齐，而不是旧的 `health/` 根目录。

## 4. 推荐实施阶段

### Phase 0：确认契约（先做）

- 已确认：仅 Linux 实际执行；Windows 返回结构化 `unsupported`。实现必须保持全项目可在 Windows 编译。
- 已确认：压测仅显式触发。命令采用 `catmonitor stress run --bench stream,hpl,hpcg --config <path> -o json|table`；可选 `stress list` 展示可用 benchmark。
- 已确认：`catmonitor health` 和 daemon 不得默认触发压测；压测耗时且会施加负载。
- 待 Linux 实机确认：HPL/HPCG 的部署资产、MPI 启动参数、root 权限策略、部署侧环境初始化方式与最大可接受负载/时长。

### Phase 1：独立执行器和结果模型

- 新建 `features/stress`，定义 `Runner`、`Benchmark`、`Request`、`Result`、`Status`（healthy / failed / timeout / unavailable / unsupported / cancelled）与稳定 JSON schema。
- Linux 执行器通过 `context.Context` 和 `exec.CommandContext` 调用 `bash benchmark_check.sh <benchmark> <benchmark-dir> [threads]`，设置明确工作目录、超时、有限 stdout/stderr 捕获；不得拼接不受控 shell 片段。Windows 执行器不调用脚本，直接返回 `unsupported`。
- 配置中声明脚本路径、每个 benchmark 的资产目录、线程数、超时、是否启用和脚本所需环境；严禁从 CLI 接受任意可执行路径或任意 shell 片段。
- 各 benchmark 的输出 / 结果文件解析必须是纯函数，保留原始摘要与解析错误。仅当脚本运行成功且所需数值成功解析时状态为 `healthy`；不做性能阈值比较。

#### HPL / HPCG 结果文件契约（已确认的旧实现行为）

- HPL：脚本先 `cd` 到配置的 HPL 资产目录，再运行 `./xhpl`；该目录必须预部署 `xhpl` 和 `HPL.dat`。旧实现从输出中提取 Time 与 Gflops，不生成或修改 `HPL.dat`，也不以 `PASSED` / `FAILED` 文本作为状态依据。
- HPCG：旧实现优先从 stdout 解析；stdout 不可用时，读取 thirdparty 目录内最新的 `HPCG_RESULT_PREFIX` 前缀结果文件，从 `Final Summary` 行提取 GFLOP/s 与时间。新实现应保留该回退路径，并把“未找到 / 无法解析结果文件”明确标为 `failed`，不能继承旧实现的空消息却标成功问题。
- STREAM：脚本执行 `stream_c.exe`，从 stdout 解析四项带宽结果；无阈值比较。

### Phase 2：CLI 与报告

- 将 `stress` 加入 `cmd/catmonitor/main.go` 的分发和帮助文本，沿用已有 `-c/--config`、`-o/--output` 习惯。
- 支持 `--bench`（逗号列表）和可选 `--timeout`；未指定时仅运行配置中启用的默认集合。
- JSON 作为自动化接口，表格作为人工诊断输出；非零退出码与结构化状态的映射写入 SPEC 并测试。
- 结果先输出到 stdout；若需要持久化，使用独立目录和明确的保留策略，不能混淆当前采集 JSONL。

### Phase 3：健康 / Web 集成（需单独确认）

- 第一期开箱即用的健康规则是“脚本运行成功且数值解析成功 = healthy”；不设置任何性能阈值，不改现有 0--100 总健康分。
- 后续若要接入总体健康评分，必须另行确认规则和基线；不得把性能数值同机型无关的固定阈值直接比较。
- Web 集成需扩展 snapshot 契约及 Web 规格，保持 HTTP 层只读快照的边界。

## 5. 测试与验收

- 单元测试：STREAM/HPL/HPCG 的成功、失败、缺失字段、异常格式解析；结果 schema；超时、取消、不可执行、命令不存在。
- 集成测试：使用可控的假 `benchmark_check.sh` 和夹具结果文件，不依赖真实 MPI/GPU/NPU 或生产环境；本地当前不能运行真实 STREAM/HPL/HPCG。
- 兼容性：`go vet ./...`、`go build ./...`、`go test ./...`、Windows 交叉编译；若特性含 OS 文件，使用 build tag 或运行时 `unsupported`，保证 Windows 编译。
- 回归：现有 `catmonitor health` 和 daemon 不运行 benchmark；既有 `HealthScore` JSON 字段不变。

## 6. 实施前风险清单

- HPL/HPCG 可能需要 MPI、UCX、root 或特定 NUMA 拓扑；部署侧也可能需要专有环境初始化。这些不是 Go 依赖可自动解决的问题。
- 压测会占用 CPU、内存、NUMA 和网络资源，必须有并发互斥、超时、取消与明确告警说明，避免在生产节点误触发。
- HPL/HPCG 参数不能照搬旧主机的 `ppr` / `pe` 数值；应配置化并进行拓扑校验。
- 输出格式随 benchmark 版本变化，解析器要带版本 / 样例夹具测试。

## 7. 当前环境验证（2026-07-24）

- 已下载并校验官方 Go 1.26.5 Windows ZIP 到 `D:\project\.tools\go`；系统 MSI 安装尝试因 1603 失败，未修改系统级 Go 环境。项目私有 Go 已可用，可复现构建。
- Windows 原生 `go build ./...` 已通过；`GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` 交叉构建也已通过。
- Windows `go test ./...` 的主体包通过，但全量测试仍有既有平台问题：`internal/source/statfs` 的测试引用 Linux-only 定义而无法编译；`internal/source/sys` 与 `features/web` 的网卡驱动断言依赖 Linux `/sys` 行为而失败。迁移实现应避免新增未加平台隔离的 Linux-only 测试。
- WSL 环境最终确认：`Ubuntu-24.04`（WSL2）正在运行，能访问 `/mnt/d/project/CATMonitor`。此前受限 Codex 会话使用 `CodexSandboxOffline` 身份，不能看见交互用户的 WSL 注册；使用交互用户 `admin` 的非沙盒会话可正常访问。
- 已在 WSL 安装并校验官方 Go 1.26.5 到 `/opt/catmonitor-tools/go`。原生 Linux `go test ./...` 和 `go build ./...` 均已通过。真实 benchmark 验证仍留待部署到目标 Linux 主机后完成。
- WSL 开发 / 脚本环境已补齐：`python` -> Python 3.12.3（原先已有 `python3`，现增加 `python-is-python3` 兼容命令）、`pip3` 24.0、OpenMPI `mpirun` 4.1.6、`numactl` 2.0.18、GCC、Make、Bash、Curl、Git。`/usr/local/bin/go` 已链接至已校验的 Go 1.26.5。
- 仍不存在且不应由本地通用环境伪造的实机资产：`/opt/benchmarks/stream/stream_c.exe`、`/opt/benchmarks/hpl/xhpl` + `HPL.dat`、`/opt/benchmarks/hpcg/xhpcg`。后续单元测试使用脚本和输出夹具；真实运行验证必须在拥有这些部署资产、匹配 MPI/UCX/CPU 拓扑的 Linux 主机完成。

## 8. `status` 与压测上报的迁移映射（2026-07-24）

### 8.1 `catmonitor status` 不是压测状态

README / DESIGN 曾列出 `catmonitor status`（查看 daemon 状态），但 `cmd/catmonitor/main.go` 的命令分发只有 `daemon`、`collect`、`health`、`list`、`version`，并没有实现 `status`。本次迁移不依赖、也不补做 daemon `status`；压测状态只在显式 `catmonitor stress` 的报告中表达。

### 8.2 实际脚本与旧版上报方式

`benchmark_check.sh` 不生成 JSON、不写存储、不调用网络上报接口。它只做两件事：按 benchmark 组装并运行命令、将 stdout 和退出码交还调用者。专有环境初始化由仓库外的部署侧包装脚本负责。

旧版 `catcli check-health -l L2` 在脚本外完成汇总：L1 基础检查 -> 逐项压测（单项失败不中止）-> L1 再检查 -> stdout JSON。JSON 包含每项 benchmark 的 `status` / `message`、总体 `health_condition` 和 `health_score`。STREAM / HPL 从 stdout 取数；HPCG 优先/回退从 `HPCG-Benchmark*.txt` 取 `Final Summary` 的 GFLOP/s 和时间。

### 8.3 新项目已有输出 / 存储语义

- `catmonitor collect`：采集后把 `collector.Metric` 逐行 JSON 输出；守护进程采集器会把同一类 `Metric` 写入按 component 分组的 JSONL。
- `catmonitor health`：单次采集、`metrics.Filter`、`health.Evaluate` 后把 `HealthScore` 输出到 stdout（JSON 或 table）。
- daemon 的 `runHealthCheck`：只把 Score / Grade 写日志；虽然函数接收 storage 参数，但当前没有写入健康报告。
- `JSONLStorage` 只能写平坦的 `collector.Metric`（component/name/value/unit/labels/timestamp），不适合无损承载一个 benchmark 的状态、执行详情、stderr 摘要、多个数值和 pre/post 健康快照。

### 8.4 已确定的新压测报告契约

`catmonitor stress run` 采用与现有 `health` 一致的默认 stdout 报告模式：`-o json` 输出一个完整 `StressReport`，`-o table` 输出人工可读表格；第一期不隐式写入 JSONL，也不接入 Web。若未来要落盘，新增专门的 report 存储，而不是伪装为采集器指标。

建议稳定 JSON 形状如下（字段名在 `STRESS_SPEC.md` 定稿）：

```json
{
  "timestamp": "2026-07-24T00:00:00Z",
  "platform": "linux",
  "health_condition": "Healthy",
  "benchmarks": [
    {
      "name": "hpl",
      "status": "healthy",
      "message": "command completed and values parsed",
      "duration_ms": 1234,
      "values": {"gflops": 1234.5, "time_seconds": 12.3}
    }
  ]
}
```

- 单项状态为 `healthy` / `unhealthy` / `timeout` / `unavailable` / `unsupported` / `cancelled`。用户确认的语义是：脚本退出成功且该 benchmark 的要求数值成功解析，即 `healthy`；不比较性能阈值。
- 请求中的每项 benchmark 都要有一条结果；失败后继续执行后续项目。
- Windows 为 `unsupported`，不尝试运行脚本。Linux 找不到脚本、资产或依赖时为 `unavailable`；脚本非零退出、结果文件缺失或解析失败为 `unhealthy`。
- HPCG 不能沿用旧版“stdout 非空却没有性能数值仍标 Healthy”的缺陷：必须从 stdout 或最新 `HPCG-Benchmark*.txt` 成功解析所需 GFLOP/s / 时间后才可标 `healthy`。
- 顶层 `health_condition` 仅是本次压测作业的聚合结论：全部请求项 `healthy` 才为 `Healthy`，否则为 `Unhealthy`。它不修改、也不折算进现有 `features/health.HealthScore` 的 0--100 分。

### 8.5 与新健康逻辑的衔接

若保留旧版 L1 -> L2 -> L1 过程，`stress run` 可在报告中附加 `pre_health` / `post_health`，两者均直接复用当前 `health.NewEvaluator(...).Evaluate(metrics)` 的 `HealthScore`。但旧版“L1 分数低于 75 跳过 L2”的门槛不能直接照搬：新项目的评分项、权重和 Grade 语义已不同。该门槛需在 `STRESS_SPEC.md` 中单独确认；在确认前，显式请求压测不应因现有健康分而被隐式跳过。

## 9. Web 仪表盘适配方案（第一版已确认纳入）

### 9.1 当前 Web 状态与界面

已在 WSL Ubuntu 实测启动 `features/web`：首页 `GET /` 返回 HTTP 200、标题为“CATMonitor 设备健康度”；`GET /api/snapshot` 返回 HTTP 200，并取得健康分 91 / Excellent。Windows 到 WSL 的 localhost 转发在当前机器不可用，但 WSL 内服务本身与页面资源正常。

当前页面是浅色单页仪表盘：顶部有 CATMonitor 标识、总健康分/等级胶囊、刷新间隔输入、“应用”和“立即刷新”按钮、自动轮询开关；主体为总览和 CPU/内存/磁盘/GPU/NPU/网络详情页。前端按 snapshot 的刷新间隔轮询 `/api/snapshot`，现有 API 仅支持读取快照/采集器、修改刷新间隔和请求立即采集。

### 9.2 不应采用的实现

不要把压测伪装成一个新的常规 collector，也不要把“运行压测”塞入现有的“立即刷新”按钮：

- 压测是长任务、会占用 CPU/内存/MPI/NUMA 资源，语义与数秒一次的采集不同。
- `Snapshot` / `JSONLStorage` 仅适合快照与扁平指标，不能无损表达作业生命周期、stderr、取消、多个结果数值和结果文件来源。
- Web 当前默认监听 `:9527`（所有接口）且没有鉴权。直接暴露任意可运行压测的 POST 接口会形成高风险远程资源消耗入口。

### 9.3 推荐第一版交互

新增顶级导航项“压测”（不是 CPU 等部件卡片），页面包含：

1. **最近结果卡**：整体 `Healthy` / `Unhealthy` / `Running` / `Unsupported`，开始/结束时间、总耗时、平台、最近作业 ID。
2. **结果表**：每项 benchmark 的名称、状态、耗时、GFLOP/s / Time 或 STREAM 四项带宽、结果来源（stdout / HPCG 结果文件）、可折叠的错误摘要。
3. **运行配置只读摘要**：脚本版本/路径、资产目录、线程数、超时；页面不允许输入任意 shell、二进制路径或 MPI 参数。
4. **选择和触发**：默认使用配置中启用集合，允许勾选已配置项；点击“开始压测”后必须弹出确认框，明确展示将运行的项目、资源占用警告和不可逆的负载影响。
5. **作业状态**：运行中显示进度、当前项、开始时间和“取消”按钮；任一项失败仍展示后续项结果。

### 9.4 推荐后端边界与 API

新增 `features/stress` 的 `Manager`，Web 只负责将受控请求排队给 Manager，不同步执行 shell：

- 一次只允许一个活跃作业；重复触发返回 409 和当前作业信息。
- 运行由 `context.Context` 支持超时与取消；子进程日志有大小上限。
- Manager 是 `stress-latest.json` 的唯一写入者，使用原子写；该独立文件不与 `snapshot.json` / 采集 JSONL 混用。
- `GET /api/stress/latest`：读取最近报告（无报告时 404 / 空对象）。
- `POST /api/stress/runs`：只接受预定义 benchmark 名称列表，不接收路径、命令或环境变量；返回 202 + `job_id`。
- `GET /api/stress/runs/{id}`：返回运行状态和最终报告。
- `POST /api/stress/runs/{id}/cancel`：请求取消当前作业。

UI 轮询作业接口；完成后刷新最近结果。概览页可只显示“最近一次压测”小卡，链接到压测页，不把压测分数合并到现有 0--100 健康度。

### 9.5 必须先满足的安全开关

第一版的“运行压测”按钮仅在以下条件满足时显示并启用：

1. `stress.web_enabled: true` 显式配置；默认 `false`。
2. Web 仅绑定回环地址（建议 `127.0.0.1:9527`），或实现认证/授权后才允许非回环绑定。
3. Linux 平台、脚本存在、所选资产目录存在；不满足时页面显示原因并禁用按钮。Windows 显示 `Unsupported`，不提供运行操作。
4. 确认框必须显示“该操作会启动高负载 benchmark，可能影响业务”的警告。

这使 Web 适配成为受控运维操作，而不是把无认证仪表盘变为任意执行入口。

## 10. 第一版实施结果（2026-07-24）

已完成：

- 新增 `features/stress`：受控单作业 Manager、原子 `stress-latest.json` 报告、Linux-only 执行、Windows `unsupported`、STREAM/HPL/HPCG 解析、HPCG 最新结果文件回退、取消/超时/输出截断。
- 原始 `benchmark_check.sh` 已迁入模块；保留通用 benchmark 调度命令，移除 HPCKit/毕昇和 `BENCHMARK_PATH` 等私有环境耦合。
- 新增 `catmonitor stress run`，支持 `--bench`、`--config`、`--output json|table`，任何项目非 healthy 时以非零退出。
- 根配置和 Web 配置均增加默认关闭的 `stress` 块；默认没有压测资产时不会执行任何 benchmark。
- Web 新增“压测”导航页和受控 API：配置、最近报告、提交作业、查询作业、取消作业。按钮仅在显式启用、回环绑定和可选项可用时启用；不改变 `HealthScore`。
- 新增 `features/stress/STRESS_SPEC.md`，更新 README 和 Web 规格。

已验证（WSL Ubuntu / Go 1.26.5）：

```bash
go test ./...
go vet ./...
go build ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
```

以上全部通过。新增测试覆盖 STREAM、HPL、HPCG 解析、HPCG 结果文件回退、脚本夹具执行、报告写入及默认禁止 Web 触发。

默认配置的 CLI 冒烟验证也符合预期：`catmonitor stress run -c configs/catmonitor.yaml` 输出 `stress: stress testing is disabled` 并以非零状态结束，因此没有启动任何 benchmark。

实机前置条件仍未满足：STREAM/HPL/HPCG 二进制和 HPL.dat 未部署，HPL/HPCG 的 MPI/UCX/NUMA 参数尚未按目标 Linux 主机拓扑校准。因此当前只允许夹具测试；部署到目标主机后再将 `stress.enabled` 和所需 benchmark 的 `enabled` 打开并完成实机验收。

### 14. 状态与报告契约复核（2026-07-24）

- `features/health` 只定义 `HealthScore.Grade`（`Excellent`、`Good`、`Warning`、`Critical`），这是 0--100 硬件健康评分，未定义、也不适合作为压测作业状态。
- 压测保留专属 `stress.Status`：`pending`/`running`/`cancelled` 表示作业生命周期，其余终态表示执行或解析结论。`healthy` 的唯一含义是“命令成功退出且所需数值已解析”，不比较性能阈值。
- 不新增全局共享状态包或配置项：目前只有压测消费该语义，过早放入 `internal` 会制造并不存在的跨领域契约。CLI 与 Web 已共享同一个 `stress.Config` 类型。
- `BenchmarkResult` 保留为逐项报告：状态、时间、来源、受限输出和数值均是必要信息。`Values map[string]float64` 适合第一版解析器，但稳定对外 schema 前应补充每项数值的单位（例如 `MB/s`、`GFLOP/s`、`s`），避免 Web/API 使用者猜测单位。
- `Report.HealthCondition` 是历史兼容的聚合展示字段；新调用方应以 `Report.Status` 及逐项 `BenchmarkResult.Status` 为准，避免把它与 `HealthScore.Grade` 混用。

### 15. 压测与实时健康评分（2026-07-24）

- 压测结果不直接写入、也不按性能阈值折算进 `HealthScore`；但当前健康规则会对 CPU 利用率、CPU 负载、温度、内存使用率/饱和度、I/O wait 等实时指标扣分。因此压测运行期间健康分可能暂时下降，完成且资源恢复后会在后续采集周期回升。
- Web 的复选框不是启用配置的入口，只用于选择 YAML 中已经 `enabled: true` 的 benchmark。开始按钮只有在 `stress.enabled`、`stress.web_enabled`、回环监听和至少一个 benchmark 启用时可用。
- Manager 在提交阶段拒绝 `enabled: false` 的 benchmark，避免 API 返回“已提交”而作业随后才显示 `unavailable` 的假成功体验。

### 16. 单元与模拟测试（2026-07-24）

- `features/stress` 单元测试覆盖 STREAM/HPL 解析、HPCG 最新结果文件回退解析、Linux 假脚本执行与原子报告写入，以及禁用 benchmark 在提交阶段被拒绝。
- `features/web` 增加模拟 Web 触发测试：通过 `httptest` 提交 `POST /api/stress/runs`，使用临时脚本输出四项 STREAM 值，轮询 `GET /api/stress/runs/{id}` 直至 `healthy`，并校验 `triad_mb_s=700.4`。测试不调用真实 HPL/HPCG/STREAM、MPI 或设备资产。
- 验证命令：WSL 中 `go test -v ./features/stress ./features/web`、`go vet ./features/stress ./features/web`、`git diff --check` 均通过。

### 17. STREAM 部署适配（2026-07-24）

- 通用 STREAM 配置新增 `executable`、`numa_policy` 和可选 `threads`。`numa_policy: interleave_all` 调用 `numactl --interleave=all <path>/<executable>`；支持 `localalloc`、`none` 两种通用策略。可执行文件名禁止包含路径分隔符。
- `threads: 0` 不设置 `OMP_NUM_THREADS`，可复现部署资产本身的默认行为；只有正整数才设置该环境变量。
- 通用脚本在 benchmark/numactl 非零退出时保留非零退出码，避免原先命令替换后被 `echo` 掩盖失败的风险。
- 已用临时 `stream_omp` 与临时 `numactl` 模拟器测试 `--interleave=all` 路径，并完成 `go test -v ./features/stress ./features/web`、`go vet ./...`、Linux 构建和 Windows 交叉构建。

### 18. 部署脚本与单次超时控制（2026-07-24）

- 按用户确认，`benchmark_check.sh` 是每台机器的适配点，不增加额外包装脚本。STREAM 的可执行文件绝对路径、`numactl --interleave=all` 与 `OMP_NUM_THREADS` 都直接维护在该脚本中；YAML 不再传递 STREAM 的路径、执行器、NUMA 或线程参数。第 17 节的通用 YAML 参数方案已废弃。
- Web 支持可选 `timeout_seconds`：仅作为一次作业的超时上限，不回写 YAML。服务端逐个核验所选 benchmark，拒绝超过 YAML 配置超时的请求；路径、脚本、环境变量、NUMA/MPI 和线程数不开放给 Web。

### 19. 51.62.10.90 实机 STREAM 验收（2026-07-25）

- 实际项目目录为 `/opt/catmonitor/CATMonitor-feature-stress-benchmark-web`，`bin/catmonitor` 与 `bin/catmonitor-web` 已正常生成。
- 节点 STREAM 资产为 `/root/haoran/stream_omp`；脚本按 `OMP_NUM_THREADS=${OMP_NUM_THREADS:-32}` 和 `numactl --interleave=all` 执行。
- 用户实机执行返回 `stream: healthy`，耗时约 1117 ms，Copy/Scale/Add/Triad 均已解析。说明 CLI、脚本、numactl、结果解析和独立报告链路可用。
- HPL/HPCG 尚无同等实机验收；需先依据
  [`../deployment/NODE_51_62_10_90_GUIDE.md`](../deployment/NODE_51_62_10_90_GUIDE.md)
  完成 MPI/UCX/NUMA 拓扑、二进制与结果文件的单项验证，再将对应 YAML 项和 Web 选项启用。

### 20. STREAM/HPL/HPCG 的限时通过语义（2026-07-25）

- 三类压测统一以 YAML `timeout` 作为单次作业运行窗口；HPL 可以设置很大的 N，HPCG/STREAM 也可以运行至窗口结束。到期由 CATMonitor 主动停止不等于压测失败。
- 新终态为 `time_limit_reached`：它表示进程在窗口内没有以非零状态自行退出，CATMonitor 在配置时限到达后主动终止；整体报告保持 `status: healthy`、`health_condition: Healthy`，CLI 返回 0。
- HPL/HPCG 通常只会在完整计算结束时输出最终 `GFLOPS/Time`，STREAM 也可能尚未形成可解析的完整结果。因此 `time_limit_reached` 不要求、也不应伪造性能值；`values` 可为空，`message` 应说明“已按时限停止；通过；未产生最终性能数据”。
- Web 将该终态显示为绿色 `OK`，概览页也随整体 `Healthy` 显示；结果行仍可通过说明文字区分“完整结果通过”和“限时窗口通过”。
- 在时限前非零退出、或自行成功退出但应该输出的最终结果无法解析，才为 `unhealthy`。旧报告中的 `timeout` 仅保留为兼容读法，仍表示历史上的 `Incomplete`。

### 21. 51.62.10.90 HPL 固定适配（2026-07-25）

- 用户已在 aarch64 Kunpeng 920（96 核、2 Socket、4 NUMA、381 GiB）节点完成 HPL 2.3 手工验证；HPL 位于 `/root/haoran/hpl-2.3/bin/MyConfig/xhpl`，OpenBLAS 库位于 `/usr/local/openblas/lib`。
- 已验证运行模型为 8 个 MPI 进程、每进程 `OPENBLAS_NUM_THREADS=12`、`OMP_NUM_THREADS=12`，不使用 `--bind-to core`。N=50000、NB=256、P=4、Q=2 时结果约 150.60 秒、553.37 GFLOPS。
- 按既定部署边界，不新增 `run_hpl.sh`。HPL 可执行文件、`HPL.dat`、OpenBLAS 路径、线程数和完整 `mpirun` 参数全部维护在 `features/health/stress/benchmark_check.sh`，与 STREAM 相同；YAML 只保留 `enabled` 和最大 `timeout`，不再要求 `hpl.path`。
- Manager 调用 HPL 时只向脚本传递 `hpl`，Go 与 Web 不解释 MPI/OpenBLAS/HPL.dat。脚本运行前检查 xhpl、HPL.dat、OpenBLAS 目录和 mpirun；旧的 `--oversubscribe`、`ppr:16:node:pe=32`、UCX/MCA 参数已从 HPL 分支删除。
- HPL 正常完成后解析 `n`、`nb`、`p`、`q`、`process`、`time_seconds` 与 `gflops`。非零 failed residual check 或独立 `FAILED` 状态为 `unhealthy`；达到配置窗口仍按 `time_limit_reached` 通过。
- 本地只能用固定 stdout 和假脚本做模拟测试，真实 CATMonitor HPL 作业仍需把本分支传到该节点后用 CLI 单项验收，再开放 Web。

### 22. 51.62.10.90 HPCG 固定适配（2026-07-25）

- 用户已完成官方 HPCG 3.1 实机验证：96 MPI × 1 OpenMP、`--map-by core --bind-to core` 覆盖 96 个物理核心，每 rank 网格 32×32×32，`--rt=60`。结果文件声明 VALID，总耗时 62.2467 秒，22.1496 GFLOP/s；相比 8 MPI × 12 OpenMP 更适合该参考实现。
- HPCG 与 STREAM/HPL 使用同一部署边界：不新增 wrapper，不从 YAML 读取可执行路径。`benchmark_check.sh` 固定 `/root/haoran/hpcg-3.1/build_Kunpeng_MPI_OMP/bin/xhpcg`、`OMP_NUM_THREADS=1`、`OMP_DYNAMIC=FALSE`、96 rank、逐核绑定和网格/时长参数；正式命令不包含 `--report-bindings`。
- YAML 仅保留 `result_dir: /root/haoran/hpcg-3.1/build_Kunpeng_MPI_OMP/bin` 和 `timeout: 3m`。`result_dir` 不是执行器路径，只供 Go 在运行前后核验本次结果文件；3 分钟上限为 60 秒主要测试阶段之外的初始化、校验与汇总留出余量。
- 正常完成的严格条件为：命令返回 0、本次产生新增或内容/元数据变化的 `HPCG-Benchmark*.txt`、文件包含 `HPCG result is VALID`，并可解析 GFLOP/s 与执行时间。stdout 即使含数值也不能替代结果文件，历史未变化文件不能复用。
- Go 已有的结果快照使用文件路径、大小、修改时间和 SHA-256，比仅使用 `date +%s`/`find -newermt` 更稳健，因此不在脚本中重复实现按秒级时间查找。
- 本地模拟测试使用假脚本生成新结果文件，并覆盖 VALID 解析、无结果文件拒绝、历史文件拒绝和限时通过；真实 CATMonitor CLI 仍需传到该节点后单项验收，再开放 Web。

### 23. OSU 范围处理（2026-07-25）

- v0.3.2 health/stress 只支持 STREAM、HPL、HPCG。OSU 没有配置、Go 结果解析、状态契约、Web 选项或实机验收，因此不应只在 Shell 中留下一个不可达的半实现分支。
- 当前项目的 `features/health/stress/benchmark_check.sh` 已删除 `osu)`/`osu_alltoall`。Manager 的 benchmark 白名单继续只包含三项，并增加脚本静态回归断言。
- `agent_memory/benchmark_check.sh` 与旧迁移分析中的 OSU 内容属于迁移来源存档，不是当前部署脚本；保留用于追溯旧项目，不代表 v0.3.2 支持 OSU。
