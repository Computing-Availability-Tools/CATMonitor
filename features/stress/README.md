# stress 可靠性压测特性

`features/stress` 是与 `health`、`dfee` 同级的独立特性。它只在用户显式请求后
运行 STREAM、HPL、HPCG 或 Ascend NPU Burn，不进入 daemon 周期，也不直接修改健康总分。
CLI 参数解析与结果展示位于 `features/stress/cli` 子包，主程序只挂载 `stress` 命令。

```bash
catmonitor stress -o table
catmonitor stress doctor -o table
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
并按 STREAM 带宽、HPL/HPCG GFLOP/s、NPU Burn 用例结果、时间和运行参数分别展示，避免不同单位
共用一个比例尺。

节点上的 benchmark 可执行文件、环境变量、MPI/NUMA 参数和工作目录统一维护在
`benchmark_check.sh`。生产环境应将适配后的脚本部署到源码目录之外，防止升级
覆盖；特定机器路径和实测数据不得提交到开源仓库。

适配脚本同时实现只读协议
`benchmark_check.sh describe <stream|hpl|hpcg|npu_burn>`。它不会启动 benchmark，
只返回实际路径、线程/MPI 规模、HPL/HPCG 问题规模、资产状态及 MPI ABI
预检 JSON。Web 在启动前展示这份 profile；作业报告和历史保存 profile、
脚本/输入资产 SHA-256 及聚合配置哈希，便于复现实机结果。未实现、未声明或
返回无效 describe v1 的部署脚本会被判定为不可用，不能启动压测。

仓库模板的 HPL/HPCG 启动命令只使用 MPICH/Hydra 与 OpenMPI 共同支持的
`-np`，并依赖已 `export` 的线程变量。部署时应先确认 launcher 与 benchmark
使用同一种 MPI 实现，再在部署副本中增加该实现专用的绑核或通信参数。

Ascend NPU Burn 源码以固定上游修订版内置在
`third_party/ascend_npu_burn/source`，并继续遵循 Mulan PSL v2；来源、Git
revision、归档哈希和逐文件哈希记录在同目录的 `UPSTREAM*` 与
`SOURCE_SHA256SUMS`。CATMonitor 不内置 CANN、PyTorch/torch_npu、驱动或基础
镜像。节点管理员可在脚本中选择宿主机原生执行，
或使用 `docker_exec` 调用一个已经运行且由管理员维护的固定容器；镜像、设备、挂载、
环境和容器命令不进入 YAML/Web。`describe` 会把 backend、容器/镜像、CANN、
torch_npu、SoC、芯片代际、NPU Burn logical device namespace 和用例作为只读
profile 参数展示并写入配置哈希。
芯片代际和 workload 必须由节点管理员显式、成对配置；CATMonitor 不根据代际
暗中改写用例。当前已验证组合为 A2 的 `matmul` 与 A3 的 `quant_matmul`，具体
可用用例仍以所部署 NPU Burn 版本为准。
当前上游版本把结果写入 `$HOME/.ascend_npu_burn/output`。CATMonitor 固定省略
有缺陷的自定义 `--output` 参数：原生模式读取同一账户的默认目录，容器模式由
bootstrap 把节点结果目录绑定到镜像内默认目录。CATMonitor 只接受本次
`npu_burn_results.csv` 中所有结果
均为 `PASS`、`err_count=0` 且全局设备汇总无 `FAIL` 的完整报告；外层超时不作为
NPU Burn 通过。

## NPU Burn 镜像构建

仓库提供管理员工具 `scripts/stress/build_npu_burn_image.sh`，默认从仓库内固定的
MindCluster AscendNPUBurn 源码和管理员批准的本地 CANN/torch_npu 基础镜像构建
可追溯镜像。管理员无需再下载或传入 NPU Burn 源码；`--source` 与
`--source-metadata` 只供上游升级、开发或兼容性验证覆盖使用。
A3 首次候选使用 `--compat-profile none`；只有实际兼容故障确认后，才用命名
profile 和显式审计补丁构建，不会默认带入 A2 修改。

基础镜像必须包含可用于构建的 CANN toolkit/devlib、PyTorch、torch_npu 和 TBE。
构建器会显式发现并 source CANN 环境，依次支持
`ascend-toolkit/set_env.sh`、`ascend-toolkit/latest/bin/setenv.bash` 和唯一的
`cann-*/set_env.sh`；多版本歧义时必须用 `--ascend-env-script` 指定镜像内绝对路径。

```bash
sudo bash scripts/stress/build_npu_burn_image.sh \
  --base-image registry.example/ascend/cann-pytorch:approved \
  --image catmonitor/npuburn:a3-candidate \
  --compat-profile none
```

Ascend 910B4（A2）、CANN 8.3.RC2、torch/torch_npu 2.8 使用已审计的
`a2-cann83` profile。若基础镜像依赖宿主机驱动库才能完成 import，可将
driver `lib64` 作为仅构建阶段输入：

```bash
sudo bash scripts/stress/build_npu_burn_image.sh \
  --base-image quay.io/ascend/vllm-ascend:v0.12.0rc1 \
  --image catmonitor/npuburn:a2-cann83 \
  --compat-profile a2-cann83 \
  --patch scripts/stress/patches/ascend_npu_burn/a2-cann83.patch \
  --build-driver-lib-dir /usr/local/Ascend/driver/lib64
```

构建采用多阶段镜像：宿主机驱动只进入 disposable builder，用于 HAL、torch_npu、
custom ops 和 wheel 验证；最终运行镜像从原始基础镜像重新开始，不包含这份驱动输入。
manifest 会记录 driver 输入哈希以及 `included_in_final_image=false`。

构建会在 wheel 之前检查 `libascend_hal.so`、torch、torch_npu 和 TBE，再执行
wheel 构建、纯本地强制重装、安装包元数据、`ascend_npu_burn`/custom ops import
与入口文件可执行性检查。构建阶段不会启动依赖 NUMA/NPU 拓扑的运行时 CLI。安装使用
`--no-index --no-deps --force-reinstall`，即使
基础镜像存在同版本包，最终镜像也必须使用本轮固定源码生成的 wheel。它不要求
`/usr/local/Ascend/driver`、`npu-smi` 或 NPU 设备，不创建运行容器，也不执行 NPU
压测。生成的
`npu-burn-image-manifest.json` 记录源码来源、上游 revision、逐文件校验清单、
兼容补丁、基础/目标镜像 ID 与摘要、模板哈希、实际 CANN 环境、wheel
文件名/哈希/安装位置、预检结果和兼容
profile。真正的驱动、设备与 ABI 验证仍由管理员固定容器、`describe npu_burn`、
单卡 smoke 和正式验收完成。

最终 runtime image 必须包含 `pciutils/lspci`。这是 upstream 枚举真实 Ascend PCI
topology 的运行依赖；缺失时 upstream 会静默退回固定八设备假设，在 16-die A3 上
产生错误范围。依赖名称由仓库的 `docker/stress/npu/runtime-packages.txt` 维护，版本
由审批基础镜像的软件仓库决定。正常节点默认使用 Docker `default` build network：
基础镜像已带 `lspci` 时不下载，否则按清单安装。受限节点可临时设置标准 HTTP(S)
代理环境变量，构建器只把已设置的变量名作为 Docker 预定义 build args 转发，不输出
值，也不写入 Git、YAML、manifest 或镜像 ENV。隔离节点可重复传入
`--pciutils-package`，把兼容 RPM/DEB 依赖闭包离线装入镜像；未显式指定网络时该路径
自动使用 `none`。不要只挂载宿主机 `/usr/bin/lspci`，它还依赖 `libpci` 等与宿主机
ABI 相关的文件。manifest 会自动记录依赖来源、离线包集合哈希、build network、路径
和版本，不要求使用者手工计算。

## NPU Burn 固定容器

镜像构建成功后，管理员使用仓库工具创建或安全启动长期运行容器：

```bash
sudo bash scripts/stress/create_npu_burn_container.sh \
  --image catmonitor/npuburn:a3-candidate \
  --name catmonitor-npuburn-a3 \
  --output-dir /var/lib/catmonitor/npu-burn-output \
  --docker-bin /usr/bin/docker \
  --runtime ascend \
  --restart-policy unless-stopped
```

工具自动枚举并 identity-map 宿主机全部 `/dev/davinciN`，同时映射必需控制设备、
已验证的驱动/工具路径和默认结果目录。它继承镜像 `Config.Env`，不会复制 CANN、
torch_npu 或 PATH 环境变量。相同 profile 的运行中容器直接复用，停止容器会被启动；
名称相同但镜像或 profile 不一致时明确失败，不会静默 `rm -f`。

CATMonitor 作业仍只执行 `docker exec`，不会调用该管理员生命周期工具。切换
`NPU_BURN_DEVICE` 不需要重建容器。对于当前支持并已验证的 fixed-container
topology，该值来自 upstream 的 PCI topology 枚举并作为 torch_npu device index；
CATMonitor 会交叉检查容器 `/dev/davinciN` ID 集合与 `lspci` topology ID 集合；
不得直接填写 `npu-smi` Phy-ID，也不得用 `torch.npu.device_count()` 推导其范围。
模板不默认选择设备，管理员必须明确配置一个或多个已预留设备，例如 `7` 或
`0,1,7`。上游支持 `all`，但它只适用于整节点已由本压测独占的场景，不作为共享
节点推荐值。`describe npu_burn` 会列出容器实际可见的 logical IDs，并在负载启动
前拒绝空值、重复值、非法格式和越界配置。

## CPU 压测资产构建

仓库提供管理员工具 `scripts/stress/build_cpu_benchmarks.sh`，用于从任意位置的
STREAM 源文件、HPL/HPCG 源码包以及管理员提供的 `HPL.dat`、`hpcg.dat` 构建
并安装原生运行资产。它支持显式选择 GCC、MPI 和 OpenBLAS，默认将资产安装到
`/opt/catmonitor/benchmarks/runtime`，并在相邻的 `manifests` 目录生成
`build-manifest.json`。脚本不会修改 CATMonitor YAML、不会覆盖节点
`benchmark_check.sh`，也不会执行完整 HPL/HPCG 压测。

```bash
sudo bash scripts/stress/build_cpu_benchmarks.sh \
  --stream-src /path/to/stream.c \
  --hpl-src /path/to/hpl-2.3.tar.gz \
  --hpl-dat /path/to/HPL.dat \
  --hpcg-src /path/to/hpcg-3.1.tar.gz \
  --hpcg-dat /path/to/hpcg.dat \
  --mpicc /absolute/path/to/mpicc \
  --mpicxx /absolute/path/to/mpicxx \
  --mpirun /absolute/path/to/mpirun \
  --openblas-include /absolute/path/to/openblas/include \
  --openblas-lib /absolute/path/to/openblas/lib
```

CPU 资产和固定 NPU 容器准备完成后，使用
`scripts/stress/generate_stress_deployment.sh` 一次生成源码目录外的完整
`benchmark_check.sh`、四项配置和部署 manifest，不再靠逐行手工复制节点变量。
随后执行 `catmonitor stress doctor -o table`，在不启动任何负载的情况下按与 Web
相同的判据检查四项可用性。构建 manifest 记录构建时事实；部署 manifest 记录配置
输入；`describe/doctor` 报告当前节点事实，三者职责不同。完整参数、增量构建、
覆盖策略和验收步骤见 [STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md)。

## 自动化测试

stress 遵循主项目的测试组织方式：Go UT/组件测试与实现包就近放置，构建工具的
无硬件 fixture 放在 `scripts/stress/tests`，只有跨真实二进制和 HTTP 边界的产品链
测试放在顶层 `tests/e2e`。常用入口：

```bash
make test-stress-ut       # Go UT/组件测试
make test-stress-build    # CPU/NPU 构建、部署和发布审计 fixture
make test-stress-e2e      # 编译真实 CLI/Web 后验证完整产品链（Linux，无硬件负载）
make test-stress-race     # Manager/Web 并发与竞态检查
make test-stress          # UT + 构建 fixture + E2E
```

仓库 E2E 使用临时 host adapter，只验证四类结果解析、CLI/Web 配置和路由、共享
报告/历史及跨进程互斥，不声称验证真实性能、MPI 实现、CANN ABI 或 NPU SDC。
STREAM/HPL/HPCG 和 NPU Burn 的真实执行仍必须按测试指南在对应 Linux/A2/A3 节点
完成。

## 文档

| 文档 | 内容 |
|---|---|
| [STRESS_SPEC.md](STRESS_SPEC.md) | 功能、配置、状态、CLI 与 API 契约 |
| [STRESS_DESIGN.md](STRESS_DESIGN.md) | 包边界、执行、互斥、持久化和 Web 设计 |
| [STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md) | CPU/NPU 资产构建、新装/升级、candidate 迁移、实机验收与回滚 |
| [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) | 仓库分发物与管理员外部资产的许可证边界 |
| [OSS_RELEASE_AUDIT.md](OSS_RELEASE_AUDIT.md) | 发布审计命令、检查范围和 SBOM 闭环条件 |
