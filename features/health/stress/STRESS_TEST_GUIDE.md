# health/stress 测试指南

本文只描述可复用的开源验证流程。具体节点地址、资产路径、MPI/NUMA 参数、
性能数值和验收记录不属于开源测试契约。

## 1. 测试文件组织

Go 单元测试遵循语言约定，与被测 package 放在同一目录并命名为
`*_test.go`。这样可以测试 package 内部边界，并由 `go test ./...` 自动发现。
仓库根目录的 `tests/testdata` 只保存多个 package 共用的模拟输入，不放置
依赖具体机器的验收结果。

## 2. 自动化检查

在仓库根目录执行：

```bash
bash -n features/health/stress/benchmark_check.sh
go test ./cmd/catmonitor ./features/health/stress ./features/web
go test ./...
go vet ./...
```

Linux 进程组终止测试需要支持 `/proc` 和 Unix 信号。若仓库位于不完整支持
符号链接的挂载文件系统，应复制到原生 Linux 文件系统后执行最终全量测试。

## 3. 构建

```bash
go build -buildvcs=false -o bin/catmonitor ./cmd/catmonitor
go build -buildvcs=false -o bin/catmonitor-web ./features/web
```

可以通过 `GOOS`、`GOARCH` 做交叉构建；真实 benchmark 第一版只支持 Linux。
Windows 构建用于验证可移植性，执行压测应明确返回 `unsupported`。

## 4. Linux 主机适配

部署前编辑 `features/health/stress/benchmark_check.sh` 顶部配置区，写入该主机
实际使用的绝对路径、线程数、MPI 进程数和运行参数。不要把执行器路径放入
YAML，也不要通过 Web 请求接收任意命令。

```bash
chmod +x features/health/stress/benchmark_check.sh
bash -n features/health/stress/benchmark_check.sh
```

配置示例：

```yaml
health:
  stress:
    enabled: true
    web_enabled: false
    script_path: /absolute/path/to/benchmark_check.sh
    report_path: /absolute/path/to/stress-latest.json
    default_benchmarks: [stream]
    benchmarks:
      stream: { enabled: true, timeout: 1m }
      hpl: { enabled: true, timeout: 2h }
      hpcg:
        enabled: true
        result_dir: /absolute/path/to/hpcg/results
        timeout: 3m
```

`hpcg.result_dir` 只用于核验本次新生成或发生变化的
`HPCG-Benchmark*.txt`，不用于定位可执行文件。

## 5. CLI 与 Web 验证

先逐项验证 CLI：

```bash
./bin/catmonitor health stress run --help
./bin/catmonitor health stress run --bench stream -c /path/to/catmonitor.yaml -o table
./bin/catmonitor health stress run --bench hpl -c /path/to/catmonitor.yaml -o table
./bin/catmonitor health stress run --bench hpcg -c /path/to/catmonitor.yaml -o json
```

CLI 验收通过后才启用 `web_enabled`。Web 服务必须绑定回环地址，远程访问使用
SSH 端口转发。页面应验证配置预检、单次缩短超时、运行/取消、刷新后表单保持
以及最新报告恢复。

## 6. 结果契约

- 正常结束：命令退出码为 0，且对应解析器取得必需结果。
- `time_limit_reached`：CATMonitor 到达配置窗口后主动停止，按通过聚合，
  允许没有最终性能数值。
- 提前非零退出或结果校验失败：`unhealthy`。
- 缺少脚本、执行器、输入文件或 HPCG 结果目录：`unavailable`。
- 用户停止：`cancelled`。

STREAM、HPL、HPCG 应逐项验收后再组合运行。压测结束后还应确认没有残留的
benchmark、MPI 启动器或同进程组子进程。
