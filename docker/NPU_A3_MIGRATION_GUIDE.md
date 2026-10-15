# A3 模型和算子压测迁移指南

本指南用于从公开源码构建 A3 候选镜像，在获准节点验证算子及 GPT3/Llama2 模型。保持 Unified Stress V2：CLI/Web → 唯一 daemon Controller → Docker Executor → workload executor。Web 继续使用 `:19322`，不挂 Docker socket。

当前 A2 原配置实验中，两个模型真实导入通过，但首次前向均 HBM OOM，完成 0/2112 步。A3、新模型入口和新增激活重计算模式的完整硬件验收均为 **PENDING**。换到 A3 不自动意味着通过，增加作业超时也不能解决显存不足。

## 1 训练方式与支持边界

| 方式 | 当前实现 | 对显存的影响 |
| --- | --- | --- |
| 标准完整参数 | 支持，A2 首次前向 OOM | 每个逻辑设备独立运行完整模型 |
| 完整参数加激活重计算 | 本分支显式候选，硬件 PENDING | 重算部分前向以降低激活保存量；不保证一定够用 |
| 选择多个设备 | 支持逐设备顺序 SDC 对照 | 不合并 HBM，不是分布式训练 |
| 减小 batch/层数/序列 | 本分支不自动修改 | 会改变压测规模及参考，不能冒充完整配置通过 |
| DDP、FSDP、张量/流水线并行 | NOT_IMPLEMENTED | 需要新的通信、分片、取消和参考协议，不能只多选几张卡 |

普通 DDP 在每卡保留完整模型；拆分全局 batch 可以降低每卡激活量，但仍须各卡容纳模型、梯度和优化器状态。模型分片也是可行开发方向，而不是本分支已有功能。[DDP 官方说明](https://huggingface.co/docs/transformers/ddp)

激活重计算保留完整 batch、形状、精度和 2112 步，以额外计算换取激活内存。候选显式保留 RNG 状态，使用非重入 checkpoint；其耗时、NPU 支持和数值一致性仍须实测。[PyTorch 2.9 说明](https://docs.pytorch.org/docs/2.9/checkpoint.html)

GPT3 用例使用 GPT2Model 加训练头，Llama2 使用 LlamaModel 加训练头；均为随机初始化和固定种子输入，不需要商业模型权重或业务数据。A3 的完整 `a3_config.json` 和基础配置保持原样。不要应用 A2 配置选择/缩减补丁到 A3。

## 2 需要带入环境的内容

从个人 fork 下载 `codex/npu-model-a3-migration` 分支，记录 `git rev-parse HEAD`。以最终交付的完整 commit 固定源码，不只看程序版本号。

```bash
git clone --branch codex/npu-model-a3-migration \
  https://github.com/spike677/CATMonitor.git
cd CATMonitor
git rev-parse HEAD
git status --short
```

Git 不可达时，可以从该分支下载源码 ZIP，在获准传输方式下带入；依赖和镜像使用环境允许的镜像仓库或离线包。不要向公开仓库上传内源源码、镜像、日志、配置、业务数据或凭据。

| 内容 | 用途 |
| --- | --- |
| `docker/build.sh`、`docker/Dockerfile.npu` | 新 Control 镜像，包含当前 CLI/Web/Controller/DFeE |
| `scripts/stress/build_npu_burn_image.sh` | 新 NPU workload 镜像及 build manifest |
| `docker/stress/npu/*` | 环境初始化、模型 adapter、NPU 启动和预检 |
| `third_party/ascend_npu_burn/source` | 完整上游树；构建时显式应用补丁，不改仓库内上游文件 |
| 三个 A3 补丁，见下方命令 | ARM64/运行兼容、可写参考/trace 路径、显式激活重计算 |
| `scripts/stress/generate_stress_deployment.sh` | YAML、工作负载 env、设备映射和部署 manifest |
| Compose 四文件或本指南 manual 命令 | 启动 4 容器；选 CPU 后为 5 容器 |

NPU-only 是 2 张镜像、4 个容器：Control 用于 daemon/Web/DFeE，NPU workload 用于 Burn。额外 CPU 压测才需要 CPU workload 镜像和容器，不要求为了模型重建 CPU 镜像。

## 3 从源码构建两张候选镜像

前置：Linux ARM64、Docker、Ascend 驱动、获准使用且版本固定的 CANN/PyTorch/torch_npu/Transformers 基础环境。先检查磁盘和 DockerRootDir；构建需要足够空间，不把少量剩余空间当作可用构建环境。

```bash
uname -m
docker version
docker info --format '{{.DockerRootDir}}'
df -h
npu-smi info

SOURCE_COMMIT=$(git rev-parse HEAD)
SHORT_COMMIT=$(git rev-parse --short=12 HEAD)
CONTROL_IMAGE="catmonitor-npu:model-migration-${SHORT_COMMIT}"
NPU_IMAGE="catmonitor-stress-npu:a3-model-migration-${SHORT_COMMIT}"

# 可选，填入本环境批准的镜像源/代理；不写入 Git、YAML 或 manifest。
# export CATMONITOR_DEBIAN_MIRROR=https://mirrors.aliyun.com
# export CATMONITOR_DOCKER_BUILD_NETWORK=host
# export GOPROXY=https://goproxy.cn,direct

bash docker/build.sh npu
docker tag catmonitor-npu "$CONTROL_IMAGE"
```

填写获准的固定基础镜像和构建目录。两种基础镜像必须满足构建脚本的软件/ABI 门禁；即使相同也要记录真实 image ID。CANN 保留在 workload 镜像中，宿主驱动只读挂载。不借用旧 A2 wheel、参考或临时覆盖包。

```bash
test -n "$NPU_BUILDER_BASE"
test -n "$NPU_RUNTIME_BASE"
test -n "$NPU_BUILD_ROOT"     # 管理员选择有足够空间的绝对路径

bash scripts/stress/build_npu_burn_image.sh \
  --builder-base-image "$NPU_BUILDER_BASE" \
  --runtime-base-image "$NPU_RUNTIME_BASE" \
  --image "$NPU_IMAGE" \
  --compat-profile portable-model-sdc-memory-v1 \
  --patch scripts/stress/patches/ascend_npu_burn/portable-runtime-v1.patch \
  --patch scripts/stress/patches/ascend_npu_burn/model-sdc-v1.patch \
  --patch scripts/stress/patches/ascend_npu_burn/model-memory-v1.patch \
  --enable-model-sdc \
  --build-root "$NPU_BUILD_ROOT"

docker image inspect "$CONTROL_IMAGE" "$NPU_IMAGE" \
  --format '{{.RepoTags}} {{.Os}}/{{.Architecture}} {{.Id}}'
```

若 Transformers 或真实 GPT2Model/LlamaModel 类不能导入，构建应失败，不临时 pip 安装到运行容器。受限环境依赖准备、Go 选择、proxy 和 pciutils 离线包方法见 [开发者指南](DEVELOPER_GUIDE.md) 和 [受限环境指南](RESTRICTED_ENV_GUIDE.md)。本步骤不训练，也不证明硬件通过。

## 4 生成配置

先确认映射及压测授权。以下 full 模式发现全部真实设备节点，仅适用于这些设备都获准映射的节点。存在业务设备且未获准映射时，不执行此模板；使用 README 的 single 受限模式，并先通过真实 PCI↔runtime 映射门禁，不能为通过 doctor 扩大映射或 privileged。

```bash
GENERATED_DIR=/etc/catmonitor/generated-model-stress
STRESS_STATE=/var/lib/catmonitor/model-stress
NPU_OUTPUT=/var/lib/catmonitor/model-stress-npu-output
install -d -m 0750 "$GENERATED_DIR" "$STRESS_STATE" "$NPU_OUTPUT"

bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$GENERATED_DIR" \
  --control-image "$CONTROL_IMAGE" --npu-image "$NPU_IMAGE" \
  --npu-chip-generation A3 --npu-runtime runc \
  --npu-device-mode full --npu-security-profile restricted \
  --npu-burn-device 0 --npu-allowed-devices 0 \
  --npu-allowed-models gpt3,llama2 --allow-npu-reference-generation \
  --npu-model-memory-mode checkpointed --npu-job-timeout 2h \
  --npu-threads 1 --npu-ascend-env-script "$ASCEND_ENV_SCRIPT" \
  --npu-output-dir "$NPU_OUTPUT" --enable-web
```

`ASCEND_ENV_SCRIPT` 必须填写 workload 内实际的绝对 CANN 初始化路径。不要套用 A2 的路径。`12h` 是候选作业上限，不是完整训练预计耗时或固定压测时长。算子 worker 的软件同步上限默认 300 秒；模型沿用直接同步，由 Controller 外层时限终止整个进程组，不宣称模型每次同步具有该 300 秒软件计时器。先测量单设备，再按实测估算顺序多设备所需时限。

示例仅授权 logical 0。获得实际拓扑后，如需 device 8、14 或多个设备，管理员修改 `--npu-burn-device` 默认值和 `--npu-allowed-devices`，在新目录重新生成配置，不能将 host `/dev/davinciN` 编号直接当成 runtime logical index，也不能因未选择而默认全卡。作业设备由 CLI/Web 每次明确选择。

检查生成的 `catmonitor-stress.yaml`、`npu-workload.env`、`stress-profile.json` 和 manifest。显存策略及 CANN 路径在公开生成结果中，不依赖临时 `docker exec -e`。

## 5 启动容器

确认容器名和 19320/19322/19323/9333 等端口未被占用。不要停止已有业务或 CATMonitor；有冲突时改用隔离名称、目录和备用端口，并记录这些差异。

### Compose

```bash
CATMONITOR_IMAGE="$CONTROL_IMAGE" \
CATMONITOR_STRESS_STATE_DIR="$STRESS_STATE" \
docker compose -p catmonitor-model \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f "$GENERATED_DIR/docker-compose.stress.generated.yml" \
  --profile stress-npu up -d
```

需要 CPU 时在生成步骤添加 `--cpu-image "$CPU_IMAGE"`，启动时再添加 `--profile stress-cpu`。仍然不需要独立 Stress Web 或旧 Unix Runner。

### Manual docker run

没有 Compose 时使用下列四容器路径。它采用 restricted NPU profile；若初始化失败先诊断并记录，不自动升级权限。下列挂载要求路径真实存在。`npu-smi` 实际安装在 sbin 时，调整 host 源路径，不在镜像内伪造二进制。

```bash
# 从生成器实际发现结果构造映射，不硬编码 16 或 0..7。
mapfile -t HOST_IDS < <(python3 - "$GENERATED_DIR/stress-profile.json" <<'PY'
import json,sys
p=json.load(open(sys.argv[1]))
assert p['npu']['enabled'] and p['npu']['host_device_ids']
for value in p['npu']['host_device_ids']: print(value)
PY
)
NPU_DEVICE_ARGS=()
for id in "${HOST_IDS[@]}"; do
  test -c "/dev/davinci$id"
  NPU_DEVICE_ARGS+=(--device "/dev/davinci$id:/dev/davinci$id")
done
for volume in cm-model-snapshot cm-model-data cm-model-straggler cm-model-control cm-model-csv cm-model-npu-state; do
  docker volume create "$volume"
done

docker run -d --name catmonitor-stress-npu --restart unless-stopped \
  --init --runtime runc --ipc shareable --read-only --network none \
  --cap-drop ALL --security-opt no-new-privileges:true \
  "${NPU_DEVICE_ARGS[@]}" \
  --device /dev/davinci_manager:/dev/davinci_manager \
  --device /dev/devmm_svm:/dev/devmm_svm --device /dev/hisi_hdc:/dev/hisi_hdc \
  --env-file "$GENERATED_DIR/npu-workload.env" \
  --tmpfs /tmp:rw,nosuid,nodev,size=256m \
  --tmpfs /opt/catmonitor/npuburn-home:rw,nosuid,nodev,size=1g,mode=0750 \
  -v cm-model-npu-state:/var/lib/catmonitor/stress \
  -v "$NPU_OUTPUT:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output" \
  -v /sys/bus/pci:/sys/bus/pci:ro \
  -v /usr/local/Ascend/driver/lib64:/usr/local/Ascend/driver/lib64:ro \
  -v /usr/local/Ascend/driver/version.info:/usr/local/Ascend/driver/version.info:ro \
  -v /etc/ascend_install.info:/etc/ascend_install.info:ro \
  -v /usr/local/dcmi:/usr/local/dcmi:ro \
  -v /usr/local/bin/npu-smi:/usr/local/bin/npu-smi:ro \
  "$NPU_IMAGE"

docker run -d --name catmonitor --restart unless-stopped \
  --privileged --network host --pid host \
  -v /:/host:ro -v /etc/os-release:/etc/os-release:ro \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common \
  -v "$GENERATED_DIR/catmonitor-stress.yaml:/etc/catmonitor/catmonitor.yaml:ro" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$STRESS_STATE:/var/lib/catmonitor/stress" \
  -v cm-model-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-model-data:/var/lib/catmonitor/data \
  -v cm-model-straggler:/var/lib/catmonitor/straggler \
  -v cm-model-control:/run/catmonitor "$CONTROL_IMAGE"

docker run -d --name catmonitor-web --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/web \
  -v cm-model-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-model-control:/run/catmonitor:ro "$CONTROL_IMAGE" \
  -addr=:19322 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -control-socket=/run/catmonitor/control.sock

docker run -d --name catmonitor-dfee --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/dfee \
  -v cm-model-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-model-csv:/var/lib/catmonitor/csv "$CONTROL_IMAGE" \
  -addr=:19323 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled -exporter-port=9333 -csv=disabled \
  -csv-dir=/var/lib/catmonitor/csv -csv-interval=10s
```

daemon 的 privileged/host 采集模式沿用 Monitoring 契约；只有它持有 Docker socket。Web/DFeE 不获得 Docker socket、模型参考目录或 workload 内部目录。Control/NPU runtime 根文件系统和权限分别按已有契约；NPU 仍只读，参考/trace 写到持久卷。

## 6 先预检，再运行

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19320/metrics >/dev/null
curl -fsS http://127.0.0.1:19322/api/stress/config
docker exec catmonitor catmonitor stress doctor -o json
npu-smi info
```

doctor/describe 必须检查实际 JSON preflight，不只看命令退出码。确认 CANN、torch_npu、lspci、映射数量、runtime logical IDs 和授权范围；available 不代表空闲，预检通过也不证明完整训练能放进 HBM。设备或映射无法确认时 STOP，不绕过预检。

先运行授权的算子，再做模型单设备参考生成：

```bash
docker exec catmonitor catmonitor stress run \
  --bench npu_burn --npu-devices 0 --npu-cases matmul -o json

docker exec catmonitor catmonitor stress run \
  --bench npu_burn --npu-devices 0 \
  --npu-model gpt3 --model-action create_reference \
  --reference-id gpt3-a3-checkpointed-01 -o json

docker exec catmonitor catmonitor stress run \
  --bench npu_burn --npu-devices 0 \
  --npu-model gpt3 --model-action compare \
  --reference-id gpt3-a3-checkpointed-01 -o json
```

Llama2 换成 `--npu-model llama2` 和独立 `llama2-a3-checkpointed-01` 参考 ID。标准模式要重新生成标准模式参考，不复用 checkpointed/A2 参考。初次参考本身不证明硬件健康，应在已确认健康设备生成，并做独立重复对照。

确认管理员授权后，compare 的 `--npu-devices` 可改为实际可用的多个 logical ID。当前仍逐卡顺序执行，不是多卡联合训练。

## 7 Web 展示与硬件验收

直接打开 `http://<node-address>:19322/stress/`；只要网络可达、监听和防火墙正确，就不要求 SSH 隧道。访问失败先区分 TCP/防火墙与 HTTP 路由，不通过第二 Web listener 规避。当前 Run/Cancel 没有认证/RBAC，只对可信网络开放。

| 展示 | 含义 |
| --- | --- |
| 预检通过 | 依赖、参数、映射/参考准入通过，不是完整 workload 通过 |
| 显存不足 · 未完成 | 本轮有明确 NPU HBM OOM；不是硬件故障诊断，也没有通过结果 |
| 压测通过 / 模型 SDC 对照通过 | Controller 收到并核验本轮完整结果、设备和 CSV |
| 参考已生成 | 完整参考保存成功；未执行 SDC 对照 |
| 其它失败/取消 | 保持实际原因；不全部猜测为显存不足 |

旧手工 probe 记录不会伪造为 Controller Job。新 UI 分类只由新构建镜像产生的作业报告驱动；历史没有结构化原因时保留原结果，不猜测。

硬件验收逐项记录 `PASS/FAIL/NOT_RUN`：新 image ID及源码/补丁身份 → 设备映射/空闲 → doctor → 算子本轮 CSV → 单设备完整参考 → 2112 步 SDC compare、错误数 0 → 授权多设备覆盖 → Web/CLI 同一 Job → Cancel → 进程及 HBM 释放。A3 完整参数是否可运行、checkpoint 的显存峰值与耗时均待实际完成后判断。

```bash
docker exec catmonitor catmonitor stress status --job '<job-id>' -o json
docker exec catmonitor catmonitor stress cancel --job '<job-id>'
docker top catmonitor-stress-npu
npu-smi info
```

出现 OOM、数值不一致、部分步骤、缺设备或 stale CSV 均不得标记 PASS。取消始终按 Job ID。内源结果可以留在内源，无需上传内部数据；仅反馈获准披露的状态即可。

## 8 停止及保留

只停止本次新建的容器，不删除镜像、参考卷或已有业务数据。执行前逐个核实名称对应本次部署。

```bash
docker stop catmonitor-web catmonitor-dfee catmonitor catmonitor-stress-npu
```

保留 build/deployment manifest、完整源码 commit、新镜像 ID和本轮报告。进一步清理必须单独确认，不能以本指南清理旧生产环境。
