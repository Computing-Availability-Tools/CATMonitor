# NPU Burn：按 Job 选择设备和算子（公开源码兼容候选）

本指南对应 `codex/npu-runtime-compat` 的开发候选，**需要同时更新 Control 与 NPU
workload 镜像**；不是现有已发布镜像的能力声明。未发布新镜像；实机结果必须绑定具体
候选镜像、节点软件栈、设备与用例。没有执行的项保持 **NOT_RUN**，尤其不能用 A2
结果代替 A3 验收。Web 地址/网络访问问题另行处理，不以 SSH 转发作为此功能前提。

## 1. 完整源码与配置

仓库 bundled 上游固定在 `381028b688a70e881d97477d7fa1ae8f2a26288e`，
`third_party/ascend_npu_burn/source` 保持完整且未直接修改。兼容修改通过显式 patch
应用到构建副本，不能使用 `--compat-profile none` 描述应用了这些修改的产物。

公开上游的公共配置有 **18 个算子 + 2 个模型**，但 `a3_config.json` 仅定义
`quant_matmul`。原来的顶层 JSON `update` 会用它替换整个公共 `cases` 列表；
兼容补丁改为**按 case 名覆盖**：保留其他公共 case，仅完整替换同名 case，
不混合 A3 的 M/K/N 与公共配置的 MKN 参数。不是把不存在的 A3 专用配置编造出来。

A2 使用独立 `a2_config.json`，只覆盖默认 matmul：FP16、4096×4096、20 次，
A2 Worker 数据池为 1 GiB。公共 `base_config.json` 与 `a3_config.json` 不被 A2
补丁改写。其他算子保留公共规模，可能很大，**不得把 A2 默认 matmul 的规模当作所有
算子的规模**。源码中模型/算子目录均保留；模型需要参考数据，不作为本轮可选算子。

上述缩减配置属于快速 smoke 候选 `a2-portable-runtime-v1`，不用于完整公共配置验收。
完整参数测试应使用下文的 `a2-upstream-base-v1`，不要混用两个候选的实机记录。

构建过程记录补丁 SHA-256，并检查最终安装 overlay 中的 Python/JSON 文件与已打补丁
源码一致；缺少算子、模型、配置文件或内容变化均构建失败。真实 Custom Ops 导入校验仍保留。
这些检查不等于 CANN/torch 软件栈官方配套认证或真实算子执行通过。

## 2. 构建候选

只使用获准且可合法获取的基础镜像。先设置 `NPU_BASE_IMAGE`、`GO_BUILDER_IMAGE`、
`NPU_WORKLOAD_IMAGE`、`BUILD_ROOT`；依赖、代理和离线边界见
[受限环境指南](RESTRICTED_ENV_GUIDE.md)，Control 构建见[开发者指南](DEVELOPER_GUIDE.md)。
以下 profile **互斥**，不同产物不要共用同一个 tag。

A3 兼容候选（不含 A2 缩小规模补丁）：

```bash
bash scripts/stress/build_npu_burn_image.sh \
  --base-image "$NPU_BASE_IMAGE" \
  --go-image "$GO_BUILDER_IMAGE" \
  --image "$NPU_WORKLOAD_IMAGE" \
  --build-root "$BUILD_ROOT" \
  --compat-profile portable-runtime-v1 \
  --patch scripts/stress/patches/ascend_npu_burn/portable-runtime-v1.patch
```

A2 兼容候选（补丁顺序必须保持）：

```bash
bash scripts/stress/build_npu_burn_image.sh \
  --base-image "$NPU_BASE_IMAGE" \
  --go-image "$GO_BUILDER_IMAGE" \
  --image "$NPU_WORKLOAD_IMAGE" \
  --build-root "$BUILD_ROOT" \
  --compat-profile a2-portable-runtime-v1 \
  --patch scripts/stress/patches/ascend_npu_burn/portable-runtime-v1.patch \
  --patch scripts/stress/patches/ascend_npu_burn/a2-portable-sizing.patch
```

不要叠加历史 `a2-cann83.patch`。构建期不运行真实 NPU workload。

需要在 A2 验证上游完整公共参数时，改用独立的完整配置候选，不叠加 sizing：

```bash
bash scripts/stress/build_npu_burn_image.sh \
  --base-image "$NPU_BASE_IMAGE" \
  --go-image "$GO_BUILDER_IMAGE" \
  --image "$NPU_WORKLOAD_IMAGE" \
  --build-root "$BUILD_ROOT" \
  --compat-profile a2-upstream-base-v1 \
  --patch scripts/stress/patches/ascend_npu_burn/portable-runtime-v1.patch \
  --patch scripts/stress/patches/ascend_npu_burn/a2-upstream-base.patch
```

此 profile 的准入与执行均读取原始 `base_config.json`，没有 `a2_config.json` 覆盖，
没有缩减 Worker 数据池。matmul 保持 100 次、5 dtype、7 shape、6 pattern；
所有 case 参数与原公共配置一致。A3 原配置和覆盖规则不变。
这不是 A2 支持所有公共用例的承诺：A2 QuantMatmul 缺少策略仍被拒绝，
gpt3/llama2 仍需单独准备 SDC 参考。先检查设备空闲和资源预算，再逐项发起 Job，
逐项核对本轮 CSV 的 case、device、result、run_count 和 err_count。
报告应区分 PASS、FAIL、UNSUPPORTED、NOT_RUN，不能将不可执行项计为通过。

## 3. 部署默认值与授权范围

沿用 [NPU 部署步骤](README-npu.md)，使用新 Control 与 workload 镜像。以下示例仅适用于
管理员已确认 logical 0、1 均获准使用的 A2 完整映射；**不是把 `/dev/davinci2,5` 直接
转成 0、1 的通用规则**。logical ID 必须经当前运行时拓扑验证。

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image "$CATMONITOR_IMAGE" \
  --npu-image "$NPU_WORKLOAD_IMAGE" \
  --npu-chip-generation A2 \
  --npu-runtime runc \
  --npu-device-mode full \
  --npu-burn-device 0 \
  --npu-allowed-devices 0,1 \
  --npu-run-case matmul \
  --enable-web
```

generator 将默认设备、用例、线程预算等写入 `npu-workload.env`；Compose 与手工
`docker run --env-file` 使用同一配置。不得只靠手工 `docker exec -e` 修好一次运行。
没有 `--force` 的示例不会覆盖已存在部署。A3 应显式选择 A3，并填入实际获准的设备范围。

`--npu-burn-device` 和 `--npu-run-case` 是**部署默认值**。每次 Run 可以提交更小的
授权设备集合及受支持的算子名，不修改共享 YAML，不重建容器。设备可见不代表设备空闲；
当前忙碌检查是保守的整节点门禁，不等同于调度器资源预约。

## 4. 先看能力，再运行

以下为只读检查，不启动 Burn：

```bash
docker exec catmonitor-stress-npu \
  catmonitor-npu-burn-preflight --cases-json

docker exec catmonitor catmonitor stress doctor -o table
```

catalog 返回每项的 `name`、`dtypes`、`selectable`、`reason`、
`hardware_validation`，以及已解析的 `selected` 与配置 SHA-256。
**`selectable=true` 只表示源码/配置/dtype 准入通过，不表示实机已验证。**
`selection_error` 非空必须修正选择，不能只看命令 RC=0。

确认资源空闲及授权后，先执行单设备、单算子：

```bash
docker exec catmonitor catmonitor stress run \
  --bench npu_burn --npu-devices 0 --npu-cases matmul -o table

docker exec catmonitor catmonitor stress status -o table
```

逐项验证完成后，才组合已确认的设备和算子，例如：

```bash
docker exec catmonitor catmonitor stress run \
  --bench npu_burn --npu-devices 0,1 --npu-cases matmul,conv2d -o table
```

不支持的 dtype、算子、超出授权范围的设备，或明确提交的空列表均拒绝；不静默替换 dtype，
不把空选择变成全卡。旧客户端省略新字段时使用部署默认值；旧 workload 镜像无 catalog
时，不能使用显式按次选算子。

取消只针对 Job ID：

```bash
docker exec catmonitor catmonitor stress cancel --job '<job-id>'
docker top catmonitor-stress-npu
```

Web `/stress/` 从实际 catalog 生成设备/算子选择，只展示已准入的算子复选框；
不支持的项目及中文原因折叠展示，模型测试不混入算子选择。桌面两列、窄屏一列。
运行确认显示本次选择；CLI 与 Web 使用同一个 Controller，不另外建立执行入口。

选择区只说明“本次跑什么”，统一提示准入不等于实机验证，不按历史作业给算子贴标签。
作业报告说明“实际跑了什么、结果如何”：主卡显示 CSV 核验后的设备、算子、通过
记录数、错误数和作业耗时；明细按 CSV 的设备/算子汇总，用例数不是算子数或卡数。
请求/固定选择与实际结果不一致时突出提示，不折叠隐藏；默认值、哈希、文件路径
及累计用例时间放在执行详情。旧报告没有分组元数据时不推算明细。
超时、取消和运行失败不能解释成芯片不支持；历史成功结果也不是新镜像或 A3/A5 认证。

### Conv2D 即时编译与只读容器

CANN 某些 Conv2D/Cast 路径会在当前工作目录创建 `kernel_meta`。
workload plugin 将每个 Job 的 **cwd 和 HOME 一起**设置为有界 tmpfs 内的独立目录，
结束/取消时删除临时 JIT 文件，仅持久化本轮结果；容器继续使用只读根文件系统。
不要通过取消 `--read-only`、扩大权限或映射宿主源码目录绕过写入失败。

`portable-runtime-v1.patch` 将后台 `stream.synchronize()` 的异常传回调用线程，
不再把“线程已经结束”误判成“同步成功”；真正超时仍走原超时/进程回收路径。
这些是跨代际运行兼容修复，不删减公共或 A3 配置，也不构成 A3 实机认证。

请求中的类型化字段示例：

```json
{"benchmarks":["npu_burn"],"npu_devices":[0,1],"npu_cases":["matmul","conv2d"]}
```

Controller 固定本次选择，Describe 和 Run 使用同一份设备/算子参数。Report/History
记录部署默认、请求、选定、实际结果。每轮使用新 CSV 目录，要求**每个选中设备都覆盖
每个选中算子**，实际 runs>0、result 成功、err_count=0；额外或缺失设备/算子均不得
标记 Healthy。该门禁不等于对算子的所有 shape/dtype 边界完成认证。

## 5. A2 / A3 支持事实

| 算子集合 | A2 候选准入 | A3 候选准入 | 新镜像硬件结果 |
|---|---|---|---|
| matmul | 使用独立 A2 默认规模，检查 dtype | 公共配置，检查 dtype | A2 单 logical 0 PASS（2026-10-12；详见下文）；A3 NOT_RUN |
| quant_matmul | 拒绝：该公开源码未注册 A2 策略，不借用 A3/A5 策略 | 使用 A3 override，检查 dtype | NOT_RUN |
| 其他 16 个算子 | 保留公共配置，逐项检查 dtype | 保留公共配置，逐项检查 dtype | NOT_RUN |
| gpt3 / llama2 模型 | 代码保留；参考数据未配置，不开放为算子选项 | 同左 | NOT_RUN |

其他 16 个算子：`weight_quant_batchmatmul`、`matmul_atomic`、`matmul_add_fp32`、
`conv2d`、`fusion_attention`、`fusion_attention_grad`、`conv3d`、`permute`、
`matmul_sdma`、`matmul_sdma_jump`、`cube_vector_sdma`、`matmul_axpy`、`matmul_pow`、
`linear_softmax`、`simplified_transformer`、`gmm_backward_rmsnorm`。

不能凭公开配置、版本号、doctor 或历史实验把上述 NOT_RUN 改成 PASS。A3 16 个设备
的合成拓扑测试也不能代替真实硬件结果。

2026-10-12 的有限 A2 证据：CANN 8.5.1 / torch_npu 2.9.0.post1，完整映射双卡，
Controller 按 Job 选择 logical 0 + A2 FP16 4096×4096/20 次 matmul，
新 CSV 1 行 PASS、err_count=0、结束后无 Worker 残留。
镜像身份和实机补丁见 [审计更新](NPU_COMPAT_REVIEW.md#2026-10-12-a2-演示更新非发布认证)。
双卡、多算子、Web 发起及真实 Cancel 尚待完成；不将候选目录的 17 个可选项宣称为已认证。

## 6. 新镜像硬件验收清单

1. 记录源码、profile/补丁 hash、image ID、CANN/torch_npu 来源；确认安装树完整。
2. 确认获准设备空闲；验证 host nodes、runtime logical ID 和 NUMA 证据，不猜映射。
3. 单设备 doctor → Controller 发起单算子 Burn → 新 CSV 的设备/算子/runs/result/err_count。
4. Web 显示同一 Job，重复上述过程得到逐算子 A2/A3 支持矩阵；失败保留真实原因。
5. 仅组合已逐项通过的设备/算子；真实启动后 Cancel，确认所有本 Job Worker 退出。
6. 完成后才讨论连续/隔夜运行预算。单次任务 timeout 是上限，不是保证持续运行时长。

本轮不杀业务、不运行隔夜负载、不扩大设备权限。未执行步骤继续标记 NOT_RUN/PENDING。
