# CATMonitor 统一可靠性压测平台规格（STRESS_SPEC）

## 1. 适用范围

本规格定义 Stress Architecture V2 的可验证产品契约。关键词“必须”“不得”是
发布门禁；“可以”表示兼容实现选择。

正式 workload：

| Plugin | Runtime | 平台 |
|---|---|---|
| `stream` | CPU workload container | Linux |
| `hpl` | CPU workload container + MPI/OpenBLAS | Linux |
| `hpcg` | CPU workload container + MPI | Linux |
| `npu_burn` | Ascend workload container | Linux/Ascend |

GPU Stress 不在本版本范围；GPU 节点只能启用 CPU Stress。

## 2. 系统不变量

1. `catmonitor daemon` 必须是唯一 active-job owner。
2. CLI 和 Web 不得在本进程创建可执行 workload 的 Manager。
3. CPU/NPU 必须使用独立 workload image；Control image 不得包含其重依赖。
4. CPU/NPU 必须使用同一 workload entrypoint 和 result envelope。
5. Web/DFeE 不得访问 Docker socket。
6. 唯一 Web listener `:19322` 必须同时注册查询、Run 与 Cancel；不得创建第二个 Stress Web listener/container。
7. API 不得接受 shell、command、path 或 environment。
8. 每个 workload container 同时最多运行一个任务；Controller 全局同时最多一个。
9. timeout/cancel 必须终止完整进程树。
10. 旧 A2-r2 evidence 只能作为回归基线。

## 3. 配置规格

### 3.1 YAML

```yaml
stress:
  enabled: false
  web_enabled: false
  control_socket: /run/catmonitor/control.sock
  report_path: /var/lib/catmonitor/stress/stress-latest.json
  default_benchmarks: [stream]
  executor:
    type: docker_exec
    docker_binary: /usr/bin/docker
    docker_socket: /var/run/docker.sock
  benchmarks:
    stream:
      enabled: false
      plugin: stream
      container: catmonitor-stress-cpu
      timeout: 1m
    hpl:
      enabled: false
      plugin: hpl
      container: catmonitor-stress-cpu
      timeout: 2h
    hpcg:
      enabled: false
      plugin: hpcg
      container: catmonitor-stress-cpu
      timeout: 3m
    npu_burn:
      enabled: false
      plugin: npu_burn
      container: catmonitor-stress-npu
      timeout: 30m
```

### 3.2 校验

- `executor.type` 本版本必须是 `docker_exec`；
- `container` 必须匹配 `[A-Za-z0-9][A-Za-z0-9_.-]*`；
- `plugin` 必须与支持列表匹配，不得是路径；
- timeout 必须为正；单次请求只能缩短配置上限；
- `default_benchmarks` 必须启用且去重；
- `control_socket`、`report_path`、`docker_binary`、`docker_socket` 必须是绝对路径；
- `user` 只能是固定数字 UID 或 UID:GID；Web 请求不得覆盖 container/plugin/user/path。

## 4. Executor 规格

Go 接口必须支持 `Describe`、`Run`、`Cancel`、`Status`，并允许测试注入 fake
executor。Manager 不得通过 benchmark 名选择 transport。

`DockerExecExecutor` 只能构造以下固定形式：

```text
docker --host unix://<configured-socket> exec <configured-container>
  /usr/local/bin/catmonitor-stress-exec <operation> ...
```

允许增加 `-i` 传 JSON stdin；不得增加来自用户的 command segment。

Transport 必须：

- 限制 stdout/stderr 和 JSON response 大小；
- 区分容器不存在、未运行、协议错误和 workload 失败；
- 在 run context 取消时主动调用 shim `cancel`；
- `cancel` 完成后等待 run 返回或在有界 grace period 后失败；
- 不自动创建、删除、重启 workload container。

## 5. workload protocol

### 5.1 通用要求

入口固定为 `/usr/local/bin/catmonitor-stress-exec`，协议版本为 1。NPU workload
必须经 `/usr/local/bin/catmonitor-npu-burn` 包装入口 source CANN 环境；不得假设
`docker exec` 会继承容器 PID 1 启动后追加的环境变量。
Job ID 必须是 1–64 位小写十六进制；benchmark 必须来自容器 allowlist。
JSON decoder 必须拒绝未知字段和多值输入；stdin 上限 64 KiB。

### 5.2 Describe

```text
catmonitor-stress-exec describe --json
catmonitor-stress-exec describe --benchmark stream --json
```

响应必须包含：protocol version、benchmark/capabilities、resources、assets、
MPI、preflight、runtime identity、typed options。失败的 required asset 或 MPI
检查必须使 preflight 为 `fail`。

默认 Describe 的 RC0 表示 JSON 成功返回，不表示 preflight 通过。
自动化/healthcheck 必须使用 `--require-pass` 并检查 JSON；fail 应返回非零。

Describe 必须只读，默认 2 秒；NPU runtime import/topology 可以配置为不超过
30 秒的只读预检。

### 5.3 Run request

```json
{
  "protocol_version": 1,
  "job_id": "0123456789abcdef",
  "benchmark": "hpl",
  "timeout_seconds": 600,
  "options": {}
}
```

当前 `options` 只接受空对象；不开放命令、路径或任意环境变量。
NPU 可另加 `"npu_devices":[0,1]`：Controller 在实时可见范围及管理员
`allowed_npu_devices` 中准入，固定到本 Job，并用于相同 Describe/Run。
省略保留默认，显式空/null、重复、负数、越界、未授权或非 NPU 项目携带设备均拒绝。
allowlist 未声明时仅授权部署默认，不把可见设备自动当授权。
NPU 可另加 `"npu_cases":["matmul","conv2d"]`：仅接受新 workload 的实际 catalog
中通过配置/dtype 准入的显式算子名，不接受 `all`、空/null、重复或 shell 参数。
省略使用部署默认；旧 workload 没有 catalog 时拒绝显式选算子。目录完整不等于各算子
硬件已认证，catalog 单独标记 `hardware_validation=NOT_RUN`。
Docker Executor 只注入已校验的设备、算子/清空 group 和 idle 标志；workload Request
与 env 必须一致。Control 和 workload 均需更新，不能只更新前端。

### 5.4 Run result

Run 正常通信时必须输出且只输出一个 JSON envelope。诊断输出放在 envelope 的
有界 `output` 字段，不得破坏 JSON stdout。

`healthy` 表示命令和结果校验均通过。CPU 的 `time_limit_reached` 可视为健康
通过；NPU Burn 未形成完整 PASS/SDC 结果时不得因外部超时变为健康。

NPU 使用每轮独立输出目录，校验 CSV result、err_count、run_count，且每个选中设备
必须覆盖全部选中算子，额外或缺失组合均失败。通过后 Result 才返回 `npu_devices` /
`npu_cases`；Report/history 的 `npu_selection` 保留部署默认、请求、固定和实际
设备/算子。Cancel 只按 Job ID。配置最大时限、本次有效时限和内部同步超时分离。

### 5.5 State

默认目录：

```text
/var/lib/catmonitor/stress/workload-jobs/<job_id>/
  request.json
  state.json
  result.json
  pgid
  cancel.requested
```

活动所有权通过同一文件系统内的原子创建实现。文件写入必须使用临时文件、
`fsync/close` 和 rename；不得跟随 symlink。终态后必须释放 active slot。

### 5.6 Cancel

`cancel --job-id` 必须：

1. 验证 job ID 和 active owner；
2. 创建取消标记；
3. 向负 PGID 发 TERM；
4. 有界等待；
5. 必要时发 KILL；
6. 确认进程组不存在；
7. 返回 JSON acknowledgement。

不存在或非 active job 必须返回 not-found，不得误杀其他进程。

## 6. Controller 与报告

Controller 必须保留现有 `Status`、`BenchmarkResult`、`Report` 的外部 JSON
语义。报告必须记录：

- job/initiator/timestamps/status；
- 每项 duration/message/values/source；
- describe profile、runtime identity 与配置哈希；
- executor type 和目标容器；
- persistence error（如有）。

latest 是运行态和最近终态的真源；history 最新优先，最多 100 条，默认返回
20 条，并移除历史中的大段 command output。

NPU 报告可附带 `npu_csv` 小型可选元数据：本轮 CSV 路径，以及按 logical ID / 算子
分组的记录数、通过数、失败数、错误数和用例时间合计。Controller 从 plugin 的
CSV 校验汇总中读取并核对总数；history 删除 command output 时仍保留这些明细。
此字段不改变执行、取消或成功判据；旧报告缺少明细时不从总数推算分组。

Controller shutdown 必须取消其 active job并等待终态持久化。

## 7. daemon control API

### 7.1 Socket

- 默认 `/run/catmonitor/control.sock`；
- mode 不得宽于 `0660`；
- daemon 退出时删除自身 socket；
- 监听失败且 Stress enabled 时 daemon 启动必须失败；
- Stress disabled 时 API 仍可提供只读配置/诊断，但提交必须返回 disabled。

### 7.2 HTTP

| Method | Path | 说明 |
|---|---|---|
| GET | `/stress/config` | 配置、能力、profile/preflight |
| GET | `/stress/latest` | 最近报告 |
| GET | `/stress/history?limit=N` | 有界历史 |
| GET | `/stress/jobs/{id}` | 作业状态 |
| POST | `/stress/jobs` | 提交 typed request |
| POST | `/stress/jobs/{id}/cancel` | 取消 active job |

POST 必须要求 `application/json`，请求体上限 64 KiB，拒绝未知字段。Busy 返回
409；disabled/invalid 返回 400 或 403；不存在返回 404。

## 8. CLI

正式命令：

```text
catmonitor stress [run] [--bench ...] [-c config] [-o json|table]
catmonitor stress doctor [-c config] [-o json|table]
catmonitor stress status [--job ID] [-c config] [-o json|table]
catmonitor stress cancel --job ID [-c config]
catmonitor stress run --bench npu_burn --npu-devices 0,1 --npu-cases matmul,conv2d [--timeout 300s]
```

CLI 只从配置读取 control socket 并作为 client；不得读取 Docker socket、启动
Manager 或直接执行 benchmark。Run 默认等待终态，进程被中断时可以请求取消，
不得仅退出并留下 workload。

## 9. Web

Web binary 参数至少包含：

```text
-addr=:19322
-snapshot-dir=/var/lib/catmonitor/snapshot
-control-socket=/run/catmonitor/control.sock
```

- 唯一 listener 注册监控页面、snapshot、Stress GET、Run 与 Cancel；
- mutating 请求必须满足 JSON、action header、same-origin 和请求体上限；
- Web 只能通过 control socket 访问 Controller，不得挂 Docker Socket；
- `control.sock` 只承担 frontend 到 Controller 的控制平面；
- Web 关闭时不得取消 daemon 拥有的 job；
- 本版本记录 `SECURITY_DEBT_WEB_OPERATOR_AUTH=true`，后续以认证/RBAC 收敛。

## 10. Compose 与生成器

### 10.1 Canonical 文件

Compose 入口只保留：base、config、GPU、NPU、统一 Stress 和生成的 Stress
hardware override。`catmonitor-install`、独立 Stress Web overlay 与 NPU Burn
兼容 overlay 必须移除。

### 10.2 Profiles

```text
stress-cpu -> catmonitor-stress-cpu
stress-npu -> catmonitor-stress-npu
```

Monitoring-only 不得创建 workload container。启用 Stress 时 daemon 可挂 Docker
socket；Web/DFeE 不得挂。

### 10.3 生成器

`generate_stress_deployment.sh` 必须是纯生成器：

- 不调用 `docker run/create/start/exec`；
- 不启动 workload；
- 枚举真实 `/dev/davinciN`；
- 输出 `catmonitor-stress.yaml`、`stress-profile.json`、
  `docker-compose.stress.generated.yml` 和 deployment manifest；
- 输出 `CATMONITOR_NPU_DEVICE_COUNT=<映射数量>`；
- 输出与映射数量一致的连续 CANN runtime visible IDs；
- NPU describe/doctor 必须在 workload 容器内验证 CANN、HAL、torch_npu、custom
  ops，并要求 `torch_npu.npu.device_count()` 等于映射数量；
- A2/CANN 8.3/runc profile 的 NPU workload 容器必须启用 `privileged` 且保持
  `network_mode: none`，该权限不得授予 Web、DFeE 或 CPU workload；
- 支持 `--disable-npu` 的 CPU-only 生成；
- 不生成 forwarding/docker-exec adapter shell。

## 11. 镜像规格

Control 镜像必须包含 daemon、Web、DFeE 和 DockerExecExecutor 所需的受支持
Docker CLI/runtime client；不得包含 CPU client。CPU/NPU 镜像必须包含统一 shim。

CPU 镜像保持 MPI/OpenBLAS/HPL/HPCG ABI 自洽；NPU 镜像保持 CANN、torch_npu、
custom ops、pciutils 和 driver mount ABI 预检。不得修改 bundled 第三方源来实现
transport。

## 12. 安全声明

V2 发布材料必须显式包含：

```text
SECURITY_DEBT_DOCKER_SOCKET=true
```

不得把该设计描述为最小权限。Docker socket 只给 daemon，节点管理员必须理解
其 root-equivalent 权限。未来替换 Executor 不得改变上层 API 和 Plugin 协议。

## 13. 自动化门禁

必须通过：

- Go unit/race tests；
- workload shim 生命周期、timeout、cancel、残留进程测试；
- Docker executor fake-CLI/contract tests；
- daemon control API 与 CLI/Web 跨入口测试；
- shell syntax/DFX/provenance audit；
- Compose config 与八种部署矩阵 fixture；
- Generic/GPU 不意外出现 NPU；
- monitoring-only 不出现 workload container；
- Control/Web/DFeE mount 权限检查。

## 14. 实机门禁

A2 Full 与 CPU-only 必须按 STRESS_TEST_GUIDE 重跑。A3 16-device 实机未完成前：

```text
A3_16_DEVICE_VALIDATED=false
```

所有自动和 A2 实机门禁通过前：

```text
READY_FOR_V036_RC_IMAGE_BUILD=false
```

## 15. Monitoring 向后兼容契约

Stress V2 是显式可选能力，不得改变既有 Monitoring 部署的启动前提：

- 旧版 daemon `docker run` 方式继续有效；
- Web 继续接受 `-addr`、`-snapshot-dir` 和旧 `-config` 参数；
- `-config` 仅作为 deprecated no-op 兼容参数，Web 的数据源仍是 snapshot，
  Stress 控制源仍是可选的 daemon control socket；
- DFeE 的既有 flags 和只读 snapshot 消费方式保持不变；
- `stress.enabled=false` 时 daemon 不创建 Manager、Executor 或 control socket，
  不要求 Docker CLI、Docker socket、workload 容器和 Stress state；
- control socket 缺失时 Web 的 Monitoring 页面与 API 必须正常工作；
  `GET /api/stress/config` 返回 HTTP 200 的禁用能力视图
  (`enabled=false`, `available=false`)，Run/Cancel 返回 `503 Service Unavailable`；
- base `docker-compose.yml` 只定义 daemon、Web、DFeE 三个容器，不挂 Docker
  socket 或 Stress state；执行面只由 `docker-compose.stress.yml` 显式加入。

配置兼容边界固定为：

```text
OLD_MONITORING_YAML_COMPATIBLE=true
OLD_STRESS_YAML_COMPATIBLE=false
```

包含 disabled 旧 Stress 字段的 Monitoring YAML 可以继续用于监控；一旦旧
`script_path`、`health.stress` 等 V1 执行配置被启用，加载器必须明确要求迁移，
不得静默解释成 V2 Docker workload 配置。

RC 镜像构建前必须通过 `make test-monitoring-compat`。Fresh Image Acceptance 还
必须用新镜像执行旧 README 中 daemon、Web、DFeE 的手工 `docker run` 命令；
不得以源码测试替代镜像级兼容验收。

## 模型 SDC 开发候选

`npu_model={name,action,reference_id}` 与算子选择互斥，沿用统一 Controller 和
Job-ID Cancel。管理员通过 `allowed_npu_models` 和
`allow_npu_reference_generation` 授权；参考生成只允许一个明确 logical ID。
模型请求不接受用户路径、训练脚本或任意参数。

`CATMONITOR_MODEL_MEMORY_MODE` 为部署策略：standard（缺省）或 checkpointed。
后者必须应用显式 model-memory-v1 补丁并通过真实 API 能力检查，不缩减形状、
batch、精度或步骤，不自动回退。参考身份包含此策略，不允许跨策略/软件栈/
代际复用。多设备仍逐卡独立执行；没有 DDP/FSDP/联合显存接口。

完整参考发布及本轮 CSV/设备/全部步骤校验成功后，才返回 reference_generated
或 sdc_passed；两者不能互换。模型同步沿用上游直接同步，整体受 Controller
时限与进程组清理控制，不冒用算子 worker 的 300 秒软件同步计时器。

明确的 NPU allocator OOM 在 terminal result/history 保存
`failure={code:npu_hbm_oom,resource:npu_hbm}`，状态仍为 unhealthy（任务未完成）。
该分类不是硬件故障诊断，不能改为 healthy；取消、驱动错误和一般失败不能
猜测为 HBM OOM。Web 分别显示资源不足、有效压测通过和参考已生成。

新模型入口与 checkpointed 模式的 A2/A3 全负载验收为 PENDING，源码与合成
回归不替代新镜像实机验收。部署及矩阵见
[A3 迁移指南](../../docker/NPU_A3_MIGRATION_GUIDE.md)。
