# health/stress 文档中心

本目录收纳 `health/stress` 子特性的部署、验收和迁移资料。功能规格与实现设计
仍位于特性根目录，避免操作手册反向定义产品行为。

## 1. 文档优先级

出现描述不一致时，按以下顺序判断：

1. [STRESS_SPEC.md](../STRESS_SPEC.md)：功能边界、配置、状态、报告和 Web API
   的规范来源；
2. [STRESS_DESIGN.md](../STRESS_DESIGN.md)：Manager、脚本、解析、持久化与安全
   设计；
3. [STRESS_TEST_GUIDE.md](../STRESS_TEST_GUIDE.md)：开发环境、自动化测试、构建
   和通用验收流程；
4. `deployment/`：特定 Linux 节点的资产路径、MPI/NUMA 参数和实际操作；
5. `testing/`：已经执行过的测试与实机结果；
6. `migration/`：旧项目分析和历史决策，仅用于追溯。

节点指南可以覆盖部署路径、运行参数和 YAML 超时，但不能改变 SPEC 定义的
支持范围或状态语义。

## 2. 文档分类

| 分类 | 文档 | 用途 | 状态 |
|---|---|---|---|
| 特性入口 | [README.md](../README.md) | 快速了解边界、命令和文档导航 | 当前 |
| 功能规格 | [STRESS_SPEC.md](../STRESS_SPEC.md) | 产品与接口契约 | 规范 |
| 实现设计 | [STRESS_DESIGN.md](../STRESS_DESIGN.md) | 代码结构和关键实现决策 | 规范 |
| 开发测试 | [STRESS_TEST_GUIDE.md](../STRESS_TEST_GUIDE.md) | WSL、本地测试、跨平台构建和通用验收 | 当前 |
| 节点部署 | [NODE_51_62_10_87_GUIDE.md](deployment/NODE_51_62_10_87_GUIDE.md) | ARM64、MPICH、统一 `/opt/catmonitor/benchmarks/runtime` 资产 | 当前实机 |
| 节点部署 | [NODE_51_62_10_90_GUIDE.md](deployment/NODE_51_62_10_90_GUIDE.md) | 鲲鹏 920、OpenMPI、`/root/haoran` 资产 | 已验证配置 |
| 验收记录 | [STRESS_ACCEPTANCE_RECORD.md](testing/STRESS_ACCEPTANCE_RECORD.md) | 本地及实机执行结果、剩余项目 | 滚动更新 |
| 迁移历史 | [LEGACY_MIGRATION_ANALYSIS.md](migration/LEGACY_MIGRATION_ANALYSIS.md) | 旧健康检查项目的功能分析 | 历史 |
| 迁移历史 | [MIGRATION_PLAN_AND_DECISIONS.md](migration/MIGRATION_PLAN_AND_DECISIONS.md) | 迁移阶段、状态映射和设计取舍 | 历史 |
| 归档 | [NODE_51_62_10_90_STREAM_EARLY_GUIDE.md](archive/NODE_51_62_10_90_STREAM_EARLY_GUIDE.md) | 51.62.10.90 仅 STREAM 阶段的早期说明 | 已被完整节点指南取代 |

## 3. 如何选择文档

### 修改代码或审查接口

依次阅读：

1. [STRESS_SPEC.md](../STRESS_SPEC.md)；
2. [STRESS_DESIGN.md](../STRESS_DESIGN.md)；
3. [STRESS_TEST_GUIDE.md](../STRESS_TEST_GUIDE.md)。

### 在新 Linux 节点部署

先执行 [STRESS_TEST_GUIDE.md](../STRESS_TEST_GUIDE.md) 中的通用预检，再选择
与目标机器最接近的节点指南作为模板。必须重新确认：

- benchmark 二进制和输入文件的完整路径；
- MPI 实现及版本；
- MPI rank、OpenMP 线程和 CPU/NUMA 绑定；
- HPL/HPCG 结果文件位置；
- 每项 YAML 运行上限。

不要直接复制另一台机器的 `benchmark_check.sh`。

### 查看当前验收进度

阅读 [STRESS_ACCEPTANCE_RECORD.md](testing/STRESS_ACCEPTANCE_RECORD.md)。操作步骤
仍以测试指南和对应节点指南为准，验收记录不复制完整命令。

### 追溯迁移决策

仅在需要理解旧脚本、旧状态或方案演变时阅读 `migration/`。其中可能保留已经
废弃的命令、目录或候选方案，不得作为当前部署依据。

## 4. 节点配置不得混用

| 项目 | 51.62.10.87 | 51.62.10.90 |
|---|---|---|
| 主要资产根目录 | `/opt/catmonitor/benchmarks/runtime` | `/root/haoran` |
| MPI | MPICH 4.1.3 | OpenMPI 4.1.5 |
| HPL | 8 MPI × 12 线程，约 421 秒 | 8 MPI × 12 线程，约 151 秒 |
| HPCG | 8 MPI × 1 线程，约 123 秒总耗时 | 96 MPI × 1 线程，约 62 秒 |
| STREAM 上限 | 1 分钟 | 1 分钟 |
| HPL 建议上限 | 10 分钟 | 4 分钟 |
| HPCG 建议上限 | 3 分钟 | 3 分钟 |

`benchmark_check.sh` 是节点适配边界。Go Manager 始终只传入固定 benchmark
名称，不理解上述机器差异。

## 5. 维护规则

- 功能行为变化：先更新 SPEC，再更新 DESIGN、测试和指南；
- 节点路径或参数变化：只更新对应节点指南和该节点脚本；
- 新增实测结果：更新 `testing/STRESS_ACCEPTANCE_RECORD.md`；
- 已完成的迁移阶段不从历史文档删除，只在顶部声明是否已过时；
- 不新建与现有节点指南重复的 STREAM/HPL/HPCG 单项手册；
- 文档中的配置示例必须保持 `health.stress` 层级；
- 第一版支持范围固定为 STREAM、HPL、HPCG，不记录 OSU 为当前能力。
