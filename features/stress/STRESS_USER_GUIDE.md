# CATMonitor 可靠性压测使用说明

本文面向 CATMonitor 使用者和节点管理员，说明如何启用、检查、执行和查看
STREAM、HPL、HPCG 与 Ascend NPU Burn。构建工具的全部参数、升级、回滚和发布
审计请继续参考 [STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md)。

## 1. 使用边界

可靠性压测只在用户显式触发时执行，不属于 daemon 周期健康检查：

- STREAM：内存带宽与数据搬运；
- HPL：高密度浮点计算与 MPI；
- HPCG：内存、计算和 MPI 综合负载；
- Ascend NPU Burn：NPU 计算与 SDC 检测。

运行期间会占用 CPU、内存、MPI/NUMA 或 NPU 资源，实时健康分可能暂时下降；
压测结果本身不计入健康总分。CPU 三项不设置性能阈值，运行成功或达到计划时限且
此前没有错误即可通过；NPU Burn 必须产生完整 PASS、`err_count=0` 且没有全局 FAIL。

当前实现保留三类镜像，不建议合并成一个巨型镜像：

| 镜像/运行面 | 职责 | 是否需要 Docker Socket |
|---|---|---|
| CATMonitor/Web/DFeE | 采集、快照、控制、展示 | CPU-only 不需要 |
| CPU Stress Runner | STREAM/HPL/HPCG 与匹配的 MPI/OpenBLAS | 不需要 |
| Ascend NPU Burn | CANN、torch_npu、custom ops 和 NPU workload | 当前过渡方案由控制面调用固定容器 |

宿主机原生 CPU 执行仍受支持，适合无 Docker 或需要保持原生 MPI/NUMA 环境的节点。

## 2. 最短使用流程

如果管理员已经安装好运行资产、节点 adapter 和 YAML，普通使用者只需：

```bash
# 不启动负载，检查配置、资产、MPI ABI、容器和设备
catmonitor stress doctor -c /etc/catmonitor/catmonitor.yaml -o table

# JSON doctor 输出包含每个项目的实际执行参数和资源规模
catmonitor stress doctor -c /etc/catmonitor/catmonitor.yaml -o json

# 执行默认项目
catmonitor stress -c /etc/catmonitor/catmonitor.yaml -o table

# 显式执行一个或多个项目；项目之间串行执行
catmonitor stress --bench stream,hpcg \
  -c /etc/catmonitor/catmonitor.yaml \
  -o table
```

CLI 规范命令是 `catmonitor stress`，没有额外的 `run` 子命令。`-o json` 返回完整
JSON；`-o table` 将状态显示为 `OK` 并把不同指标拆行展示。

建议验收顺序为 STREAM → HPCG → HPL → NPU Burn。首次不要同时选择所有项目。

## 3. 构建 CATMonitor

项目要求 Go 1.23.4 或更高版本。`GOTOOLCHAIN=local` 不会自动升级旧 Go，因此应先
确认实际调用的二进制：

```bash
GO_BIN=/opt/catmonitor/toolchains/go1.25.1/bin/go

"$GO_BIN" version
mkdir -p bin

GOTOOLCHAIN=local "$GO_BIN" build \
  -buildvcs=false -trimpath \
  -o bin/catmonitor ./cmd/catmonitor

GOTOOLCHAIN=local "$GO_BIN" build \
  -buildvcs=false -trimpath \
  -o bin/catmonitor-web ./features/web
```

如果系统 `go version` 已满足 `go.mod`，可将 `GO_BIN` 设置为 `$(command -v go)`。

## 4. 单一配置文件

CLI、daemon 和 Web 读取同一份 CATMonitor 主配置。stress 位于顶层 `stress:`，不再
维护独立 Web YAML：

```yaml
stress:
  enabled: true
  web_enabled: true
  script_path: /opt/catmonitor/stress/benchmark_check.sh
  report_path: /var/lib/catmonitor/stress/stress-latest.json
  default_benchmarks: [stream]
  benchmarks:
    stream:
      enabled: true
      timeout: 2m
    hpl:
      enabled: true
      timeout: 15m
    hpcg:
      enabled: true
      result_dir: /var/lib/catmonitor/stress/work/hpcg
      timeout: 5m
    npu_burn:
      enabled: false
      timeout: 30m

snapshot:
  enabled: true
  dir: /var/lib/catmonitor/snapshot

features: [web, dfee, health]
```

`web_enabled` 只控制网页能否提交作业。CLI 只需要 `enabled: true`；不开放网页触发时
应保持 `web_enabled: false`。Web 还必须监听回环地址并使用可写的共享报告路径。

YAML 只保存功能开关、共享报告路径、项目和最大运行窗口。可执行文件、MPI/线程、
NUMA、HPL/HPCG 规模、NPU 容器和设备选择属于节点 profile，由部署后的
`benchmark_check.sh` 管理；网页只读展示，不允许编辑脚本或任意参数。

## 5. CPU 运行方式

### 5.1 宿主机原生模式

节点管理员准备 STREAM 源文件、stock HPL/HPCG 源码、`HPL.dat`、`hpcg.dat`、
编译器、MPI 和 OpenBLAS，然后执行：

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

默认资产目录为 `/opt/catmonitor/stress/runtime`。不要用 OpenMPI 参数驱动 MPICH，
也不要让 HPL/HPCG 使用与编译 ABI 不一致的 MPI launcher。

### 5.2 CPU Runner 容器模式

容器化控制面推荐使用独立 CPU Runner：

```bash
sudo bash scripts/stress/build_cpu_runner_image.sh \
  --image catmonitor/stress-cpu:node-v1 \
  --stream-src /path/to/stream.c \
  --hpl-src /path/to/hpl-2.3.tar.gz \
  --hpl-dat /path/to/HPL.dat \
  --hpcg-src /path/to/hpcg-3.1.tar.gz \
  --hpcg-dat /path/to/hpcg.dat \
  --jobs 16 \
  --build-root /var/tmp/catmonitor-cpu-runner-build
```

构建只生成镜像和 `cpu-runner-image-manifest.json`，不会创建容器或运行长负载。
Runner 只监听私有 Unix Socket，只接受 `stream`、`hpl`、`hpcg` 三个固定项目，
不能传入任意命令、路径、参数或环境变量。控制面与 Web 不需要 Docker Socket。

CPU Runner 生产配置应保持：非特权、只读根文件系统、`network_mode: none`、
`cap_drop: ALL`、`no-new-privileges`。入口为了目录初始化和 NUMA syscall 放行而声明的
bootstrap capability 必须在 runner 启动前全部清空。

## 6. Ascend NPU Burn

仓库固定保存经过审计的 AscendNPUBurn 上游树，但不分发 CANN、torch_npu、驱动或
基础镜像。管理员必须选择与节点匹配的基础镜像。

### 6.1 已验证组合

| 节点 | 基础环境 | profile | 建议 workload |
|---|---|---|---|
| Ascend 910B4（A2） | CANN 8.3.RC2、torch_npu 2.8 | `a2-cann83` | `matmul` |
| A3 16-die 验收节点 | CANN 9.0.1、torch_npu 2.10 | `none` | `quant_matmul` |

该表是已验收组合，不代表所有驱动、CANN、PyTorch 或 SoC 版本自动兼容。

### 6.2 构建镜像

```bash
sudo bash scripts/stress/build_npu_burn_image.sh \
  --base-image registry.example/ascend/cann-pytorch:approved \
  --image catmonitor/npuburn:a3-candidate \
  --compat-profile none \
  --build-root /var/tmp/catmonitor-npu-burn-build
```

最终镜像必须包含 `pciutils/lspci`，否则上游可能退回固定八设备假设。联网节点由
构建器按 `runtime-packages.txt` 安装；受限节点可临时设置标准代理；完全离线节点
使用 `--pciutils-package` 注入与基础镜像同发行版、同架构的 RPM/DEB 依赖闭包。
不要只挂载宿主机 `/usr/bin/lspci`。

### 6.3 创建固定容器

```bash
sudo bash scripts/stress/create_npu_burn_container.sh \
  --image catmonitor/npuburn:a3-candidate \
  --name catmonitor-npuburn-a3 \
  --output-dir /var/lib/catmonitor/stress/npu-burn-output \
  --docker-bin /usr/bin/docker \
  --runtime ascend \
  --restart-policy unless-stopped
```

设备节点 ID、NPU Burn logical ID 和 `npu-smi` Phy-ID 不是跨平台永久等价关系。
管理员必须通过容器内 `/dev/davinciN`、`lspci` topology 和实际负载建立对应关系。

当前 `docker_exec` 方案需要管理员显式启用 NPU Burn Docker Socket overlay。Docker
Socket 等价于宿主机 root 权限，属于过渡部署边界；长期方案是独立受限 NPU Runner，
不应把 Socket 直接提供给 Web。

## 7. 生成与安装节点部署

资产和固定容器准备完成后，用生成器创建节点 adapter、配置片段和部署 manifest：

```bash
bash scripts/stress/generate_stress_deployment.sh --help
```

宿主机 CPU 使用 `--cpu-backend local`；CPU Runner 使用：

```text
--cpu-backend unix
--cpu-runner-image catmonitor/stress-cpu:node-v1
--cpu-runner-manifest /absolute/path/cpu-runner-image-manifest.json
```

生成完成后安装稳定目录：

```bash
sudo bash scripts/stress/install_stress_runtime.sh \
  --adapter /etc/catmonitor/stress-deployment/benchmark_check.sh \
  --cpu-runner-adapter /etc/catmonitor/stress-deployment/cpu-runner-benchmark_check.sh \
  --deployment-manifest /etc/catmonitor/stress-deployment/stress-deployment-manifest.json
```

当前安装器只安装已审核文件并准备 `/opt/catmonitor/stress` 与
`/var/lib/catmonitor/stress`，不会构建资产、编辑主 YAML、启动服务或运行负载。

## 8. 启动 Web

先启动带 snapshot 的 daemon，再启动只读 Web：

```bash
catmonitor daemon -c /etc/catmonitor/catmonitor.yaml

catmonitor-web \
  -addr 127.0.0.1:19322 \
  -snapshot-dir /var/lib/catmonitor/snapshot \
  -config /etc/catmonitor/catmonitor.yaml
```

Linux 本机检查：

```bash
curl -fsS http://127.0.0.1:19322/api/snapshot >/dev/null
curl -fsS http://127.0.0.1:19322/api/stress/config
```

从另一台 Windows 访问时使用 SSH 隧道，不要把 Web 改为公网监听：

```powershell
ssh -N `
  -o ExitOnForwardFailure=yes `
  -L 127.0.0.1:19322:127.0.0.1:19322 `
  root@server.example.com
```

浏览器入口：

```text
http://127.0.0.1:19322/
http://127.0.0.1:19322/stress/
```

Web 会显示执行前 profile、资产/MPI 预检、最近报告和最多 100 条历史作业。CLI 与
Web 共享报告和 Linux 文件锁；一个入口运行时，另一个入口提交会返回 busy/409。

如果健康概览显示“快照尚未就绪”，先检查 daemon 是否持续生成：

```bash
ls -l /var/lib/catmonitor/snapshot/snapshot*.json
pgrep -af 'catmonitor daemon'
```

仅启动 `catmonitor-web` 不会采集指标。

## 9. 统一容器安装入口

底层镜像和 Compose 仍按职责拆分，但用户不需要手工组合 overlay。先从源码树安装
统一命令及经过审核的 Compose 定义：

```bash
sudo make install-installer
catmonitor-install --help
```

该目标默认安装：

```text
/usr/local/sbin/catmonitor-install
/usr/local/lib/catmonitor/docker/*.yml
```

也可不安装，直接在源码树执行 `bash scripts/catmonitor-install ...`。安装器默认读取
`/etc/catmonitor/catmonitor.yaml`、`/opt/catmonitor/stress` 和
`/var/lib/catmonitor/stress`；只有非标准部署才需要显式传路径。

### 9.1 profile 与前置条件

| profile | 启动内容 | 额外前置条件 |
|---|---|---|
| `monitoring` | daemon、Web、DFeE | 完整主配置和本地 `catmonitor-generic` 镜像 |
| `cpu-stress` | monitoring + 受限 CPU Runner | 已安装 unix adapter、部署 manifest 和 manifest 指定的 runner 镜像 |
| `ascend-a2` | Ascend monitoring + CPU Runner + NPU Burn | A2 manifest、Ascend host 资产、已运行固定 NPU 容器 |
| `ascend-a3` | Ascend monitoring + CPU Runner + NPU Burn | A3 manifest、Ascend host 资产、已运行固定 NPU 容器 |

`monitoring` 的主 YAML 应保持 `stress.enabled: false`。`cpu-stress` 的主 YAML 必须把
`npu_burn.enabled` 保持为 `false`；该 profile 不挂载
Docker Socket，因此不会为了满足错误的 YAML 自动扩大权限。Ascend profile 才允许
在完成固定容器预检和显式权限确认后启用 NPU Burn。

先用只读计划检查最终选择。它会验证配置、镜像、manifest、adapter、Compose 模型，
Ascend profile 还会核对芯片代际、固定容器、镜像、驱动路径和 Docker Socket：

```bash
sudo catmonitor-install --profile monitoring --action plan
sudo catmonitor-install --profile cpu-stress --action plan
sudo catmonitor-install --profile ascend-a3 --action plan
```

`cpu-stress` 默认从已安装的
`manifests/stress-deployment-manifest.json` 读取经过审核的 CPU Runner 镜像名；显式
`--cpu-runner-image` 只能与 manifest 一致，避免静默切换到另一套 MPI/benchmark。

### 9.2 启动与管理

普通监控和 CPU Runner 可以直接启动：

```bash
sudo catmonitor-install --profile monitoring
sudo catmonitor-install --profile cpu-stress
```

`up` 只执行 Compose 启动、等待 CPU Runner 健康，然后运行无负载
`catmonitor stress doctor`；它不会构建/下载镜像、编译 benchmark、编辑 YAML、创建
NPU Burn 容器或运行任何压测。doctor 失败时应先修复部署，不要用
`--skip-doctor` 掩盖正式验收问题。

当前 Ascend profile 仍使用过渡期 `docker_exec` overlay。因为 Docker Socket 等价于
宿主机 root 权限，实际启动必须明确确认：

```bash
sudo catmonitor-install \
  --profile ascend-a3 \
  --acknowledge-root-docker-socket
```

A2 使用 `--profile ascend-a2`。如果 manifest 中的 `npu_chip_generation` 不匹配，
安装器会在启动前拒绝。该确认不会使 CPU-only 或 monitoring profile 获得 Socket。

常用生命周期命令：

```bash
sudo catmonitor-install --profile cpu-stress --action status
sudo catmonitor-install --profile cpu-stress --action doctor
sudo catmonitor-install --profile cpu-stress --action down
```

`down` 不删除 snapshot、history、CSV 或 Compose volume，并且即使配置/镜像/adapter
已经损坏或移走也应可执行。非标准目录和隔离 Docker 可使用 `--config`、
`--stress-root`、`--state-dir`、`--docker-socket`、`--docker-bin`；Web 端口冲突时可用
`--web-addr 127.0.0.1:19530` 覆盖，但安装器拒绝非回环监听。先执行
`--action plan` 查看解析结果。Ascend profile 会同时用 `--docker-socket` 选择宿主机
预检/Compose 所连接的 daemon，并把同一个 Socket 挂到控制容器的固定目标路径，
避免检查一个 daemon、运行时却调用另一个 daemon。

安装器内部固定组合以下五层，仍可供开发者审计，但不再是普通使用接口：基础监控、
只读主配置、Ascend 采集、CPU Runner、NPU Burn 临时 Socket。长期会用专用受限
NPU Runner 替代最后一层；在此之前不要给 Web 额外挂载其他 Docker Socket。

## 10. 验收与故障定位

每次新装或升级至少执行：

```bash
catmonitor stress doctor -c /etc/catmonitor/catmonitor.yaml -o table
catmonitor stress doctor -c /etc/catmonitor/catmonitor.yaml -o json

catmonitor stress --bench stream -c /etc/catmonitor/catmonitor.yaml -o table
catmonitor stress --bench hpcg  -c /etc/catmonitor/catmonitor.yaml -o table
catmonitor stress --bench hpl   -c /etc/catmonitor/catmonitor.yaml -o table
```

启用 NPU Burn 后再单独执行：

```bash
catmonitor stress --bench npu_burn \
  -c /etc/catmonitor/catmonitor.yaml \
  -o table
```

检查报告、历史和残留进程：

```bash
python3 -m json.tool /var/lib/catmonitor/stress/stress-latest.json
python3 -m json.tool /var/lib/catmonitor/stress/stress-history.json

pgrep -af 'stream_omp|xhpl|xhpcg|mpirun|mpiexec|ascend_npu_burn' || true
```

常见故障：

| 现象 | 优先检查 |
|---|---|
| `benchmark is disabled` | YAML 项目开关 |
| `deployment precheck failed` | `describe` 的资产、MPI ABI、容器和设备信息 |
| Web 按钮禁用 | `enabled`、`web_enabled`、回环监听、共享报告路径 |
| Web 有压测页但概览无数据 | daemon snapshot 是否启用、Web `-snapshot-dir` 是否一致 |
| 第二个作业返回 busy/409 | 正常互斥；等待当前 CLI/Web 作业结束 |
| CPU Runner 无法连接 | Unix Socket 挂载、owner/group、runner 是否存活 |
| HPL/HPCG 启动即失败 | launcher 与二进制 MPI ABI、动态库和工作目录 |
| NPU logical device 越界 | 容器内 `lspci` topology，不要直接套用设备节点或 Phy-ID |

发布前还应运行：

```bash
make test-stress
make audit-stress-release
```

需要 Docker 的容器 E2E 单独执行：

```bash
make test-stress-container-e2e
```
