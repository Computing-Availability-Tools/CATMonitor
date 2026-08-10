# stress 可靠性压测特性

`features/stress` 是与 `health`、`dfee` 同级的独立特性。它只在用户显式请求后
运行 STREAM、HPL、HPCG 或 Ascend NPU Burn，不进入 daemon 周期，也不直接修改健康总分。
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
并按 STREAM 带宽、HPL/HPCG GFLOP/s、NPU Burn 用例结果、时间和运行参数分别展示，避免不同单位
共用一个比例尺。

节点上的 benchmark 可执行文件、环境变量、MPI/NUMA 参数和工作目录统一维护在
`benchmark_check.sh`。生产环境应将适配后的脚本部署到源码目录之外，防止升级
覆盖；特定机器路径和实测数据不得提交到开源仓库。

适配脚本同时实现只读协议
`benchmark_check.sh describe <stream|hpl|hpcg|npu_burn>`。它不会启动 benchmark，
只返回实际路径、线程/MPI 规模、HPL/HPCG 问题规模、资产状态及 MPI ABI
预检 JSON。Web 在启动前展示这份 profile；作业报告和历史保存 profile、
脚本/输入资产 SHA-256 及聚合配置哈希，便于复现实机结果。旧脚本可继续运行，
但页面会提示 describe 不可用，直到部署副本合入新协议。

仓库模板的 HPL/HPCG 启动命令只使用 MPICH/Hydra 与 OpenMPI 共同支持的
`-np`，并依赖已 `export` 的线程变量。部署时应先确认 launcher 与 benchmark
使用同一种 MPI 实现，再在部署副本中增加该实现专用的绑核或通信参数。

Ascend NPU Burn 源码以固定上游修订版内置在
`third_party/ascend_npu_burn/source`，并继续遵循 Mulan PSL v2；来源、Git
revision、归档哈希和逐文件哈希记录在同目录的 `UPSTREAM*` 与
`SOURCE_SHA256SUMS`。CATMonitor 不内置 CANN、PyTorch/torch_npu、驱动或基础
镜像，也不管理容器生命周期。节点管理员可在脚本中选择宿主机原生执行，
或使用 `docker_exec` 调用一个已经运行且由管理员维护的固定容器；镜像、设备、挂载、
环境和容器命令不进入 YAML/Web。`describe` 会把 backend、容器/镜像、CANN、
torch_npu、SoC、芯片代际和用例作为只读 profile 参数展示并写入配置哈希。
当前上游版本默认从 `$HOME/.ascend_npu_burn/output` 读取结果，避免其自定义
`--output` 校验缺陷。CATMonitor 只接受本次 `npu_burn_results.csv` 中所有结果
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

构建会在 wheel 之前检查 `libascend_hal.so`、torch、torch_npu 和 TBE，再执行
wheel 构建/安装、`ascend_npu_burn` import 与 `npu-burn --version`。它不要求
`/usr/local/Ascend/driver`、`npu-smi` 或 NPU 设备，不创建运行容器，也不执行 NPU
压测。生成的
`npu-burn-image-manifest.json` 记录源码来源、上游 revision、逐文件校验清单、
兼容补丁、基础/目标镜像 ID 与摘要、模板哈希、实际 CANN 环境、预检结果和兼容
profile。真正的驱动、设备与 ABI 验证仍由管理员固定容器、`describe npu_burn`、
单卡 smoke 和正式验收完成。

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

构建完成后，管理员仍需把已安装的绝对路径和实际运行规模写入源码目录外的
`/etc/catmonitor/benchmark_check.sh`，再执行逐项 `describe` 和实机验收。构建
manifest 记录编译时事实；`describe` 报告当前节点事实，两者职责不同。完整参数、
增量构建、覆盖策略和验收步骤见 [STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md)。

## 文档

| 文档 | 内容 |
|---|---|
| [STRESS_SPEC.md](STRESS_SPEC.md) | 功能、配置、状态、CLI 与 API 契约 |
| [STRESS_DESIGN.md](STRESS_DESIGN.md) | 包边界、执行、互斥、持久化和 Web 设计 |
| [STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md) | CPU/NPU 资产构建、新装/升级、candidate 迁移、实机验收与回滚 |
