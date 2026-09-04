# CATMonitor 可靠性压测

Stress 是 CATMonitor 的显式触发可靠性负载特性，支持：

- STREAM：内存带宽与数据搬运；
- HPL：高密度浮点计算与 MPI；
- HPCG：内存、计算与 MPI 综合负载；
- Ascend NPU Burn：昇腾 NPU 高负载计算与 SDC 检测。

压测不会作为周期健康检查自动执行，也不直接计入健康总分。运行期间会占用
CPU、内存、MPI、NUMA 或 NPU 资源，因此实时健康分可能暂时下降。

## 架构

```text
catmonitor CLI ───────┐
                     │  Unix HTTP/JSON
Web operator ────────┼── /run/catmonitor/control.sock
                     ▼
             daemon Stress Controller
                     │
                     │ fixed Docker exec protocol
          ┌──────────┴──────────┐
          ▼                     ▼
 CPU workload container   NPU workload container
 STREAM / HPL / HPCG      Ascend NPU Burn
          └──── /usr/local/bin/catmonitor-stress-exec
```

关键边界：

- daemon 是唯一作业所有者，负责互斥、超时、取消、报告和历史；
- CLI 与 Web 只是本机控制 API 客户端；
- CPU/NPU 镜像实现同一 workload plugin 协议；
- Web 和 DFeE 不挂 Docker Socket；
- 当前 V2 仅 daemon 挂 Docker Socket。这是明确记录的临时高权限边界；
- 不提供网页脚本编辑、任意命令或任意 benchmark 参数编辑。

当前程序版本由项目 owner 保持为 `0.3.5`。Linux/ARM64 Stress 集成镜像使用
`arm64-v0.3.5-stress`，不得标记为通用 `v0.3.5`/`latest`，也不得复用 a2-r1。
目标发布线为 `v0.3.6`；当前标签只是 pre-release，不代表正式 `v0.3.6` 已发布。

## 配置

Linux 默认读取 `/etc/catmonitor/catmonitor.yaml`。关键配置示例：

```yaml
stress:
  enabled: true
  web_enabled: true
  control_socket: /run/catmonitor/control.sock
  report_path: /var/lib/catmonitor/stress/stress-latest.json
  default_benchmarks: [stream]
  executor:
    type: docker_exec
    docker_binary: /usr/bin/docker
    docker_socket: /var/run/docker.sock
  benchmarks:
    stream:
      enabled: true
      plugin: stream
      container: catmonitor-stress-cpu
      user: "65532:65532"
      timeout: 1m
    hpl:
      enabled: true
      plugin: hpl
      container: catmonitor-stress-cpu
      user: "65532:65532"
      timeout: 2h
    hpcg:
      enabled: true
      plugin: hpcg
      container: catmonitor-stress-cpu
      user: "65532:65532"
      timeout: 3m
    npu_burn:
      enabled: false
      plugin: npu_burn
      container: catmonitor-stress-npu
      timeout: 30m
```

配置只声明固定 plugin/container/timeout 绑定。benchmark 的 MPI、线程、问题规模、
NPU logical ID 等参数属于 workload 容器 profile，由 Compose 环境变量预先定义；
运行请求只能选择项目，并可将本次超时缩短到配置上限以内。

## 部署

先设置当前 Stress pre-release 镜像变量：

```bash
export CATMONITOR_REGISTRY='ghcr.io/spike677'
export CATMONITOR_RELEASE='arm64-v0.3.5-stress'
export CATMONITOR_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-npu:${CATMONITOR_RELEASE}"
export CPU_STRESS_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-stress-cpu:${CATMONITOR_RELEASE}"
export NPU_STRESS_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-stress-npu:${CATMONITOR_RELEASE}"
```

先生成节点部署文件：

```bash
bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --cpu-image "$CPU_STRESS_IMAGE" \
  --enable-web \
  --force
```

Ascend 节点再传 NPU 镜像；host device node 动态发现，NPU Burn 使用经 PCI
topology 校验的 logical devices：

```bash
bash scripts/stress/generate_stress_deployment.sh \
  --output-dir /etc/catmonitor/generated-stress \
  --cpu-image "$CPU_STRESS_IMAGE" \
  --npu-image "$NPU_STRESS_IMAGE" \
  --npu-burn-device all \
  --npu-chip-generation A2 \
  --enable-web \
  --force
```

生成器同时设置 CANN runtime visible IDs；doctor 会在 NPU workload 容器中执行
只读 runtime preflight，验证 CANN、torch_npu、custom ops、PCI topology 与映射
device count。A2/CANN 8.3/runc 的已验证 profile 仅对 NPU workload 容器启用
`privileged: true`，并保持 `network_mode: none`；Web/DFeE 不获得该权限。

然后叠加基础与生成的 Compose：

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu up -d
```

启用 NPU 时：

```bash
docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f /etc/catmonitor/generated-stress/docker-compose.stress.generated.yml \
  --profile stress-cpu --profile stress-npu up -d
```

## 使用

```bash
catmonitor stress --help
catmonitor stress doctor -o table
catmonitor stress run --bench stream -o table
catmonitor stress run --bench hpcg --timeout 120s -o table
catmonitor stress status -o json
catmonitor stress cancel --job JOB_ID
```

Web：

- `:19322`：唯一 Web 入口，提供监控、Stress 配置/报告/history/jobs、Run 与 Cancel；
- Web 通过 `/run/catmonitor/control.sock` 调用 daemon Controller，不挂 Docker Socket；
- 当前 Web operator authentication/RBAC 尚未实现，部署方必须自行控制 `:19322` 的网络访问。

## 状态语义

- `healthy`：进程成功且必需结果已解析；
- `time_limit_reached`：STREAM/HPL/HPCG 达到 CATMonitor 上限且此前未报错，按可靠性通过展示，但性能值可能为空；
- `unhealthy`：命令错误、结果校验失败、NPU Burn 未形成完整结果或协议错误；
- `unavailable`：容器、资产、MPI/ABI 或配置预检不满足；
- `cancelled`：用户取消，workload plugin 已终止完整进程组。

不设置 GFLOPS、带宽或运行时间阈值；数值用于记录与观察，不参与健康总分。

## 文档

- [STRESS_SPEC.md](STRESS_SPEC.md)：功能契约；
- [STRESS_DESIGN.md](STRESS_DESIGN.md)：V2 架构与安全边界；
- [STRESS_USER_GUIDE.md](STRESS_USER_GUIDE.md)：构建、生成、部署和使用；
- [STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md)：自动化测试与 A2/A3 实机门禁；
- [OSS_RELEASE_AUDIT.md](OSS_RELEASE_AUDIT.md)：开源发布审计。
