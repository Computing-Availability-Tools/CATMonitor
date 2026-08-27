# CATMonitor Stress V2 测试指南

本指南把测试分为：静态/单元、协议 E2E、镜像构建 fixture、Compose/deployment、
容器集成和实机 workload。自动化通过不等于 A2/A3 硬件验收通过。

## 1. 测试边界

必须覆盖：

- daemon 是唯一 Manager；
- CLI/Web 通过 Unix control API；
- `DockerExecExecutor` 只调用固定 workload plugin；
- workload plugin 的 describe/run/status/cancel 协议；
- STREAM/HPL/HPCG/NPU 结果归一化；
- HPCG 历史结果拒绝；
- timeout/cancel/进程组清理；
- `:19322` 统一监控/Stress GET/Run/Cancel 路由与同源/action-header 校验；
- CPU-only 与 NPU sparse-device generator；
- Web/DFeE 不挂 Docker socket；
- build 不运行真实 NPU workload。

## 2. 本地快速测试

使用满足 `go.mod` 的 Go toolchain：

```bash
go version
GOTOOLCHAIN=local go test \
  ./features/stress/... \
  ./features/web \
  ./cmd/catmonitor \
  ./internal/config
```

Shell 语法：

```bash
bash -n docker/stress/cpu/entrypoint.sh
bash -n docker/stress/npu/entrypoint.sh
bash -n scripts/stress/generate_stress_deployment.sh
bash -n scripts/stress/build_cpu_runner_image.sh
bash -n scripts/stress/build_npu_burn_image.sh
```

统一入口：

```bash
make test-stress
```

## 3. Go 单元与组件测试

重点包：

| 包 | 覆盖 |
|---|---|
| `features/stress` | Manager、互斥、报告、history、Docker executor、control API |
| `features/stress/resultparse` | 四项解析、HPCG fresh-file、NPU 完整通过 |
| `features/stress/cmd/workload-exec` | 请求白名单、路径、原子状态、进程组 |
| `features/stress/cli` | 默认配置、run/doctor/status/cancel、JSON/table |
| `features/web` | 单 listener 路由、origin/header/content-type 安全与 control-socket proxy |
| `internal/config` | YAML 默认值、V2 executor/container 字段 |

竞态：

```bash
go test -race ./features/stress/... ./features/web
```

## 4. workload plugin E2E

```bash
bash tests/e2e/stress_workload_plugin_e2e_test.sh
```

该测试构建真实 `catmonitor-stress-exec`，用固定 STREAM plugin fixture 验证：


- capability/describe；
- stdin request；
- normalized result；
- active job 互斥；
- status/cancel；
- 子进程组被回收；
- 不接受任意 command/options。

## 5. 构建 fixture

```bash
bash scripts/stress/tests/build_cpu_benchmarks_test.sh
bash scripts/stress/tests/build_cpu_runner_image_test.sh
bash scripts/stress/tests/ascend_env_test.sh
bash scripts/stress/tests/build_npu_burn_image_test.sh
bash scripts/stress/tests/runtime_preflight_test.sh
```

CPU fixture 必须确认：

- HPL top-level startup/refresh/build 顺序安全；
- STREAM/HPL/HPCG 资产进入 workload image；
- plugin 与 adapter 进入固定路径；
- 不构建 Unix Runner server/client；
- runtime 依赖和 manifest 生成正确。

NPU fixture 必须确认：

- bundled source/patch 边界；
- CANN environment source 成败是最终判据；
- builder/runtime ABI 校验；
- `pciutils/lspci` runtime 依赖；
- plugin/adapter 进入最终镜像；
- build 阶段不运行 NPU workload；
- A2 patch 不污染 A3 `none` profile。

## 6. Deployment/Compose 测试

```bash
bash scripts/stress/tests/generate_stress_deployment_test.sh
bash scripts/stress/tests/container_deployment_test.sh
bash scripts/stress/tests/audit_stress_release_test.sh
bash scripts/stress/audit_stress_release.sh
```

有 Compose 时额外验证：


```bash
docker compose \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.stress.yml \
  --profile stress-cpu config >/tmp/catmonitor-compose.yml
```

检查：

```bash
grep -n '/var/run/docker.sock' /tmp/catmonitor-compose.yml
```

期望只有 daemon 服务拥有 Docker socket；Web、DFeE 和 workload 容器都没有。

generator 至少运行两组 fixture：

1. CPU-only，无 NPU service override；
2. A2 sparse `/dev/davinci2,/dev/davinci5`，logical ID `0,1`。

还应静态检查不存在 0..7、两组四卡、最大 device ID 等于设备数等假设。

## 7. 容器集成测试

容器测试必须使用新名称和新架构：

```text
catmonitor
web
dfee
catmonitor-stress-cpu
catmonitor-stress-npu（可选）
```

最小 CPU 集成顺序：

```bash
docker compose ... --profile stress-cpu up -d
docker exec catmonitor catmonitor stress doctor -o table
docker exec catmonitor catmonitor stress run --bench stream -o table
```

验证：

- Web 单一 `:19322` listener 同时提供监控和 Stress；
- 19322 Run 成功，cross-origin/缺失 action header 请求被拒绝；
- daemon control socket 存在且为 Unix socket；
- report 的 `initiator=web`；
- latest/history/CLI 三处结果一致；
- CPU workload 容器内 job state 原子落盘；
- 完成后无 STREAM/numactl 残留进程。

取消用一个可控 fixture 或缩短 workload 验证，不能通过手工 kill daemon 代替。

## 8. A2 实机验收

发布支持声明前至少完成：

### 8.1 环境

记录但不提交节点隐私：

```bash
uname -a
uname -m
docker version
npu-smi info
ls -l /dev/davinci*
```

### 8.2 镜像身份

```bash
docker image inspect <control> <cpu> <npu> \
  --format '{{.Id}} {{.Architecture}} {{.Os}}'
```

记录 source commit、image ID/digest、CPU/NPU manifests。

### 8.3 启动与预检

```bash
docker compose ... --profile stress-cpu --profile stress-npu up -d
docker exec catmonitor catmonitor stress doctor -o table
```

必须 4/4：STREAM、HPL、HPCG、NPU Burn。

### 8.4 Workload 顺序

```bash
docker exec catmonitor catmonitor stress run --bench stream -o table
docker exec catmonitor catmonitor stress run --bench hpcg -o table
docker exec catmonitor catmonitor stress run --bench hpl -o table
docker exec catmonitor catmonitor stress run --bench npu_burn -o table
```

首次不要并发或一次勾选四项。每项后检查残留：

```bash
pgrep -af 'stream_omp|xhpl|xhpcg|mpirun|numactl' || true
docker exec catmonitor-stress-npu pgrep -af 'ascend_npu_burn|python' || true
```

### 8.5 A2 sparse device

若 host 只有 `/dev/davinci2,/dev/davinci5`，必须确认：

- Compose identity-map 2 和 5；
- 容器真实节点集合是 2、5；
- `lspci` logical topology 与 torch 设备数一致；
- NPU Burn logical ID 使用已验证的 0、1，而不是 2、5；
- device count 取映射数量 2，不取最大 ID 5。

### 8.6 Web

```bash
curl -fsS http://127.0.0.1:19322/api/stress/config
```

期望：`operator=true`、`security_debt_web_operator_auth=true`，且在 YAML
`web_enabled=true`、四项预检通过时可 Run。

通过 API/页面完成：STREAM Run、job 轮询、Cancel、history 持久化。

## 9. A3 16-device 验收矩阵

代码通用不等于 A3 已验证。未来 A3 门禁至少包含：

| 项目 | 期望 |
|---|---|
| `/dev/davinci` count | 16 |
| `lspci -D -d 19e5:` | 16 个 accelerator function |
| NPU Burn logical IDs | 0..15 |
| device 0/7/8/14/15 | describe/run 均按计划通过 |
| topology consistency | device node、PCI logical、torch index 可解释 |
| fallback | 不允许静默退回 0..7 |

必须在真实 A3 上运行至少 device 8、14、15 才能声明 16-device support validated。

## 10. 通过标准

```text
GO_TESTS=PASS
RACE=PASS
WORKLOAD_PLUGIN_E2E=PASS
CPU_BUILD_FIXTURE=PASS
NPU_BUILD_FIXTURE=PASS
DEPLOYMENT_FIXTURE=PASS
COMPOSE_SECURITY=PASS
A2_DOCTOR=PASS_4_OF_4
STREAM=PASS
HPL=PASS_OR_ACCEPTED_TIME_LIMIT
HPCG=PASS_OR_ACCEPTED_TIME_LIMIT
NPU_BURN=PASS_WITH_COMPLETE_VALIDATED_RESULT
WEB_RUN=PASS
WEB_CANCEL=PASS
PROCESS_CLEANUP=PASS
```

任何自动化 fixture 都不能替代镜像内真实 MPI/NUMA/NPU workload 验收。