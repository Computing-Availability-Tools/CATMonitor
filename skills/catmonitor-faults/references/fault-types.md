# 故障类型详解（FaultType）

faultsub 检测 8 种故障类型（`GET /faultsub/types` 返回全集）。来源：[features/faultsub/event.go](../../../features/faultsub/event.go)。

| 类型 | 含义 | 典型 severity | 对应采集指标 |
|------|------|---------------|--------------|
| `card_drop` | NPU 掉卡 / 设备不在线 | critical | `npu.card_drop`（DCMI 设备健康包装） |
| `npu_health` | NPU 健康状态非 OK（Alarm/Critical） | critical | `npu.health_status` |
| `npu_error_code` | 设备上报错误码 | critical | `npu.error_code`（dcmi_get_device_errorcode_v2） |
| `hbm_uce` | HBM 双 bit ECC（不可纠正） | critical | `npu.hbm_double_ecc` |
| `ddr_uce` | DDR 双 bit ECC（不可纠正） | critical | `npu.ddr_double_ecc` |
| `roce_link_down` | RoCE 链路 down / 不健康 | critical | `npu.roce_link_status` / `roce_link_health` |
| `driver_unhealthy` | NPU 驱动健康状态非零 | warning | `npu.driver_health` |
| `straggler_detected` | 慢节点检测命中（**外部注入**） | warning | 非采集指标，由外部 straggler 检测器 POST 注入 |

## severity

只有两档：`warning`、`critical`。订阅时可设 `min_severity` 过滤（空 = 接受全部）。
UCE 类（hbm_uce/ddr_uce）与掉卡属严重硬件事件，通常建议按 critical 处理。

## 事件产生机制（变迁驱动）

- FaultDetector 对每轮采集的 NPU 指标做规则判定，维护每个 `(npu_id, type)` 的状态。
- **新出现**故障 → 产生 `recovered: false` 事件；故障**恢复** → 产生 `recovered: true` 事件。
- 持续存在的故障**不会**重复产生事件（订阅侧另有 `debounce_ms` 抑制窗口）。
- 外部检测器（如 straggler 慢节点检测）通过 `POST /faultsub/events` 注入事件，走同一分发管道，但**不进入**活跃故障快照。

## 检测规则开关

`catmonitor.yaml` 的 `faultsub.rules.*` 可独立开关（unset = 启用）。示例配置中 `driver_unhealthy: false` 被关闭。查询/订阅前如需确认，让用户查看其配置文件的 `faultsub.rules` 段。

## 处置建议（供报告参考）

| 类型 | 建议动作 |
|------|----------|
| `card_drop` | 定位物理卡与节点，检查驱动日志，必要时换卡/隔离节点 |
| `hbm_uce` / `ddr_uce` | 不可纠正 ECC，硬件劣化信号，建议隔离设备并安排更换 |
| `npu_health` Alarm | 结合 `detail` 中的健康值与错误码判断，通常需检修 |
| `roce_link_down` | 检查线缆/交换机端口/网卡，链路层排障 |
| `npu_error_code` | 按昇腾错误码手册定位具体含义 |
| `driver_unhealthy` | 检查 NPU 驱动状态（npu-smi info），考虑重启驱动或节点 |
| `straggler_detected` | 结合 straggler KPI 数据（stragglerout 特性）分析慢节点根因 |
