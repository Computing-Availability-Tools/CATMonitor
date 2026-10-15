# GPT3 / Llama2 模型 SDC

本页只说明模型用法和结果边界。配套镜像/源码、A2/A3 区分及容器部署统一见
[当前交付指南](STRESS_JOB_RELEASE_GUIDE.md)，构建和补丁见
[开发者指南](DEVELOPER_GUIDE.md)。不再提供另一套安装或迁移命令。

## 1. 测的是什么

| 项目 | GPT3 用例 | Llama2 用例 |
| --- | --- | --- |
| 实现 | Transformers GPT2Model 加训练头 | Transformers LlamaModel 加训练头 |
| 模型和输入 | 随机初始化，固定种子生成 token/类别 | 同左 |
| 每步 | 前向、loss、反向、梯度范数、AdamW 更新、参数签名 | 同左 |
| 精度 | bfloat16，不自动替换 | 同左 |
| SDC 对照 | loss、grad_norm、checksum | 同左 |

无需商业模型权重、业务数据或训练语料。名称来自上游压力用例，不是商业模型质量评测。
checksum 是参数数值签名，不是密码学哈希。算子压测也有自身 SDC，模型入口不替代它。

完整基础配置是 2048 个测量步骤 + 64 个预热步骤（2112 步），batch 32、序列 512、
隐藏维 1280、36 层、20 个注意力头；两模型词表不同。完整 base/A3 配置保留。
小参数/短训练只能证明该参数集的功能，不能写成完整配置或隔夜验证通过。

## 2. 显存、参数与参考身份

- 每个逻辑设备独立执行完整模型；多选设备是逐卡顺序对照，**不是 DDP/FSDP/模型分片**，
  不合并 HBM。DDP 也不自动把模型和优化器状态拆到其他卡。
- `standard` 为默认方式；`checkpointed` 显式采用保留 RNG 的非重入激活重计算，
  不改变 batch、shape、精度或步数。缺少所需 API 时拒绝，不自动降级，也不保证能放入显存。
- 启动前逐卡读取可用 HBM，并以 meta model 的实际参数数目估算 BF16 权重、梯度和两份
  AdamW 状态的最低占用（8 字节/参数）。这不包含激活、上下文和临时空间，预检后仍可能 OOM。
- 历史 A2 完整标准模型曾在首次前向 OOM；它不是所有 A2/配置“不支持”的判据。
  新镜像的完整训练与 A3 硬件验收仍为 **PENDING**，不得复用旧短训练结论。

Web“高级参数 · 仅本次作业”或 CLI 的 `--model-parameters` / 单字段 flags 可以修改
batch、序列、层数、维度、run_count 等。模型、动作和 reference ID 是独立请求字段：

```bash
catmonitor stress run --bench npu_burn --npu-devices 0 \
  --npu-model gpt3 --model-action create_reference --reference-id gpt3-node-smoke-01 \
  --model-batch-size 1 --model-seq-len 16 --model-hidden-size 32 \
  --model-layers 1 --model-attention-heads 4 --model-intermediate-size 64 \
  --model-vocab-size 128 --model-sample-pool 2 --model-num-classes 2 \
  --model-run-count 2 --model-warmup 1 -o json
```

该命令仅是小参数示例，不是新的硬件通过声明。比较时必须传相同参数；不能生成小参考再
用默认完整模型对照。运行时参数经预检后冻结，不修改共享配置。

参考绑定模型、完整有效配置、代际、软件/Python/包身份、显存策略与阈值，并核验文件
哈希和全部步骤。更换其中任一项需新参考 ID；已有 ID 禁止覆盖。A2 参考不直接用于 A3。
默认 loss/grad/checksum 绝对误差阈值为 0.01/0.1/0.1；按作业显式修改阈值也会改变
参考身份，不允许静默放宽旧参考。基础导入/ABI 通过不是官方软件栈认证。

## 3. 部署授权

模型沿用 NPU-only 四容器或 Full 五容器，不新增端口、Runner 或 Controller。
当前源码的 generator 在指定 NPU image 后默认开放 GPT3/Llama2 和参考生成。
以下参数可以显式重复默认授权，并选用 checkpointed 与任务时限：

```bash
--npu-allowed-models gpt3,llama2 \
--allow-npu-reference-generation \
--npu-model-memory-mode checkpointed \
--npu-job-timeout 2h
```

以上是已有命令的附加参数片段，不是独立命令。对应 Controller 授权字段为：

```yaml
allowed_npu_models: [gpt3, llama2]
allow_npu_reference_generation: true
```

旧 YAML 不自动开放；必须重新生成并让本项目 Controller 读取。
管理员可用 `--npu-allowed-models none` 关闭模型，或用
`--deny-npu-reference-generation` 只关闭参考生成；后者仍可使用已有匹配参考。
默认开放不自动启动任务、不自动生成参考，也不把缺失参考当作已支持/已通过。
设备以本节点 runtime logical ID 和 allowlist 为准，不能照抄宿主 davinci 编号。
容器启动允许查询不等于允许忙时压测；每次作业仍强制检查空闲，不杀业务。

参考保存在 workload 的 `/var/lib/catmonitor/stress/model-references`，须持久化；
结果/trace 保存在本轮 output。cwd/HOME 使用可写临时目录，镜像根保持只读。
Web 不挂参考目录或 Docker socket，通过同一 Controller 调用；Run/Cancel 无 RBAC，
只开放给可信网络。`2h` 是作业上限，不是预计耗时或持续负载保证。

## 4. 生成、比较、查看和取消

先 `doctor` 并确认授权设备空闲。普通 npu_burn 预检通过不代表本次模型参考已匹配。
首次使用要在已确认健康的软件/硬件环境生成独立参考：

```bash
docker exec catmonitor catmonitor stress doctor -o json
docker exec catmonitor catmonitor stress run --bench npu_burn --npu-devices 0 \
  --npu-model gpt3 --model-action create_reference --reference-id gpt3-node-full-01 -o json
# 完成且参考发布成功后，按同样的参数/显存策略对照。
docker exec catmonitor catmonitor stress run --bench npu_burn --npu-devices 0 \
  --npu-model gpt3 --model-action compare --reference-id gpt3-node-full-01 -o json
```

以上采用部署默认规模，完整 A2 模型可能 OOM。改成小规模时，生成和比较都传入同一组
训练参数。Llama2 换 `--npu-model llama2` 和独立 reference ID；多设备只使用已授权列表。
生成的参考不是天然正确答案，应在健康设备独立重复对照。

CLI `run` 等待终态；在另一终端或 Web 查看/取消：

```bash
docker exec catmonitor catmonitor stress status --job '<job-id>' -o json
docker exec catmonitor catmonitor stress cancel --job '<job-id>'
docker top catmonitor-stress-npu
```

Web `:19322/stress/`：NPU Burn →“模型 SDC”→设备/模型/参考 ID。默认是对照；首次
展开“首次使用：准备参考”切换到生成参考。不存在参考不会自动生成，生成失败的 staging
不能作为对照输入。“预检本次选择（不训练）”只显示未通过项，不创建 Job、不写参考。
参数、镜像或选择变化后须重新预检，启动前 Controller 还会重检。

## 5. 结果与真实验收

| 显示/结果 | 含义 |
| --- | --- |
| 预检通过 | 依赖、映射、参数/参考准入通过，不保证完整训练能运行 |
| `reference_generated` | 完整参考已保存，不等于 SDC 对照通过或硬件健康 |
| `sdc_passed` | 本轮每个请求设备的全部有效步骤对照通过，错误数 0 |
| 显存不足 / 未完成 | 有明确 HBM OOM，不伪装为硬件故障或通过 |
| 失败 / 取消 | 保留实际原因，无效/部分步骤不能通过 |

检查 Job ID、本轮 CSV、每卡完整预热/测量步骤、错误数及 reference/config 身份；
文件存在、更新或进程退出 0 都不能单独判 PASS。取消后须确认模型及子进程退出。

新软件栈分别执行：映射/空闲 → doctor → 单卡完整参考 → 同配置完整 SDC → 授权多卡
覆盖 → Web/CLI 同一 Job → 真实 Cancel/超时及回收。A2/A3 分别记录源码、镜像、参考、
步数和错误数；仅合成回归、短训练、构建或旧镜像结果不替代新镜像验收。
参数预设/只读外部配置见 [配置说明](../configs/stress/npu-model/README.md)，完整测试矩阵见
[测试指南](../features/stress/STRESS_TEST_GUIDE.md)。
