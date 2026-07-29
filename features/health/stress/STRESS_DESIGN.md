# stress 子特性设计（STRESS_DESIGN）

`stress` 是 `health` 的子包，但与健康评分解耦。CLI 或 Web 将固定 benchmark 名称交给单作业 `Manager`，Manager 串行调用目标机器维护的 `benchmark_check.sh`，解析结果后原子写入 `stress-latest.json`。

Web 不接收脚本、路径、环境变量或 MPI/NUMA 参数，只允许选择 YAML 已启用且通过基础资产预检的项目和缩短单次超时。API 位于 `/api/health/stress/`，Web 仅在回环监听、回环来源及双开关启用时允许提交；启动/取消还要求 JSON、自定义操作头和浏览器同源校验。

STREAM、HPL 与 HPCG 的二进制、输入文件、运行目录和环境均直接固化在节点脚本中，Manager 只传 benchmark 名称。HPCG 的 YAML `result_dir` 仅供 Go 核验本次新结果文件。HPL 正常结束后解析标准结果行的规模、进程网格、时间和 GFLOPS，并拒绝非零 residual failure 或独立 `FAILED` 状态。

STREAM、HPL、HPCG 达到配置窗口且命令此前未退出报错时统一记录 `time_limit_reached`，聚合为 `Healthy`，不伪造最终性能值；提前非零退出或正常结束后解析失败才是 `unhealthy`。

HPCG 启动前记录结果目录中文件的大小、修改时间与 SHA-256，命令成功结束后强制从新增或发生变化的候选文件解析 VALID、GFLOP/s 与执行时间；stdout 不作为正常完成的替代数据源。报告使用同目录临时文件加 `os.Rename` 原子替换；初始落盘失败阻止作业启动，后续失败写入内存报告的 `report_error`。

## 相关文档

- [STRESS_SPEC.md](STRESS_SPEC.md)：功能与接口契约；
- [STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md)：自动化与实机验证；
- [STRESS_TEST_GUIDE.md](STRESS_TEST_GUIDE.md)：通用构建、测试和部署验收。
