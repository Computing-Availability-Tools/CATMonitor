# CATMonitor 统一可靠性压测平台设计（STRESS_DESIGN）

## 1. 设计状态

本文描述面向 `v0.3.6` 候选的 Stress Architecture V2。此前 A2-r2 的
CPU Unix Runner、NPU `docker exec` 节点适配器和独立 Stress Web 只作为
回归基线，不再是新架构的正式执行路径。

本次重构不改变以下产品边界：

- 压测必须由用户显式触发，不参与健康总分计算；
- 压测期间的资源占用仍可能暂时降低实时健康分；
- 不设置性能阈值，执行及结果校验成功即通过；
- CPU 和 NPU 运行时继续使用独立镜像，不合入 Control 镜像；
- Linux 是正式执行平台，其他平台只允许构建和显示“不支持”。

已完成的 A2-r2 实机结果是新架构的回归基线。架构变化后必须重新执行
A2 Full 与 CPU-only 验收，旧证据不能直接作为新版本发布证据。

## 2. 核心模型

```text
CLI ─────────────┐
                 │  local Unix HTTP/JSON
Web operator ────┼───────────────┐
                 │               v
Web read-only ───┘       catmonitor daemon
                         Stress Controller
                                │
                         Executor interface
                                │
                        DockerExecExecutor
                                │
              ┌─────────────────┴─────────────────┐
              v                                   v
   catmonitor-stress-cpu              catmonitor-stress-npu
   catmonitor-stress-exec              catmonitor-stress-exec
     STREAM/HPL/HPCG                        NPU Burn
```

三个概念必须分离：

- **Controller**：作业所有权、单作业互斥、报告、历史与取消权限；
- **Executor**：在哪里执行以及如何控制生命周期；
- **Plugin**：执行什么、有哪些选项、如何预检和解释结果。

设备类型不是 transport。不得再用 `cpu_backend=unix`、
`npu_backend=docker_exec` 表示领域模型。

## 3. 组件职责

### 3.1 CATMonitor daemon / Stress Controller

`catmonitor daemon` 是节点上唯一的 Stress Controller，拥有：

- 唯一 active job；
- benchmark 到 workload container/plugin 的绑定；
- Executor registry；
- latest/history 报告生命周期；
- CLI 与 Web 的统一取消权限；
- 超时上限和 typed request 校验。

CLI 和 Web 不再创建自己的 `Manager`。跨进程 `flock` 不再承担作业所有权；
报告仍使用原子写入，历史仍有数量上限。

### 3.2 daemon local control API

默认监听：

```text
/run/catmonitor/control.sock
```

该 Unix Socket 只承载 `CLI/Web -> Controller`，不是 workload backend。
协议采用 Unix HTTP/JSON，至少包含：

```text
GET  /stress/config
GET  /stress/latest
GET  /stress/history
GET  /stress/jobs/{job_id}
POST /stress/jobs
POST /stress/jobs/{job_id}/cancel
```

Socket 由 daemon 创建和删除。启动时不得删除非 socket 文件；退出时只删除
自身创建的 socket。请求体必须限长并拒绝未知字段。

### 3.3 Executor

Go 层提供稳定接口：

```text
Describe(ctx, Binding) -> ExecutionProfile
Run(ctx, Binding, WorkloadRequest) -> WorkloadResult
Cancel(ctx, Binding, jobID)
Status(ctx, Binding, jobID) -> WorkloadStatus
```

V2 首个正式实现为 `DockerExecExecutor`。它只负责：

- 检查目标容器；
- 调用固定入口 `/usr/local/bin/catmonitor-stress-exec`；
- 传输 JSON request/result；
- 在 Controller 取消时调用容器内 `cancel`；
- 对 transport 输出和响应大小设置上限。

Executor 不解析 STREAM、HPL、HPCG 或 NPU Burn 语义。未来可增加
`RunnerRPCExecutor`、`LocalExecutor` 或 `KubernetesExecutor`，而不修改
Controller、CLI、Web 和报告模式。

### 3.4 workload container

CPU 与 NPU 镜像继续独立：

```text
CPU: STREAM, HPL, HPCG, MPI, OpenBLAS, numactl
NPU: CANN, torch, torch_npu, NPU Burn, custom ops, pciutils
```

两个镜像都提供：

```text
/usr/local/bin/catmonitor-stress-exec
```

容器平时只保持 idle，不运行持久 RPC daemon。任务通过 `docker exec` 启动。
NPU plugin 调用 `/usr/local/bin/catmonitor-npu-burn` 环境包装入口；每次 workload
执行都重新 source 镜像内 CANN 环境，再进入上游 `npu-burn`，不得依赖 PID 1
启动时导出的临时环境。

### 3.5 workload shim

统一入口支持：

```text
catmonitor-stress-exec describe [--benchmark NAME] --json
catmonitor-stress-exec run --request -
catmonitor-stress-exec cancel --job-id ID
catmonitor-stress-exec status --job-id ID
```

shim 负责：

- benchmark allowlist；
- typed request 校验；
- 每容器单 active job；
- 独立进程组；
- pid/state/result 原子写入；
- timeout 与 cancel；
- 杀死 shell、MPI 和 benchmark 完整子树；
- 有界捕获输出；
- 统一 result envelope。

Web 请求永远不能传 command、可执行路径、环境变量或任意 shell 参数。
运行路径和资源规模来自管理员生成并挂入容器的只读 profile。NPU workload
容器保持只读根文件系统；NPU runtime HOME 使用限额 tmpfs，供日志、TBE 临时数据
和缓存使用，CSV 输出子目录再叠加持久卷。runtime HOME、日志与输出目录都必须通过
无副作用的 writable-mount 预检。

## 4. Plugin 协议

### 4.1 请求

```json
{
  "protocol_version": 1,
  "job_id": "9d3b2d3a1f6e4a12",
  "benchmark": "stream",
  "timeout_seconds": 60,
  "options": {}
}
```

NPU 请求可使用 describe 声明过的 typed option，例如：

```json
{
  "protocol_version": 1,
  "job_id": "9d3b2d3a1f6e4a12",
  "benchmark": "npu_burn",
  "timeout_seconds": 300,
  "options": {"device": 0, "case": "matmul"}
}
```

第一版可以由部署 profile 固定 NPU 选项，但协议不得退化为任意命令。

### 4.2 describe

`describe` 是无副作用协议，返回：

- 支持的 benchmark；
- typed options 及允许值；
- 设备和逻辑命名空间；
- 资源规模和超时；
- 资产/MPI/ABI 预检；
- runtime identity；
- 配置与脚本哈希。

轮询 describe 不得启动 workload、创建结果文件或改变设备状态。

### 4.3 result envelope

CPU 和 NPU 使用同一 envelope：

```json
{
  "protocol_version": 1,
  "job_id": "9d3b2d3a1f6e4a12",
  "benchmark": "stream",
  "status": "healthy",
  "started_at": "2026-08-26T08:00:00Z",
  "finished_at": "2026-08-26T08:00:01Z",
  "duration_ms": 1000,
  "message": "workload completed and required values parsed",
  "values": {"copy_mb_s": 100000.0},
  "source": "stdout",
  "output": "bounded diagnostic tail"
}
```

允许的终态与 CATMonitor 报告状态一致：`healthy`、
`time_limit_reached`、`unhealthy`、`cancelled`、`unavailable`。

## 5. 执行与取消时序

```text
client POST job
  -> daemon validates config/options/timeout
  -> daemon obtains the only active-job slot
  -> executor describe/preflight
  -> executor docker exec ... run
  -> shim writes state + starts process group
  -> plugin executes workload
  -> shim validates result and emits envelope
  -> daemon writes latest + history
```

取消：

```text
CLI/Web cancel
  -> daemon verifies active job id
  -> active context cancelled
  -> DockerExecExecutor invokes shim cancel
  -> shim kills negative PGID and waits for disappearance
  -> result becomes cancelled
  -> daemon persists terminal report
```

只杀 Docker client 不算取消成功。测试必须证明 workload/MPI 子进程已退出且没有
残留 active lock、pid 文件或运行状态。

## 6. Web

一个 `web` binary/container 只监听：

```text
:19322    monitoring + stress report/history/jobs + Run/Cancel
```

- 单一 mux/server 同时注册监控、Stress GET、Run 和 Cancel；
- 写请求保留 action header、JSON content type、same-origin 和请求体上限校验；
- Web 只连接 daemon control socket；该 socket 是 frontend control plane，不是 workload backend；
- Web 和 DFeE 都不挂 Docker socket 或 workload 私有目录；
- 不再运行 `catmonitor-stress-web` 独立容器；
- operator authentication/RBAC 是明确后续安全债务，不通过第二 listener 隔离。

## 7. 配置模型

共享 YAML 只描述 Controller 和 binding，不保存任意命令：

```yaml
stress:
  enabled: true
  web_enabled: true
  control_socket: /run/catmonitor/control.sock
  report_path: /var/lib/catmonitor/stress/stress-latest.json
  executor:
    type: docker_exec
    docker_binary: /usr/bin/docker
    docker_socket: /var/run/docker.sock
  default_benchmarks: [stream]
  benchmarks:
    stream:
      enabled: true
      container: catmonitor-stress-cpu
      plugin: stream
      timeout: 1m
```

路径、MPI 规模、NPU 设备和 workload 选项由生成的 deployment profile 与
Compose environment/mount 表达。Web 不编辑这些配置。

`render_catmonitor_config.sh` 只负责将唯一 `stress:` 片段原子合入共享配置，
不得演变为第二套 installer。

## 8. Compose 与容器拓扑

Canonical deployment 是 Docker Compose；逐条 `docker run` 仅作为文档化
fallback。目标文件：

```text
docker-compose.yml
docker-compose.config.yml
docker-compose.gpu.yml
docker-compose.npu.yml
docker-compose.stress.yml
docker-compose.stress.generated.yml
```

`docker-compose.stress.yml` 定义 `stress-cpu` 与 `stress-npu` profiles；生成
文件只补充实际 NPU devices、驱动/DCMI mounts、镜像和 typed profile。

目标容器数量：

| 部署 | 数量 |
|---|---:|
| Base monitoring | 3 |
| Generic/GPU + CPU | 4 |
| NPU + CPU | 4 |
| NPU + NPU | 4 |
| NPU Full CPU+NPU | 5 |

## 9. A2/A3 设备语义

生成器枚举真实 `/dev/davinciN`，保持节点 ID 映射，设置
`CATMONITOR_NPU_DEVICE_COUNT` 为映射数量，并为 CANN runtime 输出从 0 开始的
连续 visible IDs。workload describe 还必须执行只读 runtime preflight，验证 CANN
环境、HAL、torch、torch_npu、custom ops 与 `torch_npu.npu.device_count()`。必须区分：

- device node ID；
- NPU Burn PCI logical ID；
- torch_npu device index；
- npu-smi physical ID。

不得假设最大 `/dev/davinciN` ID 等于设备数量，不得写死 0..7、两个 NUMA
组或 A2 的 `/dev/davinci2,/dev/davinci5`。A2 稀疏节点已经是回归门禁；
A3 16-device 仍需单独实机验收。

A2/CANN 8.3/runc 的真实 workload 已证明仅映射 device nodes 不足以完成 CANN
初始化，因此生成的 NPU workload service 使用 `privileged: true`。该权限是当前
A2 执行面契约与安全债务，只作用于无网络的 NPU workload 容器；不得传播给
Web、DFeE 或 CPU workload。A3/A5 是否需要同一权限必须分别实机验收。

## 10. 安全边界与已知债务

本版本允许 daemon 挂 Docker socket：

```text
SECURITY_DEBT_DOCKER_SOCKET=true
SECURITY_DEBT_NPU_WORKLOAD_PRIVILEGED=true
```

该权限等价于节点 root，仅能部署在管理员控制的节点。减缓措施：

- Web/DFeE 不挂 socket；
- NPU privileged 仅限 `network_mode: none` 的 workload 容器；
- 请求只包含 benchmark 和 schema 声明的 typed option；
- container 名来自只读配置；
- executor 只调用固定 shim 路径；
- 禁止 shell、command、path、environment 从 API 进入；
- `SECURITY_DEBT_WEB_OPERATOR_AUTH=true`：当前 `:19322` 写接口尚无认证/RBAC。

未来用 `RunnerRPCExecutor` 替换 Docker transport 时，Controller、CLI、Web、
Plugin 和报告 schema 不应变化。

## 11. 迁移与删除

V2 完成后删除正式路径中的：

- `catmonitor-stress-cpu-client`；
- CPU Runner Unix socket server、protocol、volume 和 healthcheck；
- `catmonitor-stress-web`；
- `docker-compose.stress-web.yml`；
- `docker-compose.stress-npuburn.yml`；
- `scripts/catmonitor-install` 及其专属测试/文档；
- NPU 固定容器创建脚本（在动态 Compose 完整覆盖后）；
- generator 生成的 CPU forwarding/NPU docker-exec shell。

CPU/NPU workload image、benchmark build、NPU image build、许可证和 provenance
检查继续保留。

## 12. 验收

自动化矩阵必须覆盖：Generic、GPU、NPU 的 monitoring-only 与可支持的 Stress
组合，验证容器、镜像、mount、profile 和硬件 overlay 不串用。

A2 实机必须重新完成：

- Full doctor 4/4 + STREAM/HPCG/HPL/NPU Burn；
- CPU-only doctor 3/3 + STREAM/HPCG/HPL；
- Web Run/Cancel；
- CLI 启动作业在 Web 可见并可跨入口取消；
- Web 启动作业在 CLI 可见；
- executor/shim 无残留进程和状态。

所有门禁通过前不得发布 GHCR 镜像或创建 `v0.3.6` tag。
