# NPU runtime compatibility — public-source audit

Baseline: public CATMonitor main `c1821ca4fa2eec53eb2605dc263561c6076421d0`.
Working branch: `codex/npu-runtime-compat`. No private machine inputs used.
The bundled upstream tree remains pristine; modifications are explicit patches.
Historical experiments are not evidence for this source/image.

| ID | Audit finding | Classification | Modification / regression |
|---|---|---|---|
| A | Dockerfile imports custom ops without torch/torch_npu first; inherited LD_PRELOAD reaches command substitutions | confirmed unsafe order/capture; actual vendor output requires hardware/image | ordered real import, build-scoped preload isolation, clean machine-readable captures |
| B | const.py unconditionally accesses optional dtype APIs; internal imports use top-level common/benchmarks | confirmed | explicit portable profile, conditional dtype registry with errors, package-qualified imports including spawn |
| C | utils.py parses Socket with int and synthesizes contiguous node IDs; topology executes at CLI import | confirmed | sysfs CPU lists/online/cgroup bounds, sparse dict, selected-node affinity validation at execution |
| D | Go requires PCI count == device node count; upstream fallback invents eight devices | confirmed | explicit single-device mode, runtime count + CANN runtime PCI identity must match kernel device-to-PCI/NUMA evidence; reject unknown mapping |
| E | Engine and main block on queues without checking failed children | confirmed | bounded readiness/result waits, failure propagation, finally terminate/kill/join |
| F | result CSV freshness uses only mtime/size; concurrent Engines race on header creation | confirmed insufficient provenance / locking | fresh per-run output directory, semantic CSV/device coverage, locked header + row append |
| G | describe can return RC0 with preflight=fail | intentional data API; unsafe if consumers check only RC | opt-in healthcheck strict mode and regression |
| H | thread budget/runtime env selection absent from generated NPU config | confirmed | generator + Compose/manual environment contract |
| I | Run cannot select NPU devices independently of deployment defaults | confirmed missing capability | typed per-Job CLI/Web request, admin allowlist, fresh preflight, identical Describe/Run selection and archived actual CSV coverage |
| J | HPL execution has no live online/quota/memory admission and fixed image dimensions are easy to mismatch with ranks | confirmed | small/explicit deployment dimensions, isolated input, P×Q and effective CPU/memory gates in Describe and Run |
| K | Web listener test can finish while report/history writer still uses TempDir | confirmed fixture lifecycle defect | wait for Manager.Shutdown in fixture cleanup; 500 repeats |
| L | combining old A2 sizing patch with portable changes risks reinstating fallback topology; upstream sync timeout is not an Engine total budget | confirmed patch-composition / lifecycle boundary | separate ordered A2 sizing-only patch, Controller job budget, readiness budget and stream sync budget |
| M | ConfigManager top-level update replaces all base cases with A3's single quant_matmul entry | confirmed in frozen public source | named whole-case merge retains 18 operators + 2 models; independent A2 config, base/A3 byte-preservation regression |
| N | operator selection remains deployment-only; device-only CSV coverage cannot prove each requested operator ran | confirmed missing capability | typed per-Job npu_cases end-to-end, actual dtype/catalog admission, canonical CSV task names, device×operator coverage and history |
| O | no final installed-tree completeness gate against the patched package | confirmed missing build gate; no claim of current private-image omissions | Python/JSON inventory capture/validation after wheel install, missing/changed operator/model/config fixture failures |

Single-device sysfs mapping, vendor initialization, CANN/torch/custom ops ABI,
driver permissions, performance and actual NPU cleanup require new-image hardware
acceptance. No vendor software-stack certification is implied by import/ABI checks.

## Verification status

Local Linux/amd64 synthetic regression rerun on 2026-10-12; actual results are recorded below.
Real new-image build, offline build, A2/A3 hardware workload and new-image Controller/Web/Cancel acceptance: NOT_RUN.

## 修改位置与使用边界

- **A 构建**：[Dockerfile](stress/npu/Dockerfile)、[ascend_env.sh](stress/npu/ascend_env.sh)、
  [builder](../scripts/stress/build_npu_burn_image.sh)：仅在构建/元数据捕获范围隔离 preload，
  vendor stdout 转 stderr，记录所选脚本/版本来源，真实 Custom Ops 按 torch → torch_npu 顺序导入。
- **B/C/E 上游兼容**：[portable-runtime-v1.patch](../scripts/stress/patches/ascend_npu_burn/portable-runtime-v1.patch)：
  capability dtype、包内导入、无 import-time 拓扑、sysfs online/cgroup cpuset/进程 affinity 分层、
  Worker 失败/提前退出/超时传播和 finally 回收。大部分涉及文件只是包内 import 机械限定；
  原 bundled tree 不变。新 [launcher](stress/npu/launch.py) 优先 image-owned overlay，spawn 继承路径。
- **D/F 准入与结果**：[plugin_linux.go](../features/stress/workloadplugin/plugin_linux.go)：
  显式 single，不把 host ID 当 runtime index；每轮隔离 CSV，验证 header、设备覆盖、实际执行次数和错误。
  作业 HOME 仍在部署的限额 tmpfs 内，默认 output 链接到持久化本轮目录；清理 HOME 不删除 CSV。
- **G 严格预检**：[workload-exec](../features/stress/cmd/workload-exec/main_linux.go)：
  原 JSON 协议不变，新增 `--require-pass` 供健康检查按语义返回非零。
- **H 部署**：[generator](../scripts/stress/generate_stress_deployment.sh) / [Compose](docker-compose.stress.yml)：
  device mode、线程预算、可选 CANN 路径持久化；生成手工路径共用的 `npu-workload.env` 及其 hash。
  single 禁 privileged/drop ALL；full 既有策略保留；未增加 PID 上限。
- **输入/文档**：[受限环境](RESTRICTED_ENV_GUIDE.md)、[单设备步骤](README-npu.md#66-显式单设备兼容候选硬件验收-pending)、
  [站点模板](examples/site.env.example)、[只读检查](../scripts/stress/check_site_inputs.py)。没有新增 installer。
- **I/N 按作业选卡/算子**：[npu_selection.go](../features/stress/npu_selection.go)、CLI/Control/Web 请求、
  Docker executor、workloadapi、[前端](../features/stress/static/stress.js)：只增加类型化设备/算子列表，
  不开放任意环境变量或命令。默认/请求/固定/实际设备与最大/有效时限进入报告；Cancel 仍按 Job ID。
  [空闲检查](../features/stress/workloadplugin/npu_idle_linux.go) 是整节点保守策略，未知格式也拒绝，
  不是外部调度器资源预约或逐卡使用权证明。
- **J CPU/部署**：[HPL 资源检查](../features/stress/workloadplugin/hpl_resources_linux.go)、generator：
  N/NB/P/Q、small、容器名、allowlist、安全 profile；配置进入 env-file/Compose，Web 不编辑脚本。
- **K 测试清理**：[stress_test.go](../features/stress/stress_test.go)：仅修测试 fixture teardown。
- **L A2 组合**：[a2-portable-sizing.patch](../scripts/stress/patches/ascend_npu_burn/a2-portable-sizing.patch)：
  先 portable，再 A2 sizing；profile=`a2-portable-runtime-v1`，不使用 `none` 或旧 A2 patch 叠加。
- **M/O 完整性**：portable patch 的 `case_catalog.py` 与 `ConfigManager` 按名合并，
  A2 独立配置只调整 A2 matmul/pool；[package_inventory.py](stress/npu/package_inventory.py)
  校验最终 Python/JSON 安装树。未删除 bundled 源码，也不声称私有镜像实际缺了哪些文件。
  [算子指南](NPU_OPERATOR_GUIDE.md) 说明 18 个算子候选和逐项硬件 NOT_RUN 边界。

### 已确认、推测、待硬件三类结论

源码缺陷见 A–O。仅凭既往实验无法确认当前镜像会输出 preload 噪声、缺哪种 dtype、是否发生
模块遮蔽，也无法确认某版 CANN 支持设备子集。这些是待重现条件，不是当前新镜像的已验证事实。

单设备依赖公开 [CANN Runtime API 声明](https://github.com/hicann/runtime/blob/master/include/external/acl/acl_rt.h)
的 PCI 身份查询。接口缺失、查询失败、虚拟 PCI 与 sysfs 不同、NUMA 未知均 fail closed；
不能将 API 出现在公开头文件等同于 CANN 8.3/9.0 或非 PCIe 机器已经支持。
参见 [运行时设备语义](https://github.com/hicann/runtime/blob/master/docs/zh/api_ref/04_device_management.md)。
新 profile 不叠加旧 `a2-cann83.patch`；A2 使用独立 sizing-only patch，也不声明 A2/A3/A5 软件栈认证。
缺少字符设备到 PCI ancestry 的虚拟 devdrv 节点不能证明 single 映射，当前明确拒绝。
多节点设备子集、单独选择忙卡之外空闲卡的精确准入，仍未实现；完整映射内部按 Job 选卡已实现。

## 开发阶段回归与未验项目

以下为源码开发阶段记录；后续有限硬件验证见文末的日期化更新。

运行入口：`make test-stress`、`go test ./...`、`make test-stress-race`、`go vet ./...`，
及 `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...` / Windows amd64 build。
构建 fixture 只模拟 Docker；不等于真实 CANN wheel/image 构建。

| 门禁 | 本地结果/覆盖 |
|---|---|
| portable patch 回归 | PASS（13 项，各在 portable 与 A2 组合运行一次）：可选 dtype、同名模块来源/真实 spawn、稀疏/离线 CPU、cgroup 子集、16-device 合成完整拓扑、单设备非首 PCI/错误选择/无证明拒绝、Worker 导入失败/部分启动失败和回收、并发 CSV、独立时限、完整 case 合并/独立 A2/不支持算子拒绝 |
| preload 回归 | PASS：本地编译真实噪声 constructor 动态库；metadata stdout 不污染且调用方环境恢复 |
| Go plugin/strict describe | PASS：单设备证明正/负例、旧 CSV 不复用、CSV FAIL/err_count/无实际执行/缺设备拒绝，RC0+preflight=fail 不被 strict healthcheck 接受 |
| build/deployment fixtures | PASS：旧 A2/profile=none 回归、split/shared base、缺失离线 Go base、generator/Compose/manual 合同与只读输入检查（2 项）失败非零 |
| make test-stress | PASS：Monitoring 兼容、UT、构建/部署/audit fixtures、typed plugin describe/run/cancel 无硬件 E2E |
| NPU 按 Job 选卡/算子 | PASS：CLI/Web 请求、唯一 Controller 并发/跨入口取消、allowlist、忙碌拒绝、history/hash、旧镜像拒绝显式算子选择、每个设备的算子覆盖不完整拒绝；真实 workload executor + 合成 CSV 的 E2E |
| Web 设备/算子选择 | PASS：`node tests/e2e/stress_web_selection_test.cjs`（亦可 `make test-stress-web`）；实际 JS + DOM fixture，默认/授权/不支持算子/空选择/POST/按 Job Cancel。不是浏览器或硬件实测 |
| 最终包完整性 | PASS（2 项 fixture）：算子/模型/配置缺失或内容变化拒绝，Dockerfile capture/validate 入口；真实镜像 build 未执行 |
| HPL 资源 | PASS：online∩affinity、v1/v2 quota/memory、P×Q、描述与独立运行输入一致，未新增 HPL 实机负载证据 |
| Web listener fixture | PASS：500/500；等待异步 manager/report/history teardown |
| 全仓 Go / race / vet | PASS：Linux 原生临时源码副本验证；Windows checkout 的旧 symlink fixture 为普通文件，直接测试会失败，未改该模块 |
| Linux ARM64 / Windows amd64 | PASS：纯 Go 交叉编译，不包含 DCMI 厂商 CGO 链接/真实执行 |
| 新 NPU wheel/image + ARM64 驱动 | NOT_RUN |
| 单设备真映射、Burn/新 CSV、Web 同一作业、Cancel 真实回收 | NOT_RUN；清单见受限环境指南第 5 节 |
| 完全离线依赖闭包 | NOT_RUN；CPU 全离线构建未实现，不能将输入清单检查作为 PASS |

未修改 Controller/Executor 架构、正式版本号；未提交/推送、发布镜像或创建 tag。

## 未闭环项（不得写成硬件 PASS）

1. 真实新 NPU wheel/image、A2 matmul/Burn CSV 和 Cancel；需要获准基础依赖、足够空间及正常 Docker init。
2. single 的 CANN PCI 查询/驱动 sysfs 映射；API 不可用或虚拟节点无 ancestry 必须 UNVERIFIED。
3. A3 的 logical 0/7/8/14/15、16 个映射及 PCI/runtime 一致性；仅有合成 16-device 回归。
4. 外部调度器抢占/资源预约、精确逐卡 busy 识别、任意多节点子集不在本次实现中。
5. CPU 容量门禁目前针对 HPL，不把它描述为 STREAM/HPCG 热插拔的完整支持。
6. 新镜像逐算子 A2/A3 矩阵：目前全部硬件 NOT_RUN。A2 quant_matmul 在本公开源码无策略，
   明确拒绝；其他 case 的配置/dtype 准入不等于硬件支持。不能凭 A3 的历史 quant_matmul
   成功宣称此次恢复的完整算子目录均已通过。

## 2026-10-12 A2 演示更新（非发布认证）

- 使用公开 `quay.io/ascend/vllm-ascend:v0.18.0`，CANN 8.5.1、torch 2.9.0、
  torch_npu 2.9.0.post1，真实构建了 `a2-portable-runtime-v1` 候选。
  最终 NPU image ID：`sha256:a8d0dc4c6c89a563fc55912805c5ddaf9490c4ef5b6173ef01f1d6f6ee177608`。
- 构建、Custom Ops 导入、完整安装树、运行期 doctor 均 PASS。
- 完整映射两张 A2 设备节点后，Controller CLI 指定 logical `0`、`matmul` PASS：
  新 CSV 1 行、`result=PASS`、`err_count=0`，报告实际设备/算子与请求一致。
  作业结束后容器只剩 init/sleep。**这不等于 single-device mapping 模式验收。**
- 实机修复：CANN 原生退出诊断污染 JSON，现保持诊断 FD 重定向至解释器退出；
  Worker/Engine 固定 5 秒退出等待过短，现使用作业总 deadline，非零退出仍失败。
  新增 JSON 退出噪声/失败回归 2 项；portable/A2 两套各 14 项 PASS；`make test-stress` PASS。
- Web 已显示 CLI 同一作业和设备/算子选项；浏览器 Run 确认步骤阻塞，
  Web 发起双卡、多算子和真实 Cancel 仍 PENDING。其余算子及 A3/A5 仍 NOT_RUN。
- 未修改 bundled tree、版本号；未 commit/push、发布镜像或创建 tag。
