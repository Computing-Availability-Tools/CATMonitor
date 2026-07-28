# Linux 节点 51.62.10.90：STREAM 压测与 Windows Web 访问

> 文档状态：已归档。本文是仅接入 STREAM 时的早期操作记录，已由
> [`../deployment/NODE_51_62_10_90_GUIDE.md`](../deployment/NODE_51_62_10_90_GUIDE.md)
> 取代。新部署不得只参考本文；当前契约以
> [STRESS_SPEC.md](../../STRESS_SPEC.md) 为准。

> 已验证项目目录、实际 STREAM 结果和 HPL/HPCG 后续上线流程见
> [`../deployment/NODE_51_62_10_90_GUIDE.md`](../deployment/NODE_51_62_10_90_GUIDE.md)。
> 本文件仅保留为 STREAM/SSH 隧道的早期简版记录。

## 1. 目标与边界

- 节点：`51.62.10.90`（Linux / NPU 服务器）。
- STREAM 可执行文件：`/root/haoran/stream_omp`。
- 实际压测命令：`numactl --interleave=all /root/haoran/stream_omp`。
- Web 服务只绑定 `127.0.0.1`，Windows 使用 SSH 隧道访问。不要将压测 Web API 直接暴露到公网或业务网段。

`features/health/stress/benchmark_check.sh` 是此节点的适配脚本，当前 STREAM 分支已经写入上述绝对路径、NUMA 策略和默认 `OMP_NUM_THREADS=32`（若环境已设置该变量则保留原值）。后续换机器时在该脚本中调整，不通过网页或 YAML 传递命令、路径、NUMA/MPI 或环境变量。

## 2. 上传、编译与预检

将整个 `CATMonitor` 项目上传到 Linux，例如 `/opt/catmonitor`。以下命令以 root 执行，因为压测资产位于 `/root/haoran`：

```bash
cd /opt/catmonitor
chmod 755 features/health/stress/benchmark_check.sh

test -x /root/haoran/stream_omp
command -v numactl
bash -n features/health/stress/benchmark_check.sh

go build -o bin/catmonitor ./cmd/catmonitor
go build -o bin/catmonitor-web ./features/web
```

真实手工预检会产生压测负载，只能在确认机器空闲时执行：

```bash
numactl --interleave=all /root/haoran/stream_omp
```

输出必须包含 STREAM 的 `Copy`、`Scale`、`Add`、`Triad` 四项；CATMonitor 只有成功解析四项才会把结果标记为 `healthy`。

## 3. CLI 配置与执行

创建 `/etc/catmonitor/catmonitor.yaml`：

```yaml
health:
  stress:
    enabled: true
    web_enabled: false
    script_path: /opt/catmonitor/features/health/stress/benchmark_check.sh
    report_path: /var/lib/catmonitor/stress-latest.json
    default_benchmarks: [stream]
    benchmarks:
      stream:
        enabled: true
        timeout: 1m
```

`timeout: 1m` 是此节点 STREAM 的最长运行窗口。该节点实测通常约 1 秒完成，1 分钟可覆盖正常启动抖动并及时终止异常挂起。达到窗口后 CATMonitor 主动停止并记录 `time_limit_reached`，与 HPL/HPCG 一样按通过处理，允许没有最终带宽值。压测命令、执行器路径、NUMA 和 OMP 环境变量不在 YAML 中配置。

先使用 CLI 验收，避免先开放 Web：

```bash
cd /opt/catmonitor
./bin/catmonitor health stress run --bench stream -c /etc/catmonitor/catmonitor.yaml -o table
```

成功时报告包含四个带宽数值；非零退出、`numactl` 不存在、二进制不可执行或缺少四项输出均为失败，报告文件位于 `/var/lib/catmonitor/stress-latest.json`。

## 4. Web 配置与启动

CLI 验收后，如需网页触发，创建 `/etc/catmonitor/web.yaml`：

```yaml
server:
  addr: "127.0.0.1:9527"

collector:
  refresh_interval: 5s
  history_points: 60

storage:
  snapshot_path: /var/lib/catmonitor/snapshot.json
  runtime_path: /var/lib/catmonitor/runtime.json

health:
  stress:
    enabled: true
    web_enabled: true
    script_path: /opt/catmonitor/features/health/stress/benchmark_check.sh
    report_path: /var/lib/catmonitor/stress-latest.json
    default_benchmarks: [stream]
    benchmarks:
      stream:
        enabled: true
        timeout: 1m
```

启动服务：

```bash
mkdir -p /var/lib/catmonitor
cd /opt/catmonitor
./bin/catmonitor-web -config /etc/catmonitor/web.yaml
```

确认 Linux 本机服务正常：

```bash
curl http://127.0.0.1:9527/api/health/stress/config
```

返回中的 `enabled` 应为 `true`，且 `stream` 的 `enabled` 为 `true`。

## 5. 从另一台 Windows 访问网页

在能够 SSH 到节点的 Windows PowerShell 中保持一个隧道窗口：

```powershell
ssh -N -L 9527:127.0.0.1:9527 root@51.62.10.90
```

然后在该 Windows 浏览器访问：

```text
http://127.0.0.1:9527/#/stress
```

不要访问 `http://51.62.10.90:9527`，也不要把 Linux 配置改为 `:9527` 或 `0.0.0.0:9527`；这样会绕过当前对高负载压测接口的回环保护。

网页操作顺序：

1. 确认页面中 `stream` 显示为已配置。
2. 勾选 `stream`。
3. 可选填写“本次最长运行时间（秒）”。该值只能小于或等于 YAML 的 `1m`（60 秒），仅作用于本次作业，不会保存。
4. 点击“开始压测”，阅读确认提示后提交。
5. 在页面查看 `running`、最终状态和 Copy/Scale/Add/Triad 数值；需要提前停止时使用“取消作业”。

## 6. 运行注意事项

- 压测会抬高 CPU/内存负载、温度和 I/O；健康总分可能在运行期间暂时下降。
- 仅在业务空闲窗口执行，并确保节点有足够散热和运维监控。
- STREAM 首次实机测试仍单独执行。HPL 已按 8 MPI × 12 线程、HPCG 已按 96 MPI × 1 线程完成手工 benchmark；CATMonitor 单项接入步骤见完整版操作文档。
- 如果以非 root 用户运行 Web，必须使其具有执行 `/root/haoran/stream_omp` 的权限；更简单且与当前目录位置一致的方式是由受控 root 服务启动。
