# CATMonitor 镜像构建与发布开发者指南

本文面向 CATMonitor 开发者和发布人员，说明如何从同一份源码构建、验证并记录
Control 与 Stress workload 镜像。节点管理员如果只需要拉取镜像、生成配置并启动
服务，请从 [Docker 部署入口](README.md) 和对应节点指南开始。

构建镜像不会自动部署 CATMonitor，也不会执行 HPL、HPCG 或 NPU Burn 真实负载。
运行参数、设备映射和资源规模仍由部署阶段负责。

当前程序内部版本是 `0.3.5`，Linux/ARM64 Stress pre-release 标签是
`arm64-v0.3.5-stress`，目标发布线是 `v0.3.6`。pre-release 标签不能被描述为正式
`v0.3.6`。

## 1. 镜像矩阵与职责

| 镜像 | 用途 | 构建入口 |
|---|---|---|
| `catmonitor-generic` | 普通 Linux Control | `bash docker/build.sh generic` |
| `catmonitor-gpu` | NVIDIA Control | `bash docker/build.sh gpu` |
| `catmonitor-npu` | Ascend Control | `bash docker/build.sh npu` |
| `catmonitor-stress-cpu` | STREAM、HPL、HPCG workload | `scripts/stress/build_cpu_runner_image.sh` |
| `catmonitor-stress-npu` | Ascend NPU Burn workload | `scripts/stress/build_npu_burn_image.sh` |

Control 镜像同时包含 daemon/CLI、Web、DFeE。三个 Control 镜像只需选择与节点
硬件匹配的一张。A2 Full 模式虽然运行 5 个容器，但实际只使用 3 张镜像：

```text
catmonitor-npu
catmonitor-stress-cpu
catmonitor-stress-npu
```

`build_cpu_runner_image.sh` 的文件名保留了早期命名；它现在构建的是 V2 CPU
workload 镜像，不会创建 Unix Runner 服务或 workload 容器。

## 2. 构建前冻结源码身份

所有 RC 镜像必须来自同一个干净提交：

```bash
git status --short
export RC_SOURCE="$(git rev-parse HEAD)"
export RC_SHORT_SHA="$(git rev-parse --short=12 HEAD)"
export IMAGE_TAG='arm64-v0.3.5-stress'

printf 'RC_SOURCE=%s\nIMAGE_TAG=%s\n' "$RC_SOURCE" "$IMAGE_TAG"
```

`git status --short` 必须为空。当前镜像标签表明平台、程序版本和 Stress 用途；完整
提交号必须写入 manifest/release ledger。不得用不同源码覆盖已经发布的同名 tag；
源码变化后应按发布规则生成新的 revision tag，并重新完成验收。

构建机至少需要：

- Linux/amd64 或 Linux/arm64（NPU Control/workload 当前要求 Linux/arm64）；
- Docker Engine；
- Bash（CPU/NPU workload 构建）；
- 足够的 Docker data-root 与 build-root 空间；
- 构建 NPU Control 时可读取宿主机 Ascend driver；
- 构建 workload 时准备下文列出的受审输入。

Docker Compose 只用于部署，不是镜像构建前置条件。

## 3. 构建网络、镜像源与代理

不设置以下变量时，构建继续使用基础镜像的官方软件源和 Docker 默认构建网络：

```bash
export CATMONITOR_DOCKER_BUILD_NETWORK=default  # default、host 或 none

# Generic/Alpine；允许仓库根路径，但不要包含凭据或尾随斜杠
export CATMONITOR_ALPINE_MIRROR='https://mirror.example.com/alpine'

# GPU/NPU Control；只填写 origin，脚本会使用 <origin>/debian 和
# <origin>/debian-security
export CATMONITOR_DEBIAN_MIRROR='https://mirror.example.com'

# Go 模块下载策略
export GOPROXY='https://proxy.example.com,direct'
export GOSUMDB=off
```

受限网络可以临时设置 Docker 预定义代理变量：

```bash
export HTTP_PROXY='http://proxy.example.com:3128'
export HTTPS_PROXY='http://proxy.example.com:3128'
export NO_PROXY='127.0.0.1,localhost,registry.internal.example.com'
```

构建脚本只转发管理员已经设置的变量，不打印变量值，也不会把这些变量设置为最终
运行镜像的 `ENV`。代理 URL 可能包含凭据，不得写入仓库、manifest 或构建日志。
如果代理只监听宿主机回环地址，可在确认风险后使用 `host` 构建网络。

CPU workload 的 Debian 镜像源通过脚本参数传入：

```bash
--debian-mirror https://mirror.example.com
```

NPU workload 在线安装 `pciutils` 时会继承临时代理。完全离线时使用可重复的
`--pciutils-package <rpm-or-deb>` 提供经过审计的依赖闭包；提供离线包且未显式指定
网络时，构建脚本会选择 `--build-network none`。

构建完成后应清除仅用于本次构建的代理变量：

```bash
unset HTTP_PROXY HTTPS_PROXY NO_PROXY
```

## 4. 构建 Control 镜像

从仓库根目录执行：

```bash
bash docker/build.sh generic
bash docker/build.sh gpu
bash docker/build.sh npu
```

分别生成本地镜像：

```text
catmonitor-generic
catmonitor-gpu
catmonitor-npu
```

`npu` 模式分两步完成：先在 Go 容器中挂载宿主机
`/usr/local/Ascend/driver` 编译 DCMI 版本，再组装 Debian/glibc 运行镜像。构建只读取
driver，不会启动 NPU workload。

构建一张镜像后先检查平台、Image ID 和大小：

```bash
docker image inspect catmonitor-generic \
  --format 'id={{.Id}} os={{.Os}} arch={{.Architecture}} size={{.Size}}'

docker run --rm --network none \
  --entrypoint /usr/local/bin/catmonitor \
  catmonitor-generic version
```

GPU Control 可以使用相同的无硬件 `version` smoke。NPU Control 的二进制动态链接
Ascend driver，smoke 时需要挂载当前节点的 driver：

```bash
docker run --rm --network none \
  --entrypoint /usr/local/bin/catmonitor \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common \
  catmonitor-npu version
```

三个 Control 镜像都应包含：

```bash
docker run --rm --entrypoint /bin/sh catmonitor-generic -c '
  test -x /usr/local/bin/catmonitor
  test -x /usr/local/bin/web
  test -x /usr/local/bin/dfee
'
```

## 5. 构建 CPU workload 镜像

### 5.1 输入

构建人员必须准备并审计：

```text
stream.c
hpl-2.3.tar.gz
HPL.dat
hpcg-3.1.tar.gz
hpcg.dat
```

`HPL.dat` 中的进程网格和 `hpcg.dat` 的问题规模属于发布 profile，不能由构建脚本
替管理员猜测。构建使用同一 Debian bookworm build/runtime ABI，并在镜像内提供匹配的
MPI、OpenBLAS 与 benchmark 运行库。

### 5.2 构建

```bash
export CPU_IMAGE="catmonitor-stress-cpu:${IMAGE_TAG}"
export CPU_BUILD_ROOT="/srv/catmonitor-build/cpu-${RC_SHORT_SHA}"

bash scripts/stress/build_cpu_runner_image.sh \
  --image "$CPU_IMAGE" \
  --stream-src /srv/catmonitor-inputs/stream.c \
  --hpl-src /srv/catmonitor-inputs/hpl-2.3.tar.gz \
  --hpl-dat /srv/catmonitor-profiles/HPL.dat \
  --hpcg-src /srv/catmonitor-inputs/hpcg-3.1.tar.gz \
  --hpcg-dat /srv/catmonitor-profiles/hpcg.dat \
  --build-root "$CPU_BUILD_ROOT" \
  --manifest "$CPU_BUILD_ROOT/manifests/cpu-workload-image-manifest.json" \
  --build-network default \
  --jobs 16
```

如需 Debian mirror，将以下参数加入命令：

```bash
--debian-mirror https://mirror.example.com
```

脚本会编译、安装并验证镜像资产，生成包含输入 SHA-256、构建参数和 Image ID 的
manifest，但不会创建 workload 容器或运行 HPL/HPCG。

### 5.3 smoke

```bash
docker image inspect "$CPU_IMAGE" \
  --format 'id={{.Id}} os={{.Os}} arch={{.Architecture}} size={{.Size}}'

docker run --rm --network none \
  "$CPU_IMAGE" \
  /usr/local/bin/catmonitor-stress-exec \
  describe --benchmark stream --json
```

输出必须是合法 describe JSON，并确认 `stream` 资产可用。HPL/HPCG 真实负载属于实机
验收，不属于镜像 build smoke。

## 6. 构建 NPU workload 镜像

### 6.1 支持边界

当前 V2 发布声明只覆盖已经完成实机验收的 A2/Ascend910B4、CANN 8.3 profile。
A3 尚未完成 V2 实机验收，A5 未验证。镜像 build PASS 不能替代对应 SoC/CANN 的
doctor、拓扑、cancel 和真实 workload 验收。

正常 release 构建使用仓库 `third_party/ascend_npu_burn` 下的受审 bundled source。
`--source` 只用于上游更新、开发或兼容性验证，不应成为普通发布流程的手工输入。

### 6.2 builder/runtime 基础镜像

推荐拆分：

```text
builder base：完整 CANN/TBE/编译器/torch_npu
runtime base：精简 CANN runtime/Python/torch/torch_npu
```

两者必须具有一致的 CPU 架构、Python ABI、torch/torch_npu 和 CANN runtime ABI。
`--base-image` 共用单一大镜像只是兼容模式，不适合作为精简 release 镜像。

### 6.3 A2 构建示例

```bash
export NPU_BUILDER_BASE='<reviewed-a2-cann83-builder-image>'
export NPU_RUNTIME_BASE='<reviewed-a2-cann83-runtime-image>'
export NPU_IMAGE="catmonitor-stress-npu:${IMAGE_TAG}"
export NPU_BUILD_ROOT="/srv/catmonitor-build/npu-${RC_SHORT_SHA}"

bash scripts/stress/build_npu_burn_image.sh \
  --builder-base-image "$NPU_BUILDER_BASE" \
  --runtime-base-image "$NPU_RUNTIME_BASE" \
  --image "$NPU_IMAGE" \
  --compat-profile a2-cann83 \
  --patch scripts/stress/patches/ascend_npu_burn/a2-cann83.patch \
  --build-driver-lib-dir /usr/local/Ascend/driver/lib64 \
  --build-network default \
  --build-root "$NPU_BUILD_ROOT" \
  --manifest "$NPU_BUILD_ROOT/manifests/npu-burn-image-manifest.json"
```

`--build-driver-lib-dir` 只允许指向专用 driver `lib64` 目录，并且只用于一次性 builder
stage；driver 不会被复制进最终 runtime 镜像。运行时仍由节点挂载 driver 和设备。

构建会检查：

- bundled source/metadata/checksum 一致性；
- 补丁能在隔离源码副本上干净应用；
- HAL、torch、torch_npu、TBE 预检；
- wheel 构建、安装和包版本；
- builder/runtime ABI；
- `pciutils/lspci`、入口文件和 workload executable。

它不会创建、启动、停止容器，也不会运行真实 NPU workload。

### 6.4 构建后验证

```bash
docker image inspect "$NPU_IMAGE" \
  --format 'id={{.Id}} os={{.Os}} arch={{.Architecture}} size={{.Size}} labels={{json .Config.Labels}}'

docker run --rm --entrypoint /bin/bash "$NPU_IMAGE" -lc '
  command -v lspci
  test -x /usr/local/bin/catmonitor-stress-exec
  test -x /usr/local/bin/catmonitor-npu-burn
  test -x /usr/local/bin/catmonitor-npu-burn-preflight
'
```

完整 CANN、设备、PCI topology 和 workload describe 必须在部署时按节点指南挂载 driver
与 `/dev/davinciN` 后验证，不能从无设备 build 容器推断。

## 7. Manifest、provenance 与发布记录

每次 RC 构建至少记录：

```text
Git commit（完整 SHA）
RC tag
镜像 ref
本地 Image ID
OS/architecture/size
输入文件 SHA-256
builder/runtime base image ref 与 Image ID
生成的 build manifest
推送后的 registry digest
```

CPU/NPU 构建脚本会生成机器可读 manifest。Control 镜像和基础镜像身份仍应写入本次
release ledger；不要假设所有信息都能从 CPU manifest 推导出来。

仓库提供机械审计入口：

```bash
bash scripts/stress/audit_stress_release.sh \
  --cpu-manifest "$CPU_BUILD_ROOT/manifests/cpu-workload-image-manifest.json" \
  --npu-manifest "$NPU_BUILD_ROOT/manifests/npu-burn-image-manifest.json" \
  --require-runtime-manifests
```

该命令检查仓库材料和 manifest 完整性，不生成 SBOM，也不替代第三方许可证与基础镜像
合规审计。相关边界见
[OSS_RELEASE_AUDIT.md](../features/stress/OSS_RELEASE_AUDIT.md) 和
[THIRD_PARTY_NOTICES.md](../features/stress/THIRD_PARTY_NOTICES.md)。

## 8. 可选的独立 Build Docker daemon

大型 CPU/NPU 镜像构建会占用较多 Docker layer 和缓存。生产 Docker data-root 位于小
分区时，建议由管理员准备 data-root 位于容量充足独立文件系统的 build daemon：

```bash
export DOCKER_HOST='unix:///srv/catmonitor-build/docker/docker.sock'
docker info --format 'DockerRootDir={{.DockerRootDir}}'
df -h /srv/catmonitor-build
```

路径只是示例，不是产品固定目录。开始构建前必须确认当前 `DOCKER_HOST` 指向预期
daemon；构建结束后不要让发布或生产部署意外继续使用 build daemon。

```bash
unset DOCKER_HOST
docker info --format 'DockerRootDir={{.DockerRootDir}}'
```

独立 daemon 只解决存储和构建隔离，不改变镜像内容。将镜像转移到发布 daemon 时需用
registry 或受控的 `docker save/load`，并重新核对 Image ID 与平台。

## 9. 镜像标签

当前 Linux/ARM64 Stress 集成标签：

```text
arm64-v0.3.5-stress
```

含义：

```text
arm64   = Linux/ARM64 构建
v0.3.5 = 当前 owner 版本
stress  = Stress 集成候选，不是通用 0.3.5 镜像
```

推荐流程：

```text
clean source
→ build 五张同源 Stress 镜像
→ image smoke
→ Compose/docker run matrix
→ 实机 doctor 与 workload acceptance
→ push 不可变 Stress tag
→ fresh pull 验证
→ owner 批准后决定正式版本和 tag
```

不要把该构建标记为普通 `:v0.3.5` 或 `:latest`。`Image ID` 是本地镜像配置身份；
registry digest 是远端 manifest 身份，二者概念不同，发布记录必须分别保存。

推送后至少验证：

```bash
export PUBLISHED_IMAGE='ghcr.io/spike677/catmonitor-npu:arm64-v0.3.5-stress'
docker manifest inspect "$PUBLISHED_IMAGE" >/dev/null
docker pull "$PUBLISHED_IMAGE"
docker image inspect "$PUBLISHED_IMAGE" \
  --format 'id={{.Id}} os={{.Os}} arch={{.Architecture}}'
```

## 10. 构建与发布门禁

提交发布候选前运行：

```bash
make test-monitoring-compat
make test-stress
make test-stress-race
go vet ./...
git diff --check
```

还必须执行 Linux/Windows build、Compose matrix，以及
[Stress 测试指南](../features/stress/STRESS_TEST_GUIDE.md) 中与改动范围对应的 fixture、
容器和实机验收。镜像 build PASS 不等于 release acceptance PASS。

## 11. 用户与发布职责边界

| 操作 | 普通用户/节点管理员 | 开发者/发布人员 |
|---|---:|---:|
| `docker pull` | ✓ | ✓ |
| 生成节点配置 | ✓ | ✓ |
| Compose / `docker run` | ✓ | ✓ |
| 构建 Control | 可选 | ✓ |
| 构建 CPU workload | — | ✓ |
| 构建 NPU workload | — | ✓ |
| 设置构建 mirror/proxy | — | ✓ |
| 生成和审计 manifest/SBOM | — | ✓ |
| 制作 RC/Final tag | — | ✓ |
| 记录 Registry digest | — | ✓ |

普通用户的主链应保持为：

```text
pull → generate → run → doctor
```

开发与发布链为：

```text
source → build → validate → tag → push → fresh pull
```
