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
管理员构建期                          节点部署期                         用户运行期
build_cpu_benchmarks.sh              generate_stress_deployment.sh      catmonitor stress
  ├─ STREAM/HPL/HPCG 源码              ├─ 绝对资产路径             ├─ 选择固定 benchmark
  ├─ GCC/MPI/OpenBLAS                  ├─ MPI/NUMA/线程 profile     ├─ 互斥、超时与取消
  ├─ 安装 runtime 资产                 ├─ describe 当前节点事实     └─ 解析与保存报告
  └─ cpu-build-manifest.json           ├─ benchmark_check.sh + YAML
                                       ├─ install_stress_runtime.sh
                                       ├─ deployment manifest
                                       └─ catmonitor stress doctor
```

`scripts/stress/build_cpu_benchmarks.sh` 只接受管理员明确提供的源码、配置、工具链和
安装位置。它在专用临时目录完成构建和短 smoke，所有选中项目验证通过后才安装；
已有目标默认拒绝覆盖，只有显式 `--force` 才替换。它不修改 `/etc`、不生成
`HPL.dat`/`hpcg.dat`、不写运行 profile，也不执行完整 HPL/HPCG 作业。

HPL 使用仓库中的 `scripts/stress/templates/Make.HPL.CATMonitor`，构建脚本只替换
ARCH、TOPdir、CC、LINKER、LAinc 和 LAlib。stock HPL 2.3 顶层 Makefile 的
`install: startup refresh build` 不表达三个 prerequisite 之间的先后关系，因此脚本
显式串行执行 `startup` 和 `refresh`，再以 `-j` 执行独立 `build` target；递归
`$(MAKE)` 通过 GNU Make jobserver 继承并发上限。HPCG 从独立 build 目录执行
`configure`，并仅对已知 `ComputeResidual.cpp` OpenMP 行做幂等精确补丁。旧版
`default(none)` pragma 缺少 `n` 时补入；当前源码在非 `default(none)` pragma
中显式列出预定共享变量 `n` 时移除，以兼容 GCC 7.3；两种补丁后的布局再次输入
时保持不变。未知源码布局直接失败，不能宽泛改写。

构建清单位于 `$(dirname output-root)/manifests/cpu-build-manifest.json`。每项记录源码、
二进制和输入配置 SHA-256、编译参数、工具链/MPI 身份、动态链接检查及 HPCG 补丁
状态。分项构建会保留其他已安装项目的可信 manifest 片段。manifest 是构建时事实；
运行期 `describe` 仍以当前文件、动态库、launcher 和节点资源为准，不用静态清单
替代实时预检。

`generate_stress_deployment.sh` 是构建与运行之间的唯一可重复组装层。它消费 CPU/NPU
构建 manifest 和管理员提供的节点 profile，把模板变量安全渲染进源码目录外的 adapter，
同时生成四项配置及部署 manifest。它不接管 asset build、容器生命周期或 benchmark
执行。`stress doctor` 再通过 Manager 读取生成结果并执行实时只读预检；Web 配置 API
复用同一判据，避免部署脚本、CLI 与页面形成三套可用性定义。

容器部署把控制面与压测数据面分开。generic/NPU 主镜像携带 CATMonitor 二进制、
基础诊断依赖和固定 CPU runner client，不携带 CPU benchmark/MPI/OpenBLAS，也不
烘焙节点 adapter。`docker-compose.stress.yml` 启动独立 CPU runner image；控制面
只能通过共享 Unix Socket 提交 `stream|hpl|hpcg` 固定名称，不能提交命令、路径、
参数或环境变量。runner 无网络、无 host PID、无 privileged；入口仅以最小 capability
初始化共享卷并切换到专用 UID，workload 启动前清空全部 capability 集合，
作业串行执行；请求取消会杀死完整 shell/MPI 进程组。runner 镜像内的 benchmark
和依赖只读，HPL/HPCG 工作目录与报告放在共享状态卷。

宿主机原生 adapter 仍是默认兼容后端。部署生成器在 container runner 模式输出
控制 adapter 与 runner-local adapter：前者只转发 CPU 请求并保留 NPU 路径，后者
包含 runner 镜像内的固定 CPU profile。CLI/Web/Manager、超时、解析、latest/history
和跨进程锁保持不变。CPU 层不含 Docker socket；只有 NPU Burn `docker_exec` 需要叠加
`docker-compose.stress-npuburn.yml`，把管理员选择的 socket 挂入控制容器。
固定 NPU Burn 镜像/容器仍是独立数据面，不进入 CATMonitor 主镜像生命周期。

NPU Burn 使用相同的“构建与运行分离”边界，但构建产物是镜像：

```text
管理员镜像构建期                         A3 节点部署期                 用户运行期
build_npu_burn_image.sh                 create_npu_burn_container.sh  catmonitor stress
  ├─ 仓库固定上游源码                     ├─ 固定管理员容器                ├─ 选择 npu_burn
  ├─ CANN/torch_npu 基础镜像              ├─ benchmark_check.sh            ├─ 互斥、超时与取消
  ├─ 可选显式兼容补丁                      ├─ 全量 identity device map       └─ CSV/SDC 校验
  ├─ CANN 环境发现与构建预检              └─ docker exec
  ├─ runtime pciutils/lspci
  ├─ wheel/metadata/import 校验
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
安装并检查 NPU Burn metadata、import、custom ops 和入口 mode。Dockerfile 将
预检、native wheel 构建、
runtime `pciutils/lspci` 供应、本地 wheel 安装和最终验证拆成独立
layer；最终 layer 用 nonce 重跑只读
预检并输出 manifest marker，而高成本 C++ 编译与安装 layer 可复用缓存。
Docker build network 默认 `default`。供应顺序为基础镜像已有 `lspci`、构建上下文的
离线 RPM/DEB 依赖闭包、基础镜像在线包管理器；离线包输入在未显式覆盖 network 时
自动选择 `none`。在线包名由 `runtime-packages.txt` 解析，版本不写死。构建脚本只把
已存在代理环境变量的名字传给 Docker，让 Docker 读取当前环境值；脚本不把值展开到
argv、日志或 manifest，Dockerfile 也不声明相应 `ARG/ENV`。离线包只进入临时 build
context，安装完成后从最终文件系统清除；构建器自动记录来源与集合哈希。正式部署不
直接 bind 宿主机 `lspci`，避免把容器绑定到宿主机动态库 ABI。
wheel 安装通过 `--no-index --no-deps --force-reinstall` 确保不访问
PyPI 且不会被基础镜像
中的同版本包跳过。它不依赖登录 shell/profile，不设置
`TORCH_DEVICE_BACKEND_AUTOLOAD=0`，也不要求 `npu-smi` 或 NPU 设备。基础镜像
不自带 HAL 时，可显式把宿主机 driver `lib64` 暂存到 builder stage；最终 stage
重新从原基础镜像开始，只复制已验证 wheel、入口和许可证，不携带宿主机驱动。
构建器只调用 image inspect/build。`create_npu_burn_container.sh` 位于独立的管理员
部署面，动态 identity-map 全部 `/dev/davinciN`、控制设备、只读 driver/tool 路径和
结果目录；设备节点 ID 与 PCI topology 生成的 NPU Burn logical ID 分开记录。
它不复制 image ENV，不替换不匹配容器。默认 restart policy 为 `unless-stopped`，
管理员可以显式覆盖；策略与 image ID、runtime、设备及挂载共同进入 profile 哈希，
并参与既有容器一致性校验。该工具不由 Manager、CLI 或 Web 调用。

镜像标签和 manifest 同时记录 bundled/override 来源、上游 repository/revision、
原始/补丁后源码及补丁哈希、profile、模板哈希、基础镜像 ID/摘要、目标镜像
ID/摘要与架构，以及实际 Ascend 环境脚本、CANN 版本、build network、runtime
package list 哈希、pciutils 来源、离线包集合与 lspci 路径/版本、wheel 文件名/
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
或 HTTP 请求。固定容器由管理员在作业之外通过受控 bootstrap 创建。
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
脚本不会被试探执行，并被直接判定为不可用，避免轮询误触发任意脚本。describe
超时、退出失败、输出无效或预检失败同样阻止作业提交；Manager 不生成猜测性的
降级 profile。

脚本检查可执行文件、目录和输入文件，计算可用文件 SHA-256，并用 launcher
`--version` 与 benchmark 动态链接信息识别 MPI 实现。明确 MPICH/OpenMPI
不匹配为 fail；ABI 静态链接或无法识别为 warn。describe 不执行
STREAM/xhpl/xhpcg/npu-burn，不创建结果文件，也不改变配置。
容器 NPU Burn 额外只读检查 runtime、容器运行态、实际镜像和容器内执行器，
并把 backend、容器/镜像、CANN、torch_npu、SoC 等记录为 profile 参数；这些
参数同样参与配置哈希，不构成可由 Web 修改的容器配置接口。
adapter 还枚举当前环境的 `/dev/davinciN` 并暴露
`device_namespace=npu_burn_logical` 与 `available_devices`。在 `docker_exec` 下，
它同时在固定容器内执行与 upstream 相同语义的 `lspci -D -d 19e5:` 过滤，根据
排序后的 PCI accelerator 数量得到 `pci_topology_devices`。设备节点 ID 与 PCI
logical ID 属于不同 namespace；adapter 分别展示两者，并要求数量一致，否则拒绝
运行，避免 upstream 缺失 `lspci` 时静默
退回固定 `range(8)`。选择值在 describe 和
正式执行前都必须属于该集合；不能使用 PyTorch device count 或 `npu-smi` Phy-ID
替代这个 namespace。越界错误作为必需 `logical_devices` 资产展示，因此 CLI 与
Web 能在负载启动前给出有效 ID 和修正提示。
native backend 只枚举宿主机 namespace；`docker_exec` 只通过 `docker exec` 枚举
固定容器 namespace，探测失败不回退宿主机。设备默认值为空，避免共享节点误压
业务设备；管理员必须显式选择已预留 logical ID，`all` 仅适用于整节点独占。
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
同一运行账户的 `$HOME/.ascend_npu_burn/output` 读取 CSV。容器镜像固定使用
`/opt/catmonitor/npuburn-home` 作为 HOME，bootstrap 将管理员选择的宿主结果目录
绑定到其默认输出目录；适配器因此仍从宿主 `NPU_BURN_OUTPUT_DIR` 校验和解析本次
CSV。当前实现不保留重新传入 `--output` 的开关，避免不同 profile 意外重新触发
上游缺陷。
`--sdc_detect` 会实例化 SDC 检测器并决定 PASS/FAIL 语义，所以适配器始终显式
传入；这同时避开上游未初始化 `args.detect` 的异常，但不是无语义的兼容 workaround，
也不需要修改 bundled upstream。
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

## 7. 测试分层

测试按故障定位边界分为四层：

| 层级 | 位置 | 验证内容 | 是否需要硬件 |
|---|---|---|---|
| Go UT/组件测试 | `features/stress/*_test.go`、`features/stress/cli/*_test.go` | 解析、状态、超时、进程组、报告、历史、锁、profile、API 安全 | 否 |
| 构建/部署 fixture | `scripts/stress/tests` | CPU 构建事务、NPU 镜像输入、固定容器、部署生成和发布审计 | 否，Docker 使用受控 fixture |
| 产品链 E2E | `tests/e2e/stress_e2e_test.sh` | 编译真实 CLI/Web，贯通配置、四项 adapter、HTTP、共享报告/历史和跨进程锁 | 否，但仅支持 Linux |
| 实机验收 | 部署侧记录 | 官方 CPU benchmark、MPI ABI、CANN/torch_npu、PCI topology、NPU SDC 和清理 | 是 |

产品链 E2E 生成临时 adapter，不调用真实 benchmark，因此可以在通用 Linux CI 中
稳定运行。它不能替代实机验收；反过来，实机跑通也不能替代解析、错误路径、HTTP
安全和并发的自动化测试。固定的上游 NPU Burn 自带测试仍保留在 vendored source
中，但普通 CATMonitor CI 不直接运行依赖特定 CANN/torch_npu/NPU 的上游测试；
镜像构建负责 wheel、安装和 import gate，真实 workload 由 A2/A3 验收负责。
