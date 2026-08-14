# stress 可靠性压测特性规格（STRESS_SPEC）

## 1. 范围与边界

`features/stress` 提供显式触发的 STREAM、HPL、HPCG 和 Ascend NPU Burn 压测。它是顶层 feature：

- 不属于 `features/health`，不复用健康评分状态；
- 不进入 `catmonitor daemon` 周期；
- 不设置性能阈值，只判断执行/结果协议是否成功；
- 压测结果不直接计入 0–100 健康分；
- 压测产生的资源占用可能让同期采集到的健康分暂时下降。

第一版仅在 Linux 执行。Windows 必须可构建，执行时返回 `unsupported`。OSU、
任意 benchmark 名称及多节点 MPI 实机能力均不在第一版支持范围。

### 1.1 CPU 资产构建契约

仓库必须提供独立的 Linux 管理员构建入口
`scripts/stress/build_cpu_benchmarks.sh`。它支持从彼此无关的任意可读路径接收
STREAM 源文件、HPL/HPCG 源码 tar 包、`HPL.dat` 和 `hpcg.dat`，并支持显式
指定 output/build root、C/C++/MPI 工具链、OpenBLAS include/lib、并发数、
STREAM 编译规模、`--only`、`--skip` 和 `--force`。

默认安装根为 `/opt/catmonitor/benchmarks/runtime`，默认临时构建父目录为
`/var/tmp/catmonitor-stress-build`，默认并发为 `min(nproc, 16)`；STREAM 默认
`STREAM_ARRAY_SIZE=80000000`、`NTIMES=10`，但都必须允许覆盖。线程数、MPI
进程数、HPCG 网格/时长等运行 profile 不得进入构建参数。

构建入口必须满足：

- 不安装系统软件，不修改 CATMonitor YAML 或部署脚本，不构建容器；
- 不自动生成或调优 `HPL.dat`/`hpcg.dat`，只复制并计算哈希；
- HPL 从全新解压目录直接构建，首次构建前不运行 `make clean`；
- HPL 顶层 `startup`、`refresh` 必须按顺序串行完成，只有其后的独立 `build`
  target 可以使用请求的并发数；不得对声明为 `install: startup refresh build` 的
  顶层入口直接使用 `make -j`；
- HPCG 从独立 build 目录 configure，只对已知 OpenMP 源码布局做幂等精确补丁：
  旧 `default(none)` 布局缺少 `n` 时补入，当前非 `default(none)` 布局显式列出
  预定共享变量 `n` 时移除；两种已兼容布局不重复修改；
- STREAM 完成短实际 smoke；HPL/HPCG 只完成二进制及动态依赖检查，不运行完整压测；
- 默认拒绝覆盖已有选中资产，显式 `--force` 方可替换；
- 生成 schema 化 `build-manifest.json`，记录架构、工具链/MPI 输出、源码/配置/
  二进制 SHA-256、编译参数、动态依赖检查和补丁状态；新 manifest 的
  `schema_version` 必须是正 JSON 整数。

构建清单不能代替 `benchmark_check.sh describe`：前者是构建时事实，后者必须继续
报告当前节点上的实际资产、ABI、资源规模和运行 profile。

### 1.2 NPU Burn 镜像构建契约

仓库必须提供独立的 Linux 管理员入口
`scripts/stress/build_npu_burn_image.sh`，从仓库内固定的 MindCluster
AscendNPUBurn 上游源码和管理员已审批、已拉取或加载到本地的 CANN/torch_npu
基础镜像构建目标镜像。基础镜像已包含 `lspci` 时，标准构建只要求
`--base-image` 和 `--image`；
`--source` 与 `--source-metadata` 只作为上游升级、开发和兼容性验证的显式覆盖
入口。构建器还必须支持 `--docker-bin`、`--compat-profile`、可重复的 `--patch`、
`--build-root`、`--manifest`、`--force`、可选的 `--ascend-env-script`、显式
`--build-network`、可重复的 `--pciutils-package`，以及只进入 builder stage 的可选
`--build-driver-lib-dir`。

内置源码必须位于 `third_party/ascend_npu_burn/source`，保留上游许可证，并用
机器可读 `UPSTREAM`、审计说明和逐文件 SHA-256 清单固定 repository、revision、
Git tree、归档哈希及许可证。源码必须与所记录上游修订版一致，CATMonitor 的兼容
修改不得直接混入该目录。CANN、PyTorch/torch_npu、驱动、基础镜像、wheel、构建
产物和运行结果不得随该第三方源码目录分发。

首个 A3 候选必须使用 `--compat-profile none`，不能默认继承 A2 兼容修改。
`none` 不接受补丁；任何其他安全命名 profile 必须同时提供至少一个经过审计的
补丁。补丁只能应用到隔离的源码快照，不能修改调用者的原始源码目录。仓库不
内置未经 A3 实机失败证明所必需的 A3 专用补丁。

仓库内置的 `a2-cann83.patch` 仅适用于已实机验证的 Ascend 910B4（A2）、
CANN 8.3.RC2、torch 2.8 和 torch_npu 2.8 组合；使用时必须显式选择
`--compat-profile a2-cann83`。该补丁不得隐式应用到 A3/A5。

基础镜像必须包含构建所需的 CANN toolkit/devlib、PyTorch、torch_npu 和 TBE。
构建必须使用 Bash 显式 source 已发现的 CANN 环境，不得依赖登录 shell、
profile 或默认关闭 torch backend autoload。发现顺序为：显式 override、
`ascend-toolkit/set_env.sh`、`ascend-toolkit/latest/bin/setenv.bash`、唯一的
`cann-*/set_env.sh`。多个 versioned 路径必须拒绝并要求显式 override。

最终 runtime image 必须包含 `pciutils/lspci`。该依赖必须来自基础镜像已有内容、
构建上下文中兼容的离线 RPM/DEB 依赖闭包，或正常构建网络可用时的基础镜像包
管理器安装，不得把宿主机 `/usr/bin/lspci` 单独挂载或复制进镜像。构建必须
用 `command -v lspci` 和不会访问 NPU 的版本查询确认最终镜像依赖，并将路径、版本
与构建网络记录进 manifest。镜像构建只允许完成系统依赖安装、HAL/import 预检、
源码构建、wheel 安装、安装包元数据、NPU Burn import、custom ops import 和入口文件
可执行性检查；不得调用依赖 NUMA/NPU 拓扑的
运行时 CLI，不得映射 NPU 设备、创建或运行容器，也不得
执行 NPU 负载。自带 HAL 的基础镜像不要求宿主机 driver、`npu-smi` 或设备存在。
若基础镜像只在挂载宿主机 driver 后才能 import torch_npu，管理员可显式提供
`--build-driver-lib-dir`；构建器必须使用多阶段构建，只将其放入 builder stage，
不得复制到最终运行镜像，也不得借此映射 NPU 设备。
管理员仍负责基础镜像与宿主机驱动/CANN ABI 的匹配，以及后续固定
容器的 device、volume、env 和生命周期。

构建器必须：

- 默认拒绝覆盖已有目标镜像或 manifest，显式 `--force` 方可替换；
- 校验内置来源元数据 schema、逐文件 SHA-256、上游必需文件、LF 脚本、无符号
  链接的源码输入、Docker daemon 以及基础镜像已在本地存在；
- 在 wheel 构建前确认 `libascend_hal.so` 可解析，torch、torch_npu 和 TBE
  可 import；`npu-smi` 的警告不得在 Python 返回码为 0 时被判为失败；
- 仓库必须提供 LF、无重复且机器可读的 `docker/stress/npu/runtime-packages.txt`，
  至少声明 `pciutils`；Dockerfile 的在线安装必须读取该清单，不写死发行版版本号；
- Docker build network 默认为 `default`。基础镜像已有 `lspci` 时不得重复安装；
  缺失时按清单使用基础镜像包管理器安装。传入任一 `--pciutils-package` 且未显式设置
  network 时自动选择 `none`；隔离节点必须提供同发行版、同架构的 RPM/DEB 完整依赖
  闭包。混用 RPM/DEB、符号链接、缺失依赖或安装后没有可执行 `lspci` 均必须失败；
- 构建网络不是 `none` 时，构建器必须把宿主机已经设置且非空的 `HTTP_PROXY`、
  `HTTPS_PROXY`、`NO_PROXY` 及小写形式按 Docker 预定义 build arg 的变量名转发。
  不得输出或校验代理值，不得把值放入命令行参数、Git、CATMonitor YAML、manifest、
  Dockerfile `ARG/ENV` 或最终镜像；日志最多报告某类代理“已配置”；
- NPU Burn wheel 必须继续用
  `--no-index --no-deps --force-reinstall`
  安装，禁止访问 PyPI 或因基础镜像已安装同版本而跳过；
- 分离预检、wheel 构建、wheel 安装与 package 验证 layer，使
  native wheel 在仅最终验证变更时可复用缓存；
- 在镜像标签中记录来源类型、上游 repository/revision、原始/补丁后源码 SHA-256
  和兼容 profile，并在构建后回读校验；
- 原子生成 schema 化 manifest，记录源码、补丁、Dockerfile/entrypoint/
  entrypoint validator/Ascend helper、Docker 版本、基础/目标镜像身份、OS/架构、所选环境脚本、
  CANN 版本、build network、runtime package list 哈希、pciutils 来源、离线包数量/
  格式/集合哈希、lspci 路径与版本（但不记录代理）、wheel 文件名/SHA-256/
  安装版本与路径、离线强制重装事实、
  HAL/import/custom ops/wheel/package metadata 校验、构建期 driver 是否注入、输入哈希、
  最终镜像不包含该输入的事实以及
  “未执行 NPU 负载”的事实；
- 将上游 Mulan PSL v2 许可证随镜像保留。

镜像 manifest 是构建时供应链记录，不代替 A3 节点上的 `describe npu_burn`、
runtime smoke、短 NPU Burn 和正式 acceptance。

### 1.3 NPU Burn 固定容器契约

仓库必须提供 `scripts/stress/create_npu_burn_container.sh` 供管理员在节点部署阶段
创建或安全启动固定容器。该工具不属于 CATMonitor 作业路径；Manager、CLI、Web 和
`benchmark_check.sh npu_burn` 仍只能对既有容器执行只读 inspect 与 `docker exec`，
不得在作业期间创建、启动、停止、删除或替换容器。

管理员工具至少接收 image、container name、host output directory、docker binary，
并允许覆盖默认 `ascend` runtime。创建 profile 必须使用 `privileged`、host network、
64 MiB shm、`/workspace` workdir、`label=disable` security option、默认 PID/IPC
namespace，以及不会退出的 Bash command。它不得复制或硬编码镜像的 CANN、
torch_npu、PATH、LD_LIBRARY_PATH、ASCEND 或 ATB 环境变量。

工具必须动态枚举宿主机已有 `/dev/davinci[0-9]*`，按数字排序后 identity-map 到
容器同名路径，不得写死设备数量或补造缺失 ID；还必须校验并映射
`davinci_manager`、`devmm_svm`、`hisi_hdc`，已验证的 driver/DCMI/npu-smi 路径，
以及 host output directory 到镜像默认输出目录。已有匹配运行容器必须幂等成功；
匹配但停止的容器可以安全启动；名称相同但镜像或 profile 不一致时必须失败，且
不得静默删除。管理员工具默认使用 `unless-stopped` restart policy，并允许显式选择
`no`、`on-failure`、`always` 或 `unless-stopped`；restart policy 必须进入 profile
哈希和既有容器一致性检查。

`docker_exec` 的 NPU Burn logical ID 来自 upstream 对 Ascend PCI accelerator 的
`lspci` 枚举、排序和编号，并最终作为 torch_npu device index 使用。CATMonitor 必须
同时枚举容器内 `/dev/davinciN` 与同一容器的 upstream-compatible PCI topology；
两个 logical ID 集合完全一致时才能通过 preflight，缺失 `lspci`、没有 PCI 结果或
集合不一致时必须失败，不能接受 upstream 的固定八设备 fallback。该 ID 不是
`npu-smi` Phy-ID，也不能只用 PyTorch 报告的 device count 验证。模板不得默认选择设备；管理员必须
显式选择一个或多个已预留 logical ID。`all` 仅允许在整节点由本压测独占时显式
配置，不得作为共享节点推荐值。describe 和正式执行前，native backend 必须从
宿主机、`docker_exec` backend 必须从固定容器内的 `/dev/davinci[0-9]*` 获取可用
ID，容器探测失败时不得回退宿主机。describe v1 通过 `topology_source` 和
`pci_topology_devices` 暴露交叉检查结果。空值、重复值、非法格式或越界配置都应作为
必需资产失败，并提示有效 logical IDs。describe v1 可通过新增参数
`device_namespace=npu_burn_logical` 与 `available_devices` 向旧消费者兼容地暴露事实。

### 1.4 完整部署生成契约

仓库必须提供 `scripts/stress/generate_stress_deployment.sh`，把已经生成的 CPU build
manifest、NPU image manifest、CPU runtime 根目录、MPI/线程规模和管理员固定 NPU
容器 profile 组合为完整节点部署。输出必须至少包含：

- 从当前仓库模板渲染、可通过 `bash -n` 的 `benchmark_check.sh`；
- 顶层 `stress:` 下四项全部启用的 YAML；
- 记录两份输入 manifest 哈希、adapter/config 哈希及有效资源规模的部署 manifest。

生成器不得下载/构建 benchmark、创建/启动容器、运行负载、自动选择 NPU、修改
源码目录或默认写入 `/etc`。输出目录必须显式提供；已有文件默认拒绝覆盖，只有
`--force` 可替换。HPL/HPCG launcher/线程规模、NPU logical ID、芯片代际和 workload
必须由管理员显式给出。主配置的安全默认值继续保持全部关闭；完整启用示例放在
`configs/stress-full.example.yaml`，不改变未部署资产节点的默认行为。

## 2. CLI 与配置

规范命令为：

```bash
catmonitor stress -o table
catmonitor stress doctor -o table
```

无参数时从主配置读取 `default_benchmarks` 并启动作业；`--help` 只显示帮助。
成功或帮助返回 0，参数错误返回 2，
配置、资产、执行或结果错误返回 1。`-o json` 回显完整报告，`-o table`
将状态映射为 `OK` 等表格标签并把各数值拆成独立行。
命令行适配实现位于 `features/stress/cli`；`cmd/catmonitor` 只负责顶层命令分发。

`stress doctor` 是只读部署验收命令：它不得提交作业，而应对配置中的每个 benchmark
复用 Manager 的 `Availability/Describe` 判据，输出启用状态、可用性、preflight、
原因和 profile。feature 未启用、没有启用项目或任一启用项目不可用时返回 1；参数
错误返回 2；全部启用项目可用时返回 0。禁用项目只显示，不触发 describe 或容器探测。
`-o json|table` 与默认配置路径规则和运行命令一致。

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
    npu_burn: { enabled: true, timeout: 30m }
```

`enabled` 控制整个特性；`web_enabled` 仅授权 Web 发起高负载作业，CLI 不依赖
它。CLI 与新版只读 Web 都从平台路径加载同一份 CATMonitor 主配置；显式覆盖项
分别为 CLI 的 `-c/--config` 和 Web 的 `-config`，未指定时依次读取
`CATMONITOR_CONFIG` 环境变量与平台默认路径。Web
不复制 `stress:`，也不恢复已经移除的 Web 专用 YAML 配置。

YAML 不接收 benchmark 可执行路径。具体执行器、环境变量、MPI/NUMA 参数和
工作目录由节点 `benchmark_check.sh` 维护。Ascend NPU Burn 的执行 backend、
工具路径、容器名/镜像元数据、运行时版本、结果目录、用例/组、设备列表、芯片
代际和工具内部超时也只由节点脚本维护；当前上游
版本固定使用其 `$HOME/.ascend_npu_burn/output` 默认目录，以避开有缺陷的自定义
`--output` 校验。适配器不得提供把该参数重新打开的兼容开关；容器部署必须由
bootstrap 将节点结果目录绑定到镜像默认目录。HPCG 的 `result_dir` 仅供 Go
核验本次结果文件，不用于定位可执行文件。

NPU Burn 的芯片代际和 workload 必须在节点脚本中显式、成对配置，profile
分别以 `chip_generation` 和 `workload` 暴露。CATMonitor 不做隐式
generation-to-workload 映射；当前实机验证组合为 A2 + `matmul`、A3 +
`quant_matmul`，但节点仍须按实际安装版本确认用例存在。当前上游未实际消费的
参数不得进入模板配置、执行命令、describe、报告或 Web。

CATMonitor 作业不是容器环境管理器。第一版不在 Go 中抽象 container executor，
不接收 image/device/volume/env/command，不创建、启动、停止或删除容器。仓库
脚本模板支持 `native`，以及对管理员预先启动并维护的固定容器执行
`docker_exec`；容器创建由 1.3 节的管理员工具在部署阶段完成，不得把任意容器
参数暴露给 CLI 或 Web。
容器适配必须自行提供可验证的硬时限和清理语义，确保 CATMonitor 取消或进程
异常断开后容器内负载不会无限继续；不满足该条件的容器不得启用 Web 触发。

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
benchmark_check.sh describe npu_burn
```

命令必须无 benchmark 副作用，只向 stdout 输出一个协议版本为 1 的 JSON
对象。对象包含实际参数、资源规模、必需资产、MPI launcher 实现、二进制 ABI
识别结果和总预检状态。资产缺失或明确 ABI 不匹配为 `fail`；静态链接、厂商
MPI 等无法可靠识别的情况为 `warn`，不得误判为失败。CATMonitor 对 JSON
字段、版本、benchmark 名和状态做严格校验。未声明协议的旧脚本使用基础预检
兼容运行，但必须暴露 `unsupported` 警告。

NPU Burn 的 profile 必须通过参数数组暴露实际 backend、device namespace、选定及
可用 logical IDs。容器模式还应暴露固定
容器名、实际/声明镜像、CANN、torch_npu 和 SoC；缺少运行时元数据为 `warn`，
容器不存在、未运行、镜像不匹配、容器内执行器不可用或 logical ID 越界为
`fail`。describe 仅做 inspect、可执行性和 `/dev/davinciN` 名称检查，不得启动
benchmark，也不得改变容器生命周期。

## 3. 作业、状态和报告

Manager 同时只运行一个作业，所选项目按顺序执行。Linux 使用
`${report_path}.lock` 的非阻塞内核文件锁，保证不同 CLI/Web 进程互斥；锁随
进程退出释放。Web 每次读取最近报告时重新读取共享文件，因此可观察 CLI 作业，
但只能取消本 Web 进程启动的作业。

状态语义：

- `healthy`：命令自行成功结束，且必需结果解析成功；
- `time_limit_reached`：STREAM/HPL/HPCG 配置窗口到达后按计划停止，属于通过，允许无性能值；
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

Ascend NPU Burn 源码以 Mulan PSL v2 第三方组件形式固定在仓库中；仓库不内置
其 wheel、运行二进制、CANN、torch_npu、驱动或基础镜像。管理员可从该固定源码
原生安装，或用仓库镜像构建器结合已审批基础镜像构建运行环境。
正常完成时，节点脚本必须读取工具本次生成的
`npu_burn_results.csv`，验证存在至少一个设备和结果行、每行 `result=PASS` 且
`err_count=0`，并拒绝全局设备汇总中的 `FAIL`，再输出 CATMonitor 规范化摘要。
结果文件必须在本次命令期间新增或更新，不能接受未变化的历史 PASS 文件；工具
退出码 0 不能替代这些校验。
适配器必须显式传入 `--sdc_detect`。这是本特性的 SDC 可靠性判定语义，不是仅用于
规避上游 `args.detect` 未初始化缺陷的参数；不得以默认初始化补丁替代并暗中关闭
SDC 检测。因为该工具用于 SDC/硬件错误检测，CATMonitor 外层时限到达但没有完整 CSV 时
必须为 `unhealthy`，不得沿用其他三项的受控时限通过语义。

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

未启用的 feature/benchmark 不得因 Web 配置轮询触发 describe 或 Docker 预检。
启用但预检失败时，CLI 和 Web 必须直接显示失败资产、路径和具体原因；Web 不得
只显示“未就绪”或仅把原因放在悬停文本中。NPU 卡片还必须显示实际 backend、
容器、镜像和已声明的运行时/SoC 摘要。

结果页面必须按指标语义展示：STREAM 的四项 MB/s 可在同一比例尺比较；
HPL/HPCG 的 GFLOP/s 分别作为主指标，不得与问题规模、进程数或秒数混合归一化，
也不得直接比较 HPL 与 HPCG。时间和运行参数使用独立详情区域。同一 benchmark
存在至少两次历史性能值时可显示零基线趋势，但趋势不改变通过/失败状态。
Ascend NPU Burn 显示设备数、结果行数、通过/失败数、错误数和累计用例时间；
新报告只生成 `devices/cases/passed/failed/errors/case_time_seconds`，Web 仅在读取
历史报告时兼容旧 key。失败摘要中的计数必须保留并突出显示，
不将这些可靠性计数伪装成性能分数。

## 5. 验证要求

自动化测试必须覆盖解析、按时限通过、取消、进程组清理、报告原子写入与错误、
历史上限/排序/输出裁剪、防御性复制、跨进程锁、共享报告刷新、describe
无副作用/严格 JSON/超时/旧版降级、资产和 MPI ABI 预检、NPU logical device
范围/错误提示、profile 哈希持久化、
Web 安全策略、CLI 退出码、NPU Burn PASS/FAIL CSV 和外层超时语义及独立 SPA 资源。Linux 执行单元测试、竞态检查和
构建；Windows 交叉构建。容器节点还必须实测正常结束、外层超时、用户取消和
Web 进程异常退出后的容器内残留进程。真实性能只在资产与拓扑匹配的 Linux
节点验收。

完整部署测试必须覆盖四项 adapter 渲染、YAML/manifest、已有输出拒绝覆盖、非法
资源参数和缺失 manifest；CLI `stress doctor` 与 Web `/api/stress/config` 必须用
四项 fixture 验证全部 `available=true`，并验证 feature/benchmark 禁用时不触发
describe。发布审计必须机械校验 bundled NPU Burn 来源、逐文件哈希、许可证复制和
runtime package 清单，同时明确不把外部 CPU/NPU 构建 manifest 冒充为 SBOM。

构建工具测试还必须覆盖 CPU 三项事务安装，以及 NPU 镜像默认内置来源、开发
覆盖来源、来源元数据缺失/非法、逐文件篡改、无补丁/显式补丁、源码不变、拒绝
覆盖、输入/标签失败、manifest JSON 和禁止 Docker 容器生命周期操作。固定容器
bootstrap 测试必须覆盖设备动态枚举、控制设备缺失、结果目录、image/profile
不匹配、运行/停止容器的幂等行为，以及不得复制 image ENV。模拟 Docker 测试
不能替代真实基础镜像构建或 A3 NPU 实机验收。
