# GPT3 和 Llama2 模型候选验证结果

候选源码位于 `codex/npu-model-a3-migration` 分支，本轮基于 `c1e3d6146ae04e1636bdb52c0afef853a65f03a8`，选择性合入独立模型候选的逐卡 HBM 筛查和 OOM 原因传递。修复以该分支包含本记录的候选提交为准。保留较新的显式激活重计算设计、完整 A2/A3 配置及只读运行契约，没有整体覆盖旧代码。

2026-10-13 的候选已在 A2/Ascend910B4 部署并经唯一 Controller 实测。候选镜像基于已有、可初始化 NPU 的运行时增加新的控制程序、执行器和显式兼容 Python overlay；不是重新编译 CANN/Custom Ops 的 clean-room 镜像，也不是正式发布证据。2026-10-12 的 r6 实验保留在下方，不能代替本轮结果。

## 已通过

| 门禁 | 实际结果 |
| --- | --- |
| 模型与参考 Python 合成回归 | 27/27 PASS，包含逐卡 HBM、实际参数统计、OOM 原因、完整 A2/A3 配置及显式重计算检查 |
| 显式兼容补丁应用 | PASS，测试在临时副本应用补丁后加载实际上游类 |
| ModelRequest、ModelResult、模型 CSV 协议测试 | PASS，Windows 本地执行 |
| CLI 模型参数测试 | PASS，Windows 本地执行 |
| 既有 resultparse 测试 | PASS，Windows 本地执行 |
| Web 算子和模型选择、提交、Job ID Cancel、参考生成结果、失败结果不误报 | PASS，实际 stress.js 的 DOM fixture |
| Shell 语法 | PASS，构建脚本、生成器、entrypoint |
| Linux ARM64 编译 | PASS，Stress、CLI、Web、DFeE |
| Windows amd64 编译 | PASS，CLI、Web、DFeE |
| Stress Linux vet | PASS |
| Linux Controller 模型测试 | PASS，实际执行 |
| 完整 Linux `make test-stress` | PASS，包含构建、生成器、部署和发布审计 fixture |
| Linux Stress/Web race | PASS，实际执行 |
| 真实 Transformers 类导入 | PASS，现有 A2 r6 镜像中的 GPT2Model、LlamaModel |

新增本地测试覆盖明确 HBM OOM 分类、Controller/历史持久化、报告拷贝隔离、显存策略及参考不混用、Web“未完成/通过/参考已生成”区分。实际 plugin describe 与 Controller 协议对照测试覆盖资产来源路径，以及进程返回 0、JSON 预检失败时必须拒绝。它们是无硬件测试，不是 checkpointed 模式的实机训练通过证据。

2026-10-13 最终代码的完整 Linux `make test-stress`、Stress/Web race、`go vet ./...`、Linux ARM64（CGO disabled）和 Windows amd64 编译、前端 DOM/部署 fixture、`git diff --check` 均通过。A2 候选 Control 另以节点 DCMI 头文件和库编译；这些结果不能代替 A3 的 DCMI 镜像构建或硬件验收。

本地 Python 合成回归不运行 NPU 训练。它覆盖参考生成、SDC 比较、跨设备对照、数值不一致、训练异常、部分步骤、过期/损坏参考、配置/代际/软件栈不一致、路径拒绝、禁止覆盖以及 Transformers 类/API 缺失。真实硬件训练使用随机初始化模型和固定种子输入，训练流程包含 NPU 前向、反向及 AdamW 更新；本轮在首次前向失败，尚未执行到反向或更新。不能把合成输入与模拟训练混淆。

## 2026-10-13 候选实机结果

只在演示环境更新 Control、Web、NPU 执行器，并增加 CPU workload；DFeE 和 Grafana 未替换。Web 不挂 Docker socket，NPU 保持只读根文件系统、restricted profile 和原有设备映射。显式兼容 profile 为 `a2-upstream-model-sdc-memory-v1`。

| 项目 | 本轮结果 |
| --- | --- |
| CPU 三项 | STREAM、HPL small（N=4096）、HPCG（32³、60 s）均完成并 PASS；不是大型性能认证 |
| NPU 双卡 matmul | logical 0、1，完整上游 base_config：420/420 用例 PASS，错误数 0 |
| 设备映射 | 宿主 `/dev/davinci2,/dev/davinci5`，运行时 logical 0、1；不混用两套编号 |
| 模型基础 HBM | GPT3 logical 0 最低 5.76 GiB；Llama2 logical 1 最低 7.34 GiB，逐卡检查均 PASS |
| GPT3 标准完整训练 | Controller 作业失败，`npu_hbm_oom`，logical 0，耗时 95.709 s；无有效参考 |
| Llama2 标准完整训练 | Controller 作业失败，`npu_hbm_oom`，logical 1，耗时 86.852 s；无有效参考 |
| Web/CLI 可见性 | Web 展示上述 CLI 作业及同一 Job ID；显存不足不被展示为硬件故障 |
| Web STREAM | 真实 API 请求，`initiator=web`，完整运行 PASS |
| Web NPU Cancel | 观测真实 `launch.py` 与 multiprocessing Worker，再按 Job ID 取消；作业 cancelled、进程退出、两卡恢复空闲 |
| 历史与服务保护 | 原 41 个历史 Job 全部保留；DFeE、Grafana 的容器 ID、镜像 ID、StartedAt 不变 |

运行中的候选镜像均为 linux/arm64：Control 为 `a82e01b932d3`，CPU workload 为 `4ff205552b84`，NPU workload 为 `174722143ee5`（Image ID 前缀）。它们仅是本轮本地演示 tag，不是新的 registry release。

随后仅更新演示 Web 为 `4dc9dfd91246`：项目按 HPCG、HPL、STREAM、NPU Burn 展示；GPT3/Llama2 直接选择，默认 SDC 检测；参考生成移到显式“首次使用：准备参考”入口，不自动生成或覆盖。实际浏览器已验证模型切换和准备/检测切换，没有启动新训练。其他 Control、CPU、NPU、DFeE、Grafana 的容器 ID、镜像 ID、StartedAt 均保持不变，Web 挂载/权限契约不变。

本轮 UI 更新后的前端契约测试、完整 Linux Stress、race、vet、Linux ARM64/Windows amd64 编译，以及模型迁移文档链接/fence/shell 语法检查均通过。前端回归覆盖缺少参考不能隐式生成、生成权限撤销、单卡生成约束、直接模型选择、请求动作和确认内容。源码修复提交与镜像分发分开；下方演示镜像尚未作为新的 registry 发行版发布，不能将源码推送等同于镜像发布或 A3 硬件通过。

HBM 基础筛查统计真实模型参数，在 meta device 上构造，不分配训练样本。最低占用仅包含 BF16 权重、梯度和最低两份 AdamW 状态（8 字节/参数），不包含激活及临时空间。每台设备约 29 GiB 可用 HBM，通过基础筛查后仍在完整训练中 OOM；两台卡的显存没有合并。

完整模型保持 36 层、隐藏维度 1280、20 个注意力头、batch 32、序列长度 512、bfloat16、64 步预热和 2048 步测量。两个请求都真正进入训练，不是合成训练；但未完成训练或参考生成，不能声明模型 SDC 实机通过。

本轮另修复了实际集成发现的三个缺口：模型内存兼容 profile 的算子目录准入、模型冷启动预检预算（60 s，Controller 留 15 s 传输余量；普通算子仍 30 s、CPU 仍 2 s）、模型预检资产的必要来源路径。没有通过修改训练规模、扩大设备映射或放开 NPU privileged 来通过门禁。

Cancel 采样脚本最初误用上游 console 名匹配，未识别实际 `launch.py`，因此没有把该次脚本失败记作产品通过；随后根据本轮保存的真实进程样本、作业取消结果和退出后的进程表完成独立核验。17 项 A2 算子全部可直接选择，但本轮新候选只重跑完整 matmul，不把准入列表或旧镜像记录当作其余 16 项的新镜像实测通过。

## 2026-10-12 A2 r6 历史能力实验

现有 r6 镜像 ID 为 `sha256:4405b2ceed9179b826e01554a1d8b9b0c76461c216dfa3508038e70554f6ccfb`，软件栈为 CANN 8.5.1、torch 2.9.0+cpu、torch_npu 2.9.0.post1+gitee7ba04、Transformers 4.57.6。真实模型类导入和逻辑设备初始化已通过，不代表官方软件栈配套认证。

主机有两个 A2 逻辑设备，训练前每台设备可用 HBM 约 29.1 GiB，主机可用内存约 374 GiB。完整配置保持 36 层、隐藏维度 1280、20 个注意力头、batch 32、序列长度 512、bfloat16，64 步预热和 2048 步测量。GPT3 后顺序执行 Llama2，单模型外部时限为 2 小时。

两项完整配置的真实尝试均失败：首次前向计算耗尽 HBM，完成训练步骤为 0，未生成有效参考。失败后进程退出，两个设备均恢复空闲。

| 模型 | 结果 | 耗时 | 主机进程峰值 RSS | NPU 峰值已分配 | NPU 峰值预留 |
| --- | --- | --- | --- | --- | --- |
| GPT3 | FAIL，HBM OOM | 56.23 s | 4.770 GiB | 27.609 GiB | 29.002 GiB |
| Llama2 | FAIL，HBM OOM | 48.54 s | 5.553 GiB | 27.208 GiB | 29.022 GiB |

本轮证明真实类能够导入、模型能够构造并开始前向计算，但不能证明这台 A2 单逻辑设备能完成完整训练。没有调整 batch、层数、序列长度、精度或步数；也没有启用激活重计算等新的内存优化。两台设备的 HBM 不能直接相加为一个单设备用例的容量。

此轮复用能初始化 NPU 的现有执行容器。测试进程仅将参考和 trace 输出指向独立目录，不修改包、模型参数或训练算法。这是训练能力验证，不是新增 Controller/Web 模型入口验收，生成的原始参考也不是可直接用于生产的身份绑定参考。

测试记录位于节点 `model-capacity-20261012` 独立目录，包含完整配置、真实类来源、步骤、峰值内存、耗时和错误。新建独立容器的首次初始化报驱动错误 507899；现有执行容器的同类检查通过，原因仍待明确，不将该现象认定为模型训练缺陷。

## 尚未通过验收

| 项目 | 状态和原因 |
| --- | --- |
| 完整 Windows Stress 测试 | NOT_PASS，三项既有 Linux 运行/绝对路径测试不适用于 Windows；不是全套通过 |
| 新运行时 clean-room 构建 | NOT_RUN；本轮部署的是现有运行时增量候选，未重新编译 CANN/Custom Ops |
| A2 标准完整模型训练 | FAIL_HBM_OOM；2026-10-13 新 Controller 作业已复现，两模型均无有效参考 |
| A3 模型训练和资源规模 | NOT_RUN |
| 激活重计算完整训练及 SDC 数值复现 | NOT_RUN，A2 和 A3 均待新镜像验证 |
| 模型成功参考生成、多设备 SDC 对照 | NOT_RUN，完整标准配置未生成有效参考 |
| 模型特定 Cancel、超时 | NOT_RUN；OOM 后进程退出已验证，不能代替这些场景 |

## 修改入口

模型请求通过 `npu_model` 与算子请求分离：CLI/Web → control.sock → 唯一 Controller → Docker Executor → workload-exec → 固定模型 adapter → 上游模型类。

新构建参数 `--enable-model-sdc` 校验真实模型导入；新生成参数 `--npu-allowed-models` 和 `--allow-npu-reference-generation` 控制管理员授权。未授权部署不允许模型任务。构建不训练，运行不覆盖参考，根文件系统保持只读。

参考与软件/配置匹配后才允许比较；结果必须核验 Job ID、模型、动作、参考 ID、实际设备、CSV 的每设备步骤数和错误数。参考生成返回 `reference_generated`，检测返回 `sdc_passed`，两者不能互换。

当前结论为公开开发候选，不是模型实机通过或正式镜像发布状态。操作见 [模型 SDC 指南](NPU_MODEL_SDC_GUIDE.md)，A3 所需脚本、镜像、四容器启动和验收见 [迁移指南](NPU_A3_MIGRATION_GUIDE.md)。原始实验日志和参考不进入 Git。
