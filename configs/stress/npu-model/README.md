# A2 模型短测配置

`a2-model-b32-checkpointed-36steps.json` 是 GPT3/Llama2 的可复用 A2 短测配置：batch 32、激活重计算、4 步预热加 32 步测量。

这不是完整 64/2048 步、隔夜运行或持续满载验收，也不是 A3 支持声明。预设中的 `validation` 只记录历史来源，不是当前镜像、新节点或其他软件栈的通过证明。配套镜像、源码身份与当前验收范围以[交付指南](../../../docker/STRESS_JOB_RELEASE_GUIDE.md)为准；每个新环境仍须预检并生成匹配参考。

## 配置包含什么

| 内容 | 设置 |
| --- | --- |
| 模型 | GPT3 和 Llama2 上游压测模型，随机初始化，不下载预训练权重 |
| 每个模型每张卡的步骤 | 4 步预热加 32 步测量，全部 36 步参与 SDC |
| 显存策略 | `checkpointed`，必须通过部署环境显式设置 |
| 模型结构 | 36 层、hidden size 1280、20 个注意力头、intermediate size 5120、序列长度 512、bfloat16 |
| 设备执行 | 同一模型逐卡独立执行，不合并显存，不是双卡分布式训练 |
| 参考 | 每个模型单独生成；不随配置分发，也不能复用其他配置或软件栈的参考 |

预设只改模型 batch 和步数，保留全部算子及完整 `a3_config.json`，不修改上游源码、镜像或 SDC 阈值。生成工具遇到不匹配的模型结构会拒绝，避免把不同配置误标为本预设。

## 从配套镜像生成完整配置目录

先按[交付指南](../../../docker/STRESS_JOB_RELEASE_GUIDE.md)选定并获取配套的 `CONTROL_IMAGE`、`NPU_IMAGE`。在仓库根目录，以具备 Docker 和配置目录写权限的账号执行；这里不构建镜像。导出容器只用于 `docker cp`，不启动、不映射 NPU，也不占用设备。

```bash
: "${NPU_IMAGE:?set the matching NPU image from the deployment guide}"
: "${CONTROL_IMAGE:?set the matching Control image from the deployment guide}"
CONFIG_ROOT='/etc/catmonitor/npu-config-profiles'
CONFIG_DIR="$CONFIG_ROOT/a2-model-b32-checkpointed-36steps"

(
  set -eu
  install -d -m 0750 "$CONFIG_ROOT"
  test ! -e "$CONFIG_ROOT/image-default"
  test ! -e "$CONFIG_DIR"
  EXPORT_CONTAINER=$(docker create --entrypoint /bin/true "$NPU_IMAGE")
  trap 'docker rm "$EXPORT_CONTAINER" >/dev/null' EXIT
  docker cp "$EXPORT_CONTAINER:/opt/catmonitor/npuburn-python/ascend_npu_burn/config" \
    "$CONFIG_ROOT/image-default"
  python3 scripts/stress/npu_config_profile.py \
    --source-dir "$CONFIG_ROOT/image-default" \
    --output-dir "$CONFIG_DIR" \
    --preset configs/stress/npu-model/a2-model-b32-checkpointed-36steps.json
)
```

输出包含实际两个 JSON 的 SHA-256 和 `model_memory_mode=checkpointed`。工具禁止覆盖已有目录；再次生成时使用新目录。完整配置应来自将要运行的镜像，不从旧实验目录复制。

## 挂载到现有 NPU 部署

下面的 logical 0、1 仅适用于已经确认这两个逻辑设备存在且获授权的双卡节点；它们不是宿主机 `/dev/davinciN` 的编号。生成器动态发现设备，预设不会自动授权全卡。

```bash
bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-a2-b32-36steps \
  --control-image "$CONTROL_IMAGE" \
  --npu-image "$NPU_IMAGE" \
  --npu-chip-generation A2 --npu-runtime runc \
  --npu-burn-device 0 --npu-allowed-devices 0,1 \
  --npu-allowed-models gpt3,llama2 --allow-npu-reference-generation \
  --npu-model-memory-mode checkpointed --npu-threads 1 \
  --npu-config-dir "$CONFIG_DIR" --npu-security-profile restricted \
  --enable-web
```

生成后按 [NPU 部署指南](../../../docker/README-npu.md) 应用配置及挂载；生成文件不会自动改变运行中的容器。迁移已有部署时保留现有配置、设备、安全参数、state、output 和参考目录，不直接覆盖正在使用的 YAML。

Manual `docker run` 在原有完整 NPU 参数之外增加以下两项，而不是另建一套执行平台：

```bash
--mount type=bind,source="$CONFIG_DIR",target=/opt/catmonitor/npuburn-python/ascend_npu_burn/config,readonly \
-e CATMONITOR_MODEL_MEMORY_MODE=checkpointed
```

首次增加挂载需要在无活动 Job、设备空闲时重新创建 NPU workload 容器，但不重建镜像。根文件系统仍只读，Web 不挂 Docker socket。配置目录已经挂载后，只能在空闲时更新 JSON；配置或软件栈变化后使用新参考。

## 参考与验收边界

按[模型 SDC 指南](../../../docker/NPU_MODEL_SDC_GUIDE.md)执行预检、单卡生成参考、检测、status 和按 Job ID 取消。每次等待当前 Job 完整结束后，才提交下一项；提交成功不等于训练完成。GPT3 和 Llama2 分别使用独立参考 ID。

本预设生成参考要求 `reference_generated`、`saved_steps=36`；在两个已授权逻辑设备上检测时，要求 `sdc_passed`、`checked_steps=72`、每卡 CSV `checked_steps=36` 且 `err_count=0`，设备与本次 Job 一致。生成参考不等于 SDC 检测通过，doctor 通过不等于训练通过。

模型参数、配置哈希、软件栈、代际或显存策略变化后，旧参考不可直接复用。完整 64/2048 步、隔夜稳定性、持续满载和 A3 硬件均须另行验收，不由本预设或历史 `validation` 自动继承。A3 保留自己的完整配置和兼容 profile，在对应软件栈重新生成参考，参见[模型指南](../../../docker/NPU_MODEL_SDC_GUIDE.md)与[配套交付指南](../../../docker/STRESS_JOB_RELEASE_GUIDE.md)。
