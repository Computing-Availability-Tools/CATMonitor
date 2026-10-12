# GPT3 和 Llama2 模型候选验证结果

候选源码位于个人 fork 的 `codex/npu-model-a3-migration` 分支，基于公开提交 `c1821ca4fa2eec53eb2605dc263561c6076421d0`。2026-10-12 的 A2 原配置实验复用当时 r6 镜像，没有替换服务镜像或配置。该实验不包含本分支后来新增的激活重计算模式，不能替代新镜像验收。

## 已通过

| 门禁 | 实际结果 |
| --- | --- |
| 模型与参考 Python 合成回归 | 15/15 PASS，包含完整 A2/A3 配置、A3 无 A2 补丁链及显式重计算检查 |
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

新增本地测试覆盖明确 HBM OOM 分类、Controller/历史持久化、报告拷贝隔离、显存策略及参考不混用、Web“未完成/通过/参考已生成”区分。它们是无硬件测试，不是 checkpointed 模式的实机训练通过证据。

最终分支的 Linux ARM64（CGO disabled）和 Windows amd64 编译、Linux 目标 vet 以及前端 DOM 测试通过。迁移文档链接、fence 和四容器/补丁身份契约通过；最后一轮 WSL 审批连续超时，最终文档 Bash 语法、文档更新后的完整 deployment fixture 和最终 race 重跑尚未执行。先前整合版本的完整 Stress/race 已通过，不能代替这些最终补测。CGO-disabled 编译也不能代替 A3 的 DCMI 镜像构建。

本地 Python 合成回归不运行 NPU 训练。它覆盖参考生成、SDC 比较、跨设备对照、数值不一致、训练异常、部分步骤、过期/损坏参考、配置/代际/软件栈不一致、路径拒绝、禁止覆盖以及 Transformers 类/API 缺失。真实硬件训练使用随机初始化模型和固定种子输入，训练流程包含 NPU 前向、反向及 AdamW 更新；本轮在首次前向失败，尚未执行到反向或更新。不能把合成输入与模拟训练混淆。

## A2 完整模型能力验证

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
| 新 Control 与 NPU 镜像构建 | BLOCKED_DISK，共享 Docker 分区空间不足；未执行构建 |
| A2 完整模型训练 | FAIL，GPT3/Llama2 首次前向均 HBM OOM，0/2112 步，无有效参考 |
| A3 模型训练和资源规模 | NOT_RUN |
| 激活重计算完整训练及 SDC 数值复现 | NOT_RUN，A2 和 A3 均待新镜像验证 |
| 实机跨入口可见、Cancel、超时和进程清理 | NOT_RUN，当前仅接入既有生命周期实现 |

## 修改入口

模型请求通过 `npu_model` 与算子请求分离：CLI/Web → control.sock → 唯一 Controller → Docker Executor → workload-exec → 固定模型 adapter → 上游模型类。

新构建参数 `--enable-model-sdc` 校验真实模型导入；新生成参数 `--npu-allowed-models` 和 `--allow-npu-reference-generation` 控制管理员授权。未授权部署不允许模型任务。构建不训练，运行不覆盖参考，根文件系统保持只读。

参考与软件/配置匹配后才允许比较；结果必须核验 Job ID、模型、动作、参考 ID、实际设备、CSV 的每设备步骤数和错误数。参考生成返回 `reference_generated`，检测返回 `sdc_passed`，两者不能互换。

当前结论为公开开发候选，不是模型实机通过或正式镜像发布状态。操作见 [模型 SDC 指南](NPU_MODEL_SDC_GUIDE.md)，A3 所需脚本、镜像、四容器启动和验收见 [迁移指南](NPU_A3_MIGRATION_GUIDE.md)。原始实验日志和参考不进入 Git。
