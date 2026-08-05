# stress 可靠性压测特性设计（STRESS_DESIGN）

## 1. 组件边界

```text
catmonitor ─> stress/cli.Run ──────┐
                                  ├─> stress.Manager ─> benchmark_check.sh
catmonitor-web ─> stress.Register ┘          │
       │                           describe ──┤  (只读 profile / preflight)
       │                                     ├─> stress-latest.json
       └─> /stress/ + /api/stress/*          ├─> stress-history.json
                                             └─> .lock
```

`features/stress` 自己拥有配置类型、Manager、执行/解析、共享锁、HTTP Handler
及嵌入式 SPA。`features/web` 保持 daemon snapshot 的只读消费者，只额外创建
Manager 并调用 `stress.Register`；它不恢复进程内采集、Web YAML 或配置写回。
`stress` 不 import `health`、`web` 或 daemon；daemon 也不读取或执行 stress。

命令行适配位于 `features/stress/cli` 子包，负责参数、默认配置路径和 stdout/stderr
格式；`cmd/catmonitor/main.go` 只分发顶层 `stress` 命令。子包可以依赖
`internal/config` 与父级 `stress`，避免让父级领域包反向依赖主配置而形成循环依赖。

## 2. 配置所有权

CLI 的 `internal/config.Config` 拥有顶层 `Stress stress.Config`。新版 Web
继续用 `-addr` 和 `-snapshot-dir` 读取 daemon 快照，并默认通过
`platform.ConfigPath()` 加载同一份 CATMonitor 主配置。非标准部署可使用
`CATMONITOR_CONFIG` 或 `-config` 覆盖。这样既保留 Web 独立二进制
和 snapshot 只读边界，又避免两份 stress 配置漂移。

`enabled` 是功能总开关；`web_enabled` 是高风险入口的附加授权，不与前者重复：
前者关闭后 CLI/Web 均不能运行，后者关闭时 CLI 仍可显式运行但 Web 只能查看
共享结果。

## 3. 执行与解析

Manager 只把固定 benchmark 名称传给脚本。STREAM、HPL、HPCG 的绝对路径、
环境、NUMA/MPI 参数及工作目录属于节点部署脚本。Linux 将 Bash 与子进程放入
独立进程组，超时、取消及 Web 关闭时杀掉整个本地进程组。

仓库模板的 MPI 命令只使用 MPICH/Hydra 与 OpenMPI 共同支持的 `-np`，线程
变量在调用前由 Shell `export`。模板不携带 `-x`、`--map-by`、`--bind-to`、
`-mca` 或 `--allow-run-as-root` 等厂商参数。部署者必须让 launcher 与
benchmark 编译时的 MPI 实现匹配；确需绑核或传输调优时，只在节点部署副本中
加入经该 launcher 验证的参数。

### 3.1 describe 与预检

适配脚本用显式 marker 声明 describe v1。Manager 通过
`bash script describe benchmark` 获取严格 JSON，命令限时 2 秒；Web 配置
轮询使用 10 秒短缓存，并以脚本 mtime/size 变化使缓存失效。没有 marker 的
旧脚本不会被试探执行，直接生成带 `unsupported` 的兼容 profile，避免轮询
误触发任意旧脚本。

脚本检查可执行文件、目录和输入文件，计算可用文件 SHA-256，并用 launcher
`--version` 与 benchmark 动态链接信息识别 MPI 实现。明确 MPICH/OpenMPI
不匹配为 fail；ABI 静态链接或无法识别为 warn。describe 不执行
STREAM/xhpl/xhpcg，不创建结果文件，也不改变配置。

Go 将 YAML 的实际作业时限、HPCG 结果目录及脚本 SHA-256 合并进 profile，
对规范化 JSON 计算 benchmark 配置哈希，再对所选 benchmark 哈希计算 Report
聚合哈希。这样单次缩短超时、脚本或 HPL.dat 变化都会反映在结果身份中。

STREAM 从 stdout 解析 Copy、Scale、Add、Triad。HPL 校验标准结果和 residual
状态。HPCG 在运行前记录结果文件大小、修改时间和 SHA-256，运行后只接受新增
或内容/元数据发生变化的文件。三项在配置时间窗口到达且此前未报错时统一写
`time_limit_reached`，不伪造最终 GFLOP/s。

## 4. 互斥与可见性

进程内由 Manager 的 active job 互斥，进程间由 `${report_path}.lock` 的
`flock` 互斥。锁覆盖初始运行报告、全部 benchmark 和最终报告写入。第二个
入口返回 `ErrBusy` 以及共享报告中的活动作业。

报告在同目录临时写入、同步后 `Rename` 替换。无本进程活动作业时，
`Manager.Latest` 重新读取共享文件，使 Web 可看到 CLI 的运行态和最终结果。
取消权不跨进程：Web 对 CLI 作业只读。

每个作业进入最终状态后，在仍持有跨进程锁时更新历史 JSON。历史文件与最近
报告同目录，使用相同的临时文件、`fsync`、`Rename` 原子替换模式，按新到旧
最多保留 100 个报告。归档副本删除最多 16 KiB 的命令输出尾部，但保留指标、
状态、时间、来源、执行 profile 和配置哈希。历史写入失败只记录结构化错误，
不改变 benchmark 的执行结论，也不破坏仍可使用的 latest 报告。

## 5. Web 与安全

Handler 同时提供 `/stress/` 和 `/api/stress/*`。健康 SPA 只显示跳转链接，
不再包含 stress 状态、轮询或提交逻辑；两套页面可独立演进。第一阶段沿用同一
`catmonitor-web` 进程，暂不引入额外 daemon 或独立服务。

SPA 左侧显示当前/最近作业和最近 100 个最终作业，右侧切换报告详情。STREAM
仅在 Copy/Scale/Add/Triad 四个同单位指标内比较柱长；HPL/HPCG 将 GFLOP/s
显示为独立主指标，计算时间、总耗时、N/NB/P/Q/进程数显示为详情。历史趋势只
比较同一 benchmark 的同一指标，采用零基线，不承担阈值或健康状态语义。

项目选择区下方只读展示当前有效 profile：作业时限、MPI/线程资源、问题规模、
脚本参数、资产状态、MPI ABI 和配置哈希。启动确认再次摘要资源规模。历史详情
可展开查看当次 profile。前端没有配置写回、脚本编辑或任意参数输入；部署面
仍由 SSH、配置管理或镜像构建负责。

Web 写操作采用多层限制：显式双开关、Linux、回环监听、回环来源、JSON
Content-Type、自定义动作头、同源校验、64 KiB 请求上限、未知字段拒绝。API
只接受 benchmark 名称和单次缩短超时。

## 6. 生命周期

`catmonitor-web` 收到退出信号时调用 `Manager.Shutdown`，只取消本进程拥有的
作业并等待最终报告与锁释放。强制终止及多节点远端 MPI 清理由部署/cgroup 和
MPI 实现负责。

stress 在进入主干前没有发布旧的 health 子命令或 API，因此只提供
`catmonitor stress`、`/stress/` 和 `/api/stress/*`，不保留未发布预览接口。
