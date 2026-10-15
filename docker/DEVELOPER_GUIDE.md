# CATMonitor 镜像构建与发布开发者指南

本文面向 CATMonitor 开发者和发布人员，说明如何从同一份源码构建、验证并记录
Control 与 Stress workload 镜像。节点管理员如果只需要拉取镜像、生成配置并启动
服务，请从 [Docker 部署入口](README.md) 和对应节点指南开始。

构建镜像不会自动部署 CATMonitor，也不会执行 HPL、HPCG 或 NPU Burn 真实负载。
运行参数、设备映射和资源规模仍由部署阶段负责。

当前已发布候选的源码、镜像身份和有限验收范围统一在
[交付指南](STRESS_JOB_RELEASE_GUIDE.md) 维护；本页使用构建模板，不把旧 pre-release
或新开发产物当成正式 release。程序版本和正式 tag 由项目 owner 管理。

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
export IMAGE_TAG="arm64-v0.3.6-stress-${RC_SHORT_SHA}"

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

### 3.1 受限网络和企业 CA

Git/curl 使用宿主代理；`docker pull` 由 daemon 发起，宿主 `export` 不保证 daemon
可联网。Go 模块、Docker build 和系统包下载也各有独立网络/CA 边界。不要打开
`set -x` 或输出代理凭据。企业 CA 应分别安装到需要联网的宿主、daemon 和 build
stage（必要时 runtime）；使用获准 CA 基础镜像/安装层，不关闭 TLS 验证，也没有
虚构的通用 `--ca` 参数。mirror 和代理只由管理员临时选择，不写入用户 YAML。

### 3.2 已有镜像、离线输入与只读检查

节点已有获准镜像时直接 inspect，不强制重复 pull；离线可用 `docker save/load` 传递
获准发行镜像，核对 Image ID/平台。build-root 放另一块盘不迁移 Docker layers。
完全离线构建必须准备所有 FROM（含 Go builder）、Go 模块、源码/patch、wheel 和系统
包闭包。NPU 缺 lspci 时重复传 `--pciutils-package /absolute/package.rpm` 或 `.deb`，
匹配基础 OS/架构和动态库，不挂宿主 lspci。只有真实禁网构建成功才能写 OFFLINE_BUILD=PASS。
CPU Dockerfile 仍通过 APT 安装工具链，**CPU 完整离线构建未实现/验收**；应分发获准
预构建 CPU 镜像。单个依赖清单检查不证明闭包完整。

需要预先核对站点输入时，将 [site.env 模板](examples/site.env.example) 复制到仓库外，
填入真实镜像 ID、Docker endpoint/data-root、独立 config/state/output 路径与授权设备：

```bash
set -a
. /absolute/path/to/site.env
set +a
export DOCKER_HOST="$DOCKER_ENDPOINT"
cd "$SOURCE_DIR"
python3 scripts/stress/check_site_inputs.py
```

只读检查不会 pull/build/run 或修改 daemon；失败不得继续部署。源码 ZIP 可由可信交付
环节提供 `source-identity.json`（`commit` + `files` 相对路径/SHA-256 字典），须覆盖
全部交付文件，不能由待验目录临时自证。`--offline-inputs /path/inputs.json` 可检查
声明的 `root/files/images`；这不是自动下载器/安装器。host network 的端口冲突必须在
目标 daemon 所在网络空间检查，不能仅用另一 project 名或 `-p` 声称隔离。

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

历史 A2/CANN 8.3 profile 与当前完整参数候选是不同软件栈。当前 A2/CANN 8.5.1
候选的有限实测范围见交付指南；A3 完整硬件验收 PENDING，A5 未验证。
镜像 build PASS 不能替代对应 SoC/CANN 的
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
export NPU_BUILDER_BASE='<approved-pinned-a2-builder-image>'
export NPU_RUNTIME_BASE='<approved-pinned-a2-runtime-image>'
export NPU_IMAGE="catmonitor-stress-npu:${IMAGE_TAG}"
export NPU_BUILD_ROOT="/srv/catmonitor-build/npu-${RC_SHORT_SHA}"

bash scripts/stress/build_npu_burn_image.sh \
  --builder-base-image "$NPU_BUILDER_BASE" \
  --runtime-base-image "$NPU_RUNTIME_BASE" \
  --image "$NPU_IMAGE" \
  --compat-profile a2-upstream-model-sdc-memory-job-v1 \
  --patch scripts/stress/patches/ascend_npu_burn/portable-runtime-v1.patch \
  --patch scripts/stress/patches/ascend_npu_burn/a2-upstream-base.patch \
  --patch scripts/stress/patches/ascend_npu_burn/model-sdc-v1.patch \
  --patch scripts/stress/patches/ascend_npu_burn/model-memory-v1.patch \
  --patch scripts/stress/patches/ascend_npu_burn/job-parameters-v1.patch \
  --enable-model-sdc \
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

构建内部会使用临时 build/validation 容器，但不会部署或停止现有服务，也不运行真实
NPU workload。开启模型能力要求 runtime base 已准备固定版本的 Transformers 和依赖，
真实 GPT2Model/LlamaModel/Custom Ops 导入必须通过；不在运行容器里临时覆盖包。
`model-sdc` compatibility profile 默认开启上述模型导入检查，`--enable-model-sdc`
保留为显式兼容入口。完整 model/job profile 拒绝 `--disable-model-sdc`，不能产出
名义上完整但缺少模型依赖的镜像。部署 generator 默认开放模型/参考入口不替代构建校验。

A3 使用 `portable-model-sdc-memory-job-v1`，顺序为 portable-runtime → model-sdc →
model-memory → job-parameters，**不加 A2 补丁**。完整可执行示例只在
[交付指南第 2 节](STRESS_JOB_RELEASE_GUIDE.md#2-a3-构建补丁契约a2-用户和使用已发布镜像者跳过)
维护。其他历史 profile 的适用边界见 [补丁说明](../scripts/stress/patches/ascend_npu_burn/README.md)。
构建保留完整 bundled 树和 base/A3 配置；带兼容补丁的产物不能标成 `profile=none`。

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

开发候选应使用架构、目标版本、用途及不可变提交后缀，例如：

```text
arm64-v0.3.6-stress-<shortsha>
```

含义：

```text
arm64   = Linux/ARM64 构建
v0.3.6 = 目标版本线，不代表已获正式发布授权
stress  = Stress 集成候选
shortsha = 本批源码身份；不能代替完整 commit / Image ID / digest
```

推荐流程：

```text
clean source
→ build 所选硬件需要的 Control 与 workload 镜像
→ image smoke
→ Compose/docker run matrix
→ 实机 doctor 与 workload acceptance
→ push 不可变 Stress tag
→ fresh pull 验证
→ owner 批准后决定正式版本和 tag
```

不要擅自标记为普通版本或覆盖 `:latest`。`Image ID` 是本地镜像配置身份；
registry digest 是远端 manifest 身份，二者概念不同，发布记录必须分别保存。

推送后至少验证：

```bash
: "${PUBLISHED_IMAGE:?set the image ref published in this run}"
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

本功能同时改变 Control 请求/报告和 CPU/NPU workload 协议，需构建三张匹配候选镜像，
不能仅更新 Web 后宣称参数已被执行。不自动更新已发布 registry 标签或正在运行的容器。
精确补丁链在第 6 节维护。CPU/NPU 根文件系统保持只读，运行期临时配置由 executor 写入
有界 tmpfs；模型参数/镜像变化须匹配新参考。构建/ABI 检查不等于官方配套认证。

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
