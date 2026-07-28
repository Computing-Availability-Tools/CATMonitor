# CATMonitor health/stress 执行测试与验收记录

更新时间：2026-07-28

> 文档类型：滚动验收记录。测试方法以
> [STRESS_TEST_GUIDE.md](../../STRESS_TEST_GUIDE.md) 为准；本文只记录已
> 执行环境、结果和剩余实机项目，不定义新的功能契约。

## 1. 当前基线

- Windows 工作树：`D:\project\CATMonitor-v0.3.2-fork`
- WSL 路径：`/mnt/d/project/CATMonitor-v0.3.2-fork`
- 分支：`feature/health-stress-v0.3.2`
- 正式测试指南：
  [`../../STRESS_TEST_GUIDE.md`](../../STRESS_TEST_GUIDE.md)
- 51.62.10.87 实机使用指南：
  [`../deployment/NODE_51_62_10_87_GUIDE.md`](../deployment/NODE_51_62_10_87_GUIDE.md)

后续执行步骤以仓库内的正式指南为准；本文件只记录当前迁移状态和最近一次
验收结果，避免操作说明形成两份独立副本。

## 2. 2026-07-27 本地验收结果

WSL 环境：

```text
Go 1.26.5 linux/amd64
Bash 5.2.21
Python 3.12.3
```

通过项：

- `bash -n features/health/stress/benchmark_check.sh`
- `go test -race -buildvcs=false ./features/health/stress -count=1`
- `go test -race -buildvcs=false ./features/web -run TestHealthStress -count=1`
- `go test -buildvcs=false ./cmd/catmonitor -count=1`
- `go vet ./...`
- WSL/NTFS 兼容模式全量 Go 测试
- Linux arm64 的 CLI 与 Web 交叉构建
- Windows amd64 的 CLI 与 Web 交叉构建
- `catmonitor health stress run --help`
- Web 根页面和 `/api/health/stress/config`

不带跳过参数的全量测试只失败：

```text
features/web.TestHWNetInfo
internal/source/sys.TestNetInterfaceInfo
```

原因是仓库位于 WSL `/mnt/d`，测试创建的 Linux sysfs 符号链接夹具无法按
原生 Linux 文件系统语义工作，网卡 driver 标签为空。按以下命令验证其余
全部包均通过：

```bash
go test -buildvcs=false ./... -skip='Test.*Net.*Info' -count=1
```

这不是 stress 故障。复制到 WSL ext4 或目标 Linux 节点后，必须再执行一次
不带 `-skip` 的全量测试。

## 3. 本地 Web 预览说明

此前 WSL 页面预览仅用于检查界面，不作为持续运行的部署服务。每次使用前应重新
通过 `pgrep -af catmonitor-web` 和 `ss -lntp` 确认实际进程与端口，不依赖旧
PID 记录。

仓库默认配置只用于安全预览：

```text
platform=linux
feature_enabled=false
web_enabled=false
loopback=false
STREAM/HPL/HPCG 均 disabled
```

因此未配置项和禁用按钮是预期状态，不能从该服务提交真实压测。真实 Linux
节点若要从另一台 Windows 触发，应让 Web 监听 `127.0.0.1:9527`，同时启用
`health.stress.enabled` 和 `web_enabled`，再通过 SSH 隧道访问。

## 4. 2026-07-28 Linux 实机验收

节点 `51.62.10.87` 已将资产统一部署到：

```text
/opt/catmonitor/benchmarks/runtime/
```

CLI 三项均通过：

| Benchmark | 状态 | 结果 | 总耗时 |
|---|---|---:|---:|
| STREAM | healthy | Copy 86856.90、Scale 87821.60、Add 89099.30、Triad 91385.40 MB/s | 1.059 秒 |
| HPCG | healthy | 8.11 GFLOP/s，结果文件计算时间 61.27 秒 | 123.493 秒 |
| HPL | healthy | 205.13 GFLOP/s，N=50000、NB=256、P×Q=4×2 | 421.046 秒 |

已确认：

- CATMonitor → 节点脚本 → 固定运行目录 → 结果解析 → JSON 报告链路正常；
- HPCG 结果文件包含 `HPCG result is VALID`；
- HPL residual check 通过；
- 受限网络环境可在缓存 `yaml.v3@v3.0.1` 后离线构建 CLI 和 Web。

仍需实机完成：

- Web 分别运行 STREAM、HPCG、HPL；
- Web 主动取消与限时停止；
- 勾选项和单次超时输入跨 5 秒刷新保持；
- 取消/限时停止后的 MPI/benchmark 残留进程检查；
- 多节点 MPI 在完成远端进程清理验收前仍不声明支持。

## 5. 当前结论

0.3.2 的 STREAM/HPL/HPCG CLI 已在 Linux ARM64 实机全部验收通过，当前可以
进入 Web 实机验收阶段。三项不设置性能阈值，正常解析完成或按配置窗口主动
停止均可判为通过；提前报错、HPL 校验失败或 HPCG 正常退出后缺少本次有效
结果文件判为失败。
