# 受限环境：输入准备、构建与部署

补充 [NPU 用户指南](README-npu.md) 和 [开发者指南](DEVELOPER_GUIDE.md)，不提供第二套安装器。
只使用公开源码、获准依赖和授权设备。[兼容审计](NPU_COMPAT_REVIEW.md) 标记修改点。
新镜像构建、离线闭包与硬件验收当前均为 **NOT_RUN**；合成回归不是厂商软件栈认证。

## 1. 固定输入，先只读检查

在 Linux 主机把 [site.env.example](examples/site.env.example) 复制到仓库外填写，不放凭据。
记录源码 commit/校验清单、Docker endpoint/data-root、镜像引用/Image ID/平台、独立
config/state/output/evidence 目录、授权设备和线程预算。版本号不能证明源码相同，tag 不能代替 Image ID。

源码包允许没有 Git。可信的源码准备环节应交付 `source-identity.json`，格式例如：

```json
{"commit":"<public-commit>","files":{"go.mod":"<sha256>","scripts/stress/generate_stress_deployment.sh":"<sha256>","docker/README-npu.md":"<sha256>"}}
```

实际清单应覆盖全部交付文件（含 Dockerfile、patch、脚本、测试），不只上例三项。
不要从待验目录临时生成自证清单。

```bash
# Linux / Python >=3.9；只读，不 pull/build/run，不改 daemon。
set -a
. /absolute/path/to/site.env
set +a
export DOCKER_HOST="$DOCKER_ENDPOINT"
cd "$SOURCE_DIR"
python3 scripts/stress/check_site_inputs.py
rc=$?
printf 'INPUT_CHECK_EXIT=%s\n' "$rc"
```

RC=0 才继续。检查本地镜像身份、独立空目录、标准容器名及 19320/19322/19323/9333 端口。
必须在目标 host-network daemon 所在网络命名空间检查。旧服务占用时停止，不能停止业务或
仅加 `-p` 就声称隔离了 host networking；另选节点或经批准的独立网络命名空间。

## 2. 三条输入路径

### A. 已有本地镜像

使用 `docker image inspect "$CONTROL_IMAGE"` 和对应 workload image，不重复 pull。
管理员可先对获准离线包 `docker load`，再核对 Image ID/`linux/arm64`；不得导出实验机资产。
检查 data-root 空间；把 build/output 放另一块盘不会迁移 Docker layers。

Monitoring：一张 Control、三个容器。CPU：Control + CPU workload、四个容器。
NPU：Control + NPU workload、四个容器。CPU-only 不需要 NPU 镜像。

### B. 公开源码 + 本地基础镜像

Control/CPU 参数见开发者指南。下面是 **A2** NPU 候选，必须显式选择有序 patch/profile：

```bash
bash scripts/stress/build_npu_burn_image.sh \
  --base-image "$NPU_BASE_IMAGE" \
  --go-image "$GO_BUILDER_IMAGE" \
  --image "$NPU_WORKLOAD_IMAGE" \
  --ascend-env-script "$CANN_ENV_SCRIPT" \
  --compat-profile a2-portable-runtime-v1 \
  --patch "$SOURCE_DIR/scripts/stress/patches/ascend_npu_burn/portable-runtime-v1.patch" \
  --patch "$SOURCE_DIR/scripts/stress/patches/ascend_npu_burn/a2-portable-sizing.patch" \
  --docker-bin "$DOCKER_BIN" \
  --build-root "$BUILD_ROOT" \
  --build-network "$BUILD_NETWORK"
```

预期 RC=0、镜像及 manifest 生成；不运行 workload。需要构建 HAL 时附加已有参数
`--build-driver-lib-dir "$HOST_DRIVER_LIB_DIR"`，来源必须获准。CANN 脚本是镜像内实际路径。
记录补丁 SHA、source tree、基础/最终 Image ID 与日志。第二个补丁仅添加 A2 代际、
1 GiB 数据池及 FP16 4096×4096/20 次 matmul，保留 portable 拓扑/进程逻辑。
A3/A5 代码候选使用 `portable-runtime-v1` 且只加第一个补丁；硬件适配仍待执行，不能
因为枚举值存在而声称已支持所有 CANN/torch 组合。

`--base-image` 共用 builder/runtime，**不会自动瘦身**。有合法精简镜像时可改用
`--builder-base-image` / `--runtime-base-image`，分别指定
`--builder-ascend-env-script` / `--runtime-ascend-env-script`。ABI 和真实 Custom Ops 导入
门禁仍保留，但不等于官方配套认证。以上两种新 profile 都不与旧 `a2-cann83` 补丁叠加；
已修改上游的产物不能标记 `none`。

### C. 完全离线

准备全部 FROM（包括 Go builder）、模块、源码/patch、wheel/系统包及校验清单。
NPU base 须已含 Python/torch/torch_npu/CANN 依赖；缺少 lspci 时通过重复的
`--pciutils-package /absolute/package.rpm` 或 `.deb` 注入匹配系统的依赖闭包，不挂宿主 lspci。
可用 `check_site_inputs.py --offline-inputs /path/inputs.json` 核对声明输入：

```json
{"root":"/approved/inputs","files":{"dependency.whl":"<sha256>"},"images":{"local/go:approved":"sha256:<id>"}}
```

仅检查声明输入，不证明闭包完整。NPU 构建用 `--build-network none`，提前确认 Go base 在本地；
真正禁网成功后才能报告 OFFLINE_BUILD=PASS。**CPU 完整离线构建目前未实现/验收**：
CPU Dockerfile 仍通过 APT 安装工具链，离线用户应使用获准的预构建 CPU 镜像。

## 3. 代理与 CA 分层

- Git/curl 使用宿主代理和 CA；Docker pull 由 daemon 发起，宿主 export 不代表 daemon 能联网。
  daemon 设置只能由管理员按站点策略修改。
- NPU builder 临时转发 HTTP(S)/NO_PROXY build args，不持久化到 repo/manifest/runtime ENV。
  不开 `set -x`，不打印带凭据的代理 URL。
- Go 使用 GOPROXY/GOSUMDB；离线需模块缓存，不能依赖自动下载工具链。
- Alpine/Debian mirror 使用开发者指南已有参数，不增加私有默认源。
- 企业 CA 分别涉及宿主、daemon、下载依赖的 build stage 及需要联网的 runtime。选用已装
  获准 CA 的基础镜像或正式 CA 安装层，不关闭 TLS，也不编造未实现的 `--ca` 参数。

机制参考：[Docker proxy args](https://docs.docker.com/build/building/variables/#proxy-arguments)、
[Docker CA](https://docs.docker.com/engine/network/ca-certs/)、
[Go module proxy](https://go.dev/ref/mod#module-proxy)。

## 4. 同一套正式部署

```bash
export CATMONITOR_IMAGE="$CONTROL_IMAGE"
export CATMONITOR_CPU_STRESS_IMAGE="$CPU_WORKLOAD_IMAGE"
export CATMONITOR_NPU_STRESS_IMAGE="$NPU_WORKLOAD_IMAGE"
export CATMONITOR_GENERATED_DIR="$CONFIG_DIR"
export CATMONITOR_STRESS_STATE_DIR="$STATE_DIR"
export CATMONITOR_NPU_OUTPUT_DIR="$OUTPUT_DIR"
```

按 NPU README 对应场景执行 generator、Compose 或 `docker run`，不用内部安装器。
单设备使用新增第 6.6 节。CANN 脚本、线程与模式写入生成配置/`npu-workload.env`，
不能仅靠手工 `docker exec -e`。新 workload/Control 均从修复源码构建；旧镜像没有新
`--require-pass` 健康检查。失败应定位日志，不自动扩大设备/privileged/PID 限制。

### 4.1 A2 完整映射与本次逻辑设备选择

先由管理员核实本节点的**全部可见计算设备**均获准用于验收，无其他 NPU 业务进程。
以下 full 示例动态发现节点，默认选择 logical 0；用 site.env 中实际镜像和 CANN 脚本：

```bash
bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$CONFIG_DIR" \
  --control-image "$CONTROL_IMAGE" \
  --npu-image "$NPU_WORKLOAD_IMAGE" \
  --npu-container catmonitor-stress-npu \
  --npu-chip-generation A2 --npu-runtime runc \
  --npu-device-mode full --npu-security-profile restricted \
  --npu-burn-device 0 --npu-allowed-devices 0 \
  --npu-threads "$NPU_THREADS" \
  --npu-ascend-env-script "$CANN_ENV_SCRIPT" \
  --npu-output-dir "$OUTPUT_DIR" --enable-web
```

只有获准且确认运行时确有 logical 0、1 时，才将 allowlist 改为 `0,1`；不要直接照抄
宿主 `/dev/davinciN` 编号。`--npu-device-nodes` 是映射授权，`--npu-allowed-devices`
是 Controller 作业授权，两者不是同一命名空间。

Compose 最后叠加生成的 override。手工部署用 README 的四容器路径，但 workload 必须：

- 使用 `--env-file "$CONFIG_DIR/npu-workload.env"` 替换旧示例中逐个 `-e NPU_*`、
  `-e CATMONITOR_NPU_DEVICE_COUNT`、`-e ASCEND_RT_VISIBLE_DEVICES`，不要再用旧 `-e` 覆盖生成值；
- restricted 模式去掉旧示例的 privileged 标志，加入 `--init --cap-drop ALL --security-opt no-new-privileges:true`；
- 设备参数只来自本次 profile；保留只读驱动/PCI/sysfs 和 npu-smi 挂载、network none、独立 state/output；
- 健康检查使用 `describe --benchmark npu_burn --json --require-pass`，timeout 至少 35 秒。

full 默认仍是 `legacy-privileged`，以保留旧部署行为；本候选明确选 restricted，驱动
拒绝时记录失败，不自动提升权限。single 必须 restricted，仅映射一个授权计算节点，
且 runtime logical 0 的 PCI 身份须与 kernel device ancestry 一致。某些 SoC 的字符节点
只有虚拟 devdrv 路径、没有 PCI ancestry：当前 **UNVERIFIED/拒绝**，不能拿全机首 PCI 冒充。
多节点子集尚未实现；full 内按 Job 选择多个逻辑设备不是多节点子集隔离。

Docker 必须支持并安装 `--init` 使用的 init binary；缺失是部署前置条件，不取消此门禁。
本轮不提供后台连续压测服务；作业 timeout 是上限，不是持续运行时长。

```bash
docker exec catmonitor catmonitor stress doctor -o table
# 仅在管理员 allowlist 和实时预检允许 0、1 时：
docker exec catmonitor catmonitor stress run --bench npu_burn --npu-devices 0,1 --timeout 300s -o table
docker exec catmonitor catmonitor stress status -o json
# 从 status 取得本轮 Job ID，再取消；不根据当前页面选卡取消
docker exec catmonitor catmonitor stress cancel --job '<job-id>'
```

Web 同一 `/stress/` 页面提供设备复选框及明确全选操作。API `npu_devices:[0,1]` 是
整数数组；省略保留默认，`[]`/`null`/重复/负数/未授权值拒绝。Report/history 保存
`deployment_default`、`requested`、`selected`、`actual`，并计算本次 profile hash。
仅完整 CSV 经校验后填充 actual；缺任何选定设备不能判为 healthy。

当前空闲策略**整节点保守拒绝**：npu-smi 有任意业务进程、格式未知或表不完整都拒绝。
card 数不等于 die 数；不会据此伪造某个 logical ID 的 busy 映射。预检不是外部调度器
的资源锁，仍须维护窗口；即使其它卡已授权，也不会杀业务来使预检通过。
最大作业时限、本次有效时限、stream 同步超时和实际耗时分开记录/展示。

### 4.2 CPU 小规模与自定义容器绑定

```bash
bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$CONFIG_DIR" --control-image "$CONTROL_IMAGE" \
  --cpu-image "$CPU_WORKLOAD_IMAGE" --cpu-container catmonitor-stress-cpu \
  --hpl-profile small --hpl-processes 1 --hpl-threads 1 --enable-web
```

small=4096/256/1×1，仅是初次功能用例，不是性能建议；完整自定义需一起提供
`--hpl-n/--hpl-nb/--hpl-p/--hpl-q`，且 P×Q=ranks。`image` 默认不改镜像原始规模。
手工 workload 加 `--env-file "$CONFIG_DIR/cpu-workload.env"`，替换旧的逐项资源 `-e`；
Compose 自动包含同样值。HPL 启动前重新核对 online CPU ∩ affinity、cgroup CPU quota、
MemAvailable/cgroup 剩余内存及估算开销；Describe 与 Run 均校验，运行输入在独立目录中。
这不是资源预约或绝对峰值保证；STREAM/HPCG 未新增同等级容量检查，不泛称全 CPU hotplug 支持。

`--cpu-container` / `--npu-container` 同时写入 YAML 绑定和 Compose container_name，
手工 `--name` 必须一致。它们不隔离 host-network 端口或 Control 容器名；与正式服务
并存仍需独立目录/volume/端口方案，不得覆盖现有部署。Full 时在一次 generator 调用中
合并上述 CPU/NPU 参数；不要让第二次生成覆盖第一种 workload 配置。

## 5. 修复后镜像硬件验收（全部 NOT_RUN/PENDING）

1. 记录源码、patch、base/final Image ID、manifest、平台和 CANN 脚本/版本来源。
2. 授权单设备 → 容器节点 → 完整 PCI → runtime count=1/logical 0 → runtime PCI 与 kernel PCI/NUMA 一致；
   无法证明就停，不添加业务设备或 privileged。
3. `describe --benchmark npu_burn --json --require-pass` RC=0 且 preflight=pass，再通过 Controller doctor。
4. 空闲窗口由 Controller 发起 Burn，Web `/stress/` 显示同一个 job；分别验证省略设备、
   显式单卡和授权多卡，不支持/忙碌/未授权设备拒绝，YAML 与下一次默认不被改写。
5. 本轮 `run-*` 下 CSV 覆盖所有所选 logical ID，result=PASS、err_count=0、run_count>0；
   记录路径，不能仅检查文件存在/mtime。
6. 观察真实 Worker 后 Cancel；主进程及子进程退出。分别测试内部超时、Controller 超时、
   Worker 启动失败，保存子进程线程环境及 CPU affinity，不增大 PID 限制掩盖问题。
7. 断线重连通过 status/history 观察，断线不等于 Cancel；重启另需授权。只清理本轮资源，
   保留证据，不做全局 prune。本指南不新增 JSONL retention。

每步记录命令、RC、预期/实际和资源变化。使用 tee 时立即保存被测命令的退出码并据此
判定/返回，不能让末尾 echo 掩盖失败。旧实验不能替代以上新镜像验收。
