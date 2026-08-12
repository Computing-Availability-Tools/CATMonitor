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

### 1.1 构建、节点适配与执行分层

CPU benchmark 的源码构建不属于 Go feature 或节点运行适配器：

```text
管理员构建期                          节点部署期                  用户运行期
build_cpu_benchmarks.sh              benchmark_check.sh         catmonitor stress
  ├─ STREAM/HPL/HPCG 源码              ├─ 绝对资产路径             ├─ 选择固定 benchmark
  ├─ GCC/MPI/OpenBLAS                  ├─ MPI/NUMA/线程 profile     ├─ 互斥、超时与取消
  ├─ 安装 runtime 资产                 ├─ describe 当前节点事实     └─ 解析与保存报告
  └─ build-manifest.json               └─ execute
```

`scripts/stress/build_cpu_benchmarks.sh` 只接受管理员明确提供的源码、配置、工具链和
安装位置。它在专用临时目录完成构建和短 smoke，所有选中项目验证通过后才安装；
已有目标默认拒绝覆盖，只有显式 `--force` 才替换。它不修改 `/etc`、不生成
`HPL.dat`/`hpcg.dat`、不写运行 profile，也不执行完整 HPL/HPCG 作业。

HPL 使用仓库中的 `scripts/stress/templates/Make.HPL.CATMonitor`，构建脚本只替换
ARCH、TOPdir、CC、LINKER、LAinc 和 LAlib。HPCG 从独立 build 目录执行
`configure`，并仅对已知 `ComputeResidual.cpp` OpenMP 行做幂等精确补丁。旧版
`default(none)` pragma 缺少 `n` 时补入；当前源码在非 `default(none)` pragma
中显式列出预定共享变量 `n` 时移除，以兼容 GCC 7.3；两种补丁后的布局再次输入
时保持不变。未知源码布局直接失败，不能宽泛改写。

构建清单位于 `$(dirname output-root)/manifests/build-manifest.json`。每项记录源码、
二进制和输入配置 SHA-256、编译参数、工具链/MPI 身份、动态链接检查及 HPCG 补丁
状态。分项构建会保留其他已安装项目的可信 manifest 片段。manifest 是构建时事实；
运行期 `describe` 仍以当前文件、动态库、launcher 和节点资源为准，不用静态清单
替代实时预检。

NPU Burn 使用相同的“构建与运行分离”边界，但构建产物是镜像：

```text
管理员镜像构建期                         A3 节点部署期                 用户运行期
build_npu_burn_image.sh                 固定管理员容器                catmonitor stress
  ├─ 仓库固定上游源码                     ├─ device/volume/env            ├─ 选择 npu_burn
  ├─ CANN/torch_npu 基础镜像              ├─ benchmark_check.sh            ├─ 互斥、超时与取消
  ├─ 可选显式兼容补丁                      ├─ describe 当前 runtime         └─ CSV/SDC 校验
  ├─ CANN 环境发现与构建预检              └─ docker exec
  ├─ wheel/import/version 校验
  └─ image manifest
```

`scripts/stress/build_npu_burn_image.sh` 默认读取
`third_party/ascend_npu_burn/source`，先校验固定上游元数据和逐文件哈希，再将源码
复制到专用临时上下文后应用显式
补丁。`compat-profile=none` 不打补丁，是 A3 初次构建路径；命名 profile 只是
补丁身份，不会自动推断 SoC 或软件栈。Docker build 使用 Bash 显式发现并
source CANN 环境：显式 override 优先，其次为两个 canonical toolkit 路径，
最后仅接受唯一的 `cann-*/set_env.sh`；多版本不静默选择。在 wheel 构建前，
它验证 `libascend_hal.so` 解析及 torch、torch_npu、TBE import，然后构建、
安装并检查 NPU Burn import/version。Dockerfile 将预检、native wheel 构建、
本地 wheel 安装和最终验证拆成独立 layer；最终 layer 用 nonce 重跑只读
预检并输出 manifest marker，而高成本 C++ 编译与安装 layer 可复用缓存。
安装通过 `--no-index --no-deps --force-reinstall` 确保无网络且不会被基础镜像
中的同版本包跳过。它不依赖登录 shell/profile，不设置
`TORCH_DEVICE_BACKEND_AUTOLOAD=0`，也不要求 `npu-smi` 或 NPU 设备。基础镜像
不自带 HAL 时，可显式把宿主机 driver `lib64` 暂存到 builder stage；最终 stage
重新从原基础镜像开始，只复制已验证 wheel、入口和许可证，不携带宿主机驱动。
构建器只调用 image inspect/build，固定容器
的创建、设备、挂载和运行仍完全属于管理员部署面。

镜像标签和 manifest 同时记录 bundled/override 来源、上游 repository/revision、
原始/补丁后源码及补丁哈希、profile、模板哈希、基础镜像 ID/摘要、目标镜像
ID/摘要与架构，以及实际 Ascend 环境脚本、CANN 版本、wheel 文件名/
SHA-256/安装包位置、HAL/import 预检、构建期 driver 是否注入及其哈希。
`driver_mount_present_at_build=false` 是允许的事实，不是
失败状态。manifest 用于确认“构建了什么”，不能证明宿主机驱动、
设备健康或正式 NPU Burn 结果；这些事实必须在 A3 candidate 上由 describe 和
分级实机验收确认。

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

Manager 只把固定 benchmark 名称传给脚本。STREAM、HPL、HPCG、Ascend NPU Burn 的绝对路径、
环境、NUMA/MPI 参数及工作目录属于节点部署脚本。Linux 将 Bash 与子进程放入
独立进程组，超时、取消及 Web 关闭时杀掉整个本地进程组。

NPU Burn 不引入 Go container backend。管理员负责准备、固定和维护原生或容器
环境，节点脚本负责调用。通用模板支持直接执行宿主机程序，也支持对已经运行的
固定容器执行 `docker exec`；它只做只读 inspect/可执行性预检，不负责 pull、
create、start、stop、kill 或 rm。容器镜像、设备、挂载、环境和命令不进入 YAML
或 HTTP 请求。需要一次性 `docker run` 的节点只能在受控部署副本中固化完整命令。
本地进程组清理不能天然证明容器内 exec 进程已退出，因此容器 profile 必须由
管理员提供工具硬时限或容器侧清理机制，并在启用 Web 前完成取消/异常断开验收；
CATMonitor 不把“本地 docker 客户端已退出”误当作该部署前置条件已经满足。

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
STREAM/xhpl/xhpcg/npu-burn，不创建结果文件，也不改变配置。
容器 NPU Burn 额外只读检查 runtime、容器运行态、实际镜像和容器内执行器，
并把 backend、容器/镜像、CANN、torch_npu、SoC 等记录为 profile 参数；这些
参数同样参与配置哈希，不构成可由 Web 修改的容器配置接口。
芯片代际与 workload 也作为两个显式 profile 参数，不在 Go、Shell 或 Web 中
建立隐式映射。已验证节点可分别配置 A2 + `matmul`、A3 + `quant_matmul`；
用例存在性仍由实际安装版本和节点验收负责。

Go 将 YAML 的实际作业时限、HPCG 结果目录及脚本 SHA-256 合并进 profile，
对规范化 JSON 计算 benchmark 配置哈希，再对所选 benchmark 哈希计算 Report
聚合哈希。这样单次缩短超时、脚本或 HPL.dat 变化都会反映在结果身份中。

STREAM 从 stdout 解析 Copy、Scale、Add、Triad。HPL 校验标准结果和 residual
状态。HPCG 在运行前记录结果文件大小、修改时间和 SHA-256，运行后只接受新增
或内容/元数据发生变化的文件。三项在配置时间窗口到达且此前未报错时统一写
`time_limit_reached`，不伪造最终 GFLOP/s。

Ascend NPU Burn 由节点脚本在宿主机或管理员维护的容器中调用外部 `npu-burn`
console entry，并强制启用
`--sdc_detect`。上游进程可能在结果含 FAIL 时仍返回 0，因此脚本在命令结束后
用固定 CSV 前八列校验 `npu_burn_results.csv`，仅当所有结果行为 PASS 且错误数
为 0，且工具全局设备汇总不存在 `FAIL` 时输出规范化摘要；Go 再严格解析摘要并
保存设备数、用例数、通过/失败数、错误数和累计用例时间。摘要计数字段使用严格
无符号整数协议，时间使用非负浮点数；失败摘要仍保存计数供 CLI/Web 诊断，但
作业状态保持 `unhealthy`。脚本还比较运行前后的文件时间/大小签名，
拒绝工具退出 0 但没有更新 CSV 时误读历史 PASS 结果。
当前上游版本的自定义 `--output` 校验有缺陷，默认适配模式不传该参数，并从
同一运行账户的 `$HOME/.ascend_npu_burn/output` 读取 CSV；开关仅用于兼容后续已
验证修复的版本。
NPU Burn 的 CATMonitor 外层超时为 `unhealthy`，不能产生
`time_limit_reached`，避免把未完成的 SDC 检测误报为通过。

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
Ascend NPU Burn 使用通过用例数/总用例数作为可靠性摘要，并单独显示设备数、
失败数、错误数和累计用例时间，不与 GFLOP/s 或 STREAM 带宽比较。后端只写新
协议 key；SPA 在读取边界兼容旧历史报告 key，不把旧别名重新写回报告。

项目选择区下方只读展示当前有效 profile：作业时限、MPI/线程资源、问题规模、
脚本参数、资产状态、MPI ABI 和配置哈希。启动确认再次摘要资源规模。历史详情
可展开查看当次 profile。前端没有配置写回、脚本编辑或任意参数输入；部署面
仍由 SSH、配置管理或镜像构建负责。

配置 API 对禁用项只返回禁用原因，不调用 describe。对已启用但预检失败的项，
Manager 将失败资产、路径及消息汇总到 availability message；SPA 在禁用卡片中
直接显示该消息，并为 NPU Burn 展示 backend、容器、镜像、CANN、torch_npu 和
SoC 摘要。资产详情不依赖鼠标悬停。

Web 写操作采用多层限制：显式双开关、Linux、回环监听、回环来源、JSON
Content-Type、自定义动作头、同源校验、64 KiB 请求上限、未知字段拒绝。API
只接受 benchmark 名称和单次缩短超时。

## 6. 生命周期

`catmonitor-web` 收到退出信号时调用 `Manager.Shutdown`，只取消本进程拥有的
作业并等待最终报告与锁释放。强制终止及多节点远端 MPI 清理由部署/cgroup 和
MPI 实现负责。

stress 在进入主干前没有发布旧的 health 子命令或 API，因此只提供
`catmonitor stress`、`/stress/` 和 `/api/stress/*`，不保留未发布预览接口。
