# health/stress 执行与测试指南

> 文档定位：开发、构建和通用验收指南。生产节点的具体资产路径、MPI/NUMA
> 参数和实测结果统一放在 [docs/README.md](docs/README.md) 分类管理；若与
> 本文示例冲突，以 [STRESS_SPEC.md](STRESS_SPEC.md) 的功能契约和对应节点
> 指南为准。

本文用于验证 CATMonitor 0.3.2 的 `health/stress` 子特性。测试分为三层：

1. Windows/WSL 完成编译、单元测试和 Web 页面预览；
2. Linux 目标节点完成 CLI、Web API 与状态流转验证；
3. 具备真实 STREAM/HPL/HPCG 资产的 Linux 节点完成实机验收。

真实压测只支持 Linux。Windows 构建用于保证项目可移植，不能在 Windows
直接运行 benchmark；WSL 适合编译、单元测试和页面预览，但不能代替目标
节点的 CPU、NUMA、MPI 和结果文件验收。

## 1. WSL 构建环境

Windows 当前工作树：

```text
D:\project\CATMonitor-v0.3.2-fork
```

对应的 WSL 路径：

```text
/mnt/d/project/CATMonitor-v0.3.2-fork
```

先检查基础工具：

```bash
cd /mnt/d/project/CATMonitor-v0.3.2-fork

go version
bash --version
python3 --version
git status --short
```

Go 版本应满足 `go.mod` 中的 Go 1.23.4 要求。Python 不是
`health/stress` 的运行依赖，但项目中其他测试或辅助脚本可能使用它。
目标压测节点还应检查：

```bash
mpirun --version
numactl --version
```

## 2. 本地自动化检查

### 2.1 脚本和核心单元测试

在仓库根目录执行：

```bash
bash -n features/health/stress/benchmark_check.sh

go test -race -buildvcs=false ./features/health/stress -count=1
go test -race -buildvcs=false ./features/web -run TestHealthStress -count=1
go test -buildvcs=false ./cmd/catmonitor -count=1
go vet ./...
```

这些检查覆盖：

- STREAM、HPL、HPCG 输出解析；
- HPL residual failure 和独立 `FAILED` 检测；
- HPCG 本次结果文件与历史文件隔离；
- 三类 benchmark 的 `time_limit_reached` 通过语义；
- 进程组停止、用户取消、报告原子写入和持久化错误；
- CLI 帮助、参数和退出码；
- Web 提交保护、单次缩短超时、查询和取消。

### 2.2 全量 Go 测试

原生 Linux 文件系统中的仓库应直接运行：

```bash
go test -buildvcs=false ./... -count=1
```

仓库位于 WSL 的 `/mnt/d`（Windows NTFS）时，Linux 测试创建的 sysfs
符号链接夹具可能被落成普通文件。如果仅有网络信息符号链接用例因此失败，
可使用：

```bash
go test -buildvcs=false ./... -skip='Test.*Net.*Info' -count=1
```

该跳过项只用于 WSL/NTFS 兼容性，不代表目标 Linux 节点可以免测。把仓库
复制到 WSL 的 ext4 目录或原生 Linux 节点后，应重新执行不带 `-skip` 的
全量测试。

### 2.3 Linux 与 Windows 构建

```bash
GOOS=linux GOARCH=arm64 \
  go build -buildvcs=false -o /tmp/catmonitor-linux-arm64 ./cmd/catmonitor
GOOS=linux GOARCH=arm64 \
  go build -buildvcs=false -o /tmp/catmonitor-web-linux-arm64 ./features/web

GOOS=windows GOARCH=amd64 \
  go build -buildvcs=false -o /tmp/catmonitor-windows-amd64.exe ./cmd/catmonitor
GOOS=windows GOARCH=amd64 \
  go build -buildvcs=false -o /tmp/catmonitor-web-windows-amd64.exe ./features/web
```

交叉构建通过只说明代码可编译；Windows 上执行
`catmonitor health stress run ...` 应返回 `unsupported`。

## 3. WSL Web 页面预览

你的构建和启动方式应改为当前工作树路径：

```bash
cd /mnt/d/project/CATMonitor-v0.3.2-fork
mkdir -p features/web/bin
go build -buildvcs=false -o features/web/bin/catmonitor-web ./features/web
./features/web/bin/catmonitor-web -config features/web/config.yaml
```

启动日志会给出实际监听端口。`9527` 被占用时服务会尝试后续端口。
在 WSL 中查询地址：

```bash
hostname -I
```

然后在 Windows 浏览器访问：

```text
http://<WSL-IP>:9527/
```

默认 `features/web/config.yaml` 的 `server.addr` 是 `:9527`，且
`health.stress.enabled`、`web_enabled` 和三个 benchmark 均为 `false`。
因此它适合查看概览页和压测页面的未配置状态，但不允许提交真实压测。

页面预览至少检查：

- 概览页能够显示整体健康度、部件卡片和最近压测摘要；
- `#/stress` 能显示 STREAM/HPL/HPCG 三项；
- 未配置项不可勾选，启动按钮禁用且有可见提示；
- 结果数值逐项换行，成功状态使用与健康页一致的通过样式；
- 页面和脚本中没有 OSU 项目。

## 4. Linux 目标节点配置

先在目标节点复制并修改唯一的节点适配脚本：

```text
features/health/stress/benchmark_check.sh
```

STREAM、HPL、HPCG 的二进制绝对路径、环境变量、工作目录、MPI 和 NUMA
参数全部在该脚本中维护，不再增加二次 wrapper，也不把执行路径放入 YAML。
部署后检查：

```bash
chmod +x /opt/catmonitor/CATMonitor/features/health/stress/benchmark_check.sh
bash -n /opt/catmonitor/CATMonitor/features/health/stress/benchmark_check.sh
```

CLI 配置示例：

```yaml
health:
  stress:
    enabled: true
    web_enabled: false
    script_path: /opt/catmonitor/CATMonitor/features/health/stress/benchmark_check.sh
    report_path: /var/lib/catmonitor/stress-latest.json
    default_benchmarks: [stream]
    benchmarks:
      stream:
        enabled: true
        timeout: 1m
      hpl:
        enabled: true
        timeout: 4m
      hpcg:
        enabled: true
        result_dir: /root/haoran/hpcg-3.1/build_Kunpeng_MPI_OMP/bin
        timeout: 3m
```

`hpcg.result_dir` 只用于 Go 核验和读取本次生成的
`HPCG-Benchmark*.txt`，不是执行器路径。

## 5. CLI 实机验收

先逐项执行，确认单项稳定后再组合执行：

```bash
./catmonitor health stress run --help

./catmonitor health stress run \
  --bench stream -c /etc/catmonitor/catmonitor.yaml -o table

./catmonitor health stress run \
  --bench hpl -c /etc/catmonitor/catmonitor.yaml -o table

./catmonitor health stress run \
  --bench hpcg -c /etc/catmonitor/catmonitor.yaml -o json

./catmonitor health stress run \
  --bench stream,hpl,hpcg -c /etc/catmonitor/catmonitor.yaml -o table
```

Manager 同时只允许一个作业，组合请求也会按顺序逐项运行。

预期结果：

| 项目 | 正常完成必须验证的内容 |
|------|------------------------|
| STREAM | `healthy`，并上报 copy/scale/add/triad 带宽 |
| HPL | `healthy`，并上报 N、NB、P、Q、进程数、耗时和 GFLOPS |
| HPCG | `healthy`，来源是本次新增或变化的结果文件，并上报 GFLOP/s 和耗时 |

状态解释：

- `healthy`：命令正常结束且必需结果解析成功；
- `time_limit_reached`：到达配置的压力窗口后被 CATMonitor 主动停止，按通过
  处理，允许没有 GFLOPS/Time；
- `unhealthy`：命令在时限前非零退出，或正常结束后必需结果无效；
- `unavailable`：脚本、二进制、输入文件、结果目录等资产不可用；
- `unsupported`：当前操作系统不支持真实压测；
- `cancelled`：用户主动停止。

HPL/HPCG 的工具内部目标运行时间不等于 CATMonitor 超时。正常完成时，
HPCG 必须读取本次结果文件并包含 `HPCG result is VALID`；不能用历史文件
或 stdout 中的数值替代。只有 CATMonitor 主动到达配置窗口时，才使用
`time_limit_reached` 的无最终数值通过语义。

## 6. Web 实机验收与 Windows 远程访问

允许 Web 触发时，在 Linux 节点配置：

```yaml
server:
  addr: "127.0.0.1:9527"

health:
  stress:
    enabled: true
    web_enabled: true
```

`enabled` 是 CLI/Manager 总开关；`web_enabled` 是在总开关之上额外授权
Web 提交，两者不重复。为限制高负载操作，Web 只允许回环绑定和回环来源。

从另一台 Windows 访问 Linux 节点时建立 SSH 隧道：

```powershell
ssh -L 9527:127.0.0.1:9527 root@<Linux节点IP>
```

保持终端开启，在 Windows 浏览器打开：

```text
http://127.0.0.1:9527/
```

Web 验收顺序：

1. 打开 `#/stress`，确认只显示已配置的 STREAM/HPL/HPCG；
2. 勾选一个项目；
3. 可选填写小于等于 YAML 上限的单次超时；
4. 点击“开始压测”，确认运行中状态和停止按钮；
5. 刷新或重新进入页面，确认最新报告可恢复；
6. 验证正常完成、主动停止和 `time_limit_reached` 三类展示；
7. 确认压测结果不直接修改健康总分，但同期高负载可能使实时健康指标和
   健康分暂时变化。

直接验证 API 时，请求必须带操作头：

```bash
curl -H 'Content-Type: application/json' \
  -H 'X-CATMonitor-Action: health-stress' \
  -d '{"benchmarks":["stream"],"timeout_seconds":60}' \
  http://127.0.0.1:9527/api/health/stress/runs
```

Web 只允许为单次作业缩短 YAML 超时，不能延长或修改节点执行命令。

## 7. 运行安全与清理

压测会占满 CPU、内存带宽或 MPI 资源，应在业务空闲窗口运行，并提前确认
温度、供电和节点调度状态。停止服务或压测后先检查残留进程：

```bash
pgrep -af 'stream_omp|xhpl|xhpcg|mpirun|numactl'
```

正常情况下，取消和达到时限都会停止本地 Bash、MPI 启动器及同进程组子进程。
多节点 MPI 的远端进程清理由 MPI 实现和部署脚本决定，在完成多节点实机
验证前不视为已支持。

报告文件：

```text
/var/lib/catmonitor/stress-latest.json
```

验收时应保存配置、报告、CATMonitor 日志、benchmark stdout/stderr，以及
HPL/HPCG 本次结果文件，便于区分程序故障、资产错误和计算结果无效。

## 8. 验收清单

- [ ] WSL/Windows 和 Linux 交叉构建通过；
- [ ] `bash -n`、单元测试、竞态测试、全量测试和 `go vet` 通过；
- [ ] Windows 压测明确返回 `unsupported`；
- [ ] WSL Web 概览页和 `#/stress` 页面显示正常；
- [ ] Web 未配置状态有提示且不能误提交；
- [ ] STREAM 实机正常完成和限时停止均符合状态约定；
- [ ] HPL 正常结果、residual failure、限时停止均符合状态约定；
- [ ] HPCG 只读取本次结果文件，并验证 VALID、GFLOP/s 和耗时；
- [ ] Web 只能经回环绑定提交，并仅允许缩短单次超时；
- [ ] 取消或限时停止后没有本地残留 benchmark 进程；
- [ ] 组合执行保持串行，最终报告可从 CLI 和 Web 一致读取。
