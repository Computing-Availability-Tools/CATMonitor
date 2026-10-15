# CATMonitor 可靠性压测使用指南

最新按作业参数候选的源码、镜像与 A3 完整部署入口统一见
[候选交付指南](../../docker/STRESS_JOB_RELEASE_GUIDE.md)。旧 pre-release 标签不包含
本轮全部能力；不可把“Web 可编辑”当成旧 workload 已执行新参数。

本文面向首次部署和日常操作 CATMonitor Stress 的节点管理员。用户只需要理解：

```text
CATMonitor Monitoring
+
optional CPU Stress（STREAM / HPL / HPCG）
+
optional NPU Stress（Ascend NPU Burn）
```

项目专属 IP、代理、账号、PAT 和实机绝对路径不得写入仓库。受限节点使用临时
环境变量或离线镜像包。

> 当前程序内部版本是 `0.3.5`，当前 ARM64 pre-release 镜像标签是
> `arm64-v0.3.5-stress`，目标发布线是 `v0.3.6`。

> 公开源码兼容候选新增按 Job 选卡、严格 CSV 覆盖和资源准入，**尚未发布到上述镜像**。
> 构建/部署/未验范围见 [受限环境指南](../../docker/RESTRICTED_ENV_GUIDE.md)；
> 缺陷及修改点见 [兼容审计](../../docker/NPU_COMPAT_REVIEW.md)。

## 1. 镜像与容器

### 按作业高级参数（开发候选，须配套更新镜像）

CLI/Web 仍调用唯一 daemon Controller。Web 的“高级参数 · 仅本次作业”默认折叠，
留空继承部署值；改动只进入当前 Job，不写共享 YAML/JSON，不改变下一次作业默认值。
新 Control 与对应 CPU/NPU workload 必须同步更新；旧 workload 收到显式参数会拒绝，
不会静默忽略。已发布旧标签不因本地开发自动获得该能力。

```bash
# STREAM 数组大小/内部重复次数仍为编译参数；这里只设置 OpenMP 线程。
catmonitor stress run --bench stream --stream-threads 2 --timeout 60s

# 必须 P×Q=MPI 进程数，且 ranks×threads 不超过有效 CPU 容量。
catmonitor stress run --bench hpl --hpl-n 1024 --hpl-nb 64 \
  --hpl-p 1 --hpl-q 1 --hpl-mpi-processes 1 --hpl-threads-per-process 1

catmonitor stress run --bench hpcg --hpcg-nx 16 --hpcg-ny 16 --hpcg-nz 16 \
  --hpcg-runtime-seconds 2 --hpcg-mpi-processes 1 --hpcg-threads-per-process 1

# shape 保持上游每个算子的结构；例中两矩阵收缩维 K 必须一致。
catmonitor stress run --bench npu_burn --npu-devices 0 --npu-cases matmul \
  --npu-operator-parameters '{"matmul":{"dtype":["float16"],"shape":[[[128,128],[128,128]]],"pattern":["gauss_random"],"run_count":2}}'
```

也可用 `--job-parameters-file params.json` / `--job-parameters-file -` 提交同一份
typed JSON；不能与逐字段 flags 混用。参数不是命令模板，不能指定路径、环境变量、
容器、可执行程序或未知字段（包括 `exec_count`）。内存预算只是下界筛查，不是独占资源
预留或峰值显存保证；设备授权和启动前空闲检查仍然有效。

NPU 容器部署默认 `CATMONITOR_NPU_REQUIRE_IDLE=0`，允许在业务运行时完成容器启动
和部署 doctor/healthcheck（驱动访问、运行依赖及拓扑仍须通过）。这不是“允许并发压测”：
Controller 在每次算子/模型作业准入时强制检查空闲，workload 在真正执行前再次检查；
忙碌、未知或不完整占用信息均拒绝，不杀业务、不自动排队。普通预检 PASS 不等于
资源预约。完整分层规则和只读检查命令见
[NPU 部署指南](../../docker/README-npu.md#有-npu-业务时的容器与管理查询)。
HPCG 的本次 nx/ny/nz 必须至少 16，避免上游自动扩展小网格造成“请求值≠执行值”。
历史部署默认小网格仍按上游规则扩展，但冻结回执记录扩展后的真实尺寸。

模型参数用 `--model-parameters '{"batch_size":1,"run_count":2}'`，或
`--model-batch-size` / `--model-run-count` 等单字段 flags；配合已有的模型、
action、reference ID 入口。SDC 的 `--sdc-loss-tolerance`、`--sdc-grad-tolerance`、
`--sdc-checksum-tolerance` 与训练参数分开，二者都绑定参考身份；任一变化都要求生成
匹配的新参考，不能用旧参考静默放宽比较。支持范围仍以代际策略、真实 dtype API 和
本次预检为准；A2 `quant_matmul` 不通过改 shape/batch 自动获得 A3 策略。

Controller 将本次完整有效参数及哈希冻结；Run 使用同一快照，即使随后共享配置变化。
报告保存 `requested_parameters`、profile 的 `frozen_parameters` 及开始执行后的
`execution_parameters`。NPU 另核对实际加载的算子配置/模型配置及本轮 CSV；请求、
预检、启动进程都不等于用例 PASS。取消仍针对 Job ID，保留既有进程组回收机制。

| 镜像 | 作用 | 是否必需 |
|---|---|---:|
| Control | daemon、Web、DFeE、CLI | 必需 |
| CPU workload | STREAM、HPL、HPCG、MPI/OpenBLAS | CPU Stress 可选 |
| NPU workload | CANN、torch_npu、NPU Burn | NPU Stress 可选 |

容器数量：Monitoring-only 3 个；CPU-only 4 个；NPU-only 4 个；CPU+NPU Full
5 个。CPU-only 用户不需要下载较大的 NPU workload 镜像。

当前 Linux/ARM64 Stress 专用镜像命名：

```text
ghcr.io/spike677/catmonitor-generic:arm64-v0.3.5-stress
ghcr.io/spike677/catmonitor-gpu:arm64-v0.3.5-stress
ghcr.io/spike677/catmonitor-npu:arm64-v0.3.5-stress
ghcr.io/spike677/catmonitor-stress-cpu:arm64-v0.3.5-stress
ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.5-stress
```

程序版本由项目 owner 保持为 `0.3.5`；`stress` 后缀表示该镜像来自 Stress 集成分支，
不是通用 0.3.5 或最终 v0.3.6 正式镜像。Image ID、digest 和 Git source commit
仍须随发布记录核对，不能复用 a2-r1。

GPU pre-release package 当前为 Private；GPU 用户拉取前需要完成 GHCR 身份认证。

## 2. 前置条件

所有节点：

- Linux；
- Docker Engine；
- 足够的镜像与 workload 运行空间。

使用推荐的 Compose 路径时额外需要 Docker Compose v2；使用各硬件 README 中完整的
`docker run` 路径时不需要 Compose。

CPU Stress：

- CPU workload 镜像与节点架构匹配；
- MPI rank、线程和问题规模不超过节点在线资源。

Ascend NPU Stress：

- Linux/arm64；
- Ascend driver、`npu-smi` 和 `/dev/davinciN` 正常；
- NPU workload 镜像的 CANN/torch_npu 与宿主机驱动兼容；
- 当前 Stress 镜像只覆盖已验证的 A2/Ascend910B4，其他 SoC 需单独验收。

```bash
docker version
docker compose version 2>/dev/null || true
uname -m
```

## 3. 获取 ARM64 Stress pre-release 镜像

```bash
git clone --branch codex/stress-job-parameters https://github.com/spike677/CATMonitor.git
cd CATMonitor
git switch codex/stress-job-parameters
git status --short

export CATMONITOR_RELEASE='arm64-v0.3.5-stress'
export CATMONITOR_REGISTRY='ghcr.io/spike677'
```

按节点选择 Control 镜像：

```bash
# 三选一
export CONTROL_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-generic:${CATMONITOR_RELEASE}"
# export CONTROL_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-gpu:${CATMONITOR_RELEASE}"
# export CONTROL_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-npu:${CATMONITOR_RELEASE}"

export CPU_STRESS_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-stress-cpu:${CATMONITOR_RELEASE}"
export NPU_STRESS_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-stress-npu:${CATMONITOR_RELEASE}"
```

只拉需要的镜像：

```bash
docker pull "$CONTROL_IMAGE"
docker pull "$CPU_STRESS_IMAGE"  # 仅 CPU Stress
docker pull "$NPU_STRESS_IMAGE"  # 仅 NPU Stress
```

不同硬件的完整 Compose 命令见：

- [Generic](../../docker/README-generic.md)
- [NVIDIA GPU](../../docker/README-gpu.md)
- [Ascend NPU](../../docker/README-npu.md)

## 4. 镜像来源边界

节点管理员应优先拉取同一 release 发布的 Control 和所需 workload 镜像，然后从第 5
节开始生成节点配置。构建 STREAM/HPL/HPCG 或 NPU Burn 镜像、选择 CANN base、配置
mirror/proxy、生成 manifest 和制作 RC tag 属于开发/发布职责，统一见
[镜像构建与发布开发者指南](../../docker/DEVELOPER_GUIDE.md)。

不要把不同源码提交、RC 后缀或未经对应硬件验收的 workload 镜像混在同一部署中。

## 5. 生成节点配置

```bash
sudo install -d -m 0750 \
  /etc/catmonitor/generated-stress \
  /var/lib/catmonitor/stress \
  /var/lib/catmonitor/stress/npu-burn-output
```

### 5.1 CPU-only

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image "$CONTROL_IMAGE" \
  --cpu-image "$CPU_STRESS_IMAGE" \
  --enable-web \
  --force
```

### 5.2 NPU-only

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image "$CONTROL_IMAGE" \
  --npu-image "$NPU_STRESS_IMAGE" \
  --npu-burn-device all \
  --npu-chip-generation A2 \
  --npu-runtime runc \
  --enable-web \
  --force
```

默认动态发现实际 `/dev/davinciN`；`all` 表示使用经容器 PCI topology 验证通过的
全部 NPU Burn logical devices。不要用 host 最大 device ID 推导设备数量。generator
会按映射数量输出 CANN runtime visible IDs；doctor 会在 workload 容器中核对
CANN、torch_npu、custom ops、PCI topology 和实际 `torch_npu` device count。

A2/CANN 8.3/runc 的已验证运行契约会让 `catmonitor-stress-npu` 使用
`privileged: true` 与 `network_mode: none`。这是 NPU workload 独有权限；Web、
DFeE 与 CPU workload 不应获得该权限。A3/A5 仍需独立实机验收，不能从 A2
结果外推。

### 5.3 CPU + NPU Full

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --control-image "$CONTROL_IMAGE" \
  --cpu-image "$CPU_STRESS_IMAGE" \
  --npu-image "$NPU_STRESS_IMAGE" \
  --npu-burn-device all \
  --npu-chip-generation A2 \
  --npu-runtime runc \
  --enable-web \
  --force
```

generator 只输出配置、profile、Compose override 和 deployment manifest；不会拉取镜像、
创建容器或执行压测。

## 6. 资源参数

网页不允许编辑脚本、命令、路径、MPI 或 NPU profile。管理员在生成配置时固定参数：

| 参数 | 含义 | 默认值 |
|---|---|---:|
| `--stream-threads` | STREAM OpenMP 线程；0 表示不强制 | 0 |
| `--hpl-processes` | HPL MPI ranks | 1 |
| `--hpl-threads` | 每 rank 线程 | 1 |
| `--hpcg-processes` | HPCG MPI ranks | 1 |
| `--hpcg-threads` | 每 rank 线程 | 1 |
| `--hpcg-nx/ny/nz` | 每 rank 局部网格 | 32/32/32 |
| `--hpcg-runtime` | HPCG 目标秒数 | 60 |
| `--npu-burn-device` | NPU Burn logical IDs 或 `all` | 必填 |
| `--npu-run-case` | 部署默认 NPU Burn case；新候选支持按 Job 选择 | `matmul` |
| `--npu-internal-timeout` | NPU 单 case 超时 | 300 |

参数必须按在线 CPU、内存容量、MPI ABI 和 NPU profile 评估。修改后重新运行 generator，
再用同一 Compose 命令执行 `up -d`。

## 7. 验证、运行和取消

Controller 会通过配置的 Docker Unix socket 自动协商 daemon API 版本；无需在
YAML、Compose 或容器环境中手工设置 `DOCKER_API_VERSION`。这也允许新版 Control
镜像连接项目支持的较旧 Docker daemon。

通过 Compose project/service label 找到 daemon：

```bash
DAEMON_ID=$(docker ps \
  --filter label=com.docker.compose.project=catmonitor \
  --filter label=com.docker.compose.service=catmonitor \
  --format '{{.ID}}' | head -n 1)

test -n "$DAEMON_ID"
```

先运行 doctor：

```bash
docker exec "$DAEMON_ID" catmonitor stress doctor \
  -c /etc/catmonitor/catmonitor.yaml -o table
```

再按需运行：

```bash
docker exec "$DAEMON_ID" catmonitor stress run --bench stream -o table
docker exec "$DAEMON_ID" catmonitor stress run --bench hpcg -o table
docker exec "$DAEMON_ID" catmonitor stress run --bench hpl -o table
docker exec "$DAEMON_ID" catmonitor stress run --bench npu_burn -o table
```

状态与取消：

```bash
docker exec "$DAEMON_ID" catmonitor stress status -o table
docker exec "$DAEMON_ID" catmonitor stress cancel --job '<job-id>'
```

一次作业可通过 `--timeout 120s` 缩短超时，但不能超过 YAML 中的项目上限。
NPU Burn 的默认上限为 `2h`（7200 秒），算子、模型参考生成与模型 SDC 对照共用
`stress.benchmarks.npu_burn.timeout`。这是最长作业时限，不保证持续运行 2 小时；
不改变默认 300 秒的内部同步超时，Web/CLI 也不能将本次时限提高到部署上限以上。
同一 daemon 同时只允许一个 active job。

## 8. Web、结果和历史

唯一 Web 地址：

```text
http://<node-address>:19322/
```

同一页面提供 Monitoring、Stress 配置、Run、Cancel、latest、history 和 jobs。
当前没有 Web operator authentication/RBAC，必须通过防火墙、反向代理或可信管理网络
限制 `19322`；SSH 隧道可以使用，但不是功能前提。

结果语义：

- `healthy`：执行成功且必需结果完整；
- `time_limit_reached`：STREAM/HPL/HPCG 达到上限且此前未出错，按可靠性通过展示；
- `unhealthy`：进程错误、结果校验失败或协议错误；
- `unavailable`：镜像、资产、MPI/ABI、CANN、设备或配置预检不满足；
- `cancelled`：用户取消且 workload 进程已清理。

不设置 GFLOPS、带宽或耗时阈值。NPU Burn 必须产生完整 CSV/SDC 结果，NPU 超时
不能当作通过。daemon 保存 latest 和最近 100 次 history，CLI/Web 显示同一状态。

NPU 选择区只回答“本次跑什么”：选择已授权的 logical ID 和已通过当前镜像准入检查的
算子；不可用项目及原因折叠展示，`gpt3`/`llama2` 模型测试不混入算子复选框。准入
不是实机认证，选择区不根据历史报告给算子贴“实机通过”标签。

“已选算子的 tensor 参数（只读）”默认折叠；展开后，每个已选算子还有独立的
折叠详情，展示当前镜像/外部配置目录与芯片代际合并后的有效 shape/维度、dtype、
pattern 和 run_count。这里只预览配置，不修改参数或启动负载；未选算子不在此处
展示。没有显式 shape 的算子不会凭空推算 tensor 尺寸。

历史报告的“执行配置与结果文件”内，同样默认折叠本次算子参数。该快照来自执行端
实际交给 Engine 的 TestCase 配置，按 Job ID 绑定，并随 latest/history 保存；更换
当前配置、选择或删除长篇控制台输出都不会改变它。run_count 是加载的配置值，
不表示已经完成的迭代次数，尤其不能用它宣称取消/失败的作业通过。旧镜像/旧报告
没有有效快照时明确提示，不用当前配置或启动前预检值代替实际参数。
此展示需要更新后的 Control/Web 和 NPU workload，不覆盖正在运行的容器或配置。

模型 SDC 区域根据当前管理员授权显示 GPT3/Llama2；它们不会因“属于模型”而被
固定列为不可用算子。选好设备、模型、检测/生成参考动作及参考 ID 后，点击
“预检本次选择（不训练）”。页面只列当前预检未通过的项目，例如参考未准备、
参考与当前参数/软件栈不匹配、依赖未就绪或基础显存检查失败；通过项不重复列出。
参考未准备不代表模型不支持，也不会自动生成参考。

此入口不创建 Job、不训练、不写参考/历史报告；已有作业运行时不会探测 NPU。
更换模型、设备、动作、参考 ID 或当前配置后需重新预检，结果超过一分钟也会失效；旧请求的返回不会覆盖
新选择。连接失败或旧服务没有预检接口时显示“未完成”，不会当作模型不支持。
“预检通过”只表示本次准入检查通过，基础显存筛查也不保证训练峰值；实际结果仍
以作业报告为准，启动前 Controller 会再次检查。此功能需要更新后的 Control/Web，
细分参考失败原因需要更新后的 NPU workload；不要求更改已有模型配置或参考数据。

报告回答“实际跑了什么、结果如何”：主卡显示 CSV 核验后的设备、算子、用例通过数、
错误数及作业耗时。用例数是 CSV 结果记录数，不是算子数、设备数或迭代次数。
“分设备、分算子结果”按本轮 CSV 汇总；请求/固定选择与实际结果不一致时，在主卡
突出提示。部署默认值、请求/固定参数、配置哈希及结果文件路径放在执行详情中。

新 workload plugin 在既有报告输出中保存 `CATMONITOR_NPU_BURN_CASE_GROUPS` 分组
元数据，不改变执行请求或成功判据。旧报告缺少此数据时仍可查看原有总数，但不
推算分组；使用新版 Control 和 NPU workload 后的新作业才会有这份明细。
Controller 将校验过的分组和 CSV 路径保存为 `npu_csv`，历史归档删除大段输出时
仍保留这些明细。
累计用例时间是 CSV 各行时间合计（设备可能并行执行）；作业耗时按作业起止时间
计算，包含启动、执行和收尾，不包括提交前的准入检查，两者不可混用。

## 9. 从 Stress V1 迁移

当前开发候选新增 CLI `--npu-devices` / `--npu-cases` 及 Web 按次设备/算子选择。
需要新 Control 和 NPU workload，不是已有 pre-release 镜像的接口。完整源码与 A3
配置合并、A2 独立默认规模、构建、命令和逐算子待验清单见
[NPU 算子指南](../../docker/NPU_OPERATOR_GUIDE.md)。未验证的算子不标记硬件 PASS。

```text
OLD_MONITORING_YAML_COMPATIBLE=true
OLD_STRESS_YAML_COMPATIBLE=false
```

只做监控的旧配置和旧 `docker run` 方式继续可用。Web 仍接受原有 `-config`，但该
参数在 V2 中只是 deprecated no-op；Web 从 snapshot 读取监控数据，并仅在 daemon
control socket 可用时启用 Stress API。未启用 Stress 时不需要 Docker socket、
workload 容器或 `/run/catmonitor/control.sock`。

启用 Stress 的旧 YAML 不兼容，升级时必须重新运行 generator。不要复制旧配置中的
脚本路径、CPU Runner socket、固定 NPU 容器或独立 Stress Web 字段。Web 只保留
`19322`，不再部署第二个 Stress Web listener。

迁移后按顺序验证：

```text
stress doctor → STREAM → HPCG → HPL → NPU Burn（如启用）
```

旧报告可以归档，但不能继续作为 V2 active job 状态写入。

## 10. 停止与故障排查

使用节点指南中的相同 Compose 文件和 profile，将 `up -d` 换为：

- `stop`：停止并保留容器；
- `start`：恢复；
- `down`：删除容器和网络；
- 不要使用 `down -v`，除非确认可以删除 snapshot 和 Stress history。

| 现象 | 检查 |
|---|---|
| Web snapshot 未就绪 | daemon `19320/-/ready`、daemon 日志和 snapshot volume |
| Stress 显示未配置 | generated override 是否加入 Compose、`stress doctor` |
| CPU benchmark unavailable | CPU workload 状态、MPI ABI、benchmark 资产和资源规模 |
| NPU benchmark unavailable | runtime preflight 中的驱动、CANN/torch_npu、设备数量和 PCI topology |
| `aclInit 507899` / device invalid | 使用 generator 生成的 A2 override；确认只有 NPU workload 为 privileged |
| Web 无法 Run/Cancel | `--enable-web`、daemon 状态、`19322` 网络策略 |
| Cancel 后仍有进程 | workload 容器进程；应按缺陷处理，不能忽略 |
| Registry 不可达 | 用 `docker save/load` 转移同一标签和 Image ID 的镜像 |

设计与测试人员可继续参考 [STRESS_DESIGN.md](STRESS_DESIGN.md) 和
[STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md)。
