# HealthScore JSON 字段说明

`catmonitor health -o json` 输出单个缩进 JSON 对象（不是数组、不是 JSONL）。对应 Go 类型 `features/health/health.go` 的 `HealthScore`。

## 顶层字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `score` | int | 0-100 总分，等于各部件 `score` 之和 |
| `grade` | string | `Excellent` / `Good` / `Warning` / `Critical` |
| `server_type` | string | `cpu_only`（无 GPU/NPU）或 `accelerated`（有 GPU/NPU） |
| `components` | object | 键为部件名（小写），值为 ComponentScore |
| `timestamp` | string | RFC3339 评估时间 |

## components（ComponentScore）

参与评估的部件键：`cpu`、`memory`、`disk`、`gpu`、`npu`、`network`、`chassis`。
GPU 与 NPU 只会出现其中一个（加速卡只算一类）；某部件采不到指标时可能缺失。

| 字段 | 类型 | 说明 |
|------|------|------|
| `score` | int | 该部件实得分（满额起步，逐条规则扣减，下限 0） |
| `max` | int | 该部件满额，随权重方案变化 |
| `deductions` | array | 触发的扣分项列表；未触发任何规则时为空数组或 null |

## deductions（Deduction）

| 字段 | 类型 | 说明 |
|------|------|------|
| `rule` | string | 规则名，如 `"temp>75C"`、`"usage>90%"`、`"uce_error"`、`"smart_failed"`、`"card_drop"` |
| `penalty` | float | 实际扣减分值（绝对值） |

规则名与阈值的完整对照见 [scoring-rules.md](./scoring-rules.md)。

## 示例（8 卡 NPU 服务器，accelerated_8card 方案）

```json
{
  "score": 94,
  "grade": "Good",
  "server_type": "accelerated",
  "components": {
    "cpu":     { "score": 12, "max": 15, "deductions": [ { "rule": "temp>75C", "penalty": 2 } ] },
    "memory":  { "score": 15, "max": 15, "deductions": [] },
    "disk":    { "score": 15, "max": 15, "deductions": [] },
    "npu":     { "score": 32, "max": 35, "deductions": [ { "rule": "temp>80C", "penalty": 3 } ] },
    "network": { "score": 10, "max": 10, "deductions": [] },
    "chassis": { "score": 10, "max": 10, "deductions": [] }
  },
  "timestamp": "2026-09-08T14:30:00Z"
}
```

> CLI 的 table 输出（`catmonitor health` 不带 `-o`）只展示 cpu/memory/disk/gpu/npu 五个部件的表格，network/chassis 不单独列出但计入 TOTAL。JSON 输出没有这个截断。

## 常见误读

- **`deductions: []` 不等于"硬件一定正常"**：指标采不到（无 BMC、非昇腾环境、权限不足）时规则不触发，视为满分。结合 `catmonitor collect` 确认指标是否真的采到了。
- **`score` 是各部件之和**：部件 `max` 之和恒为 100（无 BMC 时 chassis 满额并入 cpu，chassis 记 0）。
- **server_type 只有两档**：它不区分 GPU 和 NPU，也不反映卡数；具体卡数从 NPU 指标 `npu_num` 或权重方案推断。
