# CATMonitor 可靠性压测

Stress 显式触发 STREAM、HPL、HPCG 和 Ascend NPU Burn，不作为周期健康检查自动运行，
结果不直接计入健康总分。压测会占用 CPU、内存、MPI/NUMA 或 NPU 资源。

## 文档入口

| 需求 | 文档 |
| --- | --- |
| 获取当前配套镜像并部署 | [候选交付指南](../../docker/STRESS_JOB_RELEASE_GUIDE.md) |
| 按硬件选择 Monitoring / CPU / NPU / Full | [容器部署导航](../../docker/README.md) |
| CLI/Web 参数、作业、报告、迁移和故障排查 | [用户指南](STRESS_USER_GUIDE.md) |
| 模型参数、参考和 SDC | [模型指南](../../docker/NPU_MODEL_SDC_GUIDE.md) |
| 构建、受限网络、离线依赖和发布 | [开发者指南](../../docker/DEVELOPER_GUIDE.md) |
| 功能与协议契约 | [规格](STRESS_SPEC.md) |
| Controller/Executor、状态机与安全 | [设计](STRESS_DESIGN.md) |
| 自动化及实机验收范围 | [测试指南](STRESS_TEST_GUIDE.md) |
| 开源合规 | [发布审计](OSS_RELEASE_AUDIT.md)、[第三方声明](THIRD_PARTY_NOTICES.md) |

## 唯一运行链路

```text
CLI / Web :19322
        → /run/catmonitor/control.sock
        → daemon Stress Controller
        → Docker exec → catmonitor-stress-exec → typed workload plugin
```

daemon 是唯一作业所有者，负责互斥、参数冻结、超时、取消和报告。Web/DFeE 不挂
Docker socket；control.sock 仅是前端控制平面，不是旧 CPU/NPU Unix Runner。
只接受类型化作业参数，不允许自定义命令、环境变量、脚本或容器绑定。
当前 Web Run/Cancel 尚无认证/RBAC，应限制可信网络。

Monitoring-only 无需 Stress 容器或 socket。CPU Stress 增加一个 CPU workload，
NPU Stress 增加一个 NPU workload；Full 共五个容器。构建/预检通过、算子准入和
真实 workload 通过是不同结论；未执行硬件场景保持 NOT_RUN/PENDING。
