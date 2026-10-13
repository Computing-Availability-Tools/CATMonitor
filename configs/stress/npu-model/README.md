# A2 模型短测配置

`a2-model-b32-checkpointed-36steps.json` 是 GPT3/Llama2 的可复用 A2 短测配置：batch 32、激活重计算、4 步预热加 32 步测量。2026 年 10 月 13 日，两种模型各自生成参考后，均在 logical 0、1 各完成 36 步 SDC 对照，错误数 0。

这不是完整 64/2048 步、隔夜运行或持续满载验收，也不是 A3 支持声明。验证节点为 Ascend910B4，单卡 32768 MiB；软件栈和镜像身份记录在配置的 `validation` 中。该字段只是既有结果摘要，新节点仍须自行预检和生成参考。

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

在仓库根目录，以具备 Docker 和配置目录写权限的账号执行。镜像应已经获取；这里不执行构建。导出容器只用于 `docker cp`，不启动、不映射 NPU，也不占用设备。

```bash
NPU_IMAGE='ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.6-a2-6f94e58b8336'
CONTROL_IMAGE='ghcr.io/spike677/catmonitor-npu:arm64-v0.3.6-a2-6f94e58b8336'
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

## 生成参考并逐卡检测

先执行 `npu-smi info` 和 doctor，确认授权设备空闲；doctor 通过不代表模型训练或 SDC 通过。参考应来自已确认健康的环境。

```bash
docker exec catmonitor catmonitor stress doctor \
  -c /etc/catmonitor/catmonitor.yaml -o table

(
  set -eu
  for MODEL in gpt3 llama2; do
    REFERENCE="${MODEL}-a2-b32-36-$(date +%s)"
    docker exec catmonitor catmonitor stress run \
      --bench npu_burn --npu-devices 0 \
      --npu-model "$MODEL" --model-action create_reference --reference-id "$REFERENCE" \
      -c /etc/catmonitor/catmonitor.yaml -o json
    docker exec catmonitor catmonitor stress run \
      --bench npu_burn --npu-devices 0,1 \
      --npu-model "$MODEL" --model-action compare --reference-id "$REFERENCE" \
      -c /etc/catmonitor/catmonitor.yaml -o json
  done
)
```

顺序为 GPT3 参考、GPT3 两张卡对照、Llama2 参考、Llama2 两张卡对照。生成参考要求 `reference_generated`、`saved_steps=36`；每个模型的双卡检测要求 `sdc_passed`、`checked_steps=72`、每卡 CSV `checked_steps=36` 且 `err_count=0`，设备与当前 Job 完整匹配。生成参考不等于 SDC 检测通过。

Web 使用现有 listener。例如 Web 监听 `:5001` 时，直接访问 `http://<node-address>:5001/stress/`，不新增第二端口或转发。页面和 CLI 查看同一个 Controller Job；取消始终使用 Job ID。当前 Run/Cancel 尚无认证/RBAC，访问应限于可信网络。

## 已验证和未验证范围

本次两模型双卡短测均 PASS，进程退出后设备恢复空闲。GPT3 采样 HBM 最高 14159 MiB，Llama2 16402 MiB；AICore 最高约 53%，温度峰值 88°C。这些是采样值，不是精确分配器峰值，也不是满载或温度安全认证；不要在另一节点盲目继续加压。

完整 64/2048 步、隔夜稳定性及 A3 硬件为 `NOT_RUN`，持续满载为 `NOT_PROVEN`。本预设不把其他 A2 型号或 CANN 版本自动视为已验证。A3 应保留自己的完整配置和兼容 profile，并在对应软件栈重新生成参考，参见 [模型流程](../../../docker/NPU_MODEL_SDC_GUIDE.md) 和 [A3 迁移指南](../../../docker/NPU_A3_MIGRATION_GUIDE.md)。
