# 按作业参数候选：内源 A2 / A3 获取、部署和验收

统一源码分支：`spike677/CATMonitor:codex/stress-job-parameters`；功能基线：
`f636dd000995d7693107197f1310151c29ba4941`，已整合 develop `0554f54`。
默认完整功能与 Compose 优化源码提交为 `df9176aad841f626d4fc98f45c5400b31e43a1e2`。
本批镜像构建提交固定为 `4630b98227a31fc56a679c4785e35abd98d05d4e`；
后续本页部署对象说明是纯文档更新，不声称镜像因此重新构建。**当前优先交付和验证 A2。**
本指南是候选交付，不是正式 release 或 A3 配套认证。A3 真实硬件验收为 **PENDING**；
构建/import/ABI 检查和 A2 实测不能替代 A3 workload 结果。

## 当前源码的默认能力与管理员限制

主仓 Web 默认监听 `:19322`；原 `--enable-web` 仅控制 Stress Run/Cancel，
不是启动整个 Web 的开关。当前 generator 显式部署 Stress 时默认 `web_enabled: true`，
保留 `--enable-web` 兼容入口，新增 `--disable-web` 供管理员关闭 Web 操作。
Monitoring-only 的全局配置仍为 `stress.enabled: false`，不需要 Docker socket。

指定 NPU image 后默认开放当前映射范围内全部 runtime logical ID、GPT3/Llama2
以及单设备参考生成。默认选中仍是 logical 0 / matmul，不全选、不自动执行或生成参考。
算子始终带 `--sdc_detect`；模型必须先有独立、完整且参数/软件身份匹配的参考。
模型入口开放不是依赖、显存、参考或硬件验证通过；不支持的代际/dtype 不自动替换。

完整模型 `*-model-sdc-*` compatibility profile 构建时默认启用模型依赖导入检查；
补丁链及准备好的依赖仍必须显式提供，缺失即失败。旧镜像不能只设置环境变量补齐功能。
本次默认值来自 Git 中的 generator，不会被 `docker pull` 自动写入已有 YAML。
更新源码后重新生成、审查配置，再只重建/重启属于本项目的 Controller；容器权限或
环境发生变更时才重建相应 workload，不改其他项目或已有业务。

| 管理员需求 | generator 参数 |
| --- | --- |
| 只授权 logical 0 | `--npu-allowed-devices 0` |
| 只授权指定映射设备 | `--npu-allowed-devices 0,1`（本节点存在这些 logical ID 时） |
| 只开放 GPT3 | `--npu-allowed-models gpt3` |
| 关闭全部模型，仅保留算子 | `--npu-allowed-models none` |
| 允许模型比较，但禁止生成新参考 | `--deny-npu-reference-generation` |
| 关闭 Web Run/Cancel，保留 CLI | `--disable-web` |
| 减少映射/权限 | `--npu-device-mode single --npu-device-nodes <host-id>`；仅映射明确授权设备，须通过映射验证 |

设备授权不代表空闲。容器 readiness/普通 doctor 可以在业务存在时检查依赖，
每次 Run 仍在 Controller 与 workload 两层强制检查空闲，不杀业务、不自动排队。
当前 Run/Cancel 无认证/RBAC，只能在可信管理网络开放。全局 job 上限仍默认 2h，
需要更久时明确配置 `--npu-job-timeout 8h`；内部同步 300s 不是整个任务时长。
线程预算仍为 1；readonly root、完整 base/A3 JSON 和按 Job 冻结参数保持不变。

## 1. 本批镜像和源码

三类镜像不是三套控制面。Full 使用一张 Control（daemon/Web/DFeE）、一张 CPU、
一张 NPU，共五个容器。NPU 因运行软件栈不同提供 A2/A3 两个标签，不混用。

| 角色 | 候选 registry ref |
| --- | --- |
| ARM64 Ascend Control | `ghcr.io/spike677/catmonitor-npu:arm64-v0.3.6-stress-defaults-20261015-r2` |
| ARM64 CPU workload | `ghcr.io/spike677/catmonitor-stress-cpu:arm64-v0.3.6-stress-defaults-20261015-r2` |
| A2 / CANN 8.5.1 NPU workload | `ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.6-stress-defaults-a2-cann851-20261015-r2` |
| A3 / CANN 9.1 NPU workload | `ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.6-stress-job-a3-cann91-20261015-r1` |

这些是本批候选引用，只有交付记录中的 push/pull gate 通过才算已发布。不能仅凭标签
推断内容；发布人员应另提供本批 `RELEASE.json` 的实际 source commit、Image ID、digest、
base 和 profile。管理员以该记录固定 checkout 和镜像 digest。不要覆盖旧 stable/latest。

### 1.1 已发布的 A2 三件套（2026-10-15）

当前 R2 三个标签已分别完成 push、GHCR manifest/config、pull 后 Image ID 一致性，
以及匿名 registry 读取验证。**本轮没有重建镜像**：部署默认值与 Compose 改动只在 Git
的 generator/launcher，三张执行镜像原样复用已验收的 R1 内容，Image ID/digest 与下面
记录完全相同。R1 标签及已有容器均保留，没有覆盖 stable/latest。

```bash
docker pull ghcr.io/spike677/catmonitor-npu:arm64-v0.3.6-stress-defaults-20261015-r2
docker pull ghcr.io/spike677/catmonitor-stress-cpu:arm64-v0.3.6-stress-defaults-20261015-r2
docker pull ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.6-stress-defaults-a2-cann851-20261015-r2
```

只 pull R2 不会改变旧 YAML 的授权、权限或端口。使用上述源码提交或包含它的分支，
重新生成、审查后重建属于本项目的容器。R2 发布没有重新运行硬件 workload，不能新增
“完整算子/模型、A3 或隔夜实测通过”的声明。

下列三个 digest 已通过 GHCR manifest/config、`docker pull` 后 Image ID 一致性、
匿名 registry 读取验证，平台均为 `linux/arm64`。回拉复用了当前节点的已有层，
不是“全新节点从零下载”的证据。优先用 digest 固定部署输入：

```bash
docker pull ghcr.io/spike677/catmonitor-npu@sha256:02c3dc59e2b12b0fc499ed84e6556b63d69ed1951b863f1602d3efc7045fef5b
docker pull ghcr.io/spike677/catmonitor-stress-cpu@sha256:8ceb049bdc5da4c675199277711eeb7d0a84f16a0834239328a8b5a41733d425
docker pull ghcr.io/spike677/catmonitor-stress-npu@sha256:05e84e660af248ec5ec0f86184dff9d2adf38abe97690f2c6a923b220d64e229
```

| A2 角色 | 预期 Image ID |
| --- | --- |
| Control | `sha256:9858f14b8b562a21e0429e222064998e260085dc9d58c07519879b462b806acc` |
| CPU | `sha256:758577a1677ff407d126635ee113ab35b5d99302aab10b3aefcc08c37033259a` |
| NPU | `sha256:5b97aee15006b1f46ae1d9fca45dfa4dfcb70f9bb5e4563ce7f0cfd8b9576bd2` |

124 A2 节点已验证 doctor 4/4、STREAM/HPL/HPCG 小规模改参作业，以及 logical 0、1
的 matmul FP16 小规模作业（本轮 CSV 2/2、检测错误 0）。这不是完整算子矩阵、
完整模型训练或隔夜可靠性认证，也不是 A3 的硬件验收。
NPU 镜像本地未压缩大小约 15 GiB；迁入前检查实际存储，不宣称已完成极限瘦身。

### 1.2 获取与选择代际

下面命令都在获准的 **Linux ARM64 宿主机 Bash** 执行。不需要安装器；Compose 可选。
只带入公开源码和获准分发的镜像，不向公开仓库上传内源数据或凭据。

```bash
git -c http.version=HTTP/1.1 clone --branch codex/stress-job-parameters \
  https://github.com/spike677/CATMonitor.git
cd CATMonitor
git rev-parse HEAD
git status --short
git merge-base --is-ancestor f636dd000995d7693107197f1310151c29ba4941 HEAD

# 获得发布人员给出的完整 SOURCE_COMMIT 后固定，不任意选择新 develop。
# git checkout --detach "$SOURCE_COMMIT"
export CONTROL_IMAGE=ghcr.io/spike677/catmonitor-npu:arm64-v0.3.6-stress-defaults-20261015-r2
export CPU_IMAGE=ghcr.io/spike677/catmonitor-stress-cpu:arm64-v0.3.6-stress-defaults-20261015-r2
export NPU_CHIP_GENERATION=A2  # A3 节点显式改为 A3；不根据设备个数猜代际。
case "$NPU_CHIP_GENERATION" in
  A2)
    export NPU_IMAGE=ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.6-stress-defaults-a2-cann851-20261015-r2
    export CANN_ENV_SCRIPT=/usr/local/Ascend/ascend-toolkit/set_env.sh
    export NPU_COMPAT_PROFILE=a2-upstream-model-sdc-memory-job-v1 ;;
  A3)
    export NPU_IMAGE=ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.6-stress-job-a3-cann91-20261015-r1
    export CANN_ENV_SCRIPT=/usr/local/Ascend/cann-9.1.0/set_env.sh
    export NPU_COMPAT_PROFILE=portable-model-sdc-memory-job-v1 ;;
esac

docker pull "$CONTROL_IMAGE"
docker pull "$CPU_IMAGE"
docker pull "$NPU_IMAGE"
docker image inspect "$CONTROL_IMAGE" "$CPU_IMAGE" "$NPU_IMAGE" \
  --format '{{json .RepoTags}} {{.Id}} {{.Os}}/{{.Architecture}} {{json .RepoDigests}}'
docker image inspect "$NPU_IMAGE" \
  --format '{{index .Config.Labels "io.catmonitor.npu-burn.compat-profile"}}'
```

预期全部 `linux/arm64`；profile 必须与 `$NPU_COMPAT_PROFILE` 一致。
A2 优先只拉 A2 三件套，不需要下载 A3 镜像。A2 的 CANN 为 8.5.1、torch_npu 为
2.9.0.post1+gitee7ba04；闭源 A2 节点仍需验证宿主驱动兼容性，不能因为同是 A2 就跳过门禁。
若 registry 要求认证，由管理员通过 `docker login ... --password-stdin` 登录，不把
token 写入 Git/命令行参数/本文。完全离线可在获准联网机器 `docker save` 三张镜像、
压缩后传入并 `docker load`，核对 ID 与交付记录；不是从未知旧容器提取执行包。

## 2. A3 构建补丁契约（A2 用户和使用已发布镜像者跳过）

顺序固定：`portable-runtime-v1.patch` → `model-sdc-v1.patch` →
`model-memory-v1.patch` → `job-parameters-v1.patch`。
profile 为 `portable-model-sdc-memory-job-v1`；不应用 A2 的生成代际/缩减补丁。
完整 bundled 上游目录、base/A3 JSON 保留，实际 schema 和补丁哈希写入构建 manifest。

```bash
# 填写本环境获准且固定的 builder/runtime base、构建目录。
: "${NPU_BUILDER_BASE:?set the approved pinned builder image}"
: "${NPU_RUNTIME_BASE:?set the approved pinned runtime image}"
: "${NPU_BUILD_ROOT:?set a dedicated build directory}"
bash scripts/stress/build_npu_burn_image.sh \
  --builder-base-image "$NPU_BUILDER_BASE" --runtime-base-image "$NPU_RUNTIME_BASE" \
  --image ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.6-stress-job-a3-cann91-20261015-r1 \
  --compat-profile portable-model-sdc-memory-job-v1 \
  --patch scripts/stress/patches/ascend_npu_burn/portable-runtime-v1.patch \
  --patch scripts/stress/patches/ascend_npu_burn/model-sdc-v1.patch \
  --patch scripts/stress/patches/ascend_npu_burn/model-memory-v1.patch \
  --patch scripts/stress/patches/ascend_npu_burn/job-parameters-v1.patch \
  --enable-model-sdc --build-root "$NPU_BUILD_ROOT"
```

本批 A3 候选使用 CANN 9.1.0、Python 3.12、torch 2.10.0、torch_npu 2.10.0.post6，
并准备真实 Transformers 模型依赖。这不代表任意驱动/CANN ABI 均兼容。
Control/CPU 构建继续使用 [开发者指南](DEVELOPER_GUIDE.md) 的正式入口；
不覆盖运行容器内 binary，不借用 A2 wheel、参考或修改 profile 为 none。

## 3. 宿主预检与安全 Gate

```bash
uname -m
docker version
docker info --format '{{.DockerRootDir}}'
df -h / /home
npu-smi info
ls -l /dev/davinci[0-9]*
ss -lntp | grep -E ':(19320|19321|19322|19323|9333)([[:space:]]|$)' || true
docker ps -a --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
```

有已有 CATMonitor/同名容器/端口冲突时 **STOP，不停止他人服务**。下面是新节点正式名称
模板；并行验收需先规划独立端口、容器名、目录和 generator 中的 workload 名称。
Web/DFeE 在端口占用时会递增，但 Metrics、faultsub、Exporter 不自动选端口。
本指南不执行自动 rm/rename，也不在登录 shell 用 `exit` 强制断开 SSH。

只读根保持。daemon 的 host/privileged 采集方式沿用主仓；Docker socket 只给 daemon。
以下 full NPU 示例显式选择管理员授权的 `legacy-privileged`，允许有业务时创建/检查
容器，但**不允许有业务时下发 Burn**。特权容器不是设备安全隔离；业务设备不获准暴露时
不要执行该模板，应使用 [受限单设备路径](README-npu.md#66-显式单设备兼容候选硬件验收-pending)。
禁止为了预检通过扩大设备映射或杀业务。Run/Cancel 无认证/RBAC，只对可信网络开放。

## 4. 一次生成 Full 配置

本节使用生成器的完整功能默认值，不重复传 CPU 网格、权限、选卡授权或 Web 开关。
HPL 默认 N=4096、NB=256、P=1、Q=1、MPI=1；原始镜像 HPL.dat 保留不改。
旧默认曾保留 HPL.dat 的 4×2 网格却固定 MPI=1，导致预检拒绝；不是 Compose 自动
选择了 4×2。旧源码 ZIP 需要更新生成器，或临时显式加 `--hpl-profile small`。
CANN 默认使用镜像自动发现；仅在管理员确有需要时显式覆盖镜像内脚本路径。

2026-10-15 在 124 使用现有 R2 CPU 镜像和修复后的默认生成配置，独立 CPU 容器实际
HPL 4096/256/1×1、MPI=1 完成，残差检查 1 项通过、0 项失败。该结果不是 85 已部署、
整套 Controller/Web 重验或满机 HPL 性能认证。

```bash
export GENERATED_DIR=/etc/catmonitor/generated-job-stress
export STRESS_STATE=/var/lib/catmonitor/job-stress
export NPU_OUTPUT=/var/lib/catmonitor/job-stress-npu-output
install -d -m 0750 "$GENERATED_DIR" "$STRESS_STATE" "$NPU_OUTPUT"

bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$GENERATED_DIR" --control-image "$CONTROL_IMAGE" \
  --cpu-image "$CPU_IMAGE" \
  --npu-image "$NPU_IMAGE" --npu-output-dir "$NPU_OUTPUT" \
  --npu-chip-generation "$NPU_CHIP_GENERATION"

python3 -m json.tool "$GENERATED_DIR/stress-profile.json"
```

CPU 初始采用小规模功能配置，不代表性能压测规模。NPU 默认授权当前映射内全部 logical ID，
但只默认选择 0，不自动全卡运行。完整 PCI、节点 ID、
runtime logical ID 不混同，不把 sparse 节点编号直接当逻辑编号，也不硬编码 16/0..7。
不默认挂外部实验配置；模型规模变化优先用本次作业参数，参考必须重新匹配。

管理员显式覆盖示例，按需添加到生成命令，不是普通部署的必填参数：

```bash
# 模型内存策略：--npu-model-memory-mode checkpointed
# 作业上限延长：--npu-job-timeout 8h
# 使用原始 HPL.dat（已核实 P=4/Q=2）：--hpl-profile image --hpl-processes 8
```

## 5. 启动方式：选择其一

### 5.1 Compose（可选）

```bash
export CATMONITOR_IMAGE="$CONTROL_IMAGE"
export CATMONITOR_STRESS_STATE_DIR="$STRESS_STATE"
bash docker/compose.sh --hardware npu --mode full --generated "$GENERATED_DIR" config
bash docker/compose.sh --hardware npu --mode full --generated "$GENERATED_DIR" up
bash docker/compose.sh --hardware npu --mode full --generated "$GENERATED_DIR" \
  --public-host '<node-address>' urls
```

薄入口优先 Compose v2；legacy Compose 需要 Python3/PyYAML。它不安装、不构建，
只组合主仓 canonical 定义和 generator 输出。`--public-host` 不建立 NAT 或开防火墙。
legacy NPU 使用只读源码 PID-1 helper，不要求宿主 docker-init；旧 docker-py 的
pids_limit 会转为整数，具体在第 11 节。直接运行新版 YAML 或 `docker run --init`
不等同于经过此兼容入口。

### 5.2 不用 Compose：五个 docker run

先执行第 4 节。以下只读取 generator 的 env/JSON，不需 Compose。

```bash
(
set -euo pipefail
NPU_DEVICE_ARGS=()
while IFS= read -r id; do
  test -c "/dev/davinci$id" || { printf 'Missing device: %s\n' "$id" >&2; break; }
  NPU_DEVICE_ARGS+=(--device "/dev/davinci$id:/dev/davinci$id")
done < <(python3 -c 'import json,sys; p=json.load(open(sys.argv[1])); print("\n".join(map(str,p["npu"]["host_device_ids"])))' "$GENERATED_DIR/stress-profile.json")
# 比较数组长度，拒绝部分映射后继续启动。
EXPECTED_COUNT=$(python3 -c 'import json,sys; print(len(json.load(open(sys.argv[1]))["npu"]["host_device_ids"]))' "$GENERATED_DIR/stress-profile.json")
test "$EXPECTED_COUNT" -gt 0 && test "${#NPU_DEVICE_ARGS[@]}" -eq "$((2 * EXPECTED_COUNT))"

NPU_SMI=$(command -v npu-smi)
test -n "$NPU_SMI" && test -f "$NPU_SMI"
for volume in cm-job-snapshot cm-job-data cm-job-straggler cm-job-control cm-job-csv cm-job-cpu-state cm-job-npu-state; do
  docker volume create "$volume"
done

docker run -d --name catmonitor-stress-cpu --restart unless-stopped \
  --read-only --network none --cap-drop ALL \
  --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
  --cap-add SETGID --cap-add SETPCAP --cap-add SETUID --cap-add SYS_NICE \
  --security-opt no-new-privileges:true --pids-limit 4096 --shm-size=16g \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m \
  --env-file "$GENERATED_DIR/cpu-workload.env" \
  -v cm-job-cpu-state:/var/lib/catmonitor/stress "$CPU_IMAGE"

docker run -d --name catmonitor-stress-npu --restart unless-stopped \
  --init --runtime runc --privileged --read-only --network none \
  "${NPU_DEVICE_ARGS[@]}" --device /dev/davinci_manager:/dev/davinci_manager \
  --device /dev/devmm_svm:/dev/devmm_svm --device /dev/hisi_hdc:/dev/hisi_hdc \
  --env-file "$GENERATED_DIR/npu-workload.env" \
  --tmpfs /tmp:rw,nosuid,nodev,size=256m \
  --tmpfs /opt/catmonitor/npuburn-home:rw,nosuid,nodev,size=1g,mode=0750 \
  -v cm-job-npu-state:/var/lib/catmonitor/stress \
  -v "$NPU_OUTPUT:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output" \
  -v /sys/bus/pci:/sys/bus/pci:ro \
  -v /usr/local/Ascend/driver/lib64:/usr/local/Ascend/driver/lib64:ro \
  -v /usr/local/Ascend/driver/version.info:/usr/local/Ascend/driver/version.info:ro \
  -v /etc/ascend_install.info:/etc/ascend_install.info:ro \
  -v /usr/local/dcmi:/usr/local/dcmi:ro \
  -v "$NPU_SMI:/usr/local/bin/npu-smi:ro" "$NPU_IMAGE"

docker run -d --name catmonitor --restart unless-stopped \
  --privileged --network host --pid host \
  -v /:/host:ro -v /etc/os-release:/etc/os-release:ro \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -v "$NPU_SMI:/usr/local/sbin/npu-smi:ro" \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common \
  -v "$GENERATED_DIR/catmonitor-stress.yaml:/etc/catmonitor/catmonitor.yaml:ro" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$STRESS_STATE:/var/lib/catmonitor/stress" \
  -v cm-job-snapshot:/var/lib/catmonitor/snapshot -v cm-job-data:/var/lib/catmonitor/data \
  -v cm-job-straggler:/var/lib/catmonitor/straggler -v cm-job-control:/run/catmonitor "$CONTROL_IMAGE"

docker run -d --name catmonitor-web --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/web \
  -v cm-job-snapshot:/var/lib/catmonitor/snapshot:ro -v cm-job-control:/run/catmonitor:ro \
  "$CONTROL_IMAGE" -addr=:19322 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -control-socket=/run/catmonitor/control.sock

docker run -d --name catmonitor-dfee --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/dfee \
  -v cm-job-snapshot:/var/lib/catmonitor/snapshot:ro -v cm-job-csv:/var/lib/catmonitor/csv \
  "$CONTROL_IMAGE" -addr=:19323 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled -exporter-port=9333 -csv=disabled -csv-dir=/var/lib/catmonitor/csv
)
```

设备数组检查失败时子 shell 停止，不执行后续 docker run，也不退出登录 shell。
全程保留生成结果和 `docker inspect`。
容器内 CANN 自带，仅只读挂宿主驱动；Web/DFeE 无 Docker socket、workload 内部目录。

## 6. Monitoring、预检与网页

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19320/metrics >/dev/null
docker exec catmonitor test -s /var/lib/catmonitor/snapshot/snapshot_npu.json
docker logs --tail 30 catmonitor-web
docker logs --tail 30 catmonitor-dfee
curl -fsS http://127.0.0.1:19322/stress/ >/dev/null
curl -fsS http://127.0.0.1:19322/api/stress/config
curl -fsS http://127.0.0.1:19323/ >/dev/null
docker exec catmonitor catmonitor stress doctor -o table
docker exec catmonitor-stress-npu npu-smi info
docker exec catmonitor-stress-npu /usr/local/bin/catmonitor-stress-exec \
  describe --benchmark npu_burn --json --require-pass
```

预期 5 容器、doctor 4/4；检查 actual CANN/torch_npu、PCI topology、logical 范围和
授权范围。`describe --require-pass` 防止 JSON 失败但退出 0。映射不明时停止。
浏览器直接访问 `http://<node-address>:19322/`、`/stress/`，DFeE 为 `:19323/`。
不要求 SSH 转发；若日志显示端口递增，以实际监听为准。TCP 不通先检查路由、防火墙和
监听；404 则核实服务/路径/代理，不能与“Stress 未启用”混为一谈。

## 7. CPU 按作业参数闭环

CLI `run` 会等待本次作业结束并输出报告，不是后台启动命令。需要在执行中查看或取消时，
另开宿主机终端执行 `status` / `cancel --job`；Web API 的提交则立即返回 Job。
每次等到终态再启动下一项。CLI/Web 共享作业。

```bash
docker exec catmonitor catmonitor stress run --bench stream --stream-threads 1 -o json
docker exec catmonitor catmonitor stress status -o table

docker exec catmonitor catmonitor stress run --bench hpcg \
  --hpcg-nx 16 --hpcg-ny 16 --hpcg-nz 16 --hpcg-runtime-seconds 2 \
  --hpcg-mpi-processes 1 --hpcg-threads-per-process 1 -o json
docker exec catmonitor catmonitor stress status -o table

docker exec catmonitor catmonitor stress run --bench hpl \
  --hpl-n 1024 --hpl-nb 128 --hpl-p 1 --hpl-q 1 \
  --hpl-mpi-processes 1 --hpl-threads-per-process 1 -o json
docker exec catmonitor catmonitor stress status -o json
```

检查 Healthy、实际结果和 execution_parameters，而不是仅看启动成功。
HPL 必须满足 P×Q=ranks，CPU/内存超额明确拒绝。STREAM 数组大小及内部重复次数仍是
编译参数，不能在 Web 修改。Web 在“高级参数 · 仅本次作业”展开编辑，历史只读。

## 8. A2 / A3 算子、模型、Cancel

先 `npu-smi info` 确认授权设备空闲。以下只测 logical 0；更多设备须先修改 allowlist。
小规模不是完整性能/可靠性验收，不能替代默认大规模运行。

```bash
docker exec catmonitor catmonitor stress run --bench npu_burn \
  --npu-devices 0 --npu-cases matmul --timeout 300s \
  --npu-operator-parameters '{"matmul":{"dtype":["float16"],"shape":[[[128,128],[128,128]]],"pattern":["uniform_random"],"run_count":2}}' -o json
docker exec catmonitor catmonitor stress status -o json
```

报告应核对本轮 fresh CSV、实际设备/算子/参数、result 和 err_count=0。
可选列表只是当前代际/API/配置准入；不支持的 dtype/算子拒绝，不偷偷降级。
完整 A3 配置保留，按作业更改不改共享 JSON；恢复默认参数另建作业即可。

模型先在健康设备生成独立参考，再以**相同参数、软件栈、内存策略和阈值**对照：

以下参考 ID 仅为示例，管理员改成该节点/代际自己的新 ID。A2 不提供 quant_matmul
上游策略，不能借用 A3 策略；matmul 等算子的可选范围由镜像实际准入决定。
A2 32GB 完整模型可能 OOM；小参数功能检查与完整模型验收必须分开记录。

```bash
docker exec catmonitor catmonitor stress run --bench npu_burn --npu-devices 0 \
  --npu-model gpt3 --model-action create_reference --reference-id gpt3-node-job-01 -o json
# 等参考完整生成后执行，不覆盖已有 ID。
docker exec catmonitor catmonitor stress run --bench npu_burn --npu-devices 0 \
  --npu-model gpt3 --model-action compare --reference-id gpt3-node-job-01 -o json
```

Llama2 使用独立 reference ID。默认完整训练可能 HBM OOM；checkpointed 不保证足够。
多选设备是逐卡对照，不是显存合并/DDP。修改模型参数或镜像后旧参考可能失配，不能静默
放宽阈值。完整训练结果以实际步数、错误数和 CSV 为准，未执行为 NOT_RUN。

在 Web 查看同一 Job；另启一项已授权任务，等观察到真实 workload 进程后按 Job ID Cancel：

```bash
docker top catmonitor-stress-cpu
docker top catmonitor-stress-npu
docker exec catmonitor catmonitor stress cancel --job '<job-id>'
docker exec catmonitor catmonitor stress status --job '<job-id>' -o json
docker top catmonitor-stress-cpu
docker top catmonitor-stress-npu
npu-smi info
```

终态 Cancelled 且无本轮 xhpl/MPI/ascend_npu_burn/multiprocessing 残留才通过。
另测 Web Run→CLI visible、CLI Run→Web Cancel、第二并发任务拒绝。
不按当前选择去杀设备，不清理业务进程。每项记录 PASS/FAIL/NOT_RUN，不拿旧 CSV 充数。

## 9. 保留与回滚

新节点只停止本次创建的五个容器，不自动删除卷/参考/报告或镜像。升级已有环境前
保存 docker inspect、原镜像 ID、配置及 mounts，等待当前作业终态，保留旧容器用于
回滚。切换只更新本次五个服务，不停止其他项目、业务容器或 Docker daemon。

```bash
docker stop catmonitor-web catmonitor-dfee catmonitor catmonitor-stress-cpu catmonitor-stress-npu
```

保留 SOURCE_COMMIT、镜像 digest/ID、构建和部署 manifest、本轮报告/CSV、进程前后对照。
升级失败时停止本轮新容器，恢复备份配置和原容器名后启动旧容器；不得清理唯一旧镜像。
更详细的模型边界见 [模型指南](NPU_MODEL_SDC_GUIDE.md)，构建与受限网络输入见
[开发者指南](DEVELOPER_GUIDE.md)。其他硬件和单设备模板见 [NPU 节点指南](README-npu.md)。

## 10. 85 / worker3：ZIP 源码、直接替换与旁路

以下使用修复 HPL 默认值后的源码。旧 `fb81582` ZIP 不会因 pull 镜像自动更新；
需替换源码 ZIP，或仅为旧生成器临时增加 `--hpl-profile small`。新生成器与现有 R2
执行镜像兼容，无需为本次默认值修复重新构建 Control/NPU；CPU Dockerfile 的新默认值
只在之后构建的新镜像中生效。没有 Git 也可运行。所有命令在 85 的 Bash 执行。

两种方式二选一：直接替换停止旧 CATMonitor，正常使用 host network；旁路保留旧实例，
必须增加独立 workload 名称、目录和端口映射。这些隔离参数不是 HPL 调优参数。
当前 API 无认证/RBAC，只能在可信管理网开放；只部署服务，不自动开始 Burn。

### 10.1 公共准备

把修复版 ZIP 解压到 `/home/haoran/CATMonitor-hpl-default-fix`，不要混用旧生成文件。
如解压目录不同，只改下面的 SRC。

```bash
export SRC=/home/haoran/CATMonitor-hpl-default-fix
export DOCKER_HOST=unix:///var/run/docker.sock
unset DOCKER_CONTEXT CATMONITOR_WEB_ADDR COMPOSE_FILE COMPOSE_PROFILES

export CONTROL_IMAGE=ghcr.io/spike677/catmonitor-npu:arm64-v0.3.6-stress-defaults-20261015-r2
export CPU_IMAGE=ghcr.io/spike677/catmonitor-stress-cpu:arm64-v0.3.6-stress-defaults-20261015-r2
export NPU_IMAGE=ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.6-stress-defaults-a2-cann851-20261015-r2
export CATMONITOR_IMAGE="$CONTROL_IMAGE"

docker pull "$CONTROL_IMAGE"
docker pull "$CPU_IMAGE"
docker pull "$NPU_IMAGE"
```

### 10.2 方式 A：直接替换，不制作旧容器备份

会短暂中断 CATMonitor。先在当前实例取消本项目的运行作业并等待终态；不杀其他 NPU
业务。下面只停止、删除列出的旧 CATMonitor 容器，不 rename/archive，不删除卷、
历史报告、模型参考、镜像或其他业务。数据不是构建缓存，不能混用 prune 清理。

```bash
(
set -e
for name in \
  docker-web-1 docker-dfee-1 docker-catmonitor-1 \
  catmonitor85-r2-web-1 catmonitor85-r2-dfee-1 catmonitor85-r2-catmonitor-1 \
  catmonitor-stress-cpu catmonitor-stress-npu
do
  if docker container inspect "$name" >/dev/null 2>&1; then
    docker stop "$name"
    docker rm "$name"
  fi
done
)

export DEPLOY=/home/catmonitor/catmonitor85-r2
export CATMONITOR_STRESS_STATE_DIR="$DEPLOY/stress"

(
set -e
install -d -m 0750 "$CATMONITOR_STRESS_STATE_DIR" "$DEPLOY/npu-output"
bash "$SRC/scripts/stress/generate_stress_deployment.sh" \
  --output-dir "$DEPLOY/generated" \
  --control-image "$CONTROL_IMAGE" \
  --cpu-image "$CPU_IMAGE" \
  --npu-image "$NPU_IMAGE" \
  --npu-output-dir "$DEPLOY/npu-output" \
  --npu-chip-generation A2 \
  --force

docker compose --project-directory "$SRC" -p catmonitor85-r2 \
  -f "$SRC/docker/docker-compose.yml" \
  -f "$SRC/docker/docker-compose.npu.yml" \
  -f "$SRC/docker/docker-compose.stress.yml" \
  -f "$DEPLOY/generated/docker-compose.stress.generated.yml" \
  --profile stress-cpu --profile stress-npu up -d --no-build
)

docker ps --filter label=com.docker.compose.project=catmonitor85-r2 \
  --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'

docker exec catmonitor85-r2-catmonitor-1 catmonitor stress doctor -o table
docker exec catmonitor-stress-npu npu-smi info
curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19322/api/stress/config
```

默认访问 `http://51.62.10.85:19322/stress/`，DFeE 为 `http://51.62.10.85:19323/`。
Web/DFeE 如递增端口，以本次启动日志为准；Metrics 19320 和 Exporter 9333 必须空闲。
如仍有前轮旁路实例占用 host 端口，先只停止自己创建的那个实例，不停止未知服务。

默认 HPL 运行和宿主机查看：

```bash
docker exec catmonitor85-r2-catmonitor-1 catmonitor stress run --bench hpl -o table
docker exec catmonitor85-r2-catmonitor-1 catmonitor stress status -o table
docker top catmonitor-stress-cpu
```

以上不传 HPL 参数。需要取消运行中的作业，另开终端按 Job ID 执行：

```bash
docker exec catmonitor85-r2-catmonitor-1 catmonitor stress cancel --job '<job-id>'
```

不用重命名保留旧容器。之后仅停止新实例时：

```bash
docker compose --project-directory "$SRC" -p catmonitor85-r2 \
  -f "$SRC/docker/docker-compose.yml" \
  -f "$SRC/docker/docker-compose.npu.yml" \
  -f "$SRC/docker/docker-compose.stress.yml" \
  -f "$DEPLOY/generated/docker-compose.stress.generated.yml" \
  --profile stress-cpu --profile stress-npu down
```

### 10.3 方式 B：旁路，旧环境保持运行

不要执行 10.2 的停止/删除命令。Docker bridge 必须正常；桥接监控的网络采集视角不与
host network 完全等价。硬件仍是共享的，不能同时从两套 Controller 向同一卡下发任务。
Workload 默认能力不变，只增加必要的部署隔离。

```bash
export SIDE=/home/catmonitor/catmonitor85-sidecar
export CATMONITOR_STRESS_STATE_DIR="$SIDE/stress"

(
set -e
install -d -m 0750 "$SIDE" "$CATMONITOR_STRESS_STATE_DIR" "$SIDE/npu-output"
bash "$SRC/scripts/stress/generate_stress_deployment.sh" \
  --output-dir "$SIDE/generated" \
  --control-image "$CONTROL_IMAGE" \
  --cpu-image "$CPU_IMAGE" \
  --cpu-container catmonitor85-sidecar-cpu \
  --npu-image "$NPU_IMAGE" \
  --npu-container catmonitor85-sidecar-npu \
  --npu-output-dir "$SIDE/npu-output" \
  --npu-chip-generation A2
)

tee "$SIDE/sidecar.yml" >/dev/null <<'YAML'
services:
  catmonitor:
    network_mode: bridge
    ports:
      - "127.0.0.1:19327-19360:19320"
  web:
    network_mode: bridge
    ports:
      - "51.62.10.85:19327-19360:19322"
  dfee:
    network_mode: bridge
    ports:
      - "51.62.10.85:19327-19360:19323"
      - "127.0.0.1:19327-19360:9333"
YAML

docker compose --project-directory "$SRC" -p catmonitor85-sidecar \
  -f "$SRC/docker/docker-compose.yml" \
  -f "$SRC/docker/docker-compose.npu.yml" \
  -f "$SRC/docker/docker-compose.stress.yml" \
  -f "$SIDE/generated/docker-compose.stress.generated.yml" \
  -f "$SIDE/sidecar.yml" \
  --profile stress-cpu --profile stress-npu up -d --no-build

docker ps --filter label=com.docker.compose.project=catmonitor85-sidecar \
  --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
docker port catmonitor85-sidecar-web-1 19322/tcp
docker port catmonitor85-sidecar-dfee-1 19323/tcp
docker exec catmonitor85-sidecar-catmonitor-1 catmonitor stress doctor -o table
```

用 `docker port` 输出的实际端口访问 `/stress/`，不是假定宿主仍为 19322。端口不保证
连续或重建后相同；85 管理 IP 不同时修改 YAML 中两处 `51.62.10.85`。

只停止、删除旁路容器，不动直接部署和业务，不删除数据卷：

```bash
docker compose --project-directory "$SRC" -p catmonitor85-sidecar \
  -f "$SRC/docker/docker-compose.yml" \
  -f "$SRC/docker/docker-compose.npu.yml" \
  -f "$SRC/docker/docker-compose.stress.yml" \
  -f "$SIDE/generated/docker-compose.stress.generated.yml" \
  -f "$SIDE/sidecar.yml" \
  --profile stress-cpu --profile stress-npu down
```

## 11. 83：Compose 1.29.2 / Docker 18.09，免宿主 docker-init 修复

83 的镜像导入和 ID 核对已通过；本节针对创建阶段的两个兼容缺陷，不重新构建镜像，
不安装宿主 docker-init，不重启 Docker，不改 runtime/权限/CANN 或 generated 配置。
旧适配将 pids_limit 保留为字符串，旧 docker-py 创建容器要求整数；新适配保持预算
4096，只纠正类型。NPU 则用镜像现有 Python3 运行只读源码中的 PID-1 helper，
转发 TERM/INT/HUP、回收孤儿进程并传播原入口退出码，不是简单移除 init 后让 sleep
冒充回收器。Controller → Docker exec → workload plugin 调用链保持不变。

将完整修复版源码放到 `/home/haoran/CATMonitor-stress-r2`，包含新文件
`docker/stress/container_init.py`；不要覆盖 `/home/catmonitor/catmonitor83-sidecar`。
只需替换源码包，但需要重新创建失败的 CPU/NPU 容器才能应用配置；restart 不生效。
helper 以只读 bind 挂载，部署后不能删除源码目录，不是镜像内 Python 包/wheel 热替换。

```bash
export DOCKER_HOST=unix:///var/run/docker.sock
unset DOCKER_CONTEXT COMPOSE_FILE COMPOSE_PROFILES
export SRC=/home/haoran/CATMonitor-stress-r2
export PROJECT=catmonitor83-sidecar
export DEPLOY=/home/catmonitor/catmonitor83-sidecar
export GENERATED="$DEPLOY/generated"
export CATMONITOR_IMAGE=ghcr.io/spike677/catmonitor-npu:arm64-v0.3.6-stress-defaults-20261015-r2
export CATMONITOR_STRESS_STATE_DIR="$DEPLOY/stress"
cd "$SRC"

bash "$SRC/docker/compose.sh" \
  --backend legacy --hardware npu --mode full \
  --project "$PROJECT" --generated "$GENERATED" \
  --overlay "$DEPLOY/sidecar.yml" config

bash "$SRC/docker/compose.sh" \
  --backend legacy --hardware npu --mode full \
  --project "$PROJECT" --generated "$GENERATED" \
  --overlay "$DEPLOY/sidecar.yml" \
  up --no-build --no-deps --force-recreate \
  catmonitor-stress-cpu catmonitor-stress-npu

docker inspect "${PROJECT}-cpu" \
  --format 'PidsLimit={{.HostConfig.PidsLimit}}'
docker inspect "${PROJECT}-npu" \
  --format 'Init={{.HostConfig.Init}} Entry={{json .Config.Entrypoint}}'
docker exec "${PROJECT}-npu" npu-smi info
CTRL=$(docker ps -q \
  --filter "label=com.docker.compose.project=$PROJECT" \
  --filter label=com.docker.compose.service=catmonitor)
docker exec "$CTRL" catmonitor stress doctor -o table
```

不再需要前轮单独的 `legacy-fix.yml`；文件可以留存，但上述命令不加载它。
应看到 PidsLimit=4096、Init=false，以及包含 container_init.py 的 Python entrypoint。
此命令只重建旁路两 workload，不重启旁路 Controller/Web/DFeE，不操作旧项目。
配置、数据卷、历史报告和已有镜像均无需清空。83 实际启动、doctor、Cancel/进程清理
仍需在目标机确认；本地合成进程和 Compose 测试不是 NPU 实机 PASS。
