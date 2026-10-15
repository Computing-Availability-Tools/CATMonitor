# GPT3 和 Llama2 模型压测开发候选指南

本候选为 NPU Burn 的 GPT3、Llama2 增加独立的模型任务入口，覆盖参考生成和逐步 SDC 对照。当前 A2 演示已验证算子执行、模型基础 HBM 筛查和完整标准训练的 OOM 失败链；没有完成模型参考生成/SDC 成功或 A3 硬件验收，也没有发布本轮新镜像。不要把新参数套到旧发布镜像；使用配套候选源码和 Control/NPU workload 镜像。

算子模式也启用自身 SDC 检测。这里的“模型 SDC”专指整模型训练结果与健康参考的逐步对照，不是关闭或替代算子的检测。

## 两种模型的压测内容

| 项目 | GPT3 用例 | Llama2 用例 |
| --- | --- | --- |
| 上游模型实现 | Transformers GPT2Model 加训练头 | Transformers LlamaModel 加训练头 |
| 模型来源 | 按配置随机初始化 | 按配置随机初始化 |
| 输入来源 | 固定种子生成 token 和类别 | 固定种子生成 token 和类别 |
| 每步执行 | 前向计算、loss、反向传播、梯度范数、AdamW 更新、参数数值校验 | 同左 |
| 当前模型精度 | bfloat16，不做精度替换 | bfloat16，不做精度替换 |
| SDC 比较项 | loss、grad_norm、checksum | 同左 |

不需要下载预训练权重、业务数据集或商业 GPT3 模型。名称表示上游压力用例，不代表商业模型训练或模型质量评测。

`checksum` 是上游参数数值签名，不是密码学文件哈希。上游当前使用绝对误差阈值：loss 0.01，梯度范数 0.1，参数签名 0.1。候选不修改这些阈值。

完整基础配置为 2048 个测量步骤加 64 个预热步骤，batch size 32、序列长度 512、隐藏维度 1280、36 层、20 个注意力头。两种模型的词表不同。没有裁剪完整 A3 配置；实际 HBM、内存和耗时需实机验证，作业时限不是持续压测时长。

启动前按本次逻辑设备逐卡读取可用 HBM，并在 meta device 统计实际模型参数。基础最低占用按 BF16 权重、梯度和两份 AdamW 状态合计 8 字节/参数筛查；这不是完整训练峰值，未计入激活、上下文及临时空间。双卡显存不相加，A2 不因型号或历史 OOM 被禁止启动。基础通过后仍可能 OOM；此时报告模型、逻辑设备和原始原因，不发布有效参考。Web 的“模型 SDC”按钮与算子压测并列显示，所有准入算子直接展示。

## 原有缺口和候选补充

| 原有缺口 | 候选补充 | 验收边界 |
| --- | --- | --- |
| 模型被作为不可选算子排除 | 新增独立模型请求，不把模型混入算子列表 | 不改变原算子准入 |
| 未保证 Transformers 依赖 | 检查 GPT2Config、GPT2Model、LlamaConfig、LlamaModel 的真实导入 | 构建检查不是硬件认证 |
| wheel 安装使用 no-deps | 明确要求 runtime base 已准备完整模型依赖；缺依赖时构建失败 | 不默默下载未固定版本 |
| 参考与 trace 默认写入安装目录 | 显式 model-sdc-v1 补丁提供运行目录 | 根文件系统继续只读 |
| 缺少参考生成入口 | create_reference 任务 | 单逻辑设备、独立参考 ID、禁止覆盖 |
| 缺少参考匹配和完整性检查 | 绑定配置、代际、Python、软件版本及包内容；核验文件哈希与全部步骤 | A2 参考不能直接当作 A3 参考 |
| 参考生成与检测容易混淆 | reference_generated 与 sdc_passed 两种结果 | 生成参考不证明硬件健康 |
| 报告只处理算子 CSV | 模型专用 CSV、保存/比较步数、错误数和历史结果 | 文件存在不等于成功 |
| 缺少模型 Web/CLI 调用 | 同一 Controller、同一 Job ID、同一 Cancel 路径 | 真实 Cancel 清理待实机验证 |

## 构建所需输入

使用合法公开来源的 CANN、torch、torch_npu 软件栈。runtime base 必须预装选定版本的 Transformers 及其依赖；从该软件栈的配套说明选择并固定版本，不能仅凭类导入成功宣称官方配套认证。

离线环境由管理员准备与 ARM64/Python ABI 匹配的 wheel 依赖闭包，并在构建 runtime base 时安装。不要在正在运行的 workload 容器里临时 pip 安装或覆盖包，也不要为这两个模型下载无关 vLLM、模型权重或训练语料。

候选统一位于个人 fork 的 `codex/stress-job-parameters` 分支，完整功能基线为 `f636dd000995d7693107197f1310151c29ba4941`。旧迁移分支不再作为新部署入口。迁移时记录并固定交付源码和配套镜像身份；源码提交不代表镜像已发布。当前配套引用、A3 五容器部署和逐步测试见 [候选交付指南](STRESS_JOB_RELEASE_GUIDE.md)。

以下是新镜像构建命令模板，`BUILDER_BASE` 和 `RUNTIME_BASE` 必须由管理员填入已确认的软件栈镜像，不使用任意 latest：

```bash
test -n "$BUILDER_BASE"
test -n "$RUNTIME_BASE"

# A2：沿用完整兼容补丁，同时保留完整模型配置。
bash scripts/stress/build_npu_burn_image.sh \
  --builder-base-image "$BUILDER_BASE" \
  --runtime-base-image "$RUNTIME_BASE" \
  --image catmonitor/stress-npu:model-sdc-a2-candidate \
  --compat-profile a2-upstream-model-sdc-memory-job-v1 \
  --patch scripts/stress/patches/ascend_npu_burn/portable-runtime-v1.patch \
  --patch scripts/stress/patches/ascend_npu_burn/a2-upstream-base.patch \
  --patch scripts/stress/patches/ascend_npu_burn/model-sdc-v1.patch \
  --patch scripts/stress/patches/ascend_npu_burn/model-memory-v1.patch \
  --patch scripts/stress/patches/ascend_npu_burn/job-parameters-v1.patch \
  --enable-model-sdc \
  --build-root /path/on-data-disk/npu-model-build
```

A2 使用完整 base_config，不添加 a2_config 或缩减算子配置。A3 使用 `--compat-profile portable-model-sdc-memory-job-v1`，按序应用 portable-runtime-v1、model-sdc-v1、model-memory-v1、job-parameters-v1，去掉 A2 补丁；完整 a3_config 保持不变。不要用 profile=none 描述带补丁的产物。

Control 镜像也需从该候选源码重新构建以包含新协议、CLI 和 Web。CPU workload 不负责模型，但本轮还增加 CPU 按作业参数；启用 CPU Stress 时使用同批配套 CPU 镜像。

启用模型构建检查后，最终 runtime image 必须输出 `CATMONITOR_MODEL_IMPORT=PASS`。此步骤不运行训练、不访问 NPU 设备，也不生成参考。

## 部署和授权

仍然使用既有四容器 NPU 部署：catmonitor、Web、DFeE、stress-npu。Web 不挂 Docker socket。模型不引入独立 runner、端口、容器或第二套 Controller。

生成配置时显式授权模型及参考生成：

```bash
bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image "$CONTROL_IMAGE" \
  --npu-image catmonitor/stress-npu:model-sdc-a2-candidate \
  --npu-chip-generation A2 \
  --npu-burn-device 0 \
  --npu-allowed-devices 0,1 \
  --npu-allowed-models gpt3,llama2 \
  --allow-npu-reference-generation \
  --npu-model-memory-mode checkpointed --npu-job-timeout 2h \
  --enable-web
```

示例中的 0、1 是运行时逻辑编号，必须以本节点实际 doctor/拓扑结果为准，不是宿主 `/dev/davinci2`、`/dev/davinci5` 的编号。生成器沿用设备发现机制，不为模型任务扩大设备映射或权限。

`standard` 保持原训练方式；`checkpointed` 显式启用激活重计算，完整 batch、模型形状、精度和步数不变。默认 standard，不自动切换。该模式绑定参考身份，不能混用参考；不支持 Transformers checkpoint API 时拒绝，不降级。它尚未完成 A2/A3 完整训练验证。`2h` 是整个作业的超时上限（7200 秒），不是预计耗时，也不改变单用例内部同步超时。

对应 YAML 新增字段仅位于 `stress.benchmarks.npu_burn`：

```yaml
allowed_npu_models: [gpt3, llama2]
allow_npu_reference_generation: true
```

两个字段缺省时不允许模型任务，原有算子调用继续工作。关闭参考生成权限后仍可以使用已有匹配参考进行检测。

启用参考生成意味着允许建立新的检测基准。当前 Web 尚无认证/RBAC，不要向不可信网络暴露启用了模型任务的 `:19322`。

沿用 [NPU 部署 README](README-npu.md) 的四容器 Compose 或 manual docker run 路径，使用新 Control、新 NPU image 和这份生成配置。参考位于 workload 的 `/var/lib/catmonitor/stress/model-references`，复用已有持久化 state 卷。结果和 trace 位于本轮 output 子目录，临时训练 cwd/HOME 使用既有可写目录，根文件系统保持只读。

### 迁移到另一台 A2 / A3

| 目标 | 镜像使用方式 | 必须确认 |
| --- | --- | --- |
| 另一台 A2 | 可以分发同一已验证的 ARM64 候选镜像，不要求每台重编译 | 宿主驱动与镜像软件栈兼容、实际 logical 拓扑、设备空闲及每卡 HBM；不能凭 A2 型号认定完整模型可训练 |
| A3 | 当前无本候选已验证 A3 workload 镜像，先用公开构建脚本及 A3 profile 构建；以后有合格 A3 镜像也可直接取得 | 不使用 A2 专用补丁；保留完整 a3_config；新参考在 A3 对应栈上生成 |

不是 A3 必须手写 Dockerfile，而是需要与该节点匹配的 NPU workload。统一使用 `build_npu_burn_image.sh` 驱动仓库 Dockerfile。Control 的协议/界面和 CPU workload 不因 A2→A3 产生新的职责；兼容门禁通过时不要求重复构建 CPU 镜像。CANN 位于 workload 内，宿主驱动只读挂载。

本轮候选目前只有演示节点本地 tag，尚无新的 registry digest。Git 源码提交与镜像发布是两件事；没有发布镜像时，不能把旧 GHCR tag 当作这次候选。[A3 构建和四容器部署](NPU_A3_MIGRATION_GUIDE.md)覆盖所需输入和逐步验收。

## 完整操作流程

### 环境预检

```bash
docker exec catmonitor catmonitor stress doctor \
  -c /etc/catmonitor/catmonitor.yaml -o json
```

Doctor 中普通 npu_burn 的通过不等于模型参考预检通过。提交模型任务时，Controller 会额外核验模型依赖、参考和本次设备。运行前确认设备空闲；不要因为设备可见就认为可以压测，也不要为验收杀业务进程。

### 算子压测

```bash
# 逻辑设备和算子属于本次 Job，不修改共享 YAML 或容器默认环境。
docker exec catmonitor catmonitor stress run \
  --bench npu_burn --npu-devices 0,1 --npu-cases matmul \
  -c /etc/catmonitor/catmonitor.yaml -o json
```

按本节点准入列表可指定多个算子，例如 `--npu-cases matmul,conv2d`。A2 不支持的 quant_matmul 不会套用 A3/A5 策略。完整结果必须核对请求设备、算子、本轮 CSV 与错误数；doctor 或目录中有 CSV 不代表本轮通过。

### 生成参考

先使用已确认健康、软件栈固定的环境，选择一个明确授权的逻辑设备：

```bash
docker exec catmonitor catmonitor stress run \
  --bench npu_burn --npu-devices 0 \
  --npu-model gpt3 --model-action create_reference \
  --reference-id gpt3-a2-baseline-01 \
  -c /etc/catmonitor/catmonitor.yaml -o json
```

必须得到 `outcome=reference_generated`，保存步骤覆盖全部预热及测量步骤、错误数为 0。参考校验和发布成功前，staging 目录不能作为检测输入。已有参考 ID 不覆盖；换配置或软件栈后使用新的 ID。

第一次生成的参考不是天然正确答案。生产检测参考应来自已确认健康的环境，并做独立重复对照；稳定存在的硬件错误可能被写入参考。

### SDC 对照

```bash
docker exec catmonitor catmonitor stress run \
  --bench npu_burn --npu-devices 0,1 \
  --npu-model gpt3 --model-action compare \
  --reference-id gpt3-a2-baseline-01 \
  -c /etc/catmonitor/catmonitor.yaml -o json
```

多设备在此候选中逐设备顺序训练和比较，不宣称分布式训练。必须得到 `outcome=sdc_passed`，每台设备的比较步数完整、错误数为 0、CSV 的 Job ID 和设备覆盖与请求相符。少一台设备或只比较部分步骤不能算通过。

Llama2 使用相同两步流程，把模型换成 `llama2`，使用单独的 `llama2-a2-baseline-01` 参考 ID。A3 必须在 A3 对应软件栈下重新生成自己的参考；不能复用 A2 参考冒充 A3 认证。

### Web 和取消

在现有 `:19322/stress/` 选择 NPU Burn，再点“模型 SDC”。GPT3/Llama2 直接显示，默认执行 SDC 检测，不显示操作下拉；填写已生成的参考 ID，选择授权逻辑设备，点击“开始 SDC 检测”。

首次没有参考时，展开“首次使用：准备参考”，点击“切换到生成参考（单卡）”，填写一个新的参考 ID，并选择一张已确认健康的卡；主按钮会明确变成“生成参考”。生成成功后再返回 SDC 检测，使用同一 ID。不会因参考不存在而自动生成，不会静默覆盖参考；未授权生成的部署只允许使用已有参考。软件栈、配置、代际或显存策略变化后，需要匹配的新参考。

提交的是同一 Controller Job，CLI status 与 Web 查看同一结果：

```bash
docker exec catmonitor catmonitor stress status --job '<job-id>' -o json
docker exec catmonitor catmonitor stress cancel --job '<job-id>'
docker top catmonitor-stress-npu
```

取消后必须确认模型训练进程及其子进程退出；模型检测错误、启动失败、外部超时都不能返回 Healthy。取消始终按 Job ID，不按当前页面设备选项执行。

## 本地测试和待验收项

已执行：27 项 Python 合成回归，应用真实上游补丁并调用其 SDC 比较类，核验逐卡 HBM、OOM 原因、完整 A2/A3 配置保持和显式重计算 API；模型协议/CLI/结果解析单测；前端实际脚本 DOM 回归；Shell 语法；完整 Linux Stress 回归、race、vet 和 Linux ARM64/Windows amd64 编译。

完整 Windows Stress 测试中，已有 Linux 专属运行/路径测试不能在 Windows 上通过。Linux Controller 模型测试、完整 `make test-stress`、Stress/Web race 和生成器 fixture 已在 WSL 实际执行通过。

现有公开来源 A2 r6 镜像中的 GPT2Model、LlamaModel 真实导入和设备初始化已通过。两项完整模型尝试均在首个前向计算中发生 HBM OOM，完成步骤为 0，没有有效参考。现有约 29.5 GiB HBM 的单逻辑设备在该执行方式下不足以完成完整配置；两台设备的容量不能直接合并供单设备用例使用。

2026-10-13 已将本候选的 Controller/Web 模型入口部署到 A2，并分别请求 GPT3 logical 0、Llama2 logical 1 完整标准训练。逐卡基础 HBM 预检通过，实际训练仍 OOM；Web 显示显存不足、任务未完成，原始原因包含模型与设备，不发布有效参考。测试没有缩减模型参数或启用激活重计算；后续内存优化必须显式记录执行方式，并重新完成训练、参考和 SDC 对照。

本轮 Control/NPU 镜像基于可运行的现有运行时组装新的增量候选，不是 clean-room CANN/Custom Ops 重编译，也未发布镜像。完整模型参考生成、跨设备 SDC 对照、模型特定 Cancel/超时、激活重计算模式与 A3 训练仍待验收。源码候选提交与推送不代表镜像发布或硬件通过。实际记录见 [模型验证结果](NPU_MODEL_TEST_RESULT.md)，A3 构建、Compose/manual 启动与验收见 [A3 迁移指南](NPU_A3_MIGRATION_GUIDE.md)。

进入实机前依次完成：Linux 回归 → 冻结候选源码 → 构建两张新镜像 → 独立部署 → 拓扑/空闲检查 → 单设备参考生成 → 单设备检测 → 授权多设备检测 → Web 查看同一 Job → Cancel → 进程清理。A2、A3 分别记录镜像 ID、软件栈、参考身份、步骤数及错误数，不能用旧镜像实验代替。
