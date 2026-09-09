# CATMonitor stragglerout 特性 User Story

> **文档定位**：stragglerout 特性（`features/stragglerout/`）的用户故事清单，作为测试设计的输入需求。
>
> **配套产物**：`tests/e2e/stragglerout_testcases.xmind`（测试点）、`tests/e2e/stragglerout_testcases.xlsx`（可执行用例）。
>
> **素材来源**：由 `features/stragglerout/stragglerout_SPEC.md`（v0.3.6 刷新版）反向改写为用户视角，经评审定稿。与原始需求冲突时以原始需求为准。

---

## SG-US-001 输出日级 KPI 时序文件

作为 straggler 慢节点检测器的使用者，我希望 CATMonitor 直接产出专用 KPI 文件，以便替代自带的 kpi_collect.sh 脚本。

**业务规则**：
1. 按日写 `{data_dir}/straggler_kpi_{date}.jsonl`，每行一个时刻的聚合样本；
2. `vals` 按"每时刻×每芯片"组织，键为全局设备号；
3. 字段与 straggler CSVRow 1:1 对应；
4. 计数器写原始累计值不做 delta。

## SG-US-002 双芯片设备号自算

作为 A3 双芯片平台的用户，我希望每张卡的两个芯片各占一个设备号且编号稳定，以便与 npu-smi 编号一致。

**业务规则**：
1. `device_id = npu_id × chips_per_card + chip_id`；
2. chips_per_card=历史最大 chip_id+1，跨批次只增不减；
3. 中间掉卡时编号保持稳定（保留空洞不压缩）；
4. 采集器输出的 NPU 指标（含 hccn_tool 网络指标）已统一携带 chip_id，全部按芯片键控；仅当指标既无 chip_id 也无显式 device_id 标签时才回退按 npu_id 键（罕见兜底）；
5. 显式 device_id 标签优先采用。

## SG-US-003 指标映射与别名

作为数据消费方，我希望 11 个 KPI 字段稳定映射，以便 JSON reader 直接重建时序数据。

**业务规则**：
1. temp/power/aicore_freq/aicore_util/hbm_util/tx_bandwidth/rx_pfc_pkt/roce_tx_err_pkt/roce_out_of_order/roce_new_pkt_rty 十字段映射固定；
2. roce_new_pkt_rty 主名为 `roce_new_pkt_rty_num`（别名 roce_new_pkt_rty / roce_retrans_pkt_num / roce_rx_retrans_pkt_num，按序取第一个命中）；
3. cpu_avg 按 cpu 标签聚合、忽略 total。

## SG-US-004 缓冲与周期落盘

作为 daemon 维护者，我希望 KPI 样本先缓冲再周期 flush，以便不拖慢采集管道。

**业务规则**：
1. flush_interval（默认 60s）周期落盘；
2. 非相关批次（无 NPU KPI 且无 CPU usage）不产出。

## SG-US-005 保留期清理

作为磁盘空间的管理者，我希望过期 KPI 文件自动清理，以便 data_dir 不无限增长。

**业务规则**：
1. retention（默认 15 天）删除 mtime 过期的日级文件；
2. 清理机会式执行（每小时至多一次）。

## SG-US-006 默认关闭零回归

作为 CATMonitor 维护者，我希望 straggler_output 关闭时无 KPI 文件产生，以便渐进采用。

**业务规则**：
1. enabled=false（默认）时不写任何文件、daemon 零回归；
2. 启用时 Storage 链路：Scheduler→StragglerStorage→CachingStorage→JSONL。
