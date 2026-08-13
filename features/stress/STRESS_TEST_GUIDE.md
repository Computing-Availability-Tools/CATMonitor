# stress 可靠性压测部署与验收指南

本文说明如何构建、部署、升级和验收 `features/stress`。开源文档只使用通用路径；
具体节点的 benchmark 路径、MPI/NUMA 参数、IP 地址和实测结果应保存在部署侧，
不得提交到开源仓库。

当前正式接口：

- CLI：`catmonitor stress`
- Web：`/stress/`
- API：`/api/stress/*`
- 配置：CATMonitor 主配置顶层 `stress:`
- 节点适配：源码目录外的 `benchmark_check.sh`

## 1. 部署原则

1. STREAM、HPL、HPCG、Ascend NPU Burn 的绝对路径、环境变量、MPI/NUMA/NPU 参数都放在节点脚本中，
   不允许通过 Web 任意编辑。
2. `/etc/catmonitor/benchmark_check.sh` 一旦通过实机验证，升级时不得用仓库模板
   直接覆盖。
3. 升级应以新模板生成候选脚本，只迁移旧脚本顶部的节点变量；候选脚本通过
   `describe` 后才能切换。
4. 不要把旧脚本整体复制回新版本，否则会丢失新的 `describe` 协议；也不要把
   未适配的新模板直接投入运行，否则会丢失节点路径和 MPI 参数。
5. CLI 与 Web 使用同一份主配置、`report_path` 和 Linux 文件锁。两边能共享
   报告，但不能同时启动两组压测。
6. 交互式 SSH 终端不建议全局执行 `set -e`；检查命令失败时应保留终端，先查看
   返回码和日志。

## 2. 开发机检查

在仓库的 `CATMonitor` 目录执行：

```bash
bash -n features/stress/benchmark_check.sh
bash -n scripts/stress/build_cpu_benchmarks.sh
bash -n scripts/stress/build_npu_burn_image.sh
make test-stress-build
gofmt -w features/stress features/stress/cli
go test ./features/stress/... ./features/web ./cmd/catmonitor ./internal/config
go test -race ./features/stress/... ./features/web
go vet ./features/stress/... ./features/web ./cmd/catmonitor ./internal/config
```

按目标架构验证构建：

```bash
GOOS=linux GOARCH=amd64 go build ./cmd/catmonitor ./features/web
GOOS=linux GOARCH=arm64 go build ./cmd/catmonitor ./features/web
GOOS=windows GOARCH=amd64 go build ./cmd/catmonitor ./features/web
```

Windows 产物仅用于确认项目可构建；可靠性压测执行仍只支持 Linux。

### 2.1 CPU benchmark 从源码构建

从上游官方来源获取并固定版本，下载、审批和归档由节点管理员负责：

- STREAM：<https://github.com/jeffhammond/STREAM/blob/master/stream.c>
- HPL：<https://netlib.org/benchmark/hpl/hpl-2.3.tar.gz>
- HPCG：<https://github.com/hpcg-benchmark/hpcg>（使用与节点验证一致的 3.1 源码包）

生产环境应在构建前独立校验下载物哈希；构建 manifest 会记录实际输入哈希，但不
替代供应链审批，也不会自动联网下载源码。

构建和运行必须分开。管理员只在资产准备或升级时执行：

```bash
sudo bash scripts/stress/build_cpu_benchmarks.sh \
  --stream-src /data/packages/stream.c \
  --hpl-src /data/packages/hpl-2.3.tar.gz \
  --hpl-dat /data/profiles/HPL.dat \
  --hpcg-src /mnt/software/hpcg-3.1.tar.gz \
  --hpcg-dat /data/profiles/hpcg.dat \
  --cc /absolute/toolchain/bin/gcc \
  --cxx /absolute/toolchain/bin/g++ \
  --mpicc /absolute/mpi/bin/mpicc \
  --mpicxx /absolute/mpi/bin/mpicxx \
  --mpirun /absolute/mpi/bin/mpirun \
  --openblas-include /absolute/openblas/include \
  --openblas-lib /absolute/openblas/lib \
  --output-root /opt/catmonitor/benchmarks/runtime
```

源码和配置无需复制到固定 `packages` 目录，五个输入可以位于完全不同的文件系统。
未显式提供编译器时脚本从 `PATH` 查找通用名称，但生产构建建议传入绝对路径并保留
manifest。`--only stream`、`--only hpl`、`--only hpcg` 可单项重建，`--skip`
可排除项目；已有选中资产默认阻止覆盖，确认替换时显式使用 `--force`。

源码包中的 Linux 脚本必须保留 LF 行尾。若下载和中转发生在 Windows，优先使用
`git archive` 从 Git 对象生成 tar 包，不要直接把经 `core.autocrlf` 转换的工作树
打包；受传输系统扩展名限制时可以临时改名为 `*.zip.1`，远端核对 SHA-256 后再改回
`.tar.gz`。`configure` 出现 `$'\r': command not found` 表示归档已混入 CRLF，应该
重新制作源码包，而不是在生产构建中宽泛改写第三方文件。

默认 STREAM 数组为 80000000、重复次数为 10，短 smoke 会使用 4 个 OpenMP
线程和 `numactl --interleave=all`，节点应预留约 1.8 GiB 数组内存。可在低资源的
构建验证环境用 `--stream-array-size` 和 `--stream-ntimes` 缩小，但发布资产必须
记录实际值。HPL/HPCG 构建阶段不会启动完整压测；`HPL.dat` 和 `hpcg.dat` 必须由
管理员按节点规模准备，脚本不会自动生成。

构建后检查：

```bash
test -x /opt/catmonitor/benchmarks/runtime/stream/stream_omp
test -x /opt/catmonitor/benchmarks/runtime/hpl/xhpl
test -f /opt/catmonitor/benchmarks/runtime/hpl/HPL.dat
test -x /opt/catmonitor/benchmarks/runtime/hpcg/xhpcg
test -f /opt/catmonitor/benchmarks/runtime/hpcg/hpcg.dat

python3 -m json.tool \
  /opt/catmonitor/benchmarks/manifests/build-manifest.json
```

没有 Python 时可用 `jq .` 或直接审阅 JSON。重点确认 architecture、`mpicc -show`、
`mpicxx -show`、`mpirun --version`、源码/二进制/配置 SHA-256、STREAM 编译参数和
`openmp_patch_applied`。旧版 HPCG 的 `default(none)` pragma 缺少 `n` 时脚本精确
补入；当前源码的非 `default(none)` pragma 显式列出预定共享变量 `n` 时精确移除，
以兼容 GCC 7.3。输入已经是相应兼容布局时不重复修改，并记录
`openmp_patch_applied=false`。源码不包含任一已知布局时必须失败，不能用宽泛
`sed` 继续。

仓库模拟测试不会执行真实 benchmark：

```bash
make test-stress-build
```

测试覆盖任意源路径、三项安装、分项强制替换、manifest 保留、HPCG 精确补丁拒绝
和归档路径穿越拒绝。它不替代目标 Linux 的真实 GCC/MPI/OpenBLAS 构建验收。

构建完成不等于可运行。下一步仍需按本指南后续章节把资产绝对路径、MPI/NUMA/
线程规模写入 `/etc/catmonitor/benchmark_check.sh`，再执行 `describe` 和逐项实测。

该工具面向节点本地构建，不表示 CATMonitor 仓库分发第三方源码或二进制。若后续
把 STREAM/HPL/HPCG 二进制纳入安装包或镜像，发布流程必须另行核对各上游许可证、
版权声明和二进制再分发义务，并补充对应 third-party notices。

### 2.2 Ascend NPU Burn 镜像构建

仓库已经在 `third_party/ascend_npu_burn/source` 内置经过审计的固定
MindCluster AscendNPUBurn 上游源码。`UPSTREAM`、`UPSTREAM.md` 和
`SOURCE_SHA256SUMS` 记录 repository、revision、Git tree、归档/逐文件哈希及
Mulan PSL v2 许可证边界。标准构建不需要再次下载源码或传 `--source`。

管理员只需选择与目标节点驱动、CANN 和 torch_npu 匹配的已审批基础镜像。
基础镜像必须包含可用于构建的 CANN toolkit/devlib、PyTorch、torch_npu 和 TBE，并先
用 `docker pull` 或 `docker load` 使它存在于本机。构建器不会联网选择基础镜像，
也不负责修复不匹配的软件栈：

```bash
BASE_IMAGE=registry.example/ascend/cann-pytorch:approved
docker pull "$BASE_IMAGE"  # 离线环境改用 docker load -i /path/to/image.tar
docker image inspect "$BASE_IMAGE" >/dev/null
```

A3 首次 candidate 必须先使用无补丁 profile：

```bash
sudo bash scripts/stress/build_npu_burn_image.sh \
  --base-image "$BASE_IMAGE" \
  --image catmonitor/npuburn:a3-candidate \
  --compat-profile none \
  --docker-bin /usr/bin/docker \
  --build-root /var/tmp/catmonitor-npu-burn-build
```

A2/CANN 8.3 基础镜像需要宿主机 driver 才能 import torch_npu 时，使用仓库已审计
profile：

```bash
sudo bash scripts/stress/build_npu_burn_image.sh \
  --base-image quay.io/ascend/vllm-ascend:v0.12.0rc1 \
  --image catmonitor/npuburn:a2-cann83 \
  --compat-profile a2-cann83 \
  --patch scripts/stress/patches/ascend_npu_burn/a2-cann83.patch \
  --build-driver-lib-dir /usr/local/Ascend/driver/lib64 \
  --docker-bin /usr/bin/docker \
  --build-root /var/tmp/catmonitor-npu-burn-build-a2
```

`--build-driver-lib-dir` 必须是宿主机专用 `lib64` 绝对目录且包含
`libascend_hal.so`。目录只复制到 disposable builder stage；最终 stage 从原始
base image 重建，不包含宿主机 driver。不要把 `/usr/local/Ascend/driver` 上层目录
或 NPU 设备传给镜像构建器。

构建器先核对内置逐文件哈希、元数据 schema、必需文件、LF 和符号链接；
`build-root` 必须是专用绝对目录且不能包含空白。已有目标镜像或 manifest 默认
拒绝覆盖，确认替换同一 candidate 时显式增加 `--force`。

构建器默认按确定顺序查找 CANN 环境：

1. 显式 `--ascend-env-script` 指定的镜像内绝对路径；
2. `/usr/local/Ascend/ascend-toolkit/set_env.sh`；
3. `/usr/local/Ascend/ascend-toolkit/latest/bin/setenv.bash`；
4. 唯一的 `/usr/local/Ascend/cann-*/set_env.sh`。

若有多个 `cann-*` 且无 canonical 路径，构建会明确失败，不会选取
字典序首项。例如已确认实际环境为 CANN 9.0.1 时可增加：

```bash
--ascend-env-script /usr/local/Ascend/cann-9.0.1/set_env.sh
```

构建只执行以下动作：

```text
隔离复制源码 → 发现并 source CANN 环境
             → libascend_hal + torch/torch_npu/TBE 预检
             → build/build.sh → 离线强制重装唯一 wheel
             → 校验安装包元数据 → import ascend_npu_burn/custom_ops
             → 检查运行入口存在且可执行
             → 校验镜像标签 → 写 manifest
```

它不会调用 `docker run/create/start/stop/rm`，不会映射 NPU，也不会运行矩阵或
SDC 负载。自带 HAL 的基础镜像允许宿主机 `/usr/local/Ascend/driver` 不存在；需要
宿主机 driver 的基础镜像必须显式使用 build-only 输入。`npu-smi` 不可用时的
警告也不是失败，只要 HAL 可解析且 Python 预检返回 0。Docker RUN
使用无网络模式，pip 只安装本轮 wheel，不需要配置 PyPI 代理。默认 manifest 位于：

```text
/var/tmp/catmonitor-npu-burn-build/manifests/npu-burn-image-manifest.json
```

检查构建身份：

```bash
python3 -m json.tool \
  /var/tmp/catmonitor-npu-burn-build/manifests/npu-burn-image-manifest.json

docker image inspect \
  --format '{{.Id}} {{.Architecture}} {{json .RepoDigests}}' \
  catmonitor/npuburn:a3-candidate
```

manifest 中应看到 `source.origin=bundled`、固定上游 repository/revision、来源
元数据和逐文件清单哈希、原始/补丁后源码、Dockerfile、entrypoint、entrypoint
mode validator、Ascend helper 的 SHA-256，基础/目标镜像身份、架构和
`compatibility.profile=none`。还应看到 `runtime.ascend_env_script`、
`runtime.cann_version`、HAL/torch/torch_npu/TBE/wheel/package metadata 验证结果、
`wheel.filename`、`wheel.sha256`、安装版本/路径、
`wheel.force_installed=true`、`wheel.network_access=false`、custom ops import，
`build_driver.injected/source_path/sha256/included_in_final_image`、
`validation.driver_mount_present_at_build` 以及
`validation.npu_workload_run=false`。`driver_mount_present_at_build=false` 是合法的构建记录。
这些信息只能证明镜像软件栈和包构建完成，不能证明 A3 驱动 ABI、
设备健康或压测结果。真正 driver/device 验证在管理员固定容器和
`benchmark_check.sh describe npu_burn` 阶段完成。

如果且仅如果无补丁构建或 A3 smoke 暴露了明确兼容问题，先形成最小审计补丁，再
用命名 profile 构建：

```bash
sudo bash scripts/stress/build_npu_burn_image.sh \
  --base-image "$BASE_IMAGE" \
  --image catmonitor/npuburn:a3-candidate-fix1 \
  --compat-profile a3-fix1 \
  --patch scripts/stress/patches/ascend_npu_burn/a3-fix1.patch
```

命名 profile 必须至少带一个 `--patch`，`none` 禁止带补丁。构建器先 dry-run，
再只修改临时源码快照；原始源码不会被写回。A2 上验证过的 `a2-cann83.patch`
不能默认带到 A3。仓库没有预置 A3 兼容补丁；不得在没有真实故障、审计和实机
验收的情况下虚构补丁。

仅做上游升级或开发验证时，可显式覆盖来源；这不是发布构建的常规用法：

```bash
sudo bash scripts/stress/build_npu_burn_image.sh \
  --source /data/src/ascend_npu_burn/source \
  --source-metadata /data/src/ascend_npu_burn/UPSTREAM \
  --base-image "$BASE_IMAGE" \
  --image catmonitor/npuburn:development \
  --compat-profile none
```

覆盖来源也必须提供满足 `UPSTREAM` schema 的来源元数据；manifest 会把它记录为
`source.origin=override`，避免与正式 bundled 构建混淆。

只运行构建器模拟 Docker/DFX 测试：

```bash
make test-stress-build-npu
```

该测试不需要 Docker daemon，也不执行第三方构建或 NPU 负载。真实镜像仍必须在
具备匹配基础镜像和 Docker daemon 的 Linux 构建节点完成。

## 3. 定位源码并构建候选二进制

Git 工作树和 ZIP 解压目录都可以使用。先显式设置路径，不要依赖当前目录：

```bash
REPO_ROOT=/path/to/CATHelper
CAT_ROOT="$REPO_ROOT/CATMonitor"
GO_BIN=${GO_BIN:-/opt/catmonitor/toolchains/go1.25.1/bin/go}

test -f "$CAT_ROOT/go.mod"
test -f "$CAT_ROOT/features/stress/benchmark_check.sh"
test -x "$GO_BIN"
"$GO_BIN" version
grep -q 'CATMONITOR_STRESS_DESCRIBE_PROTOCOL=1' \
  "$CAT_ROOT/features/stress/benchmark_check.sh"
```

当前 `go.mod` 要求 Go 1.23.4 或更高版本。若上述输出仍是系统默认的 Go 1.21.9，
不要继续构建；它会报 `go.mod requires go >= 1.23.4`。节点已经安装 Go 1.25.1
时，应保持 `GO_BIN=/opt/catmonitor/toolchains/go1.25.1/bin/go`，或者显式调整 PATH：

```bash
export PATH="/opt/catmonitor/toolchains/go1.25.1/bin:$PATH"
hash -r
command -v go
go version
GO_BIN=$(command -v go)
```

`GOTOOLCHAIN=local` 的含义是禁止自动下载或切换工具链，并不会把旧 Go 升级为
1.25.1。因此后续命令始终调用 `"$GO_BIN"`，不能写成未确认版本的裸 `go`。

若不知道 ZIP 的实际层级，可先定位唯一模板：

```bash
find /path/to/extracted-directory \
  -type f \
  -path '*/CATMonitor/features/stress/benchmark_check.sh' \
  -print
```

升级时先备份，再构建到临时目录，不直接覆盖运行中的二进制：

```bash
STAMP=$(date +%Y%m%d%H%M%S)
BACKUP_ROOT=/opt/catmonitor/backups/stress-upgrade-$STAMP
BUILD_DIR=/opt/catmonitor/build-stress-$STAMP

install -d -m 0750 "$BACKUP_ROOT"
install -d -m 0755 "$BUILD_DIR"

cp -a /etc/catmonitor/benchmark_check.sh "$BACKUP_ROOT/" 2>/dev/null || true
cp -a /etc/catmonitor/catmonitor.yaml "$BACKUP_ROOT/" 2>/dev/null || true
cp -a "$CAT_ROOT/bin/catmonitor" "$BACKUP_ROOT/" 2>/dev/null || true
cp -a "$CAT_ROOT/bin/catmonitor-web" "$BACKUP_ROOT/" 2>/dev/null || true
```

构建：

```bash
cd "$CAT_ROOT"

GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  "$GO_BIN" mod verify

GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  "$GO_BIN" build -buildvcs=false -trimpath \
  -o "$BUILD_DIR/catmonitor" ./cmd/catmonitor

GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  "$GO_BIN" build -buildvcs=false -trimpath \
  -o "$BUILD_DIR/catmonitor-web" ./features/web

"$BUILD_DIR/catmonitor" version
"$BUILD_DIR/catmonitor" stress --help
```

如果 `go mod verify` 因未缓存的非构建依赖失败，最终以两个 `GOPROXY=off go build`
是否成功为准；`go list -m all` 不适合作为离线构建的必要条件。

## 4. 新装节点脚本

仅在节点还没有正式脚本时，从模板创建部署副本：

```bash
install -d -m 0750 /etc/catmonitor /var/lib/catmonitor
install -m 0750 \
  "$CAT_ROOT/features/stress/benchmark_check.sh" \
  /etc/catmonitor/benchmark_check.sh
```

编辑脚本顶部配置区，写入节点真实值：

- STREAM：执行器、`numactl` 和线程数；
- HPL：工作目录、执行器、库目录、MPI launcher、进程数和线程数；
- HPCG：工作目录、执行器、MPI launcher、进程数、线程数、网格和运行时长。
- Ascend NPU Burn：执行 backend、`npu-burn`、输出目录、用例或组、设备列表、
  工具内部超时和芯片代际；容器模式还声明固定容器和运行时元数据。

所有执行器、工作目录和 launcher 必须使用绝对路径。仓库模板的 HPL/HPCG
命令只使用 MPICH/Hydra 与 OpenMPI 共同支持的 `-np`；厂商专用绑核、通信和
root 参数只能在验证过的节点部署副本中维护。

### 4.1 准备 Ascend NPU Burn 运行环境

CATMonitor 内置固定 `MindCluster-AscendNPUBurn` 源码，但不内置 wheel、CANN、
PyTorch/torch_npu、驱动、基础镜像或运行结果，运行时也不创建或管理容器。目标
节点管理员必须先准备与驱动、固件、CANN、PyTorch/torch_npu、SoC 匹配且已经
实测的固定环境。可选择以下两种模式；容器镜像按 2.2 节从内置源码构建。

原生模式直接按上游文档构建并安装：

```bash
cd "$CAT_ROOT/third_party/ascend_npu_burn/source"
bash build/build.sh
python3 -m pip install build/dist/ascend_npu_burn-*.whl

command -v npu-burn
npu-burn --version
```

容器模式由管理员在部署阶段创建并保持一个固定容器运行，再由节点脚本执行
`docker exec`。容器 profile 不写入 CATMonitor YAML，也不能由 Web 修改。
CATMonitor 作业不会自动 pull/create/start/stop/rm 容器。

项目以 Mulan PSL v2 发布，部署和再分发需保留其许可证及版权声明。原生模式要
创建 `$HOME/.ascend_npu_burn/output`；容器模式由下述管理员工具创建宿主结果目录
并挂到镜像默认输出目录。

A3 节点已有 STREAM/HPL/HPCG 时，不要为了接入 NPU Burn 先重编 CPU 三项。
保留当前已验证资产和 MPI 参数，只生成新的 CATMonitor/脚本 candidate，并对
三项重新执行 `describe`；镜像构建和 NPU 验收独立推进。

仓库镜像的默认 HOME 是 `/opt/catmonitor/npuburn-home`，entrypoint
`/usr/local/bin/catmonitor-npu-burn` 会初始化可用的 CANN 环境。镜像构建成功后，
使用仓库提供的管理员工具创建固定容器：

```bash
sudo bash scripts/stress/create_npu_burn_container.sh \
  --image catmonitor/npuburn:a3-candidate \
  --name catmonitor-npuburn-a3 \
  --output-dir /var/lib/catmonitor/npu-burn-output \
  --docker-bin /usr/bin/docker \
  --runtime ascend \
  --restart-policy unless-stopped
```

工具会检查镜像与 Docker daemon，动态枚举并按数字顺序 identity-map 宿主机全部
`/dev/davinciN`，同时要求并映射：

```text
/dev/davinci_manager
/dev/devmm_svm
/dev/hisi_hdc
/usr/local/Ascend/driver/lib64
/usr/local/Ascend/driver/version.info
/etc/ascend_install.info
/usr/local/dcmi
/usr/local/bin/npu-smi
```

宿主结果目录映射到
`/opt/catmonitor/npuburn-home/.ascend_npu_burn/output`。容器 profile 使用
`runtime=ascend`、privileged、host network、64 MiB shm、`/workspace` workdir、
默认 PID/IPC namespace 和 `label=disable`；脚本不硬编码或复制镜像的 29 个 ENV，
CANN、torch_npu、PATH、LD_LIBRARY_PATH、ASCEND/ATB 等继续由 image `Config.Env`
负责。

重复执行时，匹配且运行中的容器直接成功，匹配但停止的容器会安全启动。名称相同
但镜像或 profile 不一致时脚本失败并要求管理员检查；它不会静默 `docker rm -f`。
CATMonitor runtime 不调用这个脚本，仍然只执行 `docker exec`。

原生模式示例：

```bash
NPU_BURN_BACKEND="native"
NPU_BURN_EXECUTABLE="/absolute/path/to/bin/npu-burn"
NPU_BURN_USE_DEFAULT_OUTPUT=true
NPU_BURN_OUTPUT_DIR="${HOME}/.ascend_npu_burn/output"
NPU_BURN_RUN_CASE="quant_matmul" # 与 NPU_BURN_GROUP 二选一
NPU_BURN_GROUP=""
NPU_BURN_DEVICE="7"              # 明确选择管理员已预留的 logical device
NPU_BURN_INTERNAL_TIMEOUT_SECONDS=300
NPU_BURN_CHIP_GENERATION="A3"    # A2、A3 或 A5
```

管理员预先启动的 A3 固定容器对应脚本示例：

```bash
NPU_BURN_BACKEND="docker_exec"
NPU_BURN_CONTAINER_RUNTIME="/usr/bin/docker"
NPU_BURN_CONTAINER_NAME="catmonitor-npuburn-a3"
NPU_BURN_CONTAINER_IMAGE="catmonitor/npuburn:a3-candidate"
NPU_BURN_RUNTIME_CANN="<管理员确认的实际版本>"
NPU_BURN_RUNTIME_TORCH_NPU="<管理员确认的实际版本>"
NPU_BURN_SOC_MODEL="<实际 A3 SoC>"

# 这是容器内绝对路径；describe 会用 docker exec /usr/bin/test -x 预检。
NPU_BURN_EXECUTABLE="/usr/local/bin/catmonitor-npu-burn"
NPU_BURN_USE_DEFAULT_OUTPUT=true
NPU_BURN_OUTPUT_DIR="/var/lib/catmonitor/npu-burn-output"
NPU_BURN_RUN_CASE="quant_matmul"
NPU_BURN_GROUP=""
NPU_BURN_DEVICE="7"
NPU_BURN_INTERNAL_TIMEOUT_SECONDS=120
NPU_BURN_CHIP_GENERATION="A3"
```

对于当前支持并已验证的 fixed-container topology，`NPU_BURN_DEVICE` 使用固定
容器内 `/dev/davinciN` 的 `N` 所对应的 NPU Burn logical device ID，不是
`npu-smi` 的 Phy-ID，也不能根据 PyTorch logical device count 推导。必须明确选择
管理员已经预留的设备，例如 `7` 或 `0,1,7`；不得依靠脚本自动选择所谓空闲卡。
上游支持 `all`，但只应在整节点已经由本压测独占时显式使用。一次已验证 A3 节点
的四类事实为：

| 编号来源 | 本次节点 | 用途 |
|---|---|---|
| Linux device node | `/dev/davinci0`～`/dev/davinci7` | 固定容器 identity map |
| NPU Burn `--device` | `0`～`7` | `NPU_BURN_DEVICE` 的有效范围 |
| `torch.npu.device_count()` | `16` | PyTorch namespace，不用于本参数校验 |
| `npu-smi` Phy-ID | `0`～`15` | 运维物理编号，不可直接填入本参数 |

该节点上 logical device 7 对应 `/dev/davinci7`，并观察到 board7 的 host Phy-ID
为 14/15；这只是本机证据，不能推广成通用映射。配置 `14` 会在 describe 阶段
直接失败并列出有效 logical IDs，不再等上游以 RC=255 退出。改变 logical device
只需修改节点脚本并重新 describe，不需要重建镜像或容器。

`NPU_BURN_CHIP_GENERATION` 与 `NPU_BURN_RUN_CASE` 必须显式、成对设置，适配器
不会自动把代际映射成 workload。当前已验证 A2 使用 `matmul`、A3 使用
`quant_matmul`；这不是对所有上游版本的硬编码保证，部署前仍需按实际版本确认。

`NPU_BURN_CONTAINER_IMAGE` 是声明的可复现镜像身份；运行容器的实际镜像不一致
时预检失败。CANN、torch_npu 和 SoC 是管理员确认后的只读元数据，缺失时
describe 返回 `warn`，但不会尝试在 Web 中安装或修复环境。仓库模板不包含任意
Docker 参数输入，因此不会把宿主机控制权扩展给 Web 用户。

`docker exec` 客户端被强制终止时，容器内进程是否同时退出由运行时和容器侧命令
决定。管理员必须为 NPU Burn 配置确定的工具硬时限或容器侧清理机制，并在开启
`web_enabled` 前实际测试正常结束、CATMonitor 外层超时、用户取消和 Web 进程
异常退出四种路径；任一路径存在残留 NPU Burn 进程时不得开放 Web 触发。

脚本固定传入 `--sdc_detect`。`NPU_BURN_INTERNAL_TIMEOUT_SECONDS` 是工具内部的
单用例时限；YAML 的 `benchmarks.npu_burn.timeout` 是 CATMonitor 整个作业的
外层上限，必须覆盖所选全部用例、设备初始化和报告收尾时间。

部分 Ascend NPU Burn 版本会错误拒绝调用者传入的现有
`--output` 目录，因此默认保持 `NPU_BURN_USE_DEFAULT_OUTPUT=true`：适配器不传
`--output`，但仍从工具默认的 `$HOME/.ascend_npu_burn/output` 读取结果。CATMonitor
CLI/Web 必须与安装和创建该目录时使用同一个系统账户。仅当后续安装版本已验证
自定义 `--output` 可用时，才改成 `false` 并设置其他绝对目录。当前容器验收路径
保持 `true`，通过 bootstrap 的默认输出 bind 读取宿主结果，不额外传 `--output`。

## 5. 升级现有节点脚本

已有实机脚本时，从新模板生成 candidate，再迁移节点变量：

```bash
OLD_SCRIPT=/etc/catmonitor/benchmark_check.sh
CANDIDATE=/etc/catmonitor/benchmark_check.sh.candidate.$STAMP
NEW_TEMPLATE="$CAT_ROOT/features/stress/benchmark_check.sh"

test -f "$OLD_SCRIPT"
cp -a "$NEW_TEMPLATE" "$CANDIDATE"
chmod 0750 "$CANDIDATE"
```

下面的脚本只复制已定义的配置赋值，不复制旧实现逻辑。若变量缺失或新模板中
出现重复定义，它会失败而不会写回不完整结果：

```bash
python3 - "$OLD_SCRIPT" "$CANDIDATE" <<'PY'
from pathlib import Path
import re
import sys

old_path = Path(sys.argv[1])
new_path = Path(sys.argv[2])
old = old_path.read_text(encoding="utf-8")
new = new_path.read_text(encoding="utf-8")

variables = [
    "STREAM_EXECUTABLE", "STREAM_NUMACTL", "STREAM_THREADS",
    "HPL_WORKDIR", "HPL_EXECUTABLE", "HPL_LIBRARY_DIR",
    "HPL_MPI_LAUNCHER", "HPL_MPI_PROCESSES", "HPL_THREADS_PER_PROCESS",
    "HPCG_WORKDIR", "HPCG_EXECUTABLE", "HPCG_MPI_LAUNCHER",
    "HPCG_MPI_PROCESSES", "HPCG_THREADS_PER_PROCESS",
    "HPCG_NX", "HPCG_NY", "HPCG_NZ", "HPCG_RUNTIME_SECONDS",
]

errors = []
for name in variables:
    old_match = re.search(rf"^{re.escape(name)}=.*$", old, re.MULTILINE)
    new_matches = list(re.finditer(rf"^{re.escape(name)}=.*$", new, re.MULTILINE))
    if old_match is None:
        errors.append(f"旧脚本缺少变量：{name}")
        continue
    if len(new_matches) != 1:
        errors.append(f"新模板中的 {name} 数量不是 1：{len(new_matches)}")
        continue
    new = re.sub(
        rf"^{re.escape(name)}=.*$",
        lambda _: old_match.group(0),
        new,
        count=1,
        flags=re.MULTILINE,
    )

if errors:
    raise SystemExit("\n".join(f"ERROR: {item}" for item in errors))

new_path.write_text(new, encoding="utf-8")
print("节点变量迁移完成")
PY
```

旧脚本若早于上述变量模型，应停止自动迁移，人工把旧参数映射到新模板顶部，
不要为了通过脚本而复制旧的 `case`、解析函数或 MPI 命令实现。
NPU Burn 配置也必须按 4.1 节人工写入 candidate，尤其要保留新模板默认的
`NPU_BURN_DEVICE_ROOT="/dev"`，并重新确认 `NPU_BURN_DEVICE` 使用 logical ID；不要
从旧部署机械复制未经确认的物理编号。

检查候选脚本：

```bash
bash -n "$CANDIDATE"
grep -nE '^(STREAM_|HPL_|HPCG_)' "$CANDIDATE"
grep -nE '^NPU_BURN_' "$CANDIDATE"
diff -u "$OLD_SCRIPT" "$CANDIDATE" | sed -n '1,260p' || true
```

不要用普通文本搜索把 Bash 的 `[ -x "$file" ]` 误判为 OpenMPI 的 `-x` 参数。
若要检查仓库通用模板，应只匹配命令行起始位置：

```bash
if grep -nE -- \
  '^[[:space:]]*(-x[[:space:]]|--allow-run-as-root([[:space:]]|$)|--map-by([[:space:]]|$)|--bind-to([[:space:]]|$)|-mca[[:space:]])' \
  "$CANDIDATE"
then
  echo 'ERROR: candidate contains unreviewed OpenMPI-specific arguments' >&2
  exit 1
fi
```

## 6. `describe` 无副作用预检

启用的 benchmark 必须先通过描述检查；NPU 节点配置完成后共四项：

```bash
for benchmark in stream hpl hpcg npu_burn; do
  output="/tmp/catmonitor-describe-$benchmark.json"
  if ! "$CANDIDATE" describe "$benchmark" > "$output"; then
    echo "ERROR: describe $benchmark failed" >&2
    cat "$output" >&2
    exit 1
  fi
  python3 -m json.tool "$output"
done
```

`describe` 不得启动 benchmark，也不得生成新的 HPL/HPCG 结果文件。逐项确认：

- `protocol_version` 为 `1`，benchmark 名称正确；
- `parameters` 是节点实际路径与运行参数；
- 必需 `assets` 为 `pass`，文件资产带 SHA-256；
- `resources` 与 CPU、线程、MPI 进程数和问题规模一致；
- `mpi.status` 不得为明确的 ABI `fail`；无法判断时允许带原因的 `warn`；
- `preflight.status` 为 `pass`，或为经过人工确认的 `warn`。
- NPU profile 的 `device_namespace` 为 `npu_burn_logical`，`available_devices`
  来自当前 `/dev/davinciN`；选定 ID 不在集合中时必须为 `fail`。

MPI launcher 必须与 HPL/HPCG 编译时使用的 MPI ABI 匹配：

```bash
ldd /absolute/path/to/xhpl | grep -Ei 'mpi|mpich|open-rte|open-pal|pmix'
ldd /absolute/path/to/xhpcg | grep -Ei 'mpi|mpich|open-rte|open-pal|pmix'
/absolute/path/to/mpirun --version
```

A3 的 NPU 预检还要人工交叉核对实际 runtime，不能只填写声明值：

```bash
docker inspect catmonitor-npuburn-a3 \
  --format '{{.State.Running}} {{.Config.Image}} {{.Image}}'
docker exec catmonitor-npuburn-a3 \
  /usr/local/bin/catmonitor-npu-burn --version
docker exec catmonitor-npuburn-a3 python3 -c \
  'import torch, torch_npu; print(torch.__version__); print(torch_npu.__version__)'
npu-smi info
```

镜像名/ID应与构建 manifest 对应，CANN/torch_npu 与基础镜像和宿主机驱动兼容，
SoC、`NPU_BURN_CHIP_GENERATION=A3`、设备列表、结果目录挂载和 NPU 健康状态必须
一致。`describe npu_burn` 的资产失败不能用声明字段掩盖；只有无法静态确认且已
人工核对的兼容信息可以保留为 `warn`。

## 7. 原子切换与统一配置

仅在 candidate 的所有已启用项目 `describe` 都通过后切换。先停止旧 Web，再替换脚本和
候选二进制：

```bash
WEB_PID=$(pgrep -xo catmonitor-web || true)
if [ -n "$WEB_PID" ]; then kill "$WEB_PID"; fi

install -m 0750 "$CANDIDATE" /etc/catmonitor/benchmark_check.sh.new
mv -f /etc/catmonitor/benchmark_check.sh.new \
  /etc/catmonitor/benchmark_check.sh

install -d -m 0755 "$CAT_ROOT/bin"
install -m 0755 "$BUILD_DIR/catmonitor" "$CAT_ROOT/bin/catmonitor.new"
install -m 0755 "$BUILD_DIR/catmonitor-web" "$CAT_ROOT/bin/catmonitor-web.new"
mv -f "$CAT_ROOT/bin/catmonitor.new" "$CAT_ROOT/bin/catmonitor"
mv -f "$CAT_ROOT/bin/catmonitor-web.new" "$CAT_ROOT/bin/catmonitor-web"
```

主配置 `/etc/catmonitor/catmonitor.yaml` 使用顶层 `stress:`，不能放在
`health.stress`：

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
    npu_burn:
      enabled: true
      timeout: 30m
```

说明：

- `enabled` 启用 stress 特性；
- `web_enabled` 只授权 Web 提交，CLI 不依赖它；
- `script_path` 指向节点部署副本；
- `report_path` 同时派生 history 和跨进程锁；
- STREAM/HPL/HPCG/Ascend NPU Burn 的运行参数不写入 YAML；
- 只有 HPCG 需要 `result_dir`，用于读取本次生成或变化的结果文件；
- Web 的单次超时只能缩短 YAML 上限，不会修改配置文件。

新版 Web 不再使用独立 YAML。它是 daemon snapshot 的只读消费者，通过命令行
参数取得监听地址和 snapshot 目录，并从平台默认路径读取 CATMonitor 主配置：

```text
-addr 127.0.0.1:9527
-snapshot-dir /var/lib/catmonitor/snapshot
```

Linux 默认主配置为 `/etc/catmonitor/catmonitor.yaml`，因此常规部署无需额外参数。
非标准路径可用 `CATMONITOR_CONFIG` 环境变量或
`-config /path/to/catmonitor.yaml` 覆盖，显式 flag 优先。

## 8. CLI 实机验收

切换后再次验证正式脚本：

```bash
bash -n /etc/catmonitor/benchmark_check.sh
for benchmark in stream hpl hpcg npu_burn; do
  /etc/catmonitor/benchmark_check.sh describe "$benchmark" \
    | python3 -m json.tool
done
```

按 STREAM、HPCG、HPL、NPU Burn 逐项运行；首次不要同时选择多项：

```bash
cd "$CAT_ROOT"

./bin/catmonitor stress --bench stream -o table

./bin/catmonitor stress --bench hpcg -o table

./bin/catmonitor stress --bench hpl -o table

./bin/catmonitor stress --bench npu_burn -o table
```

A3 首次验收按三级推进，不要直接在全部设备运行长作业：

1. 在固定容器内完成单卡 runtime smoke，验证 Docker、CANN、PyTorch、
   torch_npu 和设备访问；这一步由管理员按已审批 A3 环境执行，不经过 Web。
2. 将部署脚本设为 describe 列出的单个 logical device（本次 A3 为 `7`）、
   `NPU_BURN_RUN_CASE="quant_matmul"` 和较短内部
   时限，通过 CLI 运行，要求本次 CSV 全部 PASS、
   `err_count=0` 且无残留进程。
3. 短测通过后才切换到经过评审的正式 A3 用例/profile 和设备范围，
   再做 CLI、取消/超时清理和 Web acceptance。

矩阵 shape 属于镜像内已审批的 NPU Burn 用例配置，不进入 CATMonitor YAML，也
不允许在 Web 任意编辑。需要 1024 smoke 或 4096 正式 profile 时，应先检查镜像
内实际配置和构建 manifest，不能仅凭用例名称假定资源规模。

上游 v26.1.0 虽解析 `--exec_count`，但执行链没有消费该值，受控测试也未改变
执行时间、CSV 行数或实际 `run_count`。CATMonitor 因此不传递、不校验也不展示
该无效参数。真正的单 case 循环次数由镜像内用例配置的 `run_count` 决定。

需要机器可读输出时使用 `-o json`。表格中的成功状态显示为 `OK`，JSON 使用稳定
内部状态 `healthy`，二者语义相同。

每次作业后检查：

```bash
python3 -m json.tool /var/lib/catmonitor/stress-latest.json
python3 -m json.tool /var/lib/catmonitor/stress-history.json
pgrep -af '[s]tream_omp|[x]hpl|[x]hpcg|[n]pu-burn|ascend_npu_burn|[m]pirun|[n]umactl' || true
```

正常结束、主动取消或达到时限后都不应残留 benchmark、MPI 或 NUMA 进程。

## 9. Web 与 Windows 隧道验收

CLI 启用项均通过后，将主配置的 `stress.web_enabled` 改为 `true`，再启动 Web：

```bash
cd "$CAT_ROOT"
./bin/catmonitor-web \
  -addr 127.0.0.1:9527 \
  -snapshot-dir /var/lib/catmonitor/snapshot
```

另开 Linux 终端检查：

```bash
ss -lntp | grep ':9527'
curl -I http://127.0.0.1:9527/stress/
curl -fsS http://127.0.0.1:9527/api/stress/config \
  | python3 -m json.tool
curl -fsS 'http://127.0.0.1:9527/api/stress/history?limit=20' \
  | python3 -m json.tool
```

Windows PowerShell 通过 SSH 隧道访问，不要把控制接口直接暴露到业务网络：

```powershell
ssh -N `
  -o ExitOnForwardFailure=yes `
  -L 127.0.0.1:19527:127.0.0.1:9527 `
  user@linux-host
```

浏览器打开：

```text
http://127.0.0.1:19527/stress/
```

页面应在作业启动前显示真实执行 profile：执行器、MPI launcher、进程/线程数、
问题规模、资产状态、ABI 预检和配置哈希；页面不得提供脚本、绝对路径或任意参数
编辑入口。展开的 profile 在 2 秒自动刷新后仍应保持展开。

## 10. 报告、历史和配置哈希

`report_path` 保存当前/最近作业。最终作业还会写入同目录的
`stress-history.json`，按新到旧最多保留 100 条。`latest` 只显示一条是正常设计，
历史页用于切换此前的 STREAM、HPL、HPCG 和 Ascend NPU Burn 作业。

报告应保存：

- `initiator` 和 `job_id`；
- 每项实际执行 profile；
- 脚本及输入资产 SHA-256；
- 每项和聚合 `configuration_sha256`；
- 实际超时、运行状态、耗时与指标。

在 Web 中对同一 benchmark 使用一次默认时限、一次更短的单次时限，预期：

- 脚本和输入资产哈希不变；
- 实际配置哈希变化；
- YAML 文件没有被修改；
- STREAM/HPL/HPCG 达到较短窗口且此前没有错误时，状态为 `time_limit_reached` 并按通过展示；
- Ascend NPU Burn 未生成完整 PASS CSV 就达到外层时限时为 `unhealthy`。

## 11. CLI/Web 互斥验收

在 CLI 运行 HPCG 或 HPL 时观察 Web：

- Web 在约 2 秒内显示相同 `job_id` 和 `initiator=cli`；
- Web 不允许取消 CLI 发起的作业；
- Web 同时提交新作业应被拒绝；
- 反向由 Web 运行时，第二个 CLI 也应收到“已有作业运行”的错误；
- 任一作业完成后，两端读取到同一最终报告。

实际互斥由 `${report_path}.lock` 的非阻塞内核文件锁实现。锁文件留在磁盘是正常
现象，不能用文件是否存在判断是否正在运行；进程退出后内核会释放锁。

## 12. 状态判定

| 状态 | 含义 | 通过 |
|---|---|---|
| `healthy` | 命令成功且必需结果已解析 | 是 |
| `time_limit_reached` | STREAM/HPL/HPCG 达到配置窗口，停止前没有检测到错误 | 是 |
| `running` | 正在运行 | 未完成 |
| `cancelled` | 用户主动取消 | 否 |
| `unhealthy` | 命令、校验或解析失败 | 否 |
| `unavailable` / `unsupported` | 资产、配置或平台不满足 | 否 |

HPL、HPCG 和 STREAM 都允许以“受控运行窗口”工作。达到 CATMonitor 时限前没有
检测到错误时，即使尚未产生 GFLOP/s 或 MB/s，也记录为通过；正常退出时则必须
完成各自结果校验和必需指标解析，不设置性能阈值。

Ascend NPU Burn 不采用上述时限通过规则。其上游进程可能在 CSV 含 FAIL 时仍
返回 0，也可能把 worker 异常行从 CSV 中跳过，因此必须同时满足：命令正常结束、
`npu_burn_results.csv` 至少包含一条结果、全部 `result=PASS`、全部
`err_count=0`、文件在本次命令期间确实更新，并且全局设备汇总没有 `FAIL`。
外层超时、未变化的历史 CSV、空/损坏 CSV、FAIL、worker 异常或 SDC 错误均为
`unhealthy`。

## 13. 回滚

升级异常时停止 Web，并从本次备份恢复：

```bash
WEB_PID=$(pgrep -xo catmonitor-web || true)
if [ -n "$WEB_PID" ]; then kill "$WEB_PID"; fi

install -m 0750 "$BACKUP_ROOT/benchmark_check.sh" \
  /etc/catmonitor/benchmark_check.sh
install -m 0755 "$BACKUP_ROOT/catmonitor" \
  "$CAT_ROOT/bin/catmonitor"
install -m 0755 "$BACKUP_ROOT/catmonitor-web" \
  "$CAT_ROOT/bin/catmonitor-web"

cp -a "$BACKUP_ROOT/catmonitor.yaml" /etc/catmonitor/catmonitor.yaml
```

备份中某个文件原本不存在时跳过对应恢复命令。

## 14. 最终验收清单

- [ ] CLI 和 Web 在目标架构构建成功
- [ ] NPU 内置源码通过逐文件哈希校验，来源 revision/许可证记录完整
- [ ] NPU 镜像从内置源码和已审批本地基础镜像构建，manifest 与镜像标签/ID一致且未运行 NPU 负载
- [ ] 管理员 bootstrap 动态映射全部现有 `/dev/davinciN`，固定容器 profile/结果 bind 正确且重复执行幂等
- [ ] 固定容器 restart policy 符合部署选择（默认 `unless-stopped`），并通过既有容器一致性校验
- [ ] A3 首次构建使用 `compat-profile=none`；任何补丁都有命名 profile、审计文件和哈希
- [ ] 正式节点脚本位于源码目录外并通过 `bash -n`
- [ ] 所有启用项目（含 NPU Burn）的 `describe` 无副作用且无阻断性资产/ABI 错误
- [ ] Docker/容器缺失时 CLI 与 Web 直接显示失败资产、路径和原因，禁用项不执行 describe
- [ ] `describe npu_burn` 显示 logical namespace/有效 IDs，并在 workload 前拒绝 `npu-smi` Phy-ID
- [ ] NPU device list 覆盖单卡、逗号列表、非连续 topology；空值、重复值、空格、负数、非数字和越界均失败
- [ ] 主配置只有顶层 `stress:`，Web 默认读取平台路径且可显式覆盖
- [ ] CLI 依次完成 STREAM、HPCG、HPL、Ascend NPU Burn 启用项
- [ ] 容器 NPU Burn 的正常结束、外层超时、取消和 Web 异常退出均无容器内残留进程
- [ ] JSON 报告、历史、profile 和配置哈希完整
- [ ] 正常结束、取消和超时后无残留进程
- [ ] Web 只监听回环地址并通过 SSH 隧道访问
- [ ] profile 展开状态不被自动刷新清除
- [ ] CLI 与 Web 双向共享报告并拒绝并发作业
- [ ] 单次缩短超时会改变执行配置哈希但不修改 YAML
- [ ] 回滚文件和操作步骤已验证

### 14.1 V4 pristine A3 闭环

V4 候选不能只运行容器内裸 `npu-burn`。至少完成以下产品链路：

1. 用全新名称执行 bootstrap，创建 V4 fixed candidate；再次执行同一命令应幂等
   复用，不重建、不覆盖、不删除容器。
2. 将节点脚本设置为管理员确认空闲的 logical device，例如 `7`，执行
   `describe npu_burn`，确认 `preflight=pass`、`device_namespace` 和容器内
   `available_devices`。
3. 临时改为实机确认无效的 `14`，确认 describe 在负载前失败并提示不得使用
   `npu-smi` Phy-ID；随后恢复 `7` 并重新 describe。
4. 必须通过完整入口执行：

   ```bash
   catmonitor stress -b npu_burn -o table
   ```

   当前 V4 `quant_matmul` 验收预期为整体 `Healthy`，并从本次 CSV 得到
   `devices=1`、`cases=2`、`passed=2`、`failed=0`、`errors=0`；同时核对 int32、
   int8 均为 PASS、`run_count=100`、`err_count=0`。这些数值是本次 V4 profile 的
   验收基线，不是所有 NPU Burn 用例的通用固定值。
5. Web 应展示同一报告中的 device namespace、available devices 和 `2 / 2 PASS`；
   Web 不提供 device、image、container 或 run case 编辑入口。

推荐顺序：

```text
备份
→ 临时目录构建 CLI/Web
→ 构建/核验 NPU Burn candidate 镜像（不运行 NPU）
→ 管理员用 bootstrap 创建固定容器并检查实际 runtime/NPU/logical IDs
→ 新模板生成 candidate
→ 迁移节点变量
→ candidate describe
→ 原子切换
→ CLI：STREAM → HPCG → HPL → NPU 单卡短测 → NPU 正式验收
→ 检查 report/history/profile/hash
→ 启动 Web
→ 验证页面、单次超时与跨进程互斥
```
