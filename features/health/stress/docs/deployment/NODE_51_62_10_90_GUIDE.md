# 51.62.10.90 Linux 节点：压测部署、验收与 Web 使用指南

更新时间：2026-07-25。本文记录当前已验证的 STREAM、HPL 与 HPCG 部署事实。

> 文档类型：节点部署与使用指南。该节点使用 `/root/haoran` 资产和
> OpenMPI 参数，与 51.62.10.87 的 `/opt/catmonitor/benchmarks/runtime`
> 和 MPICH 配置不同。功能契约以 [STRESS_SPEC.md](../../STRESS_SPEC.md)
> 为准；不得在两个节点指南之间混用执行命令。

## 1. 当前已验证状态

- 节点：`51.62.10.90`。
- 项目目录：`/opt/catmonitor/CATMonitor-feature-stress-benchmark-web`。
- 已生成可执行文件：`bin/catmonitor`、`bin/catmonitor-web`。
- STREAM 资产：`/root/haoran/stream_omp`。
- `features/health/stress/benchmark_check.sh` 的 STREAM 分支在该节点使用：

  ```bash
  export OMP_NUM_THREADS="${OMP_NUM_THREADS:-32}"
  numactl --interleave=all /root/haoran/stream_omp
  ```

- 实机 CLI 已成功返回 `stream: healthy`，约 1117 ms；解析到 Copy、Scale、Add、Triad 四项带宽值。`healthy` 的含义仅为命令成功退出且四项数值解析成功，不比较性能阈值。
- HPL 主机：aarch64、Kunpeng 920、96 核、2 Socket、4 NUMA、381 GiB 内存；每个 NUMA 24 核。
- HPL 2.3 可执行文件：`/root/haoran/hpl-2.3/bin/MyConfig/xhpl`；同目录使用固定 `HPL.dat`。
- HPL 依赖：OpenMPI 4.1.5、OpenBLAS、gfortran、gcc；OpenBLAS 动态库目录为 `/usr/local/openblas/lib`。
- HPL 已验证的单机运行模型为 8 个 MPI 进程、每进程 12 个 OpenBLAS/OMP 线程，不使用 `--bind-to core`。N=50000、NB=256、P=4、Q=2 时实测约 150.60 秒、553.37 GFLOPS。
- 官方 HPCG 3.1 位于 `/root/haoran/hpcg-3.1/build_Kunpeng_MPI_OMP/bin/xhpcg`。96 MPI × 1 OpenMP、逐核绑定、每 rank 32×32×32、`--rt=60` 时结果 VALID，总耗时 62.2467 秒、22.1496 GFLOP/s。

压测结果不会直接折算到 `HealthScore`；但执行期间 CPU/内存负载、温度与 I/O 指标可能使实时健康分暂时下降。

## 2. 通用启动前检查

```bash
cd /opt/catmonitor/CATMonitor-feature-stress-benchmark-web
ROOT="$PWD"

test -x "$ROOT/bin/catmonitor"
test -x "$ROOT/bin/catmonitor-web"
test -x "$ROOT/features/health/stress/benchmark_check.sh"

ldd "$ROOT/bin/catmonitor-web"
# 输出中不得包含 "not found"

bash -n "$ROOT/features/health/stress/benchmark_check.sh"
install -d -m 0750 /etc/catmonitor /var/lib/catmonitor
```

如果重新拉取代码后需要重新编译：

```bash
cd "$ROOT"
go build -o bin/catmonitor ./cmd/catmonitor
go build -o bin/catmonitor-web ./features/web
```

## 3. STREAM：当前节点的标准验收

先确认节点专属脚本适配仍然存在：

```bash
test -x /root/haoran/stream_omp
command -v numactl
grep -nE 'stream_omp|interleave|OMP_NUM_THREADS|stream\)' \
  "$ROOT/features/health/stress/benchmark_check.sh"
```

创建 CLI 配置 `/etc/catmonitor/catmonitor.yaml`：

```yaml
health:
  stress:
    enabled: true
    web_enabled: false
    script_path: /opt/catmonitor/CATMonitor-feature-stress-benchmark-web/features/health/stress/benchmark_check.sh
    report_path: /var/lib/catmonitor/stress-latest.json
    default_benchmarks: [stream]
    benchmarks:
      stream:
        enabled: true
        timeout: 1m
```

先以 CLI 验收：

```bash
cd "$ROOT"
./bin/catmonitor health stress run --help
./bin/catmonitor health stress run --bench stream \
  -c /etc/catmonitor/catmonitor.yaml -o table
echo "exit_code=$?"
python3 -m json.tool /var/lib/catmonitor/stress-latest.json
```

只在机器空闲时进行手工真实预检：

```bash
OMP_NUM_THREADS=32 numactl --interleave=all /root/haoran/stream_omp \
  2>&1 | tee /tmp/stream-manual-precheck.log
echo "stream_exit_code=${PIPESTATUS[0]}"
grep -E '^(Copy|Scale|Add|Triad):' /tmp/stream-manual-precheck.log
```

运行结束或取消后确认无残留：

```bash
pgrep -af 'stream_omp|numactl' || true
```

## 4. HPL：当前节点的固定适配

### 4.1 资产与脚本

先确认已验证资产未发生变化：

```bash
test -x /root/haoran/hpl-2.3/bin/MyConfig/xhpl
test -r /root/haoran/hpl-2.3/bin/MyConfig/HPL.dat
test -d /usr/local/openblas/lib
command -v mpirun
mpirun --version
ldd /root/haoran/hpl-2.3/bin/MyConfig/xhpl | grep openblas
```

HPL 与 STREAM 使用相同边界：完整路径、环境变量和 MPI 命令全部写在
`benchmark_check.sh`，不从 YAML 读取 HPL 路径，也不新增 `run_hpl.sh`。
当前脚本固定执行：

```bash
export LD_LIBRARY_PATH=/usr/local/openblas/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}
export OPENBLAS_NUM_THREADS=12
export OMP_NUM_THREADS=12

cd /root/haoran/hpl-2.3/bin/MyConfig
mpirun \
  -x OPENBLAS_NUM_THREADS \
  -x OMP_NUM_THREADS \
  -np 8 \
  /root/haoran/hpl-2.3/bin/MyConfig/xhpl
```

不要加入 `--bind-to core`，也不要恢复旧脚本中的
`--oversubscribe`、`ppr:16:node:pe=32`、UCX/MCA 参数。这些旧参数不属于
当前已验证配置。HPL 会从工作目录读取 `HPL.dat`；用于 CATMonitor 的版本应
固定，不要在压测前被人工 benchmark 临时修改。

推荐先用脚本本身进行手工预检，以保证与 CATMonitor 路径一致：

```bash
bash "$ROOT/features/health/stress/benchmark_check.sh" hpl \
  2>&1 | tee /tmp/hpl-manual-precheck.log
echo "hpl_exit_code=${PIPESTATUS[0]}"
```

正常结束时，输出必须存在 HPL 的最终 `Time/Gflops` 结果行。CATMonitor
解析 `n`、`nb`、`p`、`q`、`process`、`time_seconds` 和 `gflops`；
非零 failed residual check 或独立的 `FAILED` 状态会判为 `unhealthy`。

### 4.2 CLI 配置与验收

在 CLI 配置中增加（初次只启用 HPL，不与 STREAM 并发）：

```yaml
health:
  stress:
    enabled: true
    web_enabled: false
    script_path: /opt/catmonitor/CATMonitor-feature-stress-benchmark-web/features/health/stress/benchmark_check.sh
    report_path: /var/lib/catmonitor/stress-latest.json
    benchmarks:
      hpl:
        enabled: true
        timeout: 4m
```

执行：

```bash
./bin/catmonitor health stress run --bench hpl \
  -c /etc/catmonitor/catmonitor.yaml -o table
echo "hpl_catmonitor_exit_code=$?"
```

当前完整测试约 150.60 秒，首次 CATMonitor 验收建议保留余量，使用
`timeout: 4m`。正常结束时报告包含规模、进程网格、`gflops` 与
`time_seconds`。如果将计算量调大并运行至 YAML 窗口，报告为
`time_limit_reached`、整体为 `Healthy`，没有最终数值属于正常现象。
HPL 运行负载极高，只在空闲窗口执行，并确认 MPI 不会影响其他作业。

## 5. HPCG：当前节点的固定适配

### 5.1 资产与脚本

先检查已验证资产：

```bash
HPCG_DIR=/root/haoran/hpcg-3.1/build_Kunpeng_MPI_OMP/bin
test -x "$HPCG_DIR/xhpcg"
command -v mpirun
mpirun --version
```

HPCG 与 STREAM/HPL 一样由 `benchmark_check.sh` 维护完整执行参数，不新增
wrapper，也不从 YAML 读取可执行路径。固定命令为：

```bash
export OMP_NUM_THREADS=1
export OMP_DYNAMIC=FALSE

cd /root/haoran/hpcg-3.1/build_Kunpeng_MPI_OMP/bin
mpirun \
  --map-by core \
  --bind-to core \
  -x OMP_NUM_THREADS \
  -x OMP_DYNAMIC \
  -np 96 \
  ./xhpcg \
  --nx=32 --ny=32 --nz=32 --rt=60
```

正式脚本不使用 `--report-bindings`，避免大量绑定日志进入报告。旧的
`ppr:608:node:pe=1`、UCX/MCA 参数已删除。

使用统一脚本预检：

```bash
HPCG_DIR=/root/haoran/hpcg-3.1/build_Kunpeng_MPI_OMP/bin
bash "$ROOT/features/health/stress/benchmark_check.sh" hpcg \
  2>&1 | tee /tmp/hpcg-manual-precheck.log
echo "hpcg_exit_code=${PIPESTATUS[0]}"
find "$HPCG_DIR" -maxdepth 2 -type f -iname 'HPCG-Benchmark*.txt' -printf '%TY-%Tm-%Td %TT %p\n'
```

CATMonitor 在启动前记录 `result_dir` 中所有候选文件的大小、修改时间和
SHA-256。命令返回 0 后，必须找到本次新增或变化的结果文件，并从其中解析
`HPCG result is VALID`、GFLOP/s 和执行时间。stdout 不能替代结果文件，
未变化的历史文件也不能作为本次成功依据。

### 5.2 CLI 配置与验收

```yaml
health:
  stress:
    enabled: true
    web_enabled: false
    script_path: /opt/catmonitor/CATMonitor-feature-stress-benchmark-web/features/health/stress/benchmark_check.sh
    report_path: /var/lib/catmonitor/stress-latest.json
    benchmarks:
      hpcg:
        enabled: true
        result_dir: /root/haoran/hpcg-3.1/build_Kunpeng_MPI_OMP/bin
        timeout: 3m
```

执行：

```bash
./bin/catmonitor health stress run --bench hpcg \
  -c /etc/catmonitor/catmonitor.yaml -o table
echo "hpcg_catmonitor_exit_code=$?"
python3 -m json.tool /var/lib/catmonitor/stress-latest.json
```

`--rt=60` 只控制主要测试阶段，本次总耗时为 62.2467 秒，因此 YAML 使用
`timeout: 3m`，不能设置为 60 秒。正常完成时报告应包含
`gflops: 22.1496`、`time_seconds: 62.2467`，来源为 `result_file`。
若未来主动把计算量调大并运行至 CATMonitor 窗口，仍按
`time_limit_reached` 通过；但当前固定 60 秒测试应正常生成完整结果。

## 6. Web：仅在 CLI 验收后开启

Web 配置 `/etc/catmonitor/web.yaml` 示例（先只显示和执行已验收项目）：

```yaml
server:
  addr: "127.0.0.1:9527"
storage:
  snapshot_path: /var/lib/catmonitor/snapshot.json
  runtime_path: /var/lib/catmonitor/runtime.json
health:
  stress:
    enabled: true
    web_enabled: true
    script_path: /opt/catmonitor/CATMonitor-feature-stress-benchmark-web/features/health/stress/benchmark_check.sh
    report_path: /var/lib/catmonitor/stress-latest.json
    default_benchmarks: [stream]
    benchmarks:
      stream: { enabled: true, timeout: 1m }
      hpl:    { enabled: true, timeout: 4m }
      hpcg:   { enabled: true, result_dir: /root/haoran/hpcg-3.1/build_Kunpeng_MPI_OMP/bin, timeout: 3m }
```

前台启动并验证：

```bash
cd "$ROOT"
./bin/catmonitor-web -config /etc/catmonitor/web.yaml
# 第二个 SSH 终端：
curl --fail --show-error http://127.0.0.1:9527/api/health/stress/config
ss -lntp | grep ':9527'
```

监听必须是 `127.0.0.1:9527`，不能是 `0.0.0.0:9527` 或 `[::]:9527`。Web 的单次超时只可缩短，不可超过 YAML 上限；不能通过网页修改路径、脚本、环境变量、NUMA/MPI 或线程数。

Windows 通过 SSH 隧道访问，不直接暴露端口：

```powershell
ssh -N -o ExitOnForwardFailure=yes `
  -L 127.0.0.1:9527:127.0.0.1:9527 root@51.62.10.90
```

浏览器地址：`http://127.0.0.1:9527/#/stress`。概览页会显示独立的“最近压测”摘要卡，不会合并进健康总分。

## 7. 取消与故障处置

- 页面取消后，立刻在 Linux 检查：`pgrep -af 'stream_omp|xhpl|xhpcg|mpirun|numactl' || true`。
- 新版在 Linux 上按进程组停止 Bash、MPI 和 benchmark 子进程；到期或取消后仍应使用 `pgrep` 确认目标环境没有异常残留。
- `unavailable`：检查脚本、二进制、目录和 `mpirun`/`numactl`。
- `unhealthy`：命令非零退出或结果解析失败；优先查看报告中的 `output`、手工预检日志和 HPCG 结果文件。
- `time_limit_reached`：STREAM/HPL/HPCG 的计划压测窗口结束均属于通过。Web 显示绿色 `OK`，数值列显示“已按时限停止；通过；未产生最终性能数据”。
- HPCG 正常结束时只读取本次作业新增或发生变化的结果文件，不会使用目录中未变化的旧 `HPCG-Benchmark*.txt`。
- 直接调用启动/取消 API 时必须增加 `Content-Type: application/json` 和 `X-CATMonitor-Action: health-stress`；通常应直接使用同源 Web 页面。
- `timeout`：仅兼容旧版报告，语义是未完成。新版本不会为新作业产生该状态；不要通过 Web 提高 YAML 上限来掩盖问题。
