# 按作业参数候选：内源 A2 / A3 获取、部署和验收

统一源码分支：`spike677/CATMonitor:codex/stress-job-parameters`；功能基线：
`f636dd000995d7693107197f1310151c29ba4941`，已整合 develop `0554f54`。
本批镜像构建提交固定为 `4630b98227a31fc56a679c4785e35abd98d05d4e`；
后续本页部署对象说明是纯文档更新，不声称镜像因此重新构建。**当前优先交付和验证 A2。**
本指南是候选交付，不是正式 release 或 A3 配套认证。A3 真实硬件验收为 **PENDING**；
构建/import/ABI 检查和 A2 实测不能替代 A3 workload 结果。

## 1. 本批镜像和源码

三类镜像不是三套控制面。Full 使用一张 Control（daemon/Web/DFeE）、一张 CPU、
一张 NPU，共五个容器。NPU 因运行软件栈不同提供 A2/A3 两个标签，不混用。

| 角色 | 候选 registry ref |
| --- | --- |
| ARM64 Ascend Control | `ghcr.io/spike677/catmonitor-npu:arm64-v0.3.6-stress-job-20261015-r1` |
| ARM64 CPU workload | `ghcr.io/spike677/catmonitor-stress-cpu:arm64-v0.3.6-stress-job-20261015-r1` |
| A2 / CANN 8.5.1 NPU workload | `ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.6-stress-job-a2-cann851-20261015-r1` |
| A3 / CANN 9.1 NPU workload | `ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.6-stress-job-a3-cann91-20261015-r1` |

这些是本批候选引用，只有交付记录中的 push/pull gate 通过才算已发布。不能仅凭标签
推断内容；发布人员应另提供本批 `RELEASE.json` 的实际 source commit、Image ID、digest、
base 和 profile。管理员以该记录固定 checkout 和镜像 digest。不要覆盖旧 stable/latest。

### 1.1 已发布的 A2 三件套（2026-10-15）

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
export CONTROL_IMAGE=ghcr.io/spike677/catmonitor-npu:arm64-v0.3.6-stress-job-20261015-r1
export CPU_IMAGE=ghcr.io/spike677/catmonitor-stress-cpu:arm64-v0.3.6-stress-job-20261015-r1
export NPU_CHIP_GENERATION=A2  # A3 节点显式改为 A3；不根据设备个数猜代际。
case "$NPU_CHIP_GENERATION" in
  A2)
    export NPU_IMAGE=ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.6-stress-job-a2-cann851-20261015-r1
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

第 1 节按代际选择对应的 CANN 初始化脚本。若使用自己的其他 runtime，先核实其真实
路径及版本再调整；不把 A2 的路径套到 A3。本节一次生成 A2 或 A3 的 Full 配置。

```bash
export GENERATED_DIR=/etc/catmonitor/generated-job-stress
export STRESS_STATE=/var/lib/catmonitor/job-stress
export NPU_OUTPUT=/var/lib/catmonitor/job-stress-npu-output
install -d -m 0750 "$GENERATED_DIR" "$STRESS_STATE" "$NPU_OUTPUT"

bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$GENERATED_DIR" --control-image "$CONTROL_IMAGE" \
  --cpu-image "$CPU_IMAGE" --hpl-profile small \
  --stream-threads 1 --hpl-processes 1 --hpl-threads 1 \
  --hpcg-processes 1 --hpcg-threads 1 --hpcg-nx 16 --hpcg-ny 16 --hpcg-nz 16 --hpcg-runtime 2 \
  --npu-image "$NPU_IMAGE" --npu-output-dir "$NPU_OUTPUT" \
  --npu-chip-generation "$NPU_CHIP_GENERATION" --npu-runtime runc \
  --npu-device-mode full --npu-security-profile legacy-privileged \
  --npu-burn-device 0 --npu-allowed-devices 0 \
  --npu-run-case matmul --npu-allowed-models gpt3,llama2 --allow-npu-reference-generation \
  --npu-model-memory-mode checkpointed --npu-job-timeout 2h \
  --npu-threads 1 --npu-ascend-env-script "$CANN_ENV_SCRIPT" \
  --enable-web

python3 -m json.tool "$GENERATED_DIR/stress-profile.json"
```

CPU 初始采用小规模功能配置，不代表性能压测规模。NPU 初始仅授权 logical 0；如需
更多设备，先通过 topology，管理员再在新目录生成明确 allowlist。完整 PCI、节点 ID、
runtime logical ID 不混同，不把 sparse 节点编号直接当逻辑编号，也不硬编码 16/0..7。
不默认挂外部实验配置；模型规模变化优先用本次作业参数，参考必须重新匹配。

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
更详细的模型边界见 [模型指南](NPU_MODEL_SDC_GUIDE.md) 和 [A3 迁移指南](NPU_A3_MIGRATION_GUIDE.md)。
