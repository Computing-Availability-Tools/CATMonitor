# 测试目录说明

CATMonitor 遵循 Go 的标准测试组织方式：

- 单元测试使用 `*_test.go`，与被测 package 放在同一目录；
- package 私有函数的测试必须保持同 package，例如
  `cmd/catmonitor/main_test.go` 用于验证 CLI 内部退出码；
- `tests/testdata` 保存多个 package 可复用的模拟 `/proc`、`/sys` 和命令输出；
- 单个 package 专用夹具优先放在该 package 的 `testdata` 子目录；
- 具体节点地址、资产路径、性能数值和实机验收报告不进入本目录。

执行全部 Go 测试：

```bash
go test ./...
```

需要 Linux 信号、进程组或符号链接语义的测试，应在原生 Linux 文件系统中
执行最终验收。
